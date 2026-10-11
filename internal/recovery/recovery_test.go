// recovery_test.go — v1.7.1 deterministic coverage for the
// context-exhaustion recovery contract (§3.11):
//
//   - typed condition detection across BOTH backend boundaries;
//   - complete logical-state capture;
//   - hierarchical summary over contexts larger than one window;
//   - deterministic fallback summary;
//   - durable handoff (atomic write, survives "restart", identity);
//   - bounded injection (no duplicate user turn, do-not-redo list).
package recovery

import (
        "context"
        "errors"
        "os"
        "path/filepath"
        "strings"
        "testing"
)

// --- typed condition ----------------------------------------------------------

func TestExhaustionUnwrapsToSentinel(t *testing.T) {
        ex := NewExhaustion("llama.cpp", "LLM HTTP 400: the request exceeds the available context size", "")
        if !IsContextExhausted(ex) {
                t.Fatal("typed Exhaustion not detected")
        }
        wrapped := errors.Join(errors.New("outer"), ex)
        if !IsContextExhausted(wrapped) {
                t.Fatal("wrapped Exhaustion not detected via errors.Is")
        }
        if IsContextExhausted(errors.New("connection reset")) {
                t.Fatal("a transport error must NOT be classified as exhaustion")
        }
        if IsContextExhausted(nil) {
                t.Fatal("nil must not be classified as exhaustion")
        }
        if _, ok := AsExhaustion(ex); !ok {
                t.Fatal("AsExhaustion failed on typed value")
        }
        if ex.Detail == "" || ex.Backend != "llama.cpp" {
                t.Fatalf("evidence lost: %+v", ex)
        }
}

func TestExhaustionDetailBounded(t *testing.T) {
        huge := strings.Repeat("x", 100000)
        ex := NewExhaustion("native", huge, "context-bound")
        if len(ex.Detail) > 600 {
                t.Fatalf("detail not bounded: %d", len(ex.Detail))
        }
}

// --- snapshot capture ---------------------------------------------------------

func testSnapshot() *Snapshot {
        snap := NewSnapshot("sess-1", "thread-1", "task-1", "run-1", 1,
                Exhaustion{Backend: "llama.cpp", Detail: "maximum context length exceeded"})
        snap.Mission = "Refactor the parser module"
        snap.CurrentState = "ACT: editing parser.go"
        snap.Constraints = []string{"must keep the CLI flags stable"}
        snap.Facts = []string{"the parser uses a recursive descent"}
        snap.Decisions = []string{"keep the existing error type"}
        snap.OpenThreads = []string{"still need to update tests"}
        snap.Preferences = []string{"User: be brief"}
        snap.FilesInspected = []string{"parser.go", "lexer.go"}
        snap.FilesChanged = []string{"parser.go"}
        snap.ToolCalls = []ToolRecord{
                {Name: "files", Args: `{"action":"write","path":"parser.go"}`, Outcome: "written"},
                {Name: "shell", Args: `{"command":"go test ./..."}`, Outcome: "FAILED", Failed: true},
        }
        snap.TestsAndBuilds = []string{"go test ./..."}
        snap.Failures = []string{"shell: tests failed"}
        snap.Repairs = []string{"files recovered after failure"}
        snap.Artifacts = []string{"parser.go", "registered-report"}
        snap.NextActions = []string{"fix the failing tests"}
        snap.TaskTools = []string{"task_report"}
        snap.LinkedSkills = []string{"deploy-verify"}
        snap.SourceBackend = "llama.cpp"
        snap.SourceModel = "test-model"
        snap.Configuration = map[string]string{"contextTokens": "4096"}
        snap.Messages = []Message{
                {Role: "system", Content: "You are SHEYTAN."},
                {Role: "user", Content: "Refactor the parser module"},
                {Role: "assistant", Content: "Reading the parser..."},
                {Role: "tool", Content: "parser.go updated"},
                {Role: "user", Content: "Also run the full verification battery before you finish"},
        }
        snap.Bound()
        return snap
}

func TestSnapshotCapturesCompleteLogicalState(t *testing.T) {
        snap := testSnapshot()

        checks := []bool{
                snap.Mission != "",
                snap.SessionID == "sess-1",
                snap.ThreadID == "thread-1",
                snap.TaskID == "task-1",
                snap.RunID == "run-1",
                snap.SourceBackend == "llama.cpp",
                snap.SourceModel == "test-model",
                snap.Detected.Backend == "llama.cpp",
                len(snap.Facts) == 1,
                len(snap.FilesChanged) == 1,
                len(snap.ToolCalls) == 2,
                len(snap.TestsAndBuilds) == 1,
                len(snap.Failures) == 1,
                len(snap.Repairs) == 1,
                len(snap.Artifacts) == 2,
                len(snap.NextActions) == 1,
                len(snap.Messages) == 5,
                snap.EstimatedTokens > 0,
                snap.Attempt == 1,
                !snap.CreatedAt.IsZero(),
        }
        for i, cond := range checks {
                if !cond {
                        t.Fatalf("snapshot completeness check %d failed: %+v", i, snap)
                }
        }
}

func TestSnapshotCompletedWorkPreventsDuplicateExecution(t *testing.T) {
        snap := testSnapshot()
        done := snap.CompletedWork()

        joined := strings.Join(done, "\n")
        // §3.8: completed irreversible work is durably listed.
        for _, want := range []string{"file changed: parser.go", "command run: go test ./...", "artifact: registered-report"} {
                if !strings.Contains(joined, want) {
                        t.Fatalf("completed work missing %q: %v", want, done)
                }
        }
}

// --- hierarchical summary ------------------------------------------------------

type fakeSummarizer struct {
        calls    int
        failN    int // fail the first N calls
        promptsz []int
}

func (f *fakeSummarizer) Summarize(ctx context.Context, prompt string) (string, error) {
        f.calls++
        if f.calls <= f.failN {
                return "", errors.New("summarizer offline")
        }
        f.promptsz = append(f.promptsz, estTokens(prompt))
        return "summary-part-" + strings.Repeat("x", 8) + "-" + itoa(f.calls), nil
}

func itoa(n int) string {
        if n == 0 {
                return "0"
        }
        digits := ""
        for n > 0 {
                digits = string(rune('0'+n%10)) + digits
                n /= 10
        }
        return digits
}

func bigMessages(nChunks int) []Message {
        msgs := make([]Message, 0, nChunks)
        for i := 0; i < nChunks; i++ {
                content := strings.Repeat("word ", 2200) + " marker-" + itoa(i) + " "
                role := "assistant"
                if i%2 == 0 {
                        role = "user"
                }
                msgs = append(msgs, Message{Role: role, Content: content})
        }
        return msgs
}

func TestChunkMessagesCoversWholeContext(t *testing.T) {
        msgs := bigMessages(6)
        chunks := ChunkMessages(msgs, ChunkTokenBudget)
        if len(chunks) < 3 {
                t.Fatalf("expected several bounded chunks, got %d", len(chunks))
        }
        // Whole-context coverage: every message marker must appear.
        seen := 0
        for _, chunk := range chunks {
                for _, m := range chunk {
                        if strings.Contains(m.Content, "marker-") {
                                seen++
                        }
                }
                if estTokens(chunkText(chunk)) > ChunkTokenBudget*2 {
                        t.Fatalf("chunk exceeds 2x budget: %d", estTokens(chunkText(chunk)))
                }
        }
        if seen != 6 {
                t.Fatalf("chunks lost messages: covered %d of 6", seen)
        }
}

func chunkText(chunk []Message) string {
        var b strings.Builder
        for _, m := range chunk {
                b.WriteString(m.Content)
        }
        return b.String()
}

func TestHierarchicalSummaryRepresentsWholeContext(t *testing.T) {
        snap := testSnapshot()
        snap.Messages = bigMessages(6)
        snap.Bound()

        sum := &fakeSummarizer{}
        out, err := HierarchicalSummary(context.Background(), snap, sum)
        if err != nil {
                t.Fatalf("hierarchical summary: %v", err)
        }
        if out == "" || !strings.Contains(out, "summary-part") {
                t.Fatalf("summary output missing: %q", out)
        }
        // More than ONE chunk ⇒ the hierarchical path ran multiple stages.
        if sum.calls < 2 {
                t.Fatalf("expected multi-stage summarization, got %d calls", sum.calls)
        }
        // The merge stage must also fit the bounded window.
        for _, sz := range sum.promptsz {
                if sz > MergeTokenBudget+ChunkTokenBudget {
                        t.Fatalf("merge prompt exceeded budget: %d", sz)
                }
        }
}

func TestHierarchicalSummaryFailsHonestlyWithoutSummarizer(t *testing.T) {
        snap := testSnapshot()
        snap.Messages = bigMessages(2)
        if _, err := HierarchicalSummary(context.Background(), snap, nil); err == nil {
                t.Fatal("nil summarizer must error (caller falls back)")
        }
        failing := &fakeSummarizer{failN: 999}
        if _, err := HierarchicalSummary(context.Background(), snap, failing); err == nil {
                t.Fatal("failing summarizer must error (caller falls back)")
        }
}

// --- deterministic fallback (§3.5) ---------------------------------------------

func TestFallbackSummaryDeterministicAndBounded(t *testing.T) {
        snap := testSnapshot()
        a := FallbackSummary(snap)
        b := FallbackSummary(snap)
        if a != b {
                t.Fatal("fallback summary must be deterministic")
        }
        for _, want := range []string{"Refactor the parser module", "parser.go", "go test ./...", "fix the failing tests"} {
                if !strings.Contains(a, want) {
                        t.Fatalf("fallback summary missing %q", want)
                }
        }
        if len(a) > 8000 {
                t.Fatalf("fallback summary unbounded: %d chars", len(a))
        }
}

// --- durable handoff ------------------------------------------------------------

func TestHandoffRecordIdentityAndDurability(t *testing.T) {
        dir := t.TempDir()
        store := NewStore(dir)

        snap := testSnapshot()
        h := NewHandoff(snap, "the whole-context summary", "hierarchical")

        // §3.6 minimum fields.
        if h.HandoffID == "" || h.SessionID != "sess-1" || h.ThreadID != "thread-1" ||
                h.TaskID != "task-1" || h.RunID != "run-1" {
                t.Fatalf("identity fields: %+v", h)
        }
        if h.SourceAttempt != 1 || h.RecoveryAttempt != 1 {
                t.Fatalf("attempt fields: %+v", h)
        }
        if h.SourceBackend != "llama.cpp" || h.SourceModel != "test-model" {
                t.Fatalf("source fields: %+v", h)
        }
        if h.Summary != "the whole-context summary" || h.SummarySource != "hierarchical" {
                t.Fatalf("summary fields: %+v", h)
        }
        if h.LastUserPrompt != "Also run the full verification battery before you finish" {
                t.Fatalf("continuation anchor lost: %q", h.LastUserPrompt)
        }
        if len(h.CompletedWork) == 0 {
                t.Fatal("do-not-redo list missing")
        }

        if err := store.Save(h); err != nil {
                t.Fatalf("save: %v", err)
        }
        if _, err := os.Stat(filepath.Join(dir, h.HandoffID+".json")); err != nil {
                t.Fatalf("record not on disk: %v", err)
        }

        // Survives "restart": a FRESH store instance reads the same record.
        reopened := NewStore(dir)
        got, err := reopened.Get(h.HandoffID)
        if err != nil {
                t.Fatalf("reload: %v", err)
        }
        if got.HandoffID != h.HandoffID || got.Summary != h.Summary {
                t.Fatalf("round-trip mismatch: %+v", got)
        }

        // List + Latest by session.
        if all := reopened.List("sess-1"); len(all) != 1 {
                t.Fatalf("list: %d", len(all))
        }
        if other := reopened.List("sess-other"); len(other) != 0 {
                t.Fatalf("session scoping broken: %d", len(other))
        }
        if latest := reopened.Latest("sess-1"); latest == nil || latest.HandoffID != h.HandoffID {
                t.Fatalf("latest: %+v", latest)
        }
}

func TestStoreRejectsNewerVersions(t *testing.T) {
        dir := t.TempDir()
        store := NewStore(dir)
        h := NewHandoff(testSnapshot(), "s", "fallback")
        h.HandoffVersion = HandoffVersion + 5
        if err := store.Save(h); err != nil {
                t.Fatalf("save: %v", err)
        }
        if _, err := store.Get(h.HandoffID); err == nil {
                t.Fatal("newer schema version must be rejected honestly")
        }
}

// --- injection -------------------------------------------------------------------

func TestRenderInjectionBoundsAndGuarantees(t *testing.T) {
        snap := testSnapshot()
        snap.Facts = append(snap.Facts, strings.Repeat("fact ", 400)) // oversized item
        snap.Bound()
        h := NewHandoff(snap, FallbackSummary(snap), "fallback")

        inj := RenderInjection(h)
        if !strings.Contains(inj, InjectionHeader) {
                t.Fatal("injection header missing")
        }
        // §3.8: the original user turn is NOT re-quoted inside the injection.
        if strings.Contains(inj, h.LastUserPrompt) {
                t.Fatal("injection must not duplicate the user message (the orchestrator appends it separately)")
        }
        if !strings.Contains(inj, "Do NOT redo") || !strings.Contains(inj, "parser.go") {
                t.Fatal("do-not-redo section missing")
        }
        if !strings.Contains(inj, "fix the failing tests") {
                t.Fatal("next actions missing")
        }
        if estTokens(inj) > InjectionTokenBudget {
                t.Fatalf("injection over budget: %d > %d", estTokens(inj), InjectionTokenBudget)
        }
}

func TestRenderInjectionEmptyHandoff(t *testing.T) {
        if got := RenderInjection(nil); got != "" {
                t.Fatalf("nil handoff rendered %q", got)
        }
        if got := RenderInjection(&Handoff{}); got != "" {
                t.Fatalf("empty handoff rendered %q", got)
        }
}
