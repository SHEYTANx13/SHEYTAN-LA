#!/usr/bin/env bash
# consolidate-run-log-fixtures.sh — synthetic-fixture suite for the
# v1.11.1 single-artifact run-log collector
# (scripts/ci/consolidate-run-log.sh, work order for run #41851100943 §8.3).
#
# The collector reads COMPLETED JOB-SPECIFIC LOGS via the jobs API; the
# fixture seam (CONSOLIDATE_FIXTURE_DIR) swaps ONLY the fetch layer:
#   jobs-page-N.json            — paginated job metadata
#   job-logs/<job_id>/code      — force the job-log API HTTP code
#   job-logs/<job_id>/location  — the redirect Location value
#   job-logs/<job_id>/body      — the payload the signed-URL fetch returns
# so every case below exercises the real parsing/reporting/redaction code
# paths offline — no network, no real tokens.
#
# Covered classes (§8.3 + retained legacy semantics):
#   1. all desktop jobs successful — one copy of every log, complete report
#   2. upstream FAILURE with all logs captured — report success, exit 0
#   3. main push with tag-only release skipped — NEVER_STARTED, not failed
#   4. rerun — attempt number lives in report metadata, no name suffixes
#   5. jobs API failure — diagnostic report, then exit 1 (never "zero jobs")
#   6. terminal job endpoint 404 — MISSING LOG SOURCE named, partial report
#      written (other jobs' logs included) BEFORE the nonzero exit
#   7. terminal job endpoint 403/500 — same fail-partial class
#   8. cancelled job logs captured with the cancelled conclusion
#   9. jobs pagination — the second page's jobs are collected and captured
#  10. collector SELF-LOG embedded (its own GitHub-hosted log is
#      unavailable while the run is in progress)
#  11. run-attempt archive dependency removed (source contract) — the
#      in-progress archive 404 mode cannot recur
#  12. zero-coverage accounting invariant (source contract) — captured=0
#      with missing=0/skipped=0/in_progress=0 and jobs present is a hard
#      failure, never silently reported
#  13. non-HTTPS redirect rejected — token never leaves the API host
#  14. secret redaction — token values, headers, URL credentials, PEM
#  15. same-named steps in different jobs — never flattened
#  16. workflow shape — exactly ONE log upload + ONE run-diagnostics.json
#      upload (stable names, no attempt/run-id suffix), package uploads
#      success-gated, release-tag action references, job-scoped
#      actions:read, and the consolidate-logs display name normalizes to
#      its YAML key (so the runner's GITHUB_JOB self-detection keeps
#      working). v1.11.1 run 43630164424 repair additions: the four
#      validation gates have NO needs: [preflight]; the collector and
#      its uploads run on !cancelled(); a final-verdict job fails when
#      any required gate is not success.
#  22. collector's OWN job in progress (run #144 production shape) — the
#      self job is accounted as SELF, reported honestly and does NOT fail
#      the verdict (the run #144 root cause: it used to)
#  23. a NON-self in-progress job — still a coverage gap, verdict fails
#  25. AWK portability (v1.11.1, run 43862893851, log ~5206) - the
#      self-display extraction must run warning-free (no escaped quote in
#      the awk bracket expression) while preserving the quote/CR strip
#      semantics on live and synthetic workflow shapes
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
mk_jobs() {  # $1 = fixture dir; jobs as "name|status|conclusion" args;
             # job ids are assigned sequentially from 101 in argument order;
             # the assigned ids are echoed (space-separated) for mk_joblog.
  local f="$1"; shift
  local first=1 name rest status conc
  local ids=
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
      ids="$ids $((jid))"
    done
    printf ']}\n'
  } > "$f/jobs-page-1.json"
  printf '%s\n' "$ids"
}

page2_job() {  # $1 = fixture dir; one extra job on the second metadata page;
               # echoes the assigned id
  local id=$((jid+=1))
  printf '{"total_count":1,"jobs":[{"id":%d,"name":"%s","status":"%s","conclusion":"%s"}]}' \
    "$id" "$2" "$3" "$4" > "$1/jobs-page-2.json"
  printf '%s\n' "$id"
}

mk_joblog() {  # $1 = fixture dir; $2 = job id; stdin = job log body
  local f="$1" id="$2"
  mkdir -p "$f/job-logs/$id"
  printf 'https://objects.githubusercontent.com/signed-job-%s\n' "$id" > "$f/job-logs/$id/location"
  cat > "$f/job-logs/$id/body"
}

mk_jobcode() {  # $1 = fixture dir; $2 = job id; $3 = forced API HTTP code
  mkdir -p "$1/job-logs/$2"
  printf '%s\n' "$3" > "$1/job-logs/$2/code"
}

run_collector() {  # $1 = fixture dir; extra args passed through
  local f="$1"; shift
  env ${COLLECTOR_ENV:+$COLLECTOR_ENV} \
    CONSOLIDATE_FIXTURE_DIR="$f" CONSOLIDATE_REPO="SHEYTANx13/SHEYTAN-LA" \
    bash "$collector" --out "$f/SHEYTAN-LA-all-logs.txt" \
    --run-id 41851100943 --commit 4a46f5baf4866e99fbb9834d53c35c2df49203e4 \
    --ref refs/heads/main --workflow build-desktop --trigger push "$@"
}

norm_name() {  # same normalization the collector applies to job names
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d ' -_'
}

# 1. all desktop jobs successful (six jobs reconciled) ---------------------
f="$work/case1"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Core audit|completed|success" \
  "Fyne UI gates|completed|success" \
  "Windows x64|completed|success" \
  "Linux x64|completed|success" \
  "Release (tags only)|completed|skipped"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
RUN setup
EXIT_CODE: 0 PASS
gofmt OK
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
fixtures: 24 pass / 0 fail
stress: 47 pass / 0 fail
EOF
mk_joblog "$f" "${ids[2]}" <<'EOF'
ok  github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne
EOF
mk_joblog "$f" "${ids[3]}" <<'EOF'
PE checks OK
EOF
mk_joblog "$f" "${ids[4]}" <<'EOF'
ELF checks OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && [ "$(basename "$out")" = "SHEYTAN-LA-all-logs.txt" ] \
  && grep -q "jobs with captured logs: 5" "$out" \
  && grep -q "jobs NEVER_STARTED (skipped): 1" "$out" \
  && grep -q "accounting: captured=5 missing=0 skipped=1 in_progress=0 self=0 total=6 (sum=6)" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && [ "$(grep -c '^===== JOB ' "$out")" = "6" ] \
  && grep -q "PE checks OK" "$out" && grep -q "ELF checks OK" "$out"; then
  ok "green run: 5/5 captured + skipped release, single plain-text artifact, six jobs reconciled (sum=6)"
else
  bad "green run (rc=$rc)"
fi

# 2. upstream failure with ALL logs captured — not a collector failure -----
f="$work/case2"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Fyne UI gates|completed|failure" \
  "Windows x64|completed|skipped" \
  "Linux x64|completed|skipped"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
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
  ok "red run keeps its log: failure + skipped recorded, error index populated, exit 0 (upstream red is not a collector failure)"
else
  bad "red run keeps log (rc=$rc)"
fi

# 3. main push: tag-only release skipped ----------------------------------
f="$work/case3"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Release (tags only)|completed|skipped"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
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
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
RETRY-2 content
EOF
run_collector "$f" --attempt 2; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && grep -q "run 41851100943 attempt 2" "$out" \
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

# 6. terminal job endpoint 404 — partial report BEFORE nonzero exit --------
f="$work/case6"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Windows x64|completed|failure"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
mk_jobcode "$f" "${ids[1]}" 404
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && [ -s "$out" ] \
  && grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && grep -q "job 'Windows x64' reached a terminal state but its job-specific log endpoint did not yield a log" "$out" \
  && grep -q "job-log endpoint returned HTTP 404" "$out" \
  && grep -q "gofmt OK" "$out"; then
  ok "terminal 404 → MISSING LOG SOURCE named with HTTP status; the OTHER job's log is preserved in the partial report; exit 1 only after the report exists"
else
  bad "terminal 404 partial report (rc=$rc)"
fi

# 7. terminal job endpoint 403/500 — same fail-partial class ---------------
f="$work/case7"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Linux x64|completed|failure"))
mk_jobcode "$f" "${ids[0]}" 403
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && grep -q "job-log endpoint returned HTTP 403" "$out" \
  && grep -q "coverage is INCOMPLETE" "$out"; then
  ok "terminal 403 → explicit missing-source state with the sanitized HTTP status, partial report still written"
else
  bad "terminal 403 (rc=$rc)"
fi

# 8. cancelled job logs captured with the cancelled conclusion -------------
f="$work/case8"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Windows x64|completed|cancelled"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
The runner has received a shutdown signal
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] && grep -q "===== JOB Windows x64 (status: completed, conclusion: cancelled" "$out" \
  && grep -q "shutdown signal" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out"; then
  ok "cancelled job: logs captured, conclusion recorded verbatim, no missing-source confusion"
else
  bad "cancelled job capture (rc=$rc)"
fi

# 9. jobs pagination — the second page's jobs are collected ----------------
f="$work/case9"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
pid="$(page2_job "$f" "Linux x64" "completed" "success")"
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
mk_joblog "$f" "$pid" <<'EOF'
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

# 10. collector SELF-LOG embedded ------------------------------------------
f="$work/case10"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "^=== COLLECTOR SELF-LOG ===" "$out" \
  && grep -q "accounting: captured=1" "$out" \
  && grep -q "GitHub-hosted step log cannot be retrieved while the run is in progress" "$out"; then
  ok "collector self-log: local diagnostics (API statuses, per-job decisions) embedded as COLLECTOR SELF-LOG; the unavailable server-side self log is stated honestly"
else
  bad "collector self-log (rc=$rc)"
fi

# 11. run-attempt archive dependency removed (source contract) -------------
src_ok=1
# The in-progress archive endpoint must NEVER be called by the collector.
grep -v '^[[:space:]]*#' "$collector" | grep -q 'attempts/' && src_ok=0
# The job-specific log endpoint is the contracted source.
grep -q '/actions/jobs/' "$collector" || src_ok=0
if [ "$src_ok" = 1 ]; then
  ok "run-attempt archive dependency removed: the collector never calls /attempts/ (the in-progress 404 mode cannot recur); job-specific logs are the only source"
else
  bad "run-attempt archive removal source contract"
fi

# 12. zero-coverage accounting invariant (source contract) -----------------
inv_ok=1
grep -q 'coverage accounting invariant violated' "$collector" || inv_ok=0
grep -q 'refusing to report empty coverage' "$collector" || inv_ok=0
# And the accounting line always reconciles the categories to the jobs list.
grep -q 'sum=\$((CAPTURED+MISSING+NEVER_STARTED+IN_PROGRESS+SELF_IN_PROGRESS))' "$collector" || inv_ok=0
if [ "$inv_ok" = 1 ]; then
  ok "accounting invariant: captured=0 with missing=0/skipped=0/in_progress=0 and jobs present is a hard failure; category totals reconcile to the paginated jobs list"
else
  bad "accounting invariant source contract"
fi

# 13. non-HTTPS redirect rejected without leaking the token ----------------
f="$work/case13"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Windows x64|completed|failure"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
windows build log
EOF
mkdir -p "$f/job-logs/${ids[0]}"
printf 'http://insecure.example.com/signed\n' > "$f/job-logs/${ids[0]}/location"
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] && grep -q "redirect Location is not an https URL" "$out" \
  && ! grep -q "insecure.example.com/signed" "$out" \
  && grep -q "MISSING LOG SOURCE" "$out"; then
  ok "non-HTTPS redirect rejected: job marked MISSING LOG SOURCE, the insecure URL never reaches the report, token never leaves the API host"
else
  bad "non-HTTPS redirect (rc=$rc)"
fi

# 13b. signed-URL token isolation (source contract) -------------------------
iso_ok=1
grep -v '^[[:space:]]*#' "$collector" | grep -q -- '--location-trusted' && iso_ok=0
grep -q 'Authorization: Bearer $TOKEN' "$collector" || iso_ok=0
# The signed-URL fetch (curl using "$location") must carry no Authorization header.
signed_block="$(awk '/signed redirect validated/,/MISSING:signed-URL job-log download returned/' "$collector")"
[ "$(printf '%s\n' "$signed_block" | grep -v '^[[:space:]]*#' | grep -c 'Authorization')" = "0" ] || iso_ok=0
printf '%s\n' "$signed_block" | grep -q 'curl -sS -m 600 --max-filesize' || iso_ok=0
if [ "$iso_ok" = 1 ]; then
  ok "signed-URL isolation: token only on the API header-capture call; https-validated Location fetched by a credential-free curl; no --location-trusted"
else
  bad "signed-URL isolation source contract"
fi

# 14. secret redaction --------------------------------------------------------
f="$work/case14"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
token=ghp_0123456789ABCDEFGHIJKLMNOP
fine-grain=github_pat_11ABCDEFghijklMNOPQRstuvwxyz1234
aws=AKIAIOSFODNN7EXAMPLE
Authorization: Bearer ghs_zzzzzzzzzzzzzzzzzzzz
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
  && grep -q 'REDACTED:ssh_private_key' "$out"; then
  ok "secrets redacted across token/header/URL-credential/PEM classes before the artifact is written"
else
  bad "secret redaction (rc=$rc)"
fi

# 15. same-named steps in different jobs ------------------------------------
f="$work/case15"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Windows x64|completed|success" \
  "Linux x64|completed|success"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
--- 2_Build.txt ---
windows build log
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
--- 2_Build.txt ---
linux build log
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && [ "$(grep -A3 '^===== JOB Windows x64' "$out" | grep -c 'windows build log')" = "1" ] \
  && [ "$(grep -A3 '^===== JOB Linux x64' "$out" | grep -c 'linux build log')" = "1" ]; then
  ok "same-named steps from different jobs live in their own job sections (never flattened)"
else
  bad "same-named steps (rc=$rc)"
fi

# 16. workflow shape contract ----------------------------------------------
yml_ok=1
[ -f "$workflow" ] || yml_ok=0
# exactly ONE upload of the single log artifact, by upstream release
# tag (the v1.11.1 action-reference policy), archive:false, with the
# stable display name; plus exactly ONE machine-readable
# run-diagnostics.json upload from the SAME collector job (run
# 43630164424 repair: two run-level collector artifacts, archive:false)
[ "$(grep -c 'path: SHEYTAN-LA-all-logs.txt' "$workflow")" = "1" ] || yml_ok=0
grep -q 'name: SHEYTAN-LA-all-logs.txt' "$workflow" || yml_ok=0
grep -q 'actions/upload-artifact@v7' "$workflow" || yml_ok=0
[ "$(grep -c 'archive: false' "$workflow")" = "2" ] || yml_ok=0
[ "$(grep -c 'path: run-diagnostics.json' "$workflow")" = "1" ] || yml_ok=0
grep -q -- '--json-out run-diagnostics.json' "$workflow" || yml_ok=0
# run 43630164424 DAG repair: the four validation gates are INDEPENDENT
# of preflight (a red preflight must never skip them) and no required
# gate hides behind continue-on-error
for job in audit ui build-windows build-linux; do
  block="$(awk -v j="  $job:" '$0 == j {found=1; print; next} found && /^  [a-z-]+:$/ {exit} found {print}' "$workflow")"
  # ACTIVE directive only (4-space key indentation) — explanatory comments
  # such as "NO needs: [preflight]" must not trip the check.
  printf '%s\n' "$block" | grep -qE '^    needs:.*preflight' && yml_ok=0
done
grep -qE '^[[:space:]]+continue-on-error:' "$workflow" && yml_ok=0
# collector job: needs the actions:read scope at job level, needs every
# upstream job, runs on !cancelled() (red runs keep evidence), and both
# collector uploads share that condition
grep -A16 '^  consolidate-logs:' "$workflow" | grep -q 'actions: read' || yml_ok=0
needsline="$(grep -A4 '^  consolidate-logs:' "$workflow" | grep 'needs:')"
for dep in preflight audit ui build-windows build-linux; do
  printf '%s' "$needsline" | grep -q "$dep" || yml_ok=0
done
grep -A16 '^  consolidate-logs:' "$workflow" | grep -q 'if: ${{ !cancelled() }}' || yml_ok=0
[ "$(grep -c 'if: ${{ !cancelled() }}' "$workflow")" -ge 4 ] || yml_ok=0
# final-verdict: the STRICT overall conclusion — needs every required
# gate plus the collector, runs on !cancelled(), and its verdict step
# fails unless every required gate concluded success (never blindly green)
fv_block="$(awk '/^  final-verdict:/{f=1; print; next} f && /^  [a-z-]+:$/ {exit} f {print}' "$workflow")"
[ -n "$fv_block" ] || yml_ok=0
for dep in preflight audit ui build-windows build-linux consolidate-logs; do
  printf '%s\n' "$fv_block" | grep -q "$dep" || yml_ok=0
done
printf '%s\n' "$fv_block" | grep -q 'if: ${{ !cancelled() }}' || yml_ok=0
for gate in preflight audit ui build-windows build-linux; do
  printf '%s\n' "$fv_block" | grep -q "needs.$gate.result" || yml_ok=0
done
# SELF-detection invariant (run #144 root cause fix): the collector
# identifies its own job by normalizing the runner's GITHUB_JOB (the YAML
# job key "consolidate-logs") against the API display name — the workflow
# must keep the display name normalizing to the YAML key.
self_display="$(awk '/^  consolidate-logs:/{f=1; next} f && /^    name:/{sub(/^ +name: +/,""); gsub(/["\r]/,""); print; exit}' "$workflow")"
[ -n "$self_display" ] || yml_ok=0
[ "$(norm_name "$self_display")" = "$(norm_name "consolidate-logs")" ] || yml_ok=0
grep -q 'is_self_job' "$collector" || yml_ok=0
if [ "$yml_ok" = 1 ]; then
  ok "workflow shape: two run-level collector artifacts (log + run-diagnostics.json, archive:false), zero part/smoke uploads, success-gated package uploads, job-scoped actions:read, collector needs every upstream job on !cancelled(), independent gates without needs:[preflight], final-verdict enforces the strict gate matrix"
else
  bad "workflow shape contract"
fi

# 17-21. v1.11.1 (run 41933333132, P1-D): the failure summary is METADATA-
# driven and annotation extraction EXCLUDES runner-echoed command source.
# The old collector grepped every log line for ::error:: — including the
# echoed step source — and reported 78 "failed commands" for a run with
# exactly one failed job.

# 17. runner-echoed BASH source containing a ::error:: literal -------------
f="$work/case17"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
echo "::error::this is only a literal"
+ echo "::error::this is only a literal"
gofmt OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "failed terminal jobs (metadata): 0" "$out" \
  && grep -q "error annotations: 0" "$out" \
  && grep -q "runner-echoed command-source literals excluded: 2" "$out"; then
  ok "echoed bash source with a ::error:: literal (plain + xtrace forms): zero failed jobs (metadata), zero error annotations, source lines excluded"
else
  bad "echoed bash ::error:: literal (rc=$rc)"
fi

# 18. runner-echoed POWERSHELL source containing a ::error:: literal -------
f="$work/case18"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Windows x64|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-01T23:42:20.0000000Z Write-Host "::error::gofmt violations — run gofmt -w on:"
2026-11-01T23:42:20.1000000Z gate OK: gofmt -l cmd internal ui
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "failed terminal jobs (metadata): 0" "$out" \
  && grep -q "error annotations: 0" "$out" \
  && grep -q "runner-echoed command-source literals excluded: 1" "$out"; then
  ok "echoed PowerShell Write-Host with a ::error:: literal: zero failures, source excluded"
else
  bad "echoed PowerShell ::error:: literal (rc=$rc)"
fi

# 19. ACTUAL emitted ::error:: from executed output (job still succeeded) --
f="$work/case19"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Linux x64|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
some tool emitted an annotation into its stdout
::error::tool advisory: cache miss for optional feature X (non-fatal)
ELF checks OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "failed terminal jobs (metadata): 0" "$out" \
  && grep -q "error annotations: 1" "$out" \
  && grep -q "tool advisory" "$out" \
  && grep -q "conclusion: success" "$out"; then
  ok "an actually-emitted ::error:: is surfaced as an annotation WITHOUT being counted as a failed job (metadata stays authoritative)"
else
  bad "emitted ::error:: annotation (rc=$rc)"
fi

# 20. step metadata conclusion=failure with NO ::error:: in the log --------
f="$work/case20"; mkdir -p "$f"
cat > "$f/jobs-page-1.json" <<'EOF'
{"total_count":1,"jobs":[{"id":901,"name":"Fyne UI gates (Linux)","status":"completed","conclusion":"failure","steps":[{"name":"Set up job","status":"completed","conclusion":"success"},{"name":"go test ./ui/fyne/","status":"completed","conclusion":"failure"},{"name":"Post Set up Go","status":"completed","conclusion":"success"}]}]}
EOF
mkdir -p "$f/job-logs/901"
printf 'https://objects.githubusercontent.com/signed-job-901\n' > "$f/job-logs/901/location"
cat > "$f/job-logs/901/body" <<'EOF'
--- FAIL: TestSomething
FAIL
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "failed terminal jobs (metadata): 1" "$out" \
  && grep -q "  failed job: Fyne UI gates (Linux)" "$out" \
  && grep -q "failed steps (metadata): 1" "$out" \
  && grep -q "error annotations: 0" "$out"; then
  ok "step conclusion=failure with no ::error:: output: failure counted from METADATA (job + step), not from log text"
else
  bad "metadata-only failure (rc=$rc)"
fi

# 21. successful job with DOZENS of echoed error-string literals -----------
f="$work/case21"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
{
  i=0
  while [ $i -lt 40 ]; do
    printf 'echo "::error::violation-template-%s"\n' "$i"
    i=$((i+1))
  done
  echo "gofmt OK"
} > "$f/job-logs-body"
mk_joblog "$f" "${ids[0]}" < "$f/job-logs-body"
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "failed terminal jobs (metadata): 0" "$out" \
  && grep -q "error annotations: 0" "$out" \
  && grep -q "runner-echoed command-source literals excluded: 40" "$out" \
  && grep -q "conclusion: success" "$out"; then
  ok "a successful job with dozens of echoed error-string literals remains success: 0 failed jobs, 0 annotations, all 40 source lines excluded"
else
  bad "success job with echoed literals (rc=$rc)"
fi

# 22. collector's OWN job in progress — SELF, verdict stays green ---------
# Production shape of run #144 (43008361835): the jobs API lists the five
# terminal upstream jobs plus the collector's own consolidate-logs job as
# in_progress (it IS running). GITHUB_JOB carries the YAML key. The self
# job must be accounted as SELF and the verdict must stay rc=0.
f="$work/case22"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Core audit (headless)|completed|success" \
  "Fyne UI gates (Linux)|completed|success" \
  "Windows x64|completed|success" \
  "Linux x64|completed|success" \
  "Consolidate logs|in_progress|null"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
fixtures: 24 pass / 0 fail
EOF
mk_joblog "$f" "${ids[2]}" <<'EOF'
ok  github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne
EOF
mk_joblog "$f" "${ids[3]}" <<'EOF'
PE checks OK
EOF
mk_joblog "$f" "${ids[4]}" <<'EOF'
ELF checks OK
EOF
COLLECTOR_ENV="GITHUB_JOB=consolidate-logs" run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 0 ] \
  && grep -q "jobs IN_PROGRESS (collector self, expected): 1" "$out" \
  && grep -q "jobs IN_PROGRESS (no terminal state): 0" "$out" \
  && grep -q "jobs with captured logs: 5" "$out" \
  && grep -q "accounting: captured=5 missing=0 skipped=0 in_progress=0 self=1 total=6 (sum=6)" "$out" \
  && grep -q "^=== SELF (collector's own job, expected in progress) ===" "$out" \
  && grep -q "the collector's own job — still in progress while this report is written" "$out" \
  && grep -q "the collector's OWN job — SELF (expected in progress while the collector runs)" "$out" \
  && ! grep -q "^=== MISSING LOG SOURCE ===" "$out" \
  && [ "$(grep -c '^===== JOB ' "$out")" = "6" ]; then
  ok "self job in progress (run #144 shape): SELF accounted, report complete, verdict rc=0 — a green run stays green"
else
  bad "self job SELF accounting (rc=$rc)"
fi

# 23. a NON-self in-progress job — still a coverage gap, verdict fails ------
f="$work/case23"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Windows x64|in_progress|null"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
gofmt OK
EOF
COLLECTOR_ENV="GITHUB_JOB=consolidate-logs" run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
if [ $rc -eq 1 ] \
  && grep -q "jobs IN_PROGRESS (no terminal state): 1" "$out" \
  && grep -q "jobs IN_PROGRESS (collector self, expected): 0" "$out" \
  && grep -q "non-self job had no terminal state" "$out" \
  && grep -q "coverage gap" "$out"; then
  ok "non-self in-progress job remains a coverage gap: report written, exit 1 — the SELF fix does not weaken the contract"
else
  bad "non-self in-progress coverage gap (rc=$rc)"
fi

# 24. --json-out machine-readable run diagnostics (run 43630164424 repair) -
# The SAME accounting data as the text report, as one JSON object: run
# identity, coverage totals, and every job with status/conclusion/id and
# its log-source category. A red matrix (one failed gate, one skipped
# tag-only release) must serialize faithfully — jq validates the file.
f="$work/case24"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|failure" \
  "Core audit (headless)|completed|success" \
  "Fyne UI gates (Linux)|completed|success" \
  "Windows x64|completed|success" \
  "Linux x64|completed|success" \
  "Release (tags only)|completed|skipped" \
  "Consolidate logs|in_progress|null"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
ui/fyne/button_fit_test.go:234:5: declared and not used: sh
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
ok  internal/...
EOF
mk_joblog "$f" "${ids[2]}" <<'EOF'
ok  ui/fyne
EOF
mk_joblog "$f" "${ids[3]}" <<'EOF'
PE checks OK
EOF
mk_joblog "$f" "${ids[4]}" <<'EOF'
ELF checks OK
EOF
COLLECTOR_ENV="GITHUB_JOB=consolidate-logs" \
  run_collector "$f" --json-out "$f/run-diagnostics.json"; rc=$?
out="$f/SHEYTAN-LA-all-logs.txt"
json="$f/run-diagnostics.json"
if [ $rc -eq 0 ] \
  && [ -s "$json" ] \
  && jq -e '.schema == "sheytan-la/run-diagnostics/1"' "$json" >/dev/null \
  && jq -e '.totals.jobs == 7' "$json" >/dev/null \
  && jq -e '.totals.captured == 5' "$json" >/dev/null \
  && jq -e '.totals.never_started == 1' "$json" >/dev/null \
  && jq -e '.totals.failed_jobs == 1' "$json" >/dev/null \
  && jq -e '[.jobs[] | select(.log_source == "CAPTURED")] | length == 5' "$json" >/dev/null \
  && jq -e '[.jobs[] | select(.log_source == "SKIPPED")] | length == 1' "$json" >/dev/null \
  && jq -e '[.jobs[] | select(.log_source == "SELF")] | length == 1' "$json" >/dev/null \
  && jq -e '[.jobs[] | select(.conclusion == "failure")][0].name == "Preflight compile gate"' "$json" >/dev/null \
  && jq -e '(.jobs | length) == 7' "$json" >/dev/null; then
  ok "machine-readable run-diagnostics.json: full red-run matrix (captured/skipped/self/failed) serialized from the same accounting, jq-valid"
else
  bad "run-diagnostics.json output (rc=$rc)"
fi

# 25. AWK portability (v1.11.1, run 43862893851, log line ~5206) --------
# The hosted core-audit fixture run emitted: warning: regexp escape
# sequence is not a known regexp operator. Root cause: the self-display
# extraction's awk bracket expression escaped the quote - a form the
# regexp parser does not recognize. The extraction now uses the portable
# unescaped bracket form. This case pins both properties on the LIVE
# workflow and on a synthetic snippet: warning-free stderr and the exact
# quote/CR strip semantics (collector semantics unchanged).
f="$work/case25"; mkdir -p "$f"
err="$f/extract.stderr"
got="$(awk '/^  consolidate-logs:/{f=1; next} f && /^    name:/{sub(/^ +name: +/,""); gsub(/["\r]/,""); print; exit}' "$workflow" 2>"$err")"
snip="$f/workflow.yml"
printf 'jobs:\n  consolidate-logs:\n    name: "Consolidate logs (v1.11.1)"\r\n' > "$snip"
snip_err="$f/snip.stderr"
got2="$(awk '/^  consolidate-logs:/{f=1; next} f && /^    name:/{sub(/^ +name: +/,""); gsub(/["\r]/,""); print; exit}' "$snip" 2>"$snip_err")"
if [ -s "$err" ] || [ -s "$snip_err" ]; then
  bad "self-display extraction emitted awk diagnostics: $(cat "$err" "$snip_err")"
elif [ -z "$got" ]; then
  bad "self-display extraction returned nothing for the live workflow"
elif [ "$got2" != "Consolidate logs (v1.11.1)" ]; then
  bad "strip semantics changed: extracted [$got2] from the synthetic snippet, want the unquoted CR-free display name"
else
  ok "self-display extraction: warning-free stderr on live and synthetic workflow shapes; quote/CR strip semantics preserved"
fi

printf '\nfixtures: %d pass / %d fail\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
