// Startup maintenance gate regression tests (v1.6.0, spec §5).
//
// These tests exist to fail against the v1.5.1 ordering and pass against
// the v1.6.0 ordering. They are deterministic: every synchronization is
// a channel — no sleep is required for correctness (spec §5 test 8),
// and none is used.
//
// Covered cases (spec §5):
//
//  1. no-update startup → prewarm occurs after the maintenance check
//  2. required-update startup → update completes before prewarm
//  3. failed update → the current valid engine survives (prewarm continues)
//  4. required incompatible update failure → prewarm is BLOCKED
//  5. no selected model → no engine startup (model-first)
//  6. repeated startup does not trigger duplicate maintenance/prewarm
//  7. (internal/llm/maintenance_stop_test.go) deliberate engine stop
//     during maintenance is not classified as a crash
//  8. no arbitrary sleep — structural: this file uses channels only
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// decodeJSON is the shared test decoder for the maintenance surface.
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// maintenanceTestHarness wires a Server with deterministic gate seams.
type maintenanceTestHarness struct {
	srv    *Server
	cfg    *config.Config
	events *orderedEvents
}

// orderedEvents records the global ordering of maintenance/prewarm
// milestones. Channel-free reads use the mutex; ordering is asserted
// from the recorded sequence, never from timing.
type orderedEvents struct {
	mu   sync.Mutex
	seq  []string
	cond *sync.Cond
}

func newOrderedEvents() *orderedEvents {
	e := &orderedEvents{}
	e.cond = sync.NewCond(&e.mu)
	return e
}

func (e *orderedEvents) record(event string) {
	e.mu.Lock()
	e.seq = append(e.seq, event)
	e.cond.Broadcast()
	e.mu.Unlock()
}

func (e *orderedEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.seq))
	copy(out, e.seq)
	return out
}

// waitUntil blocks until the predicate over the snapshot holds (or the
// deadline expires). Deterministic notification via the cond var.
func (e *orderedEvents) waitUntil(t *testing.T, pred func([]string) bool, what string) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	e.mu.Lock()
	defer e.mu.Unlock()

	for {
		snap := make([]string, len(e.seq))
		copy(snap, e.seq)

		if pred(snap) {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; events so far: %v", what, snap)
		}

		// Condvar wait with a timeout kicker.
		e.mu.Unlock()
		time.AfterFunc(50*time.Millisecond, func() {
			e.mu.Lock()
			e.cond.Broadcast()
			e.mu.Unlock()
		})
		e.mu.Lock()
		e.cond.Wait()
	}
}

func (e *orderedEvents) has(event string) func([]string) bool {
	return func(seq []string) bool {
		for _, s := range seq {
			if s == event {
				return true
			}
		}
		return false
	}
}

// newMaintenanceTestServer builds a Server whose gate + prewarm are fully
// faked. schedule defaults to "daily" with a long-expired last check so
// the maintenance decision is DUE.
func newMaintenanceTestServer(t *testing.T, model string, autoStart bool) *maintenanceTestHarness {
	t.Helper()

	netcheck.SetProbe(func() bool { return true })
	t.Cleanup(func() { netcheck.SetProbe(nil) })

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = cfg.DataDir + "/models"
	cfg.SessionsDir = cfg.DataDir + "/sessions"
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = autoStart
	cfg.UpdateSchedule = "daily"
	cfg.LastUpdateCheck = "2020-01-01T00:00:00Z" // long expired → due
	cfg.Model = model
	cfg.LlamaBinPath = "" // no engine binary in these tests

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	return &maintenanceTestHarness{srv: srv, cfg: cfg}
}

// wire fakes the update transaction: records milestones and blocks until
// released (spec §5 test 2's deterministic hold).
func (h *maintenanceTestHarness) wire(
	events *orderedEvents,
	latest func(ctx context.Context) (string, error),
	perform func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error),
	validate func() error,
	prewarm func(),
) *orderedEvents {
	h.srv.seams = &maintenanceSeams{
		latestTag:         latest,
		performUpdate:     perform,
		validateInstalled: validate,
	}

	if prewarm != nil {
		h.srv.prewarmHook = func() {
			events.record("prewarm")
			prewarm()
		}
	} else {
		h.srv.prewarmHook = func() { events.record("prewarm") }
	}

	return events
}

// currentTagOf returns the tag the gate will compare against: the
// recorded engine tag, or the bundled default when none is recorded.
func currentTagOf(h *maintenanceTestHarness) string {
	if tag := updater.InstalledEngineTag(h.cfg); tag != "" {
		return tag
	}
	return updater.DefaultEngineTag
}

// ---------------------------------------------------------------------------
// Case 1 — no update required: exactly one prewarm, AFTER the check.
// ---------------------------------------------------------------------------

func TestMaintenanceNoUpdatePrewarmAfterCheck(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	current := currentTagOf(h)

	events := newOrderedEvents()
	events = h.wire(
		events,
		func(ctx context.Context) (string, error) { return current, nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			t.Fatal("performUpdate must not run when the engine is current")
			return "", nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	// The gate must reach a terminal phase and release the prewarm.
	events.waitUntil(t, events.has("prewarm"), "prewarm after a no-op maintenance check")

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseReadyForPrewarm {
		t.Fatalf("phase = %s, want READY_FOR_PREWARM (engine current)", st.Phase)
	}
	if !st.PrewarmPermitted {
		t.Fatal("a current engine must permit prewarm")
	}
	if st.Updated {
		t.Fatal("no update must report Updated=false")
	}

	// Case 6 (single-call half): one EnsureSetup → exactly ONE prewarm.
	if got := len(events.snapshot()); got != 1 || events.snapshot()[0] != "prewarm" {
		t.Fatalf("expected exactly one prewarm event, got %v", events.snapshot())
	}
}

// ---------------------------------------------------------------------------
// Case 2 — required update: the transaction completes BEFORE prewarm.
// This is the test that FAILS against the v1.5.1 ordering (the prewarm
// used to fire synchronously inside EnsureSetup, before the updater even
// started, and the updater then stopped that engine mid-boot).
// ---------------------------------------------------------------------------

func TestMaintenanceRequiredUpdateCompletesBeforePrewarm(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	updateStarted := make(chan struct{})
	releaseUpdate := make(chan struct{})
	enginePassed := make(chan updater.Engine, 1)

	events := newOrderedEvents()
	h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b99999", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			enginePassed <- eng
			events.record("update-start")

			close(updateStarted)

			<-releaseUpdate // hold the transaction open — deterministically

			events.record("update-end")
			return "engine updated to llama.cpp b99999", nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	<-updateStarted

	// INVARIANT (the whole point of P0): while the maintenance
	// transaction is in flight, the prewarm has NOT happened. Against
	// the v1.5.1 code the prewarm event already exists here — no sleep
	// needed to expose it, the sequence itself is the proof.
	if seq := events.snapshot(); len(seq) != 1 || seq[0] != "update-start" {
		t.Fatalf("prewarm ran before maintenance completed — the v1.5.1 ordering bug: %v", seq)
	}

	// The model is selected → the gate must drive the ENGINE-owned
	// transaction (not the model-less install-only path).
	select {
	case eng := <-enginePassed:
		if eng == nil {
			t.Fatal("with a selected model the update must run through the engine transaction")
		}
	default:
		t.Fatal("performUpdate not called")
	}

	close(releaseUpdate)

	// After the transaction, and only after it, the prewarm is released.
	events.waitUntil(t, func(seq []string) bool {
		return len(seq) >= 2 && seq[0] == "update-start" && seq[1] == "update-end" && seq[len(seq)-1] == "prewarm"
	}, "prewarm strictly after update-end")

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseReadyForPrewarm {
		t.Fatalf("phase = %s, want READY_FOR_PREWARM after a successful update", st.Phase)
	}
	if !st.Updated || st.TargetTag != "b99999" {
		t.Fatalf("update bookkeeping wrong: updated=%v tag=%q", st.Updated, st.TargetTag)
	}
}

// ---------------------------------------------------------------------------
// Case 3 — failed update, existing engine still valid: startup continues
// with the last-known-good engine (FAILED, prewarm permitted).
// ---------------------------------------------------------------------------

func TestMaintenanceFailedUpdateKeepsKnownGoodEngine(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	events := newOrderedEvents()
	h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b99999", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			events.record("update-failed")
			return "", context.DeadlineExceeded
		},
		func() error { return nil }, // installed engine statically valid
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm released after a failed-but-recoverable update")

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseFailed {
		t.Fatalf("phase = %s, want FAILED", st.Phase)
	}
	if !st.PrewarmPermitted {
		t.Fatal("a valid last-known-good engine must permit prewarm (Case C)")
	}
	if st.Reason != "update-failed-engine-valid" {
		t.Fatalf("reason = %q, want update-failed-engine-valid", st.Reason)
	}
	if st.Detail == "" {
		t.Fatal("the failure detail must carry the real cause")
	}
}

// ---------------------------------------------------------------------------
// Case 4 — failed update AND unusable installed engine: prewarm BLOCKED.
// ---------------------------------------------------------------------------

func TestMaintenanceFailedUpdateWithUnusableEngineBlocksPrewarm(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	events := newOrderedEvents()
	h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b99999", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			return "", context.DeadlineExceeded
		},
		func() error { return &staticValidationError{"engine binary fails static validation"} },
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	// Wait for the gate to finish (BLOCKED is terminal) — then prove the
	// prewarm NEVER fires.
	events.waitUntil(t, func(seq []string) bool {
		return h.srv.gateValue().Snapshot().Phase == PhaseBlocked
	}, "the gate to reach BLOCKED")

	if seq := events.snapshot(); len(seq) != 0 {
		t.Fatalf("prewarm must never fire while BLOCKED, got events %v", seq)
	}

	st := h.srv.gateValue().Snapshot()
	if st.PrewarmPermitted {
		t.Fatal("BLOCKED must refuse the prewarm")
	}
	if st.Reason != "update-failed-engine-unusable" {
		t.Fatalf("reason = %q, want update-failed-engine-unusable", st.Reason)
	}
}

type staticValidationError struct{ msg string }

func (e *staticValidationError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Case 5 — no selected model: no engine startup, model-first preserved;
// the engine FILES may still be maintained (install-only path).
// ---------------------------------------------------------------------------

func TestMaintenanceNoModelInstallsFilesButNeverStartsEngine(t *testing.T) {
	h := newMaintenanceTestServer(t, "", true) // NO model selected

	enginePassed := make(chan updater.Engine, 1)
	prewarmFired := make(chan struct{}, 1)

	events := newOrderedEvents()
	h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b99999", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			enginePassed <- eng
			events.record("update-done")
			return "engine updated to llama.cpp b99999", nil
		},
		nil,
		func() { prewarmFired <- struct{}{} },
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("update-done"), "the install-only maintenance to complete")

	// v1.7.3 determinism repair: "update-done" is recorded inside the
	// performUpdate seam BEFORE the gate goroutine publishes the
	// terminal phase — the previous immediate Snapshot read raced that
	// publication (observed once on the loaded race suite as
	// "phase = MAINTENANCE_REQUIRED, want READY_FOR_PREWARM"). The
	// gate's own Done() channel is the authority: terminal phase is
	// published BEFORE close, so waiting on the channel is
	// happens-before deterministic. No sleeps.
	select {
	case <-h.srv.gateValue().Done():
	case <-time.After(15 * time.Second):
		t.Fatalf("the maintenance gate never reached a terminal phase; last snapshot: %+v", h.srv.gateValue().Snapshot())
	}

	// Model-first: with no selection the engine is never STARTED — the
	// gate performs a file-level install through a NIL engine (no
	// start/restart authority at all).
	select {
	case eng := <-enginePassed:
		if eng != nil {
			t.Fatal("no model selected → the update must be install-only (nil engine), never a start/verify boot")
		}
	default:
		t.Fatal("performUpdate not called")
	}

	// And the prewarm must NOT have started the engine either.
	select {
	case <-prewarmFired:
		t.Fatal("no engine may be started merely because auto-start is enabled (model-first)")
	default:
	}

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseReadyForPrewarm {
		t.Fatalf("phase = %s, want READY_FOR_PREWARM after install-only maintenance", st.Phase)
	}
}

// ---------------------------------------------------------------------------
// Case 6 — repeated startup: no duplicate maintenance, no duplicate
// prewarm.
// ---------------------------------------------------------------------------

func TestMaintenanceRepeatedStartupIsSingleFlight(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	current := currentTagOf(h)

	var checks atomic.Int32

	events := newOrderedEvents()
	h.wire(
		events,
		func(ctx context.Context) (string, error) {
			checks.Add(1)
			return current, nil
		},
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			t.Fatal("performUpdate must not run when the engine is current")
			return "", nil
		},
		nil,
		nil,
	)

	for i := 0; i < 3; i++ {
		if err := h.srv.EnsureSetup(); err != nil {
			t.Fatalf("EnsureSetup #%d: %v", i+1, err)
		}
	}

	events.waitUntil(t, events.has("prewarm"), "the single prewarm after repeated EnsureSetup")

	// Repeated startup must reuse the SAME gate: one release check,
	// exactly one prewarm — no duplicate maintenance.
	if got := checks.Load(); got != 1 {
		t.Fatalf("release checks = %d, want exactly 1 (the gate is single-flight)", got)
	}

	count := 0
	for _, e := range events.snapshot() {
		if e == "prewarm" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("prewarm events = %d, want exactly 1", count)
	}
}

// ---------------------------------------------------------------------------
// /api/maintenance surface: deterministic shape, live during startup.
// ---------------------------------------------------------------------------

func TestMaintenanceEndpointExposesGateState(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	release := make(chan struct{})

	h.wire(
		newOrderedEvents(),
		func(ctx context.Context) (string, error) {
			<-release
			return "b99999", nil
		},
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			return "engine updated to llama.cpp b99999", nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	server := httptest.NewServer(h.srv.Handler())
	t.Cleanup(server.Close)

	// While the release-check is blocked the endpoint must expose the
	// live phase — CHECKING — with the deterministic shape.
	resp, err := http.Get(server.URL + "/api/maintenance")
	if err != nil {
		t.Fatalf("GET /api/maintenance: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var st MaintenanceStatus
	if err := decodeJSON(resp.Body, &st); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if st.Phase != PhaseChecking {
		t.Fatalf("live phase = %s, want CHECKING", st.Phase)
	}
	if st.PrewarmPermitted {
		t.Fatal("CHECKING must not yet permit the prewarm")
	}

	close(release)

	deadline := time.Now().Add(15 * time.Second)

	// HTTP-surface polling ticker — the gate itself synchronizes on
	// channels; this poll only re-reads the endpoint.
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()

	for time.Now().Before(deadline) {
		<-poll.C

		resp2, err := http.Get(server.URL + "/api/maintenance")
		if err != nil {
			t.Fatalf("GET /api/maintenance (settled): %v", err)
		}

		var st2 MaintenanceStatus
		err = decodeJSON(resp2.Body, &st2)
		resp2.Body.Close()

		if err != nil {
			t.Fatalf("decode settled: %v", err)
		}

		if st2.Phase == PhaseReadyForPrewarm && st2.Updated && st2.FinishedAt != nil {
			return // terminal state with bookkeeping — the contract
		}

	}

	t.Fatal("the gate never reached READY_FOR_PREWARM(updated) on /api/maintenance")
}

// ---------------------------------------------------------------------------
// Deferred phases: schedule off and not-due release the prewarm without
// any network check.
// ---------------------------------------------------------------------------

func TestMaintenanceScheduleOffDefersImmediately(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)
	h.cfg.UpdateSchedule = "off"

	h.srv.seams = &maintenanceSeams{
		latestTag: func(ctx context.Context) (string, error) {
			t.Fatal("no release check may run when updates are off")
			return "", nil
		},
	}

	events := newOrderedEvents()
	h.srv.prewarmHook = func() { events.record("prewarm") }

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm after a DEFERRED gate")

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseDeferred || st.Reason != "updates-off" {
		t.Fatalf("phase/reason = %s/%s, want DEFERRED/updates-off", st.Phase, st.Reason)
	}
}

func TestMaintenanceNotDueSkipsCheck(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)
	h.cfg.LastUpdateCheck = time.Now().UTC().Format(time.RFC3339) // just checked → not due

	h.srv.seams = &maintenanceSeams{
		latestTag: func(ctx context.Context) (string, error) {
			t.Fatal("no release check may run when not due")
			return "", nil
		},
	}

	events := newOrderedEvents()
	h.srv.prewarmHook = func() { events.record("prewarm") }

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm after a not-due gate")

	st := h.srv.gateValue().Snapshot()
	if st.Phase != PhaseDeferred || st.Reason != "not-due" {
		t.Fatalf("phase/reason = %s/%s, want DEFERRED/not-due", st.Phase, st.Reason)
	}
}

// ---------------------------------------------------------------------------
// RunScheduledAfter: the scheduled updater's first pass is held behind
// the gate (updater-side half of the ordering invariant).
// ---------------------------------------------------------------------------

func TestRunScheduledAfterWaitsForGate(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.UpdateSchedule = "off" // the pass itself is a no-op; only the gating is under test
	src := config.NewSource(cfg)

	gate := make(chan struct{})

	passRan := make(chan struct{}, 4)

	done := updater.RunScheduledAfter(
		context.Background(),
		src,
		nil,
		func(string) { passRan <- struct{}{} },
		func() {},
		gate,
	)

	select {
	case <-passRan:
		t.Fatal("the scheduled first pass ran BEFORE the startup gate completed")
	case <-done:
		t.Fatal("the scheduled loop exited while the gate was still closed")
	case <-time.After(300 * time.Millisecond):
		// held — exactly the required behavior; a bounded negative probe,
		// not an ordering sleep.
	}

	close(gate)

	select {
	case <-passRan:
		// The gated (no-op) pass ran, then the loop parked on its 6h
		// ticker — the deterministic shutdown contract stays intact.
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduled loop never completed its gated first pass")
	}

	// The loop must stay alive (parked on the ticker) until cancelled.
	select {
	case <-done:
		t.Fatal("the loop terminated without cancellation")
	default:
	}
}

// ---------------------------------------------------------------------------
// Structural (spec §5 test 8): the ordering proofs above synchronize on
// channels exclusively — this test pins that contract by refusing any
// coarse ordering sleep in this file.
// ---------------------------------------------------------------------------

func TestMaintenanceTestsUseNoCoarseSleepForOrdering(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("source location unavailable")
	}

	data, err := os.ReadFile(thisFile)
	if err != nil {
		t.Skipf("source unreadable: %v", err)
	}

	src := string(data)

	// Strip string literals first so the banned-pattern list below cannot
	// match itself.
	stripped := regexp.MustCompile(`"[^"`+"\n"+`"]*"`).ReplaceAllString(src, "")

	for _, banned := range []string{"time.Sleep(1", "time.Sleep(2", "time.Sleep(5"} {
		if strings.Contains(stripped, banned) {
			t.Fatalf("ordering tests must synchronize on channels, found %q", banned)
		}
	}
}
