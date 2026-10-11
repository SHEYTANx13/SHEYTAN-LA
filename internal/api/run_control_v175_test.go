package api

// run_control_v175_test.go — v1.7.5 PAUSE / EDIT / RESUME additions.
//
// The v1.7.4 suite pinned the happy-path state machine. This file adds the
// release-contract coverage the v1.7.4 suite did not have:
//
//   1. EMPTY assistant-draft edit — "" is a VALUE (field presence, not
//      emptiness); the erased draft never becomes final history and the
//      resume is continuation-only.
//   2. CHECKPOINT SAVE FAILURE (fault injection through pausedSaveSeam):
//      the accepted revision must NOT become authoritative — live state,
//      transcript and durable checkpoint stay at the OLD revision, the
//      transcript edit is rolled back, the retry after the fault clears
//      succeeds, and the resume then works.
//   3. TRANSCRIPT UPDATE FAILURE: a failing EditLastUserMessage is a clean
//      abort — nothing anywhere mutates.
//   4. PAUSE → RESUME → PAUSE → RESUME: the resumed run is itself
//      pausable; every cycle re-checkpoints; the final transcript carries
//      exactly one user turn and one authoritative assistant reply.
//   5. ABORT REACHES THE RESUMED GENERATION (v1.7.5 cancelCurrent fix):
//      stop during a RESUMED run settles it aborted and releases the
//      registry — the resumed run is not unkillable.
//   6. REPLACING A PAUSED RUN CONSUMES ITS CHECKPOINT (§13): the stale
//      record can never be resumed over the live run.
//   7. PAUSE WHILE REASONING: the reasoning prefix is checkpointed
//      (persistence policy allows it) and the resume continues.
//   8. RECONNECT WHILE PAUSING: the run_snapshot replays the pausing
//      phase (pure runLive unit — the wire frame the WS attach sends).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// --- extra fakes -------------------------------------------------------------

// remoteFakeEngineThen is STATEFUL: the FIRST /chat/completions call
// streams firstChunks (paced), every LATER call (the resume) streams
// thenText as content deltas. The distinct resume text lets a test prove
// which part of the final reply came from where.
func remoteFakeEngineThen(t *testing.T, firstChunks []string, thenText string, delayPerChunk time.Duration) *httptest.Server {
	t.Helper()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		stream := func(delta map[string]any) bool {
			payload := map[string]any{
				"id": "chatcmpl-v175then",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         delta,
					"finish_reason": nil,
				}},
			}

			enc, _ := json.Marshal(payload)

			if _, err := w.Write([]byte("data: " + string(enc) + "\n\n")); err != nil {
				return false
			}

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			time.Sleep(delayPerChunk)

			return true
		}

		if calls.Add(1) == 1 {
			for _, chunk := range firstChunks {
				if !stream(map[string]any{"content": chunk}) {
					return
				}
			}
		} else {
			if !stream(map[string]any{"content": thenText}) {
				return
			}
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	t.Cleanup(server.Close)

	return server
}

// remoteFakeEngineReasoning streams reasoning_content deltas followed by
// answer content deltas — the deterministic "pause while reasoning"
// engine. The answer content only arrives after the reasoning prefix, so
// a pause taken on the reasoning prefix checkpoints a reasoning draft.
func remoteFakeEngineReasoning(t *testing.T, chunks []string, delayPerChunk time.Duration) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		stream := func(delta map[string]any) bool {
			payload := map[string]any{
				"id": "chatcmpl-v175r",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         delta,
					"finish_reason": nil,
				}},
			}

			enc, _ := json.Marshal(payload)

			if _, err := w.Write([]byte("data: " + string(enc) + "\n\n")); err != nil {
				return false
			}

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			time.Sleep(delayPerChunk)

			return true
		}

		for _, chunk := range chunks {
			if !stream(map[string]any{"reasoning_content": chunk}) {
				return
			}
		}

		if !stream(map[string]any{"content": "reasoned-answer "}) {
			return
		}

		if !stream(map[string]any{"content": "after thinking."}) {
			return
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	t.Cleanup(server.Close)

	return server
}

// --- 1. edit the draft to EMPTY ----------------------------------------------

func TestPauseEditDraftToEmptyResumesContinuationOnly(t *testing.T) {
	engine := remoteFakeEngineThen(t, pauseStreamChunks(pauseChunkCount), "continuation-only-answer.", pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	if snap.PausedDraft == "" {
		t.Fatal("precondition: the paused draft must be non-empty before the erase")
	}

	// The erase: draft is EXPLICITLY empty — presence semantics, not "" ==
	// absent. A handler treating "" as "field absent" would reject this.
	empty := ""
	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"draft":     empty,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/run/edit (draft=\"\"): status = %d body = %v, want 200 ok — an intentional EMPTY draft must be accepted", status, body)
	}

	newRev := frameInt(t, body, "revision")

	// Live state: the draft is erased, the revision advanced.
	snap2 := waitForRunPhase(t, srv, sessionID, "paused")

	if snap2.Revision != newRev {
		t.Fatalf("live revision after erase = %d, want %d", snap2.Revision, newRev)
	}

	if snap2.PausedDraft != "" {
		t.Fatalf("live pausedDraft after erase = %q, want empty", snap2.PausedDraft)
	}

	// Durable checkpoint agrees.
	rec := readPausedCheckpoint(t, srv, runID)

	if rec.AssistantDraft != "" {
		t.Fatalf("checkpoint draft after erase = %q, want empty", rec.AssistantDraft)
	}

	// The discarded draft never became final history.
	if replies := assistantReplies(fetchTranscript(t, server, sessionID)); len(replies) != 0 {
		t.Fatalf("the discarded draft leaked into the transcript: %q", replies)
	}

	// Resume: continuation-only — the reply must NOT contain the erased
	// draft text, and it appears exactly once.
	resumeRun(t, server, sessionID, runID, newRev)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never settled")
	}

	if rec, ok := srv.outcomes.latest(sessionID); ok {
		t.Logf("DEBUG outcome: %+v", rec)
	}

	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("exactly one user turn required, got %d", n)
	}

	replies := assistantReplies(messages)

	if len(replies) != 1 {
		t.Fatalf("exactly one authoritative assistant reply required, got %d: %q", len(replies), replies)
	}

	if strings.Contains(replies[0], "chunk-") {
		t.Fatalf("the ERASED draft leaked into the final reply: %q", replies[0])
	}

	if strings.TrimSpace(replies[0]) != "continuation-only-answer." {
		t.Fatalf("the final reply must be the continuation only, got %q", replies[0])
	}
}

// --- 2. checkpoint save failure (§10 fault injection) ------------------------

func TestPauseEditCheckpointSaveFailureLeavesAllStateConsistent(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Arm the fault: the checkpoint commit fails AFTER the transcript edit.
	var faults sync.Map // runId → *int32 remaining failures

	pausedSaveSeam = func(rec *pausedRunRecord) error {
		one := int32(1)
		v, _ := faults.LoadOrStore(rec.RunID, &one)
		remaining := v.(*int32)

		if *remaining > 0 {
			*remaining--
			return errCheckpointInjected
		}

		return nil
	}
	defer func() { pausedSaveSeam = nil }()

	edited := "EDITED under a failing checkpoint commit."

	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   edited,
	})

	if status != http.StatusInternalServerError {
		t.Fatalf("edit status = %d, want 500 (checkpoint commit failed)", status)
	}

	if !strings.Contains(body["error"].(string), "not committed") {
		t.Fatalf("error must be explicit about the non-commit, got %v", body["error"])
	}

	// INVARIANT: the live revision did NOT advance — the accepted revision
	// never becomes authoritative before the durable state is safe.
	rs := registeredRun(srv, sessionID)

	if rs == nil {
		t.Fatal("the paused run must stay registered after a failed edit")
	}

	live := rs.live.snapshot()

	if live.Revision != snap.Revision {
		t.Fatalf("live revision after failed edit = %d, want %d (unchanged)", live.Revision, snap.Revision)
	}

	if live.Phase != "paused" {
		t.Fatalf("phase after failed edit = %q, want paused", live.Phase)
	}

	// INVARIANT: the transcript was rolled back — the session shows the
	// ORIGINAL user message, matching the OLD checkpoint revision.
	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("the rollback must not duplicate the user turn, got %d", n)
	}

	if messages[0].Content == edited {
		t.Fatal("the transcript still carries the edited prompt although the checkpoint commit FAILED — memory and durability disagree")
	}

	// INVARIANT: the durable checkpoint is untouched (old revision, no
	// edited message).
	rec := readPausedCheckpoint(t, srv, runID)

	if rec.Revision != snap.Revision {
		t.Fatalf("durable checkpoint revision = %d, want %d (the old revision)", rec.Revision, snap.Revision)
	}

	if rec.EditedUserMessage != "" {
		t.Fatalf("durable checkpoint claims an edit the failed commit never confirmed: %q", rec.EditedUserMessage)
	}

	// RECOVERY: clear the fault — the SAME edit at the SAME revision now
	// succeeds (deterministic retry, no stuck state).
	zero := int32(0)
	faults.Store(runID, &zero)

	status, body = postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   edited,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("retry edit: status = %d body = %v, want 200 ok", status, body)
	}

	newRev := frameInt(t, body, "revision")

	// Everything agrees at the new revision now.
	live = rs.live.snapshot()

	if live.Revision != newRev {
		t.Fatalf("live revision after retry = %d, want %d", live.Revision, newRev)
	}

	rec = readPausedCheckpoint(t, srv, runID)

	if rec.Revision != newRev || rec.EditedUserMessage != edited {
		t.Fatalf("checkpoint after retry: revision=%d edited=%q, want revision=%d edited=%q", rec.Revision, rec.EditedUserMessage, newRev, edited)
	}

	// And the resumed run uses the edited prompt: exactly one user turn.
	resumeRun(t, server, sessionID, runID, newRev)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never settled")
	}

	messages = fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("after recovery exactly one user turn required, got %d", n)
	}

	if messages[0].Content != edited {
		t.Fatalf("the surviving user turn = %q, want the edited prompt", messages[0].Content)
	}
}

type errCheckpointInjectedType struct{}

func (errCheckpointInjectedType) Error() string { return "injected checkpoint commit failure" }

var errCheckpointInjected = errCheckpointInjectedType{}

// --- 3. transcript update failure --------------------------------------------

func TestPauseEditTranscriptFailureCleanAbort(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Force the transcript edit to fail deterministically: the user turn is
	// GONE from the transcript (a concurrent client rewrote history — the
	// store is the authority; EditLastUserMessage refuses to invent a user
	// turn that is not there).
	if sess, err := srv.store.Get(sessionID); err != nil {
		t.Fatalf("fetch session: %v", err)
	} else {
		kept := make([]llm.Message, 0, len(sess.Messages))

		for _, m := range sess.Messages {
			if m.Role != "user" {
				kept = append(kept, m)
			}
		}

		sess.Messages = append(kept, llm.Message{
			Role:    "assistant",
			Content: "the user turn was removed underneath the paused run",
		})

		if err := srv.store.SaveMessagesKeepContext(sess); err != nil {
			t.Fatalf("rewrite transcript: %v", err)
		}
	}

	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   "this edit cannot touch the transcript",
	})

	if status != http.StatusInternalServerError {
		t.Fatalf("edit status = %d, want 500 (transcript update failed)", status)
	}

	if !strings.Contains(body["error"].(string), "edit user message") {
		t.Fatalf("error must name the transcript failure, got %v", body["error"])
	}

	// CLEAN ABORT: live state untouched, checkpoint untouched.
	rs := registeredRun(srv, sessionID)

	if rs == nil {
		t.Fatal("the paused run must stay registered")
	}

	live := rs.live.snapshot()

	if live.Revision != snap.Revision || live.Phase != "paused" {
		t.Fatalf("live state after transcript failure: revision=%d phase=%q, want revision=%d paused", live.Revision, live.Phase, snap.Revision)
	}

	rec := readPausedCheckpoint(t, srv, runID)

	if rec.Revision != snap.Revision || rec.EditedUserMessage != "" {
		t.Fatalf("checkpoint after transcript failure: revision=%d edited=%q, want %d and none", rec.Revision, rec.EditedUserMessage, snap.Revision)
	}
}

// --- 4. pause → resume → pause → resume ---------------------------------------

func TestPauseResumePauseAgainThenResumeCompletes(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	// First pause.
	snap1 := pauseRunMidStream(t, srv, server, sessionID, runID)
	draft1 := snap1.PausedDraft

	// Resume without changes.
	//
	// v1.8.0 SYNCHRONIZATION REPAIR (Actions run 36553559366): the second
	// pause below must land on a PROVEN live resumed generation. The
	// previous code reused waitForRunResponse here, whose LatestResponse
	// condition is satisfied by the STALE pre-resume cumulative snapshot —
	// it returned before the resumed stream emitted anything, and on a slow
	// runner the run settled "done" while the test waited for "paused".
	// The fix is the honest beat: capture the authoritative state BEFORE
	// resuming, then require a NEW authoritative event (seq advance +
	// changed cumulative response/reasoning) that only the resumed
	// generation can produce. No delay, no "wait longer" — the wait
	// condition itself is the evidence.
	preResume := registeredRun(srv, sessionID).live.snapshot()

	resumeRun(t, server, sessionID, runID, snap1.Revision)

	// Wait until the RESUMED generation has emitted new authoritative
	// activity, then pause a SECOND time mid-stream.
	waitForResumedGeneration(t, srv, sessionID, preResume)

	status, body := postJSON(t, server, "/api/run/pause", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("second POST /api/run/pause: status = %d body = %v, want 200 ok", status, body)
	}

	snap2 := waitForRunPhase(t, srv, sessionID, "paused")

	// The second checkpoint is durable and describes the SAME run.
	rec := readPausedCheckpoint(t, srv, runID)

	if rec.SessionID != sessionID || rec.RunID != runID {
		t.Fatalf("second checkpoint identity: session=%s run=%s, want %s/%s", rec.SessionID, rec.RunID, sessionID, runID)
	}

	if rec.ContinuationMode != "semantic" {
		t.Fatalf("second checkpoint continuation mode = %q, want semantic", rec.ContinuationMode)
	}

	// The registry entry is the SAME run state (one authoritative run).
	rs := registeredRun(srv, sessionID)

	if rs == nil || rs.live.runID != runID {
		t.Fatalf("the same run must stay registered across pause/resume/pause, got %+v", rs)
	}

	// Second resume (at the same revision — no edit happened) completes.
	resumeRun(t, server, sessionID, runID, snap2.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the twice-resumed run never settled")
	}

	waitForCheckpointGone(t, srv, runID)
	waitForRegistryRelease(t, srv, sessionID)

	// The final transcript: exactly ONE user turn, ONE authoritative reply,
	// and the first accepted draft must not be duplicated.
	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("exactly one user turn required, got %d", n)
	}

	replies := assistantReplies(messages)

	if len(replies) != 1 {
		t.Fatalf("exactly one authoritative assistant reply required, got %d: %q", len(replies), replies)
	}

	if strings.Count(replies[0], strings.TrimSpace(firstChunk(draft1))) < 1 {
		t.Fatalf("the resumed answer lost the accepted prefix entirely: %q", replies[0])
	}
}

func firstChunk(draft string) string {
	fields := strings.Fields(draft)

	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

// --- 5. abort reaches the RESUMED generation -----------------------------------

func TestAbortReachesResumedGeneration(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Resume: the generation restarts with a FRESH context (created by the
	// resume handler, cancel installed before the phase was published).
	resumeRun(t, server, sessionID, runID, snap.Revision)

	// STOP DURING THE RESUMING WINDOW: the fixture engine completes a
	// generation in tens of milliseconds, so aborting after the first delta
	// races the run's own completion. Aborting immediately after the resume
	// POST deterministically exercises the window this regression exists
	// for: the cancel installed by the resume HANDLER must reach the fresh
	// generation (the registration cancel never would).
	status, body := postJSON(t, server, "/api/abort", map[string]any{
		"sessionId": sessionID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/abort on a resumed run: status = %d body = %v, want 200 ok", status, body)
	}

	// The run MUST settle — aborted or error (the stream died mid-flight);
	// an unstoppable run would leave the phase generating forever.
	deadline := time.Now().Add(15 * time.Second)

	for {
		rs := registeredRun(srv, sessionID)

		if rs == nil {
			break // settled and released
		}

		s := rs.live.snapshot()

		if s.TerminalOutcome != "" {
			if s.TerminalOutcome != "aborted" && s.TerminalOutcome != "error" {
				t.Fatalf("resumed run settled %q, want aborted/error", s.TerminalOutcome)
			}

			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("the RESUMED run ignored /api/abort — still phase %q (the cancel never reached the fresh generation context)", s.Phase)
		}

		time.Sleep(10 * time.Millisecond)
	}

	waitForRegistryRelease(t, srv, sessionID)
}

// --- 6. replacing a paused run consumes its checkpoint -------------------------

func TestReplacingPausedRunConsumesCheckpoint(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	pauseRunMidStream(t, srv, server, sessionID, runID)

	checkpoint := filepath.Join(srv.pausedDir(), runID+".json")

	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("precondition: the paused checkpoint must exist: %v", err)
	}

	// A NEW ordinary run in the same session replaces the paused one (the
	// one-run-per-session authority). The replaced run is stopped for
	// good — its checkpoint must be consumed NOW, not left resumable.
	newRunID := postRunMessage(t, server, sessionID, "A brand new ordinary run.")

	if newRunID == runID {
		t.Fatal("the new run must have its own runId")
	}

	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("the replaced paused run's checkpoint survived the replacement — it is resumable over the live run")
	}

	// /api/run/paused no longer lists it.
	resp, err := http.Get(server.URL + "/api/run/paused?sessionId=" + sessionID)
	if err != nil {
		t.Fatalf("GET /api/run/paused: %v", err)
	}
	defer resp.Body.Close()

	var listed []map[string]any

	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode /api/run/paused: %v", err)
	}

	for _, entry := range listed {
		if entry["runId"] == runID {
			t.Fatal("the consumed checkpoint is still listed as recoverable")
		}
	}

	// The new run completes normally.
	if !waitForRunSettledFor(t, srv, sessionID, newRunID) {
		t.Fatal("the replacing run never settled")
	}
}

// --- 7. pause while reasoning ---------------------------------------------------

func TestPauseWhileReasoningCheckpointsReasoningDraft(t *testing.T) {
	engine := remoteFakeEngineReasoning(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	// Wait for the FIRST REASONING delta (the deterministic pre-pause beat).
	deadline := time.Now().Add(30 * time.Second)

	for {
		rs := registeredRun(srv, sessionID)

		if rs != nil && rs.live != nil {
			if s := rs.live.snapshot(); s.LatestReasoning != "" {
				break
			}
		}

		if time.Now().After(deadline) {
			t.Fatal("the run never streamed a reasoning delta")
		}

		time.Sleep(5 * time.Millisecond)
	}

	status, body := postJSON(t, server, "/api/run/pause", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/run/pause (reasoning): status = %d body = %v", status, body)
	}

	snap := waitForRunPhase(t, srv, sessionID, "paused")

	if snap.PausedDraft != "" {
		t.Fatalf("no answer text was streamed — the paused draft must be empty, got %q", snap.PausedDraft)
	}

	// The durable checkpoint keeps the REASONING prefix (persistence policy
	// allows it: reasoning state rides the bounded record).
	rec := readPausedCheckpoint(t, srv, runID)

	if rec.ReasoningDraft == "" {
		t.Fatal("the reasoning prefix was lost from the checkpoint")
	}

	if rec.ContinuationMode != "semantic" {
		t.Fatalf("continuation mode = %q, want semantic", rec.ContinuationMode)
	}

	// Resume completes; the transcript carries one user turn and one reply.
	resumeRun(t, server, sessionID, runID, snap.Revision)

	if !waitForRunSettledFor(t, srv, sessionID, runID) {
		t.Fatal("the resumed run never settled")
	}

	messages := fetchTranscript(t, server, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("exactly one user turn required, got %d", n)
	}

	if replies := assistantReplies(messages); len(replies) != 1 {
		t.Fatalf("exactly one authoritative assistant reply required, got %d", len(replies))
	}
}

// --- 8. reconnect while PAUSING (pure runLive unit) ------------------------------

func TestRunLiveSnapshotWhilePausingReplaysPhase(t *testing.T) {
	live := newRunLive("run-pause-snap", "sess-pause-snap", time.Now())

	// A pause request lands: the authoritative state is PAUSING until the
	// checkpoint is durable. A socket attaching NOW must replay "pausing".
	rev, phase, accepted := live.requestPause()

	if !accepted || phase != "pausing" {
		t.Fatalf("requestPause = (%d, %q, %v), want accepted pausing", rev, phase, accepted)
	}

	snap := live.snapshot()

	if snap.Phase != "pausing" || snap.Running != true {
		t.Fatalf("snapshot during pausing: phase=%q running=%v, want pausing/true", snap.Phase, snap.Running)
	}

	if snap.TerminalOutcome != "" {
		t.Fatalf("pausing is not terminal, got %q", snap.TerminalOutcome)
	}

	// The durable checkpoint lands (the caller's contract): only NOW paused.
	live.confirmPaused("partial answer draft", "paused at a safe boundary")

	snap = live.snapshot()

	if snap.Phase != "paused" || snap.PausedDraft != "partial answer draft" {
		t.Fatalf("snapshot after checkpoint: phase=%q draft=%q, want paused + draft", snap.Phase, snap.PausedDraft)
	}
}
