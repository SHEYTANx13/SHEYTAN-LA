package api

// maintenance_identity_v176_test.go — v1.7.6: the identity fallback matrix
// extended with the CORRUPT-identity cases (§6 matrix rows 7–8). The
// v1.7.5 matrix (maintenance_twoboot_v175_test.go) covers missing state,
// missing manifest and both missing; corruption is a different failure
// class: the file EXISTS but cannot be trusted. The gate must treat
// untrustworthy evidence exactly like absent evidence — an honest
// transaction when no OTHER authoritative identity exists, and a
// transparent fallback when one does. Never a fabricated "current".
//
// Row 7: state identity corrupt/invalid + valid manifest
//        → EffectiveInstalledEngineTag falls back to the manifest →
//        current, ZERO transactions (missing identity must not force a
//        redundant download when another committed identity exists).
// Row 8: binary exists but identity cannot be trusted (state corrupt AND
//        manifest missing) → honest transaction targeting the latest tag.

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

func TestMaintenanceGateCorruptStateIdentityFallsBackToManifest(t *testing.T) {
	const latest = "b11223"

	h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
	commitEngineTransaction(t, h.cfg, latest)

	// Corrupt the recorded state: invalid JSON — the file exists but its
	// identity cannot be trusted.
	if err := os.WriteFile(h.cfg.StatePath(), []byte(`{"appVersion":"1.7`), 0o644); err != nil {
		t.Fatalf("corrupt state file: %v", err)
	}

	// The identity machinery must fall back to the committed manifest
	// beside the binary — no redundant download for a recoverable case.
	if tag := updater.EffectiveInstalledEngineTag(h.cfg); tag != latest {
		t.Fatalf("identity from corrupt state + valid manifest = %q, want %q (the manifest is the fallback)", tag, latest)
	}

	assertGateCurrentWithoutTransaction(t, h, latest)
}

func TestMaintenanceGateUntrustworthyIdentityRunsHonestTransaction(t *testing.T) {
	const latest = "b11223"

	h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
	commitEngineTransaction(t, h.cfg, latest)

	// BOTH identity sources are untrustworthy: the state is corrupt AND
	// the manifest beside the (still present) binary is gone. The binary
	// alone proves nothing about WHICH build it is — currency cannot be
	// proven, so the gate must run the honest transaction.
	if err := os.WriteFile(h.cfg.StatePath(), []byte(`not json at all`), 0o644); err != nil {
		t.Fatalf("corrupt state file: %v", err)
	}

	if err := os.Remove(filepath.Join(updater.EngineBinDir(h.cfg), "engine-install.json")); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}

	if tag := updater.EffectiveInstalledEngineTag(h.cfg); tag != "" {
		t.Fatalf("identity from corrupt state + missing manifest = %q, want \"\" (untrustworthy)", tag)
	}

	var ran atomic.Bool

	events := newOrderedEvents()
	events = h.wire(
		events,
		func(ctx context.Context) (string, error) { return latest, nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			ran.Store(true)

			if tag != latest {
				t.Errorf("transaction targeted %q, want %q", tag, latest)
			}

			commitEngineTransaction(t, h.cfg, tag)

			return "engine updated to llama.cpp " + tag, nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm after the honest transaction")

	if !ran.Load() {
		t.Fatal("with NO trustworthy identity evidence the gate must run the honest transaction")
	}

	st := h.srv.gateValue().Snapshot()

	if st.Phase != PhaseReadyForPrewarm || !st.Updated {
		t.Fatalf("phase=%s updated=%v — the honest transaction must complete", st.Phase, st.Updated)
	}

	// The transaction restored a TRUSTWORTHY identity for the next boot.
	if tag := updater.EffectiveInstalledEngineTag(h.cfg); tag != latest {
		t.Fatalf("identity after the honest transaction = %q, want %q", tag, latest)
	}
}

func TestMaintenanceGateGenuinelyNewerTargetStillUpdatesCorruptState(t *testing.T) {
	// Corruption must not pin an OLD engine: a genuinely newer target
	// still updates, even when the state had to be recovered from the
	// manifest.
	const latest = "b11223"

	h := newTwoBootServer(t, t.TempDir(), "fake-model.gguf")
	commitEngineTransaction(t, h.cfg, latest)

	if err := os.WriteFile(h.cfg.StatePath(), []byte(`{invalid`), 0o644); err != nil {
		t.Fatalf("corrupt state file: %v", err)
	}

	var targeted atomic.Bool

	events := newOrderedEvents()
	events = h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b12000", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			targeted.Store(true)

			if tag != "b12000" {
				t.Errorf("transaction targeted %q, want b12000", tag)
			}

			commitEngineTransaction(t, h.cfg, tag)

			return "engine updated to llama.cpp " + tag, nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm after the newer-target transaction")

	if !targeted.Load() {
		t.Fatal("a genuinely newer release must be downloaded/installed even from a corrupted state")
	}
}
