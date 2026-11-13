package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func fixture(t *testing.T) Manager {
	t.Helper()
	dir := t.TempDir()
	project := filepath.Join(dir, "workspace")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(dir, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("GEMINI_CLI_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "appdata"))
	return Manager{Home: dir, Project: project, StateDir: filepath.Join(dir, "manager")}
}
func getTarget(t *testing.T, m Manager, id, scope string) Target {
	t.Helper()
	for _, target := range targets(m.Home, m.Project) {
		if target.ID == id && target.Scope == scope {
			return target
		}
	}
	t.Fatalf("target %s %s unavailable", id, scope)
	return Target{}
}
func getRow(t *testing.T, m Manager, target Target, name string) Registration {
	t.Helper()
	rows, warnings := m.List([]Target{target})
	if len(warnings) > 0 {
		t.Fatal(warnings)
	}
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("missing %s", name)
	return Registration{}
}
func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := atomicWrite(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func playwright() Definition {
	return Definition{Name: "playwright", Transport: "stdio", Command: "npx", Args: []string{"-y", "@playwright/mcp@latest"}}
}
func TestAllClientLifecycles(t *testing.T) {
	m := fixture(t)
	for _, target := range targets(m.Home, m.Project) {
		t.Run(target.String(), func(t *testing.T) {
			spec := playwright()
			if _, err := m.Add([]Target{target}, spec, nil); err != nil {
				t.Fatal(err)
			}
			row := getRow(t, m, target, spec.Name)
			if row.State != "Registered" || row.Transport != "stdio" {
				t.Fatal(row.State, row.Transport)
			}
			if err := m.Act(row, "disable", Definition{}); err != nil {
				t.Fatal(err)
			}
			row = getRow(t, m, target, spec.Name)
			if row.State == "Registered" {
				t.Fatal("did not disable")
			}
			if err := m.Act(row, "enable", Definition{}); err != nil {
				t.Fatal(err)
			}
			row = getRow(t, m, target, spec.Name)
			if row.State != "Registered" || !includes(stringsValue(row.entry["args"]), "@playwright/mcp@latest") {
				t.Fatal("resume lost configuration")
			}
			if err := m.Act(row, "remove", Definition{}); err != nil {
				t.Fatal(err)
			}
			rows, warnings := m.List([]Target{target})
			if len(rows) != 0 || len(warnings) != 0 {
				t.Fatal(rows, warnings)
			}
		})
	}
}
func TestAutoInitializeVSCodeSettings(t *testing.T) {
	utf16Data := []byte{0xff, 0xfe}
	for _, unit := range utf16.Encode([]rune(`{"keep":"✨"}`)) {
		utf16Data = binary.LittleEndian.AppendUint16(utf16Data, unit)
	}
	for _, tc := range []struct {
		name  string
		input []byte
		keep  string
	}{
		{"missing parent and file", nil, ""},
		{"zero bytes", []byte{}, ""},
		{"whitespace", []byte("  \r\n\t"), ""},
		{"UTF-8 BOM only", []byte{0xef, 0xbb, 0xbf}, ""},
		{"line comment only", []byte("// preserve comment"), "preserve comment"},
		{"block comment only", []byte("/* preserve comment */"), "preserve comment"},
		{"unset servers", []byte(`{"servers":null,"keep":true}`), "keep"},
		{"UTF-16 file", utf16Data, "✨"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixture(t)
			vs := getTarget(t, m, "vscode", "User")
			if tc.input != nil {
				writeFixture(t, vs.Path, tc.input)
			}
			if _, err := m.Add([]Target{vs}, playwright(), nil); err != nil {
				t.Fatal(err)
			}
			r := getRow(t, m, vs, "playwright")
			if r.entry["type"] != "stdio" || r.entry["command"] != "npx" {
				t.Fatal("wrong VS Code entry")
			}
			raw := readFixture(t, vs.Path)
			if tc.keep != "" && !strings.Contains(string(raw), tc.keep) {
				t.Fatal("lost existing data")
			}
			if tc.name == "UTF-8 BOM only" && !bytes.HasPrefix(raw, tc.input) {
				t.Fatal("UTF-8 BOM lost")
			}
		})
	}
}
func TestRecoveryRequiresMatchingApprovalAndBackup(t *testing.T) {
	m := fixture(t)
	vs, codex := getTarget(t, m, "vscode", "User"), getTarget(t, m, "codex", "User")
	raw := []byte("{\n  \"servers\": {\n  \"private\": ???\n}")
	writeFixture(t, vs.Path, raw)
	batch := []Target{codex, vs}
	_, err := m.Add(batch, playwright(), nil)
	var issue *RecoveryNeeded
	if !errors.As(err, &issue) || issue.Target.ID != "vscode" || !strings.Contains(issue.Error(), "line 3") {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
	if !bytes.Equal(readFixture(t, vs.Path), raw) {
		t.Fatal("modified malformed file")
	}
	if _, err := os.Stat(codex.Path); !os.IsNotExist(err) {
		t.Fatal("batch preflight wrote a config")
	}
	writeFixture(t, vs.Path, append(bytes.Clone(raw), '\n'))
	approval := map[string]*ConfigError{vs.Path: issue.Issue}
	if _, err := m.Add(batch, playwright(), approval); !errors.As(err, &issue) {
		t.Fatalf("accepted stale recovery approval: %v", err)
	}
	writeFixture(t, vs.Path, raw)
	backups, err := m.Add(batch, playwright(), approval)
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	if !bytes.Equal(readFixture(t, backups[0]), raw) {
		t.Fatal("backup differs from original")
	}
	getRow(t, m, vs, "playwright")
	getRow(t, m, codex, "playwright")
}
func TestRecoverWrongServerCollectionPreservesUnrelatedSettings(t *testing.T) {
	m := fixture(t)
	vs := getTarget(t, m, "vscode", "User")
	raw := []byte(`{"servers":[],"inputs":[{"id":"keep","type":"promptString"}]}`)
	writeFixture(t, vs.Path, raw)
	_, err := m.Add([]Target{vs}, playwright(), nil)
	var issue *RecoveryNeeded
	if !errors.As(err, &issue) {
		t.Fatal(err)
	}
	backups, err := m.Add([]Target{vs}, playwright(), map[string]*ConfigError{vs.Path: issue.Issue})
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	if !bytes.Contains(readFixture(t, vs.Path), []byte("\"keep\"")) {
		t.Fatal("removed unrelated settings")
	}
}
func TestAuthPreservationAndConnectionChange(t *testing.T) {
	m := fixture(t)
	codex, cursor := getTarget(t, m, "codex", "User"), getTarget(t, m, "cursor", "User")
	writeFixture(t, cursor.Path, []byte("{\n // keep comment\n \"mcpServers\":{\"existing\":{\"command\":\"npx\"}},\n}"))
	t.Setenv("MCP_TOKEN", "secret-sentinel-never-store")
	d := Definition{Name: "remote", Transport: "http", URL: "https://example.com/mcp", Bearer: "MCP_TOKEN", Headers: map[string]string{"X-API-Key": "KEY"}}
	if _, err := m.Add([]Target{codex, cursor}, d, nil); err != nil {
		t.Fatal(err)
	}
	raw := string(readFixture(t, cursor.Path))
	if !strings.Contains(raw, "keep comment") || !strings.Contains(raw, "Bearer ${env:MCP_TOKEN}") || strings.Contains(raw, "secret-sentinel") {
		t.Fatal("config did not preserve comments and environment references")
	}
	r := getRow(t, m, codex, d.Name)
	if err := m.Act(r, "disable", Definition{}); err != nil {
		t.Fatal(err)
	}
	r = getRow(t, m, codex, d.Name)
	change := Definition{URL: "https://other.example/mcp"}
	if err := m.Act(r, "connection", change); err == nil {
		t.Fatal("allowed credentials to follow changed destination")
	}
	change.ResetAuth = true
	if err := m.Act(r, "connection", change); err != nil {
		t.Fatal(err)
	}
	r = getRow(t, m, codex, d.Name)
	if r.entry["bearer_token_env_var"] != nil || r.entry["url"] != change.URL || r.State != "Disabled" {
		t.Fatal("connection edit removed or preserved the wrong fields")
	}
}
func TestProbeWithEnvironment(t *testing.T) {
	t.Setenv("TEST_TOKEN", "my-secret")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-secret" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2025-11-25\"}}\n\n")
	}))
	defer s.Close()
	r := Registration{Transport: "http", entry: object{"url": s.URL, "headers": object{"Authorization": "Bearer ${env:TEST_TOKEN}"}}}
	if result, err := probe(context.Background(), r); err != nil || !strings.Contains(result, "succeeded") {
		t.Fatal(result, err)
	}
}
