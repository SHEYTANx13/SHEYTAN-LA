//go:build linux

package localipc

import (
	"os"
	"testing"
)

// TestPeerIdentityLinuxCarriesPID: Linux SO_PEERCRED supplies uid, gid
// AND pid — the transport identity reports the peer PID truthfully for a
// same-process dial.
func TestPeerIdentityLinuxCarriesPID(t *testing.T) {
	id := dialSelfAndPeerIdentity(t)

	if !id.PIDKnown {
		t.Fatal("linux peer identity must report PIDKnown=true (ucred carries pid)")
	}
	if id.PID != os.Getpid() {
		t.Fatalf("peer pid = %d, want self %d", id.PID, os.Getpid())
	}
	if id.GID != os.Getgid() {
		t.Fatalf("peer gid = %d, want %d", id.GID, os.Getgid())
	}
}
