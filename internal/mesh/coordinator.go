package mesh

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Coordinator is the controller-side dispatch engine (blueprint §9.1
// coordinator.go): eligibility → lease (fencing epoch) → execute →
// result under the epoch → controller verification → commit, with lease
// expiry re-queue, retry cap, cancellation and typed failures.
//
// Phase 1 executes everything IN-PROCESS against registered handlers
// (fake/local workers, deterministic tests). The phase-2 transport will
// replace the execute step with a libp2p stream to ONE paired,
// authorized worker; the ledger/coordinator contracts below are the
// stable seam that transport must honor (offer → lease grant/renewal →
// result → commit).
type Coordinator struct {
	cfg      Config
	ledger   *Ledger
	registry *Registry
	now      func() time.Time
}

// NewCoordinator wires the controller engine.
func NewCoordinator(cfg Config, ledger *Ledger, registry *Registry) (*Coordinator, error) {
	if ledger == nil || registry == nil {
		return nil, fmt.Errorf("%w: nil ledger or registry", ErrInvalidConfig)
	}
	return &Coordinator{cfg: cfg, ledger: ledger, registry: registry, now: time.Now}, nil
}

// Dispatch runs ONE job end-to-end in the calling goroutine:
//
//	validate → create (pending) → lease → execute → deliver
//	  → verify → commit
//
// Failures re-queue while the retry cap and deadline allow; otherwise
// the job fails closed with the typed code. The returned record is the
// final ledger row.
func (c *Coordinator) Dispatch(ctx context.Context, env Envelope) (JobRecord, error) {
	rec, err := c.ledger.Create(env, c.cfg)
	if err != nil {
		return JobRecord{}, err
	}
	if err := c.run(ctx, rec.ID, env); err != nil {
		final, _ := c.ledger.Get(rec.ID)
		return final, err
	}
	final, _ := c.ledger.Get(rec.ID)
	return final, nil
}

// Resume drives an EXISTING pending job (restart recovery path) through
// the same loop as Dispatch.
func (c *Coordinator) Resume(ctx context.Context, id string) (JobRecord, error) {
	rec, ok := c.ledger.Get(id)
	if !ok {
		return JobRecord{}, fmt.Errorf("mesh: unknown job %s", id)
	}
	env := Envelope{
		Type:     rec.Type,
		Privacy:  rec.Privacy,
		Payload:  rec.Payload,
		Deadline: time.Until(rec.DeadlineAt),
	}
	if err := env.validate(c.cfg); err != nil {
		return rec, err
	}
	if err := c.run(ctx, id, env); err != nil {
		final, _ := c.ledger.Get(id)
		return final, err
	}
	final, _ := c.ledger.Get(id)
	return final, nil
}

// DispatchToPeer drives ONE job end-to-end with the compute executed by
// a PEER through the exchange seam (phase 2a: the phone-as-interface
// path — a compact interface device dispatches allowlisted AI-inference
// work to a workstation and commits the verified result in ITS OWN
// ledger). The requester-side durability is identical to the local
// path: the ledger row is created (pending) BEFORE any peer I/O, the
// attempt is leased under a fencing epoch, and a peer refusal or an
// unverified result fails the row closed — a dead peer never turns into
// silent success, and no retry is re-offered to a peer that just
// refused (cross-peer retries are the phase-3 transport's contract
// against the same ledger API).
func (c *Coordinator) DispatchToPeer(ctx context.Context, peer PeerExchange, env Envelope) (JobRecord, error) {
	if peer == nil {
		return JobRecord{}, fmt.Errorf("%w: nil peer exchange", ErrPeerExchange)
	}

	rec, err := c.ledger.Create(env, c.cfg)
	if err != nil {
		return JobRecord{}, err
	}

	leased, err := c.ledger.GrantLease(rec.ID, "peer/loopback", c.cfg.LeaseTTL, c.cfg.RetryCap)
	if err != nil {
		return JobRecord{}, err
	}

	if err := c.ledger.MarkRunning(rec.ID, leased.Epoch); err != nil {
		return c.failRec(rec.ID, err)
	}

	result, offerErr := peer.Offer(ctx, env)
	if offerErr != nil {
		return c.failRec(rec.ID, fmt.Errorf("%w: peer refused %s %s: %v", ErrPeerExchange, env.Type, env.Privacy, offerErr))
	}

	if err := c.ledger.DeliverResult(rec.ID, leased.Epoch, result, c.cfg); err != nil {
		return c.failRec(rec.ID, err)
	}
	if verr := verifyResult(env, result); verr != nil {
		return c.failRec(rec.ID, verr)
	}
	if err := c.ledger.MarkVerified(rec.ID); err != nil {
		return c.failRec(rec.ID, err)
	}
	if err := c.ledger.Commit(rec.ID); err != nil {
		return c.failRec(rec.ID, err)
	}

	final, _ := c.ledger.Get(rec.ID)
	return final, nil
}

// Cancel cancels a non-terminal job (owner action). The typed result
// makes the terminal-vs-canceled distinction visible.
func (c *Coordinator) Cancel(id string) error {
	return c.ledger.Cancel(id)
}

// run is the lease→execute→settle loop for one job.
func (c *Coordinator) run(ctx context.Context, id string, env Envelope) error {
	for {
		if cerr := ctx.Err(); cerr != nil {
			return errors.Join(ErrJobCanceled, cerr)
		}

		worker := "local/" + string(env.Type)
		rec, err := c.ledger.GrantLease(id, worker, c.cfg.LeaseTTL, c.cfg.RetryCap)
		if err != nil {
			return err
		}
		epoch := rec.Epoch

		h, err := c.registry.Lookup(env.Type)
		if err != nil {
			return c.fail(id, err)
		}

		if err := c.ledger.MarkRunning(id, epoch); err != nil {
			return c.fail(id, err)
		}

		execCtx, cancel := context.WithTimeout(withEpoch(ctx, epoch), c.cfg.LeaseTTL)
		result, execErr := h.Execute(execCtx, env)
		cancel()

		if cerr := ctx.Err(); cerr != nil {
			return errors.Join(ErrJobCanceled, cerr)
		}

		// Attempt did not settle: consume the attempt, re-queue while
		// the cap allows, else fail closed (blueprint §6.7).
		settleErr := execErr
		if settleErr == nil {
			err = c.ledger.DeliverResult(id, epoch, result, c.cfg)
			if err == nil {
				// Controller verification (blueprint §9.5: worker output
				// is untrusted; the controller verifies before commit).
				// Phase 1 verifies structure + size; the phase-2
				// verifier seam deepens this per handler contract.
				if verr := verifyResult(env, result); verr != nil {
					return c.fail(id, verr)
				}
				if err := c.ledger.MarkVerified(id); err != nil {
					return c.fail(id, err)
				}
				if err := c.ledger.Commit(id); err != nil {
					return c.fail(id, err)
				}
				return nil
			}
			settleErr = err
		}
		if errors.Is(settleErr, context.DeadlineExceeded) {
			settleErr = ErrLeaseExpired
		}
		err = c.ledger.RequeueAfterAttemptFailure(id, c.cfg.RetryCap, settleErr)
		if err != nil {
			return err
		}
		final, _ := c.ledger.Get(id)
		if final.State == StateFailed {
			return errors.Join(ErrRetryCapExceeded, settleErr)
		}
		// loop: next attempt
	}
}

func (c *Coordinator) fail(id string, cause error) error {
	if ferr := c.ledger.Fail(id, cause); ferr != nil {
		return errors.Join(cause, ferr)
	}
	return cause
}

// failRec is fail() for the two-value dispatch paths: the FINAL ledger
// row (state FAILED) is fetched after the typed failure is recorded, so
// callers always see the terminal state next to the error — the same
// shape Dispatch returns on failure.
func (c *Coordinator) failRec(id string, cause error) (JobRecord, error) {
	err := c.fail(id, cause)
	final, _ := c.ledger.Get(id)
	return final, err
}

// verifyResult is the phase-one controller verification: a result must
// be non-empty and within the bound. Real per-handler verification
// (e.g. embeddings dimension checks) rides the phase-2 typed handlers.
func verifyResult(env Envelope, result []byte) error {
	if len(result) == 0 {
		return fmt.Errorf("mesh: verification failed: empty result for %s", env.Type)
	}
	limit := DefaultMaxResultBytes
	if env.MaxResultBytes > 0 {
		limit = env.MaxResultBytes
	}
	if len(result) > limit {
		return fmt.Errorf("%w: result %d > envelope bound %d", ErrPayloadTooLarge, len(result), limit)
	}
	return nil
}
