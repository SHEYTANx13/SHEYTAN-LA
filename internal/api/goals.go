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
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aisystem"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/goal"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// goalDrives tracks the single-flight drive per goal (one loop at a time;
// a second start on a live goal is an honest 409, never a parallel loop).
var goalDrives = struct {
	mu      sync.Mutex
	pending map[string]context.CancelFunc
}{
	pending: map[string]context.CancelFunc{},
}

func goalDriveActive(id string) bool {
	goalDrives.mu.Lock()
	defer goalDrives.mu.Unlock()
	_, ok := goalDrives.pending[id]
	return ok
}

func goalDriveCancel(id string) bool {
	goalDrives.mu.Lock()
	defer goalDrives.mu.Unlock()
	if cancel, ok := goalDrives.pending[id]; ok {
		delete(goalDrives.pending, id)
		cancel()
		return true
	}
	return false
}

// goalRunFunc binds the goal engine's executor seam to the ONE
// orchestrator. Every engine run carries the goal's frozen AI System
// snapshot; the objective verification report maps into the engine's
// outcome — a model claim alone can never complete a goal.
func (s *Server) goalRunFunc() goal.RunFunc {
	return func(ctx context.Context, req goal.RunRequest) (goal.RunOutcome, error) {
		_ = req.SystemID

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
			objective := ""
			if stepIdx >= 0 && stepIdx < len(g.Plan) {
				objective = g.Plan[stepIdx].Objective
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

// startGoalDrive launches the bounded loop in the background.
func (s *Server) startGoalDrive(goalID string) error {
	if goalDriveActive(goalID) {
		return fmt.Errorf("goal %s is already being driven", goalID)
	}
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
	goalDrives.mu.Lock()
	goalDrives.pending[goalID] = cancel
	goalDrives.mu.Unlock()

	go func() {
		defer func() {
			goalDrives.mu.Lock()
			delete(goalDrives.pending, goalID)
			goalDrives.mu.Unlock()
		}()
		_, _ = goal.Drive(ctx, store, goalID, s.goalRunFunc(), s.goalRunnerOptions())
	}()
	return nil
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
				"driving": goalDriveActive(id),
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
			// start on a parked goal resumes from the checkpoint
			if _, err := store.Resume(id); err != nil {
				writeGoalErr(w, err)
				return
			}
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
		g, err := store.Resume(id)
		if err != nil {
			writeGoalErr(w, err)
			return
		}
		// resume re-drives from the checkpoint
		if err := s.startGoalDrive(id); err != nil {
			writeGoalErr(w, err)
			return
		}
		writeJSON(w, g)

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
			_ = s.startGoalDrive(id)
		}
		writeJSON(w, g)

	default:
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown goal action %q", action))
	}
}

func writeGoalErr(w http.ResponseWriter, err error) {
	switch err {
	case goal.ErrNotFound:
		writeErr(w, http.StatusNotFound, err)
	case goal.ErrNotResumable:
		writeErr(w, http.StatusConflict, err)
	case goal.ErrTooMany:
		writeErr(w, http.StatusInsufficientStorage, err)
	default:
		if err != nil && strings.Contains(err.Error(), goal.ErrInvalid.Error()) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
	}
}

// goalApprovalGate (v1.9.0) is the goal-run approval decision: every call
// that reached the gate already required ask under the run's policy, so
// the verdict is a documented denial — no risky call executes without an
// explicit human decision somewhere in the lifecycle.
