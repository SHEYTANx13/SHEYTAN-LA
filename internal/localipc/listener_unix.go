//go:build !windows

// Unix (Linux/macOS) transport: Unix domain socket with per-connection
// peer-credential identity verification. A foreign-uid connection is
// closed before the first byte of HTTP is read.
//
// The credential MECHANISM is OS-specific (run 44306343338 repair, §4.4:
// Linux SO_PASSCRED/SO_PEERCRED vs Darwin SOL_LOCAL/LOCAL_PEERCRED — the
// Linux-only implementation must never be compiled under a broad Unix
// build tag again). This file carries the shared transport and the uid
// policy; listener_linux.go and listener_darwin.go carry the per-OS peer
// credential implementations. Windows named-pipe peer identity stays a
// separate implementation (listener_windows.go).
package localipc

import (
	"context"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func dialEndpoint(endpoint string) (net.Conn, error) {
	// v1.11.1 Windows IPC repair: dialEndpoint delegates to the ONE
	// canonical context-aware dial (background semantics for the probe
	// paths); production clients call the public DialContext.
	return dialContext(context.Background(), endpoint)
}

// peerIdentity is the OS-provided identity of one accepted connection.
type peerIdentity struct {
	UID      int
	GID      int
	PID      int  // 0 when the OS API does not supply it
	PIDKnown bool // true ONLY when the OS API genuinely provides the peer PID
}

// listenEndpoint binds the per-user Unix socket in SOCK_STREAM mode and
// enables the OS-specific peer-credential mechanism, so every accepted
// connection carries the peer's identity.
func listenEndpoint(endpoint string) (net.Listener, error) {
	if removed, err := cleanStaleEndpoint(endpoint); err != nil {
		return nil, err
	} else if removed {
		fmt.Println("localipc: removed stale endpoint", endpoint)
	}

	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("localipc: socket: %w", err)
	}

	// Bind manually so the socket file is created with an explicit
	// restrictive mode (0600) instead of the umask-dependent default.
	addr := &unix.SockaddrUnix{Name: endpoint}
	if err := unix.Bind(fd, addr); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("localipc: bind %s: %w", endpoint, err)
	}

	if err := os.Chmod(endpoint, 0o600); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("localipc: chmod socket: %w", err)
	}

	if err := unix.Listen(fd, unix.SOMAXCONN); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("localipc: listen: %w", err)
	}

	if err := enablePeerCredentials(fd); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("localipc: peer credentials: %w", err)
	}

	files := make([]*os.File, 1)
	files[0] = os.NewFile(uintptr(fd), endpoint)
	ln, err := net.FileListener(files[0])
	if err != nil {
		_ = files[0].Close()
		return nil, fmt.Errorf("localipc: file listener: %w", err)
	}

	uln, ok := ln.(*net.UnixListener)
	if !ok {
		_ = ln.Close()
		_ = files[0].Close()
		return nil, fmt.Errorf("localipc: listener is %T, want *net.UnixListener", ln)
	}

	// The temp-dir cleanup contract: unlink on Close.
	uln.SetUnlinkOnClose(true)
	return &verifiedListener{UnixListener: uln}, nil
}

// verifiedListener wraps the Unix listener and rejects any accepted
// connection whose peer uid is not the current user BEFORE handing it to
// the HTTP server. The check is transport-level: the peer never reaches a
// handler, a header, or the handshake.
type verifiedListener struct {
	*net.UnixListener
}

func (l *verifiedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.UnixListener.AcceptUnix()
		if err != nil {
			return nil, err
		}

		if err := verifyPeer(c); err != nil {
			// Foreign local process: reject deterministically. This is not
			// an error condition for the listener itself — accept the next.
			_ = c.Close()
			continue
		}

		return c, nil
	}
}

// verifyPeer resolves the peer identity through the OS-specific
// credential mechanism and applies the uid policy.
func verifyPeer(c *net.UnixConn) error {
	id, err := peerIdentityOf(c)
	if err != nil {
		return err
	}

	if !credAllowed(id.UID) {
		return ErrUnauthorized
	}
	return nil
}
