package llm

// kv_contract_v172_test.go — v1.7.2 (P0): the llama.cpp KV-cache CLI
// contract, pinned against REAL upstream engine evidence.
//
// The v1.7.1 launcher emitted `--cache-type-kv q8_0` unconditionally. The
// real b11205 engine (and, measured afterwards, the real b10642 binary
// too) rejects that option outright:
//
//	$ llama-server --cache-type-kv q8_0 --model /nonexistent.gguf
//	error: invalid argument: --cache-type-kv
//
// while the split layout is the accepted modern form:
//
//	$ llama-server --cache-type-k q8_0 --cache-type-v q8_0 --model …
//	(accepted — proceeds to model load)
//
// The fixtures in testdata/ are the VERBATIM `llama-server --help` outputs
// of the real released binaries (llama-b10642-bin-ubuntu-x64.tar.gz and
// llama-b11205-bin-ubuntu-x64.tar.gz, downloaded and probed 2026-09), so
// the parse tests exercise the exact production help format — line
// prefixes ("0.00.000.241 I srv  llama_server: initializing …"), the
// aligned option table, the "-ctk,  --cache-type-k TYPE" spelling, and
// the "--cache-type-k-draft" family that shares the "--cache-type-k"
// PREFIX (the boundary-matching hazard).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realHelpFixture loads a real engine help fixture.
func realHelpFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

// --- help parsing: the three KV layouts ---------------------------------------

// TestKVContractRealB11205HelpParsesSplitLayout proves the REAL b11205
// help output yields the split KV layout and never the shared form.
func TestKVContractRealB11205HelpParsesSplitLayout(t *testing.T) {
	help := realHelpFixture(t, "help-b11205-real.txt")

	caps := &EngineCaps{Source: "help-parse"}
	caps.CacheTypeK = helpMentionsOption(help, "--cache-type-k")
	caps.CacheTypeV = helpMentionsOption(help, "--cache-type-v")
	caps.CacheTypeKVShared = helpMentionsOption(help, "--cache-type-kv")

	if !caps.CacheTypeK || !caps.CacheTypeV {
		t.Fatalf("real b11205 help must report the split layout: k=%v v=%v", caps.CacheTypeK, caps.CacheTypeV)
	}
	if caps.CacheTypeKVShared {
		t.Fatal("real b11205 help must NOT report --cache-type-kv — the boundary matcher has a prefix bug")
	}

	// The "--cache-type-k-draft" family must not confuse the matcher.
	if !helpMentionsOption(help, "--cache-type-k-draft") {
		t.Fatal("sanity: the draft option exists in the real help and must be found")
	}
}

// TestKVContractLegacyHelpParsesSharedLayout proves a build whose own help
// reports ONLY the legacy shared form gets the shared layout.
func TestKVContractLegacyHelpParsesSharedLayout(t *testing.T) {
	legacy := `----- common params -----

-h,    --help, --usage                  print usage and exit
-ct,   --cache-type-kv TYPE             KV cache data type (legacy shared form)
                                        allowed values: f32, f16, q8_0, q4_0
-fa,   --flash-attn                     Flash Attention (legacy bare flag)
-cr,   --cache-reuse N                  context shift
`

	if !helpMentionsOption(legacy, "--cache-type-kv") {
		t.Fatal("legacy help must report --cache-type-kv")
	}
	if helpMentionsOption(legacy, "--cache-type-k") {
		t.Fatal("legacy help must NOT report the split K option (prefix bug: --cache-type-kv contains it)")
	}
	if helpMentionsOption(legacy, "--cache-type-v") {
		t.Fatal("legacy help must NOT report the split V option")
	}
}

// TestKVContractUnsupportedEmitsNothing proves a build with no KV option
// emits NOTHING for a configured quant (fail-closed, never a guess).
func TestKVContractUnsupportedEmitsNothing(t *testing.T) {
	cfg := newCapConfig()
	cfg.KVCacheQuant = "q8_0"

	caps := &EngineCaps{Tag: "weird", CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2}
	args := SpeedArgsWithCaps(cfg, caps)

	for _, a := range args {
		if strings.HasPrefix(a, "--cache-type") {
			t.Fatalf("unsupported KV layout must emit nothing, got %v", args)
		}
	}
	if problems := argProblems(args, caps); len(problems) > 0 {
		t.Fatalf("clean profile rejected: %v", problems)
	}
}

// --- emission: the exact launch contract -------------------------------------

// TestKVContractSplitEmission: configured q8_0 on a modern profile emits
// BOTH split options.
func TestKVContractSplitEmission(t *testing.T) {
	cfg := newCapConfig()
	cfg.KVCacheQuant = "q8_0"

	caps := defaultCapsForTag("b11205")
	args := SpeedArgsWithCaps(cfg, caps)

	joined := " " + strings.Join(args, " ") + " "
	if !strings.Contains(joined, "--cache-type-k q8_0") {
		t.Fatalf("modern profile must emit --cache-type-k q8_0: %v", args)
	}
	if !strings.Contains(joined, "--cache-type-v q8_0") {
		t.Fatalf("modern profile must emit --cache-type-v q8_0: %v", args)
	}
	if strings.Contains(joined, "--cache-type-kv") {
		t.Fatalf("modern engine must NEVER receive --cache-type-kv: %v", args)
	}
	if problems := argProblems(args, caps); len(problems) > 0 {
		t.Fatalf("split profile failed validation: %v", problems)
	}
}

// TestKVContractSharedEmission: a help-verified shared layout emits exactly
// the shared option.
func TestKVContractSharedEmission(t *testing.T) {
	cfg := newCapConfig()
	cfg.KVCacheQuant = "q8_0"

	caps := &EngineCaps{
		Tag: "legacy", CacheTypeKVShared: true,
		CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2,
	}
	args := SpeedArgsWithCaps(cfg, caps)

	joined := " " + strings.Join(args, " ") + " "
	if !strings.Contains(joined, "--cache-type-kv q8_0") {
		t.Fatalf("legacy profile must emit --cache-type-kv q8_0: %v", args)
	}
	if strings.Contains(joined, "--cache-type-k ") || strings.Contains(joined, "--cache-type-v ") {
		t.Fatalf("legacy profile must not emit the split form: %v", args)
	}
	if problems := argProblems(args, caps); len(problems) > 0 {
		t.Fatalf("shared profile failed validation: %v", problems)
	}
}

// TestKVContractB11205Regression is THE regression for the reported
// failure: the full-speed profile for a modern engine must validate cleanly
// and contain no --cache-type-kv — the launch that produced
// "error: invalid argument: --cache-type-kv" can never be spawned again.
func TestKVContractB11205Regression(t *testing.T) {
	cfg := newCapConfig()
	cfg.CacheReuse = 256
	cfg.KVCacheQuant = "q8_0"
	cfg.Mlock = true

	caps := defaultCapsForTag("b11205")

	// The level-0 (full-speed) Speed Pack for the modern engine.
	args := SpeedArgsWithCaps(cfg, caps)
	joined := " " + strings.Join(args, " ") + " "

	if strings.Contains(joined, "--cache-type-kv") {
		t.Fatalf("b11205 regression: --cache-type-kv emitted for a modern engine: %v", args)
	}
	if strings.Contains(joined, "--mlock ") || strings.Contains(joined, " --mlock") {
		t.Fatalf("b11205 regression: --mlock emitted for a modern engine (removed upstream): %v", args)
	}
	if !strings.Contains(joined, "--load-mode mlock") {
		t.Fatalf("b11205: memory pinning must use --load-mode mlock: %v", args)
	}
	if problems := argProblems(args, caps); len(problems) > 0 {
		t.Fatalf("the full-speed profile must reach compatibility 0 cleanly: %v", problems)
	}
}

// --- validation: values and combinations -------------------------------------

// TestKVContractValueVocabularyMirrorsEngine: the accepted quant vocabulary
// is EXACTLY the engine's own list (lowercase, exact match).
func TestKVContractValueVocabularyMirrorsEngine(t *testing.T) {
	caps := defaultCapsForTag("b11205")

	accepted := []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}
	for _, v := range accepted {
		if problems := argProblems([]string{"--cache-type-k", v}, caps); len(problems) > 0 {
			t.Errorf("engine-accepted value %q rejected: %v", v, problems)
		}
	}

	rejected := []string{"q3_k", "Q8_0", "int8", "", "auto", "0", "--cache-type-v"}
	for _, v := range rejected {
		args := []string{"--cache-type-k"}
		if v != "" {
			args = append(args, v)
		}
		if problems := argProblems(args, caps); len(problems) == 0 {
			t.Errorf("engine-rejected value %q accepted by validation", v)
		}
	}
}

// TestKVContractFlagCannotConsumeFlag: a KV option immediately followed by
// another option is rejected before spawn — one flag can never consume
// another flag as its value.
func TestKVContractFlagCannotConsumeFlag(t *testing.T) {
	caps := defaultCapsForTag("b11205")

	bad := [][]string{
		{"--cache-type-k", "--cache-type-v"},
		{"--cache-type-v", "--mlock"},
		{"--cache-type-kv", "--model", "m.gguf"},
		{"--cache-type-k"},
		{"--load-mode", "--no-webui"},
	}
	for _, args := range bad {
		if problems := argProblems(args, caps); len(problems) == 0 {
			t.Errorf("validator accepted a KV/load-mode option consuming another flag: %v", args)
		}
	}
}

// TestKVContractRejectsInconsistentCombinations: mixed layouts and
// unsupported forms are refused before spawn.
func TestKVContractRejectsInconsistentCombinations(t *testing.T) {
	// A profile claiming BOTH layouts is inconsistent.
	mixed := &EngineCaps{
		CacheTypeKVShared: true, CacheTypeK: true, CacheTypeV: true,
		CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2,
	}
	if problems := argProblems([]string{"--model", "m.gguf"}, mixed); len(problems) == 0 {
		t.Fatal("inconsistent capability profile (both KV layouts) accepted")
	}

	// A launch profile mixing both FORMS is refused even on a permissive caps.
	caps := &EngineCaps{
		CacheTypeKVShared: true, CacheTypeK: true, CacheTypeV: true,
		CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2,
	}
	if problems := argProblems([]string{"--cache-type-kv", "q8_0", "--cache-type-v", "q8_0"}, caps); len(problems) == 0 {
		t.Fatal("mixed KV layouts in one launch profile accepted")
	}

	// The shared form on a modern (split-only) profile is refused.
	modern := defaultCapsForTag("b11205")
	if problems := argProblems([]string{"--cache-type-kv", "q8_0"}, modern); len(problems) == 0 {
		t.Fatal("--cache-type-kv accepted for a modern split-only profile")
	}

	// The split form on a shared-only profile is refused.
	legacy := &EngineCaps{CacheTypeKVShared: true, CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2}
	if problems := argProblems([]string{"--cache-type-k", "q8_0"}, legacy); len(problems) == 0 {
		t.Fatal("--cache-type-k accepted for a legacy shared-only profile")
	}
}

// --- repair: the bounded, acyclic KV chain -----------------------------------

// TestKVContractRepairSharedToSplit: the exact b11205-class failure
// ("error: invalid argument: --cache-type-kv") classifies as an
// option-class failure naming the option, and the surgical repair moves
// the profile to the split layout — NO compatibility descent, no blanket
// strip.
func TestKVContractRepairSharedToSplit(t *testing.T) {
	caps := &EngineCaps{
		Tag: "legacy-probe", CacheTypeKVShared: true,
		CacheReuse: true, NoWebUI: true, Jinja: true, UBatchSize: true, ThreadsBatch: true,
		Schema: capsSchemaV2,
	}

	sf := ClassifyStartupFailure("error: invalid argument: --cache-type-kv")
	if sf == nil || (sf.Kind != FailOptionLayout && sf.Kind != FailUnknownOption) {
		t.Fatalf("b11205-class error misclassified: %+v", sf)
	}
	if sf.Option != "cache-type-kv" {
		t.Fatalf("classification must name the option, got %q", sf.Option)
	}

	repaired := repairCapsFor(caps, sf, nil)
	if repaired == nil {
		t.Fatal("shared→split repair must exist (no compatibility descent for a one-option layout defect)")
	}
	if repaired.CacheTypeKVShared || !repaired.CacheTypeK || !repaired.CacheTypeV {
		t.Fatalf("repair must flip to the split layout: %+v", repaired)
	}
	// Unrelated options untouched.
	if !repaired.CacheReuse || !repaired.NoWebUI || !repaired.UBatchSize || !repaired.ThreadsBatch {
		t.Fatalf("repair stripped unrelated options: %+v", repaired)
	}
	// The removal is recorded — diagnostics can explain it.
	if len(repaired.Removed) == 0 {
		t.Fatal("repair must record the removal")
	}

	// The repaired profile's emission validates cleanly with the quant on.
	cfg := newCapConfig()
	cfg.KVCacheQuant = "q8_0"
	if problems := argProblems(SpeedArgsWithCaps(cfg, repaired), repaired); len(problems) > 0 {
		t.Fatalf("repaired profile failed validation: %v", problems)
	}
}

// TestKVContractRepairChainIsAcyclic: shared → split → none. A further
// rejection of the split form drops KV entirely; it can never flip back.
func TestKVContractRepairChainIsAcyclic(t *testing.T) {
	shared := &EngineCaps{CacheTypeKVShared: true, CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2}

	first := repairCapsFor(shared, &StartupFailure{Kind: FailUnknownOption, Option: "cache-type-kv"}, nil)
	if first == nil || !first.CacheTypeK {
		t.Fatalf("shared→split repair missing: %+v", first)
	}

	second := repairCapsFor(first, &StartupFailure{Kind: FailUnknownOption, Option: "cache-type-k"}, nil)
	if second == nil {
		t.Fatal("split→none repair missing")
	}
	if second.CacheTypeK || second.CacheTypeV || second.CacheTypeKVShared {
		t.Fatalf("terminal KV state must carry no layout: %+v", second)
	}

	// A repair on a profile that already carries no layout is a no-op
	// (nil) — the chain cannot loop.
	if again := repairCapsFor(second, &StartupFailure{Kind: FailUnknownOption, Option: "cache-type-k"}, nil); again != nil {
		t.Fatalf("repair chain must be terminal, got %+v", again)
	}
}

// TestKVContractMlockChain: --mlock rejected → --load-mode; --load-mode
// rejected → nothing (never back to --mlock).
func TestKVContractMlockChain(t *testing.T) {
	legacy := &EngineCaps{Mlock: true, CacheReuse: true, NoWebUI: true, Jinja: true, Schema: capsSchemaV2}

	first := repairCapsFor(legacy, &StartupFailure{Kind: FailUnknownOption, Option: "mlock"}, nil)
	if first == nil || first.Mlock || !first.LoadMode {
		t.Fatalf("--mlock→--load-mode repair missing: %+v", first)
	}

	second := repairCapsFor(first, &StartupFailure{Kind: FailUnknownOption, Option: "load-mode"}, nil)
	if second == nil || second.LoadMode || second.Mlock {
		t.Fatalf("--load-mode rejection must drop pinning entirely: %+v", second)
	}
	if again := repairCapsFor(second, &StartupFailure{Kind: FailUnknownOption, Option: "load-mode"}, nil); again != nil {
		t.Fatalf("pinning chain must be terminal: %+v", again)
	}
}

// --- profile schema ----------------------------------------------------------

// TestKVContractStalePreV172ProfileRedetected: a persisted pre-1.7.2
// profile (no KV knowledge) is STALE — it must not suppress detection.
func TestKVContractStalePreV172ProfileRedetected(t *testing.T) {
	dir := t.TempDir()
	cfg := newCapConfig()
	cfg.DataDir = dir

	// A v1.7.1-era persisted profile for the same tag: no schema, no KV
	// fields.
	stale := &EngineCaps{
		Tag: "b11205", FlashAttnEnabled: true, FlashAttnValue: true,
		CacheReuse: true, NoWebUI: true, Jinja: true, UBatchSize: true, ThreadsBatch: true,
		DeviceFlag: true, Source: "help-parse", VerifiedAt: "2026-09-01T00:00:00Z",
	}
	saveEngineCaps(cfg, stale)

	if loaded := loadEngineCaps(cfg, "b11205"); loaded != nil {
		t.Fatalf("stale pre-1.7.2 profile must not load (would suppress KV detection): %+v", loaded)
	}

	// A current-schema profile loads normally.
	fresh := defaultCapsForTag("b11205")
	fresh.VerifiedAt = "2026-09-20T00:00:00Z"
	saveEngineCaps(cfg, fresh)
	if loaded := loadEngineCaps(cfg, "b11205"); loaded == nil || !loaded.CacheTypeK {
		t.Fatalf("current-schema profile must load: %+v", loaded)
	}
}

// --- the whole launch profile, end to end ------------------------------------

// TestKVContractFullModernLaunchProfileValidates: the complete level-0
// launch profile (base args + Speed Pack) for a modern engine validates
// with zero problems — the b11205 normal launch is a compatibility-0
// profile, not a compat-2 fallback.
func TestKVContractFullModernLaunchProfileValidates(t *testing.T) {
	cfg := newCapConfig()
	cfg.CacheReuse = 256
	cfg.KVCacheQuant = "q8_0"
	cfg.Mlock = true
	cfg.DraftModel = ""

	s := NewLlamaServer(nil)
	caps := defaultCapsForTag("b11205")

	full := s.buildArgsWithCaps(cfg, "model.gguf", 0, caps)
	joined := " " + strings.Join(full, " ") + " "

	if strings.Contains(joined, "--cache-type-kv") {
		t.Fatalf("full profile carries the dead option: %v", full)
	}
	if problems := argProblems(full, caps); len(problems) > 0 {
		t.Fatalf("full modern profile must validate cleanly (compat 0): %v", problems)
	}

	// Every value-taking option is followed by a non-option value.
	for i, a := range full {
		if a == "--cache-type-k" || a == "--cache-type-v" || a == "--load-mode" ||
			a == "--flash-attn" || a == "--cache-reuse" || a == "--ubatch-size" ||
			a == "--threads-batch" {
			if i+1 >= len(full) || strings.HasPrefix(full[i+1], "-") {
				t.Fatalf("option %s consumes another flag as its value: %v", a, full)
			}
		}
	}
}
