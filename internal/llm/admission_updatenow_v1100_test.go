package llm

// admission_updatenow_v1100_test.go — v1.10.0 (P0): UpdateEngineNow (the
// same-tag/scheduled update transaction) now participates in the SAME
// destructive-boundary admission claim seam the v1.9.6 variant
// transaction uses. The pins mirror admission_boundary_v196_test.go
// deterministically through the archive-seam runtime harness (no
// network):
//
//   1. the claim is HELD across the whole destructive window: taken
//      BEFORE the first destructive step (the stop) and released only
//      AFTER the transaction committed and the engine is serving again —
//      observed from the claim seam itself (no sleeps, no polling);
//   2. begin/end balance on the success path: exactly one Begin, exactly
//      one End, mirror false after, EngineMutationActive() false;
//   3. a live verdict AT the boundary refuses with ErrEngineBusyWithRun:
//      no claim taken (ends == 0), no claim held after, and a RUNNING
//      engine is never stopped by the refusal;
//   4. a run active at ENTRY refuses early (advisory, no claim: begins
//      == 0, ends == 0, exactly one gate consult);
//   5. with the admission seam nil, the destructive-boundary re-check
//      closes the between-checks registration race: exactly two gate
//      consults (early + boundary), engine untouched.

import (
    "context"
    "errors"
    "testing"

    "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// updatenowAdmissionSpy is the same seam double as fakeAdmission plus an
// ordered observation of the transaction state at claim-taking and
// claim-release time. Both callbacks run on the transaction goroutine, so
// the recorded snapshots are race-free by construction.
type updatenowAdmissionSpy struct {
    srv *LlamaServer

    liveAtBegin bool

    begins int
    ends   int

    // stateRunningAtBegin / stateAtBegin: what the transaction looked
    // like the instant the claim was taken (BEFORE the stop).
    stateRunningAtBegin bool
    stateAtBegin        string
    claimedAtBegin      bool

    // stateRunningAtEnd / stateAtEnd: what the transaction looked like
    // the instant the claim was released (AFTER commit + verified start).
    stateRunningAtEnd bool
    stateAtEnd        string
}

func (f *updatenowAdmissionSpy) BeginEngineMutation() bool {
    f.begins++

    if f.liveAtBegin {
        return true
    }

    // The server is the claim owner; reading it inside its own callback
    // (same goroutine, this goroutine holds no s.mu here) is race-free.
    f.stateRunningAtBegin = f.srv.IsRunning()
    f.stateAtBegin = f.srv.State()
    f.claimedAtBegin = f.srv.EngineMutationActive()

    return false
}

func (f *updatenowAdmissionSpy) EndEngineMutation() {
    f.ends++

    f.stateRunningAtEnd = f.srv.IsRunning()
    f.stateAtEnd = f.srv.State()
}

// driveUpdateNowHarness builds the deterministic same-tag update harness:
// managed fake engine, staged archive seam (never the network).
func driveUpdateNowHarness(t *testing.T) (*LlamaServer, *config.Config) {
    t.Helper()

    cfg, _ := fakeManagedEngineConfig(t, "")

    srv := NewLlamaServer(config.NewSource(cfg))
    t.Cleanup(func() { _ = srv.Stop() })

    srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

    return srv, cfg
}

// PIN 1+2 — the claim is taken BEFORE the destructive phase, held across
// stop → install → verified start, and released exactly once AFTER the
// engine serves the new tag. Balance: begin/end exactly once, mirror
// cleared.
func TestUpdateNowAdmissionClaimHeldAcrossDestructiveWindow(t *testing.T) {
    srv, _ := driveUpdateNowHarness(t)

    // The engine is RUNNING when the transaction begins: the claim must
    // be taken while the engine still serves, and released only after it
    // serves again (the honest span of "the engine cannot serve" window).
    if err := srv.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }

    spy := &updatenowAdmissionSpy{srv: srv}
    srv.SetAdmissionControl(spy)

    if _, err := srv.UpdateEngineNow(context.Background(), "b14000", nil); err != nil {
        t.Fatalf("same-tag update transaction: %v", err)
    }

    if spy.begins != 1 {
        t.Fatalf("BeginEngineMutation called %d times, want 1 (once, at the boundary)", spy.begins)
    }
    if spy.ends != 1 {
        t.Fatalf("EndEngineMutation called %d times, want exactly 1 (released on the success path)", spy.ends)
    }

    // Held from BEFORE the destructive phase: at Begin the engine was
    // still running (the stop had not happened yet).
    if !spy.stateRunningAtBegin {
        t.Fatalf("at BeginEngineMutation the engine was not running (state %s) — the claim must be taken before the first destructive step", spy.stateAtBegin)
    }
    if spy.claimedAtBegin {
        t.Fatal("EngineMutationActive was already true when BeginEngineMutation ran — a claim leaked from an earlier transaction")
    }

    // Released only AFTER the transaction committed and the verified
    // engine serves again: at End the engine is running with the new tag.
    if !spy.stateRunningAtEnd {
        t.Fatalf("at EndEngineMutation the engine was not running (state %s) — the claim must span the whole non-serving window", spy.stateAtEnd)
    }

    if spy.stateAtEnd != StateReady {
        t.Fatalf("at EndEngineMutation state was %s, want %s (verified start completes before release)", spy.stateAtEnd, StateReady)
    }

    if fa := srv.EngineMutationActive(); fa {
        t.Fatal("EngineMutationActive must be false after the transaction released the claim")
    }
}

// PIN 3 — a live verdict at the destructive boundary refuses: no claim is
// taken, and a RUNNING engine is never stopped by the refusal.
func TestUpdateNowRunLiveAtBoundaryRefusesAndKeepsServing(t *testing.T) {
    srv, _ := driveUpdateNowHarness(t)

    if err := srv.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }

    fa := &fakeAdmission{}
    fa.liveAtBegin.Store(true)
    srv.SetAdmissionControl(fa)

    _, err := srv.UpdateEngineNow(context.Background(), "b14000", nil)
    if !errors.Is(err, ErrEngineBusyWithRun) {
        t.Fatalf("err = %v, want ErrEngineBusyWithRun (boundary live verdict)", err)
    }

    if fa.begins.Load() != 1 {
        t.Fatalf("BeginEngineMutation called %d times, want 1 (the boundary verdict)", fa.begins.Load())
    }
    if fa.ends.Load() != 0 {
        t.Fatal("EndEngineMutation must NOT be called when the verdict was live — no claim was taken")
    }
    if fa.claimed.Load() {
        t.Fatal("no claim may be held after a boundary refusal")
    }

    // The refusal happened at the boundary: the serving engine was never
    // stopped for a refused transaction.
    if !srv.IsRunning() {
        t.Fatal("the serving engine must stay up when the transaction refuses at the boundary")
    }
}

// PIN 4 — a run active at ENTRY refuses early (advisory): no claim, no
// stop, exactly one gate consult.
func TestUpdateNowRunActiveAtEntryRefusesBeforeAnything(t *testing.T) {
    srv, _ := driveUpdateNowHarness(t)

    if err := srv.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }

    consults := 0
    srv.SetRunGate(func() bool {
        consults++
        return true // a run is active right now
    })

    _, err := srv.UpdateEngineNow(context.Background(), "b14000", nil)
    if !errors.Is(err, ErrEngineBusyWithRun) {
        t.Fatalf("err = %v, want ErrEngineBusyWithRun (early advisory refusal)", err)
    }

    if consults != 1 {
        t.Fatalf("run gate consulted %d times, want exactly 1 (early advisory consult; the boundary is never reached)", consults)
    }

    if !srv.IsRunning() {
        t.Fatal("the serving engine must stay up when the transaction refuses at entry")
    }
}

// PIN 5 — nil admission seam: the destructive-boundary re-check alone
// closes the between-checks registration race (early consult false,
// boundary consult true), exactly as the variant transaction's nil-seam
// path does.
func TestUpdateNowNilSeamBoundaryRecheckClosesRegistration(t *testing.T) {
    srv, _ := driveUpdateNowHarness(t)

    if err := srv.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }

    consults := 0
    srv.SetRunGate(func() bool {
        consults++
        return consults >= 2 // early: clear; boundary: a run registered meanwhile
    })

    _, err := srv.UpdateEngineNow(context.Background(), "b14000", nil)
    if !errors.Is(err, ErrEngineBusyWithRun) {
        t.Fatalf("err = %v, want ErrEngineBusyWithRun (boundary re-check)", err)
    }

    if consults != 2 {
        t.Fatalf("gate consulted %d times, want exactly 2 (early + boundary)", consults)
    }

    if !srv.IsRunning() {
        t.Fatal("the serving engine must stay up when the boundary re-check refuses")
    }
}
