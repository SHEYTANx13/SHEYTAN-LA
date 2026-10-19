package api

// goal_drive.go — v1.10.13 structural extraction (register 3.7, pure
// move): the drive registry/lifecycle machinery moved out of goals.go
// byte-identically — no rename, no signature change, no behavior
// change, I1–I3 untouched. goals.go keeps the HTTP surface and the run
// binding; this file owns the single-flight drive registry.

import (
	"context"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Drive registry (v1.10.13 DRIVE-REGISTRY repair)
//
// Named invariants (encoded here, tested in goal_drive_lifecycle_test.go):
//
//	I1 — Atomic single-flight: admission and reservation happen under
//	     ONE lock; no check-then-insert gap exists anywhere a registry
//	     is used.
//	I2 — Generation ownership: cleanup removes only the record it
//	     owns; no old actor can delete a newer actor's registration.
//	I3 — No discarded outcomes: any goroutine whose result matters has
//	     its result and error observed and settled; "_ =" on a
//	     consequential error is a review failure.
//	I6 — Status and drive always agree: an active goal always has a
//	     live or finishing drive; the invariant is tested, not assumed.
// ---------------------------------------------------------------------------

// goalDriveRecord is ONE live drive generation per goal.
type goalDriveRecord struct {
	gen    uint64
	cancel context.CancelFunc
	// done closes exactly when the drive goroutine has fully unwound
	// (after the generation-safe release below). The resume path
	// serializes with the old generation on this channel — bounded
	// seconds, never the acceptance suite's own bounds.
	done chan struct{}
}

// goalDrives tracks the single-flight drive per goal (one loop at a time;
// a second start on a live goal is an honest typed 409, never a parallel
// loop and never a generic 500 for a transient unwind).
var goalDrives = struct {
	mu      sync.Mutex
	nextGen uint64
	live    map[string]*goalDriveRecord
}{live: map[string]*goalDriveRecord{}}

// goalDriveStopWait bounds how long a resume waits for the previous drive
// generation to unwind. Seconds, deliberately.
var goalDriveStopWait = 5 * time.Second

// goalDriveAdmit atomically admits a new drive generation: the duplicate
// check and the reservation are ONE critical section (I1 — the former
// check-then-insert gap let two concurrent starts both pass and
// overwrite). On conflict it returns the live record; the caller answers
// with a typed conflict.
func goalDriveAdmit(id string, cancel context.CancelFunc) (*goalDriveRecord, bool) {
	goalDrives.mu.Lock()
	defer goalDrives.mu.Unlock()
	if cur, ok := goalDrives.live[id]; ok {
		return cur, false
	}
	goalDrives.nextGen++
	rec := &goalDriveRecord{gen: goalDrives.nextGen, cancel: cancel, done: make(chan struct{})}
	goalDrives.live[id] = rec
	return rec, true
}

// goalDriveLookup returns the goal's live drive record, if any.
func goalDriveLookup(id string) *goalDriveRecord {
	goalDrives.mu.Lock()
	defer goalDrives.mu.Unlock()
	return goalDrives.live[id]
}

// goalDriveWaitStop waits bounded for the goal's live drive generation to
// unwind. false means the generation is still live after the bound — the
// caller answers with a typed conflict naming the state.
func goalDriveWaitStop(id string, timeout time.Duration) bool {
	rec := goalDriveLookup(id)
	if rec == nil {
		return true
	}
	select {
	case <-rec.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// goalDriveCancel cancels the goal's live drive generation WITHOUT
// releasing the reservation: the goroutine's own cleanup releases the
// record when the unwinding completes, so a replacement can never overlap
// a cancelled generation (the former code deleted the entry before the
// goroutine had exited).
func goalDriveCancel(id string) bool {
	rec := goalDriveLookup(id)
	if rec == nil {
		return false
	}
	rec.cancel()
	return true
}

// goalDriveRelease is the generation-safe cleanup (I2): the record is
// removed ONLY if the registry still points at THIS record (an old
// generation can never delete a newer generation's registration), and the
// done channel closes only AFTER the removal — the reservation outlives
// the goroutine's entire unwinding.
func goalDriveRelease(id string, rec *goalDriveRecord) {
	goalDrives.mu.Lock()
	if cur, ok := goalDrives.live[id]; ok && cur == rec {
		delete(goalDrives.live, id)
	}
	goalDrives.mu.Unlock()
	close(rec.done)
}
