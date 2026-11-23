#!/usr/bin/env bash
# module-preflight.sh — bounded-retry module prefetch for the CI gates
# (run 45165536396 repair).
#
# Root cause this closes: the hosted lanes' `go test ./internal/...`
# downloads modules lazily. On run 45165536396 the macos-x64 lane's
# proxy.golang.org fetches for the gogpu/wgpu dependency family timed
# out mid-gate ("read: operation timed out" from the runner VM, six
# occurrences) and the headless suite failed as a BUILD failure with
# ZERO test-failure records — a transient proxy failure that was
# indistinguishable from a code failure. A gate must never download.
#
# Contract:
#   1. Prefetches the full module graph (go mod download all) BEFORE any
#      gate runs, so the gates themselves never hit the proxy.
#   2. Bounded retries: EXACTLY three attempts with a fixed 15s backoff.
#      The sleep is not a correctness mechanism (the prefetch is
#      idempotent and the observed failure was transient) — the
#      correctness comes from the final state check: modules present or
#      the gate fails. No unbounded loop, no event-free polling.
#   3. Fail-closed posture: after the final attempt the gate fails with
#      the EXACT captured error text (not an opaque exit) so the
#      collector records the real network root cause.
#   4. The attempt log lands in ci-logs/<name>-module-preflight.log.
#
# Usage: scripts/ci/module-preflight.sh [preflight-name]
#   preflight-name defaults to "module" — pass the lane target (e.g.
#   "$TARGET") so each lane writes its own evidence log.
# Exit codes: 0 = module graph fully cached; 1 = prefetch failed after
#   the bounded retries.
set -euo pipefail

name="${1:-module}"
mkdir -p ci-logs
log="ci-logs/${name}-module-preflight.log"

# The prefetch is recorded verbatim: every attempt's output is evidence.
: > "$log"

attempts=0
max_attempts=3
backoff_seconds=15

while :; do
  attempts=$((attempts + 1))
  echo "attempt ${attempts}/${max_attempts}: go mod download all" | tee -a "$log"
  if go mod download all >> "$log" 2>&1; then
    echo "module prefetch OK after attempt ${attempts}" | tee -a "$log"
    exit 0
  fi

  if [ "$attempts" -ge "$max_attempts" ]; then
    echo "module prefetch FAILED after ${max_attempts} bounded attempts" | tee -a "$log"
    # Emit the exact network root cause so the collector's error scan
    # carries the real failure, never an opaque exit code.
    tail -n 6 "$log" | while IFS= read -r line; do
      echo "::error::module prefetch: $line"
    done
    exit 1
  fi

  echo "attempt ${attempts} failed; bounded backoff ${backoff_seconds}s before the next attempt" | tee -a "$log"
  sleep "$backoff_seconds"
done
