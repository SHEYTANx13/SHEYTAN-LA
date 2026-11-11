<#
.SYNOPSIS
  Fixtures for the fail-closed Windows package gate (v1.11.1) —
  deterministic, stream-safe, table-driven (run 42928792246 work order
  §3.2).

.DESCRIPTION
  Runs scripts/ci/PackageGate.ps1 against deterministic local fixtures
  and fails on any violation. No Pester, no network.

  Run 42928792246 root cause (reproduced locally on PowerShell 7.4): the
  v1 harness captured the gate with `2>&1`, which merges the ERROR stream
  into stdout but NEVER the INFORMATION stream — and the gate emitted
  everything through Write-Host (information stream 6). The captured
  output was therefore empty, all seven diagnostic-output assertions
  failed, and every expected negative-fixture `::error::` line leaked to
  the console as a live workflow annotation.

  The v2 harness is stream-safe and contract-pinned:

    - It captures ALL streams (`*&1`), so expected negative-fixture
      output is inert test data — never a live GitHub annotation — and
      the RAW captured lines are preserved (prefixed per line so they
      can never start a workflow command) in
      ci-logs/package-gate-fixtures.log.
    - It asserts the STABLE contract only: the single machine-readable
      token line (PACKAGE_GATE_RESULT <TOKEN>), the exit code, and the
      structured JSON result file (affected path, byte counts, missing/
      forbidden entries, installer PE machine type, dependency verdict).
      Free-form message text is never asserted (no ANSI/cwd/locale/
      formatting coupling).
    - Fixtures are REAL artifacts: real ZIP archives built with
      System.IO.Compression, real PE byte sequences (MZ + PE signature +
      machine type), real byte counts — not placeholder text.

  State matrix pinned here (token -> exit):

    1  BOTH_MISSING               2    8  ZIP_BAD_ROOT            2
    2  ZIP_MISSING                2    9  ZIP_MISSING_ENTRIES     2
    3  INSTALLER_MISSING          2   10  ZIP_FORBIDDEN_ENTRIES   2
    4  ZIP_EMPTY                  2   11  INSTALLER_NOT_PE        2
    5  INSTALLER_EMPTY            2   12  INSTALLER_BAD_ARCH      2
    6  OK (real zip + real PE)    0   13  OK with i386 NSIS stub  0
    7  ZIP_CORRUPT                2   14  INVALID_ARGUMENTS       1
    15 INVALID_ARGUMENTS (CLI)    1   16  DEPENDENCY_CLOSURE      2
    17 dependency PASS report     0

  Additional contract pins: the success line "package gate: reached"
  and the OK token appear ONLY in the OK state; the OK success line
  carries the ACTUAL on-disk byte counts and SHA-256 digests (fixture
  byte counts are asserted against the real files, never copied); the
  expected negative-fixture ::error:: output stays visible in the
  escaped fixture log without ever masquerading as a live annotation.

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

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$gateScript = Join-Path $PSScriptRoot "PackageGate.ps1"
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("packagegate-fixtures-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $work | Out-Null
$ciLogs = Join-Path $repoRoot "ci-logs"
New-Item -ItemType Directory -Force -Path $ciLogs | Out-Null
$fixtureLog = Join-Path $ciLogs "package-gate-fixtures.log"
Set-Content -LiteralPath $fixtureLog -Value "package-gate fixture suite — captured child-gate output (each line prefixed 'fixture-output: ' so expected negative-fixture ::error:: text can never be parsed as a live workflow command)" -Encoding utf8NoBOM

function Add-FixtureLog {
    param([string[]]$Lines)
    foreach ($l in $Lines) {
        # Inert-by-construction: no captured line may ever start with the
        # GitHub workflow-command prefix, so expected negative-fixture
        # errors cannot inflate the run's real annotation count.
        Add-Content -LiteralPath $script:fixtureLog -Value ("fixture-output: " + $l) -Encoding utf8NoBOM
    }
}

function Invoke-Gate {
    # Named-parameter splatting (hashtable): an ARRAY splat would bind
    # POSITIONALLY and turn the '-Zip'/'-Installer' flag strings into
    # literal parameter values. Omitted keys omit the argument — which is
    # exactly the invalid-argument state under test.
    param([string]$ZipPath, [string]$InstallerPath, [string]$ResultPath, [string]$DependencyReportPath)
    $ht = @{}
    if ($ZipPath)           { $ht["Zip"] = $ZipPath }
    if ($InstallerPath)     { $ht["Installer"] = $InstallerPath }
    if ($ResultPath)        { $ht["ResultFile"] = $ResultPath }
    if ($DependencyReportPath) { $ht["DependencyReport"] = $DependencyReportPath }
    $out = & $gateScript @ht 2>&1 6>&1
    $code = $LASTEXITCODE
    $lines = @($out | ForEach-Object { $_.ToString() })
    Add-FixtureLog -Lines $lines
    $json = $null
    if ($ResultPath -and (Test-Path -LiteralPath $ResultPath)) {
        $json = Get-Content -LiteralPath $ResultPath -Raw | ConvertFrom-Json
    }
    [pscustomobject]@{
        ExitCode = $code
        Output   = $lines
        Json     = $json
    }
}

# --- deterministic fixture builders ---------------------------------------

function New-FixtureInstallerFile {
    param([string]$Path, [int]$Machine = 0x8664, [switch]$NotPE)
    if ($NotPE) {
        Set-Content -LiteralPath $Path -Value "placeholder installer bytes (not a PE)" -Encoding utf8NoBOM
        return
    }
    $b = New-Object byte[] 4096
    $b[0] = 0x4D; $b[1] = 0x5A              # MZ
    $b[0x3C] = 0x40                          # e_lfanew -> 0x40
    $b[0x40] = 0x50; $b[0x41] = 0x45; $b[0x42] = 0x00; $b[0x43] = 0x00  # PE\0\0
    $m = [BitConverter]::GetBytes([UInt16]$Machine)
    $b[0x44] = $m[0]; $b[0x45] = $m[1]       # COFF machine type at peOffset+4
    [System.IO.File]::WriteAllBytes($Path, $b)
}

function New-FixtureZip {
    param([string]$Path, [hashtable]$Entries)
    $fs = [System.IO.File]::Create($Path)
    try {
        $archive = New-Object System.IO.Compression.ZipArchive($fs, [System.IO.Compression.ZipArchiveMode]::Create)
        try {
            foreach ($k in $Entries.Keys) {
                $entry = $archive.CreateEntry($k)
                $es = $entry.Open()
                try {
                    $bytes = $Entries[$k]
                    if ($bytes -is [string]) { $bytes = [System.Text.Encoding]::UTF8.GetBytes($bytes) }
                    $es.Write($bytes, 0, $bytes.Length)
                } finally { $es.Dispose() }
            }
        } finally { $archive.Dispose() }
    } finally { $fs.Dispose() }
}

function New-ExePEBytes {
    # A minimal real x86-64 PE byte sequence for the payload entry.
    $b = New-Object byte[] 2048
    $b[0] = 0x4D; $b[1] = 0x5A
    $b[0x3C] = 0x40
    $b[0x40] = 0x50; $b[0x41] = 0x45; $b[0x42] = 0x00; $b[0x43] = 0x00
    $m = [BitConverter]::GetBytes([UInt16]0x8664)
    $b[0x44] = $m[0]; $b[0x45] = $m[1]
    return $b
}

function New-ValidPayloadEntries {
    [ordered]@{
        "SHEYTAN-LA/SHEYTAN-LA.exe"                          = (New-ExePEBytes)
        "SHEYTAN-LA/SHEYTAN-LA.bat"                          = "@echo off`r`nSHEYTAN-LA.exe %*`r`n"
        "SHEYTAN-LA/resources/docs/README.txt"               = "SHEYTAN-LA 1.11.1 - run SHEYTAN-LA.exe"
        "SHEYTAN-LA/resources/docs/BUILD-INFO.txt"           = "product: SHEYTAN-LA`nversion: 1.11.1"
        "SHEYTAN-LA/resources/docs/LICENSE.md"               = "# License (fixture extract)"
        "SHEYTAN-LA/resources/brand/sheytan.ico"             = ([byte[]](0,0,1,0,1,0))
    }
}

function New-ValidPair {
    param([string]$ZipPath, [string]$InstPath, [int]$InstallerMachine = 0x8664)
    New-FixtureZip -Path $ZipPath -Entries (New-ValidPayloadEntries)
    New-FixtureInstallerFile -Path $InstPath -Machine $InstallerMachine
}

# --- shared outcome assertions (stable contract only) ----------------------

function Assert-GateOutcome {
    param($r, [string]$ExpectToken, [int]$ExpectCode, [string]$What, [switch]$ExpectJson)
    Assert-True ($r.ExitCode -eq $ExpectCode) "${What}: exit $ExpectCode (got $($r.ExitCode))"
    $tokenLines = @($r.Output | Where-Object { $_ -match '^PACKAGE_GATE_RESULT ' })
    Assert-True ($tokenLines.Count -eq 1) "${What}: exactly one machine-readable PACKAGE_GATE_RESULT line (got $($tokenLines.Count))"
    if ($tokenLines.Count -ge 1) {
        Assert-True ($tokenLines[0] -eq "PACKAGE_GATE_RESULT $ExpectToken") "${What}: stable outcome token $ExpectToken (got '$($tokenLines[0])')"
    }
    if ($ExpectToken -ne "OK") {
        $reached = @($r.Output | Where-Object { $_.Contains("package gate: reached") })
        Assert-True ($reached.Count -eq 0) "${What}: never prints the success line"
        Assert-True (-not ($tokenLines | Where-Object { $_ -eq "PACKAGE_GATE_RESULT OK" })) "${What}: never prints the OK token"
    }
    if ($ExpectJson) {
        Assert-True ($null -ne $r.Json) "${What}: the JSON result file was written"
        if ($null -ne $r.Json) {
            Assert-True ($r.Json.token -eq $ExpectToken) "${What}: JSON token is $ExpectToken (got $($r.Json.token))"
            Assert-True ([int]$r.Json.exitCode -eq $ExpectCode) "${What}: JSON exitCode is $ExpectCode (got $($r.Json.exitCode))"
        }
    }
}

try {
    Assert-True (Test-Path -LiteralPath $gateScript -PathType Leaf) "precondition: PackageGate.ps1 exists beside this fixture suite"

    $zipName = "SHEYTAN-LA-v1.11.1-windows-x64.zip"
    $instName = "SHEYTAN-LA-v1.11.1-windows-x64-installer.exe"
    $zipPath = Join-Path $work $zipName
    $instPath = Join-Path $work $instName

    # ------------------------------------------------------------------
    # 1. BOTH_MISSING — neither package present
    # ------------------------------------------------------------------
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "BOTH_MISSING" -ExpectCode 2 -What "state 1 (both missing)"
    Assert-True ($null -eq $r.Json -or $r.Json.zipBytes -eq $null) "state 1: JSON carries no byte counts for the absent ZIP"

    # ------------------------------------------------------------------
    # 2. ZIP_MISSING — installer present (real PE bytes)
    # ------------------------------------------------------------------
    New-FixtureInstallerFile -Path $instPath -Machine 0x8664
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "ZIP_MISSING" -ExpectCode 2 -What "state 2 (ZIP-only missing)"

    # ------------------------------------------------------------------
    # 3. INSTALLER_MISSING — ZIP present (real archive)
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $instPath -Force
    New-FixtureZip -Path $zipPath -Entries (New-ValidPayloadEntries)
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "INSTALLER_MISSING" -ExpectCode 2 -What "state 3 (installer-only missing)"

    # ------------------------------------------------------------------
    # 4. ZIP_EMPTY — 0-byte ZIP, installer present
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    New-Item -ItemType File -Force -Path $zipPath | Out-Null
    New-FixtureInstallerFile -Path $instPath -Machine 0x8664
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "ZIP_EMPTY" -ExpectCode 2 -What "state 4 (empty ZIP)"

    # ------------------------------------------------------------------
    # 5. INSTALLER_EMPTY — ZIP present, 0-byte installer
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    Remove-Item -LiteralPath $instPath -Force
    New-FixtureZip -Path $zipPath -Entries (New-ValidPayloadEntries)
    New-Item -ItemType File -Force -Path $instPath | Out-Null
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "INSTALLER_EMPTY" -ExpectCode 2 -What "state 5 (empty installer)"

    # ------------------------------------------------------------------
    # 6. OK — both present: REAL archive with the contracted entries and
    #    a REAL x86-64 PE installer; JSON result file carries the ACTUAL
    #    byte counts, entry count, machine type and SHA-256 digests.
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    Remove-Item -LiteralPath $instPath -Force
    $resultPath = Join-Path $work "gate-result.json"
    New-ValidPair -ZipPath $zipPath -InstPath $instPath
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -ResultPath $resultPath
    Assert-GateOutcome -r $r -ExpectToken "OK" -ExpectCode 0 -What "state 6 (both valid)" -ExpectJson
    $zipLen = (Get-Item -LiteralPath $zipPath).Length
    $instLen = (Get-Item -LiteralPath $instPath).Length
    $zipHash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    $instHash = (Get-FileHash -LiteralPath $instPath -Algorithm SHA256).Hash.ToLowerInvariant()
    Assert-True ([long]$r.Json.zipBytes -eq [long]$zipLen) "state 6: JSON reports the ACTUAL ZIP byte count ($zipLen, got $($r.Json.zipBytes))"
    Assert-True ([long]$r.Json.installerBytes -eq [long]$instLen) "state 6: JSON reports the ACTUAL installer byte count ($instLen, got $($r.Json.installerBytes))"
    Assert-True ($r.Json.zipEntryCount -ge 6) "state 6: JSON reports the archive entry count (got $($r.Json.zipEntryCount))"
    Assert-True ($r.Json.installerMachine -eq "0x8664") "state 6: JSON reports the installer PE machine type (got $($r.Json.installerMachine))"
    Assert-True ($r.Json.zipSha256 -eq $zipHash) "state 6: JSON carries the real ZIP SHA-256"
    Assert-True ($r.Json.installerSha256 -eq $instHash) "state 6: JSON carries the real installer SHA-256"
    $successLine = @($r.Output | Where-Object { $_.Contains("package gate: reached") })
    Assert-True ($successLine.Count -eq 1) "state 6: prints the success line exactly once"
    Assert-True ($successLine[0].Contains("portable ZIP $zipLen bytes")) "state 6: success line carries the verified ZIP byte count"
    Assert-True ($successLine[0].Contains("installer $instLen bytes")) "state 6: success line carries the verified installer byte count"
    Assert-True ($successLine[0].Contains("sha256 zip=$zipHash") -and $successLine[0].Contains("installer=$instHash")) "state 6: success line carries the verified SHA-256 digests"
    Assert-True ($successLine[0].Contains("both distribution packages present and non-empty")) "state 6: success line keeps its stable phrase"

    # ------------------------------------------------------------------
    # 7. ZIP_CORRUPT — non-empty bytes that are not a readable archive
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    Set-Content -LiteralPath $zipPath -Value "PK placeholder zip bytes (not a real archive)" -Encoding utf8NoBOM
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "ZIP_CORRUPT" -ExpectCode 2 -What "state 7 (corrupt archive)"

    # ------------------------------------------------------------------
    # 8. ZIP_BAD_ROOT — a real archive, but entries not under exactly one
    #    top-level SHEYTAN-LA/ directory
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    New-FixtureZip -Path $zipPath -Entries ([ordered]@{
        "wrong-root/SHEYTAN-LA.exe" = (New-ExePEBytes)
        "wrong-root/resources/docs/README.txt" = "misplaced"
    })
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "ZIP_BAD_ROOT" -ExpectCode 2 -What "state 8 (bad archive root)"

    # ------------------------------------------------------------------
    # 9. ZIP_MISSING_ENTRIES — real archive, correct root, contracted
    #    entry absent (the JSON names the exact missing paths)
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    $entries = New-ValidPayloadEntries
    $entries.Remove("SHEYTAN-LA/resources/docs/README.txt")
    New-FixtureZip -Path $zipPath -Entries $entries
    $resultPath9 = Join-Path $work "gate-result-9.json"
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -ResultPath $resultPath9
    Assert-GateOutcome -r $r -ExpectToken "ZIP_MISSING_ENTRIES" -ExpectCode 2 -What "state 9 (missing payload entries)" -ExpectJson
    Assert-True (@($r.Json.missingEntries) -contains "SHEYTAN-LA/resources/docs/README.txt") "state 9: JSON names the exact missing entry (got: $($r.Json.missingEntries -join ', '))"

    # ------------------------------------------------------------------
    # 10. ZIP_FORBIDDEN_ENTRIES — data residue and a Mesa DLL in the
    #     payload (the JSON names the exact offenders)
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    $entries = New-ValidPayloadEntries
    $entries["SHEYTAN-LA/data/app.log"] = "smoke residue must never ship"
    $entries["SHEYTAN-LA/opengl32.dll"] = ([byte[]](1,2,3,4))
    New-FixtureZip -Path $zipPath -Entries $entries
    $resultPath10 = Join-Path $work "gate-result-10.json"
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -ResultPath $resultPath10
    Assert-GateOutcome -r $r -ExpectToken "ZIP_FORBIDDEN_ENTRIES" -ExpectCode 2 -What "state 10 (forbidden payload entries)" -ExpectJson
    Assert-True (@($r.Json.forbiddenEntries) -contains "SHEYTAN-LA/data/app.log") "state 10: JSON names the data-residue entry (got: $($r.Json.forbiddenEntries -join ', '))"
    Assert-True (@($r.Json.forbiddenEntries) -contains "SHEYTAN-LA/opengl32.dll") "state 10: JSON names the Mesa DLL entry (got: $($r.Json.forbiddenEntries -join ', '))"

    # ------------------------------------------------------------------
    # 11. INSTALLER_NOT_PE — real archive, installer without MZ/PE
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $zipPath -Force
    New-FixtureZip -Path $zipPath -Entries (New-ValidPayloadEntries)
    Remove-Item -LiteralPath $instPath -Force
    New-FixtureInstallerFile -Path $instPath -NotPE
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "INSTALLER_NOT_PE" -ExpectCode 2 -What "state 11 (bad PE installer)"

    # ------------------------------------------------------------------
    # 12. INSTALLER_BAD_ARCH — a PE, but the wrong machine type for the
    #     contracted platform (ARM64 where i386/x86-64 is expected)
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $instPath -Force
    New-FixtureInstallerFile -Path $instPath -Machine 0xAA64
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "INSTALLER_BAD_ARCH" -ExpectCode 2 -What "state 12 (wrong-architecture installer)"

    # ------------------------------------------------------------------
    # 13. OK with an i386 installer — NSIS 3.x stubs are i386 PEs even
    #     when the payload is x64-only (the documented repo contract); the
    #     JSON must record the machine type it accepted.
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $instPath -Force
    New-FixtureInstallerFile -Path $instPath -Machine 0x014C
    $resultPath13 = Join-Path $work "gate-result-13.json"
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -ResultPath $resultPath13
    Assert-GateOutcome -r $r -ExpectToken "OK" -ExpectCode 0 -What "state 13 (i386 NSIS stub accepted)" -ExpectJson
    Assert-True ($r.Json.installerMachine -eq "0x014C") "state 13: JSON records the accepted NSIS stub machine type (got $($r.Json.installerMachine))"

    # ------------------------------------------------------------------
    # 14. INVALID_ARGUMENTS — a missing -Zip argument can never evaluate
    #     to PASS. Deterministic by explicit validation (v1 engaged the
    #     interactive mandatory-parameter prompt, which hangs a suite run
    #     with an open stdin and only failed by accident of closed CI
    #     stdin).
    # ------------------------------------------------------------------
    $r = Invoke-Gate -InstallerPath $instPath
    Assert-GateOutcome -r $r -ExpectToken "INVALID_ARGUMENTS" -ExpectCode 1 -What "state 14 (missing -Zip argument)"

    $r = Invoke-Gate -ZipPath $zipPath
    Assert-GateOutcome -r $r -ExpectToken "INVALID_ARGUMENTS" -ExpectCode 1 -What "state 14b (missing -Installer argument)"

    # ------------------------------------------------------------------
    # 15. CLI parity — the same invalid argument set through a real child
    #     pwsh -NonInteractive process yields the same token and a
    #     non-zero exit (pins that the contract is host-independent).
    # ------------------------------------------------------------------
    $cliOut = & pwsh -NoProfile -NonInteractive -File $gateScript -Installer $instPath 2>&1 6>&1
    $cliCode = $LASTEXITCODE
    $cliLines = @($cliOut | ForEach-Object { $_.ToString() })
    Add-FixtureLog -Lines $cliLines
    Assert-True ($cliCode -eq 1) "state 15: child-process missing -Zip exits 1 (got $cliCode)"
    Assert-True (@($cliLines | Where-Object { $_ -eq "PACKAGE_GATE_RESULT INVALID_ARGUMENTS" }).Count -eq 1) "state 15: child process emits the stable INVALID_ARGUMENTS token exactly once"

    # ------------------------------------------------------------------
    # 16. DEPENDENCY_CLOSURE_FAILURE — the pre-launch audit's structured
    #     verdict lines are the contract; FAIL/ERROR/absent/unrecognized
    #     reports are never a pass.
    # ------------------------------------------------------------------
    Remove-Item -LiteralPath $instPath -Force
    New-FixtureInstallerFile -Path $instPath -Machine 0x8664
    $depFail = Join-Path $work "dep-audit-FAIL.log"
    Set-Content -LiteralPath $depFail -Value @(
        "=== smoke dependency audit ===",
        "::error::smoke dependency audit: zlib.dll imported by SHEYTAN-LA.exe has no resolvable copy (exe dir, System32, PATH)",
        "smoke dependency audit: MISSING DEPENDENCIES — the launch would die at loader level with 0xC0000135"
    ) -Encoding utf8NoBOM
    $resultPath16 = Join-Path $work "gate-result-16.json"
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -DependencyReportPath $depFail -ResultPath $resultPath16
    Assert-GateOutcome -r $r -ExpectToken "DEPENDENCY_CLOSURE_FAILURE" -ExpectCode 2 -What "state 16a (audit verdict MISSING)" -ExpectJson
    Assert-True ($r.Json.dependencyVerdict -eq "FAIL") "state 16a: JSON records the FAIL verdict (got $($r.Json.dependencyVerdict))"

    $depError = Join-Path $work "dep-audit-ERROR.log"
    Set-Content -LiteralPath $depError -Value "smoke dependency audit: ERROR — at least one load-chain module could not be audited" -Encoding utf8NoBOM
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -DependencyReportPath $depError
    Assert-GateOutcome -r $r -ExpectToken "DEPENDENCY_CLOSURE_FAILURE" -ExpectCode 2 -What "state 16b (audit verdict ERROR)"

    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -DependencyReportPath (Join-Path $work "absent-audit.log")
    Assert-GateOutcome -r $r -ExpectToken "DEPENDENCY_CLOSURE_FAILURE" -ExpectCode 2 -What "state 16c (audit report absent — closure unverified)"

    # ------------------------------------------------------------------
    # 17. dependency PASS — a passing audit report keeps the OK state.
    # ------------------------------------------------------------------
    $depPass = Join-Path $work "dep-audit-PASS.log"
    Set-Content -LiteralPath $depPass -Value "smoke dependency audit: PASS — every named static import resolves (or is an OS API Set); safe to launch" -Encoding utf8NoBOM
    $resultPath17 = Join-Path $work "gate-result-17.json"
    $r = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -DependencyReportPath $depPass -ResultPath $resultPath17
    Assert-GateOutcome -r $r -ExpectToken "OK" -ExpectCode 0 -What "state 17 (dependency closure proven)" -ExpectJson
    Assert-True ($r.Json.dependencyVerdict -eq "PASS") "state 17: JSON records the PASS verdict (got $($r.Json.dependencyVerdict))"

    # ------------------------------------------------------------------
    # 18. Determinism — the same valid state run twice yields identical
    #     token/exit and byte counts.
    # ------------------------------------------------------------------
    $r1 = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -ResultPath (Join-Path $work "gate-result-det1.json")
    $r2 = Invoke-Gate -ZipPath $zipPath -InstallerPath $instPath -ResultPath (Join-Path $work "gate-result-det2.json")
    Assert-True (($r1.ExitCode -eq $r2.ExitCode) -and $r1.ExitCode -eq 0) "state 18: OK is deterministic across runs"
    Assert-True ($r1.Json.zipSha256 -eq $r2.Json.zipSha256 -and $r1.Json.installerSha256 -eq $r2.Json.installerSha256) "state 18: digests are deterministic"

    # ------------------------------------------------------------------
    # 19. Fixture-output preservation: the expected negative-fixture
    #     ::error:: lines were captured (never leaked as live
    #     annotations) and are visible — prefixed inert — in the escaped
    #     ci-logs/package-gate-fixtures.log.
    # ------------------------------------------------------------------
    $logged = @(Get-Content -LiteralPath $fixtureLog)
    Assert-True (($logged | Where-Object { $_.StartsWith("fixture-output: ::error::package gate:") }).Count -ge 1) "state 19: expected negative-fixture ::error:: output is preserved in the escaped fixture log"
    $unprefixed = @($logged | Where-Object { $_ -match '^::' })
    Assert-True ($unprefixed.Count -eq 0) "state 19: no captured line starts with a raw workflow-command prefix (annotation-inert)"

    # ------------------------------------------------------------------
    # 20. Result-file contract: an invalid state also writes the JSON
    #     (the fixtures consume structured data, never free text).
    # ------------------------------------------------------------------
    $resultPath20 = Join-Path $work "gate-result-20.json"
    $r = Invoke-Gate -ZipPath (Join-Path $work "ghost.zip") -InstallerPath $instPath -ResultPath $resultPath20
    Assert-GateOutcome -r $r -ExpectToken "ZIP_MISSING" -ExpectCode 2 -What "state 20 (JSON written on failure)" -ExpectJson
}
finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

if ($failures.Count -gt 0) {
    Write-Host ("PackageGate fixtures: {0} violation(s)" -f $failures.Count)
    $failures | ForEach-Object { Write-Host ("  - " + $_) }
    exit 1
}

Write-Host "PackageGate fixtures: all states, argument, integrity, dependency and determinism contracts hold"
exit 0
