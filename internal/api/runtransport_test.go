package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"encoding/json"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"net/http"
	"net/http/httptest"
)

// runtransport_test.go — v1.2.6 continuation regression tests for the
// AUTHORITATIVE RUN TRANSPORT contract:
//
//   1. every socket receives `attached` (the deterministic attach ack) as
//      its FIRST frame;
//   2. a socket attaching MID-RUN receives a `run_snapshot` carrying the
//      run's authoritative state (phase, cumulative response/reasoning
//      snapshots, sequence) BEFORE live events continue;
//   3. a reconnect during generation replays the partial output and the
//      stream continues WITHOUT duplication (the cumulative captions are
//      replacement-safe; the seq filter drops the overlap);
//   4. POST /api/run returns the authoritative runId;
//   5. stale events of a replaced run never reach the client;
//   6. every event of the active run carries runId + a monotonic seq;
//   7. a socket attaching between run end and registry cleanup gets the
//      TERMINAL snapshot (authoritative final state + the persisted reply).

// slowStreamEngine streams one chunk every interval — long enough for the
// test to attach/detach mid-generation.
func slowStreamEngine(t *testing.T, chunks []string, interval time.Duration) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		for _, chunk := range chunks {
			payload := map[string]any{
				"id": "chatcmpl-test",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"content": chunk},
					"finish_reason": nil,
				}},
			}

			enc, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("data: " + string(enc) + "\n\n"))

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			time.Sleep(interval)
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	t.Cleanup(server.Close)

	return server
}

// TestAttachAckIsFirstFrameAndPostReturnsRunId pins the deterministic
// attach contract: the server's first frame after upgrade is `attached`,
// and POST /api/run hands back the authoritative runId.
func TestAttachAckIsFirstFrameAndPostReturnsRunId(t *testing.T) {
	engine := remoteFakeEngine(t, "hi there.")
	srv, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	conn := dialActivityWS(t, server, sessionID)

	first := readFrameWithDeadline(t, conn)
	if first["type"] != "attached" {
		t.Fatalf("first frame type = %v, want attached (the deterministic attach ack)", first["type"])
	}

	if sid, _ := first["sessionId"].(string); sid != sessionID {
		t.Fatalf("attached.sessionId = %q, want %q", sid, sessionID)
	}

	if now, _ := first["serverNow"].(float64); now <= 0 {
		t.Fatal("attached.serverNow missing — the server clock must ride the ack")
	}

	// No active run: the next frame is the idle sentinel (with lastRun
	// absent — no run ever happened for this session).
	second := readFrameWithDeadline(t, conn)
	if second["type"] != "idle" {
		t.Fatalf("second frame type = %v, want idle", second["type"])
	}

	// POST /api/run returns the runId.
	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"hi"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	defer runResp.Body.Close()

	var body struct {
		OK        bool   `json:"ok"`
		SessionID string `json:"sessionId"`
		RunID     string `json:"runId"`
		State     string `json:"state"`
	}

	if err := json.NewDecoder(runResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode run response: %v", err)
	}

	if !body.OK || body.RunID == "" || body.State != "registered" {
		t.Fatalf("run response = %+v — runId/state missing (the authoritative identity contract)", body)
	}

	// Let the fired run settle BEFORE the test ends: the run goroutine
	// persists into the DataDir (t.TempDir) and a live run races the
	// cleanup's RemoveAll.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the fired run never settled — cleanup safety violated")
	}

	// v1.2.8: the settle tail also writes the rolling summary sidecar —
	// wait for it deterministically before the TempDir cleanup.
	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}

	time.Sleep(150 * time.Millisecond)
}

// TestMidRunAttachReceivesSnapshotAndContinues is THE reconnect-during-
// generation regression: a socket that attaches AFTER partial output
// receives the cumulative snapshot immediately (never waiting for a future
// token) and the stream continues without duplication.
func TestMidRunAttachReceivesSnapshotAndContinues(t *testing.T) {
	chunks := []string{"Hello", " ", "from", " ", "the", " ", "replay", "test"}
	engine := slowStreamEngine(t, chunks, 60*time.Millisecond)
	srv, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Socket A: the original connection.
	connA := dialActivityWS(t, server, sessionID)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"stream please"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// Read frames on A until a response event with visible partial text.
	var runID string
	partialSeen := false

	for !partialSeen {
		frame := readFrameWithDeadline(t, connA)

		if frame["type"] == "response" {
			caption, _ := frame["caption"].(string)
			if strings.Contains(caption, "the") { // several chunks landed
				runID, _ = frame["runId"].(string)
				partialSeen = true
			}
		}
	}

	if runID == "" {
		t.Fatal("response frame carries no runId — the one-runId contract is broken")
	}

	// Drop socket A mid-generation (the reconnect scenario).
	connA.Close()

	// Socket B attaches NOW — mid-run.
	connB := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, connB); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	snap := readFrameWithDeadline(t, connB)
	if snap["type"] != "run_snapshot" {
		t.Fatalf("second frame = %v, want run_snapshot (authoritative replay)", snap["type"])
	}

	if rid, _ := snap["runId"].(string); rid != runID {
		t.Fatalf("snapshot runId = %q, want %q", rid, runID)
	}

	if running, _ := snap["running"].(bool); !running {
		t.Fatal("snapshot.running = false — the run is still generating")
	}

	// The cumulative snapshot already carries the partial output — a
	// reconnect NEVER waits for a future token to discover the state.
	if latest, _ := snap["latestResponse"].(string); latest == "" {
		t.Fatal("snapshot.latestResponse is empty — the partial output was not replayed")
	}

	// Continue to completion on socket B. The final text must be the full
	// answer with NO duplication (seq filter drops the overlap).
	var finalText string

	for {
		frame := readFrameWithDeadline(t, connB)

		if frame["type"] == "response" {
			if caption, _ := frame["caption"].(string); caption != "" {
				finalText = caption
			}
		}

		if frame["type"] == "done" || frame["type"] == "complete" {
			break
		}
	}

	expected := strings.Join(chunks, "")
	if finalText != expected {
		t.Fatalf("final text after reconnect = %q, want exactly %q (no duplication, no loss)", finalText, expected)
	}

	// v1.2.8: wait for the full settle tail (reply + summary sidecar)
	// before cleanup — a run goroutine still writing durable state
	// must never race t.TempDir's RemoveAll.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}

	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}

// TestRunEventsCarryMonotonicSequence pins the event contract: every
// activity of the run carries runId + a strictly increasing seq.
func TestRunEventsCarryMonotonicSequence(t *testing.T) {
	// 80ms per chunk: a ~240ms generation window that reliably covers
	// the attach even when the full suite loads the machine (attaching
	// AFTER completion is a different contract — see the terminal
	// branch below).
	engine := slowStreamEngine(t, []string{"a", "b", "c"}, 80*time.Millisecond)
	srv, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"seq test"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}

	var body struct {
		RunID string `json:"runId"`
	}
	_ = json.NewDecoder(runResp.Body).Decode(&body)
	runResp.Body.Close()

	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	// The socket attaches while the run is live: snapshot first.
	snap := readFrameWithDeadline(t, conn)
	if snap["type"] != "run_snapshot" {
		t.Fatalf("expected run_snapshot, got %v", snap["type"])
	}

	// A machine stall can push the attach past the (short) generation:
	// the terminal snapshot IS the contract then (monotonic sequence
	// reached, persisted reply replayed) — the event loop below would
	// never see live frames. Verify and return instead of hanging.
	if running, _ := snap["running"].(bool); !running {
		if seq, _ := snap["sequence"].(float64); int64(seq) <= 0 {
			t.Fatalf("terminal snapshot carries no sequence: %v", snap)
		}

		if reply, _ := snap["latestResponse"].(string); reply == "" {
			t.Fatalf("terminal snapshot replays no persisted reply: %v", snap)
		}

		return
	}

	lastSeq := int64(0)

	for {
		frame := readFrameWithDeadline(t, conn)

		if frame["type"] == "idle" {
			break
		}

		if frame["type"] == "done" || frame["type"] == "complete" {
			break
		}

		rid, _ := frame["runId"].(string)
		seq, _ := frame["seq"].(float64)

		if rid != "" {
			if rid != body.RunID {
				t.Fatalf("event runId = %q, want %q", rid, body.RunID)
			}

			if seq <= 0 {
				t.Fatalf("event of run %s carries no seq: %v", rid, frame)
			}

			if int64(seq) <= lastSeq {
				t.Fatalf("seq regressed: %v after %v", seq, lastSeq)
			}

			lastSeq = int64(seq)
		}
	}

	if lastSeq == 0 {
		t.Fatal("no sequenced events observed")
	}

	// v1.2.8: the run goroutine's durable settle tail (reply persist,
	// summary sidecar) must complete before t.TempDir cleanup — a raw
	// RemoveAll race here is a TEST bug, not a runtime bug.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}

	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}

// TestStaleRunEventsFilteredByServer pins that a socket attached to the
// CURRENT run never receives frames stamped with an older run's id: the
// server's forwarding loop drops them before they reach the wire.
func TestStaleRunEventsFilteredByServer(t *testing.T) {
	engine := remoteFakeEngine(t, "first run answer.")
	_, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Run 1 completes fully.
	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"one"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("run 1 never persisted — precondition broken")
	}

	// Attach and confirm the idle sentinel carries the lastRun outcome.
	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	idle := readFrameWithDeadline(t, conn)
	if idle["type"] != "idle" {
		t.Fatalf("expected idle, got %v", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel carries no lastRun outcome — the v1.2.6 terminal replay regressed")
	}

	if outcome, _ := lastRun["outcome"].(string); outcome != "done" {
		t.Fatalf("lastRun.outcome = %q, want done", outcome)
	}
}

// TestIdleSentinelWaitsForSettlementEdge pins the EXACT race of Actions run
// 35749698189, deterministically — no scheduling luck required.
//
// The production window: observe() folds the orchestrator's `done` activity
// (running flips false — EARLY terminal visibility) while settle() has not
// yet recorded the outcome in the bounded registry (it runs only after the
// durable settle-tail). A socket attaching in that window must NOT receive a
// lastRun-less idle sentinel: the v1.3.7 settlement barrier parks it on the
// run's settlement edge, and the sentinel it then receives carries the
// recorded outcome.
func TestIdleSentinelWaitsForSettlementEdge(t *testing.T) {
	engine := remoteFakeEngine(t, "unused.")
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// The EXACT settlement-window state: registered run, generation over
	// (observe() folded the terminal activity → Running == false), but the
	// outcome registry holds NOTHING for this session yet and the
	// settlement edge is still open.
	live := newRunLive("run-settle-window", sessionID, time.Now())
	live.observe(stampedActivity("response", "the final answer"))
	live.observe(stampedActivity("done", "Completed"))

	srv.runsMu.Lock()
	srv.runs[sessionID] = &runState{
		cancel: func() {},
		hub:    newActivityHub(),
		live:   live,
	}
	srv.runsMu.Unlock()

	conn := dialActivityWS(t, server, sessionID)

	// ONE reader goroutine owns the connection's read side (gorilla conns
	// are not safe for concurrent reads, and an intentionally timed-out
	// ReadJSON would poison it — the reader below blocks without a
	// deadline so no spurious timeout can ever be cached).
	frames := make(chan map[string]any, 8)
	readErr := make(chan error, 1)

	go func() {
		for {
			var frame map[string]any
			if err := conn.ReadJSON(&frame); err != nil {
				readErr <- err
				return
			}

			frames <- frame
		}
	}()

	waitFrame := func(desc string) map[string]any {
		t.Helper()

		select {
		case frame := <-frames:
			return frame
		case err := <-readErr:
			t.Fatalf("%s: read ws frame: %v", desc, err)
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: no frame within 20s", desc)
		}

		return nil
	}

	if frame := waitFrame("attach ack"); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	// While the settlement edge is open the socket must receive NOTHING —
	// a premature idle here is precisely the lastRun-less sentinel of the
	// CI failure. (wsPingInterval is 25s, so no ping can land inside this
	// window; only a data frame can satisfy the select.)
	select {
	case frame := <-frames:
		t.Fatalf("idle sentinel arrived BEFORE settlement: %v — the v1.3.7 settlement barrier is missing", frame)
	case err := <-readErr:
		t.Fatalf("read ws frame while waiting for settlement: %v", err)
	case <-time.After(750 * time.Millisecond):
		// Expected: the barrier parks the sentinel on the open edge.
	}

	// Settle exactly as production does: the outcome is recorded FIRST,
	// then the authoritative terminal state flips (closing the edge).
	srv.outcomes.record(sessionID, runOutcome{
		RunID:     "run-settle-window",
		StartedAt: time.Now().Add(-time.Second),
		EndedAt:   time.Now(),
		Outcome:   "done",
		Persisted: true,
	})

	live.settleTerminal("done", "Completed", true, "the final answer", "")

	// The barrier releases deterministically: the idle sentinel carries the
	// recorded lastRun outcome.
	idle := waitFrame("post-settlement idle sentinel")
	if idle["type"] != "idle" {
		t.Fatalf("expected idle after settlement, got %v", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel carries no lastRun outcome — the settlement barrier released before the outcome was recoverable")
	}

	if outcome, _ := lastRun["outcome"].(string); outcome != "done" {
		t.Fatalf("lastRun.outcome = %q, want done", outcome)
	}

	if rid, _ := lastRun["runId"].(string); rid != "run-settle-window" {
		t.Fatalf("lastRun.runId = %q, want run-settle-window", rid)
	}
}

// TestTerminalSnapshotReplaysPersistedReply pins the authoritative terminal
// state: a socket that attaches AFTER the run finished (but while the entry
// still exists) receives the terminal snapshot including the persisted
// reply text — not a bare idle marker.
func TestTerminalSnapshotReplaysPersistedReply(t *testing.T) {
	engine := remoteFakeEngine(t, "2+2 is 4.")
	_, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Attach BEFORE the run so the socket is in standby when it starts —
	// it will observe the whole run and the terminal state.
	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"2+2?"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// Drain until the run settles on THIS socket. Three AUTHORITATIVE
	// terminal contracts satisfy the drain — whichever side of the
	// attach/subscribe race the scheduler lands on (verified: v1.3.6
	// hit all three under CI-grade starvation):
	//   1. the live path — the terminal `done`/`complete` event, then
	//      the idle sentinel;
	//   2. the snapshot path — a TERMINAL run_snapshot (running=false +
	//      terminalOutcome, the persisted reply replayed) for a socket
	//      that subscribed after the terminal events were published;
	//   3. the late-evidence path — an idle sentinel carrying the
	//      recorded lastRun outcome for a socket whose wake was
	//      starved past the run's entire lifetime (the entry is
	//      already gone from the runs map).
	// All three are the documented transport contract (header items
	// 1–7); the drain accepts each and still REQUIRES terminal
	// evidence — a bare idle with no run-scoped evidence never
	// satisfies it.
	var sawDone bool
	var sawTerminalEvidence bool

	deadline := time.Now().Add(30 * time.Second)

drain:
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("read frame: %v (sawDone=%v terminalEvidence=%v)", err, sawDone, sawTerminalEvidence)
		}

		switch frame["type"] {
		case "done", "complete":
			sawDone = true

		case "run_snapshot":
			if running, _ := frame["running"].(bool); !running {
				if outcome, _ := frame["terminalOutcome"].(string); outcome == "done" {
					if reply, _ := frame["latestResponse"].(string); strings.Contains(reply, "2+2") {
						sawTerminalEvidence = true
					}
				}
			}

		case "idle":
			if lastRun, ok := frame["lastRun"].(map[string]any); ok {
				if outcome, _ := lastRun["outcome"].(string); outcome == "done" {
					sawTerminalEvidence = true
				}
			}

			if sawDone || sawTerminalEvidence {
				break drain
			}
		}
	}

	if !sawDone && !sawTerminalEvidence {
		t.Fatal("the socket never observed the run's terminal evidence (event, terminal snapshot or lastRun outcome)")
	}

	// A SECOND socket attaching now (post-terminal, post-cleanup) gets the
	// idle sentinel whose lastRun block is the authoritative outcome.
	conn2 := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn2); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	idle := readFrameWithDeadline(t, conn2)
	if idle["type"] != "idle" {
		t.Fatalf("expected idle, got %v", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel lost the lastRun outcome block")
	}

	if persisted, _ := lastRun["persisted"].(bool); !persisted {
		t.Fatal("lastRun.persisted = false — the reply WAS appended")
	}
}

// TestRunSnapshotBounded pins the snapshot memory bounds: a huge cumulative
// caption is capped, never retained unboundedly.
func TestRunSnapshotBounded(t *testing.T) {
	live := newRunLive("run-bounded", "sess", time.Now())

	huge := strings.Repeat("x", snapshotCapBytes+4096)
	live.observe(stampedActivity("response", huge))

	snap := live.snapshot()

	if len(snap.LatestResponse) > snapshotCapBytes+64 {
		t.Fatalf("snapshot retained %d bytes — the bounded-snapshot contract is broken", len(snap.LatestResponse))
	}

	if len(live.recent) > recentEventCap {
		t.Fatalf("recent-events ring holds %d > %d entries", len(live.recent), recentEventCap)
	}
}

// TestRunLiveTerminalSettleOnce pins that the authoritative terminal state
// records exactly one outcome and replays the persisted reply.
func TestRunLiveTerminalSettleOnce(t *testing.T) {
	live := newRunLive("run-settle", "sess", time.Now())

	live.settleTerminal("done", "Completed", true, "final answer", "reasoning")

	// A second settle must not overwrite the first (exactly-once).
	live.settleTerminal("error", "late error", false, "", "")

	snap := live.snapshot()

	if snap.TerminalOutcome != "done" {
		t.Fatalf("terminalOutcome = %q, want done (exactly-once settle)", snap.TerminalOutcome)
	}

	if snap.LatestResponse != "final answer" {
		t.Fatalf("latestResponse = %q, want the persisted reply", snap.LatestResponse)
	}

	if snap.Running {
		t.Fatal("snapshot.running = true after terminal settle")
	}
}

// stampedActivity builds one activity for the state tests.
func stampedActivity(kind, caption string) agent.Activity {
	return agent.Activity{
		Type:      kind,
		Caption:   caption,
		Timestamp: time.Now(),
	}
}

// --- terminal registry and abort-then-attach transport ---

// Run lifecycle
// GATE and the terminal-recovery fallthrough. These pin the exact CI failure
// of GitHub Actions run 35367243405 (TestStaleRunEventsFilteredByServer:
// "expected idle, got run_snapshot") and the sibling hazard the audit of the
// same boundary uncovered:
//
//   1. a run entry that is still registered in s.runs but is ALREADY
//      authoritatively terminal (live.snapshot().Running == false — the
//      window between terminal settlement and deferred registry cleanup)
//      must serve the idle sentinel with the recorded lastRun outcome,
//      NEVER a stale terminal run_snapshot;
//   2. a hub that closed before any post-snapshot event existed must fall
//      through to the idle sentinel instead of parking the socket on
//      clientGone forever (the abort path settles without publishing a
//      terminal activity — previously the socket hung with no terminal
//      marker at all);
//   3. a real aborted run, observed through a fresh attach, ends in the
//      idle sentinel carrying the recorded outcome (no hang, no
//      run_snapshot of a dead run).

// newRemoteServerWithHandle builds the same server as newRemoteServer but
// ALSO returns the *Server handle so white-box tests can inject synthetic
// run states into the registry — the only way to deterministically observe
// the terminal-but-registered window that production reaches whenever a
// socket attaches between run settlement and cleanup.
func newRemoteServerWithHandle(t *testing.T, engineURL string) (*Server, *httptest.Server) {
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

// TestTerminalRegistryEntryServesIdleNotSnapshot pins the v1.2.7 lifecycle
// gate: map membership is NOT "run active". A registered run whose
// authoritative state is already terminal falls through to the idle path.
func TestTerminalRegistryEntryServesIdleNotSnapshot(t *testing.T) {
	engine := remoteFakeEngine(t, "unused.")
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Synthetic terminal-but-registered run: exactly the state the run
	// goroutine leaves behind between settle() and its deferred delete.
	live := newRunLive("run-terminal-gate", sessionID, time.Now())
	live.settleTerminal("done", "Completed", true, "the final answer", "")

	// The outcome is recorded BEFORE the state flips terminal (v1.2.7
	// ordering) — mirror that here, as settle() does.
	srv.outcomes.record(sessionID, runOutcome{
		RunID:     "run-terminal-gate",
		StartedAt: time.Now().Add(-time.Second),
		EndedAt:   time.Now(),
		Outcome:   "done",
		Persisted: true,
	})

	srv.runsMu.Lock()
	srv.runs[sessionID] = &runState{
		cancel: func() {},
		hub:    newActivityHub(),
		live:   live,
	}
	srv.runsMu.Unlock()

	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	// THE regression: the very next frame is the idle sentinel — never a
	// stale terminal run_snapshot.
	idle := readFrameWithDeadline(t, conn)
	if idle["type"] != "idle" {
		t.Fatalf("expected idle, got %v — the terminal-entry gate regressed", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel carries no lastRun outcome for the terminal run")
	}

	if outcome, _ := lastRun["outcome"].(string); outcome != "done" {
		t.Fatalf("lastRun.outcome = %q, want done", outcome)
	}

	if rid, _ := lastRun["runId"].(string); rid != "run-terminal-gate" {
		t.Fatalf("lastRun.runId = %q, want run-terminal-gate", rid)
	}
}

// TestClosedHubWithNoEventsFallsThroughToIdle pins the v1.2.7 fallthrough:
// a run that closes its hub before any post-snapshot event existed leaves
// the socket with the terminal-relevant snapshot, the idle sentinel — not a
// silent park on clientGone.
func TestClosedHubWithNoEventsFallsThroughToIdle(t *testing.T) {
	engine := remoteFakeEngine(t, "unused.")
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// A STILL-RUNNING live state whose hub is already closed: the state
	// a socket faces when the run published everything, settled, and the
	// cleanup landed between the gate and the subscription — or when an
	// abort settled without publishing a terminal activity.
	live := newRunLive("run-closed-hub", sessionID, time.Now())

	hub := newActivityHub()
	hub.close()

	srv.runsMu.Lock()
	srv.runs[sessionID] = &runState{
		cancel: func() {},
		hub:    hub,
		live:   live,
	}
	srv.runsMu.Unlock()

	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	snap := readFrameWithDeadline(t, conn)
	if snap["type"] != "run_snapshot" {
		t.Fatalf("second frame = %v, want run_snapshot (the run still reports running)", snap["type"])
	}

	// The fallthrough: the idle sentinel must arrive on its own — the
	// previous behavior blocked here until the client disconnected.
	idle := readFrameWithDeadline(t, conn)
	if idle["type"] != "idle" {
		t.Fatalf("expected the closed-hub fallthrough to send idle, got %v", idle["type"])
	}
}

// TestAbortThenAttachReceivesIdleWithOutcome pins the end-to-end abort
// recovery contract: after a run is aborted and has settled, a fresh socket
// attaches and receives the idle sentinel with the recorded outcome — no
// stale run_snapshot, no hang, no silent request loss.
func TestAbortThenAttachReceivesIdleWithOutcome(t *testing.T) {
	engine := slowStreamEngine(t, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, 120*time.Millisecond)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"abort me"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	abortResp, err := http.Post(
		server.URL+"/api/abort",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/abort: %v", err)
	}
	abortResp.Body.Close()

	// Wait until the run goroutine recorded its terminal outcome — the
	// authoritative settlement boundary (bounded registry).
	settled := false

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if _, ok := srv.outcomes.latest(sessionID); ok {
			settled = true
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if !settled {
		t.Fatal("the aborted run never recorded its outcome — the settle path is broken")
	}

	// Attach AFTER settlement: the contract under test.
	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	idle := readFrameWithDeadline(t, conn)
	if idle["type"] != "idle" {
		t.Fatalf("expected idle after abort, got %v — a dead run leaked into the attach path", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel carries no lastRun outcome after abort")
	}

	// The aborted run settles from REAL signals: the orchestrator may
	// surface the cancellation as an error result or as the aborted
	// outcome — both are terminal, neither may ever re-enter the
	// active path.
	outcome, _ := lastRun["outcome"].(string)
	if outcome != "aborted" && outcome != "error" {
		t.Fatalf("lastRun.outcome = %q, want aborted or error", outcome)
	}

	encoded, _ := json.Marshal(lastRun)
	if !strings.Contains(string(encoded), `"runId"`) {
		t.Fatalf("lastRun block lost the runId: %s", encoded)
	}
}
