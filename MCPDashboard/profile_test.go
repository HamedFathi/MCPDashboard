package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileRoundTripAndRepeatedImport(t *testing.T) {
	source := fixture(t)
	vs := getTarget(t, source, "vscode", "User")
	codex := getTarget(t, source, "codex", "User")
	cursor := getTarget(t, source, "cursor", "Workspace")
	claude := getTarget(t, source, "claude", "User")
	writeFixture(t, vs.Path, []byte(`{"editorSetting":true,"inputs":[{"id":"token","type":"promptString"}]}`))
	writeFixture(t, codex.Path, []byte("[mcp_servers.typed]\ncommand='npx'\nstartup_timeout_sec=23\nenabled=false\n"))
	if _, err := source.Add([]Target{vs, cursor, claude}, playwright(), nil); err != nil {
		t.Fatal(err)
	}
	for _, target := range []Target{cursor, claude} {
		if err := source.Act(getRow(t, source, target, "playwright"), "disable", Definition{}); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := source.exportProfile()
	if err != nil || len(profile.Destinations) != 4 {
		t.Fatal(len(profile.Destinations), err)
	}
	path, err := saveProfile(t.TempDir(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != profileExtension {
		t.Fatalf("unexpected export extension: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	profile, err = readProfile(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), source.Project) || strings.Contains(string(content), "editorSetting") {
		t.Fatal("profile contains source paths or unrelated settings")
	}
	destination := fixture(t)
	project := destination.Project
	destination.Project = ""
	if _, err := destination.planProfile(profile); err == nil || !strings.Contains(err.Error(), "choose a workspace") {
		t.Fatalf("workspace was not required: %v", err)
	}
	destination.Project = project
	newVS := getTarget(t, destination, "vscode", "User")
	writeFixture(t, newVS.Path, []byte(`{"keepMe":true,"inputs":[{"id":"different","type":"promptString"}]}`))
	plan, err := destination.planProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	first, err := destination.importProfile(plan)
	if err != nil || first.Added != 3 || first.Paused != 1 || first.Kept != 0 || first.Settings != 1 {
		t.Fatal(first, err)
	}
	newCodex := getRow(t, destination, getTarget(t, destination, "codex", "User"), "typed")
	if newCodex.State != "Disabled" || newCodex.entry["startup_timeout_sec"] != int64(23) {
		t.Fatal("TOML state or native types lost")
	}
	if !getRow(t, destination, getTarget(t, destination, "cursor", "Workspace"), "playwright").Parked {
		t.Fatal("paused state lost")
	}
	if getRow(t, destination, getTarget(t, destination, "claude", "User"), "playwright").State != "Disabled in workspace" {
		t.Fatal("Claude workspace disabled state lost")
	}
	vsConfig := string(readFixture(t, newVS.Path))
	for _, want := range []string{"keepMe", "different", "token"} {
		if !strings.Contains(vsConfig, want) {
			t.Fatalf("missing %s", want)
		}
	}
	second, err := destination.importProfile(plan)
	if err != nil || second.Added != 0 || second.Paused != 0 || second.Kept != 4 || second.Settings != 0 {
		t.Fatal(second, err)
	}
}
func TestProfileErrorsAndExistingRegistration(t *testing.T) {
	m := fixture(t)
	vs := getTarget(t, m, "vscode", "User")
	if _, err := m.Add([]Target{vs}, playwright(), nil); err != nil {
		t.Fatal(err)
	}
	duplicate := ProfileDestination{Client: "vscode", Scope: "User", Format: "json", Active: `{"servers":{"playwright":{"command":"other"}}}`}
	if _, err := m.planProfile(Profile{Schema: profileSchema, Destinations: []ProfileDestination{duplicate, duplicate}}); err == nil {
		t.Fatal("accepted duplicate destinations")
	}
	if _, err := readLegacyProfile(strings.NewReader(`{"schema":"other","destinations":[]}`)); err == nil {
		t.Fatal("accepted wrong schema")
	}
	plan, err := m.planProfile(Profile{Schema: profileSchema, Destinations: []ProfileDestination{duplicate}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.importProfile(plan)
	if err != nil || report.Kept != 1 {
		t.Fatal(report, err)
	}
	if getRow(t, m, vs, "playwright").entry["command"] != "npx" {
		t.Fatal("import overwrote existing registration")
	}
	if _, err := os.Stat(filepath.Dir(vs.Path)); err != nil {
		t.Fatal(err)
	}
}
