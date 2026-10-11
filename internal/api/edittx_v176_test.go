package api

// edittx_v176_test.go — v1.7.6 fault-injection proofs for the DURABLE
// EDIT-TRANSACTION RECOVERY (§7).
//
// The v1.7.5 in-code ordering covers every RETURNED error. What it cannot
// cover is a process crash BETWEEN persistence stages — the transcript may
// be new while the checkpoint is old, with nothing on disk explaining how
// far the edit got. The v1.7.6 journal (edittx.go) closes exactly that.
//
// The tests reproduce each crash window WITHOUT sleeps: the durable
// leftovers are produced either by driving the REAL handler with a seam
// that simulates process death (a panic unwinding through the deferred
// ctrlMu release — the same effect as the process dying mid-handler) or by
// writing the exact artifacts a crash would have left, then a FRESH Server
// on the SAME data root performs the startup recovery (api.New calls
// RecoverEditTransactions — the production path).
//
// Windows covered (§7 list):
//   1. after prepare            → converge to the OLD revision (nothing mutated)
//   2. after transcript durable → converge FORWARD to the NEW revision
//   3. after checkpoint durable → converge FORWARD (checkpoint completes)
//   4. after commit marker      → converge FORWARD, journal cleaned
//   5./6. before/after live publication before cleanup → journal cleaned
//   7. transcript write failure → clean abort, journal removed (no residue)
//   8. checkpoint write failure → clean abort, journal removed, retry works
//   9. journal write failure    → edit aborts BEFORE any mutation
//  10. restart recovery         → the recovered run lists, resumes, settles,
//     exactly one authoritative assistant turn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// newRemoteServerOnDir is newRemoteServerWithHandle pinned to an EXPLICIT
// data root, so two consecutive boots share one durable tree.
func newRemoteServerOnDir(t *testing.T, engineURL, dataDir string) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ModelsDir = dataDir + "/models"
	cfg.SessionsDir = dataDir + "/sessions"
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

// postJSONDisconnect fires one edit request and ACCEPTS a hard connection
// failure — the wire-level effect of the handler (process) dying before a
// response is written. Returns the response when one did arrive.
func postJSONDisconnect(t *testing.T, server *httptest.Server, path string, body map[string]any) (int, map[string]any, bool) {
	t.Helper()

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := server.Client().Do(req)
	if err != nil {
		return 0, nil, true // the process died mid-handler — the expected crash shape
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return resp.StatusCode, nil, true
	}

	return resp.StatusCode, decoded, false
}

// editTxBuildForEdit reproduces the journal the handler prepares for one
// paused-run edit (the handler's exact fields from the CURRENT checkpoint).
func editTxBuildForEdit(srv *Server, runID, sessionID string, revision int64, newMessage *string, newDraft *string) *editTxRecord {
	rec, ok := srv.loadPausedRecord(runID)
	if !ok {
		panic("editTxBuildForEdit: no checkpoint for " + runID)
	}

	prevUser := rec.OriginalUserMessage
	if rec.EditedUserMessage != "" {
		prevUser = rec.EditedUserMessage
	}

	tx := &editTxRecord{
		Version:               editTxSchemaVer,
		RunID:                 runID,
		SessionID:             sessionID,
		Phase:                 editTxPrepared,
		OldRevision:           revision,
		NewRevision:           revision + 1,
		UserMessageChanged:    newMessage != nil,
		OldUserMessage:        prevUser,
		PrevEditedUserMessage: rec.EditedUserMessage,
		DraftChanged:          newDraft != nil,
	}

	if tx.UserMessageChanged {
		tx.NewUserMessage = *newMessage
	}

	if tx.DraftChanged {
		oldDraft := rec.AssistantDraft
		tx.OldDraft = &oldDraft
		tx.NewDraft = newDraft
	}

	return tx
}

// journalExists reports whether the journal file for runID is on disk.
func journalExists(t *testing.T, srv *Server, runID string) bool {
	t.Helper()
	_, found := srv.loadEditTx(runID)
	if found {
		return true
	}
	_, err := os.Stat(srv.editTxPath(runID))
	return err == nil
}

// assertNoJournal fails the test unless the journal for runID is fully
// gone (live path and any residue).
func assertNoJournal(t *testing.T, srv *Server, runID string) {
	t.Helper()

	if journalExists(t, srv, runID) {
		t.Fatalf("journal for %s must be gone after recovery", runID)
	}

	if _, err := os.Stat(srv.editTxPath(runID) + ".tmp"); err == nil {
		t.Fatalf("journal temp residue for %s must be gone after recovery", runID)
	}
}

// TestEditTxJournalWriteFailureAbortsBeforeMutation — §7 #9: the journal
// cannot be made durable, so the edit must not START. Nothing mutates.
func TestEditTxJournalWriteFailureAbortsBeforeMutation(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	editTxSaveSeam = func(*editTxRecord) error { return fmt.Errorf("disk full (injected)") }
	defer func() { editTxSaveSeam = nil }()

	edited := "EDITED but the journal refused."

	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   edited,
	})

	if status != http.StatusInternalServerError {
		t.Fatalf("edit status = %d, want 500 (journal write failed)", status)
	}

	if !strings.Contains(body["error"].(string), "recovery journal") {
		t.Fatalf("error must name the recovery journal, got %v", body["error"])
	}

	// NOTHING mutated: live state, transcript, checkpoint all at the OLD
	// revision; no journal residue on disk.
	live := registeredRun(srv, sessionID).live.snapshot()
	if live.Revision != snap.Revision || live.Phase != "paused" {
		t.Fatalf("live state = (rev %d, %q), want unchanged paused", live.Revision, live.Phase)
	}

	messages := fetchTranscript(t, server, sessionID)
	if messages[0].Content == edited {
		t.Fatal("the transcript was edited although the journal write FAILED — fail-closed violated")
	}

	rec := readPausedCheckpoint(t, srv, runID)
	if rec.Revision != snap.Revision || rec.EditedUserMessage != "" {
		t.Fatalf("checkpoint = (rev %d, edited %q), want untouched", rec.Revision, rec.EditedUserMessage)
	}

	if journalExists(t, srv, runID) {
		t.Fatal("a journal residue survived the failed prepare — it must not")
	}
}

// TestEditTxTranscriptFailureCleansJournal — §7 #7: the transcript write
// fails AFTER the journal was prepared; the clean abort must remove the
// journal (this edit never happened).
func TestEditTxTranscriptFailureCleansJournal(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Force the transcript edit to fail (same mechanism as v1.7.5): the
	// user turn vanishes from the transcript underneath the paused run.
	sess, err := srv.store.Get(sessionID)
	if err != nil {
		t.Fatalf("fetch session: %v", err)
	}

	kept := make([]llm.Message, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		if m.Role != "user" {
			kept = append(kept, m)
		}
	}
	sess.Messages = append(kept, llm.Message{Role: "assistant", Content: "user turn removed"})

	if err := srv.store.SaveMessagesKeepContext(sess); err != nil {
		t.Fatalf("rewrite transcript: %v", err)
	}

	status, _ := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   "this edit cannot touch the transcript",
	})

	if status != http.StatusInternalServerError {
		t.Fatalf("edit status = %d, want 500 (transcript failure)", status)
	}

	if journalExists(t, srv, runID) {
		t.Fatal("the journal survived a transcript-failure abort — the clean abort must remove it")
	}

	rec := readPausedCheckpoint(t, srv, runID)
	if rec.Revision != snap.Revision || rec.EditedUserMessage != "" {
		t.Fatalf("checkpoint = (rev %d, edited %q), want untouched", rec.Revision, rec.EditedUserMessage)
	}
}

// TestEditTxCheckpointSaveFailureCleansJournalAndRetries — §7 #8: the
// checkpoint commit fails after the transcript edit; the ordered rollback
// must ALSO remove the journal, and a retry afterwards must succeed with a
// clean journal lifecycle.
func TestEditTxCheckpointSaveFailureCleansJournalAndRetries(t *testing.T) {
	engine := remoteFakeEngineThen(t, pauseStreamChunks(pauseChunkCount), "continuation-only-answer.", pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	pausedSaveSeam = func(*pausedRunRecord) error { return fmt.Errorf("commit failed (injected)") }

	edited := "EDITED under a failing checkpoint commit."

	status, _ := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   edited,
	})

	if status != http.StatusInternalServerError {
		t.Fatalf("edit status = %d, want 500 (checkpoint commit failed)", status)
	}

	pausedSaveSeam = nil

	if journalExists(t, srv, runID) {
		t.Fatal("the journal survived a checkpoint-commit failure — the rollback must remove it")
	}

	// The transcript was rolled back; a RETRY at the same revision works
	// and completes the full journal lifecycle (prepared → … → deleted).
	retry := "EDITED on the retry."
	status, body := postJSON(t, server, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   retry,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("retry edit status = %d body = %v, want 200 ok", status, body)
	}

	if journalExists(t, srv, runID) {
		t.Fatal("the journal must be deleted after a fully completed edit")
	}

	rec := readPausedCheckpoint(t, srv, runID)
	if rec.Revision != snap.Revision+1 || rec.EditedUserMessage != retry {
		t.Fatalf("checkpoint after retry = (rev %d, edited %q), want (rev %d, %q)",
			rec.Revision, rec.EditedUserMessage, snap.Revision+1, retry)
	}
}

// TestEditTxRecoveryConvergesEveryCrashWindow — §7 #1–4, #5/#6 and the
// draft-only variants. Each case leaves the EXACT durable leftovers of a
// crash at that stage on a shared data root, then a FRESH Server performs
// the production startup recovery and must converge to ONE coherent
// revision, deterministically and idempotently.
func TestEditTxRecoveryConvergesEveryCrashWindow(t *testing.T) {
	t.Run("journal prepared, nothing mutated → OLD revision, journal cleaned", func(t *testing.T) {
		dataRoot := t.TempDir()
		engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
		boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
		sessionID := createSessionForRun(t, server1)
		runID := postRunMessage(t, server1, sessionID, pausePrompt)
		snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

		// Crash window #1: the journal landed, NOTHING else did.
		tx := editTxBuildForEdit(boot1, runID, sessionID, snap.Revision, ptrString("EDITED then crashed."), nil)
		tx.Phase = editTxPrepared

		if err := boot1.saveEditTx(tx); err != nil {
			t.Fatalf("seed journal: %v", err)
		}

		boot2, _ := newRemoteServerOnDir(t, engine.URL, dataRoot) // api.New ran the recovery

		assertNoJournal(t, boot2, runID)

		rec := readPausedCheckpoint(t, boot2, runID)
		if rec.Revision != snap.Revision || rec.EditedUserMessage != "" {
			t.Fatalf("recovered checkpoint = (rev %d, edited %q), want the OLD revision untouched",
				rec.Revision, rec.EditedUserMessage)
		}

		messages := fetchTranscript(t, server1, sessionID)
		if messages[0].Content == "EDITED then crashed." {
			t.Fatal("the transcript carries the edit although the crash preceded it")
		}

		boot2.RecoverEditTransactions() // idempotent second pass
		assertNoJournal(t, boot2, runID)
	})

	t.Run("transcript durable, checkpoint old → converge FORWARD to the NEW revision", func(t *testing.T) {
		dataRoot := t.TempDir()
		engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
		boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
		sessionID := createSessionForRun(t, server1)
		runID := postRunMessage(t, server1, sessionID, pausePrompt)
		snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

		// Crash window #2 (seam-simulated process death): the REAL handler
		// runs until its journal stage-note AFTER the transcript edit, then
		// the process dies. Durable leftovers: journal(prepared),
		// transcript NEW, checkpoint OLD.
		edited := "EDITED then the process died."
		editTxSaveSeam = func(rec *editTxRecord) error {
			if rec.Phase == editTxTranscript {
				panic(fmt.Sprintf("simulated crash after transcript durable: runId=%s", rec.RunID))
			}
			return nil
		}

		status, _, disconnected := postJSONDisconnect(t, server1, "/api/run/edit", map[string]any{
			"sessionId": sessionID,
			"runId":     runID,
			"revision":  snap.Revision,
			"message":   edited,
		})
		editTxSaveSeam = nil

		if !disconnected && status != http.StatusInternalServerError {
			t.Fatalf("the crashing edit must not answer 200 (status %d)", status)
		}

		if !journalExists(t, boot1, runID) {
			t.Fatal("precondition: the journal must be on disk after the simulated crash")
		}

		boot2, _ := newRemoteServerOnDir(t, engine.URL, dataRoot)

		assertNoJournal(t, boot2, runID)

		rec := readPausedCheckpoint(t, boot2, runID)
		if rec.Revision != snap.Revision+1 || rec.EditedUserMessage != edited {
			t.Fatalf("recovered checkpoint = (rev %d, edited %q), want FORWARD convergence to (rev %d, %q)",
				rec.Revision, rec.EditedUserMessage, snap.Revision+1, edited)
		}

		messages := fetchTranscript(t, server1, sessionID)
		if messages[0].Content != edited {
			t.Fatalf("transcript must carry the edited message after forward convergence, got %q", messages[0].Content)
		}
	})

	t.Run("checkpoint durable, marker missing → converge FORWARD, journal cleaned", func(t *testing.T) {
		dataRoot := t.TempDir()
		engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
		boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
		sessionID := createSessionForRun(t, server1)
		runID := postRunMessage(t, server1, sessionID, pausePrompt)
		snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

		// Crash window #3 (seam-simulated): the process dies when the
		// journal's checkpoint stage-note is written — the REAL checkpoint
		// is already durable. Leftovers: journal(transcript), transcript
		// NEW, checkpoint NEW.
		edited := "EDITED and fully durable before the crash."
		editTxSaveSeam = func(rec *editTxRecord) error {
			if rec.Phase == editTxCheckpoint {
				panic(fmt.Sprintf("simulated crash after checkpoint durable: runId=%s", rec.RunID))
			}
			return nil
		}

		status, _, disconnected := postJSONDisconnect(t, server1, "/api/run/edit", map[string]any{
			"sessionId": sessionID,
			"runId":     runID,
			"revision":  snap.Revision,
			"message":   edited,
		})
		editTxSaveSeam = nil

		if !disconnected && status != http.StatusInternalServerError {
			t.Fatalf("the crashing edit must not answer 200 (status %d)", status)
		}

		boot2, _ := newRemoteServerOnDir(t, engine.URL, dataRoot)

		assertNoJournal(t, boot2, runID)

		rec := readPausedCheckpoint(t, boot2, runID)
		if rec.Revision != snap.Revision+1 || rec.EditedUserMessage != edited {
			t.Fatalf("recovered checkpoint = (rev %d, edited %q), want the NEW revision kept",
				rec.Revision, rec.EditedUserMessage)
		}
	})

	t.Run("commit marker durable, cleanup missing → journal cleaned (idempotent)", func(t *testing.T) {
		dataRoot := t.TempDir()
		engine := remoteFakeEngineThen(t, pauseStreamChunks(pauseChunkCount), "continuation-only-answer.", pauseChunkDelay)
		boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
		sessionID := createSessionForRun(t, server1)
		runID := postRunMessage(t, server1, sessionID, pausePrompt)
		snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

		// Crash windows #4/#5/#6 (after commit marker, around live
		// publication / before cleanup): the edit COMPLETES through the
		// real handler; the journal survives because the cleanup never
		// runs — exactly what a crash in that window leaves behind.
		edited := "EDITED fully, cleanup never ran."

		status, body := postJSON(t, server1, "/api/run/edit", map[string]any{
			"sessionId": sessionID,
			"runId":     runID,
			"revision":  snap.Revision,
			"message":   edited,
		})

		if status != http.StatusOK || body["ok"] != true {
			t.Fatalf("edit status = %d body = %v, want 200 ok", status, body)
		}

		tx := editTxBuildForEdit(boot1, runID, sessionID, snap.Revision, ptrString(edited), nil)
		tx.Phase = editTxCommitted

		if err := boot1.saveEditTx(tx); err != nil {
			t.Fatalf("seed committed journal: %v", err)
		}

		boot2, _ := newRemoteServerOnDir(t, engine.URL, dataRoot)

		assertNoJournal(t, boot2, runID)

		rec := readPausedCheckpoint(t, boot2, runID)
		if rec.Revision != snap.Revision+1 || rec.EditedUserMessage != edited {
			t.Fatalf("recovered checkpoint = (rev %d, edited %q), want the NEW revision kept",
				rec.Revision, rec.EditedUserMessage)
		}
	})

	t.Run("draft-only crash before checkpoint → OLD draft kept, journal cleaned", func(t *testing.T) {
		dataRoot := t.TempDir()
		engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
		boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
		sessionID := createSessionForRun(t, server1)
		runID := postRunMessage(t, server1, sessionID, pausePrompt)
		snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

		if snap.PausedDraft == "" {
			t.Fatal("precondition: paused draft must be non-empty")
		}

		tx := editTxBuildForEdit(boot1, runID, sessionID, snap.Revision, nil, ptrString(""))
		tx.Phase = editTxPrepared

		if err := boot1.saveEditTx(tx); err != nil {
			t.Fatalf("seed journal: %v", err)
		}

		boot2, _ := newRemoteServerOnDir(t, engine.URL, dataRoot)

		assertNoJournal(t, boot2, runID)

		rec := readPausedCheckpoint(t, boot2, runID)
		if rec.Revision != snap.Revision || rec.AssistantDraft != snap.PausedDraft {
			t.Fatalf("recovered checkpoint = (rev %d, draft %q), want the OLD draft kept",
				rec.Revision, rec.AssistantDraft)
		}
	})

	t.Run("draft-only crash after checkpoint → NEW draft kept, journal cleaned", func(t *testing.T) {
		dataRoot := t.TempDir()
		engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
		boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
		sessionID := createSessionForRun(t, server1)
		runID := postRunMessage(t, server1, sessionID, pausePrompt)
		snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

		erased := ""
		tx := editTxBuildForEdit(boot1, runID, sessionID, snap.Revision, nil, &erased)
		tx.Phase = editTxTranscript

		if err := boot1.saveEditTx(tx); err != nil {
			t.Fatalf("seed journal: %v", err)
		}

		// The checkpoint write landed before the crash (the draft erase).
		rec, ok := boot1.loadPausedRecord(runID)
		if !ok {
			t.Fatal("checkpoint vanished")
		}
		rec.AssistantDraft = erased
		rec.Revision = snap.Revision + 1

		if err := boot1.savePausedRecord(rec); err != nil {
			t.Fatalf("seed checkpoint: %v", err)
		}

		boot2, _ := newRemoteServerOnDir(t, engine.URL, dataRoot)

		assertNoJournal(t, boot2, runID)

		rec2 := readPausedCheckpoint(t, boot2, runID)
		if rec2.Revision != snap.Revision+1 || rec2.AssistantDraft != "" {
			t.Fatalf("recovered checkpoint = (rev %d, draft %q), want the erased draft kept",
				rec2.Revision, rec2.AssistantDraft)
		}
	})
}

// TestEditTxCorruptJournalFailsClosed — a journal whose bytes are garbage
// is QUARANTINED (fail closed): the checkpoint and transcript stay
// untouched, an unrelated session keeps working, and the paused surface
// never lists the journal as a run.
func TestEditTxCorruptJournalFailsClosed(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	boot1, server1 := newRemoteServerOnDir(t, engine.URL, t.TempDir())
	sessionID := createSessionForRun(t, server1)
	runID := postRunMessage(t, server1, sessionID, pausePrompt)
	snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

	// Corrupt the journal in place (truncated / garbage bytes).
	journal := boot1.editTxPath(runID)

	if err := os.WriteFile(journal, []byte(`{"runId":"`+runID+`","phas`), 0o644); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}

	boot2, server2 := newRemoteServerOnDir(t, engine.URL, boot1.src.Load().DataDir)

	// The corrupt journal was quarantined, not applied, not deleted.
	corruptPath := filepath.Join(boot2.pausedDir(), runID+editTxCorruptSuffx)

	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("the corrupt journal must be quarantined at %s: %v", corruptPath, err)
	}

	if _, err := os.Stat(journal); err == nil {
		t.Fatal("the corrupt journal must not remain at its live path")
	}

	// The paused state is untouched and honest.
	rec := readPausedCheckpoint(t, boot2, runID)
	if rec.Revision != snap.Revision || rec.EditedUserMessage != "" {
		t.Fatalf("checkpoint = (rev %d, edited %q), want untouched", rec.Revision, rec.EditedUserMessage)
	}

	// The paused listing never shows the journal as a run.
	resp, err := http.Get(server2.URL + "/api/run/paused?sessionId=" + sessionID)
	if err != nil {
		t.Fatalf("GET /api/run/paused: %v", err)
	}
	defer resp.Body.Close()

	var listed []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode paused listing: %v", err)
	}

	count := 0
	for _, entry := range listed {
		if entry["runId"] == runID {
			count++
		}
	}

	if count != 1 {
		t.Fatalf("the paused listing must show the checkpoint exactly once (no journal pollution), got %d", count)
	}

	// An unrelated session keeps working after the fail-closed quarantine.
	other := createSessionForRun(t, server2)
	if other == "" {
		t.Fatal("unrelated session creation failed after quarantine")
	}
}

// TestEditTxRestartRecoveryResumesCoherently — §7 #10 end-to-end: crash
// mid-edit → startup recovery converges FORWARD → /api/run/paused exposes
// the coherent revision → the run RESUMES at the recovered revision and
// settles with exactly one authoritative assistant turn and one user turn.
func TestEditTxRestartRecoveryResumesCoherently(t *testing.T) {
	dataRoot := t.TempDir()
	engine := remoteFakeEngineThen(t, pauseStreamChunks(pauseChunkCount), "continuation-only-answer.", pauseChunkDelay)
	boot1, server1 := newRemoteServerOnDir(t, engine.URL, dataRoot)
	sessionID := createSessionForRun(t, server1)
	runID := postRunMessage(t, server1, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

	// Crash after the transcript edit (seam-simulated process death).
	edited := "EDITED then recovered across a restart."
	editTxSaveSeam = func(rec *editTxRecord) error {
		if rec.Phase == editTxTranscript {
			panic("simulated crash: " + rec.RunID)
		}
		return nil
	}

	postJSONDisconnect(t, server1, "/api/run/edit", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
		"revision":  snap.Revision,
		"message":   edited,
	})
	editTxSaveSeam = nil

	// ---- BOOT 2: the recovery ran inside api.New, BEFORE any exposure.
	boot2, server2 := newRemoteServerOnDir(t, engine.URL, dataRoot)

	// The paused surface lists exactly ONE coherent entry: the NEW revision
	// and the transcript-authoritative message.
	resp, err := http.Get(server2.URL + "/api/run/paused?sessionId=" + sessionID)
	if err != nil {
		t.Fatalf("GET /api/run/paused: %v", err)
	}

	var listed []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		resp.Body.Close()
		t.Fatalf("decode paused listing: %v", err)
	}
	resp.Body.Close()

	if len(listed) != 1 {
		t.Fatalf("paused listing = %d entries, want exactly 1", len(listed))
	}

	entry := listed[0]

	if entry["runId"] != runID {
		t.Fatalf("listed runId = %v, want %s", entry["runId"], runID)
	}

	if int64(entry["revision"].(float64)) != snap.Revision+1 {
		t.Fatalf("listed revision = %v, want %d (forward convergence)", entry["revision"], snap.Revision+1)
	}

	if entry["userMessage"] != edited {
		t.Fatalf("listed userMessage = %v, want the recovered edit %q", entry["userMessage"], edited)
	}

	// The recovered run RESUMES at the recovered revision.
	resumeRun(t, server2, sessionID, runID, snap.Revision+1)

	if !waitForRunSettledFor(t, boot2, sessionID, runID) {
		t.Fatal("the recovered run never settled")
	}

	waitForCheckpointGone(t, boot2, runID)

	messages := fetchTranscript(t, server2, sessionID)

	if n := countUserMessages(messages); n != 1 {
		t.Fatalf("exactly one user turn required after recovery+resume, got %d", n)
	}

	replies := assistantReplies(messages)

	if len(replies) != 1 {
		t.Fatalf("exactly one authoritative assistant reply required, got %d: %q", len(replies), replies)
	}

	if !strings.Contains(replies[0], "continuation-only-answer.") {
		t.Fatalf("the resumed reply is not the continuation: %q", replies[0])
	}
}

// TestEditTxRecoveryIsDeterministicWithoutSleeps is a guard against
// regression to timing-based simulation: the recovery path contains no
// polling and no artificial delay — this test just documents the contract
// by running a full converge cycle repeatedly with fresh objects (no
// shared mutable state between runs).
func TestEditTxRecoveryIsDeterministicWithoutSleeps(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	boot1, server1 := newRemoteServerOnDir(t, engine.URL, t.TempDir())
	sessionID := createSessionForRun(t, server1)
	runID := postRunMessage(t, server1, sessionID, pausePrompt)
	snap := pauseRunMidStream(t, boot1, server1, sessionID, runID)

	edited := "Deterministic convergence."

	for i := 0; i < 3; i++ {
		tx := editTxBuildForEdit(boot1, runID, sessionID, snap.Revision, ptrString(edited), nil)
		tx.Phase = editTxPrepared

		if err := boot1.saveEditTx(tx); err != nil {
			t.Fatalf("pass %d: seed journal: %v", i, err)
		}

		// Direct recovery call: fresh journal, same outcome every time.
		boot1.RecoverEditTransactions()

		if journalExists(t, boot1, runID) {
			t.Fatalf("pass %d: journal not converged", i)
		}

		// The transcript is at the OLD message (the crash preceded the
		// edit) — recovery must never apply a half-seen edit forward on
		// its own authority.
		messages := fetchTranscript(t, server1, sessionID)
		if messages[0].Content == edited {
			t.Fatalf("pass %d: recovery invented the edit forward without the transcript evidence", i)
		}
	}

	// The checkpoint is untouched throughout.
	rec := readPausedCheckpoint(t, boot1, runID)
	if rec.Revision != snap.Revision || rec.EditedUserMessage != "" {
		t.Fatalf("checkpoint = (rev %d, edited %q), want untouched across all passes", rec.Revision, rec.EditedUserMessage)
	}
}

// ptrString returns a pointer to s (test helper).
func ptrString(s string) *string { return &s }

// Silence the unused imports if helpers change shape (compile-time only).
var (
	_ = context.Background
	_ = time.Now
)
