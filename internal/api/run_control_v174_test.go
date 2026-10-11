package api

// run_control_v174_test.go — v1.7.4 PAUSE / EDIT / RESUME regression coverage.
//
// The feature under test spans three authorities that must stay ONE
// contract:
//
//      runstate.go        runLive pause state machine (revision, pausedDraft)
//      runcontrol.go      handlers + durable <DataDir>/runs/paused/<runId>.json
//      agent/runcontrol   RunControl observed by the orchestrator at safe
//                         boundaries (stream delta / between-tools / round top)
//
// Contract pinned here:
//
//       1. pause mid-generation → "pausing" → "paused"; durable checkpoint with
//          the accepted draft; the run is NOT settled; no assistant message yet;
//          the WS run_snapshot replays phase + pausedDraft + revision.
//       2. resume joins draft + continuation into EXACTLY ONE assistant
//          message (never a duplicate user message), deletes the checkpoint,
//          and a follow-up ordinary chat still works.
//       3. editing the user prompt while paused replaces the transcript's user
//          message IN PLACE (one user message, never two) and bumps revision.
//       4. editing the draft while paused trims the authoritative answer.
//       5. stale revisions are rejected 409 with the current revision named.
//       6. double pause is idempotent (200, still reaches paused).
//       7. double resume is rejected 409 (no longer paused).
//       8. stop after pause settles "aborted", deletes the checkpoint, keeps
//          the process alive, and the next ordinary run works.
//       9. reconnecting during PAUSED replays the same revision + draft and
//          resume works from the fresh socket.
//      10. a checkpointed paused run whose registry entry is gone (restart)
//          is recovered by recoverPausedRun and resumes to completion.
//      11. the runLive state machine transitions, pure unit.
//      12. joinDraftContinuation + completeRoundTail, pure unit.
//
// The engine stand-in is remoteFakeEngineSlow: an OpenAI-compatible SSE
// stream paced at ~20ms per delta, so the test reliably observes the first
// delta and POSTs the pause while ~1s of stream remains.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// --- the pausable fake engine -----------------------------------------------

// remoteFakeEngineSlow streams `chunks` as SSE content deltas, one every
// delayPerChunk (flushed), then [DONE] — every /v1/chat/completions request.
// The pacing is what makes a MID-GENERATION pause deterministic: the test
// waits for the first observed delta, POSTs the pause, and the orchestrator
// still has the rest of the stream to observe it at a delta boundary.
// (remoteFakeEngine, the original one-shot engine, is untouched — other
// tests depend on its instant reply.)
func remoteFakeEngineSlow(t *testing.T, chunks []string, delayPerChunk time.Duration) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		for _, chunk := range chunks {
			payload := map[string]any{
				"id": "chatcmpl-v174",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"content": chunk},
					"finish_reason": nil,
				}},
			}

			enc, _ := json.Marshal(payload)

			if _, err := w.Write([]byte("data: " + string(enc) + "\n\n")); err != nil {
				return // the run paused/canceled the stream — nothing to deliver to
			}

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			time.Sleep(delayPerChunk)
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	t.Cleanup(server.Close)

	return server
}

// pauseStreamChunks builds `n` distinct, order-marked content chunks. The
// markers let a test prove WHICH prefix was accepted as the paused draft.
func pauseStreamChunks(n int) []string {
	chunks := make([]string, 0, n)

	for i := 0; i < n; i++ {
		chunks = append(chunks, fmt.Sprintf("chunk-%02d ", i))
	}

	return chunks
}

const (
	pauseChunkCount = 60
	pauseChunkDelay = 20 * time.Millisecond
	pausePrompt     = "Please write a long answer."
)

// --- small HTTP / polling helpers -------------------------------------------

// postJSON posts a JSON object and decodes the response body.
func postJSON(t *testing.T, server *httptest.Server, path string, body map[string]any) (int, map[string]any) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body for %s: %v", path, err)
	}

	resp, err := http.Post(server.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("POST %s: decode response: %v", path, err)
	}

	return resp.StatusCode, decoded
}

// postRunMessage submits one ordinary run and returns the accepted runId.
func postRunMessage(t *testing.T, server *httptest.Server, sessionID, message string) string {
	t.Helper()

	status, body := postJSON(t, server, "/api/run", map[string]any{
		"sessionId": sessionID,
		"message":   message,
	})

	if status != http.StatusOK {
		t.Fatalf("POST /api/run: status = %d, body = %v", status, body)
	}

	runID, _ := body["runId"].(string)
	if runID == "" {
		t.Fatalf("POST /api/run returned no runId: %v", body)
	}

	return runID
}

// registeredRun returns the session's current runState (nil when the entry
// is gone — released on terminal settlement).
func registeredRun(srv *Server, sessionID string) *runState {
	srv.runsMu.Lock()
	defer srv.runsMu.Unlock()

	return srv.runs[sessionID]
}

// waitForRunResponse polls the authoritative live state until the run has
// streamed its first response delta (the deterministic pre-pause beat).
//
// v1.8.0 HAZARD NOTE: this helper is valid ONLY for the INITIAL generation
// of a run, where LatestResponse is still empty. LatestResponse is a
// CUMULATIVE snapshot that survives pause → resume, so calling this after a
// resume returns the STALE pre-resume text immediately — it never proves
// that the resumed generation is live. That exact defect broke
// TestPauseResumePauseAgainThenResumeCompletes on the Windows runner
// (Actions run 36553559366): the second pause was issued against a
// not-yet-proven resumed generation and the run reached "done" while the
// test waited for "paused". After a resume, use waitForResumedGeneration,
// which requires evidence of a NEW authoritative event.
func waitForRunResponse(t *testing.T, srv *Server, sessionID string) string {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		rs := registeredRun(srv, sessionID)

		if rs != nil && rs.live != nil {
			snap := rs.live.snapshot()

			if snap.LatestResponse != "" {
				return snap.LatestResponse
			}

			if snap.TerminalOutcome != "" {
				t.Fatalf("run settled %q (error %q) before streaming any delta",
					snap.TerminalOutcome, snap.Error)
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("the run never streamed its first response delta")

	return ""
}

// resumedGenerationEvidence is the v1.8.0 synchronization contract for the
// window between a RESUME and the resumed generation's first observable
// output. It decides whether `now` proves that the RESUMED generation has
// published a new authoritative event beyond the captured `pre` snapshot.
//
// The rules, in order:
//
//  1. A strictly greater Sequence: every published event is stamped with
//     nextSeq() BEFORE it folds (publish → observe happens-before any
//     snapshot that sees the fold), so a response fold is always preceded
//     by its sequence bump. Without a newer sequence nothing was published.
//  2. A CHANGED cumulative response/reasoning snapshot: those fields move
//     ONLY when a response/reasoning activity folds. A status caption, a
//     task fold or any other event type bumps the sequence without being
//     generation evidence — and the pre-resume cumulative text is exactly
//     the STALE state this contract exists to reject.
//
// Together: stale cumulative state (same sequence, same text) is never
// evidence, a bare sequence bump is never evidence, and only a genuinely
// new response/reasoning event of the resumed generation is.
func resumedGenerationEvidence(pre, now runSnapshot) bool {
	if now.Sequence <= pre.Sequence {
		return false
	}

	return now.LatestResponse != pre.LatestResponse ||
		now.LatestReasoning != pre.LatestReasoning
}

// waitForResumedGeneration polls the authoritative live state until the
// RESUMED generation has objectively emitted a new response/reasoning event
// beyond the pre-resume snapshot `pre` — the deterministic post-resume beat
// (capture pre BEFORE POSTing /api/run/resume). Fails fast if the run
// settles first: a settled run can no longer be paused, and hiding that
// behind a timeout would lie about the synchronization. No delays, no
// "wait longer": the wait condition itself is the proof.
func waitForResumedGeneration(t *testing.T, srv *Server, sessionID string, pre runSnapshot) runSnapshot {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		rs := registeredRun(srv, sessionID)

		if rs != nil && rs.live != nil {
			snap := rs.live.snapshot()

			if snap.TerminalOutcome != "" {
				t.Fatalf("run settled %q (error %q) before the resumed generation emitted any new delta",
					snap.TerminalOutcome, snap.Error)
			}

			if resumedGenerationEvidence(pre, snap) {
				return snap
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("the resumed generation never emitted authoritative activity beyond seq=%d (response %d bytes, reasoning %d bytes at capture)",
		pre.Sequence, len(pre.LatestResponse), len(pre.LatestReasoning))

	return runSnapshot{}
}

// waitForRunPhase polls the authoritative runLive state until the phase
// matches, failing fast on a contradicting terminal outcome.
func waitForRunPhase(t *testing.T, srv *Server, sessionID, want string) runSnapshot {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		rs := registeredRun(srv, sessionID)

		if rs != nil && rs.live != nil {
			snap := rs.live.snapshot()

			if snap.Phase == want {
				return snap
			}

			if snap.TerminalOutcome != "" {
				t.Fatalf("run settled %q (error %q) while waiting for phase %q",
					snap.TerminalOutcome, snap.Error, want)
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	phase := "unregistered"
	if rs := registeredRun(srv, sessionID); rs != nil && rs.live != nil {
		phase = rs.live.phaseLocked()
	}

	t.Fatalf("run never reached phase %q within the deadline (last phase %q)", want, phase)

	return runSnapshot{}
}

// waitForCheckpointGone polls until the paused-run checkpoint file is
// deleted (resume consumption or stop).
func waitForCheckpointGone(t *testing.T, srv *Server, runID string) {
	t.Helper()

	path := filepath.Join(srv.pausedDir(), runID+".json")
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("paused-run checkpoint %s was never deleted", path)
}

// waitForRegistryRelease polls until the session's run registry entry is
// gone (the terminal cleanup defer).
func waitForRegistryRelease(t *testing.T, srv *Server, sessionID string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if registeredRun(srv, sessionID) == nil {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("run registry entry for session %s was never released", sessionID)
}

// readPausedCheckpoint loads the durable checkpoint through the raw file
// (the record shape contract) and the server's own loader.
func readPausedCheckpoint(t *testing.T, srv *Server, runID string) *pausedRunRecord {
	t.Helper()

	path := filepath.Join(srv.pausedDir(), runID+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read paused checkpoint %s: %v", path, err)
	}

	var rec pausedRunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("checkpoint %s does not parse as pausedRunRecord: %v", path, err)
	}

	if _, ok := srv.loadPausedRecord(runID); !ok {
		t.Fatalf("loadPausedRecord could not read its own checkpoint %s", path)
	}

	return &rec
}

type transcriptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// fetchTranscript reads the session transcript over the real API.
func fetchTranscript(t *testing.T, server *httptest.Server, sessionID string) []transcriptMessage {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("GET /api/sessions/%s: %v", sessionID, err)
	}
	defer resp.Body.Close()

	var got struct {
		Messages []transcriptMessage `json:"messages"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode session transcript: %v", err)
	}

	return got.Messages
}

func countUserMessages(messages []transcriptMessage) int {
	n := 0

	for _, m := range messages {
		if m.Role == "user" {
			n++
		}
	}

	return n
}

// assistantReplies returns the non-empty assistant messages.
func assistantReplies(messages []transcriptMessage) []string {
	out := []string{}

	for _, m := range messages {
		if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
			out = append(out, m.Content)
		}
	}

	return out
}

// frameInt extracts a numeric frame field (wire JSON numbers are float64).
func frameInt(t *testing.T, frame map[string]any, key string) int64 {
	t.Helper()

	v, ok := frame[key].(float64)
	if !ok {
		t.Fatalf("frame[%q] = %v (%T), want a number", key, frame[key], frame[key])
	}

	return int64(v)
}

// pauseRunMidStream drives a run to its FIRST streamed delta, POSTs the
// pause, and waits for the authoritative PAUSED state — the deterministic
// mid-generation pause point. Returns the paused snapshot.
func pauseRunMidStream(t *testing.T, srv *Server, server *httptest.Server, sessionID, runID string) runSnapshot {
	t.Helper()

	waitForRunResponse(t, srv, sessionID)

	status, body := postJSON(t, server, "/api/run/pause", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/run/pause: status = %d body = %v, want 200 ok", status, body)
	}

	phase, _ := body["phase"].(string)
	if phase != "pausing" && phase != "paused" {
		t.Fatalf("POST /api/run/pause: phase = %q, want \"pausing\" (the boundary lands asynchronously)", phase)
	}

	return waitForRunPhase(t, srv, sessionID, "paused")
}

// resumeRun resumes a paused run at the given revision and asserts the
// handler's acceptance contract.
func resumeRun(t *testing.T, server *httptest.Server, sessionID, runID string, revision int64) {
	t.Helper()

	status, body := postJSON(t, server, "/api/run/resume", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  revision,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/run/resume: status = %d body = %v, want 200 ok", status, body)
	}

	if phase, _ := body["phase"].(string); phase != "resuming" {
		t.Fatalf("POST /api/run/resume: phase = %q, want \"resuming\"", phase)
	}
}

// --- 1. pause mid-generation --------------------------------------------------

func TestPauseMidStreamCheckpointsDraftAndStaysUnsettled(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	fullStream := strings.Join(pauseStreamChunks(pauseChunkCount), "")

	// The accepted draft is a non-empty PROPER prefix of the full stream —
	// the pause preserved the received prefix exactly, nothing invented.
	if snap.PausedDraft == "" {
		t.Fatal("the paused run reports an empty pausedDraft")
	}

	if !strings.HasPrefix(fullStream, snap.PausedDraft) || snap.PausedDraft == fullStream {
		t.Fatalf("pausedDraft = %q, want a non-empty proper prefix of the streamed text", snap.PausedDraft)
	}

	if snap.TerminalOutcome != "" {
		t.Fatalf("paused run must NOT be terminal, got outcome %q", snap.TerminalOutcome)
	}

	if !snap.Running {
		t.Fatal("a paused run stays registered as running=true (it is not settled)")
	}

	if snap.Revision != 0 {
		t.Fatalf("a fresh pause is at revision 0, got %d", snap.Revision)
	}

	// The run stays registered — resume and reconnect continue from here.
	if registeredRun(srv, sessionID) == nil {
		t.Fatal("a paused run must stay registered in the runs registry")
	}

	// The durable checkpoint exists under <DataDir>/runs/paused and parses
	// as the record shape, with the accepted draft and honest metadata.
	rec := readPausedCheckpoint(t, srv, runID)

	if rec.Version != 1 {
		t.Fatalf("checkpoint version = %d, want 1", rec.Version)
	}

	if rec.SessionID != sessionID || rec.RunID != runID {
		t.Fatalf("checkpoint identities = (%s, %s), want (%s, %s)", rec.RunID, rec.SessionID, runID, sessionID)
	}

	if rec.AssistantDraft != snap.PausedDraft {
		t.Fatalf("checkpoint draft %q != live pausedDraft %q", rec.AssistantDraft, snap.PausedDraft)
	}

	if rec.ContinuationMode != "semantic" {
		t.Fatalf("continuationMode = %q, want \"semantic\" (honestly labeled)", rec.ContinuationMode)
	}

	if rec.OriginalUserMessage != pausePrompt {
		t.Fatalf("originalUserMessage = %q, want %q", rec.OriginalUserMessage, pausePrompt)
	}

	if rec.ToolState != string(agent.ToolStateNone) {
		t.Fatalf("toolState = %q, want the none state for a plain chat pause", rec.ToolState)
	}

	// The run is NOT settled: no terminal outcome in the registry.
	if recOutcome, ok := srv.outcomes.latest(sessionID); ok && recOutcome.Outcome != "" {
		t.Fatalf("a paused run must not record a terminal outcome, got %q", recOutcome.Outcome)
	}

	// The session does NOT contain an assistant message yet.
	if replies := assistantReplies(fetchTranscript(t, server, sessionID)); len(replies) != 0 {
		t.Fatalf("a paused run must not persist an assistant message, got %q", replies)
	}

	// The WS replay contract: a fresh socket receives a run_snapshot that
	// carries the paused phase, the accepted draft and the revision.
	conn := dialActivityWS(t, server, sessionID)

	if ack := readFrameWithDeadline(t, conn); ack["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", ack["type"])
	}

	frame := readFrameWithDeadline(t, conn)

	if frame["type"] != "run_snapshot" {
		t.Fatalf("second frame = %v, want run_snapshot for the paused run", frame["type"])
	}

	if frame["phase"] != "paused" {
		t.Fatalf("run_snapshot.phase = %v, want paused", frame["phase"])
	}

	if draft, _ := frame["pausedDraft"].(string); draft != snap.PausedDraft {
		t.Fatalf("run_snapshot.pausedDraft = %q, want %q (the reconnecting client must re-render the draft)", draft, snap.PausedDraft)
	}

	if rev := frameInt(t, frame, "revision"); rev != snap.Revision {
		t.Fatalf("run_snapshot.revision = %d, want %d (the reconnecting client resumes against the SAME revision)", rev, snap.Revision)
	}

	if frame["runId"] != runID {
		t.Fatalf("run_snapshot.runId = %v, want %s", frame["runId"], runID)
	}
}

// --- 2. resume joins draft + continuation into ONE reply ----------------------

func TestPauseThenResumeJoinsDraftIntoOneReply(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)
	draft := snap.PausedDraft

	resumeRun(t, server, sessionID, runID, snap.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never recorded a terminal outcome")
	}

	recOutcome, ok := srv.outcomes.latest(sessionID)
	if !ok || recOutcome.Outcome != "done" {
		t.Fatalf("resumed run outcome = %q, want done", recOutcome.Outcome)
	}

	messages := fetchTranscript(t, server, sessionID)

	// EXACTLY ONE user message (the original — resume never appends a
	// duplicate) and EXACTLY ONE authoritative assistant reply.
	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("session must hold EXACTLY ONE user message, got %d", n)
	}

	replies := assistantReplies(messages)
	if len(replies) != 1 {
		t.Fatalf("resumed session must hold EXACTLY ONE assistant reply, got %d: %q", len(replies), replies)
	}

	reply := replies[0]

	// The reply is the joined revision: the accepted draft prefix, then the
	// continuation — never the draft twice, never the draft alone.
	if !strings.HasPrefix(reply, strings.TrimRight(draft, " \t\n\r")) {
		t.Fatalf("final reply %q does not start with the paused draft %q", reply, draft)
	}

	fullStream := strings.Join(pauseStreamChunks(pauseChunkCount), "")
	if !strings.Contains(reply, fullStream) {
		t.Fatalf("final reply is missing the continuation text: %q", reply)
	}

	if strings.Count(reply, "chunk-59") != 1 {
		t.Fatalf("the last continuation chunk must appear exactly once (no duplication): %q", reply)
	}

	// The consumed checkpoint is deleted.
	waitForCheckpointGone(t, srv, runID)

	// The registry entry is released once the resumed run settled.
	waitForRegistryRelease(t, srv, sessionID)

	// A follow-up ordinary chat works on the same session.
	followUp := postRunMessage(t, server, sessionID, "One ordinary follow-up question.")

	if !waitForRunSettledFor(t, srv, sessionID, followUp) {
		t.Fatal("the follow-up chat never recorded a terminal outcome")
	}

	if recOutcome, ok := srv.outcomes.latest(sessionID); !ok || recOutcome.Outcome != "done" {
		t.Fatalf("follow-up chat outcome = %q, want done", recOutcome.Outcome)
	}

	if replies := assistantReplies(fetchTranscript(t, server, sessionID)); len(replies) != 2 {
		t.Fatalf("after the follow-up chat the session holds %d assistant replies, want 2", len(replies))
	}
}

// --- 3. edit the user prompt while paused -------------------------------------

func TestPauseEditUserPromptReplacesTranscriptInPlace(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	edited := "EDITED: answer this instead."
	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   edited,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/run/edit: status = %d body = %v, want 200 ok", status, body)
	}

	if rev := frameInt(t, body, "revision"); rev != snap.Revision+1 {
		t.Fatalf("accepted edit revision = %d, want %d", rev, snap.Revision+1)
	}

	// The transcript's user message was replaced IN PLACE: one user
	// message, the edited content, still no assistant message.
	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("editing must never duplicate the user message, got %d user messages", n)
	}

	if messages[0].Role != "user" || messages[0].Content != edited {
		t.Fatalf("last user message = (%s, %q), want the edited prompt %q", messages[0].Role, messages[0].Content, edited)
	}

	if replies := assistantReplies(messages); len(replies) != 0 {
		t.Fatalf("editing while paused must not persist an assistant message, got %q", replies)
	}

	// The live state carries the new revision, still paused, draft intact.
	snap2 := waitForRunPhase(t, srv, sessionID, "paused")
	if snap2.Revision != snap.Revision+1 {
		t.Fatalf("live revision after edit = %d, want %d", snap2.Revision, snap.Revision+1)
	}

	// The durable checkpoint carries the edited user message.
	rec := readPausedCheckpoint(t, srv, runID)
	if rec.EditedUserMessage != edited {
		t.Fatalf("checkpoint editedUserMessage = %q, want %q", rec.EditedUserMessage, edited)
	}

	// Resume at the NEW revision.
	resumeRun(t, server, sessionID, runID, snap.Revision+1)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never settled")
	}

	messages = fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("after resume the session must show ONE user message (edited), got %d", n)
	}

	if messages[0].Content != edited {
		t.Fatalf("the surviving user message = %q, want the edited prompt", messages[0].Content)
	}

	if replies := assistantReplies(messages); len(replies) != 1 {
		t.Fatalf("the assistant reply must exist exactly once, got %d", len(replies))
	}
}

// --- 4. edit/trim the draft while paused ---------------------------------------

func TestPauseEditDraftTrimResumesWithTrimmedDraft(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	trimmed := "TRIMMED DRAFT."
	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"draft":     trimmed,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/run/edit (draft): status = %d body = %v, want 200 ok", status, body)
	}

	newRev := frameInt(t, body, "revision")

	// The live draft is the trimmed text; the durable checkpoint agrees.
	snap2 := waitForRunPhase(t, srv, sessionID, "paused")

	if snap2.PausedDraft != trimmed {
		t.Fatalf("live pausedDraft after trim = %q, want %q", snap2.PausedDraft, trimmed)
	}

	rec := readPausedCheckpoint(t, srv, runID)

	if rec.AssistantDraft != trimmed {
		t.Fatalf("checkpoint draft after trim = %q, want %q", rec.AssistantDraft, trimmed)
	}

	if rec.Revision != newRev {
		t.Fatalf("checkpoint revision = %d, want %d", rec.Revision, newRev)
	}

	// The discarded draft is never persisted as an assistant message.
	if replies := assistantReplies(fetchTranscript(t, server, sessionID)); len(replies) != 0 {
		t.Fatalf("the discarded draft leaked into the transcript: %q", replies)
	}

	resumeRun(t, server, sessionID, runID, newRev)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never settled")
	}

	replies := assistantReplies(fetchTranscript(t, server, sessionID))

	if len(replies) != 1 {
		t.Fatalf("the final reply must not be duplicated, got %d assistant messages: %q", len(replies), replies)
	}

	if !strings.HasPrefix(replies[0], trimmed) {
		t.Fatalf("final reply %q must start with the trimmed draft %q", replies[0], trimmed)
	}

	if strings.Count(replies[0], trimmed) != 1 {
		t.Fatalf("the trimmed draft must appear exactly once in the final reply: %q", replies[0])
	}
}

// --- 5. stale revision rejection ------------------------------------------------

func TestPauseStaleRevisionRejected409(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Stale EDIT: 409 with the current revision named (actionable conflict).
	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision + 99,
		"message":   "stale edit must be rejected",
	})

	if status != http.StatusConflict {
		t.Fatalf("stale edit status = %d, want 409 (body %v)", status, body)
	}

	errMsg, _ := body["error"].(string)

	if !strings.Contains(errMsg, "stale revision") ||
		!strings.Contains(errMsg, fmt.Sprintf("revision %d", snap.Revision)) ||
		!strings.Contains(errMsg, "99") {
		t.Fatalf("stale edit error must name the conflict (current revision %d, acted-on 99), got %q", snap.Revision, errMsg)
	}

	// Stale RESUME: 409 with the current revision named.
	status, body = postJSON(t, server, "/api/run/resume", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision + 99,
	})

	if status != http.StatusConflict {
		t.Fatalf("stale resume status = %d, want 409 (body %v)", status, body)
	}

	errMsg, _ = body["error"].(string)

	if !strings.Contains(errMsg, "cannot resume") ||
		!strings.Contains(errMsg, fmt.Sprintf("revision %d", snap.Revision)) {
		t.Fatalf("stale resume error must name the conflict (current revision %d), got %q", snap.Revision, errMsg)
	}

	// The stale attempts changed nothing: the transcript keeps the original
	// prompt and the run is still paused at the same revision.
	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 || messages[0].Content != pausePrompt {
		t.Fatalf("a rejected edit must not touch the transcript, got %d user messages (first %q)", n, messages[0].Content)
	}

	if live := waitForRunPhase(t, srv, sessionID, "paused"); live.Revision != snap.Revision {
		t.Fatalf("a rejected mutation must not bump the revision, got %d want %d", live.Revision, snap.Revision)
	}

	// The run is still resumable at the CORRECT revision.
	resumeRun(t, server, sessionID, runID, snap.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the run never settled after the stale-revision dance")
	}
}

// --- 6. double pause is idempotent ------------------------------------------------

func TestPauseDoubleRequestIsIdempotent(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	waitForRunResponse(t, srv, sessionID)

	for i := 0; i < 2; i++ {
		status, body := postJSON(t, server, "/api/run/pause", map[string]any{
			"sessionId": sessionID,
			"runId":     runID,
		})

		if status != http.StatusOK || body["ok"] != true {
			t.Fatalf("pause POST #%d: status = %d body = %v, want 200 ok (idempotent)", i+1, status, body)
		}
	}

	// The run still reaches PAUSED exactly once, with ONE checkpoint.
	snap := waitForRunPhase(t, srv, sessionID, "paused")

	dir := srv.pausedDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read paused dir: %v", err)
	}

	checkpoints := 0

	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			checkpoints++
		}
	}

	if checkpoints != 1 {
		t.Fatalf("paused dir holds %d checkpoints, want exactly one", checkpoints)
	}

	// And the paused run remains resumable afterwards.
	resumeRun(t, server, sessionID, runID, snap.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the run never settled after the double pause")
	}
}

// --- 7. double resume is a 409 conflict --------------------------------------------

func TestResumeSecondIs409(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	resumeRun(t, server, sessionID, runID, snap.Revision)

	// The first resume flipped the run out of PAUSED synchronously (the
	// handler reports phase "resuming" before the resumed generation even
	// starts), so an immediate second resume can never be accepted.
	status, body := postJSON(t, server, "/api/run/resume", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
	})

	if status != http.StatusConflict {
		t.Fatalf("second resume status = %d, want 409 (body %v)", status, body)
	}

	errMsg, _ := body["error"].(string)
	if !strings.Contains(errMsg, "cannot resume") {
		t.Fatalf("second resume error = %q, want an actionable conflict naming the phase", errMsg)
	}

	// The first (legitimate) resume still completes the run.
	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the first resumed run never settled")
	}
}

// --- 8. stop after pause ------------------------------------------------------------

func TestPauseThenAbortSettlesAbortedAndNextRunWorks(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	pauseRunMidStream(t, srv, server, sessionID, runID)

	status, body := postJSON(t, server, "/api/abort", map[string]any{
		"sessionId": sessionID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/abort: status = %d body = %v, want 200 ok", status, body)
	}

	// The run settles "aborted" in the bounded outcome registry.
	if !waitForRunOutcome(t, srv, sessionID, "aborted") {
		rec, _ := srv.outcomes.latest(sessionID)
		t.Fatalf("stopping a paused run must settle \"aborted\", got %+v", rec)
	}

	// The checkpoint is deleted and the registry entry released.
	waitForCheckpointGone(t, srv, runID)
	waitForRegistryRelease(t, srv, sessionID)

	// The transcript keeps the user message; no partial draft ever became
	// an assistant message.
	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("stopped paused run must keep the user message, got %d", n)
	}

	if replies := assistantReplies(messages); len(replies) != 0 {
		t.Fatalf("a stopped paused run must persist NO assistant message, got %q", replies)
	}

	// The process is alive.
	assertServerAlive(t, server, "after stop of a paused run")

	// And a subsequent ordinary run works.
	next := postRunMessage(t, server, sessionID, "A fresh question after the stop.")

	if !waitForRunSettledFor(t, srv, sessionID, next) {
		t.Fatal("the post-stop run never settled")
	}

	if recOutcome, ok := srv.outcomes.latest(sessionID); !ok || recOutcome.Outcome != "done" {
		t.Fatalf("post-stop run outcome = %q, want done (the app must keep serving)", recOutcome.Outcome)
	}

	if replies := assistantReplies(fetchTranscript(t, server, sessionID)); len(replies) != 1 {
		t.Fatalf("post-stop session must hold exactly one assistant reply, got %d", len(replies))
	}
}

// --- 9. reconnect during PAUSED ------------------------------------------------------

func TestPausedRunReconnectSnapshotCarriesRevisionAndDraft(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Attach BEFORE the run: a socket connected during generation.
	conn1 := dialActivityWS(t, server, sessionID)

	if ack := readFrameWithDeadline(t, conn1); ack["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", ack["type"])
	}

	runID := postRunMessage(t, server, sessionID, pausePrompt)
	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Disconnect, then dial a FRESH socket — the reconnect contract.
	conn1.Close()

	conn2 := dialActivityWS(t, server, sessionID)

	if ack := readFrameWithDeadline(t, conn2); ack["type"] != "attached" {
		t.Fatalf("fresh socket first frame = %v, want attached", ack["type"])
	}

	frame := readFrameWithDeadline(t, conn2)

	if frame["type"] != "run_snapshot" {
		t.Fatalf("fresh socket second frame = %v, want run_snapshot (the paused run stays registered with an open hub)", frame["type"])
	}

	if frame["phase"] != "paused" {
		t.Fatalf("reconnect run_snapshot.phase = %v, want paused", frame["phase"])
	}

	if draft, _ := frame["pausedDraft"].(string); draft != snap.PausedDraft {
		t.Fatalf("reconnect run_snapshot.pausedDraft = %q, want the same accepted draft %q", draft, snap.PausedDraft)
	}

	if rev := frameInt(t, frame, "revision"); rev != snap.Revision {
		t.Fatalf("reconnect run_snapshot.revision = %d, want the same revision %d", rev, snap.Revision)
	}

	if frame["runId"] != runID {
		t.Fatalf("reconnect run_snapshot.runId = %v, want the SAME run %s", frame["runId"], runID)
	}

	// Resume works from the new socket's client context.
	resumeRun(t, server, sessionID, runID, snap.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never settled after the reconnect")
	}

	// The fresh socket stays attached to the SAME run and observes the
	// resumed generation's terminal frames. The stream emits one response
	// frame per coalesce tick, so the bound is a DEADLINE, not a frame
	// count (a fixed count can run out mid-generation).
	sawTerminal := false
	terminalDeadline := time.Now().Add(30 * time.Second)

	for !sawTerminal {
		if time.Now().After(terminalDeadline) {
			t.Fatal("the reconnecting socket never observed the resumed run's terminal frame")
		}

		ev := readFrameWithDeadline(t, conn2)

		switch ev["type"] {
		case "done", "complete":
			sawTerminal = true
		}
	}
}

// --- 10. restart recovery from the durable checkpoint ---------------------------------

func TestPausedRunRecoveredFromCheckpointResumes(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// The durable checkpoint outlives the process: read it back and drop
	// the live registry entry (simulating the restart).
	rec, ok := srv.loadPausedRecord(runID)
	if !ok {
		t.Fatal("precondition: the paused checkpoint must be on disk")
	}

	srv.runsMu.Lock()
	delete(srv.runs, sessionID)
	srv.runsMu.Unlock()

	// recoverPausedRun re-registers the checkpointed run: same runId, same
	// session, phase PAUSED, revision and draft from the checkpoint.
	rs := srv.recoverPausedRun(rec)
	if rs == nil {
		t.Fatal("recoverPausedRun returned nil for a checkpoint whose session still exists")
	}

	recovered := rs.live.snapshot()

	if recovered.RunID != runID || recovered.SessionID != sessionID {
		t.Fatalf("recovered identities = (%s, %s), want (%s, %s)", recovered.RunID, recovered.SessionID, runID, sessionID)
	}

	if recovered.Phase != "paused" {
		t.Fatalf("recovered phase = %q, want paused", recovered.Phase)
	}

	if recovered.PausedDraft != rec.AssistantDraft {
		t.Fatalf("recovered draft = %q, want the checkpoint draft %q", recovered.PausedDraft, rec.AssistantDraft)
	}

	if recovered.Revision != snap.Revision {
		t.Fatalf("recovered revision = %d, want %d", recovered.Revision, snap.Revision)
	}

	// Drop the manual re-registration too and let the RESUME handler's own
	// restart-recovery branch re-register from disk (the production path).
	srv.runsMu.Lock()
	delete(srv.runs, sessionID)
	srv.runsMu.Unlock()

	resumeRun(t, server, sessionID, runID, snap.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the recovered run never settled")
	}

	replies := assistantReplies(fetchTranscript(t, server, sessionID))

	if len(replies) != 1 {
		t.Fatalf("the recovered run must settle into exactly one assistant reply, got %d", len(replies))
	}

	if !strings.HasPrefix(replies[0], strings.TrimRight(rec.AssistantDraft, " \t\n\r")) {
		t.Fatalf("recovered resume reply %q does not start with the checkpointed draft %q", replies[0], rec.AssistantDraft)
	}

	waitForCheckpointGone(t, srv, runID)
}

// --- 11. runLive pause state machine (pure unit) ---------------------------------------

func TestRunControlStateMachineTransitions(t *testing.T) {
	l := newRunLive("run-ctl-unit", "sess-ctl-unit", time.Now())

	// RUNNING → PAUSING.
	rev, phase, accepted := l.requestPause()
	if !accepted || phase != "pausing" || rev != 0 {
		t.Fatalf("first requestPause = (%d, %q, %v), want (0, \"pausing\", true)", rev, phase, accepted)
	}

	// Duplicate request is an idempotent no-op with the current state.
	_, phase, accepted = l.requestPause()
	if accepted || phase != "pausing" {
		t.Fatalf("duplicate requestPause = (%q, %v), want (\"pausing\", false)", phase, accepted)
	}

	// confirmPaused publishes PAUSED (called only after the checkpoint is
	// durable — here in the test, immediately).
	l.confirmPaused("partial answer.", "unit test")

	if !l.isPausedSettled() {
		t.Fatal("confirmPaused must settle the pause state")
	}

	if snap := l.snapshot(); snap.Phase != "paused" || snap.PausedDraft != "partial answer." {
		t.Fatalf("snapshot after confirmPaused = (%q, %q), want (\"paused\", \"partial answer.\")", snap.Phase, snap.PausedDraft)
	}

	if l.currentRevision() != 0 {
		t.Fatalf("revision after pause = %d, want 0 (edits bump it)", l.currentRevision())
	}

	// Editing is only valid while paused, only at the current revision.
	if _, changed := l.bumpRevision(7); changed {
		t.Fatal("bumpRevision with a wrong revision must be rejected")
	}

	newRev, changed := l.bumpRevision(0)
	if !changed || newRev != 1 {
		t.Fatalf("bumpRevision(0) = (%d, %v), want (1, true)", newRev, changed)
	}

	// confirmEdited publishes the accepted edit atomically with the revision.
	l.confirmEdited("edited draft.", newRev)

	if snap := l.snapshot(); snap.Phase != "paused" || snap.PausedDraft != "edited draft." || snap.Revision != 1 {
		t.Fatalf("snapshot after confirmEdited = (%q, %q, %d), want (\"paused\", \"edited draft.\", 1)",
			snap.Phase, snap.PausedDraft, snap.Revision)
	}

	// Resume: wrong revision rejected.
	if _, ok := l.requestResume(0); ok {
		t.Fatal("requestResume with a stale revision must be rejected")
	}

	// Correct revision accepted → RESUMING.
	if _, ok := l.requestResume(1); !ok {
		t.Fatal("requestResume at the current revision must be accepted")
	}

	// Double resume: no longer paused.
	if _, ok := l.requestResume(1); ok {
		t.Fatal("a second requestResume while resuming must be rejected")
	}

	l.confirmResumed()

	if snap := l.snapshot(); snap.Phase != "generating" || snap.PausedDraft != "" {
		t.Fatalf("snapshot after confirmResumed = (%q, %q), want (\"generating\", \"\")", snap.Phase, snap.PausedDraft)
	}

	// Pause → resume → pause: the machine works again from the live state.
	if _, phase, accepted := l.requestPause(); !accepted || phase != "pausing" {
		t.Fatalf("re-pause after resume = (%q, %v), want (\"pausing\", true)", phase, accepted)
	}

	// Terminal runs reject everything and stay terminal.
	l.confirmPaused("must be ignored", "terminal test") // pausing → paused is a no-op guard target below
	l.settleTerminal("done", "Completed", true, "final", "")

	if _, _, accepted := l.requestPause(); accepted {
		t.Fatal("a terminal run must reject a pause request")
	}

	if _, ok := l.requestResume(0); ok {
		t.Fatal("a terminal run must reject a resume request")
	}

	l.confirmPaused("late", "terminal") // must not resurrect the run

	if snap := l.snapshot(); snap.Phase != "done" || snap.TerminalOutcome != "done" {
		t.Fatalf("terminal state mutated by pause calls: (%q, %q)", snap.Phase, snap.TerminalOutcome)
	}
}

// --- 12. joinDraftContinuation + completeRoundTail (pure unit) ---------------------------

func TestRunControlJoinDraftContinuationAndCompleteRoundTail(t *testing.T) {
	t.Run("joinDraftContinuation", func(t *testing.T) {
		tests := []struct {
			name         string
			draft        string
			continuation string
			want         string
		}{
			{"both sides joined with one space", "Hello, world", "and more.", "Hello, world and more."},
			{"empty draft yields the continuation", "", "continuation only.", "continuation only."},
			{"empty continuation yields the draft", "draft only.", "", "draft only."},
			{"newline seam gets no extra space", "ends with newline\n", "next", "ends with newline\nnext"},
			{"seam whitespace is trimmed honestly", "trailing spaces   ", "\tleading spaces", "trailing spaces leading spaces"},
		}

		for _, tt := range tests {
			if got := joinDraftContinuation(tt.draft, tt.continuation); got != tt.want {
				t.Errorf("%s: joinDraftContinuation(%q, %q) = %q, want %q", tt.name, tt.draft, tt.continuation, got, tt.want)
			}
		}
	})

	t.Run("completeRoundTail", func(t *testing.T) {
		mkCall := func(id, name string) llm.ToolCall {
			tc := llm.ToolCall{ID: id, Type: "function"}
			tc.Function.Name = name
			tc.Function.Arguments = `{"path":"a.txt"}`

			return tc
		}

		// No pending calls: the tail passes through untouched.
		committedTail := []llm.Message{
			{Role: "assistant", Content: "Working.", ToolCalls: []llm.ToolCall{mkCall("call-1", "read_file")}},
			{Role: "tool", ToolCallID: "call-1", Content: "committed result"},
		}

		got := completeRoundTail(committedTail, nil)
		if len(got) != 2 || got[1].ToolCallID != "call-1" || got[1].Content != "committed result" {
			t.Fatalf("tail with no pending calls must pass through untouched, got %+v", got)
		}

		// Mixed state: call-1 committed (real result follows), call-2 never
		// executed (pending) — call-2 gets the honest synthetic result,
		// call-1's committed result is untouched.
		tail := []llm.Message{
			{Role: "assistant", Content: "Working.", ToolCalls: []llm.ToolCall{mkCall("call-1", "read_file"), mkCall("call-2", "write_file")}},
			{Role: "tool", ToolCallID: "call-1", Content: "committed result"},
		}

		got = completeRoundTail(tail, []llm.ToolCall{mkCall("call-2", "write_file")})

		if len(got) != 3 {
			t.Fatalf("pending call must gain exactly one synthetic result, got %d messages", len(got))
		}

		if got[0].Role != "assistant" || len(got[0].ToolCalls) != 2 {
			t.Fatalf("the assistant tool-call turn must stay untouched, got %+v", got[0])
		}

		if got[1].ToolCallID != "call-1" || got[1].Content != "committed result" {
			t.Fatalf("the committed result must stay untouched, got %+v", got[1])
		}

		if got[2].Role != "tool" || got[2].ToolCallID != "call-2" {
			t.Fatalf("the synthetic result must answer the pending call id, got %+v", got[2])
		}

		if !strings.Contains(got[2].Content, "never executed") || !strings.Contains(got[2].Content, "paused") {
			t.Fatalf("the synthetic result must be honest about the pause, got %q", got[2].Content)
		}

		// Fully pending: every assembled call gets its own honest result.
		tail = []llm.Message{
			{Role: "assistant", Content: "Planning.", ToolCalls: []llm.ToolCall{mkCall("p-1", "read_file"), mkCall("p-2", "read_file")}},
		}

		got = completeRoundTail(tail, []llm.ToolCall{mkCall("p-1", "read_file"), mkCall("p-2", "read_file")})

		if len(got) != 3 {
			t.Fatalf("every pending call needs a synthetic result, got %d messages", len(got))
		}

		for _, m := range got[1:] {
			if m.Role != "tool" || (m.ToolCallID != "p-1" && m.ToolCallID != "p-2") || !strings.Contains(m.Content, "never executed") {
				t.Fatalf("synthetic pending results are wrong: %+v", got)
			}
		}
	})
}
