package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (d *desktop) catalogContent() fyne.CanvasObject {
	var list []CatalogServer
	selected, next, request := -1, "", 0
	query := newEntry("Search by server name, e.g. github", "")
	status := subdued("Search the official MCP Registry or browse its latest entries.")
	details := subdued("Select an entry to review it.")
	results := widget.NewList(func() int { return len(list) },
		func() fyne.CanvasObject { l := widget.NewLabel(""); l.Truncation = fyne.TextTruncateEllipsis; return l },
		func(id widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(list[id].Name) })
	draft := widget.NewButtonWithIcon("Review and add", theme.ContentAddIcon(), func() {
		if selected >= 0 && selected < len(list) {
			d.editor(list[selected].Draft(), nil)
		}
	})
	draft.Importance = widget.HighImportance
	host := widget.NewButton("Use for hosted setup", func() {
		if selected >= 0 && selected < len(list) {
			d.hostedURL.SetText(list[selected].Draft().URL)
			d.tabs.Select(d.hostedTab)
		}
	})
	draft.Disable()
	host.Disable()
	results.OnSelected = func(id widget.ListItemID) {
		selected = id
		server := list[id]
		title := server.Title
		if title == "" {
			title = server.Name
		}
		spec := server.Draft()
		description := fmt.Sprintf("%s\n%s\nVersion %s\n\n%s", title, server.Name, server.Version, server.Description)
		if server.Repository.URL != "" {
			description += "\n\nRepository: " + server.Repository.URL
		}
		switch {
		case spec.URL != "":
			description += "\n\nEndpoint: " + spec.URL
		case spec.Command != "":
			description += "\n\nLauncher: " + spec.Command + " " + strings.Join(spec.Args, " ") + "\nReview the publisher's required arguments and environment variables."
		default:
			description += "\n\nThis listing requires setup details from its publisher."
		}
		details.SetText(description)
		draft.Enable()
		enableButton(host, spec.Transport == "http" && strings.HasPrefix(spec.URL, "https://"))
	}
	var load func(bool)
	more := widget.NewButton("Load more", func() { load(true) })
	more.Disable()
	load = func(appendResults bool) {
		request++
		current := request
		q, cursor := strings.TrimSpace(query.Text), ""
		if appendResults {
			cursor = next
		}
		more.Disable()
		status.SetText("Loading registry entries…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
			defer cancel()
			data, cursor, err := searchRegistry(ctx, q, cursor)
			fyne.Do(func() {
				if current != request {
					return
				}
				if err != nil {
					status.SetText("Registry unavailable: " + err.Error())
					enableButton(more, next != "")
					return
				}
				if !appendResults {
					results.UnselectAll()
					list = nil
					selected = -1
					draft.Disable()
					host.Disable()
					details.SetText("Select an entry to review it.")
				}
				list = append(list, data...)
				next = cursor
				results.Refresh()
				enableButton(more, next != "")
				status.SetText(fmt.Sprintf("%d entries loaded. Review a publisher before registering a server.", len(list)))
			})
		}()
	}
	search := widget.NewButtonWithIcon("Search registry", theme.SearchIcon(), func() { load(false) })
	query.OnSubmitted = func(string) { load(false) }
	toolbar := panel(container.NewBorder(nil, nil, nil, container.NewHBox(search, more), query))
	right := panel(container.NewBorder(heading("Registry details"), container.NewGridWithColumns(2, draft, host), nil, nil, container.NewVScroll(details)))
	return container.NewBorder(toolbar, panel(status), nil, nil, container.NewGridWithColumns(2, panel(results), right))
}

type hostedGuide struct{ title, description, link, button string }

func (d *desktop) hostedContent() fyne.CanvasObject {
	d.hostedURL = newEntry("https://your-server.example/mcp", "")
	copy := widget.NewButtonWithIcon("Copy endpoint", theme.ContentCopyIcon(), func() {
		value := strings.TrimSpace(d.hostedURL.Text)
		if err := requireURL(value, false); err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		u, _ := url.Parse(value)
		if u.Scheme != "https" {
			dialog.ShowInformation("Hosted endpoint", "Enter a reachable HTTPS endpoint.", d.window)
			return
		}
		d.window.Clipboard().SetContent(value)
	})
	description := subdued("Connect a reachable HTTPS endpoint in the provider's account settings. Authentication and availability follow its workspace policy.")
	items := []fyne.CanvasObject{panel(container.NewVBox(description, container.NewBorder(nil, nil, nil, copy, d.hostedURL)))}
	for _, guide := range []hostedGuide{
		{"ChatGPT / Work", "Open Plugins → Add custom MCP server. Paste the URL and choose authentication.", "https://chatgpt.com/plugins", "Open Plugins"},
		{"Claude web / Cowork", "Open Customize → Connectors. Add a custom web connector and authenticate in Claude.", "https://claude.ai/customize/connectors", "Open Connectors"},
		{"Codex Cloud", "Cloud tasks use published environments. Follow the environment setup for resources the task needs.", "https://learn.chatgpt.com/docs/environments/cloud-environments", "Cloud guide"},
		{"Gemini web", "Use the local Gemini CLI integration to configure custom MCP servers.", "https://geminicli.com/docs/tools/mcp-server/", "Gemini CLI guide"},
	} {
		link, _ := url.Parse(guide.link)
		open := widget.NewButtonWithIcon(guide.button, theme.NavigateNextIcon(), func() {
			if err := d.app.OpenURL(link); err != nil {
				dialog.ShowError(err, d.window)
			}
		})
		items = append(items, panel(container.NewBorder(nil, nil, nil, open, container.NewVBox(heading(guide.title), subdued(guide.description)))))
	}
	return container.NewVScroll(container.NewVBox(items...))
}
