package engine

// scheduler.go — native engine request scheduler concern (v1.1.5 Phase 1:
// TYPES ONLY).
//
// The future native engine will schedule concurrent generation requests.
// The policy vocabulary below is deliberately bounded: SHEYTAN's
// bounded-resource invariants apply to the native engine too — there is
// no unlimited concurrency, ever. MaxConcurrentRequests is the hard cap
// the future scheduler enforces (Phase 1 reports the honest current value
// of 1 — the engine executes nothing in parallel today).

// SchedulerPolicy is the (future) scheduler configuration.
type SchedulerPolicy struct {
	// MaxConcurrentRequests is the hard cap on in-flight generations.
	// Bounded by design; must never become unbounded.
	MaxConcurrentRequests int `json:"maxConcurrentRequests"`

	// QueueDepthLimit bounds waiting requests; overflow fails closed with
	// a visible error instead of growing without limit.
	QueueDepthLimit int `json:"queueDepthLimit,omitempty"`
}

// DefaultSchedulerPolicy is the Phase 1 policy: one request at a time,
// small bounded queue.
func DefaultSchedulerPolicy() SchedulerPolicy {
	return SchedulerPolicy{
		MaxConcurrentRequests: 1,
		QueueDepthLimit:       8,
	}
}

// SchedulerStats is the scheduler snapshot reported through metrics.
// ActiveRequests and QueuedRequests are measured (zero in Phase 1);
// MaxConcurrentRequests is the enforced policy constant.
type SchedulerStats struct {
	ActiveRequests    int `json:"activeRequests,omitempty"`
	QueuedRequests    int `json:"queuedRequests,omitempty"`
	MaxConcurrentReqs int `json:"maxConcurrentRequests,omitempty"`
}
