#!/usr/bin/env bash
# consolidate-run-log.sh — build the ONE consolidated plain-text run log.
#
# Invoked by the consolidate-logs job of build-desktop.yml after the
# per-job log-part artifacts have been downloaded WITHOUT
# merge-multiple (each artifact lands in its own namespaced directory,
# so identically named logs from different jobs — e.g. gate-order.txt —
# can never overwrite each other).
#
# Contract (run #54 repair):
#   - Deterministic output: header → run summary → error index → warning
#     index → missing/never-started sources → full logs ordered by job.
#   - Collision detection: identical relative paths inside one job are
#     reported, never silently concatenated in an ambiguous order.
#   - A job whose parts are absent gets an explicit MISSING LOG SOURCE
#     section; a job with conclusion "skipped" gets NEVER_STARTED — it is
#     NOT reported as "failed while running".
#   - Exit code: 0 when every expected job contributed parts; 1 when any
#     source is missing — AFTER the report has been written, so the
#     upload step still publishes the full report.
#
# Usage:
#   consolidate-run-log.sh --out FILE --parts-root DIR \
#     --run-id ID --attempt N --commit SHA --ref REF \
#     --workflow NAME --trigger EVENT --started-at UTC \
#     --job "name:conclusion" [--job "name:conclusion" ...]
set -uo pipefail

usage() { echo "usage: $0 --out FILE --parts-root DIR --run-id ID --attempt N --commit SHA --ref REF --workflow NAME --trigger EVENT --started-at UTC --job name:conclusion ..." >&2; exit 64; }

OUT=""; PARTS=""; RUN_ID=""; ATTEMPT=""; COMMIT=""; REF=""; WORKFLOW=""; TRIGGER=""; STARTED=""
JOBS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2;;
    --parts-root) PARTS="$2"; shift 2;;
    --run-id) RUN_ID="$2"; shift 2;;
    --attempt) ATTEMPT="$2"; shift 2;;
    --commit) COMMIT="$2"; shift 2;;
    --ref) REF="$2"; shift 2;;
    --workflow) WORKFLOW="$2"; shift 2;;
    --trigger) TRIGGER="$2"; shift 2;;
    --started-at) STARTED="$2"; shift 2;;
    --job) JOBS+=("$2"); shift 2;;
    *) usage;;
  esac
done
[ -n "$OUT" ] && [ -n "$PARTS" ] && [ -n "$RUN_ID" ] && [ -n "$ATTEMPT" ] && [ -n "$COMMIT" ] && [ -n "$WORKFLOW" ] && [ ${#JOBS[@]} -gt 0 ] || usage

now_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# --- inventory every job's parts --------------------------------------
captured=0; missing=0; never_started=0; warnings=0; errors=0
declare -a missing_jobs=() never_jobs=()

for spec in "${JOBS[@]}"; do
  name="${spec%%:*}"; conclusion="${spec#*:}"
  if [ "$conclusion" = "skipped" ]; then
    never_jobs+=("$name"); never_started=$((never_started+1))
    continue
  fi
  if [ -d "$PARTS/$name" ] && [ -n "$(ls -A "$PARTS/$name" 2>/dev/null)" ]; then
    captured=$((captured+1))
  else
    missing_jobs+=("$name"); missing=$((missing+1))
  fi
done

total_expect=${#JOBS[@]}

# --- count structured records across all parts ------------------------
if [ -d "$PARTS" ]; then
  errors=$(grep -r -l "EXIT_CODE: [1-9][0-9]* FAIL" "$PARTS" 2>/dev/null | wc -l)
  warnings=$(grep -r -l "WARNING:" "$PARTS" 2>/dev/null | wc -l)
fi

# --- collision detection: duplicate relative paths inside one job -----
declare -a collisions=()
if [ -d "$PARTS" ]; then
  while IFS= read -r d; do
    jobname="$(basename "$d")"
    dupes="$(find "$d" -type f -printf '%P\n' 2>/dev/null | sort | uniq -d)"
    if [ -n "$dupes" ]; then
      collisions+=("$jobname: $dupes")
    fi
  done < <(find "$PARTS" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)
fi

# --- emit --------------------------------------------------------------
{
  echo "SHEYTAN-LA consolidated run log — run $RUN_ID attempt $ATTEMPT"
  echo "workflow: $WORKFLOW"
  echo "commit: $COMMIT"
  echo "ref: $REF"
  echo "trigger: $TRIGGER"
  echo "run started (UTC): $STARTED"
  echo "report generated (UTC): $now_utc"
  echo ""
  echo "=== RUN SUMMARY ==="
  echo "expected jobs: $total_expect"
  echo "jobs with captured logs: $captured"
  echo "jobs with MISSING LOG SOURCE: $missing"
  echo "jobs NEVER_STARTED (skipped): $never_started"
  echo "files with failed commands: $errors"
  echo "files with warnings: $warnings"
  echo "duplicate relative paths detected: ${#collisions[@]}"
  echo ""
  echo "=== ERROR INDEX ==="
  if [ -d "$PARTS" ]; then
    grep -rn -E "EXIT_CODE: [1-9][0-9]* FAIL|::error::|ERROR: |--- FAIL:|^FAIL|exit status [1-9]" "$PARTS" 2>/dev/null | sort || true
  fi
  echo ""
  echo "=== WARNING INDEX ==="
  if [ -d "$PARTS" ]; then
    grep -rn -E "WARNING:|::warning::" "$PARTS" 2>/dev/null | sort || true
  fi
  echo ""
  if [ ${#collisions[@]} -gt 0 ]; then
    echo "=== DUPLICATE RELATIVE PATHS (reported, not silently merged) ==="
    for c in "${collisions[@]}"; do echo "$c"; done
    echo ""
  fi
  if [ ${#never_jobs[@]} -gt 0 ]; then
    echo "=== NEVER_STARTED ==="
    for j in "${never_jobs[@]}"; do
      echo "job '$j' was SKIPPED by the workflow — it never ran; no log exists and none is expected. This is not a failed command."
    done
    echo ""
  fi
  if [ ${#missing_jobs[@]} -gt 0 ]; then
    echo "=== MISSING LOG SOURCE ==="
    for j in "${missing_jobs[@]}"; do
      echo "job '$j' concluded without contributing log parts — the artifact upload may have failed, or the job never started. Coverage is INCOMPLETE for this run."
    done
    echo ""
  fi
} >"$OUT"

# --- full logs, deterministic job order --------------------------------
for spec in "${JOBS[@]}"; do
  name="${spec%%:*}"; conclusion="${spec#*:}"
  {
    echo "===== JOB $name (conclusion: $conclusion) ====="
    if [ -d "$PARTS/$name" ]; then
      find "$PARTS/$name" -type f 2>/dev/null | LC_ALL=C sort | while IFS= read -r f; do
        rel="${f#"$PARTS"/}"
        echo "--- $rel ---"
        cat "$f"
        echo ""
      done
    else
      echo "(no log parts)"
      echo ""
    fi
  } >>"$OUT"
done

if [ "$missing" -gt 0 ]; then
  echo "consolidate-run-log: $missing missing log source(s); report written to $OUT; failing after preservation" >&2
  exit 1
fi
echo "consolidate-run-log: complete ($captured/$total_expect jobs captured) → $OUT"
exit 0
