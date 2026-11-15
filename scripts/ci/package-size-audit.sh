#!/bin/sh
# package-size-audit.sh — v1.11.1 maintenance (run 43718750096 work order
# §7.3): the hard 100,000,000-byte (100 MB decimal) PER-FILE ceiling for
# every GitHub build/release payload.
#
# Policy (mirrored by internal/releasecontract/oversized_asset_policy_test.go,
# which pins this script's presence in BOTH packaging lanes):
#   - The threshold is an UNCOMPRESSED byte size. Compressed size alone
#     never qualifies a large file as compliant — a 250 MB model weight
#     that compresses to 60 MB is still a violation.
#   - Exactly 100,000,000 bytes is NOT greater than the threshold and is
#     allowed (margin is still preferred).
#   - A file above the threshold is never permitted inside a build
#     artifact, portable ZIP, installer payload or source ZIP. Optional
#     oversized assets (model weights, engine archives, backend runtime
#     bundles) belong in the in-app managed-downloads flow
#     (internal/downloader + internal/updater), never in the package.
#   - The gate NEVER truncates, stubs or silently drops a violating file:
#     it fails closed with the exact path and size so a human classifies
#     it (required runtime component => packaging architecture must be
#     reworked; optional asset => move to managed downloads).
#
# Usage:
#   scripts/ci/package-size-audit.sh --zip <archive.zip> [--zip <archive2.zip>]
#                                    [--dir <staging-dir>] [--limit <bytes>]
#
#   --zip   audit every archive member's UNCOMPRESSED size (unzip -l).
#   --dir   audit every regular file under a staging directory (the
#           pre-zip scan: staged files are exactly what the portable ZIP
#           and the NSIS installer payload will contain).
#   --limit override the threshold — RESERVED FOR THE CONTRACT TEST that
#           proves the fail-closed path with tiny fixtures; the default
#           stays hard-pinned at 100000000 and that literal is asserted by
#           the release contract (do not raise it to make a package pass).
#
# Output is plain text on stdout (the lanes capture it into
# ci-logs/<lane>-size-audit.log, so it reaches the consolidated run-log
# artifact). Exit 0 = compliant; 1 = violation or usage/IO error.
#
# POSIX sh on purpose (same dialect as consolidate-run-log.sh). Staging
# trees in this repository never contain newlines in filenames — the same
# assumption the lanes' own zip -r / Compress-Archive steps make.
set -u

LIMIT=100000000
ZIPS=""
DIRS=""

usage() {
  sed -n '2,32p' "$0" >&2
  exit 1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --zip)
      [ $# -ge 2 ] || usage
      ZIPS="$ZIPS $2"; shift 2 ;;
    --dir)
      [ $# -ge 2 ] || usage
      DIRS="$DIRS $2"; shift 2 ;;
    --limit)
      [ $# -ge 2 ] || usage
      case "$2" in ''|*[!0-9]*) usage ;; esac
      LIMIT="$2"; shift 2 ;;
    *) usage ;;
  esac
done

[ -n "${ZIPS}${DIRS}" ] || usage

VIOLATIONS=0

# audit_one <kind> <size> <path>: single violation check.
audit_one() {
  if [ "$2" -gt "$LIMIT" ]; then
    echo "::error::size-audit violation: $3 is $2 bytes (limit $LIMIT bytes, UNCOMPRESSED) — $1"
    VIOLATIONS=$((VIOLATIONS + 1))
  fi
}

for d in $DIRS; do
  if [ ! -d "$d" ]; then
    echo "::error::size-audit: staging directory missing: $d"
    exit 1
  fi
  files=0; total=0; max=0; maxpath=""
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    s=$(wc -c < "$f" 2>/dev/null) || { echo "::error::size-audit: cannot stat $f"; exit 1; }
    files=$((files + 1)); total=$((total + s))
    if [ "$s" -gt "$max" ]; then max=$s; maxpath=$f; fi
    audit_one "staged file" "$s" "$f"
  done <<EOF
$(find "$d" -type f 2>/dev/null)
EOF
  echo "size-audit dir OK: $d — $files files, $total bytes total, largest member $maxpath ($max bytes), limit $LIMIT"
done

for z in $ZIPS; do
  if [ ! -f "$z" ]; then
    echo "::error::size-audit: archive missing: $z"
    exit 1
  fi
  if ! command -v unzip >/dev/null 2>&1; then
    echo "::error::size-audit: unzip is required to inspect archive members (not installed on this host)"
    exit 1
  fi
  # unzip -l lists every member with its UNCOMPRESSED length. The awk
  # filter keeps only member rows (first field all digits, second field a
  # date, at least 4 fields) and drops the header/footer noise lines.
  members=$(unzip -l "$z" | awk '$1 ~ /^[0-9]+$/ && $2 ~ /^[0-9\/-]+$/ && NF >= 4')
  files=0; total=0; max=0; maxpath=""
  while IFS= read -r row; do
    [ -n "$row" ] || continue
    s=$(echo "$row" | awk '{print $1}')
    name=$(echo "$row" | awk '{ $1=$2=$3=""; sub(/^[ \t]+/, ""); print }')
    case "$s" in ''|*[!0-9]*)
      echo "::error::size-audit: unparseable member row in $z: $row"
      exit 1 ;; esac
    files=$((files + 1)); total=$((total + s))
    if [ "$s" -gt "$max" ]; then max=$s; maxpath=$name; fi
    audit_one "archive member (uncompressed)" "$s" "$z :: $name"
  done <<EOF
$members
EOF
  zsize=$(wc -c < "$z" 2>/dev/null) || zsize=0
  ratio=$(awk -v t="$total" -v c="$zsize" 'BEGIN { if (c > 0) printf "%.1f%%", (t * 100.0) / c; else print "n/a" }')
  echo "size-audit zip OK: $z — $files members, $total bytes uncompressed, archive $zsize bytes compressed (ratio $ratio), largest member $maxpath ($max bytes), limit $LIMIT"
done

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "::error::package size gate FAILED: $VIOLATIONS file(s) exceed the $LIMIT-byte per-file ceiling — classify each (required runtime component vs optional downloadable asset) and fix the packaging; never truncate or silently drop"
  exit 1
fi
echo "package size gate: PASS (limit $LIMIT bytes per file, uncompressed)"
exit 0
