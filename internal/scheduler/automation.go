// automation.go — v1.7.0: the chronological Automation / Tasks layer on
// top of the existing Phase-8 scheduler foundation.
//
// This file EXTENDS the one scheduler (no second engine): same store
// (tasks.jsonl / reports.jsonl), same bounded execution model, same
// durable NextDue claim discipline, same cancellation, same Runner seam.
//
// What v1.7.0 adds:
//
//   - a schedule model beyond the flat interval: once (one-time
//     scheduled execution), interval (recurring), daily and weekly at a
//     local wall-clock time (the LOCAL timezone, not UTC);
//   - enabled/paused as two independent gates (enabled = user intent,
//     paused = temporarily held);
//   - task metadata: linked skills, task-scoped tools, task types —
//     consumed by the runtime's task-run context (skills/tools/artifacts
//     scoping) and never interpreted by the scheduler itself;
//   - event-triggered automation (file_change, git_change, test_failure,
//     ci_failure, build_failure) fired through NotifyEvent by the
//     subsystems that genuinely produced the event;
//   - RunNow (async), Pause/Resume, CancelRun, UpdateTask, Runs (the
//     chronological run history of ONE task);
//   - the durable claim extended to every schedule kind: the claim is
//     persisted BEFORE execution, so a crash can never replay the same
//     claimed deadline (once-tasks claim by disabling themselves
//     durably; recurring tasks claim by persisting the advanced
//     NextDue);
//   - a deterministic missed-deadline policy: a past deadline is claimed
//     and executed at most ONCE, and the next occurrence is computed
//     strictly after the claim instant (a daily task that missed three
//     days runs once, not three times);
//   - ShutdownSettle: shutdown cancels/settles owned workers cleanly.

package scheduler

import (
        "context"
        "errors"
        "fmt"
        "sort"
        "strings"
        "time"
)

// ErrTaskPaused is returned by RunNow for a paused task.
var ErrTaskPaused = errors.New("task is paused")

// ErrAlreadyRunning is returned when a second concurrent run of the same
// task is refused (deterministic rejection — never silent stacking).
var ErrAlreadyRunning = errors.New("task already running")

// ScheduleKind is the v1.7.0 schedule model.
type ScheduleKind string

const (
        // ScheduleInterval is the legacy recurring interval (timer trigger).
        ScheduleInterval ScheduleKind = "interval"
        // ScheduleOnce runs exactly once at a wall-clock time.
        ScheduleOnce ScheduleKind = "once"
        // ScheduleDaily runs every day at a local time of day.
        ScheduleDaily ScheduleKind = "daily"
        // ScheduleWeekly runs on one weekday at a local time of day.
        ScheduleWeekly ScheduleKind = "weekly"
)

// Schedule is the declarative schedule of one task. Interval tasks keep
// Task.Interval; the other kinds use the fields below.
type Schedule struct {
        Kind ScheduleKind `json:"kind,omitempty"`
        // At is the absolute time for once-schedules (RFC3339 in JSON).
        At time.Time `json:"at,omitempty"`
        // TimeOfDay is "HH:MM" local wall-clock for daily/weekly.
        TimeOfDay string `json:"timeOfDay,omitempty"`
        // Weekday is 0=Sunday … 6=Saturday (time.Weekday ordering) for weekly.
        Weekday int `json:"weekday,omitempty"`
}

// Equal compares two schedules for UpdateTask's timeline-reset decision.
func (sc Schedule) Equal(other Schedule) bool {
        return sc.Kind == other.Kind &&
                sc.At.Equal(other.At) &&
                sc.TimeOfDay == other.TimeOfDay &&
                sc.Weekday == other.Weekday
}

// nextOccurrence computes the next local-time occurrence strictly after
// `from` for once/daily/weekly schedules (deterministic, DST-honest via
// time.Date normalization). The LOCAL timezone applies: from is used
// as-is (callers pass time.Now() which is local).
func (sc Schedule) nextOccurrence(from time.Time) (time.Time, error) {
        switch sc.Kind {
        case ScheduleDaily:
                h, m, err := parseHHMM(sc.TimeOfDay)
                if err != nil {
                        return time.Time{}, err
                }
                loc := from.Location()
                next := time.Date(from.Year(), from.Month(), from.Day(), h, m, 0, 0, loc)
                if !next.After(from) {
                        next = next.AddDate(0, 0, 1)
                }
                return next, nil

        case ScheduleWeekly:
                h, m, err := parseHHMM(sc.TimeOfDay)
                if err != nil {
                        return time.Time{}, err
                }
                if sc.Weekday < 0 || sc.Weekday > 6 {
                        return time.Time{}, fmt.Errorf("weekday %d out of range 0..6", sc.Weekday)
                }
                loc := from.Location()
                next := time.Date(from.Year(), from.Month(), from.Day(), h, m, 0, 0, loc)
                // Advance day-by-day to the next matching weekday (≤7 hops).
                for i := 0; i <= 7; i++ {
                        if int(next.Weekday()) == sc.Weekday && next.After(from) {
                                return next, nil
                        }
                        next = next.AddDate(0, 0, 1)
                }
                return time.Time{}, fmt.Errorf("no weekly occurrence found")

        case ScheduleOnce:
                if sc.At.IsZero() {
                        return time.Time{}, fmt.Errorf("once schedule needs an absolute time")
                }
                return sc.At, nil

        default:
                return time.Time{}, fmt.Errorf("schedule kind %q has no occurrence model", sc.Kind)
        }
}

func parseHHMM(s string) (int, int, error) {
        parts := strings.Split(strings.TrimSpace(s), ":")
        if len(parts) != 2 {
                return 0, 0, fmt.Errorf("timeOfDay %q must be HH:MM", s)
        }
        var h, m int
        if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
                return 0, 0, fmt.Errorf("timeOfDay %q hour invalid", s)
        }
        if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
                return 0, 0, fmt.Errorf("timeOfDay %q minute invalid", s)
        }
        if h < 0 || h > 23 || m < 0 || m > 59 {
                return 0, 0, fmt.Errorf("timeOfDay %q out of range", s)
        }
        return h, m, nil
}

// --- RunNow -----------------------------------------------------------------

// RunNow runs a task immediately (asynchronous, bounded by MaxRuntime).
// The report is persisted when the run settles; the returned channel
// yields the final Report for callers that want to wait (tests, API
// optional wait). A paused task is refused; a task already running is
// refused (ErrAlreadyRunning) — deterministic rejection, no stacking.
//
// SETTLEMENT CONTRACT (v1.7.1): the returned channel closes exactly
// once, only AFTER the run has fully settled:
//
//      settle current attempt → reports.jsonl persisted →
//      tasks.jsonl (LastRun) persisted → running/cancel bookkeeping
//      cleared → channel closed
//
// The channel is buffered (capacity 1), so a caller that never reads it
// cannot leak or block the worker. A caller that needs a full-settlement
// barrier — e.g. before removing the store directory — must drain the
// channel until it is CLOSED (receive-report, then receive-until-closed);
// closure is the deterministic proof that every persistence write and
// bookkeeping step has completed. Receiving only the report proves the
// durable writes happened but not that the worker goroutine has exited.
func (s *Scheduler) RunNow(ctx context.Context, id string) (<-chan Report, error) {
        s.mu.Lock()
        task, ok := s.findLocked(id)
        if !ok {
                s.mu.Unlock()
                return nil, fmt.Errorf("task %q not found", id)
        }
        if task.Paused {
                s.mu.Unlock()
                return nil, ErrTaskPaused
        }
        if s.running[id] {
                s.mu.Unlock()
                return nil, ErrAlreadyRunning
        }
        s.running[id] = true
        runCtx, cancel := context.WithTimeout(ctx, task.MaxRuntime)
        s.cancels[id] = cancel
        s.mu.Unlock()

        ch := make(chan Report, 1)

        go func() {
                defer func() {
                        s.mu.Lock()
                        delete(s.running, id)
                        delete(s.cancels, id)
                        s.mu.Unlock()
                        cancel()
                        close(ch)
                }()

                report := s.execute(runCtx, task, EventManual)
                s.attachLastRun(task.ID, &report)
                ch <- report
        }()

        return ch, nil
}

// attachLastRun records the newest report on the task (the UI timeline's
// "last run" column) and persists it.
func (s *Scheduler) attachLastRun(id string, r *Report) {
        s.mu.Lock()
        defer s.mu.Unlock()

        for _, t := range s.tasks {
                if t.ID == id {
                        cp := *r
                        t.LastRun = &cp
                        break
                }
        }
        s.persistLocked()
}

// Pause holds a task: timer/event firing stops, definition stays.
func (s *Scheduler) Pause(id string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        if _, ok := s.findLocked(id); !ok {
                return fmt.Errorf("task %q not found", id)
        }

        for _, t := range s.tasks {
                if t.ID == id {
                        t.Paused = true
                        break
                }
        }
        s.persistLocked()
        return nil
}

// Resume releases a paused task and recomputes its next due time so a
// long pause never fires a burst of stale deadlines.
func (s *Scheduler) Resume(id string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        task, ok := s.findLocked(id)
        if !ok {
                return fmt.Errorf("task %q not found", id)
        }

        for _, t := range s.tasks {
                if t.ID == id {
                        t.Paused = false
                        if err := s.recomputeNextDueLocked(t); err != nil {
                                return err
                        }
                        break
                }
        }
        s.persistLocked()
        _ = task
        return nil
}

// CancelRun cancels a run in flight (the bounded context is canceled; the
// report settles as canceled). Cancelling a not-running task is a no-op.
func (s *Scheduler) CancelRun(id string) bool {
        s.mu.Lock()
        cancel, ok := s.cancels[id]
        s.mu.Unlock()

        if ok {
                cancel()
        }
        return ok
}

// IsRunning reports whether a run of the task is in flight.
func (s *Scheduler) IsRunning(id string) bool {
        s.mu.Lock()
        defer s.mu.Unlock()
        return s.running[id]
}

// UpdateTask applies a validated mutation to a task definition and
// recomputes its schedule. The mutation runs under the scheduler lock.
func (s *Scheduler) UpdateTask(id string, mutate func(*Task) error) error {
        if mutate == nil {
                return fmt.Errorf("no mutation")
        }

        s.mu.Lock()
        defer s.mu.Unlock()

        for _, t := range s.tasks {
                if t.ID != id {
                        continue
                }

                before := *t
                if err := mutate(t); err != nil {
                        return err
                }

                // Revalidate the same contract AddTask enforces.
                if strings.TrimSpace(t.Prompt) == "" {
                        *t = before
                        return fmt.Errorf("task needs a prompt")
                }
                switch t.Trigger {
                case EventTimer, EventMaintenance:
                        if t.Interval < minInterval {
                                *t = before
                                return fmt.Errorf("interval %v below the %v minimum", t.Interval, minInterval)
                        }
                        t.Interval = sanitizeInterval(t.Interval)
                case EventManual, EventStartup, EventFileChange, EventGitChange,
                        EventTestFailure, EventCIFailure, EventBuildFail:
                default:
                        *t = before
                        return fmt.Errorf("unknown trigger %q", t.Trigger)
                }
                if t.MaxRuntime <= 0 {
                        t.MaxRuntime = 15 * time.Minute
                }
                if t.MaxRuntime > 60*time.Minute {
                        t.MaxRuntime = 60 * time.Minute
                }

                // Schedule changes reset the timeline deterministically.
                if before.Trigger != t.Trigger || before.Interval != t.Interval ||
                        !before.Schedule.Equal(t.Schedule) {
                        if err := s.recomputeNextDueLocked(t); err != nil {
                                *t = before
                                return err
                        }
                }

                s.persistLocked()
                return nil
        }

        return fmt.Errorf("task %q not found", id)
}

// recomputeNextDueLocked sets NextDue for the current schedule kind.
// once → At; interval/maintenance → now+interval (or the declared
// schedule kind); daily/weekly → the next local occurrence; event kinds
// → zero (fired by NotifyEvent).
func (s *Scheduler) recomputeNextDueLocked(t *Task) error {
        switch t.Trigger {
        case EventTimer, EventMaintenance:
                if t.Schedule.Kind == ScheduleOnce || t.Schedule.Kind == ScheduleDaily ||
                        t.Schedule.Kind == ScheduleWeekly {
                        next, err := t.Schedule.nextOccurrence(s.nowFunc())
                        if err != nil {
                                return err
                        }
                        t.NextDue = next
                        return nil
                }
                t.NextDue = s.nowFunc().Add(sanitizeInterval(t.Interval))
                return nil

        case EventManual:
                if t.Schedule.Kind == ScheduleOnce && !t.Schedule.At.IsZero() {
                        t.NextDue = t.Schedule.At
                        return nil
                }
                t.NextDue = time.Time{}
                return nil

        default: // event kinds
                t.NextDue = time.Time{}
                return nil
        }
}

// Runs returns the newest n reports of ONE task (chronological run
// history, newest first).
func (s *Scheduler) Runs(taskID string, n int) []Report {
        all := s.Reports(0) // full log

        var out []Report
        for _, r := range all {
                if r.TaskID == taskID {
                        out = append(out, r)
                }
        }

        sort.SliceStable(out, func(i, j int) bool {
                return out[i].StartedAt.After(out[j].StartedAt)
        })

        if n > 0 && len(out) > n {
                out = out[:n]
        }
        return out
}

// --- event-triggered automation -------------------------------------------

// NotifyEvent fires every enabled, unpaused, not-running task whose
// trigger matches the event kind. Each run is bounded independently and
// executes ASYNCHRONOUSLY (an event emitter must never block on agent
// work). Called from the subsystems that genuinely produced the event.
func (s *Scheduler) NotifyEvent(ctx context.Context, kind EventKind) []string {
        s.mu.Lock()
        var fired []string
        for _, t := range s.tasks {
                if t.Trigger != kind || !t.Enabled || t.Paused {
                        continue
                }
                if s.running[t.ID] {
                        continue
                }
                fired = append(fired, t.ID)
        }
        s.mu.Unlock()

        var started []string
        for _, id := range fired {
                // Detached context: the emitter's request lifecycle must not cancel
                // autonomous work. MaxRuntime still bounds every run.
                if _, err := s.RunNow(context.Background(), id); err != nil {
                        continue // lost the race against another trigger — acceptable
                }
                started = append(started, id)
        }
        return started
}

// --- Tick extension: all schedule kinds, durable claims ---------------------

// TickDue is the extended due-selection: it keeps the v1.2.9
// durable-claim contract and extends it to once/daily/weekly schedules
// and the paused gate. Claims are persisted under the lock BEFORE
// execution.
func (s *Scheduler) TickDue(ctx context.Context) []Report {
        s.mu.Lock()
        now := s.nowFunc()

        var due []*Task

        for _, t := range s.tasks {
                if !t.Enabled || t.Paused || s.running[t.ID] {
                        continue
                }

                isDue := false

                switch {
                case t.Schedule.Kind == ScheduleOnce && !t.NextDue.IsZero() && !t.NextDue.After(now):
                        // ONE-TIME CLAIM: disable durably BEFORE execution so a crash
                        // after the claim can never replay the same deadline.
                        t.Enabled = false
                        t.NextDue = time.Time{}
                        isDue = true

                case (t.Schedule.Kind == ScheduleDaily || t.Schedule.Kind == ScheduleWeekly) &&
                        !t.NextDue.IsZero() && !t.NextDue.After(now):
                        // Recurring local-time claim: advance past the claimed deadline
                        // BEFORE execution (durable claim identical to v1.2.9). Missed
                        // deadlines run ONCE: the next occurrence is strictly after
                        // `now`, never retroactively multiplied.
                        next, err := t.Schedule.nextOccurrence(now)
                        if err != nil {
                                continue
                        }
                        t.NextDue = next
                        isDue = true

                case (t.Trigger == EventTimer || t.Trigger == EventMaintenance) &&
                        !t.NextDue.IsZero() && !t.NextDue.After(now):
                        // legacy interval path: advance the deadline NOW (the v1.2.9
                        // durable claim) and persist below, before execution.
                        t.NextDue = now.Add(sanitizeInterval(t.Interval))
                        isDue = true
                }

                if isDue {
                        due = append(due, t)
                }
        }

        if len(due) > 0 {
                // The claim is DURABLE: persisted under the lock, before execution.
                s.persistLocked()
        }
        s.mu.Unlock()

        var reports []Report
        for _, t := range due {
                runCtx, cancel := context.WithTimeout(ctx, t.MaxRuntime)

                s.mu.Lock()
                current, ok := s.findLocked(t.ID)
                if !ok || s.running[t.ID] {
                        s.mu.Unlock()
                        cancel()
                        continue
                }
                s.running[t.ID] = true
                s.cancels[t.ID] = cancel
                s.mu.Unlock()

                report := s.execute(runCtx, current, current.Trigger)
                s.attachLastRun(t.ID, &report)

                s.mu.Lock()
                delete(s.running, t.ID)
                delete(s.cancels, t.ID)
                s.mu.Unlock()
                cancel()

                reports = append(reports, report)
        }
        return reports
}

// ShutdownSettle waits (bounded) for all in-flight runs to settle,
// cancelling them — the clean-shutdown contract for owned workers.
func (s *Scheduler) ShutdownSettle(wait time.Duration) {
        deadline := time.Now().Add(wait)

        for {
                s.mu.Lock()
                ids := make([]string, 0, len(s.cancels))
                for id := range s.cancels {
                        ids = append(ids, id)
                }
                remaining := len(s.running)
                s.mu.Unlock()

                if remaining == 0 || time.Now().After(deadline) {
                        return
                }

                for _, id := range ids {
                        s.CancelRun(id)
                }
                time.Sleep(20 * time.Millisecond)
        }
}
