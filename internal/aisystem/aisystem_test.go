package aisystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openFresh(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func TestFreshInstallHasDefaultSystemActive(t *testing.T) {
	st := openFresh(t)

	sys, err := st.Active()
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if sys.SystemID != ReservedID {
		t.Fatalf("fresh install must activate the default system, got %q", sys.SystemID)
	}
	if sys.Instructions != "" || sys.Model != "" || sys.Reasoning != "" {
		t.Fatalf("default system must carry no overrides (pre-v1.9 behavior)")
	}
	if sys.ApprovalPolicy != ApprovalAskRisky {
		t.Fatalf("default approval policy = ask-risky, got %q", sys.ApprovalPolicy)
	}
	list, err := st.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("fresh store must hold exactly one system, got %d", len(list))
	}
}

func TestReloadPersistsActiveSelection(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	created, err := st.Create(System{Name: "Cautious Engineer", Instructions: "Always verify before claiming success.", Reasoning: ReasoningHigh})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Revision != 1 || created.SystemID == "" {
		t.Fatalf("created system must have revision 1 and an id")
	}
	if _, err := st.Select(created.SystemID); err != nil {
		t.Fatalf("select: %v", err)
	}

	// RELOAD — a brand-new store over the same directory.
	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	sys, err := st2.Active()
	if err != nil {
		t.Fatalf("active after reload: %v", err)
	}
	if sys.SystemID != created.SystemID {
		t.Fatalf("reload must restore the active selection: got %q want %q", sys.SystemID, created.SystemID)
	}
	if sys.Reasoning != ReasoningHigh {
		t.Fatalf("reasoning preference lost on reload")
	}
}

func TestUpdateIncrementsRevisionAndFreezeIsolation(t *testing.T) {
	st := openFresh(t)
	created, err := st.Create(System{Name: "Rigger", Instructions: "v1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	snap, err := st.SnapshotOf(created.SystemID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	updated, err := st.Update(created.SystemID, func(s *System) error {
		s.Instructions = "v2 with more care"
		s.AllowedTools = []string{"files", "shell"}
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("revision must increment: got %d", updated.Revision)
	}

	// THE FROZEN SNAPSHOT CONTRACT: the pre-update snapshot is never
	// mutated by the update (an in-flight run keeps its bound config).
	if snap.SystemRevision != 1 || snap.Instructions != "v1" {
		t.Fatalf("frozen snapshot mutated by a later update: %+v", snap)
	}
	snap2, _ := st.SnapshotOf(created.SystemID)
	if snap2.SystemRevision != 2 || len(snap2.AllowedTools) != 2 {
		t.Fatalf("new snapshot must reflect revision 2 + tool surface")
	}
}

func TestCloneExportImportRoundTrip(t *testing.T) {
	st := openFresh(t)
	created, err := st.Create(System{
		Name:           "Researcher",
		Instructions:   "Cite evidence.",
		AllowedTools:   []string{"research", "fetch"},
		ApprovalPolicy: ApprovalAuto,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	clone, err := st.Clone(created.SystemID)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if clone.SystemID == created.SystemID {
		t.Fatalf("clone must have a fresh identity")
	}
	if clone.Name != "Researcher (copy)" {
		t.Fatalf("clone name = %q", clone.Name)
	}
	if clone.AllowedTools[0] != "research" {
		t.Fatalf("clone lost its tool surface")
	}

	exported, err := st.Export(created.SystemID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	imported, err := st.Import(exported)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.SystemID == created.SystemID {
		t.Fatalf("import must mint a fresh identity (never overwrite)")
	}
	if imported.Instructions != "Cite evidence." || imported.ApprovalPolicy != ApprovalAuto {
		t.Fatalf("import lost payload: %+v", imported)
	}
	if imported.Revision != 1 {
		t.Fatalf("imported revision must reset to 1")
	}
}

func TestImportRejectsGarbage(t *testing.T) {
	st := openFresh(t)
	if _, err := st.Import([]byte("this is not json")); err == nil {
		t.Fatalf("garbage import must fail")
	}
}

func TestDeleteActiveFallsBackToDefault(t *testing.T) {
	st := openFresh(t)
	created, err := st.Create(System{Name: "Temp System"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.Select(created.SystemID); err != nil {
		t.Fatalf("select: %v", err)
	}
	if err := st.Delete(created.SystemID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	sys, err := st.Active()
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if sys.SystemID != ReservedID {
		t.Fatalf("deleting the active system must fall back to default, got %q", sys.SystemID)
	}
}

func TestDefaultSystemIsReserved(t *testing.T) {
	st := openFresh(t)
	if err := st.Delete(ReservedID); err != ErrReserved {
		t.Fatalf("deleting the default system must be refused, got %v", err)
	}
}

func TestValidationBoundsAndUnknownVocabularies(t *testing.T) {
	st := openFresh(t)

	if _, err := st.Create(System{Name: "  "}); err == nil {
		t.Fatalf("empty name must be rejected")
	}
	if _, err := st.Create(System{Name: strings.Repeat("x", MaxNameLen+1)}); err == nil {
		t.Fatalf("over-long name must be rejected")
	}
	if _, err := st.Create(System{Name: "Bad", VerificationPolicy: "yolo"}); err == nil {
		t.Fatalf("unknown verification policy must be rejected")
	}

	created, err := st.Create(System{Name: "Odd Vocab", Reasoning: "turbo", ApprovalPolicy: "whatever"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Reasoning != ReasoningUnspec {
		t.Fatalf("unknown reasoning must normalize to the runtime default, got %q", created.Reasoning)
	}
	if created.ApprovalPolicy != ApprovalAskRisky {
		t.Fatalf("unknown approval must normalize to the safe default, got %q", created.ApprovalPolicy)
	}
}

func TestCorruptDocumentIsToleratedByList(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := st.Create(System{Name: "Healthy"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Corrupt an unknown extra document on disk.
	bad := filepath.Join(dir, "ai-systems", "sys-broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	list, err := st.List()
	if err != nil {
		t.Fatalf("list must tolerate one corrupt document: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected default + healthy system, got %d", len(list))
	}
}

func TestDeterministicOrdering(t *testing.T) {
	st := openFresh(t)
	for _, name := range []string{"Zulu", "Alpha", "Mike"} {
		if _, err := st.Create(System{Name: name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	list, _ := st.List()
	// Default (earliest CreatedAt) first; ties broken by SystemID.
	for i := 1; i < len(list); i++ {
		a, b := list[i-1], list[i]
		if a.CreatedAt.After(b.CreatedAt) ||
			(a.CreatedAt.Equal(b.CreatedAt) && a.SystemID > b.SystemID) {
			t.Fatalf("list ordering is not deterministic at %d", i)
		}
	}
}

func TestActivePointerRepairOnDanglingTarget(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	created, _ := st.Create(System{Name: "Doomed"})
	if _, err := st.Select(created.SystemID); err != nil {
		t.Fatalf("select: %v", err)
	}
	// The document vanishes behind the store's back (user deleted the file).
	os.Remove(filepath.Join(dir, "ai-systems", created.SystemID+".json"))

	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	sys, err := st2.Active()
	if err != nil {
		t.Fatalf("active after dangling pointer: %v", err)
	}
	if sys.SystemID != ReservedID {
		t.Fatalf("dangling active pointer must repair to default, got %q", sys.SystemID)
	}
}

func TestSnapshotJSONCarriesSystemRevision(t *testing.T) {
	st := openFresh(t)
	created, _ := st.Create(System{Name: "Json Shape", Instructions: "x"})
	snap, _ := st.SnapshotOf(created.SystemID)
	data, _ := json.Marshal(snap)
	if !strings.Contains(string(data), `"systemRevision":1`) ||
		!strings.Contains(string(data), `"systemId":"`) {
		t.Fatalf("snapshot JSON must expose systemId + systemRevision: %s", data)
	}
}
