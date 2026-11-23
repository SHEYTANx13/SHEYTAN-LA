package mesh

import (
	"errors"
	"fmt"
	"time"
)

// Typed failure codes. Every mesh failure is one of these values —
// callers never parse error strings (blueprint §6: typed failure codes).
var (
	ErrMeshDisabled         = errors.New("mesh: mesh is disabled by default and has not been enabled by the owner")
	ErrInvalidEnvelope      = errors.New("mesh: job envelope failed validation")
	ErrUnknownJobType       = errors.New("mesh: job type outside the AI-inference allowlist")
	ErrPrivacyClassRefused  = errors.New("mesh: privacy class refuses off-device execution")
	ErrPayloadTooLarge      = errors.New("mesh: payload exceeds the configured bound")
	ErrHandlerNotFound      = errors.New("mesh: no allowlisted handler for the job type on this worker")
	ErrNotAuthorized        = errors.New("mesh: peer is paired-authenticated but not application-authorized")
	ErrLeaseExpired         = errors.New("mesh: lease expired before the result arrived")
	ErrStaleResult          = errors.New("mesh: result carries a stale fencing epoch")
	ErrRetryCapExceeded     = errors.New("mesh: retry cap reached; the job failed closed")
	ErrJobCanceled          = errors.New("mesh: job canceled by the controller")
	ErrLedgerClosed         = errors.New("mesh: ledger is closed")
	ErrManagerNotStarted    = errors.New("mesh: manager is not started")
	ErrInvalidConfig        = errors.New("mesh: configuration failed validation")
	ErrUnsupportedUICompute = errors.New("mesh: UI/UX compute is never shared; only AI-inference handlers may register")
)

// JobType is a fixed, allowlisted distributed-job type. The allowlist is
// the AI-inference surface ONLY (blueprint §17 "First distributed
// workloads"): embeddings batches, whole-request inference on a
// model-ready peer, and bounded deterministic read-only AI batch tasks.
// There is deliberately NO ui.* / render.* / shell.* / fs.* type: UI/UX
// compute is never shared, and arbitrary remote tools are out of scope
// for the first release (work order non-negotiable 7).
type JobType string

const (
	// JobTypeEmbeddingsBatch: bounded deterministic embeddings batch.
	// Read-only inputs, bounded output, result verified by the controller.
	JobTypeEmbeddingsBatch JobType = "ai.embeddings.batch"

	// JobTypeWholeRequestInference: one whole inference request executed
	// on ONE model-ready peer (no WAN tensor parallelism in v1).
	JobTypeWholeRequestInference JobType = "ai.inference.whole_request"

	// JobTypeReadonlyAIBatch: bounded deterministic read-only AI batch
	// task (e.g. summarization chunks over already-staged content).
	JobTypeReadonlyAIBatch JobType = "ai.readonly.batch"
)

// jobTypeAllowlist is the closed set. Registration and dispatch key off
// this map — never off a prefix check or a caller-supplied list.
var jobTypeAllowlist = map[JobType]bool{
	JobTypeEmbeddingsBatch:       true,
	JobTypeWholeRequestInference: true,
	JobTypeReadonlyAIBatch:       true,
}

// UIComputeType reports whether t is one of the DOCUMENTED UI/UX
// compute categories. Such types are structurally unrepresentable in
// the allowlist, so the mesh can never carry them. Unknown types are
// NOT UI compute — they are simply unlisted and refused as such.
func UIComputeType(t JobType) bool {
	switch t {
	case "ui.render", "ui.layout", "ui.animation", "ui.input",
		"render.frame", "render.scene", "shell.exec", "fs.read",
		"fs.write", "browser.drive", "code.exec":
		return true
	}
	return false
}

// AllowlistedJobType reports whether the mesh may carry t at all.
func AllowlistedJobType(t JobType) bool { return jobTypeAllowlist[t] }

// PrivacyClass labels what an envelope may contain (blueprint §8.4).
type PrivacyClass string

const (
	// PrivacyPublic: content that may leave the device (owner-approved).
	PrivacyPublic PrivacyClass = "public"
	// PrivacyPrivate: user-private content; the controller refuses to
	// dispatch it to a remote peer in the first release. It stays local.
	PrivacyPrivate PrivacyClass = "private"
)

// Bounds — conservative first-release limits (blueprint §8.7).
const (
	DefaultMaxPayloadBytes   = 4 << 20 // 4 MiB per payload part
	DefaultMaxResultBytes    = 4 << 20 // 4 MiB per result part
	DefaultMaxDeadline       = 10 * time.Minute
	DefaultLeaseTTL          = 90 * time.Second
	DefaultRetryCap          = 2 // one initial attempt + 2 retries
	minLeaseTTL              = 10 * time.Millisecond
	maxRetryCap              = 8
	defaultDispatchQueueSize = 64
)

// Config is the validated, non-secret mesh configuration (blueprint
// §9.1 config.go). Zero value == the safe default: disabled.
type Config struct {
	// Enabled gates EVERYTHING. The zero value is false and the manager
	// refuses Start; no listener, discovery, relay or rendezvous exists
	// in phase 1 at all, enabled or not.
	Enabled bool

	// Role selects what this device may do when the transport phase
	// lands. "controller", "worker" or "both".
	Role string

	// MaxPayloadBytes / MaxResultBytes bound every envelope and result.
	MaxPayloadBytes int
	MaxResultBytes  int

	// MaxDeadline caps the caller-supplied job deadline.
	MaxDeadline time.Duration

	// LeaseTTL is the worker lease window per attempt.
	LeaseTTL time.Duration

	// RetryCap is the maximum number of ATTEMPTS per job (initial +
	// retries). A job that exceeds it fails closed.
	RetryCap int

	// DispatchQueueSize bounds the pending-job queue (backpressure).
	// Phase 1 dispatches synchronously in the caller's goroutine, so the
	// queue is the TRANSPORT-PHASE seam: the libp2p host's bounded offer
	// queue must be sized from this value (blueprint §8.7 DoS limits).
	DispatchQueueSize int
}

// DefaultConfig returns the safe configuration: mesh DISABLED.
func DefaultConfig() Config {
	return Config{
		Enabled:           false,
		Role:              "controller",
		MaxPayloadBytes:   DefaultMaxPayloadBytes,
		MaxResultBytes:    DefaultMaxResultBytes,
		MaxDeadline:       DefaultMaxDeadline,
		LeaseTTL:          DefaultLeaseTTL,
		RetryCap:          DefaultRetryCap,
		DispatchQueueSize: defaultDispatchQueueSize,
	}
}

// Validate enforces the structural bounds; the manager refuses to start
// with an invalid configuration (fail closed).
func (c Config) Validate() error {
	var errs []error
	switch c.Role {
	case "controller", "worker", "both":
	default:
		errs = append(errs, fmt.Errorf("%w: role %q is not controller/worker/both", ErrInvalidConfig, c.Role))
	}
	if c.MaxPayloadBytes <= 0 || c.MaxPayloadBytes > DefaultMaxPayloadBytes {
		errs = append(errs, fmt.Errorf("%w: MaxPayloadBytes %d outside (0,%d]", ErrInvalidConfig, c.MaxPayloadBytes, DefaultMaxPayloadBytes))
	}
	if c.MaxResultBytes <= 0 || c.MaxResultBytes > DefaultMaxResultBytes {
		errs = append(errs, fmt.Errorf("%w: MaxResultBytes %d outside (0,%d]", ErrInvalidConfig, c.MaxResultBytes, DefaultMaxResultBytes))
	}
	if c.MaxDeadline <= 0 || c.MaxDeadline > DefaultMaxDeadline {
		errs = append(errs, fmt.Errorf("%w: MaxDeadline %s outside (0,%s]", ErrInvalidConfig, c.MaxDeadline, DefaultMaxDeadline))
	}
	if c.LeaseTTL < minLeaseTTL {
		errs = append(errs, fmt.Errorf("%w: LeaseTTL %s below the %s floor", ErrInvalidConfig, c.LeaseTTL, minLeaseTTL))
	}
	if c.RetryCap < 0 || c.RetryCap > maxRetryCap {
		errs = append(errs, fmt.Errorf("%w: RetryCap %d outside [0,%d]", ErrInvalidConfig, c.RetryCap, maxRetryCap))
	}
	if c.DispatchQueueSize <= 0 {
		errs = append(errs, fmt.Errorf("%w: DispatchQueueSize must be positive", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
