package main

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func newEntry(placeholder, value string) *widget.Entry {
	e := widget.NewEntry()
	e.SetPlaceHolder(placeholder)
	e.SetText(value)
	return e
}
func splitLines(value string, trim bool) []string {
	var output []string
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if trim {
			line = strings.TrimSpace(line)
		}
		if line != "" {
			output = append(output, line)
		}
	}
	return output
}
func lineField(label, hint, placeholder, value string, rows int) (*widget.Entry, fyne.CanvasObject) {
	e := widget.NewMultiLineEntry()
	e.TextStyle = fyne.TextStyle{Monospace: true}
	e.Wrapping = fyne.TextWrapOff
	e.SetMinRowsVisible(rows)
	e.SetPlaceHolder(placeholder)
	badge := widget.NewLabel("")
	badge.Importance = widget.LowImportance
	e.OnChanged = func(value string) {
		count := len(splitLines(value, false))
		if count == 1 {
			badge.SetText("1 line")
		} else {
			badge.SetText(fmt.Sprintf("%d lines", count))
		}
	}
	e.SetText(value)
	e.OnChanged(value)
	title := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	header := container.NewBorder(nil, nil, title, badge)
	content := container.NewVBox(header, e)
	if hint != "" {
		content.Add(subdued(hint))
	}
	return e, content
}
func parseHeaders(text string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range splitLines(text, true) {
		header, value, found := strings.Cut(line, "=")
		header, value = strings.TrimSpace(header), strings.TrimSpace(value)
		if !found || !headerName.MatchString(header) || !envName.MatchString(value) {
			return nil, errors.New("enter one HEADER=VARIABLE per line")
		}
		for name := range out {
			if strings.EqualFold(name, header) {
				return nil, errors.New("enter each header once")
			}
		}
		out[header] = value
	}
	return out, nil
}

type serverEditor struct {
	window                                             fyne.Window
	name, command, address, args, env, bearer, headers *widget.Entry
	transport                                          *widget.Select
	destinations                                       *widget.CheckGroup
	save, recover, skip                                *widget.Button
	message                                            *widget.Label
}

func (d *desktop) editor(initial Definition, existing *Registration) {
	e := d.buildEditor(initial, existing)
	e.window.Show()
}

func (d *desktop) buildEditor(initial Definition, existing *Registration) *serverEditor {
	e := &serverEditor{}
	e.name = newEntry("playwright", initial.Name)
	e.transport = widget.NewSelect([]string{"Streamable HTTP", "SSE (legacy)", "Local command (stdio)"}, nil)
	e.transport.SetSelectedIndex(map[string]int{"http": 0, "sse": 1, "stdio": 2}[initial.Transport])
	e.address = newEntry("https://example.com/mcp", initial.URL)
	e.command = newEntry("npx, uvx, or an executable path", initial.Command)
	e.bearer = newEntry("MCP_TOKEN", "")
	var argsField, envField, headerField fyne.CanvasObject
	e.args, argsField = lineField("Arguments", "One argument per line. Keep paths with spaces on a single line.", "-y\n@playwright/mcp@latest\n--headless", strings.Join(initial.Args, "\n"), 5)
	e.env, envField = lineField("Forward variables (optional)", "", "MCP_API_KEY\nPLAYWRIGHT_BROWSERS_PATH", "", 3)
	e.headers, headerField = lineField("Header references", "Optional. Use HEADER=VARIABLE. The client resolves the value.", "X-API-Key=MCP_API_KEY", "", 4)
	remote := container.NewVBox(field("Bearer variable", e.bearer), headerField)
	local := container.NewVBox(envField)
	var identity fyne.CanvasObject = container.NewVBox()
	if existing == nil {
		identity = container.NewGridWithColumns(2, field("Server name", e.name), field("Transport", e.transport))
		remote = container.NewVBox(field("Server URL", e.address), remote)
		local = container.NewVBox(field("Executable", e.command), argsField, envField)
	}
	e.transport.OnChanged = func(string) {
		if e.transport.SelectedIndex() == 2 {
			remote.Hide()
			local.Show()
		} else {
			local.Hide()
			remote.Show()
		}
	}
	e.transport.OnChanged("")
	left := panel(container.NewVBox(heading("Connection settings"), identity, remote, local))

	available := targets(d.manager.Home, d.manager.Project)
	byLabel := map[string]Target{}
	options := make([]string, 0, len(available))
	for _, t := range available {
		options = append(options, t.String())
		byLabel[t.String()] = t
	}
	e.destinations = widget.NewCheckGroup(options, nil)
	if d.client.Selected != "All clients" {
		scope := d.scope.Selected
		if scope != "Workspace" {
			scope = "User"
		}
		option := d.client.Selected + " · " + scope
		if _, ok := byLabel[option]; ok {
			e.destinations.SetSelected([]string{option})
		}
	}
	destCount := subdued("")
	e.destinations.OnChanged = func(selected []string) { destCount.SetText(fmt.Sprintf("%d selected", len(selected))) }
	e.destinations.OnChanged(e.destinations.Selected)
	title, subtitle := "Add an MCP server", "Define the server once, then choose where to register it."
	var content fyne.CanvasObject
	if existing == nil {
		destScroll := container.NewVScroll(e.destinations)
		destScroll.SetMinSize(fyne.NewSize(215, 250))
		right := panel(container.NewBorder(
			container.NewVBox(heading("Destinations"), subdued("Choose one or more client configurations.")),
			destCount, nil, nil, destScroll))
		// Equal gutters, with extra space for longer connection inputs.
		split := container.NewHSplit(container.NewVScroll(left), right)
		split.Offset = 0.65
		content = split
	} else {
		title = "Edit authentication"
		subtitle = existing.Name + "  ·  " + existing.Target.String()
		content = container.NewVScroll(left)
	}
	e.window = d.app.NewWindow(title)
	e.message = widget.NewLabel("")
	e.message.Wrapping = fyne.TextWrapWord
	e.message.Importance = widget.WarningImportance
	e.message.Hide()
	var issue *RecoveryNeeded
	approvals := map[string]*ConfigError{}
	var save func()
	showError := func(err error) {
		issue = nil
		text := err.Error()
		if errors.As(err, &issue) && existing == nil {
			text += "\nThe selected destinations were not written. Back up and recover this file, or skip this destination."
			e.recover.Show()
			if len(e.destinations.Selected) > 1 {
				e.skip.Show()
			}
		}
		e.message.SetText(text)
		e.message.Show()
	}
	save = func() {
		e.message.Hide()
		e.recover.Hide()
		e.skip.Hide()
		kind := []string{"http", "sse", "stdio"}[e.transport.SelectedIndex()]
		spec := Definition{Name: strings.TrimSpace(e.name.Text), Transport: kind}
		if kind == "stdio" {
			spec.Command = strings.TrimSpace(e.command.Text)
			spec.Args = splitLines(e.args.Text, false)
			spec.Env = splitLines(e.env.Text, true)
		} else {
			spec.URL = strings.TrimSpace(e.address.Text)
			spec.Bearer = strings.TrimSpace(e.bearer.Text)
			headers, err := parseHeaders(e.headers.Text)
			if err != nil {
				showError(err)
				return
			}
			spec.Headers = headers
		}
		var err error
		var backups []string
		if existing != nil {
			err = d.manager.Act(*existing, "auth", spec)
		} else {
			var selected []Target
			for _, label := range e.destinations.Selected {
				selected = append(selected, byLabel[label])
			}
			backups, err = d.manager.Add(selected, spec, approvals)
		}
		if err != nil {
			showError(err)
			return
		}
		d.refresh("Configuration saved.")
		e.window.Close()
		if len(backups) > 0 {
			d.copyDialog("Recovery backup saved", "The original settings were backed up before recovery.", strings.Join(backups, "\n"))
		}
	}
	e.save = widget.NewButtonWithIcon("Save registration", theme.DocumentSaveIcon(), save)
	if existing != nil {
		e.save.SetText("Save authentication")
	}
	e.save.Importance = widget.HighImportance
	e.recover = widget.NewButton("Back up and recover", func() {
		if issue == nil {
			return
		}
		selected := issue
		l := subdued("The original will be kept in a recovery backup. If the whole file is unreadable, the client will start with a fresh configuration containing this registration. Restore any other settings from the backup as needed.\n\n" + selected.Issue.Path)
		p := dialog.NewCustomConfirm("Recover "+selected.Target.String(), "Back up and recover", "Cancel", l, func(yes bool) {
			if yes {
				approvals[selected.Target.Path] = selected.Issue
				save()
			}
		}, e.window)
		p.Resize(fyne.NewSize(580, 260))
		p.Show()
	})
	e.skip = widget.NewButton("Skip and save others", func() {
		if issue == nil {
			return
		}
		var remaining []string
		for _, label := range e.destinations.Selected {
			if label != issue.Target.String() {
				remaining = append(remaining, label)
			}
		}
		e.destinations.SetSelected(remaining)
		save()
	})
	e.recover.Hide()
	e.skip.Hide()
	cancel := widget.NewButton("Cancel", e.window.Close)
	actions := container.NewBorder(nil, nil, container.NewHBox(e.recover, e.skip), container.NewGridWithColumns(2, cancel, e.save))
	footer := panel(container.NewVBox(e.message, actions))
	layout := pad(16, container.NewBorder(panel(titleBar(title, subtitle)), footer, nil, nil, content))
	if existing != nil {
		openCentered(e.window, layout, 700, 570)
	} else {
		openCentered(e.window, layout, 940, 760)
	}
	return e
}

func (d *desktop) editConnection() {
	r, ok := d.row()
	if !ok {
		return
	}
	var source, args *widget.Entry
	var fields []fyne.CanvasObject
	if r.Transport == "stdio" {
		source = newEntry("Executable", stringValue(r.entry, "command"))
		var argsField fyne.CanvasObject
		args, argsField = lineField("Arguments", "One argument per line.", "-y\n@playwright/mcp@latest", strings.Join(stringsValue(r.entry["args"]), "\n"), 5)
		fields = append(fields, field("Executable", source), argsField)
	} else {
		raw := stringValue(r.entry, "httpUrl")
		if raw == "" {
			raw = stringValue(r.entry, "url")
		}
		source = newEntry("https://example.com/mcp", raw)
		fields = append(fields, field("Server URL", source))
	}
	reset := widget.NewCheck("Clear existing auth settings when the connection changes", nil)
	note := subdued("Sign out in the client first if you need to clear its OAuth session.")
	message := widget.NewLabel("")
	message.Wrapping = fyne.TextWrapWord
	message.Importance = widget.WarningImportance
	message.Hide()
	w := d.app.NewWindow("Edit connection")
	save := widget.NewButtonWithIcon("Save connection", theme.DocumentSaveIcon(), func() {
		spec := Definition{ResetAuth: reset.Checked}
		if args != nil {
			spec.Command = strings.TrimSpace(source.Text)
			spec.Args = splitLines(args.Text, false)
		} else {
			spec.URL = strings.TrimSpace(source.Text)
		}
		if err := d.manager.Act(r, "connection", spec); err != nil {
			message.SetText(err.Error())
			message.Show()
			return
		}
		d.refresh("Connection updated.")
		w.Close()
	})
	save.Importance = widget.HighImportance
	footer := panel(container.NewVBox(message, container.NewGridWithColumns(2, widget.NewButton("Cancel", w.Close), save)))
	content := panel(container.NewVBox(append(fields, reset, note)...))
	openCentered(w, pad(16, container.NewBorder(panel(titleBar("Edit connection", r.Name+"  ·  "+r.Target.String())), footer, nil, nil, container.NewVScroll(content))), 700, 570)
	w.Show()
}
