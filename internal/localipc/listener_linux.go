//go:build linux

// Linux peer credential resolution (run 44306343338 repair §4.4): the
// SO_PASSCRED + SO_PEERCRED mechanism lives in a Linux-tagged file so it
// can never leak into the darwin build again.
package localipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// enablePeerCredentials turns on SO_PASSCRED so accepted connections on
// this socket carry the peer's struct ucred.
func enablePeerCredentials(fd int) error {
	return unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
}

// peerIdentityOf reads the peer's struct ucred (SO_PEERCRED). Linux
// supplies uid, gid AND pid — PIDKnown is true, so callers MAY verify
// the peer PID through this mechanism.
func peerIdentityOf(c *net.UnixConn) (peerIdentity, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return peerIdentity{}, err
	}

	var ucred *unix.Ucred
	var sockErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if ctrlErr != nil {
		return peerIdentity{}, ctrlErr
	}
	if sockErr != nil {
		return peerIdentity{}, fmt.Errorf("localipc: SO_PEERCRED: %w", sockErr)
	}
	if ucred == nil {
		return peerIdentity{}, ErrUnauthorized
	}

	return peerIdentity{
		UID:      int(ucred.Uid),
		GID:      int(ucred.Gid),
		PID:      int(ucred.Pid),
		PIDKnown: true,
	}, nil
}
