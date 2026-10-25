//go:build fyne

// v1.11.0 (R3a, run-#51, Task ID 45): the fyne-tagged launch hook — the
// ONE entry surface for the one-Go-UI migration spike (C19). The
// cmd/fyne-spike main package retires into this hook: `sheytan spike`
// (a binary built with -tags fyne) launches the ui/fyne shell exactly as
// the spike's standalone main did. The untagged shipped binary never
// links fyne and never changes behavior: there, fyneLaunch stays nil and
// the dispatch falls through to the same unknown-command path as before.
package cmd

import (
	"fyne.io/fyne/v2/app"

	fyneui "github.com/Parsaetak/SHEYTAN-local-agent/ui/fyne"
)

func init() {
	fyneLaunch = FyneLaunch
}

// FyneLaunch launches the ui/fyne shell (the retired spike's behavior,
// byte-for-byte: same app ID, same theme source, same shell entry) and
// reports a clean exit once the window closes.
func FyneLaunch() int {
	a := app.NewWithID("la.sheytan.spike")
	a.Settings().SetTheme(fyneui.NewTheme())
	fyneui.NewShell(a)
	a.Run()
	return 0
}
