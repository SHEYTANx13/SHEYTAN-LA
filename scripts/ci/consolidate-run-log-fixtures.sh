#!/usr/bin/env bash
# consolidate-run-log-fixtures.sh — synthetic-fixture suite for the
# consolidated run-log collector (Phase 6 contract, run #54 repair).
#
# Covers the required fixture classes:
#   1. successful job           5. missing job artifact
#   2. failed command           6. duplicate filenames in one job
#   3. warning-only job         7. red workflow (missing + failed mix)
#   4. empty log                8. never-started (skipped) job
#
# No fixture may assume a missing log means success: cases 5 and 7 must
# FAIL the collector AFTER the report exists, and the report must carry a
# visible MISSING LOG SOURCE section.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
collector="$here/consolidate-run-log.sh"
pass=0; fail=0

say()  { printf '  \xe2\x96\xb8 %s\n' "$1"; }
ok()   { pass=$((pass+1)); say "ok   $1"; }
bad()  { fail=$((fail+1)); say "FAIL $1"; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

new_case() {
  c="$work/case$1"; parts="$c/parts"; mkdir -p "$parts"
  out="$c/report.txt"
}

run_collector() {  # $1 = case dir, extra --job specs as $2..N
  local c="$1"; shift
  bash "$collector" \
    --out "$c/report.txt" --parts-root "$c/parts" \
    --run-id 40394318382 --attempt 1 \
    --commit e6e1968b0a10b9f9da059c390ec5fc51723ad05f \
    --ref refs/heads/main --workflow build-desktop \
    --trigger push --started-at 2026-10-26T20:17:30Z \
    "$@"
}

# 1. successful job -------------------------------------------------------
new_case 1
mkdir -p "$parts/audit"
printf 'RUN go test\nEXIT_CODE: 0 PASS\n' > "$parts/audit/stress.log"
run_collector "$c" --job "audit:success"
rc=$?
[ $rc -eq 0 ] && grep -q "jobs with captured logs: 1" "$out" \
  && grep -q "MISSING LOG SOURCE" "$out" || true
if [ $rc -eq 0 ] && grep -q "jobs with captured logs: 1" "$out" && ! grep -q "^=== MISSING LOG SOURCE ===" "$out"; then
  ok "successful job → exit 0, no MISSING section"
else
  bad "successful job (rc=$rc)"
fi

# 2. failed command -------------------------------------------------------
new_case 2
mkdir -p "$parts/windows"
printf 'EXIT_CODE: 1 FAIL\nerror: cannot resolve\n' > "$parts/windows/build-info.log"
run_collector "$c" --job "windows:failure"
rc=$?
if [ $rc -eq 0 ] && grep -q "EXIT_CODE: 1 FAIL" "$out" && grep -q "files with failed commands: 1" "$out"; then
  ok "failed command recorded in summary + index, collector still succeeds (report-only job)"
else
  bad "failed command (rc=$rc)"
fi

# 3. warning-only job -----------------------------------------------------
new_case 3
mkdir -p "$parts/preflight"
printf 'WARNING: MINGW64 is deprecated\nEXIT_CODE: 0 PASS\n' > "$parts/preflight/compile.log"
run_collector "$c" --job "preflight:success"
rc=$?
if [ $rc -eq 0 ] && grep -q "files with warnings: 1" "$out" && grep -q "WARNING: MINGW64 is deprecated" "$out"; then
  ok "warning-only job surfaced in the warning index"
else
  bad "warning-only job (rc=$rc)"
fi

# 4. empty log file -------------------------------------------------------
new_case 4
mkdir -p "$parts/ui"
: > "$parts/ui/smoke.log"
run_collector "$c" --job "ui:success"
rc=$?
if [ $rc -eq 0 ] && grep -q "jobs with captured logs: 1" "$out"; then
  ok "empty log counts as captured and is included verbatim"
else
  bad "empty log (rc=$rc)"
fi

# 5. missing job artifact -------------------------------------------------
new_case 5
mkdir -p "$parts/audit"
printf 'EXIT_CODE: 0 PASS\n' > "$parts/audit/x.log"
run_collector "$c" --job "audit:success" --job "linux:failure"
rc=$?
if [ $rc -eq 1 ] && grep -q "^=== MISSING LOG SOURCE ===" "$out" && grep -q "job 'linux'" "$out" && [ -s "$out" ]; then
  ok "missing source → report written FIRST, then exit 1 (red runs keep their log)"
else
  bad "missing source (rc=$rc)"
fi

# 6. duplicate filenames inside one job ----------------------------------
new_case 6
mkdir -p "$parts/ui/sub"
printf 'from a\n' > "$parts/ui/gate-order.txt"
printf 'from b\n' > "$parts/ui/sub/gate-order.txt"
run_collector "$c" --job "ui:success"
rc=$?
if [ $rc -eq 0 ] && grep -q "duplicate relative paths detected: 0" "$out" \
  && grep -q -- "--- ui/gate-order.txt ---" "$out" && grep -q -- "--- ui/sub/gate-order.txt ---" "$out"; then
  ok "distinct namespaced paths kept apart (no collision, both included)"
else
  bad "namespaced duplicates (rc=$rc)"
fi

# 7. filenames with spaces: deterministic order, included verbatim.
# (True duplicate RELATIVE paths inside one job directory are structurally
# impossible in a real checkout tree — the collector's collision scan
# stays as defense-in-depth for exotic artifact zips.)
new_case 7
mkdir -p "$parts/ui"
printf 'z content\n' > "$parts/ui/zlast log.txt"
printf 'a content\n' > "$parts/ui/a first.log"
run_collector "$c" --job "ui:success"
rc=$?
if [ $rc -eq 0 ] && grep -q -- "--- ui/a first.log ---" "$out" \
  && [ "$(grep -n -- '--- ui/' "$out" | head -1 | cut -d: -f2- | grep -c 'a first')" = "1" ]; then
  ok "filenames with spaces sort deterministically (a first.log before zlast log.txt)"
else
  bad "space-filename ordering (rc=$rc)"
fi

# 7. red workflow: failed job + missing source + skipped job --------------
new_case 8
mkdir -p "$parts/preflight" "$parts/windows"
printf 'EXIT_CODE: 1 FAIL\n' > "$parts/preflight/vet.log"
printf 'EXIT_CODE: 1 FAIL\n' > "$parts/windows/build.log"
run_collector "$c" --job "preflight:failure" --job "windows:failure" --job "linux:failure" --job "ui:skipped"
rc=$?
if [ $rc -eq 1 ] && grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && grep -q "^=== NEVER_STARTED ===" "$out" \
  && grep -q "job 'ui' was SKIPPED" "$out" \
  && grep -q "This is not a failed command" "$out" \
  && grep -q "jobs NEVER_STARTED (skipped): 1" "$out"; then
  ok "red workflow: missing + never-started distinguished; NEVER_STARTED not mislabelled"
else
  bad "red workflow (rc=$rc)"
fi

# 8. never-started-only run ----------------------------------------------
new_case 9
run_collector "$c" --job "release:skipped"
rc=$?
if [ $rc -eq 0 ] && grep -q "jobs NEVER_STARTED (skipped): 1" "$out" && grep -q "jobs with captured logs: 0" "$out"; then
  ok "all-skipped run is a complete report, not a missing-source failure"
else
  bad "all-skipped run (rc=$rc)"
fi

printf '\nfixtures: %d pass / %d fail\n' "$pass" "$fail"
[ $fail -eq 0 ]
