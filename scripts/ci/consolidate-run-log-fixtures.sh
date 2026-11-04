#!/usr/bin/env bash
# consolidate-run-log-fixtures.sh — synthetic-fixture suite for the
# v1.11.1 single-artifact run-log collector
# (scripts/ci/consolidate-run-log.sh, work order for run #41548925352 §6.3).
#
# The collector reads the GitHub run-attempt logs archive; the fixture
# seam (CONSOLIDATE_FIXTURE_DIR) swaps ONLY the fetch layer, so every
# case below exercises the real parsing/reporting/redaction code paths
# offline — no network, no real tokens.
#
# Covered classes (§6.3 + retained legacy semantics):
#   1. all desktop jobs successful — one copy of every log, complete report
#   2. UI/build failure with downstream skipped — report still complete
#   3. main push with tag-only release skipped — NEVER_STARTED, not failed
#   4. rerun — attempt number lives in report metadata, no name suffixes
#   5. jobs API failure — diagnostic report, then exit 1 (never "zero jobs")
#   6. corrupt / expired archive — diagnostic report, then exit 1
#   7. ran-and-failed job with no logs — MISSING LOG SOURCE names it, exit 1
#   8. same-named steps in different jobs — never flattened
#   9. secret redaction — token values, headers, URL credentials, PEM
#  10. signed-URL authorization-header isolation (source contract)
#  11. workflow shape — exactly ONE log upload, no part/smoke uploads,
#      package uploads success-gated, immutable SHA pin on the log upload
#  12. archive traversal entry (../evil.txt) — rejected, exit 1
#  13. cancelled job logs captured with the cancelled conclusion
#  14. jobs pagination — the second page's jobs are collected and captured
#
# No fixture may assume a missing log means success: the missing-source
# and hard-error cases must FAIL the collector AFTER the report exists,
# and the report must carry the diagnosis.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
collector="$here/consolidate-run-log.sh"
workflow="$here/../../.github/workflows/build-desktop.yml"
pass=0; fail=0

say()  { printf '  \xe2\x96\xb8 %s\n' "$1"; }
ok()   { pass=$((pass+1)); say "ok   $1"; }
bad()  { fail=$((fail+1)); say "FAIL $1"; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

jid=100
mk_jobs() {  # $1 = fixture dir; jobs as "name|status|conclusion" args
  local f="$1"; shift
  local first=1 name rest status conc
  {
    printf '{"total_count":%d,"jobs":[' "$#"
    for spec in "$@"; do
      name="${spec%%|*}"; rest="${spec#*|}"; status="${rest%%|*}"; conc="${rest#*|}"
      [ "$first" = 0 ] && printf ','
      if [ "$conc" = "null" ]; then
        printf '{"id":%d,"name":"%s","status":"%s","conclusion":null}' "$((jid+=1))" "$name" "$status"
      else
        printf '{"id":%d,"name":"%s","status":"%s","conclusion":"%s"}' "$((jid+=1))" "$name" "$status" "$conc"
      fi
      first=0
    done
    printf ']}\n'
  } > "$f/jobs-page-1.json"
}

mk_logs() {  # stdin: a ">>> dir/file" line starts a file; following lines
             # are its content, until the next ">>>" marker.
  local f="$1" spec="" dir line
  local stage="$f/stage"; rm -rf "$stage"; mkdir -p "$stage"
  while IFS= read -r line; do
    case "$line" in
      ">>>"*)
        spec="${line#\>\>\> }"
        dir="$(dirname "$stage/$spec")"
        mkdir -p "$dir"
        : > "$stage/$spec";;
      *)
        [ -n "$spec" ] && printf '%s\n' "$line" >> "$stage/$spec";;
    esac
  done
  ( cd "$stage" && zip -qr "$f/logs.zip" . )
  rm -rf "$stage"
}

run_collector() {  # $1 = fixture dir; extra args passed through
  local f="$1"; shift
  CONSOLIDATE_FIXTURE_DIR="$f" CONSOLIDATE_REPO="SHEYTANx13/SHEYTAN-LA" \
    bash "$collector" --out "$f/SHEYTAN-LA-all-logs.txt" \
    --run-id 41548925352 --commit 85d020d468af44ce473b10362b698f50b8052ef1 \
    --ref refs/heads/main --workflow build-desktop --trigger push "$@"
}

# 1. all desktop jobs successful ------------------------------------------
f="$work/case1"; mkdir -p "$f"
mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Core audit|completed|success" \
  "Fyne UI gates|completed|success" \
  "Windows x64|completed|success" \
  "Linux x64|completed|success" \
  "Release (tags only)|completed|skipped"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/0_Set up job.txt
RUN setup
EXIT_CODE: 0 PASS
>>> Preflight compile gate/2_gofmt (enforced).txt
gofmt OK
>>> Core audit/2_Collector fixtures.txt
fixtures: 14 pass / 0 fail
>>> Core audit/3_stress.txt
stress: 47 pass / 0 fail
>>> Fyne UI gates/2_go test ui fyne.txt
ok  github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne
>>> Windows x64/2_Build.txt
PE checks OK
>>> Linux x64/2_Build.txt
ELF checks OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && [ "$(basename "$out")" = "SHEYTAN-LA-all-logs.txt" ] \
  && grep -q "jobs with captured logs: 5" "$out" \
  && grep -q "jobs NEVER_STARTED (skipped): 1" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && [ "$(grep -c '^===== JOB ' "$out")" = "6" ] \
  && [ "$(grep -c -- '--- 2_Build.txt ---' "$out")" = "2" ]; then
  ok "green run: 5/5 captured + skipped release, single plain-text artifact, per-job Build steps kept apart"
else
  bad "green run (rc=$rc)"
fi

# 2. UI/build failure with downstream skipped ------------------------------
f="$work/case2"; mkdir -p "$f"
mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Fyne UI gates|completed|failure" \
  "Windows x64|completed|skipped" \
  "Linux x64|completed|skipped"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/2_gofmt (enforced).txt
gofmt OK
>>> Fyne UI gates/2_go test ui fyne.txt
--- FAIL: TestModelStudioApplyBinding (5.02s)
    model_studio_ui_test.go:108: timed out waiting for: binding selects populated
FAIL
EXIT_CODE: 1 FAIL
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && [ -s "$out" ] \
  && grep -q "conclusion: failure" "$out" \
  && grep -q "timed out waiting for: binding selects populated" "$out" \
  && grep -q "^=== ERROR INDEX ===" "$out" \
  && [ "$(sed -n '/^=== ERROR INDEX ===/,/^$/p' "$out" | grep -c .)" -gt 1 ] \
  && grep -q "jobs NEVER_STARTED (skipped): 2" "$out"; then
  ok "red run keeps its log: failure + skipped recorded, error index populated, exit 0 (coverage complete)"
else
  bad "red run keeps log (rc=$rc)"
fi

# 3. main push: tag-only release skipped ----------------------------------
f="$work/case3"; mkdir -p "$f"
mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Release (tags only)|completed|skipped"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/2_gofmt (enforced).txt
gofmt OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && grep -q "^=== NEVER_STARTED ===" "$out" \
  && grep -q "job 'Release (tags only)' was SKIPPED" "$out" \
  && grep -q "This is not a failed command" "$out"; then
  ok "tag-only release skipped is NEVER_STARTED, never mislabelled as failed"
else
  bad "tag-release skipped (rc=$rc)"
fi

# 4. rerun: attempt metadata, no suffixes ----------------------------------
f="$work/case4"; mkdir -p "$f"
mk_jobs "$f" "Preflight compile gate|completed|success"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/2_Build.txt
RETRY-2 content
EOF
run_collector "$f" --attempt 2; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && grep -q "run 41548925352 attempt 2" "$out" \
  && [ "$(basename "$out")" = "SHEYTAN-LA-all-logs.txt" ] \
  && ! printf '%s' "$(basename "$out")" | grep -qE 'attempt[-_ ][0-9]+'; then
  ok "rerun: attempt 2 recorded as provenance metadata; artifact name carries no attempt suffix"
else
  bad "rerun attempt metadata (rc=$rc)"
fi

# 5. jobs API failure -------------------------------------------------------
f="$work/case5"; mkdir -p "$f"
echo 500 > "$f/jobs-http-code"
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && [ -s "$out" ] \
  && grep -q "jobs listing failed: HTTP 500" "$out" \
  && grep -q "pagination/API error, never interpreted as zero jobs" "$out"; then
  ok "jobs API failure → diagnostic report written, exit 1, never zero-confused"
else
  bad "jobs API failure (rc=$rc)"
fi

# 6. corrupt archive --------------------------------------------------------
f="$work/case6"; mkdir -p "$f"
mk_jobs "$f" "Preflight compile gate|completed|success"
printf 'this is not a zip file at all\n' > "$f/logs.zip"
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && [ -s "$out" ] \
  && grep -q "failed the integrity test (unzip -t)" "$out"; then
  ok "corrupt/expired archive → diagnostic report, exit 1"
else
  bad "corrupt archive (rc=$rc)"
fi

# 7. ran-and-failed job with no logs in the archive ------------------------
f="$work/case7"; mkdir -p "$f"
mk_jobs "$f" \
  "Preflight compile gate|completed|failure" \
  "Windows x64|completed|skipped"
python3 - "$f/logs.zip" <<'PYEOF'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "w") as z:
    z.writestr("stray-top-level.txt", "not inside any job directory\n")
PYEOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && grep -q "job 'Preflight compile gate' ran but contributed no logs" "$out" \
  && grep -q "coverage is INCOMPLETE" "$out" \
  && grep -q "stray-top-level.txt" "$out"; then
  ok "ran-and-failed without logs → MISSING LOG SOURCE names the job; stray archive file reported, not dropped; exit 1 after the report exists"
else
  bad "missing log source (rc=$rc)"
fi

# 8. same-named steps in different jobs ------------------------------------
f="$work/case8"; mkdir -p "$f"
mk_jobs "$f" \
  "Windows x64|completed|success" \
  "Linux x64|completed|success"
mk_logs "$f" <<'EOF'
>>> Windows x64/2_Build.txt
windows build log
>>> Linux x64/2_Build.txt
linux build log
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && [ "$(grep -A2 '^===== JOB Windows x64' "$out" | grep -c 'windows build log')" = "1" ] \
  && [ "$(grep -A2 '^===== JOB Linux x64' "$out" | grep -c 'linux build log')" = "1" ]; then
  ok "same-named steps from different jobs live in their own job sections (never flattened)"
else
  bad "same-named steps (rc=$rc)"
fi

# 9. secret redaction --------------------------------------------------------
f="$work/case9"; mkdir -p "$f"
mk_jobs "$f" "Preflight compile gate|completed|success"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/2_leaky.txt
token=ghp_0123456789abcdefghijklmnop
fine-grain=github_pat_11ABCDEF012345678901234567890123456789012345678901234
aws=AKIAIOSFODNN7EXAMPLE
Authorization: Bearer ghs_zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz
url=https://user:supersecret@objects.githubusercontent.com/SIGNED
-----BEGIN RSA PRIVATE KEY-----
MIIBCgKCAQEAfake
-----END RSA PRIVATE KEY-----
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && ! grep -q 'ghp_0123456789' "$out" \
  && ! grep -q 'github_pat_11ABCDEF' "$out" \
  && ! grep -q 'AKIAIOSFODNN7EXAMPLE' "$out" \
  && ! grep -q 'ghs_zzzzzzzz' "$out" \
  && ! grep -q 'supersecret' "$out" \
  && ! grep -q 'BEGIN RSA PRIVATE KEY' "$out" \
  && grep -q 'REDACTED_GITHUB_TOKEN' "$out" \
  && grep -q 'REDACTED_AWS_KEY' "$out" \
  && grep -q 'REDACTED_CREDENTIALS@' "$out" \
  && grep -q 'REDACTED PRIVATE KEY' "$out"; then
  ok "secrets redacted across token/header/URL-credential/PEM classes before the artifact is written"
else
  bad "secret redaction (rc=$rc)"
fi

# 10. signed-URL authorization-header isolation (source contract) ----------
src_ok=1
grep -v '^[[:space:]]*#' "$collector" | grep -q -- '--location-trusted' && src_ok=0
grep -q 'Authorization: Bearer $TOKEN' "$collector" || src_ok=0
# The signed-URL fetch (curl using "$location") must carry no Authorization header.
iso_block="$(awk '/signed redirect validated/,/archive validation complete/' "$collector")"
[ "$(printf '%s\n' "$iso_block" | grep -v '^[[:space:]]*#' | grep -c 'Authorization')" = "0" ] || src_ok=0
printf '%s\n' "$iso_block" | grep -q 'curl -sS -m 600 --max-filesize' || src_ok=0
grep -q 'not an https URL' "$collector" || src_ok=0
if [ "$src_ok" = 1 ]; then
  ok "signed-URL isolation: token only on the API header-capture call; https-validated Location fetched by a credential-free curl; no --location-trusted"
else
  bad "signed-URL isolation source contract"
fi

# 11. workflow shape contract ----------------------------------------------
yml_ok=1
[ -f "$workflow" ] || yml_ok=0
# exactly ONE upload of the single log artifact, by immutable SHA, archive:false
[ "$(grep -c 'path: SHEYTAN-LA-all-logs.txt' "$workflow")" = "1" ] || yml_ok=0
grep -q 'actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9' "$workflow" || yml_ok=0
[ "$(grep -c 'archive: false' "$workflow")" = "1" ] || yml_ok=0
# no per-job log-part uploads and no smoke-output uploads remain
[ "$(grep -c 'name: .*-logs-part' "$workflow")" = "0" ] || yml_ok=0
[ "$(grep -c 'windows-smoke-app-output' "$workflow")" = "0" ] || yml_ok=0
[ "$(grep -c 'SHEYTAN-LA-all-logs-run-' "$workflow")" = "0" ] || yml_ok=0
# package uploads: success-gated (no if: always()) + error on missing files
for step in "Upload Windows package" "Upload Linux package"; do
  block="$(awk -v s="$step" 'index($0, "- name: " s) {found=1; print; next} found && /^  [^ ]/ {exit} found {print}' "$workflow")"
  printf '%s\n' "$block" | grep -q 'if: always()' && yml_ok=0
  printf '%s\n' "$block" | grep -q 'if-no-files-found: error' || yml_ok=0
done
# collector job needs the actions:read scope at job level
grep -A8 '^  consolidate-logs:' "$workflow" | grep -q 'actions: read' || yml_ok=0
if [ "$yml_ok" = 1 ]; then
  ok "workflow shape: one SHA-pinned plain-text log upload (archive:false), zero part/smoke uploads, success-gated package uploads, job-scoped actions:read"
else
  bad "workflow shape contract"
fi

# 12. archive traversal entry rejected -------------------------------------
f="$work/case12"; mkdir -p "$f"
mk_jobs "$f" "Preflight compile gate|completed|success"
python3 - "$f/logs.zip" <<'PYEOF'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "w") as z:
    z.writestr("../evil.txt", "traversal payload")
    z.writestr("Preflight compile gate/0_Set up job.txt", "RUN setup\n")
PYEOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && grep -q "unsafe entry paths (absolute/traversal)" "$out" \
  && ! grep -q "traversal payload" "$out"; then
  ok "traversal entry (../evil.txt) rejected before extraction; payload never reaches the report"
else
  bad "traversal entry rejection (rc=$rc)"
fi

# 13. cancelled job logs captured with the cancelled conclusion -------------
f="$work/case13"; mkdir -p "$f"
mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Windows x64|completed|cancelled"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/2_gofmt (enforced).txt
gofmt OK
>>> Windows x64/1_Cancel.txt
The runner has received a shutdown signal
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && grep -q "===== JOB Windows x64 (status: completed, conclusion: cancelled)" "$out" \
  && grep -q "shutdown signal" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out"; then
  ok "cancelled job: logs captured, conclusion recorded verbatim, no missing-source confusion"
else
  bad "cancelled job capture (rc=$rc)"
fi

# 14. jobs pagination — the second page's jobs are collected ----------------
f="$work/case14"; mkdir -p "$f"
mk_jobs "$f" "Preflight compile gate|completed|success"
printf '{"total_count":1,"jobs":[{"id":900,"name":"Linux x64","status":"completed","conclusion":"success"}]}' \
  > "$f/jobs-page-2.json"
mk_logs "$f" <<'EOF'
>>> Preflight compile gate/2_gofmt (enforced).txt
gofmt OK
>>> Linux x64/2_Build.txt
ELF checks OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && grep -q "jobs with captured logs: 2" "$out" \
  && grep -q "expected jobs (jobs API): 2" "$out" \
  && grep -q "ELF checks OK" "$out"; then
  ok "pagination: the second page's jobs are collected and captured (2/2)"
else
  bad "pagination second page (rc=$rc)"
fi

printf '\nfixtures: %d pass / %d fail\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
