// summary_test.go — v1.2.8 rolling session summary tests.
package sessions

import (
	"strings"
	"testing"
)

// TestUpdateSummaryFromTurnDeterministicAndRolling pins the core contract:
// the update is deterministic, the previous summary is the BASE (rolling —
// the transcript is not re-summarized), and the version increments.
func TestUpdateSummaryFromTurnDeterministicAndRolling(t *testing.T) {
	turn1 := UpdateSummaryFromTurn(nil, "s1", ModeChat,
		"I must deploy the build script only on Linux. How do we package the release?",
		"Decided to use the ./scripts/build-and-zip.sh pipeline. The artifact lands in download/.",
		[]string{"files"})

	if turn1.SessionID != "s1" || turn1.Mode != ModeChat {
		t.Fatalf("identity not carried: %+v", turn1)
	}

	if turn1.Version != 1 {
		t.Fatalf("first update version = %d, want 1", turn1.Version)
	}

	if turn1.Objective == "" {
		t.Fatal("objective empty after the first turn")
	}

	if len(turn1.ImportantUserConstraints) == 0 {
		t.Fatal("constraint extraction found nothing for 'must ... only'")
	}

	if len(turn1.KeyDecisions) == 0 {
		t.Fatal("decision extraction found nothing for 'Decided to'")
	}

	// Deterministic: identical inputs → identical output.
	again := UpdateSummaryFromTurn(nil, "s1", ModeChat,
		"I must deploy the build script only on Linux. How do we package the release?",
		"Decided to use the ./scripts/build-and-zip.sh pipeline. The artifact lands in download/.",
		[]string{"files"})

	if again.Objective != turn1.Objective ||
		strings.Join(again.KeyDecisions, "|") != strings.Join(turn1.KeyDecisions, "|") ||
		strings.Join(again.ImportantUserConstraints, "|") != strings.Join(turn1.ImportantUserConstraints, "|") {
		t.Fatal("update is not deterministic")
	}

	// Rolling: turn 2 builds ON turn 1 (objective preserved, version bumps).
	turn2 := UpdateSummaryFromTurn(turn1, "s1", ModeChat,
		"Windows support is a must not now. Ship the zip.",
		"The zip passed verification. Next step: push the tag.",
		nil)

	if turn2.Version != 2 {
		t.Fatalf("rolling version = %d, want 2", turn2.Version)
	}

	if turn2.Objective != turn1.Objective {
		t.Fatal("rolling update lost the original objective")
	}

	if turn2.CurrentState == "" {
		t.Fatal("current state not rolled forward")
	}
}

// TestSummaryCapsBounded pins the bounds: lists never exceed
// maxSummaryItems entries and long lines are clipped.
func TestSummaryCapsBounded(t *testing.T) {
	long := strings.Repeat("x", 500)

	sum := &SessionSummary{}
	for i := 0; i < 40; i++ {
		sum = UpdateSummaryFromTurn(sum, "s2", ModeAgent,
			"Turn must fix "+long+" number "+strings.Repeat("y", i+1),
			"Fixed the error by repairing the module. Next step: verify.",
			nil)
	}

	check := func(name string, items []string) {
		t.Helper()
		if len(items) > maxSummaryItems {
			t.Fatalf("%s grew to %d items, cap is %d", name, len(items), maxSummaryItems)
		}
	}

	check("constraints", sum.ImportantUserConstraints)
	check("errors", sum.ErrorsAndRepairs)
	check("facts", sum.ImportantFacts)

	for _, item := range sum.ImportantUserConstraints {
		if len(item) > maxSummaryItemChars+3 { // clip adds an ellipsis rune
			t.Fatalf("item exceeds clip bound: %d chars", len(item))
		}
	}

	if len(sum.Objective) > maxSummaryObjective+3 {
		t.Fatalf("objective exceeds bound: %d", len(sum.Objective))
	}
}

// TestSummarySidecarRoundTrip covers persistence: save → load → delete
// cleanup with the session.
func TestSummarySidecarRoundTrip(t *testing.T) {
	store := newTestStore(t)

	sess := store.CreateInMode(ModeAgent)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// No summary yet — the version-0 shell is honest.
	shell, err := store.SummaryForRun(sess.ID)
	if err != nil {
		t.Fatalf("SummaryForRun: %v", err)
	}
	if shell.Version != 0 {
		t.Fatalf("fresh summary version = %d, want 0", shell.Version)
	}
	if RenderSummaryBlock(shell) != "" {
		t.Fatal("version-0 summary must render an EMPTY block (never fabricate)")
	}

	sum := UpdateSummaryFromTurn(shell, sess.ID, sess.Mode,
		"Fix the login bug — it must not leak the session token.",
		"Fixed in internal/auth/login.go with a token redaction.",
		[]string{"files", "shell"})

	if err := store.SaveSummary(sum); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	loaded, err := store.Summary(sess.ID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if loaded == nil || loaded.Version != 1 || loaded.Objective == "" {
		t.Fatalf("sidecar round trip mismatch: %+v", loaded)
	}

	block := RenderSummaryBlock(loaded)
	if !strings.Contains(block, "[SESSION SUMMARY") ||
		!strings.Contains(block, "Objective:") {
		t.Fatalf("rendered block missing provenance/fields:\n%s", block)
	}

	// Deleting the session removes the sidecar.
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Summary(sess.ID); err == nil {
		t.Fatal("summary of a deleted session must not load")
	}
}

// TestSummaryUnknownSessionRejected pins that a summary never outlives
// its session.
func TestSummaryUnknownSessionRejected(t *testing.T) {
	store := newTestStore(t)

	err := store.SaveSummary(&SessionSummary{SessionID: "ghost", Version: 1})
	if err == nil {
		t.Fatal("SaveSummary for an unknown session must fail")
	}
}
