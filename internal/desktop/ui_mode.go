//go:build !headless

package desktop

// The desktop UI shell selection. The web UI host (the adaptive web shell
// served through the typed web workspace runtime) is the PRIMARY shell on
// every desktop platform when the build carries it (the webworkspace
// build tag); the native toolkit shells (fyne, go-gui) remain the
// selectable capabilities on their supported platforms. The selection is
// explicit and observable: SHEYTAN_UI=auto (default) prefers the web host
// when compiled in, SHEYTAN_UI=web requires it (typed failure when this
// build does not carry it), SHEYTAN_UI=fyne forces the native shell.

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvUI is the environment key selecting the desktop shell.
const EnvUI = "SHEYTAN_UI"

// Shell modes.
const (
	uiModeAuto = "auto"
	uiModeWeb  = "web"
	uiModeFyne = "fyne"
)

// ErrWebUINotCompiled is the typed answer for SHEYTAN_UI=web on a build
// that does not carry the web workspace host — a build miswire, visible
// at boot, never a silent fallback.
var ErrWebUINotCompiled = errors.New("the web UI host is not compiled into this build (build with -tags webworkspace)")

// resolveUIMode reads and validates the shell selection. The empty value
// and "auto" mean: prefer the web host when this build carries it, the
// native shell otherwise. An unknown value is a boot-visible error — a
// typo must never silently pick a different shell than the operator
// asked for.
func resolveUIMode() (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvUI))); v {
	case "", uiModeAuto:
		return uiModeAuto, nil
	case uiModeWeb:
		return uiModeWeb, nil
	case uiModeFyne:
		return uiModeFyne, nil
	default:
		return "", fmt.Errorf("unknown %s value %q (want auto, web or fyne)", EnvUI, v)
	}
}
