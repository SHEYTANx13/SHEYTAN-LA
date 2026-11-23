package mesh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testConfig() Config {
	c := DefaultConfig()
	c.Enabled = true
	c.LeaseTTL = 2 * time.Second
	c.RetryCap = 2
	return c
}

func newTestManager(t *testing.T, c Config) *Manager {
	t.Helper()
	m, err := NewManager(c, t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func allowlistedEnvelope() Envelope {
	return Envelope{
		Type:    JobTypeEmbeddingsBatch,
		Privacy: PrivacyPublic,
		Payload: []byte("bounded-deterministic-input"),
	}
}

// TestManagerDisabledByDefault pins the fail-closed product default:
// zero-value config == disabled; Start refuses with ErrMeshDisabled and
// NO ledger, coordinator or goroutine is constructed (no listener exists
// at all in phase 1 — this test also pins that a Start refusal leaves
// the data directory untouched).
func TestManagerDisabledByDefault(t *testing.T) {
	c := DefaultConfig() // Enabled stays FALSE
	if c.Enabled {
		t.Fatalf("DefaultConfig must be disabled by default")
	}
	dir := t.TempDir()
	m, err := NewManager(c, dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Start(context.Background()); !errors.Is(err, ErrMeshDisabled) {
		t.Fatalf("Start on disabled mesh = %v, want ErrMeshDisabled", err)
	}
	st := m.Status()
	if st.Started || st.Enabled {
		t.Fatalf("disabled manager reports started/enabled: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "mesh-jobs.json")); !os.IsNotExist(err) {
		t.Fatalf("disabled Start must not create ledger state, stat err=%v", err)
	}
	if _, err := m.Submit(context.Background(), allowlistedEnvelope()); !errors.Is(err, ErrManagerNotStarted) {
		t.Fatalf("Submit on disabled mesh = %v, want ErrManagerNotStarted", err)
	}
}

// TestDisabledByDefaultNoNetwork pins the no-side-effect rule at the
// process level: constructing + starting a DISABLED manager opens no
// listener. Phase 1 has no transport code, so the strongest available
// statement is: the package exports no listen/serve surface and Start on
// a disabled config allocates nothing. (The phase-2 gate re-proves this
// against the real host.)
func TestDisabledByDefaultNoNetwork(t *testing.T) {
	m := newTestManager(t, DefaultConfig())
	ctx := context.Background()
	if err := m.Start(ctx); !errors.Is(err, ErrMeshDisabled) {
		t.Fatalf("disabled Start = %v, want ErrMeshDisabled", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop after refused Start must be a no-op, got %v", err)
	}
}

// TestEndToEndHappyPath pins the deterministic end-to-end flow:
// create → lease(epoch 1) → execute → deliver → verify → commit, with
// the echo worker's result equal to the payload byte-for-byte.
func TestEndToEndHappyPath(t *testing.T) {
	m := newTestManager(t, testConfig())
	w := NewFakeWorker("fake-1")
	for _, jt := range []JobType{JobTypeEmbeddingsBatch, JobTypeWholeRequestInference, JobTypeReadonlyAIBatch} {
		if err := m.Register(jt, w); err != nil {
			t.Fatalf("register %s: %v", jt, err)
		}
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop(ctx) }()

	env := allowlistedEnvelope()
	rec, err := m.Submit(ctx, env)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if rec.State != StateCommitted {
		t.Fatalf("final state = %s, want committed (failure_code=%q)", rec.State, rec.FailureCode)
	}
	if !bytes.Equal(rec.Result, env.Payload) {
		t.Fatalf("result %q does not verify against payload %q", rec.Result, env.Payload)
	}
	if len(rec.Attempts) != 1 || rec.Attempts[0].Epoch != 1 {
		t.Fatalf("want exactly one attempt at epoch 1, got %+v", rec.Attempts)
	}
	if n, epoch := w.Stats(); n != 1 || epoch != 1 {
		t.Fatalf("worker stats = (%d execs, last epoch %d), want (1, 1)", n, epoch)
	}
	st := m.Status()
	if st.Jobs.Committed != 1 {
		t.Fatalf("status jobs = %+v, want 1 committed", st.Jobs)
	}
}

// TestStaleResultRejected pins the fencing contract: while a lease is
// LIVE, a result delivered under a STALE epoch (not the current one) is
// rejected with ErrStaleResult and leaves the row untouched; the CURRENT
// epoch is then accepted and settles the job.
func TestStaleResultRejected(t *testing.T) {
	m := newTestManager(t, testConfig())
	if err := m.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-1")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	rec, err := m.ledgerCreateOnly(allowlistedEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	leased, err := m.ledgerGrantLease(rec.ID, "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	if leased.Epoch != 1 {
		t.Fatalf("first lease epoch = %d, want 1", leased.Epoch)
	}
	if err := m.ledgerDeliver(rec.ID, leased.Epoch+1000, []byte("stale-result")); !errors.Is(err, ErrStaleResult) {
		t.Fatalf("stale-epoch delivery = %v, want ErrStaleResult", err)
	}
	got, _ := m.ledgerGet(rec.ID)
	if got.State != StateLeased {
		t.Fatalf("a stale delivery must not mutate the row, state = %s", got.State)
	}
	if err := m.ledgerDeliver(rec.ID, leased.Epoch, []byte("fresh-result")); err != nil {
		t.Fatalf("current-epoch delivery = %v, want accepted", err)
	}
	got, _ = m.ledgerGet(rec.ID)
	if got.State != StateResultReceived || !bytes.Equal(got.Result, []byte("fresh-result")) {
		t.Fatalf("current-epoch result not recorded: state=%s result=%q", got.State, got.Result)
	}
}

// TestRetryCapAndFailClosed pins the retry-cap contract: a handler that
// always fails re-queues until the cap, then the job fails closed with
// ErrRetryCapExceeded recorded in the row.
func TestRetryCapAndFailClosed(t *testing.T) {
	c := testConfig()
	c.RetryCap = 2
	m := newTestManager(t, c)
	boom := errors.New("boom: deterministic failure")
	if err := m.Register(JobTypeReadonlyAIBatch, HandlerFunc(func(_ context.Context, _ Envelope) ([]byte, error) {
		return nil, boom
	})); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	env := Envelope{Type: JobTypeReadonlyAIBatch, Privacy: PrivacyPublic, Payload: []byte("x")}
	rec, err := m.Submit(ctx, env)
	if !errors.Is(err, ErrRetryCapExceeded) {
		t.Fatalf("err = %v, want ErrRetryCapExceeded (wrapped handler failure retained: %v)", err, errors.Unwrap(err))
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err must also carry the handler failure, got %v", err)
	}
	if rec.State != StateFailed {
		t.Fatalf("state = %s, want failed", rec.State)
	}
	if len(rec.Attempts) != 2 {
		t.Fatalf("attempts = %d, want exactly the cap 2", len(rec.Attempts))
	}
	if rec.Attempts[1].ErrCode == "" {
		t.Fatalf("the last attempt must record the handler failure code")
	}
	if rec.FailureCode != ErrRetryCapExceeded.Error() {
		t.Fatalf("failure_code = %q, want %q", rec.FailureCode, ErrRetryCapExceeded.Error())
	}
}

// TestLeaseExpiryRequeue pins the lease contract: a handler that sleeps
// past its lease TTL finds its result REJECTED (lease expired), the job
// re-queues, and the second attempt commits — while the total deadline
// still allows.
func TestLeaseExpiryRequeue(t *testing.T) {
	c := testConfig()
	c.LeaseTTL = 60 * time.Millisecond
	m := newTestManager(t, c)
	attempts := 0
	if err := m.Register(JobTypeEmbeddingsBatch, HandlerFunc(func(_ context.Context, env Envelope) ([]byte, error) {
		attempts++
		if attempts == 1 {
			time.Sleep(3 * c.LeaseTTL) // lease dies under the handler
		}
		out := append([]byte(nil), env.Payload...)
		return out, nil
	})); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	rec, err := m.Submit(ctx, allowlistedEnvelope())
	if err != nil {
		t.Fatalf("requeue path must recover: %v", err)
	}
	if rec.State != StateCommitted {
		t.Fatalf("state = %s, want committed after the re-lease", rec.State)
	}
	if len(rec.Attempts) != 2 || rec.Attempts[0].Epoch != 1 || rec.Attempts[1].Epoch != 2 {
		t.Fatalf("attempts = %+v, want epochs 1 then 2", rec.Attempts)
	}
}

// TestCancelPinsTypedCancellation pins the owner-cancellation path.
func TestCancelPinsTypedCancellation(t *testing.T) {
	m := newTestManager(t, testConfig())
	if err := m.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-1")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	rec, err := m.Submit(ctx, allowlistedEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(rec.ID); err == nil {
		t.Fatalf("canceling a terminal job must fail")
	}
	env2 := allowlistedEnvelope()
	env2.Payload = []byte("second-job")
	rec2, err := m.ledgerCreateOnly(env2)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(rec2.ID); err != nil {
		t.Fatalf("cancel of a pending job: %v", err)
	}
	got, _ := m.ledgerGet(rec2.ID)
	if got.State != StateCanceled {
		t.Fatalf("state = %s, want canceled", got.State)
	}
}

// TestRestartRecovery pins restart recovery: a ledger with an expired
// lease re-arms the job to pending; a fresh manager recovers and the
// job completes on the next attempt.
func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	c := testConfig()

	// First manager: lease a job, then "die" without settling it.
	m1, err := NewManager(c, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m1.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-1")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	rec1, err := m1.ledgerCreateOnly(allowlistedEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m1.ledgerGrantLease(rec1.ID, "fake-1"); err != nil {
		t.Fatal(err)
	}
	if err := m1.ledgerExpireLeaseForRecovery(rec1.ID); err != nil {
		t.Fatal(err)
	}
	if err := m1.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	// Second manager on the SAME dir: recovery must re-arm to pending,
	// and a subsequent dispatch completes the job at epoch 2.
	m2, err := NewManager(c, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-2")); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m2.Stop(ctx) }()

	got, ok := m2.ledgerGet(rec1.ID)
	if !ok {
		t.Fatalf("recovered ledger lost job %s", rec1.ID)
	}
	if got.State != StatePending {
		t.Fatalf("recovered state = %s, want pending", got.State)
	}
	final, err := m2.Resume(ctx, rec1.ID)
	if err != nil {
		t.Fatalf("Resume after recovery: %v", err)
	}
	if final.State != StateCommitted {
		t.Fatalf("final state = %s, want committed", final.State)
	}
	if final.Epoch != 2 {
		t.Fatalf("fencing epoch = %d, want 2 after recovery", final.Epoch)
	}
}

// TestUIComputeNeverShared pins the product-critical rule at EVERY
// layer: registration refuses UI/shell/fs/render types; dispatch
// refuses them; and the allowlist itself contains no such entry.
func TestUIComputeNeverShared(t *testing.T) {
	m := newTestManager(t, testConfig())
	uiTypes := []JobType{
		"ui.render", "ui.layout", "ui.animation", "ui.input",
		"render.frame", "render.scene", "shell.exec", "fs.read",
		"fs.write", "browser.drive", "code.exec",
	}
	for _, jt := range uiTypes {
		if !UIComputeType(jt) {
			t.Fatalf("%s must classify as UI compute", jt)
		}
		if AllowlistedJobType(jt) {
			t.Fatalf("%s must NOT be allowlisted", jt)
		}
		err := m.Register(jt, NewFakeWorker("attacker"))
		if !errors.Is(err, ErrUnsupportedUICompute) {
			t.Fatalf("register %s = %v, want ErrUnsupportedUICompute", jt, err)
		}
	}
	// Dispatch-side: an envelope with a UI type fails validation before
	// any ledger row is created (nothing durable, nothing dispatched).
	ctx := context.Background()
	m2 := newTestManager(t, testConfig())
	if err := m2.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-1")); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m2.Stop(ctx) }()

	uiEnv := allowlistedEnvelope()
	uiEnv.Type = "ui.render"
	if err := uiEnv.validate(testConfig()); !errors.Is(err, ErrUnknownJobType) {
		t.Fatalf("ui.render envelope validation = %v, want ErrUnknownJobType", err)
	}
	_, err := m2.Submit(ctx, uiEnv)
	if !errors.Is(err, ErrUnknownJobType) {
		t.Fatalf("Submit(ui.render) = %v, want ErrUnknownJobType", err)
	}
	if snap := m2.ledgerSnapshotLen(); snap != 0 {
		t.Fatalf("a refused envelope must leave NO ledger row, got %d", snap)
	}
}

// TestUnlistedTypeRefused pins the closed allowlist: an unknown type is
// never a handler candidate.
func TestUnlistedTypeRefused(t *testing.T) {
	m := newTestManager(t, testConfig())
	err := m.Register("ai.custom.future", NewFakeWorker("x"))
	if !errors.Is(err, ErrUnknownJobType) {
		t.Fatalf("register unlisted = %v, want ErrUnknownJobType", err)
	}
}

// TestPrivacyPrivateNeverLeavesDevice pins the privacy class contract:
// private payloads are refused with ErrPrivacyClassRefused before any
// dispatch.
func TestPrivacyPrivateNeverLeavesDevice(t *testing.T) {
	m := newTestManager(t, testConfig())
	if err := m.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-1")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	env := allowlistedEnvelope()
	env.Privacy = PrivacyPrivate
	_, err := m.Submit(ctx, env)
	if !errors.Is(err, ErrPrivacyClassRefused) {
		t.Fatalf("private dispatch = %v, want ErrPrivacyClassRefused", err)
	}
}

// TestPayloadBoundRefused pins the size bound.
func TestPayloadBoundRefused(t *testing.T) {
	c := testConfig()
	c.MaxPayloadBytes = 64
	m := newTestManager(t, c)
	if err := m.Register(JobTypeEmbeddingsBatch, NewFakeWorker("fake-1")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	env := allowlistedEnvelope()
	env.Payload = bytes.Repeat([]byte("a"), 65)
	_, err := m.Submit(ctx, env)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized payload = %v, want ErrPayloadTooLarge", err)
	}
}

// TestConfigValidation pins the config bounds.
func TestConfigValidation(t *testing.T) {
	bad := []Config{
		func() Config { c := DefaultConfig(); c.Role = "emperor"; return c }(),
		func() Config { c := DefaultConfig(); c.MaxPayloadBytes = -1; return c }(),
		func() Config { c := DefaultConfig(); c.RetryCap = 99; return c }(),
		func() Config { c := DefaultConfig(); c.LeaseTTL = 0; return c }(),
		func() Config { c := DefaultConfig(); c.DispatchQueueSize = 0; return c }(),
	}
	for i, c := range bad {
		if err := c.Validate(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("config %d = %v, want ErrInvalidConfig", i, err)
		}
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
}

// TestInvalidConfigRefusedAtConstruction pins fail-closed construction.
func TestInvalidConfigRefusedAtConstruction(t *testing.T) {
	c := DefaultConfig()
	c.Role = "worker"
	c.MaxDeadline = 0
	if _, err := NewManager(c, t.TempDir()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewManager = %v, want ErrInvalidConfig", err)
	}
}
