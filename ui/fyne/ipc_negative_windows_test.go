//go:build windows && !headless

// ipc_negative_windows_test.go — v1.11.1 run-43601915826 coverage
// completion for the Windows lane:
//
//   - WebSocket CALLER-context cancellation through the REAL named pipe
//     (pins the ctx.Done() branch of pumpActivity against the production
//     transport, not a fake dialer — the detach path was already
//     covered; the cancel path was the gap);
//   - proof that the whole flow opened NO TCP listener (netstat,
//     PID-filtered — the transport-identity boundary is part of the
//     security contract, and a TCP fallback must fail this gate).
package fyneui_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
	fyneui "github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne"
)

// windowsListeningTCPLines counts netstat LISTENING rows owned by this
// process — the honest "no TCP listener was opened" probe on Windows.
func windowsListeningTCPLines(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		t.Fatalf("netstat: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 4 && strings.EqualFold(fields[3], "LISTENING") && fields[len(fields)-1] == fmt.Sprint(os.Getpid()) {
			count++
		}
	}
	return count
}

// TestWindowsIPCFlowOpensNoTCPListener proves the transport-identity
// boundary end to end: the real core, the real named-pipe IPC server and
// the production client complete a handshake, a core read and a
// WebSocket attach/close WITHOUT any TCP listener appearing for this
// process.
func TestWindowsIPCFlowOpensNoTCPListener(t *testing.T) {
	srv, cfg := newIsolatedCoreWindows(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	before := windowsListeningTCPLines(t)

	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		t.Fatalf("localipc.Serve: %v", err)
	}
	t.Cleanup(ipc.Stop)

	client := fyneui.NewCoreClient(ipc.Endpoint(), key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := client.Config(ctx); err != nil {
		t.Fatalf("Config: %v", err)
	}
	sess, err := client.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rt := fyneui.NewRuntime(client)
	defer rt.Shutdown()
	attachDone := make(chan error, 1)
	actx, acancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer acancel()
	go func() { attachDone <- rt.AttachActivity(actx, sess.ID) }()
	// Bounded settle so the WS dial has run before the listener recount;
	// the pump is torn down deterministically right after via Detach.
	time.Sleep(500 * time.Millisecond)
	rt.DetachActivity()
	select {
	case <-attachDone:
	case <-time.After(5 * time.Second):
		t.Fatal("attach pump did not end after detach")
	}

	after := windowsListeningTCPLines(t)
	if after != before {
		t.Fatalf("TCP listener count changed around the IPC flow: before=%d after=%d — a TCP fallback or side listener appeared", before, after)
	}
}

// TestCoreClientWebSocketCallerCancelWindows: the CALLER's context — not
// a detach — terminates the activity pump, and the pump surfaces the
// context error, over the REAL named-pipe transport.
func TestCoreClientWebSocketCallerCancelWindows(t *testing.T) {
	srv, cfg := newIsolatedCoreWindows(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		t.Fatalf("localipc.Serve: %v", err)
	}
	t.Cleanup(ipc.Stop)

	client := fyneui.NewCoreClient(ipc.Endpoint(), key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	sess, err := client.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rt := fyneui.NewRuntime(client)
	defer rt.Shutdown()

	attached := make(chan struct{}, 1)
	rt.SetCallbacks(
		func() {
			select {
			case attached <- struct{}{}:
			default:
			}
		},
		func() {},
		func() {},
	)

	attachDone := make(chan error, 1)
	actx, acancel := context.WithCancel(context.Background())
	defer acancel()
	go func() { attachDone <- rt.AttachActivity(actx, sess.ID) }()

	select {
	case <-attached:
	case <-time.After(15 * time.Second):
		t.Fatal("no WebSocket attach frame within 15s — the streaming transport is broken over the named pipe")
	}

	acancel() // the caller cancels: the pump must end with the context error
	select {
	case err := <-attachDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled attach returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attach pump did not end within 5s of caller cancellation")
	}
}
