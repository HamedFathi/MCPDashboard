package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func screenshot(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	path := os.Getenv("MCP_MANAGER_PREVIEWS")
	if path == "" {
		return
	}
	file, err := os.Create(filepath.Join(path, name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = png.Encode(file, w.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}
func TestDashboardAndEditor(t *testing.T) {
	m := fixture(t)
	vs, codex := getTarget(t, m, "vscode", "User"), getTarget(t, m, "codex", "User")
	writeFixture(t, vs.Path, []byte("  \r\n"))
	app := test.NewApp()
	defer app.Quit()
	d := buildDesktop(app, m)
	d.window.Show()
	defer d.window.Close()
	e := d.buildEditor(playwright(), nil)
	e.window.Show()
	e.destinations.SetSelected([]string{vs.String(), codex.String()})
	if e.args.MinSize().Height <= e.command.MinSize().Height*2 {
		t.Fatal("Arguments editor is too short")
	}
	screenshot(t, e.window, "Registration.png")
	test.Tap(e.save)
	if len(d.rows) != 2 {
		t.Fatalf("save failed: %s", e.message.Text)
	}
	d.search.SetText("playwright")
	d.client.SetSelected("Codex")
	if len(d.filtered) != 1 {
		t.Fatal("filter failed")
	}
	d.table.Select(widget.TableCellID{Row: 0, Col: 0})
	test.Tap(d.disable)
	d.table.Select(widget.TableCellID{Row: 0, Col: 0})
	if r, ok := d.row(); !ok || r.State != "Disabled" {
		t.Fatal("disable button failed")
	}
	test.Tap(d.enable)
	d.client.SetSelected("All clients")
	d.project.SetText("Example workspace")
	d.table.Select(widget.TableCellID{Row: 0, Col: 0})
	d.details.SetText("playwright  ·  ~/.codex/config.toml")
	screenshot(t, d.window, "Dashboard.png")
	d.tabs.SelectIndex(1)
	d.tabs.Refresh()
	screenshot(t, d.window, "Profiles.png")
}
func TestEditorCanSkipDamagedDestination(t *testing.T) {
	m := fixture(t)
	vs, codex := getTarget(t, m, "vscode", "User"), getTarget(t, m, "codex", "User")
	raw := []byte("{bad")
	writeFixture(t, vs.Path, raw)
	app := test.NewApp()
	defer app.Quit()
	d := buildDesktop(app, m)
	d.window.Show()
	defer d.window.Close()
	e := d.buildEditor(playwright(), nil)
	e.window.Show()
	e.destinations.SetSelected([]string{vs.String(), codex.String()})
	test.Tap(e.save)
	if !e.recover.Visible() || !e.skip.Visible() {
		t.Fatalf("recovery actions unavailable: %s", e.message.Text)
	}
	test.Tap(e.skip)
	getRow(t, m, codex, "playwright")
	if string(readFixture(t, vs.Path)) != string(raw) {
		t.Fatal("skip wrote the damaged file")
	}
}

func TestClaudeUserPauseWithoutWorkspace(t *testing.T) {
	m := fixture(t)
	workspace := m.Project
	m.Project = ""
	destinations := targets(m.Home, "")
	claude := getTarget(t, m, "claude", "User")
	if _, err := m.Add(destinations, playwright(), nil); err != nil {
		t.Fatal(err)
	}
	before := string(readFixture(t, claude.Path))

	app := test.NewApp()
	defer app.Quit()
	d := buildDesktop(app, m)
	d.window.Show()
	defer d.window.Close()
	d.client.SetSelected("Claude Code")
	d.table.Select(widget.TableCellID{Row: 0, Col: 0})
	if d.disable.Disabled() || d.disable.Text != "Pause in workspace…" {
		t.Fatalf("missing workspace action: %q", d.disable.Text)
	}
	test.Tap(d.disable)
	overlays := d.window.Canvas().Overlays()
	if overlays.Top() == nil {
		t.Fatal("pause did not open the folder picker")
	}
	if d.manager.Project != "" || string(readFixture(t, claude.Path)) != before {
		t.Fatal("pause changed configuration before folder selection")
	}
	overlays.Remove(overlays.Top())

	d.setWorkspace(workspace)
	d.table.Select(widget.TableCellID{Row: 0, Col: 0})
	test.Tap(d.disable)
	if row, ok := d.row(); !ok || row.State != "Disabled in workspace" || d.enable.Disabled() {
		t.Fatal("pause did not update the selected row and expose resume")
	}
	m.Project = t.TempDir()
	if getRow(t, m, claude, "playwright").State != "Registered" {
		t.Fatal("pause changed the user registration outside the chosen workspace")
	}
	test.Tap(d.enable)
	if row, ok := d.row(); !ok || row.State != "Registered" || d.disable.Disabled() {
		t.Fatal("resume did not restore the row")
	}
}
