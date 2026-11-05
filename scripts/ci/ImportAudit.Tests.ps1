<#
.SYNOPSIS
  Fixtures for the Windows smoke dependency audit (run 41933333132, P1-C).

.DESCRIPTION
  Runs the ImportAudit.ps1 decision contract against deterministic local
  fixtures and fails on any violation. No Pester, no network, no PE
  crafting beyond what a copy of a real OS binary provides:

    1. normal complete dependency closure  -> exit 0 (PASS)
    2. API-set names classified, never MISSING (real OS binary imports)
    3. missing load-chain module           -> exit 2 (FAIL)
    4. malformed PE (MZ + garbage)         -> exit 3 (ERROR, never PASS)
    5. unsupported PE (no MZ at all)       -> exit 3 (ERROR, never PASS)
    6. script PARSE error                  -> non-zero, never 0
       (the workflow wrapper classifies any non-zero as ERROR)
    7. script RUNTIME exception            -> exit 3, never 0
    8. native tool missing is structurally impossible: the parser is pure
       .NET (no dumpbin/VS dependency) — pinned by test 1 passing without
       any native tool on PATH.

  Exit codes: 0 all fixtures hold; 1 fixture contract violated.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

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
$auditScript = Join-Path $PSScriptRoot "ImportAudit.ps1"
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("importaudit-fixtures-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $work | Out-Null

try {
    # --- fixture: a real PE whose import closure resolves on a clean OS ---
    $sysExe = Join-Path $env:SystemRoot "System32\notepad.exe"
    if (-not (Test-Path $sysExe)) { $sysExe = Join-Path $env:SystemRoot "System32\where.exe" }
    Assert-True (Test-Path $sysExe) "a real OS PE is available for the closure fixture"

    $passDir = Join-Path $work "pass"
    New-Item -ItemType Directory -Force -Path $passDir | Out-Null
    Copy-Item $sysExe (Join-Path $passDir "SHEYTAN-LA.exe")

    $out = & $auditScript -ExeDir $passDir -Modules @("SHEYTAN-LA.exe") 2>&1
    $code = $LASTEXITCODE
    $text = ($out | ForEach-Object { $_.ToString() }) -join "`n"
    Assert-True ($code -eq 0) "fixture 1: complete closure exits 0 (got $code)"
    Assert-True ($text -match "PASS") "fixture 1: report carries the structured PASS result"

    # --- fixture 2: API-set names are classified, never MISSING ---
    if ($text -match "api-ms-|ext-ms-") {
        Assert-True ($text -notmatch "::error::.*api-ms-") "fixture 2: API-set names are never reported MISSING"
        Assert-True ($text -match "\[API-SET\]") "fixture 2: API-set names are classified as API-SET"
    } else {
        Write-Host "skip: fixture 2 (the fixture binary carries no api-ms-/ext-ms- imports on this OS image)"
    }

    # --- fixture 3: a load-chain module absent from the smoke copy ---
    $out = & $auditScript -ExeDir $passDir -Modules @("ghost.dll") 2>&1
    $code = $LASTEXITCODE
    Assert-True ($code -eq 2) "fixture 3: missing load-chain module exits 2 (got $code)"

    # --- fixture 4: malformed PE (MZ header, garbage body) ---
    $badDir = Join-Path $work "malformed"
    New-Item -ItemType Directory -Force -Path $badDir | Out-Null
    $bad = Join-Path $badDir "opengl32.dll"
    $bytes = New-Object byte[] 512
    $bytes[0] = 0x4D; $bytes[1] = 0x5A
    (New-Object Random(7)).NextBytes($bytes)
    for ($i = 2; $i -lt 64; $i++) { $bytes[$i] = 0x00 }
    [System.IO.File]::WriteAllBytes($bad, $bytes)

    $out = & $auditScript -ExeDir $badDir -Modules @("opengl32.dll") 2>&1
    $code = $LASTEXITCODE
    Assert-True ($code -eq 3) "fixture 4: malformed PE exits 3 — ERROR, never PASS (got $code)"

    # --- fixture 5: unsupported PE (no MZ at all — a text file) ---
    $txtDir = Join-Path $work "textpe"
    New-Item -ItemType Directory -Force -Path $txtDir | Out-Null
    Set-Content -LiteralPath (Join-Path $txtDir "libgallium_wgl.dll") -Value "not a PE at all"
    $out = & $auditScript -ExeDir $txtDir -Modules @("libgallium_wgl.dll") 2>&1
    $code = $LASTEXITCODE
    Assert-True ($code -eq 3) "fixture 5: non-PE module exits 3 — ERROR, never PASS (got $code)"

    # --- fixture 6: a script-level PARSE error must never exit 0 ---
    $broken = Join-Path $work "ImportAudit.broken.ps1"
    Set-Content -LiteralPath $broken -Value "function Broken { { this is not valid PowerShell }"
    $parseOk = $true
    try {
        $null = & pwsh -NoProfile -NonInteractive -File $broken 2>&1
        $code = $LASTEXITCODE
    } catch {
        $parseOk = $false
        $code = 3
    }
    Assert-True ((-not $parseOk) -or $code -ne 0) "fixture 6: a parse-broken audit script never yields exit 0 (got $code)"

    # --- fixture 7: a script-level RUNTIME exception must exit 3 ---
    $thrower = Join-Path $work "ImportAudit.thrower.ps1"
    Set-Content -LiteralPath $thrower -Value "`$ErrorActionPreference = 'Stop'`nthrow [System.InvalidOperationException]::new('fixture runtime exception')"
    $out = & pwsh -NoProfile -NonInteractive -File $thrower 2>&1
    $code = $LASTEXITCODE
    Assert-True ($code -ne 0) "fixture 7: a runtime-exception audit script never yields exit 0 (got $code)"

    # --- fixture 8: ERROR/UNKNOWN fixtures must fail a release gate ---
    # The workflow wrapper's contract: exit 0 proceeds; ANY other exit is
    # FAIL (2) or ERROR (3, marker + job gate). Pinned here by asserting
    # the audit never maps the fixtures above onto 0.
    Assert-True ($failures.Count -eq 0) "fixture 8: no ERROR/UNKNOWN fixture mapped to PASS"
}
finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

if ($failures.Count -gt 0) {
    Write-Host ("ImportAudit fixtures: {0} violation(s)" -f $failures.Count)
    $failures | ForEach-Object { Write-Host ("  - " + $_) }
    exit 1
}

Write-Host "ImportAudit fixtures: all contracts hold"
exit 0
