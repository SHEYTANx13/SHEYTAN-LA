package goal

// runner.go — the LONG-HORIZON LOOP (v1.9.0).
//
// UNDERSTAND → PLAN → DELEGATE/ACT → OBSERVE → VERIFY → REPLAN → COMPLETE
//
// The runner evolves the existing planner→executor→critic→summarizer
// runtime toward an explicit, checkpointed, evidence-driven loop. Every
// model-facing step executes through the injected RunFunc (the ONE
// orchestrator with the run's frozen AI System binding) — the runner
// owns STATE, BOUNDS and CHECKPOINTS, never a model loop of its own.
//
// Honesty rules enforced here:
//   - progress derives from actual plan/task state (no percentages);
//   - a model claim never proves completion (verification is objective);
//   - failed/blocked steps stay failed/blocked — no fake success;
//   - every pause/approval/blocker is durable before it is observable;
//   - the loop is bounded by turn budget and bounded replans.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// RunResult is the outcome of one full Drive attempt.
type RunResult struct {
	GoalID    string
	Status    string
	Phase     string
	Notes     string
	TurnsUsed int
}

// settledElsewhere reports whether a store-write error is the TYPED
// terminal refusal (the goal was cancelled/completed/failed by another
// actor while this drive was in flight — the run-#28 cancel race). The
// drive must then stop CLEANLY at its next re-read: never overwrite the
// durable state (I6), never swallow the outcome as a silent discard
// (I3) — the refusal is observed and acted on at every site below.
func settledElsewhere(err error) bool {
	return errors.Is(err, ErrNotResumable)
}

// Drive executes the long-horizon loop until a terminal state, a bound,
// a pause, or an approval parking point is reached. It is resumable:
// calling Drive again after a pause/blocked state continues from the
// checkpoint (committed mutations are never replayed).
func Drive(ctx context.Context, store *Store, goalID string, runs RunFunc, opts RunnerOptions) (RunResult, error) {
	g, err := store.Get(goalID)
	if err != nil {
		return RunResult{}, err
	}
	if g.IsTerminal() {
		return RunResult{GoalID: g.GoalID, Status: g.Status, Phase: g.Phase, Notes: "already settled"}, nil
	}
	if g.Status != StatusActive {
		return RunResult{GoalID: g.GoalID, Status: g.Status, Phase: g.Phase,
			Notes: "goal is not active (paused, blocked or awaiting approval) — resume first"}, nil
	}
	if runs == nil {
		return RunResult{}, fmt.Errorf("goal %s: no run executor bound", goalID)
	}
	if opts.TurnBudget <= 0 {
		opts.TurnBudget = DefaultTurnBudget
	}
	if opts.MaxReplans <= 0 {
		opts.MaxReplans = MaxReplans
	}

	res := RunResult{GoalID: g.GoalID}

	// The loop runs one PHASE per drive segment; the API layer drives
	// segments from run boundaries so every state transition lands on a
	// persisted checkpoint.
	for {
		if ctx.Err() != nil {
			// v1.10.13 (Task ID 28 I3/I6): the pause is OBSERVED —
			// a typed terminal refusal means the goal was settled
			// (cancelled) while the drive unwound; the durable
			// state wins and is reported honestly.
			if _, err := store.Pause(goalID); err != nil && !settledElsewhere(err) {
				return res, err
			}
			if g2, gerr := store.Get(goalID); gerr == nil {
				res.Status, res.Phase = g2.Status, g2.Phase
				if res.Status == StatusPaused {
					res.Notes = "context cancelled — parked at a checkpoint"
				} else {
					res.Notes = "context cancelled — the goal was settled during the unwind (" + res.Status + ")"
				}
			} else {
				res.Status = StatusPaused
				res.Notes = "context cancelled — parked at a checkpoint"
			}
			return res, ctx.Err()
		}

		g, err = store.Get(goalID)
		if err != nil {
			return res, err
		}
		res.Status = g.Status
		res.Phase = g.Phase
		res.TurnsUsed = g.TurnsUsed

		if g.TurnsUsed >= opts.TurnBudget {
			if _, err := store.AppendEvidence(goalID, Evidence{
				Phase: g.Phase, Action: "turn budget exhausted",
				Status: "blocked", Detail: fmt.Sprintf("budget %d turns reached", opts.TurnBudget),
			}); err != nil {
				return res, err
			}
			if _, err := store.Pause(goalID); err != nil && !settledElsewhere(err) {
				return res, err
			}
			if g2, gerr := store.Get(goalID); gerr == nil {
				res.Status, res.Phase = g2.Status, g2.Phase
			}
			if res.Status != StatusPaused {
				res.Notes = "turn budget exhausted — the goal was settled during the unwind (" + res.Status + ")"
				return res, nil
			}
			res.Status = StatusPaused
			res.Notes = "turn budget exhausted — parked for explicit resume or cancel"
			return res, nil
		}

		if opts.PauseCheck != nil && opts.PauseCheck(goalID) {
			if _, err := store.Pause(goalID); err != nil && !settledElsewhere(err) {
				return res, err
			}
			if g2, gerr := store.Get(goalID); gerr == nil {
				res.Status, res.Phase = g2.Status, g2.Phase
			}
			if res.Status != StatusPaused {
				res.Notes = "pause requested — the goal was settled during the unwind (" + res.Status + ")"
				return res, nil
			}
			res.Status = StatusPaused
			res.Notes = "pause requested"
			return res, nil
		}

		switch g.Phase {
		case PhaseUnderstanding:
			if err := driveUnderstanding(ctx, store, runs, goalID); err != nil {
				return res, err
			}

		case PhasePlanning:
			if err := drivePlanning(ctx, store, runs, goalID); err != nil {
				return res, err
			}

		case PhaseActing:
			done, err := driveActing(ctx, store, runs, goalID, opts)
			if err != nil {
				return res, err
			}
			if !done {
				// parked (approval / pause) — return honestly
				g2, _ := store.Get(goalID)
				res.Status = g2.Status
				res.Phase = g2.Phase
				return res, nil
			}

		case PhaseVerifying:
			if err := driveVerifying(ctx, store, runs, goalID); err != nil {
				return res, err
			}

		case PhaseCompleted:
			g2, _ := store.Get(goalID)
			res.Status = g2.Status
			res.Phase = g2.Phase
			res.Notes = g2.Verification
			return res, nil

		default:
			return res, fmt.Errorf("goal %s: unknown phase %q", goalID, g.Phase)
		}

		// Re-read after each phase segment: a terminal settlement, a
		// parked state or a bound ends the drive segment.
		g2, err := store.Get(goalID)
		if err != nil {
			return res, err
		}
		res.Status = g2.Status
		res.Phase = g2.Phase
		res.TurnsUsed = g2.TurnsUsed
		// The ONE continuation exception: a BLOCKED step with
		// replan budget remaining continues in-drive — evidence-
		// driven replanning is the loop's own recovery path, not
		// a parked state (an exhausted replan budget parks).
		replanPending := g2.Status == StatusBlocked && g2.Phase == PhaseActing &&
			g2.Replans < opts.MaxReplans
		if g2.IsTerminal() || (g2.Status != StatusActive && !replanPending) {
			if g2.IsTerminal() {
				res.Notes = g2.Verification
			} else {
				res.Notes = "parked: " + g2.Status
			}
			return res, nil
		}
	}
}

// ---------------------------------------------------------------------------
// Phase drivers
// ---------------------------------------------------------------------------

func driveUnderstanding(ctx context.Context, store *Store, runs RunFunc, goalID string) error {
	g, _ := store.Get(goalID)
	out, err := runs(ctx, RunRequest{
		GoalID: goalID, Phase: PhaseUnderstanding,
		Prompt: understandingPrompt(g.OriginalGoal), SystemID: g.SystemID, SystemRevision: g.SystemRevision,
	})
	if err != nil {
		if _, evErr := store.AppendEvidence(goalID, Evidence{
			Phase: PhaseUnderstanding, Action: "understanding run failed",
			Status: "error", Detail: err.Error(),
		}); evErr != nil {
			// I3 (Task ID 28): the primary run error is preserved.
			return fmt.Errorf("understanding run failed (%v); the evidence append also failed: %w", err, evErr)
		}
		return err
	}
	if _, evErr := store.AppendEvidence(goalID, Evidence{
		Phase: PhaseUnderstanding, Action: "goal classified",
		Status: "ok", Detail: firstLine(out.Text),
	}); evErr != nil {
		return evErr
	}
	_, err = store.SetPhase(goalID, PhasePlanning)
	return err
}

func drivePlanning(ctx context.Context, store *Store, runs RunFunc, goalID string) error {
	g, _ := store.Get(goalID)
	out, err := runs(ctx, RunRequest{
		GoalID: goalID, Phase: PhasePlanning,
		Prompt: planningPrompt(g.OriginalGoal), SystemID: g.SystemID, SystemRevision: g.SystemRevision,
	})
	if err != nil {
		if _, evErr := store.AppendEvidence(goalID, Evidence{
			Phase: PhasePlanning, Action: "planning run failed",
			Status: "error", Detail: err.Error(),
		}); evErr != nil {
			// I3 (Task ID 28): the primary run error is preserved.
			return fmt.Errorf("planning run failed (%v); the evidence append also failed: %w", err, evErr)
		}
		return err
	}

	steps := parsePlan(out.Text)
	if len(steps) == 0 {
		// Honest fallback: ONE step carrying the goal itself, marked as
		// the unplanned fallback — never a fabricated decomposition.
		steps = []string{"(fallback) " + g.OriginalGoal}
		if _, evErr := store.AppendEvidence(goalID, Evidence{
			Phase: PhasePlanning, Action: "plan parse failed — fallback single-step plan",
			Status: "ok", Detail: firstLine(out.Text),
		}); evErr != nil {
			return evErr
		}
	}
	_, err = store.SetPlan(goalID, steps)
	return err
}

// driveActing executes the next pending step. Returns false when the
// goal parked (approval / pause request).
func driveActing(ctx context.Context, store *Store, runs RunFunc, goalID string, opts RunnerOptions) (bool, error) {
	g, err := store.Get(goalID)
	if err != nil {
		return false, err
	}

	// Bounded REPLAN: when a blocked step exists and the replan budget
	// allows, one replanning run revises the REMAINING plan (completed
	// steps are never re-executed, never replayed).
	blockedIdx := -1
	for i := range g.Plan {
		if g.Plan[i].Status == StepBlocked {
			blockedIdx = i
			break
		}
	}
	if blockedIdx >= 0 {
		if g.Replans >= opts.MaxReplans {
			if _, err := store.SetVerification(goalID, false,
				fmt.Sprintf("step %d stayed blocked after %d bounded replans", blockedIdx, g.Replans)); err != nil && !settledElsewhere(err) {
				return false, err
			}
			return true, nil
		}
		if _, err := store.mutate(goalID, func(gg *Goal) error {
			gg.Replans++
			gg.LastAction = fmt.Sprintf("replan %d after blocked step %d", gg.Replans, blockedIdx)
			return nil
		}); err != nil {
			return false, err
		}
		out, err := runs(ctx, RunRequest{
			GoalID: goalID, Phase: "replanning",
			Prompt: replanPrompt(g.OriginalGoal, g.Plan, blockedIdx), SystemID: g.SystemID, SystemRevision: g.SystemRevision,
		})
		if err != nil {
			if _, evErr := store.AppendEvidence(goalID, Evidence{
				Phase: PhaseActing, Action: "replanning run failed",
				Status: "error", Detail: err.Error(),
			}); evErr != nil {
				// I3 (Task ID 28): the primary run error is
				// preserved; the evidence failure is wrapped in.
				return false, fmt.Errorf("replanning run failed (%v); the evidence append also failed: %w", err, evErr)
			}
			return false, err
		}
		if revised := parsePlan(out.Text); len(revised) > 0 {
			if _, pmErr := store.mutate(goalID, func(gg *Goal) error {
				// Replace only PENDING steps from the blocked one on;
				// completed steps keep their evidence (no replay).
				added := make([]PlanStep, 0, len(revised))
				idx := len(gg.Plan)
				for _, o := range revised {
					added = append(added, PlanStep{Index: idx, Objective: o, Status: StepPending})
					idx++
				}
				kept := make([]PlanStep, 0, len(gg.Plan)+len(added))
				for _, st := range gg.Plan {
					if st.Index == blockedIdx {
						continue
					}
					kept = append(kept, st)
				}
				kept = append(kept, added...)
				for i := range kept {
					kept[i].Index = i
				}
				gg.Plan = kept
				return nil
			}); pmErr != nil {
				return false, pmErr
			}
			if _, evErr := store.AppendEvidence(goalID, Evidence{
				Phase: PhaseActing, Action: "plan revised after evidence",
				Status: "ok", Detail: fmt.Sprintf("%d revised steps", len(revised)),
			}); evErr != nil {
				return false, evErr
			}
		}
		if _, err := store.mutate(goalID, func(gg *Goal) error {
			// v1.10.13 TERMINAL-GUARD (I6, Task ID 28 R1(b) — the
			// pre-committed V7 fix): the replan path must never
			// resurrect a terminal goal to active.
			if gg.IsTerminal() {
				return ErrNotResumable
			}
			gg.Status = StatusActive
			return nil
		}); err != nil {
			if settledElsewhere(err) {
				return true, nil
			}
			return false, err
		}
		return true, nil
	}

	// Find the next pending step (deterministic: lowest index first).
	next := -1
	for i := range g.Plan {
		if g.Plan[i].Status == StepPending {
			next = g.Plan[i].Index
			break
		}
	}
	if next < 0 {
		// All steps settled — move to verification.
		_, err = store.SetPhase(goalID, PhaseVerifying)
		return true, err
	}

	// Approval boundary (spec §11): the configured approval policy may
	// park the goal BEFORE the step runs. Never execute before approval.
	if opts.ApprovalCheck != nil {
		if pending, pa := opts.ApprovalCheck(goalID, next); pending && pa != nil {
			if _, err := store.RequestApproval(goalID, pa); err != nil {
				return false, err
			}
			return false, nil
		}
	}

	_, _ = store.mutate(goalID, func(gg *Goal) error {
		gg.Plan[next].Status = StepActive
		gg.CurrentStep = next
		return nil
	})

	out, err := runs(ctx, RunRequest{
		GoalID: goalID, Phase: PhaseActing, StepIdx: next,
		Prompt: stepPrompt(g.OriginalGoal, g.Plan, next), SystemID: g.SystemID, SystemRevision: g.SystemRevision,
	})
	if err != nil {
		// A recoverable tool failure does NOT destroy the whole goal:
		// the step is marked blocked (evidence kept at GOAL level too,
		// so a plan revision cannot erase the failure record) and
		// bounded replanning owns the recovery.
		if _, evErr := store.AppendEvidence(goalID, Evidence{
			Phase: PhaseActing, Action: "step run failed", Target: g.Plan[next].Objective,
			Status: "error", Detail: err.Error(),
		}); evErr != nil {
			// I3 (Task ID 28): the primary run error is preserved.
			return false, fmt.Errorf("step run failed (%v); the evidence append also failed: %w", err, evErr)
		}
		if _, msErr := store.MarkStep(goalID, next, StepBlocked, "run failed: "+err.Error(), []Evidence{{
			Phase: PhaseActing, Action: "step run failed", Target: g.Plan[next].Objective,
			Status: "error", Detail: err.Error(),
		}}, nil, nil); msErr != nil {
			if settledElsewhere(msErr) {
				// The goal was settled (cancelled) while the run
				// was in flight — unwind at the re-read; the
				// durable status stays terminal (I6).
				return false, nil
			}
			return false, msErr
		}
		return true, nil
	}

	status := StepCompleted
	if strings.TrimSpace(out.Text) == "" && len(out.Evidence) == 0 {
		status = StepBlocked
	}
	_, err = store.MarkStep(goalID, next, status, firstLine(out.Text), out.Evidence, out.ChangedFiles, out.Artifacts)
	if err != nil && settledElsewhere(err) {
		// v1.10.13 (Task ID 28, PROBE A's exact overwrite site): the
		// late settlement of a cancelled goal is REFUSED typed — the
		// drive stops cleanly and the cancelled status survives.
		return false, nil
	}
	return true, err
}

func driveVerifying(ctx context.Context, store *Store, runs RunFunc, goalID string) error {
	g, _ := store.Get(goalID)
	out, err := runs(ctx, RunRequest{
		GoalID: goalID, Phase: PhaseVerifying,
		Prompt: verifyPrompt(g.OriginalGoal, g.Plan), SystemID: g.SystemID, SystemRevision: g.SystemRevision,
	})
	if err != nil {
		if _, svErr := store.SetVerification(goalID, false, "verification run failed: "+err.Error()); svErr != nil && !settledElsewhere(svErr) {
			return svErr
		}
		return nil
	}
	// The verdict must be EVIDENCE-backed: the runner (the orchestrator
	// adapter) derives out.Verified from objective checks — the model's
	// prose claim in out.Text alone can never set it.
	_, err = store.SetVerification(goalID, out.Verified, verifyNotes(out, g))
	if err != nil && settledElsewhere(err) {
		// v1.10.13 (Task ID 28): a concurrent cancel owns the terminal
		// state — the verdict is never written over it (I6).
		return nil
	}
	return err
}

func verifyNotes(out RunOutcome, g Goal) string {
	notes := out.VerifyNotes
	if notes == "" {
		notes = firstLine(out.Text)
	}
	if len(out.Evidence) > 0 {
		notes += fmt.Sprintf(" (evidence records: %d)", len(out.Evidence))
	}
	return notes
}

// ---------------------------------------------------------------------------
// Prompt builders (compact, bounded, state-derived)
// ---------------------------------------------------------------------------

func understandingPrompt(original string) string {
	return "Goal: " + original + "\n\n" +
		"Establish what this goal requires: restate the objective, the concrete " +
		"success criteria and the constraints. Do not execute anything yet."
}

func planningPrompt(original string) string {
	return "Goal: " + original + "\n\n" +
		"Decompose this goal into a bounded, verifiable plan of 1 to " +
		fmt.Sprintf("%d", MaxPlanSteps) + " steps. Reply with ONLY a JSON array " +
		"of step strings — each step independently verifiable. Example: " +
		`["inspect the current state", "apply the change", "verify the result"]`
}

func replanPrompt(original string, plan []PlanStep, blockedIdx int) string {
	var b strings.Builder
	b.WriteString("Goal: " + original + "\n\nCurrent plan state:\n")
	for _, st := range plan {
		marker := "  "
		if st.Index == blockedIdx {
			marker = "✗ "
		}
		b.WriteString(fmt.Sprintf("%s%d. [%s] %s\n", marker, st.Index, st.Status, st.Objective))
	}
	b.WriteString(fmt.Sprintf("\nStep %d is blocked. Revise the REMAINING work into 1 to %d new steps. "+
		"Completed steps stay completed — do not repeat them. Reply with ONLY a JSON array of step strings.",
		blockedIdx, MaxPlanSteps))
	return b.String()
}

func stepPrompt(original string, plan []PlanStep, idx int) string {
	var b strings.Builder
	b.WriteString("Overall goal: " + original + "\n\n")
	b.WriteString("Completed so far:\n")
	any := false
	for _, st := range plan {
		if st.Status == StepCompleted {
			any = true
			b.WriteString(fmt.Sprintf("- step %d: %s (%s)\n", st.Index, st.Objective, firstLine(st.Result)))
		}
	}
	if !any {
		b.WriteString("- (nothing yet)\n")
	}
	b.WriteString(fmt.Sprintf("\nExecute NOW exactly this step and nothing else: %s\n"+
		"Report concretely what you did and the result. Do not jump ahead to later steps.",
		plan[idx].Objective))
	return b.String()
}

func verifyPrompt(original string, plan []PlanStep) string {
	var b strings.Builder
	b.WriteString("Goal: " + original + "\n\nExecuted plan:\n")
	for _, st := range plan {
		b.WriteString(fmt.Sprintf("- step %d [%s]: %s → %s\n", st.Index, st.Status, st.Objective, firstLine(st.Result)))
	}
	b.WriteString("\nVERIFY the goal against OBJECTIVE evidence only (tool results, file " +
		"states, test outputs). Run the checks yourself. State clearly whether every " +
		"success criterion is objectively met.")
	return b.String()
}

// parsePlan extracts a JSON string array from a run response (bounded).
func parsePlan(text string) []string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil
	}
	var steps []string
	if err := json.Unmarshal([]byte(text[start:end+1]), &steps); err != nil {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, st := range steps {
		st = strings.TrimSpace(st)
		if st != "" {
			out = append(out, st)
		}
		if len(out) >= MaxPlanSteps {
			break
		}
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
