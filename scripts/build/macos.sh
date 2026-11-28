#!/usr/bin/env bash
# scripts/build/macos.sh - the macOS lane of the consolidated platform
# build (CI consolidation: the retired .github/workflows/build-macos.yml
# reusable lane, now dispatched per-phase by the platform-build matrix
# job of .github/workflows/build.yml for targets macos-arm64/macos-x64).
#
# Contract carried over from build-macos.yml:
#   - native compile for the target arch on the pinned runner
#     (macos-15 = Apple Silicon, macos-15-intel = Intel - the two
#     architectures are DISTINCT matrix entries, never one alias);
#   - the headless core suite always passes -json (the collector's
#     stateful `go test -json` classifier);
#   - ad-hoc signed .app bundle (codesign -s -): NO developer identity,
#     NO notarization, NO PKG/DMG - the honest portable test-build status
#     is recorded on the artifact, never papered over (master work order
#     section 10.1);
#   - the portable .zip is written to
#     dist/SHEYTAN-LA-ALPHA-macos-<arch>-portable.zip (the consolidation
#     FIXES the retired lane's upload-path bug: the old upload step named
#     SHEYTAN-LA-ALPHA-macos-<arch>.zip while the produced file carried
#     the -portable suffix - the name is now derived from ONE constant);
#   - every verification gate writes the upstream-failure marker
#     ci-logs/<target>-upstream-failure.txt before failing.
#
# Usage: macos.sh <phase>
#   gofmt | headless-tests | release-contract | build | build-info-probe |
#   stage-app | smoke | zip | verify-zip
set -euo pipefail

TARGET="${TARGET:?TARGET must be provided (macos-arm64 or macos-x64)}"
MARKER="ci-logs/${TARGET}-upstream-failure.txt"
GO_TEST_TIMEOUT="${GO_TEST_TIMEOUT:-25m}"
APP_VERSION="${APP_VERSION:?APP_VERSION must be provided by the workflow env}"

case "$TARGET" in
  macos-arm64) GOARCH_VALUE="arm64"; ARCH="arm64" ;;
  macos-x64)   GOARCH_VALUE="amd64"; ARCH="x64" ;;
  *) echo "::error::macos.sh: unsupported TARGET '$TARGET' (want macos-arm64 or macos-x64)"; echo "macos.sh (target dispatch)" > "$MARKER" 2>/dev/null || true; exit 1 ;;
esac

ZIP="dist/SHEYTAN-LA-ALPHA-macos-${ARCH}-portable.zip"
APP_DIR="dist/SHEYTAN-LA-ALPHA-macos-${ARCH}-portable"
APP_BUNDLE="${APP_DIR}/SHEYTAN-LA.app"

mkdir -p ci-logs

fail_gate() { # $1 = step name, $2 = human message
  echo "::error::$2"
  echo "$1" > "$MARKER"
  exit 1
}

# summarize_headless_failure (run 45165536396 repair): extracts the
# suite's OWN root-cause records from the tee'd go test -json stream and
# emits them as ::error:: annotations. Run 45165536396's macos-arm64 and
# iOS-lane reports carried ONLY "Process completed with exit code 1"
# because a suite-timeout panic or a package crash produces NO
# `--- FAIL:` records for the collector to classify — the diagnosis died
# with the stream. Both evidence shapes (JSON-wrapped frames and raw
# stream lines) are matched here and emitted verbatim, so the errors/
# warnings report names the failing package and the real reason.
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
  # because these lanes run macOS BSD awk and mawk alike.
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

# write_info_plist emits the bundle's Info.plist (heredoc terminator must
# sit at column 0; the emitted content is unindented by the leading
# spaces stripped below via sed -i 's/^[[:space:]]*//' before plutil
# validates it).
write_info_plist() { # $1 = destination file
  cat > "$1" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>SHEYTAN-LA</string>
  <key>CFBundleDisplayName</key><string>SHEYTAN-LA - ALPHA</string>
  <key>CFBundleIdentifier</key><string>io.github.sheytanx13.SHEYTAN-LA</string>
  <key>CFBundleExecutable</key><string>SHEYTAN-LA</string>
  <key>CFBundleShortVersionString</key><string>ALPHA</string>
  <key>CFBundleVersion</key><string>ALPHA</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>sheytan</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
}

phase="${1:-}"

case "$phase" in
  gofmt)
    # SCOPE PARITY: the ONE canonical scope, identical to every lane's
    # gofmt gate: cmd internal ui scripts.
    unformatted="$(gofmt -l cmd internal ui scripts)" || fail_gate "gofmt (enforced)" "gofmt invocation failed"
    if [ -n "$unformatted" ]; then
      printf 'toolchain: %s\nfiles:\n%s\n' "$(go version)" "$unformatted" | tee ci-logs/macos-gofmt.log
      fail_gate "gofmt (enforced)" "gofmt violations - run gofmt -w on the files in ci-logs/macos-gofmt.log"
    fi
    echo "gofmt OK"
    ;;

  headless-tests)
    # Run 45165536396 repair: the gate must never download modules
    # lazily — the macos-x64 lane's lazy proxy fetches (gogpu/wgpu
    # family) timed out mid-gate and the suite failed as an opaque build
    # failure with zero test-failure records. The bounded-retry prefetch
    # (scripts/ci/module-preflight.sh) runs FIRST and fails closed with
    # the exact captured proxy error.
    if ! bash scripts/ci/module-preflight.sh "${TARGET}"; then
      fail_gate "Module prefetch (darwin/${GOARCH_VALUE})" "module prefetch failed on darwin/${GOARCH_VALUE} — the exact proxy error is in ci-logs/${TARGET}-module-preflight.log and in this step log"
    fi
    # -json is CONTRACTUAL: the collector's stateful `go test -json`
    # classifier (run 44457547289 3.4) needs the stream.
    echo "RUN go test ./internal/... -tags headless -json (darwin/${GOARCH_VALUE})" >> ci-logs/gate-order.txt
    if ! CGO_ENABLED=1 go test ./internal/... -tags headless -json -count=1 -timeout "${GO_TEST_TIMEOUT}" 2>&1 | tee ci-logs/macos-go-test-headless.log; then
      # Run 45165536396 repair: a bare exit code is not a diagnosis —
      # surface the suite's own root-cause records before the gate
      # marker, so the report names the failing package and reason.
      summarize_headless_failure ci-logs/macos-go-test-headless.log "darwin/${GOARCH_VALUE}"
      fail_gate "Headless core suite (darwin/${GOARCH_VALUE})" "headless core tests failed on darwin/${GOARCH_VALUE}"
    fi
    echo "gate OK: headless core suite (darwin/${GOARCH_VALUE})" >> ci-logs/gate-order.txt
    ;;

  release-contract)
    if ! go test ./internal/releasecontract/ -count=1 2>&1 | tee ci-logs/macos-release-contract.log; then
      fail_gate "Release-contract suite (darwin/${GOARCH_VALUE})" "release-contract tests failed on darwin/${GOARCH_VALUE}"
    fi
    ;;

  build)
    export CGO_ENABLED=1
    mkdir -p "${APP_DIR}/app"
    if ! GOOS=darwin GOARCH="${GOARCH_VALUE}" go build -trimpath -tags webworkspace -o "${APP_DIR}/app/SHEYTAN-LA" ./cmd/sheytan-local-agent; then
      fail_gate "Build the full desktop application (native darwin/${GOARCH_VALUE})" "the darwin/${GOARCH_VALUE} desktop build failed"
    fi
    ;;

  build-info-probe)
    bin="$(realpath "${APP_DIR}/app/SHEYTAN-LA")"
    test -x "$bin" || fail_gate "Cross-check the compiled binary's build identity" "built executable missing or not executable: $bin"
    probeDir="$(mktemp -d "${RUNNER_TEMP:-/tmp}/build-info-probe.XXXXXX")"
    cd "$probeDir"
    raw="$("$bin" build-info 2>/dev/null)" || fail_gate "Cross-check the compiled binary's build identity" "the compiled build-info probe exited nonzero"
    [ -n "$raw" ] || fail_gate "Cross-check the compiled binary's build identity" "the compiled build-info probe produced no output"
    echo "compiled build-info: $raw"
    stage="$(printf '%s' "$raw" | sed -n 's/.*"product_stage":"\([^"]*\)".*/\1/p')"
    build_id="$(printf '%s' "$raw" | sed -n 's/.*"build_id":"\([^"]*\)".*/\1/p')"
    [ "$stage" = "ALPHA" ] || fail_gate "Cross-check the compiled binary's build identity" "compiled product_stage is '$stage', want ALPHA"
    [ -n "$build_id" ] || fail_gate "Cross-check the compiled binary's build identity" "build-info carries no build_id field"
    echo "compiled build identity OK: product_stage=$stage build_id=$build_id"
    ;;

  stage-app)
    app="$APP_BUNDLE"
    mkdir -p "${app}/Contents/MacOS" "${app}/Contents/Resources"
    cp "${APP_DIR}/app/SHEYTAN-LA" "${app}/Contents/MacOS/SHEYTAN-LA"
    chmod +x "${app}/Contents/MacOS/SHEYTAN-LA"
    printf 'product: SHEYTAN-LA\nstage: ALPHA\ncommit: %s\nui: adaptive web shell (web workspace host)\n' "${GITHUB_SHA:-local}" > "${app}/Contents/Resources/BUILD-INFO.txt"
    # App icon (run 46131092469 repair): the staged .app shipped with NO
    # icon - the Info.plist carried no CFBundleIconFile and Resources
    # carried no .icns, so Finder/Dock showed the generic executable
    # face. The icns is built HERE, on the runner, from the brand logo
    # the shell already ships, with the macOS-native tools every runner
    # image has (sips resample + iconutil); no image binary is committed.
    # Every stage below fails closed - a bundle without its icon must
    # never pass as complete.
    logo="internal/webshell/assets/logo.png"
    [ -f "$logo" ] || fail_gate "Stage the .app bundle (portable, ad-hoc signed)" "brand logo missing: $logo"
    iconset="${RUNNER_TEMP:-/tmp}/sheytan-icon.$$.iconset"
    rm -rf "$iconset"; mkdir -p "$iconset"
    icon_ok=true
    for pair in "16:icon_16x16.png" "32:icon_16x16@2x.png" "32:icon_32x32.png" "64:icon_32x32@2x.png" "128:icon_128x128.png" "256:icon_128x128@2x.png" "256:icon_256x256.png" "512:icon_256x256@2x.png" "512:icon_512x512.png" "1024:icon_512x512@2x.png"; do
      size="${pair%%:*}"; out="${pair#*:}"
      sips -z "$size" "$size" "$logo" --out "$iconset/$out" >/dev/null 2>&1 || { icon_ok=false; break; }
    done
    $icon_ok || { rm -rf "$iconset"; fail_gate "Stage the .app bundle (portable, ad-hoc signed)" "the app iconset could not be built with sips from $logo"; }
    iconutil -c icns "$iconset" -o "${app}/Contents/Resources/sheytan.icns" || { rm -rf "$iconset"; fail_gate "Stage the .app bundle (portable, ad-hoc signed)" "iconutil could not build sheytan.icns from the iconset"; }
    rm -rf "$iconset"
    [ -s "${app}/Contents/Resources/sheytan.icns" ] || fail_gate "Stage the .app bundle (portable, ad-hoc signed)" "the staged .app is missing its sheytan.icns icon"
    write_info_plist "${app}/Contents/Info.plist"
    # The emitted plist is already unindented; the sed pass below is a
    # belt-and-braces guard so the XML declaration sits at byte 0, then
    # the plist is validated before it ships.
    sed -i '' 's/^[[:space:]]*//' "${app}/Contents/Info.plist"
    plutil -lint "${app}/Contents/Info.plist" || fail_gate "Stage the .app bundle (portable, ad-hoc signed)" "Info.plist failed plutil -lint"
    # Ad-hoc signature: honest test-build status, no identity, no
    # notarization (recorded on the artifact, never claimed otherwise).
    codesign --force --deep -s - "${app}" || fail_gate "Stage the .app bundle (portable, ad-hoc signed)" "codesign ad-hoc signing failed"
    codesign -dv "${app}" 2>&1 | head -5 || true
    echo ".app staged (ad-hoc signed, unnotarized - portable test build)"
    ;;

  smoke)
    # Bounded, honest launch/lifecycle smoke on the runner's window
    # server: alive at t+15s, SIGTERM honored within 20s.
    app="${APP_BUNDLE}/Contents/MacOS/SHEYTAN-LA"
    smokeDir="$(mktemp -d "${RUNNER_TEMP:-/tmp}/macos-smoke.XXXXXX")"
    export SHEYTAN_DATA_DIR="${smokeDir}/data"
    "./$app" &
    appPid=$!
    sleep 15
    if ! kill -0 "$appPid" 2>/dev/null; then
      wait "$appPid" || true
      fail_gate "Launch smoke (bounded, runner window server)" "the SHEYTAN-LA process exited before the bounded smoke window elapsed"
    fi
    kill -TERM "$appPid" 2>/dev/null || true
    for i in $(seq 1 20); do
      kill -0 "$appPid" 2>/dev/null || break
      sleep 1
    done
    if kill -0 "$appPid" 2>/dev/null; then
      kill -KILL "$appPid" 2>/dev/null || true
      fail_gate "Launch smoke (bounded, runner window server)" "the process ignored SIGTERM for 20s - graceful shutdown regressed"
    fi
    echo "launch smoke OK: alive at t+15s, SIGTERM honored"
    ;;

  zip)
    # ditto preserves the bundle layout exactly (no resource forks). The
    # zip lands at the dist/ ROOT under the artifact-derived name (one
    # constant feeding the verifier below and the workflow upload):
    # dist/SHEYTAN-LA-ALPHA-macos-<arch>-portable.zip.
    if ! ditto -c -k --keepParent "${APP_BUNDLE}" "$ZIP"; then
      fail_gate "Create the portable .zip (no PKG/DMG)" "ditto failed for SHEYTAN-LA.app"
    fi
    test -s "$ZIP" || fail_gate "Create the portable .zip (no PKG/DMG)" "the produced .zip is missing or empty"
    echo "portable .zip created (no installer): $ZIP"
    ;;

  verify-zip)
    zip="$ZIP"
    test -s "$zip" || fail_gate "Verify the portable .zip (fail-closed)" "portable .zip missing at $zip"
    members="$(unzip -Z1 "$zip")"
    echo "$members" | grep -q "SHEYTAN-LA.app/Contents/MacOS/SHEYTAN-LA" || fail_gate "Verify the portable .zip (fail-closed)" "required member missing from the bundle zip"
    echo "$members" | grep -q "SHEYTAN-LA.app/Contents/Info.plist" || fail_gate "Verify the portable .zip (fail-closed)" "Info.plist missing from the bundle zip"
    echo "$members" | grep -q "SHEYTAN-LA.app/Contents/Resources/sheytan.icns" || fail_gate "Verify the portable .zip (fail-closed)" "the bundle app icon (sheytan.icns) is missing from the bundle zip"
    if echo "$members" | grep -Eqi '\.(pkg|dmg|nsi|msi)$'; then
      fail_gate "Verify the portable .zip (fail-closed)" "installer-class payload found in the portable archive"
    fi
    echo "archive verification OK: bundle complete, no installer payloads"
    ;;

  *)
    echo "usage: macos.sh <phase>" >&2
    echo "phases: gofmt headless-tests release-contract build build-info-probe stage-app smoke zip verify-zip" >&2
    exit 64
    ;;
esac
