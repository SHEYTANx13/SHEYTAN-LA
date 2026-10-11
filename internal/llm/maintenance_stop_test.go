// Maintenance-stop classification test (v1.6.0, spec §5 test 7).
//
// The startup maintenance gate performs its engine update through the
// ENGINE-OWNED transaction (LlamaServer.UpdateEngineNow). While that
// transaction stops the running engine to swap the files, the stop is
// DELIBERATE — the exit watcher must classify it as such: never as a
// crash, never counted as an unexpected auto-restart.
package llm

import (
	"context"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

func TestMaintenanceStopIsNotClassifiedAsCrash(t *testing.T) {
	// A healthy managed engine, running for the whole test.
	cfg, _ := fakeManagedEngineConfig(t, "")

	goodArchive := buildFakeEngineArchive(t, t.TempDir())

	if _, err := updater.InstallStagedFromArchive(cfg, "b10642", goodArchive); err != nil {
		t.Fatalf("seed good engine: %v", err)
	}

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The auto-restart budget of the healthy episode starts at zero.
	restartsBefore := srv.Restarts()

	// The maintenance transaction: deterministic staged archive (never
	// the network), engine running → the transaction STOPs it.
	srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

	if _, err := srv.UpdateEngineNow(context.Background(), "b14000", nil); err != nil {
		t.Fatalf("maintenance transaction failed: %v", err)
	}

	// The transaction restarted the engine itself (verified commit) —
	// the engine must be serving again.
	if !srv.IsRunning() {
		t.Fatalf("engine must serve after the maintenance transaction, state=%s detail=%s",
			srv.State(), srv.Detail())
	}

	// THE assertion (spec §5 test 7): the deliberate maintenance stop
	// never consumed the crash/auto-restart budget. A crash
	// classification would have incremented the episode's counter
	// before the transaction's own restart.
	restartsAfter := srv.Restarts()

	if restartsAfter != restartsBefore {
		t.Fatalf("deliberate maintenance stop classified as a crash: restarts %d → %d",
			restartsBefore, restartsAfter)
	}

	// And the transaction committed the new tag.
	if got := updater.InstalledEngineTag(cfg); got != "b14000" {
		t.Fatalf("installed tag = %q, want b14000", got)
	}
}
