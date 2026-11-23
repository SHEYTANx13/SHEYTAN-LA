package mesh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Ledger is the controller-owned durable job ledger (blueprint §9.1
// ledger.go; work order non-negotiable 8: controller-owned durable
// state, no consensus, no replicated sessions, no second scheduler).
//
// Persistence: ONE JSON document per mesh data directory, written
// atomically (temp file + rename + fsync) under a mutex. Restart
// recovery reloads the document and re-arms in-flight jobs: attempts
// whose lease has expired move back to pending while the retry cap
// allows, else the job fails closed with ErrRetryCapExceeded.
type Ledger struct {
	mu   sync.Mutex
	path string
	jobs map[string]*JobRecord
	open bool
	now  func() time.Time
}

// OpenLedger loads (or initializes) the durable ledger under dir. The
// directory is created with 0700; the document itself is written with
// 0600. The mesh is local-first and single-user — no world-readable
// state exists.
func OpenLedger(dir string) (*Ledger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mesh: ledger dir: %w", err)
	}
	l := &Ledger{
		path: filepath.Join(dir, "mesh-jobs.json"),
		jobs: make(map[string]*JobRecord),
		open: true,
		now:  time.Now,
	}
	if raw, err := os.ReadFile(l.path); err == nil {
		if err := json.Unmarshal(raw, &l.jobs); err != nil {
			return nil, fmt.Errorf("mesh: ledger %s is corrupt: %w", l.path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("mesh: ledger read: %w", err)
	}
	l.recover()
	return l, nil
}

// recover re-arms in-flight jobs after a restart. A leased/running/
// result_received attempt with an EXPIRED lease cannot be resumed by the
// controller (the worker's lease is dead) — the job returns to pending
// for a fresh attempt while the retry cap allows; otherwise it fails
// closed. Unexpired leases from a restart are broken too (the worker
// process died with the controller): they expire naturally, so only the
// expired ones are re-armed now and the rest when their TTL lapses.
func (l *Ledger) recover() {
	now := l.now()
	for _, j := range l.jobs {
		if terminal(j.State) {
			continue
		}
		switch j.State {
		case StateLeased, StateRunning, StateResultReceived:
			last := j.Attempts[len(j.Attempts)-1]
			if now.After(last.ExpiresAt) {
				l.requeue(j, now)
			}
		case StateOffered:
			// Offers carry no lease; a restart voids them.
			l.requeue(j, now)
		}
	}
	_ = l.persistLocked()
}

func (l *Ledger) requeue(j *JobRecord, now time.Time) {
	// The retry cap is re-checked by GrantLease before the next lease
	// (the live config cap), so recovery simply returns the job to the
	// pending queue; an exhausted job fails closed at the next grant.
	j.State = StatePending
	j.UpdatedAt = now
}

// persistLocked writes the document atomically. The mutex must be held.
func (l *Ledger) persistLocked() error {
	raw, err := json.MarshalIndent(l.jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("mesh: ledger marshal: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("mesh: ledger write: %w", err)
	}
	if f, ferr := os.Open(tmp); ferr == nil {
		// Sync before rename: a crash must never leave the rename with
		// unsynced content behind it.
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("mesh: ledger rename: %w", err)
	}
	return nil
}

// Create validates the envelope against cfg and inserts a pending row.
func (l *Ledger) Create(env Envelope, cfg Config) (*JobRecord, error) {
	if err := env.validate(cfg); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open {
		return nil, ErrLedgerClosed
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := l.now()
	deadline := now.Add(cfg.MaxDeadline)
	if env.Deadline > 0 {
		deadline = now.Add(env.Deadline)
	}
	rec := &JobRecord{
		ID:         id,
		Type:       env.Type,
		Privacy:    env.Privacy,
		Payload:    append([]byte(nil), env.Payload...),
		CreatedAt:  now,
		DeadlineAt: deadline,
		State:      StatePending,
		UpdatedAt:  now,
	}
	l.jobs[id] = rec
	if err := l.persistLocked(); err != nil {
		delete(l.jobs, id)
		return nil, err
	}
	cp := *rec
	return &cp, nil
}

// Snapshot returns a sorted copy of every job (read-only view).
func (l *Ledger) Snapshot() []JobRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]JobRecord, 0, len(l.jobs))
	for _, j := range l.jobs {
		out = append(out, *j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].CreatedAt.Before(out[k].CreatedAt) })
	return out
}

// Get copies one job row.
func (l *Ledger) Get(id string) (JobRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j, ok := l.jobs[id]
	if !ok {
		return JobRecord{}, false
	}
	return *j, true
}

// mutate applies fn to the live row under the lock, persists, and
// returns a copy. fn must not call back into the ledger.
func (l *Ledger) mutate(id string, fn func(j *JobRecord, now time.Time) error) (JobRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open {
		return JobRecord{}, ErrLedgerClosed
	}
	j, ok := l.jobs[id]
	if !ok {
		return JobRecord{}, fmt.Errorf("mesh: unknown job %s", id)
	}
	if err := fn(j, l.now()); err != nil {
		return JobRecord{}, err
	}
	j.UpdatedAt = l.now()
	if err := l.persistLocked(); err != nil {
		return JobRecord{}, err
	}
	return *j, nil
}

// GrantLease moves a pending (re-)queueable job to leased for worker,
// opening the NEXT fencing epoch. It refuses terminal jobs, expired
// deadlines, jobs whose retry budget is exhausted, and live leases.
func (l *Ledger) GrantLease(id, worker string, ttl time.Duration, retryCap int) (JobRecord, error) {
	return l.mutate(id, func(j *JobRecord, now time.Time) error {
		if terminal(j.State) {
			return fmt.Errorf("mesh: job %s is terminal (%s)", id, j.State)
		}
		if j.State == StateLeased || j.State == StateRunning || j.State == StateResultReceived {
			last := j.Attempts[len(j.Attempts)-1]
			if now.Before(last.ExpiresAt) {
				return fmt.Errorf("%w: attempt %d lease until %s", ErrLeaseExpired, last.Epoch, last.ExpiresAt.Format(time.RFC3339))
			}
		}
		if now.After(j.DeadlineAt) {
			j.State = StateFailed
			j.FailureCode = ErrLeaseExpired.Error()
			return fmt.Errorf("%w: deadline passed", ErrLeaseExpired)
		}
		if len(j.Attempts) >= retryCap {
			j.State = StateFailed
			j.FailureCode = ErrRetryCapExceeded.Error()
			return fmt.Errorf("%w: %d attempts used of cap %d", ErrRetryCapExceeded, len(j.Attempts), retryCap)
		}
		j.Epoch = len(j.Attempts) + 1
		j.Attempts = append(j.Attempts, Attempt{
			Epoch:     j.Epoch,
			WorkerID:  worker,
			GrantedAt: now,
			ExpiresAt: now.Add(ttl),
		})
		j.State = StateLeased
		return nil
	})
}

// MarkRunning records that the leased attempt started executing.
func (l *Ledger) MarkRunning(id string, epoch int) error {
	_, err := l.mutate(id, func(j *JobRecord, now time.Time) error {
		if j.State != StateLeased {
			return fmt.Errorf("mesh: job %s state %s, want leased", id, j.State)
		}
		if err := j.currentEpoch(epoch); err != nil {
			return err
		}
		j.State = StateRunning
		return nil
	})
	return err
}

// DeliverResult records the attempt's result under its fencing epoch.
// A result from any epoch other than the CURRENT one is rejected as
// stale (work order: stale-result rejection) and the ledger is left
// untouched for that attempt's outcome.
func (l *Ledger) DeliverResult(id string, epoch int, result []byte, cfg Config) error {
	_, err := l.mutate(id, func(j *JobRecord, now time.Time) error {
		if terminal(j.State) {
			return fmt.Errorf("mesh: job %s is terminal (%s)", id, j.State)
		}
		if err := j.currentEpoch(epoch); err != nil {
			return err
		}
		last := &j.Attempts[len(j.Attempts)-1]
		if now.After(last.ExpiresAt) {
			return ErrLeaseExpired
		}
		limit := cfg.MaxResultBytes
		if limit <= 0 {
			limit = DefaultMaxResultBytes
		}
		if len(result) > limit {
			return fmt.Errorf("%w: result %d bytes > bound %d", ErrPayloadTooLarge, len(result), limit)
		}
		last.Result = append([]byte(nil), result...)
		last.FinishedAt = now
		j.Result = last.Result
		j.State = StateResultReceived
		return nil
	})
	return err
}

// MarkVerified records controller verification of the delivered result.
func (l *Ledger) MarkVerified(id string) error {
	_, err := l.mutate(id, func(j *JobRecord, _ time.Time) error {
		if j.State != StateResultReceived {
			return fmt.Errorf("mesh: job %s state %s, want result_received", id, j.State)
		}
		j.State = StateVerified
		return nil
	})
	return err
}

// Commit is the terminal success transition.
func (l *Ledger) Commit(id string) error {
	_, err := l.mutate(id, func(j *JobRecord, _ time.Time) error {
		if j.State != StateVerified {
			return fmt.Errorf("mesh: job %s state %s, want verified", id, j.State)
		}
		j.State = StateCommitted
		return nil
	})
	return err
}

// Fail closes the job as failed with a typed failure code.
func (l *Ledger) Fail(id string, code error) error {
	_, err := l.mutate(id, func(j *JobRecord, _ time.Time) error {
		if terminal(j.State) {
			return fmt.Errorf("mesh: job %s is terminal (%s)", id, j.State)
		}
		j.State = StateFailed
		j.FailureCode = code.Error()
		return nil
	})
	return err
}

// Cancel closes a non-terminal job as canceled.
func (l *Ledger) Cancel(id string) error {
	_, err := l.mutate(id, func(j *JobRecord, _ time.Time) error {
		if terminal(j.State) {
			return fmt.Errorf("mesh: job %s is terminal (%s)", id, j.State)
		}
		j.State = StateCanceled
		return nil
	})
	return err
}

// RequeueAfterAttemptFailure records a failed attempt (code on the
// attempt row), returns the job to pending while the retry cap allows,
// and fails the job closed with ErrRetryCapExceeded once the cap is
// reached (blueprint §6.7 retry policy: bounded retries, then fail).
func (l *Ledger) RequeueAfterAttemptFailure(id string, retryCap int, code error) error {
	_, err := l.mutate(id, func(j *JobRecord, now time.Time) error {
		if terminal(j.State) {
			return fmt.Errorf("mesh: job %s is terminal (%s)", id, j.State)
		}
		if len(j.Attempts) > 0 {
			j.Attempts[len(j.Attempts)-1].ErrCode = code.Error()
			j.Attempts[len(j.Attempts)-1].FinishedAt = now
		}
		if len(j.Attempts) >= retryCap {
			j.State = StateFailed
			j.FailureCode = ErrRetryCapExceeded.Error()
			return nil
		}
		j.State = StatePending
		j.UpdatedAt = now
		return nil
	})
	return err
}

// RequeueAfterLeaseExpiry is called by the coordinator when an attempt
// outlives its lease: the epoch stays for forensics, the job returns to
// pending while the retry cap allows, else it fails closed.
func (l *Ledger) RequeueAfterLeaseExpiry(id string, retryCap int) error {
	_, err := l.mutate(id, func(j *JobRecord, now time.Time) error {
		if terminal(j.State) {
			return fmt.Errorf("mesh: job %s is terminal (%s)", id, j.State)
		}
		if len(j.Attempts) >= retryCap {
			j.State = StateFailed
			j.FailureCode = ErrRetryCapExceeded.Error()
			return nil
		}
		j.State = StatePending
		j.UpdatedAt = now
		return nil
	})
	return err
}

// Close flushes and closes the ledger; further mutations fail with
// ErrLedgerClosed.
func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open {
		return nil
	}
	err := l.persistLocked()
	l.open = false
	return err
}

// currentEpoch enforces the fencing contract on the live row.
func (j *JobRecord) currentEpoch(epoch int) error {
	if epoch != j.Epoch || len(j.Attempts) == 0 {
		return fmt.Errorf("%w: attempt epoch %d, current epoch %d", ErrStaleResult, epoch, j.Epoch)
	}
	return nil
}
