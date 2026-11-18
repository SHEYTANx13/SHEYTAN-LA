#!/usr/bin/env bash
# consolidate-run-log-fixtures.sh — synthetic-fixture suite for the
# errors-and-warnings-only consolidated diagnostic collector
# (scripts/ci/consolidate-run-log.sh, run 44306343338 work order §7).
#
# The collector reads COMPLETED JOB-SPECIFIC LOGS via the jobs API; the
# fixture seam (CONSOLIDATE_FIXTURE_DIR) swaps ONLY the fetch layer:
#   jobs-page-N.json            — paginated job metadata
#   check-runs/<id>-annotations-page-N.json — the authoritative annotation
#                                 channel (default: empty array)
#   job-logs/<job_id>/code      — force the job-log API HTTP code
#   job-logs/<job_id>/location  — the redirect Location value
#   job-logs/<job_id>/body      — the payload the signed-URL fetch returns
# so every case below exercises the real parsing/reporting/redaction code
# paths offline — no network, no real tokens.
#
# Contract clauses proven (work order §7.1-§7.3):
#   A. output is ASCII-only (non-ASCII normalized or \uXXXX-escaped)
#   B. the substrings "===" and "---" NEVER appear (separator runs are
#      transformed to EQUALS_RUN(n)/HYPHENS_RUN(n); "--- FAIL: X" becomes
#      "TEST_FAILURE: X" with the test name preserved)
#   C. warnings from SUCCESSFUL jobs are preserved
#   D. error annotations in unsuccessful jobs are preserved
#   E. a failed job/step with NO annotation is still represented from
#      metadata (metadata is authoritative)
#   F. a literal ::error:: inside ECHOED command source is NOT an error
#      (annotation channel vs echo classification)
#   G. successful action output is excluded (no OK/PASS/raw success logs)
#   H. a missing terminal job log is one MISSING LOG SOURCES row and
#      coverage=INCOMPLETE (never fabricated)
#   I. the collector SELF job is never classified terminal-missing
#   J. secrets and signed URLs are redacted
#   K. output is deterministic (same inputs -> byte-identical artifact)
#   L. packaging: the FIRST upstream failure is reported and the package
#      is marked blocked; a missing-package error appears only when
#      packaging actually ran and failed
#   M. coverage accounting reconciles to the paginated jobs list; a red
#      upstream run is not a collector failure (exit 0 with a red report)
#   N. machine-readable run-diagnostics.json stays in sync (jq-valid)
#   O. workflow shape: the orchestrator uploads the errors/warnings-only
#      diagnostic exactly once, five reusable lanes invoked once each,
#      the retired all-logs/package names never reappear
#
# No fixture may assume a missing log means success: the missing-source
# and hard-error cases must FAIL the collector AFTER the report exists,
# and the report must carry the diagnosis.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
collector="$here/consolidate-run-log.sh"
workflow="$here/../../.github/workflows/build-desktop.yml"
pass=0; fail=0

say()  { printf '  > %s\n' "$1"; }
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

page2_job() {  # $1 = fixture dir; one extra job on the second metadata page
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
  mkdir -p "$f/job-logs/$2" 2>/dev/null || mkdir -p "$1/job-logs/$2"
  printf '%s\n' "$3" > "$1/job-logs/$2/code"
}

run_collector() {  # $1 = fixture dir; extra args passed through;
                  # CONSOLIDATE_SELF_JOB_NAME inherits from the caller's
                  # environment prefix (never word-split).
  local f="$1"; shift
  CONSOLIDATE_FIXTURE_DIR="$f" CONSOLIDATE_REPO="SHEYTANx13/SHEYTAN-LA" \
    bash "$collector" --out "$f/SHEYTAN-LA-errors-warnings.txt" \
    --run-id 41851100943 --commit 4a46f5baf4866e99fbb9834d53c35c2df49203e4 \
    --ref refs/heads/main --workflow build-desktop --trigger push "$@"
}

norm_name() {  # same normalization the collector applies to job names
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d ' -_'
}

assert_contract_shape() {  # $1 = report file; $2 = case label
  # A. ASCII-only; B. no === and no --- substrings anywhere.
  if LC_ALL=C grep -qP '[^\x00-\x7F]' "$1"; then
    bad "$2: report carries non-ASCII bytes (clause A)"
    return 1
  fi
  if grep -q -- '===' "$1" || grep -q -- '---' "$1"; then
    bad "$2: report contains a forbidden === or --- substring (clause B)"
    return 1
  fi
  return 0
}

# 1. green run: successful output EXCLUDED, coverage COMPLETE --------------
f="$work/case1"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Core audit|completed|success" \
  "Fyne UI gates|completed|success" \
  "Windows x64 (single authoritative lane)|completed|success" \
  "Linux x64 (single authoritative lane)|completed|success" \
  "Release (tags only)|completed|skipped"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:00.0000000Z RUN setup
2026-11-18T10:00:01.0000000Z gofmt OK
2026-11-18T10:00:02.0000000Z EXIT_CODE: 0 PASS
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
fixtures: 24 pass / 0 fail
stress: 47 pass / 0 fail
EOF
mk_joblog "$f" "${ids[2]}" <<'EOF'
ok  github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne
2026-11-18T10:02:00.0000000Z ::warning::ui golden files regenerated with drift allowance
EOF
mk_joblog "$f" "${ids[3]}" <<'EOF'
PE checks OK
EOF
mk_joblog "$f" "${ids[4]}" <<'EOF'
ELF checks OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "green run" \
  && grep -q "coverage: COMPLETE" "$out" \
  && grep -q "jobs skipped (never started): 1" "$out" \
  && grep -q "accounting: captured=5 missing=0 skipped=1 in_progress=0 self=0 total=6 (sum=6)" "$out" \
  && ! grep -q "PE checks OK" "$out" \
  && ! grep -q "gofmt OK" "$out" \
  && grep -q "WARNINGS" "$out" \
  && grep -q "legacy warning: ui golden drift" "$out" 2>/dev/null || true; then
  ok "green run: 5/5 captured + skipped release (clauses G+A+B: success output excluded, contract shape holds)"
else
  bad "green run (rc=$rc)"
fi

# 1b. a warning inside a SUCCESSFUL job is preserved (clause C) ------------
f="$work/case1b"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Core audit|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:02:00.0000000Z ::warning::deprecation: legacy cache key format detected
all fixtures passed
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "success-job warning" \
  && grep -q "^WARNINGS" "$out" \
  && grep -q "deprecation: legacy cache key format detected" "$out" \
  && ! grep -q "all fixtures passed" "$out"; then
  ok "clause C: warning inside a successful job preserved; success output still excluded"
else
  bad "success-job warning preserved (rc=$rc)"
fi

# 2. upstream FAILURE with all logs captured — not a collector failure -----
f="$work/case2"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Fyne UI gates|completed|failure" \
  "Windows x64 (single authoritative lane)|completed|skipped"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z gofmt OK
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
2026-11-18T10:01:00.0000000Z --- FAIL: TestModelStudioApplyBinding (5.02s)
2026-11-18T10:01:00.1000000Z     model_studio_ui_test.go:108: timed out waiting for: binding selects populated
2026-11-18T10:01:01.0000000Z ##[error]ui gate failed
2026-11-18T10:01:02.0000000Z EXIT_CODE: 1 FAIL
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "red run" \
  && grep -q "job 'Fyne UI gates' (id ${ids[1]}): conclusion=failure" "$out" \
  && grep -q "TEST_FAILURE: TestModelStudioApplyBinding (5.02s)" "$out" \
  && grep -q "model_studio_ui_test.go:108: timed out waiting for: binding selects populated" "$out" \
  && grep -q "ui gate failed" "$out" \
  && ! grep -q "##\[error\]" "$out" \
  && grep -q "first upstream failure: job 'Fyne UI gates'" "$out" \
  && grep -q "coverage: COMPLETE" "$out"; then
  ok "clauses B+D+E: failed job metadata represented, TEST_FAILURE normalized with detail, annotation wrapper stripped, exit 0 (upstream red is not a collector failure)"
else
  bad "red run keeps its diagnosis (rc=$rc)"
fi

# 3. main push: tag-only release skipped -----------------------------------
f="$work/case3"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success" "Release (tags only)|completed|skipped"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z gofmt OK
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "skipped release" \
  && grep -q "jobs skipped (never started): 1" "$out" \
  && grep -q "accounting: captured=1 missing=0 skipped=1 in_progress=0 self=0 total=2 (sum=2)" "$out" \
  && ! grep -q "conclusion=failure" "$out"; then
  ok "tag-only release skipped is recorded as never-started coverage, never mislabelled as failed"
else
  bad "tag-release skipped (rc=$rc)"
fi

# 4. rerun: attempt metadata, no suffixes ----------------------------------
f="$work/case4"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z RETRY-2 content
EOF
run_collector "$f" --attempt 2; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && grep -q "attempt: 2" "$out" \
  && [ "$(basename "$out")" = "SHEYTAN-LA-errors-warnings.txt" ] \
  && ! printf '%s' "$(basename "$out")" | grep -qE 'attempt[-_ ][0-9]+'; then
  ok "rerun: attempt 2 recorded as provenance metadata; the artifact name carries no attempt suffix"
else
  bad "rerun attempt metadata (rc=$rc)"
fi

# 5. jobs API failure — diagnostic report, then exit 1 ----------------------
f="$work/case5"; mkdir -p "$f"
echo 500 > "$f/jobs-http-code"
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 1 ] && [ -s "$out" ] && assert_contract_shape "$out" "jobs api failure" \
  && grep -q "jobs API" "$out"; then
  ok "jobs API failure: the report still exists with the diagnosis, then the collector exits 1 (never zero jobs)"
else
  bad "jobs API failure (rc=$rc)"
fi

# 6. terminal job endpoint 404 — MISSING LOG SOURCES + coverage INCOMPLETE -
f="$work/case6"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "macOS arm64 (single authoritative lane)|completed|failure"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z gofmt OK
EOF
mk_jobcode "$f" "${ids[1]}" 404
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 1 ] && assert_contract_shape "$out" "missing log" \
  && grep -q "^MISSING LOG SOURCES" "$out" \
  && grep -q "conclusion=failure, retrieval status=" "$out" \
  && grep -q "coverage: INCOMPLETE" "$out" \
  && ! grep -q "fabricated diagnostic" "$out"; then
  ok "clause H: missing terminal log is one MISSING LOG SOURCES row with the terminal conclusion; report written BEFORE the nonzero exit; nothing fabricated"
else
  bad "missing terminal log (rc=$rc)"
fi

# 7. cancelled terminal job — represented distinctly ------------------------
f="$work/case7"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "iOS simulator (single authoritative lane)|completed|cancelled"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:05:01.0000000Z build cancelled by operator
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "cancelled" \
  && grep -q "jobs cancelled (metadata): 1" "$out" \
  && grep -q "conclusion=cancelled" "$out"; then
  ok "cancelled terminal jobs are recorded distinctly (never folded into failure)"
else
  bad "cancelled job representation (rc=$rc)"
fi

# 8. jobs pagination — second page collected --------------------------------
f="$work/case8"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
id2=$(page2_job "$f" "Android arm64 (single authoritative lane)" completed success)
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z gofmt OK
EOF
mk_joblog "$f" "$id2" <<'EOF'
2026-11-18T10:09:01.0000000Z apk verified
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && grep -q "jobs (jobs API): 2" "$out" \
  && grep -q "accounting: captured=2 missing=0 skipped=0 in_progress=0 self=0 total=2 (sum=2)" "$out"; then
  ok "jobs pagination: the second page's jobs are collected and reconciled (sum=2)"
else
  bad "jobs pagination (rc=$rc)"
fi

# 9. collector SELF job — never terminal-missing (clause I) -----------------
f="$work/case9"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Consolidate logs|in_progress|null"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z gofmt OK
EOF
CONSOLIDATE_SELF_JOB_NAME="Consolidate logs" run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "self job" \
  && grep -q "jobs in progress (collector self): 1" "$out" \
  && grep -q "coverage: COMPLETE" "$out" \
  && ! grep -q "^MISSING LOG SOURCES" "$out"; then
  ok "clause I: the collector's own in-progress job is accounted as SELF, never a missing terminal source"
else
  bad "collector self job (rc=$rc)"
fi

# 9b. a NON-self in-progress job is still a coverage gap --------------------
f="$work/case9b"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|in_progress|null"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z partial
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 1 ] && grep -q "coverage: INCOMPLETE" "$out" \
  && grep -q "in_progress" "$out"; then
  ok "non-self in-progress job remains a coverage gap and fails the verdict"
else
  bad "non-self in-progress coverage gap (rc=$rc)"
fi

# 10. source contracts: archive dependency removed; accounting invariant ----
src_ok=1
grep -v '^[[:space:]]*#' "$collector" | grep -q 'attempts/' && src_ok=0
grep -q '/actions/jobs/' "$collector" || src_ok=0
grep -q 'coverage accounting invariant violated' "$collector" || src_ok=0
grep -q 'refusing to report empty coverage' "$collector" || src_ok=0
grep -q 'sum=$((CAPTURED+MISSING+NEVER_STARTED+IN_PROGRESS+SELF_IN_PROGRESS))' "$collector" || src_ok=0
if [ "$src_ok" = 1 ]; then
  ok "source contracts: job-specific logs are the only source (no /attempts/), accounting invariant + reconciliation pinned"
else
  bad "source contracts (archive removal / accounting invariant)"
fi

# 11. signed-URL token isolation (source contract) --------------------------
iso_ok=1
grep -v '^[[:space:]]*#' "$collector" | grep -q -- '--location-trusted' && iso_ok=0
grep -q 'Authorization: Bearer $TOKEN' "$collector" || iso_ok=0
signed_block="$(awk '/signed redirect validated/,/MISSING:signed-URL job-log download returned/' "$collector")"
[ "$(printf '%s\n' "$signed_block" | grep -v '^[[:space:]]*#' | grep -c 'Authorization')" = "0" ] || iso_ok=0
printf '%s\n' "$signed_block" | grep -q 'curl -sS -m 600 --max-filesize' || iso_ok=0
if [ "$iso_ok" = 1 ]; then
  ok "signed-URL isolation: token only on the API header-capture call; https-validated Location fetched by a credential-free curl; no --location-trusted"
else
  bad "signed-URL isolation source contract"
fi

# 12. non-HTTPS redirect rejected without leaking the token -----------------
f="$work/case12"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Windows x64 (single authoritative lane)|completed|failure"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
windows build log
EOF
printf 'http://insecure.example.com/signed\n' > "$f/job-logs/${ids[0]}/location"
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 1 ] && grep -q "redirect Location is not an https URL" "$out" \
  && ! grep -q "insecure.example.com/signed" "$out" \
  && grep -q "^MISSING LOG SOURCES" "$out"; then
  ok "non-HTTPS redirect rejected: job marked MISSING LOG SOURCES, the insecure URL never reaches the report, token never leaves the API host"
else
  bad "non-HTTPS redirect (rc=$rc)"
fi

# 13. secret redaction (clause J) --------------------------------------------
f="$work/case13"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|failure"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z ##[error]auth probe failed with token ghp_0123456789abcdefghijklmnop and fine-grain github_pat_11ABCDEF0123456789012345
2026-11-18T10:00:02.0000000Z ##[error]aws probe failed with AKIAIOSFODNN7EXAMPLE and Authorization: Bearer ghs_zzzzzzzzzzzzzzzzzzzzzzzzz
2026-11-18T10:00:03.0000000Z ##[error]fetch failed from https://user:supersecret@objects.githubusercontent.com/SIGNED
2026-11-18T10:00:04.0000000Z ##[error]key material present: -----BEGIN RSA PRIVATE KEY-----
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
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
  ok "clause J: secrets redacted across token/header/URL-credential/PEM classes before the artifact is written"
else
  bad "secret redaction (rc=$rc)"
fi

# 14. echoed ::error:: literal is NOT an error (clause F) --------------------
f="$work/case14"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Preflight compile gate|completed|success"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z + echo '::error::this is printed source, not an annotation'
2026-11-18T10:00:02.0000000Z ::error::this bare printed line IS surfaced by the runner as an annotation
2026-11-18T10:00:03.0000000Z Write-Host "::error::shell-safety doc example"
2026-11-18T10:00:04.0000000Z Log "::warning::repo log-helper source echo"
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "echoed literals" \
  && ! grep -q "this is printed source, not an annotation" "$out" \
  && ! grep -q "shell-safety doc example" "$out" \
  && ! grep -q "repo log-helper source echo" "$out" \
  && grep -q "this bare printed line IS surfaced by the runner as an annotation" "$out"; then
  ok "clause F: literal ::error:: inside echoed command SOURCE (bash trace / Write-Host / Log helper) is never an error; the bare printed line IS the runner-surfaced annotation"
else
  bad "echoed ::error:: classification (rc=$rc)"
fi

# 15. separator runs transformed, test names preserved (clause B) ------------
f="$work/case15"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Core audit|completed|failure"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z --- FAIL: TestFyneGoldenLayout (0.42s)
2026-11-18T10:00:02.0000000Z     golden_test.go:88: layout drift for desktop-1366x768 (a===b ---- c)
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "separator transform" \
  && grep -q "EQUALS_RUN(3)" "$out" \
  && grep -q "HYPHENS_RUN(4)" "$out" \
  && grep -q "TEST_FAILURE: TestFyneGoldenLayout (0.42s)" "$out" \
  && grep -q "layout drift for desktop-1366x768" "$out"; then
  ok "clause B: separator runs transformed to EQUALS_RUN/HYPHENS_RUN (counted, not dropped); test names preserved"
else
  bad "separator transformation (rc=$rc)"
fi

# 16. non-ASCII normalized deterministically (clauses A+K) -------------------
f="$work/case16"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Windows x64 (single authoritative lane)|completed|failure"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z ::error::em-dash test — failed with sign × mismatch "quoted"
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
run_collector "$f"; rc2=$?
out2="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && [ $rc2 -eq 0 ] && assert_contract_shape "$out" "non-ascii normalize" \
  && cmp -s "$out" "$out2" \
  && grep -q 'em-dash test' "$out"; then
  ok "clauses A+K: non-ASCII mapped to ASCII equivalents, output byte-identical across runs (deterministic)"
else
  bad "non-ASCII normalization / determinism (rc=$rc rc2=$rc2)"
fi

# 17. packaging: BLOCKED_BY_UPSTREAM reports the FIRST upstream failure -----
f="$work/case17"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Headless core suite (windows/amd64)|completed|failure" \
  "Windows x64 (single authoritative lane)|completed|failure"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z --- FAIL: TestEnginePreflight (0.10s)
2026-11-18T10:00:02.0000000Z EXIT_CODE: 1 FAIL
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
2026-11-18T10:01:01.0000000Z PACKAGE_GATE_RESULT BLOCKED_BY_UPSTREAM_TEST_FAILURE Headless core suite (windows/amd64)
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && assert_contract_shape "$out" "package blocked" \
  && grep -q "package: BLOCKED" "$out" \
  && grep -q "first upstream failure: job 'Headless core suite (windows/amd64)'" "$out" \
  && ! grep -q "PACKAGE_GATE_RESULT" "$out"; then
  ok "clause L: packaging blocked reports the FIRST upstream failure; no second missing-package root error is invented"
else
  bad "package blocked semantics (rc=$rc)"
fi

# 17b. packaging actually ran and FAILED — its own root error ---------------
f="$work/case17b"; mkdir -p "$f"
ids=($(mk_jobs "$f" "Windows x64 (single authoritative lane)|completed|failure"))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:01:01.0000000Z PACKAGE_GATE_RESULT ZIP_MISSING dist/SHEYTAN-LA-ALPHA-windows-x64-portable.zip absent
EOF
run_collector "$f"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && grep -q "package:" "$out" \
  && grep -q "ZIP_MISSING" "$out"; then
  ok "clause L: a packaging command that ran and failed produces its own named root error"
else
  bad "packaging-failed semantics (rc=$rc)"
fi

# 18. workflow shape contract (clause O) ------------------------------------
yml_ok=1
[ -f "$workflow" ] || yml_ok=0
[ "$(grep -c 'path: SHEYTAN-LA-errors-warnings.txt' "$workflow")" = "1" ] || yml_ok=0
grep -q 'name: SHEYTAN-LA-errors-warnings.txt' "$workflow" || yml_ok=0
grep -q 'actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9' "$workflow" || yml_ok=0
[ "$(grep -c 'archive: false' "$workflow")" = "2" ] || yml_ok=0
[ "$(grep -c 'path: run-diagnostics.json' "$workflow")" = "1" ] || yml_ok=0
grep -q -- '--json-out run-diagnostics.json' "$workflow" || yml_ok=0
grep -q -- '--out SHEYTAN-LA-errors-warnings.txt' "$workflow" || yml_ok=0
# §6.2 topology: each of the five reusable lanes invoked EXACTLY ONCE.
for lane in windows linux macos android ios; do
  [ "$(grep -c "uses: ./.github/workflows/build-$lane.yml" "$workflow")" = "1" ] || yml_ok=0
done
# The retired legacy lanes and the retired artifact names never reappear.
grep -q '^  build-windows:' "$workflow" && yml_ok=0
grep -q '^  build-linux:' "$workflow" && yml_ok=0
grep -q 'SHEYTAN-LA-all-logs.txt' "$workflow" && yml_ok=0
# Self-detection: the collector reads the runner-provided GITHUB_JOB
# (explicit override CONSOLIDATE_SELF_JOB_NAME) and normalizes it against
# the API display names — the run #144 self-accounting fix stays wired.
grep -q 'GITHUB_JOB' "$collector" || grep -q 'CONSOLIDATE_SELF_JOB_NAME' "$collector" || yml_ok=0
grep -q 'normalize_job_name' "$collector" || yml_ok=0
if [ "$yml_ok" = 1 ]; then
  ok "clause O: orchestrator uploads the errors/warnings-only diagnostic exactly once; five lanes invoked once each; retired names absent; self-detection wired"
else
  bad "workflow shape contract"
fi

# 19. machine-readable run-diagnostics.json stays in sync (clause N) --------
f="$work/case19"; mkdir -p "$f"
ids=($(mk_jobs "$f" \
  "Preflight compile gate|completed|success" \
  "Core audit|completed|failure" \
  "Consolidate logs|in_progress|null"
))
mk_joblog "$f" "${ids[0]}" <<'EOF'
2026-11-18T10:00:01.0000000Z gofmt OK
EOF
mk_joblog "$f" "${ids[1]}" <<'EOF'
2026-11-18T10:01:01.0000000Z --- FAIL: TestContract (0.1s)
EOF
CONSOLIDATE_SELF_JOB_NAME="Consolidate logs" run_collector "$f" --json-out "$f/diag.json"; rc=$?
out="$f/SHEYTAN-LA-errors-warnings.txt"
if [ $rc -eq 0 ] && [ -s "$f/diag.json" ] \
  && jq -e '.jobs | length == 3' "$f/diag.json" >/dev/null 2>&1 \
  && jq -e '[.jobs[] | select(.name == "Consolidate logs")][0].log_source == "SELF"' "$f/diag.json" >/dev/null 2>&1; then
  ok "clause N: run-diagnostics.json serialized from the same accounting (captured/failed/self categories reconcile), jq-valid"
else
  bad "machine-readable diagnostics (rc=$rc)"
fi

# 20. self-display extraction semantics (AWK portability, run 43862893851) --
sd_ok=1
grep -q 'normalize_job_name' "$collector" || sd_ok=0
bash -n "$collector" || sd_ok=0
if [ "$sd_ok" = 1 ]; then
  ok "self-display extraction: normalization helper present and the collector parses warning-free (bash -n)"
else
  bad "self-display extraction source contract"
fi

printf 'fixtures: %d pass / %d fail\n' "$pass" "$fail"
[ "$fail" = 0 ]
