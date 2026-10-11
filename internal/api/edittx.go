// edittx.go — v1.7.6 DURABLE EDIT-TRANSACTION RECOVERY RECORD (§7).
//
// The v1.7.5 edit path made the ordering of the durable stages correct
// (transcript replace → checkpoint commit → rollback on failure → live
// publication last, all serialized by one per-run ctrlMu). An ordered
// rollback still cannot cover a PROCESS CRASH between two persistence
// stages: nothing survives the crash that explains how far the edit got.
//
// This file adds exactly one small recovery journal under the EXISTING
// data root — <DataDir>/runs/paused/<runId>.edittx.json — written
// atomically (temp+rename, the same pattern as the checkpoint itself).
// It is NOT a second session or run authority:
//
//   - the session transcript remains the conversation authority;
//   - the paused checkpoint (<runId>.json) remains the run-control
//     authority;
//   - the journal exists ONLY to make a crash between persistence stages
//     deterministically recoverable, and it is deleted once the edit is
//     complete. In the normal path it lives for the duration of the edit.
//
// PROTOCOL (deterministic prepare/commit/recovery):
//
//	prepared          — journal exists, nothing else mutated yet
//	transcript        — the in-place transcript replace is durable
//	checkpoint        — the new checkpoint is durable
//	committed         — all durable state is at the NEW revision
//	(live publication by confirmEdited, then journal deletion)
//
// RECOVERY (RecoverEditTransactions, runs at startup, before any paused
// run can be exposed): for every journal, inspect the ACTUAL durable
// state (checkpoint revision + transcript user message) — never trust the
// phase alone — and converge to exactly ONE coherent revision:
//
//   - checkpoint missing/consumed (the run was stopped): undo any visible
//     transcript edit to the recorded pre-edit message, delete journal;
//   - transcript still at the OLD message (or the edit was draft-only and
//     the checkpoint is still old): converge to the OLD revision — the
//     checkpoint is rewritten back if it had advanced (fields restored
//     from the journal);
//   - transcript at the NEW message: converge FORWARD to the NEW revision
//     — the checkpoint is completed if it had not advanced;
//   - transcript matches NEITHER: integrity cannot be trusted — the
//     journal is quarantined (<runId>.edittx.corrupt.json) and an
//     actionable error is logged. Nothing is guessed; every other session
//     and run stays usable.
//
// A journal whose bytes or integrity hash do not verify is treated the
// same way (fail closed). Recovery is idempotent: converged records are
// deleted, so a second pass finds nothing.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// Edit-transaction phases (durable, in journal order).
const (
	editTxPrepared     = "prepared"
	editTxTranscript   = "transcript"
	editTxCheckpoint   = "checkpoint"
	editTxCommitted    = "committed"
	editTxSchemaVer    = 1
	editTxSuffix       = ".edittx.json"
	editTxCorruptSuffx = ".edittx.corrupt.json"
)

// editTxRecord is the bounded recovery record for ONE paused-run edit.
type editTxRecord struct {
	Version   int    `json:"version"` // editTxSchemaVer
	RunID     string `json:"runId"`
	SessionID string `json:"sessionId"`

	Phase string `json:"phase"` // prepared | transcript | checkpoint | committed

	OldRevision int64 `json:"oldRevision"`
	NewRevision int64 `json:"newRevision"`

	// Bounded pre-/post-edit conversation identity (the exact strings the
	// recovery needs to converge either way — no transcript re-derivation,
	// no unbounded duplication: both are capped by the edit body limit).
	UserMessageChanged bool   `json:"userMessageChanged"`
	OldUserMessage     string `json:"oldUserMessage,omitempty"`
	NewUserMessage     string `json:"newUserMessage,omitempty"`
	// The checkpoint's EditedUserMessage BEFORE this edit ("" when the old
	// revision was never edited): rollback restores it exactly instead of
	// wiping a previous edit's message.
	PrevEditedUserMessage string `json:"prevEditedUserMessage,omitempty"`

	// Draft state when the DRAFT changed (nil = untouched by this edit).
	DraftChanged bool    `json:"draftChanged"`
	OldDraft     *string `json:"oldDraft,omitempty"`
	NewDraft     *string `json:"newDraft,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`

	// Integrity: sha256 over the record marshaled with Integrity="".
	Integrity string `json:"integrity"`
}

// editTxSaveSeam is the v1.7.6 fault-injection seam for EVERY journal
// write (the same pattern as pausedSaveSeam). Tests substitute it to
// force prepare/phase-advance failures — the exact crash windows §7
// forbids from becoming inconsistent. Nil in production; production
// behavior is marshal + temp + rename only.
var editTxSaveSeam func(rec *editTxRecord) error

// editTxIntegrity computes the record's integrity hash over the canonical
// marshaled body with the Integrity field empty.
func editTxIntegrity(rec *editTxRecord) string {
	clone := *rec
	clone.Integrity = ""

	data, err := json.Marshal(&clone)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// verifyEditTx parses and integrity-checks one journal body.
func verifyEditTx(data []byte) (*editTxRecord, error) {
	if len(data) > 2*(1<<20) {
		return nil, fmt.Errorf("journal exceeds the bounded size")
	}

	var rec editTxRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("journal is not valid JSON: %w", err)
	}

	if rec.Version != editTxSchemaVer {
		return nil, fmt.Errorf("journal schema version %d unsupported", rec.Version)
	}

	if rec.RunID == "" || rec.SessionID == "" {
		return nil, fmt.Errorf("journal missing runId/sessionId")
	}

	want := editTxIntegrity(&rec)
	if want == "" || want != rec.Integrity {
		return nil, fmt.Errorf("journal integrity mismatch (want %s, recorded %s)", want, rec.Integrity)
	}

	switch rec.Phase {
	case editTxPrepared, editTxTranscript, editTxCheckpoint, editTxCommitted:
	default:
		return nil, fmt.Errorf("journal phase %q unknown", rec.Phase)
	}

	if rec.NewRevision <= rec.OldRevision {
		return nil, fmt.Errorf("journal revisions not advancing (%d -> %d)", rec.OldRevision, rec.NewRevision)
	}

	return &rec, nil
}

// editTxPath is the journal path for one run.
func (s *Server) editTxPath(runID string) string {
	return filepath.Join(s.pausedDir(), runID+editTxSuffix)
}

// saveEditTx persists one journal state ATOMICALLY (temp + rename) with a
// freshly computed integrity hash.
func (s *Server) saveEditTx(rec *editTxRecord) error {
	// Fault-injection seam: a non-nil error SIMULATES the failed write;
	// nil falls through to the REAL durable write.
	if editTxSaveSeam != nil {
		if err := editTxSaveSeam(rec); err != nil {
			return fmt.Errorf("edit transaction: %w", err)
		}
	}

	dir := s.pausedDir()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("paused runs dir: %w", err)
	}

	rec.UpdatedAt = time.Now().UTC()
	rec.Integrity = editTxIntegrity(rec)

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal edit transaction: %w", err)
	}

	path := s.editTxPath(rec.RunID)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write edit transaction: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit edit transaction: %w", err)
	}

	return nil
}

// loadEditTx reads one journal by run id; ok=false when absent.
func (s *Server) loadEditTx(runID string) (*editTxRecord, bool) {
	if runID == "" {
		return nil, false
	}

	data, err := os.ReadFile(s.editTxPath(runID))
	if err != nil {
		return nil, false
	}

	rec, err := verifyEditTx(data)
	if err != nil {
		return nil, false
	}

	return rec, true
}

// deleteEditTx removes a converged or aborted journal (idempotent).
func (s *Server) deleteEditTx(runID string) {
	if runID == "" {
		return
	}
	_ = os.Remove(s.editTxPath(runID))
}

// quarantineEditTx moves an UNTRUSTWORTHY journal aside (fail closed):
// nothing about that edit is guessed; the file stays on disk for
// actionable recovery, every other session and run stays usable.
func (s *Server) quarantineEditTx(runID string, reason string) {
	src := s.editTxPath(runID)
	dst := filepath.Join(s.pausedDir(), runID+editTxCorruptSuffx)

	if err := os.Rename(src, dst); err != nil {
		// A move failure must not lose the evidence silently: keep the
		// original in place and say exactly where it is.
		logging.Default().Error("run",
			"edit transaction quarantine FAILED — journal kept in place: %s (%v)", src, err)
		return
	}

	logging.Default().Error("run",
		// v1.9.5 LOG CONTRACT: no runId in ordinary logs; the quarantine
		// record path (not an identifier) is the actionable locator.
		"edit transaction QUARANTINED (fail closed): reason=%q — the record is at %s; "+
			"the paused checkpoint and transcript were left untouched, the edit was neither completed nor rolled back",
		reason, dst)
}

// lastUserMessageContent returns the transcript's last user message
// content (the conversation authority's current position).
func (s *Server) lastUserMessageContent(sessionID string) (string, bool) {
	if sessionID == "" || s.store == nil {
		return "", false
	}

	sess, err := s.store.Get(sessionID)
	if err != nil || sess == nil {
		return "", false
	}

	for i := len(sess.Messages) - 1; i >= 0; i-- {
		if sess.Messages[i].Role == "user" {
			return sess.Messages[i].Content, true
		}
	}

	return "", false
}

// RecoverEditTransactions converges every interrupted paused-run edit to
// ONE coherent revision (deterministic, idempotent, fail-closed on
// untrustworthy evidence). Called at startup — before any paused run can
// be listed, edited or resumed — and safe to call again (converged
// journals are deleted).
func (s *Server) RecoverEditTransactions() {
	dir := s.pausedDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no paused dir yet — nothing to recover
	}

	for _, entry := range entries {
		name := entry.Name()
		if len(name) <= len(editTxSuffix) || name[len(name)-len(editTxSuffix):] != editTxSuffix {
			continue
		}

		runID := name[:len(name)-len(editTxSuffix)]

		data, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}

		rec, verr := verifyEditTx(data)
		if verr != nil {
			s.quarantineEditTx(runID, verr.Error())
			continue
		}

		s.recoverOneEditTx(rec)
	}
}

// recoverOneEditTx converges ONE journal from actual durable evidence.
func (s *Server) recoverOneEditTx(rec *editTxRecord) {
	logger := logging.Default()

	ck, ckFound := s.loadPausedRecord(rec.RunID)
	userNow, userFound := s.lastUserMessageContent(rec.SessionID)

	// --- Case 1: the checkpoint is gone (the run was stopped/consumed).
	// The transcript must go back to the pre-edit message; nothing else.
	if !ckFound {
		if rec.UserMessageChanged && userFound {
			switch {
			case userNow == rec.NewUserMessage:
				if _, err := s.store.EditLastUserMessage(rec.SessionID, rec.OldUserMessage); err != nil {
					logger.Error("run",
						"edit recovery could not restore the transcript (checkpoint already consumed): %v",
						err)
					return // leave the journal for the next boot — do not guess
				}
			case userNow != rec.OldUserMessage:
				// Matches neither recorded identity — fail closed.
				s.quarantineEditTx(rec.RunID, fmt.Sprintf(
					"checkpoint consumed and transcript matches neither recorded state (phase %s)", rec.Phase))
				return
			}
		}
		s.deleteEditTx(rec.RunID)
		logger.Info("run", "edit recovery: consumed run %s — journal cleaned, transcript at the pre-edit message", rec.RunID)
		return
	}

	// --- Case 2: decide the target revision from DURABLE evidence.
	if rec.UserMessageChanged {
		switch {
		case userFound && userNow == rec.NewUserMessage:
			// The transcript replace landed: converge FORWARD to the new
			// revision (transcript NEW + checkpoint NEW).
			s.convergeEditTx(rec, ck, rec.NewRevision, rec.NewUserMessage, rec.NewDraft)
			return

		case !userFound || userNow == rec.OldUserMessage:
			// The transcript replace never landed (or was already rolled
			// back): converge BACK to the old revision.
			s.convergeEditTx(rec, ck, rec.OldRevision, rec.OldUserMessage, rec.OldDraft)
			return

		default:
			// The transcript position matches NEITHER recorded identity —
			// the evidence cannot be trusted. Fail closed.
			s.quarantineEditTx(rec.RunID, fmt.Sprintf(
				"transcript user message matches neither the recorded old nor new state (phase %s)", rec.Phase))
			return
		}
	}

	// Draft-only edit: the transcript is untouched by design; the
	// checkpoint revision is the evidence.
	if ck.Revision == rec.NewRevision {
		s.convergeEditTx(rec, ck, rec.NewRevision, "", rec.NewDraft)
		return
	}

	s.convergeEditTx(rec, ck, rec.OldRevision, "", rec.OldDraft)
}

// convergeEditTx forces the checkpoint to exactly `revision` (using the
// journal's recorded state to restore/complete it) and deletes the
// journal. The transcript is already at the target position — the caller
// established that from durable evidence.
func (s *Server) convergeEditTx(rec *editTxRecord, ck *pausedRunRecord, revision int64, userMessage string, draft *string) {
	logger := logging.Default()

	if ck.Revision != revision {
		ck.Revision = revision
	}

	if rec.UserMessageChanged {
		if revision == rec.NewRevision {
			ck.EditedUserMessage = rec.NewUserMessage
		} else {
			// Roll back to exactly the pre-edit checkpoint identity: a
			// previous edit's message stays in EditedUserMessage; a first
			// edit's rollback leaves it empty.
			ck.EditedUserMessage = rec.PrevEditedUserMessage
		}
	}

	if rec.DraftChanged && draft != nil {
		ck.AssistantDraft = *draft
	}

	ck.UpdatedAt = time.Now().UTC()

	if err := s.savePausedRecord(ck); err != nil {
		logger.Error("run",
			"edit recovery could not rewrite the checkpoint: %v — the journal is kept for the next boot",
			err)
		return // keep the journal; the next boot retries deterministically
	}

	s.deleteEditTx(rec.RunID)

	logger.Info("run",
		"edit recovery converged deterministically at revision %d (journal removed)",
		revision)
}
