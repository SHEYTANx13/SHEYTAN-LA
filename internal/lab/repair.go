package lab

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrRepairMaxIterations indicates that the bounded repair budget was
// exhausted before the workspace passed independent verification.
var ErrRepairMaxIterations = errors.New(
	"lab: repair loop reached maximum iterations",
)

// ErrRepairRepeatedCommand prevents an autonomous controller from retrying the
// exact same command indefinitely without making progress.
var ErrRepairRepeatedCommand = errors.New(
	"lab: repair loop repeated the same command without progress",
)

// ErrRepairNoAction indicates that the repair agent could not produce a
// concrete, executable repair action.
var ErrRepairNoAction = errors.New(
	"lab: repair agent produced no action",
)

// RepairDecision is the bounded repair-agent output for one iteration.
//
// The controller intentionally receives one concrete command at a time.
// Filesystem, shell, policy, timeout, environment, and process isolation still
// remain under TaskManager/Runner control.
type RepairDecision struct {
	Command     Command
	Explanation string
}

// RepairAgent chooses the next repair action after an independent verification
// failure.
//
// An LLM-backed implementation can inspect the task workspace, diagnostics,
// and previous repair history through the application's normal tool system.
// Returning an empty command terminates the loop safely.
type RepairAgent interface {
	Repair(
		ctx context.Context,
		task *Task,
		verification VerificationSummary,
		history []RepairIteration,
	) (RepairDecision, error)
}

// RepairIteration records one bounded controller iteration.
type RepairIteration struct {
	Iteration    int                  `json:"iteration"`
	Decision     RepairDecision       `json:"decision"`
	Result       CommandResult        `json:"result"`
	Verification *VerificationSummary `json:"verification,omitempty"`
	Error        string               `json:"error,omitempty"`
	StartedAt    time.Time            `json:"startedAt"`
	FinishedAt   time.Time            `json:"finishedAt"`
}

// RepairSummary describes the complete bounded repair run.
type RepairSummary struct {
	Passed        bool                 `json:"passed"`
	Iterations    int                  `json:"iterations"`
	MaxIterations int                  `json:"maxIterations"`
	History       []RepairIteration    `json:"history"`
	Verification  *VerificationSummary `json:"verification,omitempty"`
	Error         string               `json:"error,omitempty"`
	StartedAt     time.Time            `json:"startedAt"`
	FinishedAt    time.Time            `json:"finishedAt"`
	Duration      time.Duration        `json:"duration"`
}

// RepairController owns bounded autonomous repair for one Coding Lab task.
//
// The controller deliberately uses VerifyNative rather than caller-selected
// verification commands. This prevents an autonomous repair agent from making
// its own success criterion trivially pass.
type RepairController struct {
	Tasks    *TaskManager
	Verifier *Verifier

	// MaxIterations <= 0 means the controller default of 25.
	// Values above 100 are always clamped to 100.
	MaxIterations int
}

// NewRepairController creates a bounded repair controller.
func NewRepairController(
	tasks *TaskManager,
	verifier *Verifier,
	maxIterations int,
) (*RepairController, error) {
	if tasks == nil {
		return nil, errors.New("lab: repair task manager is nil")
	}

	if verifier == nil {
		return nil, errors.New("lab: repair verifier is nil")
	}

	if maxIterations <= 0 {
		maxIterations = 25
	}

	if maxIterations > 100 {
		maxIterations = 100
	}

	return &RepairController{
		Tasks:         tasks,
		Verifier:      verifier,
		MaxIterations: maxIterations,
	}, nil
}

// Run executes bounded autonomous repair.
//
// Each iteration follows this sequence:
//
//  1. independently verify the current workspace;
//  2. stop immediately if verification passes;
//  3. ask the repair agent for one concrete command;
//  4. reject an empty action;
//  5. reject repeated identical commands after two executions;
//  6. execute the repair through TaskManager so policy and process controls
//     remain authoritative;
//  7. continue until verification passes or the iteration budget is exhausted.
//
// Verification does not consume repair iterations. The maximum number of
// repair commands is therefore exactly MaxIterations.
func (c *RepairController) Run(
	ctx context.Context,
	task *Task,
	agent RepairAgent,
) (RepairSummary, error) {
	started := time.Now().UTC()

	summary := RepairSummary{
		MaxIterations: c.effectiveMaxIterations(),
		History:       make([]RepairIteration, 0, c.effectiveMaxIterations()),
		StartedAt:     started,
	}

	finish := func(err error) (RepairSummary, error) {
		finished := time.Now().UTC()

		summary.FinishedAt = finished
		summary.Duration = finished.Sub(started)

		if err != nil {
			summary.Error = err.Error()
			summary.Passed = false
		}

		return summary, err
	}

	if c == nil ||
		c.Tasks == nil ||
		c.Verifier == nil {
		return finish(errors.New("lab: repair controller is not configured"))
	}

	if task == nil {
		return finish(ErrTaskInvalid)
	}

	if task.Status != TaskRunning {
		return finish(fmt.Errorf(
			"%w: current status=%s",
			ErrTaskNotRunnable,
			task.Status,
		))
	}

	if task.Workspace == nil {
		return finish(ErrInvalidWorkspace)
	}

	if agent == nil {
		return finish(errors.New("lab: repair agent is nil"))
	}

	if ctx == nil {
		ctx = context.Background()
	}

	seenCommands := make(map[string]int)

	for iteration := 1; iteration <= summary.MaxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}

		verification, verifyErr := c.Verifier.VerifyNative(
			ctx,
			task,
		)

		summary.Verification = &verification

		if verifyErr == nil && verification.Passed {
			summary.Passed = true
			summary.Iterations = iteration - 1

			return finish(nil)
		}

		historySnapshot := append(
			[]RepairIteration(nil),
			summary.History...,
		)

		decisionStarted := time.Now().UTC()

		decision, decisionErr := agent.Repair(
			ctx,
			task,
			verification,
			historySnapshot,
		)

		entry := RepairIteration{
			Iteration: iteration,
			Decision:  decision,
			StartedAt: decisionStarted,
		}

		if decisionErr != nil {
			entry.Error = decisionErr.Error()
			entry.FinishedAt = time.Now().UTC()

			summary.History = append(
				summary.History,
				entry,
			)
			summary.Iterations = iteration

			return finish(decisionErr)
		}

		commandText := strings.TrimSpace(decision.Command.Command)

		if commandText == "" {
			entry.Error = ErrRepairNoAction.Error()
			entry.FinishedAt = time.Now().UTC()

			summary.History = append(
				summary.History,
				entry,
			)
			summary.Iterations = iteration

			return finish(ErrRepairNoAction)
		}

		commandKey := normalizeRepairCommand(decision.Command)

		seenCommands[commandKey]++

		// Two executions are sufficient to distinguish a transient failure
		// from a repair strategy that is making no progress. A third
		// identical execution is rejected deterministically — unless this
		// is the final permitted iteration, where the budget cap itself
		// terminates the loop and takes error precedence.
		if seenCommands[commandKey] > 2 &&
			iteration < summary.MaxIterations {
			entry.Error = ErrRepairRepeatedCommand.Error()
			entry.FinishedAt = time.Now().UTC()

			summary.History = append(
				summary.History,
				entry,
			)
			summary.Iterations = iteration

			return finish(
				fmt.Errorf(
					"%w: %q",
					ErrRepairRepeatedCommand,
					commandText,
				),
			)
		}

		result, runErr := c.Tasks.RunCommand(
			ctx,
			task,
			decision.Command,
		)

		entry.Result = result
		entry.FinishedAt = time.Now().UTC()

		if runErr != nil {
			entry.Error = runErr.Error()
		}

		summary.History = append(
			summary.History,
			entry,
		)
		summary.Iterations = iteration

		// RunCommand invalidates the task verification state before execution,
		// so a failed repair can never leave a stale successful verification
		// attached to the task.
		if runErr != nil && result.Success {
			// Defensive consistency guard for unusual runner implementations.
			return finish(runErr)
		}
	}

	// The final native verification is deliberately performed after the last
	// permitted repair command. Thus a repair that succeeds on iteration N is
	// still recognized without allowing iteration N+1 to execute.
	if err := ctx.Err(); err != nil {
		return finish(err)
	}

	finalVerification, finalErr := c.Verifier.VerifyNative(
		ctx,
		task,
	)

	summary.Verification = &finalVerification

	if finalErr == nil && finalVerification.Passed {
		summary.Passed = true
		return finish(nil)
	}

	return finish(ErrRepairMaxIterations)
}

// effectiveMaxIterations applies the controller's hard safety bound.
func (c *RepairController) effectiveMaxIterations() int {
	if c == nil || c.MaxIterations <= 0 {
		return 25
	}

	if c.MaxIterations > 100 {
		return 100
	}

	return c.MaxIterations
}

func normalizeRepairCommand(command Command) string {
	return strings.Join(
		[]string{
			strings.TrimSpace(command.Command),
			strings.TrimSpace(command.WorkingDir),
			strings.Join(command.Environment, "\x00"),
			fmt.Sprintf("%d", command.Timeout),
			fmt.Sprintf("%d", command.MaxOutputBytes),
		},
		"\x00",
	)
}
