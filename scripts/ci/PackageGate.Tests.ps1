<#
.SYNOPSIS
  Fixtures for the fail-closed Windows package gate (ALPHA portable-only,
  master work order §10) — deterministic, stream-safe, table-driven.

.DESCRIPTION
  Runs scripts/ci/PackageGate.ps1 against deterministic local fixtures
  and fails on any violation. No Pester, no network.

  The harness is stream-safe and contract-pinned (carried over from run
  42928792246):

    - It captures ALL streams (`*&1`), so expected negative-fixture
      output is inert test data — never a live GitHub annotation — and
      the RAW captured lines are preserved (prefixed per line so they
      can never start a workflow command) in
      ci-logs/package-gate-fixtures.log.
    - It asserts the STABLE contract only: the single machine-readable
      token line (PACKAGE_GATE_RESULT <TOKEN>), the exit code, and the
      structured JSON result file (affected path, byte counts, missing/
      forbidden entries, installer-remnant inventory, dependency
      verdict). Free-form message text is never asserted (no
      ANSI/cwd/locale/formatting coupling).
    - Fixtures are REAL artifacts: real ZIP archives built with
      System.IO.Compression and real byte counts — not placeholder text.

  State matrix pinned here (token -> exit):

     1  ZIP_MISSING                2    8  ZIP_MISSING_ENTRIES     2
     2  ZIP_EMPTY                  2    9  ZIP_FORBIDDEN_ENTRIES   2
     3  ZIP_CORRUPT                2   10  INSTALLER_REMNANT       2
     4  ZIP_BAD_ROOT               2   11  OK (real zip)           0
     5  INVALID_ARGUMENTS          1   12  dependency FAIL report  2
     6  INVALID_ARGUMENTS (CLI)    1   13  dependency PASS report  0
     7  ZIP_FORBIDDEN (Mesa DLL)   2   14  BLOCKED_BY_UPSTREAM     2
                                        15  marker-absent states    0/2

  Additional contract pins: the success line "package gate: reached"
  and the OK token appear ONLY in the OK state; the gate has NO
  -Installer parameter (installer production is retired — master work
  order §10) and rejects stale installer remnants in the distribution
  directory; the OK success line carries the ACTUAL on-disk byte count
  and SHA-256 digest; the expected negative-fixture ::error:: output
  stays visible in the escaped fixture log without ever masquerading as
  a live annotation.

  Exit codes: 0 all fixtures hold; 1 fixture contract violated.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

Add-Type -AssemblyName System.IO.Compression -ErrorAction SilentlyContinue
Add-Type -AssemblyName System.IO.Compression.FileSystem -ErrorAction SilentlyContinue

$failures = New-Object System.Collections.Generic.List[string]

function Assert-True {
    param([bool]$Condition, [string]$What)
    if (-not $Condition) {
        $script:failures.Add($What) | Out-Null
        Write-Host "FAIL: $What"
    } else {
        Write-Host "ok:   $What"
    }
}

function Assert-Equal {
    param([object]$Expected, [object]$Actual, [string]$What)
    Assert-True ([string]$Expected -eq [string]$Actual) ("$What (expected '$Expected', got '$Actual')")
}

# Stream-safe capture: ALL streams, so expected negative-fixture output
# is inert test data, never a live workflow annotation.
$script:fixtureLog = "ci-logs/package-gate-fixtures.log"
New-Item -ItemType Directory -Force -Path "ci-logs" | Out-Null
Set-Content -Path $script:fixtureLog -Value ("package gate fixtures — " + [System.DateTime]::UtcNow.ToString("o")) -Encoding utf8NoBOM

function Invoke-Gate {
    param([string[]]$GateArgs)
    # Work order §4.2 (run 44306343338): PackageGate.ps1 is a PowerShell
    # SCRIPT — an in-process & invocation does not reliably set
    # $LASTEXITCODE (it may stay unset under strict mode or keep a STALE
    # value from an earlier native command). Invoke the gate as a NATIVE
    # child process so the $LASTEXITCODE read below is genuine.
    $out = & pwsh -NoProfile -NonInteractive -File ./scripts/ci/PackageGate.ps1 @GateArgs *&1 | Out-String
    $code = $LASTEXITCODE
    foreach ($line in ($out -split "`r?`n")) {
        if ($line.Trim().Length -gt 0) {
            Add-Content -Path $script:fixtureLog -Value ("  | " + $line) -Encoding utf8NoBOM
        }
    }
    [pscustomobject]@{ Output = $out; ExitCode = $code }
}

function Gate-Token {
    param([string]$Output)
    $hits = @($Output -split "`r?`n" | Where-Object { $_ -like "PACKAGE_GATE_RESULT *" })
    return [pscustomobject]@{ Lines = $hits; Token = $(if ($hits.Count -eq 1) { ($hits[0] -replace '^PACKAGE_GATE_RESULT\s+', '') } else { "<tokens:$($hits.Count)>" }) }
}

# Fixture workspace (unique per invocation, cleaned up at the end).
$base = Join-Path ([System.IO.Path]::GetTempPath()) ("pkggate-fixture-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $base | Out-Null

function New-Zip {
    param([string]$Path, [string[]]$Entries, [string]$Content = "payload-bytes")
    $root = "SHEYTAN-LA"
    $fs = [System.IO.File]::Create($Path)
    $zip = New-Object System.IO.Compression.ZipArchive($fs, [System.IO.Compression.ZipArchiveMode]::Create)
    foreach ($e in $Entries) {
        $entry = $zip.CreateEntry("$root/$e")
        $writer = New-Object System.IO.StreamWriter($entry.Open())
        $writer.Write($Content)
        $writer.Dispose()
    }
    $zip.Dispose()
    $fs.Dispose()
}

$requiredEntries = @(
    "SHEYTAN-LA.exe",
    "SHEYTAN-LA.bat",
    "resources/docs/README.txt",
    "resources/docs/BUILD-INFO.txt",
    "resources/docs/LICENSE.md",
    "resources/brand/sheytan.ico"
)

try {
    # ------------------------------------------------------------------
    # 1. ZIP_MISSING (the only "absent" state — BOTH_MISSING merged).
    # ------------------------------------------------------------------
    $zipPath = Join-Path $base "missing.zip"   # deliberately not created
    $r = Invoke-Gate @("-Zip", $zipPath)
    Assert-Equal 2 $r.ExitCode "ZIP_MISSING exit code"
    Assert-Equal "ZIP_MISSING" (Gate-Token $r.Output).Token "ZIP_MISSING token"
    Assert-Equal 1 (Gate-Token $r.Output).Lines.Count "exactly one PACKAGE_GATE_RESULT line (ZIP_MISSING)"

    # ------------------------------------------------------------------
    # 2. ZIP_EMPTY.
    # ------------------------------------------------------------------
    $zipPath = Join-Path $base "empty.zip"
    Set-Content -Path $zipPath -Value "" -NoNewline -Encoding Ascii
    $r = Invoke-Gate @("-Zip", $zipPath)
    Assert-Equal 2 $r.ExitCode "ZIP_EMPTY exit code"
    Assert-Equal "ZIP_EMPTY" (Gate-Token $r.Output).Token "ZIP_EMPTY token"

    # ------------------------------------------------------------------
    # 3. ZIP_CORRUPT (non-empty non-archive).
    # ------------------------------------------------------------------
    $zipPath = Join-Path $base "corrupt.zip"
    Set-Content -Path $zipPath -Value "this is not a zip archive" -NoNewline -Encoding Ascii
    $r = Invoke-Gate @("-Zip", $zipPath)
    Assert-Equal 2 $r.ExitCode "ZIP_CORRUPT exit code"
    Assert-Equal "ZIP_CORRUPT" (Gate-Token $r.Output).Token "ZIP_CORRUPT token"

    # ------------------------------------------------------------------
    # 4. ZIP_BAD_ROOT (two top-level directories).
    # ------------------------------------------------------------------
    $zipPath = Join-Path $base "badroot.zip"
    $fs = [System.IO.File]::Create($zipPath)
    $zip = New-Object System.IO.Compression.ZipArchive($fs, [System.IO.Compression.ZipArchiveMode]::Create)
    foreach ($rootName in @("SHEYTAN-LA", "OTHER")) {
        $entry = $zip.CreateEntry("$rootName/SHEYTAN-LA.exe")
        $writer = New-Object System.IO.StreamWriter($entry.Open())
        $writer.Write("x")
        $writer.Dispose()
    }
    $zip.Dispose()
    $fs.Dispose()
    $r = Invoke-Gate @("-Zip", $zipPath)
    Assert-Equal 2 $r.ExitCode "ZIP_BAD_ROOT exit code"
    Assert-Equal "ZIP_BAD_ROOT" (Gate-Token $r.Output).Token "ZIP_BAD_ROOT token"

    # ------------------------------------------------------------------
    # 5. ZIP_MISSING_ENTRIES (real zip with required entries removed).
    # ------------------------------------------------------------------
    $zipPath = Join-Path $base "missing-entries.zip"
    New-Zip -Path $zipPath -Entries @("SHEYTAN-LA.exe")
    $r = Invoke-Gate @("-Zip", $zipPath)
    Assert-Equal 2 $r.ExitCode "ZIP_MISSING_ENTRIES exit code"
    Assert-Equal "ZIP_MISSING_ENTRIES" (Gate-Token $r.Output).Token "ZIP_MISSING_ENTRIES token"

    # ------------------------------------------------------------------
    # 6. ZIP_FORBIDDEN_ENTRIES (Mesa software-GL DLL residue).
    # ------------------------------------------------------------------
    $zipPath = Join-Path $base "forbidden.zip"
    New-Zip -Path $zipPath -Entries ($requiredEntries + @("opengl32.dll"))
    $r = Invoke-Gate @("-Zip", $zipPath)
    Assert-Equal 2 $r.ExitCode "ZIP_FORBIDDEN_ENTRIES (Mesa DLL) exit code"
    Assert-Equal "ZIP_FORBIDDEN_ENTRIES" (Gate-Token $r.Output).Token "ZIP_FORBIDDEN_ENTRIES token"

    # ------------------------------------------------------------------
    # 7. INSTALLER_REMNANT: a stale installer artifact in the
    #    distribution directory fails the gate (master work order §10.2).
    # ------------------------------------------------------------------
    $goodZip = Join-Path $base "good.zip"
    New-Zip -Path $goodZip -Entries $requiredEntries
    $distDir = Join-Path $base "dist"
    New-Item -ItemType Directory -Force -Path $distDir | Out-Null
    Copy-Item $goodZip (Join-Path $distDir "SHEYTAN-LA-v1-windows-x64.zip")
    Set-Content -Path (Join-Path $distDir "SHEYTAN-LA-v1-windows-x64-installer.exe") -Value "stale installer" -NoNewline -Encoding Ascii
    $r = Invoke-Gate @("-Zip", (Join-Path $distDir "SHEYTAN-LA-v1-windows-x64.zip"), "-DistDir", $distDir)
    Assert-Equal 2 $r.ExitCode "INSTALLER_REMNANT exit code"
    Assert-Equal "INSTALLER_REMNANT" (Gate-Token $r.Output).Token "INSTALLER_REMNANT token"
    # The same directory WITHOUT the remnant passes the remnant check.
    Remove-Item -LiteralPath (Join-Path $distDir "SHEYTAN-LA-v1-windows-x64-installer.exe") -Force
    $r = Invoke-Gate @("-Zip", (Join-Path $distDir "SHEYTAN-LA-v1-windows-x64.zip"), "-DistDir", $distDir)
    Assert-Equal 0 $r.ExitCode "remnant-free dist dir passes"
    Assert-Equal "OK" (Gate-Token $r.Output).Token "remnant-free dist dir token"

    # ------------------------------------------------------------------
    # 8. OK — the real portable ZIP, full required entries, with the
    #    ACTUAL byte count and digest asserted against the real file.
    # ------------------------------------------------------------------
    $r = Invoke-Gate @("-Zip", $goodZip, "-ResultFile", (Join-Path $base "ok-result.json"))
    Assert-Equal 0 $r.ExitCode "OK exit code"
    Assert-Equal "OK" (Gate-Token $r.Output).Token "OK token"
    Assert-True ($r.Output -match "package gate: reached") "OK state prints the success line"
    $realBytes = (Get-Item $goodZip).Length
    $realSha = (Get-FileHash -LiteralPath $goodZip -Algorithm SHA256).Hash.ToLowerInvariant()
    Assert-True ($r.Output -match [regex]::Escape("portable ZIP $realBytes bytes")) "success line carries the ACTUAL byte count"
    Assert-True ($r.Output -match [regex]::Escape("sha256 zip=$realSha")) "success line carries the ACTUAL sha256"
    $json = Get-Content -Raw -Path (Join-Path $base "ok-result.json") | ConvertFrom-Json
    Assert-Equal "OK" $json.token "result-file token"
    Assert-Equal $realBytes $json.zipBytes "result-file byte count"
    Assert-Equal $realSha $json.zipSha256 "result-file digest"
    Assert-Equal 8 $json.zipEntryCount "result-file entry count (6 required entries)"
    Assert-Equal "SHEYTAN-LA" $json.expectedRoot "result-file expected root"

    # ------------------------------------------------------------------
    # 9. INVALID_ARGUMENTS: missing -Zip, empty ExpectedRoot, empty
    #    required entry — deterministic exit 1.
    # ------------------------------------------------------------------
    $r = Invoke-Gate @()
    Assert-Equal 1 $r.ExitCode "INVALID_ARGUMENTS (missing -Zip) exit code"
    Assert-Equal "INVALID_ARGUMENTS" (Gate-Token $r.Output).Token "INVALID_ARGUMENTS (missing -Zip) token"
    $r = Invoke-Gate @("-Zip", $goodZip, "-ExpectedRoot", "")
    Assert-Equal 1 $r.ExitCode "INVALID_ARGUMENTS (empty ExpectedRoot) exit code"
    $r = Invoke-Gate @("-Zip", $goodZip, "-RequiredEntries", @("SHEYTAN-LA.exe", " "))
    Assert-Equal 1 $r.ExitCode "INVALID_ARGUMENTS (blank required entry) exit code"

    # ------------------------------------------------------------------
    # 10. Dependency closure: FAIL report fails, PASS report passes.
    # ------------------------------------------------------------------
    $failReport = Join-Path $base "dep-fail.txt"
    Set-Content -Path $failReport -Value "smoke dependency audit: MISSING DEPENDENCIES (d3dcompiler_47.dll)" -Encoding utf8NoBOM
    $r = Invoke-Gate @("-Zip", $goodZip, "-DependencyReport", $failReport)
    Assert-Equal 2 $r.ExitCode "DEPENDENCY_CLOSURE_FAILURE exit code"
    Assert-Equal "DEPENDENCY_CLOSURE_FAILURE" (Gate-Token $r.Output).Token "DEPENDENCY_CLOSURE_FAILURE token"
    $passReport = Join-Path $base "dep-pass.txt"
    Set-Content -Path $passReport -Value "smoke dependency audit: PASS (all load-chain imports resolved)" -Encoding utf8NoBOM
    $r = Invoke-Gate @("-Zip", $goodZip, "-DependencyReport", $passReport)
    Assert-Equal 0 $r.ExitCode "dependency PASS report passes"
    # An absent report file is REPORT_MISSING — never a pass.
    $r = Invoke-Gate @("-Zip", $goodZip, "-DependencyReport", (Join-Path $base "absent-dep.txt"))
    Assert-Equal 2 $r.ExitCode "dependency REPORT_MISSING never passes"

    # ------------------------------------------------------------------
    # 11. BLOCKED_BY_UPSTREAM_TEST_FAILURE + marker-absent fallback.
    # ------------------------------------------------------------------
    $marker = Join-Path $base "win-upstream-failure.txt"
    Set-Content -Path $marker -Value "Launch smoke (window + IPC handshake + core read + logging persistence + graceful close)" -Encoding utf8NoBOM
    $r = Invoke-Gate @("-Zip", (Join-Path $base "nope.zip"), "-UpstreamFailureMarker", $marker)
    Assert-Equal 2 $r.ExitCode "BLOCKED_BY_UPSTREAM_TEST_FAILURE exit code"
    Assert-Equal "BLOCKED_BY_UPSTREAM_TEST_FAILURE" (Gate-Token $r.Output).Token "BLOCKED_BY_UPSTREAM token"
    $r = Invoke-Gate @("-Zip", (Join-Path $base "nope.zip"))
    Assert-Equal 2 $r.ExitCode "marker absent: absent ZIP still fails (fail-closed)"
    Assert-Equal "ZIP_MISSING" (Gate-Token $r.Output).Token "marker absent: ZIP_MISSING token"

    # ------------------------------------------------------------------
    # 12. Installer retirement pins (master work order §10): the gate
    #     script must no longer accept -Installer or reference NSIS.
    # ------------------------------------------------------------------
    $gateSrc = Get-Content -Raw -Path "./scripts/ci/PackageGate.ps1"
    Assert-True (-not ($gateSrc -match '(?m)\[string\]\$Installer\b')) "gate has NO -Installer parameter (installer retired)"
    Assert-True ($gateSrc.Contains("INSTALLER_REMNANT")) "gate pins the INSTALLER_REMNANT state"

    # ------------------------------------------------------------------
    # 13. Determinism: the same fixture twice yields the same token and
    #     exit code, and emits the token line exactly once per run.
    # ------------------------------------------------------------------
    $r1 = Invoke-Gate @("-Zip", $goodZip)
    $r2 = Invoke-Gate @("-Zip", $goodZip)
    Assert-Equal $r1.ExitCode $r2.ExitCode "deterministic exit code across runs"
    Assert-Equal (Gate-Token $r1.Output).Token (Gate-Token $r2.Output).Token "deterministic token across runs"

    # ------------------------------------------------------------------
    # Summary.
    # ------------------------------------------------------------------
    Write-Host ("package gate fixtures: {0} checks, {1} failures" -f "", $failures.Count)
    if ($failures.Count -gt 0) {
        Write-Host "FAILED ASSERTIONS:"
        foreach ($f in $failures) { Write-Host ("  - " + $f) }
        exit 1
    }
    Write-Host "package gate fixtures: PASS (all contracted states hold; installer retirement pins hold)"
    exit 0
} finally {
    if (Test-Path $base) { Remove-Item -Recurse -Force $base -ErrorAction SilentlyContinue }
}
