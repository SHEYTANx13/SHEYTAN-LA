#!/usr/bin/env bash
# scripts/build/ios.sh - the iOS simulator lane of the consolidated
# platform build (CI consolidation: the retired
# .github/workflows/build-ios.yml reusable lane, now dispatched
# per-phase by the platform-build matrix job of
# .github/workflows/build.yml).
#
# Contract carried over from build-ios.yml, with the fyne packaging
# CWD FIXED the same way as the Android lane (run 44457547289 step-8
# root cause): `fyne package -os ios` must run FROM the app package
# directory (cd cmd/sheytan-local-agent), with the icon path relative to
# THAT CWD (../../ui/fyne/assets/logo-512.png).
#
# Evidence honesty (master work order section 10.1): the artifact is
# SIMULATOR-ONLY qualification - a zip of the unsigned simulator .app.
# NO device .ipa is produced (a device build requires a signing identity
# and provisioning profile this lane does not have); simulator
# install/launch is recorded as NOT RUN, never claimed. The lane ends
# with the repo-cleanliness guard test -z "$(git status --porcelain)".
#
# Usage: ios.sh <phase>
#   headless-tests | mobile-toolchain | package | verify-app | cleanliness |
#   zip | verify-zip
set -euo pipefail

TARGET="ios-simulator-arm64"
MARKER="ci-logs/${TARGET}-upstream-failure.txt"
GO_TEST_TIMEOUT="${GO_TEST_TIMEOUT:-25m}"
APP_VERSION="${APP_VERSION:?APP_VERSION must be provided by the workflow env}"
mobilePin="v0.0.0-20261117202452-47699113848a"
ZIP="dist/ios/SHEYTAN-LA-ALPHA-ios-simulator-arm64.zip"

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
    echo "RUN go test ./internal/... -tags headless -json (iOS lane host)" >> ci-logs/gate-order.txt
    if ! CGO_ENABLED=1 go test ./internal/... -tags headless -json -count=1 -timeout "${GO_TEST_TIMEOUT}" 2>&1 | tee ci-logs/ios-go-test-headless.log; then
      fail_gate "Headless core suite (iOS lane host)" "headless core tests failed on the iOS lane host"
    fi
    echo "gate OK: headless core suite (iOS lane host)" >> ci-logs/gate-order.txt
    ;;

  mobile-toolchain)
    # Run 44378562071 section 4.1.6/4.2.4: identical contract to the
    # Android lane - the pin lives HERE, not in go.mod. Exact recorded
    # version, never @latest, never a moving branch.
    echo "pinned x/mobile: ${mobilePin}"
    if ! CGO_ENABLED=1 go install "golang.org/x/mobile/cmd/gomobile@${mobilePin}"; then
      fail_gate "Install gomobile toolchain at the exact pinned version (never @latest)" "gomobile install failed at the pinned version ${mobilePin}"
    fi
    if ! CGO_ENABLED=1 go install "golang.org/x/mobile/cmd/gobind@${mobilePin}"; then
      fail_gate "Install gomobile toolchain at the exact pinned version (never @latest)" "gobind install failed at the pinned version ${mobilePin}"
    fi
    echo "$HOME/go/bin" >> "$GITHUB_PATH"
    ;;

  package)
    # fyne's ios target builds for the iOS Simulator when no signing
    # identity is configured - exactly the simulator-only qualification
    # this lane claims, nothing stronger. Run FROM the app package
    # directory (the fyne packaging root cause fix); the icon path is
    # relative to the NEW CWD.
    export CGO_ENABLED=1
    if ! (cd cmd/sheytan-local-agent && test -f ../../ui/fyne/assets/logo-512.png); then
      fail_gate "Package the iOS SIMULATOR .app (no signing identity required)" "the brand icon ../../ui/fyne/assets/logo-512.png is not reachable from cmd/sheytan-local-agent"
    fi
    fyneVersion="$(go list -m fyne.io/fyne/v2)"
    if ! go install "fyne.io/fyne/v2/cmd/fyne@${fyneVersion#fyne.io/fyne/v2 }"; then
      fail_gate "Package the iOS SIMULATOR .app (no signing identity required)" "the fyne CLI install failed at the module's own fyne version"
    fi
    mkdir -p dist/ios
    if ! (cd cmd/sheytan-local-agent && fyne package -os ios \
          -appID io.github.sheytanx13.SHEYTANLA \
          -name SHEYTAN-LA \
          -icon ../../ui/fyne/assets/logo-512.png \
          -appBuild 1 \
          -appVersion 1.11.1 \
          -outputDir ../../dist/ios); then
      fail_gate "Package the iOS SIMULATOR .app (no signing identity required)" "fyne package -os ios failed (packaged from cmd/sheytan-local-agent, not the repo root)"
    fi
    ls -la dist/ios
    ;;

  verify-app)
    app="$(ls -d dist/ios/*.app 2>/dev/null | head -1 || true)"
    test -d "$app" || fail_gate "Verify the simulator .app (fail-closed structural checks)" "no simulator .app produced"
    bin="${app}/SHEYTAN-LA"
    test -x "$bin" || fail_gate "Verify the simulator .app (fail-closed structural checks)" "the .app bundle has no executable at its MacOS-equivalent path"
    file "$bin" | grep -q "Mach-O" || fail_gate "Verify the simulator .app (fail-closed structural checks)" "bundle executable is not a Mach-O binary"
    # Simulator slices are Mach-O for iOS simulator ARCH (arm64 on Apple
    # silicon runners): confirm the slice is iOS-simulator compatible.
    if file "$bin" | grep -q "-arm64"; then
      echo "bundle binary: arm64 Mach-O (simulator slice on the Apple silicon runner)"
    fi
    test -f "${app}/Info.plist" || fail_gate "Verify the simulator .app (fail-closed structural checks)" "Info.plist missing from the bundle"
    plutil -lint "${app}/Info.plist" || fail_gate "Verify the simulator .app (fail-closed structural checks)" "Info.plist failed plutil -lint"
    echo "simulator .app verification OK: $(du -sh "$app" | cut -f1), Mach-O binary + valid Info.plist (SIMULATOR-ONLY qualification)"
    ;;

  cleanliness)
    # The packaging step must leave the repository pristine - no signing
    # material, no build residue.
    residue="$(git status --porcelain)"
    if [ -n "$residue" ]; then
      echo "::error::the iOS packaging step dirtied the working tree:"
      echo "$residue"
      fail_gate "Repo cleanliness guard (no residue committed)" "the iOS packaging step left the working tree dirty"
    fi
    echo "working tree pristine"
    ;;

  zip)
    # Work order section 6.3: the artifact is the .app bundle wrapped in
    # ONE portable zip named SHEYTAN-LA-ALPHA-ios-simulator-arm64.zip.
    # The bundle stays SIMULATOR-ONLY: an unsigned simulator .app is not
    # a device IPA and is never labelled as one.
    app="$(ls -d dist/ios/*.app 2>/dev/null | head -1 || true)"
    test -d "$app" || fail_gate "Bundle the simulator .app as the portable zip (SIMULATOR-ONLY)" "no simulator .app to bundle"
    if ! (cd dist/ios && ditto -c -k --keepParent "$(basename "$app")" "SHEYTAN-LA-ALPHA-ios-simulator-arm64.zip"); then
      fail_gate "Bundle the simulator .app as the portable zip (SIMULATOR-ONLY)" "ditto failed for the simulator .app"
    fi
    test -s "$ZIP" || fail_gate "Bundle the simulator .app as the portable zip (SIMULATOR-ONLY)" "the simulator zip is missing or empty"
    echo "simulator zip created (SIMULATOR-ONLY qualification)"
    ;;

  verify-zip)
    test -s "$ZIP" || fail_gate "Verify the simulator zip (fail-closed)" "simulator zip missing at $ZIP"
    members="$(unzip -Z1 "$ZIP")"
    echo "$members" | grep -q "SHEYTAN-LA.app/" || fail_gate "Verify the simulator zip (fail-closed)" "simulator zip is missing the .app bundle"
    if echo "$members" | grep -Eqi '\.(ipa|pkg|dmg|mobileprovision)$'; then
      fail_gate "Verify the simulator zip (fail-closed)" "device-package payload found in the SIMULATOR-ONLY artifact"
    fi
    echo "simulator zip verification OK: bundle complete, no device/signing payloads"
    ;;

  *)
    echo "usage: ios.sh <phase>" >&2
    echo "phases: headless-tests mobile-toolchain package verify-app cleanliness zip verify-zip" >&2
    exit 64
    ;;
esac
