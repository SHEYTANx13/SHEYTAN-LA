package llm

// Rediscover — the Repair/Rediscover surface (v1.3.6, spec §9/§8).
//
// Until now the Tier-2 system discovery pass (engdiscovery.FullScan)
// existed but had no production caller: QuickFind (Tier 0/1) ran on the
// boot path and nothing else. This is the missing production wiring:
//
//   - an explicit, user-triggerable API action (POST /api/engine/rediscover);
//   - it never runs on the startup path (spec §11: no startup disk crawl);
//   - it re-runs the discovery order BEFORE falling back to any download:
//     managed dir → persisted cache → Tier 1 → bounded Tier 2;
//   - a present managed binary is trusted ONLY after the real preflight
//     gate (StaticValidate + dependency closure + bounded --version probe)
//     — os.Stat alone is NOT proof of a usable installation (v1.3.7);
//   - a validated candidate is imported through the ONE provisioning
//     authority (updater.ImportCandidate — closure-restricted, §11/§12);
//   - the whole operation holds the lifecycle lock AND the cross-process
//     ownership lease (spec §15/§16);
//   - an engine that was stopped for the operation is RESTORED afterwards
//     (v1.3.7: the v1.3.6 code left a previously-running engine stopped
//     while reporting ok — the restart condition was inverted);
//   - after a successful import the engine is restarted and verified —
//     an engine that does not start after rediscovery is an ERROR (the
//     v1.3.6 code logged and swallowed it, reporting ok), and the
//     previous package state is honestly reported.
//
// The scan itself is bounded (workers/duration/depth/candidates, see
// engdiscovery.DefaultScanOptions) and access-denied safe.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engdiscovery"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/englease"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// rediscoverScanTimeout bounds the Tier-2 walk for the explicit
// rediscovery operation. The default scan options already bound workers,
// depth and candidate count; this caps the wall clock.
const rediscoverScanTimeout = 3 * time.Minute

// Rediscover runs the full discovery ladder (Tier 0→1→2), imports a
// validated candidate when one exists, and restarts the engine. It is
// the user-facing Repair/Rediscover action.
func (s *LlamaServer) Rediscover() (string, error) {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	cfg := s.src.Load()

	lease, leaseErr := englease.Acquire(engineLeaseDir(cfg), englease.OwnerLease, "engine-owner")
	if leaseErr != nil {
		return "", fmt.Errorf("rediscover refused: %w", leaseErr)
	}

	defer lease.Release()

	// Expectations for the managed binary path.
	binPath := expectedEngineBinPath(cfg)

	wasRunning := s.IsRunning()

	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()

	if wasRunning {
		s.setState(StateUpdating)

		logging.Default().Info("engine", "rediscover: stopping engine for package re-import")

		if err := s.Stop(); err != nil {
			s.mu.Lock()
			s.stopping = false
			s.mu.Unlock()

			return "", fmt.Errorf("stop engine before rediscover: %w", err)
		}
	} else {
		s.setState(StateUpdating)
	}

	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()

	restartLastKnownGood := func() error {
		if startErr := s.startLocked(); startErr != nil {
			return fmt.Errorf("engine restart after rediscovery failed: %w", startErr)
		}

		return nil
	}

	// 1) QUICK ladder first (Tier 0 managed dir): when a VALID engine is
	// already there, rediscovery is a no-op that reports honestly instead
	// of touching anything. v1.3.7: presence alone (os.Stat) is NOT proof
	// the installation is usable — the same preflight gate the boot path
	// runs (format/arch sniff, dependency closure, bounded --version
	// probe) decides. A present-but-broken package (wrong architecture,
	// missing DLLs, loader-class probe failure) falls THROUGH to the
	// repair ladder — that is exactly what the Repair action exists for.
	if _, err := os.Stat(binPath); err == nil {
		_, pfail := s.preflightBinary(cfg, binPath)

		if pfail == nil {
			// Valid package. Restore the engine state this operation
			// interrupted: wasRunning means we stopped it above, so restart
			// it; a deliberately-stopped engine stays stopped (preflight
			// already proved the package executes).
			if wasRunning {
				if rerr := restartLastKnownGood(); rerr != nil {
					return "", rerr
				}
			}

			return fmt.Sprintf(
				"managed engine already present and usable at %s — nothing to rediscover",
				binPath,
			), nil
		}

		s.logf("rediscover: managed engine present but failed preflight validation — %s; searching for a replacement", pfail.Detail)
	}

	// 2) QuickFind (Tier 0b cache + Tier 1 cheap locations).
	if s.tryImportDiscoveredEngine(cfg) {
		if _, err := os.Stat(binPath); err == nil {
			if rerr := restartLastKnownGood(); rerr != nil {
				return "", rerr
			}

			return "rediscovered engine imported from the quick discovery tier (cache/PATH/known locations)", nil
		}
	}

	// 3) Tier-2 bounded background-grade scan — here run synchronously
	// because the user EXPLICITLY asked for rediscovery (never on the
	// startup path, spec §11).
	scanCtx, cancel := context.WithTimeout(context.Background(), rediscoverScanTimeout)
	defer cancel()

	s.logf("rediscover: running bounded full-system discovery scan (tier 2)")

	cands := engdiscovery.FullScan(scanCtx, cfg, llamaBinaryName(), engdiscovery.DefaultScanOptions())

	var best *engdiscovery.Candidate

	for i := range cands {
		if cands[i].Validated && cands[i].Kind == "llama-server" {
			if best == nil || cands[i].Tier < best.Tier {
				best = &cands[i]
			}
		}
	}

	if best == nil {
		// Honest failure: nothing usable exists locally. The caller can
		// now decide to download (online) or stay offline with a clear
		// error — rediscovery never downloads by itself.
		msg := "system discovery found no usable engine on this machine (Tier 0/1/2 exhausted)"

		if wasRunning {
			if rerr := restartLastKnownGood(); rerr != nil {
				return "", fmt.Errorf("%s; additionally, restoring the previous engine failed: %w", msg, rerr)
			}
		}

		return msg, errors.New(msg)
	}

	s.logf("rediscover: importing validated candidate %s (tier %d, sha256 %.12s…)",
		best.Path, best.Tier, best.SHA256)

	result, ierr := updater.ImportCandidate(cfg, filepath.Dir(best.Path), filepath.Base(best.Path))
	if ierr != nil {
		if wasRunning {
			if rerr := restartLastKnownGood(); rerr != nil {
				return "", fmt.Errorf("rediscovered candidate rejected: %v; additionally, restoring the previous engine failed: %w", ierr, rerr)
			}
		}

		return "", fmt.Errorf("rediscovered candidate rejected: %w", ierr)
	}

	if best.Tag != "" {
		updater.RecordEngineTag(cfg, best.Tag)
	}

	// The import succeeded: restart and VERIFY (the header contract — an
	// engine that does not start after rediscovery is an ERROR, never a
	// swallowed warning with an ok result).
	if rerr := restartLastKnownGood(); rerr != nil {
		return "", rerr
	}

	logging.Default().Info("engine", "rediscover imported: %s", result.Outcome)

	return result.Outcome, nil
}
