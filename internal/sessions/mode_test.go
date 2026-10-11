// mode_test.go — v1.2.8 session mode identity + deterministic migration.
package sessions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// TestLegacySessionMigratesToAgentMode pins the deterministic migration
// rule: a session file that predates the mode field (mode == "") is
// labeled DefaultMode ("agent") on load, WITHOUT rewriting the on-disk
// bytes and WITHOUT duplicating conversation content.
func TestLegacySessionMigratesToAgentMode(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	// Create + persist a session, then STRIP the mode field from the file
	// to simulate a pre-v1.2.8 session.
	sess := store.Create()
	sess.Title = "legacy conversation"
	sess.Messages = append(sess.Messages, llm.Message{Role: "user", Content: "hello from the past"})
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(dir, sess.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	rewritten := string(data)
	if !contains(rewritten, `"id":"`) {
		t.Fatalf("unexpected session file shape: %s", rewritten)
	}

	// A session saved through CreateInMode carries mode; write it back
	// without one.
	sess.Mode = ""
	if err := store.Save(sess); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("restore legacy bytes: %v", err)
	}

	// Fresh store over the same dir: the legacy file must migrate.
	reopened := New(dir)
	loaded, err := reopened.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}

	if loaded.Mode != ModeAgent {
		t.Fatalf("legacy session mode = %q, want %q", loaded.Mode, ModeAgent)
	}

	// NO content duplication: exactly one user message.
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello from the past" {
		t.Fatalf("migration duplicated or lost content: %+v", loaded.Messages)
	}

	// The on-disk bytes stay untouched until the next natural save.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after load: %v", err)
	}
	if string(after) != string(data) {
		t.Fatal("migration rewrote the session file on load — must be lazy")
	}
}

// TestLegacyIndexStubsMigrateToAgentMode covers the index path: legacy
// index.json stubs (no mode) get DefaultMode so mode filtering works
// before any re-save.
func TestLegacyIndexStubsMigrateToAgentMode(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	sess := store.CreateInMode(ModeAgent)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Rewrite the index without mode fields (legacy shape).
	indexPath := filepath.Join(dir, "index.json")
	if err := os.WriteFile(indexPath, []byte(`[{"id":"`+sess.ID+`","title":"legacy stub"}]`), 0o644); err != nil {
		t.Fatalf("write legacy index: %v", err)
	}

	reopened := New(dir)
	list, err := reopened.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("List returned %d stubs, want 1", len(list))
	}

	if list[0].Mode != ModeAgent {
		t.Fatalf("legacy stub mode = %q, want %q", list[0].Mode, ModeAgent)
	}
}

// TestCreateInModeAndModeFilter pins creation + listing semantics: the
// mode is fixed at creation, ListByMode-style filtering keeps the spaces
// separate, and an empty mode resolves to DefaultMode.
func TestCreateInModeAndModeFilter(t *testing.T) {
	store := newTestStore(t)

	chat := store.CreateInMode(ModeChat)
	agentSess := store.CreateInMode(ModeAgent)
	fallback := store.CreateInMode("nonsense")

	if chat.Mode != ModeChat {
		t.Fatalf("chat session mode = %q", chat.Mode)
	}
	if agentSess.Mode != ModeAgent {
		t.Fatalf("agent session mode = %q", agentSess.Mode)
	}
	if fallback.Mode != DefaultMode {
		t.Fatalf("unknown mode resolved to %q, want %q", fallback.Mode, DefaultMode)
	}

	for _, s := range []*Session{chat, agentSess} {
		if err := store.Save(s); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	seen := map[string]map[string]bool{}
	for _, st := range list {
		if seen[st.Mode] == nil {
			seen[st.Mode] = map[string]bool{}
		}
		seen[st.Mode][st.ID] = true
	}

	if len(seen[ModeChat]) != 1 || !seen[ModeChat][chat.ID] {
		t.Fatalf("chat space wrong: %v", seen)
	}
	if len(seen[ModeAgent]) != 2 || !seen[ModeAgent][agentSess.ID] {
		t.Fatalf("agent space wrong: %v", seen)
	}
}

// TestModeChangeDoesNotDuplicateContent pins the no-duplication rule:
// re-saving a session (e.g. after the mode migration materializes) keeps
// exactly the same message count — migration is a LABEL, never a copy.
func TestModeChangeDoesNotDuplicateContent(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeChat)
	for i := 0; i < 5; i++ {
		if _, err := store.AppendMessage(sess.ID, llm.Message{Role: "user", Content: "turn"}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	loaded.Mode = ModeAgent
	if err := store.Save(loaded); err != nil {
		t.Fatalf("Save after mode materialization: %v", err)
	}

	again, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get after save: %v", err)
	}

	if len(again.Messages) != 5 {
		t.Fatalf("message count changed on mode materialization: %d", len(again.Messages))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
