// Package memmanager implements the v1.2.4 runtime memory policy.
//
// Before this package, cleanup was ad-hoc: each component bounded itself
// (contextcache LRU, recall 5000 capsules, telemetry 4096 records) but
// nothing coordinated release, nothing measured reclamation, and nothing
// reacted to memory pressure. The Manager is the single owner of that
// policy. It is deliberately small:
//
//   - components REGISTER trim functions (bounded caches stay bounded by
//     themselves; the manager only sheds the cold tail under coordination);
//   - runs are TRACKED so cleanup can distinguish "run finished, release
//     one-shot state" from "generation active, touch nothing";
//   - PRESSURE (heap above a high watermark) triggers GC + trims, but only
//     when no run is active — active generation, current workspace data and
//     hot caches are never sacrificed for bytes;
//   - an IDLE loop keeps steady-state memory honest between runs;
//   - every action is MEASURED: memory before/after, bytes freed, duration.
//
// It observes and coordinates; it never frees user data (sessions, memory
// entries, attachments) — those are user-visible content with explicit
// lifetime rules, not caches.
package memmanager

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultHighWatermark is the heap watermark (bytes) above which a
// pressure cleanup is triggered. Overridable per instance.
const DefaultHighWatermark uint64 = 1 << 30 // 1 GiB

// trimmer is one registered cache's shedding hook. fn performs the trim
// and returns an ESTIMATE of bytes freed (0 when nothing was shed).
type trimmer struct {
	name string
	fn   func() int64
}

// Stats is the manager's telemetry snapshot (surfaced via /api/perf and
// diagnostics): makes allocations/reclaims measurable instead of assumed.
type Stats struct {
	// Live process memory at snapshot time.
	HeapAllocBytes uint64 `json:"heapAllocBytes"`
	HeapInuseBytes uint64 `json:"heapInuseBytes"`
	SysBytes       uint64 `json:"sysBytes"`
	NumGC          uint32 `json:"numGC"`

	// Active agent runs — cleanup defers while > 0.
	ActiveRuns int64 `json:"activeRuns"`

	// Cleanup bookkeeping.
	CleanupsRun       uint64       `json:"cleanupsRun"`
	PressureCleanups  uint64       `json:"pressureCleanups"`
	LastCleanupAt     string       `json:"lastCleanupAt,omitempty"`
	LastCleanupReason string       `json:"lastCleanupReason,omitempty"`
	LastCleanupDurMs  int64        `json:"lastCleanupDurMs"`
	LastFreedBytes    int64        `json:"lastFreedBytes"`
	FreedBytesTotal   int64        `json:"freedBytesTotal"`
	HeapBeforeLast    uint64       `json:"heapBeforeLast"`
	HeapAfterLast     uint64       `json:"heapAfterLast"`
	PerTrimFreedLast  []TrimRecord `json:"perTrimFreedLast,omitempty"`

	// Registered components.
	RegisteredTrims []string `json:"registeredTrims"`
}

// TrimRecord records what one registered trim released in the last cleanup.
type TrimRecord struct {
	Name       string `json:"name"`
	FreedBytes int64  `json:"freedBytes"`
}

// Manager coordinates the runtime memory policy. Zero value is unusable;
// use New.
type Manager struct {
	mu            sync.Mutex
	trims         []trimmer
	highWatermark uint64
	now           func() time.Time // injectable for tests

	// own (v1.2.5) is the explicit ownership ladder accounting
	// (ACTIVE/SESSION/HOT/COLD/EXPIRED/RELEASE).
	own *ownership

	activeRuns atomic.Int64

	stats struct {
		cleanupsRun      uint64
		pressureCleanups uint64
		lastCleanupAt    time.Time
		lastReason       string
		lastDur          time.Duration
		lastFreed        int64
		freedTotal       int64
		heapBefore       uint64
		heapAfter        uint64
		perTrimLast      []TrimRecord
	}
}

// New returns a Manager with the default high watermark.
func New() *Manager {
	return &Manager{highWatermark: DefaultHighWatermark, now: time.Now, own: newOwnership()}
}

// WithHighWatermark overrides the pressure threshold (bytes).
func WithHighWatermark(b uint64) *Manager {
	m := New()
	if b > 0 {
		m.highWatermark = b
	}
	return m
}

// HoldActive marks a NAMED live request as an ACTIVE owner (v1.2.5):
// while held, coordinated cleanup refuses to run — the request's data is
// never evicted. Pair with ReleaseActive (defer).
func (m *Manager) HoldActive(name string) {
	m.own.HoldActive(name)
}

// ReleaseActive clears one named ACTIVE owner.
func (m *Manager) ReleaseActive(name string) {
	m.own.ReleaseActive(name)
}

// RegisterTrim registers a named shedding hook (idempotent by name).
// Callers own their normal bounds; the hook only sheds the COLD tail.
// Registration order is cleanup order.
func (m *Manager) RegisterTrim(name string, fn func() int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.trims {
		if t.name == name {
			m.trims[i].fn = fn // re-register replaces
			return
		}
	}
	m.trims = append(m.trims, trimmer{name: name, fn: fn})
}

// TrackRunStart marks one agent run (or equivalent live generation) active.
// Cleanup that could disturb a live run is deferred while any run is open.
// v1.2.5: the run is also a NAMED ACTIVE owner in the ownership ladder.
func (m *Manager) TrackRunStart() {
	m.activeRuns.Add(1)
	m.own.HoldActive("run")
}

// TrackRunEnd releases the run claim and runs a bounded post-run cleanup:
// one-shot large buffers are released while their absence is still safe
// (the run that produced them is over). Cancellation-aware by construction:
// an aborted run also reaches TrackRunEnd via its owner's defer.
func (m *Manager) TrackRunEnd() {
	m.activeRuns.Add(-1)
	m.own.ReleaseActive("run")
	m.Cleanup("run-finished")
}

// ActiveRuns reports how many runs are currently marked active.
func (m *Manager) ActiveRuns() int64 { return m.activeRuns.Load() }

// Cleanup runs all registered trims and records memory before/after,
// duration, and per-trim bytes freed. Safe to call at any time; callers
// that might overlap a live generation use MaybeCleanup instead.
func (m *Manager) Cleanup(reason string) Stats {
	heapBefore := heapInUse()
	start := m.now()

	m.mu.Lock()
	perTrim := make([]TrimRecord, 0, len(m.trims))
	var freed int64
	for _, t := range m.trims {
		if t.fn == nil {
			continue
		}
		f := t.fn()
		perTrim = append(perTrim, TrimRecord{Name: t.name, FreedBytes: f})
		freed += f
	}
	m.mu.Unlock()

	runtime.GC()

	after := heapInUse()
	dur := m.now().Sub(start)

	m.mu.Lock()
	s := m.stats
	s.cleanupsRun++
	s.lastCleanupAt = m.now().UTC()
	s.lastReason = reason
	s.lastDur = dur
	s.lastFreed = freed
	s.freedTotal += freed
	s.heapBefore = heapBefore
	s.heapAfter = after
	s.perTrimLast = perTrim
	m.stats = s
	out := m.snapshotLocked()
	m.mu.Unlock()

	// v1.2.5: the RELEASE level accounts what the cleanup actually freed.
	m.own.recordRelease(freed, m.now())

	return out
}

// MaybeCleanup triggers a cleanup only when it is SAFE (no active run) and
// merited (either the reason is mandatory — e.g. pressure — or enough time
// has passed since the last one). Returns the stats when it ran.
func (m *Manager) MaybeCleanup(reason string, minInterval time.Duration) (Stats, bool) {
	if m.activeRuns.Load() > 0 {
		return Stats{}, false
	}
	if m.pressure() {
		m.mu.Lock()
		m.stats.pressureCleanups++
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		last := m.stats.lastCleanupAt
		m.mu.Unlock()
		if !last.IsZero() && m.now().Sub(last) < minInterval {
			return Stats{}, false
		}
	}
	return m.Cleanup(reason), true
}

// Pressure reports whether the heap currently sits above the high
// watermark.
func (m *Manager) Pressure() bool { return m.pressure() }

func (m *Manager) pressure() bool {
	return heapInUse() > m.watermark()
}

func (m *Manager) watermark() uint64 {
	m.mu.Lock()
	w := m.highWatermark
	m.mu.Unlock()
	if w == 0 {
		w = DefaultHighWatermark
	}
	return w
}

// IdleLoop runs until stop is closed: an interval ticker that performs
// housekeeping ONLY while the app is idle (no active run). This is the
// "background cleanup only when idle" leg of the policy.
func (m *Manager) IdleLoop(stop <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.MaybeCleanup("idle", interval/2)
		}
	}
}

// Stats snapshots the telemetry.
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Stats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	out := Stats{
		HeapAllocBytes:   ms.HeapAlloc,
		HeapInuseBytes:   ms.HeapInuse,
		SysBytes:         ms.Sys,
		NumGC:            ms.NumGC,
		ActiveRuns:       m.activeRuns.Load(),
		CleanupsRun:      m.stats.cleanupsRun,
		PressureCleanups: m.stats.pressureCleanups,
		LastCleanupDurMs: m.stats.lastDur.Milliseconds(),
		LastFreedBytes:   m.stats.lastFreed,
		FreedBytesTotal:  m.stats.freedTotal,
		HeapBeforeLast:   m.stats.heapBefore,
		HeapAfterLast:    m.stats.heapAfter,
		RegisteredTrims:  make([]string, 0, len(m.trims)),
	}
	if !m.stats.lastCleanupAt.IsZero() {
		out.LastCleanupAt = m.stats.lastCleanupAt.UTC().Format(time.RFC3339)
		out.LastCleanupReason = m.stats.lastReason
	}
	if m.stats.perTrimLast != nil {
		out.PerTrimFreedLast = append([]TrimRecord(nil), m.stats.perTrimLast...)
	}
	for _, t := range m.trims {
		out.RegisteredTrims = append(out.RegisteredTrims, t.name)
	}
	return out
}

func heapInUse() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}
