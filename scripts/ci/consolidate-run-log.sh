#!/usr/bin/env bash
# consolidate-run-log.sh — build the ONE consolidated plain-text run log
# for the build-desktop workflow, collected from COMPLETED JOB-SPECIFIC
# LOGS via the GitHub jobs API (v1.11.1 single-artifact contract, work
# order for run #41851100943 §3.2/§8):
#
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}/jobs?per_page=100&page=N
#   GET /repos/{owner}/{repo}/actions/jobs/{job_id}/logs
#
# WHY NOT THE RUN-ATTEMPT ARCHIVE: GitHub may return HTTP 404 for
# /runs/{id}/attempts/{n}/logs while the run is still in progress — and
# the collector itself runs INSIDE that very run. The archive can never
# contain this collector's own still-running step log either. The
# collector therefore depends ONLY on job-specific logs of upstream jobs
# that have already reached a terminal state (the consolidate-logs job
# `needs:` every upstream job and runs with if: always()), and embeds its
# own progress as a local COLLECTOR SELF-LOG. It NEVER calls the
# run-attempt archive endpoint. (Post-run operators may still use the
# archive externally; it is not this script's runtime dependency.)
#
# Contract:
#   - Output: exactly ONE plain-text file (default SHEYTAN-LA-all-logs.txt).
#     The workflow uploads it with upload-artifact archive:false and
#     name: SHEYTAN-LA-all-logs.txt, so the artifact downloads as the
#     file itself and the display name equals the file name.
#   - Every job from the paginated jobs API appears in EXACTLY ONE
#     accounting category, and the categories reconcile to the jobs list:
#       CAPTURED (terminal job, full log retrieved via jobs/{id}/logs),
#       SKIPPED/NEVER_STARTED (conclusion "skipped" — no log exists),
#       COLLECTOR SELF-LOG (this collector's local diagnostics),
#       IN_PROGRESS (no terminal state yet — only where topology permits),
#       MISSING LOG SOURCE (terminal job whose log endpoint failed).
#   - Invariant: a jobs list with at least one job but zero CAPTURED,
#     zero MISSING, zero SKIPPED and zero IN_PROGRESS is impossible and
#     is treated as a collector integrity failure.
#   - A red or cancelled upstream run is NOT a collector failure: as long
#     as every terminal job contributed its log, the collector exits 0
#     and the report says what failed upstream.
#   - A terminal job whose log endpoint genuinely fails is a MISSING LOG
#     SOURCE: the partial report is still written (and uploaded by the
#     workflow's if: always() step) and only then the collector exits 1.
#   - Job boundaries are preserved: one "===== JOB … =====" section per
#     job with its exact status/conclusion and job id; no flattening.
#   - Signed-URL token isolation: the bearer token is sent ONLY to the
#     API host on the job-log header-capture request. The redirect's
#     Location must be an https URL; the signed URL is fetched by a
#     SEPARATE curl invocation carrying NO Authorization header.
#     --location-trusted is never used. Signed URLs are never written
#     into the report.
#   - Secret redaction is applied to the whole report: the caller's own
#     token value, ghp_/gho_/ghs_/github_pat_ tokens, AWS access-key ids,
#     PEM private-key headers, Authorization header values and
#     user:password@ URL credentials are REDACTED before the file is
#     written. Environment variables are never printed.
#   - Fail closed: any jobs-API, pagination, accounting-invariant or
#     missing-log-source condition produces the best-available report
#     FIRST and exits nonzero AFTER — a red run keeps its log.
#   - Collector self-diagnostics: the collector's own progress log (API
#     statuses, per-job decisions) is embedded in the report under
#     COLLECTOR SELF-LOG. The GitHub-hosted copy of this job's own step
#     log is unavailable while the run is in progress; the report says so.
#
# Test seam (offline fixture suite, no network): set CONSOLIDATE_FIXTURE_DIR
# to a directory containing run.json (optional), jobs-page-N.json pages,
# and a job-logs/<job_id>/ directory per job with:
#   code      (optional) HTTP code the API call returns (e.g. 404, 403);
#   location  (optional) the redirect Location value (default: a valid
#             https signed-URL placeholder);
#   body      (optional) the payload the signed-URL fetch returns.
# The fetch layer is swapped; all parsing/reporting code paths are the
# production ones.
set -uo pipefail

usage() { echo "usage: $0 --out FILE [--repo OWNER/REPO] [--run-id ID] [--attempt N] [--commit SHA] [--ref REF] [--workflow NAME] [--trigger EVENT]" >&2; exit 64; }

OUT="SHEYTAN-LA-all-logs.txt"
REPO="${CONSOLIDATE_REPO:-${GITHUB_REPOSITORY:-}}"
RUN_ID="${CONSOLIDATE_RUN_ID:-${GITHUB_RUN_ID:-}}"
ATTEMPT="${CONSOLIDATE_ATTEMPT:-${GITHUB_RUN_ATTEMPT:-1}}"
COMMIT="${CONSOLIDATE_COMMIT:-${GITHUB_SHA:-}}"
REF="${CONSOLIDATE_REF:-${GITHUB_REF:-}}"
WORKFLOW="${CONSOLIDATE_WORKFLOW:-${GITHUB_WORKFLOW:-build-desktop}}"
TRIGGER="${CONSOLIDATE_TRIGGER:-${GITHUB_EVENT_NAME:-}}"
API="${CONSOLIDATE_API_BASE:-https://api.github.com}"
TOKEN="${GH_TOKEN:-}"
FIXTURE_DIR="${CONSOLIDATE_FIXTURE_DIR:-}"
JOBLOG_MAX_BYTES=268435456      # 256 MiB cap per job log

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2;;
    --repo) REPO="$2"; shift 2;;
    --run-id) RUN_ID="$2"; shift 2;;
    --attempt) ATTEMPT="$2"; shift 2;;
    --commit) COMMIT="$2"; shift 2;;
    --ref) REF="$2"; shift 2;;
    --workflow) WORKFLOW="$2"; shift 2;;
    --trigger) TRIGGER="$2"; shift 2;;
    *) usage;;
  esac
done
[ -n "$OUT" ] && [ -n "$REPO" ] && [ -n "$RUN_ID" ] || usage

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
DIAG="$WORK/collector-diagnostics.log"
RAW="$WORK/report-raw.txt"

diag() { echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*" >>"$DIAG"; }

# --- controlled exit: report first, then the verdict -------------------
HARD_ERROR=0; CAPTURED=0; MISSING=0; NEVER_STARTED=0; IN_PROGRESS=0
# v1.11.1 (run 41933333132, P1-D): failures come from METADATA, not from
# grepping log text. The old "files/lines with failed commands" count
# grepped every log line for ::error:: — including the runner's ECHOED
# step source (a script line `echo "::error::gofmt violations"` is
# source code, not an executed failure) — and reported 78 failed
# commands for a run whose jobs metadata showed exactly ONE failed job.
hard_fail() { diag "HARD ERROR: $*"; HARD_ERROR=$((HARD_ERROR+1)); }

# --- redaction ----------------------------------------------------------
redact_stream() {  # $1 = raw file; writes redacted content to stdout
  local esc=""
  if [ -n "$TOKEN" ]; then
    esc="$(printf '%s' "$TOKEN" | sed -e 's/[][\/.*^$]/\\&/g')"
  fi
  sed -E \
    ${esc:+-e "s/${esc}/REDACTED_TOKEN/g"} \
    -e 's/gh[pousr]_[A-Za-z0-9]{16,}/REDACTED_GITHUB_TOKEN/g' \
    -e 's/github_pat_[A-Za-z0-9_]{20,}/REDACTED_GITHUB_TOKEN/g' \
    -e 's/AKIA[0-9A-Z]{16}/REDACTED_AWS_KEY/g' \
    -e 's/([Aa]uthorization:[[:space:]]*).*/\1REDACTED/' \
    -e 's#(https?://)[^/:[:space:]]+:[^@[:space:]]+@#\1REDACTED_CREDENTIALS@#g' \
    -e 's/^-----BEGIN [A-Z ]*PRIVATE KEY-----/[REDACTED:ssh_private_key]/' \
    "$1"
}

# --- fetch layer (network or fixture seam) ------------------------------
fetch_json() {  # $1 = api path, $2 = outfile; echoes HTTP code
  local path="$1" outfile="$2" code=""
  if [ -n "$FIXTURE_DIR" ]; then
    case "$path" in
      *"/jobs"*)
        if [ -f "$FIXTURE_DIR/jobs-http-code" ]; then
          cat "$FIXTURE_DIR/jobs-http-code"; return 0
        fi
        if [ -f "$FIXTURE_DIR/jobs-page-$FETCH_PAGE.json" ]; then
          cp "$FIXTURE_DIR/jobs-page-$FETCH_PAGE.json" "$outfile"; echo "200"
        else
          # Beyond the last page the API answers 200 with an empty array —
          # the fixture seam mirrors that; a missing page is not an error.
          printf '{"total_count":0,"jobs":[]}' > "$outfile"; echo "200"
        fi;;
      *)
        if [ -f "$FIXTURE_DIR/run.json" ]; then
          cp "$FIXTURE_DIR/run.json" "$outfile"; echo "200"
        else
          echo "404"
        fi;;
    esac
    return 0
  fi
  code="$(curl -sS -m 60 -o "$outfile" -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "$API$path")" || { echo "000"; return 0; }
  echo "$code"
}
FETCH_PAGE=1

# --- 0. token presence (production only) --------------------------------
if [ -z "$FIXTURE_DIR" ] && [ -z "$TOKEN" ]; then
  hard_fail "GH_TOKEN is empty — the job-logs API cannot be called without it"
fi

# --- 1. run metadata (best effort provenance) ---------------------------
RUN_NAME=""; META_OK=0
if [ -n "$FIXTURE_DIR" ] && [ -f "$FIXTURE_DIR/run.json" ]; then
  RUN_NAME="$(timeout 10 jq -r '.name // ""' "$FIXTURE_DIR/run.json" 2>/dev/null || true)"
  META_OK=1
  diag "run metadata loaded from fixture"
elif [ -n "$FIXTURE_DIR" ]; then
  diag "run metadata unavailable (no run.json in fixture) — provenance falls back to collector arguments"
else
  code="$(fetch_json "/repos/$REPO/actions/runs/$RUN_ID" "$WORK/run.json")"
  if [ "$code" = "200" ] && timeout 10 jq -e '.id' "$WORK/run.json" >/dev/null 2>&1; then
    RUN_NAME="$(timeout 10 jq -r '.name // ""' "$WORK/run.json")"
    META_OK=1
    diag "run metadata fetched (HTTP 200)"
  else
    diag "run metadata unavailable (HTTP $code) — provenance falls back to collector arguments"
  fi
fi
[ "$META_OK" = "1" ] && [ "$RUN_NAME" != "null" ] || RUN_NAME=""

# --- 2. jobs metadata with pagination (REQUIRED) ------------------------
: > "$WORK/jobs.ndjson"
page=1; pages=0
while [ "$page" -le 50 ]; do
  FETCH_PAGE="$page"
  code="$(fetch_json "/repos/$REPO/actions/runs/$RUN_ID/jobs?per_page=100&page=$page" "$WORK/jobs-$page.json")"
  if [ "$code" != "200" ]; then
    hard_fail "jobs listing failed: HTTP $code on page $page (/repos/$REPO/actions/runs/$RUN_ID/jobs) — pagination/API error, never interpreted as zero jobs"
    break
  fi
  if ! timeout 10 jq -e '.jobs | type == "array"' "$WORK/jobs-$page.json" >/dev/null 2>&1; then
    hard_fail "jobs listing returned a non-JSON or unexpected body on page $page (HTTP $code)"
    break
  fi
  n="$(timeout 10 jq -r '.jobs | length' "$WORK/jobs-$page.json")"
  # v1.11.1 (P1-D): carry the per-job STEPS (name + conclusion) from the
  # jobs API response — the authoritative per-step failure facts the
  # summary reports. Jobs without a steps array normalize to [] .
  timeout 10 jq -c '.jobs[] | {id, name, status, conclusion, steps: [(.steps // [])[] | {name, conclusion}]}' "$WORK/jobs-$page.json" >> "$WORK/jobs.ndjson" 2>>"$DIAG" \
    || hard_fail "failed to normalize jobs metadata on page $page"
  pages=$((pages+1))
  [ "$n" -eq 0 ] && break
  page=$((page+1))
done
TOTAL_JOBS="$(wc -l < "$WORK/jobs.ndjson")"
diag "jobs metadata: $pages page(s) fetched, $TOTAL_JOBS job(s)"

# --- 3. per-job logs — the ONLY log source (REQUIRED) --------------------
# Terminal job  -> GET /actions/jobs/{id}/logs (302 + signed URL, no
#                  credentials on the signed fetch).
# skipped       -> NEVER_STARTED: no fetch, no log, never "missing".
# not terminal  -> IN_PROGRESS: represented explicitly, not a missing log.
# endpoint fail -> MISSING LOG SOURCE for that job; everything else is
#                  still captured and the report is still written.
capture_job_log() {  # $1 = job id; on success writes $2 = log file; echoes "" on success, "MISSING:<reason>" otherwise
  local jid="$1" outfile="$2" d="" code="" location="" bcode="" body=""
  if [ -n "$FIXTURE_DIR" ]; then
    d="$FIXTURE_DIR/job-logs/$jid"
    if [ -f "$d/code" ]; then
      code="$(cat "$d/code")"
    elif [ -d "$d" ]; then
      code="302"
    else
      echo "MISSING:no fixture seam entry for job $jid"
      return 0
    fi
    if [ "$code" != "302" ] && [ "$code" != "200" ]; then
      echo "MISSING:job-log endpoint returned HTTP $code"
      return 0
    fi
    location="https://objects.githubusercontent.com/signed-job-$jid"
    if [ -f "$d/location" ]; then location="$(cat "$d/location")"; fi
    if [ "${location#https://}" = "$location" ]; then
      echo "MISSING:redirect Location is not an https URL — refusing to follow (token never leaves the API host)"
      return 0
    fi
    if [ "$code" = "302" ]; then
      body="$d/body"
      if [ ! -f "$body" ]; then
        echo "MISSING:signed-URL fetch returned no payload (fixture)"
        return 0
      fi
      cp "$body" "$outfile"
    else
      # Direct 200: the seam serves the payload the same way.
      body="$d/body"
      if [ -f "$body" ]; then cp "$body" "$outfile"; else : > "$outfile"; fi
      [ -s "$outfile" ] || { echo "MISSING:direct job-log response is empty"; return 0; }
    fi
  else
    headers="$WORK/headers-$jid.txt"
    code="$(curl -sS -m 60 -D "$headers" -o /dev/null -w '%{http_code}' \
      -H "Authorization: Bearer $TOKEN" \
      -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      "$API/repos/$REPO/actions/jobs/$jid/logs")" \
      || { echo "MISSING:job-log request failed (curl transport error)"; return 0; }
    if [ "$code" != "302" ] && [ "$code" != "200" ]; then
      echo "MISSING:job-log endpoint returned HTTP $code"
      return 0
    fi
    location="$(awk 'tolower($1)=="location:" {sub(/^[^:]*:[ \t]*/,""); gsub(/\r$/,""); print; exit}' "$headers")"
    if [ -z "$location" ]; then
      echo "MISSING:job-log redirect did not carry a Location header"
      return 0
    fi
    if [ "${location#https://}" = "$location" ]; then
      echo "MISSING:redirect Location is not an https URL — refusing to follow (token never leaves the API host)"
      return 0
    fi
    diag "signed redirect validated (https); fetching job log WITHOUT credentials"
    bcode="$(curl -sS -m 600 --max-filesize "$JOBLOG_MAX_BYTES" -o "$outfile" \
      -w '%{http_code}' "$location")" \
      || { echo "MISSING:signed-URL job-log download failed (curl transport error or size cap exceeded)"; return 0; }
    [ "$bcode" = "200" ] || { echo "MISSING:signed-URL job-log download returned HTTP $bcode"; return 0; }
    [ -s "$outfile" ] || { echo "MISSING:downloaded job log is empty"; return 0; }
  fi
  return 0
}

# --- 4. report ------------------------------------------------------------
now_utc="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
ERRIDX="$WORK/erridx.txt"; WARNIDX="$WORK/warnidx.txt"
: > "$ERRIDX"; : > "$WARNIDX"
NEVER_LIST="$WORK/never.txt"; MISSING_LIST="$WORK/missing.txt"
: > "$NEVER_LIST"; : > "$MISSING_LIST"

# v1.11.1 (P1-D) two labeled data sources, never mixed again:
#   AUTHORITATIVE — job/step conclusions from the paginated jobs API
#   (metadata; the only thing that can call a job failed).
#   ANNOTATIONS   — runner annotation tokens found in the captured log
#   OUTPUT, EXCLUDING runner-echoed command SOURCE lines (the literal
#   `echo "::error::…"` / `Write-Host "::error::…"` statements the
#   runner prints when echoing the step's script). Raw log lines are
#   never discarded — only the indexes are classified.
FAILED_JOBS=0; FAILED_JOB_LIST=""; STEP_FAILURES=0
ANN_ERRORS=0; ANN_WARNINGS=0; ECHOED_SOURCE=0

is_echoed_annotation_source() {  # $1 = one log line (no line-number prefix)
  # A command SOURCE echo whose string LITERAL contains the annotation
  # token: bash `echo/printf`, PowerShell `Write-*`, and the repo's Log
  # helper. An ACTUAL emitted annotation is command OUTPUT — the token
  # is not a quoted literal after a print command on the same line.
  printf '%s\n' "$1" | grep -qE "(^|[;&|[:space:]+(-])(echo|printf|Write-Host|Write-Output|Write-Warning|Write-Error|Log)[[:space:]].*[\"'](::error::|##\\[error\\]|::warning::|##\\[warning\\])"
}

classify_job_log_annotations() {  # $1 = job name; $2 = captured log file
  local jname="$1" sec="$2" hit content
  while IFS= read -r hit; do
    [ -z "$hit" ] && continue
    content="${hit#*:}"   # strip grep's line-number prefix
    case "$content" in
      *"::error::"*|*"##[error]"*|*"EXIT_CODE: "[1-9]*" FAIL"*)
        if is_echoed_annotation_source "$content"; then
          ECHOED_SOURCE=$((ECHOED_SOURCE+1))
          continue
        fi
        ANN_ERRORS=$((ANN_ERRORS+1))
        printf 'job %s: %s\n' "$jname" "${hit:0:240}" >> "$ERRIDX";;
      *"::warning::"*|*"##[warning]"*)
        if is_echoed_annotation_source "$content"; then
          ECHOED_SOURCE=$((ECHOED_SOURCE+1))
          continue
        fi
        ANN_WARNINGS=$((ANN_WARNINGS+1))
        printf 'job %s: %s\n' "$jname" "${hit:0:240}" >> "$WARNIDX";;
    esac
  done < <(grep -nE '::error::|##\[(error|warning)\]|::warning::|EXIT_CODE: [1-9][0-9]* FAIL' "$sec" 2>/dev/null | cut -c1-260 || true)
}

# Job sections are emitted in jobs-API order, one per job, exactly once.
declare -a JOB_SECTIONS=()

if [ -s "$WORK/jobs.ndjson" ]; then
  while IFS= read -r job; do
    jid="$(printf '%s' "$job" | jq -r '.id')"
    jname="$(printf '%s' "$job" | jq -r '.name')"
    jstatus="$(printf '%s' "$job" | jq -r '.status')"
    jconc="$(printf '%s' "$job" | jq -r '.conclusion // "null"')"

    # AUTHORITATIVE facts (P1-D): failed terminal jobs and failed steps
    # come from the metadata, never from log text.
    if [ "$jconc" = "failure" ]; then
      FAILED_JOBS=$((FAILED_JOBS+1))
      FAILED_JOB_LIST="${FAILED_JOB_LIST}${jname}\n"
    fi
    sfail="$(printf '%s' "$job" | timeout 10 jq -r '[(.steps // [])[] | select(.conclusion == "failure")] | length' 2>/dev/null || echo 0)"
    STEP_FAILURES=$((STEP_FAILURES + sfail))

    if [ "$jconc" = "skipped" ]; then
      NEVER_STARTED=$((NEVER_STARTED+1))
      printf '%s\n' "$jname" >> "$NEVER_LIST"
      JOB_SECTIONS+=("$jname|$jstatus|$jconc|$jid|SKIPPED")
      diag "job '$jname' (id $jid): skipped by the workflow — NEVER_STARTED, no log expected"
      continue
    fi

    if [ "$jconc" = "null" ] || [ "$jstatus" != "completed" ]; then
      IN_PROGRESS=$((IN_PROGRESS+1))
      JOB_SECTIONS+=("$jname|$jstatus|$jconc|$jid|IN_PROGRESS")
      diag "job '$jname' (id $jid): status=$jstatus conclusion=$jconc — IN_PROGRESS, no terminal log yet"
      continue
    fi

    sec="$WORK/job-$jid.log"
    res="$(capture_job_log "$jid" "$sec")"
    if [ -n "$res" ]; then
      MISSING=$((MISSING+1))
      printf '%s\n' "$jname" >> "$MISSING_LIST"
      JOB_SECTIONS+=("$jname|$jstatus|$jconc|$jid|MISSING:$res")
      diag "job '$jname' (id $jid): MISSING LOG SOURCE — $res"
      continue
    fi
    CAPTURED=$((CAPTURED+1))
    diag "job '$jname' (id $jid): log captured ($(wc -c < "$sec") bytes)"
    {
      # NOTE: the JOB header is added exactly once, in the report
      # assembly loop below — the section file carries only the marker
      # and the log body.
      echo "--- job log (GET /repos/$REPO/actions/jobs/$jid/logs) ---"
      cat "$sec"
      echo ""
      # v1.11.1 (P1-D): annotation extraction with runner-echoed source
      # exclusion — see classify_job_log_annotations above.
      classify_job_log_annotations "$jname" "$sec"
    } > "${sec}.section"
    mv "${sec}.section" "$sec"
    JOB_SECTIONS+=("$jname|$jstatus|$jconc|$jid|CAPTURED")
  done < "$WORK/jobs.ndjson"
fi

# --- accounting invariant --------------------------------------------------
# Every job appears in exactly one category; a jobs list with content but
# no category assignment at all is impossible and must fail closed.
if [ "$TOTAL_JOBS" -gt 0 ] \
   && [ "$CAPTURED" -eq 0 ] && [ "$MISSING" -eq 0 ] \
   && [ "$NEVER_STARTED" -eq 0 ] && [ "$IN_PROGRESS" -eq 0 ]; then
  hard_fail "coverage accounting invariant violated: $TOTAL_JOBS job(s) listed but zero captured, zero missing, zero skipped, zero in-progress — refusing to report empty coverage"
fi
diag "accounting: captured=$CAPTURED missing=$MISSING skipped=$NEVER_STARTED in_progress=$IN_PROGRESS total=$TOTAL_JOBS (sum=$((CAPTURED+MISSING+NEVER_STARTED+IN_PROGRESS)))"

# --- assemble the raw report ---------------------------------------------
{
  echo "SHEYTAN-LA consolidated run log — run $RUN_ID attempt $ATTEMPT"
  [ -n "$RUN_NAME" ] && echo "workflow: $RUN_NAME"
  echo "workflow (job input): $WORKFLOW"
  echo "repository: $REPO"
  echo "commit: $COMMIT"
  echo "ref: $REF"
  echo "trigger: $TRIGGER"
  echo "report generated (UTC): $now_utc"
  echo ""
  echo "=== RUN SUMMARY ==="
  echo "expected jobs (jobs API): $TOTAL_JOBS"
  echo "jobs with captured logs: $CAPTURED"
  echo "jobs with MISSING LOG SOURCE: $MISSING"
  echo "jobs NEVER_STARTED (skipped): $NEVER_STARTED"
  echo "jobs IN_PROGRESS (no terminal state): $IN_PROGRESS"
  echo "collector self-log sections: 1"
  echo ""
  echo "=== AUTHORITATIVE FAILURE SUMMARY (job/step metadata from the paginated jobs API) ==="
  echo "failed terminal jobs (metadata): $FAILED_JOBS"
  if [ "$FAILED_JOBS" -gt 0 ]; then
    printf '%b' "$FAILED_JOB_LIST" | sed 's/^/  failed job: /'
  fi
  echo "failed steps (metadata): $STEP_FAILURES"
  echo ""
  echo "=== LOG ANNOTATIONS (emitted by executed commands; runner-echoed source lines excluded) ==="
  echo "error annotations: $ANN_ERRORS"
  echo "warning annotations: $ANN_WARNINGS"
  echo "runner-echoed command-source literals excluded: $ECHOED_SOURCE"
  echo "collector hard errors: $HARD_ERROR"
  echo ""
  echo "=== ERROR INDEX ==="
  cat "$ERRIDX"
  echo ""
  echo "=== WARNING INDEX ==="
  cat "$WARNIDX"
  echo ""
  if [ "$NEVER_STARTED" -gt 0 ]; then
    echo "=== NEVER_STARTED ==="
    while IFS= read -r j; do
      echo "job '$j' was SKIPPED by the workflow — it never ran; no log exists and none is expected. This is not a failed command."
    done < "$NEVER_LIST"
    echo ""
  fi
  if [ "$MISSING" -gt 0 ]; then
    echo "=== MISSING LOG SOURCE ==="
    while IFS= read -r j; do
      echo "job '$j' reached a terminal state but its job-specific log endpoint did not yield a log — coverage is INCOMPLETE for this run."
    done < "$MISSING_LIST"
    echo ""
  fi
  if [ "$IN_PROGRESS" -gt 0 ]; then
    echo "=== IN_PROGRESS (no terminal state) ==="
    echo "these jobs had not reached a terminal state when the collector read the jobs API; the collector does not wait or poll — their logs are not part of this report."
    echo ""
  fi
  echo "=== COLLECTOR SELF-LOG ==="
  echo "collector: scripts/ci/consolidate-run-log.sh (job-specific logs contract)"
  echo "source: paginated jobs metadata + GET /repos/*/actions/jobs/{job_id}/logs per terminal job"
  echo "note: this collector job's own GitHub-hosted step log cannot be retrieved while the run is in progress; the collector's own progress follows from its local diagnostics file."
  echo "note: the run-attempt logs archive endpoint is intentionally NEVER called from inside a running workflow (it may 404 until the run completes)."
  echo "token handling: bearer token sent only to the API host; signed job-log URLs fetched without credentials; https-only redirects; signed URLs never written into this report."
  cat "$DIAG"
  echo ""
  echo "=== FULL LOGS (jobs-API order) ==="
  echo ""
} > "$RAW"

for spec in "${JOB_SECTIONS[@]:-}"; do
  [ -z "$spec" ] && continue
  jname="${spec%%|*}"; rest="${spec#*|}"
  jstatus="${rest%%|*}"; rest="${rest#*|}"
  jconc="${rest%%|*}"; rest="${rest#*|}"
  jid="${rest%%|*}"; state="${rest#*|}"
  {
    echo "===== JOB $jname (status: $jstatus, conclusion: $jconc, id: $jid) ====="
    case "$state" in
      CAPTURED)
        [ -f "$WORK/job-$jid.log" ] && cat "$WORK/job-$jid.log";;
      SKIPPED)
        echo "(job was skipped — no log exists)";;
      IN_PROGRESS)
        echo "(job had no terminal state when the collector read the jobs API — no log collected)";;
      MISSING*)
        echo "(no logs recovered from the job-specific log endpoint — MISSING LOG SOURCE)";;
    esac
    echo ""
  } >> "$RAW"
done

# --- redact + write the final artifact -----------------------------------
redact_stream "$RAW" > "$OUT"

# --- verdict --------------------------------------------------------------
if [ "$HARD_ERROR" -ne 0 ] || [ "$MISSING" -gt 0 ] || [ "$IN_PROGRESS" -gt 0 ]; then
  diag "verdict: FAIL (hard_error=$HARD_ERROR, missing=$MISSING, in_progress=$IN_PROGRESS, captured=$CAPTURED, never_started=$NEVER_STARTED)"
  echo "consolidate-run-log: INCOMPLETE report written to $OUT (hard_error=$HARD_ERROR missing=$MISSING in_progress=$IN_PROGRESS captured=$CAPTURED)" >&2
  exit 1
fi
diag "verdict: OK (captured=$CAPTURED, never_started=$NEVER_STARTED)"
echo "consolidate-run-log: complete report ($CAPTURED jobs captured) → $OUT"
exit 0
