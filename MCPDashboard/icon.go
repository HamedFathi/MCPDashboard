package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// appIcon is the window, taskbar and dock icon. Regenerate it, along with the
// .ico and .icns files, with packaging/icon/make_icon.py.
//
//go:embed packaging/icon/icon.png
var appIconPNG []byte

var appIcon = fyne.NewStaticResource("icon.png", appIconPNG)
