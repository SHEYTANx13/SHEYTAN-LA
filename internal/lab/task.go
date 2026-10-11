package lab

import (
        "context"
        "errors"
        "fmt"
        "strings"
        "time"
)

var (
        ErrTaskInvalid           = errors.New("lab: invalid task")
        ErrTaskNotRunnable       = errors.New("lab: task is not runnable")
        ErrTaskAlreadyClosed     = errors.New("lab: task is already closed")
        ErrTaskNotVerified       = errors.New("lab: task has not passed verification")
        ErrTaskVerificationStale = errors.New("lab: task verification is stale")
)

// TaskStatus represents the lifecycle state of one Coding Lab task.
type TaskStatus string

const (
        TaskPending   TaskStatus = "pending"
        TaskRunning   TaskStatus = "running"
        TaskSucceeded TaskStatus = "succeeded"
        TaskFailed    TaskStatus = "failed"
        TaskCanceled  TaskStatus = "canceled"
        TaskBlocked   TaskStatus = "blocked"
)

// Task describes one autonomous coding operation.
//
// A task owns a disposable workspace and a sequence of command executions.
// Verification state is attached directly to the task so successful
// completion can never be declared independently from objective verification.
type Task struct {
        ID          string            `json:"id"`
        Title       string            `json:"title"`
        Description string            `json:"description"`
        Status      TaskStatus        `json:"status"`
        Workspace   *Workspace        `json:"workspace,omitempty"`
        Commands    []Command         `json:"commands,omitempty"`
        Results     []CommandResult   `json:"results,omitempty"`
        Metadata    map[string]string `json:"metadata,omitempty"`

        // LastVerification contains the most recent objective verification result.
        LastVerification *VerificationSummary `json:"lastVerification,omitempty"`

        // VerificationPassed indicates that the current workspace state passed all
        // required verification checks.
        VerificationPassed bool `json:"verificationPassed"`

        // VerifiedAt records when the current workspace state was verified.
        VerifiedAt *time.Time `json:"verifiedAt,omitempty"`

        CreatedAt  time.Time  `json:"createdAt"`
        StartedAt  *time.Time `json:"startedAt,omitempty"`
        FinishedAt *time.Time `json:"finishedAt,omitempty"`
        Error      string     `json:"error,omitempty"`
}

// TaskManager coordinates task lifecycle, workspace creation, command
// execution, policy enforcement, and verification state.
type TaskManager struct {
        Workspaces *WorkspaceManager
        Runner     *Runner
        Policy     Policy

        // KeepWorkspaces controls whether completed task workspaces are retained.
        KeepWorkspaces bool
}

// NewTaskManager constructs a Coding Lab task manager.
func NewTaskManager(
        workspaces *WorkspaceManager,
        runner *Runner,
        policy Policy,
        keepWorkspaces bool,
) (*TaskManager, error) {
        if workspaces == nil {
                return nil, ErrInvalidWorkspace
        }

        if runner == nil {
                return nil, errors.New("lab: runner is nil")
        }

        return &TaskManager{
                Workspaces:     workspaces,
                Runner:         runner,
                Policy:         policy,
                KeepWorkspaces: keepWorkspaces,
        }, nil
}

// NewTask creates a pending task.
//
// The definitive runtime ID is assigned when Start creates the isolated
// workspace. The workspace manager already uses a cryptographically random
// identifier, and that identifier becomes the task/session identifier too.
func (m *TaskManager) NewTask(title, description string) *Task {
        now := time.Now().UTC()

        return &Task{
                ID:          newTaskID(now),
                Title:       strings.TrimSpace(title),
                Description: strings.TrimSpace(description),
                Status:      TaskPending,
                Commands:    make([]Command, 0),
                Results:     make([]CommandResult, 0),
                Metadata:    make(map[string]string),
                CreatedAt:   now,
        }
}

// Start creates an isolated workspace for the task.
func (m *TaskManager) Start(
        ctx context.Context,
        task *Task,
        source string,
) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Status != TaskPending {
                return fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        if strings.TrimSpace(source) == "" {
                return ErrInvalidSource
        }

        if ctx == nil {
                ctx = context.Background()
        }

        workspace, err := m.Workspaces.Create(ctx, source)
        if err != nil {
                task.Status = TaskFailed
                task.Error = err.Error()
                return err
        }

        // Workspace IDs are the authoritative random runtime IDs. Reuse the exact
        // same value for Task/session correlation instead of maintaining two
        // independent identifiers.
        task.ID = workspace.ID

        now := time.Now().UTC()

        task.Workspace = workspace
        task.StartedAt = &now
        task.Status = TaskRunning
        task.Error = ""

        m.invalidateVerification(task)

        return nil
}

// RunCommand validates and executes one command belonging to the task.
//
// Any new command invalidates the previous verification because the command
// may have changed the workspace after the previous verification completed.
func (m *TaskManager) RunCommand(
        ctx context.Context,
        task *Task,
        command Command,
) (CommandResult, error) {
        if err := m.validateTask(task); err != nil {
                return CommandResult{}, err
        }

        if task.Status != TaskRunning {
                return CommandResult{}, fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        if task.Workspace == nil {
                return CommandResult{}, ErrInvalidWorkspace
        }

        if err := m.Policy.EvaluateForWorkspace(
                command.Command,
                command.WorkingDir,
        ); err != nil {
                return CommandResult{}, err
        }

        if ctx == nil {
                ctx = context.Background()
        }

        // The workspace may change even if the command later fails. Therefore
        // invalidate verification before execution, not after it.
        m.invalidateVerification(task)

        result, err := m.Runner.Run(ctx, task.Workspace, command)

        task.Commands = append(task.Commands, command)
        task.Results = append(task.Results, result)

        return result, err
}

// RecordVerification stores the latest verification result on the task.
//
// A task is considered verified only when the aggregate verification passed.
// The summary is copied so later caller-side modifications cannot silently
// alter the task's verification state.
func (m *TaskManager) RecordVerification(
        task *Task,
        summary VerificationSummary,
) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Status != TaskRunning {
                return fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        snapshot := summary
        snapshot.Results = append(
                []VerificationResult(nil),
                summary.Results...,
        )

        task.LastVerification = &snapshot
        task.VerificationPassed = summary.Passed

        if summary.Passed {
                verifiedAt := summary.FinishedAt

                if verifiedAt.IsZero() {
                        verifiedAt = time.Now().UTC()
                }

                task.VerifiedAt = &verifiedAt
        } else {
                task.VerifiedAt = nil
        }

        // v1.7.0: the genuine verification-settled event. Every recorded
        // verification (pass OR fail) flows through this single point; the
        // runtime installs the hook to feed the automation scheduler's
        // test_failure trigger (a failed verification IS a real test failure —
        // never a synthetic event). Nil in tests without automation.
        if OnVerificationSettled != nil {
                OnVerificationSettled(task.ID, summary.Passed)
        }

        return nil
}

// OnVerificationSettled is the v1.7.0 automation hook: fired after every
// recorded Lab verification with the task id and the objective verdict.
var OnVerificationSettled func(taskID string, passed bool)

// IsVerified reports whether the task currently has a passing verification.
func (t *Task) IsVerified() bool {
        if t == nil {
                return false
        }

        return t.Status == TaskRunning &&
                t.VerificationPassed &&
                t.LastVerification != nil &&
                t.LastVerification.Passed &&
                t.VerifiedAt != nil
}

// Finish marks a task as successfully completed.
//
// Completion is impossible without a current successful verification.
func (m *TaskManager) Finish(task *Task) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Status != TaskRunning {
                return fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        if task.LastVerification == nil {
                return ErrTaskNotVerified
        }

        if !task.IsVerified() {
                return ErrTaskVerificationStale
        }

        now := time.Now().UTC()
        task.FinishedAt = &now
        task.Status = TaskSucceeded
        task.Error = ""

        return m.cleanupTaskWorkspace(task)
}

// Fail marks a task as failed.
func (m *TaskManager) Fail(task *Task, cause error) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Status != TaskRunning && task.Status != TaskPending {
                return fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        now := time.Now().UTC()
        task.FinishedAt = &now
        task.Status = TaskFailed

        if cause != nil {
                task.Error = cause.Error()
        } else {
                task.Error = "task failed"
        }

        return m.cleanupTaskWorkspace(task)
}

// Cancel marks a task as canceled.
func (m *TaskManager) Cancel(task *Task) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Status != TaskRunning && task.Status != TaskPending {
                return fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        now := time.Now().UTC()
        task.FinishedAt = &now
        task.Status = TaskCanceled
        task.Error = "task canceled"

        return m.cleanupTaskWorkspace(task)
}

// Block marks a task as blocked.
func (m *TaskManager) Block(task *Task, reason string) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Status != TaskPending && task.Status != TaskRunning {
                return fmt.Errorf(
                        "%w: current status=%s",
                        ErrTaskNotRunnable,
                        task.Status,
                )
        }

        now := time.Now().UTC()
        task.FinishedAt = &now
        task.Status = TaskBlocked

        reason = strings.TrimSpace(reason)
        if reason == "" {
                reason = "task blocked"
        }

        task.Error = reason

        return m.cleanupTaskWorkspace(task)
}

// Close cleans up a task workspace without changing the task status.
func (m *TaskManager) Close(task *Task) error {
        if err := m.validateTask(task); err != nil {
                return err
        }

        if task.Workspace == nil {
                return ErrInvalidWorkspace
        }

        if m.KeepWorkspaces {
                return nil
        }

        if err := m.Workspaces.Remove(task.Workspace); err != nil {
                return err
        }

        task.Workspace = nil

        return nil
}

// LastResult returns the most recent command result.
func (t *Task) LastResult() *CommandResult {
        if t == nil || len(t.Results) == 0 {
                return nil
        }

        return &t.Results[len(t.Results)-1]
}

// SuccessfulCommands returns the number of commands that completed
// successfully.
func (t *Task) SuccessfulCommands() int {
        if t == nil {
                return 0
        }

        count := 0

        for _, result := range t.Results {
                if result.Success {
                        count++
                }
        }

        return count
}

// FailedCommands returns the number of commands that did not complete
// successfully.
func (t *Task) FailedCommands() int {
        if t == nil {
                return 0
        }

        count := 0

        for _, result := range t.Results {
                if !result.Success {
                        count++
                }
        }

        return count
}

func (m *TaskManager) validateTask(task *Task) error {
        if m == nil {
                return ErrTaskInvalid
        }

        if task == nil {
                return ErrTaskInvalid
        }

        if strings.TrimSpace(task.ID) == "" {
                return ErrTaskInvalid
        }

        return nil
}

func (m *TaskManager) invalidateVerification(task *Task) {
        if task == nil {
                return
        }

        // The previous verification summary is intentionally retained but marked
        // non-passing, so Finish can distinguish "never verified" from
        // "verification invalidated by a later mutation" (stale).
        task.VerificationPassed = false
        task.VerifiedAt = nil
}

func (m *TaskManager) cleanupTaskWorkspace(task *Task) error {
        if task == nil || task.Workspace == nil {
                return nil
        }

        if m.KeepWorkspaces {
                return nil
        }

        if err := m.Workspaces.Remove(task.Workspace); err != nil {
                return err
        }

        task.Workspace = nil

        return nil
}

func newTaskID(now time.Time) string {
        return fmt.Sprintf(
                "task-%s",
                now.UTC().Format("20060102-150405.000000000"),
        )
}
