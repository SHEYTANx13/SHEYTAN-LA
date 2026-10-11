// Package continuum is SHEYTAN's v1.0.7 answer to the context-window wall:
// "almost unlimited context" through chapter rollover.
//
// A long conversation becomes a THREAD of chained CHAPTER sessions. After
// every completed turn the app estimates the history pressure; when it nears
// the budget, the engine:
//
//  1. DISTILLS the finished chapter into a living Framework — a structured,
//     token-budgeted state snapshot (mission, durable facts, decisions,
//     open threads, artifacts, user preferences, rolling summary).
//  2. CREATES the next chapter session in the background, seeded with the
//     rendered Framework briefing + the most recent messages, linked into
//     the thread chain (ThreadID / ParentID / Chapter).
//  3. SWAPS the active session — the user sees one unbroken conversation
//     with a subtle "chapter extended" divider, while every chapter stays
//     small enough to prefill instantly.
//
// The base distiller is deterministic, pure Go and offline-safe (microseconds,
// no LLM call). When an engine is available, Enhance() refines the Framework
// with an LLM pass in the background — extractive first, smarter moments
// later; a failed or slow enhancement never blocks or degrades the rollover.
package continuum

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// BriefingHeader prefixes every rendered Framework briefing message so the
// UI (chapter dividers) and future distillation passes can recognize and
// supersede old briefings cheaply.
const BriefingHeader = "[CONTINUUM FRAMEWORK"

// Framework version — bumped when the schema changes incompatibly.
const FrameworkVersion = 1

// Section caps: the Framework must stay prompt-injectable in bulk, so every
// list is bounded. Older items fall off the back (newest first).
const (
	maxMissionChars = 220
	maxItemChars    = 200
	maxFacts        = 24
	maxDecisions    = 12
	maxOpenThreads  = 8
	maxArtifacts    = 16
	maxPreferences  = 8
	maxSummaryChars = 900
)

// Framework is the distilled, evolving state of one conversation thread.
// It is deliberately compact: everything here is rendered into the briefing
// of every new chapter within a token budget.
type Framework struct {
	Version     int       `json:"version"`
	Mission     string    `json:"mission,omitempty"`
	Facts       []string  `json:"facts,omitempty"`
	Decisions   []string  `json:"decisions,omitempty"`
	OpenThreads []string  `json:"openThreads,omitempty"`
	Artifacts   []string  `json:"artifacts,omitempty"`
	Preferences []string  `json:"preferences,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Chapters    int       `json:"chapters"` // chapters created so far in the thread
	UpdatedAt   time.Time `json:"updatedAt"`
}

// NewFramework returns an empty Framework with the current version stamp.
func NewFramework() *Framework {
	return &Framework{Version: FrameworkVersion}
}

// Empty reports whether the framework carries no signal at all.
func (f *Framework) Empty() bool {
	return f == nil || (f.Mission == "" && len(f.Facts) == 0 && len(f.Decisions) == 0 &&
		len(f.OpenThreads) == 0 && len(f.Artifacts) == 0 && len(f.Preferences) == 0 && f.Summary == "")
}

// FactCount returns the total number of stored items (UI stat).
func (f *Framework) FactCount() int {
	if f == nil {
		return 0
	}
	return len(f.Facts) + len(f.Decisions) + len(f.OpenThreads) + len(f.Artifacts) + len(f.Preferences)
}

// ---------------------------------------------------------------------------
// Distillation — extractive, deterministic, offline
// ---------------------------------------------------------------------------

// Distill folds one chapter's messages into the framework. It never shrinks
// existing state except through the caps — knowledge is monotonically
// accumulated; refinement is the LLM pass's job (Enhance).
func Distill(fw *Framework, msgs []llm.Message) *Framework {
	if fw == nil {
		fw = NewFramework()
	}
	if fw.Version == 0 {
		fw.Version = FrameworkVersion
	}

	var newFacts, newDecisions, newThreads, newArts, newPrefs []string
	var conclusions []string

	for _, m := range msgs {
		// Old chapter briefings are superseded state, not conversation.
		if IsBriefing(m.Content) {
			continue
		}
		content := stripMarkdownNoise(m.Content)
		switch m.Role {
		case "user":
			if fw.Mission == "" {
				if cand := firstMeaningfulSentence(content, maxMissionChars); cand != "" {
					fw.Mission = cand
				}
			}
			newFacts = append(newFacts, factCandidates(content)...)
			newPrefs = append(newPrefs, preferenceCandidates(content)...)
			newThreads = append(newThreads, threadCandidates(content)...)
			newArts = append(newArts, artifactCandidates(content)...)
		case "assistant":
			newDecisions = append(newDecisions, decisionCandidates(content)...)
			newArts = append(newArts, artifactCandidates(content)...)
			if c := concludingSentence(content); c != "" {
				conclusions = append(conclusions, c)
			}
		case "tool":
			newArts = append(newArts, artifactCandidates(content)...)
		}
	}

	fw.Facts = mergeItems(fw.Facts, newFacts, maxFacts)
	fw.Decisions = mergeItems(fw.Decisions, newDecisions, maxDecisions)
	fw.OpenThreads = mergeItems(fw.OpenThreads, newThreads, maxOpenThreads)
	fw.Artifacts = mergeItems(fw.Artifacts, dedupePaths(newArts), maxArtifacts)
	fw.Preferences = mergeItems(fw.Preferences, newPrefs, maxPreferences)
	fw.Summary = rollSummary(fw.Summary, conclusions)
	fw.UpdatedAt = time.Now().UTC()
	return fw
}

// IsBriefing reports whether a message content is a rendered Framework
// briefing (seeded at the head of every chapter ≥ 2).
func IsBriefing(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), BriefingHeader)
}

// stripMarkdownNoise removes the attachment fencing and image notes the
// composer adds, so distillation reads the human words, not the chrome.
func stripMarkdownNoise(s string) string {
	if i := strings.Index(s, "### Attached files"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "[image attached:", "[")
	return s
}

// mergeItems merges incoming items (newest-first priority) into the existing
// list with fuzzy dedup, then caps. The result keeps the newest items at the
// front so the cap drops the OLDEST knowledge first.
func mergeItems(existing, incoming []string, cap int) []string {
	out := make([]string, 0, len(existing)+len(incoming))
	seen := make([]string, 0, len(existing)+len(incoming))
	add := func(item string) {
		if item == "" {
			return
		}
		k := normKey(item)
		for _, s := range seen {
			if k == s || (len(k) >= 18 && len(s) >= 18 && (strings.Contains(k, s) || strings.Contains(s, k))) {
				return
			}
		}
		seen = append(seen, k)
		out = append(out, item)
	}
	for _, item := range incoming {
		add(item)
	}
	for _, item := range existing {
		add(item)
	}
	if cap > 0 && len(out) > cap {
		out = out[:cap]
	}
	return out
}

// normKey normalizes an item for fuzzy comparison: lowercase, alnum only.
func normKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// clipItem trims an item to the per-item char cap at a word boundary.
func clipItem(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexAny(cut, " ,;:"); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

// ---------------------------------------------------------------------------
// Candidate extraction heuristics
// ---------------------------------------------------------------------------

// sentences splits text into sentence-ish chunks.
func sentences(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		// Strip markdown decorations.
		para = strings.TrimLeft(para, "#>-*• \t")
		for _, sent := range splitSentences(para) {
			sent = strings.TrimSpace(sent)
			if len(sent) >= 8 { // ignore fragments
				out = append(out, sent)
			}
		}
	}
	return out
}

func splitSentences(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		switch r {
		case '.', '!', '?', ';':
			if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\t' {
				if seg := strings.TrimSpace(s[start : i+1]); seg != "" {
					out = append(out, seg)
				}
				start = i + 1
			}
		}
	}
	if seg := strings.TrimSpace(s[start:]); seg != "" {
		out = append(out, seg)
	}
	return out
}

func firstMeaningfulSentence(s string, maxChars int) string {
	for _, sent := range sentences(s) {
		low := strings.ToLower(sent)
		// Skip pure greetings / meta lines; the mission is the first real ask.
		if strings.HasPrefix(low, "hi") || strings.HasPrefix(low, "hello") || strings.HasPrefix(low, "hey") {
			continue
		}
		return clipItem(sent, maxChars)
	}
	return ""
}

// factMarkers introduce durable facts in user speech.
var factMarkers = []string{
	"remember", "note that", "don't forget", "keep in mind",
	"my name is", "my name's", "i am ", "i'm ", "i work", "i use", "we use",
	"our ", "the file is", "the path is", "it is located", "located at",
	"the password", "the key is", "version", "the model is",
}

func factCandidates(s string) []string {
	var out []string
	for _, sent := range sentences(s) {
		low := strings.ToLower(sent)
		for _, mk := range factMarkers {
			if strings.Contains(low, mk) {
				out = append(out, clipItem(sent, maxItemChars))
				break
			}
		}
	}
	// URLs and absolute paths are facts even outside marker sentences.
	out = append(out, extractURLs(s)...)
	out = append(out, extractNumbers(s)...)
	return out
}

var prefMarkers = []string{
	"i prefer", "i'd rather", "i would rather", "from now on", "always ",
	"never ", "don't ", "do not ", "stop using", "keep it ", "use simple",
	"no emojis", "be brief", "i like it when",
}

func preferenceCandidates(s string) []string {
	var out []string
	for _, sent := range sentences(s) {
		low := strings.ToLower(sent)
		for _, mk := range prefMarkers {
			if strings.Contains(low, mk) {
				out = append(out, clipItem("User: "+sent, maxItemChars))
				break
			}
		}
	}
	return out
}

var threadMarkers = []string{
	"next we", "next you", "then you", "after that", "todo", "to-do",
	"still need", "we also need", "don't forget to", "make sure", "later we",
	"coming up",
}

func threadCandidates(s string) []string {
	var out []string
	for _, sent := range sentences(s) {
		low := strings.ToLower(sent)
		if strings.HasSuffix(low, "?") {
			out = append(out, clipItem(sent, maxItemChars))
			continue
		}
		for _, mk := range threadMarkers {
			if strings.Contains(low, mk) {
				out = append(out, clipItem(sent, maxItemChars))
				break
			}
		}
	}
	return out
}

var decisionMarkers = []string{
	"i'll use", "i will use", "i'll go with", "let's use", "let's go with",
	"we'll use", "we'll go with", "going with", "i've chosen", "chosen:",
	"i recommend", "the best option is", "i've created", "i have created",
	"i created", "i've saved", "i saved", "i've written", "i wrote",
	"i've generated", "generated the", "the result is", "we decided",
}

func decisionCandidates(s string) []string {
	var out []string
	for _, sent := range sentences(s) {
		low := strings.ToLower(sent)
		for _, mk := range decisionMarkers {
			if strings.Contains(low, mk) {
				out = append(out, clipItem(sent, maxItemChars))
				break
			}
		}
	}
	return out
}

// concludingSentence picks the final substantive sentence of an assistant
// answer — usually the outcome ("Done — the chart is saved to …").
func concludingSentence(s string) string {
	ss := sentences(s)
	for i := len(ss) - 1; i >= 0; i-- {
		low := strings.ToLower(ss[i])
		// Skip trailing questions back to the user — those are threads, not results.
		if strings.HasSuffix(low, "?") {
			continue
		}
		return clipItem(ss[i], maxItemChars)
	}
	return ""
}

// rollSummary keeps a rolling narrative: each chapter's conclusion is
// appended (newest last reads naturally), bounded to maxSummaryChars.
func rollSummary(existing string, conclusions []string) string {
	if len(conclusions) == 0 {
		return existing
	}
	add := strings.Join(dedupeStrings(conclusions), " ")
	if existing == "" {
		return clipItem(add, maxSummaryChars)
	}
	merged := existing + " " + add
	if len(merged) > maxSummaryChars {
		// Keep the TAIL (most recent), mark the cut.
		cut := merged[len(merged)-maxSummaryChars:]
		if i := strings.IndexByte(cut, ' '); i > 0 {
			cut = cut[i+1:]
		}
		return "… " + cut
	}
	return merged
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := normKey(s)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

func dedupePaths(in []string) []string {
	return dedupeStrings(in)
}

// ---------------------------------------------------------------------------
// Artifact / URL / number extraction
// ---------------------------------------------------------------------------

var urlRe = regexp.MustCompile(`https?://[^\s)>\]"']{4,}`)
var pathRe = regexp.MustCompile(`(?:[A-Za-z]:)?[A-Za-z0-9_.\-]+(?:[\\/][A-Za-z0-9_.\-]+)*\.(?:md|txt|go|py|rs|js|ts|json|csv|tsv|html|css|svg|png|jpg|jpeg|webp|gif|zip|exe|dll|gguf|yaml|yml|toml|log|bat|sh|ps1)`)
var numRe = regexp.MustCompile(`\b\d[\d,.]*\s*(?:%|x|KB|MB|GB|TB|kB|kb|mb|gb|tb|tokens?|ctx|fps|ms|cores|threads|layers)\b`)

func extractURLs(s string) []string {
	var out []string
	for _, u := range urlRe.FindAllString(s, 6) {
		out = append(out, clipItem(u, maxItemChars))
	}
	return out
}

func extractNumbers(s string) []string {
	var out []string
	for _, n := range numRe.FindAllString(s, 8) {
		out = append(out, clipItem("value: "+strings.TrimSpace(n), maxItemChars))
	}
	return out
}

func artifactCandidates(s string) []string {
	var out []string
	for _, p := range pathRe.FindAllString(s, 10) {
		p = strings.TrimRight(p, ".,;:!?")
		if len(p) > 4 && strings.ContainsAny(p, "/\\.") {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Rendering — the chapter briefing
// ---------------------------------------------------------------------------

// Render builds the compact briefing message seeded at the head of the next
// chapter. Sections are added in priority order (mission → facts →
// decisions → threads → artifacts → preferences → summary) and trimmed to
// the token budget; a framework with no signal renders an empty string.
func Render(fw *Framework, tokenBudget int) string {
	if fw.Empty() {
		return ""
	}
	if tokenBudget < 120 {
		tokenBudget = 120
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s — your working memory of chapters 1..%d of this conversation thread]\n", BriefingHeader, maxInt(fw.Chapters, 1)))
	b.WriteString("Everything below is distilled from the earlier part of THIS conversation. Treat it as your own memory: continue seamlessly, never re-ask what is already answered here.\n")

	used := chunking.EstimateTokens(b.String())
	if fw.Mission != "" {
		line := "Mission: " + fw.Mission + "\n"
		used += chunking.EstimateTokens(line)
		b.WriteString(line)
	}

	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		head := "\n" + title + "\n"
		// Only add the section (head cost) if at least one item fits.
		itemLines := make([]string, 0, len(items))
		cost := chunking.EstimateTokens(head)
		for _, it := range items {
			l := "- " + it + "\n"
			c := chunking.EstimateTokens(l)
			if used+cost+c > tokenBudget {
				break
			}
			cost += c
			itemLines = append(itemLines, l)
		}
		if len(itemLines) == 0 {
			return
		}
		used += cost
		b.WriteString(head)
		for _, l := range itemLines {
			b.WriteString(l)
		}
	}

	section("Key facts:", fw.Facts)
	section("Decisions made:", fw.Decisions)
	section("Open threads (pick these up):", fw.OpenThreads)
	section("Files involved:", fw.Artifacts)
	section("User preferences:", fw.Preferences)

	if fw.Summary != "" {
		line := "\nWhat happened so far: " + fw.Summary + "\n"
		if used+chunking.EstimateTokens(line) <= tokenBudget {
			b.WriteString(line)
		} else if used+chunking.EstimateTokens("\nWhat happened so far: (see memory recall)\n") <= tokenBudget {
			b.WriteString("\nWhat happened so far: (see memory recall)\n")
		}
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Persistence — per-session sidecar files
// ---------------------------------------------------------------------------

// frameworkPath returns sessions/<id>.framework.json.
func frameworkPath(sessionsDir, sessionID string) string {
	return filepath.Join(sessionsDir, sessionID+".framework.json")
}

// SaveFramework persists the framework sidecar for a session.
func SaveFramework(sessionsDir, sessionID string, fw *Framework) error {
	if fw == nil {
		return nil
	}
	fw.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(fw, "", "  ")
	if err != nil {
		return err
	}
	tmp := frameworkPath(sessionsDir, sessionID) + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return os.ErrPermission
	}
	return os.Rename(tmp, frameworkPath(sessionsDir, sessionID))
}

// LoadFramework reads the framework sidecar for a session (nil when absent
// or unreadable — the caller falls back to a fresh framework).
func LoadFramework(sessionsDir, sessionID string) *Framework {
	data, err := os.ReadFile(frameworkPath(sessionsDir, sessionID))
	if err != nil {
		return nil
	}
	var fw Framework
	if json.Unmarshal(data, &fw) != nil || fw.Version > FrameworkVersion {
		return nil
	}
	if fw.Version == 0 {
		fw.Version = FrameworkVersion
	}
	return &fw
}

// ---------------------------------------------------------------------------
// Usage estimation
// ---------------------------------------------------------------------------

// Usage is the context-pressure snapshot driving the meter and the rollover
// decision.
type Usage struct {
	EstTokens    int     `json:"estTokens"`    // estimated tokens of the history
	BudgetTokens int     `json:"budgetTokens"` // history token budget
	Pct          float64 `json:"pct"`          // est/budget*100 (can exceed 100)
}

// Level classifies the pressure for the meter color.
func (u Usage) Level() string {
	switch {
	case u.Pct >= 95:
		return "critical"
	case u.Pct >= 75:
		return "high"
	case u.Pct >= 50:
		return "warm"
	default:
		return "ok"
	}
}

// EstimateUsage measures a message list against a token budget.
func EstimateUsage(msgs []llm.Message, budgetTokens int) Usage {
	if budgetTokens < 1 {
		budgetTokens = 1
	}
	est := 0
	for i := range msgs {
		est += chunking.EstimateTokens(msgs[i].Content)
		for _, tc := range msgs[i].ToolCalls {
			est += chunking.EstimateTokens(tc.Function.Arguments) + 4
		}
	}
	return Usage{EstTokens: est, BudgetTokens: budgetTokens, Pct: float64(est) * 100 / float64(budgetTokens)}
}
