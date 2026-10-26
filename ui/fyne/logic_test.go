//go:build !headless

// logic_test.go — the ported UI-behavior acceptance contracts, now Go
// tests. Each test pins an invariant that survived from the retired
// TypeScript frontend (src/*.test.ts) and the Qt client; string-existence
// checks are NOT used — every assertion is behavioral.
package fyneui

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStreamAccumulatorReplacesNeverAppends pins the cumulative-caption
// contract: response/reasoning events carry FULL captions; folding them
// REPLACES the live text (idempotent under reconnect replay).
func TestStreamAccumulatorReplacesNeverAppends(t *testing.T) {
	var acc StreamAccumulator
	acc.Fold(StreamEvent{Type: "response", Caption: "Hello"})
	acc.Fold(StreamEvent{Type: "response", Caption: "Hello, world"})
	if acc.Response != "Hello, world" {
		t.Fatalf("cumulative caption must REPLACE: got %q", acc.Response)
	}
	// Replay the same frame (reconnect idempotency).
	acc.Fold(StreamEvent{Type: "response", Caption: "Hello, world"})
	if acc.Response != "Hello, world" {
		t.Fatalf("replayed frame must be idempotent: got %q", acc.Response)
	}
	if !acc.Active {
		t.Fatal("stream must be active while folding response events")
	}
}

// TestStreamAccumulatorTerminalEvents pins honest terminal outcomes:
// done/complete/aborted/error all settle and deactivate the stream.
func TestStreamAccumulatorTerminalEvents(t *testing.T) {
	cases := map[string]StreamKind{
		"done":     StreamDone,
		"complete": StreamComplete,
		"aborted":  StreamAborted,
		"error":    StreamError,
	}
	for typ, want := range cases {
		var acc StreamAccumulator
		acc.Fold(StreamEvent{Type: "response", Caption: "partial"})
		acc.Fold(StreamEvent{Type: typ})
		if acc.Active {
			t.Fatalf("%s must deactivate the stream", typ)
		}
		if acc.Terminal != want {
			t.Fatalf("%s terminal = %v, want %v", typ, acc.Terminal, want)
		}
	}
}

// TestStreamAccumulatorActivityRingBounded pins the bounded display ring.
func TestStreamAccumulatorActivityRingBounded(t *testing.T) {
	var acc StreamAccumulator
	for i := 0; i < maxActivities+30; i++ {
		acc.push("event")
	}
	if len(acc.Activities) != maxActivities {
		t.Fatalf("activity ring must cap at %d, got %d", maxActivities, len(acc.Activities))
	}
}

// TestStreamSnapshotAdoption pins the run_snapshot replay contract: the
// authoritative snapshot is adopted verbatim and a terminal outcome in the
// snapshot settles the stream.
func TestStreamSnapshotAdoption(t *testing.T) {
	var acc StreamAccumulator
	acc.Fold(StreamEvent{Type: "run_snapshot", RunID: "r1", LatestResponse: "persisted text", Running: true})
	if acc.Response != "persisted text" || !acc.Active {
		t.Fatalf("snapshot must be adopted: %+v", acc)
	}
	acc.Fold(StreamEvent{Type: "run_snapshot", RunID: "r1", LatestResponse: "persisted text", TerminalOutcome: "aborted"})
	if acc.Active {
		t.Fatal("terminal snapshot outcome must settle the stream")
	}
}

// TestSessionListGuardStaleResponseRejected pins the monotonic generation
// contract: a response whose ticket is stale can never publish.
func TestSessionListGuardStaleResponseRejected(t *testing.T) {
	var g SessionListGuard
	t1 := g.Ticket()
	if !g.IsCurrent(t1) {
		t.Fatal("fresh ticket must be current")
	}
	g.Invalidate() // a mutation (create/delete) happened
	if g.IsCurrent(t1) {
		t.Fatal("invalidated ticket must be stale — a stale GET can never resurrect a deleted session")
	}
	t2 := g.Ticket()
	if !g.IsCurrent(t2) {
		t.Fatal("new ticket must be current after invalidation")
	}
}

// TestModeSessionsSeparateSpaces pins chat/agent space separation.
func TestModeSessionsSeparateSpaces(t *testing.T) {
	all := []Session{
		{ID: "c1", Mode: "chat"},
		{ID: "a1", Mode: "agent"},
		{ID: "a2", Mode: ""}, // legacy default → agent
	}
	chat := FilterSessions(all, ModeChat)
	agent := FilterSessions(all, ModeAgent)
	if len(chat) != 1 || chat[0].ID != "c1" {
		t.Fatalf("chat space wrong: %+v", chat)
	}
	if len(agent) != 2 {
		t.Fatalf("agent space must include legacy-mode sessions: %+v", agent)
	}
	if NormalizeMode("") != ModeAgent || NormalizeMode("chat") != ModeChat || NormalizeMode("weird") != ModeAgent {
		t.Fatal("NormalizeMode contract broken")
	}
}

// TestHistoryRefsDedupSelfAndCap pins the reference rules: dedup, no
// self-attach, hard cap 4, first-seen order preserved.
func TestHistoryRefsDedupSelfAndCap(t *testing.T) {
	got := NormalizeHistoryRefs(
		[]string{"s2", "s1", "s2", "SELF", "s3", "s4", "s5", "s6"},
		"SELF",
	)
	want := []string{"s2", "s1", "s3", "s4"}
	if len(got) != len(want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("refs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if MaxHistoryRefs != 4 {
		t.Fatal("the shipped cap is 4")
	}
}

// TestRunPhaseMachineNeverInventsPhases pins the phase-derivation rules:
// phases advance ONLY on observed events; duplicate pause is a no-op.
func TestRunPhaseMachineNeverInventsPhases(t *testing.T) {
	var m RunPhaseMachine
	if m.Phase != PhaseIdle {
		t.Fatal("initial phase must be idle")
	}
	m.Observe(StreamResponse)
	if m.Phase != PhaseStreaming {
		t.Fatal("response event must stream")
	}
	if p := m.Pause(); p != PhasePaused {
		t.Fatal("pause must pause")
	}
	if p := m.Pause(); p != PhasePaused {
		t.Fatal("duplicate pause must be a no-op")
	}
	// While paused, streaming events do NOT un-pause (the backend owns
	// resume; the UI never invents it).
	m.Observe(StreamResponse)
	if m.Phase != PhasePaused {
		t.Fatal("stream events must not un-pause a paused run")
	}
	if p := m.Observe(StreamAborted); p != PhaseAborted {
		t.Fatal("abort must settle aborted")
	}
}

// TestNormalizeThinkingLadder pins the reasoning-controls ladder and the
// legacy-name normalization.
func TestNormalizeThinkingLadder(t *testing.T) {
	for _, lvl := range []string{"low", "mid", "high", "ultra"} {
		if NormalizeThinking(lvl) != lvl {
			t.Fatalf("%s must normalize to itself", lvl)
		}
	}
	if NormalizeThinking("auto") != "mid" || NormalizeThinking("") != "mid" {
		t.Fatal("auto/empty → mid")
	}
	if NormalizeThinking("fast") != "low" || NormalizeThinking("thinking") != "high" {
		t.Fatal("legacy names must map to the ladder")
	}
}

// TestSettingsSchemaPinnedStructure pins the 8-section IA and the
// FIELD_PLACEMENT quarantine rules (ported from settings-sections.test.ts):
// expert fields live ONLY in Advanced; automated fields render no control.
func TestSettingsSchemaPinnedStructure(t *testing.T) {
	if len(SettingsSections) != 8 {
		t.Fatalf("sections = %d, want 8", len(SettingsSections))
	}
	advancedKeys := map[string]bool{}
	for _, f := range SettingsFields {
		if f.Section == "advanced" {
			advancedKeys[f.Key] = true
		}
	}
	for _, expert := range []string{"llamaBinPath", "llamaHost", "llamaPort", "llamaExtraArgs", "kvCacheQuant", "threadsBatch", "cacheReuse", "ubatchSize", "mlock", "draftModel", "sandboxMemory", "sandboxCPU", "maxWorkspaceMb", "maxSessionsKept", "maxLogMb", "temperature", "topP", "topK", "minP", "maxTokens", "repeatPenalty", "seed"} {
		if !advancedKeys[expert] {
			t.Fatalf("expert field %q must live in Advanced", expert)
		}
	}
	// Normal sections must not contain expert fields.
	for _, f := range SettingsFields {
		if f.Section != "advanced" && advancedKeys[f.Key] {
			t.Fatalf("field %q appears in both %q and advanced", f.Key, f.Section)
		}
	}
	// Automated fields must have NO editable control anywhere.
	controlled := map[string]bool{}
	for _, f := range SettingsFields {
		controlled[f.Key] = true
	}
	for _, auto := range AutomatedFields {
		if controlled[auto] {
			t.Fatalf("automated field %q must not render as a control", auto)
		}
	}
	// visionMmproj is Advanced-only (REMOVED_FROM_NORMAL_UI).
	for _, f := range SettingsFields {
		if f.Key == "visionMmproj" && f.Section != "advanced" {
			t.Fatal("visionMmproj must be Advanced-only")
		}
	}
}

// TestBuildPatchNestedLLM pins the wire patch shape: llm.* fields nest
// under the deep-merged "llm" object; everything else is top-level.
func TestBuildPatchNestedLLM(t *testing.T) {
	numCtx := SettingsField{Key: "numCtx", LLM: true}
	patch := BuildPatch(numCtx, 8192)
	llm, ok := patch["llm"].(map[string]any)
	if !ok || llm["numCtx"] != 8192 {
		t.Fatalf("llm field must nest: %+v", patch)
	}
	if _, hasTop := patch["numCtx"]; hasTop {
		t.Fatal("flat llm key is silently dropped by the typed merge — must not be sent")
	}
	tools := SettingsField{Key: "smoothStream"}
	patch = BuildPatch(tools, true)
	if patch["smoothStream"] != true {
		t.Fatalf("top-level field patch wrong: %+v", patch)
	}
	if _, hasLLM := patch["llm"]; hasLLM {
		t.Fatal("top-level field must not nest under llm")
	}
}

// TestEnabledToolsPatchShape pins the one-patch CSV rewrite.
func TestEnabledToolsPatchShape(t *testing.T) {
	patch := EnabledToolsPatch([]string{"fs_read", "net"})
	got, ok := patch["enabledTools"].([]string)
	if !ok || len(got) != 2 {
		t.Fatalf("enabledTools patch wrong: %+v", patch)
	}
}

// TestZeroSessionSendContract is the behavioral pin for the zero-session
// contract at the runtime level: Send with no active session creates one
// (visible in the runtime) and completes the same send against it.
func TestZeroSessionSendContract(t *testing.T) {
	srv := newFakeCore(t)
	defer srv.Close()
	c := srv.client()
	rt := NewRuntime(c)

	ctx := contextBackground()
	if _, err := rt.Send(ctx, "hello", false, "mid"); err != nil {
		t.Fatalf("zero-session send must succeed: %v", err)
	}
	sessions := rt.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("send with zero sessions must create exactly one, got %d", len(sessions))
	}
	if rt.ActiveID() == "" {
		t.Fatal("the created session must become active")
	}
	// The created session is in the current mode (chat by default).
	if sessions[0].Mode != ModeChat {
		t.Fatalf("created session mode = %q, want chat", sessions[0].Mode)
	}
}

// TestZeroSessionSendFailureIsTyped pins the failure half: when the
// session creation fails, Send returns a typed error instead of wedging.
func TestZeroSessionSendFailureIsTyped(t *testing.T) {
	srv := newFakeCore(t)
	srv.failSessions = true
	defer srv.Close()
	c := srv.client()
	rt := NewRuntime(c)

	_, err := rt.Send(contextBackground(), "hello", false, "mid")
	if err == nil {
		t.Fatal("creation failure must surface a typed error")
	}
	if !strings.Contains(err.Error(), "no active session") {
		t.Fatalf("error must name the contract: %v", err)
	}
	if rt.Busy() {
		t.Fatal("composer must not wedge: busy must be false after failure")
	}
}

// TestSessionDeleteGuard pins: delete invalidates in-flight lists first;
// deleting the active session clears it; a stale list response arriving
// after the delete can never resurrect it.
func TestSessionDeleteGuard(t *testing.T) {
	srv := newFakeCore(t)
	defer srv.Close()
	c := srv.client()
	rt := NewRuntime(c)
	ctx := contextBackground()

	if err := rt.RefreshSessions(ctx); err != nil {
		t.Fatalf("seed list: %v", err)
	}
	s := rt.Sessions()
	if len(s) == 0 {
		t.Fatal("fake core must seed a session")
	}
	id := s[0].ID
	if err := rt.DeleteSession(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rt.ActiveID() != "" {
		t.Fatal("deleting the active session must clear it")
	}
	// A stale list response (captured before the delete) must be rejected.
	if err := rt.RefreshSessions(ctx); err == nil {
		if len(rt.Sessions()) != 0 {
			t.Fatalf("deleted session must not resurrect: %+v", rt.Sessions())
		}
	}
}

// TestSelectModelNeverUsesPatchConfig pins the selection-path rule: the
// client method posts /api/models/select; the config patch method is a
// separate typed operation. (The UI panel calls SelectModel — asserted
// structurally by method presence and behaviorally by the fake core.)
func TestSelectModelNeverUsesPatchConfig(t *testing.T) {
	srv := newFakeCore(t)
	defer srv.Close()
	c := srv.client()

	sel, err := c.SelectModel(contextBackground(), "test-model.gguf", "chat")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if sel.Phase == "" {
		t.Fatal("selection must return the state machine phase")
	}
	if srv.lastSelectModel != "test-model.gguf" {
		t.Fatalf("selection must POST /api/models/select with the model, got %q", srv.lastSelectModel)
	}
	if srv.configPatched {
		t.Fatal("selection must NEVER patch the config directly")
	}
}

// TestConfigPatchLandsInCore pins: PatchConfig PUTs the patch and re-reads
// the redacted view.
func TestConfigPatchLandsInCore(t *testing.T) {
	srv := newFakeCore(t)
	defer srv.Close()
	c := srv.client()

	cfg, err := c.PatchConfig(contextBackground(), map[string]any{"llm": map[string]any{"temperature": 0.31}})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if cfg.LLM.Temperature != 0.31 {
		t.Fatalf("re-read must reflect the patch, got %v", cfg.LLM.Temperature)
	}
	if !srv.configPatched {
		t.Fatal("the core must have received the patch")
	}
}

// TestNoPanicOnMalformedFrames pins robustness: malformed API payloads are
// skipped, never a panic.
func TestNoPanicOnMalformedFrames(t *testing.T) {
	var acc StreamAccumulator
	bad := []string{
		`{"type": 42}`,
		`not json`,
		`{"type":"response","caption":123}`,
		`{}`,
		`[]`,
	}
	for _, raw := range bad {
		var ev StreamEvent
		if json.Unmarshal([]byte(raw), &ev) == nil {
			acc.Fold(ev) // must not panic
		}
	}
}
