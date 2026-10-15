// localchat_e2e_v181_test.go — the v1.8.1 P0 regression, END TO END on
// the LOCAL engine path: local provider + small Gemma-class model +
// one-word "hi" + ordinary chat + no capability signals.
//
// Acceptance pinned here (the mission's real-world regression list):
//   - no panic, no nil dereference (the v1.8.0 Windows crash);
//   - the run reaches the generation backend (recorded engine evidence);
//   - at least one response chunk arrives BEFORE the done frame (the WS
//     sequence proves streaming order deterministically — no timing);
//   - the final response is persisted exactly once;
//   - the run settles exactly once, as "done" (never "error");
//   - a SECOND ordinary chat turn on the same engine also completes.
package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// v181GGUFWriter writes GGUF v3 little-endian scalars.
type v181GGUFWriter struct {
	buf bytes.Buffer
}

func (w *v181GGUFWriter) u32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.buf.Write(b[:])
}

func (w *v181GGUFWriter) u64(v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	w.buf.Write(b[:])
}

func (w *v181GGUFWriter) str(s string) {
	w.u64(uint64(len(s)))
	w.buf.WriteString(s)
}

// writeGemmaClassModel writes a VALID GGUF v3 with the Gemma E2B
// tokenizer shape whose metadata block (~10.5 MiB) exceeded the
// pre-v1.8.1 8 MiB parser bound — the exact model class of the reported
// Windows crash.
func writeGemmaClassModel(t *testing.T, path string) {
	t.Helper()

	const tokenCount = 262144
	const tokenLen = 24

	w := &v181GGUFWriter{}
	w.buf.WriteString("GGUF")
	w.u32(3) // version 3
	w.u64(0) // tensor count
	w.u64(7) // kv count

	w.str("general.architecture")
	w.u32(8)
	w.str("gemma3n")

	w.str("general.name")
	w.u32(8)
	w.str("Gemma E2B IT")

	w.str("gemma3n.context_length")
	w.u32(4)
	w.u32(32768)

	w.str("gemma3n.block_count")
	w.u32(4)
	w.u32(28)

	w.str("gemma3n.embedding_length")
	w.u32(4)
	w.u32(2048)

	w.str("tokenizer.ggml.tokens")
	w.u32(9)
	w.u32(8) // string elements
	w.u64(uint64(tokenCount))
	tok := strings.Repeat("a", tokenLen)
	for i := 0; i < tokenCount; i++ {
		w.str(tok)
	}

	w.str("tokenizer.ggml.scores")
	w.u32(9)
	w.u32(6) // float32
	w.u64(uint64(tokenCount))
	for i := 0; i < tokenCount; i++ {
		w.u32(0)
	}

	w.str("tokenizer.ggml.token_type")
	w.u32(9)
	w.u32(5) // int32
	w.u64(uint64(tokenCount))
	for i := 0; i < tokenCount; i++ {
		w.u32(1)
	}

	if err := os.WriteFile(path, w.buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write gemma-class gguf: %v", err)
	}
}

// newLocalEngineServer builds a fully-wired API server whose local
// llama.cpp engine is the TEST BINARY re-executed as the fake engine
// (see fakellama_v181_test.go). The request log captures the engine
// evidence the assertions read.
func newLocalEngineServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = config.ProviderLocal
	cfg.LlamaAutoStart = false // the run gate owns the cold start
	cfg.UpdateSchedule = "off"
	// v1.9.6: an EXPLICIT CPU profile keeps the unrelated background AUTO
	// Vulkan bootstrap out of this fixture (the probe is eligible on
	// windows/amd64 — GPU detected, online, model resolvable — and its
	// transaction would otherwise race this test with real network
	// work). This regression pins the local fake-engine chat path on the
	// CPU posture; the AUTO GPU candidate has its own bounded-state
	// harness (variant_auto_v172_test.go) and must never be a hidden
	// dependency of an unrelated E2E.
	cfg.Accelerator = "cpu"
	cfg.LlamaBinPath = exe
	cfg.LlamaHost = "127.0.0.1"
	cfg.LlamaPort = v181FreePort(t)
	cfg.EngineCompat = 3 // bare flags: the fake engine ignores tuning
	cfg.Model = "gemma-4-E2B-it-Q4_K_M.gguf"
	cfg.VisionEnabled = false

	if err := os.MkdirAll(cfg.ModelsDir, 0o755); err != nil {
		t.Fatalf("models dir: %v", err)
	}

	writeGemmaClassModel(t, filepath.Join(cfg.ModelsDir, cfg.Model))

	requestLog := filepath.Join(t.TempDir(), "engine-requests.log")

	t.Setenv("SHEYTAN_FAKE_LLAMA", "1")
	t.Setenv("SHEYTAN_V181_REQUEST_LOG", requestLog)

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	if err := srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	t.Cleanup(srv.Close)

	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	return srv, server, requestLog
}

// v181WSRecorder collects activity frames in arrival order until the
// run reaches a terminal frame (done/error/aborted) — the deterministic
// ordering evidence for "streamed before completion".
type v181WSRecorder struct {
	mu     sync.Mutex
	frames []map[string]any
}

func (rec *v181WSRecorder) kinds() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	kinds := make([]string, 0, len(rec.frames))
	for _, f := range rec.frames {
		if k, _ := f["type"].(string); k != "" {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// recordWS reads frames until the terminal frame for this run arrives
// (or the deadline — a failure surfaced as a test error, never a hang).
func recordWS(t *testing.T, server *httptest.Server, sessionID, runID string) *v181WSRecorder {
	t.Helper()

	rec := &v181WSRecorder{}
	conn := dialActivityWS(t, server, sessionID)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

			var frame map[string]any
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}

			rec.mu.Lock()
			rec.frames = append(rec.frames, frame)
			rec.mu.Unlock()

			if typ, _ := frame["type"].(string); typ == "done" || typ == "error" || typ == "aborted" {
				return
			}
		}
	}()

	t.Cleanup(func() {
		_ = conn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})

	return rec
}

// waitForTerminalFrame blocks until the recorder observed the run's
// terminal frame (bounded; the run must settle in that window).
func waitForTerminalFrame(t *testing.T, rec *v181WSRecorder) string {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		for _, k := range rec.kinds() {
			if k == "done" || k == "error" || k == "aborted" {
				return k
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("the run never published a terminal frame within the deadline")
	return ""
}

// TestLocalGemmaHiChatCompletes is THE v1.8.1 regression: the exact
// real-world Windows failure class, end to end, local engine, small
// Gemma-class model, one-word chat.
func TestLocalGemmaHiChatCompletes(t *testing.T) {
	srv, server, requestLog := newLocalEngineServer(t)
	sessionID := createSessionForRun(t, server)

	rec := recordWS(t, server, sessionID, "")

	runID := postRunMessage(t, server, sessionID, "hi")

	// Attach the recorder to this run (the hub stamps runId on every
	// frame; the recorder above already collects everything).
	_ = runID

	terminal := waitForTerminalFrame(t, rec)

	// THE crash regression: the run must reach the generation backend and
	// settle "done" — the pre-v1.8.1 build panicked here and settled
	// "error" in ~57 ms with NO engine request.
	if terminal != "done" {
		kinds := rec.kinds()
		t.Fatalf("run terminated %q, want done — frame kinds: %v", terminal, kinds)
	}

	// Engine evidence: the generation request REALLY reached the local
	// backend (request-sent proof, not UI presence).
	raw, err := os.ReadFile(requestLog)
	if err != nil {
		t.Fatalf("no engine request evidence: %v", err)
	}
	if !strings.Contains(string(raw), "chat model=") {
		t.Fatalf("engine request log has no chat entry: %q", string(raw))
	}

	// Streaming BEFORE completion: at least one response frame precedes
	// the terminal frame — deterministic ordering evidence.
	kinds := rec.kinds()
	firstResponse := -1
	terminalAt := -1
	for i, k := range kinds {
		if k == "response" && firstResponse == -1 {
			firstResponse = i
		}
		if k == "done" || k == "error" || k == "aborted" {
			terminalAt = i
			break
		}
	}
	if firstResponse == -1 {
		t.Fatalf("no streamed response frame arrived before completion: %v", kinds)
	}
	if terminalAt == -1 || firstResponse >= terminalAt {
		t.Fatalf("terminal frame preceded the first response frame: %v", kinds)
	}

	// The final reply is persisted exactly once.
	waitForReplyPersisted(t, server, sessionID)

	resp, err := http.Get(server.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer resp.Body.Close()

	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	assistantCount := 0
	finalContent := ""
	for _, m := range got.Messages {
		if m.Role == "assistant" {
			assistantCount++
			finalContent = m.Content
		}
	}
	if assistantCount != 1 {
		t.Fatalf("persisted assistant replies = %d, want exactly 1", assistantCount)
	}
	want := "Hello! How can I help you today?"
	if strings.TrimSpace(finalContent) != want {
		t.Fatalf("persisted reply = %q, want %q", finalContent, want)
	}

	// The run settled exactly once and was released (no orphaned run).
	waitForRegistryRelease(t, srv, sessionID)

	// A SECOND ordinary chat on the same live engine also completes —
	// the repair is not a one-shot.
	rec2 := recordWS(t, server, sessionID, "")
	postRunMessage(t, server, sessionID, "thanks, bye")
	terminal2 := waitForTerminalFrame(t, rec2)

	if terminal2 != "done" {
		t.Fatalf("second ordinary chat terminated %q, want done", terminal2)
	}

	waitForReplyPersisted(t, server, sessionID)
	waitForRegistryRelease(t, srv, sessionID)
}

// TestLocalChatSurvivesUnreadableModelCard pins the crash class at the
// FULL pipeline level: a model file whose GGUF card cannot be read at
// all (nil caps — the exact posture of the v1.8.0 Windows crash after
// ANY card-read failure) must never crash the run. The engine serves it
// (it loaded the model itself); the orchestrator must fall back to the
// configured context and complete the ordinary chat.
func TestLocalChatSurvivesUnreadableModelCard(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = config.ProviderLocal
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"
	// v1.9.6: explicit CPU — same fixture-boundary rule as
	// newLocalEngineServer: the unrelated background AUTO Vulkan probe
	// must never race this regression with real network work.
	cfg.Accelerator = "cpu"
	cfg.LlamaBinPath = exe
	cfg.LlamaHost = "127.0.0.1"
	cfg.LlamaPort = v181FreePort(t)
	cfg.EngineCompat = 3
	cfg.Model = "not-really-gguf.gguf"
	cfg.VisionEnabled = false

	if err := os.MkdirAll(cfg.ModelsDir, 0o755); err != nil {
		t.Fatalf("models dir: %v", err)
	}

	// A present-but-unparseable model file: ResolveModelPath resolves it,
	// ReadModelCard fails at the magic → caps nil → the pre-v1.8.1 run
	// panicked in resolveEffectiveContext.
	if err := os.WriteFile(
		filepath.Join(cfg.ModelsDir, cfg.Model),
		[]byte("NOT A GGUF FILE — card read fails, caps is nil"),
		0o644,
	); err != nil {
		t.Fatalf("write model: %v", err)
	}

	requestLog := filepath.Join(t.TempDir(), "engine-requests.log")
	t.Setenv("SHEYTAN_FAKE_LLAMA", "1")
	t.Setenv("SHEYTAN_V181_REQUEST_LOG", requestLog)

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	if err := srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}
	t.Cleanup(srv.Close)

	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	sessionID := createSessionForRun(t, server)
	rec := recordWS(t, server, sessionID, "")

	postRunMessage(t, server, sessionID, "hi")

	terminal := waitForTerminalFrame(t, rec)
	if terminal != "done" {
		t.Fatalf("run with unreadable model card terminated %q, want done (frame kinds: %v)",
			terminal, rec.kinds())
	}

	// The generation backend was reached and the reply persisted once.
	if raw, err := os.ReadFile(requestLog); err != nil || !strings.Contains(string(raw), "chat model=") {
		t.Fatalf("engine request evidence missing: err=%v", err)
	}

	waitForReplyPersisted(t, server, sessionID)
	waitForRegistryRelease(t, srv, sessionID)
}
