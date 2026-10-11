// Package taskclassify implements the v1.2.5 adaptive context tiers.
//
// The v1.2.4 orchestrator built ONE context shape for every request: the
// full ~7.4K-token briefing, every tool schema, recall, project card and
// skills — composed BEFORE the engine was ever called, so even "hi" paid a
// ~10K-token prefill. This package replaces that with a measured decision:
//
//      classify the task → choose a tier (FAST/STANDARD/DEEP/MAX) from the
//      task profile AND the machine's real resources → build only what the
//      tier allows → escalate ONLY when run evidence requires it.
//
// Everything here is pure math over inputs — no I/O, no goroutines — so the
// tier decision is deterministic and unit-testable. The orchestrator feeds
// it the measured values (effective context, RAM, vision payload, tool
// requirements) and owns the escalation evidence.
package taskclassify

import (
        "strings"
)

// Kind is the coarse task class the classifier assigns.
type Kind string

const (
        KindChat         Kind = "chat"         // casual conversation, greetings
        KindQuestion     Kind = "question"     // factual question, explanation
        KindCoding       Kind = "coding"       // bug fix, implementation, refactor
        KindArchitecture Kind = "architecture" // project-wide design/analysis
        KindResearch     Kind = "research"     // web/external research
        KindVision       Kind = "vision"       // image understanding
        KindSystem       Kind = "system"       // system diagnosis/config
        KindData         Kind = "data"         // data analysis/charts
)

// Signals are the boolean capability needs detected in the request.
type Signals struct {
        NeedsFiles       bool // references files/paths in a coding context
        NeedsTerminal    bool // build/run/execute commands
        NeedsTests       bool // testing/verification vocabulary
        NeedsResearch    bool // web/research vocabulary
        NeedsVision      bool // images staged or vision vocabulary
        NeedsMemory      bool // remember/recall vocabulary
        NeedsDepth       bool // architecture/analysis/multi-step vocabulary
        NeedsRepoContext bool // "this project", "the codebase", relative refs

        // SelfDescribe (v1.8.2) is the deterministic capability-intent
        // signal: the user asks what the runtime itself has — its tools,
        // capabilities, access or current model ("what tools do you have?",
        // "what can you do?", "what model are you running?"). The
        // orchestrator answers it from the EXISTING registry/capability
        // authorities (one shared self-model builder) — never web research,
        // never blind historical recall. Detection is vocabulary-based and
        // deterministic, mirroring every other signal here.
        SelfDescribe bool
}

// Profile is the classification result for one incoming request.
type Profile struct {
        Kind       Kind
        Complexity int // 0..100 — drives the tier ladder
        Signals    Signals
        Reason     string // human-readable primary evidence, for logs/UI
}

// ClassifyOptions carry the request facts the classifier cannot derive
// from text alone.
type ClassifyOptions struct {
        // HasImages is true when image attachments ride the request.
        HasImages bool

        // AttachmentCount is the number of staged attachments.
        AttachmentCount int

        // AttachmentBytes is the total staged attachment size.
        AttachmentBytes int64

        // HistoryMessages is the conversation length so far.
        HistoryMessages int

        // UserDepth is the explicit thinking control from the composer:
        // "fast" | "auto" | "thinking" ("" = auto/unset).
        UserDepth string
}

// keyword tables — additive signal scoring, mirroring toolsets.TaskSignals
// style so behaviour stays explainable.
type signalRule struct {
        words []string
        set   func(*Signals)
        score int
}

var signalRules = []signalRule{
        {[]string{"fix", "bug", "error", "compile", "build", "refactor", "implement", "function", "class", "method", "debug", "stack trace", "traceback", "warning", "crash"},
                func(s *Signals) { s.NeedsFiles = true }, 14},
        {[]string{"test", "tests", "unittest", "unit test", "verify", "validate", "assert", "coverage", "lint"},
                func(s *Signals) { s.NeedsTests = true }, 10},
        {[]string{"run", "execute", "command", "terminal", "shell", "install", "script", "make", "npm", "cargo", "go run", "docker"},
                func(s *Signals) { s.NeedsTerminal = true }, 8},
        {[]string{"search", "research", "latest", "news", "look up", "find online", "web", "internet", "documentation online", "compare sources"},
                func(s *Signals) { s.NeedsResearch = true }, 12},
        {[]string{"image", "picture", "photo", "screenshot", "diagram", "look at this", "what do you see", "analyze this image"},
                func(s *Signals) { s.NeedsVision = true }, 10},
        {[]string{"remember", "recall", "preference", "my name is", "don't forget"},
                func(s *Signals) { s.NeedsMemory = true }, 4},
        // v1.2.5: references to the conversation's own past — the signal for
        // targeted history retrieval (recall digests) rather than blind chat.
        {[]string{"earlier", "previously", "what did we discuss", "what did you say", "you mentioned", "remember when", "as we discussed", "last time you"},
                func(s *Signals) { s.NeedsDepth = true }, 14},
        {[]string{"design", "architecture", "trade-off", "tradeoff", "analyze the codebase", "whole project", "this project", "the codebase", "our repo", "repository", "migrate", "plan", "roadmap", "step by step", "deep dive", "review"},
                func(s *Signals) { s.NeedsDepth = true }, 16},
        {[]string{"csv", "json data", "dataset", "statistics", "chart", "plot", "analyze data", "dataframe", "spreadsheet"},
                func(s *Signals) {}, 8}, // data complexity bump (kind decided below)
        // v1.8.2: capability/self-description intent. Phrases are matched
        // with strings.Contains against the lowercased request, exactly like
        // every other signal table. No complexity score here — the signal
        // changes WHAT the orchestrator composes (the runtime self-model
        // block), not how deep the tier ladder goes.
        {selfDescribePhrases(),
                func(s *Signals) { s.SelfDescribe = true }, 0},
}

// selfDescribePhrases is the deterministic capability-intent vocabulary
// (v1.8.2). Lowercase; matched with strings.Contains.
func selfDescribePhrases() []string {
        return []string{
        "what tools do you have",
        "which tools do you have",
        "what tools do you have access",
        "your tools",
        "your available tools",
        "your capabilities",
        "what capabilities",
        "which capabilities",
        "what can you do",
        "what all can you do",
        "what can you access",
        "what do you have access to",
        "what are you able to do",
        "what model are you",
        "which model are you",
        "what model are you running",
        "which model are you running",
        "what are you running",
        "what are you running on",
        "list your tools",
        "list your capabilities",
        "show your tools",
        "what functions do you have",
        "what features do you have",
        "what do you support",
        "what hardware are you",
        "what engine are you",
        "what backend are you",
        }
}

// Classify derives the task profile from the request text plus the
// request facts. Pure function; deterministic.
func Classify(query string, opts ClassifyOptions) Profile {
        q := strings.ToLower(query)
        words := strings.Fields(q)

        p := Profile{Kind: KindChat, Complexity: 8}

        if len(words) == 0 && opts.AttachmentCount == 0 {
                p.Reason = "empty request"
                return p
        }

        // Signal scoring.
        for _, rule := range signalRules {
                for _, w := range rule.words {
                        if strings.Contains(q, w) {
                                rule.set(&p.Signals)
                                p.Complexity += rule.score
                                break
                        }
                }
        }

        // Long, structured requests are inherently more complex.
        switch {
        case len(words) >= 120:
                p.Complexity += 22
        case len(words) >= 60:
                p.Complexity += 12
        case len(words) >= 25:
                p.Complexity += 5
        }

        // Attachment pressure.
        if opts.AttachmentCount > 0 {
                p.Complexity += 8 + opts.AttachmentCount*2
                if p.Complexity > 100 {
                        p.Complexity = 100
                }
        }

        if opts.HasImages {
                p.Signals.NeedsVision = true
                p.Complexity += 12
        }

        // Kind resolution — most specific first.
        switch {
        case p.Signals.NeedsVision && (opts.HasImages || strings.Contains(q, "image") || strings.Contains(q, "screenshot") || strings.Contains(q, "photo")):
                p.Kind = KindVision
        case p.Signals.NeedsResearch && (p.Signals.NeedsDepth || len(words) > 12):
                p.Kind = KindResearch
        case p.Signals.NeedsDepth && (p.Signals.NeedsFiles || strings.Contains(q, "codebase") || strings.Contains(q, "project") || strings.Contains(q, "architecture")):
                p.Kind = KindArchitecture
        case p.Signals.NeedsFiles || p.Signals.NeedsTerminal || p.Signals.NeedsTests:
                p.Kind = KindCoding
        case strings.Contains(q, "csv") || strings.Contains(q, "dataset") || strings.Contains(q, "chart") || strings.Contains(q, "statistics"):
                p.Kind = KindData
        case p.Signals.NeedsResearch:
                p.Kind = KindResearch
        case isSystemQuery(q):
                p.Kind = KindSystem
        case isQuestion(q):
                p.Kind = KindQuestion
        }

        // Kind complexity floors — the class itself carries weight: an
        // architecture analysis is inherently deeper than a question even
        // before keyword scoring runs.
        p.Complexity += kindWeight(p.Kind)

        // Long conversation with references — targeted history retrieval.
        if opts.HistoryMessages >= 12 {
                p.Complexity += 6
        }

        // Explicit user depth control shifts complexity (v1.2.6: thinking
        // levels no longer add complexity — reasoning depth is NOT context
        // size; the thinking nudge + the evidence-driven escalation ladder
        // handle depth, and the measured v1.2.5 over-injection (13K tokens for
        // a one-line question) came exactly from this double penalty).
        // v1.8.5: the four-level ladder — "low" (and legacy "fast") keeps the
        // latency-first posture; mid is neutral; high/ultra stay complexity-
        // neutral (their depth comes from the real reasoning budget, not from
        // context inflation).
        switch strings.ToLower(strings.TrimSpace(opts.UserDepth)) {
        case "fast", "low":
                p.Complexity -= 25
        }

        if p.Complexity < 0 {
                p.Complexity = 0
        }

        if p.Complexity > 100 {
                p.Complexity = 100
        }

        p.Reason = classifyReason(p, words)

        return p
}

// kindWeight is the per-kind complexity floor contribution.
func kindWeight(k Kind) int {
        switch k {
        case KindArchitecture:
                return 26
        case KindResearch:
                return 10
        case KindCoding, KindSystem:
                return 4
        case KindData:
                return 2
        default:
                return 0
        }
}

func isQuestion(q string) bool {
        q = strings.TrimSpace(q)
        if q == "" {
                return false
        }

        // Terminal question marks (ASCII and full-width) both count.
        if strings.HasSuffix(q, "?") || strings.HasSuffix(q, "？") {
                return true
        }

        for _, starter := range []string{"what", "why", "how", "when", "where", "who", "which", "explain", "can you", "could you", "is it", "does it", "tell me"} {
                if strings.HasPrefix(q, starter+" ") || strings.HasPrefix(q, starter+",") {
                        return true
                }
        }

        return false
}

func isSystemQuery(q string) bool {
        for _, w := range []string{"system", "cpu", "gpu", "ram", "memory usage", "disk", "driver", "startup", "performance of the app", "diagnose", "benchmark the machine", "os version"} {
                if strings.Contains(q, w) {
                        return true
                }
        }

        return false
}

func classifyReason(p Profile, words []string) string {
        var parts []string

        if p.Kind != KindChat {
                parts = append(parts, "kind="+string(p.Kind))
        }

        if p.Signals.NeedsFiles {
                parts = append(parts, "file/code signals")
        }

        if p.Signals.NeedsResearch {
                parts = append(parts, "research signals")
        }

        if p.Signals.NeedsVision {
                parts = append(parts, "vision payload")
        }

        if p.Signals.NeedsDepth {
                parts = append(parts, "depth signals")
        }

        if p.Signals.SelfDescribe {
                parts = append(parts, "capability/self-description intent")
        }

        if len(parts) == 0 {
                parts = append(parts, "no capability signals — conversational")
        }

        parts = append(parts, plural("word", len(words)))

        return strings.Join(parts, ", ")
}

func plural(noun string, n int) string {
        if n == 1 {
                return "1 " + noun
        }

        return itoa(n) + " " + noun + "s"
}

func itoa(n int) string {
        if n == 0 {
                return "0"
        }

        neg := n < 0
        if neg {
                n = -n
        }

        var buf [20]byte
        i := len(buf)

        for n > 0 {
                i--
                buf[i] = byte('0' + n%10)
                n /= 10
        }

        if neg {
                i--
                buf[i] = '-'
        }

        return string(buf[i:])
}
