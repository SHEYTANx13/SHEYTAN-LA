package llm

// admission_v195_test.go — v1.9.5 (P1), revised v1.9.6: the engine-mutation
// ADMISSION seam, pinned deterministically (no engine binary, no network):
//
//   1. NON-DESTRUCTIVE exits never touch admission: eligibility walks, the
//      already-installed no-op and the early live-run refusal take and
//      release NO claim (v1.9.6 moved the claim to the destructive
//      boundary — the network/download phases must never hold run
//      admission hostage); the destructive-boundary claim itself is pinned
//      in admission_boundary_v196_test.go;
//   2. the early registry refusal: a run active BEFORE the transaction
//      refuses it with ErrEngineBusyWithRun and NEVER takes or releases a
//      claim — the engine is untouched;
//   3. EngineMutationActive mirrors the held claim (the advisory signal
//      the API admission pre-checks read);
//   4. the historical nil-seam behavior is preserved: the
//      destructive-boundary run gate alone.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
)

// fakeAdmission is the deterministic seam double. begin records what the
// transaction observed; released counts EndEngineMutation calls.
type fakeAdmission struct {
	liveAtBegin atomic.Bool
	begins      atomic.Int64
	ends        atomic.Int64
	claimed     atomic.Bool
}

func (f *fakeAdmission) BeginEngineMutation() bool {
	f.begins.Add(1)

	if f.liveAtBegin.Load() {
		return true
	}

	f.claimed.Store(true)

	return false
}

func (f *fakeAdmission) EndEngineMutation() {
	f.ends.Add(1)
	f.claimed.Store(false)
}

func TestAdmissionNonDestructiveExitsNeverClaim(t *testing.T) {
	s := newGateTestServer(t)

	fa := &fakeAdmission{}
	s.SetAdmissionControl(fa)

	// Offline no-op transaction (fresh server, CPU variant already
	// installed): the no-op is a NON-DESTRUCTIVE exit — nothing is
	// stopped, swapped or started, so no run-admission claim may be
	// taken for it (the v1.9.6 boundary contract; the pre-v1.9.6
	// claim-at-start placement held the claim across the whole
	// transaction window including the network phases).
	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantCPU, nil, nil)
	if err != nil {
		t.Fatalf("offline no-op transaction: %v", err)
	}

	if fa.begins.Load() != 0 {
		t.Fatalf("BeginEngineMutation called %d times, want 0 (non-destructive exit never claims)", fa.begins.Load())
	}
	if fa.ends.Load() != 0 {
		t.Fatalf("EndEngineMutation called %d times, want 0 (nothing to release)", fa.ends.Load())
	}
	if fa.claimed.Load() {
		t.Fatal("claim held after a non-destructive exit")
	}
	if s.EngineMutationActive() {
		t.Fatal("EngineMutationActive must be false after a non-destructive exit")
	}
}

func TestAdmissionLiveRunRefusesAndNeverClaims(t *testing.T) {
	s := newGateTestServer(t)

	// The serving engine is RUNNING: the coordination point is live.
	// v1.9.6: the run-active refusal is the EARLY registry check (the
	// same registry the admission claim reads), consulted read-only —
	// the transaction is refused before anything happens and the
	// admission seam is never engaged.
	s.setState(StateRunning)

	consulted := 0
	s.SetRunGate(func() bool {
		consulted++
		return true
	})

	fa := &fakeAdmission{}
	s.SetAdmissionControl(fa)

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantCPU, nil, nil)
	if !errors.Is(err, ErrEngineBusyWithRun) {
		t.Fatalf("err = %v, want ErrEngineBusyWithRun", err)
	}

	if consulted != 1 {
		t.Fatalf("run gate consulted %d times, want exactly 1 (early refusal)", consulted)
	}
	if fa.begins.Load() != 0 {
		t.Fatalf("BeginEngineMutation called %d times, want 0 (refusal never claims)", fa.begins.Load())
	}
	if fa.ends.Load() != 0 {
		t.Fatal("EndEngineMutation must NOT be called when no claim was taken")
	}
	if fa.claimed.Load() {
		t.Fatal("no claim may be taken when a run is already live")
	}
	if !s.IsRunning() {
		t.Fatal("the running engine must remain running after the refusal")
	}
}

func TestEngineMutationActiveMirrorsClaim(t *testing.T) {
	s := newGateTestServer(t)

	if s.EngineMutationActive() {
		t.Fatal("no claim installed — must report false")
	}

	fa := &fakeAdmission{}
	s.SetAdmissionControl(fa)

	if s.EngineMutationActive() {
		t.Fatal("installed but never begun — must report false")
	}

	// Simulate exactly what the transaction does between Begin and End.
	if live := fa.BeginEngineMutation(); live {
		t.Fatal("BeginEngineMutation must report not-live for an empty registry")
	}

	s.mu.Lock()
	s.mutationClaimed = true
	s.mu.Unlock()

	if !s.EngineMutationActive() {
		t.Fatal("held claim must be reported active")
	}

	fa.EndEngineMutation()

	s.mu.Lock()
	s.mutationClaimed = false
	s.mu.Unlock()

	if s.EngineMutationActive() {
		t.Fatal("released claim must be reported inactive")
	}
}

func TestNilAdmissionKeepsHistoricalGate(t *testing.T) {
	s := newGateTestServer(t)

	s.setState(StateRunning)

	consulted := 0
	s.SetRunGate(func() bool {
		consulted++
		return true
	})

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantCPU, nil, nil)
	if !errors.Is(err, ErrEngineBusyWithRun) {
		t.Fatalf("err = %v, want ErrEngineBusyWithRun (historical gate)", err)
	}
	if consulted != 1 {
		t.Fatalf("gate consulted %d times, want exactly 1", consulted)
	}
}
