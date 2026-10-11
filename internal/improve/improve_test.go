package improve

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestProposeRequiresCompleteDefinition(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.Propose(Tactic{ID: "t1", Condition: "build fails", Action: "run tests first"}); err == nil {
		t.Fatal("tactic without a prediction must be rejected")
	}

	err := store.Propose(Tactic{
		ID:         "t1",
		Condition:  "build fails after an edit",
		Action:     "re-run the smallest verification command before diagnosing",
		Prediction: "the failing test is identified within one iteration",
	})
	if err != nil {
		t.Fatalf("valid proposal: %v", err)
	}
}

// TestUnverifiedAttemptsNeverPromote: the model's own success claims can
// never make a tactic active — only objective verification can.
func TestUnverifiedAttemptsNeverPromote(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Propose(Tactic{
		ID: "t1", Condition: "c", Action: "a", Prediction: "p",
	})

	for i := 0; i < 5; i++ {
		state, err := store.RecordAttempt(Attempt{
			TacticID:     "t1",
			Predicted:    "p",
			Observed:     "p",
			Matched:      true,
			Verification: "not_verified", // model prose, no objective evidence
		})
		if err != nil {
			t.Fatal(err)
		}
		if state == StateActive {
			t.Fatal("unverified attempts must never activate a tactic")
		}
	}

	active := store.Active()
	if len(active) != 0 {
		t.Fatalf("active tactics = %d, want 0", len(active))
	}
}

// TestVerifiedPredictionsPromote: two independently verified matching
// predictions promote the tactic; one verified contradiction retires it.
func TestVerifiedPredictionsPromoteThenRetire(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Propose(Tactic{
		ID: "t2", Condition: "c", Action: "a", Prediction: "p",
	})

	state, err := store.RecordAttempt(Attempt{
		TacticID: "t2", Predicted: "p", Observed: "p",
		Matched: true, Verification: "verified",
	})
	if err != nil || state == StateActive {
		t.Fatalf("first verified success must leave the candidate: state=%s err=%v", state, err)
	}

	state, err = store.RecordAttempt(Attempt{
		TacticID: "t2", Predicted: "p", Observed: "p",
		Matched: true, Verification: "verified",
	})
	if err != nil || state != StateActive {
		t.Fatalf("second verified success must activate: state=%s err=%v", state, err)
	}

	active := store.Active()
	if len(active) != 1 {
		t.Fatalf("active = %d, want 1", len(active))
	}

	// A verified contradiction retires the tactic.
	state, err = store.RecordAttempt(Attempt{
		TacticID: "t2", Predicted: "p", Observed: "something else",
		Matched: false, Verification: "verified",
	})
	if err != nil || state != StateRetired {
		t.Fatalf("verified contradiction must retire: state=%s err=%v", state, err)
	}
	if len(store.Active()) != 0 {
		t.Fatal("retired tactic must not stay active")
	}
}

func TestRenderActive(t *testing.T) {
	out := RenderActive([]Tactic{{
		ID: "t", Condition: "the build breaks", Action: "run the smallest check",
		Prediction: "root cause in one step", Successes: 2, Uses: 2,
	}}, 2000)
	if out == "" {
		t.Fatal("rendered block empty")
	}
	if RenderActive(nil, 2000) != "" {
		t.Fatal("empty tactics must render nothing")
	}
}

// deadlockWatchdog fails the test with a diagnostic if fn does not return
// within d. Regression guard for the 1.1.6 CI hang: Propose/RecordAttempt
// re-acquired the store mutex through Load() while already holding it
// (sync.Mutex is not reentrant, so the goroutine deadlocked).
func deadlockWatchdog(t *testing.T, d time.Duration, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		// completed in time
	case <-time.After(d):
		t.Fatalf("%s deadlocked (did not complete within %v) — recursive mutex acquisition regression", name, d)
	}
}

// TestProposeCompletesImmediately proves Propose no longer self-deadlocks
// by calling the locking Load() while holding the store mutex.
func TestProposeCompletesImmediately(t *testing.T) {
	store := NewStore(t.TempDir())
	deadlockWatchdog(t, 10*time.Second, "Propose", func() {
		if err := store.Propose(Tactic{
			ID: "t-dead", Condition: "c", Action: "a", Prediction: "p",
		}); err != nil {
			t.Errorf("Propose: %v", err)
		}
	})
	// A second proposal on a non-empty store exercises loadLocked() again.
	deadlockWatchdog(t, 10*time.Second, "Propose(second)", func() {
		if err := store.Propose(Tactic{
			ID: "t-dead", Condition: "c", Action: "a", Prediction: "p",
		}); err == nil {
			t.Error("duplicate proposal must be rejected")
		}
	})
}

// TestRecordAttemptCompletesImmediately proves RecordAttempt no longer
// self-deadlocks on a populated store.
func TestRecordAttemptCompletesImmediately(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Propose(Tactic{ID: "t-ra", Condition: "c", Action: "a", Prediction: "p"})
	deadlockWatchdog(t, 10*time.Second, "RecordAttempt", func() {
		if _, err := store.RecordAttempt(Attempt{
			TacticID: "t-ra", Predicted: "p", Observed: "p",
			Matched: true, Verification: "verified",
		}); err != nil {
			t.Errorf("RecordAttempt: %v", err)
		}
	})
}

// TestConcurrentStoreAccess hammers the store from many goroutines using
// every public path; nothing may deadlock, panic or lose persisted data.
func TestConcurrentStoreAccess(t *testing.T) {
	store := NewStore(t.TempDir())

	const writers = 8
	const perWriter = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter*2)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := fmt.Sprintf("t-conc-%d", w)
			if err := store.Propose(Tactic{ID: id, Condition: "c", Action: "a", Prediction: "p"}); err != nil {
				errs <- fmt.Errorf("propose %s: %w", id, err)
				return
			}
			for i := 0; i < perWriter; i++ {
				if _, err := store.RecordAttempt(Attempt{
					TacticID:     id,
					Predicted:    "p",
					Observed:     "p",
					Matched:      true,
					Verification: "verified",
				}); err != nil {
					errs <- fmt.Errorf("record %s #%d: %w", id, i, err)
					return
				}
			}
		}(w)
	}

	// Concurrent readers, including Active() and Load().
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = store.Active()
				_, _ = store.Load()
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	all, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != writers {
		t.Fatalf("persisted tactics = %d, want %d", len(all), writers)
	}
	for _, tactic := range all {
		if tactic.Uses != perWriter {
			t.Fatalf("tactic %s uses = %d, want %d (lost update)", tactic.ID, tactic.Uses, perWriter)
		}
		if tactic.State != StateActive {
			t.Fatalf("tactic %s state = %s, want active after %d verified successes", tactic.ID, tactic.State, perWriter)
		}
	}
}

// TestPersistenceRoundTrip: tactics survive a fresh store over the same
// directory, and JSONL state is preserved exactly.
func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Propose(Tactic{ID: "keep", Condition: "c", Action: "a", Prediction: "p"})
	for i := 0; i < 2; i++ {
		_, _ = store.RecordAttempt(Attempt{
			TacticID: "keep", Predicted: "p", Observed: "p",
			Matched: true, Verification: "verified",
		})
	}

	reopened := NewStore(dir)
	all, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded tactics = %d, want 1", len(all))
	}
	got := all[0]
	if got.State != StateActive || got.Successes != 2 || got.Uses != 2 || len(got.Evidence) != 2 {
		t.Fatalf("reloaded tactic drifted: %+v", got)
	}
	if len(reopened.Active()) != 1 {
		t.Fatal("reloaded active tactics lost")
	}
}

// TestEvidenceBounded: the evidence trail is capped (bounded store).
func TestEvidenceBounded(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Propose(Tactic{ID: "t-ev", Condition: "c", Action: "a", Prediction: "p"})
	for i := 0; i < 64; i++ {
		if _, err := store.RecordAttempt(Attempt{
			TacticID: "t-ev", Predicted: "p", Observed: "p",
			Matched: true, Verification: "verified",
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := store.Load()
	if len(all) != 1 || len(all[0].Evidence) > 16 {
		t.Fatalf("evidence not bounded: %d entries", len(all[0].Evidence))
	}
}

// TestStoreBoundedToMaxTactics: the store stays bounded on overflow.
func TestStoreBoundedToMaxTactics(t *testing.T) {
	store := NewStore(t.TempDir())
	for i := 0; i < store.maxTactics+40; i++ {
		if err := store.Propose(Tactic{
			ID: fmt.Sprintf("t-b-%d", i), Condition: "c", Action: "a", Prediction: "p",
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := store.Load()
	if len(all) > store.maxTactics {
		t.Fatalf("store not bounded: %d tactics > %d", len(all), store.maxTactics)
	}
	// Oldest entries are evicted, newest survive.
	if all[len(all)-1].ID != fmt.Sprintf("t-b-%d", store.maxTactics+39) {
		t.Fatalf("newest tactic evicted instead of oldest: %s", all[len(all)-1].ID)
	}
}
