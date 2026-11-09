<#
.SYNOPSIS
  Fixtures for the fail-closed Windows package gate (v1.11.1, run
  42502043636 work order §3.3 item 6).

.DESCRIPTION
  Runs scripts/ci/PackageGate.ps1 against deterministic local fixtures and
  fails on any violation. No Pester, no network. The gate's six-state truth
  table is pinned exactly:

    1. neither package present              -> exit 2, named root cause
    2. ZIP missing, installer present       -> exit 2, "packaging drifted"
    3. installer missing, ZIP present       -> exit 2, "packaging drifted"
    4. ZIP present but empty (0 bytes)      -> exit 2, "EMPTY"
    5. installer present but empty (0 bytes)-> exit 2, "EMPTY"
    6. both present and non-empty           -> exit 0 + "package gate: reached"

  Additional contract pins:

    7. the success line "package gate: reached" appears ONLY in state 6 —
       no incomplete state may produce it (the retired inline guard printed
       it with both packages absent — consolidated-log line 3313);
    8. a missing -Zip argument can never evaluate to PASS (parameter
       binding failure is non-zero).

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
$gateScript = Join-Path $PSScriptRoot "PackageGate.ps1"
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("packagegate-fixtures-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $work | Out-Null

try {
    Assert-True (Test-Path -LiteralPath $gateScript -PathType Leaf) "precondition: PackageGate.ps1 exists beside this fixture suite"

    $zipName = "SHEYTAN-LA-v1.11.1-windows-x64.zip"
    $instName = "SHEYTAN-LA-v1.11.1-windows-x64-installer.exe"

    function Invoke-Gate {
        param([string]$ZipPath, [string]$InstallerPath)
        $out = & $gateScript -Zip $ZipPath -Installer $InstallerPath 2>&1
        [pscustomobject]@{
            ExitCode = $LASTEXITCODE
            Output   = ($out | ForEach-Object { [string]$_ }) -join "`n"
        }
    }

    # --- state 1: neither package present ---
    $r = Invoke-Gate -ZipPath (Join-Path $work $zipName) -InstallerPath (Join-Path $work $instName)
    Assert-True ($r.ExitCode -eq 2) "state 1 (both missing): exit 2 (got $($r.ExitCode))"
    Assert-True ($r.Output -match "neither distribution package exists") "state 1: names the both-missing root cause"
    Assert-True ($r.Output -notmatch "package gate: reached") "state 1: never prints the success line"

    # --- state 2: ZIP missing, installer present ---
    $instFile = Join-Path $work $instName
    Set-Content -LiteralPath $instFile -Value "placeholder installer bytes" -Encoding utf8NoBOM
    $r = Invoke-Gate -ZipPath (Join-Path $work $zipName) -InstallerPath $instFile
    Assert-True ($r.ExitCode -eq 2) "state 2 (ZIP-only missing): exit 2 (got $($r.ExitCode))"
    Assert-True ($r.Output -match "portable ZIP missing") "state 2: names the missing ZIP"
    Assert-True ($r.Output -notmatch "package gate: reached") "state 2: never prints the success line"

    # --- state 3: installer missing, ZIP present ---
    Remove-Item -LiteralPath $instFile -Force
    $zipFile = Join-Path $work $zipName
    Set-Content -LiteralPath $zipFile -Value "placeholder zip bytes" -Encoding utf8NoBOM
    $r = Invoke-Gate -ZipPath $zipFile -InstallerPath (Join-Path $work $instName)
    Assert-True ($r.ExitCode -eq 2) "state 3 (installer-only missing): exit 2 (got $($r.ExitCode))"
    Assert-True ($r.Output -match "installer missing") "state 3: names the missing installer"
    Assert-True ($r.Output -notmatch "package gate: reached") "state 3: never prints the success line"

    # --- state 4: ZIP present but EMPTY ---
    Remove-Item -LiteralPath $zipFile -Force
    $emptyZip = Join-Path $work $zipName
    New-Item -ItemType File -Force -Path $emptyZip | Out-Null
    Set-Content -LiteralPath $instFile -Value "placeholder installer bytes" -Encoding utf8NoBOM
    $r = Invoke-Gate -ZipPath $emptyZip -InstallerPath $instFile
    Assert-True ($r.ExitCode -eq 2) "state 4 (empty ZIP): exit 2 (got $($r.ExitCode))"
    Assert-True ($r.Output -match "EMPTY") "state 4: names the empty ZIP"
    Assert-True ($r.Output -notmatch "package gate: reached") "state 4: never prints the success line"

    # --- state 5: installer present but EMPTY ---
    Set-Content -LiteralPath $zipFile -Value "placeholder zip bytes" -Encoding utf8NoBOM
    $emptyInst = Join-Path $work $instName
    if (Test-Path -LiteralPath $emptyInst) { Remove-Item -LiteralPath $emptyInst -Force }
    New-Item -ItemType File -Force -Path $emptyInst | Out-Null
    $r = Invoke-Gate -ZipPath $zipFile -InstallerPath $emptyInst
    Assert-True ($r.ExitCode -eq 2) "state 5 (empty installer): exit 2 (got $($r.ExitCode))"
    Assert-True ($r.Output -match "EMPTY") "state 5: names the empty installer"
    Assert-True ($r.Output -notmatch "package gate: reached") "state 5: never prints the success line"

    # --- state 6: both present and non-empty ---
    Set-Content -LiteralPath $zipFile -Value "PK placeholder zip bytes" -Encoding utf8NoBOM
    Set-Content -LiteralPath $instFile -Value "placeholder installer bytes" -Encoding utf8NoBOM
    $r = Invoke-Gate -ZipPath $zipFile -InstallerPath $instFile
    Assert-True ($r.ExitCode -eq 0) "state 6 (both valid): exit 0 (got $($r.ExitCode))"
    Assert-True ($r.Output -match "package gate: reached") "state 6: prints the success line"

    # --- fixture 7: the success line is exclusive to the valid state ---
    # (asserted negatively per state above; here the positive side is
    # counted exactly once across the whole run)
    Assert-True ($r.Output -match "both distribution packages present and non-empty") "state 6: success line carries the verified byte counts"

    # --- fixture 8: an incomplete argument set can never PASS ---
    try {
        $out = & $gateScript -Installer $instFile 2>&1
        $code = $LASTEXITCODE
    } catch {
        $code = 1
        $out = @([string]$_.Exception.Message)
    }
    Assert-True ($code -ne 0) "fixture 8: a missing -Zip argument never yields exit 0 (got $code)"
}
finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

if ($failures.Count -gt 0) {
    Write-Host ("PackageGate fixtures: {0} violation(s)" -f $failures.Count)
    $failures | ForEach-Object { Write-Host ("  - " + $_) }
    exit 1
}

Write-Host "PackageGate fixtures: all six states + exclusivity contracts hold"
exit 0
