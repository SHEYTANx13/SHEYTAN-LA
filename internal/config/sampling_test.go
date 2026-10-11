package config

// sampling_test.go — v1.6.1 P0: the sampling-value contract at every entry
// point (config load, normalization, validation) is proven here; the API
// patch rejection is proven in internal/api, the engine boot gate in
// internal/llm.

import (
        "math"
        "os"
        "path/filepath"
        "strings"
        "testing"
)

func TestValidateSamplingOptions(t *testing.T) {
        valid := Default().LLM
        if problems := ValidateSamplingOptions(valid); len(problems) != 0 {
                t.Fatalf("documented defaults must validate, got %v", problems)
        }

        for _, tc := range []struct {
                name  string
                mutate func(*LLMOptions)
                field string
        }{
                {"repeat penalty zero", func(o *LLMOptions) { o.RepeatPenalty = 0 }, "llm.repeatPenalty"},
                {"repeat penalty negative", func(o *LLMOptions) { o.RepeatPenalty = -1.1 }, "llm.repeatPenalty"},
                {"repeat penalty NaN", func(o *LLMOptions) { o.RepeatPenalty = math.NaN() }, "llm.repeatPenalty"},
                {"repeat penalty +Inf", func(o *LLMOptions) { o.RepeatPenalty = math.Inf(1) }, "llm.repeatPenalty"},
                {"repeat penalty -Inf", func(o *LLMOptions) { o.RepeatPenalty = math.Inf(-1) }, "llm.repeatPenalty"},
                {"temperature NaN", func(o *LLMOptions) { o.Temperature = math.NaN() }, "llm.temperature"},
                {"temperature negative", func(o *LLMOptions) { o.Temperature = -0.1 }, "llm.temperature"},
                {"topP above one", func(o *LLMOptions) { o.TopP = 1.5 }, "llm.topP"},
                {"topP negative", func(o *LLMOptions) { o.TopP = -0.2 }, "llm.topP"},
                {"topP NaN", func(o *LLMOptions) { o.TopP = math.NaN() }, "llm.topP"},
                {"minP above one", func(o *LLMOptions) { o.MinP = 2 }, "llm.minP"},
                {"numCtx zero", func(o *LLMOptions) { o.NumCtx = 0 }, "llm.numCtx"},
                {"numCtx negative", func(o *LLMOptions) { o.NumCtx = -8192 }, "llm.numCtx"},
                {"numBatch zero", func(o *LLMOptions) { o.NumBatch = 0 }, "llm.numBatch"},
                {"topK negative", func(o *LLMOptions) { o.TopK = -5 }, "llm.topK"},
                {"mirostat invalid mode", func(o *LLMOptions) { o.Mirostat = 3 }, "llm.mirostat"},
                {"presence penalty Inf", func(o *LLMOptions) { o.PresencePenalty = math.Inf(1) }, "llm.presencePenalty"},
                {"frequency penalty NaN", func(o *LLMOptions) { o.FrequencyPenalty = math.NaN() }, "llm.frequencyPenalty"},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        o := Default().LLM
                        tc.mutate(&o)

                        problems := ValidateSamplingOptions(o)
                        if len(problems) == 0 {
                                t.Fatalf("must be rejected")
                        }

                        if problems[0].Field != tc.field {
                                t.Fatalf("problem must name %s, got %s (%v)", tc.field, problems[0].Field, problems)
                        }
                        if problems[0].Fix == "" {
                                t.Fatalf("problem must carry an actionable fix: %v", problems[0])
                        }
                })
        }
}

// TestValidateSamplingOptionsAcceptsValidButUnusual pins the
// no-over-validation contract: values the engine's parser accepts are
// never reshaped, even when they are unusual.
func TestValidateSamplingOptionsAcceptsValidButUnusual(t *testing.T) {
        for _, tc := range []struct {
                name   string
                mutate func(*LLMOptions)
        }{
                {"repeat penalty 1.0 disabled", func(o *LLMOptions) { o.RepeatPenalty = 1.0 }},
                {"repeat penalty 2.5", func(o *LLMOptions) { o.RepeatPenalty = 2.5 }},
                {"temperature 0", func(o *LLMOptions) { o.Temperature = 0 }},
                {"temperature 3", func(o *LLMOptions) { o.Temperature = 3 }},
                {"topP 1", func(o *LLMOptions) { o.TopP = 1 }},
                {"minP 0 unset", func(o *LLMOptions) { o.MinP = 0 }},
                {"presence penalty negative", func(o *LLMOptions) { o.PresencePenalty = -1.5 }},
                {"frequency penalty 2", func(o *LLMOptions) { o.FrequencyPenalty = 2 }},
                {"mirostat 1", func(o *LLMOptions) { o.Mirostat = 1 }},
                {"mirostat 2", func(o *LLMOptions) { o.Mirostat = 2 }},
                {"mirostat tau 5", func(o *LLMOptions) { o.MirostatTau = 5.0; o.Mirostat = 2 }},
                {"repeat last n 0 unset", func(o *LLMOptions) { o.RepeatLastN = 0 }},
                {"topK 0 disabled", func(o *LLMOptions) { o.TopK = 0 }},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        o := Default().LLM
                        tc.mutate(&o)

                        if problems := ValidateSamplingOptions(o); len(problems) > 0 {
                                t.Fatalf("valid value must not be rejected: %v", problems)
                        }
                })
        }
}

// TestNormalizeSamplingOptionsRepairsInvalidValues proves the safe-repair
// half: every parser-invalid value resets to its documented default with
// one report line, and valid values survive verbatim.
func TestNormalizeSamplingOptionsRepairsInvalidValues(t *testing.T) {
        o := Default().LLM
        o.RepeatPenalty = 0 // the in-the-wild defect value
        o.Temperature = math.NaN()
        o.TopP = 1.5

        notes := NormalizeSamplingOptions(&o)

        if len(notes) != 3 {
                t.Fatalf("expected one note per repair, got %d: %v", len(notes), notes)
        }

        def := Default().LLM
        if o.RepeatPenalty != def.RepeatPenalty {
                t.Fatalf("repeatPenalty must reset to %v, got %v", def.RepeatPenalty, o.RepeatPenalty)
        }
        // NaN != anything (including the reset default), so a surviving NaN
        // fails this comparison exactly like any other wrong value.
        if o.Temperature != def.Temperature {
                t.Fatalf("temperature must reset to %v, got %v", def.Temperature, o.Temperature)
        }
        if o.TopP != def.TopP {
                t.Fatalf("topP must reset to %v, got %v", def.TopP, o.TopP)
        }

        for _, note := range notes {
                if !strings.Contains(note, "rejected") {
                        t.Fatalf("each note must say what was rejected and why: %q", note)
                }
        }

        // After repair the config must validate.
        if problems := ValidateSamplingOptions(o); len(problems) > 0 {
                t.Fatalf("post-normalization config must validate, got %v", problems)
        }
}

// TestNormalizeSamplingOptionsPreservesValidValues: no note, no change.
func TestNormalizeSamplingOptionsPreservesValidValues(t *testing.T) {
        o := Default().LLM
        o.RepeatPenalty = 1.03 // unusual-but-valid user setting

        notes := NormalizeSamplingOptions(&o)

        if len(notes) != 0 {
                t.Fatalf("valid settings must never be touched, got notes %v", notes)
        }
        if o.RepeatPenalty != 1.03 {
                t.Fatalf("valid setting must survive verbatim, got %v", o.RepeatPenalty)
        }
}

// TestLoadRepairsInvalidSamplingFromDisk: the v1.6.0-era config.json that
// carried repeatPenalty 0 boots v1.6.1 with the documented default — and
// says so.
func TestLoadRepairsInvalidSamplingFromDisk(t *testing.T) {
        dir := t.TempDir()
        t.Setenv("SHEYTAN_DATA_DIR", dir)

        path := filepath.Join(dir, "config.json")

        broken := `{
                "dataDir": "` + strings.ReplaceAll(dir, `\`, `\\`) + `",
                "llm": {"repeatPenalty": 0, "temperature": -1, "topP": 2}
        }`
        if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
                t.Fatal(err)
        }

        cfg, err := Load(path)
        if err != nil {
                t.Fatalf("broken sampling values must not fail Load (safe repair): %v", err)
        }

        def := Default().LLM
        if cfg.LLM.RepeatPenalty != def.RepeatPenalty {
                t.Fatalf("repeatPenalty must be repaired to %v, got %v", def.RepeatPenalty, cfg.LLM.RepeatPenalty)
        }
        if cfg.LLM.Temperature != def.Temperature {
                t.Fatalf("temperature must be repaired to %v, got %v", def.Temperature, cfg.LLM.Temperature)
        }
        if cfg.LLM.TopP != def.TopP {
                t.Fatalf("topP must be repaired to %v, got %v", def.TopP, cfg.LLM.TopP)
        }

        notes := TakeSamplingNotes(cfg)
        // v1.6.2: three repairs plus ONE persistence report (the repaired
        // values are written back so the next launch needs no repair).
        if len(notes) != 4 {
                t.Fatalf("loader must report each repair plus the persistence outcome, got %v", notes)
        }
        if !strings.Contains(notes[len(notes)-1], "persisted") {
                t.Fatalf("the final note must report the v1.6.2 persistence, got %v", notes)
        }
        if TakeSamplingNotes(cfg) != nil {
                t.Fatal("notes must drain exactly once")
        }

        // v1.6.2: the corruption must not require the same repair on the
        // NEXT launch — reload and expect zero repair notes.
        cfg2, err := Load(path)
        if err != nil {
                t.Fatalf("reload after persistence: %v", err)
        }
        if notes2 := TakeSamplingNotes(cfg2); len(notes2) != 0 {
                t.Fatalf("a persisted repair must not repeat on reload, got %v", notes2)
        }
        if cfg2.LLM.RepeatPenalty != def.RepeatPenalty || cfg2.LLM.Temperature != def.Temperature || cfg2.LLM.TopP != def.TopP {
                t.Fatalf("reloaded values must stay repaired: %+v", cfg2.LLM)
        }
}

// TestSamplingProblemsErrorRendering: the API rejection message carries
// every problem with its fix.
func TestSamplingProblemsErrorRendering(t *testing.T) {
        o := Default().LLM
        o.RepeatPenalty = 0

        err := &SamplingProblemsError{Problems: ValidateSamplingOptions(o)}
        msg := err.Error()

        for _, want := range []string{"invalid sampling configuration", "llm.repeatPenalty=0", "greater than 0"} {
                if !strings.Contains(msg, want) {
                        t.Fatalf("message must contain %q, got:\n%s", want, msg)
                }
        }
}
