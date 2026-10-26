//go:build !headless

package fyneui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/logo-512.png
var logoPNG []byte

// brandIconResource returns the embedded product logo (the same asset
// gen-syso compiles into the Windows version resource — one brand source).
func brandIconResource() fyne.Resource {
	return &fyne.StaticResource{
		StaticName:    "logo-512.png",
		StaticContent: logoPNG,
	}
}
