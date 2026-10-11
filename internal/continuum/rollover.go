// Rollover: the chapter transition itself. The Manager owns the session
// store and the sessions directory; it decides WHEN a session should roll
// (ShouldRollover) and performs the atomic handover (Rollover): distill →
// create child → seed briefing + carried messages → persist chain + framework
// sidecars. Enhance() is the optional background LLM refinement pass.
package continuum

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// Manager performs chapter rollovers against a session store.
type Manager struct {
	Store       *sessions.Store
	SessionsDir string
}

// NewManager returns a rollover manager for the given store.
func NewManager(store *sessions.Store, sessionsDir string) *Manager {
	return &Manager{Store: store, SessionsDir: sessionsDir}
}

// ThreadID returns the stable thread identifier of a session (a session
// without a thread starts its own).
func ThreadID(sess *sessions.Session) string {
	if sess == nil {
		return ""
	}
	if sess.ThreadID != "" {
		return sess.ThreadID
	}
	return sess.ID
}

// SessionUsage measures the session's history against the configured budget.
func SessionUsage(sess *sessions.Session, cfg *config.Config) Usage {
	if sess == nil {
		return Usage{}
	}
	return EstimateUsage(sess.Messages, cfg.HistoryWindowTokens())
}

// ShouldRollover reports whether the session's history pressure crossed the
// configured threshold. Called by the UI after every completed turn.
func (m *Manager) ShouldRollover(sess *sessions.Session, cfg *config.Config) bool {
	if sess == nil || cfg == nil || !cfg.ContinuumEnabled {
		return false
	}
	// A session with no exchange yet can never be under pressure.
	if len(sess.Messages) < 2 {
		return false
	}
	u := SessionUsage(sess, cfg)
	return u.Pct >= float64(cfg.EffectiveContinuumThreshold())
}

// Rollover distills the parent session and creates the next chapter:
//
//	child.ThreadID  = parent's thread (stable across the whole chain)
//	child.ParentID  = parent.ID
//	child.Chapter   = parent.Chapter + 1
//	child.Messages  = [system briefing] + last K non-briefing messages
//
// The framework sidecar is written for BOTH sessions (the parent keeps its
// distilled state so reopening it shows what it knew; the child starts life
// with the same snapshot).
func (m *Manager) Rollover(parent *sessions.Session, cfg *config.Config) (*sessions.Session, *Framework, error) {
	if parent == nil {
		return nil, nil, fmt.Errorf("continuum: nil session")
	}
	if m.Store == nil {
		return nil, nil, fmt.Errorf("continuum: nil store")
	}

	// 1. Load-or-create the thread framework and distill this chapter.
	fw := LoadFramework(m.SessionsDir, parent.ID)
	if fw == nil {
		// Inherit the thread's latest known state: walk to the parent's own
		// parent sidecar when present (chains created before a restart).
		if parent.ParentID != "" {
			fw = LoadFramework(m.SessionsDir, parent.ParentID)
		}
		if fw == nil {
			fw = NewFramework()
		}
	}
	fw = Distill(fw, parent.Messages)

	newChapter := parent.Chapter + 1
	if parent.Chapter == 0 {
		newChapter = 1
	}
	// Chapter numbering: parent.Chapter==0 → this is chapter 2 of the thread
	// (chapter 1 is the original session). Chapters counts chapters created.
	chaptersCreated := newChapter
	if chaptersCreated < fw.Chapters {
		chaptersCreated = fw.Chapters
	}
	fw.Chapters = chaptersCreated

	// 2. Create the child session in the background.
	child := m.Store.Create()
	child.Title = parent.Title
	child.Model = parent.Model
	child.Preset = parent.Preset
	child.Context = parent.Context
	child.ThreadID = ThreadID(parent)
	child.ParentID = parent.ID
	child.Chapter = newChapter

	// 3. Seed: framework briefing first, then the carried recent tail.
	carry := carryableMessages(parent.Messages, cfg.EffectiveContinuumCarry())
	child.Messages = nil
	if briefing := Render(fw, cfg.EffectiveContinuumFrameworkTokens()); briefing != "" {
		child.Messages = append(child.Messages, llm.Message{Role: "system", Content: briefing})
	}
	child.Messages = append(child.Messages, carry...)

	if err := m.Store.Save(child); err != nil {
		return nil, nil, fmt.Errorf("continuum: save child: %w", err)
	}
	if err := SaveFramework(m.SessionsDir, child.ID, fw); err != nil {
		return nil, nil, fmt.Errorf("continuum: save child framework: %w", err)
	}
	if err := SaveFramework(m.SessionsDir, parent.ID, fw); err != nil {
		return nil, nil, fmt.Errorf("continuum: save parent framework: %w", err)
	}
	return child, fw, nil
}

// carryableMessages returns the last K messages, skipping chapter briefings
// and leading system chrome (the child gets a FRESH briefing, never two).
func carryableMessages(msgs []llm.Message, k int) []llm.Message {
	if k <= 0 || len(msgs) == 0 {
		return nil
	}
	var carry []llm.Message
	for i := len(msgs) - 1; i >= 0 && len(carry) < k; i-- {
		m := msgs[i]
		if m.Role == "system" || IsBriefing(m.Content) {
			continue
		}
		carry = append([]llm.Message{m}, carry...)
	}
	return carry
}

// ---------------------------------------------------------------------------
// LLM enhancement (optional, background)
// ---------------------------------------------------------------------------

// Summarizer is the minimal LLM surface Enhance needs — *llm.Client
// satisfies it. Keeping it an interface makes the pass fake-server testable.
type Summarizer interface {
	Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error)
}

// enhancePromptLimit bounds the conversation excerpt sent for refinement
// (chars, ~1200 tokens — the pass must stay cheap next to a real turn).
const enhancePromptLimit = 6000

// Enhance refines an extractive Framework with one LLM pass: the model sees
// the current framework + a bounded excerpt of the chapter and returns a
// corrected JSON framework. On ANY failure it returns nil (the caller keeps
// the extractive result) — enhancement is strictly best-effort.
func Enhance(ctx context.Context, sum Summarizer, model string, fw *Framework, msgs []llm.Message) *Framework {
	if sum == nil || fw == nil || len(msgs) == 0 {
		return nil
	}
	excerpt := buildExcerpt(msgs, enhancePromptLimit)
	if excerpt == "" {
		return nil
	}

	prompt := fmt.Sprintf(`You are the state-distillation module of a desktop AI agent. Update the conversation state framework.

CURRENT FRAMEWORK (JSON):
%s

RECENT CONVERSATION EXCERPT:
%s

Return ONLY a JSON object with this exact shape:
{"mission": "...", "facts": ["..."], "decisions": ["..."], "openThreads": ["..."], "artifacts": ["..."], "preferences": ["..."], "summary": "..."}

Rules:
- Carry forward every still-relevant item from the current framework; drop only what is obsolete or wrong.
- Each item: one line, max 160 chars, no numbering.
- "openThreads" = unfinished work or questions the user expects to be picked up.
- "summary" = a compact narrative of the whole conversation so far (max 120 words).
- No markdown, no commentary — JSON only.`,
		mustJSON(map[string]any{
			"mission":     fw.Mission,
			"facts":       fw.Facts,
			"decisions":   fw.Decisions,
			"openThreads": fw.OpenThreads,
			"artifacts":   fw.Artifacts,
			"preferences": fw.Preferences,
			"summary":     fw.Summary,
		}), excerpt)

	req := &llm.ChatRequest{
		Model:       model,
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: 0.2,
		MaxTokens:   900,
	}
	req.Stream = false
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := sum.Chat(ctx, req)
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	refined, err := ParseLLMFramework(resp.Choices[0].Message.Content)
	if err != nil {
		return nil
	}

	// Merge refined items over the extractive base (LLM first, extractive
	// fills gaps; caps enforced).
	out := &Framework{
		Version:     FrameworkVersion,
		Mission:     refined.Mission,
		Facts:       mergeItems(refined.Facts, fw.Facts, maxFacts),
		Decisions:   mergeItems(refined.Decisions, fw.Decisions, maxDecisions),
		OpenThreads: mergeItems(refined.OpenThreads, fw.OpenThreads, maxOpenThreads),
		Artifacts:   mergeItems(refined.Artifacts, fw.Artifacts, maxArtifacts),
		Preferences: mergeItems(refined.Preferences, fw.Preferences, maxPreferences),
		Summary:     refined.Summary,
		Chapters:    fw.Chapters,
		UpdatedAt:   time.Now().UTC(),
	}
	if out.Mission == "" {
		out.Mission = fw.Mission
	}
	if out.Summary == "" {
		out.Summary = fw.Summary
	}
	if out.Empty() {
		return nil
	}
	return out
}

// llmFramework is the wire shape the enhancement prompt asks for.
type llmFramework struct {
	Mission     string   `json:"mission"`
	Facts       []string `json:"facts"`
	Decisions   []string `json:"decisions"`
	OpenThreads []string `json:"openThreads"`
	Artifacts   []string `json:"artifacts"`
	Preferences []string `json:"preferences"`
	Summary     string   `json:"summary"`
}

// ParseLLMFramework extracts and sanitizes the JSON framework from an LLM
// reply (tolerating code fences and stray prose around it).
func ParseLLMFramework(text string) (*llmFramework, error) {
	if text == "" {
		return nil, fmt.Errorf("empty reply")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in reply")
	}
	var lf llmFramework
	if err := json.Unmarshal([]byte(text[start:end+1]), &lf); err != nil {
		return nil, err
	}
	sanitize := func(in []string, cap int) []string {
		var out []string
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s == "" || strings.HasPrefix(s, "```") {
				continue
			}
			out = append(out, clipItem(s, maxItemChars))
			if len(out) >= cap {
				break
			}
		}
		return out
	}
	lf.Mission = clipItem(strings.TrimSpace(lf.Mission), maxMissionChars)
	lf.Summary = clipItem(strings.TrimSpace(lf.Summary), maxSummaryChars)
	lf.Facts = sanitize(lf.Facts, maxFacts)
	lf.Decisions = sanitize(lf.Decisions, maxDecisions)
	lf.OpenThreads = sanitize(lf.OpenThreads, maxOpenThreads)
	lf.Artifacts = sanitize(lf.Artifacts, maxArtifacts)
	lf.Preferences = sanitize(lf.Preferences, maxPreferences)
	return &lf, nil
}

// buildExcerpt renders a bounded user/assistant-only excerpt of the chapter.
func buildExcerpt(msgs []llm.Message, limitChars int) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if IsBriefing(m.Content) {
			continue
		}
		content := stripMarkdownNoise(m.Content)
		if len(content) > 1200 {
			content = content[:1200] + "…"
		}
		fmt.Fprintf(&b, "%s: %s\n", strings.ToUpper(m.Role[:1])+m.Role[1:], content)
		if b.Len() > limitChars {
			break
		}
	}
	if b.Len() > limitChars {
		return b.String()[:limitChars] + "…"
	}
	return strings.TrimSpace(b.String())
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
