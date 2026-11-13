package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	profileMagic   = "MCPD\r\n\x1a\n"
	profileVersion = 1
	profileLimit   = 32 << 20
	// Allow the small overhead of zlib for incompressible data.
	profileCompressedLimit = profileLimit + (64 << 10)
	profileHeaderSize      = 52
	profileExtension       = ".mcpd"
)

func encodeProfile(p Profile) ([]byte, error) {
	if p.Schema != profileSchema || len(p.Destinations) == 0 {
		return nil, errors.New("profile must contain MCPDashboard destinations")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode profile: %w", err)
	}
	if len(data) > profileLimit {
		return nil, errors.New("profile exceeds the 32 MiB decoded size limit")
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, err = writer.Write(data)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("compress profile: %w", err)
	}
	payload := compressed.Bytes()
	if len(payload) > profileCompressedLimit {
		return nil, errors.New("compressed profile exceeds its size limit")
	}

	file := make([]byte, profileHeaderSize, profileHeaderSize+len(payload))
	copy(file, profileMagic)
	binary.BigEndian.PutUint16(file[8:10], profileVersion)
	// Bytes 10–11 are reserved flags. Version 1 writes zero.
	binary.BigEndian.PutUint32(file[12:16], uint32(len(payload)))
	binary.BigEndian.PutUint32(file[16:20], uint32(len(data)))
	hash := sha256.Sum256(payload)
	copy(file[20:profileHeaderSize], hash[:])
	return append(file, payload...), nil
}

func saveProfile(folder string, p Profile) (string, error) {
	path := filepath.Join(folder, "mcpdashboard-profile-"+time.Now().UTC().Format("20060102T150405.000000000Z")+profileExtension)
	if err := writeProfile(path, p, false); err != nil {
		return "", err
	}
	return path, nil
}

func writeProfile(path string, p Profile, overwrite bool) error {
	data, err := encodeProfile(p)
	if err != nil {
		return err
	}
	if overwrite {
		info, err := os.Lstat(path)
		switch {
		case err == nil:
			if !info.Mode().IsRegular() {
				return errors.New("the output to replace must be a regular file")
			}
			if err := atomicWrite(path, data, 0600); err != nil {
				return fmt.Errorf("replace profile: %w", err)
			}
			return nil
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("check profile output: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write profile: %w", err)
	}
	return nil
}

func readProfile(r io.Reader) (Profile, error) {
	var header [profileHeaderSize]byte
	if _, err := io.ReadFull(r, header[:len(profileMagic)]); err != nil {
		return Profile{}, errors.New("the file is too short to contain an MCPDashboard profile signature")
	}
	if string(header[:len(profileMagic)]) != profileMagic {
		return Profile{}, errors.New("this file does not have the MCPDashboard .mcpd signature")
	}
	if _, err := io.ReadFull(r, header[len(profileMagic):]); err != nil {
		return Profile{}, errors.New("the MCPDashboard profile header is incomplete")
	}
	if version := binary.BigEndian.Uint16(header[8:10]); version != profileVersion {
		return Profile{}, fmt.Errorf("MCPDashboard profile format version %d is unsupported", version)
	}
	if binary.BigEndian.Uint16(header[10:12]) != 0 {
		return Profile{}, errors.New("this MCPDashboard profile uses unsupported format flags")
	}
	encodedSize := binary.BigEndian.Uint32(header[12:16])
	decodedSize := binary.BigEndian.Uint32(header[16:20])
	if encodedSize == 0 || encodedSize > profileCompressedLimit || decodedSize == 0 || decodedSize > profileLimit {
		return Profile{}, errors.New("the profile declares an invalid payload size")
	}
	payload, err := io.ReadAll(io.LimitReader(r, int64(encodedSize)+1))
	if err != nil {
		return Profile{}, fmt.Errorf("read profile payload: %w", err)
	}
	if len(payload) < int(encodedSize) {
		return Profile{}, errors.New("the profile is incomplete")
	}
	if len(payload) > int(encodedSize) {
		return Profile{}, errors.New("the profile contains unexpected trailing data")
	}
	hash := sha256.Sum256(payload)
	if !bytes.Equal(hash[:], header[20:]) {
		return Profile{}, errors.New("the profile checksum does not match; the file may be damaged")
	}
	source := bytes.NewReader(payload)
	inflater, err := zlib.NewReader(source)
	if err != nil {
		return Profile{}, errors.New("the profile payload is not valid zlib data")
	}
	raw, err := io.ReadAll(io.LimitReader(inflater, int64(decodedSize)+1))
	_ = inflater.Close()
	if err != nil {
		return Profile{}, errors.New("the profile payload could not be decompressed")
	}
	if len(raw) != int(decodedSize) || source.Len() != 0 {
		return Profile{}, errors.New("the decoded profile does not match its declared size")
	}
	return decodeProfileJSON(raw)
}

func readProfileFile(filename string, r io.Reader) (Profile, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case profileExtension:
		return readProfile(r)
	case ".json":
		return readLegacyProfile(r)
	default:
		return Profile{}, errors.New("select an .mcpd profile or an older MCPDashboard .json export")
	}
}

func readLegacyProfile(r io.Reader) (Profile, error) {
	data, err := io.ReadAll(io.LimitReader(r, profileLimit+1))
	if err != nil {
		return Profile{}, fmt.Errorf("read legacy JSON profile: %w", err)
	}
	if len(data) > profileLimit {
		return Profile{}, errors.New("legacy profile exceeds 32 MiB")
	}
	return decodeProfileJSON(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
}

func decodeProfileJSON(data []byte) (Profile, error) {
	var p Profile
	if !utf8.Valid(data) {
		return p, errors.New("the profile data is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, errors.New("the file does not contain valid MCPDashboard profile data")
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return p, errors.New("unexpected data after the profile")
	}
	if p.Schema != profileSchema {
		return p, errors.New("unsupported MCPDashboard profile schema")
	}
	if len(p.Destinations) == 0 {
		return p, errors.New("the profile contains no destinations")
	}
	return p, nil
}
