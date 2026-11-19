//go:build !windows

package localipc

import (
	"context"
	"net"
)

// dialContext is the Unix (Linux/macOS) half of the canonical client dial:
// a Unix domain socket connect carrying the caller's context, exactly the
// transport listener_unix.go publishes (per-user 0700 socket directory +
// SO_PEERCRED uid verification on the server side).
func dialContext(ctx context.Context, endpoint string) (net.Conn, error) {
	// net.Dialer context semantics (documented on the public DialContext):
	// an already-canceled context returns the context error without
	// touching the endpoint — the path validation below must not preempt
	// that contract.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Run 44378562071 §4.4: the dialer validates the path with the same
	// OS-aware validator as the listener — a too-long path is the typed
	// ErrSocketPathTooLong, never an OS-level connect mystery.
	if err := ValidateUnixSocketPath(endpoint); err != nil {
		return nil, err
	}

	var d net.Dialer
	return d.DialContext(ctx, "unix", endpoint)
}
