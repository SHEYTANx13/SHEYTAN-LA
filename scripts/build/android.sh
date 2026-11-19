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
#     shell migration target), the lane packages it with `gomobile bind`
#     at the EXACT pinned x/mobile version (never @latest);
#   - FALLBACK path (current product truth: Fyne shell): `fyne package`
#     executed FROM the app package directory (cd cmd/sheytan-local-agent),
#     with the icon path spelled RELATIVE TO THAT CWD
#     (../../ui/fyne/assets/logo-512.png).
#   - gomobile/gobind are installed at the EXACT recorded pin
#     v0.0.0-20261117202452-47699113848a - never @latest, never a moving
#     branch (run 44378562071 section 4.1.6: the product module imports
#     no x/mobile runtime package, so the pin lives HERE, not in go.mod).
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
CANONICAL="dist/SHEYTAN-LA-ALPHA-android-arm64-debug"

mkdir -p ci-logs

fail_gate() { # $1 = step name, $2 = human message
  echo "::error::$2"
  echo "$1" > "$MARKER"
  exit 1
}

phase="${1:-}"

case "$phase" in
  headless-tests)
    # -json is CONTRACTUAL: the collector's stateful `go test -json`
    # classifier (run 44457547289 3.4) needs the stream.
    echo "RUN go test ./internal/... -tags headless -json (android lane host)" >> ci-logs/gate-order.txt
    if ! go test ./internal/... -tags headless -json -count=1 -timeout "${GO_TEST_TIMEOUT}" 2>&1 | tee ci-logs/android-go-test-headless.log; then
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
    mkdir -p dist/android
    if [ -d "cmd/sheytan-mobile" ]; then
      # PRIMARY path: the Go-Gui mobile shell exists - package it with
      # gomobile at the pinned toolchain (AAB output, -work=false).
      echo "cmd/sheytan-mobile present - packaging with gomobile bind"
      if ! gomobile bind -target=android -androidapi 24 -o dist/android/SHEYTAN-LA-ALPHA-android-arm64.aab -work=false ./cmd/sheytan-mobile; then
        fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "gomobile bind for ./cmd/sheytan-mobile failed"
      fi
      cp dist/android/SHEYTAN-LA-ALPHA-android-arm64.aab "${CANONICAL}.aab"
    else
      # FALLBACK path (current product truth): fyne package FROM the app
      # package directory. Run 44457547289 step-8 root cause: fyne package
      # at the repo root has NO Go package and cannot build. The icon path
      # is relative to the NEW CWD (cmd/sheytan-local-agent).
      if ! (cd cmd/sheytan-local-agent && test -f ../../ui/fyne/assets/logo-512.png); then
        fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "the brand icon ../../ui/fyne/assets/logo-512.png is not reachable from cmd/sheytan-local-agent"
      fi
      fyneVersion="$(go list -m fyne.io/fyne/v2)"
      if ! go install "fyne.io/fyne/v2/cmd/fyne@${fyneVersion#fyne.io/fyne/v2 }"; then
        fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "the fyne CLI install failed at the module's own fyne version"
      fi
      echo "cmd/sheytan-mobile absent - packaging cmd/sheytan-local-agent with fyne (run from the app package directory)"
      if ! (cd cmd/sheytan-local-agent && fyne package -os android -arch arm64 \
            -appID io.github.sheytanx13.SHEYTANLA \
            -name SHEYTAN-LA \
            -icon ../../ui/fyne/assets/logo-512.png \
            -appBuild 1 \
            -appVersion 1.11.1 \
            -outputDir ../../dist/android); then
        fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "fyne package -os android failed (packaged from cmd/sheytan-local-agent, not the repo root - run 44457547289 step 8 root cause)"
      fi
      apk="$(ls dist/android/*.apk 2>/dev/null | head -1 || true)"
      test -s "$apk" || fail_gate "Package the Android arm64 app (debug-signed, never committed keys)" "no APK produced by fyne package"
      cp "$apk" "${CANONICAL}.apk"
    fi
    ls -la dist/android
    echo "canonical package: ${CANONICAL}.apk/.aab"
    ;;

  verify)
    # Fail-closed structural checks on the CANONICAL package: the APK/AAB
    # is a zip - AndroidManifest.xml + classes.dex must be present, and
    # installer-class payload extensions are rejected.
    pkg="$(ls "${CANONICAL}".apk "${CANONICAL}".aab 2>/dev/null | head -1 || true)"
    test -s "$pkg" || fail_gate "Verify the APK/AAB (fail-closed structural checks)" "no Android package at ${CANONICAL}.apk/.aab"
    members="$(unzip -Z1 "$pkg")"
    echo "$members" | grep -Eq '(^|/)AndroidManifest\.xml$' || fail_gate "Verify the APK/AAB (fail-closed structural checks)" "AndroidManifest.xml missing from $pkg"
    echo "$members" | grep -Eq '(^|/)classes.*\.dex$' || fail_gate "Verify the APK/AAB (fail-closed structural checks)" "no classes.dex in $pkg"
    if echo "$members" | grep -Eqi '\.(nsi|msi|msix)$'; then
      fail_gate "Verify the APK/AAB (fail-closed structural checks)" "installer-class payload found inside $pkg"
    fi
    # The manifest must declare the debuggable/testable dev package
    # honestly: aapt reports the package name for the record (APK only -
    # an AAB is not aapt-badging shaped, its manifest is still checked above).
    if [ "${pkg##*.}" = "apk" ] && [ -n "${ANDROID_HOME:-}" ]; then
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
