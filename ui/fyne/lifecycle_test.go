//go:build !headless

// lifecycle_test.go — regression tests for the ONE idempotent shutdown
// coordinator (work order for run 43008361835 §F2/§5.2): repeated and
// racing close requests run exactly one teardown; close during an active
// activity stream cancels the pump and completes gracefully; a pump that
// ignores cancellation produces an INCOMPLETE, non-graceful result after
// the bounded deadline — never a hang, never a false "graceful" record.
package fyneui

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// armedApp builds an App whose lifecycle is armed the way Run() arms it,
// without starting the event loop (unit seam: the coordinator, root context
// and runtime are real; the window stays nil).
func armedApp(t *testing.T, client *CoreClient, deadline time.Duration) *App {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())
	ap := NewApp(test.NewApp(), client)
	ap.rootCtx, ap.cancel = context.WithCancel(context.Background())
	t.Cleanup(ap.cancel)
	ap.coord = newShutdownCoordinator(ap, deadline)
	return ap
}

// blockingDialClient builds a CoreClient whose activity-stream dial blocks
// until the test releases it. honorsCtx=true: the dial aborts when the
// passed context is cancelled (a well-behaved pump); honorsCtx=false: the
// dial ignores cancellation entirely (a stuck pump, released only by the
// test at cleanup).
func blockingDialClient(t *testing.T, honorsCtx bool) (*CoreClient, *atomic.Int32) {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var dialed atomic.Int32
	c := NewCoreClientForTest("127.0.0.1:1", "test-key") // endpoint never reached: dial blocks first
	c.wsdial.NetDialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialed.Add(1)
		if honorsCtx {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		<-release
		return nil, context.Canceled
	}
	return c, &dialed
}

// TestShutdownCoordinatorRunsExactlyOneTeardown: ten racing close requests
// share ONE teardown (a second teardown would close c.done twice and panic)
// and the result is graceful with the pump cancelled.
func TestShutdownCoordinatorRunsExactlyOneTeardown(t *testing.T) {
	client, dialed := blockingDialClient(t, true)
	ap := armedApp(t, client, 2*time.Second)

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- ap.rt.AttachActivity(ap.Ctx(), "s1") }()
	waitFor(t, func() bool { return dialed.Load() == 1 }, "pump dial")

	for i := 0; i < 10; i++ {
		go ap.RequestClose()
	}
	ap.RequestClose() // racing close from the "signal handler" path too

	select {
	case <-ap.coord.wait():
	case <-time.After(3 * time.Second):
		t.Fatal("teardown never completed")
	}
	res := ap.coord.snapshotResult()
	if !res.Graceful || res.Incomplete {
		t.Fatalf("expected graceful result, got %+v", res)
	}
	if res.Duration < 0 {
		t.Fatalf("negative teardown duration: %+v", res)
	}
	// The pump observed cancellation and exited.
	select {
	case <-pumpDone:
	case <-time.After(2 * time.Second):
		t.Fatal("activity pump never observed cancellation")
	}
	if st := ap.LifecycleState(); st != "closing" {
		t.Fatalf("state before window close = %q, want closing (closed is set by SetOnClosed)", st)
	}
}

// TestShutdownDeadlineMarksIncomplete: a pump that IGNORES cancellation
// still cannot hang the close — the bounded deadline expires, the result is
// recorded as INCOMPLETE (never graceful), and the teardown completes so
// the window close proceeds.
func TestShutdownDeadlineMarksIncomplete(t *testing.T) {
	client, dialed := blockingDialClient(t, false)
	ap := armedApp(t, client, 50*time.Millisecond)

	pumpDone := make(chan error, 1)
	go func() { pumpDone <- ap.rt.AttachActivity(ap.Ctx(), "s1") }()
	waitFor(t, func() bool { return dialed.Load() == 1 }, "pump dial")

	ap.RequestClose()
	select {
	case <-ap.coord.wait():
	case <-time.After(3 * time.Second):
		t.Fatal("teardown hung past its deadline")
	}
	res := ap.coord.snapshotResult()
	if res.Graceful {
		t.Fatal("a deadline-exceeded shutdown must never be recorded graceful")
	}
	if !res.Incomplete {
		t.Fatal("expected incomplete=true after the bounded deadline")
	}
	if res.Err == nil {
		t.Fatal("expected the deadline error to be preserved in the result")
	}
}

// TestShutdownIdleCloseIsGraceful: closing while idle tears down the
// workspace hub and runtime without error and records a graceful result.
func TestShutdownIdleCloseIsGraceful(t *testing.T) {
	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	ap := armedApp(t, srv.client(), 2*time.Second)

	ap.RequestClose()
	select {
	case <-ap.coord.wait():
	case <-time.After(3 * time.Second):
		t.Fatal("idle close never completed")
	}
	res := ap.coord.snapshotResult()
	if !res.Graceful {
		t.Fatalf("idle close must be graceful, got %+v", res)
	}
}

// TestCloseBeforeRunIsSafe: Close() on an un-armed App (no coordinator, no
// window — e.g. an early signal) must not panic.
func TestCloseBeforeRunIsSafe(t *testing.T) {
	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	t.Cleanup(SetTestUIMarshal())
	ap := NewApp(test.NewApp(), srv.client())
	ap.Close() // nil coordinator AND nil window: degrades to a no-op
	if ap.LifecycleState() != "running" {
		t.Fatalf("un-armed app state = %q, want running", ap.LifecycleState())
	}
}

// TestMarkClosedTransitions: SetOnClosed bookkeeping moves closing → closed
// and never resurrects a completed lifecycle.
func TestMarkClosedTransitions(t *testing.T) {
	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	ap := armedApp(t, srv.client(), 1*time.Second)

	ap.coord.markClosed()
	if got := ap.LifecycleState(); got != "closed" {
		t.Fatalf("markClosed from running = %q, want closed", got)
	}
	ap.coord.markClosed()
	if got := ap.LifecycleState(); got != "closed" {
		t.Fatalf("markClosed must be idempotent, got %q", got)
	}
}
