package api

// v1.7.4 (P0 #3): the startup maintenance gate must decide from the
// EFFECTIVE installed engine tag. The production incident: a state file
// without a recorded tag + a committed engine-install.json manifest beside
// the binary — the gate used to fall back to the bundled default tag and
// re-downloaded the engine on EVERY boot, although the manifest described
// exactly the package it was about to re-download.
//
// The test reuses the deterministic harness of maintenance_test.go
// (channel-synchronized, no sleeps, faked transaction).

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// TestMaintenanceGateUsesManifestTagWhenStateHasNone drives the gate with
// a tag-less installed.json and a committed manifest carrying b11223: the
// release probe answers b11223 and the gate must finish READY_FOR_PREWARM
// ("engine is current") WITHOUT ever attempting the update transaction.
func TestMaintenanceGateUsesManifestTagWhenStateHasNone(t *testing.T) {
	h := newMaintenanceTestServer(t, "fake-model.gguf", true)

	// The committed manifest beside the (hypothetical) managed engine.
	binDir := updater.EngineBinDir(h.cfg)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	manifest := `{"tag":"b11223","sha256":"deadbeef","size":1024,"source":"test"}`

	if err := os.WriteFile(filepath.Join(binDir, "engine-install.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// A tag-less state file, exactly what a detection rewrite used to
	// produce (and what boot 2 of the incident read).
	tagless := `{"appVersion":"1.7.3","components":{"llamaServer":{"status":"installed","meta":{"path":"` +
		filepath.Join(binDir, "llama-server") + `"}}}}`

	if err := os.WriteFile(h.cfg.StatePath(), []byte(tagless), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	events := newOrderedEvents()

	attempted := false

	events = h.wire(
		events,
		func(ctx context.Context) (string, error) { return "b11223", nil },
		func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
			attempted = true // must NEVER run — b11223 is already installed

			events.record("update-attempted")

			return "engine updated to llama.cpp " + tag, nil
		},
		nil,
		nil,
	)

	if err := h.srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	events.waitUntil(t, events.has("prewarm"), "prewarm after a no-op maintenance decision")

	st := h.srv.gateValue().Snapshot()

	if st.Phase != PhaseReadyForPrewarm {
		t.Fatalf("phase = %s, want READY_FOR_PREWARM (the manifest tag b11223 IS the latest release)", st.Phase)
	}

	if st.Reason != "current" {
		t.Fatalf("reason = %q, want \"current\" — the gate re-decided an update was required (detail: %s)", st.Reason, st.Detail)
	}

	if st.Updated || st.TargetTag != "" {
		t.Fatalf("no update must have run: updated=%v targetTag=%q", st.Updated, st.TargetTag)
	}

	if attempted {
		t.Fatal("the update transaction was attempted although the effective tag equals the latest release")
	}
}
