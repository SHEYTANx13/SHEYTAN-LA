#!/usr/bin/env bash
# scripts/build/linux.sh — the ONE authoritative Linux x64 build lane
# (CI consolidation: the retired .github/workflows/build-linux.yml
# reusable lane, now dispatched per-phase by the platform-build matrix
# job of .github/workflows/build.yml).
#
# Contract carried over from build-linux.yml + the retired audit job:
#   - every verification gate writes the upstream-failure marker
#     ci-logs/linux-x64-upstream-failure.txt (first failed gate's step
#     name) before failing, so the if:always() package-gate root-cause
#     reporter names ONE root error (work order §4.3.4/§4.3.5);
#   - the headless core suite always passes -json so the consolidated
#     run-log collector's stateful `go test -json` classifier works;
#   - the audit-job essentials (race gate, headless stress + release
#     surface, collector fixtures, diagnostics self-test, file-catalog
#     freshness) run here on the same ubuntu-26.04 host;
#   - packaging is portable-archive-only: no DEB/RPM installer is
#     produced (master work order §10).
#
# Usage: linux.sh <phase>
#   gofmt | headless-tests | release-contract | race | stress | fixtures |
#   catalog-freshness | version | prereqs | ui-tests | build |
#   build-info-probe | verify-elf | smoke | build-info | archive |
#   verify-archive
set -euo pipefail

TARGET="linux-x64"
MARKER="ci-logs/${TARGET}-upstream-failure.txt"
GO_TEST_TIMEOUT="${GO_TEST_TIMEOUT:-25m}"
APP_VERSION="${APP_VERSION:?APP_VERSION must be provided by the workflow env}"
LINUX_PKG_ROOT="${LINUX_PKG_ROOT:?LINUX_PKG_ROOT must be provided by the workflow env}"
STAGE="dist/SHEYTAN-LA-ALPHA-linux-x64-portable"
ARCHIVE="dist/SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz"

mkdir -p ci-logs

# fail_gate writes the upstream-failure marker (step name) and exits 1 —
# the exact bash marker pattern the platform-build contract requires:
#   if ! <cmd>; then echo "::error::..."; echo "<step name>" > marker; exit 1; fi
fail_gate() { # $1 = step name, $2 = human message
  echo "::error::$2"
  echo "$1" > "$MARKER"
  exit 1
}

fail_gate() { # $1 = step name, $2 = human message
  echo "::error::$2"
  echo "$1" > "$MARKER"
  exit 1
}

# summarize_headless_failure (run 45424141601 repair — lane parity with
# macos.sh): a bare exit code is not a diagnosis. Extracts the suite's
# OWN root-cause records (FAIL frames, panics, timeouts) AND the failing
# tests' message lines (file.go:NN: ...) plus the darwin walk's walk-diag
# stage evidence from the tee'd -json stream, bounded to 15 each, so the
# annotation channel names the failing package, test and REASON - a
# repair session without step-log auth still gets the why.
summarize_headless_failure() { # $1 = tee'd stream log, $2 = platform label
  local stream="$1" label="$2"
  [ -f "$stream" ] || return 0
  echo "::error::headless suite failure evidence (${label}): the suite's own root-cause records follow (bounded to 15; the full -json stream is in the step log)"
  # Run 45522548507 repair (verdict-aware messages): the run's windows
  # lane proved the previous message selector grabs SKIP messages and
  # success-path t.Logf lines when it reads the whole stream verdict-
  # blind - 8 of its 9 recorded "failure messages" described tests that
  # never failed. The failed test names are collected from the -json fail
  # events FIRST (an output event can echo the literal string, so fail
  # events are the lines that are NOT output events), and only those
  # tests' message lines are emitted - messages before frames, so the
  # consolidated channel's bounded per-lane budget carries the WHY and
  # not just the frame list. POSIX-safe awk only ([ \t], no char classes)
  # because these lanes run BSD awk and mawk alike.
  awk '
    FNR == NR {
      if ($0 ~ /"Action":"fail"/ && $0 !~ /"Action":"output"/) {
        if (match($0, /"Test":"[^"]+"/)) failed[substr($0, RSTART + 8, RLENGTH - 9)] = 1
        else failed[""] = 1
      }
      next
    }
    emitted >= 15 { next }
    $0 ~ /(walk-diag |[ \t][A-Za-z0-9_\/]+\.go:[0-9]+: )/ && $0 !~ /\.go:[0-9]+: (SKIP|skip)/ {
      name = ""
      if (match($0, /"Test":"[^"]+"/)) name = substr($0, RSTART + 8, RLENGTH - 9)
      if (name in failed) { print; emitted++ }
    }
  ' "$stream" "$stream" | head -n 15 | while IFS= read -r ev; do
    echo "::error::${label} failure message: ${ev}"
  done
  grep -n -E '(--- FAIL:|panic: |fatal error: |^FAIL|test timed out|signal: [A-Z]+)' "$stream" | head -n 15 | while IFS= read -r ev; do
    echo "::error::${label} suite evidence: ${ev}"
  done
}

phase="${1:-}"

case "$phase" in
  gofmt)
    # SCOPE PARITY: the ONE canonical scope, identical to every lane's
    # gofmt gate: cmd internal ui scripts.
    unformatted="$(gofmt -l cmd internal ui scripts)" || fail_gate "gofmt (enforced)" "gofmt invocation failed"
    if [ -n "$unformatted" ]; then
      printf 'toolchain: %s\nfiles:\n%s\n' "$(go version)" "$unformatted" | tee ci-logs/linux-gofmt.log
      fail_gate "gofmt (enforced)" "gofmt violations — run gofmt -w on the files in ci-logs/linux-gofmt.log"
    fi
    echo "gofmt OK"
    ;;

  headless-tests)
    # Run 45165536396 repair: the gate must never download modules
    # lazily — the same bounded-retry prefetch every lane runs (the
    # macos-x64 lane's lazy proxy fetches timed out mid-gate and failed
    # the suite as an opaque build failure).
    if ! bash scripts/ci/module-preflight.sh "linux"; then
      fail_gate "Module prefetch (linux/amd64)" "module prefetch failed on linux/amd64 — the exact proxy error is in ci-logs/linux-module-preflight.log and in this step log"
    fi
    # -json is CONTRACTUAL: the consolidated run-log collector's stateful
    # `go test -json` classifier (run 44457547289 3.4) needs the stream.
    echo "RUN go test ./internal/... -tags headless -json (linux/amd64)" >> ci-logs/gate-order.txt
    if ! go test ./internal/... -tags headless -json -count=1 -timeout "${GO_TEST_TIMEOUT}" 2>&1 | tee ci-logs/linux-go-test-headless.log; then
      summarize_headless_failure ci-logs/linux-go-test-headless.log "linux/amd64"
      fail_gate "Headless core suite (linux/amd64)" "headless core tests failed on linux/amd64 (failing tests in ci-logs/linux-go-test-headless.log)"
    fi
    echo "gate OK: headless core suite (linux/amd64)" >> ci-logs/gate-order.txt
    ;;

  release-contract)
    if ! go test ./internal/releasecontract/ -count=1 2>&1 | tee ci-logs/linux-release-contract.log; then
      fail_gate "Release-contract suite (linux/amd64)" "release-contract tests failed on linux/amd64"
    fi
    ;;

  race)
    # Audit-job essential: the race gates on the critical concurrent
    # packages (the same set the retired Core audit job ran).
    if ! go test -race -tags headless -count=1 ./internal/api ./internal/agent ./internal/sessions ./internal/contextplan ./internal/histref ./internal/runtime ./internal/localipc 2>&1 | tee ci-logs/linux-go-race.log; then
      fail_gate "Race gate (critical concurrent packages)" "go test -race failed on the critical concurrent packages"
    fi
    ;;

  stress)
    # Audit-job essential: the stress + release-surface gate runs HEADLESS
    # (release-contract assertion, cmd/stress_release_surface.go 3l).
    if ! go run -tags headless ./cmd/sheytan-local-agent stress 2>&1 | tee ci-logs/linux-stress-surface.log; then
      fail_gate "Stress + release-surface gate (headless)" "the stress suite (release_surface scenario) failed"
    fi
    ;;

  fixtures)
    # Audit-job essential: the consolidated-log collector fixture suite
    # and the diagnostics-index self-test (regression-proof the primary
    # deliverable's construction logic on every run).
    if ! bash scripts/ci/consolidate-run-log-fixtures.sh 2>&1 | tee ci-logs/linux-log-fixtures.log; then
      fail_gate "Consolidated-log collector fixtures" "the collector fixture suite reported failures"
    fi
    if ! bash scripts/ci/diagnostics-index.sh --self-test 2>&1 | tee ci-logs/linux-diagnostics-selftest.log; then
      fail_gate "Diagnostics-index fixture (self-test)" "the diagnostics-index self-test failed"
    fi
    ;;

  catalog-freshness)
    # Audit-job essential: regenerating the FILE_CATALOG from the tracked
    # tree must be a byte-level no-op (any diff = a tracked file was
    # added/renamed/deleted without the final regeneration step).
    if ! go run ./scripts/gen-filecatalog 2>&1 | tee ci-logs/linux-catalog-freshness.log; then
      fail_gate "File-catalog freshness gate (regeneration is a no-op)" "the catalog generator failed"
    fi
    if ! git diff --exit-code -- docs/architecture/FILE_CATALOG.json docs/architecture/FILE_CATALOG.md > ci-logs/linux-catalog-freshness.diff; then
      echo "::error::the checked-in FILE_CATALOG is stale — regenerating it from the tracked tree produced a diff (ci-logs/linux-catalog-freshness.diff). Run \`go run ./scripts/gen-filecatalog\` after the final source/test/workflow/doc edit and commit both artifacts."
      sed -n '1,60p' ci-logs/linux-catalog-freshness.diff || true
      fail_gate "File-catalog freshness gate (regeneration is a no-op)" "FILE_CATALOG regeneration is not a no-op against the tracked tree"
    fi
    ;;

  version)
    # APP_VERSION derives from the ONE Go authority (internal/config
    # AppVersion) via the CGO-free resolver (run #54 root cause: the
    # full agent binary needs the Fyne CGO toolchain, which the version
    # step must never require).
    set +e
    raw="$(CGO_ENABLED=0 go run ./cmd/printversion --plain 2>&1)"
    rc=$?
    set -e
    printf '%s\n' "EXIT_CODE: $rc" "$raw" | tee ci-logs/linux-version-resolution.log
    if [ $rc -ne 0 ]; then
      fail_gate "Resolve APP_VERSION from the Go authority" "printversion exited $rc — version resolution failed (raw output above)"
    fi
    resolved="$(printf '%s' "$raw" | tr -d '[:space:]')"
    if ! [[ "$resolved" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      fail_gate "Resolve APP_VERSION from the Go authority" "printversion produced a malformed version: '$resolved'"
    fi
    if [ "$resolved" != "$APP_VERSION" ]; then
      fail_gate "Resolve APP_VERSION from the Go authority" "workflow APP_VERSION ($APP_VERSION) disagrees with internal/config.AppVersion ($resolved)"
    fi
    echo "APP_VERSION = $resolved (matches internal/config.AppVersion)"
    ;;

  prereqs)
    # The HONEST dependency contract for the WebKitGTK-backed web-shell CGO desktop
    # build + the Xvfb smoke (no Qt, no Node, no GTK4; webkit2gtk-4.1 is
    # the WebKit backend the web workspace v3 runtime links on Linux).
    sudo apt-get update -q
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q \
      gcc pkg-config \
      libgtk-3-dev libwebkit2gtk-4.1-dev libglib2.0-dev \
      libgl1-mesa-dev libegl1-mesa-dev \
      libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev \
      libwayland-dev libxkbcommon-dev \
      xorg-dev xauth xvfb x11-utils zip unzip
    echo "webkit2gtk-4.1 pkg-config version: $(pkg-config --modversion webkit2gtk-4.1)"
    echo "gl pkg-config version: $(pkg-config --modversion gl)"
    ;;

  ui-tests)
    # The UI suite runs ONLY on the linux-x64 lane (the ubuntu host has
    # Xvfb available for the Fyne gates).
    if ! go test ./ui/... -count=1 -timeout "${GO_TEST_TIMEOUT}" 2>&1 | tee ci-logs/linux-ui-tests.log; then
      fail_gate "Fyne UI suite (linux/amd64)" "the Fyne UI suite failed on linux/amd64"
    fi
    ;;

  build)
    mkdir -p "${STAGE}/${LINUX_PKG_ROOT}"
    # Run 44547031698 §3.2: the pinned goffi v0.6.8 documents that a
    # binary containing goffi AND other cgo code (go-gl, purego, the
    # WebKitGTK backend) fails to link on Linux with "unhandled
    # relocation ... (SDYNIMPORT)" (goffi issue #72; macOS and Windows
    # are not affected). The Linux desktop build therefore EXCLUDES the
    # gogpu adapter through the -tags nogogpu seam (internal/desktop:
    # the render-verification wiring publishes the honest not-wired
    # observation) and ships the web-shell primary window. The combined
    # gogpu link regression proof runs on the macOS/Windows lanes, where
    # upstream documents the combination as supported.
    if ! go build -trimpath -tags nogogpu,webworkspace -o "${STAGE}/${LINUX_PKG_ROOT}/SHEYTAN-LA" ./cmd/sheytan-local-agent; then
      fail_gate "Build the full desktop application (native linux/amd64)" "the linux/amd64 desktop build failed"
    fi
    chmod +x "${STAGE}/${LINUX_PKG_ROOT}/SHEYTAN-LA"
    # Run 44547031698 §3.7.4/§3.7.5: the combined go-gui+gogpu link
    # regression proof. ONE binary imports BOTH stacks (purego + goffi);
    # under CGO_ENABLED=0 their fakecgo copies duplicate _cgo_init unless
    # goffi's documented Workaround A (-tags nofakecgo) is applied. The
    # probe links and runs here on every CI build so the documented
    # configuration can never silently regress. (The CGO=1 desktop binary
    # itself carries fyne+gogpu on the macOS/Windows lanes, where goffi
    # documents the combination as supported.)
    if ! CGO_ENABLED=0 go build -tags nofakecgo -o dist/linux/sheytan-renderprobe ./cmd/sheytan-renderprobe; then
      fail_gate "Build the full desktop application (native linux/amd64)" "the combined go-gui+gogpu link probe failed (CGO_ENABLED=0 -tags nofakecgo)"
    fi
    if ! dist/linux/sheytan-renderprobe > dist/linux/renderprobe.log 2>&1; then
      fail_gate "Build the full desktop application (native linux/amd64)" "the combined-link probe binary did not run cleanly (see dist/linux/renderprobe.log)"
    fi
    echo "combined go-gui+gogpu link probe: $(cat dist/linux/renderprobe.log | tail -1)"
    ;;

  build-info-probe)
    # Work order §4.3: resolve the ABSOLUTE canonical path and verify it
    # is executable BEFORE changing into the isolated probe directory
    # (a relative path after cd is exactly the run-443 Linux exit 127).
    repoDir="$(pwd)"
    bin="$(realpath "${repoDir}/${STAGE}/${LINUX_PKG_ROOT}/SHEYTAN-LA")"
    test -x "$bin" || fail_gate "Cross-check the compiled binary's build identity" "built executable missing or not executable: $bin"
    probeDir="$(mktemp -d "${RUNNER_TEMP:-/tmp}/build-info-probe.XXXXXX")"
    cd "$probeDir"
    set +e
    raw="$("$bin" build-info 2>/dev/null)"
    rc=$?
    set -e
    printf 'compiled build-info reports (exit %s): %s\n' "$rc" "$raw" | tee "${repoDir}/ci-logs/linux-build-info.log"
    if [ $rc -ne 0 ] || [ -z "$raw" ]; then
      fail_gate "Cross-check the compiled binary's build identity" "the compiled SHEYTAN-LA build-info probe exited $rc or produced no output"
    fi
    stage="$(printf '%s' "$raw" | sed -n 's/.*"product_stage":"\([^"]*\)".*/\1/p')"
    build_id="$(printf '%s' "$raw" | sed -n 's/.*"build_id":"\([^"]*\)".*/\1/p')"
    if [ -z "$stage" ]; then
      fail_gate "Cross-check the compiled binary's build identity" "build-info carries no product_stage field"
    fi
    if [ "$stage" != "ALPHA" ]; then
      fail_gate "Cross-check the compiled binary's build identity" "compiled product_stage is '$stage', want the public ALPHA identity (master work order §12.1)"
    fi
    if [ -z "$build_id" ]; then
      fail_gate "Cross-check the compiled binary's build identity" "build-info carries no build_id field"
    fi
    if [ "$build_id" != "$APP_VERSION" ]; then
      fail_gate "Cross-check the compiled binary's build identity" "compiled build_id ($build_id) disagrees with the internal authority APP_VERSION ($APP_VERSION)"
    fi
    residue="$(ls -A "$probeDir" || true)"
    if [ -n "$residue" ]; then
      fail_gate "Cross-check the compiled binary's build identity" "build-info probe created filesystem residue in the isolated probe dir: $residue (the identity dispatch must stay read-only)"
    fi
    echo "compiled build identity OK: product_stage=$stage (public) build_id=$build_id (internal) — probe directory left clean"
    ;;

  verify-elf)
    bin="${STAGE}/${LINUX_PKG_ROOT}/SHEYTAN-LA"
    test -x "$bin" || fail_gate "Verify the ELF (x86-64, executable bit)" "the Linux binary lost its executable bit"
    file "$bin" | tee ci-logs/linux-elf-check.log
    file "$bin" | grep -q "ELF 64-bit LSB.*x86-64" || fail_gate "Verify the ELF (x86-64, executable bit)" "not an x86-64 ELF binary"
    ;;

  smoke)
    # Bounded observable readiness, soak, real SIGTERM with zombie-safe
    # reaping (the same smoke contract the retired ui job + Linux lane ran).
    work="$RUNNER_TEMP/linux-smoke"
    mkdir -p "$work" && cp "${STAGE}/${LINUX_PKG_ROOT}/SHEYTAN-LA" "$work/"
    cd "$work"
    Xvfb :92 -screen 0 1280x800x24 >/dev/null 2>&1 &
    XVFB_PID=$!
    xvfb_ready=0
    for i in $(seq 1 20); do
      if DISPLAY=:92 xdpyinfo >/dev/null 2>&1; then xvfb_ready=1; break; fi
      sleep 0.5
    done
    if [ "$xvfb_ready" -ne 1 ]; then
      fail_gate "Launch smoke (Xvfb: boot, window, clean shutdown)" "desktop smoke: Xvfb never became ready within 10s"
    fi
    DISPLAY=:92 ./SHEYTAN-LA > smoke-stdout.log 2>&1 &
    APP_PID=$!
    app_ready=0
    for i in $(seq 1 60); do
      if ! kill -0 "$APP_PID" 2>/dev/null; then break; fi
      if DISPLAY=:92 xwininfo -root -tree 2>/dev/null | grep -q "SHEYTAN-Local-Agent"; then
        app_ready=1
        break
      fi
      sleep 0.5
    done
    if [ "$app_ready" -ne 1 ]; then
      cat smoke-stdout.log || true
      kill "$XVFB_PID" 2>/dev/null || true
      fail_gate "Launch smoke (Xvfb: boot, window, clean shutdown)" "desktop smoke: the application window was never observable within 30s"
    fi
    soak_ok=1
    for i in $(seq 1 20); do
      if ! kill -0 "$APP_PID" 2>/dev/null; then soak_ok=0; break; fi
      sleep 0.5
    done
    if [ "$soak_ok" -ne 1 ]; then
      cat smoke-stdout.log || true
      kill "$XVFB_PID" 2>/dev/null || true
      fail_gate "Launch smoke (Xvfb: boot, window, clean shutdown)" "desktop smoke: the application exited during the 10s soak"
    fi
    kill -TERM "$APP_PID"
    reaped=0
    for i in $(seq 1 30); do
      st="$(ps -o stat= -p "$APP_PID" 2>/dev/null || true)"
      if [ -z "$st" ]; then reaped=1; break; fi
      case "$st" in
        *Z*) wait "$APP_PID" 2>/dev/null || true; reaped=1; break;;
      esac
      sleep 0.5
    done
    if [ "$reaped" -ne 1 ]; then
      kill -KILL "$APP_PID" 2>/dev/null || true
      wait "$APP_PID" 2>/dev/null || true
      kill "$XVFB_PID" 2>/dev/null || true
      fail_gate "Launch smoke (Xvfb: boot, window, clean shutdown)" "desktop smoke: the application ignored SIGTERM"
    fi
    wait "$APP_PID" 2>/dev/null || true
    kill "$XVFB_PID" 2>/dev/null || true
    echo "SMOKE OK: window booted, event loop alive, clean SIGTERM shutdown with reap"
    cp smoke-stdout.log "$GITHUB_WORKSPACE/ci-logs/linux-smoke-stdout.log"
    ;;

  build-info)
    # v1.11.1 minimal root: static package content under resources/docs;
    # no root-level models/workspace placeholders.
    root="${STAGE}/${LINUX_PKG_ROOT}"
    mkdir -p "$root/resources/docs"
    cat > "$root/resources/docs/BUILD-INFO.txt" <<EOF
product: SHEYTAN-LA
version: ${APP_VERSION}
commit: ${GITHUB_SHA:-local}
ui: adaptive web shell (web workspace host)
EOF
    echo "SHEYTAN-LA ${APP_VERSION} — run ./SHEYTAN-LA. Models are downloaded in-app; user data lives under data/ next to the executable. Static package content lives under resources/." > "$root/resources/docs/README.txt"
    for f in "$root/resources/docs/BUILD-INFO.txt" "$root/resources/docs/README.txt"; do
      test -s "$f" || fail_gate "Write BUILD-INFO.txt and portable metadata" "metadata file missing or empty: $f"
    done
    grep -q "version: ${APP_VERSION}" "$root/resources/docs/BUILD-INFO.txt" || fail_gate "Write BUILD-INFO.txt and portable metadata" "BUILD-INFO.txt lacks the version line"
    cp LICENSE.md "$root/resources/docs/LICENSE.md"
    cp SIGNATURE "$root/resources/docs/SIGNATURE"
    ;;

  archive)
    # Portable archive only — no DEB/RPM (master work order §10).
    root="${STAGE}/${LINUX_PKG_ROOT}"
    test -x "${root}/SHEYTAN-LA" || fail_gate "Create the portable archive (no DEB/RPM)" "staging tree lost the executable before archiving"
    if ! tar -czf "$ARCHIVE" -C "$STAGE" "${LINUX_PKG_ROOT}"; then
      fail_gate "Create the portable archive (no DEB/RPM)" "tar -czf failed for $ARCHIVE"
    fi
    echo "portable archive created (no installer)"
    ;;

  verify-archive)
    test -s "$ARCHIVE" || fail_gate "Verify the portable archive (fail-closed)" "portable archive missing or empty"
    members="$(tar -tzf "$ARCHIVE")"
    echo "$members" | grep -qx "${LINUX_PKG_ROOT}/SHEYTAN-LA" || fail_gate "Verify the portable archive (fail-closed)" "required member ${LINUX_PKG_ROOT}/SHEYTAN-LA missing"
    echo "$members" | grep -qx "${LINUX_PKG_ROOT}/resources/docs/BUILD-INFO.txt" || fail_gate "Verify the portable archive (fail-closed)" "required member BUILD-INFO.txt missing"
    echo "$members" | grep -qx "${LINUX_PKG_ROOT}/resources/docs/README.txt" || fail_gate "Verify the portable archive (fail-closed)" "required member README.txt missing"
    echo "$members" | grep -qx "${LINUX_PKG_ROOT}/resources/docs/LICENSE.md" || fail_gate "Verify the portable archive (fail-closed)" "required member LICENSE.md missing"
    if echo "$members" | grep -Eqi '\.(deb|rpm|nsi|msi|msix)$'; then
      fail_gate "Verify the portable archive (fail-closed)" "installer-class payload found in the portable archive"
    fi
    if echo "$members" | grep -Eq "^SHEYTAN-LA/(data|models|workspace)/"; then
      fail_gate "Verify the portable archive (fail-closed)" "archive purity violation: data-root residue or retired root-level placeholders present"
    fi
    rm -rf "$RUNNER_TEMP/tarcheck" && mkdir -p "$RUNNER_TEMP/tarcheck"
    tar -xzf "$ARCHIVE" -C "$RUNNER_TEMP/tarcheck"
    test -x "$RUNNER_TEMP/tarcheck/SHEYTAN-LA/SHEYTAN-LA" || fail_gate "Verify the portable archive (fail-closed)" "executable bit lost in the archive"
    count="$(echo "$members" | wc -l)"
    echo "archive verification OK: ${count} members, binary + metadata present, executable bit preserved, no installer payloads"
    ;;

  *)
    echo "usage: linux.sh <phase>" >&2
    echo "phases: gofmt headless-tests release-contract race stress fixtures catalog-freshness version prereqs ui-tests build build-info-probe verify-elf smoke build-info archive verify-archive" >&2
    exit 64
    ;;
esac
