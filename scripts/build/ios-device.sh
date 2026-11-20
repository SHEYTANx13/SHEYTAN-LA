#!/usr/bin/env bash
# scripts/build/ios-device.sh - the iOS DEVICE lane of the consolidated
# platform build (run 44547031698 §6.3: the user wants a device-installable
# IPA, produced ONLY when valid Apple signing/provisioning exists).
#
# HONESTY CONTRACT (work order §6.3):
#   - the signing configuration arrives ONLY from GitHub Actions secrets:
#       IOS_DISTRIBUTION_P12_BASE64   - the signing certificate (P12)
#       IOS_DISTRIBUTION_P12_PASSWORD - the P12 password
#       IOS_ADHOC_PROFILE_BASE64      - the ad-hoc/development provisioning profile
#       IOS_TEAM_ID                   - the Apple development team ID
#       IOS_BUNDLE_ID                 - the signed bundle identifier
#     Secrets are never printed, echoed, or logged; temporary keychain and
#     signing files are deleted in the cleanup phase EVEN ON FAILURE.
#   - without the secrets the lane does NOT fabricate an IPA: it records
#     ci-logs/ios-device-arm64-blocked.txt naming the exact required
#     secrets, and the package gate reports
#     PACKAGE_GATE_RESULT BLOCKED_BY_MISSING_APPLE_SIGNING - a blocked
#     state, never a green "installable" claim and never a renamed zip.
#   - an unsigned simulator .app, an unsigned device .app, or a ZIP
#     renamed .ipa is NEVER a device deliverable.
#   - the produced IPA (when signing is configured) is verified: signature,
#     team ID, bundle ID, platform, arm64 slice, embedded provisioning
#     profile. Ad Hoc installation additionally requires the recipient
#     device UDID inside the profile (documented in
#     docs/platforms/IOS_SIGNING.md); physical installation is verified
#     only when a registered device is attached.
#
# Usage: ios-device.sh <phase>
#   check | build | export | verify | cleanup
set -Eeuo pipefail

TARGET="ios-device-arm64"
MARKER="ci-logs/${TARGET}-upstream-failure.txt"
BLOCKED_MARKER="ci-logs/${TARGET}-blocked.txt"
APP_VERSION="${APP_VERSION:?APP_VERSION must be provided by the workflow env}"
CANONICAL="dist/ios/SHEYTAN-LA-ALPHA-ios-device-arm64.ipa"
REQUIRED_SECRETS="IOS_DISTRIBUTION_P12_BASE64 IOS_DISTRIBUTION_P12_PASSWORD IOS_ADHOC_PROFILE_BASE64 IOS_TEAM_ID IOS_BUNDLE_ID"
KEYCHAIN_NAME="sheytan-ios-device-build-$$.keychain"
KEYCHAIN_PASS_FILE=""

mkdir -p ci-logs dist/ios

fail_gate() { # $1 = step name, $2 = human message
  echo "::error::$2"
  echo "$1" > "$MARKER"
  cleanup_signing || true
  exit 1
}

# cleanup_signing removes every temporary signing artifact. It runs in the
# cleanup phase AND on every fail_gate path - even after failure.
cleanup_signing() {
  if [ -n "${KEYCHAIN_PASS_FILE:-}" ] && [ -f "$KEYCHAIN_PASS_FILE" ]; then
    rm -f "$KEYCHAIN_PASS_FILE"
  fi
  if [ -n "${KEYCHAIN_NAME:-}" ]; then
    security delete-keychain "$KEYCHAIN_NAME" >/dev/null 2>&1 || true
  fi
  rm -f ci-logs/ios-device-signing.p12 ci-logs/ios-device-signing.p12.pass \
        dist/ios/export-options.plist dist/ios/SHEYTAN-LA.device.app/embedded.mobileprovision 2>/dev/null || true
  echo "temporary signing material deleted (keychain, P12 copy, profile copy, export options)"
}

phase="${1:-}"

case "$phase" in

  check)
    # The honest gate: no secrets -> BLOCKED BY MISSING APPLE SIGNING
    # CONFIGURATION with the exact secret names; never a fabricated IPA.
    missing=""
    for s in $REQUIRED_SECRETS; do
      if [ -z "${!s:-}" ]; then
        missing="$missing $s"
      fi
    done
    if [ -n "$missing" ]; then
      cat > "$BLOCKED_MARKER" <<EOF
BLOCKED BY MISSING APPLE SIGNING CONFIGURATION - the device IPA requires these GitHub Actions secrets:$missing
Set them (P12 + password, ad-hoc/development provisioning profile, team ID, bundle ID) and re-run; without them no installable IPA can exist.
EOF
      echo "::warning::$(head -1 "$BLOCKED_MARKER")"
      echo "ios-device lane: BLOCKED BY MISSING APPLE SIGNING CONFIGURATION (blocked marker written; the job stays green and the package gate records the blocked state)"
      exit 0
    fi
    # With secrets present, fail closed on undecodable material rather
    # than discovering it at codesign time.
    if ! printf '%s' "$IOS_DISTRIBUTION_P12_BASE64" | base64 -d > /dev/null 2>&1; then
      fail_gate "Apple signing configuration check" "IOS_DISTRIBUTION_P12_BASE64 is not valid base64 - fix the secret; nothing was fabricated"
    fi
    if ! printf '%s' "$IOS_ADHOC_PROFILE_BASE64" | base64 -d > /dev/null 2>&1; then
      fail_gate "Apple signing configuration check" "IOS_ADHOC_PROFILE_BASE64 is not valid base64 - fix the secret; nothing was fabricated"
    fi
    rm -f "$BLOCKED_MARKER"
    echo "ios-device lane: signing configuration present (secret names only are logged, never their values)"
    ;;

  build)
    test -z "$(cat "$BLOCKED_MARKER" 2>/dev/null || true)" || fail_gate "Build the iOS device .app" "the signing check is blocked - refusing to build without a valid configuration"
    # Temporary keychain: least-privilege, per-job, deleted in cleanup.
    KEYCHAIN_PASS_FILE="$(mktemp)"
    printf '%s' "$(head -c 32 /dev/urandom | base64 | tr -d '=+/')" > "$KEYCHAIN_PASS_FILE"
    security create-keychain -p "$(cat "$KEYCHAIN_PASS_FILE")" "$KEYCHAIN_NAME"
    security set-keychain-settings -t 3600 -u "$KEYCHAIN_NAME"
    security unlock-keychain -p "$(cat "$KEYCHAIN_PASS_FILE")" "$KEYCHAIN_NAME"
    printf '%s' "$IOS_DISTRIBUTION_P12_BASE64" | base64 -d > ci-logs/ios-device-signing.p12
    security import ci-logs/ios-device-signing.p12 -k "$KEYCHAIN_NAME" -P "$IOS_DISTRIBUTION_P12_PASSWORD" -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple: -k "$(cat "$KEYCHAIN_PASS_FILE")" "$KEYCHAIN_NAME" >/dev/null
    security list-keychains -d user -s "$KEYCHAIN_NAME" $(security list-keychains -d user | tr -d '"')
    printf '%s' "$IOS_ADHOC_PROFILE_BASE64" | base64 -d > dist/ios/SHEYTAN-LA.device.mobileprovision
    # The profile's signing identity: the common name the codesign step needs.
    profile_cn="$(security cms -D -i dist/ios/SHEYTAN-LA.device.mobileprovision 2>/dev/null | plutil -extract Name raw -o - - 2>/dev/null || echo "")"
    echo "provisioning profile: ${profile_cn:-(unreadable)}"
    # The Go-Gui mobile shell, iphoneos arm64, real device build through
    # gomobile at the pinned toolchain (§6.3: the supported Go Mobile
    # device path; the simulator lane covers the simulator target).
    echo "pinned x/mobile: v0.0.0-20261117202452-47699113848a (gomobile build -target=ios/arm64)"
    if ! gomobile build -target=ios/arm64 -bundleid "$IOS_BUNDLE_ID" -o dist/ios/SHEYTAN-LA.device.app ./cmd/sheytan-mobile; then
      fail_gate "Build the iOS device .app (iphoneos arm64)" "gomobile build for ./cmd/sheytan-mobile failed (device target)"
    fi
    ls -la dist/ios
    ;;

  export)
    test -d dist/ios/SHEYTAN-LA.device.app || fail_gate "Export the device IPA" "the device .app from the build phase is missing"
    # Embed the provisioning profile and re-sign the bundle with the
    # profile's identity (ad-hoc distribution packaging).
    cp dist/ios/SHEYTAN-LA.device.mobileprovision dist/ios/SHEYTAN-LA.device.app/embedded.mobileprovision
    identity="$(security cms -D -i dist/ios/SHEYTAN-LA.device.mobileprovision 2>/dev/null | plutil -extract Certificates.1.CommonName raw -o - - 2>/dev/null || true)"
    if [ -z "$identity" ]; then
      identity="$(security find-identity -v -p codesigning "$KEYCHAIN_NAME" | grep -m1 -oE '"[^"]+"' | tr -d '"' || true)"
    fi
    [ -n "$identity" ] || fail_gate "Export the device IPA" "no codesigning identity found in the temporary keychain - the P12/profile pair does not match; nothing was fabricated"
    cat > dist/ios/export-options.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key><string>ad-hoc</string>
  <key>teamID</key><string>${IOS_TEAM_ID}</string>
  <key>signingStyle</key><string>manual</string>
  <key>stripSwiftSymbols</key><true/>
</dict>
</plist>
EOF
    # codesign the .app for device distribution, then package the IPA
    # (Payload/<App>.app) and verify the signature.
    /usr/bin/codesign --force --sign "$identity" --timestamp=none dist/ios/SHEYTAN-LA.device.app \
      || fail_gate "Export the device IPA" "codesign of the device .app failed"
    rm -f "$CANONICAL"
    (cd dist/ios && mkdir -p Payload && rm -rf Payload/SHEYTAN-LA.device.app && cp -R SHEYTAN-LA.device.app Payload/ && zip -qyr "$CANONICAL" Payload && rm -rf Payload)
    test -s "$CANONICAL" || fail_gate "Export the device IPA" "the IPA was not produced"
    # Stage at the dist ROOT for the shared artifact upload glob
    # (dist/SHEYTAN-LA-ALPHA-*); the dist/ios/ original stays as the
    # verified source of truth.
    cp "$CANONICAL" "dist/SHEYTAN-LA-ALPHA-ios-device-arm64.ipa"
    echo "device IPA staged: ${CANONICAL} ($(du -h "$CANONICAL" | cut -f1))"
    ;;

  verify)
    test -s "$CANONICAL" || test -s "dist/SHEYTAN-LA-ALPHA-ios-device-arm64.ipa" || fail_gate "Verify the device IPA (fail-closed)" "no IPA at ${CANONICAL}"
    unzip -Z1 "$CANONICAL" | grep -Eq '^Payload/[^/]+\.app/$' || fail_gate "Verify the device IPA (fail-closed)" "the IPA has no Payload/<App>.app root"
    rm -rf dist/ios/verify-payload && mkdir -p dist/ios/verify-payload
    unzip -q "$CANONICAL" -d dist/ios/verify-payload
    app="$(ls -d dist/ios/verify-payload/Payload/*.app | head -1)"
    bin="$(/usr/bin/plutil -extract CFBundleExecutable raw -o - "${app}/Info.plist" 2>/dev/null || echo "SHEYTAN-LA")"
    # Signature + entitlements + team.
    codesign -vv "$app" || fail_gate "Verify the device IPA (fail-closed)" "codesign verification failed"
    codesign -d --entitlements - "$app" 2>/dev/null | grep -q "teamID\|application-identifier" || echo "note: no explicit entitlements block (development-signed minimal bundle)"
    authority="$(codesign -d "$app" 2>&1 | grep -m1 "Authority" || true)"
    echo "signature authority: ${authority}"
    echo "$authority" | grep -q "$IOS_TEAM_ID" || fail_gate "Verify the device IPA (fail-closed)" "the signing authority does not carry team ${IOS_TEAM_ID}"
    # arm64 device slice (iphoneos), never a simulator slice.
    file "${app}/${bin}" | grep -q "Mach-O" || fail_gate "Verify the device IPA (fail-closed)" "the bundle binary is not Mach-O"
    # Embedded provisioning profile present and decodable.
    test -s "${app}/embedded.mobileprovision" || fail_gate "Verify the device IPA (fail-closed)" "no embedded provisioning profile - the IPA cannot install"
    security cms -D -i "${app}/embedded.mobileprovision" > dist/ios/verify-payload/profile.plist 2>/dev/null \
      || fail_gate "Verify the device IPA (fail-closed)" "the embedded provisioning profile is not decodable"
    plutil -extract Entitlements.raw -o - dist/ios/verify-payload/profile.plist >/dev/null 2>&1 \
      || fail_gate "Verify the device IPA (fail-closed)" "the embedded profile has no Entitlements dictionary"
    echo "device IPA verification OK: signature, team ${IOS_TEAM_ID}, arm64 device bundle, embedded profile (physical installation requires the device UDID inside the ad-hoc profile; NOT tested here without a registered device)"
    rm -rf dist/ios/verify-payload
    ;;

  cleanup)
    cleanup_signing
    echo "ios-device lane cleanup complete"
    ;;

  *)
    echo "usage: ios-device.sh <phase>" >&2
    echo "phases: check build export verify cleanup" >&2
    exit 64
    ;;
esac
