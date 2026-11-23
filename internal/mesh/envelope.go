package mesh

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Envelope is the immutable typed work unit (blueprint §6.3/§6.4). The
// planner PROPOSES an envelope; deterministic application policy (the
// coordinator) decides whether and where it executes. A model can never
// invent a peer, widen a privacy label or extend the allowlist.
type Envelope struct {
	// Type is one of the fixed AI-inference allowlist entries.
	Type JobType

	// Privacy bounds what the payload may carry. Private payloads are
	// never dispatched to a peer in the first release.
	Privacy PrivacyClass

	// Payload is the task-scoped input (already frozen by the caller;
	// the coordinator copies it before persisting).
	Payload []byte

	// Deadline bounds the WHOLE job (dispatch + attempts). Zero means
	// the configured MaxDeadline.
	Deadline time.Duration

	// MaxResultBytes optionally narrows the result bound.
	MaxResultBytes int
}

func (e Envelope) validate(c Config) error {
	if !AllowlistedJobType(e.Type) {
		return fmt.Errorf("%w: %q (UI/UX compute is never shared; the allowlist is AI-inference only)", ErrUnknownJobType, e.Type)
	}
	if e.Privacy != PrivacyPublic && e.Privacy != PrivacyPrivate {
		return fmt.Errorf("%w: unknown privacy class %q", ErrInvalidEnvelope, e.Privacy)
	}
	if e.Privacy == PrivacyPrivate {
		// Blueprint §8.4: private content stays on-device in the first
		// release. The typed failure makes the refusal visible.
		return fmt.Errorf("%w: private payloads are not dispatched in the first release", ErrPrivacyClassRefused)
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("%w: empty payload", ErrInvalidEnvelope)
	}
	limit := c.MaxPayloadBytes
	if e.MaxResultBytes < 0 || (e.MaxResultBytes != 0 && e.MaxResultBytes > c.MaxResultBytes) {
		return fmt.Errorf("%w: MaxResultBytes %d exceeds the configured bound %d", ErrInvalidEnvelope, e.MaxResultBytes, c.MaxResultBytes)
	}
	if len(e.Payload) > limit {
		return fmt.Errorf("%w: payload %d bytes > bound %d", ErrPayloadTooLarge, len(e.Payload), limit)
	}
	if e.Deadline < 0 || e.Deadline > c.MaxDeadline {
		return fmt.Errorf("%w: deadline %s outside (0,%s]", ErrInvalidEnvelope, e.Deadline, c.MaxDeadline)
	}
	return nil
}

// newID returns a 128-bit random hex identifier (jobs, attempts).
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mesh: id entropy unavailable: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Job state machine (blueprint §6.4 durable state machine):
//
//	pending → offered → leased → running → result_received
//	   → verified → committed
//
// with terminal failed / canceled states and the lease-expiry path
// leased|running|result_received → pending (re-lease) while attempts
// remain, else failed (retry cap).
type State string

const (
	StatePending        State = "pending"
	StateOffered        State = "offered"
	StateLeased         State = "leased"
	StateRunning        State = "running"
	StateResultReceived State = "result_received"
	StateVerified       State = "verified"
	StateCommitted      State = "committed"
	StateFailed         State = "failed"
	StateCanceled       State = "canceled"
)

// terminal reports whether s ends the job's lifecycle.
func terminal(s State) bool {
	return s == StateCommitted || s == StateFailed || s == StateCanceled
}

// Attempt is ONE execution attempt with its fencing epoch. The epoch is
// the attempt number: a result is accepted only from the CURRENT epoch,
// so a slow worker from attempt N can never overwrite the ledger after
// attempt N+1 started (stale-result rejection).
type Attempt struct {
	Epoch      int       // fencing epoch == attempt ordinal (1-based)
	WorkerID   string    // executor identity (fake peer in phase 1)
	GrantedAt  time.Time // lease grant time
	ExpiresAt  time.Time // lease expiry (GrantedAt + LeaseTTL)
	Result     []byte    // result delivered by THIS attempt
	ErrCode    string    // typed failure code, empty on success
	FinishedAt time.Time // zero while the attempt is live
}

// JobRecord is the durable ledger row for one job.
type JobRecord struct {
	ID          string       `json:"id"`
	Type        JobType      `json:"type"`
	Privacy     PrivacyClass `json:"privacy"`
	Payload     []byte       `json:"payload"`
	CreatedAt   time.Time    `json:"created_at"`
	DeadlineAt  time.Time    `json:"deadline_at"`
	State       State        `json:"state"`
	Attempts    []Attempt    `json:"attempts"`
	Epoch       int          `json:"epoch"` // current fencing epoch
	Result      []byte       `json:"result,omitempty"`
	FailureCode string       `json:"failure_code,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at"`
}
