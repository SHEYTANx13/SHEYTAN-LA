package mesh

import (
	"context"
	"time"
)

// Test-only seams. These helpers expose the manager's internal ledger /
// coordinator state so the deterministic tests can drive single
// transitions (stale delivery, recovery expiry, create-only) without a
// transport layer. They live ONLY in test files — the production
// surface is Start/Submit/Resume/Cancel/Status/Stop/Close/Register.

func (m *Manager) ledgerGet(id string) (JobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledger == nil {
		return JobRecord{}, false
	}
	return m.ledger.Get(id)
}

func (m *Manager) ledgerCreateOnly(env Envelope) (JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledger == nil {
		return JobRecord{}, ErrManagerNotStarted
	}
	rec, err := m.ledger.Create(env, m.cfg)
	if err != nil {
		return JobRecord{}, err
	}
	return *rec, nil
}

func (m *Manager) ledgerSnapshotLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledger == nil {
		return 0
	}
	return len(m.ledger.Snapshot())
}

func (m *Manager) ledgerGrantLease(id, worker string) (JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ledger.GrantLease(id, worker, m.cfg.LeaseTTL, m.cfg.RetryCap)
}

// ledgerDeliverStale delivers a result under epoch+1000 — always stale.
func (m *Manager) ledgerDeliverStale(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.ledger.Get(id)
	if !ok {
		return ErrManagerNotStarted
	}
	return m.ledger.DeliverResult(id, rec.Epoch+1000, []byte("stale-result"), m.cfg)
}

// ledgerDeliver delivers result under the given epoch (stale or current).
func (m *Manager) ledgerDeliver(id string, epoch int, result []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledger == nil {
		return ErrManagerNotStarted
	}
	return m.ledger.DeliverResult(id, epoch, result, m.cfg)
}

// ledgerExpireLeaseForRecovery rewinds the live lease into the past so
// a restart's recover() sees an expired lease (the deterministic
// stand-in for a TTL that lapses while the process is down).
func (m *Manager) ledgerExpireLeaseForRecovery(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.ledger.mutate(id, func(j *JobRecord, _ time.Time) error {
		if len(j.Attempts) == 0 {
			return ErrLeaseExpired
		}
		last := &j.Attempts[len(j.Attempts)-1]
		last.ExpiresAt = time.Now().Add(-time.Minute)
		return nil
	})
	return err
}

var _ = context.Background // keep the import if future seams need it
