#!/usr/bin/env bash
# diagnostics-index.sh — the per-job machine-readable diagnostics index
# (work order for run 43008361835 §F6/§5.6).
#
# Every job tees its costly commands into ci-logs/*.log and records the gate
# sequence in ci-logs/gate-order.txt. This builder, run as the job's FINAL
# step with if: always(), consolidates that evidence into ONE
# ci-logs/diagnostics-index.json and prints it to the step log so it also
# reaches the consolidated run-log artifact:
#
#   job          — the job display name (argument)
#   runner       — OS/image/arch facts (allowlisted env only)
#   go           — go version + GOMODCACHE/GOCACHE (normalized, no secrets)
#   cache        — setup-go cache-hit output when provided
#   generated    — UTC build time
#   gates        — the RUN/gate-OK sequence from gate-order.txt; a RUN line
#                  without a matching "gate OK" is reported not_completed
#                  (red or cancelled — never converted to a pass)
#   logs         — per-file name, bytes, sha256, mtime (UTC)
#
# Discipline (§F6): NO tokens, NO environment dumps, NO user paths, NO
# model contents, NO signed URLs — only the allowlisted fields above.
# Pure bash + coreutils (runs on ubuntu runners and the Windows job's Git
# Bash alike; no jq dependency). CRLF in pwsh-written files is tolerated.
#
# Self-test: --self-test builds a synthetic ci-logs tree, generates the
# index and validates the JSON with python3 (available on all runners).
set -uo pipefail

JOB_NAME="unknown-job"
MODE="build"
while [ $# -gt 0 ]; do
  case "$1" in
    --job) JOB_NAME="$2"; shift 2;;
    --self-test) MODE="self-test"; shift;;
    *) echo "usage: $0 --job NAME [--self-test]" >&2; exit 64;;
  esac
done

json_escape() {  # escape a string for a JSON literal (quotes, backslash, control chars)
  printf '%s' "$1" | python3 -c 'import sys,json; sys.stdout.write(json.dumps(sys.stdin.read()))' 2>/dev/null \
    || printf '"%s"' "$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\r\n')"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    sha256 -r "$1" 2>/dev/null | cut -d' ' -f1 || echo "unavailable"
  fi
}

build_index() {  # $1 = ci-logs dir, $2 = job name, $3 = cache-hit value ("" = unknown)
  local dir="$1" job="$2" cache_hit="$3"
  local gate_order="$dir/gate-order.txt"

  local go_version="" gomodcache="" gocache=""
  if command -v go >/dev/null 2>&1; then
    go_version="$(go version 2>/dev/null | head -1)"
    gomodcache="$(go env GOMODCACHE 2>/dev/null | sed 's#^.*/pkg/mod$#<gomodcache>#')"
    gocache="$(go env GOCACHE 2>/dev/null | sed 's#^.*/go-build$#<gocache>#')"
  fi

  local cache_json="null"
  if [ -n "$cache_hit" ]; then
    cache_json="$(json_escape "$cache_hit")"
  fi

  {
    printf '{\n'
    printf '  "schema": "sheytan-ci-diagnostics/1",\n'
    printf '  "job": %s,\n' "$(json_escape "$job")"
    printf '  "runner": {"os": %s, "image": %s, "arch": %s},\n' \
      "$(json_escape "${RUNNER_OS:-unknown}")" \
      "$(json_escape "${ImageOS:-${RUNNER_ENVIRONMENT:-unknown}}")" \
      "$(json_escape "$(uname -m 2>/dev/null || echo unknown)")"
    printf '  "go": {"version": %s, "modcache": %s, "buildcache": %s},\n' \
      "$(json_escape "$go_version")" "$(json_escape "$gomodcache")" "$(json_escape "$gocache")"
    printf '  "cache_hit": %s,\n' "$cache_json"
    printf '  "generated_utc": "%s",\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

    # Gates: RUN ... followed by "gate OK: ..." completes a gate; anything
    # still open at collection time is reported not_completed.
    printf '  "gates": ['
    local first=1
    if [ -f "$gate_order" ]; then
      local current=""
      while IFS= read -r line || [ -n "$line" ]; do
        line="$(printf '%s' "$line" | tr -d '\r')"
        case "$line" in
          "RUN "*)
            if [ -n "$current" ]; then
              [ $first -eq 0 ] && printf ','
              first=0
              printf '\n    {"command": %s, "status": "not_completed"}' "$(json_escape "$current")"
            fi
            current="${line#RUN }"
            ;;
          "gate OK: "*)
            if [ -n "$current" ]; then
              [ $first -eq 0 ] && printf ','
              first=0
              printf '\n    {"command": %s, "status": "ok"}' "$(json_escape "$current")"
              current=""
            fi
            ;;
        esac
      done < "$gate_order"
      if [ -n "$current" ]; then
        [ $first -eq 0 ] && printf ','
        printf '\n    {"command": %s, "status": "not_completed"}' "$(json_escape "$current")"
      fi
    fi
    printf '\n  ],\n'

    # Log inventory: name, bytes, sha256, mtime — the exact files the
    # step logs teed (their content is preserved verbatim in the
    # consolidated run-log artifact).
    printf '  "logs": ['
    local f name
    first=1
    for f in "$dir"/*.log "$dir"/gate-order.txt; do
      [ -f "$f" ] || continue
      name="$(basename "$f")"
      [ $first -eq 0 ] && printf ','
      first=0
      printf '\n    {"name": %s, "bytes": %s, "sha256": "%s", "mtime_utc": "%s"}' \
        "$(json_escape "$name")" \
        "$(wc -c < "$f" | tr -d ' ')" \
        "$(sha256_file "$f")" \
        "$(date -u -r "$f" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo unknown)"
    done
    printf '\n  ]\n'
    printf '}\n'
  }
}

case "$MODE" in
  self-test)
    work="$(mktemp -d)"
    trap 'rm -rf "$work"' EXIT
    mkdir -p "$work/ci-logs"
    printf 'RUN go build ./...\ngate OK: go build ./...\nRUN go test ./internal/... (headless)\n' > "$work/ci-logs/gate-order.txt"
    printf 'toolchain: go1.27.4\nfiles:\ngofmt OK\n' > "$work/ci-logs/preflight-gofmt.log"
    printf 'token=ghp_abcdefghijklmnopqrstuvwxyz123456\n' > "$work/ci-logs/seeded-secret.log"
    out="$work/ci-logs/diagnostics-index.json"
    build_index "$work/ci-logs" "self-test-job" "true" > "$out"
    python3 - "$out" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
assert doc["schema"] == "sheytan-ci-diagnostics/1", doc
assert doc["job"] == "self-test-job"
assert doc["cache_hit"] == "true"
gates = {g["command"]: g["status"] for g in doc["gates"]}
assert gates["go build ./..."] == "ok", gates
assert gates["go test ./internal/... (headless)"] == "not_completed", gates
names = [l["name"] for l in doc["logs"]]
assert "preflight-gofmt.log" in names and "gate-order.txt" in names, names
by_name = {l["name"]: l for l in doc["logs"]}
assert by_name["preflight-gofmt.log"]["bytes"] > 0
assert len(by_name["preflight-gofmt.log"]["sha256"]) == 64
print("diagnostics-index self-test: JSON schema, gate statuses, log inventory OK")
PY
    # Redaction discipline: the seeded token must never reach the index.
    if grep -q 'ghp_abcdefghijklmnopqrstuvwxyz123456' "$out"; then
      echo "::error::diagnostics-index self-test: seeded token leaked into the index" >&2
      exit 1
    fi
    echo "diagnostics-index self-test: secret redaction OK"
    ;;
  build)
    cd "${GITHUB_WORKSPACE:-.}" || exit 1
    out="ci-logs/diagnostics-index.json"
    mkdir -p ci-logs
    build_index "ci-logs" "$JOB_NAME" "${DIAGNOSTICS_CACHE_HIT:-}" > "$out"
    cat "$out"
    ;;
esac
