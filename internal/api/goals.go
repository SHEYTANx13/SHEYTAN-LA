package api

// goals.go — v1.9.0 HTTP surface for the durable long-horizon Goal engine.
//
// Routes:
//
//      GET    /api/goals                  list
//      POST   /api/goals                  create (+ optional immediate start)
//      GET    /api/goals/{id}             one goal (full durable state)
//      DELETE /api/goals/{id}             cancel (terminal; never resurrects)
//      POST   /api/goals/{id}/start       drive the loop (async, bounded)
//      POST   /api/goals/{id}/pause       park at the next checkpoint boundary
//      POST   /api/goals/{id}/resume      continue from the checkpoint
//      POST   /api/goals/{id}/cancel      terminal cancel
//      POST   /api/goals/{id}/approve     approve the pending exact call
//      POST   /api/goals/{id}/reject      reject the pending exact call
//
// The drive executor binds to the ONE orchestrator (with the run's frozen
// AI System snapshot). The loop is bounded by turn budget + replans and
// parks at checkpoints; no decorative progress, no fake autonomy.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/agent"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/aisystem"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/goal"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
)

// goalRunFunc binds the goal engine's executor seam to the ONE
// orchestrator. Every engine run carries the goal's frozen AI System
// snapshot; the objective verification report maps into the engine's
// outcome — a model claim alone can never complete a goal.
func (s *Server) goalRunFunc() goal.RunFunc {
	return func(ctx context.Context, req goal.RunRequest) (goal.RunOutcome, error) {
		// v1.10.13 SYSTEMID-DISCARDED repair: the goal's frozen AI
		// System identity is BINDING at the orchestration boundary — the
		// runner passes it into every phase request (understanding,
		// planning, replan, acting, verify). The referenced system must
		// resolve; an unavailable system fails the run explicitly as a
		// typed invalid-goal error. The active system is NEVER silently
		// substituted, and an unbound goal (no frozen identity) runs
		// unbound exactly as before.
		var runOpts []agent.RunOption
		if req.SystemID != "" {
			if s.systemsStore() == nil {
				return goal.RunOutcome{}, fmt.Errorf("%w: goal %s references AI system %q but the systems store is unavailable", goal.ErrInvalid, req.GoalID, req.SystemID)
			}
			snap, snapErr := s.resolveGoalSystemSnapshot(req.SystemID, req.SystemRevision)
			if snapErr != nil {
				return goal.RunOutcome{}, fmt.Errorf("%w: goal %s references AI system %q: %v", goal.ErrInvalid, req.GoalID, req.SystemID, snapErr)
			}
			runOpts = append(runOpts, agent.WithAISystem(&snap))
			// v1.10.13 PHANTOM-GATE repair (register 3.4): the
			// documented denial gate is now WIRED — a bound run
			// carries it, an unbound goal runs exactly as before.
			runOpts = append(runOpts, agent.WithApprovalGate(goalApprovalGate{}))
		}

		messages := []llm.Message{{Role: "user", Content: req.Prompt}}
		res, err := s.orch.RunDetailed(ctx, messages,
			func(a agent.Activity) {
				// Goal-loop runs are engine-internal: no session hub is
				// bound, so activities are intentionally not streamed.
				// The durable evidence is the goal document itself.
			},

			// v1.9.0: goal runs install the approval gate — a
			// call whose deterministic risk class requires ask
			// under the goal's AI System policy is DENIED
			// before execution (the denial is evidence; the
			// model adapts to safer approaches). The
			// interactive approval path is the ask-all
			// pre-step parking above; nothing risky ever
			// executes silently.

			runOpts...,
		)
		if err != nil {
			return goal.RunOutcome{}, err
		}

		out := goal.RunOutcome{Text: res.Text}
		if res.Verification.Outcome == agent.VerificationVerified {
			out.Verified = true
		}
		out.VerifyNotes = res.Verification.Summary()
		for i, t := range res.ToolsUsed {
			if i >= 8 {
				break
			}
			out.Evidence = append(out.Evidence, goal.Evidence{
				Phase: req.Phase, Action: "tool used", Target: t, Status: "ok",
			})
		}
		return out, nil
	}
}

// goalRunnerOptions builds the bounded loop options: the pause check
// polls the DURABLE state (the store is the control surface), and the
// approval check parks ask-all systems before every acting step.
func (s *Server) goalRunnerOptions() goal.RunnerOptions {
	return goal.RunnerOptions{
		PauseCheck: func(goalID string) bool {
			g, err := s.goalStore().Get(goalID)
			if err != nil {
				return true // the goal vanished — stop driving
			}
			return g.Status == goal.StatusPaused || g.IsTerminal()
		},
		ApprovalCheck: func(goalID string, stepIdx int) (bool, *goal.PendingApproval) {
			g, err := s.goalStore().Get(goalID)
			if err != nil {
				return false, nil
			}
			// v1.10.13 APPROVAL-ONE-USE (I4) + APPROVAL-IDENTITY: an approved
			// decision is consumed EXACTLY once, for the EXACT canonical action
			// — the step, its tool and its objective as the approval envelope
			// carried them. The recorded grant for THIS identity honors the
			// human decision — the step executes and the grant is cleared
			// atomically under the store lock, so the next drive can never
			// re-park the same approved step. A mismatched identity (e.g. a
			// replanned objective behind an unchanged step index) is a typed
			// rejection with no side effects: the grant survives for the exact
			// action, the mismatch is logged, and the policy below re-parks
			// with a fresh pending call.
			objective := ""
			if stepIdx >= 0 && stepIdx < len(g.Plan) {
				objective = g.Plan[stepIdx].Objective
			}
			granted, grantErr := s.goalStore().TakeApprovedAction(goalID, goal.ActionIdentity{
				StepIdx:   stepIdx,
				ToolName:  "goal.step",
				Objective: objective,
			})
			if grantErr != nil {
				log.Printf("[goal] %s: approval grant not consumed at step %d: %v", goalID, stepIdx, grantErr)
			} else if granted {
				return false, nil
			}
			// The approval POLICY comes from the goal's bound AI System;
			// ask-all parks every acting step (the durable pending call
			// carries the step identity). ask-risky/none defer to the
			// per-tool approval gate inside the run.
			policy := aisystem.ApprovalAskRisky
			if s.systemsStore() != nil {
				if sys, sysErr := s.systemsStore().Get(g.SystemID); sysErr == nil {
					policy = aisystem.NormalizeApproval(sys.ApprovalPolicy)
				}
			}
			if policy != aisystem.ApprovalAskAll {
				return false, nil
			}
			return true, &goal.PendingApproval{
				ApprovalID: fmt.Sprintf("%s-step-%d", g.GoalID, stepIdx),
				ToolName:   "goal.step",
				Args:       map[string]any{"step": stepIdx, "objective": objective},
				Risk:       "workspace-write",
				Reason:     "the AI System's approval policy is ask-all",
			}
		},
	}
}

// startGoalDrive launches the bounded loop in the background. Admission
// is atomic (I1); the goroutine's completion is OBSERVED (I3) and a
// failed drive settles the goal deliberately — never an active goal
// without a live or finishing drive (I6).
func (s *Server) startGoalDrive(goalID string) error {
	store := s.goalStore()
	if store == nil {
		return fmt.Errorf("goal store unavailable")
	}
	g, err := store.Get(goalID)
	if err != nil {
		return err
	}
	if g.IsTerminal() {
		return goal.ErrNotResumable
	}

	ctx, cancel := context.WithCancel(context.Background())
	rec, admitted := goalDriveAdmit(goalID, cancel)
	if !admitted {
		// The unused context is released; the live generation keeps
		// owning the goal. Typed conflict — never a generic 500.
		cancel()
		return fmt.Errorf("%w: goal %s is already being driven", goal.ErrDriveConflict, goalID)
	}

	go func() {
		// I2: cleanup removes ONLY this generation's record, and the
		// done channel closes after the removal — the reservation
		// outlives the goroutine's entire unwinding.
		defer goalDriveRelease(goalID, rec)
		// I3: no discarded outcomes — the result AND error of the drive
		// are observed; a failed drive settles the goal deliberately
		// (the former `_, _ =` discarded both, leaving an active goal
		// with no functioning drive and zero diagnostics).
		_, driveErr := goal.Drive(ctx, store, goalID, s.goalRunFunc(), s.goalRunnerOptions())
		if driveErr != nil {
			s.settleDriveError(goalID, driveErr)
		}
	}()
	return nil
}

// redactDriveDetail bounds and redacts a settlement detail before it
// becomes durable evidence (rule 7 — no secrets in evidence): named-pipe
// endpoints are masked wholesale, key-shaped assignments are masked, and
// the store clamps the length. Plain scanning — no regex, no escaping
// ambiguity at the orchestration boundary.
func redactDriveDetail(detail string) string {
	detail = strings.ReplaceAll(detail, `\\.\pipe\`, `\\.\pipe\<redacted>`)
	detail = maskKeyAssignments(detail)
	if len(detail) > goal.MaxEvidenceDetailLen {
		detail = detail[:goal.MaxEvidenceDetailLen]
	}
	return detail
}

// maskKeyAssignments masks "key=value" / "key: value" shaped whitespace
// separated tokens (case-insensitive over the assignment's key).
func maskKeyAssignments(detail string) string {
	fields := strings.Fields(detail)
	for i, f := range fields {
		low := strings.ToLower(f)
		for _, k := range []string{"key", "token", "secret", "password"} {
			if strings.HasPrefix(low, k+"=") || strings.HasPrefix(low, k+":") {
				fields[i] = k + "=<redacted>"
				break
			}
		}
	}
	return strings.Join(fields, " ")
}

// settleDriveError (I3/I6) is the observed completion path of a failed
// drive: bounded evidence with phase, error category, timestamp and
// redacted detail lands in the durable document, the status moves OFF
// active (blocked — resumable honestly, no fake success, no fake
// failure), and the settled state is emitted into the log. A cancelled
// drive is a CONTROL outcome (pause/cancel/stop): the durable state
// already says why the drive stopped, so only the evidence is recorded.
func (s *Server) settleDriveError(goalID string, driveErr error) {
	category := "run"
	switch {
	case errors.Is(driveErr, context.Canceled):
		category = "cancelled"
	case errors.Is(driveErr, context.DeadlineExceeded):
		category = "deadline"
	}
	store := s.goalStore()
	if store == nil {
		log.Printf("[goal] %s drive %s: store unavailable — error not settled: %v", goalID, category, driveErr)
		return
	}
	g, err := store.Get(goalID)
	if err != nil {
		log.Printf("[goal] %s drive %s: the goal vanished before settlement: %v", goalID, category, driveErr)
		return
	}
	detail := redactDriveDetail(fmt.Sprintf("category=%s phase=%s error=%v", category, g.Phase, driveErr))
	if _, err := store.AppendEvidence(goalID, goal.Evidence{
		Phase:  g.Phase,
		Action: "drive failed",
		Status: "error",
		Detail: detail,
	}); err != nil {
		log.Printf("[goal] %s drive %s: evidence append failed: %v", goalID, category, err)
	}
	if category == "cancelled" {
		log.Printf("[goal] %s drive cancelled at phase %s (status %s)", goalID, g.Phase, g.Status)
		return
	}
	if _, err := store.SettleDriveFailure(goalID, "the drive failed — resume from the checkpoint or cancel"); err != nil {
		log.Printf("[goal] %s drive %s: settlement failed: %v", goalID, category, err)
		return
	}
	log.Printf("[goal] %s drive %s settled at phase %s: status=blocked detail=%s", goalID, category, g.Phase, detail)
}

// resumeAndDrive is the ONE serialized resume path (v1.10.13 RESUME-500
// repair): the previous drive generation must be fully unwound before
// the store flips the goal active and the new generation is admitted —
// one live drive per goal, always (I6). The former code flipped the
// durable status to active, called startGoalDrive, lost the "already
// being driven" race against the old generation's unwinding, and
// answered a generic 500 while the durable store said active with no
// drive. The wait is bounded by goalDriveStopWait (seconds); a drive
// that will not stop in the bound is a typed conflict naming the state.
func (s *Server) resumeAndDrive(w http.ResponseWriter, store *goal.Store, id string) {
	if !goalDriveWaitStop(id, goalDriveStopWait) {
		writeGoalErr(w, fmt.Errorf("%w: goal %s is still stopping — its previous drive generation has not unwound within %s", goal.ErrDriveConflict, id, goalDriveStopWait))
		return
	}
	g, err := store.Resume(id)
	if err != nil {
		writeGoalErr(w, err)
		return
	}
	if err := s.startGoalDrive(id); err != nil {
		// The status flip is compensated: the goal never sits active
		// without a drive (I6). The checkpoint and any armed approval
		// grant survive — an explicit resume re-drives. A FAILED
		// compensating write is structured-logged, never discarded (I3):
		// a silent rollback failure would leave the durable state
		// contradicting the drive registry with zero diagnostics.
		if _, evErr := store.AppendEvidence(id, goal.Evidence{
			Phase:  g.Phase,
			Action: "resume rolled back — the drive could not be admitted",
			Status: "conflict",
			Detail: err.Error(),
		}); evErr != nil {
			log.Printf("[goal] %s: resume rollback: conflict evidence append FAILED: %v", id, evErr)
		}
		if _, pauseErr := store.Pause(id); pauseErr != nil {
			log.Printf("[goal] %s: resume rollback: pause FAILED — the goal may sit active without a live drive (I6): %v", id, pauseErr)
		}
		if _, evErr := store.AppendEvidence(id, goal.Evidence{
			Phase:  g.Phase,
			Action: "goal parked — resume the goal to re-drive it",
			Status: "paused",
		}); evErr != nil {
			log.Printf("[goal] %s: resume rollback: parked evidence append FAILED: %v", id, evErr)
		}
		writeGoalErr(w, err)
		return
	}
	writeJSON(w, g)
}

// resolveGoalSystemSnapshot resolves one goal's frozen AI System
// identity at the run boundary (v1.10.13 SYSTEM-REVISION probe).
// The store serves its CURRENT document; it keeps no revision
// history, so a system revised after the goal was created can never
// re-serve the frozen revision — instead of silently running
// against different instructions, the run boundary fails typed
// (goal.ErrInvalid) and names both revisions. Goals frozen before
// revisions existed (revision 0) run exactly as before.
func (s *Server) resolveGoalSystemSnapshot(systemID string, wantRevision int) (aisystem.Snapshot, error) {
	if s.systemsStore() == nil {
		return aisystem.Snapshot{}, fmt.Errorf("%w: the systems store is unavailable", goal.ErrInvalid)
	}
	snap, err := s.systemsStore().SnapshotOf(systemID)
	if err != nil {
		return aisystem.Snapshot{}, fmt.Errorf("%w: ai system %q is unavailable: %v", goal.ErrInvalid, systemID, err)
	}
	if wantRevision > 0 && snap.SystemRevision != wantRevision {
		return aisystem.Snapshot{}, fmt.Errorf("%w: the goal was created against ai system %q revision %d, but the store now holds revision %d — the frozen-revision contract refuses to run against a revised system; re-create or re-bind the goal", goal.ErrInvalid, systemID, wantRevision, snap.SystemRevision)
	}
	return snap, nil
}

func (s *Server) goalStore() *goal.Store {
	if s.stack == nil {
		return nil
	}
	return s.stack.Goals
}

// handleGoals serves /api/goals.
func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	store := s.goalStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("goals unavailable"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		goals, err := store.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if goals == nil {
			goals = []goal.Goal{}
		}
		writeJSON(w, map[string]any{"goals": goals})

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
		var body struct {
			Goal      string `json:"goal"`
			SessionID string `json:"sessionId,omitempty"`
			SystemID  string `json:"systemId,omitempty"`
			Start     bool   `json:"start,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		// The binding identity: an explicit systemId wins; otherwise the
		// ACTIVE system's frozen identity (the same select-once contract
		// as every run).
		systemID := body.SystemID
		revision := 0
		if systemID == "" && s.systemsStore() != nil {
			if active, err := s.systemsStore().Active(); err == nil {
				systemID = active.SystemID
				revision = active.Revision
			}
		}

		g, err := store.Create(body.Goal, body.SessionID, systemID, revision, "")
		if err != nil {
			writeGoalErr(w, err)
			return
		}
		if body.Start {
			if err := s.startGoalDrive(g.GoalID); err != nil {
				writeGoalErr(w, err)
				return
			}
		}
		writeJSON(w, g)

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// handleGoalItem serves /api/goals/{id} and the action subroutes.
func (s *Server) handleGoalItem(w http.ResponseWriter, r *http.Request) {
	store := s.goalStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("goals unavailable"))
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/goals/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		s.handleGoals(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "" {
		switch r.Method {
		case http.MethodGet:
			g, err := store.Get(id)
			if err != nil {
				writeGoalErr(w, err)
				return
			}
			done, total := g.Progress()
			writeJSON(w, map[string]any{
				"goal":    g,
				"driving": goalDriveLookup(id) != nil,
				"done":    done,
				"total":   total,
			})
		case http.MethodDelete:
			if _, err := store.Cancel(id, "deleted by the user"); err != nil {
				writeGoalErr(w, err)
				return
			}
			goalDriveCancel(id)
			writeJSON(w, map[string]any{"ok": true, "cancelled": id})
		default:
			writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		}
		return
	}

	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	switch action {
	case "start":
		g, err := store.Get(id)
		if err != nil {
			writeGoalErr(w, err)
			return
		}
		if g.Status == goal.StatusPaused || g.Status == goal.StatusBlocked ||
			g.Status == goal.StatusWaitingApproval {
			// start on a parked goal resumes from the checkpoint through
			// the ONE serialized path. waiting_for_approval is NOT a
			// resume source any more — the store's Resume rejects it with
			// a typed error; approve/reject owns that transition.
			s.resumeAndDrive(w, store, id)
			return
		}
		if err := s.startGoalDrive(id); err != nil {
			writeGoalErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "started": id})

	case "pause":
		g, err := store.Pause(id)
		if err != nil {
			writeGoalErr(w, err)
			return
		}
		writeJSON(w, g)

	case "resume":
		// resume re-drives from the checkpoint through the ONE
		// serialized path (the old generation unwinds first — bounded).
		s.resumeAndDrive(w, store, id)

	case "cancel":
		g, err := store.Cancel(id, "cancelled by the user")
		if err != nil {
			writeGoalErr(w, err)
			return
		}
		goalDriveCancel(id)
		writeJSON(w, g)

	case "approve", "reject":
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		var body struct {
			ApprovalID string `json:"approvalId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		decision := "approved"
		if action == "reject" {
			decision = "rejected"
		}
		g, err := store.ResolveApproval(id, body.ApprovalID, decision)
		if err != nil {
			writeGoalErr(w, err)
			return
		}
		// An approved goal resumes the EXACT approved step.
		if decision == "approved" {
			// v1.10.13 SWALLOWED-ERRORS repair: the restart failure is
			// returned and typed — never ignored. The parked generation
			// unwound when the goal parked, but the bounded wait keeps
			// the admission honest under automation-speed callers.
			if !goalDriveWaitStop(id, goalDriveStopWait) {
				writeGoalErr(w, fmt.Errorf("%w: goal %s is still stopping — the approved decision is armed; resume once the previous drive generation has unwound", goal.ErrDriveConflict, id))
				return
			}
			if err := s.startGoalDrive(id); err != nil {
				// The approved decision stays durable (evidence + ONE-USE
				// grant armed); the goal is parked paused so it never sits
				// active without a drive (I6) — an explicit resume
				// executes the approved step. Failed compensations are
				// structured-logged, never discarded (I3).
				if _, evErr := store.AppendEvidence(id, goal.Evidence{
					Phase:  g.Phase,
					Action: "approved decision armed — the drive restart failed; resume to execute the approved step",
					Status: "conflict",
					Detail: err.Error(),
				}); evErr != nil {
					log.Printf("[goal] %s: approve rollback: conflict evidence append FAILED: %v", id, evErr)
				}
				if _, pauseErr := store.Pause(id); pauseErr != nil {
					log.Printf("[goal] %s: approve rollback: pause FAILED — the goal may sit active without a live drive (I6): %v", id, pauseErr)
				}
				writeGoalErr(w, err)
				return
			}
		}
		writeJSON(w, g)

	default:
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown goal action %q", action))
	}
}

// writeGoalErr maps the goal package's typed errors onto the HTTP
// contract. Sentinel identity is matched with errors.Is — a wrapped
// sentinel IS the sentinel; sentinel identity is never string-matched
// (the former strings.Contains matched an error whose TEXT happened to
// embed another error's text).
func writeGoalErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, goal.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, goal.ErrDriveConflict):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, goal.ErrNotResumable):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, goal.ErrTooMany):
		writeErr(w, http.StatusInsufficientStorage, err)
	case errors.Is(err, goal.ErrInvalid):
		writeErr(w, http.StatusBadRequest, err)
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
}

// goalApprovalGate (v1.9.0, wired v1.10.13) is the goal-run approval
// decision: every call that reached the gate already required ask under
// the run's policy (the orchestrator consults the gate only after
// approval.RequiresAsk), so the verdict is a documented denial — no
// risky call executes without an explicit human decision somewhere in
// the lifecycle. The denial is evidence — it travels back to the model
// as the tool result and the model adapts; it is never a run error.
type goalApprovalGate struct{}

func (goalApprovalGate) Evaluate(req agent.ApprovalRequest) agent.ApprovalDecision {
	return agent.ApprovalDecision{
		Approved: false,
		Reason: fmt.Sprintf("the goal's AI System approval policy %q requires ask for %s calls: approve the parked goal step through the goals surface, or rebind the goal to a narrower approval policy",
			req.Policy, req.Risk),
	}
}
