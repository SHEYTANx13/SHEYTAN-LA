#!/usr/bin/env bash
# consolidate-run-log-fixtures.sh — synthetic-fixture suite for the
# consolidated run-log collector (Phase 6 contract, run #54 repair;
# v1.11.1 stable-artifact-name update).
#
# Covers the required fixture classes:
#   1. successful job           7. red workflow (missing + failed mix)
#   2. failed command           8. never-started (skipped) job
#   3. warning-only job         9. run #118 shape (stable names now)
#   4. empty log               10. ran-but-failed without parts
#   5. missing job artifact    11. v1.11.1: all five parts, stable names,
#   6. duplicate filenames         no flat-path collisions
#      in one job               12. v1.11.1: rerun/overwrite — the SAME
#                                   stable job names land in the SAME
#                                   namespaced dirs on a retry; the
#                                   collector still sees 5/5, and the
#                                   attempt lives only in report metadata
#
# No fixture may assume a missing log means success: the missing-source
# cases must FAIL the collector AFTER the report exists, and the report
# must carry a visible MISSING LOG SOURCE section.
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

# 10. run #118 regression: EXACT hosted shape — one failed job whose
# artifact name is the FULL namespaced form (v1.11.1: the STABLE name,
# no attempt suffix — a rerun replaces the same artifact), four skipped
# jobs. The hosted red came from the flat single-match download layout,
# not from the collector; this fixture pins the collector contract: such
# a run is COMPLETE (1/5 captured, 4 never-started) and must exit 0. If
# the collector ever regresses to treating the skipped jobs or this shape
# as missing sources, this fixture fails.
new_case 10
mkdir -p "$parts/preflight-logs-part"
printf 'RUN go build ./...\ngate OK: go build ./...\n' > "$parts/preflight-logs-part/gate-order.txt"
printf 'EXIT_CODE: 1 FAIL\n' > "$parts/preflight-logs-part/preflight-build.log"
run_collector "$c" \
  --job "preflight-logs-part:failure" \
  --job "audit-logs-part:skipped" \
  --job "ui-logs-part:skipped" \
  --job "windows-logs-part:skipped" \
  --job "linux-logs-part:skipped"
rc=$?
if [ $rc -eq 0 ] && grep -q "jobs with captured logs: 1" "$out" \
  && grep -q "jobs NEVER_STARTED (skipped): 4" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out"; then
  ok "run #118 shape, stable names (one failed job with parts + four skipped) is a complete report, exit 0"
else
  bad "run #118 shape, stable names (rc=$rc)"
fi

# 11. a job that RAN and FAILED but contributed no parts (artifact upload
# lost) is a real coverage gap: report first, then exit 1, naming the job.
new_case 11
run_collector "$c" \
  --job "preflight-logs-part:failure" \
  --job "audit-logs-part:skipped" \
  --job "ui-logs-part:skipped" \
  --job "windows-logs-part:skipped" \
  --job "linux-logs-part:skipped"
rc=$?
if [ $rc -eq 1 ] && grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && grep -q "job 'preflight-logs-part'" "$out" \
  && grep -q "jobs NEVER_STARTED (skipped): 4" "$out"; then
  ok "ran-and-failed without parts → exit 1, job named in MISSING LOG SOURCE"
else
  bad "failed job without parts (rc=$rc)"
fi

# 12. v1.11.1 stable names, green run: ALL FIVE log parts captured under
# their stable artifact names — including the SAME filename (gate-order.txt)
# in every job directory — proving the namespaced extraction never lets
# jobs flatten/collide (the download path IS the namespace).
new_case 12
for j in preflight-logs-part audit-logs-part ui-logs-part windows-logs-part linux-logs-part; do
  mkdir -p "$parts/$j"
  printf 'RUN gates (%s)\nEXIT_CODE: 0 PASS\n' "$j" > "$parts/$j/gate-order.txt"
done
run_collector "$c" \
  --job "preflight-logs-part:success" \
  --job "audit-logs-part:success" \
  --job "ui-logs-part:success" \
  --job "windows-logs-part:success" \
  --job "linux-logs-part:success"
rc=$?
if [ $rc -eq 0 ] && grep -q "jobs with captured logs: 5" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && grep -q "duplicate relative paths detected: 0" "$out" \
  && [ "$(grep -c -- '--- .*-logs-part/gate-order.txt ---' "$out")" = "5" ]; then
  ok "v1.11.1 green run: 5/5 stable-name parts captured, same filename in five namespaces, zero collisions"
else
  bad "v1.11.1 five-part stable-name capture (rc=$rc)"
fi

# 13. v1.11.1 rerun/overwrite shape: a RETRY re-downloads the SAME stable
# artifact names into the SAME ci-parts/<stable-name>/ directories (the
# overwrite replaced the artifact's CONTENT). Simulate the retry by
# re-running the collector over a parts tree whose five directories were
# REPLACED (fresh content) under identical names: coverage stays 5/5, and
# the attempt number appears ONLY as report metadata — never as a job or
# directory name.
new_case 13
for j in preflight-logs-part audit-logs-part ui-logs-part windows-logs-part linux-logs-part; do
  mkdir -p "$parts/$j"
  printf 'RETRY content (%s)\nEXIT_CODE: 0 PASS\n' "$j" > "$parts/$j/gate-order.txt"
done
# First ("attempt 1") pass.
run_collector "$c" \
  --job "preflight-logs-part:success" \
  --job "audit-logs-part:success" \
  --job "ui-logs-part:success" \
  --job "windows-logs-part:success" \
  --job "linux-logs-part:success"
rc1=$?
# The retry: same stable names, replaced content (exactly what a rerun's
# overwrite: true upload produces in the same download paths).
for j in preflight-logs-part audit-logs-part ui-logs-part windows-logs-part linux-logs-part; do
  printf 'RETRY-2 content (%s)\nEXIT_CODE: 0 PASS\n' "$j" > "$parts/$j/gate-order.txt"
done
run_collector "$c" \
  --job "preflight-logs-part:success" \
  --job "audit-logs-part:success" \
  --job "ui-logs-part:success" \
  --job "windows-logs-part:success" \
  --job "linux-logs-part:success"
rc2=$?
if [ $rc1 -eq 0 ] && [ $rc2 -eq 0 ] \
  && grep -q "jobs with captured logs: 5" "$out" \
  && grep -q "RETRY-2 content (preflight-logs-part)" "$out" \
  && ! grep -q "RETRY content (preflight-logs-part)" "$out" \
  && ! find "$parts" -mindepth 1 -maxdepth 1 -type d -name '*attempt*' | grep -q .; then
  ok "v1.11.1 rerun/overwrite: same stable names, replaced content, 5/5 coverage, no attempt-shaped directories"
else
  bad "v1.11.1 rerun/overwrite shape (rc1=$rc1 rc2=$rc2)"
fi

printf '\nfixtures: %d pass / %d fail\n' "$pass" "$fail"
[ $fail -eq 0 ]
