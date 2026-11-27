#!/usr/bin/env bash
# scripts/build/android.sh - the Android arm64 lane of the consolidated
# platform build (CI consolidation: the retired
# .github/workflows/build-android.yml reusable lane, now dispatched
# per-phase by the platform-build matrix job of
# .github/workflows/build.yml).
#
# Contract carried over from build-android.yml, with the run
# 44457547289 step-8 ROOT CAUSE FIXED: the old lane ran `fyne package`
# at the repository root, which has NO Go package - the packaging step
# must run from a directory that actually contains a Go main package:
#   - PRIMARY path: when cmd/sheytan-mobile exists (the Go-Gui mobile
#     shell migration target), the lane packages it with `gomobile build`
#     at the EXACT pinned x/mobile version (never @latest) - the APK
#     command. Run 44547031698 §6: `gomobile bind` builds an AAR
#     LIBRARY and failed on the main package - it is retired here.
#   - FALLBACK path (current product truth: Fyne shell): `fyne package`
#     executed FROM the app package directory (cd cmd/sheytan-local-agent),
#     with the icon path spelled RELATIVE TO THAT CWD
#     (../../ui/fyne/assets/logo-512.png).
#   - gomobile/gobind are installed at the EXACT recorded pin
#     v0.0.0-20261117202452-47699113848a - never @latest, never a moving
#     branch. Run 44663205295 repair: the product module NOW imports
#     golang.org/x/mobile (app + event lifecycle/size/paint) in
#     cmd/sheytan-mobile/mobile_android_main.go - the Android entrypoint
#     bridges the NativeActivity glue to the go-gui Pattern-B Android
#     backend - at the SAME exact pin, carried in go.mod (the version
#     authority); this lane keeps installing the TOOLCHAIN binaries at
#     the identical recorded version, so both sides always match.
#   - the NDK setup is fail-closed; debug-signed only, never committed
#     keys; the lane ends with the repo-cleanliness guard
#     test -z "$(git status --porcelain)".
#
# Evidence honesty (master work order section 10.1): this artifact is a
# DEVELOPMENT/TEST package. No emulator/device smoke is claimed here.
#
# Usage: android.sh <phase>
#   headless-tests | mobile-toolchain | ndk | package | verify | cleanliness
set -euo pipefail

TARGET="android-arm64"
MARKER="ci-logs/${TARGET}-upstream-failure.txt"
GO_TEST_TIMEOUT="${GO_TEST_TIMEOUT:-25m}"
APP_VERSION="${APP_VERSION:?APP_VERSION must be provided by the workflow env}"
mobilePin="v0.0.0-20261117202452-47699113848a"
CANONICAL="dist/SHEYTAN-LA-ALPHA-android-arm64/SHEYTAN-LA-ALPHA-android-arm64.apk"

mkdir -p ci-logs

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
  headless-tests)
    # Run 45165536396 repair: the gate must never download modules
    # lazily — the same bounded-retry prefetch every lane runs (the
    # macos-x64 lane's lazy proxy fetches timed out mid-gate and failed
    # the suite as an opaque build failure).
    if ! bash scripts/ci/module-preflight.sh "${TARGET}"; then
      fail_gate "Module prefetch (android lane host)" "module prefetch failed on the android lane host — the exact proxy error is in ci-logs/${TARGET}-module-preflight.log and in this step log"
    fi
    # -json is CONTRACTUAL: the collector's stateful `go test -json`
    # classifier (run 44457547289 3.4) needs the stream.
    echo "RUN go test ./internal/... -tags headless -json (android lane host)" >> ci-logs/gate-order.txt
    if ! go test ./internal/... -tags headless -json -count=1 -timeout "${GO_TEST_TIMEOUT}" 2>&1 | tee ci-logs/android-go-test-headless.log; then
      summarize_headless_failure ci-logs/android-go-test-headless.log "android lane host"
      fail_gate "Headless core suite (android lane host)" "headless core tests failed on the android lane host"
    fi
    echo "gate OK: headless core suite (android lane host)" >> ci-logs/gate-order.txt
    ;;

  mobile-toolchain)
    # Run 44378562071 section 4.1.6/4.2.4: the mobile toolchain pin lives
    # HERE, not in go.mod. Exact recorded version - never @latest, never
    # a moving branch, never a graph re-resolution.
    echo "pinned x/mobile: ${mobilePin}"
    if ! go install "golang.org/x/mobile/cmd/gomobile@${mobilePin}"; then
      fail_gate "Install gomobile toolchain at the exact pinned version (never @latest)" "gomobile install failed at the pinned version ${mobilePin}"
    fi
    if ! go install "golang.org/x/mobile/cmd/gobind@${mobilePin}"; then
      fail_gate "Install gomobile toolchain at the exact pinned version (never @latest)" "gobind install failed at the pinned version ${mobilePin}"
    fi
    echo "$HOME/go/bin" >> "$GITHUB_PATH"
    ;;

  ndk)
    # Fail-closed NDK setup: locate the preinstalled NDK or install a
    # current LTS NDK via sdkmanager; an unavailable NDK stops the lane.
    ndkDir="$(ls -d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1 || true)"
    if [ -z "$ndkDir" ]; then
      echo "no preinstalled NDK - installing a current LTS NDK via sdkmanager"
      yes | "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "ndk;27.2.12479018" >/dev/null
      ndkDir="$(ls -d "$ANDROID_HOME"/ndk/27.2.12479018)"
    fi
    test -d "$ndkDir" || fail_gate "Locate or install the Android NDK (fail-closed, actionable)" "NDK unavailable and sdkmanager install failed - the Android lane cannot proceed"
    echo "ANDROID_NDK_HOME=$ndkDir" >> "$GITHUB_ENV"
    echo "using NDK at $ndkDir"
    ;;

  package)
    # Run 44547031698 §6 ROOT CAUSE: the previous lane ran
    # `gomobile bind -o dist/android/....aab ./cmd/sheytan-mobile` and the
    # hosted run failed exactly there ("gomobile bind for
    # ./cmd/sheytan-mobile failed"). The official Go Mobile command
    # contract (x/mobile cmd/gomobile/doc.go at the pinned version):
    #   gomobile bind   builds a LIBRARY for Android/iOS (an AAR) and
    #                   requires exported binding APIs;
    #   gomobile build  "compiles and encodes the app named by the import
    #                   path. The named package must define a main
    #                   function" and produces the Android APK.
    # cmd/sheytan-mobile is `package main` with `func main` - the
    # gomobile BUILD (APK) shape. The APK is debug-signed by gomobile's
    # own embedded debug certificate (cmd/gomobile/build_androidapp.go:
    # debugCert) - no keystore file is created, so nothing can leak into
    # the repository or the artifact.
    # Run 45634345922 update: the retired fyne CLI packaging fallback is
    # gone with the Fyne shell itself; the mobile shell (the Go-Gui
    # mobile surface) is the only Android package path.
    if [ ! -d "cmd/sheytan-mobile" ]; then
      fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "cmd/sheytan-mobile is absent — the mobile shell is the only Android package path (the fyne CLI fallback is retired)"
    fi
    # PRIMARY path: the Go-Gui mobile shell - the arm64-v8a APK through
    # gomobile build at the pinned toolchain (single ABI, -work=false).
    # The canonical APK is staged UNDER the matching directory so the
    # platform artifact (the unpacked portable tree) carries it.
    # Run 45716559459 root cause: the nested CANONICAL path is two
    # levels deep and NOTHING created its parent directory - gomobile
    # build -o does not mkdir -p, so the APK write failed on the hosted
    # lane before the toolchain ever produced a byte.
    mkdir -p "$(dirname "${CANONICAL}")"
    echo "building the arm64-v8a APK with gomobile build (the app command; bind builds an AAR library)"
    if ! gomobile build -target=android/arm64 -androidapi 24 -work=false -o "${CANONICAL}" ./cmd/sheytan-mobile; then
      fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "gomobile build for ./cmd/sheytan-mobile failed (the APK command; the retired bind command builds an AAR library, never the APK)"
    fi
    echo "canonical package: ${CANONICAL} (debug-signed app build - emulator/device smoke NOT RUN here, work order §6.1 gates it separately)"
    ;;

  verify)
    # Fail-closed structural checks on the CANONICAL package: the APK
    # is a zip - AndroidManifest.xml + classes.dex must be present, and
    # installer-class payload extensions are rejected.
    pkg="${CANONICAL}"
    test -s "$pkg" || fail_gate "Verify the APK (fail-closed structural checks)" "no APK at ${CANONICAL}"
    members="$(unzip -Z1 "$pkg")"
    echo "$members" | grep -Eq '(^|/)AndroidManifest\.xml$' || fail_gate "Verify the APK (fail-closed structural checks)" "AndroidManifest.xml missing from $pkg"
    echo "$members" | grep -Eq '(^|/)classes.*\.dex$' || fail_gate "Verify the APK (fail-closed structural checks)" "no classes.dex in $pkg"
    if echo "$members" | grep -Eqi '\.(nsi|msi|msix)$'; then
      fail_gate "Verify the APK (fail-closed structural checks)" "installer-class payload found inside $pkg"
    fi
    # Single-ABI honesty: the artifact is the android-ARM64 package, so
    # the only native library ABI is arm64-v8a (run 44547031698 §6.1:
    # no unexpected ABI, no duplicate libraries).
    libabis="$(echo "$members" | grep -E '^lib/[^/]+/' | cut -d/ -f2 | sort -u || true)"
    if [ -n "$libabis" ] && [ "$libabis" != "arm64-v8a" ]; then
      fail_gate "Verify the APK (fail-closed structural checks)" "unexpected native ABI set in $pkg: $(echo "$libabis" | tr '\n' ' ')(want arm64-v8a only)"
    fi
    # The manifest must declare the debuggable/testable dev package
    # honestly: aapt reports the package name for the record.
    if [ -n "${ANDROID_HOME:-}" ]; then
      buildToolsDir="$(ls -d "$ANDROID_HOME"/build-tools/* 2>/dev/null | sort -V | tail -1 || true)"
      if [ -n "$buildToolsDir" ] && [ -x "$buildToolsDir/aapt" ]; then
        "$buildToolsDir/aapt" dump badging "$pkg" | head -4 || true
      fi
    fi
    echo "Android package verification OK: $(du -h "$pkg" | cut -f1), manifest + dex present, debug-signed dev artifact"
    ;;

  cleanliness)
    # The debug keystore gomobile/fyne generated lives in the runner HOME
    # - it must never reach the repository. Fail if ANY untracked file
    # appeared (the packaging step must leave the tree pristine).
    residue="$(git status --porcelain)"
    if [ -n "$residue" ]; then
      echo "::error::the Android packaging step dirtied the working tree - debug keystores and build products must never be committed:"
      echo "$residue"
      fail_gate "Repo cleanliness guard (no keys, no build residue committed)" "the Android packaging step left the working tree dirty"
    fi
    echo "working tree pristine - no keystore or residue tracked"
    ;;

  *)
    echo "usage: android.sh <phase>" >&2
    echo "phases: headless-tests mobile-toolchain ndk package verify cleanliness" >&2
    exit 64
    ;;
esac
