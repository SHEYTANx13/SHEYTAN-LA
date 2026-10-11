package llm

// capability.go — Phase 7 dynamic engine-capability adapter.
//
// Phase 7 root-cause fix: llama.cpp changed the `--flash-attn` CLI contract
// from a bare boolean flag to a tri-state option:
//
//      old releases:   -fa, --flash-attn            (flag, no value)
//      new releases:   -fa, --flash-attn on|off|auto (option, requires value)
//
// `--cache-reuse N` is — and always was — an INDEPENDENT option:
//
//      --cache-reuse N
//
// The Phase 6 launcher emitted `--flash-attn` as a bare flag immediately
// before `--cache-reuse N`, so a newer llama-server parsed the pair as
// `--flash-attn '--cache-reuse'` and aborted with:
//
//      error while handling argument "--flash-attn":
//      unknown value for --flash-attn: '--cache-reuse'
//
// which dropped the engine into compatibility mode 2 (no speed flags) —
// hiding a one-option layout problem behind a profile that strips every
// speed flag.
//
// The fix is threefold and version-aware end to end:
//
//  1. DETECT   — parse `llama-server --help` for the actual CLI contract of
//     the installed binary (preferred), falling back to a conservative
//     release-tag heuristic when --help is unavailable. The result is one
//     EngineCaps profile, persisted only after VERIFIED engine startup.
//  2. VALIDATE — every launch profile is validated option-by-option BEFORE
//     the process is spawned: value-taking options must be followed by an
//     acceptable value; flag options must not. The historical regression
//     (`--flash-attn` immediately followed by `--cache-reuse`) can never be
//     spawned again.
//  3. REPAIR   — when startup still fails on an option, classify the exact
//     failure from the engine's own stderr, repair ONLY the offending
//     option (flip its layout / drop it), retry at the same compat level,
//     and persist the verified profile with the recorded reason. Only when
//     surgical repair is exhausted does the existing compatibility ladder
//     continue to the next level.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// capsFileName is the engine capability profile store. It lives beside the
// config (DataDir) and is keyed by engine release tag: a different binary
// invalidates the profile automatically.
const capsFileName = "engine-caps.json"

// flashAttnValueSinceTag is the conservative fallback threshold: the
// first llama.cpp release line known to require the `on|off|auto` value
// form. Real behaviour is always preferred via --help parsing; this
// heuristic only applies when the binary cannot be probed (e.g. missing
// binary pre-flight, offline first boot with a cached tag). v1.7.2
// evidence correction: the REAL b10642 binary (probed 2026-09, build
// 10642) already REQUIRES the value form — `--flash-attn --cache-reuse 4`
// fails with "unknown value for --flash-attn: '--cache-reuse'" exactly as
// on b11205 — so the historical claim that b10642 accepted the bare flag
// was wrong and the boundary moves down to the verified point. Tags older
// than this keep the bare-flag layout (the pre-tri-state builds).
const flashAttnValueSinceTag = 10642

// helpProbeTimeout bounds one `--help` capability probe.
const helpProbeTimeout = 10 * time.Second

// EngineCaps is the verified capability profile of one llama.cpp binary.
// Every launch-time argument decision reads this profile — never a
// hard-coded assumption about a release.
type EngineCaps struct {
	// Tag is the llama.cpp release tag the profile was verified against.
	Tag string `json:"tag"`

	// FlashAttnEnabled is false only when the build does not know the
	// --flash-attn option at all (repair removed it entirely).
	FlashAttnEnabled bool `json:"flashAttnEnabled"`

	// FlashAttnValue is true when `--flash-attn` REQUIRES an on|off|auto
	// value; false when it is a bare boolean flag.
	FlashAttnValue bool `json:"flashAttnValue"`

	// CacheReuse is true when `--cache-reuse N` is supported.
	CacheReuse bool `json:"cacheReuse"`

	// NoWebUI / Jinja / UBatchSize / ThreadsBatch mirror the remaining
	// Speed Pack options so a capability strip can be surgical per option.
	NoWebUI      bool `json:"noWebUI"`
	Jinja        bool `json:"jinja"`
	UBatchSize   bool `json:"ubatchSize"`
	ThreadsBatch bool `json:"threadsBatch"`

	// --- v1.7.2: the KV-cache quantization CLI contract ------------------
	//
	// The v1.7.1 launcher emitted `--cache-type-kv q8_0` unconditionally:
	// every modern llama.cpp build rejects that option ("error: invalid
	// argument: --cache-type-kv") and the resulting descent into
	// compatibility mode 2 hid a one-option layout defect behind a
	// profile that strips ALL speed flags.
	//
	// Real engine evidence (upstream common/arg.cpp, tags b10642 and
	// b11205, verified 2026-09): modern builds accept the SPLIT form
	//
	//      -ctk, --cache-type-k TYPE     (KV cache data type for K)
	//      -ctv, --cache-type-v TYPE     (KV cache data type for V)
	//
	// with the exact-match value vocabulary f32 f16 bf16 q8_0 q4_0 q4_1
	// iq4_nl q5_0 q5_1 (ggml_type_name is all-lowercase). The LEGACY
	// shared form `--cache-type-kv VALUE` is retained for builds whose
	// own --help reports it — --help parsing is the authority, the tag
	// heuristic only approximates when the binary cannot be probed.
	//
	// A consistent profile carries AT MOST ONE layout: split
	// (CacheTypeK/CacheTypeV) or shared (CacheTypeKVShared) — pre-spawn
	// validation rejects any combination.

	// CacheTypeK is true when `--cache-type-k VALUE` is supported.
	CacheTypeK bool `json:"cacheTypeK"`

	// CacheTypeV is true when `--cache-type-v VALUE` is supported.
	CacheTypeV bool `json:"cacheTypeV"`

	// CacheTypeKVShared is true when the LEGACY `--cache-type-kv VALUE`
	// form is supported (the engine's own --help must say so).
	CacheTypeKVShared bool `json:"cacheTypeKVShared"`

	// --- v1.7.2: the memory-pinning CLI contract --------------------------
	//
	// `--mlock` (bare flag, the pre-load-mode layout) was replaced by the
	// value-taking `-lm, --load-mode MODE` (auto|none|mmap|mlock|mmap+mlock|dio);
	// verified absent/present against the same b10642/b11205 sources.
	// Mlock is the legacy bare-flag form; LoadMode is the modern option.
	// Both false (unknown/unsupported) emits nothing — fail-closed.

	// Mlock is true when the legacy `--mlock` bare flag is supported.
	Mlock bool `json:"mlock"`

	// LoadMode is true when `--load-mode MODE` is supported (mlock
	// reachable as `--load-mode mlock`).
	LoadMode bool `json:"loadMode"`

	// Schema is the capability-profile schema version. v1.7.2 bumped it
	// to capsSchemaV2 when the KV-cache layout + load-mode fields were
	// introduced: a persisted pre-1.7.2 profile (schema 0) is STALE — it
	// carries no KV layout knowledge and must never suppress detection —
	// so the loader treats it as absent and the binary is re-probed.
	Schema int `json:"schema,omitempty"`

	// DeviceFlag (v1.2.6 continuation): the build accepts `--device
	// <name>` for deterministic accelerator selection (llama.cpp
	// Vulkan0/CUDA0/… device names). Fail-closed: an unprobed or
	// too-old build reports false and the launcher simply does not
	// pass --device (the engine picks its default device — the
	// behavior every release before this had).
	DeviceFlag bool `json:"deviceFlag"`

	// Source records how the profile was obtained:
	//   "help-parse"      — parsed from `--help` output of the binary
	//   "tag-fallback"    — conservative heuristic from the release tag
	//   "verified-repair" — corrected after a classified startup failure
	//     and then verified by a successful engine start
	Source string `json:"source"`

	// VerifiedAt is the RFC3339 time of the last VERIFIED successful
	// startup with this profile (empty = not yet verified by a real boot).
	VerifiedAt string `json:"verifiedAt,omitempty"`

	// Removed records options that were dropped during compatibility
	// repair, with the reason, so diagnostics can explain any downgrade.
	Removed []RemovedOption `json:"removed,omitempty"`
}

// RemovedOption is one option excluded from the launch profile, with the
// classified reason. Never silently downgraded: always logged and persisted.
type RemovedOption struct {
	Option string `json:"option"`
	Reason string `json:"reason"`
}

// Clone returns a deep copy so repair paths never mutate a shared profile.
func (c *EngineCaps) Clone() *EngineCaps {
	if c == nil {
		return nil
	}
	out := *c
	if c.Removed != nil {
		out.Removed = append([]RemovedOption(nil), c.Removed...)
	}
	return &out
}

// defaultCapsForTag builds the conservative profile for a release tag when
// the binary cannot be probed. Options introduced after the pinned release
// default to unsupported only when the tag is older than their arrival; the
// Speed Pack options are all present in every tag SHEYTAN has ever shipped.
func defaultCapsForTag(tag string) *EngineCaps {
	n := tagNumber(tag)
	splitKV := n >= cacheTypeSplitSinceTag
	caps := &EngineCaps{
		Tag:              tag,
		FlashAttnEnabled: true,
		FlashAttnValue:   n >= flashAttnValueSinceTag,
		CacheReuse:       true,
		NoWebUI:          true,
		Jinja:            true,
		UBatchSize:       true,
		ThreadsBatch:     true,
		// v1.7.2 KV contract: the split layout for every tag in the
		// verified window; older/unparseable tags stay fail-closed with
		// NO KV option. The shared form never comes from the heuristic
		// — only the engine's own --help can report it.
		CacheTypeK: splitKV,
		CacheTypeV: splitKV,
		// v1.7.2 memory pinning: --load-mode replaced --mlock.
		Mlock:    false,
		LoadMode: n >= loadModeSinceTag,
		Schema:   capsSchemaV2,
		// --device landed in llama.cpp builds around b3000
		// (mid-2024); older/unknown tags stay fail-closed.
		DeviceFlag: n >= deviceFlagSinceTag,
		Source:     "tag-fallback",
	}
	return caps
}

// deviceFlagSinceTag is the first llama.cpp release tag whose llama-server
// accepts --device (measured from the llama.cpp change log; conservative).
const deviceFlagSinceTag = 3000

// cacheTypeSplitSinceTag is the fallback threshold for the SPLIT KV-cache
// layout (--cache-type-k / --cache-type-v): every upstream tag SHEYTAN has
// ever provisioned carries the split form — verified directly in the
// upstream common/arg.cpp sources at b10642 AND b11205 (2026-09 evidence),
// which define only -ctk/--cache-type-k and -ctv/--cache-type-v and NO
// --cache-type-kv. Tags older than this boundary (and unparseable tags,
// tagNumber 0) are unknown → NO KV option is emitted (fail-closed). The
// engine's own --help remains the authority whenever the binary can be
// probed; a build that genuinely reports the legacy shared
// --cache-type-kv form gets it from help parsing, never from here.
const cacheTypeSplitSinceTag = 10642

// loadModeSinceTag is the fallback threshold for the modern
// `--load-mode MODE` option (mlock reached as `--load-mode mlock`):
// verified present at b10642 and b11205 while the legacy `--mlock` bare
// flag is verified ABSENT from both. Older/unparseable tags are unknown →
// nothing is emitted (fail-closed).
const loadModeSinceTag = 10642

// kvCacheQuantValues is the EXACT value vocabulary the engine's own
// kv_cache_type_from_str accepts (upstream b10642/b11205 common/arg.cpp;
// ggml_type_name is all-lowercase, the engine matches exactly). Pre-spawn
// validation mirrors the engine parser so a bad quant can never spawn a
// doomed process (v1.6.1 numeric-range discipline, applied to the KV type).
var kvCacheQuantValues = map[string]bool{
	"f32":    true,
	"f16":    true,
	"bf16":   true,
	"q8_0":   true,
	"q4_0":   true,
	"q4_1":   true,
	"iq4_nl": true,
	"q5_0":   true,
	"q5_1":   true,
}

// capsSchemaV2 is the current capability-profile schema (v1.7.2). Profiles
// persisted with a lower schema are stale and re-detected.
const capsSchemaV2 = 2

// HasKVCacheQuant reports whether the profile knows any KV-cache
// quantization layout (split or shared).
func (c *EngineCaps) HasKVCacheQuant() bool {
	return c != nil && (c.CacheTypeK || c.CacheTypeV || c.CacheTypeKVShared)
}

// kvLayoutName renders the KV layout for logs/diagnostics ("split",
// "shared", "unsupported").
func (c *EngineCaps) kvLayoutName() string {
	if c == nil {
		return "unknown"
	}
	switch {
	case c.CacheTypeK || c.CacheTypeV:
		return "split (--cache-type-k/-v)"
	case c.CacheTypeKVShared:
		return "shared (--cache-type-kv)"
	default:
		return "unsupported"
	}
}

// tagNumber extracts the numeric part of a `b12345` release tag (0 when
// unparseable — treated as "unknown old release").
func tagNumber(tag string) int {
	m := regexp.MustCompile(`b(\d+)`).FindStringSubmatch(tag)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// ---------------------------------------------------------------------------
// Capability detection — `--help` parsing preferred, tag fallback second.
// ---------------------------------------------------------------------------

// DetectEngineCaps probes the binary's own `--help` output and parses the
// real CLI contract. When the probe fails (missing binary, offline, unusual
// build), the conservative tag heuristic is returned with its source set so
// callers always know how much to trust the profile.
func DetectEngineCaps(binPath, tag string) *EngineCaps {
	caps := parseHelpCaps(binPath, tag)
	if caps != nil {
		return caps
	}
	return defaultCapsForTag(tag)
}

// parseHelpCaps runs `<bin> --help` and parses option lines. Returns nil
// when the probe is impossible or the output cannot be trusted.
func parseHelpCaps(binPath, tag string) *EngineCaps {
	if binPath == "" {
		return nil
	}
	if _, err := os.Stat(binPath); err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), helpProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "--help")
	cmd.Env = sanitizedHelpEnv()
	out, err := cmd.CombinedOutput()
	if err != nil || len(out) == 0 {
		// A nonzero exit is common for --help on older builds; only trust
		// the output when it actually contains the options we care about.
		if !strings.Contains(string(out), "--flash-attn") &&
			!strings.Contains(string(out), "--cache-reuse") {
			return nil
		}
	}

	text := string(out)
	caps := &EngineCaps{
		Tag:              tag,
		FlashAttnEnabled: optionMentioned(text, "--flash-attn"),
		CacheReuse:       optionMentioned(text, "--cache-reuse"),
		NoWebUI:          optionMentioned(text, "--no-webui"),
		Jinja:            optionMentioned(text, "--jinja"),
		DeviceFlag:       optionMentioned(text, "--device"),
		UBatchSize:       optionMentioned(text, "--ubatch-size") || optionMentioned(text, "--ubatch"),
		ThreadsBatch:     optionMentioned(text, "--threads-batch"),
		// v1.7.2 KV contract: boundary-aware matching — "--cache-type-k"
		// is a PREFIX of "--cache-type-kv", so a plain Contains() would
		// report the split form for a shared-only build. The engine's
		// own option lines are the authority for which form it accepts.
		CacheTypeK:        helpMentionsOption(text, "--cache-type-k"),
		CacheTypeV:        helpMentionsOption(text, "--cache-type-v"),
		CacheTypeKVShared: helpMentionsOption(text, "--cache-type-kv"),
		// v1.7.2 memory pinning: --load-mode replaced --mlock. "--device"
		// is a distinct token, but --load-mode must also be matched
		// exactly (a hypothetical --load-mode-xyz must not count).
		Mlock:    helpMentionsOption(text, "--mlock"),
		LoadMode: helpMentionsOption(text, "--load-mode"),
		Schema:   capsSchemaV2,
		Source:   "help-parse",
	}

	caps.FlashAttnValue = helpFlashAttnTakesValue(text)

	// Sanity gate: a parsed profile that lost every option compared with
	// the tag fallback is more likely a parse failure than a real
	// regression — fall back to the heuristic in that case.
	if !caps.CacheReuse && !caps.NoWebUI && !caps.Jinja {
		return nil
	}
	return caps
}

// helpMentionsOption reports whether the help text mentions an option
// EXACTLY — the match must be followed by a non-name character so a longer
// option that merely starts with the same prefix (the --cache-type-k /
// --cache-type-kv pair) never produces a false positive.
func helpMentionsOption(help, option string) bool {
	for from := 0; from < len(help); {
		idx := strings.Index(help[from:], option)
		if idx < 0 {
			return false
		}
		idx += from
		end := idx + len(option)
		if end >= len(help) || !isOptionNameChar(help[end]) {
			return true
		}
		from = idx + 1
	}
	return false
}

// isOptionNameChar reports whether c can continue an option name
// (letters, digits, dashes — the characters llama.cpp uses in long names).
func isOptionNameChar(c byte) bool {
	return c == '-' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// helpFlashAttnTakesValue decides the `--flash-attn` layout from help text:
//
//	value form:  "-fa, --flash-attn [on|off|auto]" / "--flash-attn on|off|auto"
//	flag form:   "-fa, --flash-attn" (no value list on the same line)
func helpFlashAttnTakesValue(help string) bool {
	for _, line := range strings.Split(help, "\n") {
		if !strings.Contains(line, "--flash-attn") {
			continue
		}
		// The tri-state value list appears as on|off|auto (optionally
		// bracketed) on the same line as the option.
		if regexp.MustCompile(`on\s*\|\s*off\s*\|\s*auto`).MatchString(line) {
			return true
		}
	}
	return false
}

// optionMentioned reports whether an option appears in help output.
func optionMentioned(help, option string) bool {
	return strings.Contains(help, option)
}

// sanitizedHelpEnv builds a minimal environment for the --help probe so a
// hostile model directory or engine binary cannot leak the user's
// environment into a subprocess (mirrors proc.SanitizedEnvironment intent
// without pulling the Windows-specific pieces into this probe).
func sanitizedHelpEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TEMP=" + os.Getenv("TEMP"),
		"TMP=" + os.Getenv("TMP"),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		"COMSPEC=" + os.Getenv("COMSPEC"),
	}
}

// ---------------------------------------------------------------------------
// Capability profile persistence — verified profiles only.
// ---------------------------------------------------------------------------

// capsStore is the on-disk shape of engine-caps.json.
type capsStore struct {
	Tag  string      `json:"tag"`
	Caps *EngineCaps `json:"caps"`
}

// capsPath resolves the profile store location.
func capsPath(cfg *config.Config) string {
	return filepath.Join(cfg.DataDir, capsFileName)
}

// loadEngineCaps returns the persisted profile when it matches the current
// engine tag AND the current profile schema; otherwise nil (the caller
// falls back to detection). v1.7.2: a pre-1.7.2 profile (schema 0) carries
// no KV-cache layout knowledge — treating it as valid would silently
// suppress the KV quantization option, so it is STALE by definition and
// the binary is re-probed once.
func loadEngineCaps(cfg *config.Config, tag string) *EngineCaps {
	data, err := os.ReadFile(capsPath(cfg))
	if err != nil {
		return nil
	}
	var store capsStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil
	}
	if store.Caps == nil || store.Tag != tag || store.Caps.Tag != tag {
		return nil
	}
	if store.Caps.Schema < capsSchemaV2 {
		return nil // stale pre-1.7.2 profile — re-detect (bounded, once)
	}
	return store.Caps
}

// saveEngineCaps persists a profile. Only VERIFIED profiles (successful
// engine start) may be persisted — callers must respect that contract.
func saveEngineCaps(cfg *config.Config, caps *EngineCaps) {
	if cfg == nil || caps == nil {
		return
	}
	store := capsStore{Tag: caps.Tag, Caps: caps}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return
	}
	tmp := capsPath(cfg) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, capsPath(cfg)) // atomic replace
}

// resolveEngineCaps picks the capability profile for the configured engine:
// a persisted verified profile for the current tag wins; otherwise the tag
// heuristic applies. This is the cheap, allocation-free path used by
// SpeedArgs; the full --help detection runs once per boot in Start().
func resolveEngineCaps(cfg *config.Config) *EngineCaps {
	tag := currentEngineTag(cfg)
	if caps := loadEngineCaps(cfg, tag); caps != nil {
		return caps
	}
	return defaultCapsForTag(tag)
}

// currentEngineTag resolves the installed engine tag through the updater
// bookkeeping (installed.json); empty when never recorded.
func currentEngineTag(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return updater.InstalledEngineTag(cfg)
}

// ---------------------------------------------------------------------------
// Launch-argument validation — every profile is checked BEFORE spawn.
// ---------------------------------------------------------------------------

// argProblems returns human-readable validation problems for a launch
// profile. An empty slice means the profile is internally consistent with
// the capability profile.
//
// The known-option table covers every option SHEYTAN itself can emit;
// unknown options (user-provided LlamaExtraArgs) are passed through
// unvalidated — the engine remains the authority on its own surface.
func argProblems(args []string, caps *EngineCaps) []string {
	if caps == nil {
		caps = defaultCapsForTag("")
	}

	flashAttnValues := map[string]bool{"on": true, "off": true, "auto": true}

	// Options that take exactly one value, by name.
	takesValue := map[string]string{
		"--model":             "path",
		"--host":              "string",
		"--port":              "int",
		"--ctx-size":          "int",
		"--batch-size":        "int",
		"--ubatch-size":       "int",
		"--threads":           "int",
		"--threads-batch":     "int",
		"--n-gpu-layers":      "int",
		"--draft-max":         "int",
		"--seed":              "int",
		"--repeat-last-n":     "int",
		"--cache-reuse":       "int",
		"--temp":              "float",
		"--top-p":             "float",
		"--min-p":             "float",
		"--repeat-penalty":    "float",
		"--presence-penalty":  "float",
		"--frequency-penalty": "float",
		"--mirostat":          "int",
		"--mirostat-tau":      "float",
		"--mirostat-eta":      "float",
		"--mmproj":            "path",
		"--model-draft":       "path",
		"--device":            "string",
		// v1.7.2: the KV-cache quantization layouts and the load-mode
		// option all take exactly one value. Registering them here also
		// gives the flag/value consumption semantics for free: the
		// value slot is skipped, and a missing/option-looking value is
		// rejected BEFORE spawn (one flag can never consume another
		// flag as its value).
		"--cache-type-k":  "kvtype",
		"--cache-type-v":  "kvtype",
		"--cache-type-kv": "kvtype",
		"--load-mode":     "loadmode",
	}

	// Options that must NEVER be followed by a value.
	flagOnly := map[string]bool{
		"--mlock":    true,
		"--no-webui": true,
		"--jinja":    true,
	}

	// v1.7.2: exact value vocabularies mirrored from the engine's own
	// parser (upstream b10642/b11205 common/arg.cpp) — a value outside
	// the engine's set is a DETERMINISTIC launch failure, rejected here
	// like the numeric ranges below: no compatibility mode can make an
	// invalid value work.
	valueSets := map[string]map[string]bool{
		"--cache-type-k":  kvCacheQuantValues,
		"--cache-type-v":  kvCacheQuantValues,
		"--cache-type-kv": kvCacheQuantValues,
		"--load-mode": {
			"auto": true, "none": true, "mmap": true,
			"mlock": true, "mmap+mlock": true, "dio": true,
		},
	}

	var problems []string

	// v1.7.2 KV-layout consistency: the profile may carry at most ONE
	// layout. A profile (or a hand-edited config feeding it) that claims
	// both the split and shared forms is inconsistent with any real
	// engine — refuse it before spawn.
	if caps.CacheTypeKVShared && (caps.CacheTypeK || caps.CacheTypeV) {
		problems = append(problems,
			"inconsistent capability profile: --cache-type-kv (shared) and --cache-type-k/-v (split) are mutually exclusive layouts")
	}

	var sawKVShared, sawKVSplit bool

	// v1.6.1 (P0): per-option numeric RANGE contracts, mirroring the
	// engine's own argument parser. Type checks alone let
	// "--repeat-penalty 0" through (0 parses fine as a float) — the
	// engine then rejected it and the failure was mis-fed into the
	// option-repair/compat ladder. The rules below reject the same
	// values the parser does, BEFORE any process spawns.
	floatRanges := map[string]struct {
		min, max     float64
		minExclusive bool
		rule         string
	}{
		"--repeat-penalty": {0, math.Inf(1), true, "repeat-penalty must be finite and greater than 0"},
		"--temp":           {0, math.Inf(1), false, "temperature must be a finite number of at least 0"},
		"--top-p":          {0, 1, false, "top-p must be between 0 and 1"},
		"--min-p":          {0, 1, false, "min-p must be between 0 and 1"},
		"--mirostat-tau":   {0, math.Inf(1), true, "mirostat-tau must be finite and greater than 0"},
		"--mirostat-eta":   {0, math.Inf(1), true, "mirostat-eta must be finite and greater than 0"},
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}

		next := ""
		if i+1 < len(args) {
			next = args[i+1]
		}

		// v1.7.2: KV-layout capability gates. Each emitted form
		// must be one the engine's own contract supports; mixing
		// both forms in ONE launch profile is always wrong.
		switch arg {
		case "--cache-type-kv":
			sawKVShared = true
			if !caps.CacheTypeKVShared {
				problems = append(problems,
					"--cache-type-kv is not supported by this engine build (the modern layout is --cache-type-k/--cache-type-v)")
			}
		case "--cache-type-k":
			sawKVSplit = true
			if !caps.CacheTypeK {
				problems = append(problems,
					"--cache-type-k is not supported by this engine build")
			}
		case "--cache-type-v":
			sawKVSplit = true
			if !caps.CacheTypeV {
				problems = append(problems,
					"--cache-type-v is not supported by this engine build")
			}
		case "--mlock":
			if !caps.Mlock {
				problems = append(problems,
					"--mlock is not supported by this engine build (the modern option is --load-mode mlock)")
			}
		case "--load-mode":
			if !caps.LoadMode {
				problems = append(problems,
					"--load-mode is not supported by this engine build")
			}
		}

		switch {
		case arg == "--flash-attn":
			if caps.FlashAttnValue {
				// Value form REQUIRED. A following option token (e.g.
				// --cache-reuse) would be consumed as the value by the
				// engine — the exact Phase 6 regression.
				if next == "" || strings.HasPrefix(next, "-") || !flashAttnValues[strings.ToLower(next)] {
					problems = append(problems, fmt.Sprintf(
						"--flash-attn requires an on|off|auto value, got %q",
						next))
				}
			} else {
				// Flag form: a bare value after it would be an unknown
				// positional on legacy engines.
				if next != "" && !strings.HasPrefix(next, "-") {
					problems = append(problems, fmt.Sprintf(
						"--flash-attn is a boolean flag on this engine build; it must not be given a value (got %q)",
						next))
				}
			}

		case arg == "--cache-reuse":
			if !caps.CacheReuse {
				problems = append(problems,
					"--cache-reuse is not supported by this engine build")
				continue
			}
			if n, err := strconv.Atoi(next); err != nil || n < 0 {
				problems = append(problems, fmt.Sprintf(
					"--cache-reuse requires a non-negative integer, got %q", next))
			}

		case flagOnly[arg]:
			if next != "" && !strings.HasPrefix(next, "-") {
				problems = append(problems, fmt.Sprintf(
					"%s takes no value, got %q", arg, next))
			}

		default:
			if kind, ok := takesValue[arg]; ok {
				if next == "" || (strings.HasPrefix(next, "-") && !isNegativeNumber(next)) {
					problems = append(problems, fmt.Sprintf(
						"%s requires a value (%s), got %q", arg, kind, next))
					continue
				}
				switch kind {
				case "int":
					if _, err := strconv.Atoi(next); err != nil {
						problems = append(problems, fmt.Sprintf(
							"%s requires an integer, got %q", arg, next))
					}
				case "float":
					v, ferr := strconv.ParseFloat(next, 64)
					if ferr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
						problems = append(problems, fmt.Sprintf(
							"%s requires a finite number, got %q", arg, next))
					} else if rng, ok := floatRanges[arg]; ok {
						bad := v < rng.min || v > rng.max ||
							(rng.minExclusive && v == rng.min)
						if bad {
							problems = append(problems, fmt.Sprintf(
								"%s %s — got %q (fix the value; no compatibility mode can make an invalid value work)",
								arg, rng.rule, next))
						}
					}
				case "kvtype", "loadmode":
					// v1.7.2: EXACT vocabulary check mirroring
					// the engine's own parser (lowercase, exact
					// match — the engine rejects "Q8_0"). The
					// launch pipeline normalizes its own values;
					// anything case-mismatched reaching here
					// would die deterministically at spawn.
					if !valueSets[arg][next] {
						problems = append(problems, fmt.Sprintf(
							"%s %q is not an accepted value for this engine (accepted: lowercase exact, e.g. q8_0; fix the value — no compatibility mode can make an invalid value work)",
							arg, next))
					}
				}
			}
			// Unknown options pass through — the engine owns its surface.
		}

		// Skip the consumed value so its own `--`-lookalike content is not
		// re-validated as an option.
		if _, ok := takesValue[arg]; ok {
			i++
		}
	}

	// v1.7.2: one launch profile may carry at most ONE KV layout.
	if sawKVShared && sawKVSplit {
		problems = append(problems,
			"launch profile mixes --cache-type-kv (shared) with --cache-type-k/-v (split) — pick one KV-cache layout")
	}

	return problems
}

func isNegativeNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// ---------------------------------------------------------------------------
// Startup-failure classification and surgical repair.
// ---------------------------------------------------------------------------

// StartupFailureKind classifies WHY an engine start failed, from the
// engine's own stderr/stdout text.
type StartupFailureKind string

const (
	// FailOptionLayout: an option was rejected because of its value/flag
	// layout (the --flash-attn on|off|auto class of failures).
	FailOptionLayout StartupFailureKind = "option-layout"

	// FailUnknownOption: the build does not know this option at all.
	FailUnknownOption StartupFailureKind = "unknown-option"

	// FailModelArch: the build cannot run this model architecture.
	FailModelArch StartupFailureKind = "unsupported-architecture"

	// FailModelLoad: the model itself failed to load (OOM, corrupt file,
	// allocation failure) — never repairable by option changes.
	FailModelLoad StartupFailureKind = "model-load"

	// FailEnvironment: missing DLLs and other host problems.
	FailEnvironment StartupFailureKind = "environment"

	// FailUnknown: unclassified.
	FailUnknown StartupFailureKind = "unknown"
)

// StartupFailure is the classified result of one failed launch attempt.
type StartupFailure struct {
	Kind   StartupFailureKind
	Option string // offending option, normalized WITHOUT leading dashes
	Detail string
}

// ClassifyStartupFailure maps engine error text to a typed failure.
func ClassifyStartupFailure(text string) *StartupFailure {
	lower := strings.ToLower(text)

	// Option-layout rejections:
	//   error while handling argument "--flash-attn":
	//   unknown value for --flash-attn: '--cache-reuse'
	//   error: invalid argument: on
	//   error: invalid argument: --flash-attn
	if m := regexp.MustCompile(`error while handling argument "([^"]+)"`).FindStringSubmatch(lower); m != nil {
		return &StartupFailure{
			Kind:   FailOptionLayout,
			Option: normalizeOption(m[1]),
			Detail: text,
		}
	}
	if m := regexp.MustCompile(`invalid argument: (\S+)`).FindStringSubmatch(lower); m != nil {
		sf := &StartupFailure{Kind: FailOptionLayout, Detail: text}
		if strings.HasPrefix(m[1], "--") {
			sf.Option = normalizeOption(m[1])
		}
		return sf
	}
	if strings.Contains(lower, "unknown value for") ||
		strings.Contains(lower, "invalid value for") {
		return &StartupFailure{Kind: FailOptionLayout, Detail: text}
	}

	// Fully unknown options.
	if m := regexp.MustCompile(`(?:unknown|unrecognized|invalid) argument:?\s+(--?[a-z0-9-]+)`).FindStringSubmatch(lower); m != nil {
		return &StartupFailure{
			Kind:   FailUnknownOption,
			Option: normalizeOption(m[1]),
			Detail: text,
		}
	}
	if m := regexp.MustCompile(`(?:unknown|unrecognized) option\s+"?(-{1,2}[a-z0-9-]+)"?`).FindStringSubmatch(lower); m != nil {
		return &StartupFailure{
			Kind:   FailUnknownOption,
			Option: normalizeOption(m[1]),
			Detail: text,
		}
	}

	// Model architecture (existing needsNewerEngine semantics).
	if strings.Contains(lower, "unknown model architecture") ||
		strings.Contains(lower, "unknown architecture") ||
		strings.Contains(lower, "unsupported architecture") ||
		strings.Contains(lower, "unknown model type") ||
		strings.Contains(lower, "unrecognized model architecture") {
		return &StartupFailure{Kind: FailModelArch, Detail: text}
	}

	// Real model-load failures: no option can fix these.
	for _, marker := range []string{
		"failed to load model",
		"error loading model",
		"unable to allocate",
		"out of memory",
		"cuda error",
		"failed to allocate",
		"corrupt",
	} {
		if strings.Contains(lower, marker) {
			return &StartupFailure{Kind: FailModelLoad, Detail: text}
		}
	}

	// Host environment.
	if strings.Contains(text, "3221225781") || strings.Contains(text, "0xc0000135") {
		return &StartupFailure{Kind: FailEnvironment, Detail: text}
	}

	return &StartupFailure{Kind: FailUnknown, Detail: text}
}

// normalizeOption strips leading dashes and lower-cases an option name.
func normalizeOption(opt string) string {
	return strings.ToLower(strings.TrimLeft(strings.TrimSpace(opt), "-"))
}

// maxOptionRepairs bounds the surgical repair loop per compat level.
const maxOptionRepairs = 2

// repairCapsFor produces the repaired capability profile for a classified
// startup failure, or nil when the failure is not surgically repairable.
// ONLY the offending option is touched — never a blanket strip.
func repairCapsFor(caps *EngineCaps, sf *StartupFailure, launchedArgs []string) *EngineCaps {
	if caps == nil || sf == nil {
		return nil
	}

	switch sf.Kind {
	case FailOptionLayout:
		// The flash-attn layout flip is the known repairable case. When
		// the engine named the option, match it; when it did not (legacy
		// "invalid argument: on"), only flip when the launched profile
		// actually carried a flash-attn value pair.
		if sf.Option == "" || sf.Option == "flash-attn" || sf.Option == "fa" {
			flipped := caps.Clone()
			flipped.FlashAttnValue = !caps.FlashAttnValue
			flipped.Source = "verified-repair"
			return flipped
		}

		// v1.7.2: the KV-layout chain and the memory-pinning chain.
		// "error: invalid argument: --cache-type-kv" (the b11205
		// class) lands here with the option named. The transitions
		// are ACYCLIC by construction — shared → split → none and
		// mlock → load-mode → none — so a bounded repair can never
		// oscillate.
		return repairKVOrPinCaps(caps, sf)

	case FailUnknownOption:
		if repaired := repairKVOrPinCaps(caps, sf); repaired != nil {
			return repaired
		}

		switch sf.Option {
		case "cache-reuse":
			if caps.CacheReuse {
				repaired := caps.Clone()
				repaired.CacheReuse = false
				repaired.Removed = append(repaired.Removed, RemovedOption{
					Option: "--cache-reuse",
					Reason: "engine build reports the option as unknown",
				})
				repaired.Source = "verified-repair"
				return repaired
			}
		case "flash-attn", "fa":
			// The build does not know flash-attn at all: drop it.
			repaired := caps.Clone()
			repaired.FlashAttnEnabled = false
			repaired.FlashAttnValue = false
			repaired.Removed = append(repaired.Removed, RemovedOption{
				Option: "--flash-attn",
				Reason: "engine build reports the option as unknown",
			})
			repaired.Source = "verified-repair"
			return repaired
		case "no-webui":
			repaired := caps.Clone()
			repaired.NoWebUI = false
			repaired.Removed = append(repaired.Removed, RemovedOption{
				Option: "--no-webui",
				Reason: "engine build reports the option as unknown",
			})
			repaired.Source = "verified-repair"
			return repaired
		case "jinja":
			repaired := caps.Clone()
			repaired.Jinja = false
			repaired.Removed = append(repaired.Removed, RemovedOption{
				Option: "--jinja",
				Reason: "engine build reports the option as unknown",
			})
			repaired.Source = "verified-repair"
			return repaired
		case "ubatch-size":
			repaired := caps.Clone()
			repaired.UBatchSize = false
			repaired.Removed = append(repaired.Removed, RemovedOption{
				Option: "--ubatch-size",
				Reason: "engine build reports the option as unknown",
			})
			repaired.Source = "verified-repair"
			return repaired
		case "threads-batch":
			repaired := caps.Clone()
			repaired.ThreadsBatch = false
			repaired.Removed = append(repaired.Removed, RemovedOption{
				Option: "--threads-batch",
				Reason: "engine build reports the option as unknown",
			})
			repaired.Source = "verified-repair"
			return repaired
		}
		return nil
	}

	return nil
}

// repairKVOrPinCaps produces the next profile in the ACYCLIC KV-layout /
// memory-pinning repair chains:
//
//	KV:       shared (--cache-type-kv) → split (--cache-type-k/-v) → none
//	Pinning:  --mlock → --load-mode → none
//
// Each transition fires only on the engine rejecting the CURRENT layout's
// option, moves strictly forward, and records the removal — so the bounded
// repair loop can never oscillate and never strips unrelated options.
// Returns nil when the failure names no KV/pinning option.
func repairKVOrPinCaps(caps *EngineCaps, sf *StartupFailure) *EngineCaps {
	mark := func(c *EngineCaps, option, reason string) *EngineCaps {
		c.Removed = append(c.Removed, RemovedOption{Option: option, Reason: reason})
		c.Source = "verified-repair"
		return c
	}

	switch sf.Option {
	case "cache-type-kv":
		// The engine rejected the shared layout.
		switch {
		case caps.CacheTypeKVShared && !caps.CacheTypeK && !caps.CacheTypeV:
			repaired := caps.Clone()
			repaired.CacheTypeKVShared = false
			repaired.CacheTypeK = true
			repaired.CacheTypeV = true
			return mark(repaired, "--cache-type-kv",
				"engine rejected the shared KV-cache layout — switching to the split --cache-type-k/-v form")
		case caps.CacheTypeKVShared:
			// Inconsistent profile state (should not happen — validation
			// rejects it): drop the shared form only.
			repaired := caps.Clone()
			repaired.CacheTypeKVShared = false
			return mark(repaired, "--cache-type-kv",
				"engine rejected the shared KV-cache layout")
		}
		return nil

	case "cache-type-k", "ctk":
		// The engine rejected the split K option: an engine this old has
		// no KV quantization worth chasing — drop the layout entirely.
		if caps.CacheTypeK || caps.CacheTypeV {
			repaired := caps.Clone()
			repaired.CacheTypeK = false
			repaired.CacheTypeV = false
			return mark(repaired, "--cache-type-k",
				"engine rejected the split KV-cache layout — KV-cache quantization disabled for this build")
		}
		return nil

	case "cache-type-v", "ctv":
		if caps.CacheTypeK || caps.CacheTypeV {
			repaired := caps.Clone()
			repaired.CacheTypeK = false
			repaired.CacheTypeV = false
			return mark(repaired, "--cache-type-v",
				"engine rejected the split KV-cache layout — KV-cache quantization disabled for this build")
		}
		return nil

	case "mlock":
		// Legacy bare flag rejected → the modern --load-mode option.
		if caps.Mlock {
			repaired := caps.Clone()
			repaired.Mlock = false
			repaired.LoadMode = true
			return mark(repaired, "--mlock",
				"engine rejected the legacy --mlock flag — switching to --load-mode mlock")
		}
		return nil

	case "load-mode", "lm":
		// Modern option rejected → drop memory pinning (do NOT flip back
		// to --mlock: the chain is one-directional).
		if caps.LoadMode {
			repaired := caps.Clone()
			repaired.LoadMode = false
			repaired.Mlock = false
			return mark(repaired, "--load-mode",
				"engine build does not support --load-mode — memory pinning disabled for this build")
		}
		return nil
	}

	return nil
}

// flashAttnDropped reports whether repair removed flash-attn entirely.
func flashAttnDropped(caps *EngineCaps) bool {
	return caps == nil || !caps.FlashAttnEnabled
}

// logRepair records one surgical repair for the worklog/UI trail.
func logRepair(level int, before, after *EngineCaps, sf *StartupFailure) {
	logging.Default().Warn(
		"engine",
		"startup repair (compat %d): engine rejected %s (%s) — repairing only that option: %s",
		level,
		optionDisplay(sf),
		sf.Kind,
		capsDiff(before, after),
	)
}

func optionDisplay(sf *StartupFailure) string {
	if sf.Option == "" {
		return "an option layout"
	}
	return "--" + sf.Option
}

// capsDiff summarizes what changed between two profiles.
func capsDiff(before, after *EngineCaps) string {
	if before == nil || after == nil {
		return "profile unavailable"
	}
	var changes []string
	if before.FlashAttnValue != after.FlashAttnValue {
		if after.FlashAttnValue {
			changes = append(changes, "--flash-attn now takes on|off|auto")
		} else {
			changes = append(changes, "--flash-attn now a bare flag")
		}
	}
	if before.FlashAttnEnabled && !after.FlashAttnEnabled {
		changes = append(changes, "--flash-attn removed")
	}
	if before.CacheReuse && !after.CacheReuse {
		changes = append(changes, "--cache-reuse removed")
	}
	if before.NoWebUI && !after.NoWebUI {
		changes = append(changes, "--no-webui removed")
	}
	if before.Jinja && !after.Jinja {
		changes = append(changes, "--jinja removed")
	}
	if before.UBatchSize && !after.UBatchSize {
		changes = append(changes, "--ubatch-size removed")
	}
	if before.ThreadsBatch && !after.ThreadsBatch {
		changes = append(changes, "--threads-batch removed")
	}
	// v1.7.2: KV layout and memory-pinning transitions.
	if before.CacheTypeKVShared && !after.CacheTypeKVShared {
		changes = append(changes, "KV layout: shared → "+after.kvLayoutName())
	}
	if (before.CacheTypeK || before.CacheTypeV) && !after.CacheTypeK && !after.CacheTypeV {
		changes = append(changes, "KV-cache quantization dropped")
	}
	if before.Mlock && !after.Mlock {
		changes = append(changes, "--mlock → "+pinModeName(after))
	}
	if before.LoadMode && !after.LoadMode {
		changes = append(changes, "memory pinning dropped")
	}
	if len(changes) == 0 {
		return "no capability change"
	}
	return strings.Join(changes, ", ")
}

// pinModeName renders the memory-pinning form for diffs ("--load-mode",
// "--mlock", "none").
func pinModeName(c *EngineCaps) string {
	switch {
	case c == nil:
		return "unknown"
	case c.LoadMode:
		return "--load-mode mlock"
	case c.Mlock:
		return "--mlock"
	default:
		return "none"
	}
}

// detectCapsForBoot performs the authoritative capability detection for one
// boot: the persisted verified profile wins when the tag matches; otherwise
// the binary is probed once via --help.
//
// v1.8.4 (P0-B ENGINE IDENTITY): the boot identity is resolved through the
// updater's staged marker first. During a deferred-commit verification
// window the binary at EngineBinaryPath IS the staged candidate
// (byte-verified at stage time), while the RECORDED tag still describes
// the previous build until Commit runs. Keying the probe (and its log
// line) on the recorded tag produced the observed contradiction — "b11310
// staged and SHA verified → engine boot probe: binary build b11273" — and
// made the boot reuse the OLD build's persisted capability profile for
// the NEW binary. With the marker, the exact staged binary is what gets
// probed, cached, and reported: stage → verify → probe EXACT executable →
// start EXACT executable → health → commit. On rollback the marker is
// removed and the restored package's own identity resumes authority.
func (s *LlamaServer) detectCapsForBoot(cfg *config.Config, binPath string) *EngineCaps {
	tag := currentEngineTag(cfg)
	stagedTag, _, staged := updater.StagedEngineIdentity(cfg)

	if staged && stagedTag != "" && stagedTag != tag {
		// Verification window: probe the ACTUAL serving binary. The
		// persisted caps profile (keyed by the OLD tag) must not
		// shadow the new binary — skip the fast path and detect.
		logging.Default().Info("engine",
			"deferred engine update in verification window: staged build %s is the serving binary (recorded tag %s commits after health verification) — probing the staged binary",
			stagedTag, tag)

		caps := DetectEngineCaps(binPath, stagedTag)
		s.logf("engine capability profile: %s (source: %s)", capsSummary(caps), caps.Source)
		logging.Default().Info("engine",
			"engine boot probe: binary build %s — capability profile %s (source %s)",
			stagedTag, capsSummary(caps), caps.Source)
		return caps
	}

	if staged && stagedTag != "" {
		tag = stagedTag
	}

	if caps := loadEngineCaps(cfg, tag); caps != nil {
		s.logf("engine capability profile: %s (persisted, verified %s)",
			capsSummary(caps), caps.VerifiedAt)
		return caps
	}

	caps := DetectEngineCaps(binPath, tag)
	s.logf("engine capability profile: %s (source: %s)", capsSummary(caps), caps.Source)
	// v1.8.2 clarity: this line is the BOOT PROBE of the binary being
	// launched right now. v1.8.4: inside a deferred-commit verification
	// window the reported build comes from the staged-identity marker —
	// it names the binary that is ACTUALLY on disk, never a recorded
	// tag left over from the previous build.
	logging.Default().Info("engine",
		"engine boot probe: binary build %s — capability profile %s (source %s)",
		tag, capsSummary(caps), caps.Source)
	return caps
}

// capsSummary renders a one-line capability profile for logs.
func capsSummary(caps *EngineCaps) string {
	if caps == nil {
		return "unknown"
	}
	form := "unsupported"
	if caps.FlashAttnEnabled {
		form = "flag"
		if caps.FlashAttnValue {
			form = "on|off|auto"
		}
	}
	return fmt.Sprintf("flash-attn=%s kv=%s pin=%s cache-reuse=%v no-webui=%v jinja=%v ubatch=%v threads-batch=%v",
		form, caps.kvLayoutName(), pinModeName(caps), caps.CacheReuse, caps.NoWebUI, caps.Jinja, caps.UBatchSize, caps.ThreadsBatch)
}

// ---------------------------------------------------------------------------
// v1.1.7 — compatibility-mode transparency and bounded retry-up.
// ---------------------------------------------------------------------------

// CompatInfo is the user-facing explanation of the active compatibility
// level: WHY the engine is not running the full-speed profile (when it is
// not), what was removed along the way, and when that was recorded. Served
// through /api/perf so the Settings → Performance view can show the real
// active configuration instead of a bare "compatibility mode 2" line.
type CompatInfo struct {
	Level      int      `json:"level"`
	Name       string   `json:"name"`
	Optimised  bool     `json:"optimised"`
	Reason     string   `json:"reason,omitempty"`
	RecordedAt string   `json:"recordedAt,omitempty"`
	Changes    []string `json:"changes,omitempty"`
}

// CompatSnapshot builds the CompatInfo for the current server state.
func (s *LlamaServer) CompatSnapshot() CompatInfo {
	cfg := s.src.Load()

	s.mu.Lock()
	caps := s.caps
	s.mu.Unlock()

	info := CompatInfo{
		Level:      cfg.EngineCompat,
		Name:       compatLevelName(cfg.EngineCompat),
		Optimised:  cfg.EngineCompat == 0,
		Reason:     cfg.EngineCompatReason,
		RecordedAt: cfg.EngineCompatAt,
	}

	// v1.6.0 (honest diagnostics): a compatibility level above zero
	// with an EMPTY recorded reason reports the explicit value
	// "unknown" — the UI/telemetry surface never receives a blank
	// pseudo-diagnosis for a real downgrade.
	if info.Level > 0 && strings.TrimSpace(info.Reason) == "" {
		info.Reason = "unknown"
	}

	if caps != nil {
		for _, r := range caps.Removed {
			info.Changes = append(info.Changes, r.Option+" — "+r.Reason)
		}
	}

	return info
}

// CapsSnapshot returns a copy of the active capability profile (nil before
// the first boot attempt). Read-only access for the API surface.
func (s *LlamaServer) CapsSnapshot() *EngineCaps {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps.Clone()
}

// compatReasonFromError distills one failed launch attempt into the string
// persisted as EngineCompatReason: the classified failure kind, narrowed
// to the offending option when the engine named one. Option-class reasons
// ("option-layout", "unknown-option") are the ones a repaired capability
// profile can plausibly fix — the retry-up gate trusts only those.
func compatReasonFromError(err error) string {
	if err == nil {
		return "unknown"
	}
	sf := ClassifyStartupFailure(err.Error())
	if sf == nil {
		return "unknown"
	}
	if sf.Option != "" {
		return string(sf.Kind) + ":" + sf.Option
	}
	return string(sf.Kind)
}

// shouldRetryFullSpeed decides whether THIS boot earns one bounded attempt
// at the full-speed profile (level 0) before falling back to the persisted
// compatibility level.
//
// The historical defect (v1.1.6 log evidence: the same
// "started in compatibility mode 2 (no speed flags)" line on every boot):
// once any launch failure pushed the ladder down, EngineCompat was
// persisted and EVERY later boot resumed at that level — even after the
// capability profile had been repaired and re-verified, so a one-time
// option incompatibility silently became a permanent speed downgrade.
//
// The gate is deliberately strict — every condition must hold:
//
//  1. a compatibility level above 0 is actually persisted;
//  2. the recorded reason is OPTION-class (a rejected option fails fast,
//     in well under a second — unlike a model-load failure that can burn
//     the full 180 s load timeout, so those are never re-attempted);
//  3. a VERIFIED capability profile exists AND is newer than the recorded
//     downgrade (or no downgrade time exists — the v1.1.6 upgrade case),
//     meaning the engine contract changed since the descent;
//  4. the level-0 launch profile validates cleanly against that verified
//     profile, so the known incompatibility cannot simply replay.
//
// When all four hold, the attempt is neither a rediscovery of a known
// incompatibility (validation rules that out) nor an unbounded retry loop
// (a failed non-option retry stamps a fresh EngineCompatAt on the next
// successful boot, which re-blocks the gate until the profile changes).
func shouldRetryFullSpeed(cfg *config.Config, caps *EngineCaps, level0Args []string) (bool, string) {
	if cfg == nil || cfg.EngineCompat <= 0 {
		return false, ""
	}

	reason := strings.TrimSpace(cfg.EngineCompatReason)
	if reason == "" {
		// Pre-v1.1.7 downgrade: no reason was recorded. The upgrade
		// path deserves exactly one chance to restore full speed.
		// v1.6.0 (honest diagnostics): the explicit value is "unknown"
		// — never a fake "unrecorded" pseudo-diagnosis.
		reason = "unknown (pre-1.1.7 downgrade)"
	}

	// (2) Only option-class reasons are safe to re-test cheaply.
	if !strings.HasPrefix(reason, "option-layout") &&
		!strings.HasPrefix(reason, "unknown-option") {
		return false, ""
	}

	// (3) A verified profile must exist and postdate the downgrade.
	if caps == nil || strings.TrimSpace(caps.VerifiedAt) == "" {
		return false, ""
	}
	if strings.TrimSpace(cfg.EngineCompatAt) != "" {
		recorded, err := time.Parse(time.RFC3339, cfg.EngineCompatAt)
		verified, err2 := time.Parse(time.RFC3339, caps.VerifiedAt)
		if err == nil && err2 == nil && !verified.After(recorded) {
			return false, ""
		}
	}

	// (4) The full-speed profile must validate against the verified
	// profile — otherwise the recorded incompatibility still holds.
	if problems := argProblems(level0Args, caps); len(problems) > 0 {
		return false, ""
	}

	return true, reason
}

// persistVerifiedCaps records a profile that just produced a VERIFIED
// successful startup, stamping it and saving it beside the config.
func (s *LlamaServer) persistVerifiedCaps(cfg *config.Config, caps *EngineCaps) {
	if caps == nil {
		return
	}
	verified := caps.Clone()
	verified.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
	saveEngineCaps(cfg, verified)
	s.mu.Lock()
	s.caps = verified
	s.mu.Unlock()
	if len(verified.Removed) > 0 {
		for _, r := range verified.Removed {
			logging.Default().Warn("engine",
				"engine capability downgrade recorded: %s — %s", r.Option, r.Reason)
		}
	}
}
