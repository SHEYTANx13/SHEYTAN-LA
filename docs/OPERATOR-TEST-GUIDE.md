# Operator Test Guide — SHEYTAN-LA ALPHA (portable test builds)

> Stage: ALPHA. Portable archives only — there are no installers.
> Every claim below is what the producing CI lane actually verified;
> anything a lane did NOT verify is stated on the artifact.

## 1. First launch

- Windows x64: unzip `SHEYTAN-LA-ALPHA-windows-x64-portable.zip`, run
  `SHEYTAN-LA.bat` (or `SHEYTAN-LA.exe`). User data lives under
  `data\` next to the executable; static content under `resources\`.
- Linux x64: untar `SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz`, run
  `./SHEYTAN-LA`.
- macOS arm64/x64: unzip the portable zip, right-click open the
  ad-hoc-signed `SHEYTAN-LA.app` (unnotarized — Gatekeeper requires the
  explicit right-click open). NO notarization is claimed.
- Android arm64: the debug-signed APK is a development/test artifact;
  physical-device behavior is never asserted from CI (no emulator
  smoke on hosted runners).
- iOS: the artifact is a SIMULATOR-ONLY .app bundle; it is not a
  device IPA and no signing identity is involved.

The CI lanes performed a bounded launch smoke per desktop platform
(window observable, soak, graceful close). That is NOT a full UI
qualification.

## 2. Model setup and inference

Models are downloaded in-app (never bundled). First run without a
model shows an informative setup state — not a Send-time error. Engine
readiness is earned through staged states
(DISCOVERED→AVAILABLE→DOWNLOADING→INSTALLED→INTEGRITY_VERIFIED→
LAUNCH_VERIFIED→READY); a READY badge means the engine handshake
actually succeeded.

## 3. What to test (in order)

1. First launch + onboarding cards + settings.
2. Model import/download (pause/resume/cancel a download; verify the
   checksum gate by interrupting one).
3. A chat turn against the installed model (stop/regenerate/copy).
4. Workspace/project open + repository scan exclusions.
5. Git clone of a small public repo (observe the destination
   confirmation; no push/commit remote write exists in the product).
6. Diagnostics page: startup phases, engine state, queue view, support
   bundle (redaction preview; nothing uploads without consent).
7. Restart recovery: leave a download paused, restart, resume.

## 4. Known constraints for this candidate

- The desktop shell is the Fyne UI; the Go-Gui migration is authorized
  work, not shipped behavior.
- Hosted CI for the current candidate (`work/alpha-run443063-repair`)
  is NOT RUN until the operator authorizes a push; the platform lanes'
  own hosted evidence is the authority for any package's qualification.
- The consolidated CI diagnostic is errors/warnings-only
  (`SHEYTAN-LA-errors-warnings.txt`); application runtime logs remain
  structured and local-only.
