package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
)

// ErrUnsupportedTransport is the typed refusal returned by DialContext on
// a platform with no supported local transport (neither the Windows named
// pipe nor the Unix domain socket). A refusal is never a fallback: no TCP,
// no loopback HTTP, no arbitrary filesystem socket is ever dialed instead
// (v1.11.1 Windows IPC repair — the transport identity boundary is part of
// the security contract, not a convenience).
var ErrUnsupportedTransport = errors.New("localipc: no supported local transport on this platform")

// DialContext is the ONE canonical client dial to a localipc endpoint
// (v1.11.1 Windows IPC repair).
//
// It is the single platform-aware entrypoint every in-process client uses:
//
//   - Windows: the named-pipe client contract from listener_windows.go
//     (CreateFile with FILE_FLAG_OVERLAPPED; on ERROR_PIPE_BUSY the OS's
//     WaitNamedPipeW readiness notification, retried for as long as the
//     tighter of the caller's deadline and the documented busy-dial budget
//     allows). Per-user DACL, the current-user SID check and the pipe-name
//     normalization are unchanged — this dial is the client half of the
//     exact transport the server publishes.
//   - Linux/macOS: a Unix domain socket dial carrying the caller's context.
//
// The former defect this function replaces: ui/fyne's CoreClient dialed
// "unix" unconditionally, which broke every HTTP- and WebSocket-backed tab
// (including Logs) on Windows, where the endpoint is a \\.\pipe\... path.
// All Fyne HTTP and WebSocket traffic now routes through this function;
// there is no second dial implementation and no TCP fallback.
//
// Context semantics follow net.Dialer: an already-canceled context returns
// the context error without touching the endpoint; a deadline bounds the
// busy-dial budget (the caller's deadline is the tighter limit); a context
// canceled while a WaitNamedPipe readiness wait is outstanding terminates
// the dial within that bounded wait — the wait is the OS's readiness
// mechanism, never a sleep-based poll.
//
// Errors are wrapped with the dialed pipe name for diagnosis and preserve
// errors.Is/errors.As behavior for the underlying OS/network error.
func DialContext(ctx context.Context, endpoint string) (net.Conn, error) {
	switch runtime.GOOS {
	case "windows", "linux", "darwin":
		return dialContext(ctx, endpoint)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedTransport, runtime.GOOS)
	}
}
