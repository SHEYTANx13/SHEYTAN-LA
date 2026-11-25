//go:build !headless

package desktop

// The desktop UI shell selection. The web UI host (the adaptive web shell
// served through the typed web workspace runtime) is THE shell on every
// desktop platform; the packaged desktop builds all carry it (the
// webworkspace build tag). The selection is explicit and observable:
// SHEYTAN_UI=auto (default) prefers the web host when compiled in,
// SHEYTAN_UI=web requires it (typed failure when this build does not
// carry it), and SHEYTAN_UI=fyne names a RETIRED shell — the typed
// retirement error points at docs/retired.md instead of silently falling
// back (a typo or a stale launcher script must never pick a different
// shell than the operator asked for).

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

// ErrFyneRetired is the typed answer for SHEYTAN_UI=fyne: the Fyne
// toolkit shell was deleted once the adaptive web shell carried its full
// surface (run 45634345922 update, docs/retired.md). The retirement is
// honest and loud, never a silent fallback to a different shell.
var ErrFyneRetired = errors.New("the Fyne toolkit shell is retired — the adaptive web shell is the product UI (see docs/retired.md)")

// resolveUIMode reads and validates the shell selection. The empty value
// and "auto" mean: prefer the web host when this build carries it, the
// native shell otherwise. An unknown value is a boot-visible error — a
// typo must never silently pick a different shell than the operator
// asked for.
func resolveUIMode() (string, error) {
	return resolveUIModeEnv(os.Getenv(EnvUI))
}

// resolveUIModeEnv is the pure form of the selection (the env indirection
// keeps the contract unit-testable without process environment mutation).
func resolveUIModeEnv(value string) (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(value)); v {
	case "", uiModeAuto:
		return uiModeAuto, nil
	case uiModeWeb:
		return uiModeWeb, nil
	case uiModeFyne:
		return "", ErrFyneRetired
	default:
		return "", fmt.Errorf("unknown %s value %q (want auto, web or fyne)", EnvUI, v)
	}
}
