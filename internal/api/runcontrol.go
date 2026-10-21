// runcontrol.go — v1.7.4 backend-neutral PAUSE / EDIT / RESUME.
//
// ONE authoritative contract over the EXISTING run authorities:
//
//   - runLive (runstate.go) remains the ONE live lifecycle authority — the
//     pause state machine methods live there; nothing here duplicates it.
//   - the WS replay contract (run_snapshot at seq N + events > N) is
//     unchanged; a paused run stays registered with an open hub, so a
//     reconnect during PAUSED replays the draft and the revision.
//   - the durable settlement of a resumed run is the SAME
//     finishSuccessfulRun method a fresh run uses — one lifecycle, one
//     outcome path, no second run-state system.
//
// Paused-run checkpoints are BOUNDED durable records under the existing
// data root (<DataDir>/runs/paused/<runId>.json, atomic temp+rename) —
// never a new storage root, never unbounded prompt duplication.
//
// API surface (extends /api/run + /api/abort, one coherent contract):
//
//	POST /api/run/pause   {sessionId, runId}            → pause request
//	POST /api/run/edit    {sessionId, runId, revision, message?, draft?}
//	POST /api/run/resume  {sessionId, runId, revision}  → resume
//	GET  /api/run/paused  ?sessionId=                   → recoverable paused runs
//
// Pause is SEMANTIC CONTINUATION (honestly labeled): the stream stops at a
// safe boundary, the accepted prefix is checkpointed, and resume rebuilds
// the request from the persisted transcript + draft. It is not a claimed
// byte-identical KV continuation.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// pausedRecordCap bounds the largest persisted draft — the same bound the
// live snapshot enforces, so a checkpoint can never outgrow what the live
// state would retain.
const pausedRecordCap = 256 << 10

// pausedRunRecord is the durable checkpoint of ONE paused run (spec §6:
// bounded durable run state — identities, the accepted prefix, the tool
// state needed to resume, generation settings, integrity metadata).
type pausedRunRecord struct {
	Version   int `json:"version"` // 1
	CreatedAt time.Time
	UpdatedAt time.Time

	RunID     string `json:"runId"`
	SessionID string `json:"sessionId"`
	Revision  int64  `json:"revision"`

	// Conversation identity: the ORIGINAL user message and, when the user
	// edited it while paused, the edited one. The session transcript stays
	// the authority; these fields make the checkpoint self-describing and
	// let a restart-recovered UI show what will be resumed.
	OriginalUserMessage string `json:"originalUserMessage,omitempty"`
	EditedUserMessage   string `json:"editedUserMessage,omitempty"`

	// The accepted generated assistant prefix (semantic continuation).
	AssistantDraft string `json:"assistantDraft,omitempty"`
	ReasoningDraft string `json:"reasoningDraft,omitempty"`

	// Tool state at the pause boundary — none / pending / committed, plus
	// the assembled-but-unexecuted calls (never blindly replayed: a resume
	// re-offers them to the model through the normal loop).
	ToolState        string         `json:"toolState,omitempty"`
	PendingToolCalls []llm.ToolCall `json:"pendingToolCalls,omitempty"`
	RoundTail        []llm.Message  `json:"roundTail,omitempty"`

	// Model/backend identity and the generation settings a resume must
	// reproduce for one coherent revision.
	Model     string   `json:"model,omitempty"`
	Backend   string   `json:"backend,omitempty"`
	Thinking  string   `json:"thinking,omitempty"`
	ToolMode  string   `json:"toolMode,omitempty"`
	ToolAllow []string `json:"toolAllow,omitempty"`
	NetSearch bool     `json:"netSearch,omitempty"`

	// ContinuationMode is honest about what resume does.
	ContinuationMode string `json:"continuationMode"` // "semantic"
}

// successRunParams carries the shared durable-settlement inputs.
type successRunParams struct {
	sess            *sessions.Session
	userMessage     string
	res             agent.RunResult
	resultOutcome   string
	terminalCaption string
	tl              *runTimeline
	settle          func(result string, caption string, persisted bool, replyText, reasonText string)
	publish         func(agent.Activity)
	stampRun        func(agent.Activity) agent.Activity
	// v1.10.13 Task 30 (run #33 R2): publishes the orchestrator's held
	// terminal frames the moment the transcript write commits. Nil for
	// the resume path (its settlement keeps today's broadcast timing —
	// the same-class resume race is recorded as future work).
	flushTerminal func()
}

// finishSuccessfulRun is the ONE durable settlement path for a successful
// (non-error) run result — fresh or resumed: append the authoritative
// assistant reply exactly once, roll the session summary, write the
// mandatory agent.md handoff, index recall, roll continuum, then publish
// the terminal state. v1.7.4: extracted verbatim from the run goroutine so
// Pause/Resume cannot grow a divergent settlement.
func (s *Server) finishSuccessfulRun(p successRunParams) {
	sess := p.sess
	res := p.res
	resultOutcome := p.resultOutcome
	terminalCaption := p.terminalCaption

	// Append assistant reply — exactly once, by the one settlement path.
	if res.Text != "" {
		if _, err := s.store.AppendMessage(
			sess.ID,
			llm.Message{
				Role:      "assistant",
				Content:   res.Text,
				Reasoning: res.Reasoning,
			},
		); err != nil {
			// v1.1.4: a lost reply is a REAL failure the user must see,
			// not a swallowed error.
			// v1.10.13 Task 30: flush any held terminal frames
			// BEFORE the error publication — the true sequence
			// (the orchestrator finished, the persist failed)
			// reaches the client in order; no held frame is
			// silently dropped (I3).
			if p.flushTerminal != nil {
				p.flushTerminal()
			}

			p.publish(p.stampRun(agent.Activity{
				Type:      "error",
				Caption:   "The reply was generated but could not be saved to the session: " + err.Error(),
				Timestamp: time.Now(),
			}))

			logging.Default().Error(
				"api",
				"append assistant reply: %v",
				err,
			)

			p.settle("error", "reply persistence failed: "+err.Error(), false, res.Text, res.Reasoning)
			return
		}
	}

	p.tl.persistedAt = time.Now()

	// v1.10.13 Task 30 (run #33 R2): the transcript write has committed —
	// publish the orchestrator's held terminal frames NOW, before the
	// remaining durable steps, so every client that reloads the
	// transcript on their arrival (the Qt settle path) reads the reply
	// that is already on disk. This is v1.2.9's ordering rule (durable
	// writes before terminal publication) enforced at the one boundary
	// the chat-persist race rides.
	if p.flushTerminal != nil {
		p.flushTerminal()
	}

	// v1.2.9 DURABLE COMPLETION ORDERING (see the original comment in the
	// run goroutine): every durable write happens BEFORE the terminal
	// publication; the post-terminal publishes are UI events only.

	// Durable step 1 — rolling session summary (best-effort).
	if res.Text != "" {
		s.updateSessionSummaryRolling(sess, p.userMessage, res.Text, res.ToolsUsed)
	}

	// Durable step 2 — mandatory agent.md handoff (Agent mode).
	handoffWriteErr := error(nil)
	handoffPath := ""

	if sess.Mode == sessions.ModeAgent && resultOutcome == "done" {
		handoffPath, handoffWriteErr = s.writeAgentHandoff(sess, res.Task, resultOutcome)
	}

	if handoffWriteErr != nil {
		resultOutcome = "error"
		terminalCaption = "agent.md handoff failed: " + handoffWriteErr.Error()
	}

	// Durable step 3 — index the completed exchange into persistent recall.
	if s.recall != nil && res.Text != "" {
		if err := s.recall.IndexTurn(
			sess.ID,
			sess.Title,
			p.userMessage,
			res.Text,
			res.ToolsUsed,
		); err != nil {
			logging.Default().Warn(
				"recall",
				"index turn: %v",
				err,
			)
		}
	}

	// Durable step 4 — Continuum chapter rollover.
	var continuumChild *sessions.Session
	var continuumPrev string

	if s.continuum != nil {
		if fresh, err := s.store.Get(sess.ID); err == nil {
			if s.continuum.ShouldRollover(fresh, s.src.Load()) {
				if child, _, err := s.continuum.Rollover(fresh, s.src.Load()); err == nil {
					continuumChild = child
					continuumPrev = fresh.ID

					logging.Default().Info(
						"continuum",
						"rolled over session %s → chapter %d (%s)",
						fresh.ID,
						child.Chapter,
						child.ID,
					)
				} else {
					logging.Default().Warn(
						"continuum",
						"rollover failed (conversation continues in current session): %v",
						err,
					)
				}
			}
		}
	}

	// Terminal publication — every required durable artifact above is
	// already on disk.
	p.settle(resultOutcome, terminalCaption, res.Text != "", res.Text, res.Reasoning)

	// Post-terminal UI events (non-durable).
	if handoffWriteErr != nil {
		p.publish(p.stampRun(agent.Activity{
			Type:    "error",
			Caption: "agent.md handoff failed: " + handoffWriteErr.Error(),
			Detail: map[string]any{
				"sessionId": sess.ID,
			},
			Timestamp: time.Now(),
		}))
	} else if handoffPath != "" {
		p.publish(p.stampRun(agent.Activity{
			Type:    "handoff",
			Caption: "agent.md handoff updated for the next agent",
			Detail: map[string]any{
				"path":      handoffPath,
				"sessionId": sess.ID,
			},
			Timestamp: time.Now(),
		}))
	}

	if continuumChild != nil {
		p.publish(agent.Activity{
			Type:    "session",
			Caption: "Context threshold reached — conversation continued in chapter " + fmt.Sprint(continuumChild.Chapter),
			Detail: map[string]any{
				"sessionId": continuumChild.ID,
				"threadId":  continuumChild.ThreadID,
				"chapter":   continuumChild.Chapter,
				"previous":  continuumPrev,
			},
			Timestamp: time.Now(),
		})
	}
}

// ---------------------------------------------------------------------------
// Durable paused-run checkpoint store (<DataDir>/runs/paused)
// ---------------------------------------------------------------------------

func (s *Server) pausedDir() string {
	return filepath.Join(s.src.Load().DataDir, "runs", "paused")
}

// pausedSaveSeam is the v1.7.5 fault-injection seam for the durable
// checkpoint commit (the same pattern as internal/config's renameForTest):
// tests substitute it to force a save failure AFTER the transcript edit —
// the exact crash window §10 forbids from becoming authoritative. Nil in
// production; production behavior is os.WriteFile + os.Rename only.
var pausedSaveSeam func(rec *pausedRunRecord) error

// savePausedRecord persists one checkpoint ATOMICALLY (temp + rename).
func (s *Server) savePausedRecord(rec *pausedRunRecord) error {
	// v1.7.5 fault-injection seam: a non-nil error SIMULATES the failed
	// commit; nil falls through to the REAL durable write (a seam that
	// short-circuits on nil would skip the very persistence it guards).
	if pausedSaveSeam != nil {
		if err := pausedSaveSeam(rec); err != nil {
			return fmt.Errorf("paused run: %w", err)
		}
	}

	dir := s.pausedDir()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("paused runs dir: %w", err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal paused run: %w", err)
	}

	path := filepath.Join(dir, rec.RunID+".json")
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write paused run: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit paused run: %w", err)
	}

	return nil
}

// loadPausedRecord reads one checkpoint by run id.
func (s *Server) loadPausedRecord(runID string) (*pausedRunRecord, bool) {
	if runID == "" {
		return nil, false
	}

	data, err := os.ReadFile(filepath.Join(s.pausedDir(), runID+".json"))
	if err != nil || len(data) > 4*(1<<20) {
		return nil, false
	}

	var rec pausedRunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, false
	}

	return &rec, true
}

// deletePausedRecord removes a consumed or aborted checkpoint.
func (s *Server) deletePausedRecord(runID string) {
	if runID == "" {
		return
	}
	_ = os.Remove(filepath.Join(s.pausedDir(), runID+".json"))
}

// completeRoundTail makes a paused round tail wire-valid: every tool call
// in an assistant message must carry a result. Calls recorded as PENDING
// get an honest synthetic result ("never executed — run paused"); the
// model re-issues them through the normal loop on resume.
func completeRoundTail(tail []llm.Message, pending []llm.ToolCall) []llm.Message {
	if len(pending) == 0 {
		return tail
	}

	pendingIDs := make(map[string]bool, len(pending))
	for _, tc := range pending {
		if tc.ID != "" {
			pendingIDs[tc.ID] = true
		}
	}

	out := make([]llm.Message, len(tail))
	copy(out, tail)

	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != "assistant" || len(out[i].ToolCalls) == 0 {
			continue
		}

		have := make(map[string]bool, len(out[i].ToolCalls))
		for _, tc := range out[i].ToolCalls {
			have[tc.ID] = true
		}

		for _, tc := range out[i].ToolCalls {
			if have[tc.ID] && tc.ID != "" && !pendingIDs[tc.ID] {
				continue // a real result already follows
			}
			if !pendingIDs[tc.ID] {
				continue
			}

			out = append(out, llm.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    "Error: this tool call was never executed — the run was paused by the user before it started. Request it again if it is still needed.",
			})

			delete(pendingIDs, tc.ID)
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleRunPause requests a cooperative pause of the session's active run.
// Idempotent: a duplicate request returns the current state instead of an
// error. The orchestrator stops at the NEXT SAFE BOUNDARY; the response
// therefore reports phase "pausing", never an instant "paused".
func (s *Server) handleRunPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

	var body struct {
		SessionID string `json:"sessionId"`
		RunID     string `json:"runId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("pause: %w", err))
		return
	}

	if body.SessionID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
		return
	}

	s.runsMu.Lock()
	rs, ok := s.runs[body.SessionID]
	s.runsMu.Unlock()

	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no active run for session %s", body.SessionID))
		return
	}

	if body.RunID != "" && body.RunID != rs.live.runID {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"runId mismatch: active run is %s, request targeted %s", rs.live.runID, body.RunID))
		return
	}

	revision, phase, accepted := rs.live.requestPause()

	if accepted {
		// The orchestrator observes this flag at safe boundaries only.
		rs.ctrl.RequestPause()

		logging.Default().Info("run", "pause requested")
	}

	writeJSON(w, map[string]any{
		"ok":       true,
		"runId":    rs.live.runID,
		"revision": revision,
		"phase":    phase,
	})
}

// handleRunEdit edits a PAUSED run: replace the user prompt and/or trim,
// replace or ERASE the partial assistant draft. Every accepted edit creates
// a new monotonic revision; a stale revision is rejected with the current
// value. The discarded draft is never persisted as an assistant message.
//
//	v1.7.5 TRANSACTIONAL CONSISTENCY (§10): the accepted revision does not
//
// become authoritative until ALL durable state for the edit is safe. The
// handler validates the CAS revision WITHOUT mutating, performs the
// durable work (transcript replace + checkpoint commit), rolls the
// transcript back if the checkpoint commit fails, and only then publishes
// the new revision/draft to the live state. One ctrlMu per run makes the
// whole sequence atomic against resume/stop; a failed save can therefore
// never leave live revision, transcript and checkpoint at different
// revisions. Field presence, not emptiness: Draft is a *string so an
// intentionally EMPTY replacement is expressible ("" is a value, not an
// omission).
//
// v1.7.6 DURABLE RECOVERY (§7): an ordered rollback covers returned errors,
// but NOT a process crash between two persistence stages. The edit is
// therefore wrapped in a small durable journal (edittx.go,
// <DataDir>/runs/paused/<runId>.edittx.json): prepared BEFORE any mutation
// (a journal-write failure aborts the edit with NOTHING changed), advanced
// as the durable stages land, and deleted after live publication. Startup
// recovery (RecoverEditTransactions) converges any interrupted journal to
// ONE coherent revision from the ACTUAL durable evidence — the transcript
// and the checkpoint remain the authorities; the journal is forensic only.
func (s *Server) handleRunEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var body struct {
		SessionID string  `json:"sessionId"`
		RunID     string  `json:"runId"`
		Revision  int64   `json:"revision"`
		Message   *string `json:"message,omitempty"`
		Draft     *string `json:"draft,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("edit: %w", err))
		return
	}

	if body.SessionID == "" || body.RunID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId and runId required"))
		return
	}

	if body.Message == nil && body.Draft == nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("nothing to edit: provide message and/or draft"))
		return
	}

	if body.Message != nil && strings.TrimSpace(*body.Message) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("the user message cannot be replaced with an empty prompt"))
		return
	}

	s.runsMu.Lock()
	rs, ok := s.runs[body.SessionID]
	s.runsMu.Unlock()

	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no registered run %s for session %s", body.RunID, body.SessionID))
		return
	}

	if rs.live.runID != body.RunID {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"runId mismatch: registered run is %s, request targeted %s", rs.live.runID, body.RunID))
		return
	}

	if !rs.live.isPausedSettled() {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"run %s is not editable: phase is %q (editing is allowed only while paused)", body.RunID, rs.live.phaseLocked()))
		return
	}

	// Serialize the whole control transaction against resume/stop/another
	// edit (one mutex per run; the critical section is bounded file work).
	rs.ctrlMu.Lock()
	defer rs.ctrlMu.Unlock()

	// CAS-peek: validate the revision WITHOUT mutating the live state. The
	// revision becomes authoritative only after the checkpoint is durable.
	if !rs.live.canEdit(body.Revision) {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"stale revision: run %s is at revision %d, request acted on %d — reload the paused state and retry",
			body.RunID, rs.live.currentRevision(), body.Revision))
		return
	}

	rec, found := s.loadPausedRecord(body.RunID)
	if !found {
		writeErr(w, http.StatusConflict, fmt.Errorf("paused checkpoint for %s vanished — the run can only be stopped", body.RunID))
		return
	}

	// v1.7.6: prepare the durable recovery journal BEFORE any mutation.
	// It records the bounded pre-/post-edit identities so startup recovery
	// can converge either way without re-deriving anything. Fail closed:
	// without a journal this edit must not start.
	prevUser := rec.OriginalUserMessage
	if rec.EditedUserMessage != "" {
		prevUser = rec.EditedUserMessage
	}

	tx := &editTxRecord{
		Version:               editTxSchemaVer,
		RunID:                 body.RunID,
		SessionID:             body.SessionID,
		Phase:                 editTxPrepared,
		OldRevision:           body.Revision,
		NewRevision:           body.Revision + 1,
		UserMessageChanged:    body.Message != nil,
		OldUserMessage:        prevUser,
		PrevEditedUserMessage: rec.EditedUserMessage,
		DraftChanged:          body.Draft != nil,
	}

	if tx.UserMessageChanged {
		tx.NewUserMessage = *body.Message
	}

	if tx.DraftChanged {
		oldDraft := rec.AssistantDraft
		tx.OldDraft = &oldDraft
	}

	if err := s.saveEditTx(tx); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf(
			"paused edit could not prepare its recovery journal: %w — nothing was changed; retry", err))
		return
	}

	// The durable work, in rollback-able order: the transcript first (one
	// in-place replace — never a second copy), then the checkpoint. The
	// transcript is the session authority; the checkpoint is the
	// run-control authority; the rollback below keeps them reconciled.
	if body.Message != nil {
		rec.EditedUserMessage = *body.Message

		if _, editErr := s.store.EditLastUserMessage(body.SessionID, *body.Message); editErr != nil {
			// Nothing else mutated: live revision and checkpoint are
			// untouched — a clean abort, no reconciliation needed. The
			// journal is removed: this edit never happened.
			s.deleteEditTx(body.RunID)
			writeErr(w, http.StatusInternalServerError, fmt.Errorf("edit user message: %w", editErr))
			return
		}
	}

	// Stage note: the journal phase is forensic detail — startup recovery
	// converges from the ACTUAL durable evidence (transcript position +
	// checkpoint revision), never from the phase alone, so a failed phase
	// write cannot make recovery guess.
	tx.Phase = editTxTranscript
	if err := s.saveEditTx(tx); err != nil {
		logging.Default().Warn("run",
			"edit journal phase update failed (recovery stays evidence-driven): %v", err)
	}

	if body.Draft != nil {
		rec.AssistantDraft = *body.Draft
	}

	rec.Revision = body.Revision + 1
	rec.UpdatedAt = time.Now()

	if err := s.savePausedRecord(rec); err != nil {
		// Roll the transcript back so the checkpoint (still at the old
		// revision, still describing the ORIGINAL message) and the session
		// cannot disagree about what will be resumed.
		if body.Message != nil {
			if _, rbErr := s.store.EditLastUserMessage(body.SessionID, rec.OriginalUserMessage); rbErr != nil {
				logging.Default().Error("run",
					"paused edit rollback failed: %v — resume reconciles from the transcript authority",
					rbErr)
			}
		}

		// The journal is removed: this edit never committed. (If the
		// removal itself failed, the journal is harmless — recovery
		// finds transcript+checkpoint both at the OLD state and
		// converges to the old revision idempotently.)
		s.deleteEditTx(body.RunID)

		// The live state was NEVER mutated: revision still N everywhere the
		// client can see. The conflict is explicit and actionable.
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("paused edit not committed: %w (the run stays paused at revision %d — retry)", err, body.Revision))
		return
	}

	tx.Phase = editTxCheckpoint
	if err := s.saveEditTx(tx); err != nil {
		logging.Default().Warn("run",
			"edit journal phase update failed (recovery stays evidence-driven): %v", err)
	}

	// Commit marker: all durable state is at the NEW revision. The marker
	// itself is tolerant (evidence-driven recovery covers its absence).
	tx.Phase = editTxCommitted
	if err := s.saveEditTx(tx); err != nil {
		logging.Default().Warn("run",
			"edit journal commit marker failed (recovery stays evidence-driven): %v", err)
	}

	// Checkpoint durable + transcript consistent — NOW the accepted edit
	// becomes authoritative on the live state (revision + draft advance
	// together under the state lock).
	rs.live.confirmEdited(rec.AssistantDraft, rec.Revision)

	// Journal cleanup: the edit is fully done. A failed cleanup is
	// harmless — the next boot's recovery finds a coherent NEW state and
	// removes it idempotently.
	s.deleteEditTx(body.RunID)

	logging.Default().Info("run", "paused run edited: revision=%d", rec.Revision)

	writeJSON(w, map[string]any{
		"ok":       true,
		"runId":    body.RunID,
		"revision": rec.Revision,
		"phase":    "paused",
	})
}

// handleRunResume resumes a PAUSED run from its durable checkpoint: the
// SAME runId, hub and live state continue (one authoritative run); the
// transcript + accepted draft are rebuilt into a semantic continuation
// request. The resumed settlement is the shared finishSuccessfulRun path.
func (s *Server) handleRunResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

	var body struct {
		SessionID string `json:"sessionId"`
		RunID     string `json:"runId"`
		Revision  int64  `json:"revision"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("resume: %w", err))
		return
	}

	if body.SessionID == "" || body.RunID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId and runId required"))
		return
	}

	// v1.9.5 (P1): a resumed run is also run ADMISSION — it re-enters
	// the generation pipeline and would observe a mid-transaction
	// engine stop exactly like a fresh run. The same deterministic
	// refusal applies while an engine variant transaction holds the
	// mutation claim (the airtight registration-side coordination in
	// recoverPausedRun's callers keeps the fresh-run path honest; the
	// checkpoint is durable and the resume stays available).
	if s.llama != nil && s.llama.EngineMutationActive() {
		writeErr(w, http.StatusServiceUnavailable, errEngineMutationInProgress)
		return
	}

	s.runsMu.Lock()
	rs, ok := s.runs[body.SessionID]
	s.runsMu.Unlock()

	// Restart-recovery: a paused run whose registry entry is gone (process
	// restart) is re-registered from its durable checkpoint before the
	// state-machine checks run — the paused run remains resumable.
	if !ok {
		rec, found := s.loadPausedRecord(body.RunID)
		if !found || rec.SessionID != body.SessionID {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no resumable run %s for session %s", body.RunID, body.SessionID))
			return
		}

		if rs = s.recoverPausedRun(rec); rs == nil {
			writeErr(w, http.StatusInternalServerError, fmt.Errorf("failed to re-register paused run %s", body.RunID))
			return
		}
	}

	if rs.live.runID != body.RunID {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"runId mismatch: registered run is %s, request targeted %s", rs.live.runID, body.RunID))
		return
	}

	// Serialize the resume decision against an in-flight edit/stop
	// transaction (v1.7.5): the phase CAS and the checkpoint load must
	// observe ONE consistent paused revision, never a mid-edit world.
	rs.ctrlMu.Lock()

	if _, ok := rs.live.requestResume(body.Revision); !ok {
		rs.ctrlMu.Unlock()
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"cannot resume run %s: phase is %q, revision %d (request acted on %d)",
			body.RunID, rs.live.phaseLocked(), rs.live.currentRevision(), body.Revision))
		return
	}

	rec, found := s.loadPausedRecord(body.RunID)
	if !found {
		// The state machine said paused but the checkpoint is gone —
		// honest failure, never a fabricated continuation.
		rs.live.confirmResumed()
		rs.ctrlMu.Unlock()
		writeErr(w, http.StatusConflict, fmt.Errorf("paused checkpoint for %s is missing — cannot resume", body.RunID))
		return
	}

	rs.ctrlMu.Unlock()

	// v1.7.5: the resumed generation's context is created HERE — before the
	// phase reaches the client as "resuming" — and its cancel is installed
	// as the live one. A stop that lands while the run is still RESUMING
	// (the spawn/gate window) must reach the fresh generation; cancelling
	// only from the goroutine left that window unkillable. (The run-budget
	// timeout layers INSIDE runResumed: a handler-scoped defer would cancel
	// the run the moment this handler returns.)
	resumedCtx, resumedCancel := context.WithCancel(context.Background())

	rs.setLiveCancel(resumedCancel)

	logging.Default().Info("run", "resume: revision=%d", body.Revision)

	// v1.10.1: join bookkeeping for the resumed run. The Add happens
	// under runsMu against the same closing flag Close stores before
	// its own runsMu critical section, so the ordering is total: either
	// this section runs first (Close's Wait covers the resumed goroutine)
	// or Close's ran first (the resume is refused — the process is
	// shutting down and no generation may start).
	s.runsMu.Lock()
	if s.closing.Load() {
		s.runsMu.Unlock()
		resumedCancel()

		// Leave the state machine honest: the resume never started,
		// so the run stays PAUSED and resumable in the next session.
		rs.ctrlMu.Lock()
		rs.live.revertResumingLocked()
		rs.ctrlMu.Unlock()

		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("server is shutting down; the run stays paused and resumable"))
		return
	}
	s.runWG.Add(1)
	s.runsMu.Unlock()

	go func() {
		defer s.runWG.Done()

		s.runResumed(rs, rec, resumedCtx, resumedCancel)
	}()

	writeJSON(w, map[string]any{
		"ok":       true,
		"runId":    body.RunID,
		"revision": body.Revision,
		"phase":    "resuming",
	})
}

// pausedUserMessageFor reconciles the paused run's conversation identity
// from the TRANSCRIPT authority (§10 recovery protocol, v1.7.5): the
// session's last user turn is what a resume actually rebuilds from, so it
// is what the paused surface must display and what the settlement must
// record. The checkpoint fields remain the fallback when the session is
// unavailable. This is the deterministic reconciliation that keeps a
// checkpoint claiming nothing the session cannot reconstruct.
func (s *Server) pausedUserMessageFor(sess *sessions.Session, rec *pausedRunRecord) string {
	if sess != nil {
		for i := len(sess.Messages) - 1; i >= 0; i-- {
			if sess.Messages[i].Role == "user" && strings.TrimSpace(sess.Messages[i].Content) != "" {
				return sess.Messages[i].Content
			}
		}
	}

	if rec.EditedUserMessage != "" {
		return rec.EditedUserMessage
	}

	return rec.OriginalUserMessage
}

// handlePausedRuns lists recoverable paused checkpoints — the restart
// recovery surface: after a process restart a paused run is still paused.
func (s *Server) handlePausedRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	sessionID := r.URL.Query().Get("sessionId")

	entries, err := os.ReadDir(s.pausedDir())
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	out := make([]map[string]any, 0)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()

		// v1.7.6: edit-transaction journals (.edittx.json), their
		// quarantined leftovers (.edittx.corrupt.json) and in-flight
		// temp files are NOT checkpoints — never listed, never
		// recoverable runs.
		if strings.HasSuffix(name, editTxSuffix) ||
			strings.HasSuffix(name, editTxCorruptSuffx) ||
			strings.HasSuffix(name, ".tmp") {
			continue
		}

		if filepath.Ext(name) != ".json" {
			continue
		}

		rec, ok := s.loadPausedRecord(name[:len(name)-len(".json")])
		if !ok {
			continue
		}

		if sessionID != "" && rec.SessionID != sessionID {
			continue
		}

		// v1.7.5: the user message shown for the paused run reconciles
		// from the transcript authority (see pausedUserMessageFor).
		var sess *sessions.Session

		if loaded, err := s.store.Get(rec.SessionID); err == nil {
			sess = loaded
		}

		out = append(out, map[string]any{
			"runId":          rec.RunID,
			"sessionId":      rec.SessionID,
			"revision":       rec.Revision,
			"userMessage":    s.pausedUserMessageFor(sess, rec),
			"assistantDraft": rec.AssistantDraft,
			"updatedAt":      rec.UpdatedAt,
		})
	}

	writeJSON(w, out)
}

// recoverPausedRun re-registers a checkpointed paused run after a process
// restart: same runId, same session, phase PAUSED, revision from the
// checkpoint. The hub is fresh (the old one died with the process).
func (s *Server) recoverPausedRun(rec *pausedRunRecord) *runState {
	if _, err := s.store.Get(rec.SessionID); err != nil {
		return nil // the session vanished — nothing to attach to
	}

	live := newRunLive(rec.RunID, rec.SessionID, rec.CreatedAt)
	live.confirmPaused(rec.AssistantDraft, "recovered from restart")

	// v1.7.6 (§7/§12): the recovered live state must claim the CHECKPOINT's
	// revision — a run edited before its process died recovers at revision
	// ≥ 1, and a fresh 0 would contradict the durable state.
	live.restoreRevision(rec.Revision)

	// A recovered paused run owns a cancellation token only so the
	// one-run-per-session replacement keeps working; the resume runner
	// derives its own context.
	_, cancel := context.WithCancel(context.Background())
	hub := newActivityHub()

	rs := &runState{
		cancel: cancel,
		hub:    hub,
		live:   live,
		ctrl:   agent.NewRunControl(),
	}

	s.runsMu.Lock()

	if old, ok := s.runs[rec.SessionID]; ok {
		old.cancel()
		old.hub.close()
	}

	s.runs[rec.SessionID] = rs
	s.runsMu.Unlock()

	return rs
}

// runResumed is the resume half of the ONE run lifecycle: it reuses the
// registered runState (same runId, hub, live, ctrl), rebuilds the
// continuation request from the durable checkpoint and the CURRENT session
// transcript, and settles through the shared finishSuccessfulRun path.
// The generation context is created by the resume HANDLER (v1.7.5) so an
// abort racing the spawn window still reaches the fresh generation.
func (s *Server) runResumed(rs *runState, rec *pausedRunRecord, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	sess, err := s.store.Get(rec.SessionID)
	if err != nil {
		rs.live.settleTerminal("error", "resume failed: session unavailable", false, "", "")
		return
	}

	// v1.8.0: capture the orchestrator's honest terminal caption. The
	// resumed path previously hardcoded "Completed" - an aborted
	// resumed run then recorded the outcome "aborted" with the
	// caption "Completed". The capture mirrors the fresh run path:
	// one honest terminal label.
	terminalCaption := ""

	publish := func(a agent.Activity) {
		a.RunID = rs.live.runID
		a.Seq = rs.live.nextSeq()
		rs.live.observe(a)
		rs.hub.publish(a)

		if a.Type == "done" || a.Type == "complete" || a.Type == "aborted" {
			if a.Caption != "" {
				terminalCaption = a.Caption
			}
		}
	}

	stampRun := func(a agent.Activity) agent.Activity {
		a.RunID = rs.live.runID
		return a
	}

	var settledOnce sync.Once

	settle := func(result, caption string, persisted bool, replyText, reasonText string) {
		settledOnce.Do(func() {
			if s.outcomes != nil {
				s.outcomes.record(sess.ID, runOutcome{
					RunID:       rs.live.runID,
					StartedAt:   rs.live.startedAt,
					EndedAt:     time.Now(),
					Outcome:     result,
					Caption:     caption,
					Persisted:   persisted,
					ReplyChars:  len(replyText),
					ReasonChars: len(reasonText),
				})
			}

			rs.live.settleTerminal(result, caption, persisted, replyText, reasonText)

			logging.Default().Info("run", "resumed run settled: outcome=%s", result)
		})
	}

	// The generation context (ctx/cancel) is created by the resume HANDLER
	// (v1.7.5) and already installed as the live cancel — the abort surface
	// covers the whole resumed lifecycle. The run-budget timeout layers
	// UNDER it here (it must live for the run's duration, not the
	// handler's).
	genCtx := ctx
	genCancel := cancel

	if budget := s.src.Load().EffectiveRunTimeout(); budget > 0 {
		genCtx, genCancel = context.WithTimeout(ctx, budget)
		defer genCancel()
	}

	// Release the registry entry when the RESUMED run settles (the pause
	// path left it in place on purpose).
	defer func() {
		rs.live.mu.Lock()
		terminal := rs.live.terminalOutcome != ""
		rs.live.mu.Unlock()

		if terminal {
			s.runsMu.Lock()
			if current, ok := s.runs[sess.ID]; ok && current == rs {
				delete(s.runs, sess.ID)
			}
			s.runsMu.Unlock()

			rs.hub.close()
			s.deletePausedRecord(rs.live.runID)
		}
	}()

	// The user message is ALREADY in the transcript (appended when the run
	// started); the edit path replaced it in place. Resume never appends a
	// second user message.
	messages := make([]llm.Message, 0, len(sess.Messages))
	messages = append(messages, sess.Messages...)

	// Wire-valid round tail: real results stay, pending calls get honest
	// synthetic results so the conversation contract holds.
	tail := completeRoundTail(rec.RoundTail, rec.PendingToolCalls)
	messages = append(messages, tail...)

	// One authoritative revision: the accepted draft rides the request;
	// the final assistant message is draft + continuation, appended once
	// by finishSuccessfulRun.
	resumeDraft := rec.AssistantDraft

	// Engine gate — identical posture to a fresh run.
	if err := s.stack.EnsureLLMContext(ctx); err != nil {
		settle("error", fmt.Sprintf("Engine unavailable: %v", err), false, "", "")
		publish(stampRun(agent.Activity{
			Type:      "error",
			Caption:   fmt.Sprintf("Engine unavailable: %v", err),
			Timestamp: time.Now(),
		}))
		return
	}

	// Back to generating: the run control is reset so the SAME ctrl can be
	// reused for a future pause of the resumed run.
	rs.ctrl.Clear()
	rs.live.confirmResumed()

	res, err := s.orch.RunDetailed(
		genCtx,
		messages,
		func(a agent.Activity) { publish(a) },
		agent.WithThinkingMode(rec.Thinking),
		agent.WithToolPolicy(rec.ToolMode, rec.ToolAllow),
		agent.WithNetSearch(rec.NetSearch),
		agent.WithRunIdentity(sess.ID, sess.ThreadID, rs.live.runID),
		agent.WithRunControl(rs.ctrl),
		agent.WithResumeContinuation(resumeDraft, rec.ReasoningDraft),
	)

	if err != nil {
		settle("error", err.Error(), false, res.Text, res.Reasoning)
		publish(stampRun(agent.Activity{
			Type:      "error",
			Caption:   err.Error(),
			Timestamp: time.Now(),
		}))
		return
	}

	// A resumed run may itself be PAUSED again (pause → resume → pause):
	// that is the same contract, applied to the updated checkpoint.
	if res.Paused {
		// v1.7.5 ZOMBIE CHECKPOINT GUARD (same rule as the fresh-run
		// pause path): a run that no longer owns its session slot must
		// never checkpoint — the replacement would stay resumable over
		// the live run.
		s.runsMu.Lock()
		current, stillOwns := s.runs[sess.ID]
		s.runsMu.Unlock()

		if !stillOwns || current != rs {
			settle("aborted", "Run replaced by a newer request before it could pause", false, res.Text, res.Reasoning)
			return
		}

		s.pauseResumedRun(rs, rec, res, settle, publish, stampRun)
		return
	}

	// Join draft + continuation into ONE authoritative revision (never
	// duplicated: the draft was never persisted as an assistant message).
	if resumeDraft != "" && res.Text != "" {
		res.Text = joinDraftContinuation(resumeDraft, res.Text)
	} else if resumeDraft != "" && res.Text == "" {
		// The continuation produced nothing new — the draft IS the answer.
		res.Text = resumeDraft
	}

	resultOutcome := "done"
	if genCtx.Err() != nil {
		resultOutcome = "aborted"
	}

	if terminalCaption == "" {
		terminalCaption = "Completed"
	}

	// v1.7.5: the TRANSCRIPT is the authority for what will be resumed —
	// reconcile the conversation identity from it (the checkpoint fields
	// are only the fallback), so a checkpoint/transcript divergence can
	// never surface as a resumed run summarizing the wrong prompt.
	userMessage := s.pausedUserMessageFor(sess, rec)

	s.finishSuccessfulRun(successRunParams{
		sess:            sess,
		userMessage:     userMessage,
		res:             res,
		resultOutcome:   resultOutcome,
		terminalCaption: terminalCaption,
		tl:              &runTimeline{runID: rs.live.runID, sessionID: sess.ID, acceptedAt: rs.live.startedAt, registeredAt: rs.live.startedAt},
		settle:          settle,
		publish:         publish,
		stampRun:        stampRun,
	})
}

// pauseResumedRun checkpoints a pause that happened on a RESUMED run: the
// same state machine, a bumped history, an updated draft.
func (s *Server) pauseResumedRun(
	rs *runState,
	prev *pausedRunRecord,
	res agent.RunResult,
	settle func(string, string, bool, string, string),
	publish func(agent.Activity),
	stampRun func(agent.Activity) agent.Activity,
) {
	draft := res.Text
	if prev.AssistantDraft != "" && draft == "" {
		// A committed-boundary pause mid-continuation: the accepted prefix
		// is still the previous draft.
		draft = prev.AssistantDraft
	} else if prev.AssistantDraft != "" && draft != "" && res.PauseToolState == agent.ToolStateCommitted {
		draft = prev.AssistantDraft
	}

	rec := *prev
	rec.AssistantDraft = draft
	rec.ReasoningDraft = res.Reasoning
	rec.ToolState = string(res.PauseToolState)
	rec.PendingToolCalls = res.PendingToolCalls
	rec.RoundTail = completeRoundTail(res.RoundTail, res.PendingToolCalls)
	rec.UpdatedAt = time.Now()

	if err := s.savePausedRecord(&rec); err != nil {
		settle("error", "pause checkpoint failed: "+err.Error(), false, draft, res.Reasoning)
		publish(stampRun(agent.Activity{
			Type:      "error",
			Caption:   "The run could not be paused safely: " + err.Error(),
			Timestamp: time.Now(),
		}))
		return
	}

	// Checkpoint is durable — only now does the state promise PAUSED.
	rs.live.confirmPaused(draft, "paused at a safe boundary")

	publish(stampRun(agent.Activity{
		Type:    "status",
		Caption: "Paused",
		Detail: map[string]any{
			"phase":    "paused",
			"revision": rec.Revision,
			// v1.7.5: the accepted draft rides the confirmation.
			"pausedDraft": rec.AssistantDraft,
		},
		Timestamp: time.Now(),
	}))
}

// joinDraftContinuation merges the accepted prefix and the continuation
// into one authoritative revision. The join is byte-honest: no invented
// punctuation, one space when both sides need a separator.
func joinDraftContinuation(draft, continuation string) string {
	// The seam is decided from the ORIGINAL draft tail: a draft that
	// already ends with a newline needs no space (the newline IS the
	// separator). Deciding after the TrimRight below made the newline
	// check dead code and injected a stray space after every
	// newline-terminated draft — the "byte-honest" join regressed.
	seam := " "
	if strings.HasSuffix(draft, "\n") {
		seam = "\n"
	}

	draft = strings.TrimRight(draft, " \t\n\r")
	continuation = strings.TrimLeft(continuation, " \t")

	if draft == "" {
		return continuation
	}
	if continuation == "" {
		return draft
	}

	return draft + seam + continuation
}
