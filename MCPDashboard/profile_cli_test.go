package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cliFor(t *testing.T, m Manager) func(int, ...string) (string, string) {
	t.Helper()
	return func(want int, args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		status := runProfileCLI(args, &stdout, &stderr, func() (Manager, error) { return m, nil })
		if status != want {
			t.Fatalf("%v exited %d, want %d: %s", args, status, want, stderr.String())
		}
		if want == 0 && stderr.Len() != 0 {
			t.Fatalf("%v wrote to stderr: %s", args, stderr.String())
		}
		if want != 0 && (stdout.Len() != 0 || stderr.Len() == 0) {
			t.Fatalf("%v invalid failure streams: stdout=%q, stderr=%q", args, stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String()
	}
}

func TestProfileCLIRoundTripAndDryRun(t *testing.T) {
	source := fixture(t)
	user := getTarget(t, source, "codex", "User")
	project := getTarget(t, source, "vscode", "Workspace")
	if _, err := source.Add([]Target{user, project}, playwright(), nil); err != nil {
		t.Fatal(err)
	}

	export := cliFor(t, source)
	path := filepath.Join(t.TempDir(), "backup file.mcpd")
	output, _ := export(0, "export", "--output", path)
	if !strings.Contains(output, `"destinations":1`) {
		t.Fatalf("export used the UI workspace: %s", output)
	}
	original := readFixture(t, path)
	_, message := export(1, "export", "--output", path, "--workspace", source.Project)
	if !strings.Contains(message, "--overwrite") || !bytes.Equal(original, readFixture(t, path)) {
		t.Fatalf("export without overwrite changed existing file: %s", message)
	}
	output, _ = export(0, "export", "--output", path, "--workspace", source.Project, "--overwrite")
	if !strings.Contains(output, `"destinations":2`) {
		t.Fatalf("workspace export: %s", output)
	}

	destination := fixture(t)
	targetProject := filepath.Join(destination.Home, "target workspace")
	if err := os.Mkdir(targetProject, 0700); err != nil {
		t.Fatal(err)
	}
	destination.Project = targetProject
	runImport := cliFor(t, destination)
	_, message = runImport(1, "import", "--input", path)
	if !strings.Contains(message, "choose a workspace") {
		t.Fatal(message)
	}

	decode := func(text string) importCLIResult {
		t.Helper()
		var result importCLIResult
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	output, _ = runImport(0, "import", "--input", path, "--workspace", targetProject, "--dry-run")
	if result := decode(output); !result.DryRun || result.Added != 2 || result.Kept != 0 {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	for _, target := range targets(destination.Home, targetProject) {
		if _, err := os.Stat(target.Path); !os.IsNotExist(err) {
			t.Fatalf("dry run changed %s: %v", target.Path, err)
		}
	}
	if _, err := os.Stat(destination.StateDir); !os.IsNotExist(err) {
		t.Fatalf("dry run created the app state directory: %v", err)
	}

	output, _ = runImport(0, "import", "--input", path, "--workspace", targetProject)
	if result := decode(output); result.DryRun || result.Added != 2 || result.Kept != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	getRow(t, destination, getTarget(t, destination, "vscode", "Workspace"), "playwright")
	output, _ = runImport(0, "import", "--input", path, "--workspace", targetProject)
	if result := decode(output); result.Added != 0 || result.Kept != 2 {
		t.Fatalf("repeat import duplicated registrations: %+v", result)
	}
}

func TestProfileCLIArgumentsAndLegacyInput(t *testing.T) {
	m := fixture(t)
	run := cliFor(t, m)
	if out, _ := run(0, "--help"); !strings.Contains(out, "export --output") {
		t.Fatal(out)
	}
	if out, _ := run(0, "--version"); strings.TrimSpace(out) != "MCPDashboard 1.0.0" {
		t.Fatal(out)
	}
	run(2, "launch")
	run(2, "export", "--output", "backup.json")
	run(2, "import")
	run(2, "import", "--input", "extra.mcpd", "extra")
	run(2, "import", "--input", "extra.mcpd", "--overwrite")
	run(1, "import", "--input", filepath.Join(t.TempDir(), "missing.mcpd"))

	legacy := filepath.Join(t.TempDir(), "previous.json")
	data, err := json.Marshal(fileTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacy, data)
	run(0, "import", "--input", legacy)
	getRow(t, m, getTarget(t, m, "vscode", "User"), "docs")
	corrupt := filepath.Join(t.TempDir(), "corrupt.mcpd")
	writeFixture(t, corrupt, data)
	_, message := run(1, "import", "--input", corrupt)
	if !strings.Contains(message, "signature") {
		t.Fatal(message)
	}
}

func TestProfileCLIOverwriteRefusesSymlink(t *testing.T) {
	m := fixture(t)
	if _, err := m.Add([]Target{getTarget(t, m, "vscode", "User")}, playwright(), nil); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	original := filepath.Join(directory, "original.mcpd")
	link := filepath.Join(directory, "link.mcpd")
	writeFixture(t, original, []byte("original contents"))
	if err := os.Symlink(original, link); err != nil {
		t.Skipf("creating symlinks is unavailable: %v", err)
	}
	_, message := cliFor(t, m)(1, "export", "--output", link, "--overwrite")
	if !strings.Contains(message, "regular file") || string(readFixture(t, original)) != "original contents" {
		t.Fatal(message)
	}
}
