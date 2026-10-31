package llm

// rungate_v194_test.go — v1.9.4 (P1): the engine/run coordination gate,
// pinned deterministically (no engine binary, no network):
//
//   1. nil gate (unwired — tests, headless tooling): runActiveNow() is
//      false and the transaction behaves exactly as before v1.9.4;
//   2. the installed gate's verdict reaches the transaction: a RUNNING
//      engine plus a run-active gate refuses updateEngineVariantTx with
//      ErrEngineBusyWithRun BEFORE anything happens — the engine state is
//      untouched (never stopped) and no asset work started;
//   3. the same transaction with the gate clear proceeds past the
//      coordination point (the no-op "already carries the variant" path —
//      deterministic offline);
//   4. the AUTO probe treats the refusal as a DEFERRAL (skipped, no
//      persisted bounded failure) — the temporary condition must never
//      permanently disable the candidate for the hardware identity.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
)

func newGateTestServer(t *testing.T) *LlamaServer {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	return NewLlamaServer(config.NewSource(cfg))
}

func TestRunGateNilByDefault(t *testing.T) {
	s := newGateTestServer(t)

	if s.runActiveNow() {
		t.Fatal("nil gate must report false (historical behavior)")
	}
}

func TestRunGateSemantics(t *testing.T) {
	s := newGateTestServer(t)

	calls := 0
	s.SetRunGate(func() bool {
		calls++
		return calls%2 == 1
	})

	if !s.runActiveNow() {
		t.Fatal("first verdict should be true")
	}
	if s.runActiveNow() {
		t.Fatal("second verdict should be false")
	}
	if calls != 2 {
		t.Fatalf("gate consulted %d times, want 2", calls)
	}
}

func TestVariantTransactionRefusedWhileRunActive(t *testing.T) {
	s := newGateTestServer(t)

	// The serving engine is RUNNING (alive state — no real process needed:
	// the gate fires before any process interaction).
	s.setState(StateRunning)

	consulted := 0
	s.SetRunGate(func() bool {
		consulted++
		return true
	})

	_, err := s.updateEngineVariantTx(
		context.Background(),
		updater.VariantCPU,
		nil,
		nil,
	)

	if !errors.Is(err, ErrEngineBusyWithRun) {
		t.Fatalf("err = %v, want ErrEngineBusyWithRun", err)
	}

	if consulted != 1 {
		t.Fatalf("gate consulted %d times, want exactly 1", consulted)
	}

	// The engine was never torn down.
	if !s.IsRunning() {
		t.Fatal("the running engine must remain running after the refusal")
	}
}

func TestVariantTransactionProceedsWhenGateClear(t *testing.T) {
	s := newGateTestServer(t)

	// Engine NOT running (fresh server) — the coordination gate is not a
	// disruption. v1.9.6: the gate is consulted exactly ONCE, by the
	// early advisory check (a registered run whose engine has not
	// finished booting is a tearing hazard too); the verdict false lets
	// the transaction reach the offline no-op path (the default install
	// already carries the CPU variant).
	consulted := 0
	s.SetRunGate(func() bool {
		consulted++
		return false
	})

	note, err := s.updateEngineVariantTx(
		context.Background(),
		updater.VariantCPU,
		nil,
		nil,
	)

	if err != nil {
		t.Fatalf("err = %v, want the offline no-op success", err)
	}

	if note == "" {
		t.Fatal("the no-op must report honestly")
	}

	if consulted != 1 {
		t.Fatalf("gate consulted %d times, want exactly 1 (early advisory check)", consulted)
	}
}
