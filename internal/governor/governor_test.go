// governor_test.go — v1.8.0 DETERMINISTIC GOVERNOR TESTS.
//
// Every test drives the Governor through injected samples and an injected
// clock — no sleeps, no real time, no platform facts. The suite pins:
//
//   - resource-state calculation (measured vs explicit unknowns)
//   - threshold/level policy boundaries (the monitor classifies; the
//     Governor's envelope must agree with the vocabulary)
//   - sustained-pressure awareness (time-dimension hysteresis)
//   - rolling/decay behavior (one noisy sample never flips policy)
//   - unknown measurements → conservative admission, never fake certainty
//   - the admission-control matrix (heavyweight + memory plans)
//   - live vs next-run vs reload adjustment classes
//   - deterministic explanations (reasons name the condition)
//   - the self-model schema with provenance
//   - concurrency safety of the shared state (race gate covers it)
package governor

import (
        "sync"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

// fakeClock is a deterministic clock: Observe and Envelope read the same
// stepped timeline, so sustained-duration policy is exact.
type fakeClock struct {
        mu  sync.Mutex
        now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
        return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
        c.mu.Lock()
        defer c.mu.Unlock()
        return c.now
}

func (c *fakeClock) Step(d time.Duration) {
        c.mu.Lock()
        c.now = c.now.Add(d)
        c.mu.Unlock()
}

// sample builds a measured RAM sample at a given availability fraction.
func sample(total int64, availFrac float64, level preflight.PressureLevel) preflight.Sample {
        return sampleRSS(total, availFrac, 1*int64(gib), level)
}

// sampleRSS builds a measured sample with an explicit process RSS.
func sampleRSS(total int64, availFrac float64, procRSS int64, level preflight.PressureLevel) preflight.Sample {
        return preflight.Sample{
                At:                time.Now().UTC(),
                RAMTotalBytes:     total,
                RAMAvailableBytes: int64(float64(total) * availFrac),
                ProcRSSBytes:      procRSS,
                Level:             level,
        }
}

const gib = 1 << 30

// --- resource state -----------------------------------------------------------

func TestObserveFoldsMeasuredFacts(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        cpuCalls := 0
        infCalls := 0
        g := New(DefaultThresholds(), func() (float64, bool) {
                cpuCalls++
                return 42, true
        }, func() EngineFacts {
                return EngineFacts{Running: true, Model: "qwen2-7b-q4_k_m.gguf", RSSBytes: 5 * gib, Known: true}
        }, clk.Now)

        // v1.8.6: the inference footprint source (the wiring reads the
        // model-card/context authorities — a deterministic fixture here).
        g.SetInferenceSource(func() InferenceFootprint {
                infCalls++
                return InferenceFootprint{ModelBytes: 4 * gib, KVBytes: 2 * gib, Known: true}
        })

        st := g.Observe(sampleRSS(32*int64(gib), 0.5, 2*int64(gib), preflight.PressureOK))

        if !st.AvailableKnown || st.RAMTotalBytes != 32*int64(gib) {
                t.Fatalf("RAM facts not folded: %+v", st)
        }

        if st.ProcRSSBytes != 2*int64(gib) {
                t.Fatalf("process RSS not folded: %+v", st)
        }

        if st.Level != preflight.PressureOK {
                t.Fatalf("level = %q, want ok", st.Level)
        }

        if !st.CPULoadKnown || st.CPULoadPercent != 42 {
                t.Fatalf("CPU fact not folded: %+v", st)
        }

        if !st.EngineRunning || st.EngineRSSBytes != 5*gib || st.EngineModel != "qwen2-7b-q4_k_m.gguf" {
                t.Fatalf("engine facts not folded: %+v", st)
        }

        // v1.8.6: the inference workload is folded as a measured fact.
        if !st.InferenceKnown || st.InferenceModelBytes != 4*gib || st.InferenceKVBytes != 2*gib {
                t.Fatalf("inference footprint not folded: %+v", st)
        }

        if len(st.Unknowns) != 0 {
                t.Fatalf("a fully measured state must report no unknowns, got %v", st.Unknowns)
        }

        if cpuCalls != 1 {
                t.Fatalf("CPU seam called %d times per observe, want 1", cpuCalls)
        }

        if infCalls != 1 {
                t.Fatalf("inference source called %d times per observe, want 1", infCalls)
        }
}

func TestObserveReportsUnknownsExplicitly(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        // A zero-value sample: NOTHING was measurable.
        st := g.Observe(preflight.Sample{Level: preflight.PressureOK})

        if st.AvailableKnown {
                t.Fatal("an unmeasurable RAM fact must not be reported as known")
        }

        if st.CPULoadKnown {
                t.Fatal("a missing CPU seam must not be reported as known")
        }

        if len(st.Unknowns) < 2 {
                t.Fatalf("the state must name its unknowns explicitly, got %v", st.Unknowns)
        }

        // The self-model must reflect the same honesty.
        sm := g.SelfModel()
        if ram, ok := sm["ram"].(map[string]any); !ok || ram["measured"] != false {
                t.Fatalf("self-model ram must report measured=false, got %v", sm["ram"])
        }
        if cpu, ok := sm["cpu"].(map[string]any); !ok || cpu["measured"] != false {
                t.Fatalf("self-model cpu must report measured=false, got %v", sm["cpu"])
        }
}

// --- sustained pressure (time-dimension hysteresis) ---------------------------

func TestSustainedWarningRequiresDuration(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        th := DefaultThresholds()
        g := New(th, nil, nil, clk.Now)

        // Fresh warning: sustained below the threshold — watch, don't act.
        g.Observe(sample(32*int64(gib), 0.20, preflight.PressureWarning))
        env := g.Envelope()

        if env.ReduceBackground {
                t.Fatal("a fresh warning must not yet reduce background work")
        }

        if env.AdjustmentClass != AdjustNone {
                t.Fatalf("fresh warning adjustment class = %q, want none", env.AdjustmentClass)
        }

        // The SAME level sustains past the threshold — policy engages.
        clk.Step(th.SustainedWarning + time.Second)
        g.Observe(sample(32*int64(gib), 0.20, preflight.PressureWarning))
        env = g.Envelope()

        if !env.ReduceBackground || !env.ReduceContextWork {
                t.Fatal("sustained warning must reduce background and optional context work")
        }

        if env.AdjustmentClass != AdjustLive {
                t.Fatalf("sustained warning adjustment class = %q, want live", env.AdjustmentClass)
        }

        // Warning reduces work but does NOT close admission by itself —
        // memory fit still decides (the pressure gate closes at high).
        if !env.AdmitHeavyweight {
                t.Fatal("warning pressure must not close heavyweight admission outright")
        }
}

func TestSustainedTimerResetsOnLevelChange(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        g.Observe(sample(32*int64(gib), 0.20, preflight.PressureWarning))
        clk.Step(45 * time.Second)

        // The level CHANGES (improves): the sustained timer must restart.
        g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))

        if s := g.SustainedFor(); s > time.Second {
                t.Fatalf("sustained timer did not reset on level change: %v", s)
        }
}

// --- envelope policy boundaries ------------------------------------------------

func TestEnvelopePolicyMatrix(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        cases := []struct {
                name         string
                level        preflight.PressureLevel
                sustain      time.Duration
                wantAdmit    bool
                wantReduceBg bool
                wantClass    AdjustmentClass
        }{
                {"ok admits", preflight.PressureOK, 0, true, false, AdjustNone},
                {"fresh warning still admits", preflight.PressureWarning, 5 * time.Second, true, false, AdjustNone},
                {"sustained warning reduces", preflight.PressureWarning, 45 * time.Second, true, true, AdjustLive},
                {"high defers and reduces", preflight.PressureHigh, 0, false, true, AdjustLive},
                {"critical refuses everything", preflight.PressureCritical, 0, false, true, AdjustLive},
        }

        for _, tc := range cases {
                clk.Step(2 * time.Minute) // separate the sustained windows
                g.Observe(sample(32*int64(gib), 0.5, tc.level))

                // Re-observe at the same level after stepping the clock so the
                // sustained duration accumulates to tc.sustain.
                clk.Step(tc.sustain)
                g.Observe(sample(32*int64(gib), 0.5, tc.level))

                env := g.Envelope()

                if env.AdmitHeavyweight != tc.wantAdmit {
                        t.Fatalf("%s: admit = %v, want %v (reasons %v)", tc.name, env.AdmitHeavyweight, tc.wantAdmit, env.Reasons)
                }

                if env.ReduceBackground != tc.wantReduceBg {
                        t.Fatalf("%s: reduceBackground = %v, want %v", tc.name, env.ReduceBackground, tc.wantReduceBg)
                }

                if env.AdjustmentClass != tc.wantClass {
                        t.Fatalf("%s: adjustment class = %q, want %q", tc.name, env.AdjustmentClass, tc.wantClass)
                }

                if len(env.Reasons) == 0 {
                        t.Fatalf("%s: the envelope must always explain itself", tc.name)
                }
        }
}

func TestCriticalEnvelopeDropsBudget(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        g.Observe(sample(32*int64(gib), 0.05, preflight.PressureCritical))
        env := g.Envelope()

        if env.ResidentMemoryBudgetBytes != 0 {
                t.Fatalf("critical envelope must offer no resident budget, got %d", env.ResidentMemoryBudgetBytes)
        }

        if env.AdmitHeavyweight {
                t.Fatal("critical envelope must refuse heavyweight admission")
        }
}

// --- rolling signal -------------------------------------------------------------

func TestRollingSignalDecaysAndBounds(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        th := DefaultThresholds()
        g := New(th, nil, nil, clk.Now)

        // Sustained low CPU never triggers reduction.
        for i := 0; i < 10; i++ {
                g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        }
        st := g.State()

        // With no CPU seam, CPU policy must stay OFF.
        if st.CPULoadKnown {
                t.Fatal("CPU must be unknown without a seam")
        }

        // A CPU seam that reports a spike then silence: the ROLLING average
        // must smooth it — one spike never flips the envelope.
        spiky := []float64{10, 10, 10, 99, 10, 10, 10}
        idx := 0
        g2 := New(th, func() (float64, bool) {
                v := spiky[idx%len(spiky)]
                idx++
                return v, true
        }, nil, clk.Now)

        for i := 0; i < len(spiky); i++ {
                g2.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        }

        if st2 := g2.State(); st2.CPURollingAvg >= th.CPUReduceAbove {
                t.Fatalf("one spike must not dominate the rolling average: %.1f", st2.CPURollingAvg)
        }

        if env := g2.Envelope(); env.ReduceBackground {
                t.Fatal("a smoothed CPU blip must not reduce background work")
        }
}

func TestSustainedCPUHigherThanThresholdReduces(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        th := DefaultThresholds()
        g := New(th, func() (float64, bool) { return 95, true }, nil, clk.Now)

        // v1.9.3 contract: the CPU branch requires a SUSTAINED rolling
        // signal — fold cpuWarmupSamples consecutive measured samples (the
        // name says sustained; the fold count now means it).
        for i := 0; i < cpuWarmupSamples; i++ {
                g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        }
        env := g.Envelope()

        if !env.ReduceBackground || !env.ReduceToolConcurrency {
                t.Fatal("sustained measured CPU above the threshold must reduce background and tool concurrency")
        }

        if env.AdjustmentClass != AdjustLive {
                t.Fatalf("CPU-driven reduction class = %q, want live", env.AdjustmentClass)
        }

        found := false
        for _, r := range env.Reasons {
                if contains(r, "rolling CPU load") {
                        found = true
                }
        }
        if !found {
                t.Fatalf("the CPU reason must be stated, got %v", env.Reasons)
        }
}

// TestLoneCPUSpikeDoesNotFlipEnvelope pins BOTH halves of the v1.9.3 CPU
// policy contract: one high measured sample (including the FIRST sample
// ever folded, whose EWMA equals the raw value) must NOT flip the
// envelope, and a genuinely sustained high load MUST.
func TestLoneCPUSpikeDoesNotFlipEnvelope(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        th := DefaultThresholds()
        g := New(th, func() (float64, bool) { return 99, true }, nil, clk.Now)

        // ONE sample: the rolling series holds a lone observation — the
        // envelope must stay class none at ok pressure.
        g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        env := g.Envelope()

        if env.AdjustmentClass != AdjustNone {
                t.Fatalf("a lone CPU spike flipped the envelope: class = %q, want none", env.AdjustmentClass)
        }

        if env.ReduceBackground || env.ReduceToolConcurrency {
                t.Fatal("a lone CPU spike must not reduce background or tool concurrency")
        }

        // The same load SUSTAINED past the warm-up floor: policy engages.
        for i := 1; i < cpuWarmupSamples; i++ {
                g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        }
        env = g.Envelope()

        if env.AdjustmentClass != AdjustLive {
                t.Fatalf("sustained CPU load class = %q, want live", env.AdjustmentClass)
        }

        if !env.ReduceBackground || !env.ReduceToolConcurrency {
                t.Fatal("sustained CPU load must reduce background and tool concurrency")
        }
}

func contains(s, sub string) bool {
        return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
        for i := 0; i+len(sub) <= len(s); i++ {
                if s[i:i+len(sub)] == sub {
                        return i
                }
        }
        return -1
}

// --- admission control -----------------------------------------------------------

func TestAdmissionMatrix(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        // State: 32 GiB total, 50% available (16 GiB), process RSS 2 GiB.
        // Budget = 16 GiB * 0.9 - 2 GiB = 12.4 GiB.
        g.Observe(sampleRSS(32*int64(gib), 0.5, 2*int64(gib), preflight.PressureOK))

        fits := g.AdmitMemory(MemoryPlan{Workload: "model load: 8b-q4", ResidentBytes: 6 * gib})
        if !fits.Admitted {
                t.Fatalf("a plan inside the budget must be admitted: %s", fits.Why)
        }

        tooBig := g.AdmitMemory(MemoryPlan{Workload: "model load: 70b-q4", ResidentBytes: 48 * gib})
        if tooBig.Admitted {
                t.Fatal("a plan beyond the budget must be refused")
        }

        if !contains(tooBig.Why, "exceeds the current resident budget") {
                t.Fatalf("the refusal must name the budget math: %s", tooBig.Why)
        }

        // A missing resident estimate is refused — mapped file size is not
        // resident memory and the Governor never guesses.
        noPlan := g.AdmitMemory(MemoryPlan{Workload: "model load: unknown-card", ResidentBytes: 0})
        if noPlan.Admitted {
                t.Fatal("admission without a resident estimate must be refused")
        }

        // Heavyweight coarse gate at ok.
        if d := g.AdmitHeavyweight("repository index"); !d.Admitted {
                t.Fatalf("heavyweight admission at ok pressure must pass: %s", d.Why)
        }
}

func TestAdmissionAtHighPressureDefers(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        g.Observe(sample(32*int64(gib), 0.10, preflight.PressureHigh))

        d := g.AdmitMemory(MemoryPlan{Workload: "model load: 3b-q4", ResidentBytes: 2 * gib})
        if d.Admitted {
                t.Fatal("high pressure must defer even plans that would arithmetically fit")
        }

        if !contains(d.Why, "high_pressure pressure sustained") {
                t.Fatalf("the deferral must name the pressure class and duration: %s", d.Why)
        }
}

func TestUnknownRAMIsConservative(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        // The platform measured nothing: level ok, but NO RAM facts.
        g.Observe(preflight.Sample{Level: preflight.PressureOK})

        if d := g.AdmitHeavyweight("research batch"); d.Admitted {
                t.Fatal("admission with unknown RAM must be refused (conservative)")
        }

        if d := g.AdmitMemory(MemoryPlan{Workload: "model load: 8b", ResidentBytes: 6 * gib}); d.Admitted {
                t.Fatal("memory admission with unknown RAM must be refused (conservative)")
        }

        env := g.Envelope()
        if env.AdmitHeavyweight {
                t.Fatal("the envelope with unknown RAM must refuse heavyweight admission")
        }

        if env.AdjustmentClass != AdjustNextRun {
                t.Fatalf("unknown-RAM envelope class = %q, want next-run", env.AdjustmentClass)
        }
}

func TestEngineRSSIsNotSubtractedTwice(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, func() EngineFacts {
                return EngineFacts{Running: true, Model: "m.gguf", RSSBytes: 4 * gib, Known: true}
        }, clk.Now)

        // 32 GiB total, 8 GiB available (the engine's 4 GiB already shrank it),
        // process RSS 2 GiB. Budget = 8*0.9 - 2 = 5.2 GiB — the engine RSS is
        // NOT subtracted again.
        g.Observe(sampleRSS(32*int64(gib), 0.25, 2*int64(gib), preflight.PressureOK))

        want := int64(float64(8*gib)*(1-DefaultThresholds().HeadroomFraction)) - 2*gib
        env := g.Envelope()

        if env.ResidentMemoryBudgetBytes != want {
                t.Fatalf("resident budget = %d, want %d (engine RSS must not be subtracted twice)", env.ResidentMemoryBudgetBytes, want)
        }
}

// --- adjustment classes -----------------------------------------------------------

func TestAdjustmentClassesAreHonest(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        // ok → none.
        g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        if env := g.Envelope(); env.AdjustmentClass != AdjustNone {
                t.Fatalf("ok class = %q, want none", env.AdjustmentClass)
        }

        // warning, sustained past the threshold → live (background/context
        // are live authorities).
        g.Observe(sample(32*int64(gib), 0.20, preflight.PressureWarning))
        clk.Step(60 * time.Second)
        g.Observe(sample(32*int64(gib), 0.20, preflight.PressureWarning))
        env := g.Envelope()
        if env.AdjustmentClass != AdjustLive {
                t.Fatalf("sustained warning class = %q, want live", env.AdjustmentClass)
        }
        if env.ReduceContextWork && env.AdjustmentClass == AdjustNextRun {
                t.Fatal("context work reduction at live class must not be demoted")
        }
}

// --- self-model ------------------------------------------------------------------

func TestSelfModelSchemaWithProvenance(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), func() (float64, bool) { return 30, true }, func() EngineFacts {
                return EngineFacts{Running: true, Model: "m.gguf", RSSBytes: 0, Known: false}
        }, clk.Now)

        g.Observe(sample(32*int64(gib), 0.5, preflight.PressureOK))
        sm := g.SelfModel()

        for _, key := range []string{"at", "level", "ram", "process", "engine", "cpu", "activeRuns", "sustained", "observedSamples"} {
                if _, ok := sm[key]; !ok {
                        t.Fatalf("self-model missing key %q: %v", key, sm)
                }
        }

        engine, ok := sm["engine"].(map[string]any)
        if !ok || engine["running"] != true {
                t.Fatalf("self-model engine must carry the running fact: %v", sm["engine"])
        }

        if engine["rssMeasured"] != false {
                t.Fatalf("a running engine with unmeasurable RSS must say so: %v", engine)
        }

        unknowns, ok := sm["unknowns"].([]string)
        if !ok || len(unknowns) == 0 {
                t.Fatalf("engine-RSS unknown must be listed: %v", sm["unknowns"])
        }
}

// --- concurrency -------------------------------------------------------------------

func TestGovernorIsConcurrencySafe(t *testing.T) {
        clk := newFakeClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), func() (float64, bool) { return 50, true }, nil, clk.Now)

        var wg sync.WaitGroup
        levels := []preflight.PressureLevel{
                preflight.PressureOK,
                preflight.PressureWarning,
                preflight.PressureHigh,
                preflight.PressureCritical,
        }

        for i := 0; i < 8; i++ {
                wg.Add(1)
                go func(n int) {
                        defer wg.Done()
                        for j := 0; j < 50; j++ {
                                g.Observe(sample(32*int64(gib), 0.5, levels[(n+j)%len(levels)]))
                                _ = g.Envelope()
                                _ = g.AdmitHeavyweight("probe")
                                _ = g.AdmitMemory(MemoryPlan{Workload: "probe", ResidentBytes: gib})
                                _ = g.SelfModel()
                                _ = g.SustainedFor()
                        }
                }(i)
        }

        wg.Wait()
}
