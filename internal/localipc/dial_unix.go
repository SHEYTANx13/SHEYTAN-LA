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
	var d net.Dialer
	return d.DialContext(ctx, "unix", endpoint)
}
