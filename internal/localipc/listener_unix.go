//go:build !windows

// Unix (Linux/macOS) transport: Unix domain socket with per-connection
// SO_PEERCRED identity verification. A foreign-uid connection is closed
// before the first byte of HTTP is read.
package localipc

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func dialEndpoint(endpoint string) (net.Conn, error) {
	return net.Dial("unix", endpoint)
}

// listenEndpoint binds the per-user Unix socket in SOCK_STREAM mode with
// the credential-passing option, so every accepted connection carries the
// peer's uid.
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

	// With CREDENTIALS the conn carries the peer uid for verification.
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("localipc: SO_PASSCRED: %w", err)
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

// verifyPeer reads the SO_PEERCRED structure from the connection.
func verifyPeer(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}

	var ucred *unix.Ucred
	var sockErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if ctrlErr != nil {
		return ctrlErr
	}
	if sockErr != nil {
		return sockErr
	}
	if ucred == nil {
		return ErrUnauthorized
	}

	if !credAllowed(int(ucred.Uid)) {
		return ErrUnauthorized
	}
	return nil
}
