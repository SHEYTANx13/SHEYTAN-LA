package multiagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func spec(id string, readOnly bool, ms time.Duration) SubtaskSpec {
	return SubtaskSpec{
		SubtaskID: id, Role: RoleResearcher, Objective: "objective " + id,
		ReadOnly: readOnly, Deadline: ms,
	}
}

func TestDeterministicMergeInSubtaskIDOrder(t *testing.T) {
	// Completion order is deliberately REVERSED (later ids finish
	// first); the merge must still return subtaskId order.
	var mu sync.Mutex
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		delay := map[string]time.Duration{
			"st-1": 60 * time.Millisecond,
			"st-2": 40 * time.Millisecond,
			"st-3": 10 * time.Millisecond,
		}[s.SubtaskID]
		time.Sleep(delay)
		mu.Lock()
		defer mu.Unlock()
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted, Result: "done " + s.SubtaskID}
	}

	results := RunSubtasks(context.Background(),
		[]SubtaskSpec{spec("st-1", true, time.Second), spec("st-2", true, time.Second), spec("st-3", true, time.Second)},
		executor, nil)

	if len(results) != 3 {
		t.Fatalf("3 results expected")
	}
	for i, r := range results {
		want := fmt.Sprintf("st-%d", i+1)
		if r.SubtaskID != want {
			t.Fatalf("merge order violated at %d: got %s want %s", i, r.SubtaskID, want)
		}
	}
}

func TestReadOnlyParallelismBoundedToTwo(t *testing.T) {
	var concurrent int32
	var peak int32
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		c := atomic.AddInt32(&concurrent, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if c <= p || atomic.CompareAndSwapInt32(&peak, p, c) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted}
	}

	specs := make([]SubtaskSpec, 0, 6)
	for i := 1; i <= 6; i++ {
		specs = append(specs, spec(fmt.Sprintf("st-%d", i), true, time.Second))
	}
	RunSubtasks(context.Background(), specs, executor, nil)

	if peak > MaxSimultaneousReadOnly {
		t.Fatalf("read-only parallelism exceeded the bound: peak %d", peak)
	}
	if peak < 2 {
		t.Fatalf("read-only parallelism should reach 2, peak %d", peak)
	}
}

func TestMutatingSubtasksStrictlySerialized(t *testing.T) {
	var running int32
	var violated int32
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		if atomic.AddInt32(&running, 1) > 1 {
			atomic.AddInt32(&violated, 1)
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted}
	}

	specs := make([]SubtaskSpec, 0, 4)
	for i := 1; i <= 4; i++ {
		specs = append(specs, spec(fmt.Sprintf("st-%d", i), false, time.Second))
	}
	RunSubtasks(context.Background(), specs, executor, nil)

	if violated != 0 {
		t.Fatalf("mutating subtasks ran concurrently %d times", violated)
	}
}

func TestConcurrencySeamCanOnlyTighten(t *testing.T) {
	var peak int32
	var concurrent int32
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		c := atomic.AddInt32(&concurrent, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if c <= p || atomic.CompareAndSwapInt32(&peak, p, c) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted}
	}

	specs := make([]SubtaskSpec, 0, 4)
	for i := 1; i <= 4; i++ {
		specs = append(specs, spec(fmt.Sprintf("st-%d", i), true, time.Second))
	}
	RunSubtasks(context.Background(), specs, executor, func() int { return 1 })

	if peak > 1 {
		t.Fatalf("the concurrency seam must be able to tighten to 1, peak %d", peak)
	}
	// A WIDER seam value must be ignored (never widen the default bound).
	RunSubtasks(context.Background(), specs, executor, func() int { return 16 })
	if atomic.LoadInt32(&peak) > MaxSimultaneousReadOnly {
		t.Fatalf("the seam must never widen the bound, peak %d", peak)
	}
}

func TestFailedSubtaskStaysFailed(t *testing.T) {
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		if s.SubtaskID == "st-2" {
			return SubtaskResult{
				SubtaskID: s.SubtaskID, Status: SubtaskFailed,
				Result:     "tool failed deterministically",
				Unresolved: []string{"the compiler is not installed"},
			}
		}
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted, Result: "ok"}
	}

	results := RunSubtasks(context.Background(),
		[]SubtaskSpec{spec("st-1", true, time.Second), spec("st-2", true, time.Second)},
		executor, nil)

	if results[1].Status != SubtaskFailed || len(results[1].Unresolved) == 0 {
		t.Fatalf("failed subtask must stay failed with unresolved issues")
	}
	merged := MergeSubtaskResults(results)
	if !strings.Contains(merged, "1 failed/blocked") ||
		!strings.Contains(merged, "the compiler is not installed") {
		t.Fatalf("merge must surface failures honestly: %s", merged)
	}
}

func TestDeadlineIsAnHonestBlock(t *testing.T) {
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		<-ctx.Done() // the subtask hangs past its deadline
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskBlocked,
			Unresolved: []string{"deadline exceeded"}}
	}

	results := RunSubtasks(context.Background(),
		[]SubtaskSpec{spec("st-1", true, 40*time.Millisecond)},
		executor, nil)
	if results[0].Status != SubtaskBlocked {
		t.Fatalf("deadline hit must be an honest block, got %q", results[0].Status)
	}
}

func TestNoRunawayFanOut(t *testing.T) {
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted}
	}
	specs := make([]SubtaskSpec, 0, 50)
	for i := 1; i <= 50; i++ {
		specs = append(specs, spec(fmt.Sprintf("st-%d", i), true, time.Second))
	}
	results := RunSubtasks(context.Background(), specs, executor, nil)
	if len(results) > MaxSubtasksPerGoal {
		t.Fatalf("fan-out must be bounded to %d, got %d", MaxSubtasksPerGoal, len(results))
	}
}

func TestParentCancellationBlocksUnstarted(t *testing.T) {
	executor := func(ctx context.Context, s SubtaskSpec) SubtaskResult {
		time.Sleep(30 * time.Millisecond)
		return SubtaskResult{SubtaskID: s.SubtaskID, Status: SubtaskCompleted}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	specs := make([]SubtaskSpec, 0, 4)
	for i := 1; i <= 4; i++ {
		specs = append(specs, spec(fmt.Sprintf("st-%d", i), false, time.Second))
	}
	results := RunSubtasks(ctx, specs, executor, nil)

	blockedOrDone := 0
	for _, r := range results {
		if r.Status == SubtaskCompleted || r.Status == SubtaskBlocked {
			blockedOrDone++
		}
	}
	if blockedOrDone != 4 {
		t.Fatalf("every subtask needs an honest terminal record, got %d", blockedOrDone)
	}
}
