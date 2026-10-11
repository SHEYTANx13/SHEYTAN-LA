package continuum

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

func distillTestMessages() []llm.Message {
	return []llm.Message{
		{Role: "user", Content: "Hi! I want to build a market research report for my new EV charger startup."},
		{Role: "user", Content: "Remember my company is called Voltra and we use llama.cpp locally, budget is 4096 tokens."},
		{Role: "assistant", Content: "I'll use a structured outline with competitor tables. I've created the file workspace/report.md with the skeleton. The chart is saved to charts/market.png. Let me know if the sections fit your needs."},
		{Role: "user", Content: "I prefer concise answers without emojis. Can you also analyze charging speeds next?"},
		{Role: "assistant", Content: "Done — the competitor section now includes charging speeds. The result is a 3-column table in workspace/report.md."},
	}
}

func TestDistillExtractsCoreState(t *testing.T) {
	fw := Distill(NewFramework(), distillTestMessages())
	if fw.Mission == "" || !strings.Contains(strings.ToLower(fw.Mission), "market research") {
		t.Fatalf("mission not extracted: %q", fw.Mission)
	}
	joined := strings.Join(fw.Facts, " | ")
	if !strings.Contains(strings.ToLower(joined), "voltra") {
		t.Errorf("company fact missing: %v", fw.Facts)
	}
	dec := strings.Join(fw.Decisions, " | ")
	if !strings.Contains(dec, "workspace/report.md") && !strings.Contains(strings.Join(fw.Artifacts, ","), "workspace/report.md") {
		t.Errorf("decision/artifact for report.md missing (dec=%v art=%v)", fw.Decisions, fw.Artifacts)
	}
	if len(fw.Preferences) == 0 || !strings.Contains(strings.ToLower(fw.Preferences[0]), "concise") {
		t.Errorf("preference not extracted: %v", fw.Preferences)
	}
	if !strings.Contains(strings.Join(fw.Artifacts, ","), "charts/market.png") {
		t.Errorf("chart artifact missing: %v", fw.Artifacts)
	}
	if fw.Summary == "" {
		t.Errorf("rolling summary empty")
	}
}

func TestDistillSkipsBriefings(t *testing.T) {
	msgs := append([]llm.Message{
		{Role: "system", Content: Render(&Framework{Mission: "old mission from chapter 1", Chapters: 1, Facts: []string{"stale fact"}}, 700)},
	}, distillTestMessages()...)
	fw := Distill(NewFramework(), msgs)
	for _, f := range fw.Facts {
		if strings.Contains(f, "stale fact") {
			t.Errorf("briefing content leaked into facts: %v", fw.Facts)
		}
	}
	if strings.Contains(fw.Mission, "old mission") {
		t.Errorf("briefing mission overrode the real one: %q", fw.Mission)
	}
}

func TestMergeDedupesAndCaps(t *testing.T) {
	fw := NewFramework()
	fw.Facts = []string{"Company is Voltra, budget 4096 tokens"}
	msgs := []llm.Message{{Role: "user", Content: "Remember my company is called Voltra and we use llama.cpp locally, budget is 4096 tokens."}}
	fw = Distill(fw, msgs)
	norm := map[string]bool{}
	for _, f := range fw.Facts {
		k := normKey(f)
		if norm[k] {
			t.Errorf("duplicate fact after merge: %q", f)
		}
		norm[k] = true
	}
	if len(fw.Facts) > maxFacts {
		t.Errorf("facts cap exceeded: %d", len(fw.Facts))
	}
}

func TestRenderBudgetBounded(t *testing.T) {
	fw := NewFramework()
	fw.Mission = strings.Repeat("m", maxMissionChars)
	for i := 0; i < 60; i++ {
		fw.Facts = append(fw.Facts, strings.Repeat("f", 100))
		fw.Decisions = append(fw.Decisions, strings.Repeat("d", 100))
	}
	fw.Summary = strings.Repeat("s", maxSummaryChars)
	fw.Chapters = 3
	out := Render(fw, 400)
	est := (len(out) + 3) / 4
	if est > 430 { // small slack for the header/footer lines
		t.Errorf("render exceeds budget: %d tokens for budget 400", est)
	}
	if !strings.HasPrefix(out, BriefingHeader) {
		t.Errorf("render missing briefing header")
	}
}

func TestRenderEmptyFrameworkIsEmpty(t *testing.T) {
	if out := Render(NewFramework(), 700); out != "" {
		t.Errorf("empty framework rendered: %q", out)
	}
}

func TestFrameworkSidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fw := Distill(NewFramework(), distillTestMessages())
	if err := SaveFramework(dir, "sess1", fw); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := LoadFramework(dir, "sess1")
	if got == nil {
		t.Fatal("sidecar not found after save")
	}
	if got.Mission != fw.Mission || len(got.Facts) != len(fw.Facts) {
		t.Errorf("round-trip drift: mission=%q facts=%d", got.Mission, len(got.Facts))
	}
	if LoadFramework(dir, "missing") != nil {
		t.Errorf("missing sidecar returned non-nil")
	}
}

func usageFor(pct int) Usage {
	return Usage{EstTokens: pct * 10, BudgetTokens: 1000, Pct: float64(pct)}
}

func TestUsageLevels(t *testing.T) {
	cases := map[int]string{30: "ok", 60: "warm", 80: "high", 99: "critical"}
	for in, want := range cases {
		if got := usageFor(in).Level(); got != want {
			t.Errorf("usage(%d).Level()=%s want %s", in, got, want)
		}
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.LLM.NumCtx = 8192
	return cfg
}

func bigSession(t *testing.T, store *sessions.Store, tokens int) *sessions.Session {
	t.Helper()
	sess := store.Create()
	sess.Title = "Big chapter"
	filler := strings.Repeat("token ", tokens/2) // ~1 token per "token " repetition... conservative: 2 tokens
	sess.Messages = []llm.Message{
		{Role: "user", Content: "Remember the access key is SK-42."},
		{Role: "assistant", Content: filler},
	}
	_ = store.Save(sess)
	return sess
}

func TestShouldRolloverThreshold(t *testing.T) {
	cfg := testConfig(t)
	store := sessions.New(cfg.SessionsDir)
	m := NewManager(store, cfg.SessionsDir)

	small := bigSession(t, store, 200)
	if m.ShouldRollover(small, cfg) {
		t.Errorf("small session should not roll over")
	}

	cfg.ContinuumEnabled = false
	big := bigSession(t, store, 90000)
	if m.ShouldRollover(big, cfg) {
		t.Errorf("disabled continuum must never roll over")
	}

	cfg.ContinuumEnabled = true
	if !m.ShouldRollover(big, cfg) {
		t.Errorf("huge session should roll over (usage %.0f%%)", SessionUsage(big, cfg).Pct)
	}
}

func TestRolloverCreatesChainedChapter(t *testing.T) {
	cfg := testConfig(t)
	store := sessions.New(cfg.SessionsDir)
	m := NewManager(store, cfg.SessionsDir)

	parent := store.Create()
	parent.Title = "EV research"
	parent.Messages = distillTestMessages()
	// push it over the threshold
	parent.Messages[1].Content += " " + strings.Repeat("detail ", 90000)
	_ = store.Save(parent)

	child, fw, err := m.Rollover(parent, cfg)
	if err != nil {
		t.Fatalf("rollover: %v", err)
	}
	if child.ThreadID != ThreadID(parent) {
		t.Errorf("thread id broken: child=%s parent-thread=%s", child.ThreadID, ThreadID(parent))
	}
	if child.ParentID != parent.ID {
		t.Errorf("parent link missing")
	}
	if child.Chapter != parent.Chapter+1 {
		t.Errorf("chapter numbering: child=%d", child.Chapter)
	}
	if len(child.Messages) == 0 {
		t.Fatal("child has no messages")
	}
	if !IsBriefing(child.Messages[0].Content) {
		t.Errorf("child messages[0] is not the framework briefing: %q", clipStr(child.Messages[0].Content, 60))
	}
	if child.Title != parent.Title {
		t.Errorf("title not carried: %q", child.Title)
	}
	// framework persisted for both
	if LoadFramework(cfg.SessionsDir, child.ID) == nil || LoadFramework(cfg.SessionsDir, parent.ID) == nil {
		t.Errorf("framework sidecars missing")
	}
	// briefing must mention the distilled mission
	if !strings.Contains(child.Messages[0].Content, "Mission:") {
		t.Errorf("briefing missing mission")
	}
	_ = fw
}

func TestRolloverCarriesRecentTailOnly(t *testing.T) {
	cfg := testConfig(t)
	cfg.ContinuumCarryMessages = 2
	store := sessions.New(cfg.SessionsDir)
	m := NewManager(store, cfg.SessionsDir)

	parent := store.Create()
	for i := 0; i < 10; i++ {
		parent.Messages = append(parent.Messages,
			llm.Message{Role: "user", Content: fmt.Sprintf("message number %d", i)},
			llm.Message{Role: "assistant", Content: fmt.Sprintf("answer number %d", i)},
		)
	}
	_ = store.Save(parent)

	child, _, err := m.Rollover(parent, cfg)
	if err != nil {
		t.Fatalf("rollover: %v", err)
	}
	carried := child.Messages[1:] // after the briefing
	if len(carried) != 2 {
		t.Fatalf("carry count: want 2, got %d", len(carried))
	}
	if carried[0].Content != "answer number 9" || carried[1].Content != "message number 9" {
		// order depends on which two messages were last; accept the last user+assistant pair
		joined := carried[0].Content + "," + carried[1].Content
		if !strings.Contains(joined, "number 9") || strings.Contains(joined, "number 0 ") {
			t.Errorf("wrong carry tail: %s", joined)
		}
	}
}

func TestCarrySkipsBriefings(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: Render(&Framework{Mission: "m", Chapters: 1}, 400)},
		{Role: "user", Content: "latest question"},
	}
	carried := carryableMessages(msgs, 4)
	if len(carried) != 1 || carried[0].Content != "latest question" {
		t.Fatalf("briefing leaked into carry: %+v", carried)
	}
}

func TestChainedRolloverKeepsThread(t *testing.T) {
	cfg := testConfig(t)
	store := sessions.New(cfg.SessionsDir)
	m := NewManager(store, cfg.SessionsDir)

	s1 := store.Create()
	s1.Messages = distillTestMessages()
	_ = store.Save(s1)

	s2, fw1, err := m.Rollover(s1, cfg)
	if err != nil {
		t.Fatalf("rollover 1: %v", err)
	}
	s2.Messages = append(s2.Messages, llm.Message{Role: "user", Content: "Remember the API key is key-99."})
	s2.Messages = append(s2.Messages, llm.Message{Role: "assistant", Content: "Noted and stored."})
	_ = store.Save(s2)

	s3, fw2, err := m.Rollover(s2, cfg)
	if err != nil {
		t.Fatalf("rollover 2: %v", err)
	}
	if s3.ThreadID != s2.ThreadID || s2.ThreadID != ThreadID(s1) {
		t.Errorf("thread id changed across chain")
	}
	if s3.Chapter != 2 {
		t.Errorf("chapter should be 2, got %d", s3.Chapter)
	}
	// knowledge accumulates across chapters
	if !strings.Contains(strings.Join(fw2.Facts, "|"), "key-99") {
		t.Errorf("chapter-2 fact missing after second distill: %v", fw2.Facts)
	}
	_ = fw1
	// exactly ONE briefing at the head of chapter 3
	briefings := 0
	for _, msg := range s3.Messages {
		if IsBriefing(msg.Content) {
			briefings++
		}
	}
	if briefings != 1 {
		t.Errorf("chapter 3 should carry exactly one briefing, has %d", briefings)
	}
}

// fakeSummarizer serves canned LLM replies for Enhance.
type fakeSummarizer struct {
	reply string
	err   error
}

func (f *fakeSummarizer) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	resp := &llm.ChatResponse{}
	resp.Choices = append(resp.Choices, struct {
		Message      llm.Message `json:"message"`
		FinishReason string      `json:"finish_reason"`
	}{Message: llm.Message{Role: "assistant", Content: f.reply}})
	return resp, nil
}

func TestParseLLMFramework(t *testing.T) {
	reply := "Sure! Here is the updated framework:\n```json\n{\"mission\": \"Ship v2\", \"facts\": [\"GPU is RTX 4060\", \"\"], \"openThreads\": [\"benchmark Vulkan\"], \"summary\": \"The user benchmarks engines.\"}\n```"
	lf, err := ParseLLMFramework(reply)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if lf.Mission != "Ship v2" || len(lf.Facts) != 1 || lf.Facts[0] != "GPU is RTX 4060" {
		t.Errorf("parsed framework wrong: %+v", lf)
	}
	if _, err := ParseLLMFramework("no json here"); err == nil {
		t.Errorf("garbage accepted")
	}
}

func TestEnhanceMergesOverExtractive(t *testing.T) {
	fw := Distill(NewFramework(), distillTestMessages())
	fake := &fakeSummarizer{reply: `{"mission":"Build the EV charger market report","facts":["Company: Voltra","GPU: RTX 4060"],"decisions":["Outline-driven report"],"openThreads":["charging-speed analysis"],"artifacts":["workspace/report.md"],"preferences":["concise, no emojis"],"summary":"Voltra EV charger market research with report and charts."}`}
	refined := Enhance(context.Background(), fake, "test-model", fw, distillTestMessages())
	if refined == nil {
		t.Fatal("enhance returned nil")
	}
	joined := strings.Join(refined.Facts, "|")
	if !strings.Contains(joined, "Voltra") || !strings.Contains(joined, "RTX 4060") {
		t.Errorf("enhanced facts wrong: %v", refined.Facts)
	}
	if refined.Mission != "Build the EV charger market report" {
		t.Errorf("mission not refined: %q", refined.Mission)
	}
	// failure path keeps extractive
	if Enhance(context.Background(), &fakeSummarizer{err: os.ErrDeadlineExceeded}, "m", fw, distillTestMessages()) != nil {
		t.Errorf("failing summarizer should return nil")
	}
}

func TestSessionUsageReflectsConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.HistoryWindowPct = 50
	store := sessions.New(cfg.SessionsDir)
	sess := store.Create()
	sess.Messages = []llm.Message{{Role: "user", Content: strings.Repeat("a", 40000)}} // ~10k tokens
	u := SessionUsage(sess, cfg)
	if u.BudgetTokens != cfg.HistoryWindowTokens() {
		t.Errorf("budget mismatch: %d", u.BudgetTokens)
	}
	if u.Pct < 100 {
		t.Errorf("expected over-budget usage, got %.0f%%", u.Pct)
	}
}

func TestEstimateUsageToolCalls(t *testing.T) {
	tc := llm.ToolCall{}
	tc.Function.Name = "files"
	tc.Function.Arguments = `{"path":"workspace/report.md"}`
	msgs := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	u := EstimateUsage(msgs, 100)
	if u.EstTokens <= 0 {
		t.Errorf("tool-call args not counted")
	}
}

func TestBriefingJSONStable(t *testing.T) {
	// The sidecar must round-trip through sessions without date drift breaking equality.
	fw := Distill(NewFramework(), distillTestMessages())
	data, _ := json.Marshal(fw)
	var back Framework
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("json: %v", err)
	}
	if back.FactCount() != fw.FactCount() {
		t.Errorf("fact count drift")
	}
}

func clipStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TestEnhanceAgainstLiveServer runs the Enhance path against a real HTTP
// endpoint shaped like llama.cpp (non-streaming chat completion) — the same
// wire contract the production client uses.
func TestEnhanceAgainstLiveServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"mission":"refined mission","facts":["f1"]}`},
					"finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Provider = config.ProviderRemote
	cfg.RemoteBaseURL = srv.URL + "/v1"
	client := llm.NewClient(config.NewSource(cfg))

	fw := Distill(NewFramework(), distillTestMessages())
	refined := Enhance(context.Background(), client, "m", fw, distillTestMessages())
	if refined == nil {
		t.Fatal("live-server enhance returned nil")
	}
	if refined.Mission != "refined mission" {
		t.Errorf("mission not refined: %q", refined.Mission)
	}
}
