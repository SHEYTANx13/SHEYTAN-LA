package llm

// capability_test.go — Phase 7 regression tests for the llama.cpp launch
// contract. The historical failure this file exists to prevent:
//
//      --flash-attn --cache-reuse        (an option consumed as a value)
//
// must NEVER be produced, for ANY engine version, while every valid
// combination must keep working.

import (
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func newCapConfig() *config.Config {
        cfg := config.Default()
        cfg.DataDir = "/tmp/sheyTan-test-caps"
        cfg.Provider = "local"
        cfg.FlashAttention = true
        cfg.CacheReuse = 32
        return cfg
}

// TestSpeedArgsNeverProducesFlashAttnCacheReuse is THE historical
// regression test: with value-form engines, a `--flash-attn` token must be
// followed by on|off|auto — never by another option.
func TestSpeedArgsNeverProducesFlashAttnCacheReuse(t *testing.T) {
        for _, caps := range []*EngineCaps{
                defaultCapsForTag("b10642"), // legacy: bare flag
                defaultCapsForTag("b10936"), // modern: on|off|auto
                nil,                         // nil profile must stay safe
        } {
                cfg := newCapConfig()
                args := SpeedArgsWithCaps(cfg, caps)

                for i, a := range args {
                        if a != "--flash-attn" {
                                continue
                        }
                        if i+1 >= len(args) {
                                t.Fatalf("dangling --flash-attn in %v", args)
                        }
                        next := args[i+1]
                        if caps != nil && caps.FlashAttnValue {
                                if next != "on" && next != "off" && next != "auto" {
                                        t.Fatalf("value-form profile produced --flash-attn %q — the historical regression — in %v", next, args)
                                }
                        } else {
                                if !strings.HasPrefix(next, "--") {
                                        t.Fatalf("flag-form profile produced a value after --flash-attn in %v", args)
                                }
                        }
                }

                // The literal historical pair is a regression ONLY on a
                // value-form engine (on legacy engines a flag followed by an
                // option is valid).
                joined := strings.Join(args, " ")
                if caps != nil && caps.FlashAttnValue &&
                        strings.Contains(joined, "--flash-attn --cache-reuse") {
                        t.Fatalf("historical regression emitted: %v", args)
                }
        }
}

// TestSpeedArgsValidModernCombination: the valid modern combination
// (--flash-attn on + --cache-reuse 256) must be produced for a
// value-form profile with cache reuse configured.
func TestSpeedArgsValidModernCombination(t *testing.T) {
        cfg := newCapConfig()
        cfg.CacheReuse = 256

        caps := defaultCapsForTag("b10936")
        args := SpeedArgsWithCaps(cfg, caps)

        joined := " " + strings.Join(args, " ") + " "
        if !strings.Contains(joined, " --flash-attn on ") {
                t.Fatalf("expected --flash-attn on in %v", args)
        }
        if !strings.Contains(joined, " --cache-reuse 256 ") {
                t.Fatalf("expected --cache-reuse 256 in %v", args)
        }

        if problems := argProblems(args, caps); len(problems) > 0 {
                t.Fatalf("valid modern combination failed validation: %v", problems)
        }
}

// TestSpeedArgsAutoWithoutCacheReuse: --flash-attn auto semantics are
// covered by an explicit `off` for a disabled toggle; with cache reuse
// disabled (0) no --cache-reuse may appear.
func TestSpeedArgsFlashAttnWithoutCacheReuse(t *testing.T) {
        cfg := newCapConfig()
        cfg.CacheReuse = 0         // disabled
        cfg.FlashAttention = false // explicit off — value-form engines must say so

        caps := defaultCapsForTag("b10936")
        args := SpeedArgsWithCaps(cfg, caps)

        joined := " " + strings.Join(args, " ") + " "
        if strings.Contains(joined, "--cache-reuse") {
                t.Fatalf("cache reuse disabled but flag emitted: %v", args)
        }
        if !strings.Contains(joined, " --flash-attn off ") {
                t.Fatalf("disabled flash attention must be explicit off on value-form engines: %v", args)
        }
        if problems := argProblems(args, caps); len(problems) > 0 {
                t.Fatalf("validation problems: %v", problems)
        }
}

// TestSpeedArgsLegacyFlagForm: old engines get the bare flag and never a
// value pair. v1.7.2 evidence correction: the REAL b10642 binary already
// requires the on|off|auto value form (probed 2026-09), so the legacy
// flag-form profile is now represented by a pre-tri-state tag.
func TestSpeedArgsLegacyFlagForm(t *testing.T) {
        cfg := newCapConfig()

        caps := defaultCapsForTag("b4000")
        args := SpeedArgsWithCaps(cfg, caps)

        joined := " " + strings.Join(args, " ") + " "
        if !strings.Contains(joined, " --flash-attn ") {
                t.Fatalf("expected bare --flash-attn flag for legacy engine: %v", args)
        }
        if strings.Contains(joined, "--flash-attn on") {
                t.Fatalf("legacy engine must not receive a value form: %v", args)
        }
        if problems := argProblems(args, caps); len(problems) > 0 {
                t.Fatalf("legacy profile failed validation: %v", problems)
        }
}

// TestArgProblemsRejectsHistoricalRegression: the validator must catch the
// malformed pair even if some future code path regressed.
func TestArgProblemsRejectsHistoricalRegression(t *testing.T) {
        caps := defaultCapsForTag("b10936")

        bad := []string{"--model", "m.gguf", "--flash-attn", "--cache-reuse", "32"}
        problems := argProblems(bad, caps)
        if len(problems) == 0 {
                t.Fatal("validator accepted --flash-attn --cache-reuse — it must reject the historical regression")
        }
        for _, p := range problems {
                if strings.Contains(p, "on|off|auto") {
                        return // the precise rejection exists
                }
        }
        t.Fatalf("rejection lacks the on|off|auto explanation: %v", problems)
}

// TestArgProblemsRejectsValueOnLegacy: the mirror case — `--flash-attn on`
// sent to a legacy flag-form engine (pre-tri-state tag; see the v1.7.2
// evidence correction in TestSpeedArgsLegacyFlagForm).
func TestArgProblemsRejectsValueOnLegacy(t *testing.T) {
        caps := defaultCapsForTag("b4000")

        bad := []string{"--model", "m.gguf", "--flash-attn", "on"}
        if problems := argProblems(bad, caps); len(problems) == 0 {
                t.Fatal("validator accepted a value after --flash-attn on a legacy profile")
        }
}

// TestArgProblemsAcceptsWellFormedProfiles: every option SHEYTAN emits must
// validate cleanly on a matching profile.
func TestArgProblemsAcceptsWellFormedProfiles(t *testing.T) {
        for _, tag := range []string{"b10642", "b10936"} {
                cfg := newCapConfig()
                cfg.CacheReuse = 256
                cfg.Mlock = true
                cfg.KVCacheQuant = "q8_0"

                caps := defaultCapsForTag(tag)
                args := SpeedArgsWithCaps(cfg, caps)

                if problems := argProblems(args, caps); len(problems) > 0 {
                        t.Fatalf("tag %s: clean profile rejected: %v", tag, problems)
                }
        }
}

// TestClassifyStartupFailureHistoricalError: the EXACT engine error from
// the reproduction log must classify as an option-layout failure naming
// --flash-attn.
func TestClassifyStartupFailureHistoricalError(t *testing.T) {
        text := `error while handling argument "--flash-attn":
unknown value for --flash-attn: '--cache-reuse'`

        sf := ClassifyStartupFailure(text)
        if sf == nil {
                t.Fatal("no classification")
        }
        if sf.Kind != FailOptionLayout {
                t.Fatalf("kind = %s, want %s", sf.Kind, FailOptionLayout)
        }
        if sf.Option != "flash-attn" {
                t.Fatalf("option = %q, want flash-attn", sf.Option)
        }
}

// TestClassifyStartupFailureLegacyInvalidArgument: the legacy mirror error
// (`error: invalid argument: on`) must also classify as option-layout.
func TestClassifyStartupFailureLegacyInvalidArgument(t *testing.T) {
        sf := ClassifyStartupFailure("error: invalid argument: on")
        if sf == nil || sf.Kind != FailOptionLayout {
                t.Fatalf("got %+v, want option-layout failure", sf)
        }
}

// TestClassifyStartupFailureOtherKinds: model-load, architecture and
// environment failures must NOT classify as repairable option problems.
func TestClassifyStartupFailureOtherKinds(t *testing.T) {
        cases := []struct {
                text string
                want StartupFailureKind
        }{
                {"llama_init_from_model: failed to load model", FailModelLoad},
                {"error: failed to allocate KV cache", FailModelLoad},
                {"unknown model architecture: 'weird-net'", FailModelArch},
                {"error while loading model: unknown architecture", FailModelArch},
                {"llama.cpp could not start: a required Windows DLL is missing. 0xc0000135", FailEnvironment},
                {"unknown argument: --definitely-made-up", FailUnknownOption},
        }
        for _, c := range cases {
                sf := ClassifyStartupFailure(c.text)
                if sf == nil || sf.Kind != c.want {
                        t.Fatalf("%q → %+v, want %s", c.text, sf, c.want)
                }
        }
}

// TestRepairCapsForSurgicalOnly: a layout failure repairs ONLY the
// flash-attn layout; an unknown --cache-reuse drops ONLY cache-reuse.
func TestRepairCapsForSurgicalOnly(t *testing.T) {
        caps := defaultCapsForTag("b10642")

        // Layout flip.
        sf := &StartupFailure{Kind: FailOptionLayout, Option: "flash-attn"}
        repaired := repairCapsFor(caps, sf, nil)
        if repaired == nil {
                t.Fatal("layout failure must be repairable")
        }
        if repaired.FlashAttnValue == caps.FlashAttnValue {
                t.Fatal("flash-attn layout was not flipped")
        }
        if repaired.CacheReuse != caps.CacheReuse || repaired.NoWebUI != caps.NoWebUI ||
                repaired.Jinja != caps.Jinja {
                t.Fatal("repair touched options beyond flash-attn")
        }

        // Unknown option drop.
        sf2 := &StartupFailure{Kind: FailUnknownOption, Option: "cache-reuse"}
        repaired2 := repairCapsFor(caps, sf2, nil)
        if repaired2 == nil {
                t.Fatal("unknown cache-reuse must be repairable")
        }
        if repaired2.CacheReuse {
                t.Fatal("cache-reuse not dropped")
        }
        if repaired2.FlashAttnValue != caps.FlashAttnValue {
                t.Fatal("repair touched flash-attn layout while dropping cache-reuse")
        }

        // Real model failures are never repairable.
        sf3 := &StartupFailure{Kind: FailModelLoad}
        if repairCapsFor(caps, sf3, nil) != nil {
                t.Fatal("model-load failure must not produce a capability repair")
        }
}

// TestEngineStartRepairsHistoricalRegression is the END-TO-END acceptance:
// against a strict-new-args engine (the b10936 contract) the launcher must
// fail → classify → repair ONLY the flash-attn layout → retry → reach
// READY with `--flash-attn on` and `--cache-reuse 32` in the actual argv,
// then persist the verified profile. No compatibility-mode fallback.
func TestEngineStartRepairsHistoricalRegression(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "strict-new-args")
        cfg.EngineCompat = 0 // full speed profile — the repair must NOT walk the ladder
        cfg.FlashAttention = true
        cfg.CacheReuse = 32

        argsOut := filepath.Join(t.TempDir(), "launched-args.txt")
        t.Setenv("SHEYTAN_FAKE_ARGS_OUT", argsOut)

        s := NewLlamaServer(config.NewSource(cfg))

        // Deterministic subprocess ownership: every started engine must be
        // stopped before the test ends. The fake engine IS this test
        // binary — a leaked child locks llm.test(.exe) and survives as an
        // orphan (on Windows: "unlinkat llm.test.exe: Access is denied").
        t.Cleanup(func() { _ = s.Stop() })

        if err := s.Start(); err != nil {
                t.Fatalf("engine start with repair: %v", err)
        }
        if s.State() != StateReady {
                t.Fatalf("state = %s, want ready", s.State())
        }

        // The actual launched args must contain the CORRECTED pair.
        data, err := os.ReadFile(argsOut)
        if err != nil {
                t.Fatalf("launched args file: %v", err)
        }
        argv := string(data)

        if !strings.Contains(argv, "--flash-attn\non") {
                t.Fatalf("engine never received `--flash-attn on`; argv:\n%s", argv)
        }
        if !strings.Contains(argv, "--cache-reuse\n32") {
                t.Fatalf("engine never received `--cache-reuse 32`; argv:\n%s", argv)
        }
        if strings.Contains(argv, "--flash-attn\n--cache-reuse") {
                t.Fatalf("historical malformed pair reached the engine:\n%s", argv)
        }

        // The verified capability profile must be persisted.
        caps := s.Caps()
        if caps == nil || !caps.FlashAttnValue {
                t.Fatalf("caps = %+v, want FlashAttnValue=true after verified repair", caps)
        }
        persisted := loadEngineCaps(cfg, caps.Tag)
        if persisted == nil || !persisted.FlashAttnValue {
                t.Fatalf("verified profile not persisted: %+v", persisted)
        }
        if persisted.VerifiedAt == "" {
                t.Fatal("persisted profile lacks the verified timestamp")
        }
}

// TestEngineStartLegacyProfileStaysFlagForm: with a legacy strict engine,
// the bare-flag profile must start cleanly without any repair.
func TestEngineStartLegacyProfileStaysFlagForm(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "strict-legacy-args")
        cfg.EngineCompat = 0
        cfg.FlashAttention = true
        cfg.CacheReuse = 32

        argsOut := filepath.Join(t.TempDir(), "launched-args.txt")
        t.Setenv("SHEYTAN_FAKE_ARGS_OUT", argsOut)

        s := NewLlamaServer(config.NewSource(cfg))

        // Deterministic subprocess ownership (see the repair test above).
        t.Cleanup(func() { _ = s.Stop() })

        if err := s.Start(); err != nil {
                t.Fatalf("legacy start: %v", err)
        }

        data, _ := os.ReadFile(argsOut)
        argv := string(data)
        if !strings.Contains(argv, "\n--flash-attn\n--cache-reuse") {
                t.Fatalf("legacy engine should receive bare flag then cache-reuse; argv:\n%s", argv)
        }
}

// TestStartupVerificationRecordsModelAndContext: after a verified start the
// state machine must have verified the served model and the context
// capability (the fake engine serves /v1/models and /props).
func TestStartupVerificationRecordsModelAndContext(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")
        cfg.EngineCompat = 0

        s := NewLlamaServer(config.NewSource(cfg))

        // Deterministic subprocess ownership (see the repair test above).
        t.Cleanup(func() { _ = s.Stop() })

        if err := s.Start(); err != nil {
                t.Fatalf("start: %v", err)
        }

        if got := s.VerifiedModel(); got != "fake-model.gguf" {
                t.Fatalf("verified model = %q, want fake-model.gguf", got)
        }
        if got := s.VerifiedContext(); got != 16384 {
                t.Fatalf("verified context = %d, want 16384", got)
        }
}

// TestDetectEngineCapsHelpParsing: the --help parser must detect the
// on|off|auto contract from realistic help output.
func TestDetectEngineCapsHelpParsing(t *testing.T) {
        got := helpFlashAttnTakesValue(`  -fa, --flash-attn [on|off|auto]    use FlashAttention`)
        if !got {
                t.Fatal("value form not detected from help text")
        }
        got = helpFlashAttnTakesValue(`  -fa, --flash-attn                  use FlashAttention`)
        if got {
                t.Fatal("flag form misdetected as value form")
        }
}
