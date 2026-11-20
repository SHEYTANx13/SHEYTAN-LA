# iOS Device Signing — Secret Contract and Honest Gates

Run 44547031698 §6.3: the user wants a device-installable IPA. An
installable IPA requires a valid Apple signing identity AND a provisioning
profile that covers the target device; without them no honest device
deliverable exists. The `ios-device-arm64` matrix lane
(`scripts/build/ios-device.sh`) therefore never fabricates one.

## Secret contract (GitHub Actions secrets, aligned with the lane)

| Secret | Contents |
|---|---|
| `IOS_DISTRIBUTION_P12_BASE64` | base64 of the signing certificate (.p12) |
| `IOS_DISTRIBUTION_P12_PASSWORD` | the .p12 password |
| `IOS_ADHOC_PROFILE_BASE64` | base64 of an Ad Hoc (or Development) provisioning profile |
| `IOS_TEAM_ID` | the Apple development team identifier |
| `IOS_BUNDLE_ID` | the signed bundle identifier |

Rules enforced by the lane:

- Secrets are read from the environment only; they are never printed,
  echoed, or written into artifacts, logs, or the source tree.
- The P12 and profile are materialized only inside the per-job workspace
  (`ci-logs/`, `dist/ios/`) and deleted by the cleanup phase — which runs
  `always()`, even after a failure — together with the temporary per-job
  keychain.
- Without the secrets the lane records
  `BLOCKED BY MISSING APPLE SIGNING CONFIGURATION` naming the exact missing
  secret names; the job stays green and the package gate emits
  `PACKAGE_GATE_RESULT BLOCKED_BY_MISSING_APPLE_SIGNING`. No IPA is
  produced and none is claimed.

## Ad Hoc installation requirement

An Ad Hoc profile lists the registered device UDIDs it may install on. A
recipient device MUST have its UDID inside the profile, or iOS refuses the
installation. Physical installation on a registered device is verified ONLY
when such a device is attached to the runner; otherwise the lane states
`NOT tested here without a registered device` and the IPA carries the
profile/signature verification evidence only.

## Verification the lane enforces (fail-closed)

`Payload/<App>.app` root, `codesign -vv` signature, team identifier match,
arm64 iphoneos Mach-O slice, embedded decodable `embedded.mobileprovision`
with an Entitlements dictionary. An unsigned simulator `.app`, an unsigned
device `.app`, or a renamed ZIP is never a device deliverable.
