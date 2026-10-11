package lab

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// ErrTaskSnapshotNotFound indicates that the requested Lab task does not
// currently exist in the active task registry.
var ErrTaskSnapshotNotFound = errors.New(
	"lab: task snapshot not found",
)

// CommandSnapshot is the browser/API-safe representation of one Lab command.
//
// Environment variables are deliberately excluded. The internal Command type
// may contain caller-supplied environment values, but the UI does not need
// them to display execution history.
type CommandSnapshot struct {
	Command        string        `json:"command"`
	WorkingDir     string        `json:"workingDir,omitempty"`
	Timeout        time.Duration `json:"timeout,omitempty"`
	MaxOutputBytes int64         `json:"maxOutputBytes,omitempty"`
}

// TaskSnapshot is a stable, read-only representation of one Coding Lab task.
//
// It is constructed while the owning Lab session is locked so the API cannot
// observe partially-mutated task state during concurrent agent operations.
type TaskSnapshot struct {
	ID                 string               `json:"id"`
	Title              string               `json:"title"`
	Description        string               `json:"description"`
	Status             TaskStatus           `json:"status"`
	Workspace          *Workspace           `json:"workspace,omitempty"`
	Commands           []CommandSnapshot    `json:"commands,omitempty"`
	Results            []CommandResult      `json:"results,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
	LastVerification   *VerificationSummary `json:"lastVerification,omitempty"`
	VerificationPassed bool                 `json:"verificationPassed"`
	VerifiedAt         *time.Time           `json:"verifiedAt,omitempty"`
	CreatedAt          time.Time            `json:"createdAt"`
	StartedAt          *time.Time           `json:"startedAt,omitempty"`
	FinishedAt         *time.Time           `json:"finishedAt,omitempty"`
	Error              string               `json:"error,omitempty"`
}

// TaskSessionSnapshot combines the task state with its active Lab-session
// metadata.
type TaskSessionSnapshot struct {
	ID        string       `json:"id"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
	Task      TaskSnapshot `json:"task"`
}

// Snapshot returns one immutable API-safe view of an active Lab task.
//
// The session lock is held while the task is copied so callers receive a
// coherent state boundary rather than a mixture of fields from different
// moments in the task lifecycle.
func (t *Tool) Snapshot(taskID string) (TaskSessionSnapshot, error) {
	if t == nil || t.sessions == nil {
		return TaskSessionSnapshot{}, errors.New(
			"lab: session registry is not configured",
		)
	}

	taskID = strings.TrimSpace(taskID)

	if taskID == "" {
		return TaskSessionSnapshot{}, ErrTaskSnapshotNotFound
	}

	session, err := t.sessions.Get(taskID)
	if err != nil {
		return TaskSessionSnapshot{}, err
	}

	session.Lock()
	defer session.Unlock()

	if session.Task == nil {
		return TaskSessionSnapshot{}, ErrTaskSnapshotNotFound
	}

	return TaskSessionSnapshot{
		ID:        session.ID,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Task:      snapshotTask(session.Task),
	}, nil
}

// Snapshots returns a deterministic snapshot of every currently active Lab
// task.
//
// The returned values are detached from the live session registry. Mutating a
// returned snapshot therefore cannot mutate the active task state.
func (t *Tool) Snapshots() []TaskSessionSnapshot {
	if t == nil || t.sessions == nil {
		return nil
	}

	sessions := t.sessions.List()

	result := make(
		[]TaskSessionSnapshot,
		0,
		len(sessions),
	)

	for _, session := range sessions {
		if session == nil || session.Task == nil {
			continue
		}

		session.Lock()

		snapshot := TaskSessionSnapshot{
			ID:        session.ID,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
			Task:      snapshotTask(session.Task),
		}

		session.Unlock()

		result = append(result, snapshot)
	}

	sort.SliceStable(
		result,
		func(i, j int) bool {
			if !result[i].UpdatedAt.Equal(
				result[j].UpdatedAt,
			) {
				return result[i].UpdatedAt.Before(
					result[j].UpdatedAt,
				)
			}

			return result[i].ID < result[j].ID
		},
	)

	return result
}

// snapshotTask creates a detached API-safe copy of a Task.
func snapshotTask(task *Task) TaskSnapshot {
	if task == nil {
		return TaskSnapshot{}
	}

	snapshot := TaskSnapshot{
		ID:                 task.ID,
		Title:              task.Title,
		Description:        task.Description,
		Status:             task.Status,
		VerificationPassed: task.VerificationPassed,
		CreatedAt:          task.CreatedAt,
		Error:              task.Error,
	}

	if task.Workspace != nil {
		workspace := *task.Workspace
		snapshot.Workspace = &workspace
	}

	if task.VerifiedAt != nil {
		verifiedAt := *task.VerifiedAt
		snapshot.VerifiedAt = &verifiedAt
	}

	if task.StartedAt != nil {
		startedAt := *task.StartedAt
		snapshot.StartedAt = &startedAt
	}

	if task.FinishedAt != nil {
		finishedAt := *task.FinishedAt
		snapshot.FinishedAt = &finishedAt
	}

	if len(task.Commands) > 0 {
		snapshot.Commands = make(
			[]CommandSnapshot,
			0,
			len(task.Commands),
		)

		for _, command := range task.Commands {
			snapshot.Commands = append(
				snapshot.Commands,
				CommandSnapshot{
					Command:        command.Command,
					WorkingDir:     command.WorkingDir,
					Timeout:        command.Timeout,
					MaxOutputBytes: command.MaxOutputBytes,
				},
			)
		}
	}

	if len(task.Results) > 0 {
		snapshot.Results = append(
			[]CommandResult(nil),
			task.Results...,
		)
	}

	if len(task.Metadata) > 0 {
		snapshot.Metadata = make(
			map[string]string,
			len(task.Metadata),
		)

		for key, value := range task.Metadata {
			snapshot.Metadata[key] = value
		}
	}

	if task.LastVerification != nil {
		verification := *task.LastVerification

		verification.Results = append(
			[]VerificationResult(nil),
			task.LastVerification.Results...,
		)

		snapshot.LastVerification = &verification
	}

	return snapshot
}
