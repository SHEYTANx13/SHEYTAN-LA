package sessions

import (
        "os"
        "path/filepath"
        "testing"
)

// v1.8.3: deterministic coverage of the PENDING-session delete path —
// the class the run-107 browser failure put under the microscope. A new
// empty session stays in the pending map until its first durable write;
// its deletion must clean every authority (pending map, index, file,
// activity sidecar, summary sidecar, hot cache) and the honest
// unknown-session error contract must hold on the repeat.

func TestDeletePendingSessionRemovesEveryAuthority(t *testing.T) {
        store := newTestStore(t)

        // A pending chat session (created, never persisted by a durable write).
        pending := store.CreateInMode(ModeChat)

        // It is visible in the list before the delete.
        list, err := store.List()
        if err != nil {
                t.Fatalf("List before delete: %v", err)
        }

        found := false

        for _, st := range list {
                if st.ID == pending.ID {
                        found = true
                }
        }

        if !found {
                t.Fatal("a pending session must be visible in List before deletion")
        }

        if err := store.Delete(pending.ID); err != nil {
                t.Fatalf("delete of a pending session: %v", err)
        }

        // Gone from the authoritative list.
        list, err = store.List()
        if err != nil {
                t.Fatalf("List after delete: %v", err)
        }

        for _, st := range list {
                if st.ID == pending.ID {
                        t.Fatalf("pending session %s resurrected by List after delete", pending.ID)
                }
        }

        // Gone from the pending map (Get must fail).
        if _, err := store.Get(pending.ID); err == nil {
                t.Fatal("Get of a deleted pending session must fail")
        }

        // The honest unknown-session contract on the repeat.
        if err := store.Delete(pending.ID); err == nil {
                t.Fatal("second delete of a pending session must error")
        }
}

func TestDeletePersistedSessionCleansIndexFileAndSidecars(t *testing.T) {
        store := newTestStore(t)

        sess := store.CreateInMode(ModeChat)
        sess.Title = "persisted"

        if err := store.Save(sess); err != nil {
                t.Fatalf("Save: %v", err)
        }

        // Durable activity + summary sidecars exist for the session.
        if err := store.AppendActivity(sess.ID, ActivityEntry{Type: "note", Caption: "persisted marker"}); err != nil {
                t.Fatalf("AppendActivity: %v", err)
        }

        if err := store.Delete(sess.ID); err != nil {
                t.Fatalf("delete of a persisted session: %v", err)
        }

        // The session file and both sidecars are gone from disk.
        for _, suffix := range []string{".json", ".activities.jsonl"} {
                path := filepath.Join(store.Dir(), sess.ID+suffix)

                if _, err := os.Stat(path); !os.IsNotExist(err) {
                        t.Fatalf("session %s sidecar %s still on disk after delete (stat err=%v)", sess.ID, suffix, err)
                }
        }

        // A FRESH store over the same directory must not resurrect it (the
        // index was rewritten and the file is gone).
        fresh := New(store.Dir())

        list, err := fresh.List()
        if err != nil {
                t.Fatalf("fresh List: %v", err)
        }

        for _, st := range list {
                if st.ID == sess.ID {
                        t.Fatal("deleted persisted session resurrected by a fresh store (stale index)")
                }
        }

        if _, err := fresh.Get(sess.ID); err == nil {
                t.Fatal("fresh Get of a deleted session must fail")
        }
}

func TestDeleteUnknownSessionFailsHonestly(t *testing.T) {
        store := newTestStore(t)

        if err := store.Delete("s-never-existed"); err == nil {
                t.Fatal("deleting an unknown id must fail — never a fake success")
        }
}
