package mesh

import (
	"context"
	"fmt"
	"time"
)

// Phase 2a — the peer-exchange SEAM (owner work order, 2026-11-23: the
// phone-as-interface scenario). A real LAN/Internet transport is the
// NEXT gated phase (blueprint phase 3: libp2p host, mDNS discovery,
// rendezvous/relay — no networking dependency is added in this pass).
// What ships here is the typed seam that transport must implement PLUS
// the in-process loopback exchange, so the two-role scenario (a compact
// interface device dispatching allowlisted AI-inference work to a
// workstation peer) is executable and regression-tested TODAY on one
// machine — over memory, with zero sockets, zero goroutines and the
// mesh still disabled by default.
//
// The seam is deliberately narrow: ONE method. A future libp2p host
// implements Offer by carrying the envelope to the peer over the wire;
// the controller/ledger/registry contracts around it are already
// stable (job.go/envelope.go/ledger.go/coordinator.go).

// PeerExchange carries one validated AI-inference envelope from a
// requesting manager to a providing manager and returns the executed
// result bytes. Implementations must be synchronous and bounded by the
// caller's context; the envelope has ALREADY passed the requester's
// validation (allowlist, privacy class, size bounds, deadline) when it
// reaches the seam, and the provider re-validates on arrival — both
// sides refuse independently, so a peer can never smuggle a UI/UX
// compute type, a private payload or an oversized envelope through the
// seam.
type PeerExchange interface {
	Offer(ctx context.Context, env Envelope) ([]byte, error)
}

// ErrPeerExchange describes seam-level failures (unstarted peer,
// refused envelope, handler missing on the provider).
var ErrPeerExchange = fmt.Errorf("mesh: peer exchange refused")

// LoopbackPeerExchange connects ONE requesting manager to ONE providing
// manager inside the same process (the deterministic phase-2a test
// surface; the phase-3 transport replaces the mechanism, never the
// contract). Offer re-validates the envelope against the PROVIDER's
// configuration and executes it against the provider's allowlisted
// handler registry — the exact gates the provider's own coordinator
// applies locally (job.go allowlist + envelope.go bounds + privacy
// gate), then returns the raw result to the requester, which verifies
// and commits it in ITS ledger.
//
// Honest boundary: this loopback seam does not create provider-side
// ledger records (the durable lease/fencing flow for remote peers is
// the phase-3 transport's contract against the SAME ledger API). It is
// the integration seam, not a production transport, and never opens a
// listener.
type LoopbackPeerExchange struct {
	provider *Manager
}

// NewLoopbackPeerExchange wires an exchange to a provider manager. The
// provider must be constructed and started (its allowlist handlers
// registered); the exchange holds no state of its own.
func NewLoopbackPeerExchange(provider *Manager) (*LoopbackPeerExchange, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: nil provider manager", ErrPeerExchange)
	}
	return &LoopbackPeerExchange{provider: provider}, nil
}

// Offer re-validates the envelope against the PROVIDER configuration
// and executes it against the provider's allowlisted registry. Every
// refusal is typed: ErrManagerNotStarted before the provider started,
// the envelope's own typed validation errors otherwise, and
// ErrHandlerNotFound when the provider does not carry the allowlisted
// handler.
func (x *LoopbackPeerExchange) Offer(ctx context.Context, env Envelope) ([]byte, error) {
	x.provider.mu.Lock()
	started, cfg, reg := x.provider.started, x.provider.cfg, x.provider.reg
	x.provider.mu.Unlock()

	if !started || reg == nil {
		return nil, fmt.Errorf("%w: provider manager is not started", ErrPeerExchange)
	}

	if err := env.validate(cfg); err != nil {
		return nil, err
	}

	h, err := reg.Lookup(env.Type)
	if err != nil {
		return nil, err
	}

	if env.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, env.Deadline)
		defer cancel()
	}

	// The provider executes in the caller's goroutine: the loopback
	// exchange spawns nothing, keeps no goroutine and owns no resource.
	return h.Execute(ctx, env)
}

// SubmitToPeer is the compact-device interface path: the requester
// dispatches one job to a peer through the seam and commits the
// verified result in ITS OWN ledger (the phone-as-interface scenario:
// the compact device's ledger records the whole attempt lifecycle —
// pending → running → committed — while the compute ran on the peer).
// The orchestration lives in Coordinator.DispatchToPeer so durability,
// fencing and verification follow the EXACT same ledger contract as a
// local attempt. The requester must be started.
func (m *Manager) SubmitToPeer(ctx context.Context, peer PeerExchange, env Envelope) (JobRecord, error) {
	m.mu.Lock()
	started, coord := m.started, m.coord
	m.mu.Unlock()
	if !started || coord == nil {
		return JobRecord{}, ErrManagerNotStarted
	}

	return coord.DispatchToPeer(ctx, peer, env)
}

// guardDeadline bounds a peer wait by the manager's deadline ceiling;
// shared by the phase-3 transport's bounded offers.
func guardDeadline(d time.Duration) time.Duration {
	if d <= 0 || d > DefaultMaxDeadline {
		return DefaultMaxDeadline
	}
	return d
}
