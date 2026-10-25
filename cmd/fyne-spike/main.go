//go:build fyne

// cmd/fyne-spike — the one-Go-UI migration's isolated launch entry
// (run-#50 R4c). The shipped binary (cmd/sheytan-local-agent), the Qt
// host (ui/qt), and the hosted workflow are untouched by this package:
// it exists so the spike can be launched and inspected on demand
// (`go build -tags fyne ./cmd/fyne-spike`). It is never a required
// hosted gate and never ships in a release payload this cycle.
package main

import (
	"fyne.io/fyne/v2/app"

	fyneui "github.com/Parsaetak/SHEYTAN-local-agent/ui/fyne"
)

func main() {
	a := app.NewWithID("la.sheytan.spike")
	a.Settings().SetTheme(fyneui.NewTheme())
	fyneui.NewShell(a)
	a.Run()
}
