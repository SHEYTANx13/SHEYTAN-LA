//go:build (!webworkspace || mobile) && !headless

package desktop

// The no-web-host builds carry the typed, boot-visible answer: the web UI
// host was requested (or auto-preferred) but this binary does not link
// it. A build without the host never silently falls back — the operator
// asked for a shell this binary does not carry, and the failure names it.

import "github.com/SHEYTANx13/SHEYTAN-LA/internal/api"

// webUIHostAvailable reports that this build does NOT link the web UI
// host; auto mode falls back to the native toolkit shell.
func webUIHostAvailable() bool { return false }

// runWebUI is unreachable when auto mode falls back (webUIHostAvailable
// is false); the explicit SHEYTAN_UI=web request lands here and fails
// loudly with the typed build-miswire error.
func runWebUI(*api.Server) int {
	return failBoot("ui mode", ErrWebUINotCompiled)
}
