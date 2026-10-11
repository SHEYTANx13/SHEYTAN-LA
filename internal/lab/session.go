package lab

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrSessionNotFound = errors.New("lab: task session not found")
	ErrSessionExists   = errors.New("lab: task session already exists")
)

// Session owns one active Coding Lab task.
//
// The registry mutex protects the collection of sessions. The per-session
// mutex below protects operations that mutate the Task belonging to this
// session, preventing concurrent run/verify/lifecycle operations from acting
// on the same workspace at the same time.
type Session struct {
	ID        string
	Task      *Task
	CreatedAt time.Time
	UpdatedAt time.Time

	mu sync.Mutex
}

// Lock serializes mutation of this session's task.
//
// Callers must pair Lock with Unlock. The mutex is deliberately unexported;
// the methods provide the only synchronization surface exposed to the tool
// layer.
func (s *Session) Lock() {
	if s == nil {
		return
	}

	s.mu.Lock()
}

// Unlock releases the session's task serialization lock.
func (s *Session) Unlock() {
	if s == nil {
		return
	}

	s.mu.Unlock()
}

// SessionRegistry stores active Coding Lab sessions.
//
// The registry is intentionally independent from persistence. This provides a
// clean boundary for a future persistent task database without changing the
// agent tool contract.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionRegistry creates an empty active-task registry.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*Session),
	}
}

// Create registers a task as an active session.
func (r *SessionRegistry) Create(task *Task) (*Session, error) {
	if r == nil {
		return nil, errors.New("lab: session registry is nil")
	}

	if task == nil || task.ID == "" {
		return nil, ErrTaskInvalid
	}

	now := time.Now().UTC()

	session := &Session{
		ID:        task.ID,
		Task:      task,
		CreatedAt: now,
		UpdatedAt: now,
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[task.ID]; exists {
		return nil, fmt.Errorf("%w: %s", ErrSessionExists, task.ID)
	}

	r.sessions[task.ID] = session

	return session, nil
}

// Get returns an active session by task ID.
func (r *SessionRegistry) Get(taskID string) (*Session, error) {
	if r == nil {
		return nil, errors.New("lab: session registry is nil")
	}

	if taskID == "" {
		return nil, ErrSessionNotFound
	}

	r.mu.RLock()
	session, ok := r.sessions[taskID]
	r.mu.RUnlock()

	if !ok || session == nil || session.Task == nil {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, taskID)
	}

	return session, nil
}

// GetTask returns the active Task associated with a task ID.
//
// This method remains intentionally lock-free with respect to task execution.
// Callers that mutate or inspect the task as part of a multi-step operation
// should acquire the corresponding Session lock through Get + Lock.
func (r *SessionRegistry) GetTask(taskID string) (*Task, error) {
	session, err := r.Get(taskID)
	if err != nil {
		return nil, err
	}

	return session.Task, nil
}

// WithTask serializes one complete task operation.
//
// The callback executes while the session mutex is held. This is the preferred
// helper for tool-layer operations that need to perform multiple task state
// transitions atomically.
func (r *SessionRegistry) WithTask(
	taskID string,
	fn func(*Task) error,
) error {
	if r == nil {
		return errors.New("lab: session registry is nil")
	}

	if fn == nil {
		return errors.New("lab: session callback is nil")
	}

	session, err := r.Get(taskID)
	if err != nil {
		return err
	}

	session.Lock()
	defer session.Unlock()

	return fn(session.Task)
}

// Touch updates the session's activity timestamp.
func (r *SessionRegistry) Touch(taskID string) error {
	if r == nil {
		return errors.New("lab: session registry is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[taskID]
	if !ok || session == nil {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, taskID)
	}

	session.UpdatedAt = time.Now().UTC()
	return nil
}

// Delete removes a task session from the active registry.
//
// It does not delete the task's workspace. Workspace cleanup remains the
// responsibility of TaskManager, which guarantees that filesystem boundaries
// are enforced in one place.
func (r *SessionRegistry) Delete(taskID string) error {
	if r == nil {
		return errors.New("lab: session registry is nil")
	}

	if taskID == "" {
		return ErrSessionNotFound
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sessions[taskID]; !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, taskID)
	}

	delete(r.sessions, taskID)

	return nil
}

// List returns a snapshot of all active sessions.
//
// The returned slice and session pointers are detached from the registry map,
// preventing callers from mutating the map itself. Task contents are still
// shared and should be treated as runtime state owned by the session.
func (r *SessionRegistry) List() []*Session {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Session, 0, len(r.sessions))

	for _, session := range r.sessions {
		if session == nil {
			continue
		}

		result = append(result, session)
	}

	return result
}

// Count returns the number of active sessions.
func (r *SessionRegistry) Count() int {
	if r == nil {
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.sessions)
}

// Active reports whether a task currently has an active session.
func (r *SessionRegistry) Active(taskID string) bool {
	if r == nil || taskID == "" {
		return false
	}

	r.mu.RLock()
	_, ok := r.sessions[taskID]
	r.mu.RUnlock()

	return ok
}

// RemoveCompleted removes terminal tasks from the active registry.
//
// This is useful after Finish, Fail, Cancel, or Block when the tool no longer
// needs the task in the active runtime map. The Task object itself can still
// be retained by the caller for reporting.
func (r *SessionRegistry) RemoveCompleted(taskID string) error {
	if r == nil {
		return errors.New("lab: session registry is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[taskID]
	if !ok || session == nil {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, taskID)
	}

	if session.Task == nil {
		return fmt.Errorf("%w: %s", ErrTaskNotFoundForCleanup(), taskID)
	}

	switch session.Task.Status {
	case TaskSucceeded, TaskFailed, TaskCanceled, TaskBlocked:
		delete(r.sessions, taskID)
		return nil

	default:
		return fmt.Errorf(
			"lab: task %q is still active with status %s",
			taskID,
			session.Task.Status,
		)
	}
}

// SnapshotTasks returns the active tasks as a stable slice.
func (r *SessionRegistry) SnapshotTasks() []*Task {
	sessions := r.List()

	result := make([]*Task, 0, len(sessions))

	for _, session := range sessions {
		if session == nil || session.Task == nil {
			continue
		}

		result = append(result, session.Task)
	}

	return result
}

func ErrTaskNotFoundForCleanup() error {
	return ErrSessionNotFound
}
