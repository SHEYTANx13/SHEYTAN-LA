package goal

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

// scriptedRuns builds a deterministic RunFunc from a phase → response map.
type runLog struct {
	requests []RunRequest
}

func scriptedRuns(responses map[string]string, verified bool, log *runLog) RunFunc {
	return func(ctx context.Context, req RunRequest) (RunOutcome, error) {
		log.requests = append(log.requests, req)
		out := RunOutcome{Text: responses[req.Phase]}
		if req.Phase == PhaseVerifying {
			out.Verified = verified
			out.VerifyNotes = "scripted objective verification"
			out.Evidence = []Evidence{{Phase: req.Phase, Action: "verification check", Status: "verified"}}
		}
		if req.Phase == PhaseActing {
			out.Evidence = []Evidence{{
				Phase: PhaseActing, Action: "executed step", Target: fmt.Sprintf("step %d", req.StepIdx),
				Status: "ok", Detail: "deterministic scripted execution",
			}}
			out.ChangedFiles = []string{fmt.Sprintf("out/step-%d.txt", req.StepIdx)}
		}
		return out, nil
	}
}

func TestCreateAndTerminalJourney(t *testing.T) {
	store := openStore(t)
	log := &runLog{}
	runs := scriptedRuns(map[string]string{
		PhaseUnderstanding: "The goal is to write a demo file and verify it.",
		PhasePlanning:      `["write the file", "check the file exists"]`,
		PhaseActing:        "done: the step completed with objective output",
		PhaseVerifying:     "all criteria met",
	}, true, log)

	g, err := store.Create("produce a demo artifact", "sess-1", "sys-1", 3, "high")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.Phase != PhaseUnderstanding || g.Status != StatusActive {
		t.Fatalf("fresh goal must be active/understanding: %+v", g)
	}

	res, err := Drive(context.Background(), store, g.GoalID, runs, RunnerOptions{})
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("goal must complete, got %q (%s)", res.Status, res.Notes)
	}
	final, _ := store.Get(g.GoalID)
	if !final.VerificationPassed {
		t.Fatalf("terminal completion requires objective verification")
	}
	if final.Phase != PhaseCompleted || final.TerminalAt == nil {
		t.Fatalf("terminal settlement incomplete: %+v", final)
	}
	done, total := final.Progress()
	if done != 2 || total != 2 {
		t.Fatalf("progress must derive from plan state: %d/%d", done, total)
	}
	// Every acting step left a durable changed-file record.
	if len(final.ChangedFiles) != 2 {
		t.Fatalf("changed files must be recorded: %v", final.ChangedFiles)
	}
	// The checkpoint carries the compact state.
	if final.Checkpoint == nil || len(final.Checkpoint.CompletedSteps) != 2 {
		t.Fatalf("checkpoint must retain completed steps: %+v", final.Checkpoint)
	}
}

func TestPlanParseFailureFallsBackHonest(t *testing.T) {
	store := openStore(t)
	log := &runLog{}
	runs := scriptedRuns(map[string]string{
		PhaseUnderstanding: "classify",
		PhasePlanning:      "I cannot produce a plan right now, sorry.",
		PhaseActing:        "executed",
		PhaseVerifying:     "verified",
	}, true, log)

	g, _ := store.Create("fuzzy objective", "", "", 0, "")
	if _, err := Drive(context.Background(), store, g.GoalID, runs, RunnerOptions{}); err != nil {
		t.Fatalf("drive: %v", err)
	}
	final, _ := store.Get(g.GoalID)
	if len(final.Plan) != 1 || !strings.HasPrefix(final.Plan[0].Objective, "(fallback)") {
		t.Fatalf("unparseable plan must produce the honest single-step fallback: %+v", final.Plan)
	}
}

func TestPauseResumeCheckpointContinuation(t *testing.T) {
	store := openStore(t)
	log := &runLog{}
	runs := scriptedRuns(map[string]string{
		PhaseUnderstanding: "understood",
		PhasePlanning:      `["step A", "step B", "step C"]`,
		PhaseActing:        "step executed",
		PhaseVerifying:     "verified",
	}, true, log)

	g, _ := store.Create("three-step journey", "", "", 0, "")
	ctx := context.Background()

	// Drive once, then pause between steps (the pause check trips after
	// the first acting step completed).
	pauseAfter := 0
	_, err := Drive(ctx, store, g.GoalID, runs, RunnerOptions{
		PauseCheck: func(goalID string) bool {
			pauseAfter++
			cur, _ := store.Get(goalID)
			return pauseAfter >= 6 && cur.Phase == PhaseActing && cur.CurrentStep >= 1
		},
	})
	if err != nil && ctx.Err() == nil {
		t.Fatalf("drive: %v", err)
	}
	paused, _ := store.Get(g.GoalID)
	if paused.Status != StatusPaused && !paused.IsTerminal() {
		t.Fatalf("expected a parked goal, got %q", paused.Status)
	}
	if paused.IsTerminal() {
		t.Fatalf("pause must be observable before terminal settlement for this test to mean anything")
	}

	// RESUME: the loop continues from the checkpoint — completed steps
	// are NOT re-executed (no replay of committed mutations).
	if _, err := store.Resume(g.GoalID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	requestsBefore := len(log.requests)
	res, err := Drive(ctx, store, g.GoalID, runs, RunnerOptions{})
	if err != nil {
		t.Fatalf("resume drive: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("resumed goal must complete, got %q", res.Status)
	}
	for _, req := range log.requests[requestsBefore:] {
		if req.Phase == PhaseActing {
			// Only steps not yet completed may run after the resume.
			final, _ := store.Get(g.GoalID)
			for _, st := range final.Plan {
				if st.Status == StepCompleted && st.Index > req.StepIdx {
					// a later step completed after an earlier re-run —
					// would indicate replay; the monotone execution
					// order forbids it
					t.Fatalf("step replay detected after resume: run %d after completed %d", req.StepIdx, st.Index)
				}
			}
		}
	}
}

func TestCancelIsTerminalAndSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	g, _ := store.Create("abandoned objective", "", "", 0, "")
	if _, err := store.Cancel(g.GoalID, "user abandoned it"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := store.Resume(g.GoalID); err == nil {
		t.Fatalf("resuming a cancelled goal must be refused")
	}
	// Reload: the cancelled goal stays cancelled — abandoned goals can
	// never become falsely running.
	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	final, err := st2.Get(g.GoalID)
	if err != nil {
		t.Fatalf("get after reload: %v", err)
	}
	if final.Status != StatusCancelled || !final.IsTerminal() {
		t.Fatalf("cancel must survive reload: %q", final.Status)
	}
}

func TestRecoverOnBootNeverFalselyRunning(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	active, _ := store.Create("was running", "", "", 0, "")
	waiting, _ := store.Create("was waiting for approval", "", "", 0, "")
	done, _ := store.Create("was completed", "", "", 0, "")

	// Simulate a crash: statuses on disk are non-terminal and live.
	_, _ = store.mutate(active.GoalID, func(g *Goal) error { g.Status = StatusActive; return nil })
	_, _ = store.mutate(waiting.GoalID, func(g *Goal) error { g.Status = StatusWaitingApproval; return nil })
	if _, err := store.SetVerification(done.GoalID, true, "objectively verified"); err != nil {
		t.Fatalf("settle: %v", err)
	}

	repaired, err := store.RecoverOnBoot()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if repaired != 2 {
		t.Fatalf("two live goals must be repaired, got %d", repaired)
	}
	a, _ := store.Get(active.GoalID)
	w, _ := store.Get(waiting.GoalID)
	d, _ := store.Get(done.GoalID)
	if a.Status != StatusPaused || w.Status != StatusPaused {
		t.Fatalf("live goals must park as paused on boot: %q %q", a.Status, w.Status)
	}
	if d.Status != StatusCompleted {
		t.Fatalf("completed goal must stay completed: %q", d.Status)
	}

	// Reload recovery is IDEMPOTENT.
	repaired2, _ := store.RecoverOnBoot()
	if repaired2 != 0 {
		t.Fatalf("second recovery must repair nothing, got %d", repaired2)
	}
}

func TestApprovalParkingAndStaleRejection(t *testing.T) {
	store := openStore(t)
	g, _ := store.Create("needs a risky step", "", "", 0, "")

	pa := &PendingApproval{
		ApprovalID:  "apr-1",
		ToolName:    "shell",
		Args:        map[string]any{"command": "rm -rf out/old"},
		Risk:        "destructive",
		Reason:      "destructive workspace command",
		RequestedAt: timeNow(),
	}
	if _, err := store.RequestApproval(g.GoalID, pa); err != nil {
		t.Fatalf("request: %v", err)
	}
	parked, _ := store.Get(g.GoalID)
	if parked.Status != StatusWaitingApproval || parked.PendingApproval == nil {
		t.Fatalf("goal must park waiting_for_approval with a durable pending call")
	}

	// Stale approval id — rejected explicitly.
	if _, err := store.ResolveApproval(g.GoalID, "apr-stale", "approved"); err == nil {
		t.Fatalf("stale approval must be rejected")
	}

	// The exact approval: approve resumes; reject blocks.
	if _, err := store.ResolveApproval(g.GoalID, "apr-1", "approved"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	resumed, _ := store.Get(g.GoalID)
	if resumed.Status != StatusActive || resumed.PendingApproval != nil {
		t.Fatalf("approved goal must resume active with no pending call")
	}

	_, _ = store.RequestApproval(g.GoalID, &PendingApproval{
		ApprovalID: "apr-2", ToolName: "fetch", Risk: "external-network",
		RequestedAt: timeNow(),
	})
	if _, err := store.ResolveApproval(g.GoalID, "apr-2", "rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	rejected, _ := store.Get(g.GoalID)
	if rejected.Status != StatusBlocked {
		t.Fatalf("rejected call must block the goal, got %q", rejected.Status)
	}
	// The rejection is evidence.
	found := false
	for _, ev := range rejected.Evidence {
		if ev.Action == "approval decision" && ev.Status == "rejected" && ev.Target == "fetch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rejection must be recorded as evidence")
	}
}

func TestBoundedReplanOnBlockedStep(t *testing.T) {
	store := openStore(t)
	log := &runLog{}
	runs := scriptedRuns(map[string]string{
		PhaseUnderstanding: "understood",
		PhasePlanning:      `["always fails", "recover step"]`,
		PhaseActing:        "executed",
		PhaseVerifying:     "verified",
		"replanning":       `["revised recovery step"]`,
	}, true, log)

	// The first acting run for a step-0 index fails EXACTLY ONCE (the
	// original plan's step); the revised step runs fine — the replan is
	// what recovers, not a retry storm.
	calls := 0
	base := scriptedRuns(map[string]string{
		PhaseUnderstanding: "understood",
		PhasePlanning:      `["always fails", "recover step"]`,
		PhaseActing:        "executed fine",
		PhaseVerifying:     "verified",
		"replanning":       `["revised recovery step", "another one"]`,
	}, true, log)
	runs = func(ctx context.Context, req RunRequest) (RunOutcome, error) {
		if req.Phase == PhaseActing && req.StepIdx == 0 && calls == 0 {
			calls++
			return RunOutcome{}, fmt.Errorf("deterministic tool failure %d", calls)
		}
		return base(ctx, req)
	}

	g, _ := store.Create("recoverable journey", "", "", 0, "")
	res, err := Drive(context.Background(), store, g.GoalID, runs, RunnerOptions{})
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("a recoverable failure must not destroy the goal: %q (%s)", res.Status, res.Notes)
	}
	final, _ := store.Get(g.GoalID)
	if final.Replans == 0 {
		t.Fatalf("at least one bounded replan must have happened")
	}
	// The failure is honestly recorded at GOAL level — a plan revision
	// must not erase the evidence (no fake success synthesis).
	failureSeen := false
	for _, ev := range final.Evidence {
		if ev.Status == "error" && strings.Contains(ev.Action, "step run failed") {
			failureSeen = true
		}
	}
	if !failureSeen {
		t.Fatalf("the tool failure must be recorded in the goal evidence journal")
	}
	// The revised plan must contain the recovery step.
	revisedSeen := false
	for _, st := range final.Plan {
		if strings.Contains(st.Objective, "revised recovery step") {
			revisedSeen = true
		}
	}
	if !revisedSeen {
		t.Fatalf("the revised plan must be installed: %+v", final.Plan)
	}
}

func TestTurnBudgetParksInsteadOfRunningAway(t *testing.T) {
	store := openStore(t)
	log := &runLog{}
	runs := scriptedRuns(map[string]string{
		PhaseUnderstanding: "understood",
		PhasePlanning:      `["a", "b", "c", "d", "e", "f", "g", "h"]`,
		PhaseActing:        "executed",
		PhaseVerifying:     "verified",
	}, true, log)

	g, _ := store.Create("long journey", "", "", 0, "")
	res, err := Drive(context.Background(), store, g.GoalID, runs, RunnerOptions{TurnBudget: 3})
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if res.Status != StatusPaused {
		t.Fatalf("exhausted budget must park the goal, got %q", res.Status)
	}
	final, _ := store.Get(g.GoalID)
	if final.TurnsUsed > 3 {
		t.Fatalf("turn budget must bound execution: %d", final.TurnsUsed)
	}
}

func TestReloadPreservesPlanApprovalAndEvidence(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	g, _ := store.Create("durable objective", "sess-9", "sys-2", 5, "mid")
	_, _ = store.SetPlan(g.GoalID, []string{"first", "second"})
	_, _ = store.MarkStep(g.GoalID, 0, StepCompleted, "first done", []Evidence{{
		Phase: PhaseActing, Action: "did the thing", Status: "ok",
	}}, []string{"a.txt"}, nil)
	_, _ = store.RequestApproval(g.GoalID, &PendingApproval{
		ApprovalID: "apr-x", ToolName: "shell", Risk: "privileged", RequestedAt: timeNow(),
	})

	store2, err := Open(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	final, err := store2.Get(g.GoalID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(final.Plan) != 2 || final.Plan[0].Status != StepCompleted {
		t.Fatalf("plan must survive reload: %+v", final.Plan)
	}
	if final.PendingApproval == nil || final.PendingApproval.ApprovalID != "apr-x" {
		t.Fatalf("pending approval must survive reload")
	}
	if len(final.Plan) == 0 || len(final.Plan[0].Evidence) == 0 {
		t.Fatalf("step-level evidence must survive reload: %+v", final.Plan)
	}
	if len(final.Evidence) == 0 {
		t.Fatalf("goal-level evidence journal must survive reload")
	}
	if len(final.ChangedFiles) == 0 || final.ChangedFiles[0] != "a.txt" {
		t.Fatalf("changed files must survive reload")
	}
	if final.Checkpoint == nil || len(final.Checkpoint.CompletedSteps) != 1 {
		t.Fatalf("checkpoint must survive reload: %+v", final.Checkpoint)
	}
	if final.Status != StatusWaitingApproval {
		t.Fatalf("waiting status must survive reload, got %q", final.Status)
	}
}

func TestListOrderingDeterministic(t *testing.T) {
	store := openStore(t)
	for _, txt := range []string{"zulu goal", "alpha goal", "mike goal"} {
		_, err := store.Create(txt, "", "", 0, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	list, _ := store.List()
	for i := 1; i < len(list); i++ {
		a, b := list[i-1], list[i]
		if a.CreatedAt.After(b.CreatedAt) ||
			(a.CreatedAt.Equal(b.CreatedAt) && a.GoalID > b.GoalID) {
			t.Fatalf("goal list ordering is not deterministic at %d", i)
		}
	}
}

func timeNow() time.Time {
	return time.Now().UTC()
}
