#!/usr/bin/env bash
# capture.sh — first-byte command capture for the consolidated run log.
#
# Usage: capture.sh <job> <step-name> <logfile> -- <command...>
#
# Contract (build-desktop consolidated-log v2, run #54 repair):
#   - stdout AND stderr are captured from the first byte (tee, pipefail).
#   - The original exit code is preserved and propagated — never swallowed.
#   - A structured record block is appended to the namespaced log file:
#       STARTED / EXIT_CODE <n> PASS|FAIL / ENDED, plus command + workdir.
#   - The caller's `set -e` still fires on FAIL because this helper exits
#     with the command's own status.
set -uo pipefail

job="${1:?capture.sh: job name required}"
step="${2:?capture.sh: step name required}"
logfile="${3:?capture.sh: log path required}"
shift 3
[ "${1:-}" = "--" ] && shift
[ $# -gt 0 ] || { echo "capture.sh: no command given" >&2; exit 64; }

mkdir -p "$(dirname "$logfile")"

start_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
{
  echo "=== COMMAND RECORD ==="
  echo "JOB: $job"
  echo "STEP: $step"
  echo "STARTED: $start_utc"
  echo "WORKDIR: $PWD"
  echo "COMMAND: $*"
} >>"$logfile"

"$@" > >(tee -a "$logfile") 2> >(tee -a "$logfile" >&2)
status=$?

end_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ver=""
if command -v go >/dev/null 2>&1; then ver="go $(go version 2>/dev/null | awk '{print $3}')"; fi

{
  echo "EXIT_CODE: $status $([ $status -eq 0 ] && echo PASS || echo FAIL)"
  echo "ENDED: $end_utc"
  echo "TOOLCONTEXT: ${ver:-system tools}"
  echo "=== END COMMAND RECORD ==="
} >>"$logfile"

exit $status
