//go:build !windows

package localipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dialSelfAndPeerIdentity drives one same-user connection through the
// verified listener and returns the OS-provided peer identity. Used by
// the shared test and the per-OS PID contract tests.
func dialSelfAndPeerIdentity(t *testing.T) peerIdentity {
	t.Helper()

	dir := t.TempDir()
	endpoint := filepath.Join(dir, "cred.sock")

	ln, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listenEndpoint: %v", err)
	}
	defer func() { _ = ln.Close() }()

	type result struct {
		id  peerIdentity
		err error
	}
	got := make(chan result, 1)

	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			got <- result{err: aerr}
			return
		}
		defer func() { _ = c.Close() }()

		uc, ok := c.(*net.UnixConn)
		if !ok {
			got <- result{err: fmt.Errorf("accept returned %T, want *net.UnixConn", c)}
			return
		}
		id, ierr := peerIdentityOf(uc)
		got <- result{id: id, err: ierr}
	}()

	conn, derr := net.DialTimeout("unix", endpoint, 2*time.Second)
	if derr != nil {
		t.Fatalf("self dial: %v", derr)
	}
	_ = conn.Close()

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("peer identity: %v", r.err)
		}
		return r.id
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the accepted connection identity")
		return peerIdentity{}
	}
}

// TestPeerIdentitySelfDial: a same-user connection over the verified
// listener carries the local uid through the OS credential mechanism,
// and the listener unlinks its socket file on Close (cleanup contract).
func TestPeerIdentitySelfDial(t *testing.T) {
	dir := t.TempDir()
	endpoint := filepath.Join(dir, "cred.sock")

	ln, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listenEndpoint: %v", err)
	}
	defer func() { _ = ln.Close() }()

	id := dialSelfAndPeerIdentity(t)
	if id.UID != os.Getuid() {
		t.Fatalf("peer uid = %d, want local uid %d", id.UID, os.Getuid())
	}

	// Permission/cleanup contract: the socket file exists while bound and
	// is removed on Close.
	if err := ln.Close(); err != nil {
		t.Fatalf("listener close: %v", err)
	}
	if _, lerr := os.Lstat(endpoint); !os.IsNotExist(lerr) {
		t.Fatalf("listener socket %s must be unlinked on Close, Lstat err=%v", endpoint, lerr)
	}
}
