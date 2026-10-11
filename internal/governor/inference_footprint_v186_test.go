package governor

// inference_footprint_v186_test.go — v1.8.6 Phase 2: the Governor accounts
// for the CURRENT inference workload (§6).
//
// THE CONTRACT:
//
//   - the footprint folds into the state ONLY when the source reports it
//     measured (unknown stays unknown — no fabricated weights/KV);
//   - the envelope ACCOUNTS for a footprint that consumes the resident
//     budget (background reduced, reason stated — an existing honest
//     adjustment class);
//   - an unknown or comfortably-fitting footprint changes nothing;
//   - AdmitMemory keeps refusing plans that do not fit the measured
//     envelope (RAM admission over the model+KV plan).

import (
        "strings"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

func footprintGovernor(t *testing.T, inf InferenceSource, availFrac float64) *Governor {
        t.Helper()

        clk := newFakeClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)

        if inf != nil {
                g.SetInferenceSource(inf)
        }

        // 32 GiB machine, generous availability, OK pressure.
        g.Observe(sampleRSS(32*int64(gib), availFrac, 2*int64(gib), preflight.PressureOK))

        return g
}

func TestInferenceFootprintUnknownStaysUnknown(t *testing.T) {
        g := footprintGovernor(t, nil, 0.9)

        st := g.State()
        if st.InferenceKnown {
                t.Fatal("no inference source wired — the footprint must stay unknown, never fabricated")
        }

        if st.InferenceModelBytes != 0 || st.InferenceKVBytes != 0 {
                t.Fatalf("unknown footprint carried values: %+v", st)
        }

        found := false
        for _, u := range st.Unknowns {
                if strings.Contains(u, "inference") {
                        found = true
                }
        }
        if !found {
                t.Fatalf("the unknown footprint must be named, got %v", st.Unknowns)
        }
}

func TestInferenceFootprintFoldsWhenMeasured(t *testing.T) {
        g := footprintGovernor(t, func() InferenceFootprint {
                return InferenceFootprint{ModelBytes: 4 * gib, KVBytes: 2 * gib, Known: true}
        }, 0.9)

        st := g.State()
        if !st.InferenceKnown || st.InferenceModelBytes != 4*gib || st.InferenceKVBytes != 2*gib {
                t.Fatalf("measured footprint not folded: %+v", st)
        }
}

func TestEnvelopeAccountsForInferenceFootprint(t *testing.T) {
        // A footprint (weights+KV) that plus the process RSS consumes the
        // resident budget: 32 GiB * 0.9 available = 28.8 GiB; budget =
        // 28.8 GiB * 0.9 (headroom) - 2 GiB (RSS) ≈ 23.9 GiB; footprint
        // 24 GiB ≥ budget → the envelope must account for it.
        g := footprintGovernor(t, func() InferenceFootprint {
                return InferenceFootprint{ModelBytes: 20 * gib, KVBytes: 4 * gib, Known: true}
        }, 0.9)

        env := g.Envelope()
        if !env.ReduceBackground {
                t.Fatalf("a footprint that consumes the resident budget must reduce background work: %+v", env)
        }

        found := false
        for _, r := range env.Reasons {
                if strings.Contains(r, "inference footprint") {
                        found = true
                }
        }
        if !found {
                t.Fatalf("the reason must name the inference footprint, got %v", env.Reasons)
        }
}

func TestEnvelopeIgnoresUnknownFootprint(t *testing.T) {
        // The SAME memory pressure but an UNKNOWN footprint: no inference
        // branch fires (unknown never drives policy).
        g := footprintGovernor(t, nil, 0.9)

        env := g.Envelope()
        for _, r := range env.Reasons {
                if strings.Contains(r, "inference footprint") {
                        t.Fatalf("an unknown footprint must never fire the inference reason: %v", env.Reasons)
                }
        }
}

func TestEnvelopeIgnoresComfortableFootprint(t *testing.T) {
        g := footprintGovernor(t, func() InferenceFootprint {
                return InferenceFootprint{ModelBytes: 1 * gib, KVBytes: 512 << 20, Known: true}
        }, 0.9)

        env := g.Envelope()
        for _, r := range env.Reasons {
                if strings.Contains(r, "inference footprint") {
                        t.Fatalf("a comfortably-fitting footprint must not fire the inference reason: %v", env.Reasons)
                }
        }
}

func TestAdmitMemoryRAMAdmissionOverModelPlan(t *testing.T) {
        // 32 GiB, 25% available (8 GiB), RSS 2 GiB → budget ≈ 8*0.9 - 2 ≈ 5.2 GiB.
        g := footprintGovernor(t, nil, 0.25)

        // A 4 GiB resident plan fits; a 6 GiB plan is deferred with the honest
        // reason. (Sustained < SustainedHeavy, pressure OK → the pressure gate
        // passes and the BUDGET is what decides.)
        if d := g.AdmitMemory(MemoryPlan{Workload: "model load: small", ResidentBytes: 4 * gib}); !d.Admitted {
                t.Fatalf("a fitting plan must be admitted, got: %s", d.Why)
        }

        if d := g.AdmitMemory(MemoryPlan{Workload: "model load: big", ResidentBytes: 6 * gib}); d.Admitted {
                t.Fatal("an oversized plan must be deferred")
        } else if !strings.Contains(d.Why, "exceeds the current resident budget") {
                t.Fatalf("the deferral must name the budget arithmetic: %s", d.Why)
        }
}

func TestSelfModelCarriesInferenceFootprint(t *testing.T) {
        g := footprintGovernor(t, func() InferenceFootprint {
                return InferenceFootprint{ModelBytes: 4 * gib, KVBytes: 2 * gib, Known: true}
        }, 0.9)

        sm := g.SelfModel()

        inf, ok := sm["inference"].(map[string]any)
        if !ok || inf["measured"] != true {
                t.Fatalf("self-model inference block missing/mismeasured: %v", sm["inference"])
        }

        if inf["modelBytes"] != int64(4*gib) || inf["kvBytes"] != int64(2*gib) {
                t.Fatalf("self-model inference values wrong: %v", inf)
        }

        // Unknown variant.
        g2 := footprintGovernor(t, nil, 0.9)
        sm2 := g2.SelfModel()
        if inf2, ok := sm2["inference"].(map[string]any); !ok || inf2["measured"] != false {
                t.Fatalf("unknown footprint must render measured=false: %v", sm2["inference"])
        }
}

func TestInferenceFootprintSustainedClockAdvances(t *testing.T) {
        // The footprint policy must not disturb the sustained-level clock
        // (the time-dimension hysteresis stays owned by the level tracker).
        clk := newFakeClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
        g := New(DefaultThresholds(), nil, nil, clk.Now)
        g.SetInferenceSource(func() InferenceFootprint {
                return InferenceFootprint{ModelBytes: 20 * gib, KVBytes: 4 * gib, Known: true}
        })

        g.Observe(sampleRSS(32*int64(gib), 0.9, 2*int64(gib), preflight.PressureWarning))
        clk.Step(45 * time.Second)
        st := g.Observe(sampleRSS(32*int64(gib), 0.9, 2*int64(gib), preflight.PressureWarning))

        if st.Sustained < 45*time.Second {
                t.Fatalf("sustained tracking disturbed: %s", st.Sustained)
        }
}
