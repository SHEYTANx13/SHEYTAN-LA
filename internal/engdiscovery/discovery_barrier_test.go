package engdiscovery

// v1.8.7 — deterministic retention barrier regression suite (the v1.8.6
// Windows CI failure, Actions run 37273354268).
//
// The v1.8.6 frontier ordered QUEUED directories correctly but sealed
// the scan on first ARRIVAL of the MaxCandidates-th candidate, so with
// multiple workers the discovery order was race-dependent: an
// already-running deeper worker could report before a shallower worker
// and seal the scan. These tests pin the repaired contract:
//
//      all eligible work at a higher-priority discovery level outranks
//      deeper work, regardless of which worker finishes first.
//
// Concretely: parallel scan + same dataset + repeated executions →
// same winning candidate, for any worker count and any interleaving.
// No sleeps, no timing assumptions, no statistical margins — the
// barrier makes retention a pure function of the scanned dataset.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// runBarrierScan executes one FullScan against the barrier fixture
// dataset with the given worker count.
func runBarrierScan(t *testing.T, cfg *config.Config, workers int, maxCandidates int) []Candidate {
	t.Helper()

	return FullScan(context.Background(), cfg, discoveryEngineName(), ScanOptions{
		Workers:       workers,
		Timeout:       30 * time.Second,
		MaxDepth:      6,
		MaxCandidates: maxCandidates,
	})
}

// TestFullScanDeterministicWinnerAcrossWorkerCounts is the core proof
// of the v1.8.7 repair. The dataset is the failing CI dataset — a
// lexically-early DEEP candidate and a lexically-late SHALLOW candidate
// in the same root — executed repeatedly across the whole worker-count
// spectrum. Every run must elect the same winner (the shallow
// candidate); the pre-v1.8.7 code lost this whenever a deeper worker
// happened to emit first (loaded runners, slow FS).
func TestFullScanDeterministicWinnerAcrossWorkerCounts(t *testing.T) {
	cfg := fixtureConfig(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this runner")
	}

	root := filepath.Join(home, "sheytan-barrier-fixture")
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	deepDir := filepath.Join(root, "aaa-early", "deep")
	deep := stageTestBinary(t, deepDir, discoveryEngineName())

	shallowDir := filepath.Join(root, "zzz-late-engine")
	shallow := stageTestBinary(t, shallowDir, discoveryEngineName())

	_ = deep

	want := func(cands []Candidate) {
		t.Helper()

		if len(cands) != 1 {
			t.Fatalf("MaxCandidates=1 must yield exactly one candidate, got %d (%+v)", len(cands), cands)
		}

		if filepath.Clean(cands[0].Path) != filepath.Clean(shallow) {
			t.Fatalf("shallow candidate must deterministically win: got %s, want %s (deep decoy at %s)",
				cands[0].Path, shallow, deep)
		}
	}

	for _, workers := range []int{1, 2, 4, 8, 16} {
		for run := 1; run <= 3; run++ {
			cands := runBarrierScan(t, cfg, workers, 1)
			want(cands)
		}
	}
}

// TestFullScanSameDepthCandidatesAreLexicallyDeterministic: candidates
// at the SAME priority level are retained in lexical path order — with
// MaxCandidates=1 the lexically-earlier same-level candidate wins on
// every run, and with MaxCandidates=2 both are retained in sorted
// order. Completion order among same-level workers must not matter.
func TestFullScanSameDepthCandidatesAreLexicallyDeterministic(t *testing.T) {
	cfg := fixtureConfig(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this runner")
	}

	root := filepath.Join(home, "sheytan-samedepth-fixture")
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	late := stageTestBinary(t, filepath.Join(root, "b-late-engine"), discoveryEngineName())
	early := stageTestBinary(t, filepath.Join(root, "a-early-engine"), discoveryEngineName())

	// Same-depth sibling decoys widen the level so workers genuinely
	// interleave inside it.
	for i := 0; i < 4; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("decoy-%d", i), "child"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for run := 1; run <= 5; run++ {
		cands := runBarrierScan(t, cfg, 8, 1)

		if len(cands) != 1 {
			t.Fatalf("run %d: MaxCandidates=1 must yield exactly one candidate, got %d", run, len(cands))
		}

		if filepath.Clean(cands[0].Path) != filepath.Clean(early) {
			t.Fatalf("run %d: lexically-earlier same-depth candidate must win deterministically: got %s, want %s",
				run, cands[0].Path, early)
		}
	}

	// With the cap above the level's candidate count, both are
	// retained in deterministic (level, path) order.
	cands := runBarrierScan(t, cfg, 8, 2)
	if len(cands) != 2 {
		t.Fatalf("MaxCandidates=2 must retain both same-depth candidates, got %d (%+v)", len(cands), cands)
	}

	if filepath.Clean(cands[0].Path) != filepath.Clean(early) ||
		filepath.Clean(cands[1].Path) != filepath.Clean(late) {
		t.Fatalf("retention must be lexically sorted within a level: got [%s, %s], want [%s, %s]",
			cands[0].Path, cands[1].Path, early, late)
	}
}

// TestFullScanMultipleRootsHighestPriorityRootWins: candidates seeded
// from two distinct roots — the user home tree (class 0) and a
// cache-replayed external location (class 1) — must elect the
// higher-priority root's candidate deterministically, and both roots
// must be scannable when the cap allows. The class-1 seed exercises
// the cache-parent seeding path of FullScan.
func TestFullScanMultipleRootsHighestPriorityRootWins(t *testing.T) {
	cfg := fixtureConfig(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this runner")
	}

	homeDir := filepath.Join(home, "sheytan-multiroot-home")
	homeCand := stageTestBinary(t, homeDir, discoveryEngineName())
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })

	// A second root OUTSIDE the fixture config's data dir, replayed via
	// the discovery cache (seeded at class 1 — the per-user application
	// root class).
	externalDir := filepath.Join(t.TempDir(), "sheytan-multiroot-external")
	externalCand := stageTestBinary(t, externalDir, discoveryEngineName())

	if err := SaveCache(cfg, []Candidate{{
		Path: externalCand, Size: 1, ModTime: 1, Tier: 2, Source: "cache-seed",
	}}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	for run := 1; run <= 3; run++ {
		cands := runBarrierScan(t, cfg, 4, 1)

		if len(cands) != 1 {
			t.Fatalf("run %d: MaxCandidates=1 must yield exactly one candidate, got %d", run, len(cands))
		}

		if filepath.Clean(cands[0].Path) != filepath.Clean(homeCand) {
			t.Fatalf("run %d: the higher-priority root (home, class 0) must win over the cache-seeded root (class 1): got %s, want %s",
				run, cands[0].Path, homeCand)
		}
	}

	// With headroom, both roots' candidates are retained and the
	// higher-priority root comes first (retention is level-ordered).
	cands := runBarrierScan(t, cfg, 4, 8)

	if len(cands) < 2 {
		t.Fatalf("both roots must be scannable with headroom, got %d (%+v)", len(cands), cands)
	}

	if filepath.Clean(cands[0].Path) != filepath.Clean(homeCand) {
		t.Fatalf("retention must be level-ordered: got %s first, want %s (home)", cands[0].Path, homeCand)
	}

	second := filepath.Clean(cands[1].Path)
	if second != filepath.Clean(externalCand) && second != filepath.Clean(homeCand) {
		t.Fatalf("unexpected second candidate: %s", cands[1].Path)
	}
}

// TestFullScanSealedScanDoesNotLeakWorkers: the MaxCandidates seal path
// (the one the v1.8.6 race mis-fired on) must return every worker and
// the cancellation watcher — repeated sealed scans must not grow the
// goroutine count and must never deadlock.
func TestFullScanSealedScanDoesNotLeakWorkers(t *testing.T) {
	cfg := fixtureConfig(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this runner")
	}

	root := filepath.Join(home, "sheytan-seal-fixture")
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	deepDir := filepath.Join(root, "aaa-early", "deep")
	deep := stageTestBinary(t, deepDir, discoveryEngineName())
	shallowDir := filepath.Join(root, "zzz-late-engine")
	shallow := stageTestBinary(t, shallowDir, discoveryEngineName())

	_ = deep

	before := runtime.NumGoroutine()

	for run := 1; run <= 3; run++ {
		done := make(chan []Candidate, 1)

		go func() {
			done <- runBarrierScan(t, cfg, 8, 1)
		}()

		select {
		case cands := <-done:
			if len(cands) != 1 || filepath.Clean(cands[0].Path) != filepath.Clean(shallow) {
				t.Fatalf("run %d: sealed scan must deterministically elect the shallow candidate, got %+v", run, cands)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("run %d: sealed scan deadlocked", run)
		}
	}

	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("goroutine leak across sealed scans: before=%d after=%d", before, after)
	}
}
