//go:build darwin

package localipc

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// TestPeerIdentityDarwinPIDNotClaimed: darwin LOCAL_PEERCRED supplies
// uid/gid only. The transport must NOT claim peer PID verification
// (work order §4.4.4 — never claim PID when the API only supplies
// UID/GID).
func TestPeerIdentityDarwinPIDNotClaimed(t *testing.T) {
	id := dialSelfAndPeerIdentity(t)

	if id.PIDKnown {
		t.Fatal("darwin peer identity must NOT claim PID verification (LOCAL_PEERCRED carries uid/gid only)")
	}
	if id.PID != 0 {
		t.Fatalf("darwin peer PID must stay 0 when unknown, got %d", id.PID)
	}
	if id.UID <= 0 {
		t.Fatalf("darwin peer uid must be the kernel-provided value, got %d", id.UID)
	}
}

// TestXucredMalformedVersionRejected: a credential structure with a
// wrong version stamp is a typed rejection — the peer is never trusted.
func TestXucredMalformedVersionRejected(t *testing.T) {
	_, err := xucredToIdentity(&unix.Xucred{Version: 99, Uid: 0})
	if !errors.Is(err, ErrPeerCredentialsMalformed) {
		t.Fatalf("wrong xucred version = %v, want ErrPeerCredentialsMalformed", err)
	}

	if _, err := xucredToIdentity(nil); err == nil {
		t.Fatal("nil xucred must be rejected")
	}
}
