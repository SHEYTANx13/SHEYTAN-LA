#!/usr/bin/env python3
"""Parse a `go build -json` JSONL stream and surface the REAL failure.

Work order section 6.2 (run 44663205241 repair): the previous failure
handler ran

    cat codeql-go-build-a.jsonl | grep -m1 -E '"ImportPath|Err"'

which matches the FIRST metadata line of the stream (an "ImportPath"
line of the first compiled package) instead of the actual build-fail
event and its compiler output - so source locations and diagnostics
disappeared from the job log exactly when they mattered.

`go build -json` emits one JSON object per line, shaped like:

    {"ImportPath": "pkg", "Action": "build-output", "Output": "# pkg\\n"}
    {"ImportPath": "pkg", "Action": "build-fail"}

Compiler/linker diagnostics ride on build-output events of the package
whose build ultimately fails (Action == "build-fail"). This script:

  1. parses EVERY line as JSON (never regex-scans raw JSONL);
  2. collects the build-output text per package in stream order;
  3. reports each build-failed package with its FULL compiler output
     (exact text, including `path/file.go:line:col: diagnostic` lines
     and linker output), first failing package first;
  4. prints a concise FIRST-FAILURE summary: package, first diagnostic
     line, and the count of failed packages;
  5. never swallows malformed lines: they are reported, not dropped.

Exit code is ALWAYS 0 - the caller decides failure (the workflow step
runs this on its `||` path and then exits 1 itself). The full raw JSONL
stays the job's evidence; this script only adds the concise
first-failure entry to the log.

Summary mode (--summary) prints, per stream, the number of DISTINCT
packages that produced build events (build-output or build-fail).
`go build -json` emits events ONLY for packages that produce output or
fail - silent successful compiles produce no event - so this count is
an evidence marker that the traced build ran and where its visible
events came from, NOT a count of every compiled package. Exit code is
always 0 in summary mode.
"""

import argparse
import json
import re
import sys

# A Go compiler/linker diagnostic location line:
#   path/to/file.go:line:col: diagnostic
#   path/to/file.go:line: diagnostic
# also CGo headers: include chain lines start with '#'.
DIAGNOSTIC_RE = re.compile(r"\S+\.(go|c|h|s|S):\d+(:\d+)?:\s")


def read_stream(path):
    """Yield (lineno, parsed_or_None, raw_line) for every line."""
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        for lineno, raw in enumerate(fh, start=1):
            line = raw.rstrip("\n")
            if not line.strip():
                continue
            try:
                yield lineno, json.loads(line), line
            except json.JSONDecodeError:
                yield lineno, None, line


def collect(path):
    """Return (failed_packages ordered, outputs per package, malformed lines)."""
    outputs = {}
    failed_order = []
    failed_set = set()
    malformed = []
    for lineno, event, raw in read_stream(path):
        if event is None:
            malformed.append((lineno, raw[:200]))
            continue
        if not isinstance(event, dict):
            malformed.append((lineno, raw[:200]))
            continue
        pkg = event.get("ImportPath") or "<unknown>"
        action = event.get("Action")
        if action == "build-output":
            outputs.setdefault(pkg, []).append(event.get("Output") or "")
        elif action == "build-fail":
            if pkg not in failed_set:
                failed_set.add(pkg)
                failed_order.append(pkg)
    return failed_order, outputs, malformed


def first_diagnostic(text):
    """The first line carrying a source location, else first non-# line."""
    for line in text.splitlines():
        if DIAGNOSTIC_RE.search(line):
            return line
    for line in text.splitlines():
        s = line.strip()
        if s and not s.startswith("#"):
            return s
    return "(no compiler output captured)"


def report_failures(paths):
    any_fail = False
    for path in paths:
        failed_order, outputs, malformed = collect(path)
        print(f"=== go build -json stream: {path}")
        if malformed:
            print(f"  non-JSON lines (reported, never dropped): {len(malformed)}")
            for lineno, snippet in malformed[:5]:
                print(f"    line {lineno}: {snippet}")
        if not failed_order:
            print("  no build-fail events in this stream")
            continue
        any_fail = True
        print(f"  build-failed packages: {len(failed_order)}")
        for pkg in failed_order:
            text = "".join(outputs.get(pkg, []))
            print(f"  --- FAILED: {pkg}")
            print(f"  first diagnostic: {first_diagnostic(text)}")
            if text.strip():
                print("  full compiler output (exact):")
                for line in text.splitlines():
                    print(f"    | {line}")
            else:
                print("    | (no build-output events captured for this package)")
    if any_fail:
        print(
            "go build -json: report complete. The full raw JSONL stream is "
            "preserved as the job's evidence; the exit decision belongs to the caller."
        )


def report_summary(paths):
    print("=== go build -json stream evidence")
    print(
    "    (events exist only for packages that produced output or failed;\n"
    "     silent successful compiles emit no event - this is NOT a count\n"
    "     of every compiled package)"
    )
    for path in paths:
        packages = set()
        for _, event, _ in read_stream(path):
            if event and isinstance(event, dict) and event.get("ImportPath"):
                packages.add(event["ImportPath"])
        print(f"  {path}: {len(packages)} distinct packages produced build events")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("files", nargs="+", help="go build -json JSONL stream files")
    parser.add_argument(
        "--summary",
        action="store_true",
        help="print per-stream package-capture counts instead of failure details",
    )
    args = parser.parse_args()
    if args.summary:
        report_summary(args.files)
    else:
        report_failures(args.files)
    return 0


if __name__ == "__main__":
    sys.exit(main())
