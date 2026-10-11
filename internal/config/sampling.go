// sampling.go — v1.6.1 P0: the authoritative sampling-argument contract.
//
// THE DEFECT (runtime evidence against v1.6.0): a repeatPenalty of 0
// reached the launch arguments as "--repeat-penalty 0" and the engine
// rejected it deterministically:
//
//      error while handling argument "--repeat-penalty":
//      repeat-penalty must be finite and greater than 0
//
// The failure was then classified as a repairable option-layout problem
// and re-fed through the compatibility ladder — four-plus engine spawns
// for a value no compatibility level can ever fix.
//
// This file is the single authority for what a sampling value must
// satisfy BEFORE any engine process starts. The rules mirror the
// llama.cpp argument parser's own rejection set, kept deliberately
// conservative: only values the engine's parser rejects (or that are
// never meaningful — NaN/Inf/negative-where-positive-is-required) are
// invalid. Valid user settings are never reshaped.
//
// Two operations are provided:
//
//   ValidateSamplingOptions — reports every violation with the field
//     name, the violated rule and the actionable fix (used by the API
//     patch layer to REJECT bad input, and by the engine boot gate to
//     refuse the launch with a classified diagnostic);
//
//   NormalizeSamplingOptions — safely repairs invalid values back to
//     the documented defaults, reporting each repair (used by config
//     Load/migration so a legacy broken config.json still boots).
package config

import (
        "fmt"
        "math"
        "strconv"
        "strings"
)

// SamplingProblem is one invalid sampling value: everything the user (or
// the log) needs to fix it.
type SamplingProblem struct {
        // Field is the configuration path ("llm.repeatPenalty").
        Field string
        // Option is the engine flag the value feeds ("--repeat-penalty").
        Option string
        // Got is the offending value as written.
        Got string
        // Rule states the violated constraint in one sentence.
        Rule string
        // Fix is the actionable remediation.
        Fix string
}

// String renders the actionable one-line diagnostic.
func (p SamplingProblem) String() string {
        return fmt.Sprintf("%s=%s is invalid: %s (%s) — %s",
                p.Field, p.Got, p.Rule, p.Option, p.Fix)
}

// floatRule describes one float field's engine-parser contract.
type floatRule struct {
        field string
        opt   string
        min   float64
        max   float64
        // noMin disables the lower bound entirely (any finite value passes —
        // the presence/frequency penalties accept any sign).
        noMin bool
        // hasMax enables the upper bound (a zero-value max means "no upper
        // bound" — the historical bug where an unset max rejected everything).
        hasMax bool
        // minExclusive refines the lower bound when true.
        minExclusive bool
        rule         string
        fix          string
        defaultVal   float64
}

// valid reports whether v satisfies the rule.
func (r floatRule) valid(v float64) bool {
        if math.IsNaN(v) || math.IsInf(v, 0) {
                return false
        }
        if !r.noMin {
                if r.minExclusive {
                        if v <= r.min {
                                return false
                        }
                } else if v < r.min {
                        return false
                }
        }
        if r.hasMax && v > r.max {
                return false
        }
        return true
}

// describeRule renders the human rule including the exact bound text.
func (r floatRule) describeRule() string {
        return r.rule
}

// intRule describes one integer field's engine-parser contract.
type intRule struct {
        field  string
        opt    string
        min    int
        minExc bool
        rule   string
        fix    string
}

func (r intRule) valid(v int) bool {
        if r.minExc {
                return v > r.min
        }
        return v >= r.min
}

// samplingFloatRules are the float sampling fields. Bounds mirror the
// llama.cpp argument parser:
//
//   --repeat-penalty   must be finite and > 0 (the v1.6.1 defect)
//   --temp             finite, >= 0
//   --top-p            finite, [0, 1]
//   --min-p            finite, [0, 1] (0 = unset — never emitted)
//   --presence-penalty finite (any sign is valid)
//   --frequency-penalty finite (any sign is valid)
//   --mirostat-tau     finite, > 0 when emitted
//   --mirostat-eta     finite, > 0 when emitted
var samplingFloatRules = []floatRule{
        {
                field: "llm.repeatPenalty", opt: "--repeat-penalty",
                min: 0, minExclusive: true,
                rule:       "repeat-penalty must be finite and greater than 0 (1.0 disables the penalty)",
                fix:        "set a positive value, e.g. 1.1 (Settings → Sampling → Repeat penalty, or llm.repeatPenalty in config.json)",
                defaultVal: 1.1,
        },
        {
                field: "llm.temperature", opt: "--temp",
                min: 0,
                rule:       "temperature must be a finite number of at least 0",
                fix:        "set a non-negative value, e.g. 0.7 (Settings → Sampling → Temperature)",
                defaultVal: 0.7,
        },
        {
                field: "llm.topP", opt: "--top-p",
                min: 0, max: 1, hasMax: true,
                rule:       "top-p must be a finite number between 0 and 1",
                fix:        "set a value in [0, 1], e.g. 0.95 (Settings → Sampling → Top P)",
                defaultVal: 0.95,
        },
        {
                field: "llm.minP", opt: "--min-p",
                min: 0, max: 1, hasMax: true,
                rule:       "min-p must be a finite number between 0 and 1 (0 leaves it unset)",
                fix:        "set a value in [0, 1], e.g. 0.05 (Settings → Sampling → Min P)",
                defaultVal: 0,
        },
        {
                field: "llm.presencePenalty", opt: "--presence-penalty",
                noMin: true,
                rule:       "presence penalty must be a finite number (0 leaves it unset)",
                fix:        "set a finite value or 0 to unset (Settings → Sampling)",
                defaultVal: 0,
        },
        {
                field: "llm.frequencyPenalty", opt: "--frequency-penalty",
                noMin: true,
                rule:       "frequency penalty must be a finite number (0 leaves it unset)",
                fix:        "set a finite value or 0 to unset (Settings → Sampling)",
                defaultVal: 0,
        },
        {
                field: "llm.mirostatTau", opt: "--mirostat-tau",
                min: 0,
                rule:       "mirostat-tau must be a finite number of at least 0 (0 leaves it unset)",
                fix:        "set a non-negative value, e.g. 5.0, or 0 to unset (Settings → Sampling → Mirostat)",
                defaultVal: 0,
        },
        {
                field: "llm.mirostatEta", opt: "--mirostat-eta",
                min: 0,
                rule:       "mirostat-eta must be a finite number of at least 0 (0 leaves it unset)",
                fix:        "set a non-negative value, e.g. 0.1, or 0 to unset (Settings → Sampling → Mirostat)",
                defaultVal: 0,
        },
}

// samplingIntRules are the integer sampling fields the engine parses.
var samplingIntRules = []intRule{
        {
                field: "llm.topK", opt: "--top-k",
                min: 0,
                rule: "top-k must be at least 0",
                fix:  "set a non-negative value, e.g. 40 (Settings → Sampling → Top K)",
        },
        {
                field: "llm.repeatLastN", opt: "--repeat-last-n",
                min: 0,
                rule: "repeat-last-n must be at least 0 (0 leaves it unset)",
                fix:  "set a non-negative value or 0 to unset (Settings → Sampling)",
        },
        {
                field: "llm.numCtx", opt: "--ctx-size",
                min: 0, minExc: true,
                rule: "context size must be a positive number of tokens",
                fix:  "set a positive value, e.g. 8192 (Settings → Performance → Context)",
        },
        {
                field: "llm.numBatch", opt: "--batch-size",
                min: 0, minExc: true,
                rule: "batch size must be a positive number",
                fix:  "set a positive value, e.g. 512 (Settings → Performance)",
        },
        // v1.6.2 audit: max tokens / prediction length is REQUEST-side
        // (n_predict on the completions API, never a launch flag), but the
        // same deterministic contract applies: -1 is the documented
        // "unlimited" and anything below it is never meaningful.
        {
                field: "llm.maxTokens", opt: "n_predict",
                min: -1,
                rule: "max tokens must be -1 (unlimited) or a non-negative count",
                fix:  "set -1 for unlimited or a positive count, e.g. 1024 (Settings → Sampling → Max tokens)",
        },
}

// renderFloat renders a float the way the launch arguments would (the
// engine sees the %v form).
func renderFloat(v float64) string {
        return fmt.Sprintf("%v", v)
}

// ValidateSamplingOptions returns every parser-invalid sampling value in
// the given options, with actionable diagnostics. Empty result = valid.
func ValidateSamplingOptions(o LLMOptions) []SamplingProblem {
        var problems []SamplingProblem

        for _, r := range samplingFloatRules {
                var v float64
                switch r.field {
                case "llm.repeatPenalty":
                        v = o.RepeatPenalty
                case "llm.temperature":
                        v = o.Temperature
                case "llm.topP":
                        v = o.TopP
                case "llm.minP":
                        v = o.MinP
                case "llm.presencePenalty":
                        v = o.PresencePenalty
                case "llm.frequencyPenalty":
                        v = o.FrequencyPenalty
                case "llm.mirostatTau":
                        v = o.MirostatTau
                case "llm.mirostatEta":
                        v = o.MirostatEta
                }

                if math.IsNaN(v) || math.IsInf(v, 0) {
                        problems = append(problems, SamplingProblem{
                                Field: r.field, Option: r.opt, Got: renderFloat(v),
                                Rule: r.describeRule(),
                                Fix:  r.fix,
                        })
                        continue
                }

                if !r.valid(v) {
                        problems = append(problems, SamplingProblem{
                                Field: r.field, Option: r.opt, Got: renderFloat(v),
                                Rule: r.describeRule(),
                                Fix:  r.fix,
                        })
                }
        }

        for _, r := range samplingIntRules {
                var v int
                switch r.field {
                case "llm.topK":
                        v = o.TopK
                case "llm.repeatLastN":
                        v = o.RepeatLastN
                case "llm.numCtx":
                        v = o.NumCtx
                case "llm.numBatch":
                        v = o.NumBatch
                case "llm.maxTokens":
                        v = o.MaxTokens
                }

                if !r.valid(v) {
                        problems = append(problems, SamplingProblem{
                                Field: r.field, Option: r.opt, Got: fmt.Sprintf("%d", v),
                                Rule: r.rule,
                                Fix:  r.fix,
                        })
                }
        }

        // Mirostat mode: the engine implements 0 (off), 1 and 2.
        if o.Mirostat != 0 && o.Mirostat != 1 && o.Mirostat != 2 {
                problems = append(problems, SamplingProblem{
                        Field: "llm.mirostat", Option: "--mirostat",
                        Got:   fmt.Sprintf("%d", o.Mirostat),
                        Rule:  "mirostat mode must be 0 (off), 1 or 2",
                        Fix:   "set 0, 1 or 2 (Settings → Sampling → Mirostat)",
                })
        }

        return problems
}

// NormalizeSamplingOptions safely repairs every parser-invalid sampling
// value back to its documented default and returns one report line per
// repair. Valid values — including unusual-but-valid user settings — are
// preserved verbatim.
func NormalizeSamplingOptions(o *LLMOptions) []string {
        if o == nil {
                return nil
        }

        var notes []string

        for _, r := range samplingFloatRules {
                var v float64
                switch r.field {
                case "llm.repeatPenalty":
                        v = o.RepeatPenalty
                case "llm.temperature":
                        v = o.Temperature
                case "llm.topP":
                        v = o.TopP
                case "llm.minP":
                        v = o.MinP
                case "llm.presencePenalty":
                        v = o.PresencePenalty
                case "llm.frequencyPenalty":
                        v = o.FrequencyPenalty
                case "llm.mirostatTau":
                        v = o.MirostatTau
                case "llm.mirostatEta":
                        v = o.MirostatEta
                default:
                        continue
                }

                if !r.valid(v) {
                        notes = append(notes, fmt.Sprintf(
                                "%s=%s rejected (%s) — reset to %v",
                                r.field, renderFloat(v), r.rule, r.defaultVal,
                        ))

                        switch r.field {
                        case "llm.repeatPenalty":
                                o.RepeatPenalty = r.defaultVal
                        case "llm.temperature":
                                o.Temperature = r.defaultVal
                        case "llm.topP":
                                o.TopP = r.defaultVal
                        case "llm.minP":
                                o.MinP = r.defaultVal
                        case "llm.presencePenalty":
                                o.PresencePenalty = r.defaultVal
                        case "llm.frequencyPenalty":
                                o.FrequencyPenalty = r.defaultVal
                        case "llm.mirostatTau":
                                o.MirostatTau = r.defaultVal
                        case "llm.mirostatEta":
                                o.MirostatEta = r.defaultVal
                        }
                }
        }

        def := Default()

        for _, r := range samplingIntRules {
                v := 0
                switch r.field {
                case "llm.topK":
                        v = o.TopK
                case "llm.repeatLastN":
                        v = o.RepeatLastN
                case "llm.numCtx":
                        v = o.NumCtx
                case "llm.numBatch":
                        v = o.NumBatch
                case "llm.maxTokens":
                        v = o.MaxTokens
                }

                if !r.valid(v) {
                        notes = append(notes, fmt.Sprintf(
                                "%s=%d rejected (%s) — reset to the documented default",
                                r.field, v, r.rule,
                        ))

                        switch r.field {
                        case "llm.topK":
                                o.TopK = def.LLM.TopK
                        case "llm.repeatLastN":
                                o.RepeatLastN = def.LLM.RepeatLastN
                        case "llm.numCtx":
                                o.NumCtx = def.LLM.NumCtx
                        case "llm.numBatch":
                                o.NumBatch = def.LLM.NumBatch
                        case "llm.maxTokens":
                                o.MaxTokens = def.LLM.MaxTokens
                        }
                }
        }

        if o.Mirostat != 0 && o.Mirostat != 1 && o.Mirostat != 2 {
                notes = append(notes, fmt.Sprintf(
                        "llm.mirostat=%d rejected (mirostat mode must be 0, 1 or 2) — reset to 0",
                        o.Mirostat,
                ))
                o.Mirostat = 0
        }

        return notes
}

// SamplingProblemsError renders a problem list as one classified,
// actionable error (the API layer rejects bad patches with it).
type SamplingProblemsError struct {
        Problems []SamplingProblem
}

func (e *SamplingProblemsError) Error() string {
        out := "invalid sampling configuration (rejected before any engine start):"

        for _, p := range e.Problems {
                out += "\n  " + p.String()
        }

        return out
}

// extraArgNumericRules are the llama.cpp launch flags that take a NUMERIC
// value and whose values SHEYTAN can validate deterministically BEFORE
// the process spawns. The v1.6.2 audit of every argument actually emitted
// by buildArgsWithCaps and related paths found the remaining gap: the raw
// user extra arguments (llamaExtraArgs) were passed through verbatim, so a
// malformed numeric value ("--temp abc", "--top-k 1e", "--temp nan")
// reached the engine's own parser, failed deterministically, and fed the
// compatibility/repair machinery — the exact storm shape the v1.6.1 gate
// closed for first-class fields.
//
// The rule set is deliberately CONSERVATIVE: only flags whose numeric
// contract is known are linted (they are the flags the launcher itself
// emits); unknown flags pass through untouched — the engine's own parser
// stays the authority for everything else, and valid user values are
// never reshaped.
type extraArgRule struct {
        integer bool
        // range-checks reuse the float rule contract where the flag is one
        // of the managed sampling options (nil = numeric-parse check only)
        rule *floatRule
}

var extraArgNumericRules = map[string]extraArgRule{
        // Managed sampling options (full range contract).
        "--temp": {rule: &floatRule{
                field: "llamaExtraArgs --temp", opt: "--temp", min: 0,
                rule: "temperature must be a finite number of at least 0",
                fix:  "pass a non-negative number, e.g. --temp 0.7",
        }},
        "--top-p": {rule: &floatRule{
                field: "llamaExtraArgs --top-p", opt: "--top-p", min: 0, max: 1, hasMax: true,
                rule: "top-p must be a finite number between 0 and 1",
                fix:  "pass a value in [0, 1], e.g. --top-p 0.95",
        }},
        "--min-p": {rule: &floatRule{
                field: "llamaExtraArgs --min-p", opt: "--min-p", min: 0, max: 1, hasMax: true,
                rule: "min-p must be a finite number between 0 and 1",
                fix:  "pass a value in [0, 1], e.g. --min-p 0.05",
        }},
        "--repeat-penalty": {rule: &floatRule{
                field: "llamaExtraArgs --repeat-penalty", opt: "--repeat-penalty", min: 0, minExclusive: true,
                rule: "repeat-penalty must be finite and greater than 0 (1.0 disables the penalty)",
                fix:  "pass a positive value, e.g. --repeat-penalty 1.1",
        }},
        "--presence-penalty": {rule: &floatRule{
                field: "llamaExtraArgs --presence-penalty", opt: "--presence-penalty", noMin: true,
                rule: "presence penalty must be a finite number",
                fix:  "pass a finite number, e.g. --presence-penalty 0.5",
        }},
        "--frequency-penalty": {rule: &floatRule{
                field: "llamaExtraArgs --frequency-penalty", opt: "--frequency-penalty", noMin: true,
                rule: "frequency penalty must be a finite number",
                fix:  "pass a finite number, e.g. --frequency-penalty 0.5",
        }},
        "--mirostat-tau": {rule: &floatRule{
                field: "llamaExtraArgs --mirostat-tau", opt: "--mirostat-tau", min: 0,
                rule: "mirostat-tau must be a finite number of at least 0",
                fix:  "pass a non-negative number, e.g. --mirostat-tau 5.0",
        }},
        "--mirostat-eta": {rule: &floatRule{
                field: "llamaExtraArgs --mirostat-eta", opt: "--mirostat-eta", min: 0,
                rule: "mirostat-eta must be a finite number of at least 0",
                fix:  "pass a non-negative number, e.g. --mirostat-eta 0.1",
        }},

        // Integer-valued launch options the launcher itself emits or that
        // have an unambiguous integer contract (numeric-parse check; the
        // engine's parser rejects non-integers deterministically).
        "--top-k":         {integer: true},
        "--repeat-last-n": {integer: true},
        "--mirostat":      {integer: true},
        "--ctx-size":      {integer: true},
        "--batch-size":    {integer: true},
        "--ubatch-size":   {integer: true},
        "--threads":       {integer: true},
        "--threads-batch": {integer: true},
        "--n-gpu-layers":  {integer: true},
        "--seed":          {integer: true},
        "--cache-reuse":   {integer: true},
        "--n-predict":     {integer: true},
        "--draft-max":     {integer: true},
        "--port":          {integer: true},
}

// ValidateExtraArgs lints the raw user extra-argument string
// (llamaExtraArgs) against the known numeric launch-flag contract.
// It returns one SamplingProblem per violation; empty means the string
// is safe to hand to the engine as far as SHEYTAN can determine
// deterministically. Both "--flag value" and "--flag=value" forms are
// handled; unknown flags are not linted (the engine's parser decides).
func ValidateExtraArgs(extra string) []SamplingProblem {
        tokens := strings.Fields(extra)

        var problems []SamplingProblem

        for i := 0; i < len(tokens); i++ {
                flag := tokens[i]

                rule, known := extraArgNumericRules[flag]
                value := ""
                hasValue := false

                if !known {
                        // "--flag=value" form of a known numeric flag.
                        if eq := strings.Index(flag, "="); eq > 2 {
                                if r, k := extraArgNumericRules[flag[:eq]]; k {
                                        rule, known = r, true
                                        value, hasValue = flag[eq+1:], true
                                }
                        }
                        if !known {
                                continue // not ours to judge — engine authority
                        }
                } else if i+1 < len(tokens) {
                        value, hasValue = tokens[i+1], true
                        i++ // consumed the value token
                }

                if !hasValue || strings.TrimSpace(value) == "" {
                        problems = append(problems, SamplingProblem{
                                Field: "llamaExtraArgs",
                                Option: flag,
                                Got:   value,
                                Rule:  "the numeric option is missing its value",
                                Fix:   fmt.Sprintf("pass a value, e.g. %s <number> (Settings → Advanced → Extra engine arguments)", flag),
                        })
                        continue
                }

                if rule.integer {
                        if _, err := strconv.Atoi(strings.TrimSpace(value)); err != nil {
                                problems = append(problems, SamplingProblem{
                                        Field: "llamaExtraArgs",
                                        Option: flag,
                                        Got:   value,
                                        Rule:  "the option requires an integer value",
                                        Fix:   fmt.Sprintf("pass an integer, e.g. %s 512 (Settings → Advanced → Extra engine arguments)", flag),
                                })
                        }
                        continue
                }

                v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
                if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
                        problems = append(problems, SamplingProblem{
                                Field: "llamaExtraArgs",
                                Option: flag,
                                Got:   value,
                                Rule:  "the option requires a finite number",
                                Fix:   fmt.Sprintf("pass a finite number, e.g. %s 0.8 (Settings → Advanced → Extra engine arguments)", flag),
                        })
                        continue
                }

                if rule.rule != nil && !rule.rule.valid(v) {
                        problems = append(problems, SamplingProblem{
                                Field: "llamaExtraArgs",
                                Option: flag,
                                Got:   value,
                                Rule:  rule.rule.rule,
                                Fix:   rule.rule.fix,
                        })
                }
        }

        return problems
}
