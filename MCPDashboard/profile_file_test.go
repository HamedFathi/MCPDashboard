package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fileTestProfile() Profile {
	return Profile{
		Schema: profileSchema,
		Destinations: []ProfileDestination{{
			Client: "vscode", Scope: "User", Format: "json",
			Active: `{"servers":{"docs":{"type":"http","url":"https://example.com/mcp"}}}`,
		}},
	}
}

func TestProfileFileRoundTripAndLegacy(t *testing.T) {
	profile := fileTestProfile()
	file, err := encodeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(file, []byte(profileMagic)) || json.Valid(file) {
		t.Fatal("export was not an MCPD binary container")
	}
	got, err := readProfileFile("config.MCPD", bytes.NewReader(file))
	if err != nil || !reflect.DeepEqual(got, profile) {
		t.Fatalf("binary round trip: %#v, %v", got, err)
	}

	legacy, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	got, err = readProfileFile("earlier-export.json", bytes.NewReader(legacy))
	if err != nil || !reflect.DeepEqual(got, profile) {
		t.Fatalf("legacy round trip: %#v, %v", got, err)
	}
	if _, err := readProfileFile("renamed.mcpd", bytes.NewReader(legacy)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("accepted renamed JSON as a binary profile: %v", err)
	}
	if _, err := readProfileFile("renamed.txt", bytes.NewReader(file)); err == nil {
		t.Fatal("accepted an unsupported file extension")
	}
}

func TestProfileFileRejectsMalformedContainer(t *testing.T) {
	file, err := encodeProfile(fileTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, message string
		edit          func([]byte) []byte
	}{
		{"bad signature", "signature", func(data []byte) []byte { data[0] = 'X'; return data }},
		{"short header", "header", func(data []byte) []byte { return data[:profileHeaderSize-1] }},
		{"future version", "version 2", func(data []byte) []byte { binary.BigEndian.PutUint16(data[8:10], 2); return data }},
		{"unknown flags", "flags", func(data []byte) []byte { data[11] = 1; return data }},
		{"oversize compressed", "payload size", func(data []byte) []byte {
			binary.BigEndian.PutUint32(data[12:16], profileCompressedLimit+1)
			return data
		}},
		{"oversize decoded", "payload size", func(data []byte) []byte { binary.BigEndian.PutUint32(data[16:20], profileLimit+1); return data }},
		{"truncated payload", "incomplete", func(data []byte) []byte { return data[:len(data)-1] }},
		{"corrupt payload", "checksum", func(data []byte) []byte { data[len(data)-1] ^= 1; return data }},
		{"trailing bytes", "trailing data", func(data []byte) []byte { return append(data, 0) }},
		{"decoded size mismatch", "declared size", func(data []byte) []byte { binary.BigEndian.PutUint32(data[16:20], 1); return data }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := readProfile(bytes.NewReader(test.edit(bytes.Clone(file))))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q, got %v", test.message, err)
			}
		})
	}
}

func TestProfileFileRejectsHiddenTrailingCompressedData(t *testing.T) {
	file, err := encodeProfile(fileTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	// An additional zlib stream cannot be smuggled in after the declared profile.
	extra := bytes.Buffer{}
	writer := zlib.NewWriter(&extra)
	if _, err := writer.Write([]byte("extra stream")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file = append(file, extra.Bytes()...)
	binary.BigEndian.PutUint32(file[12:16], uint32(len(file)-profileHeaderSize))
	hash := sha256.Sum256(file[profileHeaderSize:])
	copy(file[20:profileHeaderSize], hash[:])
	if _, err := readProfile(bytes.NewReader(file)); err == nil {
		t.Fatal("accepted trailing compressed data")
	}
}

func TestBundledMCPDExample(t *testing.T) {
	file, err := os.Open("examples/microsoft-learn.mcpd")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	profile, err := readProfileFile(file.Name(), file)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := fixture(t).planProfile(profile)
	if err != nil || len(plan) != 2 {
		t.Fatalf("example import plan: %v, %v", plan, err)
	}
	for _, item := range plan {
		servers, err := child(item.active, item.target.Key, false)
		if err != nil || len(servers) != 1 {
			t.Fatalf("example definitions: %v, %v", servers, err)
		}
		server, ok := servers["microsoft-learn"].(map[string]any)
		if !ok || stringValue(server, "url") != "https://learn.microsoft.com/api/mcp" {
			t.Fatal("example endpoint changed")
		}
	}
}
