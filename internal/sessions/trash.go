// trash.go — v1.11.1 content-integrity milestone: recoverable session
// deletion.
//
// Contract (docs/architecture/CONTENT-INTEGRITY.md §delete/restore):
// deleting a chat session must NOT be an unrecoverable destroy. Delete
// moves the session file and its sidecars (activities, summary) into
// <dir>/trash/<id>/ together with a tombstone record, removes the session
// from the active index, and stays recoverable until an explicit, separate
// purge. Restore validates the destination is unoccupied before moving
// anything back, re-indexes the session and verifies the restored file by
// reading it before reporting success. PurgeDeleted is the ONLY code path
// in this package that permanently destroys session bytes, and it requires
// the session to already be in the trash — the two-step policy that keeps
// permanent destruction an explicit, deliberate act.
//
// Crash safety (staged/journaled sequence):
//
//  1. mkdir trash/<id>/;
//  2. rename the session file, then sidecars, INTO trash/<id>/;
//  3. write tombstone.json (atomic tmp+rename) — the completion marker;
//  4. drop the index entry, pending copy and hot-cache entry, rewrite
//     the index.
//
// A crash at any earlier point leaves the safe direction: the file either
// still lives at its original path (reconcile folds it back into the
// index) or sits in trash/<id>/ without a tombstone — and
// trashReconcileLocked rebuilds that tombstone from the moved file,
// deterministically, without ever destroying anything.
//
// Containment: every path is built through validSessionID/artifactPath or
// the trash-relative builders below and executed through the store-dir
// os.Root — the same sink-level rooting as every other session artifact
// (path traversal, absolute spellings and symlink escapes are refused at
// the OS layer). The main index reconcile skips directories, so trash/ is
// invisible to List/Get; only the trash API reads it.
package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// trashDirName is the store-subdirectory holding recoverably-deleted
// sessions. reconcileLocked skips directories when scanning the store
// root, so this directory is invisible to the active index by design.
const trashDirName = "trash"

// Tombstone actors.
const (
	ActorUser     = "user"     // the deliberate DELETE via the API
	ActorRecovery = "recovery" // rebuilt by trashReconcileLocked after a crash
)

// Tombstone is the recoverable-delete record for one session: metadata
// only — id, title, mode, size class, who deleted it and when. It never
// carries message payloads, prompts or any private content beyond the
// title the trash listing must show the user.
type Tombstone struct {
	ID           string    `json:"id"`
	Title        string    `json:"title,omitempty"`
	Mode         string    `json:"mode,omitempty"`
	MessageCount int       `json:"messageCount,omitempty"`
	Seq          int64     `json:"seq,omitempty"`
	DeletedAt    time.Time `json:"deletedAt"`
	Actor        string    `json:"actor"`
}

// Typed trash errors — callers must use errors.Is, never string matching.
var (
	// ErrNotTrashed marks an id that has no tombstone: nothing to restore
	// or purge (never a fake success, never a guess).
	ErrNotTrashed = errors.New("session is not in the trash")
	// ErrRestoreTargetOccupied marks a restore whose destination session
	// file already exists — restore NEVER overwrites a live session.
	ErrRestoreTargetOccupied = errors.New("restore target already exists")
	// ErrTrashEntryConflict marks a delete whose trash destination already
	// holds a payload for the same id — fail closed instead of overwriting
	// the earlier recoverable copy.
	ErrTrashEntryConflict = errors.New("trash already holds a session with this id")
)

// Delete removes a session from the active store RECOVERABLY (v1.11.1):
// the file and sidecars move into trash/<id>/ with a tombstone, the index
// entry, pending copy and hot-cache entry go away. Deleting an unknown id
// still fails — callers rely on the second delete erroring. Use Restore to
// undo, PurgeDeleted to destroy permanently.
func (s *Store) Delete(id string) error {
	return s.deleteAs(id, ActorUser)
}

// deleteAs is Delete with the acting principal recorded on the tombstone.
// Caller contract and observable behavior are otherwise identical.
func (s *Store) deleteAs(id, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()

	p, pErr := s.path(id)
	if pErr != nil {
		return pErr // invalid id: fail closed before any filesystem op
	}

	// Build the tombstone from the best available metadata BEFORE any
	// removal: the index stub first, else the file itself (an orphan).
	tomb := Tombstone{ID: id, DeletedAt: time.Now().UTC(), Actor: actor}
	found := false
	for _, st := range s.index {
		if st != nil && st.ID == id {
			tomb.Title = st.Title
			tomb.Mode = st.Mode
			tomb.MessageCount = st.MessageCount()
			tomb.Seq = st.Seq
			found = true
			break
		}
	}

	if _, ok := s.pending[id]; ok {
		delete(s.pending, id)
		found = true // pending sessions carry no bytes: nothing to trash
	}

	// Persisted artifacts move into the trash ONLY when the session file
	// exists on disk.
	if _, statErr := s.storeStat(p); statErr == nil {
		if !found {
			if sess, rerr := s.readLocked(id); rerr == nil {
				tomb.Title = sess.Title
				tomb.Mode = sess.Mode
				tomb.MessageCount = sess.MessageCount()
				tomb.Seq = sess.Seq
			}
		}
		if merr := s.moveToTrashLocked(id, tomb); merr != nil {
			return merr
		}
		found = true
	}

	// Drop the index entry.
	for i, st := range s.index {
		if st != nil && st.ID == id {
			s.index = append(s.index[:i], s.index[i+1:]...)
			break
		}
	}

	delete(s.hot, id)

	if !found {
		return fmt.Errorf("session %s not found", id)
	}
	return s.saveIndexLocked()
}

// Restore recovers a recoverably-deleted session: the tombstone must exist
// (a restore never guesses), the destination must be unoccupied (a live
// session is NEVER overwritten), and the restored file is re-read and
// re-indexed before success is reported. On any failure the safe direction
// holds: the trash entry is untouched unless the payload is already back.
func (s *Store) Restore(id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	s.trashReconcileLocked()

	if !validSessionID(id) {
		return nil, fmt.Errorf("sessions: %w %q", errInvalidSessionID, id)
	}

	root, err := s.storeRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	// The tombstone is the authority that this id was deliberately deleted.
	tombRel := trashRel(id, "tombstone.json")
	if _, err := root.Stat(tombRel); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotTrashed, id)
	}

	// Destination safety: never overwrite a live session.
	dst, derr := s.path(id)
	if derr != nil {
		return nil, derr
	}
	if _, err := s.storeStat(dst); err == nil {
		return nil, fmt.Errorf("%w: session %s", ErrRestoreTargetOccupied, id)
	}

	// Move the payload back first: a crash mid-restore leaves the file at
	// its live path (reconcile folds it back into the index) with the
	// tombstone still in the trash — recoverable in both directions.
	if err := root.Rename(trashRel(id, id+".json"), filepath.Base(dst)); err != nil {
		return nil, fmt.Errorf("restore session %s: %w", id, err)
	}
	for _, suffix := range []string{".activities.jsonl", ".summary.json"} {
		rel := trashRel(id, id+suffix)
		if _, err := root.Stat(rel); err == nil {
			if rerr := root.Rename(rel, id+suffix); rerr != nil {
				return nil, fmt.Errorf("restore session %s sidecar %s: %w", id, suffix, rerr)
			}
		}
	}

	// The tombstone directory goes away only after payload and sidecars
	// are back at their live paths.
	if err := root.RemoveAll(trashDir(id)); err != nil {
		return nil, fmt.Errorf("restore session %s cleanup: %w", id, err)
	}
	delete(s.hot, id)

	// Read-after-write verification before reporting success: the restored
	// file must parse and carry the session's identity.
	sess, err := s.readLocked(id)
	if err != nil {
		return nil, fmt.Errorf("restore verify session %s: %w", id, err)
	}
	s.upsertIndexLocked(sess)
	s.sortIndexLocked()
	if err := s.saveIndexLocked(); err != nil {
		return nil, err
	}
	return copySession(sess), nil
}

// PurgeDeleted PERMANENTLY destroys a recoverably-deleted session's bytes
// (file, sidecars, tombstone). It is the only permanent-destroy path in
// this package and requires the session to already be in the trash — the
// two-step delete→purge policy. After a purge the session is gone for
// good; Restore then fails with ErrNotTrashed.
func (s *Store) PurgeDeleted(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	s.trashReconcileLocked()

	if !validSessionID(id) {
		return fmt.Errorf("sessions: %w %q", errInvalidSessionID, id)
	}

	root, err := s.storeRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	if _, err := root.Stat(trashRel(id, "tombstone.json")); err != nil {
		return fmt.Errorf("%w: %s", ErrNotTrashed, id)
	}
	if err := root.RemoveAll(trashDir(id)); err != nil {
		return fmt.Errorf("purge session %s: %w", id, err)
	}
	// Verify the destruction before reporting success.
	if _, err := root.Stat(trashDir(id)); !os.IsNotExist(err) {
		return fmt.Errorf("purge session %s: trash entry still present", id)
	}
	return nil
}

// ListDeleted returns the tombstones of recoverably-deleted sessions,
// newest first. It performs the crash-recovery reconcile first, so a
// delete interrupted between the file move and the tombstone write shows
// up with a rebuilt (actor=recovery) tombstone instead of vanishing.
func (s *Store) ListDeleted() ([]*Tombstone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	s.trashReconcileLocked()

	root, err := s.storeRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	f, err := root.Open(trashDirName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // nothing ever deleted — an honest empty list
		}
		return nil, err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}

	out := make([]*Tombstone, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !validSessionID(id) {
			continue // hostile or foreign name: never surfaced, never touched
		}
		data, rerr := root.ReadFile(trashRel(id, "tombstone.json"))
		if rerr != nil {
			continue // reconcile just rebuilt rebuildable ones
		}
		var tomb Tombstone
		if jerr := json.Unmarshal(data, &tomb); jerr != nil || tomb.ID != id {
			continue
		}
		out = append(out, &tomb)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeletedAt.Equal(out[j].DeletedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].DeletedAt.After(out[j].DeletedAt)
	})
	return out, nil
}

// --- internal helpers -------------------------------------------------------

// trashDir is the root-relative directory of one trashed session.
func trashDir(id string) string {
	return filepath.Join(trashDirName, id)
}

// trashRel is the root-relative path of one file inside a trashed session's
// directory. The id is validated (the same single-component grammar as
// every session artifact) and the joined name re-verified as a local
// relative path — a hostile id can never escape the store root.
func trashRel(id, name string) string {
	if !validSessionID(id) {
		return filepath.Join(trashDirName, "__invalid__", name)
	}
	rel := filepath.Join(trashDirName, id, name)
	if !filepath.IsLocal(rel) {
		return filepath.Join(trashDirName, "__invalid__", name)
	}
	return rel
}

// moveToTrashLocked moves the session file and its sidecars into
// trash/<id>/ and writes the tombstone last (the completion marker).
// Caller holds mu.
func (s *Store) moveToTrashLocked(id string, tomb Tombstone) error {
	root, err := s.storeRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	if err := root.MkdirAll(trashDirName, 0o755); err != nil {
		return err
	}
	if err := root.Mkdir(trashDir(id), 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	// Fail closed when the trash destination already holds a payload for
	// this id — the earlier recoverable copy is never overwritten.
	if _, err := root.Stat(trashRel(id, id+".json")); err == nil {
		return fmt.Errorf("%w: %s", ErrTrashEntryConflict, id)
	}

	for _, suffix := range []string{".json", ".activities.jsonl", ".summary.json"} {
		p, pErr := s.artifactPath(id, suffix)
		if pErr != nil {
			return pErr
		}
		if _, statErr := s.storeStat(p); statErr != nil {
			continue // sidecar absent: moving what exists is correct
		}
		if err := root.Rename(filepath.Base(p), trashRel(id, filepath.Base(p))); err != nil {
			return fmt.Errorf("move session %s artifact %s to trash: %w", id, suffix, err)
		}
	}
	return s.writeTombstoneLocked(root, id, tomb)
}

// writeTombstoneLocked persists the tombstone atomically (tmp+rename).
// Caller holds mu.
func (s *Store) writeTombstoneLocked(root *os.Root, id string, tomb Tombstone) error {
	if tomb.ID == "" {
		tomb.ID = id
	}
	if tomb.DeletedAt.IsZero() {
		tomb.DeletedAt = time.Now().UTC()
	}
	if tomb.Actor == "" {
		tomb.Actor = ActorRecovery
	}
	data, err := json.MarshalIndent(tomb, "", "  ")
	if err != nil {
		return err
	}
	rel := trashRel(id, "tombstone.json")
	tmp := rel + ".tmp"
	if err := root.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return root.Rename(tmp, rel)
}

// trashReconcileLocked rebuilds missing tombstones for trashed sessions
// whose payload arrived but whose tombstone write never did (a crash
// between the file move and the marker). Additive only: it never deletes
// or rewrites anything that already exists, so it is idempotent and safe
// to run on every trash operation. Caller holds mu.
func (s *Store) trashReconcileLocked() {
	root, err := s.storeRoot()
	if err != nil {
		return
	}
	defer root.Close()

	f, err := root.Open(trashDirName)
	if err != nil {
		return // no trash yet — nothing to reconcile
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !validSessionID(id) {
			continue
		}
		if _, err := root.Stat(trashRel(id, "tombstone.json")); err == nil {
			continue // marker present: nothing to rebuild
		}
		data, rerr := root.ReadFile(trashRel(id, id+".json"))
		if rerr != nil {
			continue // no payload either: nothing to describe
		}
		var sess Session
		if jerr := json.Unmarshal(data, &sess); jerr != nil {
			continue // unreadable payload: leave untouched, listed only if a marker exists
		}
		deletedAt := time.Now().UTC()
		if fi, ierr := e.Info(); ierr == nil && !fi.ModTime().IsZero() {
			deletedAt = fi.ModTime().UTC()
		}
		tomb := Tombstone{
			ID:           id,
			Title:        sess.Title,
			Mode:         sess.Mode,
			MessageCount: sess.MessageCount(),
			Seq:          sess.Seq,
			DeletedAt:    deletedAt,
			Actor:        ActorRecovery,
		}
		// Best-effort rebuild: a failure surfaces on ListDeleted/Restore
		// through the missing-marker path; nothing is destroyed.
		_ = s.writeTombstoneLocked(root, id, tomb)
	}
}
