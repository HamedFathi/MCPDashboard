package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
)

type object = map[string]any

type ConfigError struct {
	Path, Detail string
	Digest       [32]byte
}

func (e *ConfigError) Error() string { return fmt.Sprintf("%s: %s", e.Path, e.Detail) }

type config struct {
	file                string
	raw, document       []byte
	mode                fs.FileMode
	toml, recovery, bom bool
	original, values    object
}

var jsonLocation = regexp.MustCompile(`line [0-9]+, column [0-9]+`)

func decodedDocument(raw []byte) ([]byte, bool, error) {
	bom := bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if bom {
		raw = raw[3:]
	}
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		order = binary.LittleEndian
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		order = binary.BigEndian
	}
	if order == nil {
		if !utf8.Valid(raw) {
			return nil, false, errors.New("invalid UTF-8; save the file as UTF-8 or recover it from a backup")
		}
		return raw, bom, nil
	}
	input := raw[2:]
	if len(input)%2 != 0 {
		return nil, false, errors.New("incomplete UTF-16 data")
	}
	runes := make([]uint16, len(input)/2)
	for i := range runes {
		runes[i] = order.Uint16(input[2*i:])
	}
	for i := 0; i < len(runes); i++ {
		unit := runes[i]
		switch {
		case unit >= 0xd800 && unit <= 0xdbff:
			if i+1 >= len(runes) || runes[i+1] < 0xdc00 || runes[i+1] > 0xdfff {
				return nil, false, errors.New("invalid UTF-16 sequence")
			}
			i++
		case unit >= 0xdc00 && unit <= 0xdfff:
			return nil, false, errors.New("invalid UTF-16 sequence")
		}
	}
	return []byte(string(utf16.Decode(runes))), false, nil
}

func onlyJSONCComments(data []byte) bool {
	for data = bytes.TrimSpace(data); len(data) > 0; data = bytes.TrimSpace(data) {
		switch {
		case bytes.HasPrefix(data, []byte("//")):
			n := bytes.IndexAny(data, "\r\n")
			if n < 0 {
				return true
			}
			data = data[n+1:]
		case bytes.HasPrefix(data, []byte("/*")):
			n := bytes.Index(data[2:], []byte("*/"))
			if n < 0 {
				return false
			}
			data = data[n+4:]
		default:
			return false
		}
	}
	return true
}

func loadConfig(path string, isTOML bool, approved *ConfigError) (*config, error) {
	file := path
	if resolved, err := filepath.EvalSymlinks(file); err == nil {
		file = resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else if info, err := os.Lstat(file); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("configuration is a broken symlink: %s", file)
	}
	raw, mode, err := snapshot(file)
	if err != nil {
		return nil, err
	}
	c := &config{file: file, raw: raw, mode: mode, toml: isTOML}
	doc, bom, decodeErr := decodedDocument(raw)
	c.document, c.bom = doc, bom
	if decodeErr == nil {
		if !isTOML && onlyJSONCComments(doc) {
			if len(bytes.TrimSpace(doc)) == 0 {
				c.document = []byte("{}\n")
			} else {
				c.document = append(append(bytes.Clone(doc), '\n'), []byte("{}\n")...)
			}
		}
		c.original, decodeErr = parseConfig(c.document, isTOML)
	}
	if decodeErr != nil {
		issue := &ConfigError{Path: file, Detail: decodeErr.Error(), Digest: sha256.Sum256(raw)}
		if approved == nil || approved.Path != file || approved.Digest != issue.Digest {
			return nil, issue
		}
		c.document, c.bom, c.recovery = nil, false, true
		c.original, c.values = object{}, object{}
		return c, nil
	}
	c.values, err = parseConfig(c.document, isTOML)
	return c, err
}

func parseConfig(data []byte, isTOML bool) (object, error) {
	result := object{}
	if isTOML {
		if err := toml.Unmarshal(data, &result); err != nil {
			var decode *toml.DecodeError
			if errors.As(err, &decode) {
				line, col := decode.Position()
				return nil, fmt.Errorf("invalid TOML at line %d, column %d", line, col)
			}
			return nil, errors.New("invalid TOML configuration")
		}
	} else {
		v, err := hujson.Parse(bytes.Clone(data))
		if err != nil {
			if loc := jsonLocation.FindString(err.Error()); loc != "" {
				return nil, fmt.Errorf("invalid JSON configuration at %s", loc)
			}
			return nil, errors.New("invalid JSON configuration")
		}
		v.Standardize()
		dec := json.NewDecoder(bytes.NewReader(v.Pack()))
		dec.UseNumber()
		if err = dec.Decode(&result); err != nil || result == nil {
			return nil, errors.New("configuration must be a JSON object")
		}
	}
	return result, nil
}

func snapshot(file string) ([]byte, fs.FileMode, error) {
	info, err := os.Stat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0600, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("not a regular file: %s", file)
	}
	if info.Size() > 32<<20 {
		return nil, 0, fmt.Errorf("configuration exceeds 32 MiB: %s", file)
	}
	data, err := os.ReadFile(file)
	if data == nil && err == nil {
		data = []byte{}
	}
	return data, info.Mode().Perm(), err
}

func child(parent object, key string, create bool) (object, error) {
	if v, exists := parent[key]; exists {
		if m, ok := v.(map[string]any); ok && m != nil {
			return m, nil
		}
		return nil, fmt.Errorf("%s must be an object", key)
	}
	m := object{}
	if create {
		parent[key] = m
	}
	return m, nil
}

func stringValue(m object, key string) string { v, _ := m[key].(string); return v }
func stringsValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func jsonDiff(path string, before, after object, ops *[]patchOp) {
	keys := make([]string, 0, len(before)+len(after))
	for k := range before {
		keys = append(keys, k)
	}
	for k := range after {
		if _, exists := before[k]; !exists {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	escape := strings.NewReplacer("~", "~0", "/", "~1")
	for _, k := range keys {
		a, hasA := before[k]
		b, hasB := after[k]
		field := path + "/" + escape.Replace(k)
		switch {
		case !hasB:
			*ops = append(*ops, patchOp{"remove", field, nil})
		case !hasA:
			*ops = append(*ops, patchOp{"add", field, b})
		case !reflect.DeepEqual(a, b):
			aMap, okA := a.(map[string]any)
			bMap, okB := b.(map[string]any)
			if okA && okB {
				jsonDiff(field, aMap, bMap, ops)
			} else {
				*ops = append(*ops, patchOp{"replace", field, b})
			}
		}
	}
}

func (c *config) encode() ([]byte, error) {
	var data []byte
	var err error
	switch {
	case c.toml:
		data, err = toml.Marshal(c.values)
	case c.document == nil:
		data, err = json.MarshalIndent(c.values, "", "  ")
		data = append(data, '\n')
	default:
		var v hujson.Value
		v, err = hujson.Parse(bytes.Clone(c.document))
		if err != nil {
			return nil, err
		}
		var ops []patchOp
		jsonDiff("", c.original, c.values, &ops)
		patch, err := json.Marshal(ops)
		if err != nil {
			return nil, err
		}
		if err = v.Patch(patch); err != nil {
			return nil, err
		}
		v.Format()
		data = v.Pack()
	}
	if c.bom {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}
	return data, err
}

func atomicWrite(file string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".mcp-manager-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return replaceFile(tmp.Name(), file)
}
