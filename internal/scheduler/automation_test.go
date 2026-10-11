package scheduler

// automation_test.go — v1.7.0 Automation / Tasks regression contract:
//
//   - once / daily / weekly / interval scheduling with local-time math;
//   - RunNow (manual run), pause/resume, cancellation;
//   - the DURABLE DEADLINE CLAIM extended to every schedule kind (a crash
//     between claim and execution can never replay the claimed deadline);
//   - duplicate-execution prevention (concurrent runs rejected);
//   - restart/reload (state survives the process);
//   - event-triggered automation (NotifyEvent);
//   - task metadata (linked skills / task tools) round-trips;
//   - update/delete.

import (
        "context"
        "errors"
        "strings"
        "sync"
        "sync/atomic"
        "testing"
        "time"
)

// --- once scheduling + durable claim ----------------------------------------

func TestOnceTaskRunsExactlyOnceAcrossRestarts(t *testing.T) {
        dir := t.TempDir()

        var fired atomic.Int32
        storeDir := dir

        mk := func() *Scheduler {
                return New(storeDir, func(ctx context.Context, task Task) (string, error) {
                        fired.Add(1)
                        return "once-work", nil
                }, nil)
        }

        s1 := mk()
        at := time.Now().Add(-time.Second) // already due
        if err := s1.AddTask(Task{
                ID: "once1", Name: "one-time", Trigger: EventManual,
                Prompt: "p", Schedule: Schedule{Kind: ScheduleOnce, At: at},
        }); err != nil {
                t.Fatal(err)
        }

        // The claim phase alone (crash between claim and execution): the task
        // must be durably disabled — a later reload can NEVER replay it.
        if got := s1.TickDue(context.Background()); len(got) != 1 {
                t.Fatalf("once task did not fire on tick: %+v", got)
        }

        // Reload as a fresh process.
        s2 := mk()
        if err := s2.Load(); err != nil {
                t.Fatal(err)
        }

        // Advance the clock far past everything; tick repeatedly.
        for i := 0; i < 3; i++ {
                if got := s2.TickDue(context.Background()); len(got) != 0 {
                        t.Fatalf("a claimed once-task replayed after restart: %+v", got)
                }
        }

        if got := fired.Load(); got != 1 {
                t.Fatalf("once task executed %d times, want exactly 1", got)
        }

        // The definition survives with Enabled=false (the claim is visible).
        tasks := s2.Tasks()
        if len(tasks) != 1 || tasks[0].Enabled {
                t.Fatalf("once task after claim: %+v", tasks)
        }
}

func TestOnceTaskClaimPersistsBeforeExecution(t *testing.T) {
        dir := t.TempDir()

        // A runner that blocks until released: execution is IN FLIGHT while we
        // inspect the persisted state.
        release := make(chan struct{})
        var wg sync.WaitGroup
        wg.Add(1)

        s := New(dir, func(ctx context.Context, task Task) (string, error) {
                <-release
                wg.Done()
                return "work", nil
        }, nil)

        if err := s.AddTask(Task{
                ID: "claim1", Name: "claim", Trigger: EventManual,
                Prompt: "p", Schedule: Schedule{Kind: ScheduleOnce, At: time.Now().Add(-time.Second)},
        }); err != nil {
                t.Fatal(err)
        }

        done := make(chan []Report, 1)
        go func() { done <- s.TickDue(context.Background()) }()

        // Wait until the run started (the runner is blocked), then prove the
        // claim is ALREADY durable.
        waitFor := func(cond func() bool, what string) {
                deadline := time.Now().Add(3 * time.Second)
                for time.Now().Before(deadline) {
                        if cond() {
                                return
                        }
                        time.Sleep(5 * time.Millisecond)
                }
                t.Fatalf("timed out waiting for %s", what)
        }

        waitFor(func() bool { return s.IsRunning("claim1") }, "run start")

        tasks := s.Tasks()
        if len(tasks) != 1 || tasks[0].Enabled || !tasks[0].NextDue.IsZero() {
                t.Fatalf("claim not durable while the run is in flight: %+v", tasks)
        }

        close(release)
        wg.Wait()

        if got := <-done; len(got) != 1 || !got[0].OK {
                t.Fatalf("once report: %+v", got)
        }
}

// --- daily / weekly local-time scheduling ------------------------------------

func TestDailyScheduleLocalTimeAndMissedDeadlinePolicy(t *testing.T) {
        dir := t.TempDir()

        var fired atomic.Int32
        s := New(dir, func(ctx context.Context, task Task) (string, error) {
                fired.Add(1)
                return "daily-work", nil
        }, nil)

        // 00:00 local today — always in the past when this test runs.
        if err := s.AddTask(Task{
                ID: "daily1", Name: "daily", Trigger: EventTimer,
                Interval: 5 * time.Minute, Prompt: "p",
                Schedule: Schedule{Kind: ScheduleDaily, TimeOfDay: "00:00"},
        }); err != nil {
                t.Fatal(err)
        }

        // Simulate a stale persisted deadline (created yesterday, machine off
        // over midnight): NextDue = 00:00 LOCAL today, already in the past.
        s.mu.Lock()
        for _, tt := range s.tasks {
                if tt.ID == "daily1" {
                        tt.NextDue = time.Date(time.Now().Year(), time.Now().Month(),
                                time.Now().Day(), 0, 0, 0, 0, time.Local)
                }
        }
        s.mu.Unlock()

        // The missed deadline fires EXACTLY ONCE...
        if got := s.TickDue(context.Background()); len(got) != 1 {
                t.Fatalf("missed daily deadline did not fire: %+v", got)
        }

        // ...and the next occurrence is tomorrow 00:00 LOCAL — never a burst
        // of retroactive runs.
        tasks := s.Tasks()
        next := tasks[0].NextDue
        todayMidnight := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.Local)
        want := todayMidnight.AddDate(0, 0, 1)

        if !next.Equal(want) {
                t.Fatalf("next daily occurrence = %v, want %v (local time, missed deadlines run once)", next, want)
        }

        // Ticking again must not fire (the next deadline is tomorrow).
        if got := s.TickDue(context.Background()); len(got) != 0 {
                t.Fatalf("daily task fired twice for one deadline: %+v", got)
        }

        if got := fired.Load(); got != 1 {
                t.Fatalf("daily task fired %d times, want 1", got)
        }
}

func TestWeeklySchedulePicksNextMatchingWeekday(t *testing.T) {
        now := time.Now()
        sc := Schedule{Kind: ScheduleWeekly, TimeOfDay: "09:30", Weekday: int(now.Weekday())}

        next, err := sc.nextOccurrence(now)
        if err != nil {
                t.Fatal(err)
        }

        // Same weekday: the next occurrence is TODAY 09:30 if still ahead,
        // otherwise NEXT WEEK same weekday.
        todayAt := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, now.Location())
        if now.Before(todayAt) {
                if !next.Equal(todayAt) {
                        t.Fatalf("weekly next = %v, want today 09:30 %v", next, todayAt)
                }
        } else {
                if want := todayAt.AddDate(0, 0, 7); !next.Equal(want) {
                        t.Fatalf("weekly next = %v, want next week %v", next, want)
                }
        }

        if int(next.Weekday()) != sc.Weekday {
                t.Fatalf("weekly next weekday = %v, want %v", next.Weekday(), sc.Weekday)
        }
}

// --- manual run / pause / cancel ----------------------------------------------

func TestRunNowManualRunAndPauseGate(t *testing.T) {
        s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
                return "manual-ok", nil
        }, nil)

        if err := s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "p"}); err != nil {
                t.Fatal(err)
        }

        ch, err := s.RunNow(context.Background(), "m1")
        if err != nil {
                t.Fatal(err)
        }

        report := <-ch
        if !report.OK || report.Output != "manual-ok" || report.Trigger != EventManual {
                t.Fatalf("manual report: %+v", report)
        }
        // v1.7.1 settlement contract: drain to CLOSE before touching
        // anything that outlives the worker (TempDir cleanup runs at test
        // return). Close is the deterministic full-settlement barrier:
        // reports.jsonl + tasks.jsonl persisted, bookkeeping cleared.

        // Last-run pointer + chronological history.
        if s.Tasks()[0].LastRun == nil || !s.Tasks()[0].LastRun.OK {
                t.Fatal("lastRun not recorded on the task")
        }
        if runs := s.Runs("m1", 10); len(runs) != 1 || !runs[0].OK {
                t.Fatalf("run history: %+v", runs)
        }

        // A second concurrent run is rejected while one is in flight.
        blocked := make(chan struct{})
        s2 := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
                select {
                case <-blocked:
                        return "slow", nil
                case <-ctx.Done():
                        return "", ctx.Err()
                }
        }, nil)
        _ = s2.AddTask(Task{ID: "slow1", Name: "slow", Trigger: EventManual, Prompt: "p", MaxRuntime: 5 * time.Second})

        ch2, err := s2.RunNow(context.Background(), "slow1")
        if err != nil {
                t.Fatal(err)
        }

        if _, err := s2.RunNow(context.Background(), "slow1"); !errors.Is(err, ErrAlreadyRunning) {
                t.Fatalf("concurrent run err = %v, want ErrAlreadyRunning", err)
        }

        // Cancel the in-flight run: it settles as canceled.
        if !s2.CancelRun("slow1") {
                t.Fatal("CancelRun could not find the run")
        }
        rep := <-ch2
        if !rep.Canceled {
                t.Fatalf("canceled report: %+v", rep)
        }
        for range ch2 { // settle the canceled run's worker fully
        }
        close(blocked)

        // Pause refuses manual runs; resume re-allows.
        if err := s.Pause("m1"); err != nil {
                t.Fatal(err)
        }
        if _, err := s.RunNow(context.Background(), "m1"); !errors.Is(err, ErrTaskPaused) {
                t.Fatalf("paused run err = %v, want ErrTaskPaused", err)
        }
        if err := s.Resume("m1"); err != nil {
                t.Fatal(err)
        }
        // The final resumed run must be FULLY settled before the test
        // returns: its worker persists into t.TempDir(), and TempDir
        // cleanup races any persistence that outlives the test body.
        // (v1.7.0 regression: this run leaked a goroutine that wrote
        // tasks.jsonl/reports.jsonl during TempDir RemoveAll — "directory
        // is not empty". Drain-to-close is the deterministic barrier; no
        // sleeps, no cleanup retries, no weakened assertions.)
        ch3, err := s.RunNow(context.Background(), "m1")
        if err != nil {
                t.Fatalf("resume: %v", err)
        }
        for range ch3 { // drain until deterministic close = fully settled
        }
}

func TestPauseGatesTimerFiring(t *testing.T) {
        s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
                return "ran", nil
        }, nil)

        if err := s.AddTask(Task{ID: "t1", Name: "timer", Trigger: EventTimer, Interval: 5 * time.Minute, Prompt: "p"}); err != nil {
                t.Fatal(err)
        }

        // Force due, then pause BEFORE the tick.
        s.mu.Lock()
        for _, tt := range s.tasks {
                if tt.ID == "t1" {
                        tt.NextDue = time.Now().Add(-time.Second)
                }
        }
        s.mu.Unlock()

        if err := s.Pause("t1"); err != nil {
                t.Fatal(err)
        }

        if got := s.TickDue(context.Background()); len(got) != 0 {
                t.Fatalf("paused task fired: %+v", got)
        }

        if err := s.Resume("t1"); err != nil {
                t.Fatal(err)
        }

        // After resume the next due is recomputed from NOW (no stale burst).
        if got := s.TickDue(context.Background()); len(got) != 0 {
                t.Fatalf("resumed task fired a stale deadline: %+v", got)
        }
        if tasks := s.Tasks(); tasks[0].NextDue.Before(time.Now()) {
                t.Fatalf("resume did not recompute NextDue: %v", tasks[0].NextDue)
        }
}

// --- event triggers -----------------------------------------------------------

func TestNotifyEventFiresMatchingTasks(t *testing.T) {
        s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
                return "event-work", nil
        }, nil)

        _ = s.AddTask(Task{ID: "ev1", Name: "on test failure", Trigger: EventTestFailure, Prompt: "p"})
        _ = s.AddTask(Task{ID: "ev2", Name: "on build failure", Trigger: EventBuildFail, Prompt: "p"})
        _ = s.AddTask(Task{ID: "ev3", Name: "paused listener", Trigger: EventTestFailure, Prompt: "p"})
        _ = s.Pause("ev3")

        started := s.NotifyEvent(context.Background(), EventTestFailure)
        if len(started) != 1 || started[0] != "ev1" {
                t.Fatalf("NotifyEvent fired %v, want [ev1]", started)
        }

        // Wait for settle and check history.
        deadline := time.Now().Add(3 * time.Second)
        for s.IsRunning("ev1") && time.Now().Before(deadline) {
                time.Sleep(5 * time.Millisecond)
        }

        if runs := s.Runs("ev1", 5); len(runs) != 1 {
                t.Fatalf("event run history: %+v", runs)
        }
        if runs := s.Runs("ev3", 5); len(runs) != 0 {
                t.Fatalf("paused task must never fire: %+v", runs)
        }
}

// --- update / metadata / persistence ------------------------------------------

func TestUpdateTaskAndMetadataRoundTrip(t *testing.T) {
        dir := t.TempDir()
        s := New(dir, func(ctx context.Context, task Task) (string, error) {
                return "w", nil
        }, nil)

        _ = s.AddTask(Task{
                ID: "meta1", Name: "metadata", Trigger: EventTimer, Interval: 5 * time.Minute, Prompt: "p",
                LinkedSkills: []string{"deploy-verify"},
                TaskTools:    []string{"task_report"},
                TaskTypes:    []string{"coding"},
        })

        if err := s.UpdateTask("meta1", func(t *Task) error {
                t.Prompt = "updated prompt"
                return nil
        }); err != nil {
                t.Fatal(err)
        }

        if err := s.UpdateTask("meta1", func(t *Task) error {
                t.Interval = time.Minute // below policy floor
                return nil
        }); err == nil {
                t.Fatal("interval below the floor must be rejected")
        }

        // Reload: metadata + updated prompt survive the process.
        s2 := New(dir, nil, nil)
        if err := s2.Load(); err != nil {
                t.Fatal(err)
        }
        tasks := s2.Tasks()
        if len(tasks) != 1 {
                t.Fatalf("tasks after reload: %+v", tasks)
        }
        got := tasks[0]
        if got.Prompt != "updated prompt" {
                t.Fatalf("prompt after reload: %q", got.Prompt)
        }
        if len(got.LinkedSkills) != 1 || got.LinkedSkills[0] != "deploy-verify" ||
                len(got.TaskTools) != 1 || got.TaskTools[0] != "task_report" ||
                len(got.TaskTypes) != 1 || got.TaskTypes[0] != "coding" {
                t.Fatalf("task metadata lost across reload: %+v", got)
        }
        if got.Interval != 5*time.Minute {
                t.Fatalf("interval round-trip: %v", got.Interval)
        }
}

func TestDeleteTask(t *testing.T) {
        s := New(t.TempDir(), nil, nil)
        _ = s.AddTask(Task{ID: "d1", Name: "doomed", Trigger: EventManual, Prompt: "p"})
        if err := s.RemoveTask("d1"); err != nil {
                t.Fatal(err)
        }
        if tasks := s.Tasks(); len(tasks) != 0 {
                t.Fatalf("task survived delete: %+v", tasks)
        }
        if err := s.RemoveTask("d1"); err == nil {
                t.Fatal("deleting a missing task must error")
        }
}

// --- shutdown settlement -------------------------------------------------------

func TestShutdownSettleCancelsInFlightRuns(t *testing.T) {
        started := make(chan struct{})
        s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
                close(started)
                <-ctx.Done()
                return "", ctx.Err()
        }, nil)

        _ = s.AddTask(Task{ID: "long", Name: "long", Trigger: EventManual, Prompt: "p", MaxRuntime: 30 * time.Second})

        ch, err := s.RunNow(context.Background(), "long")
        if err != nil {
                t.Fatal(err)
        }
        <-started

        s.ShutdownSettle(3 * time.Second)

        rep := <-ch
        if !rep.Canceled {
                t.Fatalf("shutdown did not cancel the run: %+v", rep)
        }
        for range ch { // settle the worker: close follows bookkeeping cleanup
        }
        if s.IsRunning("long") {
                t.Fatal("run survived ShutdownSettle")
        }
}

// TestRunNowChannelCloseIsFullSettlement is the v1.7.1 regression for the
// CI failure in TestRunNowManualRunAndPauseGate ("TempDir cleanup:
// directory is not empty"): a RunNow worker persisted reports.jsonl and
// tasks.jsonl AFTER the test returned, racing t.TempDir() RemoveAll.
//
// The contract locked here: when the RunNow report channel CLOSES, the
// run is already fully settled on disk and in memory — synchronously
// assertable, no sleeps:
//
//   1. the runner function has returned;
//   2. reports.jsonl contains the settled report (read from DISK);
//   3. tasks.jsonl carries LastRun (reloaded from DISK);
//   4. the running/cancel bookkeeping is cleared.
//
// If channel close ever moves before persistence or bookkeeping, this
// test fails deterministically instead of flaking in CI.
func TestRunNowChannelCloseIsFullSettlement(t *testing.T) {
        dir := t.TempDir()

        runnerReturned := make(chan struct{}, 1)
        s := New(dir, func(ctx context.Context, task Task) (string, error) {
                defer func() { runnerReturned <- struct{}{} }()
                return "settled-ok", nil
        }, nil)

        if err := s.AddTask(Task{ID: "settle1", Name: "settle", Trigger: EventManual, Prompt: "p"}); err != nil {
                t.Fatal(err)
        }

        ch, err := s.RunNow(context.Background(), "settle1")
        if err != nil {
                t.Fatal(err)
        }

        var report Report
        for r := range ch { // drain until deterministic close
                report = r
        }
        if !report.OK || report.Output != "settled-ok" || report.Trigger != EventManual {
                t.Fatalf("report: %+v", report)
        }

        // (1) The runner returned before close.
        select {
        case <-runnerReturned:
        default:
                t.Fatal("channel closed before the runner returned")
        }

        // (2) reports.jsonl holds the settled report (disk read, synchronous).
        reports := s.Reports(10)
        if len(reports) != 1 || !reports[0].OK || reports[0].Output != "settled-ok" {
                t.Fatalf("report log not settled at channel close: %+v", reports)
        }

        // (3) tasks.jsonl carries LastRun — proven by a FRESH reload from disk.
        reloaded := New(dir, nil, nil)
        if err := reloaded.Load(); err != nil {
                t.Fatal(err)
        }
        tasks := reloaded.Tasks()
        if len(tasks) != 1 || tasks[0].LastRun == nil || !tasks[0].LastRun.OK {
                t.Fatalf("LastRun not durably persisted at channel close: %+v", tasks)
        }

        // (4) Bookkeeping cleared before close.
        if s.IsRunning("settle1") {
                t.Fatal("channel closed while the run was still registered as running")
        }
}

// --- RunNow refusal paths -------------------------------------------------------

func TestRunNowRefusals(t *testing.T) {
        s := New(t.TempDir(), nil, nil)
        _ = s.AddTask(Task{ID: "x", Name: "x", Trigger: EventManual, Prompt: "p"})

        if _, err := s.RunNow(context.Background(), "missing"); err == nil ||
                !strings.Contains(err.Error(), "not found") {
                t.Fatalf("missing task err = %v", err)
        }
}
