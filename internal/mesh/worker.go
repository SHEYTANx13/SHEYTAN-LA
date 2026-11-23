package mesh

import (
	"context"
	"fmt"
	"sync"
)

// Handler executes ONE allowlisted AI-inference job type on this device.
// A handler MUST be deterministic-or-honest: it returns either a result
// it actually computed or an error — never fabricated telemetry, never
// a canned output for a real input (blueprint §8.5 execution
// containment; work order non-negotiable 11).
//
// The ctx carries cancellation: the coordinator cancels handlers when
// the job deadline or lease expires, or the owner cancels the job.
type Handler interface {
	Execute(ctx context.Context, env Envelope) ([]byte, error)
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, env Envelope) ([]byte, error)

// Execute implements Handler.
func (f HandlerFunc) Execute(ctx context.Context, env Envelope) ([]byte, error) {
	return f(ctx, env)
}

// Registry is the allowlisted typed-handler registry (blueprint §9.1
// worker.go). Registration is structurally limited to the fixed
// AI-inference allowlist: there is no runtime path that turns a UI
// handler, a shell, a filesystem tool, a browser or a credential store
// into a mesh handler (work order non-negotiable 7).
type Registry struct {
	mu       sync.Mutex
	handlers map[JobType]Handler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[JobType]Handler)}
}

// Register installs h for t. Registration FAILS for any type outside
// the AI-inference allowlist — including every UI/UX compute type —
// with ErrUnsupportedUICompute for those and ErrUnknownJobType for
// everything unlisted. A nil handler is refused.
func (r *Registry) Register(t JobType, h Handler) error {
	if h == nil {
		return fmt.Errorf("mesh: nil handler for %s", t)
	}
	if !AllowlistedJobType(t) {
		if UIComputeType(t) {
			return fmt.Errorf("%w: %q", ErrUnsupportedUICompute, t)
		}
		return fmt.Errorf("%w: %q", ErrUnknownJobType, t)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[t] = h
	return nil
}

// Lookup returns the allowlisted handler for t, or ErrHandlerNotFound.
func (r *Registry) Lookup(t JobType) (Handler, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.handlers[t]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrHandlerNotFound, t)
	}
	return h, nil
}

// FakeWorker is the deterministic in-process worker used by the
// phase-one end-to-end tests (blueprint §16: fake worker interface and
// deterministic end-to-end tests). It records the fencing epoch of every
// execution so tests can prove stale-result rejection, and it can be
// programmed to fail, sleep past its lease, or deliver a result late.
type FakeWorker struct {
	// ID is the fake peer identity the ledger records.
	ID string

	// Program, when set, replaces the default echo behaviour.
	Program func(ctx context.Context, env Envelope) ([]byte, error)

	mu        sync.Mutex
	execCount int
	lastEpoch int // -1 before the first execution
}

// NewFakeWorker returns a deterministic echo worker.
func NewFakeWorker(id string) *FakeWorker {
	return &FakeWorker{ID: id, lastEpoch: -1}
}

// Stats returns the execution count and the LAST execution's fencing
// epoch (-1 when the worker never executed).
func (w *FakeWorker) Stats() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.execCount, w.lastEpoch
}

// Execute runs the programmed behaviour; epochCtx keys the fencing
// epoch (set by the coordinator).
func (w *FakeWorker) Execute(ctx context.Context, env Envelope) ([]byte, error) {
	w.mu.Lock()
	w.execCount++
	w.lastEpoch = epochOf(ctx)
	w.mu.Unlock()

	if w.Program != nil {
		return w.Program(ctx, env)
	}

	// Deterministic default: an echo result the controller can verify
	// byte-for-byte (bounded by the result limit the coordinator
	// already enforced on the envelope).
	out := make([]byte, len(env.Payload))
	copy(out, env.Payload)
	return out, nil
}

// epochKey is the coordinator→worker fencing channel. The REAL remote
// transport (phase 2) will carry the epoch in the protocol envelope;
// the context key is the in-process equivalent.
type epochKey struct{}

// withEpoch attaches the fencing epoch to the execution context.
func withEpoch(ctx context.Context, epoch int) context.Context {
	return context.WithValue(ctx, epochKey{}, epoch)
}

// epochOf reads the fencing epoch back (0 when absent).
func epochOf(ctx context.Context) int {
	if v, ok := ctx.Value(epochKey{}).(int); ok {
		return v
	}
	return 0
}
