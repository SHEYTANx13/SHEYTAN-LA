// taskctx.go — v1.7.0: the per-run TASK CONTEXT for agent-created
// task-scoped resources.
//
// The runtime's task runner (scheduler → orchestrator) installs the
// current task/run identity before the agent loop starts and clears it
// afterwards. Tools created under a task context (artifact_create,
// skill_create) link their outputs to that task/run and write into
// task-scoped roots — chat runs without a task context simply get
// "no task context" errors for task-scoped operations.

package tools

import "sync"

// TaskContext is the installed per-run identity.
type TaskContext struct {
	TaskID  string
	RunID   string
	Trigger string // scheduler event kind ("manual", "timer", "test_failure"…)
}

var (
	taskMu      sync.RWMutex
	currentTask *TaskContext
)

// SetTaskContext installs the run's task identity (nil clears it).
func SetTaskContext(tc *TaskContext) {
	taskMu.Lock()
	defer taskMu.Unlock()
	currentTask = tc
}

// CurrentTaskContext returns the installed identity (nil outside task runs).
func CurrentTaskContext() *TaskContext {
	taskMu.RLock()
	defer taskMu.RUnlock()
	if currentTask == nil {
		return nil
	}
	cp := *currentTask
	return &cp
}
