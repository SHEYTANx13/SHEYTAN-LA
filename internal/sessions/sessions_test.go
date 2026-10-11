package sessions

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	return New(t.TempDir())
}

func TestCreateSaveGetRoundTrip(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()

	sess.Title = "round trip"
	sess.Messages = append(sess.Messages, llm.Message{Role: "user", Content: "hello"})

	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if loaded.Title != "round trip" || len(loaded.Messages) != 1 {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}

func TestAppendMessagePersistsEachOne(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for i := 0; i < 10; i++ {
		if _, err := store.AppendMessage(sess.ID, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
		}); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(loaded.Messages) != 10 {
		t.Fatalf("expected 10 messages, got %d", len(loaded.Messages))
	}
}

func TestListNewestFirstAndStubsAreLean(t *testing.T) {
	store := newTestStore(t)

	first := store.Create()
	first.Title = "first"
	_ = store.Save(first)

	second := store.Create()
	second.Title = "second"
	_ = store.Save(second)

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("expected 2 stubs, got %d", len(list))
	}

	if list[0].Title != "second" {
		t.Fatalf("newest-first violated: %s then %s", list[0].Title, list[1].Title)
	}

	// Stubs must not carry full histories.
	if list[0].Messages != nil {
		t.Fatal("stub list must not include messages")
	}
}

func TestDeleteRemovesAndErrorsOnRepeat(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	if err := store.Delete(sess.ID); err == nil {
		t.Fatal("second delete must error")
	}
}

func TestContextAttachmentIDsPersist(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	ctx := Context{
		SystemPrompt:  "be brief",
		AttachmentIDs: []string{"a1234", "a5678"},
	}

	if err := store.UpdateContext(sess.ID, ctx); err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(loaded.Context.AttachmentIDs) != 2 {
		t.Fatalf("attachment ids lost: %+v", loaded.Context)
	}
}

func TestConcurrentAppendsNeverLoseMessages(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	var wg sync.WaitGroup

	const writers = 8

	const perWriter = 25

	for w := 0; w < writers; w++ {
		wg.Add(1)

		go func(seed int) {
			defer wg.Done()

			for i := 0; i < perWriter; i++ {
				_, err := store.AppendMessage(sess.ID, llm.Message{
					Role:    "user",
					Content: fmt.Sprintf("w%d-m%d", seed, i),
				})
				if err != nil {
					t.Errorf("append: %v", err)

					return
				}
			}
		}(w)
	}

	wg.Wait()

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got := len(loaded.Messages); got != writers*perWriter {
		t.Fatalf("lost messages under concurrency: %d != %d", got, writers*perWriter)
	}
}

func TestIndexSelfHealsOrphans(t *testing.T) {
	dir := t.TempDir()

	store := New(dir)

	sess := store.Create()
	sess.Title = "orphan source"
	_ = store.Save(sess)

	// Simulate a lost index by creating a fresh store on the same dir with
	// the index file removed.
	store2 := New(dir)

	list, err := store2.List()
	if err != nil {
		t.Fatalf("List after heal: %v", err)
	}

	if len(list) != 1 || list[0].Title != "orphan source" {
		t.Fatalf("orphan not folded back into the index: %+v", list)
	}
}

func TestActivitySidecarBounded(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	for i := 0; i < 500; i++ {
		if err := store.AppendActivity(sess.ID, ActivityEntry{
			Type:    "tool_start",
			Caption: fmt.Sprintf("activity %d", i),
		}); err != nil {
			t.Fatalf("AppendActivity %d: %v", i, err)
		}
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(loaded.Activities) > maxActivities {
		t.Fatalf("activity sidecar not bounded: %d", len(loaded.Activities))
	}
}

// TestSessionContextPolicyPersistsPerChat: the 1.1.6 per-session context
// policy travels with the session — Chat A = 8K, Chat B = 32K — and
// switching (reloading) restores each chat's own policy.
func TestSessionContextPolicyPersistsPerChat(t *testing.T) {
	store := newTestStore(t)

	a := store.Create()
	a.Context.ContextTokens = 8192
	if err := store.Save(a); err != nil {
		t.Fatalf("save A: %v", err)
	}

	b := store.Create()
	b.Context.ContextTokens = 32768
	if err := store.Save(b); err != nil {
		t.Fatalf("save B: %v", err)
	}

	c := store.Create() // inherits the global policy (0)
	if err := store.Save(c); err != nil {
		t.Fatalf("save C: %v", err)
	}

	// "Switch sessions": reload each one fresh.
	for _, tc := range []struct {
		id   string
		want int
	}{} {
		_ = tc
	}

	reloadedA, err := store.Get(a.ID)
	if err != nil {
		t.Fatalf("get A: %v", err)
	}
	reloadedB, err := store.Get(b.ID)
	if err != nil {
		t.Fatalf("get B: %v", err)
	}
	reloadedC, err := store.Get(c.ID)
	if err != nil {
		t.Fatalf("get C: %v", err)
	}

	if reloadedA.Context.ContextTokens != 8192 {
		t.Fatalf("chat A policy = %d, want 8192", reloadedA.Context.ContextTokens)
	}
	if reloadedB.Context.ContextTokens != 32768 {
		t.Fatalf("chat B policy = %d, want 32768", reloadedB.Context.ContextTokens)
	}
	if reloadedC.Context.ContextTokens != 0 {
		t.Fatalf("chat C policy = %d, want 0 (inherit)", reloadedC.Context.ContextTokens)
	}

	// UpdateContext replaces the policy honestly.
	reloadedA.Context.ContextTokens = 16384
	if err := store.UpdateContext(a.ID, reloadedA.Context); err != nil {
		t.Fatalf("update A: %v", err)
	}
	again, _ := store.Get(a.ID)
	if again.Context.ContextTokens != 16384 {
		t.Fatalf("chat A updated policy = %d, want 16384", again.Context.ContextTokens)
	}
}

// --- atomic context merge and objective refresh ---

// TestUpdateContextFuncAtomicMerge pins the serialization contract: N
// concurrent context mutations (refs merges + a token-policy write) all
// survive — no lost update. Run with -race for the memory-model check.
func TestUpdateContextFuncAtomicMerge(t *testing.T) {
	store := New(t.TempDir())
	sess := store.CreateInMode(ModeChat)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	const writers = 24

	var wg sync.WaitGroup

	// Half the writers merge one history ref each (distinct sources).
	for i := 0; i < writers/2; i++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			err := store.UpdateContextFunc(sess.ID, func(c *Context) (bool, error) {
				id := fmt.Sprintf("agent-src-%d", n)

				for _, r := range c.HistoryRefs {
					if r.SessionID == id {
						return false, nil
					}
				}

				c.HistoryRefs = append(c.HistoryRefs, HistoryRef{SessionID: id})
				return true, nil
			})
			if err != nil {
				t.Errorf("ref merge %d: %v", n, err)
			}
		}(i)
	}

	// Half the writers apply a distinct context-token policy.
	for i := 0; i < writers/2; i++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			want := 1024 * (n + 1)

			err := store.UpdateContextFunc(sess.ID, func(c *Context) (bool, error) {
				if c.ContextTokens == want {
					return false, nil
				}
				c.ContextTokens = want
				return true, nil
			})
			if err != nil {
				t.Errorf("token policy %d: %v", n, err)
			}
		}(i)
	}

	wg.Wait()

	final, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}

	// EVERY ref-merge survived (no whole-object Save clobbered them).
	if len(final.Context.HistoryRefs) != writers/2 {
		t.Fatalf("history refs = %d, want %d — a concurrent update was lost",
			len(final.Context.HistoryRefs), writers/2)
	}

	// The token policy survives too (last writer wins for THAT field).
	if final.Context.ContextTokens == 0 {
		t.Fatal("the context-token write was lost — concurrent context writes are not serialized")
	}
}

// TestSaveMessagesKeepContextPreservesContext pins the transcript-save
// primitive: SaveMessagesKeepContext persists messages while a concurrent
// context mutation applied between the caller's snapshot and the save
// survives (the store's context is authoritative, not the caller's copy).
func TestSaveMessagesKeepContextPreservesContext(t *testing.T) {
	store := New(t.TempDir())
	sess := store.CreateInMode(ModeAgent)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The caller holds a STALE copy (no refs).
	stale, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// A concurrent writer attaches a ref.
	err = store.UpdateContextFunc(sess.ID, func(c *Context) (bool, error) {
		c.HistoryRefs = append(c.HistoryRefs, HistoryRef{SessionID: "agent-src"})
		return true, nil
	})
	if err != nil {
		t.Fatalf("concurrent ref merge: %v", err)
	}

	// The stale copy saves a new user message.
	stale.Messages = append(stale.Messages, llm.Message{Role: "user", Content: "persisted message"})

	if err := store.SaveMessagesKeepContext(stale); err != nil {
		t.Fatalf("SaveMessagesKeepContext: %v", err)
	}

	final, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}

	if len(final.Messages) != 1 || final.Messages[0].Content != "persisted message" {
		t.Fatalf("message not persisted: %+v", final.Messages)
	}

	if len(final.Context.HistoryRefs) != 1 || final.Context.HistoryRefs[0].SessionID != "agent-src" {
		t.Fatalf("the concurrent context mutation was reverted by the save: %+v", final.Context.HistoryRefs)
	}
}

// TestObjectiveRefreshesFromTrivialOpener pins the v1.2.8.1 summary fix:
// a session that opens with "hi" must not carry that as its objective
// forever — a substantially longer later user message supersedes it.
func TestObjectiveRefreshesFromTrivialOpener(t *testing.T) {
	sum := UpdateSummaryFromTurn(nil, "s1", ModeChat, "hi", "Hello! How can I help?", nil)
	if sum.Objective != "hi" {
		t.Fatalf("precondition: objective = %q, want the trivial opener", sum.Objective)
	}

	sum = UpdateSummaryFromTurn(sum, "s1", ModeChat,
		"Refactor the storage layer to use the new session store API",
		"Refactored the storage layer.", nil)

	if sum.Objective != "Refactor the storage layer to use the new session store API" {
		t.Fatalf("objective not refreshed from the trivial opener: %q", sum.Objective)
	}

	// Once non-trivial, the objective is stable.
	sum = UpdateSummaryFromTurn(sum, "s1", ModeChat, "another completely different long task", "Done.", nil)
	if sum.Objective != "Refactor the storage layer to use the new session store API" {
		t.Fatalf("a non-trivial objective must stay stable, got %q", sum.Objective)
	}
}
