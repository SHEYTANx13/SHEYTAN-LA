package api

// server_native_test.go — v1.1.5 Phase 1: the engine snapshot must expose
// the backend selection and the native engine status honestly, without
// changing any v1.1.4 behavior when the native path is disabled (the
// default).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestEngineSnapshotDefaultHasNoNativeBlock(t *testing.T) {
	server, _ := newTestServer(t) // default: EngineBackend = llama

	resp, err := http.Get(server.URL + "/api/engine")
	if err != nil {
		t.Fatalf("GET /api/engine: %v", err)
	}
	defer resp.Body.Close()

	var snap struct {
		Backend string           `json:"backend"`
		Native  *json.RawMessage `json:"native"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Default: generation backend is llama and NO native block exists —
	// byte-compatible with the v1.1.4 payload plus the backend name.
	if snap.Backend != "llama" {
		t.Fatalf("backend = %q, want llama (default)", snap.Backend)
	}

	if snap.Native != nil {
		t.Fatalf("native block must be absent by default, got %s", *snap.Native)
	}
}

func newNativeTestServer(t *testing.T, engineBackend string) *httptest.Server {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off" // v1.2.0: no network in unit tests — the scheduled updater is owned and closed, never exercised here
	cfg.EngineBackend = engineBackend

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

	return server
}

func TestEngineSnapshotNativeSelectedButUnavailable(t *testing.T) {
	// Native selected, host binary NOT built → the block must say so
	// honestly and the generation backend must remain llama.
	server := newNativeTestServer(t, "native")

	resp, err := http.Get(server.URL + "/api/engine")
	if err != nil {
		t.Fatalf("GET /api/engine: %v", err)
	}
	defer resp.Body.Close()

	var snap struct {
		Backend string `json:"backend"`
		Native  *struct {
			Selected  bool   `json:"selected"`
			Available bool   `json:"available"`
			State     string `json:"state"`
			Detail    string `json:"detail,omitempty"`
		} `json:"native"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if snap.Backend != "llama" {
		t.Fatalf("backend = %q, want llama (native cannot generate in Phase 1)", snap.Backend)
	}

	if snap.Native == nil {
		t.Fatal("native block missing when the native path is selected")
	}

	if !snap.Native.Selected {
		t.Fatal("native.selected must be true")
	}

	if snap.Native.Available {
		t.Fatal("native.available must be false (no host binary built in tests)")
	}

	// No start was attempted (no prewarm with LlamaAutoStart=false): the
	// engine is idle — an honest, reachable state.
	if snap.Native.State != llm.StateIdle {
		t.Fatalf("native state = %q, want idle before any start attempt", snap.Native.State)
	}
}

func TestEngineToggleStartReportsLlamaState(t *testing.T) {
	// The POST /api/llama action contract is unchanged: the response
	// carries the llama engine state; native failures never leak into
	// the response path (they surface in /api/engine).
	//
	// v1.5.0 model-first repair: an engine start now requires a
	// SELECTED model (a start with no selection is a clean 400 — never
	// an arbitrary model boot). To keep proving the ORIGINAL contract
	// (a missing llama binary fails with 500 and native failures never
	// leak), the test selects a model fixture first; the selection
	// itself is exercised end-to-end by selection_test.go.
	server := newNativeTestServer(t, "native")

	modelFixture := filepath.Join(t.TempDir(), "toggle-model.gguf")
	if err := os.WriteFile(modelFixture, []byte("GGUF toggle fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := fmt.Sprintf(`{"model": %q}`, modelFixture)
	preq, err := http.NewRequest(http.MethodPut, server.URL+"/api/config", jsonReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	preq.Header.Set("Content-Type", "application/json")
	presp, err := http.DefaultClient.Do(preq)
	if err != nil {
		t.Fatalf("PUT /api/config: %v", err)
	}
	presp.Body.Close()

	body := `{"action":"start"}`
	resp, err := http.Post(server.URL+"/api/llama", "application/json", jsonReader(body))
	if err != nil {
		t.Fatalf("POST /api/llama: %v", err)
	}
	defer resp.Body.Close()

	// No llama binary exists in the test environment: the start must
	// fail with 500 exactly as in v1.1.4 (no native interference).
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (missing engine binary, unchanged behavior)", resp.StatusCode)
	}
}

func jsonReader(s string) io.Reader {
	return strings.NewReader(s)
}

// TestEngineToggleNativeServesGeneration — v1.1.5 repair regression
// (HTTP-level, the layer a user notices). The engine toggle must produce a
// USABLE native engine — host started AND the selected model loaded
// natively — and a plain-text run must stream REAL native generation
// end-to-end on a native-only deployment (no llama.cpp binary anywhere).
// Before the repair the toggle started the host but never loaded a model,
// so the engine stayed generation-incapable, every run still gated on
// llama.cpp, and the toggle returned 500 on exactly the deployment that
// had no llama.cpp to fall back to.
//
// Skips when the C++ host binary has not been built (same contract as the
// internal/native/engine integration suites).
func TestEngineToggleNativeServesGeneration(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot resolve repository root")
	}

	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	hostBin := filepath.Join(
		repoRoot, "native", "engine", "build", "shtn-engine-host",
	)

	if _, err := os.Stat(hostBin); err != nil {
		t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", hostBin)
	}

	fixture := filepath.Join(
		repoRoot, "native", "engine", "tests", "fixtures", "tiny-llama-app.gguf",
	)

	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("app fixture missing (run native/engine/tests/reference/make_fixture.py): %v", err)
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.EngineBackend = "native"
	cfg.NativeEnginePath = hostBin
	cfg.Model = fixture

	// Pre-write a user-authored AI-CONTEXT.md (no version marker →
	// aicontext.EnsureFile keeps it untouched). The embedded briefing
	// (~28 KB) byte-tokenizes far beyond the model's real 2048-token
	// window on the fixture vocabulary and the native engine would
	// REJECT the request honestly (prompt+max_tokens > context) — the
	// production behavior for an oversized briefing, not what this test
	// pins.
	aiCtx := filepath.Join(cfg.DataDir, "AI-CONTEXT.md")
	if err := os.WriteFile(aiCtx, []byte("Be brief. Answer in one short sentence.\n"), 0o644); err != nil {
		t.Fatalf("write AI-CONTEXT.md: %v", err)
	}

	// The native path serves PLAIN-TEXT requests only (documented Phase 5
	// limit): disable all tools so this run routes natively.
	cfg.EnabledTools = []string{"__none__"}

	// Match the model's real window and sampling: greedy (temperature 0)
	// deterministically runs the fixture to max_tokens instead of an
	// instant EOS; 2048 context fits the real system briefing.
	cfg.LLM.NumCtx = 2048
	cfg.LLM.MaxTokens = 32
	cfg.LLM.Temperature = 0

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

	// 1. The toggle must SUCCEED on a native-only deployment (before the
	// repair: 500, because llama.Start() failed and native had no model).
	resp, err := http.Post(
		server.URL+"/api/llama",
		"application/json",
		strings.NewReader(`{"action":"start"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/llama start: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("toggle start status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}

	resp.Body.Close()

	// 2. The effective generation backend becomes native (selection policy:
	// native selected AND the loaded model validated natively executable).
	backendNative := false

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		engineResp, err := http.Get(server.URL + "/api/engine")
		if err == nil {
			var snap struct {
				Backend string `json:"backend"`
				State   string `json:"state"`
			}

			err = json.NewDecoder(engineResp.Body).Decode(&snap)
			engineResp.Body.Close()
			if err == nil && snap.Backend == "native" {
				backendNative = true
				break
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !backendNative {
		t.Fatalf("backend never became native after toggle start (GenerationCapable/load verdict broken)")
	}

	// 3. A plain-text run completes with a REAL, persisted native reply.
	sessResp, err := http.Post(
		server.URL+"/api/sessions",
		"application/json",
		strings.NewReader(`{"name":"native-e2e"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}

	var sess struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(sessResp.Body).Decode(&sess); err != nil {
		sessResp.Body.Close()
		t.Fatalf("decode session: %v", err)
	}
	sessResp.Body.Close()

	if sess.ID == "" {
		t.Fatal("session id empty")
	}

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(`{"sessionId":"`+sess.ID+`","message":"Say hello."}`),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	replied := false

	deadline = time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) && !replied {
		sessGet, err := http.Get(server.URL + "/api/sessions/" + sess.ID)
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}

		var got struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}

		err = json.NewDecoder(sessGet.Body).Decode(&got)
		sessGet.Body.Close()
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}

		for _, m := range got.Messages {
			if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
				replied = true
				break
			}
		}

		if !replied {
			time.Sleep(300 * time.Millisecond)
		}
	}

	if !replied {
		t.Fatal("no assistant reply persisted — native generation did not serve the run")
	}
}
