package goal

// drive_lifecycle_test.go — v1.10.13 regression tests for the state
// machine and approval repairs. Deterministic, no sleeps: every wait is
// a bounded poll on a condition; every assertion names the invariant it
// pins. The invariants survive this cycle (quoted from the register):
//
//	I4 — Approvals are exact, one-use, and durable: bound to the
//	     canonical action, consumed atomically at execution, never
//	     clearable by generic resume.
//	    — waiting_for_approval transitions ONLY through approve/reject
//	      (cancel is the terminal escape); a generic resume must never
//	      erase an unresolved human approval decision.
//	I3 — No discarded outcomes: a failed drive settles the goal
//	     deliberately (blocked), never active-without-drive.

import (
	"errors"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func mustCreate(t *testing.T, s *Store, text string) Goal {
	t.Helper()
	g, err := s.Create(text, "sess", "", 0, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return g
}

func pendingStepPA(goalID string, stepIdx int) *PendingApproval {
	return &PendingApproval{
		ApprovalID:  goalID + "-step-0",
		ToolName:    "goal.step",
		Args:        map[string]any{"step": stepIdx, "objective": "fixture step"},
		Risk:        "workspace-write",
		Reason:      "fixture policy",
		RequestedAt: time.Now().UTC(),
	}
}

// TestResumeRejectsWaitingApproval pins the STATE-MACHINE repair: a
// generic resume on a waiting_for_approval goal is a typed rejection and
// the unresolved approval decision SURVIVES (the former code accepted the
// resume AND set PendingApproval = nil — an erased human decision).
func TestResumeRejectsWaitingApproval(t *testing.T) {
	s := openTestStore(t)
	g := mustCreate(t, s, "resume must not erase a pending approval")

	if _, err := s.RequestApproval(g.GoalID, pendingStepPA(g.GoalID, 0)); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	_, err := s.Resume(g.GoalID)
	if !errors.Is(err, ErrNotResumable) {
		t.Fatalf("Resume on waiting_for_approval = %v, want ErrNotResumable", err)
	}

	got, err := s.Get(g.GoalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusWaitingApproval {
		t.Fatalf("status = %q, want %q (the rejection must have no side effects)", got.Status, StatusWaitingApproval)
	}
	if got.PendingApproval == nil {
		t.Fatal("PendingApproval was erased by a rejected resume — an unresolved human decision must survive")
	}
	if got.PendingApproval.ApprovalID != g.GoalID+"-step-0" {
		t.Fatalf("PendingApproval.ApprovalID = %q, want the durable original", got.PendingApproval.ApprovalID)
	}
}

// TestResumeStillAllowsPausedAndBlocked guards the repair against
// overreach: paused and blocked goals remain honest resume sources.
func TestResumeStillAllowsPausedAndBlocked(t *testing.T) {
	s := openTestStore(t)

	paused := mustCreate(t, s, "paused goal resumes")
	if _, err := s.Pause(paused.GoalID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := s.Resume(paused.GoalID); err != nil {
		t.Fatalf("Resume(paused): %v", err)
	}
	if got, _ := s.Get(paused.GoalID); got.Status != StatusActive {
		t.Fatalf("paused goal resume status = %q, want active", got.Status)
	}

	blocked := mustCreate(t, s, "blocked goal resumes")
	if _, err := s.SettleDriveFailure(blocked.GoalID, "fixture drive failure"); err != nil {
		t.Fatalf("SettleDriveFailure: %v", err)
	}
	if _, err := s.Resume(blocked.GoalID); err != nil {
		t.Fatalf("Resume(blocked): %v", err)
	}
	if got, _ := s.Get(blocked.GoalID); got.Status != StatusActive {
		t.Fatalf("blocked goal resume status = %q, want active", got.Status)
	}
}

// TestApprovedActionConsumedExactlyOnce pins I4: the approved decision
// arms EXACTLY the approved step as a durable ONE-USE grant; the first
// consumption at that step succeeds, the second finds nothing.
func TestApprovedActionConsumedExactlyOnce(t *testing.T) {
	s := openTestStore(t)
	g := mustCreate(t, s, "one-use approval grant")

	if _, err := s.RequestApproval(g.GoalID, pendingStepPA(g.GoalID, 0)); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if _, err := s.ResolveApproval(g.GoalID, g.GoalID+"-step-0", "approved"); err != nil {
		t.Fatalf("ResolveApproval(approve): %v", err)
	}

	got, _ := s.Get(g.GoalID)
	if got.ApprovedAction == nil {
		t.Fatal("approve did not arm the ONE-USE grant (ApprovedAction nil)")
	}
	if got.ApprovedAction.StepIdx != 0 || got.ApprovedAction.ApprovalID != g.GoalID+"-step-0" {
		t.Fatalf("armed grant = %+v, want the exact canonical action (step 0, its approvalId)", got.ApprovedAction)
	}

	first, err := s.TakeApprovedAction(g.GoalID, 0)
	if err != nil || !first {
		t.Fatalf("first TakeApprovedAction(0) = %v, %v; want true, nil", first, err)
	}
	second, err := s.TakeApprovedAction(g.GoalID, 0)
	if err != nil || second {
		t.Fatalf("second TakeApprovedAction(0) = %v, %v; want false, nil (one-use)", second, err)
	}
	got, _ = s.Get(g.GoalID)
	if got.ApprovedAction != nil {
		t.Fatalf("grant survived consumption: %+v", got.ApprovedAction)
	}
}

// TestApprovedActionMismatchedStepNotConsumed pins the exact-action
// binding: a grant for step 0 is never consumed at any other step, with
// no side effects.
func TestApprovedActionMismatchedStepNotConsumed(t *testing.T) {
	s := openTestStore(t)
	g := mustCreate(t, s, "mismatched step never consumes")

	if _, err := s.RequestApproval(g.GoalID, pendingStepPA(g.GoalID, 0)); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if _, err := s.ResolveApproval(g.GoalID, g.GoalID+"-step-0", "approved"); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}

	ok, err := s.TakeApprovedAction(g.GoalID, 1)
	if err != nil || ok {
		t.Fatalf("TakeApprovedAction(1) = %v, %v; want false, nil (mismatched step)", ok, err)
	}
	got, _ := s.Get(g.GoalID)
	if got.ApprovedAction == nil || got.ApprovedAction.StepIdx != 0 {
		t.Fatalf("grant = %+v, want untouched grant for step 0", got.ApprovedAction)
	}
}

// TestRejectClearsArmedGrant pins rejection terminality: when the NEWEST
// decision for a step is a rejection, any armed grant for THAT step is
// cleared — a later drive can never execute it.
func TestRejectClearsArmedGrant(t *testing.T) {
	s := openTestStore(t)
	g := mustCreate(t, s, "rejection clears the armed grant")

	if _, err := s.RequestApproval(g.GoalID, pendingStepPA(g.GoalID, 0)); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if _, err := s.ResolveApproval(g.GoalID, g.GoalID+"-step-0", "approved"); err != nil {
		t.Fatalf("ResolveApproval(approve): %v", err)
	}

	// The goal re-parks for the same step (drive restart raced, fixture
	// restart — any path that produces a FRESH pending call for step 0).
	reParked, err := s.RequestApproval(g.GoalID, pendingStepPA(g.GoalID, 0))
	if err != nil {
		t.Fatalf("re-park RequestApproval: %v", err)
	}
	if _, err := s.ResolveApproval(g.GoalID, reParked.PendingApproval.ApprovalID, "rejected"); err != nil {
		t.Fatalf("ResolveApproval(reject): %v", err)
	}

	got, _ := s.Get(g.GoalID)
	if got.ApprovedAction != nil {
		t.Fatalf("armed grant survived the newest rejection: %+v — a rejection is terminal for the pending action", got.ApprovedAction)
	}
	if got.Status != StatusBlocked {
		t.Fatalf("status after reject = %q, want blocked", got.Status)
	}
}

// TestApprovedGrantSurvivesReload pins the DURABLE half of I4: the grant
// is a stored document field, and the canonical step identity survives
// the JSON round trip (Args["step"] becomes a float64 on reload — the
// consumption path must still bind to the exact step).
func TestApprovedGrantSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	g := mustCreate(t, s, "durable one-use grant")
	if _, err := s.RequestApproval(g.GoalID, pendingStepPA(g.GoalID, 0)); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if _, err := s.ResolveApproval(g.GoalID, g.GoalID+"-step-0", "approved"); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}

	// Reopen: a fresh store over the same directory (the reload path).
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.Get(g.GoalID)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got.ApprovedAction == nil || got.ApprovedAction.StepIdx != 0 {
		t.Fatalf("grant after reload = %+v, want the durable step-0 grant", got.ApprovedAction)
	}
	ok, err := s2.TakeApprovedAction(g.GoalID, 0)
	if err != nil || !ok {
		t.Fatalf("post-reload TakeApprovedAction(0) = %v, %v; want true, nil", ok, err)
	}
}

// TestSettleDriveFailure pins I3/I6: an observed drive failure settles an
// ACTIVE goal to blocked (resumable honestly); terminal goals stay
// terminal; paused/waiting states are left untouched.
func TestSettleDriveFailure(t *testing.T) {
	s := openTestStore(t)

	active := mustCreate(t, s, "failed drive settles blocked")
	if _, err := s.SettleDriveFailure(active.GoalID, "the drive failed"); err != nil {
		t.Fatalf("SettleDriveFailure: %v", err)
	}
	got, _ := s.Get(active.GoalID)
	if got.Status != StatusBlocked {
		t.Fatalf("settled status = %q, want blocked", got.Status)
	}
	if got.LastAction != "the drive failed" {
		t.Fatalf("LastAction = %q, want the settlement reason", got.LastAction)
	}
	if got.IsTerminal() {
		t.Fatal("a settled drive failure must stay resumable (blocked), not terminal")
	}

	cancelled := mustCreate(t, s, "terminal stays terminal")
	if _, err := s.Cancel(cancelled.GoalID, "fixture"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := s.SettleDriveFailure(cancelled.GoalID, "late drive error"); err != nil {
		t.Fatalf("SettleDriveFailure(cancelled): %v", err)
	}
	if got, _ := s.Get(cancelled.GoalID); got.Status != StatusCancelled {
		t.Fatalf("terminal status = %q after settlement, want cancelled (no resurrection)", got.Status)
	}

	paused := mustCreate(t, s, "paused stays paused")
	if _, err := s.Pause(paused.GoalID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := s.SettleDriveFailure(paused.GoalID, "late drive error"); err != nil {
		t.Fatalf("SettleDriveFailure(paused): %v", err)
	}
	if got, _ := s.Get(paused.GoalID); got.Status != StatusPaused {
		t.Fatalf("paused status = %q after settlement, want paused (the state already says why the drive stopped)", got.Status)
	}
}
