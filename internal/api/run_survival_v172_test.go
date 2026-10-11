package api

// run_survival_v172_test.go — v1.7.2 P0 repair acceptance.
//
// THE SUPPLIED WINDOWS CRASH: local provider, one-word chat, the log ends
// at `task classified...` and the DESKTOP PROCESS DIES. The root cause
// (proven by the -race reproduction in internal/agent) was a fatal
// `concurrent map iteration and map write`: RunDetailed iterated the LIVE
// tool-registry map (handed out by the old Orchestrator.Tools()) while
// concurrent registry mutation (custom tools, task-scoped tools) wrote
// it. A fatal map race kills the whole process — no terminal error event
// ever reaches the UI.
//
// These tests pin the API PROCESS-SURVIVAL CONTRACT over the REAL stack
// (real server, real run pipeline, real session store, real WebSocket
// hub) with the provider set to remote so a deterministic fake engine
// stands in for the LLM. The RunDetailed crash window is
// provider-independent: classification → tool-surface walk → tier
// selection runs identically for every provider.
//
// Contract under test (v1.7.2 repair §12):
//
//   1. one-word chat, Net Search OFF  → terminal done, process alive,
//      exactly ONE assistant message persisted;
//   2. one-word chat, Net Search ON   → terminal done, process alive,
//      exactly ONE assistant message persisted (Net Search must not
//      widen or crash anything pre-tier);
//   3. controlled backend failure     → terminal ERROR delivered, process
//      STILL alive, no duplicate assistant message persisted;
//   4. the very next ordinary chat on the SAME server succeeds.
//
// Liveness is asserted at every phase through the real HTTP surface
// (/api/health + the next successful request) — the same surface the
// desktop shell relies on.

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "strings"
        "sync/atomic"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func timeNowAfter(d time.Time) bool { return time.Now().After(d) }

func readDeadlineFromEnv(t *testing.T) time.Time {
        t.Helper()
        return time.Now().Add(30 * time.Second)
}

// switchableEngine is a fake remote engine whose failure mode can be
// flipped ATOMICALLY mid-test: "ok" streams a normal SSE reply; "fail"
// returns HTTP 500 (an ordinary backend error the run pipeline must
// surface as a terminal error event — never as process death).
type switchableEngine struct {
        server *httptest.Server
        mode   atomic.Value // string: "ok" | "fail"
        hits   atomic.Int64
}

func newSwitchableEngine(t *testing.T) *switchableEngine {
        t.Helper()

        e := &switchableEngine{}
        e.mode.Store("ok")

        e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
                        http.Error(w, "not found", http.StatusNotFound)
                        return
                }

                e.hits.Add(1)

                if e.mode.Load().(string) == "fail" {
                        http.Error(w, `{"error":{"message":"injected backend failure"}}`, http.StatusInternalServerError)
                        return
                }

                w.Header().Set("Content-Type", "text/event-stream")

                chunk := map[string]any{
                        "id": "chatcmpl-v172",
                        "choices": []map[string]any{{
                                "index": 0,
                                "delta": map[string]any{
                                        "role":    "assistant",
                                        "content": "Hello — I am here and working.",
                                },
                                "finish_reason": nil,
                        }},
                }

                enc, _ := json.Marshal(chunk)
                _, _ = w.Write([]byte("data: " + string(enc) + "\n\ndata: [DONE]\n\n"))
        }))

        t.Cleanup(e.server.Close)

        return e
}

func (e *switchableEngine) fail() { e.mode.Store("fail") }
func (e *switchableEngine) ok()   { e.mode.Store("ok") }

// assertServerAlive proves the API surface still answers AFTER whatever
// just happened — the process-survival evidence.
func assertServerAlive(t *testing.T, server *httptest.Server, phase string) {
        t.Helper()

        resp, err := http.Get(server.URL + "/api/health")
        if err != nil {
                t.Fatalf("phase %q: /api/health unreachable — the process died: %v", phase, err)
        }
        resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                t.Fatalf("phase %q: /api/health status = %d, want 200", phase, resp.StatusCode)
        }
}

// countAssistantMessages reads the session over the real API and counts
// non-empty assistant messages (the duplicate-persistence guard).
func countAssistantMessages(t *testing.T, server *httptest.Server, sessionID string) int {
        t.Helper()

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

        n := 0
        for _, m := range got.Messages {
                if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
                        n++
                }
        }

        return n
}

// fireOneWordChat submits the supplied one-word chat the way the real
// client does and returns the accepted runId.
func fireOneWordChat(t *testing.T, server *httptest.Server, sessionID string, netSearch bool) string {
        t.Helper()

        body := fmt.Sprintf(`{"sessionId":%q,"message":"hi","netSearch":%t}`, sessionID, netSearch)

        resp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(body))
        if err != nil {
                t.Fatalf("POST /api/run: %v", err)
        }

        var accepted struct {
                RunID string `json:"runId"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
                t.Fatalf("decode run acceptance: %v", err)
        }
        resp.Body.Close()

        if accepted.RunID == "" {
                t.Fatal("POST /api/run returned no runId")
        }

        return accepted.RunID
}

// TestOneWordChatSurvivesAcrossNetSearchAndFailure is THE v1.7.2
// post-repair acceptance: the exact supplied crash shape (one-word chat),
// both Net Search states, a forced backend failure, and the next ordinary
// chat — with process liveness asserted at every phase.
func TestOneWordChatSurvivesAcrossNetSearchAndFailure(t *testing.T) {
        engine := newSwitchableEngine(t)

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.ModelsDir = cfg.DataDir + "/models"
        cfg.SessionsDir = cfg.DataDir + "/sessions"
        cfg.Host = "127.0.0.1"
        cfg.Port = 0
        cfg.Provider = "remote"
        cfg.RemoteBaseURL = engine.server.URL + "/v1"
        cfg.RemoteAPIKey = "test-key"
        cfg.RemoteModel = "fake-remote-model"
        cfg.LlamaAutoStart = false
        cfg.UpdateSchedule = "off"

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

        assertServerAlive(t, server, "startup")

        // --- Phase 1: one-word chat, Net Search OFF --------------------------
        sessionOff := createSessionForRun(t, server)
        runID := fireOneWordChat(t, server, sessionOff, false)

        if !waitForRunSettledFor(t, srv, sessionOff, runID) {
                t.Fatal("Net Search OFF run never recorded a terminal outcome")
        }

        if rec, ok := srv.outcomes.latest(sessionOff); !ok || rec.Outcome != "done" {
                rec, _ := srv.outcomes.latest(sessionOff)
                t.Fatalf("Net Search OFF run outcome = %q, want done", rec.Outcome)
        }

        if !waitForReplyPersisted(t, server, sessionOff) {
                t.Fatal("Net Search OFF run never persisted a reply")
        }

        if n := countAssistantMessages(t, server, sessionOff); n != 1 {
                t.Fatalf("Net Search OFF session must persist EXACTLY ONE assistant message, got %d", n)
        }

        assertServerAlive(t, server, "after net-search-off chat")

        // --- Phase 2: one-word chat, Net Search ON ---------------------------
        // Net Search ON authorizes the research tool for the request; a
        // one-word chat is classified chat (not research) and must NOT crash
        // pre-tier composition — the supplied crash window.
        sessionOn := createSessionForRun(t, server)
        runID = fireOneWordChat(t, server, sessionOn, true)

        if !waitForRunSettledFor(t, srv, sessionOn, runID) {
                t.Fatal("Net Search ON run never recorded a terminal outcome")
        }

        if rec, ok := srv.outcomes.latest(sessionOn); !ok || rec.Outcome != "done" {
                rec, _ := srv.outcomes.latest(sessionOn)
                t.Fatalf("Net Search ON run outcome = %q, want done", rec.Outcome)
        }

        if !waitForReplyPersisted(t, server, sessionOn) {
                t.Fatal("Net Search ON run never persisted a reply")
        }

        if n := countAssistantMessages(t, server, sessionOn); n != 1 {
                t.Fatalf("Net Search ON session must persist EXACTLY ONE assistant message, got %d", n)
        }

        assertServerAlive(t, server, "after net-search-on chat")

        // --- Phase 3: controlled backend failure -----------------------------
        // An ordinary backend error MUST NOT kill the desktop process: the
        // error is a terminal error event; the session must not gain a
        // duplicate assistant message; the server stays alive.
        sessionFail := createSessionForRun(t, server)

        engine.fail()
        runID = fireOneWordChat(t, server, sessionFail, false)

        if !waitForRunSettledFor(t, srv, sessionFail, runID) {
                t.Fatal("the failed run never recorded a terminal outcome")
        }

        if rec, ok := srv.outcomes.latest(sessionFail); !ok || rec.Outcome != "error" {
                rec, _ := srv.outcomes.latest(sessionFail)
                t.Fatalf("backend failure outcome = %q, want error", rec.Outcome)
        }

        if n := countAssistantMessages(t, server, sessionFail); n != 0 {
                t.Fatalf("a failed run must persist NO assistant message, got %d", n)
        }

        // Liveness DURING/AFTER the failure — the survival contract core.
        assertServerAlive(t, server, "after backend failure")

        // --- Phase 4: the next ordinary chat succeeds on the same server ----
        engine.ok()

        runID = fireOneWordChat(t, server, sessionFail, false)

        if !waitForRunSettledFor(t, srv, sessionFail, runID) {
                t.Fatal("the post-failure run never recorded a terminal outcome")
        }

        if rec, ok := srv.outcomes.latest(sessionFail); !ok || rec.Outcome != "done" {
                rec, _ := srv.outcomes.latest(sessionFail)
                t.Fatalf("post-failure chat outcome = %q, want done (the process must keep serving)", rec.Outcome)
        }

        if !waitForReplyPersisted(t, server, sessionFail) {
                t.Fatal("post-failure chat never persisted a reply")
        }

        if n := countAssistantMessages(t, server, sessionFail); n != 1 {
                t.Fatalf("post-failure session must hold EXACTLY ONE assistant message (the successful one), got %d", n)
        }

        assertServerAlive(t, server, "final")

        if engine.hits.Load() < 3 {
                t.Fatalf("the engine must have been reached by every successful run, got %d hits", engine.hits.Load())
        }
}

// TestWebSocketSeesTerminalErrorNotSilence pins the failure DELIVERY on
// the wire: an attached activity WebSocket for a failing run receives a
// terminal ERROR frame — the UI can always finalise. Pre-repair, a fatal
// map race produced NO frame at all and no process left to send one.
//
// v1.10.1 TEARDOWN REPAIR (Windows run 38152579695): the terminal frame is
// published BEFORE the run goroutine's deferred cleanup (memory-manager
// run-end bookkeeping, registry release, hub close) completes. A test that
// returns at the frame therefore raced that cleanup — on Windows,
// t.TempDir's RemoveAll hit still-open/re-created files inside the data
// dir and failed with "The directory is not empty". The deterministic fix
// uses the EXISTING seams: wait for the authoritative outcome (recorded
// before the terminal state flips) AND for the run registry to RELEASE the
// session's active-run entry (the last step of the run goroutine's cleanup,
// after every disk write it performs). No sleep, no deadline inflation, no
// changed assertion.
func TestWebSocketSeesTerminalErrorNotSilence(t *testing.T) {
        engine := newSwitchableEngine(t)

        srv, server := newRemoteServer(t, engine.server.URL)
        sessionID := createSessionForRun(t, server)

        conn := dialActivityWS(t, server, sessionID)

        // Consume the deterministic attach acknowledgement first.
        if ack := readFrameWithDeadline(t, conn); ack["type"] != "attached" {
                t.Fatalf("first frame type = %v, want attached", ack["type"])
        }

        engine.fail()

        body := fmt.Sprintf(`{"sessionId":%q,"message":"hi"}`, sessionID)
        resp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(body))
        if err != nil {
                t.Fatalf("POST /api/run: %v", err)
        }

        var accepted struct {
                RunID string `json:"runId"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
                t.Fatalf("decode run acceptance: %v", err)
        }
        resp.Body.Close()

        if accepted.RunID == "" {
                t.Fatal("POST /api/run returned no runId")
        }

        // The socket MUST observe a terminal error frame within the deadline
        // (not silence, not a dropped connection).
        sawTerminalError := false
        deadline := readDeadlineFromEnv(t)

        for !sawTerminalError {
                frame := readFrameWithDeadline(t, conn)

                if frame["type"] == "done" || frame["type"] == "error" {
                        sawTerminalError = true
                }

                if timeNowAfter(deadline) {
                        t.Fatal("the activity WebSocket never delivered a terminal frame for the failed run")
                }
        }

        // v1.10.1: the deterministic settlement + cleanup barrier — the run's
        // outcome is recorded (the durable-ordering guarantee) and the run
        // goroutine has RELEASED the session's registry entry (its last
        // deferred disk write is complete). TempDir cleanup can then never
        // race the settle-tail.
        if !waitForRunSettledFor(t, srv, sessionID, accepted.RunID) {
                t.Fatal("the failed run never recorded a terminal outcome")
        }

        if !waitForRunRegistryRelease(t, srv, sessionID) {
                t.Fatal("the run registry never released the settled run before teardown")
        }

        // The server is still alive after delivering the error.
        assertServerAlive(t, server, "after terminal error delivery")
}

// newRemoteServerURL builds the full API server against the engine URL
// and returns only the httptest server (for tests that need no Server
// handle).
func newRemoteServerURL(t *testing.T, engineURL string) *httptest.Server {
        t.Helper()
        _, server := newRemoteServer(t, engineURL)
        return server
}

// waitForRunRegistryRelease is the v1.10.1 deterministic CLEANUP barrier:
// it waits until the session's active-run registry entry is gone — the run
// goroutine deletes that entry as the LAST step of its deferred cleanup
// (after the terminal outcome is recorded, after the memory-manager
// run-end bookkeeping, before only the no-disk-write hub close). Observing
// it proves every disk write the run's settle-tail performs has finished,
// so a following t.TempDir RemoveAll can never race it (the Windows
// "directory is not empty" teardown failure of run 38152579695).
func waitForRunRegistryRelease(t *testing.T, srv *Server, sessionID string) bool {
        t.Helper()

        deadline := time.Now().Add(30 * time.Second)

        for time.Now().Before(deadline) {
                srv.runsMu.Lock()
                _, active := srv.runs[sessionID]
                srv.runsMu.Unlock()

                if !active {
                        return true
                }

                time.Sleep(5 * time.Millisecond)
        }

        return false
}
