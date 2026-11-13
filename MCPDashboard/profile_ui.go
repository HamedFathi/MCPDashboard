package main

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (d *desktop) profileContent() fyne.CanvasObject {
	export := widget.NewButtonWithIcon("Export MCP profile", theme.DocumentSaveIcon(), d.exportDialog)
	export.Importance = widget.HighImportance
	importButton := widget.NewButtonWithIcon("Open MCP profile", theme.FolderOpenIcon(), d.importDialog)
	importButton.Importance = widget.HighImportance
	backup := panel(container.NewBorder(
		container.NewVBox(heading("Export"), subdued("Save one reusable .mcpd file with all user registrations and the selected workspace. The export includes paused and disabled entries.")),
		export, nil, nil, subdued("Keep the file private. Existing server definitions can contain tokens, headers, or credentials in arguments.")))
	restore := panel(container.NewBorder(
		container.NewVBox(heading("Import"), subdued("Review an .mcpd profile or an older MCPDashboard JSON export before applying it. Workspace entries go to the workspace you selected.")),
		importButton, nil, nil, subdued("A registration already present at the same destination is kept. You can import the same file again without adding duplicates.")))
	intro := panel(titleBar("Reusable MCP profiles", "Back up your setup and reuse it across machines or workspaces."))
	footer := panel(subdued("Choose a workspace on the Local registrations tab to include or restore workspace settings."))
	return container.NewBorder(intro, footer, nil, nil, container.NewGridWithColumns(2, backup, restore))
}

func (d *desktop) exportDialog() {
	p, err := d.manager.exportProfile()
	if err != nil {
		dialog.ShowError(fmt.Errorf("%w\nCorrect or recover the affected configuration before exporting the complete profile", err), d.window)
		return
	}
	if len(p.Destinations) == 0 {
		dialog.ShowInformation("Nothing to export", "Add a server first, or choose a workspace that has registrations.", d.window)
		return
	}
	warning := subdued("The file includes credentials already stored in server definitions. Anyone with the file can decode its contents. Choose a private folder.")
	confirm := dialog.NewCustomConfirm("Export MCP profile", "Choose folder", "Cancel", warning, func(yes bool) {
		if !yes {
			return
		}
		dialog.ShowFolderOpen(func(folder fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, d.window)
				return
			}
			if folder == nil {
				return
			}
			path, err := saveProfile(folder.Path(), p)
			if err != nil {
				dialog.ShowError(err, d.window)
				return
			}
			d.copyDialog("MCP profile exported", fmt.Sprintf("Saved %d destinations to one .mcpd file.", len(p.Destinations)), path)
		}, d.window)
	}, d.window)
	confirm.Resize(fyne.NewSize(570, 200))
	confirm.Show()
}

func (d *desktop) importDialog() {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if reader == nil {
			return
		}
		p, err := readProfileFile(reader.URI().Name(), reader)
		_ = reader.Close()
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		plan, err := d.manager.planProfile(p)
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		summary := widget.NewLabel(profileSummary(plan))
		summary.Wrapping = fyne.TextWrapWord
		scroll := container.NewVScroll(summary)
		scroll.SetMinSize(fyne.NewSize(500, 230))
		note := subdued("Existing registrations with the same name stay in place. Local launch commands can run when you reopen a client, so import profiles from sources you trust.")
		preview := dialog.NewCustomConfirm("Review MCP profile", "Import profile", "Cancel", container.NewVBox(note, scroll), func(yes bool) {
			if !yes {
				return
			}
			report, err := d.manager.importProfile(plan)
			if err != nil {
				var issue *RecoveryNeeded
				if errors.As(err, &issue) {
					err = fmt.Errorf("%w\nRepair this destination before importing the profile", err)
				}
				dialog.ShowError(err, d.window)
				return
			}
			d.refresh("Profile imported.")
			d.tabs.SelectIndex(0)
			dialog.ShowInformation("Profile imported", fmt.Sprintf("%d registered, %d paused, %d existing kept, %d option groups restored.", report.Added, report.Paused, report.Kept, report.Settings), d.window)
		}, d.window)
		preview.Resize(fyne.NewSize(650, 440))
		preview.Show()
	}, d.window)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{profileExtension, ".json"}))
	picker.Resize(fyne.NewSize(770, 530))
	picker.Show()
}
