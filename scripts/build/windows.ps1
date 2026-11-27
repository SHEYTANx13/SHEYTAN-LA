# scripts/build/windows.ps1 - the ONE authoritative Windows x64 build lane
# (CI consolidation: the retired .github/workflows/build-windows.yml
# reusable lane, now dispatched per-phase by the platform-build matrix
# job of .github/workflows/build.yml).
#
# Contract carried over from build-windows.yml:
#   - every verification gate writes the upstream-failure marker
#     ci-logs/windows-x64-upstream-failure.txt (the FIRST failed gate's
#     step name) before failing, so the if:always() package gate reports
#     BLOCKED_BY_UPSTREAM_TEST_FAILURE naming that gate - ONE root cause,
#     never a second missing-package error (work order 4.3.4/4.3.5);
#   - the headless core suite always passes -json so the consolidated
#     run-log collector's stateful `go test -json` classifier works;
#   - PowerShell error contract (work order 4.2): $? is captured
#     immediately after a pure PowerShell script invocation;
#     $LASTEXITCODE is read ONLY after native executables (go, pwsh -File,
#     git, 7z), captured immediately. scripts/ci/StepInvocationContract.Tests.ps1
#     pins the contract.
#   - NO installer is produced: the portable ZIP is the ONE supported
#     package (master work order section 10).
#
# Usage: windows.ps1 -Phase <name> [-Target windows-x64]
#   Phases: gofmt | vet | headless-tests | release-contract | version |
#   mesa-acquire | ipc-integration | gen-syso | stage | build | verify-pe |
#   stage-smoke | dep-audit | launch-smoke | collect-smoke-diagnostics |
#   build-info
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$Phase,

  [string]$Target = "windows-x64"
)

$ErrorActionPreference = "Stop"

$markerPath = "ci-logs/$Target-upstream-failure.txt"
$pkgRoot = $env:WIN_PKG_ROOT
if (-not $pkgRoot) { $pkgRoot = "SHEYTAN-LA" }
$stagingRoot = "dist/SHEYTAN-LA-ALPHA-windows-x64-portable/$pkgRoot"
$goTestTimeout = if ($env:GO_TEST_TIMEOUT) { $env:GO_TEST_TIMEOUT } else { "25m" }

New-Item -ItemType Directory -Force -Path ci-logs | Out-Null

# Fail-Gate writes the upstream-failure marker (the step name) and throws -
# the pwsh marker pattern the platform-build contract requires:
#   try { ... } catch { Write-Host '::error::...';
#         Set-Content -Path ci-logs/<target>-upstream-failure.txt -Value '<step>';
#         throw }
function Fail-Gate {
  param([string]$StepName, [string]$Message)
  Write-Host "::error::$Message"
  Set-Content -Path $markerPath -Value $StepName -Encoding utf8NoBOM
  throw $Message
}

# The UCRT64 CGO environment for every Fyne-touching invocation (msys2
# October 2026: MINGW64 is deprecated - UCRT64 is the supported CRT).
function Use-Ucrt64 {
  $env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
  $env:CGO_ENABLED = "1"
}

switch ($Phase) {

  # ------------------------------------------------------------------
  "gofmt" {
    # Evidence first: the exact formatter binary version is part of the
    # gate's contract (gofmt rules are version-sensitive; the version
    # skew was run 44378562071's root cause). $LASTEXITCODE is read
    # immediately after the native `go` invocation (StepInvocationContract).
    $gov = (go version) 2>&1
    if ($LASTEXITCODE -ne 0) { Fail-Gate "gofmt (enforced)" "go version failed for gofmt evidence" }
    "toolchain: $gov" | Set-Content -Path "ci-logs/win-gofmt.log" -Encoding utf8NoBOM
    # SCOPE PARITY (run 44378562071 section 4.3.3): the ONE canonical
    # scope, identical to every lane's gofmt gate: cmd internal ui scripts.
    $unformatted = gofmt -l cmd internal ui scripts
    if ($LASTEXITCODE -ne 0) { Fail-Gate "gofmt (enforced)" "gofmt invocation failed" }
    if ($unformatted) {
      $unformatted | Add-Content -Path "ci-logs/win-gofmt.log" -Encoding utf8NoBOM
      Fail-Gate "gofmt (enforced)" "gofmt violations - run gofmt -w on: $unformatted"
    }
    Write-Host "gofmt OK"
  }

  # ------------------------------------------------------------------
  "vet" {
    Use-Ucrt64
    $null = New-Item -ItemType Directory -Force -Path ci-logs
    go vet ./... 2>&1 | Tee-Object -FilePath "ci-logs/win-vet.log"
    if ($LASTEXITCODE -ne 0) {
      Fail-Gate "go vet ./..." "go vet failed on windows/amd64 (exit $LASTEXITCODE; output in ci-logs/win-vet.log)"
    }
    Write-Host "go vet OK (windows/amd64)"
  }

  # ------------------------------------------------------------------
  "headless-tests" {
    Use-Ucrt64
    # -json is CONTRACTUAL: the consolidated run-log collector's stateful
    # `go test -json` classifier (run 44457547289 3.4) needs the stream.
    go test -tags headless -json -count=1 -timeout $goTestTimeout ./internal/... 2>&1 | Tee-Object -FilePath "ci-logs/win-go-test-headless.log"
    if ($LASTEXITCODE -ne 0) {
      # Run 45424141601 repair (lane parity with macos.sh/linux.sh): a
      # bare exit code is not a diagnosis - surface the suite's own
      # root-cause records AND the failing tests' message lines (the
      # walk-diag stage evidence) as ::error:: annotations, bounded.
      # Run 45522548507 repair (verdict-aware messages): collect the
      # FAILED test names from the -json fail events FIRST - an output
      # event can echo the literal string, so fail events are the lines
      # that are NOT output events - and emit only those tests' message
      # lines. The previous verdict-blind selector surfaced SKIP messages
      # and success-path t.Logf lines as "failure evidence" (8 of the 9
      # records in run 45522548507 described tests that never failed).
      # Messages are emitted before frames so the consolidated channel's
      # bounded budget carries the WHY.
      Write-Host "::error::headless suite failure evidence (windows/amd64): the suite's own root-cause records follow (bounded to 15; the full -json stream is in the step log)"
      $failedTests = @{}
      Get-Content "ci-logs/win-go-test-headless.log" | ForEach-Object {
        if ($_.Contains('"Action":"fail"') -and -not $_.Contains('"Action":"output"')) {
          if ($_ -match '"Test":"([^"]+)"') { $failedTests[$Matches[1]] = $true } else { $failedTests[''] = $true }
        }
      }
      Select-String -Path "ci-logs/win-go-test-headless.log" -Pattern '(walk-diag |\s[A-Za-z0-9_/]+\.go:[0-9]+: )' | Where-Object { $_.Line -notmatch '\.go:[0-9]+: (SKIP|skip)' } | ForEach-Object {
        $test = ''
        if ($_.Line -match '"Test":"([^"]+)"') { $test = $Matches[1] }
        if ($failedTests.ContainsKey($test)) { $_ }
      } | Select-Object -First 15 | ForEach-Object {
        Write-Host "::error::windows/amd64 failure message: $($_.LineNumber):$($_.Line)"
      }
      Select-String -Path "ci-logs/win-go-test-headless.log" -Pattern '(--- FAIL:|panic: |fatal error: |^FAIL|test timed out|signal: [A-Z]+)' | Select-Object -First 15 | ForEach-Object {
        Write-Host "::error::windows/amd64 suite evidence: $($_.LineNumber):$($_.Line)"
      }
      Fail-Gate "Headless core tests (windows/amd64)" "headless core tests failed on windows/amd64 (exit $LASTEXITCODE; failing test names in ci-logs/win-go-test-headless.log)"
    }
    Write-Host "headless core tests OK (windows/amd64)"
  }

  # ------------------------------------------------------------------
  "release-contract" {
    go test ./internal/releasecontract/ -count=1
    if ($LASTEXITCODE -ne 0) { Fail-Gate "Release-contract suite (windows/amd64)" "release-contract tests failed on windows/amd64" }
    Write-Host "release-contract suite OK (windows/amd64)"
  }

  # ------------------------------------------------------------------
  "version" {
    # APP_VERSION derives from the ONE Go authority (internal/config
    # AppVersion) via the CGO-free resolver - run #54 root cause.
    $raw = $null
    $code = 0
    try {
      $env:CGO_ENABLED = "0"
      $raw = go run ./cmd/printversion --plain 2>&1
      $code = $LASTEXITCODE
    } finally {
      $env:CGO_ENABLED = "1"
    }
    $raw | Add-Content -Path ci-logs/win-version-resolution.log -Encoding utf8NoBOM
    if ($code -ne 0) {
      Fail-Gate "Resolve APP_VERSION from the Go authority" "printversion exited $code - version resolution failed (output above)"
    }
    $resolved = ([string]$raw).Trim()
    if ($resolved -notmatch '^\d+\.\d+\.\d+$') {
      Fail-Gate "Resolve APP_VERSION from the Go authority" "printversion produced a malformed version: '$resolved'"
    }
    if ($resolved -ne $env:APP_VERSION) {
      Fail-Gate "Resolve APP_VERSION from the Go authority" "workflow APP_VERSION ($env:APP_VERSION) disagrees with internal/config.AppVersion ($resolved) - one authority only"
    }
    Write-Host "APP_VERSION = $resolved (matches internal/config.AppVersion)"
    "EXIT_CODE: 0 PASS" | Add-Content -Path ci-logs/win-version-resolution.log -Encoding utf8NoBOM
  }

  # ------------------------------------------------------------------
  "mesa-acquire" {
    # Pinned Mesa archive: cache-or-download with checksum verification,
    # fail-closed (the smoke needs the pinned llvmpipe two-DLL set; the
    # cache restore step runs in the workflow before this phase).
    $ErrorActionPreference = "Stop"
    $ProgressPreference = 'SilentlyContinue'
    $mesaLog = "ci-logs/win-mesa-acquire.log"
    function Log([string]$m) { Write-Host $m; Add-Content -Path $mesaLog -Value $m -Encoding utf8NoBOM }
    $mesaUrl = "https://github.com/pal1000/mesa-dist-win/releases/download/26.2.5/mesa3d-26.2.5-release-msvc.7z"
    $mesaSha256 = "997123ef4ce8896204ab912d5b54f19372c487ff9835bcefafa2fca23b8953c2"
    $mesaArchive = Join-Path $env:RUNNER_TEMP "mesa3d-26.2.5-release-msvc.7z"
    $stage = "resolve-source"
    try {
      if (Test-Path $mesaArchive) {
        Log "pinned Mesa archive present after cache restore - re-verifying (cached is not trusted until re-hashed)"
      } else {
        Log "pinned Mesa archive not restored from cache - downloading $mesaUrl"
        $stage = "download"
        $downloaded = $false
        for ($attempt = 1; $attempt -le 3 -and -not $downloaded; $attempt++) {
          try {
            Log ("download attempt {0}/3 via Invoke-WebRequest (progress rendering disabled)" -f $attempt)
            Invoke-WebRequest -Uri $mesaUrl -OutFile $mesaArchive -ErrorAction Stop
            $downloaded = $true
            Log ("download attempt {0} succeeded ({1} bytes)" -f $attempt, (Get-Item $mesaArchive).Length)
          } catch {
            Log ("download attempt {0} failed: {1}" -f $attempt, $_.Exception.Message)
            if (Test-Path $mesaArchive) { Remove-Item $mesaArchive -Force -ErrorAction SilentlyContinue }
          }
        }
        if (-not $downloaded) {
          $stage = "download-curl-fallback"
          Log "all 3 Invoke-WebRequest attempts failed - falling back to curl.exe -L --retry 3 (preinstalled on the runner)"
          & curl.exe -L --retry 3 --fail --show-error --output $mesaArchive $mesaUrl 2>&1 | ForEach-Object { Log ("curl: {0}" -f $_) }
          if ($LASTEXITCODE -ne 0 -or -not (Test-Path $mesaArchive)) {
            throw ("Mesa acquisition failed after 3 Invoke-WebRequest attempts and the curl.exe fallback (curl exit {0})" -f $LASTEXITCODE)
          }
          Log ("curl.exe fallback succeeded ({0} bytes)" -f (Get-Item $mesaArchive).Length)
        }
      }
      $stage = "checksum"
      $hash = (Get-FileHash -Algorithm SHA256 $mesaArchive).Hash.ToLowerInvariant()
      Log ("sha256 got {0}" -f $hash)
      Log ("sha256 want {0}" -f $mesaSha256)
      if ($hash -ne $mesaSha256) {
        Remove-Item $mesaArchive -Force
        throw ("Mesa checksum mismatch: got {0}, want {1}" -f $hash, $mesaSha256)
      }
      Log "pinned Mesa archive VERIFIED (sha256 OK) at $mesaArchive - the staging step lists + extracts it after the build"
    } catch {
      Log ("::error::Mesa acquisition failed at stage '{0}': {1}" -f $stage, $_.Exception.Message)
      Fail-Gate "Acquire pinned Mesa archive (cache-or-download, verify checksum)" ("Mesa acquisition failed at stage '{0}': {1}" -f $stage, $_.Exception.Message)
    }
  }

  # ------------------------------------------------------------------
  "ipc-integration" {
    # v1.11.1 Windows IPC repair: the REAL production CoreClient over the
    # REAL Windows named pipe - handshake, representative read-only APIs,
    # the WebSocket attach path with a normal close, wrong-key refusal,
    # and the transport contract tests. No GPU, no model, no outbound net.
    Use-Ucrt64
    go test ./internal/localipc/ -count=1 -timeout 15m -v 2>&1 | Tee-Object -FilePath "ci-logs/win-localipc-tests.log"
    if ($LASTEXITCODE -ne 0) {
      Fail-Gate "Windows named-pipe CoreClient integration (production client over localipc.Serve)" "localipc transport tests failed on windows/amd64 (exit $LASTEXITCODE; output in ci-logs/win-localipc-tests.log)"
    }
    # Run 45716559459 root cause: the client cluster moved ui/fyne ->
    # internal/coreclient (the run 45634345922 flip deleted ui/fyne);
    # this gate still named the deleted package and failed at compile.
    # The production CoreClient integration suite now runs from its one
    # tracked home.
    go test ./internal/coreclient/ -count=1 -timeout 15m -v -run "TestCoreClientEndToEndOverWindowsNamedPipe|TestCoreClientRejectsWrongKeyWindows|TestCoreClientWebSocketCallerCancelWindows|TestWindowsIPCFlowOpensNoTCPListener|TestCoreClientRefusesMismatchedProtocolWindows" 2>&1 | Tee-Object -FilePath "ci-logs/win-ipc-integration.log"
    if ($LASTEXITCODE -ne 0) {
      Fail-Gate "Windows named-pipe CoreClient integration (production client over localipc.Serve)" "Windows named-pipe CoreClient integration failed (exit $LASTEXITCODE) - the production client cannot reach the core through the pipe"
    }
    Write-Host "IPC integration gate: production CoreClient handshake + API + WebSocket over the Windows named pipe PASSED"
  }

  # ------------------------------------------------------------------
  "gen-syso" {
    Use-Ucrt64
    go run ./scripts/gen-syso
    if ($LASTEXITCODE -ne 0) { Fail-Gate "Windows resource pipeline (gen-syso: icon + version + DPI manifest)" "gen-syso failed (exit $LASTEXITCODE)" }
  }

  # ------------------------------------------------------------------
  "stage" {
    # v1.11.1 MINIMAL INSTALLED ROOT: the portable application directory
    # carries ONLY the runtime-required items - the executable, the
    # launcher and the canonical resources/docs + resources/brand trees.
    $null = New-Item -ItemType Directory -Force -Path "$stagingRoot/resources/docs"
    $null = New-Item -ItemType Directory -Force -Path "$stagingRoot/resources/brand"
    Copy-Item "packaging/SHEYTAN-LA.bat" "$stagingRoot/SHEYTAN-LA.bat"
    Copy-Item "LICENSE.md" "$stagingRoot/resources/docs/LICENSE.md"
    Copy-Item "SIGNATURE" "$stagingRoot/resources/docs/SIGNATURE"
    Copy-Item "build/sheytan.ico" "$stagingRoot/resources/brand/sheytan.ico"
    Write-Host "staged $stagingRoot (minimal installed root)"
  }

  # ------------------------------------------------------------------
  "build" {
    Use-Ucrt64
    # -H=windowsgui is the STRESS PIN (3g) - kept. -s -w strips the symbol
    # tables and DWARF debug info (v1.11.0 size work).
    go build -tags webworkspace -ldflags "-H=windowsgui -s -w" -o "$stagingRoot/SHEYTAN-LA.exe" ./cmd/sheytan-local-agent
    if ($LASTEXITCODE -ne 0) {
      Fail-Gate "Build SHEYTAN-LA.exe (windowsgui subsystem)" "SHEYTAN-LA.exe build failed (go build exit $LASTEXITCODE, windowsgui subsystem, CGO toolchain above)"
    }
    Write-Host "SHEYTAN-LA.exe built (windowsgui subsystem)"
  }

  # ------------------------------------------------------------------
  "verify-pe" {
    $exe = "$stagingRoot/SHEYTAN-LA.exe"
    if (-not (Test-Path $exe)) { Fail-Gate "Verify the PE (x86-64, windowsgui)" "PE verify guard: built executable missing at $exe" }
    $bytes = [System.IO.File]::ReadAllBytes($exe)
    if ($bytes[0] -ne 0x4D -or $bytes[1] -ne 0x5A) { Fail-Gate "Verify the PE (x86-64, windowsgui)" "PE verify guard: $exe is not a PE binary (missing MZ header)" }
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
    if ($bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45) { Fail-Gate "Verify the PE (x86-64, windowsgui)" "PE signature missing in $exe (peOffset=$peOffset)" }
    $machine = [BitConverter]::ToUInt16($bytes, $peOffset + 4)
    if ($machine -ne 0x8664) {
      Fail-Gate "Verify the PE (x86-64, windowsgui)" ("PE verify guard: PE machine = 0x{0:X4}, want x86-64 (0x8664) in {1}" -f $machine, $exe)
    }
    Write-Host "PE OK: x86-64"
  }

  # ------------------------------------------------------------------
  "stage-smoke" {
    # ZIP purity foundation (run #118 defect): the launch smoke NEVER
    # runs inside the staging tree - the smoke launches from a full copy
    # under RUNNER_TEMP with the pinned Mesa two-DLL set beside the exe.
    $ErrorActionPreference = "Stop"
    $stageLog = "ci-logs/win-smoke-staging.log"
    function Log([string]$m) { Write-Host $m; Add-Content -Path $stageLog -Value $m -Encoding utf8NoBOM }
    $stage = "smoke-copy"
    try {
      $src = (Resolve-Path $stagingRoot).Path
      $smokeRoot = Join-Path $env:RUNNER_TEMP "win-smoke\$pkgRoot"
      if (Test-Path $smokeRoot) { Remove-Item -Recurse -Force $smokeRoot }
      New-Item -ItemType Directory -Force -Path (Split-Path $smokeRoot) | Out-Null
      Copy-Item -Recurse -Force $src $smokeRoot
      if (-not (Test-Path (Join-Path $smokeRoot "SHEYTAN-LA.exe"))) {
        throw ("smoke copy is missing SHEYTAN-LA.exe under {0}" -f $smokeRoot)
      }
      $stage = "purity-guards"
      $stagingData = "$stagingRoot/data"
      if (Test-Path $stagingData) {
        throw ("pristine-staging violation: {0} exists BEFORE the smoke" -f $stagingData)
      }
      foreach ($banned in @("opengl32.dll", "libgallium_wgl.dll")) {
        if (Test-Path "$stagingRoot/$banned") {
          throw ("purity violation: {0} must never enter the staging tree" -f $banned)
        }
      }
      $stage = "listing-contract"
      $mesaArchive = Join-Path $env:RUNNER_TEMP "mesa3d-26.2.5-release-msvc.7z"
      if (-not (Test-Path $mesaArchive)) {
        throw ("pinned Mesa archive absent: {0} - the mesa-acquire phase did not run or failed" -f $mesaArchive)
      }
      # List before extracting: the two-DLL deployment contract is checked
      # against the archive's ACTUAL entries (normalized separators;
      # Windows 7-Zip may render entry paths with backslashes - run #120).
      $listing = & "C:\Program Files\7-Zip\7z.exe" l $mesaArchive | Out-String
      $rawLines = @($listing -split "`r?`n")
      $rawTail = ($rawLines | Select-Object -Last 12) -join "`n"
      Log "raw 7z listing tail (last 12 lines):"
      Log $rawTail
      $x64Entries = @($rawLines |
        ForEach-Object { ($_ -split '\s+')[-1] } |
        Where-Object { $_ -and $_.Trim() -ne "" } |
        ForEach-Object { $_.Trim() -replace '\\', '/' } |
        Where-Object { $_ -like "x64/*.dll" } |
        Sort-Object -Unique)
      Log ("parsed x64 DLL entries (normalized to forward slashes): {0}" -f ($x64Entries -join ', '))
      foreach ($required in @("x64/opengl32.dll", "x64/libgallium_wgl.dll")) {
        $hit = @($x64Entries | Where-Object { $_.ToLowerInvariant() -eq $required.ToLowerInvariant() })
        if ($hit.Count -eq 0) {
          throw ("Mesa archive contract violation: '{0}' absent from the pinned 26.2.5 release-msvc archive" -f $required)
        }
      }
      $stage = "two-dll-extraction"
      $mesaExtract = Join-Path $env:RUNNER_TEMP "mesa3d-extract"
      if (Test-Path $mesaExtract) { Remove-Item -Recurse -Force $mesaExtract }
      New-Item -ItemType Directory -Force -Path $mesaExtract | Out-Null
      # ONE invocation, BOTH DLLs, same pinned archive, same sha256.
      & "C:\Program Files\7-Zip\7z.exe" e "$mesaArchive" "-o$mesaExtract" "x64/opengl32.dll" "x64/libgallium_wgl.dll" -y | Out-Null
      if ($LASTEXITCODE -ne 0) {
        throw ("7z extraction of the Mesa two-DLL set failed (exit {0})" -f $LASTEXITCODE)
      }
      $stage = "two-dll-placement"
      foreach ($name in @("opengl32.dll", "libgallium_wgl.dll")) {
        $dll = Join-Path $mesaExtract $name
        if (-not (Test-Path $dll)) {
          throw ("extracted Mesa {0} missing under {1}" -f $name, $mesaExtract)
        }
        Copy-Item $dll (Join-Path $smokeRoot $name) -Force
      }
      Log "smoke copy staged at $smokeRoot with pinned Mesa llvmpipe opengl32.dll + libgallium_wgl.dll (two-DLL contract satisfied, purity guards passed)"
    } catch {
      Log ("::error::smoke staging failed at stage '{0}': {1}" -f $stage, $_.Exception.Message)
      Fail-Gate "Stage smoke copy (purity + two-DLL placement)" ("smoke staging failed at stage '{0}': {1}" -f $stage, $_.Exception.Message)
    }
  }

  # ------------------------------------------------------------------
  "dep-audit" {
    # Pre-launch dependency audit (run #119): 0xC0000135 is raised by the
    # Windows loader while resolving the STATIC import chain at process
    # init. v1.11.1 (run 41933333132) - the audit is a RELEASE GATE:
    #   exit 0 = PASS; exit 2 = FAIL (named missing DLL) - throws here;
    #   exit 3 = ERROR (audit could not complete) - NEVER a pass; the
    #   launch smoke still runs for evidence and the job fails at the end
    #   of that step via the ERROR marker.
    $smokeRoot = Join-Path $env:RUNNER_TEMP "win-smoke\$pkgRoot"
    if (Test-Path "ci-logs/win-dep-audit-ERROR.marker") { Remove-Item "ci-logs/win-dep-audit-ERROR.marker" -Force }
    $auditExit = 0
    try {
      & pwsh -NoProfile -NonInteractive -File ./scripts/ci/ImportAudit.ps1 -ExeDir $smokeRoot -Modules @("SHEYTAN-LA.exe", "opengl32.dll", "libgallium_wgl.dll") 2>&1 | Tee-Object -FilePath "ci-logs/win-smoke-dep-audit.log"
      $auditExit = $LASTEXITCODE
    } catch {
      $diag = "ImportAudit.ps1 crashed before producing an exit code: {0} | stack: {1}" -f $_.Exception.Message, ($_.ScriptStackTrace -replace "\r?\n", " | ")
      Write-Host ("::error::smoke dependency audit: {0} - the dependency closure is UNKNOWN (ERROR is never PASS)" -f $diag)
      $diag | Tee-Object -FilePath "ci-logs/win-smoke-dep-audit.log" -Append
      $auditExit = 3
    }
    if ($auditExit -eq 2) {
      Fail-Gate "Pre-launch dependency audit (static imports, release gate)" "smoke dependency audit: unresolvable static import - the launch would die with 0xC0000135 (named DLL in ci-logs/win-smoke-dep-audit.log)"
    }
    if ($auditExit -ne 0) {
      "audit exit $auditExit - dependency closure UNVERIFIED" | Set-Content "ci-logs/win-dep-audit-ERROR.marker"
      Write-Host "::error::smoke dependency audit: audit exit $auditExit - dependency closure UNVERIFIED; launch smoke proceeds for evidence only, the job gate fails afterwards"
    }
  }

  # ------------------------------------------------------------------
  "launch-smoke" {
    # Launch smoke (run #54 repair; #55 hardening): bounded polling of
    # observable readiness - process alive + real MainWindowHandle OR a
    # visible top-level window found by PID via EnumWindows. Then the
    # IPC contract stages (handshake, core read, log persistence), the
    # managed-engine settle window + verdict, the soak, the graceful
    # close, the shutdown records, and the JSONL persistence checks.
    $smokeRoot = Join-Path $env:RUNNER_TEMP "win-smoke\$pkgRoot"
    if (-not (Test-Path (Join-Path $smokeRoot "SHEYTAN-LA.exe"))) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke guard: smoke copy missing SHEYTAN-LA.exe under $smokeRoot - the staging step did not run or failed"
    }
    $exe = Join-Path (Resolve-Path $smokeRoot) "SHEYTAN-LA.exe"
    $smokeLog = Join-Path (Get-Location) "smoke-app-output.log"
    $env:GALLIUM_DRIVER = "llvmpipe"
    $env:SHEYTAN_STARTUP_DIAGNOSTICS = "1"
    $p = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe) -PassThru `
           -RedirectStandardOutput $smokeLog -RedirectStandardError ($smokeLog + ".err")
    $bootDeadline = (Get-Date).AddSeconds(60)
    $booted = $false
    $enumSeen = $false
    Add-Type -Namespace Smoke -Name Win -MemberDefinition @"
          [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc cb, IntPtr lp);
          [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
          [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
          [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h, System.Text.StringBuilder t, int n);
          public delegate bool EnumWindowsProc(IntPtr h, IntPtr lp);
"@
    function Get-CandidateWindows([uint32]$targetPid) {
      $found = New-Object System.Collections.Generic.List[string]
      $cb = [Smoke.Win+EnumWindowsProc]{ param($h, $lp)
        [uint32]$wpid = 0
        $null = [Smoke.Win]::GetWindowThreadProcessId($h, [ref]$wpid)
        if ($wpid -eq $targetPid -and [Smoke.Win]::IsWindowVisible($h)) {
          $sb = New-Object System.Text.StringBuilder 256
          $null = [Smoke.Win]::GetWindowText($h, $sb, 256)
          $found.Add("hwnd=0x{0:X} visible title='{1}'" -f $h.ToInt64(), $sb.ToString())
        }
        return $true
      }
      $null = [Smoke.Win]::EnumWindows($cb, [IntPtr]::Zero)
      return $found
    }
    while ((Get-Date) -lt $bootDeadline) {
      if ($p.HasExited) { break }
      $p.Refresh()
      if ($p.MainWindowHandle -ne [IntPtr]::Zero) { $booted = $true; break }
      if ((Get-CandidateWindows $p.Id).Count -gt 0) { $enumSeen = $true; $booted = $true; break }
      Start-Sleep -Milliseconds 500
    }
    if (-not $booted) {
      Write-Host "--- application output (smoke-app-output.log) ---"
      if (Test-Path $smokeLog) { Get-Content $smokeLog -Tail 40 } else { Write-Host "(no stdout captured)" }
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke: the application never produced an observable window within 60s (HasExited=$($p.HasExited) ExitCode=$($p.ExitCode))"
    }
    Write-Host "window observed (MainWindowHandle: $($p.MainWindowHandle), enumPath: $enumSeen)"

    # IPC contract stages: handshake, first core read, log persistence.
    $appLog = Join-Path (Split-Path $exe) "data\logs\app.log"
    $ipcDeadline = (Get-Date).AddSeconds(30)
    $handshakeSeen = $false
    $refreshSeen = $false
    $logDirSeen = $false
    while ((Get-Date) -lt $ipcDeadline) {
      if ($p.HasExited) { break }
      if (-not $handshakeSeen -and (Test-Path $appLog)) {
        $hits = Select-String -LiteralPath $appLog -Pattern "core handshake successful" -SimpleMatch
        if ($hits) {
          $pipeHit = $hits | Where-Object { $_.Line -match "transport=windows-named-pipe" }
          if ($pipeHit) { $handshakeSeen = $true }
        }
      }
      if (-not $refreshSeen -and (Test-Path $appLog)) {
        if (Select-String -LiteralPath $appLog -Pattern "first session refresh succeeded" -SimpleMatch) { $refreshSeen = $true }
      }
      if (-not $logDirSeen -and (Test-Path $appLog)) {
        if (Select-String -LiteralPath $appLog -Pattern "log directory:" -SimpleMatch) { $logDirSeen = $true }
      }
      if ($handshakeSeen -and $refreshSeen -and $logDirSeen) { break }
      Start-Sleep -Milliseconds 500
    }
    if (-not $handshakeSeen) {
      if (Test-Path $appLog) { Write-Host "--- data\logs\app.log tail ---"; Get-Content $appLog -Tail 40 }
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage IPC-HANDSHAKE: no successful windows-named-pipe handshake record in data\logs\app.log within 30s - a visible window alone is no longer a pass"
    }
    if (-not $refreshSeen) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage CORE-READ: the first session refresh (a real core API read after the handshake) never succeeded within 30s"
    }
    if (-not $logDirSeen) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage LOG-PERSISTENCE: app.log exists but carries no resolved-log-directory startup record"
    }
    Write-Host "IPC gate: handshake successful (windows-named-pipe), first core read succeeded, log directory recorded"

    # Managed-engine settle window (run 43995689377): give transactional
    # engine maintenance a BOUNDED window to reach a terminal record.
    $engineBinRel = Join-Path "data" "bin" "llama-server.exe"
    $maintenanceDeadline = (Get-Date).AddSeconds(150)
    $maintenanceTerminal = $false
    while ((Get-Date) -lt $maintenanceDeadline) {
      if ($p.HasExited) { break }
      if (Test-Path $appLog) {
        $done = Select-String -LiteralPath $appLog -Pattern "prewarm released" -SimpleMatch
        $current = Select-String -LiteralPath $appLog -Pattern "engine is current" -SimpleMatch
        $blocked = Select-String -LiteralPath $appLog -Pattern "prewarm BLOCKED" -SimpleMatch
        $off = Select-String -LiteralPath $appLog -Pattern "no maintenance this boot" -SimpleMatch
        $upstreamNone = Select-String -LiteralPath $appLog -Pattern "no recent release ships a prebuilt asset" -SimpleMatch
        if ($done -or $current -or $blocked -or $off -or $upstreamNone) { $maintenanceTerminal = $true; break }
      }
      Start-Sleep -Milliseconds 1000
    }
    Write-Host "engine maintenance settle window: terminal record observed = $maintenanceTerminal (deadline 150s)"

    # Soak: the event loop must stay alive AND responsive.
    $soakDeadline = (Get-Date).AddSeconds(12)
    while ((Get-Date) -lt $soakDeadline) {
      if ($p.HasExited) {
        if (Test-Path $smokeLog) { Get-Content $smokeLog -Tail 40 }
        Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke: the application exited early with code $($p.ExitCode)"
      }
      if (-not $p.Responding) {
        Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage SOAK: the window stopped responding during the 12s soak"
      }
      Start-Sleep -Milliseconds 500
    }
    # Graceful close first - the same path a user's window close takes.
    $null = $p.CloseMainWindow()
    $closed = $false
    $closeDeadline = (Get-Date).AddSeconds(20)
    while ((Get-Date) -lt $closeDeadline) {
      if ($p.HasExited) { $closed = $true; break }
      Start-Sleep -Milliseconds 500
    }
    if (-not $closed) {
      Write-Host "::error::launch smoke stage GRACEFUL-CLOSE: CloseMainWindow did not exit within 20s - force-terminating as the documented last resort; this is a FAILED smoke, never a pass"
      if (Test-Path $appLog) { Write-Host "--- data\logs\app.log tail ---"; Get-Content $appLog -Tail 40 }
      Stop-Process -Id $p.Id -Force
      $p.WaitForExit(15000) | Out-Null
      Set-Content -Path $markerPath -Value "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" -Encoding utf8NoBOM
      throw "Windows launch smoke failed at stage GRACEFUL-CLOSE (process had to be force-terminated)"
    }

    # Shutdown evidence must be IN the persisted log.
    $shutDeadline = (Get-Date).AddSeconds(10)
    $ipcStopped = $false
    $coreClosed = $false
    $sinksClosed = $false
    while ((Get-Date) -lt $shutDeadline) {
      if (Test-Path $appLog) {
        if (-not $ipcStopped -and (Select-String -LiteralPath $appLog -Pattern "ipc stopped" -SimpleMatch)) { $ipcStopped = $true }
        if (-not $coreClosed -and (Select-String -LiteralPath $appLog -Pattern "server closed" -SimpleMatch)) { $coreClosed = $true }
        if (-not $sinksClosed -and (Select-String -LiteralPath $appLog -Pattern "log sinks closing" -SimpleMatch)) { $sinksClosed = $true }
      }
      if ($ipcStopped -and $coreClosed -and $sinksClosed) { break }
      Start-Sleep -Milliseconds 500
    }
    if (-not $ipcStopped) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage SHUTDOWN-LOG: the 'ipc stopped' record never reached data\logs\app.log"
    }
    if (-not $coreClosed) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage SHUTDOWN-LOG: the 'server closed' core-close record never reached data\logs\app.log"
    }
    if (-not $sinksClosed) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage SHUTDOWN-LOG: the 'log sinks closing' close-outcome record never reached data\logs\app.log"
    }

    # Logging persistence: the structured sinks exist and the session
    # banner + handshake record survived the clean close. JSONL validity
    # is asserted line-by-line (run-43601915826 audit gap 3a).
    $logsDir = Join-Path (Split-Path $exe) "data\logs"
    foreach ($sink in @("app.log", "tools.jsonl", "llm.jsonl")) {
      if (-not (Test-Path (Join-Path $logsDir $sink))) {
        Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage LOG-PERSISTENCE: expected sink $sink missing under data\logs"
      }
    }
    foreach ($sink in @("tools.jsonl", "llm.jsonl")) {
      $path = Join-Path $logsDir $sink
      $n = 0
      foreach ($line in (Get-Content -LiteralPath $path)) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        $n++
        try { $null = $line | ConvertFrom-Json } catch {
          Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage LOG-PERSISTENCE: $sink line $n is not valid JSON: $($_.Exception.Message)"
        }
      }
      Write-Host "JSONL validity: $sink carries $n valid record(s) (empty is allowed until real calls occur)"
    }
    if (-not (Select-String -LiteralPath $appLog -Pattern "session start" -SimpleMatch)) {
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage LOG-PERSISTENCE: the session banner did not persist in app.log"
    }

    # MANAGED-ENGINE-READY GATE (the run-43995689377 false-green fix):
    # shell green + engine red is a RED job with the explicit label.
    $engineReady = $false
    $engineWhy = @()
    if (Test-Path $appLog) {
      if (Select-String -LiteralPath $appLog -Pattern "update-failed-engine-unusable" -SimpleMatch) { $engineWhy += "maintenance BLOCKED (update-failed-engine-unusable)" }
      if (Select-String -LiteralPath $appLog -Pattern "prewarm BLOCKED" -SimpleMatch) { $engineWhy += "prewarm refused (prewarm BLOCKED)" }
      if (Select-String -LiteralPath $appLog -Pattern "stage engine swap" -SimpleMatch) { $engineWhy += "engine swap failure (stage engine swap)" }
    }
    $engineBin = Join-Path (Split-Path $exe) $engineBinRel
    $engineManifest = Join-Path (Split-Path $exe) (Join-Path "data" (Join-Path "bin" "engine-install.json"))
    $engineFilesOK = (Test-Path $engineBin) -and (Test-Path $engineManifest)
    $engineSuccessRecord = $false
    if (Test-Path $appLog) {
      $rel = Select-String -LiteralPath $appLog -Pattern "prewarm released" -SimpleMatch
      $cur = Select-String -LiteralPath $appLog -Pattern "engine is current" -SimpleMatch
      $validOld = Select-String -LiteralPath $appLog -Pattern "keeping the last-known-good engine" -SimpleMatch
      if ($rel -or $cur -or $validOld) { $engineSuccessRecord = $true }
    }
    if ($engineSuccessRecord -and $engineFilesOK -and ($engineWhy.Count -eq 0)) { $engineReady = $true }
    if (-not $engineReady) {
      if (-not $engineSuccessRecord) { $engineWhy += "no maintenance success record (engine verified / current / last-known-good)" }
      if (-not $engineFilesOK) { $engineWhy += "engine files missing (data\bin\llama-server.exe or data\bin\engine-install.json)" }
      Write-Host "SHELL SMOKE OK; ENGINE NOT READY - this artifact is UI-ONLY-TEST at best, never TEST-READY"
      if (Test-Path $appLog) { Write-Host "--- data\logs\app.log tail ---"; Get-Content $appLog -Tail 50 }
      Fail-Gate "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" "launch smoke stage MANAGED-ENGINE-READY: the desktop shell PASSED but the managed engine is NOT ready - the Windows AI test-ready gate is RED. Engine evidence: $($engineWhy -join '; ')"
    }
    Write-Host "managed engine ready: install manifest + llama-server.exe present, maintenance success record observed, no BLOCKED/refused/swap-failure records"
    Write-Host "INFERENCE: NOT RUN (no model selected on the smoke profile - engine health is not inference proof)"
    Write-Host "SMOKE OK: window booted, IPC handshake succeeded (windows-named-pipe), core read succeeded, soak survived, graceful close, shutdown records persisted, managed engine ready"

    # Dependency-audit gate (release gate, run 41933333132): ERROR is
    # never PASS - the job fails here, AFTER the smoke outcome was recorded.
    if (Test-Path "ci-logs/win-dep-audit-ERROR.marker") {
      Write-Host (Get-Content "ci-logs/win-dep-audit-ERROR.marker")
      Set-Content -Path $markerPath -Value "Pre-launch dependency audit (static imports, release gate)" -Encoding utf8NoBOM
      throw "dependency audit ERROR - dependency closure unverified (job gate)"
    }
  }

  # ------------------------------------------------------------------
  "collect-smoke-diagnostics" {
    # Best-effort collection of the app's portable data residue into
    # ci-logs (run #117: zero diagnostics survived the step log). Never
    # masks the original job conclusion.
    $ErrorActionPreference = "Continue"
    $smokeRoot = Join-Path $env:RUNNER_TEMP "win-smoke\$pkgRoot"
    $null = New-Item -ItemType Directory -Force -Path "ci-logs"
    try {
      $appLogs = @(Get-ChildItem -Path (Join-Path $smokeRoot "data") -Filter "app.log" -Recurse -ErrorAction SilentlyContinue)
      if ($appLogs.Count -gt 0) {
        Copy-Item $appLogs[0].FullName "ci-logs/win-smoke-app.log" -Force
        Write-Host "collected $($appLogs[0].FullName) -> ci-logs/win-smoke-app.log"
      } else { Write-Host "smoke copy produced no app.log (no data root created)" }
      $crashes = @(Get-ChildItem -Path (Join-Path $smokeRoot "data") -Filter "crash-*.log" -Recurse -ErrorAction SilentlyContinue)
      foreach ($c in $crashes) {
        Copy-Item $c.FullName ("ci-logs/win-smoke-" + $c.Name) -Force
        Write-Host "collected $($c.FullName) -> ci-logs/win-smoke-$($c.Name)"
      }
      foreach ($f in @("smoke-app-output.log", "smoke-app-output.log.err")) {
        if (Test-Path $f) { Copy-Item $f ("ci-logs/" + $f) -Force; Write-Host "collected $f -> ci-logs" }
      }
    } catch { Write-Host "diagnostic collection (best-effort) failed: $($_.Exception.Message)" }
  }

  # ------------------------------------------------------------------
  "build-info" {
    # BUILD-INFO and package metadata (run #54 root cause #3): explicit,
    # individually-validated UTF-8 (no BOM) writes with per-file
    # diagnostics.
    $root = $stagingRoot
    if (-not (Test-Path $root)) { Fail-Gate "Write BUILD-INFO.txt and package metadata" "BUILD-INFO guard: staging root missing at $root" }
    foreach ($dir in @("resources/docs", "resources/brand")) {
      if (-not (Test-Path (Join-Path $root $dir))) { Fail-Gate "Write BUILD-INFO.txt and package metadata" "BUILD-INFO guard: staging directory missing at $root\$dir" }
    }
    $files = @(
      @{ Path = "$root/resources/docs/BUILD-INFO.txt"; Marker = "version: $($env:APP_VERSION)";
         Lines = @("product: SHEYTAN-LA", "version: $($env:APP_VERSION)", "commit: ${env:GITHUB_SHA}", "ui: fyne (one Go UI)") },
      @{ Path = "$root/resources/docs/README.txt"; Marker = "run SHEYTAN-LA.exe";
         Lines = @("SHEYTAN-LA $($env:APP_VERSION) - run SHEYTAN-LA.exe. Models are downloaded in-app; user data lives under data\ next to the executable. Static package content lives under resources\.") }
    )
    foreach ($f in $files) {
      try {
        Set-Content -Path $f.Path -Value $f.Lines -Encoding utf8NoBOM
      } catch {
        Fail-Gate "Write BUILD-INFO.txt and package metadata" ("BUILD-INFO guard: writing {0} failed: {1}" -f $f.Path, $_.Exception.Message)
      }
      if (-not (Test-Path $f.Path)) { Fail-Gate "Write BUILD-INFO.txt and package metadata" "BUILD-INFO guard: write reported success but the file is missing: $($f.Path)" }
      $size = (Get-Item $f.Path).Length
      if ($size -le 0) { Fail-Gate "Write BUILD-INFO.txt and package metadata" "BUILD-INFO guard: written file is empty: $($f.Path) (size=$size)" }
      $content = Get-Content -Raw -Path $f.Path
      if (-not $content.Contains($f.Marker)) { Fail-Gate "Write BUILD-INFO.txt and package metadata" "BUILD-INFO guard: written file lacks the expected content marker '$($f.Marker)': $($f.Path)" }
      Write-Host "wrote $($f.Path) ($size bytes, marker OK)"
    }
  }

  default {
    Write-Host "usage: windows.ps1 -Phase <name>" 
    Write-Host "phases: gofmt vet headless-tests release-contract version mesa-acquire ipc-integration gen-syso stage build verify-pe stage-smoke dep-audit launch-smoke collect-smoke-diagnostics build-info"
    exit 64
  }
}
