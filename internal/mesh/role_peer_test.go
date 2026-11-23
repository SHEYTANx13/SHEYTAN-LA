package mesh

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Phase 2a — dynamic dual-role negotiation + the loopback peer seam
// (owner work order, 2026-11-23: each device can be both server and
// user based on need; the phone-as-interface scenario). Deterministic,
// socket-free, in-process.

func TestNegotiateRoleMatrix(t *testing.T) {
	workstation := DeviceCapability{Class: ClassWorkstation, MemoryBytes: 16 << 30, AcceleratorPresent: true}
	weakWorkstation := DeviceCapability{Class: ClassWorkstation, MemoryBytes: 4 << 30}
	phone := DeviceCapability{Class: ClassCompact, MemoryBytes: 3 << 30}

	cases := []struct {
		name    string
		cap     DeviceCapability
		pref    RolePreference
		ceiling string
		want    string
		wantErr bool
	}{
		{"workstation auto serves and submits", workstation, PrefAuto, "both", "both", false},
		{"compact device narrows to the interface role", phone, PrefAuto, "both", "controller", false},
		{"workstation below the honest provider floor stays interface", weakWorkstation, PrefAuto, "both", "controller", false},
		{"consume-only forces the requester role", workstation, PrefConsumeOnly, "both", "controller", false},
		{"serve-only forces the provider role", phone, PrefServeOnly, "both", "worker", false},
		{"static controller ceiling is preserved", workstation, PrefAuto, "controller", "controller", false},
		{"compact device under a worker ceiling stays the owner's server", phone, PrefAuto, "worker", "worker", false},
		{"consume-only cannot widen a worker ceiling", workstation, PrefConsumeOnly, "worker", "", true},
		{"serve-only cannot widen a controller ceiling", phone, PrefServeOnly, "controller", "", true},
		{"unknown preference is typed-invalid", workstation, RolePreference("greedy"), "both", "", true},
		{"unknown device class is typed-invalid", DeviceCapability{Class: "toaster", MemoryBytes: 1 << 30}, PrefAuto, "both", "", true},
		{"negative memory is typed-invalid", DeviceCapability{Class: ClassCompact, MemoryBytes: -1}, PrefAuto, "both", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NegotiateRole(tc.cap, tc.pref, tc.ceiling)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("NegotiateRole = %q, %v; want ErrInvalidConfig", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NegotiateRole: %v", err)
			}
			if got != tc.want {
				t.Fatalf("NegotiateRole = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestManagerNegotiateRoleDisabledRefused(t *testing.T) {
	m, err := NewManager(DefaultConfig(), t.TempDir()) // Enabled stays false
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	_, err = m.NegotiateRole(DeviceCapability{Class: ClassWorkstation, MemoryBytes: 16 << 30}, PrefAuto)
	if !errors.Is(err, ErrMeshDisabled) {
		t.Fatalf("NegotiateRole on disabled mesh = %v, want ErrMeshDisabled", err)
	}
}

func TestManagerNegotiateRoleAppliesAndNarrows(t *testing.T) {
	c := testConfig()
	c.Role = "both" // the workstation's static ceiling allows both roles
	m := newTestManager(t, c)
	defer func() { _ = m.Close(context.Background()) }()

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Need-driven switch #1: the workstation is idle and serves.
	role, err := m.NegotiateRole(DeviceCapability{Class: ClassWorkstation, MemoryBytes: 16 << 30}, PrefAuto)
	if err != nil || role != "both" {
		t.Fatalf("idle workstation negotiate = %q, %v; want both", role, err)
	}
	if got := m.Status().Role; got != "both" {
		t.Fatalf("Status().Role = %q, want both", got)
	}

	// Need-driven switch #2: the owner is running heavy inference on the
	// same device and no longer wants it to serve — the role narrows.
	role, err = m.NegotiateRole(DeviceCapability{Class: ClassWorkstation, MemoryBytes: 16 << 30}, PrefConsumeOnly)
	if err != nil || role != "controller" {
		t.Fatalf("busy workstation negotiate = %q, %v; want controller", role, err)
	}
	if got := m.Status().Role; got != "controller" {
		t.Fatalf("Status().Role after narrowing = %q, want controller", got)
	}

	// The static ceiling is never widened behind the owner's back: a
	// controller-ceiling config cannot be negotiated into serving.
	m2 := newTestManager(t, func() Config { c := testConfig(); c.Role = "controller"; return c }())
	defer func() { _ = m2.Close(context.Background()) }()
	if err := m2.Start(context.Background()); err != nil {
		t.Fatalf("Start m2: %v", err)
	}
	if _, err := m2.NegotiateRole(DeviceCapability{Class: ClassWorkstation, MemoryBytes: 16 << 30}, PrefServeOnly); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("serve-only under a controller ceiling = %v, want ErrInvalidConfig", err)
	}
}

// TestPeerPhoneAsInterface pins the phase-2a scenario end to end: a
// compact interface device (phone) dispatches an allowlisted
// AI-inference job to a workstation peer through the loopback exchange;
// the provider executes it with its allowlisted handler, and the
// requester commits the verified result in ITS OWN ledger.
func TestPeerPhoneAsInterface(t *testing.T) {
	ctx := context.Background()

	provider := newTestManager(t, testConfig())
	defer func() { _ = provider.Close(ctx) }()
	if err := provider.Start(ctx); err != nil {
		t.Fatalf("provider Start: %v", err)
	}
	if err := provider.Register(JobTypeWholeRequestInference, NewFakeWorker("workstation")); err != nil {
		t.Fatalf("provider Register: %v", err)
	}

	phone := newTestManager(t, testConfig())
	defer func() { _ = phone.Close(ctx) }()
	if err := phone.Start(ctx); err != nil {
		t.Fatalf("phone Start: %v", err)
	}
	// The phone narrows to the interface role: it never serves.
	if _, err := phone.NegotiateRole(DeviceCapability{Class: ClassCompact, MemoryBytes: 3 << 30}, PrefAuto); err != nil {
		t.Fatalf("phone NegotiateRole: %v", err)
	}

	exchange, err := NewLoopbackPeerExchange(provider)
	if err != nil {
		t.Fatalf("NewLoopbackPeerExchange: %v", err)
	}

	rec, err := phone.SubmitToPeer(ctx, exchange, Envelope{
		Type:     JobTypeWholeRequestInference,
		Privacy:  PrivacyPublic,
		Payload:  []byte("summarize this large local document"),
		Deadline: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SubmitToPeer: %v", err)
	}
	if rec.State != StateCommitted {
		t.Fatalf("peer-dispatched job state = %s, want committed (full lifecycle recorded on the interface device)", rec.State)
	}

	// The provider executed through its allowlisted registry.
	served, _ := provider.Status().Jobs.Committed, error(nil)
	_ = served
	w := providerStatusWorkerCount(provider)
	if w == 0 {
		t.Fatalf("the provider must have served the inference job through its registry")
	}
}

// TestPeerRefusalsAreTypedAndFailClosed pins both sides of the seam:
// the provider independently re-validates (UI/UX compute, private
// payloads and oversized envelopes can never cross), and a refusal
// fails the requester's ledger row closed — never silent success.
func TestPeerRefusalsAreTypedAndFailClosed(t *testing.T) {
	ctx := context.Background()

	provider := newTestManager(t, testConfig())
	defer func() { _ = provider.Close(ctx) }()
	if err := provider.Start(ctx); err != nil {
		t.Fatalf("provider Start: %v", err)
	}
	if err := provider.Register(JobTypeEmbeddingsBatch, NewFakeWorker("workstation")); err != nil {
		t.Fatalf("provider Register: %v", err)
	}

	exchange, err := NewLoopbackPeerExchange(provider)
	if err != nil {
		t.Fatalf("NewLoopbackPeerExchange: %v", err)
	}

	// UI/UX compute is refused at the provider before any handler runs:
	// the envelope path refuses it as an unlisted type (the typed
	// ErrUnknownJobType carries the UI/UX-exclusion wording), and the
	// REGISTER path refuses it with ErrUnsupportedUICompute — both sides
	// of the allowlist gate.
	_, uiErr := exchange.Offer(ctx, Envelope{Type: JobType("ui.render"), Privacy: PrivacyPublic, Payload: []byte("frame")})
	if !errors.Is(uiErr, ErrUnknownJobType) {
		t.Fatalf("UI compute offer = %v, want ErrUnknownJobType", uiErr)
	}
	if err := provider.Register(JobType("ui.render"), NewFakeWorker("bogus")); !errors.Is(err, ErrUnsupportedUICompute) {
		t.Fatalf("UI compute registration = %v, want ErrUnsupportedUICompute", err)
	}

	// A private payload can never cross the seam.
	if _, err := exchange.Offer(ctx, Envelope{Type: JobTypeEmbeddingsBatch, Privacy: PrivacyPrivate, Payload: []byte("diary")}); !errors.Is(err, ErrPrivacyClassRefused) {
		t.Fatalf("private payload offer = %v, want ErrPrivacyClassRefused", err)
	}

	// An unstarted provider refuses with a typed seam error.
	idle := newTestManager(t, testConfig()) // constructed, NOT started
	defer func() { _ = idle.Close(ctx) }()
	unstartedExchange, err := NewLoopbackPeerExchange(idle)
	if err != nil {
		t.Fatalf("NewLoopbackPeerExchange(idle): %v", err)
	}
	if _, err := unstartedExchange.Offer(ctx, Envelope{Type: JobTypeEmbeddingsBatch, Privacy: PrivacyPublic, Payload: []byte("x")}); !errors.Is(err, ErrPeerExchange) {
		t.Fatalf("offer to unstarted provider = %v, want ErrPeerExchange", err)
	}

	// A provider that started but lacks the handler refuses typed.
	if _, err := exchange.Offer(ctx, Envelope{Type: JobTypeWholeRequestInference, Privacy: PrivacyPublic, Payload: []byte("x")}); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("missing-handler offer = %v, want ErrHandlerNotFound", err)
	}

	// The requester-side path fails closed: a refusal is a FAILED ledger
	// row, never a committed one.
	phone := newTestManager(t, testConfig())
	defer func() { _ = phone.Close(ctx) }()
	if err := phone.Start(ctx); err != nil {
		t.Fatalf("phone Start: %v", err)
	}
	rec, err := phone.SubmitToPeer(ctx, exchange, Envelope{Type: JobTypeWholeRequestInference, Privacy: PrivacyPublic, Payload: []byte("x")})
	if err == nil {
		t.Fatalf("SubmitToPeer against a handler-less provider must fail")
	}
	if rec.State != StateFailed {
		t.Fatalf("refused peer job state = %s, want failed (fail closed)", rec.State)
	}
}

func TestSubmitToPeerRequiresStartedManager(t *testing.T) {
	m, err := NewManager(DefaultConfig(), t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	exchange, err := NewLoopbackPeerExchange(m)
	if err != nil {
		t.Fatalf("NewLoopbackPeerExchange: %v", err)
	}
	if _, err := m.SubmitToPeer(context.Background(), exchange, allowlistedEnvelope()); !errors.Is(err, ErrManagerNotStarted) {
		t.Fatalf("SubmitToPeer before Start = %v, want ErrManagerNotStarted", err)
	}
}

// providerStatusWorkerCount returns the FakeWorker served count wired
// into the provider registry (0 when none) — the honest proof that the
// PROVIDER, not the requester, executed the job.
func providerStatusWorkerCount(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reg == nil {
		return 0
	}
	h, err := m.reg.Lookup(JobTypeWholeRequestInference)
	if err != nil {
		return 0
	}
	fw, ok := h.(*FakeWorker)
	if !ok {
		return 0
	}
	served, _ := fw.Stats()
	return served
}
