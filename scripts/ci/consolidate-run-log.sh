#!/usr/bin/env bash
# consolidate-run-log.sh — build the ONE consolidated plain-text run log
# for the build-desktop workflow, collected directly from the GitHub
# run-attempt logs API (v1.11.1 single-artifact contract, work order for
# run #41548925352 §6.2):
#
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}/jobs?per_page=100&page=N
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt}/logs
#
# Contract:
#   - Output: exactly ONE plain-text file (default SHEYTAN-LA-all-logs.txt).
#     The workflow uploads it with upload-artifact@v7 archive:false, so the
#     artifact downloads as the file itself, NOT a ZIP, and the filename is
#     the artifact display name.
#   - Job and step boundaries are preserved: one "===== JOB … ====="
#     section per job, one "--- <step file> ---" marker per step log.
#     Same-named steps from different jobs live in different job sections
#     and are never flattened into one ambiguous stream.
#   - The header records workflow/run/attempt/commit/ref/event; every job
#     section records its status and conclusion (success, failure,
#     cancelled, skipped).
#   - A job with conclusion "skipped" (e.g. the tag-only release on a main
#     push) is NEVER_STARTED — it is NOT reported as "failed while running".
#   - A ran-and-finished job whose logs are absent from the attempt archive
#     is a MISSING LOG SOURCE (coverage gap, named).
#   - Deterministic order: header → run summary → error index → warning
#     index → never-started → missing sources → unmatched directories →
#     collector diagnostics → full logs in jobs-API order.
#   - Secret redaction is applied to the whole report: the caller's own
#     token value, ghp_/gho_/ghs_/github_pat_ tokens, AWS access-key ids,
#     PEM private-key headers, Authorization header values and
#     user:password@ URL credentials are REDACTED before the file is
#     written. Environment variables are never printed.
#   - Signed-URL token isolation: the GitHub bearer token is sent ONLY to
#     the API host, in the header-capture request for the redirect. The
#     signed download URL is fetched by a SEPARATE curl invocation that
#     carries NO Authorization header, after validating that the redirect
#     Location is an https URL. --location-trusted is never used.
#   - Fail closed: any jobs-API, pagination, redirect, download, size-cap
#     or ZIP-validation error produces a diagnostic report (best effort)
#     and exit 1 AFTER the report exists — a red run keeps its log.
#   - Collector self-diagnostics: the collector's own progress log is
#     embedded in the report. The run-attempt archive cannot contain the
#     still-running collector job's own step log; the report says so.
#   - Exit code: 0 when every non-skipped job contributed logs and every
#     verification passed; 1 when any gap remains — AFTER the report has
#     been written, so the always() upload still publishes it.
#
# Test seam (offline fixture suite, no network): set CONSOLIDATE_FIXTURE_DIR
# to a directory containing run.json (optional), jobs-page-N.json pages,
# logs.zip, and optional failure markers (jobs-http-code, logs-http-code,
# logs-no-location, logs-bad-location). The fetch layer is swapped; all
# parsing/reporting code paths are the production ones.
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
ARCHIVE_MAX_BYTES=1073741824   # 1 GiB cap on the run-attempt logs archive

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
LOGS_ZIP="$WORK/logs.zip"
EXTRACT="$WORK/logs"

diag() { echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*" >>"$DIAG"; }

# --- controlled exit: report first, then the verdict -------------------
HARD_ERROR=0; MISSING=0; NEVER_STARTED=0; CAPTURED=0
hard_fail() { diag "HARD ERROR: $*"; HARD_ERROR=1; }

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
    -e 's/^-----BEGIN [A-Z ]*PRIVATE KEY-----/-----BEGIN REDACTED PRIVATE KEY-----/' \
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
  hard_fail "GH_TOKEN is empty — the run-attempt logs API cannot be called without it"
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
  timeout 10 jq -c '.jobs[] | {id, name, status, conclusion}' "$WORK/jobs-$page.json" >> "$WORK/jobs.ndjson" 2>>"$DIAG" \
    || hard_fail "failed to normalize jobs metadata on page $page"
  pages=$((pages+1))
  [ "$n" -eq 0 ] && break
  page=$((page+1))
done
diag "jobs metadata: $pages page(s) fetched, $(wc -l < "$WORK/jobs.ndjson") job(s)"

# --- 3. run-attempt logs archive (REQUIRED) ------------------------------
if [ "$HARD_ERROR" -eq 0 ]; then
  if [ -n "$FIXTURE_DIR" ]; then
    if [ -f "$FIXTURE_DIR/logs-http-code" ]; then
      hard_fail "run-attempt logs download failed: HTTP $(cat "$FIXTURE_DIR/logs-http-code") (fixture)"
    elif [ -f "$FIXTURE_DIR/logs-no-location" ]; then
      hard_fail "run-attempt logs redirect did not carry a Location header — fail closed"
    elif [ -f "$FIXTURE_DIR/logs-bad-location" ]; then
      hard_fail "run-attempt logs redirect Location is not an https URL ('$(cat "$FIXTURE_DIR/logs-bad-location")') — refusing to follow"
    else
      cp "$FIXTURE_DIR/logs.zip" "$LOGS_ZIP" 2>/dev/null \
        || hard_fail "fixture logs.zip is missing"
    fi
  else
    headers="$WORK/headers.txt"
    code="$(curl -sS -m 60 -D "$headers" -o /dev/null -w '%{http_code}' \
      -H "Authorization: Bearer $TOKEN" \
      -H "Accept: application/vnd.github+json" \
      "$API/repos/$REPO/actions/runs/$RUN_ID/attempts/$ATTEMPT/logs")" \
      || hard_fail "run-attempt logs request failed (curl transport error)"
    if [ "$code" != "302" ] && [ "$code" != "200" ]; then
      hard_fail "run-attempt logs request returned HTTP $code (expected 302/200)"
    fi
    location="$(awk 'tolower($1)=="location:" {sub(/^[^:]*:[ \t]*/,""); gsub(/\r$/,""); print; exit}' "$headers")"
    if [ -z "$location" ]; then
      hard_fail "run-attempt logs redirect did not carry a Location header — fail closed"
    elif [ "${location#https://}" = "$location" ]; then
      hard_fail "run-attempt logs redirect Location is not an https URL — refusing to follow"
    else
      diag "signed redirect validated (https); fetching archive WITHOUT credentials"
      # Token isolation by construction: this curl carries NO Authorization
      # header, and --location-trusted is never used anywhere in this script.
      dcode="$(curl -sS -m 600 --max-filesize "$ARCHIVE_MAX_BYTES" -o "$LOGS_ZIP" \
        -w '%{http_code}' "$location")" \
        || hard_fail "signed-URL archive download failed (curl transport error or size cap exceeded)"
      [ "$dcode" = "200" ] || hard_fail "signed-URL archive download returned HTTP $dcode"
      [ -s "$LOGS_ZIP" ] || hard_fail "downloaded archive is empty"
    fi
  fi
fi

# --- 4. archive validation (fail closed) ---------------------------------
if [ "$HARD_ERROR" -eq 0 ] && [ -s "$LOGS_ZIP" ]; then
  if ! unzip -t -qq "$LOGS_ZIP" >/dev/null 2>&1; then
    hard_fail "run-attempt logs archive failed the integrity test (unzip -t) — corrupt or truncated"
  else
    unsafe="$(unzip -Z1 "$LOGS_ZIP" 2>/dev/null | grep -E '(^/|(^|/)\.\.(/|$))' || true)"
    if [ -n "$unsafe" ]; then
      hard_fail "run-attempt logs archive contains unsafe entry paths (absolute/traversal): $unsafe"
    else
      mkdir -p "$EXTRACT"
      if ! unzip -qq "$LOGS_ZIP" -d "$EXTRACT" >/dev/null 2>&1; then
        hard_fail "run-attempt logs archive failed to extract"
      fi
      links="$(find "$EXTRACT" -type l 2>/dev/null || true)"
      if [ -n "$links" ]; then
        hard_fail "run-attempt logs archive contains symlink entries — refusing: $links"
      fi
    fi
  fi
fi
diag "archive validation complete (hard_error=$HARD_ERROR)"

# --- 5. report ------------------------------------------------------------
now_utc="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
ERRIDX="$WORK/erridx.txt"; WARNIDX="$WORK/warnidx.txt"
: > "$ERRIDX"; : > "$WARNIDX"
NEVER_LIST="$WORK/never.txt"; MISSING_LIST="$WORK/missing.txt"; UNMATCHED_LIST="$WORK/unmatched.txt"
: > "$NEVER_LIST"; : > "$MISSING_LIST"; : > "$UNMATCHED_LIST"

norm() { printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_' | sed 's/_\{2,\}/_/g; s/^_*//; s/_*$//'; }

# Job sections are emitted in jobs-API order; archive directories are
# matched by normalized name, extras are reported as unmatched (never
# dropped), and metadata jobs without a directory are skipped →
# NEVER_STARTED, anything else → MISSING LOG SOURCE.
declare -a JOB_SECTIONS=()
seen_dirs="$WORK/seen-dirs.txt"; : > "$seen_dirs"

if [ -s "$WORK/jobs.ndjson" ] && [ "$HARD_ERROR" -eq 0 ] && [ -d "$EXTRACT" ]; then
  while IFS= read -r job; do
    jid="$(printf '%s' "$job" | jq -r '.id')"
    jname="$(printf '%s' "$job" | jq -r '.name')"
    jstatus="$(printf '%s' "$job" | jq -r '.status')"
    jconc="$(printf '%s' "$job" | jq -r '.conclusion // "running"')"
    nd="$(norm "$jname")"
    jdir=""
    for d in "$EXTRACT"/*/; do
      [ -d "$d" ] || continue
      if [ "$(norm "$(basename "$d")")" = "$nd" ]; then jdir="$d"; break; fi
    done
    if [ -z "$jdir" ]; then
      if [ "$jconc" = "skipped" ]; then
        NEVER_STARTED=$((NEVER_STARTED+1))
        printf '%s\n' "$jname" >> "$NEVER_LIST"
      else
        MISSING=$((MISSING+1))
        printf '%s\n' "$jname" >> "$MISSING_LIST"
      fi
      JOB_SECTIONS+=("$jname|$jstatus|$jconc|")
      continue
    fi
    basename "$d" >/dev/null 2>&1
    d="$(printf '%s' "$jdir" | sed 's:/$::')"
    printf '%s\n' "$d" >> "$seen_dirs"
    CAPTURED=$((CAPTURED+1))
    sec="$WORK/job-$jid.log"
    {
      echo "===== JOB $jname (status: $jstatus, conclusion: $jconc) ====="
      find "$d" -type f 2>/dev/null | LC_ALL=C sort -V | while IFS= read -r f; do
        echo "--- ${f#"$d"/} ---"
        cat "$f"
        echo ""
        while IFS= read -r hit; do
          rel="$(printf '%s' "$hit" | cut -d: -f1)"
          case "$hit" in
            *::error::*|*"##[error]"*|*"EXIT_CODE: "[1-9]*" FAIL"*) printf 'job %s, %s: %s\n' "$jname" "${hit%%:*}" "${hit#*:}" >> "$ERRIDX";;
            *::warning::*|*"##[warning]"*) printf 'job %s, %s: %s\n' "$jname" "${hit%%:*}" "${hit#*:}" >> "$WARNIDX";;
          esac
        done < <(grep -nE '::error::|##\[(error|warning)\]|::warning::|EXIT_CODE: [1-9][0-9]* FAIL' "$f" 2>/dev/null | cut -c1-240 || true)
      done
      echo ""
    } > "$sec"
    JOB_SECTIONS+=("$jname|$jstatus|$jconc|$sec")
  done < "$WORK/jobs.ndjson"

  # Archive directories with no matching metadata job: report, never drop.
  for d in "$EXTRACT"/*/; do
    [ -d "$d" ] || continue
    d="$(printf '%s' "$d" | sed 's:/$::')"
    bn="$(basename "$d")"
    hit=0
    while IFS= read -r s; do
      [ -z "$s" ] && continue
      [ "$(norm "${s%%|*}")" = "$(norm "$bn")" ] && { hit=1; break; }
    done < <(printf '%s\n' "${JOB_SECTIONS[@]:-}")
    [ "$hit" = "0" ] && printf '%s\n' "$bn" >> "$UNMATCHED_LIST"
  done
  # Top-level stray files (no job directory): report and include — never drop.
  for sfile in "$EXTRACT"/*; do
    [ -f "$sfile" ] || continue
    printf '%s\n' "$(basename "$sfile")" >> "$UNMATCHED_LIST"
  done
elif [ "$HARD_ERROR" -eq 0 ] && [ ! -d "$EXTRACT" ]; then
  hard_fail "run-attempt logs archive was not extracted — no job logs can be included"
fi

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
  echo "expected jobs (jobs API): $(wc -l < "$WORK/jobs.ndjson")"
  echo "jobs with captured logs: $CAPTURED"
  echo "jobs with MISSING LOG SOURCE: $MISSING"
  echo "jobs NEVER_STARTED (skipped): $NEVER_STARTED"
  echo "unmatched archive directories: $(wc -l < "$UNMATCHED_LIST")"
  echo "files/lines with failed commands: $(wc -l < "$ERRIDX")"
  echo "files/lines with warnings: $(wc -l < "$WARNIDX")"
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
      echo "job '$j' ran but contributed no logs to the run-attempt archive — coverage is INCOMPLETE for this run."
    done < "$MISSING_LIST"
    echo ""
  fi
  if [ -s "$UNMATCHED_LIST" ]; then
    echo "=== UNMATCHED ARCHIVE DIRECTORIES (logs included below, identity unverified) ==="
    cat "$UNMATCHED_LIST"
    echo ""
  fi
  echo "=== COLLECTOR DIAGNOSTICS ==="
  echo "collector: scripts/ci/consolidate-run-log.sh (run-attempt logs API contract)"
  echo "source: the GET /actions/runs/$RUN_ID/attempts/$ATTEMPT/logs archive + paginated jobs metadata"
  echo "note: the archive cannot contain this collector job's own still-running step log; the collector's own progress follows."
  echo "token handling: bearer token sent only to the API host; signed download URL fetched without credentials; https-only redirects."
  cat "$DIAG"
  echo ""
  echo "=== FULL LOGS (jobs-API order) ==="
  echo ""
} > "$RAW"

for spec in "${JOB_SECTIONS[@]:-}"; do
  [ -z "$spec" ] && continue
  jname="${spec%%|*}"; rest="${spec#*|}"
  jstatus="${rest%%|*}"; rest="${rest#*|}"
  jconc="${rest%%|*}"; sec="${rest#*|}"
  {
    echo "===== JOB $jname (status: $jstatus, conclusion: $jconc) ====="
    if [ -n "$sec" ] && [ -f "$sec" ]; then
      tail -n +2 "$sec"
    elif [ "$jconc" = "skipped" ]; then
      echo "(job was skipped — no log exists)"
    else
      echo "(no logs recovered from the run-attempt archive)"
    fi
    echo ""
  } >> "$RAW"
done
if [ -s "$UNMATCHED_LIST" ]; then
  {
    echo "===== UNMATCHED ARCHIVE DIRECTORIES ====="
    while IFS= read -r bn; do
      if [ -d "$EXTRACT/$bn" ]; then
        echo "--- directory $bn/ ---"
        find "$EXTRACT/$bn" -type f 2>/dev/null | LC_ALL=C sort -V | while IFS= read -r f; do
          echo "--- ${f#"$EXTRACT"/} ---"
          cat "$f"; echo ""
        done
      else
        echo "--- file $bn ---"
        cat "$EXTRACT/$bn" 2>/dev/null; echo ""
      fi
      echo ""
    done < "$UNMATCHED_LIST"
  } >> "$RAW"
fi

# --- redact + write the final artifact -----------------------------------
redact_stream "$RAW" > "$OUT"

# --- verdict --------------------------------------------------------------
if [ "$HARD_ERROR" -ne 0 ] || [ "$MISSING" -gt 0 ]; then
  diag "verdict: FAIL (hard_error=$HARD_ERROR, missing=$MISSING, captured=$CAPTURED, never_started=$NEVER_STARTED)"
  echo "consolidate-run-log: INCOMPLETE report written to $OUT (hard_error=$HARD_ERROR missing=$MISSING captured=$CAPTURED)" >&2
  exit 1
fi
diag "verdict: OK (captured=$CAPTURED, never_started=$NEVER_STARTED)"
echo "consolidate-run-log: complete report ($CAPTURED jobs captured) → $OUT"
exit 0
