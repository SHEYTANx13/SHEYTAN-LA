package api

import (
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// Close stops active agent runs and releases the resources owned by the
// shared runtime stack.
//
// The API server is the owner of the Stack instance created by New(), so
// shutting down the API must also shut down the runtime resources it owns.
func (s *Server) Close() {
	if s == nil {
		return
	}

	// v1.10.1: refuse NEW run registrations FIRST — after this store, the
	// runWG Add/Wait contract is closed and the join below can never miss
	// a late registration.
	s.closing.Store(true)

	// v1.2.3: stop an in-flight app-update staging download immediately —
	// the .part file stays for a resumable retry next launch.
	if fn := s.appUpdateCancel.Load(); fn != nil {
		(*fn)()
	}
	if job := s.appUpdateJob.Load(); job != nil {
		job.Cancel()
	}

	// v1.11.0: the 3D Studio viewer must never outlive the server —
	// no orphaned loopback listener, no stale launch token.
	if s.studioViewer != nil {
		if err := s.studioViewer.Stop(); err != nil {
			logging.Default().Warn("studio", "viewer stop during close: %v", err)
		}
	}

	// Stop the scheduled engine-update loop FIRST and wait (bounded) for
	// its in-flight pass to finish.
	//
	// v1.2.0: the updater goroutine was previously never cancelled — it
	// outlived the server and, in the test suite, kept writing into the
	// temp data dir while t.TempDir() cleanup ran RemoveAll, producing
	// the observed "bin\.update-stage: The directory is not empty"
	// failures. Deterministic ownership: cancel → wait → then tear down.
	s.updateCancelIfNeeded()

	// Cancel every active HTTP-triggered run.
	//
	// Do not hold runsMu while calling into the runtime; cancellation can
	// cause callbacks/goroutines to touch the run registry.
	s.runsMu.Lock()
	cancels := make([]func(), 0, len(s.runs))

	for _, rs := range s.runs {
		if rs != nil && rs.cancel != nil {
			cancels = append(cancels, rs.cancel)
		}
	}

	s.runsMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	// The orchestrator itself needs no explicit abort: every run it
	// executes is bound to the request context canceled above, so
	// canceling the registered runs stops all orchestrator work.

	// v1.10.1 (P0, Windows run 38152579695): JOIN every run goroutine
	// (bounded) before releasing shared resources below. A run goroutine's
	// deferred cleanup — memory-manager run-end bookkeeping, registry
	// release, hub close — runs AFTER its terminal frame is published, so
	// any waiter that stops at the terminal frame (or at cancel()) can
	// race that cleanup. In the test suite that race materialized as
	// t.TempDir RemoveAll failing with "The directory is not empty" while
	// post-terminal bookkeeping was still writing inside the data dir.
	// The wait is bounded: every run goroutine is cancellation-aware by
	// construction, so the bound only fires on a genuinely stuck cleanup,
	// and shutdown must stay finite even then.
	runDone := make(chan struct{})
	go func() {
		s.runWG.Wait()
		close(runDone)
	}()

	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		logging.Default().Warn("api", "bounded wait: run goroutines did not join within 5s during shutdown")
	}

	// Stop the engine event fan-out before tearing down the engine it
	// subscribes to.
	if s.engineStop != nil {
		select {
		case <-s.engineStop:
		default:
			close(s.engineStop)
		}
	}

	if s.engineDone != nil {
		select {
		case <-s.engineDone:
		case <-time.After(2 * time.Second):
		}
	}

	// Release the shared runtime resources.
	if s.stack != nil {
		s.stack.Close()
	}
}

// updateCancelIfNeeded cancels the scheduled engine-update loop and waits
// up to 3 seconds for its completion signal. Safe on servers that never
// started the loop.
//
// v1.7.3: the handles are read under updateMu — the same mutex that
// publishes them in EnsureSetup — so a concurrent (or repeated) startup
// can never race the teardown into using a stale or half-published pair.
func (s *Server) updateCancelIfNeeded() {
	s.updateMu.Lock()
	cancel := s.updateCancel
	done := s.updateDone
	s.updateMu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
}
