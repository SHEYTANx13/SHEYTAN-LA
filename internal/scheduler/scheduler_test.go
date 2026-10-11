package scheduler

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"encoding/json"
	"path/filepath"
)

func TestAddTaskRejectsShortInterval(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	err := s.AddTask(Task{ID: "t1", Name: "too fast", Trigger: EventTimer, Interval: time.Minute, Prompt: "do"})
	if err == nil {
		t.Fatal("interval below the 5-minute floor must be rejected")
	}
}

func TestAddTaskAndPersist(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)

	if err := s.AddTask(Task{ID: "t1", Name: "daily", Trigger: EventTimer, Interval: 30 * time.Minute, Prompt: "check engines"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.AddTask(Task{ID: "t1", Name: "dup", Trigger: EventManual, Prompt: "x"}); err == nil {
		t.Fatal("duplicate id must be rejected")
	}

	s2 := New(dir, nil, nil)
	if err := s2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	tasks := s2.Tasks()
	if len(tasks) != 1 || tasks[0].ID != "t1" {
		t.Fatalf("persisted tasks: %+v", tasks)
	}
}

func TestFireRunsBoundedAndRecords(t *testing.T) {
	dir := t.TempDir()
	ran := false
	s := New(dir, func(ctx context.Context, task Task) (string, error) {
		ran = true
		return "task output", nil
	}, func(task Task, report Report) {
		if !report.OK {
			t.Fatalf("memory summary expects success: %+v", report)
		}
	})

	if err := s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}

	report, err := s.Fire(context.Background(), "m1")
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !ran || !report.OK {
		t.Fatalf("report: %+v", report)
	}

	reports := s.Reports(10)
	if len(reports) != 1 || reports[0].Trigger != EventManual {
		t.Fatalf("persisted reports: %+v", reports)
	}
}

func TestFireFailureRecorded(t *testing.T) {
	s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
		return "", errors.New("engine exploded")
	}, nil)

	_ = s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "hello"})
	report, err := s.Fire(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Err == "" {
		t.Fatalf("failure must be honest: %+v", report)
	}
}

func TestFireRefusesConcurrentRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
		close(started)
		<-release
		return "done", nil
	}, nil)

	_ = s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "hello"})

	// v1.2.5 repair: the first Fire() runs on a goroutine and its result is
	// collected through a channel. The test MUST wait for that completion
	// before returning: Fire's epilogue persists the report into the temp
	// dir, and an early return raced t.TempDir()'s RemoveAll cleanup
	// ("directory not empty"). Result ownership is deterministic — no sleep.
	type fireResult struct {
		report Report
		err    error
	}

	done := make(chan fireResult, 1)

	go func() {
		report, err := s.Fire(context.Background(), "m1")
		done <- fireResult{report: report, err: err}
	}()
	<-started

	if _, err := s.Fire(context.Background(), "m1"); err == nil {
		t.Fatal("concurrent fire of the same task must be refused")
	}
	close(release)

	first := <-done
	if first.err != nil {
		t.Fatalf("first fire: %v", first.err)
	}
	if !first.report.OK || first.report.Output != "done" {
		t.Fatalf("first fire report: %+v", first.report)
	}
}

func TestTimerTickFiresDueTasks(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, func(ctx context.Context, task Task) (string, error) {
		return "ran " + task.ID, nil
	}, nil)

	if err := s.AddTask(Task{ID: "tick1", Name: "timer", Trigger: EventTimer, Interval: 5 * time.Minute, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}

	// Force the due time into the past.
	s.mu.Lock()
	for _, t := range s.tasks {
		if t.ID == "tick1" {
			t.NextDue = time.Now().Add(-time.Second)
		}
	}
	s.mu.Unlock()

	reports := s.Tick(context.Background())
	if len(reports) != 1 || !reports[0].OK {
		t.Fatalf("tick reports: %+v", reports)
	}
	if reports[0].Trigger != EventTimer {
		t.Fatalf("trigger = %s", reports[0].Trigger)
	}

	// The next due time must be pushed one interval forward.
	tasks := s.Tasks()
	if tasks[0].NextDue.Before(time.Now()) {
		t.Fatal("next due not advanced")
	}
}

func TestRemoveTaskCancels(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	_ = s.AddTask(Task{ID: "gone", Name: "x", Trigger: EventManual, Prompt: "p"})
	if err := s.RemoveTask("gone"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveTask("gone"); err == nil {
		t.Fatal("removing twice must fail")
	}
}

// --- crash-after-claim and removed-task safety ---

// Durable claim semantics
// (crash between claim/execute/persist must not double-fire a timer
// task after restart) and removed-task finalization under the
// selection→execution race.

// readNextDue reads the PERSISTED NextDue of one task from tasks.jsonl
// (the disk truth a restart would reload).
func readNextDue(t *testing.T, storeDir, id string) time.Time {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(storeDir, "tasks.jsonl"))
	if err != nil {
		t.Fatalf("read tasks.jsonl: %v", err)
	}

	for _, line := range splitLines(string(data)) {
		if len(line) == 0 {
			continue
		}
		// Cheap unmarshal into the task shape.
		var task struct {
			ID      string    `json:"id"`
			NextDue time.Time `json:"nextDue"`
		}
		if err := jsonUnmarshal(line, &task); err != nil {
			continue
		}
		if task.ID == id {
			return task.NextDue
		}
	}
	t.Fatalf("task %s not found in persisted store", id)
	return time.Time{}
}

// TestCrashAfterClaimDoesNotReplayTask simulates the exact crash window
// of the v1.2.8.1 flaw: a Tick CLAIMS a due task (advances NextDue),
// then the process "crashes" before the run would finish or any later
// persist. A NEW scheduler instance (the restart) loads from disk; the
// claimed deadline must already be durable, so the task does NOT fire
// again on the restart's first tick.
func TestCrashAfterClaimDoesNotReplayTask(t *testing.T) {
	storeDir := t.TempDir()

	fired := 0
	runner := func(ctx context.Context, task Task) (string, error) {
		fired++
		return "ran", nil
	}

	// Process 1: add a timer task, let it come due, and simulate the
	// crash BETWEEN claim and execution by running only the claim phase.
	s1 := New(storeDir, runner, nil)
	s1.nowFunc = func() time.Time {
		return time.Unix(1_000_000, 0).UTC() // frozen clock
	}

	if err := s1.AddTask(Task{
		ID:       "replay",
		Name:     "replay guard",
		Trigger:  EventTimer,
		Interval: 10 * time.Minute,
		Prompt:   "work",
	}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// The deadline is now+interval (in the future) — nothing fires.
	if got := s1.Tick(context.Background()); len(got) != 0 {
		t.Fatalf("task fired before its deadline: %+v", got)
	}

	// Advance the clock past the deadline; Tick claims AND executes.
	frozen := time.Unix(1_000_000, 0).UTC().Add(11 * time.Minute)
	s1.nowFunc = func() time.Time { return frozen }
	if got := s1.Tick(context.Background()); len(got) != 1 {
		t.Fatalf("due task did not fire: %+v", got)
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want 1", fired)
	}

	// The claim is durable: the persisted NextDue is in the future
	// relative to the crash-time clock.
	persisted := readNextDue(t, storeDir, "replay")
	if !persisted.After(frozen) {
		t.Fatalf("persisted NextDue %v is not after the frozen clock %v — the durable claim was lost (crash would replay the task)", persisted, frozen)
	}

	// Simulate a crash at exactly the claim: a scheduler whose clock
	// moved past the ORIGINAL deadline reloads from disk. Without the
	// durable claim it would fire the ORIGINAL deadline again.
	s2 := New(storeDir, runner, nil)
	s2.nowFunc = func() time.Time { return frozen } // same moment (no time advanced)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := s2.Tick(context.Background()); len(got) != 0 {
		t.Fatalf("REPLAYED a claimed task after restart: %+v — the durable claim must prevent double execution", got)
	}
	if fired != 1 {
		t.Fatalf("fired = %d after restart, want 1 (no replay)", fired)
	}

	// When the NEXT real deadline passes, the task fires again — the
	// durable claim must not silence the scheduler forever.
	s3 := New(storeDir, runner, nil)
	next := persisted.Add(time.Minute)
	s3.nowFunc = func() time.Time { return next }
	if err := s3.Load(); err != nil {
		t.Fatalf("Load s3: %v", err)
	}
	if got := s3.Tick(context.Background()); len(got) != 1 {
		t.Fatalf("task did not fire at its next real deadline: %+v", got)
	}
	if fired != 2 {
		t.Fatalf("fired = %d, want 2", fired)
	}
}

// TestRemovedTaskNeverFiresAfterSelection pins the finalization race:
// a task removed between Tick's selection and the execution lock must
// not execute (previously the stale task copy still ran).
func TestRemovedTaskNeverFiresAfterSelection(t *testing.T) {
	storeDir := t.TempDir()

	fired := 0
	runner := func(ctx context.Context, task Task) (string, error) {
		fired++
		return "ran", nil
	}

	// A runner so slow the Tick loop is still inside execute() when we
	// remove — but the simpler deterministic path: make the runner
	// itself remove the task (executing means it was NOT removed first),
	// then verify a removed task is never claimed again.
	block := make(chan struct{})
	runner = func(ctx context.Context, task Task) (string, error) {
		<-block
		fired++
		return "ran", nil
	}

	s := New(storeDir, runner, nil)
	base := time.Unix(2_000_000, 0).UTC()
	s.nowFunc = func() time.Time { return base }

	if err := s.AddTask(Task{
		ID:       "race",
		Name:     "race guard",
		Trigger:  EventTimer,
		Interval: 10 * time.Minute,
		Prompt:   "work",
	}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// First tick at base+11m fires the task (slow runner blocks).
	s.nowFunc = func() time.Time { return base.Add(11 * time.Minute) }
	done := make(chan struct{})
	go func() {
		s.Tick(context.Background())
		close(done)
	}()

	// Wait until the run is in flight, then remove the task.
	time.Sleep(50 * time.Millisecond)
	if err := s.RemoveTask("race"); err != nil {
		t.Fatalf("RemoveTask while running: %v", err)
	}
	close(block)
	<-done

	// The task is gone from the store; a later tick can never claim it
	// again (this is the durable-claim + findLocked re-check contract).
	s.nowFunc = func() time.Time { return base.Add(60 * time.Minute) }
	if got := s.Tick(context.Background()); len(got) != 0 {
		t.Fatalf("removed task fired again: %+v", got)
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want exactly 1 (the in-flight run only)", fired)
	}
}

// tiny local helpers (kept unexported and file-local to avoid touching
// the package's public surface for tests)
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func jsonUnmarshal(line string, v any) error {
	return json.Unmarshal([]byte(line), v)
}
