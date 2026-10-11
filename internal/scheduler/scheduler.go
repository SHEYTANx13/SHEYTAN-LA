// Package scheduler implements Phase 8: the local autonomous event and
// scheduler foundation.
//
// The abstraction maps one event to one bounded task run:
//
//      event → task definition → planner → execution → verification →
//      report → memory
//
// Supported triggers in this foundation release: manual, startup, timer
// (interval), and maintenance (a timer variant labeled for housekeeping).
// File-change, git-change, test/CI/build-failure triggers are declared as
// event kinds so future emitters can feed the same pipeline without a
// schema change — they are documented as foundation, not implemented
// emitters.
//
// Every scheduled run is bounded: a wall-clock budget per run, a run
// history with bounded retention, cancellation through the context, and
// reports persisted to disk (scheduler/reports.jsonl) with a memory
// summary appended through the injected recorder.
package scheduler

import (
        "context"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"
)

// Event kinds — the future-compatible taxonomy.
type EventKind string

const (
        EventManual      EventKind = "manual"
        EventStartup     EventKind = "startup"
        EventTimer       EventKind = "timer"
        EventFileChange  EventKind = "file_change"
        EventGitChange   EventKind = "git_change"
        EventTestFailure EventKind = "test_failure"
        EventCIFailure   EventKind = "ci_failure"
        EventBuildFail   EventKind = "build_failure"
        EventMaintenance EventKind = "scheduled_maintenance"
)

// Task is one scheduled task definition.
type Task struct {
        ID      string    `json:"id"`
        Name    string    `json:"name"`
        Trigger EventKind `json:"trigger"`
        // Interval applies to timer/maintenance triggers (>= 5 minutes by
        // policy — shorter intervals are rejected).
        Interval time.Duration `json:"-"`
        // IntervalSeconds/MaxRuntimeSeconds are the SERIALIZED companions of
        // the duration fields (durations do not JSON-round-trip); kept in sync
        // by persistLocked/Load.
        IntervalSeconds   int64    `json:"intervalSeconds,omitempty"`
        MaxRuntimeSeconds int64    `json:"maxRuntimeSeconds,omitempty"`
        // Prompt is the task instruction handed to the planner/executor.
        Prompt string `json:"prompt"`
        // MaxRuntime bounds one run (default 15m, max 60m).
        MaxRuntime time.Duration `json:"-"`
        // Enabled gates timer firing (manual/startup tasks run once).
        Enabled bool `json:"enabled"`
        // Paused (v1.7.0) temporarily holds scheduled/event firing without
        // deleting the task; RunNow refuses a paused task too.
        Paused bool `json:"paused,omitempty"`
        // Schedule (v1.7.0) is the extended schedule model: once / daily /
        // weekly at a local wall-clock time; interval tasks may leave it zero.
        Schedule Schedule `json:"scheduleV17,omitempty"`
        // LinkedSkills (v1.7.0) lists skill ids injected into this task's runs
        // (task-scoped skills; consumed by the runtime run context).
        LinkedSkills []string `json:"linkedSkills,omitempty"`
        // TaskTools (v1.7.0) lists task-scoped tool names available to this
        // task's runs (consumed by the runtime run context).
        TaskTools []string `json:"taskTools,omitempty"`
        // TaskTypes (v1.7.0) classifies the task for skills/scoping.
        TaskTypes []string `json:"taskTypes,omitempty"`
        // LastRun (v1.7.0) is the newest settled report (UI timeline).
        LastRun *Report `json:"lastRun,omitempty"`
        // NextDue is the next timer deadline (timer tasks only).
        NextDue time.Time `json:"nextDue,omitempty"`
        Created time.Time `json:"created"`
}

// minInterval is the policy floor for timer triggers.
const minInterval = 5 * time.Minute

// Report is one run's outcome.
type Report struct {
        TaskID     string    `json:"taskId"`
        TaskName   string    `json:"taskName"`
        Trigger    EventKind `json:"trigger"`
        StartedAt  time.Time `json:"startedAt"`
        DurationMs int64     `json:"durationMs"`
        OK         bool      `json:"ok"`
        Output     string    `json:"output,omitempty"`
        Err        string    `json:"err,omitempty"`
        Canceled   bool      `json:"canceled,omitempty"`
}

// Runner executes one task's prompt through the agent stack (the runtime
// injects this seam; the scheduler itself never talks to an LLM).
type Runner func(ctx context.Context, task Task) (string, error)

// MemoryRecorder persists one bounded summary of a completed run.
type MemoryRecorder func(task Task, report Report)

// Scheduler is the local event/task engine.
type Scheduler struct {
        mu       sync.Mutex
        tasks    []*Task
        runner   Runner
        memory   MemoryRecorder
        storeDir string
        running  map[string]bool // taskID → run in flight
        cancels  map[string]context.CancelFunc
        // maxReports bounds the persisted report log per compaction.
        maxReports int
        // nowFunc is overridable in tests.
        nowFunc func() time.Time
}

// New creates the scheduler; storeDir holds tasks.jsonl and reports.jsonl.
func New(storeDir string, runner Runner, memory MemoryRecorder) *Scheduler {
        return &Scheduler{
                storeDir:   storeDir,
                runner:     runner,
                memory:     memory,
                running:    map[string]bool{},
                cancels:    map[string]context.CancelFunc{},
                maxReports: 512,
                nowFunc:    time.Now,
        }
}

// Load restores persisted tasks.
func (s *Scheduler) Load() error {
        s.mu.Lock()
        defer s.mu.Unlock()

        data, err := os.ReadFile(filepath.Join(s.storeDir, "tasks.jsonl"))
        if err != nil {
                if os.IsNotExist(err) {
                        return nil
                }
                return err
        }

        var tasks []*Task
        for _, line := range strings.Split(string(data), "\n") {
                line = strings.TrimSpace(line)
                if line == "" {
                        continue
                }
                var t Task
                if json.Unmarshal([]byte(line), &t) == nil {
                        if t.Interval > 0 {
                                t.Interval = sanitizeInterval(t.Interval)
                        }
                        // v1.7.0: restore the duration companions.
                        if t.Interval == 0 && t.IntervalSeconds > 0 {
                                t.Interval = sanitizeInterval(time.Duration(t.IntervalSeconds) * time.Second)
                        }
                        if t.MaxRuntime == 0 && t.MaxRuntimeSeconds > 0 {
                                t.MaxRuntime = time.Duration(t.MaxRuntimeSeconds) * time.Second
                        }
                        tasks = append(tasks, &t)
                }
        }
        s.tasks = tasks
        return nil
}

func sanitizeInterval(d time.Duration) time.Duration {
        if d < minInterval {
                return minInterval
        }
        return d
}

// persistLocked rewrites the task file.
func (s *Scheduler) persistLocked() {
        if err := os.MkdirAll(s.storeDir, 0o755); err != nil {
                return
        }
        var b strings.Builder
        for _, t := range s.tasks {
                // v1.7.0: keep the serialized duration companions in sync.
                if t.Interval > 0 {
                        t.IntervalSeconds = int64(t.Interval / time.Second)
                }
                if t.MaxRuntime > 0 {
                        t.MaxRuntimeSeconds = int64(t.MaxRuntime / time.Second)
                }
                line, err := json.Marshal(t)
                if err != nil {
                        continue
                }
                b.Write(line)
                b.WriteByte('\n')
        }
        tmp := filepath.Join(s.storeDir, "tasks.jsonl.tmp")
        if os.WriteFile(tmp, []byte(b.String()), 0o600) == nil {
                _ = os.Rename(tmp, filepath.Join(s.storeDir, "tasks.jsonl"))
        }
}

// AddTask registers a task. Timer intervals below 5 minutes are rejected
// (policy floor, matching the runtime's scheduling rules).
func (s *Scheduler) AddTask(t Task) error {
        if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Prompt) == "" {
                return fmt.Errorf("task needs an id and a prompt")
        }

        switch t.Trigger {
        case EventTimer, EventMaintenance:
                if t.Interval < minInterval {
                        return fmt.Errorf("interval %v below the %v minimum", t.Interval, minInterval)
                }
                t.Interval = sanitizeInterval(t.Interval)
        case EventManual, EventStartup, EventFileChange, EventGitChange,
                EventTestFailure, EventCIFailure, EventBuildFail:
                // one-shot / emitter-fed kinds: no interval needed
        default:
                return fmt.Errorf("unknown trigger %q", t.Trigger)
        }

        s.mu.Lock()
        defer s.mu.Unlock()

        for _, existing := range s.tasks {
                if existing.ID == t.ID {
                        return fmt.Errorf("task %q already exists", t.ID)
                }
        }

        t.Created = s.nowFunc().UTC()
        if t.MaxRuntime <= 0 {
                t.MaxRuntime = 15 * time.Minute
        }
        if t.MaxRuntime > 60*time.Minute {
                t.MaxRuntime = 60 * time.Minute
        }
        if t.Trigger == EventTimer || t.Trigger == EventMaintenance {
                // v1.7.0: a declared once/daily/weekly schedule on a timer
                // trigger computes its first LOCAL occurrence; the flat
                // interval stays the default.
                if t.Schedule.Kind == ScheduleOnce || t.Schedule.Kind == ScheduleDaily ||
                        t.Schedule.Kind == ScheduleWeekly {
                        next, nerr := t.Schedule.nextOccurrence(s.nowFunc())
                        if nerr != nil {
                                return nerr
                        }
                        t.NextDue = next
                } else {
                        t.NextDue = s.nowFunc().Add(t.Interval)
                }
        } else if t.Trigger == EventManual && t.Schedule.Kind == ScheduleOnce &&
                !t.Schedule.At.IsZero() {
                // one-time scheduled execution of a manual task.
                t.NextDue = t.Schedule.At
        }
        t.Enabled = true
        s.tasks = append(s.tasks, &t)
        s.persistLocked()
        return nil
}

// RemoveTask deletes a task and cancels a run in flight.
func (s *Scheduler) RemoveTask(id string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        if cancel, ok := s.cancels[id]; ok {
                cancel()
        }

        out := s.tasks[:0]
        found := false
        for _, t := range s.tasks {
                if t.ID == id {
                        found = true
                        continue
                }
                out = append(out, t)
        }
        if !found {
                return fmt.Errorf("task %q not found", id)
        }
        s.tasks = out
        s.persistLocked()
        return nil
}

// Tasks returns a snapshot of task definitions.
func (s *Scheduler) Tasks() []Task {
        s.mu.Lock()
        defer s.mu.Unlock()
        out := make([]Task, 0, len(s.tasks))
        for _, t := range s.tasks {
                out = append(out, *t)
        }
        return out
}

// Fire runs one task NOW (manual trigger path). Bounded by the task's
// runtime budget; a run already in flight is refused, not queued —
// autonomous work must never stack silently.
func (s *Scheduler) Fire(ctx context.Context, id string) (Report, error) {
        s.mu.Lock()
        task, ok := s.findLocked(id)
        if !ok {
                s.mu.Unlock()
                return Report{}, fmt.Errorf("task %q not found", id)
        }
        if s.running[id] {
                s.mu.Unlock()
                return Report{}, fmt.Errorf("task %q is already running", id)
        }
        s.running[id] = true
        runCtx, cancel := context.WithTimeout(ctx, task.MaxRuntime)
        s.cancels[id] = cancel
        s.mu.Unlock()

        defer func() {
                s.mu.Lock()
                delete(s.running, id)
                delete(s.cancels, id)
                s.mu.Unlock()
                cancel()
        }()

        return s.execute(runCtx, task, EventManual), nil
}

func (s *Scheduler) findLocked(id string) (Task, bool) {
        for _, t := range s.tasks {
                if t.ID == id {
                        return *t, true
                }
        }
        return Task{}, false
}

// execute performs the event→execution→verification→report→memory flow.
func (s *Scheduler) execute(ctx context.Context, task Task, trigger EventKind) Report {
        report := Report{
                TaskID:    task.ID,
                TaskName:  task.Name,
                Trigger:   trigger,
                StartedAt: s.nowFunc().UTC(),
        }

        started := time.Now()

        if s.runner == nil {
                report.Err = "no runner installed"
                s.finalize(report)
                return report
        }

        output, err := s.runner(ctx, task)

        report.DurationMs = time.Since(started).Milliseconds()
        report.Output = output

        if err != nil {
                if ctx.Err() != nil {
                        report.Canceled = true
                        report.Err = "canceled: " + err.Error()
                } else {
                        report.OK = false
                        report.Err = err.Error()
                }
        } else {
                report.OK = true
        }

        s.finalize(report)
        return report
}

func (s *Scheduler) finalize(report Report) {
        s.persistReport(report)
        if s.memory != nil && report.TaskID != "" {
                // Memory gets the bounded summary only.
                var task Task
                s.mu.Lock()
                task, _ = s.findLocked(report.TaskID)
                s.mu.Unlock()
                if task.ID == report.TaskID {
                        s.memory(task, report)
                }
        }
}

// Tick advances timer tasks: every task whose NextDue has passed and is
// enabled fires (in its own bounded run). Called from the runtime's loop.
//
// v1.2.9 DURABLE CLAIM SEMANTICS: when a due task is claimed, its
// advanced NextDue is persisted immediately (under the claim lock,
// before execution starts), so a crash between claiming/executing/
// persisting can never replay the same deadline twice after restart.
//
// v1.7.0: the extended due-selection (once / daily / weekly schedules,
// paused gate, event tasks untouched) lives in TickDue and REPLACES the
// legacy body — the legacy interval path is preserved byte-compatibly.
func (s *Scheduler) Tick(ctx context.Context) []Report {
        return s.TickDue(ctx)
}

// persistReport appends one report, compacting to the newest half when the
// bound is exceeded.
func (s *Scheduler) persistReport(r Report) {
        s.mu.Lock()
        defer s.mu.Unlock()

        if err := os.MkdirAll(s.storeDir, 0o755); err != nil {
                return
        }

        path := filepath.Join(s.storeDir, "reports.jsonl")

        var reports []Report
        if data, err := os.ReadFile(path); err == nil {
                for _, line := range strings.Split(string(data), "\n") {
                        line = strings.TrimSpace(line)
                        if line == "" {
                                continue
                        }
                        var stored Report
                        if json.Unmarshal([]byte(line), &stored) == nil {
                                reports = append(reports, stored)
                        }
                }
        }
        reports = append(reports, r)
        if len(reports) > s.maxReports {
                reports = append([]Report(nil), reports[len(reports)-s.maxReports/2:]...)
        }

        var b strings.Builder
        for _, stored := range reports {
                line, err := json.Marshal(stored)
                if err != nil {
                        continue
                }
                b.Write(line)
                b.WriteByte('\n')
        }

        tmp := path + ".tmp"
        if os.WriteFile(tmp, []byte(b.String()), 0o600) == nil {
                _ = os.Rename(tmp, path)
        }
}

// Reports returns the newest n run reports.
func (s *Scheduler) Reports(n int) []Report {
        s.mu.Lock()
        defer s.mu.Unlock()

        data, err := os.ReadFile(filepath.Join(s.storeDir, "reports.jsonl"))
        if err != nil {
                return nil
        }
        var reports []Report
        for _, line := range strings.Split(string(data), "\n") {
                line = strings.TrimSpace(line)
                if line == "" {
                        continue
                }
                var r Report
                if json.Unmarshal([]byte(line), &r) == nil {
                        reports = append(reports, r)
                }
        }
        if n > 0 && len(reports) > n {
                reports = reports[len(reports)-n:]
        }
        return reports
}
