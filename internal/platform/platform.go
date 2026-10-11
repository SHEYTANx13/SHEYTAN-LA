// Package platform is SHEYTAN's cross-platform OS-integration abstraction
// (v1.2.0). It exists to stop OS-specific calls from scattering through
// business logic: identity (AppUserModelID), firewall rule management and
// future app-integration surfaces live behind one package with explicit
// per-OS implementations.
//
// Rules of the house:
//
//   - only interfaces backed by functionality this release actually needs;
//   - no-ops are HONEST no-ops (they report ErrUnsupportedPlatform, not
//     fake success);
//   - nothing here mutates system state silently — firewall writes are
//     explicit, idempotent, removable and verified.
package platform

import (
	"errors"
	"runtime"
)

// ErrUnsupportedPlatform is returned by operations the current OS does not
// implement. Callers surface it as "not available on this platform" —
// never as success.
var ErrUnsupportedPlatform = errors.New("not supported on this platform")

// AppUserModelID is the Windows application identity (P1). Taskbar
// grouping, Start Menu entries, shortcuts, toast notifications and the
// installer all align on this exact string.
const AppUserModelID = "Parsaetak.SHEYTAN-LA"

// SetAppUserModelID registers the process-level AppUserModelID so the
// taskbar, jump lists and notifications identify the app as
// Parsaetak.SHEYTAN-LA instead of an exe-path hash. On non-Windows
// platforms it returns ErrUnsupportedPlatform.
func SetAppUserModelID(id string) error {
	return setAppUserModelID(id)
}

// CurrentOS is a tiny helper for honest capability reporting in the
// Environment Centre / health views.
func CurrentOS() string { return runtime.GOOS }
