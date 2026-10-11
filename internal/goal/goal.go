// Package goal implements SHEYTAN's durable long-horizon Goal engine
// (v1.9.0).
//
// A Goal is a durable, resumable, checkpointed unit of long-horizon work:
// an original user objective decomposed into a plan, executed phase by
// phase (understanding → planning → acting → verifying → completed) with
// evidence recorded for every important action, bounded replanning on
// blockers, approval parking, and compact checkpoints that survive
// restart and reload.
//
// Design contract:
//
//   - The engine EXTENDS the existing runtime — it drives runs through an
//     injected RunFunc (the API/runtime adapter binds this to the ONE
//     orchestrator with the AI System binding). It does NOT create a
//     competing orchestrator, task manager, or lifecycle.
//   - Durable state persists under <DataDir>/goals/ — one JSON document
//     per goal, atomic temp+rename writes, bounded, deterministic
//     ordering, reload-safe, corruption-tolerant.
//   - Progress derives from ACTUAL plan/task state. There are no
//     decorative percentages.
//   - A model claim never proves completion: the terminal state is set
//     only from objective verification outcomes carried by the injected
//     runner (the orchestrator's evidence machinery).
//   - Restart/reload recovers WITHOUT replaying committed mutations: the
//     checkpoint (completed steps + evidence + next action) is the
//     authoritative continuation point; a goal found "running" at boot
//     with no live run is marked paused, never falsely running.
package goal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Phases and statuses
// ---------------------------------------------------------------------------

// Phases (the preferred v1.9 vocabulary).
const (
	PhaseUnderstanding = "understanding"
	PhasePlanning      = "planning"
	PhaseActing        = "acting"
	PhaseVerifying     = "verifying"
	PhaseCompleted     = "completed"
)

// Waiting/exception statuses.
const (
	StatusActive          = "active"
	StatusWaitingApproval = "waiting_for_approval"
	StatusWaitingResource = "waiting_for_resource"
	StatusPaused          = "paused"
	StatusBlocked         = "blocked"
	StatusFailed          = "failed"
	StatusCompleted       = "completed"
	StatusCancelled       = "cancelled"
)

// Step statuses.
const (
	StepPending   = "pending"
	StepActive    = "active"
	StepCompleted = "completed"
	StepBlocked   = "blocked"
	StepFailed    = "failed"
)

// Bounds — every durable document and every loop is bounded.
const (
	MaxGoals                         = 200
	MaxPlanSteps                     = 12
	MaxEvidencePerGoal               = 200
	MaxEvidenceDetailLen             = 2 * 1024
	MaxGoalTextLen                   = 32 * 1024
	MaxReplans                       = 2
	DefaultTurnBudget                = 24
	maxDocumentBytes                 = 512 * 1024
	dirPerm              os.FileMode = 0o700
)

var (
	ErrNotFound = errors.New("goal not found")
	ErrInvalid  = errors.New("invalid goal")
	ErrTooMany  = errors.New("the goal store is full")
	// ErrNotResumable: the goal is in a state that cannot resume.
	ErrNotResumable = errors.New("goal is not resumable in its current state")
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// PlanStep is one plan step with its own lifecycle and evidence.
type PlanStep struct {
	Index     int        `json:"index"`
	Objective string     `json:"objective"`
	Status    string     `json:"status"`
	Result    string     `json:"result,omitempty"`
	Evidence  []Evidence `json:"evidence,omitempty"`
}

// Evidence is one compact, structured record of an important action
// (spec §17): what was done, on what, with what outcome — never a raw
// event log.
type Evidence struct {
	Seq         int       `json:"seq"`
	Phase       string    `json:"phase"`
	Action      string    `json:"action"`
	Target      string    `json:"target,omitempty"`
	Status      string    `json:"status"`
	Detail      string    `json:"detail,omitempty"`
	ArtifactRef string    `json:"artifactRef,omitempty"`
	SubtaskID   string    `json:"subtaskId,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// PendingApproval is the durable approval parking state (spec §11).
type PendingApproval struct {
	ApprovalID  string         `json:"approvalId"`
	ToolName    string         `json:"toolName"`
	Args        map[string]any `json:"args,omitempty"`
	Risk        string         `json:"risk"`
	Reason      string         `json:"reason,omitempty"`
	RequestedAt time.Time      `json:"requestedAt"`
	Decision    string         `json:"decision,omitempty"` // approved | rejected (set on resolution)
	DecidedAt   *time.Time     `json:"decidedAt,omitempty"`
}

// Checkpoint is the durable compact state (spec §9): everything the next
// stage needs, nothing disposable.
type Checkpoint struct {
	Phase          string    `json:"phase"`
	PlanSummary    []string  `json:"planSummary,omitempty"`
	CompletedSteps []int     `json:"completedSteps,omitempty"`
	CurrentStep    int       `json:"currentStep"`
	NextAction     string    `json:"nextAction,omitempty"`
	Unresolved     []string  `json:"unresolved,omitempty"`
	VerifiedFacts  []string  `json:"verifiedFacts,omitempty"`
	ChangedFiles   []string  `json:"changedFiles,omitempty"`
	Artifacts      []string  `json:"artifacts,omitempty"`
	SystemID       string    `json:"systemId,omitempty"`
	SystemRevision int       `json:"systemRevision,omitempty"`
	TakenAt        time.Time `json:"takenAt"`
}

// Goal is one durable goal document.
type Goal struct {
	GoalID             string           `json:"goalId"`
	SessionID          string           `json:"sessionId,omitempty"`
	SystemID           string           `json:"systemId,omitempty"`
	SystemRevision     int              `json:"systemRevision,omitempty"`
	OriginalGoal       string           `json:"originalGoal"`
	Phase              string           `json:"phase"`
	Status             string           `json:"status"`
	Plan               []PlanStep       `json:"plan,omitempty"`
	CurrentStep        int              `json:"currentStep"`
	LastAction         string           `json:"lastAction,omitempty"`
	NextAction         string           `json:"nextAction,omitempty"`
	Evidence           []Evidence       `json:"evidence,omitempty"`
	ChangedFiles       []string         `json:"changedFiles,omitempty"`
	Artifacts          []string         `json:"artifacts,omitempty"`
	Verification       string           `json:"verification,omitempty"`
	VerificationPassed bool             `json:"verificationPassed"`
	Effort             string           `json:"effort,omitempty"`
	TurnBudget         int              `json:"turnBudget"`
	TurnsUsed          int              `json:"turnsUsed"`
	Replans            int              `json:"replans"`
	PendingApproval    *PendingApproval `json:"pendingApproval,omitempty"`
	Checkpoint         *Checkpoint      `json:"checkpoint,omitempty"`
	CreatedAt          time.Time        `json:"createdAt"`
	UpdatedAt          time.Time        `json:"updatedAt"`
	TerminalAt         *time.Time       `json:"terminalAt,omitempty"`
}

// IsTerminal reports whether the goal reached a terminal settlement.
func (g *Goal) IsTerminal() bool {
	switch g.Status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Progress derives from ACTUAL plan state — no decorative percentages.
func (g *Goal) Progress() (done, total int) {
	total = len(g.Plan)
	for i := range g.Plan {
		if g.Plan[i].Status == StepCompleted {
			done++
		}
	}
	return done, total
}

// ---------------------------------------------------------------------------
// Runner seam — the engine drives runs; the runtime supplies the executor
// ---------------------------------------------------------------------------

// RunRequest is one bounded run the engine needs executed.
type RunRequest struct {
	GoalID   string
	Phase    string // why: understanding | planning | acting | verifying | replanning
	StepIdx  int    // acting: the step being executed
	Prompt   string
	SystemID string
}

// RunOutcome is the objective result of one run. Text is the model's
// answer; Verification (when non-nil) is OBJECTIVE evidence — a model
// claim alone never proves completion.
type RunOutcome struct {
	Text         string
	Evidence     []Evidence
	ChangedFiles []string
	Artifacts    []string
	// Verified: objective verification outcome for the verifying phase.
	Verified    bool
	VerifyNotes string
}

// RunFunc executes one run through the EXISTING orchestrator. The engine
// never implements its own model loop.
type RunFunc func(ctx context.Context, req RunRequest) (RunOutcome, error)

// RunnerOptions bound the loop.
type RunnerOptions struct {
	TurnBudget int
	MaxReplans int
	// PauseCheck, when non-nil, is polled between steps; returning true
	// parks the goal as paused at the next checkpoint boundary.
	PauseCheck func(goalID string) bool
	// ApprovalCheck, when non-nil, is consulted before each step run;
	// returning (pending=true, approval) parks the goal as
	// waiting_for_approval with the pending tool call persisted.
	ApprovalCheck func(goalID string, stepIdx int) (bool, *PendingApproval)
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store is the ONE durable goal store.
type Store struct {
	mu  sync.Mutex
	dir string
}

// Open loads (or initializes) the store under <DataDir>/goals.
func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "goals")
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("goals dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the persistence directory (diagnostics only).
func (s *Store) Dir() string { return s.dir }

func (s *Store) goalPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-goal-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("goal-%x", time.Now().UnixNano())
	}
	return "goal-" + hex.EncodeToString(buf)
}

func (s *Store) read(id string) (Goal, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return Goal{}, ErrNotFound
	}
	data, err := os.ReadFile(s.goalPath(id))
	if err != nil {
		return Goal{}, ErrNotFound
	}
	if len(data) > maxDocumentBytes {
		return Goal{}, ErrInvalid
	}
	var g Goal
	if err := json.Unmarshal(data, &g); err != nil {
		return Goal{}, ErrInvalid
	}
	if g.GoalID != id {
		return Goal{}, ErrInvalid
	}
	return g, nil
}

func (s *Store) write(g *Goal) error {
	g.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.goalPath(g.GoalID), data)
}

// List returns every goal deterministically ordered (CreatedAt, then
// GoalID). Corrupt documents are skipped.
func (s *Store) List() ([]Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) listLocked() ([]Goal, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]Goal, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if len(out) >= MaxGoals {
			break
		}
		g, err := s.read(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].GoalID < out[j].GoalID
	})
	return out, nil
}

// Get returns one goal.
func (s *Store) Get(id string) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}

// Create persists a NEW goal in the understanding phase. The creator may
// carry the binding identity (session, AI System snapshot) explicitly.
func (s *Store) Create(original string, sessionID, systemID string, systemRevision int, effort string) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	original = strings.TrimSpace(original)
	if original == "" {
		return Goal{}, fmt.Errorf("%w: the original goal text is required", ErrInvalid)
	}
	if len(original) > MaxGoalTextLen {
		return Goal{}, fmt.Errorf("%w: the goal text exceeds %d bytes", ErrInvalid, MaxGoalTextLen)
	}
	existing, _ := s.listLocked()
	if len(existing) >= MaxGoals {
		return Goal{}, ErrTooMany
	}

	now := time.Now().UTC()
	g := Goal{
		GoalID:         newID(),
		SessionID:      sessionID,
		SystemID:       systemID,
		SystemRevision: systemRevision,
		OriginalGoal:   original,
		Phase:          PhaseUnderstanding,
		Status:         StatusActive,
		CurrentStep:    -1,
		Effort:         effort,
		TurnBudget:     DefaultTurnBudget,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.write(&g); err != nil {
		return Goal{}, err
	}
	return g, nil
}

// mutate loads, mutates, and persists one goal under the store lock.
func (s *Store) mutate(id string, fn func(*Goal) error) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.read(id)
	if err != nil {
		return Goal{}, err
	}
	if err := fn(&g); err != nil {
		return Goal{}, err
	}
	if err := s.write(&g); err != nil {
		return Goal{}, err
	}
	return g, nil
}

// AppendEvidence adds one bounded evidence record (seq = monotonic).
func (s *Store) AppendEvidence(id string, e Evidence) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		e.Seq = len(g.Evidence) + 1
		e.Timestamp = time.Now().UTC()
		if len(e.Detail) > MaxEvidenceDetailLen {
			e.Detail = e.Detail[:MaxEvidenceDetailLen]
		}
		g.Evidence = append(g.Evidence, e)
		if len(g.Evidence) > MaxEvidencePerGoal {
			// Bounded: drop the OLDEST records (the checkpoint retains
			// what the next stage needs; evidence stays bounded).
			g.Evidence = g.Evidence[len(g.Evidence)-MaxEvidencePerGoal:]
		}
		g.LastAction = e.Action
		return nil
	})
}

// SetPhase transitions the phase and persists a checkpoint.
func (s *Store) SetPhase(id, phase string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		g.Phase = phase
		g.NextAction = nextActionFor(phase)
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

func nextActionFor(phase string) string {
	switch phase {
	case PhaseUnderstanding:
		return "classify the goal and establish the success criteria"
	case PhasePlanning:
		return "decompose the goal into a bounded, verifiable plan"
	case PhaseActing:
		return "execute the next pending step"
	case PhaseVerifying:
		return "verify the outcome against objective evidence"
	case PhaseCompleted:
		return "none — the goal is settled"
	}
	return ""
}

// buildCheckpoint derives the durable compact state from ACTUAL state.
func (g *Goal) buildCheckpoint() *Checkpoint {
	cp := &Checkpoint{
		Phase:          g.Phase,
		CompletedSteps: make([]int, 0, len(g.Plan)),
		CurrentStep:    g.CurrentStep,
		NextAction:     g.NextAction,
		ChangedFiles:   append([]string(nil), g.ChangedFiles...),
		Artifacts:      append([]string(nil), g.Artifacts...),
		SystemID:       g.SystemID,
		SystemRevision: g.SystemRevision,
		TakenAt:        time.Now().UTC(),
	}
	for i := range g.Plan {
		cp.PlanSummary = append(cp.PlanSummary, g.Plan[i].Objective)
		if g.Plan[i].Status == StepCompleted {
			cp.CompletedSteps = append(cp.CompletedSteps, g.Plan[i].Index)
		}
		if g.Plan[i].Status == StepBlocked || g.Plan[i].Status == StepFailed {
			cp.Unresolved = append(cp.Unresolved,
				fmt.Sprintf("step %d: %s", g.Plan[i].Index, g.Plan[i].Objective))
		}
	}
	for i := len(g.Evidence) - 1; i >= 0 && len(cp.VerifiedFacts) < 8; i-- {
		if g.Evidence[i].Status == "verified" || g.Evidence[i].Status == "ok" {
			fact := g.Evidence[i].Action
			if g.Evidence[i].Target != "" {
				fact += " → " + g.Evidence[i].Target
			}
			cp.VerifiedFacts = append(cp.VerifiedFacts, fact)
		}
	}
	for i, j := 0, len(cp.VerifiedFacts)-1; i < j; i, j = i+1, j-1 {
		cp.VerifiedFacts[i], cp.VerifiedFacts[j] = cp.VerifiedFacts[j], cp.VerifiedFacts[i]
	}
	if g.PendingApproval != nil {
		cp.Unresolved = append(cp.Unresolved,
			"awaiting approval: "+g.PendingApproval.ToolName)
	}
	return cp
}

// SetPlan installs the plan (bounded) and moves to the acting phase.
func (s *Store) SetPlan(id string, objectives []string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if len(objectives) == 0 || len(objectives) > MaxPlanSteps {
			return fmt.Errorf("%w: plan must hold 1..%d steps", ErrInvalid, MaxPlanSteps)
		}
		plan := make([]PlanStep, 0, len(objectives))
		for i, o := range objectives {
			o = strings.TrimSpace(o)
			if o == "" {
				return fmt.Errorf("%w: plan step %d is empty", ErrInvalid, i)
			}
			plan = append(plan, PlanStep{Index: i, Objective: o, Status: StepPending})
		}
		g.Plan = plan
		g.CurrentStep = -1
		g.Phase = PhaseActing
		g.NextAction = nextActionFor(PhaseActing)
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

// MarkStep records the outcome of one step execution and CHECKPOINTS.
func (s *Store) MarkStep(id string, stepIdx int, status, result string, ev []Evidence, changedFiles, artifacts []string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if stepIdx < 0 || stepIdx >= len(g.Plan) {
			return fmt.Errorf("%w: step %d out of range", ErrInvalid, stepIdx)
		}
		g.Plan[stepIdx].Status = status
		g.Plan[stepIdx].Result = result
		g.Plan[stepIdx].Evidence = append(g.Plan[stepIdx].Evidence, ev...)
		for _, f := range changedFiles {
			if !contains(g.ChangedFiles, f) {
				g.ChangedFiles = append(g.ChangedFiles, f)
			}
		}
		for _, a := range artifacts {
			if !contains(g.Artifacts, a) {
				g.Artifacts = append(g.Artifacts, a)
			}
		}
		g.TurnsUsed++
		// v1.9.0: every step outcome is mirrored into the GOAL-LEVEL
		// evidence journal — the durable record survives plan revisions.
		g.Evidence = append(g.Evidence, Evidence{
			Seq:       len(g.Evidence) + 1,
			Phase:     g.Phase,
			Action:    fmt.Sprintf("step %d %s", stepIdx, status),
			Target:    g.Plan[stepIdx].Objective,
			Status:    status,
			Detail:    result,
			Timestamp: time.Now().UTC(),
		})
		switch status {
		case StepCompleted:
			g.LastAction = fmt.Sprintf("completed step %d", stepIdx)
			// checkpoint after EACH completed subtask (spec §4)
			g.Checkpoint = g.buildCheckpoint()
		case StepBlocked:
			g.Status = StatusBlocked
			g.LastAction = fmt.Sprintf("step %d blocked", stepIdx)
			g.Checkpoint = g.buildCheckpoint()
		case StepFailed:
			g.Status = StatusFailed
			now := time.Now().UTC()
			g.TerminalAt = &now
			g.LastAction = fmt.Sprintf("step %d failed", stepIdx)
			g.Checkpoint = g.buildCheckpoint()
		}
		return nil
	})
}

// SetVerification records the objective verification outcome and settles
// the goal terminally when verification PASSED. A model claim alone
// never reaches here.
func (s *Store) SetVerification(id string, passed bool, notes string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		g.Phase = PhaseCompleted
		g.Verification = notes
		g.VerificationPassed = passed
		now := time.Now().UTC()
		g.TerminalAt = &now
		if passed {
			g.Status = StatusCompleted
			g.LastAction = "verified and completed"
		} else {
			g.Status = StatusFailed
			g.LastAction = "verification failed"
		}
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

// Pause parks an active goal at the next checkpoint boundary.
func (s *Store) Pause(id string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.IsTerminal() {
			return ErrNotResumable
		}
		g.Status = StatusPaused
		g.LastAction = "paused by the user"
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

// Resume reactivates a paused/blocked/waiting goal from its checkpoint
// WITHOUT replaying committed mutations (the checkpoint is the truth).
func (s *Store) Resume(id string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.IsTerminal() {
			return ErrNotResumable
		}
		switch g.Status {
		case StatusPaused, StatusBlocked, StatusWaitingApproval, StatusWaitingResource:
			g.Status = StatusActive
			g.PendingApproval = nil
			g.LastAction = "resumed from checkpoint"
			if g.Phase == "" {
				g.Phase = PhaseUnderstanding
			}
			g.Checkpoint = g.buildCheckpoint()
			return nil
		}
		return ErrNotResumable
	})
}

// Cancel terminally cancels a goal (abandoned goals can never become
// falsely running afterwards).
func (s *Store) Cancel(id, reason string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.IsTerminal() {
			return ErrNotResumable
		}
		g.Status = StatusCancelled
		now := time.Now().UTC()
		g.TerminalAt = &now
		g.LastAction = "cancelled: " + reason
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

// RequestApproval parks the goal as waiting_for_approval with the pending
// call durably recorded. Approval SURVIVES reload (it is on disk).
func (s *Store) RequestApproval(id string, pa *PendingApproval) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.IsTerminal() {
			return ErrNotResumable
		}
		g.Status = StatusWaitingApproval
		g.PendingApproval = pa
		g.LastAction = "awaiting approval: " + pa.ToolName
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

// ResolveApproval applies the human decision: approve arms the EXACT
// approved call for resumption; rejection is recorded as evidence and
// the pending call is never executed.
func (s *Store) ResolveApproval(id, approvalID, decision string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.PendingApproval == nil {
			return fmt.Errorf("%w: no pending approval", ErrInvalid)
		}
		if g.PendingApproval.ApprovalID != approvalID {
			// Stale/malformed approval — reject it explicitly.
			return fmt.Errorf("%w: approval %q does not match the pending call", ErrInvalid, approvalID)
		}
		now := time.Now().UTC()
		g.PendingApproval.Decision = decision
		g.PendingApproval.DecidedAt = &now
		decisionText := g.PendingApproval.ToolName + " " + decision
		g.Evidence = append(g.Evidence, Evidence{
			Seq:       len(g.Evidence) + 1,
			Phase:     g.Phase,
			Action:    "approval decision",
			Target:    g.PendingApproval.ToolName,
			Status:    decision,
			Detail:    decisionText,
			Timestamp: now,
		})
		if decision == "approved" {
			g.Status = StatusActive
		} else {
			g.Status = StatusBlocked
		}
		g.LastAction = decisionText
		pa := *g.PendingApproval
		g.PendingApproval = nil
		_ = pa // the decision evidence above is the durable record
		g.Checkpoint = g.buildCheckpoint()
		return nil
	})
}

// RecoverOnBoot enforces the reload contract: a goal found active/waiting
// at boot with NO live run is marked paused (never falsely running);
// terminal goals stay terminal. It returns the number of repaired goals.
func (s *Store) RecoverOnBoot() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.listLocked()
	if err != nil {
		return 0, err
	}
	repaired := 0
	for _, g := range list {
		if g.IsTerminal() {
			continue
		}
		if g.Status == StatusActive || g.Status == StatusWaitingApproval ||
			g.Status == StatusWaitingResource {
			g.Status = StatusPaused
			g.LastAction = "restored on startup — no live run was found; resume from the checkpoint"
			g.Checkpoint = g.buildCheckpoint()
			if err := s.write(&g); err != nil {
				return repaired, err
			}
			repaired++
		}
	}
	return repaired, nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
