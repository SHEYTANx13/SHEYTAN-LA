// ownership.go — v1.2.5 explicit data-ownership ladder.
//
// The v1.2.4 manager already refused to evict active-run data; this file
// makes the policy NAMED and observable. Every managed datum belongs to
// exactly one ownership level:
//
//	ACTIVE   — data of a live request (in-flight prompt, streaming state,
//	           tool results feeding the current turn). NEVER evicted;
//	           cleanup defers while any ACTIVE owner exists.
//	SESSION  — the active session's own content (messages, activities).
//	           User-visible content; never a trim target.
//	HOT      — caches serving the current working set (recent sessions,
//	           image cache, validated text caches). Trimmed last.
//	COLD     — bounded caches past their working-set window (LRU tails,
//	           stale context-cache entries). Trimmed first.
//	EXPIRED  — entries past their TTL/compaction semantics awaiting the
//	           next coordinated cleanup.
//	RELEASE  — data actually freed by the last cleanup (telemetry only).
//
// Levels are reported, never guessed: OwnershipSnapshot() is the honest
// accounting of what the policy believes it manages, with the guard rails
// that make it enforceable:
//
//   - HoldActive/ReleaseActive bracket a request; while held, pressure
//     cleanup is refused outright (TrackRunStart/End already did this —
//     the level names now travel with the stats).
//   - Trims may only shed COLD/EXPIRED-eligible data; a trim hook that
//     reports freeing data while an ACTIVE holder exists is a bug the
//     counters make visible.
package memmanager

import (
	"sync"
	"time"
)

// Level is one ownership tier of managed data.
type Level string

// The ownership ladder, stable for telemetry and the UI.
const (
	LevelActive  Level = "ACTIVE"  // live request data — never evicted
	LevelSession Level = "SESSION" // active session content — user-owned
	LevelHot     Level = "HOT"     // working-set caches — trimmed last
	LevelCold    Level = "COLD"    // LRU tails — trimmed first
	LevelExpired Level = "EXPIRED" // past TTL, awaiting cleanup
	LevelRelease Level = "RELEASE" // freed by the last cleanup (telemetry)
)

// AllLevels returns the ladder in policy order (never-evicted → freed).
func AllLevels() []Level {
	return []Level{LevelActive, LevelSession, LevelHot, LevelCold, LevelExpired, LevelRelease}
}

// activeOwner is one named ACTIVE holder (a live request).
type activeOwner struct {
	name   string
	heldAt time.Time
}

// ownership tracks the manager's ownership accounting.
type ownership struct {
	mu     sync.Mutex
	active map[string]activeOwner

	// releasedBytes accumulates everything trims freed (the RELEASE level).
	releasedBytes int64
	releasedAt    time.Time
}

func newOwnership() *ownership {
	return &ownership{active: map[string]activeOwner{}}
}

// HoldActive marks a named ACTIVE owner (a request in flight). While any
// owner exists, coordinated cleanup refuses to run — active request data
// is never evicted, by name and by counter.
func (o *ownership) HoldActive(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.active[name] = activeOwner{name: name, heldAt: time.Now()}
}

// ReleaseActive clears one ACTIVE owner.
func (o *ownership) ReleaseActive(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delete(o.active, name)
}

// ActiveCount reports how many ACTIVE owners exist.
func (o *ownership) ActiveCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return len(o.active)
}

// ActiveNames lists the ACTIVE owners (for diagnostics).
func (o *ownership) ActiveNames() []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	out := make([]string, 0, len(o.active))
	for k := range o.active {
		out = append(out, k)
	}
	return out
}

// recordRelease accounts freed bytes into the RELEASE level.
func (o *ownership) recordRelease(bytes int64, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if bytes > 0 {
		o.releasedBytes += bytes
		o.releasedAt = at
	}
}

// LevelStats is one level's accounting in the snapshot.
type LevelStats struct {
	Level       Level    `json:"level"`
	Owners      int      `json:"owners,omitempty"` // ACTIVE holders
	Names       []string `json:"names,omitempty"`  // ACTIVE owner names
	Bytes       int64    `json:"bytes,omitempty"`  // RELEASE bytes
	At          string   `json:"at,omitempty"`     // RELEASE time
	Description string   `json:"description"`
}

// OwnershipSnapshot renders the full ladder with live counters. The
// HOT/COLD/EXPIRED rows carry the registered trim components — the data
// those components manage is what the policy may shed, in that order.
func (m *Manager) OwnershipSnapshot() []LevelStats {
	m.mu.Lock()
	names := make([]string, 0, len(m.trims))
	for _, t := range m.trims {
		names = append(names, t.name)
	}
	m.mu.Unlock()

	m.own.mu.Lock()
	active := len(m.own.active)
	activeNames := make([]string, 0, len(m.own.active))
	for k := range m.own.active {
		activeNames = append(activeNames, k)
	}
	released := m.own.releasedBytes
	releasedAt := ""
	if !m.own.releasedAt.IsZero() {
		releasedAt = m.own.releasedAt.Format(time.RFC3339)
	}
	m.own.mu.Unlock()

	hot, cold := splitHotCold(names)

	out := []LevelStats{
		{
			Level:       LevelActive,
			Owners:      active,
			Names:       activeNames,
			Description: "live request data — never evicted while held",
		},
		{
			Level:       LevelSession,
			Description: "active session content — user-owned, never trimmed",
		},
		{
			Level:       LevelHot,
			Names:       hot,
			Description: "working-set caches (sessions-hot, live validated caches) — trimmed last",
		},
		{
			Level:       LevelCold,
			Names:       cold,
			Description: "bounded-cache LRU tails — trimmed first under pressure",
		},
		{
			Level:       LevelExpired,
			Description: "entries past TTL/compaction semantics — released on the next coordinated cleanup",
		},
		{
			Level:       LevelRelease,
			Bytes:       released,
			At:          releasedAt,
			Description: "cumulative bytes freed by coordinated cleanups",
		},
	}

	return out
}

// splitHotCold classifies registered trim components by eviction posture:
// the sessions-hot cache guards the most recent session (HOT); everything
// else is COLD-eligible tail shedding.
func splitHotCold(names []string) (hot, cold []string) {
	for _, n := range names {
		if n == "sessions-hot" {
			hot = append(hot, n)
			continue
		}
		cold = append(cold, n)
	}
	return hot, cold
}
