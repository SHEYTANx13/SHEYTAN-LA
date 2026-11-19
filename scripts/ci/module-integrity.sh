#!/usr/bin/env bash
# module-integrity.sh — reproducible Go module integrity gate
# (run 44378562071 repair, work order §4.1).
#
# Contract:
#   1. Uses the EXACT repository toolchain: the go.mod toolchain directive
#      is the one version authority; GOTOOLCHAIN=local forbids any other
#      toolchain from silently answering (version drift between lanes was
#      the Windows gofmt root cause class this run repairs).
#   2. go mod verify must report all modules verified.
#   3. go mod tidy -diff must be EMPTY — twice, in the same environment
#      (GOTOOLCHAIN/GOWORK/GOFLAGS/GOOS/GOARCH recorded for
#      reproducibility). A non-empty diff is printed with the real module
#      root and exits 1 — never a mutating tidy followed by concealment.
#
# Usage: scripts/ci/module-integrity.sh [--log-dir ci-logs]
# Exit codes: 0 = clean; 1 = drift or verification failure.
set -euo pipefail

log_dir="ci-logs"
while [ $# -gt 0 ]; do
  case "$1" in
    --log-dir) log_dir="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

# Root-anchored: independent of the caller's CWD/staging directory.
modroot="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$modroot"
mkdir -p "$log_dir"

# The recorded environment — printed into the evidence log so a drift
# report is reproducible byte-for-byte on any machine.
envlog="$log_dir/dep-module-env.log"
{
  echo "module root: $modroot"
  echo "toolchain: $(go version)"
  echo "GOTOOLCHAIN=$(go env GOTOOLCHAIN) GOWORK=$(go env GOWORK) GOFLAGS=$(go env GOFLAGS)"
  echo "GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) CGO_ENABLED=$(go env CGO_ENABLED)"
  echo "GOPROXY=$(go env GOPROXY)"
} | tee "$envlog"

echo "gate: go mod verify" | tee -a "$envlog"
go mod verify | tee "$log_dir/dep-integrity.log"

# tidy -diff pass 1: never mutates go.mod/go.sum — the committed state is
# compared against the toolchain-canonical state directly.
echo "gate: go mod tidy -diff (pass 1)" | tee -a "$envlog"
if ! go mod tidy -diff > "$log_dir/dep-tidy-drift.log" 2>&1; then
  echo "::error::go mod tidy -diff (pass 1) non-empty — the committed go.mod/go.sum differ from the toolchain-canonical state (module root $modroot; diff at $log_dir/dep-tidy-drift.log)"
  cat "$log_dir/dep-tidy-drift.log"
  exit 1
fi

# tidy -diff pass 2: proves determinism of the first answer.
echo "gate: go mod tidy -diff (pass 2)" | tee -a "$envlog"
if ! go mod tidy -diff > "$log_dir/dep-tidy-drift-2.log" 2>&1; then
  echo "::error::go mod tidy -diff (pass 2) non-empty — the tidied state is not idempotent in this environment (module root $modroot; diff at $log_dir/dep-tidy-drift-2.log)"
  cat "$log_dir/dep-tidy-drift-2.log"
  exit 1
fi

echo "module integrity OK: go mod verify clean; go mod tidy -diff empty twice" | tee -a "$envlog"
