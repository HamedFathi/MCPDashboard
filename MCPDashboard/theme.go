package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	background = rgb(0xf3f6fa)
	surface    = rgb(0xffffff)
	inset      = rgb(0xf7f9fc)
	edge       = rgb(0xd6dee8)
	textColor  = rgb(0x1c2a3a)
	muted      = rgb(0x5c6b7e)
	accent     = rgb(0x0078d4)
)

func rgb(value uint32) color.Color {
	return color.NRGBA{R: uint8(value >> 16), G: uint8(value >> 8), B: uint8(value), A: 255}
}

type managerTheme struct{ fyne.Theme }

func (t managerTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return background
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground, theme.ColorNameHeaderBackground:
		return surface
	case theme.ColorNameInputBackground:
		return inset
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return edge
	case theme.ColorNameForeground:
		return textColor
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return muted
	case theme.ColorNameButton:
		return rgb(0xc8d3e0)
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return accent
	case theme.ColorNameForegroundOnPrimary:
		return rgb(0xffffff)
	case theme.ColorNameSelection:
		return rgb(0xcfe4f7)
	// Fyne blends Focus, Hover and Pressed over the widget's own color, so they
	// must be translucent: an opaque tint would wash out the blue buttons.
	case theme.ColorNameFocus:
		return color.NRGBA{R: 0x00, G: 0x78, B: 0xd4, A: 0x38}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0x0a, G: 0x1e, B: 0x3c, A: 0x1c}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 0x0a, G: 0x1e, B: 0x3c, A: 0x30}
	case theme.ColorNameError:
		return rgb(0xc93a46)
	case theme.ColorNameWarning:
		return rgb(0xb26a00)
	}
	return t.Theme.Color(name, theme.VariantLight)
}
func (t managerTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameHeadingText:
		return 27
	case theme.SizeNameSubHeadingText:
		return 18
	}
	return t.Theme.Size(name)
}
func pad(px float32, content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(px, px, px, px), content)
}
func panel(content fyne.CanvasObject) fyne.CanvasObject {
	box := canvas.NewRectangle(surface)
	box.CornerRadius = 12
	box.StrokeColor = edge
	box.StrokeWidth = 1
	return container.NewStack(box, pad(12, content))
}
func subdued(message string) *widget.Label {
	l := widget.NewLabel(message)
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}
func heading(message string) *widget.Label {
	l := widget.NewLabelWithStyle(message, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = theme.SizeNameSubHeadingText
	return l
}
func titleBar(title, subtitle string) fyne.CanvasObject {
	h := heading(title)
	h.SizeName = theme.SizeNameHeadingText
	return container.NewVBox(h, subdued(subtitle))
}
func field(title string, input fyne.CanvasObject) fyne.CanvasObject {
	l := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(l, input)
}
