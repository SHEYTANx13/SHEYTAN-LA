//go:build darwin

// Darwin peer credential resolution (run 44306343338 repair §4.4): the
// documented macOS/BSD local-domain-socket credential API —
// getsockopt(SOL_LOCAL, LOCAL_PEERCRED) returning struct xucred —
// through the pinned golang.org/x/sys version (constants and
// GetsockoptXucred verified against x/sys v0.50.0).
package localipc

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// xucredVersion mirrors Darwin's XUCRED_VERSION (0): the struct version
// stamp the kernel writes into struct xucred.cr_version. The pinned
// golang.org/x/sys does not export the constant, so the verified value
// is declared here and checked against the kernel answer at runtime.
const xucredVersion = 0

// ErrPeerCredentialsMalformed is returned when the Darwin credential
// structure does not carry the expected version stamp. Malformed peer
// credentials are a hard rejection — the connection is never trusted on
// the strength of its socket path.
var ErrPeerCredentialsMalformed = errors.New("localipc: malformed darwin peer credential structure")

// enablePeerCredentials is a no-op on darwin: LOCAL_PEERCRED resolution
// does not require a listener-side option (unlike Linux SO_PASSCRED).
func enablePeerCredentials(fd int) error {
	_ = fd
	return nil
}

// peerIdentityOf resolves the peer identity through
// getsockopt(SOL_LOCAL, LOCAL_PEERCRED).
//
// Darwin supplies UID/GID ONLY — the API has no peer PID. PIDKnown stays
// false and PID stays 0: peer-PID verification is never claimed from
// this mechanism. If a future requirement needs PID identity, a genuine
// darwin-specific method must be implemented; silently trusting the
// socket path would be a downgrade, not evidence.
func peerIdentityOf(c *net.UnixConn) (peerIdentity, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return peerIdentity{}, err
	}

	var cred *unix.Xucred
	var sockErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	})
	if ctrlErr != nil {
		return peerIdentity{}, ctrlErr
	}
	if sockErr != nil {
		return peerIdentity{}, fmt.Errorf("localipc: LOCAL_PEERCRED: %w", sockErr)
	}
	if cred == nil {
		return peerIdentity{}, ErrUnauthorized
	}

	return xucredToIdentity(cred)
}

// xucredToIdentity validates the kernel-provided xucred structure and
// converts it to the transport identity. Malformed structures (wrong
// version stamp) are rejected explicitly.
func xucredToIdentity(cred *unix.Xucred) (peerIdentity, error) {
	if cred == nil {
		return peerIdentity{}, ErrUnauthorized
	}
	if cred.Version != xucredVersion {
		return peerIdentity{}, fmt.Errorf("%w: xucred version %d, want %d",
			ErrPeerCredentialsMalformed, cred.Version, xucredVersion)
	}

	id := peerIdentity{UID: int(cred.Uid)}
	if cred.Ngroups > 0 {
		id.GID = int(cred.Groups[0])
	}
	return id, nil
}
