#!/usr/bin/env bash
# consolidate-run-log.sh — build the ONE consolidated ERRORS-AND-WARNINGS
# plain-text run report for the build-desktop workflow, collected from
# COMPLETED JOB-SPECIFIC LOGS via the GitHub jobs API (work order for run
# #41851100943 §3.2/§8, errors-and-warnings output contract v2):
#
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}
#   GET /repos/{owner}/{repo}/actions/runs/{run_id}/jobs?per_page=100&page=N
#   GET /repos/{owner}/{repo}/actions/jobs/{job_id}/logs
#   GET /repos/{owner}/{repo}/check-runs/{check_run_id}/annotations?per_page=100&page=N
#
# WHY NOT THE RUN-ATTEMPT ARCHIVE: GitHub may return HTTP 404 for the
# run-attempt logs while the run is still in progress — and the collector
# itself runs INSIDE that very run. The collector therefore depends ONLY
# on job-specific logs of upstream jobs that have already reached a
# terminal state (the consolidate-logs job `needs:` every upstream job and
# runs with if: always()), and reconciles its own job as COLLECTOR SELF.
#
# OUTPUT CONTRACT (errors-and-warnings report, ASCII-only, English,
# deterministic — no wall-clock values, stable ordering):
#   - Section headings, exactly and only when non-empty:
#       RUN SUMMARY, FAILED JOBS, ERRORS, WARNINGS, MISSING LOG SOURCES,
#       TEST FAILURES, ROOT CAUSE, COVERAGE.
#   - INCLUDED: actual errors; warnings (from successful AND failed jobs);
#     failed assertions/test names with concise detail; failed/cancelled
#     job+step metadata; missing-log-source and cancelled-job coverage
#     facts; a concise run/failure summary.
#   - EXCLUDED: successful actions/steps/test output/OK/PASS/informational
#     lines/raw successful logs. No decorative separator rows.
#   - FORBIDDEN SUBSTRINGS: "===" and "---" never appear in the output.
#     transform_separator_runs rewrites every run of >=3 '=' to
#     EQUALS_RUN(n) and every run of >=3 '-' to HYPHENS_RUN(n).
#     Test markers are normalized first: "--- FAIL: TestX" becomes
#     "TEST_FAILURE: TestX" (name + concise following detail preserved).
#   - Non-ASCII is deterministically mapped to an ASCII equivalent or
#     escaped as \uXXXX / \UXXXXXXXX; nothing is silently dropped.
#   - METADATA IS AUTHORITATIVE: failed/cancelled jobs and failed steps
#     come from the jobs API, never from grepping log text. A failed
#     step/job with NO annotation is still represented from metadata.
#   - ANNOTATION CHANNEL vs ECHO: native ::error::/::warning:: annotations
#     are authoritative. When the jobs payload carries check_run ids, the
#     collector fetches the per-job check-run annotations (paginated). A
#     literal ::error:: in log text that matches NO fetched annotation is
#     echoed command source, not a real error (unless it also matches a
#     rule-(d) diagnostic pattern). Without a check-run channel the
#     legacy echo-heuristic applies.
#   - Rule (d): diagnostics parsed from log text even without
#     annotations — compiler error lines, test-runner failure markers
#     (normalized per above), PowerShell/tool error prefixes, nonzero
#     gate exit markers, and structured PACKAGE_GATE_RESULT <TOKEN> lines
#     (non-OK tokens are errors; BLOCKED_BY_UPSTREAM* marks the package
#     blocked and reports the FIRST upstream failure).
#   - PACKAGING: report the first upstream failure and mark the package
#     blocked; a missing-package error is reported only when packaging
#     actually ran and failed to produce a gate result.
#   - Coverage: every job appears in exactly one category (CAPTURED /
#     SKIPPED / MISSING:* / IN_PROGRESS / SELF) and the categories
#     reconcile to the jobs list. The collector's OWN job is SELF
#     (IN_PROGRESS_DURING_COLLECTION), never "missing terminal".
#     Cancelled terminal jobs are represented distinctly (conclusion
#     recorded, never folded into failure). Missing terminal job log ->
#     one MISSING LOG SOURCES row and state coverage=INCOMPLETE.
#   - A red or cancelled upstream run is NOT a collector failure; a
#     terminal job whose log endpoint fails IS (fail closed: the report
#     is always written first, the verdict exits nonzero after).
#   - Signed-URL token isolation: the bearer token is sent ONLY to the
#     API host on the job-log header-capture request. The redirect's
#     Location must be an https URL; the signed URL is fetched by a
#     SEPARATE curl invocation carrying NO Authorization header.
#     --location-trusted is never used. Signed URLs are never written
#     into the report.
#   - Secret redaction before the artifact is written: the caller's own
#     token value, ghp_/gho_/ghs_/github_pat_ tokens, AWS access-key ids,
#     PEM private-key headers, Authorization header values, URL
#     credentials and signed-URL values are REDACTED. Environment
#     variables are never printed.
#
# Test seam (offline fixture suite, no network): set CONSOLIDATE_FIXTURE_DIR
# to a directory containing run.json (optional), jobs-page-N.json pages,
# check-runs/<id>-annotations-page-N.json (optional; default []), and a
# job-logs/<job_id>/ directory per job with:
#   code      (optional) HTTP code the job-log API call returns (404, 403…);
#   location  (optional) the redirect Location value (default: a valid
#             https signed-URL placeholder);
#   body      (optional) the payload the signed-URL fetch returns.
# The fetch layer is swapped; all parsing/reporting/redaction code paths
# are the production ones.
set -uo pipefail

usage() { echo "usage: $0 --out FILE [--json-out FILE] [--repo OWNER/REPO] [--run-id ID] [--attempt N] [--commit SHA] [--ref REF] [--workflow NAME] [--trigger EVENT]" >&2; exit 64; }

OUT="SHEYTAN-LA-errors-warnings.txt"
JSON_OUT=""
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

# --- collector self-job identity (run #144 root cause) -------------------
# The collector runs INSIDE the consolidate-logs job, so the jobs API always
# lists that job itself as in_progress. It is identified by name and
# accounted as SELF (expected, never a coverage gap). Identification:
#   1. CONSOLIDATE_SELF_JOB_NAME — explicit display-name override;
#   2. GITHUB_JOB — the runner-provided YAML job key (e.g. "consolidate-logs"),
#      matched against the API display name with a normalized comparison
#      (case-insensitive, spaces/dashes/underscores stripped).
SELF_JOB_NAME="${CONSOLIDATE_SELF_JOB_NAME:-${GITHUB_JOB:-}}"

normalize_job_name() {  # lowercase, strip spaces/dashes/underscores
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d ' -_'
}

is_self_job() {  # $1 = job display name from the jobs API
  [ -n "$SELF_JOB_NAME" ] \
    && [ "$(normalize_job_name "$1")" = "$(normalize_job_name "$SELF_JOB_NAME")" ]
}

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2;;
    --json-out) JSON_OUT="$2"; shift 2;;
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
tabchar="$(printf '\t')"

# Classified-entry indexes (tab fields: job-index, timestamp, seq, text).
# They exist from the very first hard error so collector faults land in
# the report's ERRORS section too.
ERRIDX="$WORK/erridx.txt"; WARNIDX="$WORK/warnidx.txt"; TFAILIDX="$WORK/tfailidx.txt"
FAILEDJOBS="$WORK/failed-jobs.txt"; MISSINGROWS="$WORK/missing-rows.txt"
JNAMES="$WORK/jnames.txt"; PKGJOBS="$WORK/pkgjobs.txt"
: > "$ERRIDX"; : > "$WARNIDX"; : > "$TFAILIDX"
: > "$FAILEDJOBS"; : > "$MISSINGROWS"; : > "$JNAMES"; : > "$PKGJOBS"

diag() { echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*" >>"$DIAG"; }

# --- controlled exit: report first, then the verdict -------------------
HARD_ERROR=0; CAPTURED=0; MISSING=0; NEVER_STARTED=0; IN_PROGRESS=0; SELF_IN_PROGRESS=0
FAILED_JOBS=0; CANCELLED=0
FIRST_FAIL_NAME=""; FIRST_FAIL_ID=""; FIRST_FAIL_JIDX=""
# Failures come from METADATA, not from grepping log text for annotation
# tokens — runner-echoed command source is never a failure fact.
hard_fail() {
  diag "HARD ERROR: $*"
  HARD_ERROR=$((HARD_ERROR+1))
  printf '0\t0000\t%d\tcollector: %s\n' "$HARD_ERROR" "$*" >> "$ERRIDX"
}

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
    -e 's/-----BEGIN [A-Z ]*PRIVATE KEY-----/[REDACTED:ssh_private_key]/g' \
    -e 's#https://objects\.githubusercontent\.com/[^[:space:]]*#[REDACTED:signed_url]#g' \
    -e 's#(X-Amz-[A-Za-z-]+=)[^&[:space:]]*#\1REDACTED#g' \
    -e 's#([?&]token=)[^&[:space:]]*#\1REDACTED#g' \
    "$1"
}

# --- text cleaning pipeline ----------------------------------------------
# strip_ansi_stream: byte-level removal of ANSI escape sequences (CSI,
# OSC, two-byte ESC forms) and CR bytes; applied before classification
# so patterns never see escape noise.
strip_ansi_stream() {  # $1 = input file; stdout = cleaned
  LC_ALL=C awk '
    BEGIN { for (i = 1; i < 256; i++) ORD[sprintf("%c", i)] = i }
    {
      s = $0; out = ""; n = length(s); i = 1
      while (i <= n) {
        c = substr(s, i, 1); b = ORD[c] + 0
        if (b == 27) {
          if (i + 1 <= n) {
            db = ORD[substr(s, i + 1, 1)] + 0
            if (db == 91) {            # CSI: ESC [ ... final-byte >= 64
              j = i + 2
              while (j <= n && (ORD[substr(s, j, 1)] + 0) < 64) j++
              i = j + 1; continue
            } else if (db == 93) {     # OSC: ESC ] ... BEL or ESC \
              j = i + 2
              while (j <= n) {
                pb = ORD[substr(s, j, 1)] + 0
                if (pb == 7) break
                if (pb == 27) { if (substr(s, j + 1, 1) == "\\") j++; break }
                j++
              }
              i = j + 1; continue
            } else { i = i + 2; continue }
          } else { i++; continue }
        }
        if (b != 13) out = out c
        i++
      }
      print out
    }' "$1"
}

# clean_stream: the ASCII/forbidden-substring normalization applied to the
# assembled report (and usable on any text stream):
#   1. non-ASCII -> ASCII equivalent or \uXXXX / \UXXXXXXXX escape;
#   2. test markers: "--- FAIL: X" -> "TEST_FAILURE: X";
#   3. transform_separator_runs: every run of >=3 '=' -> EQUALS_RUN(n),
#      every run of >=3 '-' -> HYPHENS_RUN(n) — "===" and "---" can never
#      survive into the artifact, and the runs are counted, not dropped.
clean_stream() {  # $1 = input file; stdout = normalized
  LC_ALL=C awk '
    BEGIN { for (i = 1; i < 256; i++) ORD[sprintf("%c", i)] = i }
    function esc_cp(cp) {
      if (cp <= 65535) return sprintf("\\u%04X", cp)
      return sprintf("\\U%08X", cp)
    }
    function map_cp(cp) {
      if (cp == 160) return " "
      if (cp == 169) return "(c)"
      if (cp == 174) return "(r)"
      if (cp == 171) return "<"
      if (cp == 187) return ">"
      if (cp == 183) return "."
      if (cp == 215) return "x"
      if (cp == 8211 || cp == 8722) return "-"
      if (cp == 8212 || cp == 8213) return "--"
      if (cp >= 8216 && cp <= 8219) return "\047"
      if (cp >= 8220 && cp <= 8223) return "\""
      if (cp == 8224 || cp == 8225 || cp == 8226) return "*"
      if (cp == 8230) return "..."
      if (cp == 8249) return "<"
      if (cp == 8250) return ">"
      if (cp == 8592) return "<-"
      if (cp == 8594) return "->"
      if (cp == 8656) return "<="
      if (cp == 8658) return "=>"
      if (cp == 8800) return "!="
      if (cp == 8804) return "<="
      if (cp == 8805) return ">="
      if (cp == 9474 || cp == 9475 || cp == 9553) return "|"
      if (cp == 9484 || cp == 9488 || cp == 9492 || cp == 9496 || cp == 9516 || cp == 9524 || cp == 9532) return "+"
      if (cp >= 9472 && cp <= 9599) return "-"
      if (cp >= 9600 && cp <= 9631) return "#"
      if (cp == 9642 || cp == 9656 || cp == 9657 || cp == 9679) return "*"
      return ""
    }
    function transform_separator_runs(s, re, name,   r, run) {
      r = ""
      while (match(s, re)) {
        run = substr(s, RSTART, RLENGTH)
        if (length(run) >= 3) {
          r = r substr(s, 1, RSTART - 1) name "(" length(run) ")"
        } else {
          r = r substr(s, 1, RSTART + RLENGTH - 1)
        }
        s = substr(s, RSTART + RLENGTH)
      }
      return r s
    }
    {
      s = $0; out = ""; n = length(s); i = 1
      while (i <= n) {
        c = substr(s, i, 1); b = ORD[c] + 0
        if (b >= 128) {
          cp = 0; len = 0
          if (b >= 194 && b <= 223) { cp = b - 192; len = 2 }
          else if (b >= 224 && b <= 239) { cp = b - 224; len = 3 }
          else if (b >= 240 && b <= 247) { cp = b - 240; len = 4 }
          if (len == 0) { out = out esc_cp(b); i++; continue }
          ok = 1
          for (k = 1; k < len; k++) {
            nb = ORD[substr(s, i + k, 1)] + 0
            if (i + k > n || nb < 128 || nb > 191) { ok = 0; break }
            cp = cp * 64 + (nb - 128)
          }
          if (!ok) { out = out esc_cp(b); i++; continue }
          m = map_cp(cp)
          if (m != "") out = out m; else out = out esc_cp(cp)
          i += len; continue
        }
        if ((b < 32 && b != 9) || b == 127) { out = out esc_cp(b); i++; continue }
        out = out c
        i++
      }
      gsub(/--- FAIL: ?/, "TEST_FAILURE: ", out)
      out = transform_separator_runs(out, "==+", "EQUALS_RUN")
      out = transform_separator_runs(out, "--+", "HYPHENS_RUN")
      print out
    }' "$1"
}

# --- fetch layer (network or fixture seam) ------------------------------
fetch_json() {  # $1 = api path, $2 = outfile; echoes HTTP code
  local path="$1" outfile="$2" code=""
  if [ -n "$FIXTURE_DIR" ]; then
    case "$path" in
      *"/check-runs/"*)
        # Per-job check-run annotations (the authoritative annotation
        # channel). Fixture seam: check-runs/<id>-annotations-page-N.json,
        # defaulting to an empty array exactly like the live endpoint.
        crid="$(printf '%s' "$path" | sed -E 's#^.*/check-runs/([0-9]+)/.*$#\1#')"
        annf="$FIXTURE_DIR/check-runs/$crid-annotations-page-$FETCH_PAGE.json"
        if [ -f "$annf" ]; then
          cp "$annf" "$outfile"; echo "200"
        else
          printf '[]' > "$outfile"; echo "200"
        fi;;
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

# Per-job check-run annotations — the authoritative annotation channel.
# Returns 1 (fetched) via stdout when the endpoint answered 200 for every
# page; 0 means unavailable (fall back to the echo heuristic).
fetch_annotations() {  # $1 = check-run id, $2 = ndjson outfile (level \t message)
  local crid="$1" out="$2" page=1 code="" n=""
  : > "$out"
  while [ "$page" -le 20 ]; do
    code="$(fetch_json "/repos/$REPO/check-runs/$crid/annotations?per_page=100&page=$page" "$WORK/ann-$crid-$page.json")"
    if [ "$code" != "200" ]; then echo 0; return 0; fi
    if ! timeout 10 jq -e 'type == "array"' "$WORK/ann-$crid-$page.json" >/dev/null 2>&1; then
      echo 0; return 0
    fi
    timeout 10 jq -r '.[] | [(.annotation_level // ""), (((.path // "") + ":" + ((.start_line // 0) | tostring) + ": " + (.message // "")) | gsub("\\s+"; " "))] | @tsv' \
      "$WORK/ann-$crid-$page.json" >> "$out" 2>>"$DIAG" || { echo 0; return 0; }
    n="$(timeout 10 jq -r 'length' "$WORK/ann-$crid-$page.json" 2>/dev/null || echo 0)"
    [ "${n:-0}" -lt 100 ] && break
    page=$((page+1))
  done
  echo 1
  return 0
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
: > "$WORK/jobs-diag.ndjson"
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
  # Carry the per-job STEPS (authoritative per-step failure facts), the
  # check_run id (the annotation channel key), and the terminal facts.
  timeout 10 jq -c '.jobs[] | {id, name, status, conclusion,
    check_run_id: (if ((.check_run_url // "") | contains("check-runs/")) then (.check_run_url | sub(".*check-runs/"; "")) else "" end),
    steps: [(.steps // [] | to_entries[]) | {number: (.key + 1), name: .value.name, conclusion: .value.conclusion}]}' \
    "$WORK/jobs-$page.json" >> "$WORK/jobs.ndjson" 2>>"$DIAG" \
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
# skipped       -> SKIPPED: no fetch, no log, never "missing".
# not terminal  -> IN_PROGRESS (SELF for the collector's own job).
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

# --- 4. classification ----------------------------------------------------
# classify_job_log — rule (d) diagnostics + annotation-channel matching,
# per captured job. Entry rows are TAB-separated:
#   job-index \t timestamp \t seq \t text
# so each section can be sorted by workflow job order, then timestamp.
classify_job_log() {  # $1 = job index; $2 = has_ann(0/1); $3 = annotations ndjson; $4 = package-state file; $5 = job conclusion; log on stdin
  LC_ALL=C awk -v jidx="$1" -v has_ann="$2" -v annfile="$3" -v pkgfile="$4" -v jconc="$5" \
    -v errfile="$ERRIDX" -v warnfile="$WARNIDX" -v tfailfile="$TFAILIDX" -v sq="'" '
    function normmsg(m) { gsub(/[ \t]+/, " ", m); sub(/^ /, "", m); sub(/ $/, "", m); return m }
    function emit(lvl, t, txt,   k) {
      if (length(txt) > 240) txt = substr(txt, 1, 240)
      if (t == "") t = "0000"
      k = ++SEQ
      if (lvl == "error") print jidx "\t" t "\t" k "\t" txt >> errfile
      else if (lvl == "warn") print jidx "\t" t "\t" k "\t" txt >> warnfile
      else print jidx "\t" t "\t" k "\t" txt >> tfailfile
    }
    function is_echo_source(s,   re) {
      # A command SOURCE echo whose string LITERAL contains the annotation
      # token: bash echo/printf, PowerShell Write-*, and the repo Log helper.
      re = "(^|[;&|[ \t+(-])(echo|printf|Write-Host|Write-Output|Write-Warning|Write-Error|Log)[ \t].*(\"|" sq ")(::error::|##\\[error\\]|::warning::|##\\[warning\\])"
      return s ~ re
    }
    BEGIN {
      NA = 0
      if (has_ann && annfile != "") {
        while ((getline aline < annfile) > 0) {
          if (aline == "") continue
          p = index(aline, "\t")
          NA++
          annlvl[NA] = substr(aline, 1, p - 1)
          annmsg[NA] = substr(aline, p + 1)
          if (annlvl[NA] == "failure") annerr[normmsg(annmsg[NA])] = 1
          else if (annlvl[NA] == "warning") annwarn[normmsg(annmsg[NA])] = 1
        }
        close(annfile)
      }
      # Authoritative annotation rows are emitted first (stable seq order).
      for (i = 1; i <= NA; i++) {
        if (annlvl[i] == "failure") emit("error", "", "[annotation] " annmsg[i])
        else if (annlvl[i] == "warning") emit("warn", "", "[annotation] " annmsg[i])
      }
      pending = 0
    }
    {
      ts = ""; rest = $0
      if (match(rest, /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9](\.[0-9]+)?Z?/)) {
        ts = substr(rest, 1, RLENGTH)
        rest = substr(rest, RLENGTH + 1)
        sub(/^[ \t]+/, "", rest)
      }
      # structured PACKAGE_GATE_RESULT (checked first; BLOCKED never becomes
      # an error row — it reports the first upstream failure instead)
      if (rest ~ /^PACKAGE_GATE_RESULT[ \t]+/) {
        body = rest; sub(/^PACKAGE_GATE_RESULT[ \t]+/, "", body)
        tok = body; sub(/[ \t].*$/, "", tok)
        det = body; sub(/^[ \t]*[^ \t]+[ \t]*/, "", det)
        if (tok == "OK") {
          print "OK\t" >> pkgfile
        } else if (tok ~ /^BLOCKED_BY_UPSTREAM/) {
          if (det == "") det = tok
          print "BLOCKED\t" det >> pkgfile
        } else {
          print "FAILED\t" tok >> pkgfile
          emit("error", ts, rest)
        }
        next
      }
      # runner annotation tokens
      kind = ""; tokstr = ""
      if (index(rest, "::error::") > 0) { kind = "error"; tokstr = "::error::" }
      else if (index(rest, "::warning::") > 0) { kind = "warning"; tokstr = "::warning::" }
      else if (index(rest, "##[error]") > 0) { kind = "error"; tokstr = "##[error]" }
      else if (index(rest, "##[warning]") > 0) { kind = "warning"; tokstr = "##[warning]" }
      if (kind != "") {
        p = index(rest, tokstr)
        after = substr(rest, p + length(tokstr))
        msg = after
        q = index(after, "::")
        if (q > 0 && index(substr(after, 1, q - 1), "=") > 0) msg = substr(after, q + 2)
        sub(/[ \t]+$/, "", msg)
        if (has_ann) {
          # Annotation channel is authoritative: a log token that matches a
          # fetched annotation is consumed (the annotation row already
          # represents it); one that matches NOTHING is echoed source.
          matched = 0
          k = normmsg(msg)
          if (kind == "error" && (k in annerr)) matched = 1
          else if (kind == "warning" && (k in annwarn)) matched = 1
          if (!matched && length(k) >= 8) {
            for (w in annerr) if (index(k, w) > 0) matched = 1
            if (!matched) for (w in annwarn) if (index(k, w) > 0) matched = 1
          }
          if (matched) next
          # fall through: an unmatched token can still be a rule-(d) line
        } else {
          if (is_echo_source($0)) next
          # Strip the token by INDEX (sub() would treat the token as a
          # regex: "##[error]" contains a character class).
          clean = rest
          p2 = index(clean, tokstr)
          if (p2 > 0) clean = substr(clean, 1, p2 - 1) substr(clean, p2 + length(tokstr))
          lvl = (kind == "warning") ? "warn" : kind
          emit(lvl, ts, clean)
          next
        }
      }
      # test-runner failure markers (+ concise detail lookahead)
      if (match(rest, /--- FAIL: ?/)) {
        nm = substr(rest, RSTART + RLENGTH)
        sub(/[ \t]+$/, "", nm)
        emit("tfail", ts, "TEST_FAILURE: " nm)
        pending = 2
        next
      }
      if (pending > 0) {
        if (rest ~ /^[ \t]+[^ \t]/) {
          d = rest; sub(/^[ \t]+/, "", d)
          emit("tfail", ts, d)
          pending--
          next
        }
        pending = 0
      }
      # compiler / static-analysis lines (file.ext:line[:col]: message)
      # Run 44378562071 §4.7/§8.3.5-8.3.6: a file:line: message is only a
      # failure diagnostic when the JOB METADATA says the job failed (or
      # was cancelled). A SUCCESSFUL job file:line: lines are ordinary
      # passing-test t.Log diagnostics (the Core audit installer scan and
      # the oversized-asset size report are the recorded false-positive
      # cases) - they are excluded from ERRORS; warning-prefixed ones stay
      # warnings (clause C). Metadata is authoritative, never log text.
      if (match(rest, /^[A-Za-z0-9_./\\+-]+\.[A-Za-z][A-Za-z0-9]*:[0-9]+:[0-9]+:/) || match(rest, /^[A-Za-z0-9_./\\+-]+\.[A-Za-z][A-Za-z0-9]*:[0-9]+:/)) {
        msg = substr(rest, RLENGTH + 1)
        if (tolower(msg) ~ /^[ \t]*warning/) { emit("warn", ts, rest); next }
        if (jconc != "failure" && jconc != "cancelled") next
        emit("error", ts, rest)
        next
      }
      # PowerShell / tool error prefixes — same conclusion gating: in a
      # successful job these are ordinary diagnostics, never failures.
      if (jconc == "failure" || jconc == "cancelled") {
        if (rest ~ /^Write-Error: / || rest ~ /^ERROR: / || rest ~ /^Exception calling/) { emit("error", ts, rest); next }
        # gate-script exit marker (nonzero only; "EXIT_CODE: 0 PASS" is success noise)
        if (match(rest, /^EXIT_CODE: [1-9][0-9]* FAIL/)) { emit("error", ts, rest); next }
      }
      # everything else (OK/PASS/informational/successful output) is excluded
    }'
}

# --- 5. per-job walk (jobs-API order) -------------------------------------
JIDX=0
if [ -s "$WORK/jobs.ndjson" ]; then
  while IFS= read -r job; do
    [ -z "$job" ] && continue
    JIDX=$((JIDX+1))
    jid="$(printf '%s' "$job" | jq -r '.id')"
    jname="$(printf '%s' "$job" | jq -r '.name')"
    jstatus="$(printf '%s' "$job" | jq -r '.status')"
    jconc="$(printf '%s' "$job" | jq -r '.conclusion // "null"')"
    crid="$(printf '%s' "$job" | jq -r '.check_run_id // ""')"
    printf '%d\t%s\n' "$JIDX" "$jname" >> "$JNAMES"

    # AUTHORITATIVE facts: failed/cancelled jobs and failed steps come from
    # the metadata, never from log text.
    if [ "$jconc" = "failure" ]; then
      FAILED_JOBS=$((FAILED_JOBS+1))
      printf 'job \047%s\047 (id %s): conclusion=failure\n' "$jname" "$jid" >> "$FAILEDJOBS"
      if [ -z "$FIRST_FAIL_NAME" ]; then
        FIRST_FAIL_NAME="$jname"; FIRST_FAIL_ID="$jid"; FIRST_FAIL_JIDX="$JIDX"
      fi
    fi
    if [ "$jconc" = "cancelled" ]; then
      CANCELLED=$((CANCELLED+1))
      printf 'job \047%s\047 (id %s): conclusion=cancelled\n' "$jname" "$jid" >> "$FAILEDJOBS"
    fi
    if [ "$jconc" = "failure" ] || [ "$jconc" = "cancelled" ]; then
      while IFS= read -r srow; do
        [ -z "$srow" ] && continue
        snum="${srow%%"$tabchar"*}"; sname="${srow#*"$tabchar"}"
        printf 'job \047%s\047 (id %s): failed step %s \047%s\047 (conclusion failure)\n' \
          "$jname" "$jid" "$snum" "$sname" >> "$FAILEDJOBS"
      done < <(printf '%s' "$job" | timeout 10 jq -r '[(.steps // [] | to_entries[]) | select(.value.conclusion == "failure")] | .[] | ([.key + 1, .value.name] | @tsv)' 2>>"$DIAG")
    fi

    if [ "$jconc" = "skipped" ]; then
      NEVER_STARTED=$((NEVER_STARTED+1))
      diag "job '$jname' (id $jid): skipped by the workflow — SKIPPED, no log expected"
      printf '%s' "$job" | jq -c --arg l "SKIPPED" '{name: .name, status: .status, conclusion: (if .conclusion == null then null else .conclusion end), id: (.id | tostring), log_source: $l}' >> "$WORK/jobs-diag.ndjson" 2>>"$DIAG"
      continue
    fi

    if [ "$jconc" = "null" ] || [ "$jstatus" != "completed" ]; then
      if is_self_job "$jname"; then
        SELF_IN_PROGRESS=$((SELF_IN_PROGRESS+1))
        diag "job '$jname' (id $jid): the collector's OWN job — SELF (expected in progress while the collector runs)"
        printf '%s' "$job" | jq -c --arg l "SELF" '{name: .name, status: .status, conclusion: (if .conclusion == null then null else .conclusion end), id: (.id | tostring), log_source: $l}' >> "$WORK/jobs-diag.ndjson" 2>>"$DIAG"
      else
        IN_PROGRESS=$((IN_PROGRESS+1))
        diag "job '$jname' (id $jid): status=$jstatus conclusion=$jconc — IN_PROGRESS, no terminal log yet"
        printf '%s' "$job" | jq -c --arg l "IN_PROGRESS" '{name: .name, status: .status, conclusion: (if .conclusion == null then null else .conclusion end), id: (.id | tostring), log_source: $l}' >> "$WORK/jobs-diag.ndjson" 2>>"$DIAG"
      fi
      continue
    fi

    sec="$WORK/job-$jid.log"
    res="$(capture_job_log "$jid" "$sec")"
    if [ -n "$res" ]; then
      MISSING=$((MISSING+1))
      printf 'job \047%s\047 (id %s): conclusion=%s, retrieval status=%s, coverage=INCOMPLETE\n' \
        "$jname" "$jid" "$jconc" "${res#MISSING:}" >> "$MISSINGROWS"
      diag "job '$jname' (id $jid): MISSING LOG SOURCE — $res"
      printf '%s' "$job" | jq -c --arg l "$res" '{name: .name, status: .status, conclusion: (if .conclusion == null then null else .conclusion end), id: (.id | tostring), log_source: $l}' >> "$WORK/jobs-diag.ndjson" 2>>"$DIAG"
      continue
    fi
    CAPTURED=$((CAPTURED+1))
    diag "job '$jname' (id $jid): log captured ($(wc -c < "$sec") bytes)"

    # annotation channel (authoritative) when the jobs payload has a
    # check_run id; unavailable channel falls back to the echo heuristic
    has_ann=0; annfile=""
    if [ -n "$crid" ]; then
      annfile="$WORK/ann-$jid.ndjson"
      has_ann="$(fetch_annotations "$crid" "$annfile")"
      diag "job '$jname' (id $jid): check-run annotations fetched=$has_ann (check_run $crid)"
    fi
    c1="$WORK/clean-$jid.1"; c2="$WORK/clean-$jid.2"
    strip_ansi_stream "$sec" > "$c1"
    redact_stream "$c1" > "$c2"
    pkgfile="$WORK/pkg-$jid.txt"
    classify_job_log "$JIDX" "$has_ann" "$annfile" "$pkgfile" "$jconc" < "$c2"

    case "$(normalize_job_name "$jname")" in
      *package*) printf '%d\t%s\n' "$JIDX" "$jid" >> "$PKGJOBS";;
    esac

    printf '%s' "$job" | jq -c --arg l "CAPTURED" '{name: .name, status: .status, conclusion: (if .conclusion == null then null else .conclusion end), id: (.id | tostring), log_source: $l}' >> "$WORK/jobs-diag.ndjson" 2>>"$DIAG"
  done < "$WORK/jobs.ndjson"
fi

# --- accounting invariant --------------------------------------------------
# Every job appears in exactly one category; a jobs list with content but
# no category assignment at all is impossible and must fail closed.
if [ "$TOTAL_JOBS" -gt 0 ] \
   && [ "$CAPTURED" -eq 0 ] && [ "$MISSING" -eq 0 ] \
   && [ "$NEVER_STARTED" -eq 0 ] && [ "$IN_PROGRESS" -eq 0 ] \
   && [ "$SELF_IN_PROGRESS" -eq 0 ]; then
  hard_fail "coverage accounting invariant violated: $TOTAL_JOBS job(s) listed but zero captured, zero missing, zero skipped, zero in-progress — refusing to report empty coverage"
fi
diag "accounting: captured=$CAPTURED missing=$MISSING skipped=$NEVER_STARTED in_progress=$IN_PROGRESS self=$SELF_IN_PROGRESS total=$TOTAL_JOBS (sum=$((CAPTURED+MISSING+NEVER_STARTED+IN_PROGRESS+SELF_IN_PROGRESS)))"

# --- packaging state -------------------------------------------------------
# BLOCKED_BY_UPSTREAM* -> package blocked, FIRST upstream failure reported.
# A missing-package error is reported ONLY when packaging actually ran
# (a terminal package job was captured) and produced no gate result.
pkg_blocked=0; pkg_failed=0; pkg_ok=0; PKG_DETAIL=""
for pf in "$WORK"/pkg-*.txt; do
  [ -e "$pf" ] || continue
  while IFS= read -r pl; do
    [ -z "$pl" ] && continue
    st="${pl%%"$tabchar"*}"; dt="${pl#*"$tabchar"}"
    case "$st" in
      BLOCKED) pkg_blocked=1; if [ -n "$dt" ] && [ -z "$PKG_DETAIL" ]; then PKG_DETAIL="$dt"; fi;;
      FAILED) pkg_failed=1;;
      OK) pkg_ok=1;;
    esac
  done < "$pf"
done
PKG="NOT_DETECTED"
if [ "$pkg_blocked" = 1 ]; then
  PKG="BLOCKED"
  diag "package: BLOCKED by upstream failure (${PKG_DETAIL:-no detail})"
elif [ "$pkg_failed" = 1 ]; then
  PKG="FAILED"
  diag "package: FAILED (non-OK PACKAGE_GATE_RESULT)"
elif [ "$pkg_ok" = 1 ]; then
  PKG="OK"
  diag "package: OK"
elif [ -s "$PKGJOBS" ]; then
  PKG="MISSING"
  pj="$(sed -n '1p' "$PKGJOBS")"
  pjidx="${pj%%"$tabchar"*}"
  printf '%s\t0000\t999999\tpackaging ran but produced no PACKAGE_GATE_RESULT (package output missing)\n' "$pjidx" >> "$ERRIDX"
  diag "package: MISSING (packaging ran but produced no PACKAGE_GATE_RESULT)"
fi

# --- deterministic ordering: job order, then timestamp, then seq -----------
sort_idx() {  # $1 in, $2 out
  LC_ALL=C sort -t "$tabchar" -k1,1n -k2,2 -k3,3n -s "$1" > "$2"
}
render_idx() {  # $1 sorted index, $2 names file -> stdout
  LC_ALL=C awk -v names="$2" '
    BEGIN { while ((getline l < names) > 0) { p = index(l, "\t"); nm[substr(l, 1, p - 1) + 0] = substr(l, p + 1) } }
    {
      l = $0
      p1 = index(l, "\t"); a = substr(l, 1, p1 - 1); r = substr(l, p1 + 1)
      p2 = index(r, "\t"); r = substr(r, p2 + 1)
      p3 = index(r, "\t"); txt = substr(r, p3 + 1)
      if (a + 0 == 0) print txt
      else print "job \047" nm[a + 0] "\047: " txt
    }' "$1"
}
sort_idx "$ERRIDX" "$WORK/err.sorted"
sort_idx "$WARNIDX" "$WORK/warn.sorted"
sort_idx "$TFAILIDX" "$WORK/tfail.sorted"
N_ERRORS="$(wc -l < "$WORK/err.sorted")"
N_WARNINGS="$(wc -l < "$WORK/warn.sorted")"
N_TFAIL="$(wc -l < "$WORK/tfail.sorted")"

FIRST_FAIL_ERR=""
if [ -n "$FIRST_FAIL_JIDX" ] && [ -s "$WORK/err.sorted" ]; then
  FIRST_FAIL_ERR="$(LC_ALL=C awk -v target="$FIRST_FAIL_JIDX" '
    {
      p1 = index($0, "\t"); a = substr($0, 1, p1 - 1)
      if (a + 0 == target + 0) {
        r = substr($0, p1 + 1); p2 = index(r, "\t"); r = substr(r, p2 + 1)
        p3 = index(r, "\t"); print substr(r, p3 + 1); exit
      }
    }' "$WORK/err.sorted")"
fi

# --- 6. assemble the errors-and-warnings report ---------------------------
COVERAGE_STATE="COMPLETE"
if [ "$HARD_ERROR" -ne 0 ] || [ "$MISSING" -gt 0 ] || [ "$IN_PROGRESS" -gt 0 ]; then
  COVERAGE_STATE="INCOMPLETE"
fi
now_utc="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

{
  echo "SHEYTAN-LA errors-and-warnings run report"
  echo ""
  echo "RUN SUMMARY"
  echo "run: $RUN_ID attempt: $ATTEMPT"
  [ -n "$RUN_NAME" ] && echo "run name: $RUN_NAME"
  echo "workflow: $WORKFLOW"
  echo "repository: $REPO"
  echo "commit: $COMMIT"
  echo "ref: $REF"
  echo "trigger: $TRIGGER"
  echo "jobs (jobs API): $TOTAL_JOBS"
  echo "logs captured: $CAPTURED"
  echo "jobs failed (metadata): $FAILED_JOBS"
  echo "jobs cancelled (metadata): $CANCELLED"
  echo "jobs skipped (never started): $NEVER_STARTED"
  echo "jobs in progress (non-self, coverage gap): $IN_PROGRESS"
  echo "jobs in progress (collector self): $SELF_IN_PROGRESS"
  echo "missing log sources: $MISSING"
  echo "errors: $N_ERRORS"
  echo "warnings: $N_WARNINGS"
  echo "test failures: $N_TFAIL"
  echo "package: $PKG"
  echo ""
  if [ "$FAILED_JOBS" -gt 0 ] || [ "$CANCELLED" -gt 0 ]; then
    echo "FAILED JOBS"
    cat "$FAILEDJOBS"
    echo ""
  fi
  if [ "$N_ERRORS" -gt 0 ]; then
    echo "ERRORS"
    render_idx "$WORK/err.sorted" "$JNAMES"
    echo ""
  fi
  if [ "$N_WARNINGS" -gt 0 ]; then
    echo "WARNINGS"
    render_idx "$WORK/warn.sorted" "$JNAMES"
    echo ""
  fi
  if [ "$MISSING" -gt 0 ]; then
    echo "MISSING LOG SOURCES"
    cat "$MISSINGROWS"
    echo ""
  fi
  if [ "$N_TFAIL" -gt 0 ]; then
    echo "TEST FAILURES"
    render_idx "$WORK/tfail.sorted" "$JNAMES"
    echo ""
  fi
  if [ "$FAILED_JOBS" -gt 0 ] || [ "$HARD_ERROR" -gt 0 ] || [ "$PKG" = "BLOCKED" ]; then
    echo "ROOT CAUSE"
    if [ -n "$FIRST_FAIL_NAME" ]; then
      echo "first upstream failure: job '$FIRST_FAIL_NAME' (id $FIRST_FAIL_ID, conclusion failure)"
      [ -n "$FIRST_FAIL_ERR" ] && echo "first error: job '$FIRST_FAIL_NAME': $FIRST_FAIL_ERR"
    fi
    if [ "$PKG" = "BLOCKED" ]; then
      echo "package: BLOCKED by upstream failure (${PKG_DETAIL:-no detail})"
    fi
    [ "$HARD_ERROR" -gt 0 ] && echo "collector hard errors: $HARD_ERROR (see ERRORS and COVERAGE)"
    echo ""
  fi
  echo "COVERAGE"
  echo "accounting: captured=$CAPTURED missing=$MISSING skipped=$NEVER_STARTED in_progress=$IN_PROGRESS self=$SELF_IN_PROGRESS total=$TOTAL_JOBS (sum=$((CAPTURED+MISSING+NEVER_STARTED+IN_PROGRESS+SELF_IN_PROGRESS)))"
  echo "coverage: $COVERAGE_STATE"
  [ "$SELF_IN_PROGRESS" -gt 0 ] && echo "collector self job: IN_PROGRESS_DURING_COLLECTION (expected, not a coverage gap)"
} > "$RAW"

# Final normalization pass: strip ANSI, redact, then ASCII-normalize,
# normalize test markers and run transform_separator_runs — the artifact
# can never contain "===", "---" or non-ASCII bytes.
strip_ansi_stream "$RAW" > "$WORK/final.1"
redact_stream "$WORK/final.1" > "$WORK/final.2"
clean_stream "$WORK/final.2" > "$OUT" || hard_fail "failed to write the normalized report to $OUT"
[ -s "$OUT" ] || hard_fail "text report is empty"

# --- machine-readable run diagnostics index -------------------------------
# One JSON object per run, from the SAME accounting data as the text report
# (single source of truth). Jobs-API metadata only — no secrets, no signed
# URLs, so no redaction is needed.
if [ -n "$JSON_OUT" ]; then
  jq -n \
    --arg schema "sheytan-la/run-diagnostics/1" \
    --arg repo "$REPO" --arg run "$RUN_ID" --arg attempt "$ATTEMPT" \
    --arg workflow "$WORKFLOW" --arg trigger "$TRIGGER" \
    --arg commit "$COMMIT" --arg ref "$REF" --arg generated "$now_utc" \
    --argjson totals "{\"jobs\": $TOTAL_JOBS, \"captured\": $CAPTURED, \"missing_log_source\": $MISSING, \"never_started\": $NEVER_STARTED, \"in_progress\": $IN_PROGRESS, \"self_in_progress\": $SELF_IN_PROGRESS, \"failed_jobs\": $FAILED_JOBS, \"collector_hard_errors\": $HARD_ERROR}" \
    --slurpfile jobrows "$WORK/jobs-diag.ndjson" \
    '{schema: $schema, repository: $repo, run_id: $run, attempt: $attempt,
      workflow: $workflow, trigger: $trigger, commit: $commit, ref: $ref,
      generated_utc: $generated, totals: $totals,
      jobs: ($jobrows // []) }' > "$JSON_OUT"
  [ -s "$JSON_OUT" ] || { hard_fail "--json-out produced an empty file"; }
fi

# --- verdict --------------------------------------------------------------
# SELF never fails: the collector's own job cannot be terminal while the
# collector runs. Everything else keeps the strict fail-closed semantics.
if [ "$HARD_ERROR" -ne 0 ] || [ "$MISSING" -gt 0 ] || [ "$IN_PROGRESS" -gt 0 ]; then
  diag "verdict: FAIL (hard_error=$HARD_ERROR, missing=$MISSING, in_progress=$IN_PROGRESS, self=$SELF_IN_PROGRESS, captured=$CAPTURED, never_started=$NEVER_STARTED)"
  echo "consolidate-run-log: INCOMPLETE report written to $OUT (hard_error=$HARD_ERROR missing=$MISSING in_progress=$IN_PROGRESS self=$SELF_IN_PROGRESS captured=$CAPTURED)" >&2
  exit 1
fi
diag "verdict: OK (captured=$CAPTURED, never_started=$NEVER_STARTED, self=$SELF_IN_PROGRESS)"
echo "consolidate-run-log: complete report ($CAPTURED jobs captured, $SELF_IN_PROGRESS self) -> $OUT"
exit 0
