package sessions

// trash_test.go — v1.11.1 content-integrity milestone: deterministic
// coverage of the recoverable delete/restore/purge lifecycle. Every test
// asserts the integrity invariants the milestone is named for:
//
//   - delete never destroys bytes; it moves them with a tombstone;
//   - the tombstone is metadata only (no message/activity payloads);
//   - restore recovers content AND sidecars, refuses occupied
//     destinations, and verifies by reading before reporting success;
//   - purge is the only permanent destroy, requires the trash state and
//     verifies its own effect;
//   - a crash between the payload move and the tombstone write is
//     reconciled deterministically (nothing vanishes, nothing is guessed);
//   - hostile ids fail closed before any filesystem operation.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
)

func TestDeleteMovesSessionToTrashWithTombstone(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	sess.Title = "recoverable"
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.AppendActivity(sess.ID, ActivityEntry{Type: "note", Caption: "private marker"}); err != nil {
		t.Fatalf("AppendActivity: %v", err)
	}

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The ORIGINAL paths are vacated (the pre-v1.11.1 delete contract
	// holds — nothing resurrects through the active store).
	for _, suffix := range []string{".json", ".activities.jsonl"} {
		path := filepath.Join(store.Dir(), sess.ID+suffix)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("session %s artifact %s still at its live path (stat err=%v)", sess.ID, suffix, err)
		}
	}

	// The payload is RECOVERABLE: trash/<id>/ holds the session file and
	// the tombstone.
	trashFile := filepath.Join(store.Dir(), "trash", sess.ID, sess.ID+".json")
	if _, err := os.Stat(trashFile); err != nil {
		t.Fatalf("deleted session payload must survive under trash/: %v", err)
	}
	tombPath := filepath.Join(store.Dir(), "trash", sess.ID, "tombstone.json")
	data, err := os.ReadFile(tombPath)
	if err != nil {
		t.Fatalf("tombstone missing: %v", err)
	}

	// The tombstone is metadata only — no message or activity payloads.
	if strings.Contains(string(data), "private marker") {
		t.Fatal("tombstone leaked activity payload content")
	}
	var tomb Tombstone
	if err := json.Unmarshal(data, &tomb); err != nil {
		t.Fatalf("tombstone unreadable: %v", err)
	}
	if tomb.ID != sess.ID || tomb.Title != "recoverable" || tomb.Actor != ActorUser {
		t.Fatalf("tombstone = %+v, want id=%s title=%q actor=%s", tomb, sess.ID, "recoverable", ActorUser)
	}

	// The trash listing shows exactly this session.
	tombs, err := store.ListDeleted()
	if err != nil {
		t.Fatalf("ListDeleted: %v", err)
	}
	if len(tombs) != 1 || tombs[0].ID != sess.ID {
		t.Fatalf("ListDeleted = %d entries, want exactly [%s]", len(tombs), sess.ID)
	}
}

func TestRestoreRecoversSessionSidecarsAndIndex(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	sess.Title = "resurrect me"
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.AppendMessage(sess.ID, llm.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if _, err := store.AppendMessage(sess.ID, llm.Message{Role: "assistant", Content: "world"}); err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}
	if err := store.AppendActivity(sess.ID, ActivityEntry{Type: "note", Caption: "sidecar marker"}); err != nil {
		t.Fatalf("AppendActivity: %v", err)
	}
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	restored, err := store.Restore(sess.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.ID != sess.ID {
		t.Fatalf("restored id = %s, want %s", restored.ID, sess.ID)
	}
	if restored.MessageCount() != 2 {
		t.Fatalf("restored message count = %d, want 2 (restore must not lose messages)", restored.MessageCount())
	}

	// Read-after-write: Get serves the restored session; the activity
	// sidecar is back at its live path; the trash is empty.
	got, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get after restore: %v", err)
	}
	if got.Title != "resurrect me" {
		t.Fatalf("restored title = %q", got.Title)
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), sess.ID+".activities.jsonl")); err != nil {
		t.Fatalf("activity sidecar not restored: %v", err)
	}
	tombs, err := store.ListDeleted()
	if err != nil {
		t.Fatalf("ListDeleted after restore: %v", err)
	}
	if len(tombs) != 0 {
		t.Fatalf("trash still holds %d entries after restore", len(tombs))
	}

	// A second restore fails honestly: the session is live, not trashed.
	if _, err := store.Restore(sess.ID); !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("second restore error = %v, want ErrNotTrashed", err)
	}

	// The restored session is fully functional: it accepts new messages.
	if _, err := store.AppendMessage(sess.ID, llm.Message{Role: "user", Content: "after restore"}); err != nil {
		t.Fatalf("AppendMessage after restore: %v", err)
	}
}

func TestRestoreRefusesOccupiedDestination(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	sess.Title = "occupied target"
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Plant a decoy live file at the destination — the crash/tamper shape
	// where the payload got restored but a tombstone also survived.
	decoy := []byte(`{"id":"` + sess.ID + `","title":"decoy"}`)
	if err := os.WriteFile(filepath.Join(store.Dir(), sess.ID+".json"), decoy, 0o644); err != nil {
		t.Fatalf("plant decoy: %v", err)
	}

	_, err := store.Restore(sess.ID)
	if !errors.Is(err, ErrRestoreTargetOccupied) {
		t.Fatalf("restore into occupied destination error = %v, want ErrRestoreTargetOccupied", err)
	}

	// The refusal destroyed nothing: the decoy is untouched and the trash
	// entry survives for a later, clean retry.
	got, rerr := os.ReadFile(filepath.Join(store.Dir(), sess.ID+".json"))
	if rerr != nil || string(got) != string(decoy) {
		t.Fatalf("occupied-destination restore must not touch the live decoy (err=%v)", rerr)
	}
	tombs, terr := store.ListDeleted()
	if terr != nil || len(tombs) != 1 {
		t.Fatalf("trash must keep the recoverable entry after a refused restore (err=%v n=%d)", terr, len(tombs))
	}
}

func TestPurgeDeletedDestroysBytesPermanently(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Purge requires the trash state: a live (never deleted) session id
	// is refused, never silently deleted.
	other := store.CreateInMode(ModeChat)
	if err := store.Save(other); err != nil {
		t.Fatalf("Save other: %v", err)
	}
	if err := store.PurgeDeleted(other.ID); !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("purge of a live session error = %v, want ErrNotTrashed (two-step policy)", err)
	}

	if err := store.PurgeDeleted(sess.ID); err != nil {
		t.Fatalf("PurgeDeleted: %v", err)
	}

	// The bytes are gone — permanently — and the effect is verified.
	if _, err := os.Stat(filepath.Join(store.Dir(), "trash", sess.ID)); !os.IsNotExist(err) {
		t.Fatalf("trash entry still present after purge (stat err=%v)", err)
	}
	tombs, err := store.ListDeleted()
	if err != nil || len(tombs) != 0 {
		t.Fatalf("ListDeleted after purge = %d entries (err=%v)", len(tombs), err)
	}
	if _, err := store.Restore(sess.ID); !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("restore after purge error = %v, want ErrNotTrashed", err)
	}
}

func TestDeletePendingSessionLeavesNoTrashEntry(t *testing.T) {
	store := newTestStore(t)

	pending := store.CreateInMode(ModeChat) // never persisted: no bytes
	if err := store.Delete(pending.ID); err != nil {
		t.Fatalf("Delete pending: %v", err)
	}

	tombs, err := store.ListDeleted()
	if err != nil {
		t.Fatalf("ListDeleted: %v", err)
	}
	if len(tombs) != 0 {
		t.Fatalf("pending session delete must not create a tombstone (n=%d)", len(tombs))
	}
}

func TestTrashReconcileRebuildsMissingTombstoneAfterCrash(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	sess.Title = "crash survivor"
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.AppendMessage(sess.ID, llm.Message{Role: "user", Content: "survive"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Simulate the crash window between the payload move and the
	// tombstone write: remove the marker, leave the payload.
	tombPath := filepath.Join(store.Dir(), "trash", sess.ID, "tombstone.json")
	if err := os.Remove(tombPath); err != nil {
		t.Fatalf("remove tombstone: %v", err)
	}

	// A FRESH store (the restart) must reconcile deterministically: the
	// listing shows the session again with a rebuilt recovery tombstone,
	// and the restore then works end-to-end.
	fresh := New(store.Dir())
	tombs, err := fresh.ListDeleted()
	if err != nil {
		t.Fatalf("fresh ListDeleted: %v", err)
	}
	if len(tombs) != 1 || tombs[0].ID != sess.ID {
		t.Fatalf("reconciled trash = %d entries, want [%s]", len(tombs), sess.ID)
	}
	if tombs[0].Actor != ActorRecovery {
		t.Fatalf("rebuilt tombstone actor = %q, want %q", tombs[0].Actor, ActorRecovery)
	}

	restored, err := fresh.Restore(sess.ID)
	if err != nil {
		t.Fatalf("Restore after crash reconcile: %v", err)
	}
	if restored.MessageCount() != 1 || restored.Title != "crash survivor" {
		t.Fatalf("restored session = %d messages, title %q", restored.MessageCount(), restored.Title)
	}
}

func TestTrashLifecycleAcrossFreshStores(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	sess.Title = "cross-boot"
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The trash is durable state: a brand-new store over the same
	// directory lists it and restores it.
	fresh := New(store.Dir())
	tombs, err := fresh.ListDeleted()
	if err != nil || len(tombs) != 1 {
		t.Fatalf("fresh store lost the trash (err=%v n=%d)", err, len(tombs))
	}
	if _, err := fresh.Restore(sess.ID); err != nil {
		t.Fatalf("fresh Restore: %v", err)
	}

	// Delete → restore → delete again: the lifecycle is repeatable.
	if err := fresh.Delete(sess.ID); err != nil {
		t.Fatalf("re-delete after restore: %v", err)
	}
	tombs2, err := fresh.ListDeleted()
	if err != nil || len(tombs2) != 1 {
		t.Fatalf("re-delete trash listing (err=%v n=%d)", err, len(tombs2))
	}
}

func TestTrashOperationsFailClosedOnHostileIDs(t *testing.T) {
	store := newTestStore(t)

	hostile := []string{
		"../../escape",
		"..\\..\\windows",
		".hidden",
		"a b",
		"x:y",
		"CON",
		"with/slash",
		"",
	}
	for _, id := range hostile {
		if _, err := store.Restore(id); err == nil {
			t.Fatalf("Restore(%q) must fail closed", id)
		}
		if err := store.PurgeDeleted(id); err == nil {
			t.Fatalf("PurgeDeleted(%q) must fail closed", id)
		}
		if err := store.Delete(id); err == nil {
			t.Fatalf("Delete(%q) must fail closed", id)
		}
	}

	// Nothing hostile ever materialized inside or beside the store.
	storeRoot := filepath.Join(store.Dir(), "trash")
	if _, err := os.Stat(storeRoot); err == nil {
		entries, derr := os.ReadDir(storeRoot)
		if derr != nil {
			t.Fatalf("read trash: %v", derr)
		}
		for _, e := range entries {
			if !validSessionID(e.Name()) {
				t.Fatalf("hostile trash entry materialized: %q", e.Name())
			}
		}
	}
}

func TestRestoreUnknownSessionFailsHonestly(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.Restore("s-never-deleted"); !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("restore of a never-deleted session error = %v, want ErrNotTrashed", err)
	}
	if err := store.PurgeDeleted("s-never-deleted"); !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("purge of a never-deleted session error = %v, want ErrNotTrashed", err)
	}
}
