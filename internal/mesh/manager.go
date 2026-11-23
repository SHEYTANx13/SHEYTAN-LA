package mesh

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Status is the manager's lifecycle + queue state (blueprint §9.2).
type Status struct {
	// Enabled mirrors cfg.Enabled — the owner's explicit decision.
	Enabled bool
	// Started reports whether Start succeeded. A disabled manager can
	// never be started; nothing (listener, discovery, worker) exists
	// before Started.
	Started bool
	// Role from the configuration.
	Role string
	// Jobs summarizes the ledger.
	Jobs JobsSummary
}

// JobsSummary is the sanitized job-count view for UI/API surfaces.
type JobsSummary struct {
	Pending   int
	Leased    int
	Running   int
	Committed int
	Failed    int
	Canceled  int
}

// Manager is the SINGLE mesh lifecycle authority (blueprint §9.1
// manager.go / §9.2). Construction has NO side effects: no socket, no
// discovery, no goroutine. Start is refused while the configuration is
// disabled — mesh is OFF by default and only the owner's explicit
// enable turns anything on (work order non-negotiable 5). Phase 1 has
// no transport at all: Start/Stop manage the ledger and coordinator
// lifecycle only, which keeps the seam stable for the phase-2 libp2p
// host (host.go/discovery_*.go join INSIDE this manager, never beside
// it).
type Manager struct {
	mu      sync.Mutex
	cfg     Config
	dir     string
	open    bool
	started bool
	ledger  *Ledger
	coord   *Coordinator
	reg     *Registry
}

// NewManager validates cfg and constructs the manager WITHOUT starting
// anything. The registry starts empty: no AI-inference handler is
// available until an explicit Register call — and registration is
// allowlist-gated, so a UI/UX handler can never join.
func NewManager(cfg Config, dir string) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, fmt.Errorf("%w: data directory required", ErrInvalidConfig)
	}
	return &Manager{cfg: cfg, dir: dir}, nil
}

// Register installs an allowlisted AI-inference handler. It may be
// called before or after Start; the allowlist rules are identical.
func (m *Manager) Register(t JobType, h Handler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reg == nil {
		m.reg = NewRegistry()
	}
	return m.reg.Register(t, h)
}

// Start opens the durable ledger, wires the coordinator, and marks the
// manager started. With cfg.Enabled == false it refuses with
// ErrMeshDisabled and NOTHING is constructed — the fail-closed default.
func (m *Manager) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil // idempotent
	}
	if !m.cfg.Enabled {
		return ErrMeshDisabled
	}
	ledger, err := OpenLedger(m.dir)
	if err != nil {
		return err
	}
	if m.reg == nil {
		m.reg = NewRegistry()
	}
	coord, err := NewCoordinator(m.cfg, ledger, m.reg)
	if err != nil {
		_ = ledger.Close()
		return err
	}
	m.ledger = ledger
	m.coord = coord
	m.started = true
	m.open = true
	return nil
}

// Submit validates against the manager config and dispatches one job
// synchronously in the caller's goroutine (phase 1: in-process,
// deterministic; the phase-2 transport makes this a bounded async offer
// through the SAME ledger/coordinator seam).
func (m *Manager) Submit(ctx context.Context, env Envelope) (JobRecord, error) {
	m.mu.Lock()
	started, coord := m.started, m.coord
	m.mu.Unlock()
	if !started || coord == nil {
		return JobRecord{}, ErrManagerNotStarted
	}
	return coord.Dispatch(ctx, env)
}

// Resume re-drives a pending job found by recovery after a restart.
func (m *Manager) Resume(ctx context.Context, id string) (JobRecord, error) {
	m.mu.Lock()
	started, coord := m.started, m.coord
	m.mu.Unlock()
	if !started || coord == nil {
		return JobRecord{}, ErrManagerNotStarted
	}
	return coord.Resume(ctx, id)
}

// Cancel cancels a non-terminal job (owner action through the loopback
// API in later phases).
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	started, coord := m.started, m.coord
	m.mu.Unlock()
	if !started || coord == nil {
		return ErrManagerNotStarted
	}
	return coord.Cancel(id)
}

// Status snapshots the lifecycle and queue state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		Enabled: m.cfg.Enabled,
		Started: m.started,
		Role:    m.cfg.Role,
	}
	if m.ledger != nil {
		for _, j := range m.ledger.Snapshot() {
			switch j.State {
			case StatePending:
				st.Jobs.Pending++
			case StateLeased, StateOffered:
				st.Jobs.Leased++
			case StateRunning:
				st.Jobs.Running++
			case StateCommitted, StateVerified:
				st.Jobs.Committed++
			case StateFailed:
				st.Jobs.Failed++
			case StateCanceled:
				st.Jobs.Canceled++
			}
		}
	}
	return st
}

// Stop is the bounded idempotent stop: the ledger is flushed and closed,
// the coordinator released, and the manager returns to the not-started
// state (Start may follow again — e.g. after an owner re-enable).
func (m *Manager) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return nil // idempotent
	}
	err := m.ledger.Close()
	m.ledger = nil
	m.coord = nil
	m.started = false
	m.open = false
	return err
}

// Close stops (if started) and marks the manager closed for good; a
// closed manager refuses Start (process shutdown semantics).
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.open = false
	m.mu.Unlock()
	return m.Stop(ctx)
}

// guardTimeout bounds any manager wait by the job deadline floor; kept
// as a helper for the phase-2 transport's bounded waits.
func guardTimeout(d time.Duration) time.Duration {
	if d <= 0 || d > DefaultMaxDeadline {
		return DefaultMaxDeadline
	}
	return d
}
