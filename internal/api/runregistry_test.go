package api

// runregistry_test.go — v1.2.6 regression tests for the missing-answer
// failure mode: a run that finishes BEFORE the activity WebSocket attaches.
//
// The v1.2.5 wire had NO authoritative replay: the late socket saw a bare
// `idle` sentinel, the frontend's grace guard dropped it (it is
// indistinguishable from the standby marker), and the UI stayed "running"
// with no visible answer — forever. These tests pin the v1.2.6 contract:
//
//   1. a socket attaching AFTER the run finished receives the idle sentinel
//      WITH the last-run outcome block (runId, outcome, endedAt, persisted);
//   2. a socket attaching DURING the run receives every activity stamped
//      with the SAME runId, ending with done;
//   3. the outcome registry honours its bounds.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// remoteFakeEngine spins an OpenAI-compatible SSE endpoint for one run.
func remoteFakeEngine(t *testing.T, reply string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		chunk := map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"role":    "assistant",
					"content": reply,
				},
				"finish_reason": nil,
			}},
		}

		enc, _ := json.Marshal(chunk)
		_, _ = w.Write([]byte("data: " + string(enc) + "\n\ndata: [DONE]\n\n"))
	}))

	t.Cleanup(server.Close)

	return server
}

// newRemoteServer builds a full API server against the fake remote
// engine. v1.3.3: returns the *Server handle as well so tests can poll
// the outcome registry — the deterministic settlement barrier (see
// waitForRunSettled).
func newRemoteServer(t *testing.T, engineURL string) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = cfg.DataDir + "/models"
	cfg.SessionsDir = cfg.DataDir + "/sessions"
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = engineURL + "/v1"
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

	return srv, server
}

func createSessionForRun(t *testing.T, server *httptest.Server) string {
	t.Helper()

	resp, err := http.Post(
		server.URL+"/api/sessions",
		"application/json",
		strings.NewReader(`{"name":"late-attach"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	defer resp.Body.Close()

	var sess struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	if sess.ID == "" {
		t.Fatal("session id empty")
	}

	return sess.ID
}

// dialActivityWS opens the activity WebSocket for a session.
func dialActivityWS(t *testing.T, server *httptest.Server, sessionID string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/activity?sessionId=" + sessionID

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial activity ws: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	return conn
}

// readFrameWithDeadline reads one JSON frame or fails the test.
func readFrameWithDeadline(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))

	var frame map[string]any
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read ws frame: %v", err)
	}

	return frame
}

// waitForReplyPersisted polls the session until an assistant message lands
// (the run is then COMPLETE — exactly the "fast run" precondition).
func waitForReplyPersisted(t *testing.T, server *httptest.Server, sessionID string) bool {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := http.Get(server.URL + "/api/sessions/" + sessionID)
		if err == nil {
			var got struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}

			err = json.NewDecoder(resp.Body).Decode(&got)
			resp.Body.Close()

			if err == nil {
				for _, m := range got.Messages {
					if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
						return true
					}
				}
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	return false
}

// waitForRunSettled is the DETERMINISTIC SETTLEMENT BARRIER (v1.3.3).
//
// PROBLEM: the previous barrier waited for the rolling summary to reach
// version >= 1 — but production writes the summary (durable step 1)
// BEFORE the agent.md handoff (durable step 2), recall indexing and the
// continuum rollover, and records the terminal outcome only after ALL
// of them. A test that stops waiting at the summary therefore observes
// a run whose settle-tail is still in flight: on a slow/loaded CI runner
// the poll can land in the summary→handoff window and read a missing
// agent.md (the v1.3.2 CI failure "agent.md handoff missing" — a real
// race, not a flaky assertion).
//
// FIX: settle on the TERMINAL OUTCOME itself. The v1.2.9 durable
// completion ordering guarantees the bounded outcome registry records
// the run's terminal outcome only AFTER every required durable artifact
// (rolled summary, agent.md handoff, recall index, continuum rollover)
// is on disk — so observing the terminal outcome IS observing complete
// settlement. Summary completion alone is never a settlement proxy
// again; the artifact assertions after this barrier verify the files
// directly.
func waitForRunSettled(t *testing.T, srv *Server, sessionID string) bool {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if rec, ok := srv.outcomes.latest(sessionID); ok && rec.Outcome != "" {
			return true
		}

		time.Sleep(5 * time.Millisecond)
	}

	return false
}

// waitForRunSettledFor is the repeated-settlement companion of the
// barrier: it waits for the run with the EXACT runId to record its
// terminal outcome. A session that fires sequential runs leaves the
// previous run's terminal record in the registry — waiting for "any
// terminal outcome" would cross the barrier on the STALE record while
// the new run's settle-tail is still in flight. Keying on runId makes
// the barrier precise for every settlement after the first.
func waitForRunSettledFor(t *testing.T, srv *Server, sessionID, runID string) bool {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if rec, ok := srv.outcomes.latest(sessionID); ok && rec.RunID == runID && rec.Outcome != "" {
			return true
		}

		time.Sleep(5 * time.Millisecond)
	}

	return false
}

// waitForRunOutcome waits for the run's terminal outcome to equal the
// exact wanted value (or any of wanted) — the v1.3.3 honesty companion:
// tests that pin WHICH terminal state a run settles into (e.g. a
// mandatory-handoff failure must settle "error", never "done") use this
// instead of accepting any terminal record.
func waitForRunOutcome(t *testing.T, srv *Server, sessionID string, want ...string) bool {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if rec, ok := srv.outcomes.latest(sessionID); ok {
			for _, w := range want {
				if rec.Outcome == w {
					return true
				}
			}

			// A terminal record that contradicts every wanted value is
			// an immediate, honest failure — do not spin to the deadline.
			if rec.Outcome != "" {
				return false
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	return false
}

// TestLateAttachSocketReceivesRunOutcome is THE v1.2.6 regression: the run
// completes before the socket attaches, and the first frame the socket
// receives must carry the authoritative outcome the frontend needs to
// finalise (previously: a bare idle sentinel the grace guard dropped).
func TestLateAttachSocketReceivesRunOutcome(t *testing.T) {
	engine := remoteFakeEngine(t, "2+2 is 4.")
	srv, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Fire the run and WAIT for it to fully settle before attaching.
	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"2+2?"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the fake-engine run never persisted a reply — test precondition broken")
	}

	// v1.2.8: wait for the settle tail (summary sidecar) before the
	// registry assertions — deterministic, not a guessed beat.
	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}

	// Give the run goroutine's deferred cleanup a beat to close the hub and
	// drop the run from the registry (the ordering the race depends on).
	time.Sleep(200 * time.Millisecond)

	// NOW attach: the socket must be told what happened.
	conn := dialActivityWS(t, server, sessionID)

	// v1.2.6 continuation: the FIRST frame is now the deterministic
	// attach acknowledgement (the contract run() waits on before the
	// POST); the outcome evidence follows on the idle sentinel.
	ack := readFrameWithDeadline(t, conn)

	if ack["type"] != "attached" {
		t.Fatalf("first frame type = %v, want attached (the attach ack)", ack["type"])
	}

	frame := readFrameWithDeadline(t, conn)

	if frame["type"] != "idle" {
		t.Fatalf("second frame type = %v, want idle (run already finished)", frame["type"])
	}

	lastRunRaw, ok := frame["lastRun"].(map[string]any)
	if !ok {
		t.Fatalf("idle sentinel carries NO lastRun evidence — the v1.2.5 missing-answer race is back: %v", frame)
	}

	if outcome, _ := lastRunRaw["outcome"].(string); outcome != "done" {
		t.Fatalf("lastRun.outcome = %q, want done", outcome)
	}

	if runID, _ := lastRunRaw["runId"].(string); runID == "" {
		t.Fatal("lastRun.runId is empty")
	}

	if endedAt, _ := lastRunRaw["endedAt"].(string); endedAt == "" {
		t.Fatal("lastRun.endedAt is empty")
	}

	if persisted, _ := lastRunRaw["persisted"].(bool); !persisted {
		t.Fatal("lastRun.persisted = false, want true (the reply was appended)")
	}
}

// TestActivitiesCarryRunIdAndDoneAttachesMidRun pins the runId contract for
// a socket that attaches DURING a run: every activity frame of the run
// carries the SAME runId and the run ends with a done frame.
func TestActivitiesCarryRunIdAndDoneAttachesMidRun(t *testing.T) {
	engine := remoteFakeEngine(t, "Hello!")
	srv, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"hi"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	conn := dialActivityWS(t, server, sessionID)

	sawRunID := ""
	sawDone := false
	frames := 0

	// v1.2.6 continuation: the attach ack is always the first frame.
	if ack := readFrameWithDeadline(t, conn); ack["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", ack["type"])
	}

	// The socket may attach after the fast run finished — in that case the
	// idle sentinel with lastRun is the next frame (also correct; a
	// run_snapshot may precede it when the runs-map entry lingers).
	// Either way, the loop must terminate on idle-with-evidence or done.
	for i := 0; i < 64; i++ {
		frame := readFrameWithDeadline(t, conn)
		frames++

		frameType, _ := frame["type"].(string)

		if frameType == "idle" {
			if last, ok := frame["lastRun"].(map[string]any); ok {
				if outcome, _ := last["outcome"].(string); outcome != "done" {
					t.Fatalf("idle lastRun.outcome = %q, want done", outcome)
				}
			}

			break
		}

		if frameType == "done" || frameType == "complete" {
			sawDone = true
		}

		if runID, _ := frame["runId"].(string); runID != "" {
			if sawRunID == "" {
				sawRunID = runID
			} else if sawRunID != runID {
				t.Fatalf("two different runIds on one run's frames: %q vs %q", sawRunID, runID)
			}
		}

		if sawDone {
			break
		}
	}

	if frames == 0 {
		t.Fatal("no frames arrived on the activity socket")
	}

	// v1.2.8: wait for the run goroutine's durable settle tail (reply
	// persist + summary sidecar) before t.TempDir cleanup.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}

	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}

	// Either we observed the run's frames (stamped with one runId + done),
	// or we attached after it settled (the idle-with-evidence path above).
	if sawRunID == "" && frames < 1 {
		t.Fatal("neither run frames with runId nor an idle evidence frame arrived")
	}

	// If frames of the run were observed, the runId must be present on them
	// and a done/complete must have arrived (the hub delivers buffered
	// events to subscribers that attached mid-run, then closes).
	if sawRunID != "" && !sawDone {
		// Attach-after-done: acceptable only when the LAST frame was idle
		// with evidence — but we would have exited via the idle branch.
		// Reaching here means frames arrived but no terminal event.
		t.Fatal("run frames carried a runId but neither done nor idle-evidence terminated the stream")
	}
}

// TestRunRegistryBounds pins the bounded-memory contract of the registry.
func TestRunRegistryBounds(t *testing.T) {
	r := newRunRegistry()

	// Ring bound: the 5th outcome for one session must evict the 1st.
	for i := 0; i < lastOutcomesPerSession+3; i++ {
		r.record("sess", runOutcome{
			RunID:   fmt.Sprintf("run-%d", i),
			Outcome: "done",
			EndedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	latest, ok := r.latest("sess")
	if !ok {
		t.Fatal("no latest outcome for sess")
	}

	if latest.RunID != fmt.Sprintf("run-%d", lastOutcomesPerSession+2) {
		t.Fatalf("latest = %q, want the newest run", latest.RunID)
	}

	// Total bound: the oldest session is evicted beyond lastOutcomesMax.
	for i := 0; i < lastOutcomesMax+5; i++ {
		r.record(fmt.Sprintf("session-%d", i), runOutcome{
			RunID:   "x",
			Outcome: "done",
		})
	}

	if _, ok := r.latest("sess"); ok {
		t.Fatal("the oldest session was not evicted at the total bound")
	}

	if _, ok := r.latest(fmt.Sprintf("session-%d", lastOutcomesMax+4)); !ok {
		t.Fatal("the newest session must be retained")
	}
}
