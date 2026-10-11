// calibration_test.go — the CONTROL-CONTRACT tests for the bounded
// automatic performance check (v1.5.1 transactional pass).
//
// The scripted runner exists to prove retain/rollback/budget/dedup
// behavior — the product NEVER fabricates performance numbers, and these
// tests assert exactly that: an unmeasured candidate can never win, and
// "not benchmarked" is the honest verdict when nothing measured.
//
// v1.5.1: the budget-expiry contract is proven DETERMINISTICALLY through
// an injectable clock — never by racing the wall clock (the Windows
// scheduler's coarse monotonic granularity made a nanosecond budget
// flaky). Every restore path (boot failure, generation failure,
// instability, cancellation, budget exhaustion, winner re-apply failure,
// rollback failure) is exercised against a scripted engine.
package calibration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// testModelPath is the model every Calibrate call in this suite drives.
const testModelPath = "/models/m.gguf"

// realisticRunner is a full fake with per-candidate outcomes, keyed by
// the candidate label the Calibrate loop passes via ApplyProfile.
//
// It models the PRODUCTION runner's transaction shape:
//   - every ApplyProfile persists the candidate profile FIRST (the
//     config write), then (re)starts the engine;
//   - a boot failure leaves the persisted config on the candidate while
//     the engine keeps serving the previous profile — the untrusted
//     state the controller must restore from.
type realisticRunner struct {
	// initial is the label of the profile ALREADY serving when
	// Calibrate starts (the safe baseline is measured in place — the
	// runner never applies it).
	initial string
	bootErr map[string]error
	// measure is the FIXED sample per label (repeated samples repeat it).
	measure map[string]llm.PerfStats
	// measureSeq is a sequential sample list per label (variance tests);
	// an exhausted sequence fails the generation.
	measureSeq map[string][]llm.PerfStats
	measureErr map[string]error
	stable     map[string]bool
	// divertModel simulates the engine serving a DIFFERENT model than
	// the expected one (model-serving verification, spec §2.13).
	divertModel bool

	// applied is the successful ApplyProfile history (what the engine is
	// serving, last entry wins; initial before the first entry).
	applied []string
	// persisted is EVERY config write (including boots that then failed)
	// — the production runner persists before the restart.
	persisted []string
	measured  []string
}

// currentLabel is what the fake engine is serving right now.
func (r *realisticRunner) currentLabel() string {
	if len(r.applied) > 0 {
		return r.applied[len(r.applied)-1]
	}
	return r.initial
}

func (r *realisticRunner) ApplyProfile(_ context.Context, _ string, rec recommendation.Recommendation) error {
	// Identify the candidate by its distinctive field values.
	label := candidateLabel(rec)
	if err, ok := r.bootErr[label]; ok {
		if errors.Is(err, ErrCalibrationAborted) {
			// The production runner checks for active runs BEFORE the
			// config write: the clean abort leaves no trace at all.
			return err
		}
		// The config now carries the candidate; the engine never
		// switched. This is exactly the untrusted state production can
		// reach when a restart fails after the config write.
		r.persisted = append(r.persisted, label)
		return err
	}
	r.persisted = append(r.persisted, label)
	r.applied = append(r.applied, label)
	return nil
}

func (r *realisticRunner) MeasureGeneration(_ context.Context, _ string, _ int) (llm.PerfStats, error) {
	label := r.currentLabel()
	r.measured = append(r.measured, label)

	if err, ok := r.measureErr[label]; ok {
		return llm.PerfStats{}, err
	}
	if seq, ok := r.measureSeq[label]; ok {
		if len(seq) == 0 {
			return llm.PerfStats{}, errors.New("sample sequence exhausted")
		}
		p := seq[0]
		r.measureSeq[label] = seq[1:]
		return p, nil
	}
	if p, ok := r.measure[label]; ok {
		return p, nil
	}
	return llm.PerfStats{}, nil
}

// EngineStable verifies stability AND that the expected model is still
// the one being served (spec §2.13).
func (r *realisticRunner) EngineStable(expectedModel string) bool {
	if expectedModel != testModelPath || r.divertModel {
		// The engine is not serving the expected model.
		return false
	}
	if v, ok := r.stable[r.currentLabel()]; ok {
		return v
	}
	return true
}

// candidateLabel derives a stable identity from the fields the candidate
// variants actually change (GPU posture + context).
func candidateLabel(rec recommendation.Recommendation) string {
	gpu := "cpu"
	if rec.GPUAutoOffload || rec.GPULayers > 0 {
		gpu = "gpu"
	}
	return gpu + "/" + itoa(rec.Context)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func baseRec() recommendation.Recommendation {
	return recommendation.Recommendation{
		Task:           recommendation.TaskChat,
		Context:        8192,
		Threads:        8,
		GPULayers:      0,
		UBatchSize:     512,
		FlashAttention: true,
		KVCacheQuant:   "f16",
	}
}

func baseWithGPU() recommendation.Recommendation {
	r := baseRec()
	r.GPUAutoOffload = true
	r.GPULayers = 99
	return r
}

func baseWithFastCtx() recommendation.Recommendation {
	r := baseRec()
	r.Context = 4096
	return r
}

// fakeClock is the deterministic clock seam: the FIRST reading is the
// pass start; every later reading is far past any realistic deadline.
type fakeClock struct {
	calls int
	base  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.calls++
	if c.calls == 1 {
		return c.base
	}
	return c.base.Add(time.Hour)
}

// TestCandidatesBoundedAndEvidenceDerived proves the candidate set is
// small, deduplicated, and only derived from measured evidence.
func TestCandidatesBoundedAndEvidenceDerived(t *testing.T) {
	// CPU-only hardware: a CPU-baseline recommendation yields just the
	// context variant (context 8192 is NOT > 8192 → none) → 1 candidate.
	hwCPU := hardware.Profile{}
	cands := Candidates(baseRec(), hwCPU, nil)
	if len(cands) != 1 || cands[0].Label != BaselineLabel {
		t.Fatalf("CPU-only baseline should yield exactly the baseline, got %+v", cands)
	}

	// GPU measured + CPU baseline → GPU offload variant joins.
	hwGPU := hardware.Profile{}
	hwGPU.GPUs = []hardware.GPU{{Vendor: "Intel", Name: "Arc A770", VRAMBytes: 16 << 30}}
	cands = Candidates(baseRec(), hwGPU, nil)
	if len(cands) != 2 {
		t.Fatalf("GPU evidence should add the GPU-offload candidate, got %+v", cands)
	}
	if cands[1].Label != "GPU offload" || !cands[1].Profile.GPUAutoOffload {
		t.Fatalf("second candidate must be the GPU-offload variant, got %+v", cands[1])
	}

	// GPU baseline → CPU-safe variant joins (no GPU evidence needed —
	// the baseline itself offloads, bounding the risk is the reason).
	cands = Candidates(baseWithGPU(), hardware.Profile{}, nil)
	if len(cands) != 2 || cands[1].Label != "CPU safe" {
		t.Fatalf("GPU baseline should add the CPU-safe candidate, got %+v", cands)
	}

	// Big context → the fast-context variant; total capped at 3.
	big := baseWithGPU()
	big.Context = 32768
	cands = Candidates(big, hwGPU, nil)
	if len(cands) > MaxCandidates {
		t.Fatalf("candidate set must stay bounded at %d, got %d", MaxCandidates, len(cands))
	}
	labels := map[string]bool{}
	for _, c := range cands {
		labels[c.Label] = true
	}
	if len(labels) != len(cands) {
		t.Fatalf("duplicate candidates: %+v", cands)
	}
}

// TestCalibrateExpiredBudgetSkipsAllDeterministically replaces the old
// wall-clock budget test: an already-expired budget must skip EVERY
// candidate (including the in-place baseline) — deterministically, with
// no dependence on the platform's clock granularity. The honest verdict
// is "not benchmarked", and the already-applied baseline stays.
func TestCalibrateExpiredBudgetSkipsAllDeterministically(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 100, Tokens: 24, TokensPerSec: 12, WallMs: 300},
			"gpu/8192": {TTFTMs: 100, Tokens: 24, TokensPerSec: 40, WallMs: 300},
		},
	}

	// The clock's FIRST reading sets the pass start; every later reading
	// is an hour later — far past the nanosecond budget, on every
	// platform, forever. This is the deterministic seam the flaky
	// wall-clock version could never provide.
	clock := &fakeClock{base: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	sum := calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Nanosecond, clock.Now)

	skipped := 0
	for _, m := range sum.Measurements {
		if m.Note == "skipped: calibration budget exhausted" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Fatalf("the exhausted budget must skip every candidate, measurements: %+v", sum.Measurements)
	}
	if sum.Ran || sum.Winner != "" || sum.WinnerProfile != nil || sum.Failed {
		t.Fatalf("an exhausted budget must never claim a measured winner, got %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("the honest not-benchmarked reason must be present")
	}
	// Nothing was applied, nothing was measured, nothing was persisted.
	if len(runner.applied) != 0 || len(runner.measured) != 0 || len(runner.persisted) != 0 {
		t.Fatalf("a skipped pass must not touch the engine or config: applied=%v persisted=%v measured=%v",
			runner.applied, runner.persisted, runner.measured)
	}
}

// TestCalibrateBaselineOnlyRun proves a baseline-only pass measures in
// place, never restarts the engine, and records the baseline as the
// winner with ITS OWN measurement.
func TestCalibrateBaselineOnlyRun(t *testing.T) {
	base := baseRec()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 2000},
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel {
		t.Fatalf("baseline-only run must win in place, got %+v", sum)
	}
	if sum.Changed {
		t.Fatal("the baseline winning in place must not mark Changed")
	}
	if sum.WinnerMeasurement == nil || sum.WinnerMeasurement.Candidate != BaselineLabel || sum.WinnerMeasurement.GenTokensPerSec != 12 {
		t.Fatalf("winner measurement must be the baseline's own, got %+v", sum.WinnerMeasurement)
	}
	// Measured in place: repeated samples, zero ApplyProfile calls.
	if len(runner.applied) != 0 || len(runner.persisted) != 0 {
		t.Fatalf("a baseline-only pass must not restart the engine: applied=%v persisted=%v", runner.applied, runner.persisted)
	}
	if len(runner.measured) != MeasureSamples {
		t.Fatalf("repeated short samples expected (%d), got %d", MeasureSamples, len(runner.measured))
	}
}

// TestCalibrateWinnerRetainedFromMeasuredRuns proves: only measured
// stable candidates can win, the winner's real numbers are carried
// verbatim, and NO redundant re-apply happens when the winning candidate
// is already the one serving (no wasted stop/start cycles).
func TestCalibrateWinnerRetainedFromMeasuredRuns(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, PromptTokens: 9, WallMs: 900},
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran {
		t.Fatalf("calibration must report ran with two stable measured candidates: %+v", sum)
	}
	if sum.Winner != "GPU offload" {
		t.Fatalf("winner = %q, want GPU offload (40 tok/s beats 12)", sum.Winner)
	}
	if !sum.Changed {
		t.Fatal("a non-baseline final profile must mark Changed")
	}
	if sum.Failed {
		t.Fatalf("a successful pass must not be Failed: %+v", sum)
	}

	// The winner's real numbers are carried verbatim — and they are the
	// WINNER's numbers, never another candidate's.
	if sum.WinnerMeasurement == nil || sum.WinnerMeasurement.Candidate != "GPU offload" {
		t.Fatalf("winner measurement must belong to the winner, got %+v", sum.WinnerMeasurement)
	}
	if m := sum.WinnerMeasurement; m.GenTokensPerSec != 40 || m.TTFTSeconds != 0.3 {
		t.Fatalf("winner measurement must carry the measured values, got %+v", m)
	}
	if m := sum.WinnerMeasurement; m.PromptTokensPerSec != 30 { // 9 prompt tokens / 0.3s
		t.Fatalf("prompt throughput must be derived from measured values, got %+v", m)
	}

	// Final state consistency: the winner is the last (only) apply — the
	// engine, the config and the recorded winner all agree.
	if got := runner.applied[len(runner.applied)-1]; got != "gpu/8192" {
		t.Fatalf("final applied profile = %q, want the GPU winner", got)
	}
	if len(runner.applied) != 1 {
		t.Fatalf("the winning candidate was measured last — no re-apply cycle may run, got %v", runner.applied)
	}
	if got := runner.persisted[len(runner.persisted)-1]; got != "gpu/8192" {
		t.Fatalf("final persisted profile = %q, want the GPU winner", got)
	}
}

// TestCalibrateUnmeasuredCandidateNeverWins proves the honesty rule: a
// candidate whose generation fails can NEVER be the winner — and a
// failed non-baseline candidate never leaves its configuration active:
// the last verified profile (the baseline) is restored.
func TestCalibrateUnmeasuredCandidateNeverWins(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 2000},
		},
		measureErr: map[string]error{
			"gpu/8192": errors.New("engine died mid-generation"),
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran {
		t.Fatalf("the stable measured baseline still counts as a run: %+v", sum)
	}
	if sum.Winner != BaselineLabel {
		t.Fatalf("an unmeasured candidate must never win; winner = %q", sum.Winner)
	}
	if sum.Changed {
		t.Fatal("baseline winner must not mark Changed (final profile IS the baseline)")
	}
	if sum.Failed {
		t.Fatalf("a clean restore is not a failure: %+v", sum)
	}
	// The GPU candidate booted (applied) and then failed to generate —
	// the baseline must be RESTORED (spec §2.6/§2.7).
	if len(runner.applied) != 2 || runner.applied[len(runner.applied)-1] != "cpu/8192" {
		t.Fatalf("failed candidate must be rolled back to the baseline, applied=%v", runner.applied)
	}
	if got := runner.persisted[len(runner.persisted)-1]; got != "cpu/8192" {
		t.Fatalf("persisted config must end on the restored baseline, got %q", got)
	}
}

// TestCalibrateBaselineWinsAfterNonBaselineMeasured proves spec §2.8:
// when the baseline wins after another candidate was measured, the
// baseline is EXPLICITLY restored — the slower candidate may not stay
// active just because it was measured last.
func TestCalibrateBaselineWinsAfterNonBaselineMeasured(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 8, WallMs: 900}, // slower!
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel {
		t.Fatalf("the faster measured baseline must win, got %+v", sum)
	}
	if sum.Changed {
		t.Fatal("the final profile IS the baseline — Changed must be false")
	}
	if len(runner.applied) != 2 || runner.applied[len(runner.applied)-1] != "cpu/8192" {
		t.Fatalf("baseline must be explicitly restored after the GPU candidate, applied=%v", runner.applied)
	}
	if sum.Reason == "" {
		t.Fatal("the restore should be documented in the summary reason")
	}
}

// TestCalibrateBootFailureRestoresBaseline proves a candidate that never
// boots is recorded honestly and the verified baseline is restored
// before the pass continues (spec §2.6/§2.7).
func TestCalibrateBootFailureRestoresBaseline(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
		},
		bootErr: map[string]error{
			"gpu/8192": errors.New("vulkan device creation failed"),
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel || sum.Changed {
		t.Fatalf("a boot-failed candidate must never win, got %+v", sum)
	}
	for _, m := range sum.Measurements {
		if m.Candidate == "GPU offload" && m.Booted {
			t.Fatalf("boot failure must be recorded as booted=false: %+v", m)
		}
	}
	// The failed boot persists the candidate (production shape) — the
	// controller must restore the verified baseline config.
	if len(runner.persisted) != 2 || runner.persisted[len(runner.persisted)-1] != "cpu/8192" {
		t.Fatalf("persisted config must be restored to the baseline, persisted=%v", runner.persisted)
	}
	if len(runner.applied) != 1 || runner.applied[0] != "cpu/8192" {
		t.Fatalf("the restore re-applies the verified baseline, applied=%v", runner.applied)
	}
}

// TestCalibrateGenerationFailureRestoresBaseline covers the restore
// after a failed generation on a candidate that DID boot.
func TestCalibrateGenerationFailureRestoresBaseline(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
		},
		measureErr: map[string]error{
			"gpu/8192": errors.New("generation timed out"),
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel || sum.Failed {
		t.Fatalf("honest baseline win after a generation failure, got %+v", sum)
	}
	if len(runner.applied) != 2 || runner.applied[len(runner.applied)-1] != "cpu/8192" {
		t.Fatalf("baseline must be restored after the failed generation, applied=%v", runner.applied)
	}
}

// TestCalibrateUnstableCandidateRestoresBaseline covers the instability
// path: a candidate that measures but reports the engine as unstable is
// never a winner and is rolled back.
func TestCalibrateUnstableCandidateRestoresBaseline(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
		},
		stable: map[string]bool{
			"cpu/8192": true,
			"gpu/8192": false, // the watchdog restarted the engine mid-run
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel || sum.Changed {
		t.Fatalf("an unstable candidate must never win, got %+v", sum)
	}
	for _, m := range sum.Measurements {
		if m.Candidate == "GPU offload" && m.Stable {
			t.Fatalf("instability must be recorded honestly: %+v", m)
		}
	}
	if len(runner.applied) != 2 || runner.applied[len(runner.applied)-1] != "cpu/8192" {
		t.Fatalf("unstable candidate must be rolled back, applied=%v", runner.applied)
	}
}

// TestCalibrateNoStableCandidateRestoresBaseline proves the honest
// "not benchmarked" verdict when no candidate produces a measured run.
func TestCalibrateNoStableCandidateRestoresBaseline(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measureErr: map[string]error{
			"cpu/8192": errors.New("offline"),
			"gpu/8192": errors.New("offline"),
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if sum.Ran {
		t.Fatalf("nothing measured must never report Ran, got %+v", sum)
	}
	if sum.Winner != "" || sum.WinnerProfile != nil || sum.WinnerMeasurement != nil {
		t.Fatalf("no winner without measurement, got %+v", sum)
	}
	if sum.Failed {
		t.Fatalf("a clean pass with no stable candidate is not a failure: %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("the not-benchmarked reason must be present")
	}
	// The GPU candidate booted and failed — the baseline stays.
	if got := runner.applied[len(runner.applied)-1]; got != "cpu/8192" {
		t.Fatalf("final applied = %q, want the restored baseline", got)
	}
}

// TestCalibrateVarianceRejected proves the repeated-sampling contract:
// a candidate whose samples diverge beyond VarianceRejectRatio is
// rejected as unstable — a single lucky sample cannot win.
func TestCalibrateVarianceRejected(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
		},
		measureSeq: map[string][]llm.PerfStats{
			"gpu/8192": {
				{TTFTMs: 300, Tokens: 24, TokensPerSec: 60, WallMs: 600}, // warmup (discarded)
				{TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 600}, // lucky burst
				{TTFTMs: 300, Tokens: 24, TokensPerSec: 15, WallMs: 1600},
			},
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel {
		t.Fatalf("a high-variance candidate must never win, got %+v", sum)
	}
	for _, m := range sum.Measurements {
		if m.Candidate == "GPU offload" {
			if !m.HighVariance {
				t.Fatalf("variance rejection must be recorded: %+v", m)
			}
			if m.Stable {
				t.Fatalf("a variance-rejected candidate is not stable: %+v", m)
			}
		}
	}
	if len(runner.applied) != 2 || runner.applied[len(runner.applied)-1] != "cpu/8192" {
		t.Fatalf("variance-rejected candidate must be rolled back, applied=%v", runner.applied)
	}
}

// TestCalibrateMedianAggregation proves repeated short measurements
// aggregate deterministically (medians) instead of trusting one sample.
func TestCalibrateMedianAggregation(t *testing.T) {
	base := baseRec()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measureSeq: map[string][]llm.PerfStats{
			"cpu/8192": {
				{TTFTMs: 900, Tokens: 24, TokensPerSec: 6, PromptTokens: 9, WallMs: 5000}, // warmup (discarded)
				{TTFTMs: 300, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 1000},
				{TTFTMs: 500, Tokens: 24, TokensPerSec: 14, PromptTokens: 9, WallMs: 3000},
			},
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
	}, time.Minute)

	if !sum.Ran || sum.Winner != BaselineLabel {
		t.Fatalf("baseline pass with three samples must run, got %+v", sum)
	}
	m := sum.WinnerMeasurement
	if m == nil || m.Samples != 2 {
		t.Fatalf("the two post-warmup samples must be aggregated, got %+v", m)
	}
	// The middle KEPT sample: 12 tok/s, 0.3s TTFT — the warmup 6 must
	// not drag the score down, and no lucky sample can inflate it (the
	// reported number is always an observed measurement).
	if m.GenTokensPerSec != 12 || m.TTFTSeconds != 0.3 {
		t.Fatalf("middle-sample aggregation expected (12 tok/s, 0.3s), got %+v", m)
	}
}

// TestCalibrateWinnerReapplyFailureRollsBack proves the rollback
// contract: when the winner is not the profile currently applied and its
// re-apply FAILS, the verified baseline is restored (spec §2.10).
func TestCalibrateWinnerReapplyFailureRollsBack(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()
	fast := baseWithFastCtx() // measured LAST, slower than the GPU candidate

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
			"cpu/4096": {TTFTMs: 500, Tokens: 24, TokensPerSec: 20, WallMs: 1500},
		},
	}

	// Apply call sequence: (1) GPU boot, (2) fast-context boot,
	// (3) winner re-apply — the third must fail so the rollback to the
	// verified baseline runs (call 4).
	wrapped := &failingApply{runner: runner, failOn: map[int]bool{3: true}}

	sum := Calibrate(context.Background(), wrapped, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
		{Label: "fast context", Profile: fast},
	}, time.Minute)

	if sum.Winner != BaselineLabel || sum.Changed {
		t.Fatalf("failed winner re-apply must roll back to the baseline, got %+v", sum)
	}
	if sum.Failed {
		t.Fatalf("a successful rollback is NOT a supervised failure: %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("rollback must be documented in the summary reason")
	}
	// The last applied profile is the rollback (cpu baseline).
	if got := runner.applied[len(runner.applied)-1]; got != "cpu/8192" {
		t.Fatalf("final applied = %q, want the rolled-back baseline", got)
	}
	// The rollback failure path must not keep the failed winner's metrics.
	if sum.WinnerMeasurement != nil && sum.WinnerMeasurement.Candidate != BaselineLabel {
		t.Fatalf("winner measurement must follow the rolled-back winner, got %+v", sum.WinnerMeasurement)
	}
}

// TestCalibrateRollbackFailureIsSupervised proves spec §2.11: when the
// winner re-apply fails AND the baseline rollback fails too, the pass
// ends in the supervised failure state — no winner, no verified claim,
// the real reason surfaced.
func TestCalibrateRollbackFailureIsSupervised(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()
	fast := baseWithFastCtx()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
			"cpu/4096": {TTFTMs: 500, Tokens: 24, TokensPerSec: 20, WallMs: 1500},
		},
	}

	// (1) GPU boot, (2) fast-context boot, (3) winner re-apply fails,
	// (4) baseline rollback ALSO fails.
	wrapped := &failingApply{runner: runner, failOn: map[int]bool{3: true, 4: true}}

	sum := Calibrate(context.Background(), wrapped, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
		{Label: "fast context", Profile: fast},
	}, time.Minute)

	if !sum.Failed {
		t.Fatalf("a failed rollback must surface the supervised failure state, got %+v", sum)
	}
	if sum.Winner != "" || sum.WinnerProfile != nil || sum.WinnerMeasurement != nil {
		t.Fatalf("a supervised failure must never claim a winning profile, got %+v", sum)
	}
	if sum.Reason == "" || !containsAll(sum.Reason, "rollback failed", "supervised") {
		t.Fatalf("the supervised reason must document both failures, got %q", sum.Reason)
	}
	// The measurements are honest history; the verdict is the failure.
	if len(sum.Measurements) == 0 {
		t.Fatal("the failed pass still records what it measured")
	}
}

// TestCalibrateMidPassRestoreFailureIsSupervised proves the supervised
// failure state also covers a failed RESTORE after a candidate failure
// (the engine cannot be brought back to the verified profile).
func TestCalibrateMidPassRestoreFailureIsSupervised(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
		},
		measureErr: map[string]error{
			"gpu/8192": errors.New("generation failed"),
		},
	}

	// (1) GPU boot, (2) restore-to-baseline — must fail.
	wrapped := &failingApply{runner: runner, failOn: map[int]bool{2: true}}

	sum := Calibrate(context.Background(), wrapped, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Failed {
		t.Fatalf("a failed mid-pass restore is a supervised failure, got %+v", sum)
	}
	if sum.Winner != "" || sum.Changed {
		t.Fatalf("a supervised failure never claims a winner, got %+v", sum)
	}
}

// TestCalibrateCancellationRestoresBaseline proves the cancellation
// path: a measurement cancelled mid-flight is a failed candidate — the
// verified baseline is restored and no winner is claimed.
func TestCalibrateCancellationRestoresBaseline(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
		},
		measureErr: map[string]error{
			"gpu/8192": context.Canceled,
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if sum.Failed {
		t.Fatalf("a clean restore after cancellation is not a supervised failure: %+v", sum)
	}
	if sum.Winner != BaselineLabel {
		t.Fatalf("the baseline must win after cancellation, got %+v", sum)
	}
	if len(runner.applied) != 2 || runner.applied[len(runner.applied)-1] != "cpu/8192" {
		t.Fatalf("cancellation must restore the baseline, applied=%v", runner.applied)
	}
}

// TestCalibrateRunArrivalAbortsCleanly proves the run-race contract: the
// runner's run check fires BEFORE any mutation, so an aborted pass leaves
// the verified state exactly as it was and claims nothing.
func TestCalibrateRunArrivalAbortsCleanly(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
		},
		bootErr: map[string]error{
			"gpu/8192": fmt.Errorf("wrapped: %w", ErrCalibrationAborted),
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Aborted {
		t.Fatalf("a run-arrival abort must be the clean abort state, got %+v", sum)
	}
	if sum.Ran || sum.Winner != "" || sum.WinnerProfile != nil || sum.Failed {
		t.Fatalf("an aborted pass must claim nothing, got %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("the abort reason must be documented")
	}
	// The abort fired BEFORE the config write and BEFORE the restart —
	// no restore cycle may run (that would restart under the live run).
	if len(runner.persisted) != 0 || len(runner.applied) != 0 {
		t.Fatalf("an aborted pass must not touch engine or config: applied=%v persisted=%v",
			runner.applied, runner.persisted)
	}
	// The baseline's in-place measurement happened before the abort.
	if len(runner.measured) != MeasureSamples {
		t.Fatalf("the baseline measurement runs before the abort, measured=%v", runner.measured)
	}
}

// TestCalibrateModelServingVerificationRequired proves spec §2.13: a
// candidate whose engine silently swapped to a DIFFERENT model is NOT
// stable — it can never win, and the baseline is restored.
func TestCalibrateModelServingVerificationRequired(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
		},
		divertModel: true, // the engine stopped serving the expected model
		stable:      map[string]bool{"cpu/8192": true, "gpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if sum.Ran {
		t.Fatalf("no candidate can win when the served model does not match, got %+v", sum)
	}
	if sum.Winner != "" || sum.Failed {
		t.Fatalf("a model mismatch is an honest not-benchmarked verdict, got %+v", sum)
	}
	// The GPU candidate was rolled back; the engine is back on the
	// baseline profile.
	if got := runner.applied[len(runner.applied)-1]; got != "cpu/8192" {
		t.Fatalf("baseline restored after the model mismatch, applied=%v", runner.applied)
	}
}

// TestCalibrateFinalStateConsistency proves spec §2.9 end-to-end: after
// the pass, the serving profile, the persisted config and the recorded
// winner all describe the SAME profile.
func TestCalibrateFinalStateConsistency(t *testing.T) {
	cases := []struct {
		name           string
		measure        map[string]llm.PerfStats
		measureErrDone bool
		wantWinning    string // "" = no winner (baseline final)
		wantFinal      string
		wantChanged    bool
	}{
		{
			name: "winner is the last measured candidate",
			measure: map[string]llm.PerfStats{
				"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
				"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
			},
			wantWinning: "GPU offload",
			wantFinal:   "gpu/8192",
			wantChanged: true,
		},
		{
			name: "baseline wins after a slower candidate",
			measure: map[string]llm.PerfStats{
				"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
				"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 8, WallMs: 900},
			},
			wantWinning: BaselineLabel,
			wantFinal:   "cpu/8192",
			wantChanged: false,
		},
		{
			name: "nothing stable",
			measure: map[string]llm.PerfStats{
				"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
				"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
			},
			measureErrDone: true,
			wantWinning:    "",
			wantFinal:      "cpu/8192",
			wantChanged:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := baseRec()
			gpu := baseWithGPU()

			runner := &realisticRunner{
				initial: "cpu/8192",
				measure: tc.measure,
			}
			if tc.measureErrDone {
				runner.measureErr = map[string]error{
					"cpu/8192": errors.New("offline"),
					"gpu/8192": errors.New("offline"),
				}
			}

			sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
				{Label: BaselineLabel, Profile: base},
				{Label: "GPU offload", Profile: gpu},
			}, time.Minute)

			if tc.wantWinning == "" && sum.Ran {
				t.Fatalf("no winner expected, got %+v", sum)
			}
			if tc.wantWinning != "" && sum.Winner != tc.wantWinning {
				t.Fatalf("winner = %q, want %q", sum.Winner, tc.wantWinning)
			}

			serving := runner.currentLabel()
			persisted := runner.persisted[len(runner.persisted)-1]
			if serving != tc.wantFinal || persisted != tc.wantFinal {
				t.Fatalf("final consistency broken: serving=%q persisted=%q want=%q (sum=%+v)",
					serving, persisted, tc.wantFinal, sum)
			}
			if sum.Changed != tc.wantChanged {
				t.Fatalf("Changed = %v, want %v (final %q vs baseline)", sum.Changed, tc.wantChanged, tc.wantFinal)
			}
			if sum.Failed {
				t.Fatalf("clean pass must not be Failed: %+v", sum)
			}
		})
	}
}

// TestCalibrateWinnerMetricsCorrespondToWinnerCandidate proves spec §7
// at the source: WinnerMeasurement belongs to the candidate whose label
// equals Winner — never Measurements[0] unless that IS the winner.
func TestCalibrateWinnerMetricsCorrespondToWinnerCandidate(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()
	fast := baseWithFastCtx()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, WallMs: 900},
			"cpu/4096": {TTFTMs: 500, Tokens: 24, TokensPerSec: 20, WallMs: 1500},
		},
	}

	sum := Calibrate(context.Background(), runner, testModelPath, base, []Candidate{
		{Label: BaselineLabel, Profile: base}, // measured FIRST (Measurements[0])
		{Label: "GPU offload", Profile: gpu},  // the winner
		{Label: "fast context", Profile: fast},
	}, time.Minute)

	if sum.Winner != "GPU offload" {
		t.Fatalf("winner = %q, want GPU offload", sum.Winner)
	}
	first := sum.Measurements[0]
	if first.Candidate != BaselineLabel {
		t.Fatalf("test premise: Measurements[0] must be the baseline, got %+v", first)
	}
	if sum.WinnerMeasurement == nil || sum.WinnerMeasurement.Candidate != "GPU offload" {
		t.Fatalf("winner metrics must come from the winner candidate, got %+v", sum.WinnerMeasurement)
	}
	if sum.WinnerMeasurement.GenTokensPerSec == first.GenTokensPerSec {
		t.Fatal("winner metrics must not be Measurements[0] in disguise")
	}
	if sum.WinnerMeasurement.GenTokensPerSec != 40 || sum.WinnerMeasurement.TTFTSeconds != 0.3 {
		t.Fatalf("winner metrics must be the GPU candidate's own numbers, got %+v", sum.WinnerMeasurement)
	}
}

// failingApply fails selected ApplyProfile calls (proving the rollback
// and supervised-failure paths).
type failingApply struct {
	runner  *realisticRunner
	failOn  map[int]bool
	applied int
}

func (f *failingApply) ApplyProfile(ctx context.Context, path string, rec recommendation.Recommendation) error {
	f.applied++
	if f.failOn[f.applied] {
		// Production persists BEFORE the restart fails: the runner's own
		// bookkeeping records the write even though the apply failed.
		f.runner.ApplyProfile(ctx, path, rec)
		return errors.New("apply exploded")
	}
	return f.runner.ApplyProfile(ctx, path, rec)
}

func (f *failingApply) MeasureGeneration(ctx context.Context, prompt string, maxTokens int) (llm.PerfStats, error) {
	return f.runner.MeasureGeneration(ctx, prompt, maxTokens)
}

func (f *failingApply) EngineStable(expectedModel string) bool {
	return f.runner.EngineStable(expectedModel)
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---- store tests (spec §6: retained calibration invalidated safely) ----

// TestStoreRetainAndReuse proves the retain/reuse fingerprinting: a
// retained record is reused only for the SAME model fingerprint, the
// SAME machine/engine fingerprint, AND the SAME task profile.
func TestStoreRetainAndReuse(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(model, []byte("gguf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	mk, err := ModelKey(model)
	if err != nil {
		t.Fatal(err)
	}
	hwKey := "os=linux/amd64;cpu=Test/8/16;ram=17179869184;gpu=unknown;backend=vulkan=false;tag=b1;bin=missing"

	if _, ok := Retained(dir, mk, hwKey, "chat"); ok {
		t.Fatal("empty store must not retain anything")
	}

	rec := Record{
		ModelPath:       model,
		Winner:          "GPU offload",
		Profile:         baseWithGPU(),
		GenTokensPerSec: 40,
		TTFTSeconds:     0.3,
		MeasuredAt:      time.Now().UTC(),
	}
	if rec.Profile.Task != recommendation.TaskChat {
		t.Fatal("test premise: the retained profile must carry the chat task")
	}
	if err := Retain(dir, mk, hwKey, rec); err != nil {
		t.Fatalf("retain: %v", err)
	}

	got, ok := Retained(dir, mk, hwKey, "chat")
	if !ok || got.Winner != "GPU offload" || got.GenTokensPerSec != 40 {
		t.Fatalf("retained record must round-trip, got %+v (ok=%v)", got, ok)
	}
	if got.Task != "chat" {
		t.Fatalf("record must be scoped to the profile's task, got %q", got.Task)
	}

	// A DIFFERENT machine fingerprint must NOT reuse the record.
	if _, ok := Retained(dir, mk, "os=linux/amd64;cpu=Other/4/8;ram=8589934592;gpu=unknown;backend=vulkan=false;tag=b1;bin=missing", "chat"); ok {
		t.Fatal("a different machine must not reuse the retained measurement")
	}

	// A DIFFERENT task profile must NOT reuse the record (a chat-tuned
	// profile must not silently serve an agent run — spec §5).
	if _, ok := Retained(dir, mk, hwKey, "agent"); ok {
		t.Fatal("a different task must not reuse the retained measurement")
	}

	// A DIFFERENT model fingerprint must NOT reuse the record.
	other := filepath.Join(dir, "other.gguf")
	if err := os.WriteFile(other, []byte("other-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	omk, err := ModelKey(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Retained(dir, omk, hwKey, "chat"); ok {
		t.Fatal("a different model must not reuse the retained measurement")
	}

	// The same model file, REPLACED (new mtime), must NOT reuse.
	if err := os.WriteFile(model, []byte("gguf-bytes-v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk2, err := ModelKey(model)
	if err != nil {
		t.Fatal(err)
	}
	if mk2 == mk {
		t.Fatal("test premise: rewriting the model must change its fingerprint")
	}
	if _, ok := Retained(dir, mk2, hwKey, "chat"); ok {
		t.Fatal("a changed model file must not reuse the retained measurement")
	}
}

// TestStoreIncompleteRecordFailsClosed proves corrupt/incomplete records
// are never reusable (spec §6: fail closed).
func TestStoreIncompleteRecordFailsClosed(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(model, []byte("gguf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk, err := ModelKey(model)
	if err != nil {
		t.Fatal(err)
	}
	hwKey := "os=linux/amd64;cpu=Test/8/16;ram=1;gpu=unknown;backend=vulkan=false;tag=b1;bin=missing"

	for name, mutate := range map[string]func(*Record){
		"missing winner":  func(r *Record) { r.Winner = "" },
		"missing task":    func(r *Record) { r.Task = "" },
		"bogus task":      func(r *Record) { r.Task = "gaming" },
		"missing profile": func(r *Record) { r.Profile.Context = 0 },
		"missing time":    func(r *Record) { r.MeasuredAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			// Write the record with a BROKEN task field directly (Retain
			// refuses invalid records, so the store is crafted by hand).
			rec := Record{
				ModelKey:        mk,
				HWKey:           hwKey,
				Task:            "chat",
				Winner:          "GPU offload",
				Profile:         baseWithGPU(),
				GenTokensPerSec: 40,
				MeasuredAt:      time.Now().UTC(),
			}
			mutate(&rec)

			sf := storeFile{Records: []Record{rec}}
			data, err := json.Marshal(sf)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, StoreFileName), data, 0o644); err != nil {
				t.Fatal(err)
			}

			if _, ok := Retained(dir, mk, hwKey, "chat"); ok {
				t.Fatalf("an incomplete record must fail closed (%s)", name)
			}
		})
	}
}

// TestStoreCorruptFileFailsClosed proves a corrupt store is a miss, and
// the next successful Retain replaces it (never trusted, never a crash).
func TestStoreCorruptFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StoreFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := Retained(dir, "mk", "hw", "chat"); ok {
		t.Fatal("a corrupt store must fail closed")
	}

	model := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(model, []byte("gguf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk, err := ModelKey(model)
	if err != nil {
		t.Fatal(err)
	}

	rec := Record{Winner: "safe baseline", Profile: baseRec(), MeasuredAt: time.Now().UTC()}
	if err := Retain(dir, mk, "hw", rec); err != nil {
		t.Fatalf("retain must replace a corrupt store: %v", err)
	}
	if _, ok := Retained(dir, mk, "hw", "chat"); !ok {
		t.Fatal("the replaced store must serve the new record")
	}
}

// TestHWKeySensitiveToIdentityParts proves the machine fingerprint
// distinguishes every identity dimension spec §6 requires: CPU identity,
// RAM, GPU identity + driver, accelerator/backend, and engine build.
func TestHWKeySensitiveToIdentityParts(t *testing.T) {
	base := hardware.Profile{
		OS:      "windows",
		Arch:    "amd64",
		CPU:     hardware.CPU{Name: "Ryzen 7", PhysicalCores: 8, LogicalCores: 16},
		RAM:     hardware.RAM{TotalBytes: 32 << 30},
		GPUs:    []hardware.GPU{{Vendor: "AMD", Name: "RX 7800", VRAMBytes: 16 << 30, DriverVer: "24.5.1"}},
		Backend: hardware.Backend{Vulkan: true, EngineTag: "b4321", EngineBinary: ""},
	}

	baseKey := HWKey(base)

	mutants := map[string]func(*hardware.Profile){
		"cpu name":    func(p *hardware.Profile) { p.CPU.Name = "Ryzen 9" },
		"core count":  func(p *hardware.Profile) { p.CPU.PhysicalCores = 12 },
		"ram":         func(p *hardware.Profile) { p.RAM.TotalBytes = 64 << 30 },
		"gpu name":    func(p *hardware.Profile) { p.GPUs[0].Name = "RX 7900" },
		"gpu vram":    func(p *hardware.Profile) { p.GPUs[0].VRAMBytes = 20 << 30 },
		"gpu driver":  func(p *hardware.Profile) { p.GPUs[0].DriverVer = "24.9.1" },
		"backend":     func(p *hardware.Profile) { p.Backend.Vulkan = false },
		"engine tag":  func(p *hardware.Profile) { p.Backend.EngineTag = "b5000" },
		"os":          func(p *hardware.Profile) { p.OS = "linux" },
		"gpu removed": func(p *hardware.Profile) { p.GPUs = nil },
	}

	for name, mutate := range mutants {
		p := base
		// Deep-copy the GPU slice: the profile copy shares the backing
		// array, and a shallow copy would corrupt the base fixture.
		p.GPUs = append([]hardware.GPU(nil), base.GPUs...)
		mutate(&p)
		if HWKey(p) == baseKey {
			t.Fatalf("HWKey must change when %s changes", name)
		}
	}

	// Stable input → stable key (the SAME machine keeps its records).
	if HWKey(base) != baseKey {
		t.Fatal("HWKey must be deterministic for identical input")
	}
}
