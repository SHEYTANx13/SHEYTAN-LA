package llm

// admission_boundary_v196_test.go — v1.9.6 (P0): the admission claim lives
// AT THE DESTRUCTIVE BOUNDARY of the variant transaction, pinned
// deterministically through the archive-seam runtime harness (no network):
//
//   1. the claim is HELD inside the destructive window: the verification
//      extension (which runs between the swap and the commit, under the
//      claim) observes EngineMutationActive() == true — the exact signal
//      the API run-admission pre-checks read;
//   2. the claim is released exactly once on the success path (begin/end
//      balance, mirror false after);
//   3. a run registered during the claim-free download phase (the
//      v1.9.5-on-Windows regression mechanism: the background AUTO Vulkan
//      probe downloaded while the user typed) is seen by the boundary's
//      live verdict — the transaction refuses with ErrEngineBusyWithRun,
//      never claims, and never touches the engine or the manifest;
//   4. with the seam nil, the destructive-boundary gate re-check refuses a
//      run that registered after the early check (the same race, historical
//      seam): exactly one early consult + one boundary consult.
//
// These pins close the v1.9.6 Windows admission regression class: the
// claim can no longer span the non-destructive network phases, and the
// destructive window keeps both races closed.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// driveBoundaryTransaction wires the deterministic windows/amd64 variant
// harness (the same seams driveVariantTransaction uses) and hands the
// caller the server so it can install admission/run-gate doubles and drive
// updateEngineVariantTx directly (the extraVerify extension is only
// reachable at that depth).
func driveBoundaryTransaction(t *testing.T) *LlamaServer {
	t.Helper()

	restorePlatform := updater.SetPlatformForTest("windows", "amd64")
	t.Cleanup(restorePlatform)

	updater.SetVariantExistsForTest(func(context.Context, string, updater.AssetVariant) bool {
		return false // the pinned/current tag never serves it — force the list scan
	})
	t.Cleanup(func() { updater.SetVariantExistsForTest(nil) })

	updater.SetReleaseListForTest(func(context.Context) ([]updater.ReleaseInfo, error) {
		return []updater.ReleaseInfo{{
			TagName: "b11191",
			Assets: []updater.AssetInfo{
				{Name: "llama-b11191-bin-win-cpu-x64.zip"},
				{Name: "llama-b11191-bin-win-vulkan-x64.zip"},
			},
		}}, nil
	})
	t.Cleanup(func() { updater.SetReleaseListForTest(nil) })

	cfg, _ := fakeManagedEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))
	t.Cleanup(func() { _ = srv.Stop() })

	srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

	return srv
}

// The claim is HELD inside the destructive window (observed from the
// verification hook — the deepest assertion point under the claim) and
// released exactly once on the success path.
func TestAdmissionClaimHeldInsideDestructiveBoundary(t *testing.T) {
	s := driveBoundaryTransaction(t)

	fa := &fakeAdmission{}
	s.SetAdmissionControl(fa)

	claimSeenAtVerify := false

	verify := func(cfg *config.Config, variant updater.AssetVariant, note string) (string, error) {
		claimSeenAtVerify = s.EngineMutationActive()
		return "boundary-hook", nil
	}

	outcome, err := s.updateEngineVariantTx(context.Background(), updater.VariantVulkan, nil, verify)
	if err != nil {
		t.Fatalf("boundary transaction: %v", err)
	}

	if !strings.Contains(outcome, "boundary-hook") {
		t.Fatalf("the verification note must reach the committed outcome: %q", outcome)
	}

	if !claimSeenAtVerify {
		t.Fatal("EngineMutationActive was false inside the destructive window — the claim must be held from the boundary through the verification")
	}

	if fa.begins.Load() != 1 {
		t.Fatalf("BeginEngineMutation called %d times, want 1 (once, at the boundary)", fa.begins.Load())
	}
	if fa.ends.Load() != 1 {
		t.Fatalf("EndEngineMutation called %d times, want exactly 1 (released on the success path)", fa.ends.Load())
	}
	if fa.claimed.Load() {
		t.Fatal("claim still held after the transaction returned")
	}
	if s.EngineMutationActive() {
		t.Fatal("EngineMutationActive must be false after the transaction released the claim")
	}
}

// A run registered during the claim-free download phase (between the early
// check and the destructive boundary) is refused BY THE BOUNDARY: the live
// verdict sees it, the transaction refuses, the claim is never taken, and
// the engine/manifest are untouched.
func TestAdmissionRunRegisteredDuringDownloadRefusesAtBoundary(t *testing.T) {
	s := driveBoundaryTransaction(t)

	// The early advisory check passes (no run yet); the run "registers
	// during the download" — modeled by the boundary verdict going live.
	s.SetRunGate(func() bool { return false })

	fa := &fakeAdmission{}
	fa.liveAtBegin.Store(true)
	s.SetAdmissionControl(fa)

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantVulkan, nil, nil)
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
	if s.IsRunning() {
		t.Fatal("no engine may have been started by a refused transaction")
	}
}

// Nil seam: the destructive-boundary gate re-check still closes the
// mid-download registration race — one early consult (false: nothing live
// yet) plus one boundary consult (true: the run registered during the
// download) refuses the transaction before any stop.
func TestNilGateBoundaryRecheckClosesMidDownloadRegistration(t *testing.T) {
	s := driveBoundaryTransaction(t)

	consults := 0
	s.SetRunGate(func() bool {
		consults++
		return consults >= 2 // early: clear; boundary: a run registered meanwhile
	})

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantVulkan, nil, nil)
	if !errors.Is(err, ErrEngineBusyWithRun) {
		t.Fatalf("err = %v, want ErrEngineBusyWithRun (boundary re-check)", err)
	}

	if consults != 2 {
		t.Fatalf("gate consulted %d times, want exactly 2 (early + boundary)", consults)
	}

	if s.IsRunning() {
		t.Fatal("no engine may have been started by a refused transaction")
	}
}
