package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type desktop struct {
	app                                                             fyne.App
	window                                                          fyne.Window
	tabs                                                            *container.AppTabs
	hostedTab                                                       *container.TabItem
	manager                                                         Manager
	rows, filtered                                                  []Registration
	selected                                                        int
	table                                                           *widget.Table
	search                                                          *widget.Entry
	client, scope                                                   *widget.Select
	project, count, status, details                                 *widget.Label
	enable, disable, edit, auth, login, logout, probeButton, remove *widget.Button
	hostedURL                                                       *widget.Entry
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--oauth-terminal" {
		os.Exit(runOAuthHelper(os.Args[2:]))
	}
	if len(os.Args) > 1 {
		os.Exit(runProfileCLI(os.Args[1:], os.Stdout, os.Stderr, localManager))
	}
	manager, err := localManager()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	app.SetMetadata(fyne.AppMetadata{ID: applicationID, Name: productName, Version: productVersion, Build: 1, Icon: appIcon})
	a := app.NewWithID(applicationID)
	a.SetIcon(appIcon)
	project := a.Preferences().String("project")
	if info, err := os.Stat(project); err != nil || !info.IsDir() {
		project = ""
	}
	manager.Project = project
	d := buildDesktop(a, manager)
	d.window.ShowAndRun()
}

func openCentered(w fyne.Window, content fyne.CanvasObject, width, height float32) {
	w.SetContent(content)
	w.Resize(fyne.NewSize(width, height))
	w.CenterOnScreen()
}

func buildDesktop(a fyne.App, m Manager) *desktop {
	a.Settings().SetTheme(managerTheme{theme.DefaultTheme()})
	d := &desktop{app: a, window: a.NewWindow(productName), manager: m, selected: -1}
	d.hostedTab = container.NewTabItem("Hosted connections", d.hostedContent())
	d.tabs = container.NewAppTabs(
		container.NewTabItem("Local registrations", d.localContent()),
		container.NewTabItem("Profiles", d.profileContent()),
		container.NewTabItem("MCP Registry", d.catalogContent()),
		d.hostedTab,
	)
	top := panel(titleBar(productName, "Version "+productVersion+"  ·  Manage MCP servers across your clients."))
	content := pad(16, container.NewBorder(top, nil, nil, nil, d.tabs))
	openCentered(d.window, content, 1120, 740)
	d.refresh("")
	return d
}

func (d *desktop) localContent() fyne.CanvasObject {
	d.project = widget.NewLabel("")
	d.project.Truncation = fyne.TextTruncateEllipsis
	pick := widget.NewButtonWithIcon("Choose workspace", theme.FolderOpenIcon(), d.pickWorkspace)
	clear := widget.NewButton("Clear", func() {
		d.manager.Project = ""
		d.app.Preferences().SetString("project", "")
		d.refresh("Workspace cleared.")
	})
	d.search = newEntry("Filter by server name", "")
	d.search.OnChanged = func(string) { d.filter() }
	d.client = widget.NewSelect([]string{"All clients", "Codex", "Claude Code", "Claude Desktop", "Gemini CLI", "Cursor", "VS Code"}, func(string) { d.filter() })
	d.client.SetSelectedIndex(0)
	d.scope = widget.NewSelect([]string{"All scopes", "User", "Workspace"}, func(string) { d.filter() })
	d.scope.SetSelectedIndex(0)
	d.count = subdued("")
	add := widget.NewButtonWithIcon("Add server", theme.ContentAddIcon(), func() { d.editor(Definition{Transport: "http"}, nil) })
	add.Importance = widget.HighImportance
	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() { d.refresh("Configuration reloaded.") })
	// Give the stacked button rows shared column widths so "Choose workspace"
	// sits exactly above "Refresh" and "Clear" above "Add server".
	first := fyne.NewSize(max(pick.MinSize().Width, refresh.MinSize().Width), max(pick.MinSize().Height, refresh.MinSize().Height))
	second := fyne.NewSize(max(clear.MinSize().Width, add.MinSize().Width), max(clear.MinSize().Height, add.MinSize().Height))
	buttons := func(a, b fyne.CanvasObject) fyne.CanvasObject {
		return container.NewHBox(container.NewGridWrap(first, a), container.NewGridWrap(second, b))
	}
	workspace := container.NewBorder(nil, nil, nil, buttons(pick, clear), d.project)
	filters := container.NewGridWithColumns(3, d.search, d.client, d.scope)
	toolbar := container.NewBorder(nil, nil, nil, buttons(refresh, add), filters)
	headers := []string{"Server", "Client", "Scope", "Transport", "Authentication", "Configuration state"}
	d.table = widget.NewTableWithHeaders(
		func() (int, int) { return len(d.filtered), len(headers) },
		func() fyne.CanvasObject { l := widget.NewLabel(""); l.Truncation = fyne.TextTruncateEllipsis; return l },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			if id.Row < 0 || id.Row >= len(d.filtered) {
				return
			}
			r := d.filtered[id.Row]
			values := []string{r.Name, r.Target.Label, r.Target.Scope, strings.ToUpper(r.Transport), r.Auth, r.State}
			l := o.(*widget.Label)
			l.Importance = widget.MediumImportance
			if id.Col == 5 && r.State != "Registered" {
				l.Importance = widget.WarningImportance
			}
			l.SetText(values[id.Col])
		},
	)
	d.table.ShowHeaderColumn = false
	d.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	d.table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 && id.Col < len(headers) {
			o.(*widget.Label).SetText(headers[id.Col])
		}
	}
	for i, width := range []float32{187, 148, 102, 106, 150, 181} {
		d.table.SetColumnWidth(i, width)
	}
	d.table.OnSelected = func(id widget.TableCellID) { d.selected = id.Row; d.selection() }
	d.table.OnUnselected = func(widget.TableCellID) { d.selected = -1; d.selection() }

	d.enable = widget.NewButton("Enable / resume", func() { d.act("enable") })
	d.disable = widget.NewButton("Disable / pause", func() { d.act("disable") })
	d.edit = widget.NewButton("Edit connection", d.editConnection)
	d.auth = widget.NewButton("Edit auth", func() {
		if r, ok := d.row(); ok {
			d.editor(Definition{Transport: r.Transport}, &r)
		}
	})
	d.login = widget.NewButton("OAuth setup", func() { d.oauth("login") })
	d.logout = widget.NewButton("OAuth logout", func() { d.oauth("logout") })
	d.probeButton = widget.NewButton("Test HTTP", d.testHTTP)
	d.remove = widget.NewButton("Remove", func() { d.act("remove") })
	d.remove.Importance = widget.DangerImportance
	d.details = widget.NewLabel("Select a registration to manage it.")
	d.details.Truncation = fyne.TextTruncateEllipsis
	d.status = subdued("")
	d.status.Truncation = fyne.TextTruncateEllipsis
	d.status.Wrapping = fyne.TextWrapOff
	actions := container.NewGridWithColumns(4, d.enable, d.disable, d.edit, d.auth, d.login, d.logout, d.probeButton, d.remove)
	footer := panel(container.NewVBox(d.details, actions, d.status))
	return container.NewBorder(panel(container.NewVBox(workspace, toolbar, d.count)), footer, nil, nil, panel(d.table))
}

func (d *desktop) pickWorkspace() {
	d.chooseWorkspace("Choose workspace", nil)
}

func (d *desktop) chooseWorkspace(title string, afterSelection func()) {
	picker := dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if u == nil {
			return
		}
		project, err := filepath.Abs(u.Path())
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if resolved, err := filepath.EvalSymlinks(project); err == nil {
			project = resolved
		}
		d.setWorkspace(project)
		if afterSelection != nil {
			afterSelection()
		}
	}, d.window)
	picker.SetTitleText(title)
	picker.Show()
}

func (d *desktop) setWorkspace(project string) {
	d.manager.Project = project
	d.app.Preferences().SetString("project", project)
	d.refresh("Workspace selected.")
}
func (d *desktop) refresh(message string) {
	project := d.manager.Project
	if project == "" {
		project = "No workspace selected"
	}
	d.project.SetText(project)
	var warnings []string
	d.rows, warnings = d.manager.List(targets(d.manager.Home, d.manager.Project))
	d.filter()
	if len(warnings) > 0 {
		d.status.SetText(fmt.Sprintf("%d configuration issue(s). %s", len(warnings), warnings[0]))
		return
	}
	if message == "" {
		message = "Close affected clients before saving. Reopen them to load changes."
	}
	d.status.SetText(message)
}
func (d *desktop) filter() {
	if d.table == nil {
		return
	}
	query := strings.ToLower(strings.TrimSpace(d.search.Text))
	d.filtered = nil
	for _, r := range d.rows {
		if d.client.Selected != "All clients" && d.client.Selected != r.Target.Label {
			continue
		}
		if d.scope.Selected != "All scopes" && d.scope.Selected != r.Target.Scope {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Name), query) {
			continue
		}
		d.filtered = append(d.filtered, r)
	}
	d.selected = -1
	d.table.UnselectAll()
	d.table.Refresh()
	d.count.SetText(fmt.Sprintf("%d shown   /   %d total", len(d.filtered), len(d.rows)))
	d.selection()
}
func (d *desktop) row() (Registration, bool) {
	if d.selected < 0 || d.selected >= len(d.filtered) {
		return Registration{}, false
	}
	return d.filtered[d.selected], true
}
func enableButton(b *widget.Button, on bool) {
	if on {
		b.Enable()
	} else {
		b.Disable()
	}
}
func (d *desktop) selection() {
	r, ok := d.row()
	needsWorkspace := ok && r.Target.Toggle == "claude" && d.manager.Project == ""
	d.disable.SetText("Disable / pause")
	if !ok {
		d.details.SetText("Select a registration to manage it.")
	} else if needsWorkspace {
		d.details.SetText(r.Name + "  ·  Claude Code applies pause and resume per workspace.")
		d.disable.SetText("Pause in workspace…")
	} else {
		d.details.SetText(r.Name + "  ·  " + r.Target.Path)
	}
	disabled := r.Parked || strings.HasPrefix(r.State, "Disabled")
	enableButton(d.enable, ok && disabled && !needsWorkspace)
	enableButton(d.disable, ok && !disabled)
	enableButton(d.edit, ok && (r.Transport == "http" || r.Transport == "sse" || r.Transport == "stdio"))
	enableButton(d.auth, ok && r.Target.EnvStyle != "" && (r.Transport == "stdio" || ((r.Transport == "http" || r.Transport == "sse") && r.Target.ID != "gemini")))
	remote := ok && (r.Transport == "http" || r.Transport == "sse") && !r.Parked
	enableButton(d.login, remote)
	enableButton(d.logout, remote && (r.Target.ID == "codex" || r.Target.ID == "claude"))
	enableButton(d.probeButton, ok && r.Transport == "http")
	enableButton(d.remove, ok)
}
func (d *desktop) act(action string) {
	r, ok := d.row()
	if !ok {
		return
	}
	run := func() {
		if err := d.manager.Act(r, action, Definition{}); err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		message := "Updated " + r.Name + " in " + r.Target.String() + "."
		if r.Target.Toggle == "claude" && action != "remove" {
			verb := "Paused"
			if action == "enable" {
				verb = "Resumed"
			}
			message = verb + " " + r.Name + " for the selected Claude Code workspace."
		}
		d.refresh(message)
		if action == "enable" || action == "disable" {
			for i, row := range d.filtered {
				if row.Name == r.Name && row.Target == r.Target {
					d.table.Select(widget.TableCellID{Row: i, Col: 0})
					break
				}
			}
		}
	}
	if action == "disable" && r.Target.Toggle == "claude" && d.manager.Project == "" {
		d.chooseWorkspace("Choose the folder you use with Claude Code", run)
	} else if action == "remove" {
		dialog.ShowConfirm("Remove registration", fmt.Sprintf("Remove %s from %s? The client manages its OAuth credentials and package caches.", r.Name, r.Target.String()), func(ok bool) {
			if ok {
				run()
			}
		}, d.window)
	} else {
		run()
	}
}
func (d *desktop) oauth(action string) {
	r, ok := d.row()
	if !ok {
		return
	}
	show := func() {
		switch r.Target.ID {
		case "claude", "codex":
			if err := oauthTerminal(r.Target, action, r.Name, d.manager.Project); err == nil {
				d.status.SetText("Continue in the client's terminal.")
			} else {
				d.copyDialog("OAuth command", err.Error()+"\nRun this command in the client workspace.", r.Target.ID+" mcp "+action+" "+r.Name)
			}
		case "gemini":
			d.copyDialog("Gemini CLI OAuth", "Open Gemini CLI in the appropriate workspace and enter:", "/mcp auth "+r.Name)
		case "vscode":
			d.copyDialog("VS Code OAuth", "Use the Command Palette, select the server, and start it. VS Code prompts when authentication is required.", "MCP: List Servers")
		case "cursor":
			dialog.ShowInformation("Cursor OAuth", "Open Cursor Settings → Tools & MCP and connect the server.", d.window)
		default:
			dialog.ShowInformation("OAuth setup", "Complete authentication in the target client.", d.window)
		}
	}
	if action == "logout" {
		dialog.ShowConfirm("OAuth logout", "Run the client's logout for "+r.Name+"?", func(yes bool) {
			if yes {
				show()
			}
		}, d.window)
	} else {
		show()
	}
}
func (d *desktop) copyDialog(title, description, value string) {
	l := subdued(description)
	e := newEntry("", value)
	copy := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() { d.window.Clipboard().SetContent(value) })
	p := dialog.NewCustom(title, "Close", container.NewVBox(l, container.NewBorder(nil, nil, nil, copy, e)), d.window)
	p.Resize(fyne.NewSize(580, 190))
	p.Show()
}
func (d *desktop) testHTTP() {
	r, ok := d.row()
	if !ok {
		return
	}
	dialog.ShowConfirm("Test MCP endpoint", "Send an MCP initialize request to this endpoint using its configured headers. OAuth sessions stay in the client.", func(yes bool) {
		if !yes {
			return
		}
		d.status.SetText("Testing " + r.Name + "…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
			defer cancel()
			message, err := probe(ctx, r)
			fyne.Do(func() {
				if err != nil {
					d.status.SetText("Test failed: " + err.Error())
					dialog.ShowError(err, d.window)
					return
				}
				d.status.SetText(message)
			})
		}()
	}, d.window)
}
