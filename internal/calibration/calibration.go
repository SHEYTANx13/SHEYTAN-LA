// Package calibration — the v1.5.0 bounded automatic performance check,
// made a real TRANSACTIONAL optimization process in v1.5.1.
//
// This is NOT a second tuning engine. Every candidate profile is derived
// from the ONE recommendation engine (internal/recommendation), which
// already computes the evidence-based profile from measured hardware,
// GGUF facts and verified engine capabilities. Calibration only ADDS
// the missing stages of the documented pipeline:
//
//	selected model → GGUF facts → measured RAM/CPU/GPU/NPU →
//	engine capability/device enumeration → resource-fit calculation →
//	safe initial profile → engine start → SHORT BOUNDED CALIBRATION →
//	measured profile comparison → retain verified profile
//	                                         ^^^^^^^^^^ this package
//
// Contract rules (spec §4):
//   - the candidate set is SMALL and derived from hardware/model
//     evidence (safe baseline + GPU-offload + CPU-safe + at most one
//     batch/context variant; capped at MaxCandidates);
//   - a candidate wins ONLY from a measured successful run — never from
//     filename, size or heuristics;
//   - measured values are REAL (TTFT / generation throughput / prompt
//     throughput through the live engine); nothing is fabricated;
//   - repeated short real measurements are preferred over a single
//     noisy sample when the total budget permits; aggregation is
//     deterministic (medians) and high-variance candidates are rejected;
//   - when real calibration is unavailable, the evidence-based profile
//     is kept and honestly marked "not benchmarked";
//   - budgets are bounded: a candidate that cannot boot or finish one
//     short generation within its budget is skipped, never retried in a
//     loop;
//   - the fastest STABLE measured profile is retained (persisted per
//     model+machine fingerprint) so the next selection of the same model
//     reuses the measured result instead of re-benchmarking.
//
// Transaction rules (v1.5.1, spec §2) — Calibrate is one transaction over
// the engine/configuration state:
//
//   - the baseline profile and serving model are already applied and
//     verified BEFORE the pass starts (the caller's contract);
//   - the pass tracks the EXACT profile currently believed to be applied
//     and the last VERIFIED-good profile as two separate facts;
//   - after every failed boot, failed generation, unstable measurement,
//     cancellation or budget exhaustion, the last verified profile is
//     restored — a failed non-baseline candidate never leaves its
//     configuration active;
//   - when the baseline wins after another candidate was measured, the
//     baseline is explicitly restored;
//   - at completion the running engine, the applied profile and the
//     recorded winner all describe the same model + profile;
//   - a winner re-apply failure rolls back to the verified baseline, and
//     a rollback failure surfaces a supervised failure state (Failed) —
//     never a ready/verified/winner claim;
//   - EngineStable also verifies the expected model is still being served.
package calibration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// Bounded budgets (spec §4: "short bounded calibration" — never an
// enormous search space, never unbounded).
const (
	// DefaultTotalBudget bounds the WHOLE calibration pass.
	DefaultTotalBudget = 120 * time.Second
	// DefaultBootBudget bounds one candidate's engine start (and one
	// winner/rollback re-apply).
	DefaultBootBudget = 60 * time.Second
	// DefaultMeasureBudget bounds ONE short generation sample.
	DefaultMeasureBudget = 30 * time.Second
	// MaxCandidates caps the candidate set INCLUDING the safe baseline.
	MaxCandidates = 3
	// MeasureMaxTokens is the short generation length used per sample.
	MeasureMaxTokens = 24
	// MeasureSamples is the maximum number of repeated short real
	// measurements taken per candidate. Repeated short samples beat a
	// single noisy sample; the pass stops early when the budget is spent.
	MeasureSamples = 3
	// VarianceRejectRatio is the deterministic stability gate: when two
	// or more samples succeeded and the fastest exceeds the slowest by
	// more than this ratio, the candidate is rejected as unstable.
	VarianceRejectRatio = 2.0
	// MeasurePrompt is the calibration prompt (short, deterministic; the
	// CONTENT is irrelevant — only the measured timings matter).
	MeasurePrompt = "Reply with a short sentence about the weather."

	// BaselineLabel is the reserved label of the already-applied,
	// already-verified baseline candidate (measured in place — it never
	// costs a boot cycle).
	BaselineLabel = "safe baseline"
)

// ErrCalibrationAborted is returned by a Runner when the pass must stop
// because a user run arrived (the user's work wins over the benchmark).
var ErrCalibrationAborted = errors.New("a run started — calibration aborted")

// Runner is the engine boundary calibration drives. The runtime stack
// implements it; tests inject a scripted runner to prove the CONTROL
// CONTRACT (retain/rollback/budget), never to fabricate product
// performance claims.
type Runner interface {
	// ApplyProfile atomically applies the candidate profile for
	// modelPath and (re)starts the engine. It returns after the engine
	// is verified ready AND serving modelPath, or with the failure. On
	// failure the engine/configuration state is untrusted — the caller
	// must restore the last verified profile.
	ApplyProfile(ctx context.Context, modelPath string, rec recommendation.Recommendation) error

	// MeasureGeneration runs ONE short real generation against the live
	// engine and returns the measured performance. maxTokens bounds the
	// generation so a slow candidate cannot blow the budget.
	MeasureGeneration(ctx context.Context, prompt string, maxTokens int) (llm.PerfStats, error)

	// EngineStable reports whether the engine stayed healthy through the
	// last measurement (no death, no watchdog restart) AND the expected
	// model is still the one being served (spec §2.13).
	EngineStable(expectedModel string) bool
}

// Candidate is one bounded profile variant. Every candidate is derived
// from the recommendation engine's baseline — there is no independent
// knob arithmetic in this package.
type Candidate struct {
	// Label is the human-readable candidate name surfaced in summaries.
	Label string
	// Profile is the recommendation.Recommendation variant to apply.
	Profile recommendation.Recommendation
	// Reason cites the measured evidence that justified the variant.
	Reason string
}

// Measurement is one candidate's REAL measured outcome (aggregated
// deterministically over the taken samples). Zero values mean "not
// measured" — never a fabricated number.
type Measurement struct {
	Candidate          string  `json:"candidate"`
	Booted             bool    `json:"booted"`
	Measured           bool    `json:"measured"`
	TTFTSeconds        float64 `json:"ttftSeconds,omitempty"`
	GenTokensPerSec    float64 `json:"genTokensPerSec,omitempty"`
	PromptTokensPerSec float64 `json:"promptTokensPerSec,omitempty"`
	TotalSeconds       float64 `json:"totalSeconds,omitempty"`
	Stable             bool    `json:"stable"`
	// Samples is how many short real measurements were aggregated (0 for
	// candidates that never reached a measurement).
	Samples int `json:"samples,omitempty"`
	// HighVariance marks candidates whose repeated samples diverged
	// beyond VarianceRejectRatio — deterministic rejection input.
	HighVariance bool   `json:"highVariance,omitempty"`
	Note         string `json:"note,omitempty"`
}

// Summary is the calibration verdict.
type Summary struct {
	// Ran is true iff at least one candidate produced a measured, stable
	// run. When false (and Failed is false), Reason explains why
	// calibration was unavailable — never a fake "we measured something"
	// claim. A Failed pass keeps Ran as the honest history of what was
	// measured, but never claims a winner.
	Ran bool `json:"ran"`
	// Reason documents why calibration did not run / stopped early /
	// ended in a supervised failure.
	Reason string `json:"reason,omitempty"`
	// Winner is the label of the fastest stable measured candidate.
	// Empty when Failed — a supervised failure never claims a winner.
	Winner string `json:"winner,omitempty"`
	// WinnerProfile is the winning profile (nil when not ran or failed).
	WinnerProfile *recommendation.Recommendation `json:"winnerProfile,omitempty"`
	// WinnerMeasurement is the MEASURED run of the candidate whose label
	// equals Winner — never another candidate's numbers (spec §7).
	WinnerMeasurement *Measurement `json:"winnerMeasurement,omitempty"`
	// Measurements carries every candidate's honest outcome.
	Measurements []Measurement `json:"measurements"`
	// Changed describes the ACTUAL FINAL profile versus the original
	// baseline (not merely the winner label): true when the final
	// applied profile differs from the baseline the pass started from.
	Changed bool `json:"changed"`
	// Failed is the supervised failure state (spec §2.11): a rollback
	// itself failed, so the engine/config state is NOT verified-good.
	// The caller must surface a failure — never ready, never verified,
	// never a winning profile.
	Failed bool `json:"failed,omitempty"`
	// Aborted is the clean abort state: a user run arrived and the pass
	// stopped BEFORE mutating anything (the runner's run check fires
	// before its config write). The verified profile stays active; no
	// winner is claimed. The user's work wins over the benchmark.
	Aborted bool `json:"aborted,omitempty"`
	// At is when the pass finished.
	At time.Time `json:"at"`
}

// Candidates derives the SMALL bounded candidate set from the
// recommendation baseline and measured evidence:
//
//  1. "safe baseline"  — the recommendation as computed (always first);
//  2. "GPU offload"    — only when a GPU was actually measured and the
//     baseline stayed on CPU;
//  3. "CPU safe"       — only when the baseline offloads to the GPU
//     (bounds the risk of a bad GPU path);
//  4. "fast context"   — only when the baseline context exceeds 8192
//     (one prompt-throughput variant: context halved, floor 4096).
//
// Identical duplicates are dropped and the set is capped at
// MaxCandidates. No candidate is invented from filename or size.
func Candidates(base recommendation.Recommendation, hw hardware.Profile, caps *llm.EngineCaps) []Candidate {
	out := []Candidate{{
		Label:   BaselineLabel,
		Profile: base,
		Reason:  "resource-fit recommendation from measured hardware + model facts + verified engine capabilities",
	}}

	gpuEvidence := hw.HasGPU()

	if gpuEvidence && !base.GPUAutoOffload && base.GPULayers == 0 {
		v := base
		v.GPUAutoOffload = true
		v.GPULayers = 99
		if !profileEqual(out[0].Profile, v) {
			out = append(out, Candidate{
				Label:   "GPU offload",
				Profile: v,
				Reason:  "measured GPU present — verify the offload posture actually wins on this machine",
			})
		}
	}

	if base.GPUAutoOffload || base.GPULayers > 0 {
		v := base
		v.GPUAutoOffload = false
		v.GPULayers = 0
		if !profileEqual(out[0].Profile, v) && !profileEqual(lastProfile(out), v) {
			out = append(out, Candidate{
				Label:   "CPU safe",
				Profile: v,
				Reason:  "baseline offloads to the GPU — bound the risk with a measured CPU-only comparison",
			})
		}
	}

	if base.Context > 8192 {
		v := base
		v.Context = base.Context / 2
		if v.Context < 4096 {
			v.Context = 4096
		}
		if !profileEqual(out[0].Profile, v) && !profileEqual(lastProfile(out), v) {
			out = append(out, Candidate{
				Label:   "fast context",
				Profile: v,
				Reason:  "one prompt-throughput variant: halved context with the same verified posture",
			})
		}
	}

	if len(out) > MaxCandidates {
		out = out[:MaxCandidates]
	}

	return out
}

func lastProfile(cands []Candidate) recommendation.Recommendation {
	if len(cands) == 0 {
		return recommendation.Recommendation{}
	}
	return cands[len(cands)-1].Profile
}

// profileEqual compares the fields that change engine launch args.
func profileEqual(a, b recommendation.Recommendation) bool {
	return a.Context == b.Context &&
		a.Threads == b.Threads &&
		a.ThreadsBatch == b.ThreadsBatch &&
		a.GPULayers == b.GPULayers &&
		a.GPUAutoOffload == b.GPUAutoOffload &&
		a.UBatchSize == b.UBatchSize &&
		a.FlashAttention == b.FlashAttention &&
		a.KVCacheQuant == b.KVCacheQuant
}

// Calibrate runs the bounded pass. The baseline profile MUST already be
// applied and verified serving (the selection flow applies and verifies
// it before calling) — the baseline candidate is measured in place, each
// other candidate costs one ApplyProfile, and the pass leaves the
// fastest stable measured profile applied.
//
// Calibrate is transactional: on every failed boot, failed generation,
// unstable measurement, cancellation or budget exhaustion the last
// verified profile is restored; a winner switch (or a baseline win after
// a non-baseline candidate was measured) is applied explicitly; and a
// rollback failure surfaces a supervised failure state instead of a
// winner claim.
func Calibrate(
	ctx context.Context,
	r Runner,
	modelPath string,
	base recommendation.Recommendation,
	cands []Candidate,
	totalBudget time.Duration,
) Summary {
	return calibrate(ctx, r, modelPath, base, cands, totalBudget, nil)
}

// calibrate is the implementation with an injectable clock. Production
// passes nil (the wall clock); tests inject a deterministic clock — the
// Windows scheduler's coarse monotonic granularity made a nanosecond
// budget flaky, and a bounded-budget contract must be provable without
// racing the scheduler (spec §8: "already-expired budget skips all
// candidates deterministically").
func calibrate(
	ctx context.Context,
	r Runner,
	modelPath string,
	base recommendation.Recommendation,
	cands []Candidate,
	totalBudget time.Duration,
	now func() time.Time,
) Summary {
	if totalBudget <= 0 {
		totalBudget = DefaultTotalBudget
	}
	if len(cands) == 0 {
		cands = []Candidate{{Label: BaselineLabel, Profile: base}}
	}
	if now == nil {
		now = time.Now
	}

	start := now()
	sum := Summary{At: start}
	deadline := start.Add(totalBudget)

	// ---- transaction state -------------------------------------------------
	//
	// applied  — the profile the pass last COMMANDED onto the engine and
	//            config (starts at the baseline the caller applied).
	// verified — the last profile PROVEN good: booted, measured and
	//            stable (starts at the baseline, which the caller
	//            verified before starting the pass — spec §2.1).
	//
	// A candidate's boot makes it applied but NOT verified; only a
	// stable measured run promotes it. Every failure path restores
	// `verified`; a restore is a real transition only when the engine
	// currently sits on something else (no wasted stop/start cycles).
	applied := base
	verified := base

	// restore brings the engine/config back to the last verified profile
	// and reports whether that verified state holds afterwards.
	//
	// force marks transitions where the failed step may have left the
	// PERSISTED config on a candidate (the production runner persists
	// before the restart): the verified profile is re-applied even when
	// the controller believes nothing changed — an idempotent
	// re-assertion, never trusted bookkeeping.
	//
	// When the restore itself fails the pass ends in the supervised
	// failure state: no winner, no ready claim (spec §2.11).
	restore := func(force bool, why string) bool {
		if !force && profileEqual(applied, verified) {
			// Nothing was displaced — the verified profile is still the
			// applied one (e.g. the baseline failed in place).
			return true
		}

		rbCtx, rbCancel := boundedCtx(ctx, DefaultBootBudget, deadline, now)
		err := r.ApplyProfile(rbCtx, modelPath, verified)
		rbCancel()

		if err != nil {
			sum.Failed = true
			sum.Winner = ""
			sum.WinnerProfile = nil
			sum.WinnerMeasurement = nil
			sum.Changed = false
			sum.Reason = fmt.Sprintf(
				"calibration aborted (%s): restoring the verified profile failed: %v — engine state is supervised",
				why, err,
			)
			return false
		}

		applied = verified
		return true
	}

	// abortPass settles the clean run-arrival abort: the runner checked
	// for runs BEFORE any mutation, so the verified state holds; the
	// pass simply stops claiming anything.
	abortPass := func() {
		sum.Aborted = true
		sum.Ran = false
		sum.Winner = ""
		sum.WinnerProfile = nil
		sum.WinnerMeasurement = nil
		sum.Changed = false
		sum.Reason = "calibration skipped — a user run started; the verified profile stays active"
	}

	type scored struct {
		cand Candidate
		meas Measurement
	}
	var stable []scored

	for _, cand := range cands {
		if now().After(deadline) {
			sum.Measurements = append(sum.Measurements, Measurement{
				Candidate: cand.Label,
				Note:      "skipped: calibration budget exhausted",
			})
			continue
		}

		// The baseline is already applied and verified serving — measuring
		// it in place avoids one stop/start cycle (spec §6). Every other
		// candidate needs its own verified boot.
		if cand.Label != BaselineLabel {
			bootCtx, bootCancel := boundedCtx(ctx, DefaultBootBudget, deadline, now)
			err := r.ApplyProfile(bootCtx, modelPath, cand.Profile)
			bootCancel()

			if err != nil {
				if errors.Is(err, ErrCalibrationAborted) {
					// A user run arrived — the runner stopped BEFORE any
					// mutation, so the verified state still holds.
					abortPass()
					return sum
				}
				sum.Measurements = append(sum.Measurements, Measurement{
					Candidate: cand.Label,
					Booted:    false,
					Note:      fmt.Sprintf("did not boot: %v", err),
				})
				// A failed boot leaves the engine/config state UNTRUSTED
				// (the runner persists the candidate before the restart).
				// Restore the last verified profile before continuing —
				// a failed non-baseline candidate never stays active.
				if !restore(true, fmt.Sprintf("candidate %q failed to boot", cand.Label)) {
					return sum
				}
				continue
			}
			applied = cand.Profile
		}

		// ---- measurement: repeated short REAL samples -------------------
		var samples []llm.PerfStats
		var sampleErr error

		for i := 0; i < MeasureSamples; i++ {
			if i > 0 && now().After(deadline) {
				break // the budget bounds the sampling, never a retry loop
			}

			measCtx, measCancel := boundedCtx(ctx, DefaultMeasureBudget, deadline, now)
			perf, err := r.MeasureGeneration(measCtx, MeasurePrompt, MeasureMaxTokens)
			measCancel()

			if err != nil {
				sampleErr = err
				break // a failed generation ends this candidate's sampling
			}
			samples = append(samples, perf)
		}

		if len(samples) == 0 {
			note := "generation failed"
			if sampleErr != nil {
				note = fmt.Sprintf("generation failed: %v", sampleErr)
			}
			sum.Measurements = append(sum.Measurements, Measurement{
				Candidate: cand.Label,
				Booted:    true,
				Measured:  false,
				Samples:   len(samples),
				Note:      note,
			})
			if !restore(false, fmt.Sprintf("candidate %q failed to generate", cand.Label)) {
				return sum
			}
			continue
		}

		m := aggregate(cand.Label, samples)
		m.Stable = r.EngineStable(modelPath)

		// Prompt throughput is derived from the MEASURED prompt size and
		// the MEASURED time-to-first-token (prompt processing completes
		// before the first token): still a real measurement, no fabric.
		if m.GenTokensPerSec <= 0 {
			m.Stable = false
			m.Note = "unmeasured throughput — never a winner"
		} else if m.HighVariance {
			m.Stable = false
			m.Note = "high-variance samples rejected as unstable — never a winner"
		} else if !m.Stable {
			m.Note = "engine not stable through the measurement — never a winner"
		}

		sum.Measurements = append(sum.Measurements, m)

		if !m.Stable {
			if !restore(false, fmt.Sprintf("candidate %q measured unstable", cand.Label)) {
				return sum
			}
			continue
		}

		// Measured, stable, still serving the expected model: the
		// candidate profile is now the verified-good state.
		verified = cand.Profile
		applied = cand.Profile
		stable = append(stable, scored{cand: cand, meas: m})
	}

	if len(stable) == 0 {
		if !sum.Failed {
			sum.Ran = false
			sum.Reason = "no candidate produced a measured stable run — evidence-based profile kept, not benchmarked"
		}
		return sum
	}

	// Fastest stable measured profile: generation throughput first, TTFT
	// as the tie-breaker. sort.SliceStable keeps deterministic order.
	sort.SliceStable(stable, func(i, j int) bool {
		if stable[i].meas.GenTokensPerSec != stable[j].meas.GenTokensPerSec {
			return stable[i].meas.GenTokensPerSec > stable[j].meas.GenTokensPerSec
		}
		return stable[i].meas.TTFTSeconds < stable[j].meas.TTFTSeconds
	})

	winner := stable[0]
	sum.Ran = true
	sum.Winner = winner.cand.Label
	p := winner.cand.Profile
	sum.WinnerProfile = &p
	wm := winner.meas
	sum.WinnerMeasurement = &wm

	// ---- transaction close -------------------------------------------------
	//
	// Leave the WINNING profile applied. A re-apply is only a real
	// transition when the winner is not already the applied profile (no
	// wasted stop/start cycles). When that apply fails, roll back to the
	// verified baseline; when the rollback fails, surface the supervised
	// failure state (spec §2.10/§2.11).
	if !profileEqual(applied, winner.cand.Profile) {
		applyCtx, applyCancel := boundedCtx(ctx, DefaultBootBudget, deadline, now)
		err := r.ApplyProfile(applyCtx, modelPath, winner.cand.Profile)
		applyCancel()

		if err != nil {
			sum.Winner = BaselineLabel
			baseProfile := base
			sum.WinnerProfile = &baseProfile
			sum.WinnerMeasurement = measurementFor(sum.Measurements, BaselineLabel)

			rbCtx, rbCancel := boundedCtx(ctx, DefaultBootBudget, deadline, now)
			rbErr := r.ApplyProfile(rbCtx, modelPath, base)
			rbCancel()

			if rbErr != nil {
				sum.Failed = true
				sum.Winner = ""
				sum.WinnerProfile = nil
				sum.WinnerMeasurement = nil
				sum.Changed = false
				sum.Reason = fmt.Sprintf(
					"winner %q failed to re-apply (%v) AND baseline rollback failed (%v) — engine state is supervised",
					winner.cand.Label, err, rbErr,
				)
				return sum
			}

			applied = base
			verified = base
			sum.Reason = fmt.Sprintf(
				"winner %q failed to re-apply (%v) — rolled back to the safe baseline",
				winner.cand.Label, err,
			)
		} else {
			applied = winner.cand.Profile
			if winner.cand.Label == BaselineLabel {
				sum.Reason = "the baseline won — restored after non-baseline candidates were measured"
			}
		}
	}

	// Final consistency (spec §2.9): the recorded winner now describes
	// exactly the applied profile, and Changed describes the ACTUAL
	// FINAL profile versus the original baseline (spec §2.12).
	final := base
	if sum.WinnerProfile != nil {
		final = *sum.WinnerProfile
	}
	sum.Changed = !profileEqual(final, base)

	return sum
}

// aggregate folds the taken samples into ONE deterministic measurement:
// medians for every timing, and a HighVariance flag when the fastest and
// slowest KEPT samples diverge beyond VarianceRejectRatio.
//
// The FIRST sample is engine warmup whenever more than one sample was
// taken (cache fill, page-in, first-touch allocation) and is excluded
// from the aggregation deterministically — a cold first read must not
// poison the comparison, and a single lucky later sample cannot win
// either (the variance gate still applies to what is kept).
func aggregate(label string, samples []llm.PerfStats) Measurement {
	kept := samples
	if len(samples) > 1 {
		kept = samples[1:]
	}

	m := Measurement{Candidate: label, Booted: true, Measured: true, Samples: len(kept)}

	gens := make([]float64, 0, len(kept))
	ttfts := make([]float64, 0, len(kept))
	totals := make([]float64, 0, len(kept))
	prompts := make([]float64, 0, len(kept))

	for _, s := range kept {
		gens = append(gens, s.TokensPerSec)
		ttfts = append(ttfts, float64(s.TTFTMs)/1000.0)
		totals = append(totals, float64(s.WallMs)/1000.0)
		prompts = append(prompts, float64(s.PromptTokens))
	}

	m.GenTokensPerSec = median(gens)
	m.TTFTSeconds = median(ttfts)
	m.TotalSeconds = median(totals)

	if ttft := median(ttfts); ttft > 0 {
		if pt := median(prompts); pt > 0 {
			m.PromptTokensPerSec = pt / ttft
		}
	}

	if len(kept) >= 2 {
		fast, slow := maxOf(gens), minOf(gens)
		if slow > 0 && fast/slow > VarianceRejectRatio {
			m.HighVariance = true
		}
	}

	return m
}

// median returns the middle KEPT sample — the LOWER middle for an even
// count, so every reported number is a value the engine actually
// produced (never an interpolation), and the aggregate never flatters a
// noisy candidate.
func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sorted := append([]float64(nil), v...)
	sort.Float64s(sorted)
	return sorted[(len(sorted)-1)/2]
}

func maxOf(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func minOf(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

// measurementFor finds the recorded measurement of a candidate label
// (nil when that candidate produced no measurement).
func measurementFor(ms []Measurement, label string) *Measurement {
	for i := range ms {
		if ms[i].Candidate == label && ms[i].Measured {
			return &ms[i]
		}
	}
	return nil
}

// boundedCtx derives a bounded context: bounded by the step budget, the
// overall pass deadline and the parent cancellation. The clock seam
// keeps the derivation deterministic under tests.
func boundedCtx(parent context.Context, budget time.Duration, overallDeadline time.Time, now func() time.Time) (context.Context, context.CancelFunc) {
	end := now().Add(budget)
	if overallDeadline.Before(end) {
		end = overallDeadline
	}
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(end) {
		end = parentDeadline
	}
	return context.WithDeadline(parent, end)
}
