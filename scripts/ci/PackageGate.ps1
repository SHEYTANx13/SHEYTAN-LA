<#
.SYNOPSIS
  Fail-closed Windows package gate (v1.11.1, run 42502043636 work order §3.3 item 6).

.DESCRIPTION
  Independently requires BOTH distribution files — the portable ZIP and the
  NSIS installer — to exist as regular, non-empty files before the job may
  report "package gate: reached". The predicate rejects EVERY incomplete
  state (the retired inline guard only rejected "ZIP missing AND installer
  exists", so it printed "package gate: reached" with both packages absent —
  exactly the captured consolidated-log line 3313 defect):

    1. neither package present              -> exit 2 (named state)
    2. ZIP missing, installer present       -> exit 2 (packaging drifted)
    3. installer missing, ZIP present       -> exit 2 (packaging drifted)
    4. ZIP present but empty (0 bytes)      -> exit 2
    5. installer present but empty (0 bytes)-> exit 2
    6. both present and non-empty           -> exit 0 + "package gate: reached"

  The success line is emitted ONLY after every assertion succeeds. The step
  that calls this script keeps `if: always()` so a red upstream build still
  produces the named root cause — but with this fail-closed predicate that
  outcome is now an error, never a green-looking "reached".

  Exit codes: 0 = both packages verified; 2 = package contract violated
  (named incomplete state); anything else = the gate could not evaluate
  (never 0 on any failure path).
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Zip,
    [Parameter(Mandatory = $true)][string]$Installer
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Fail([string]$What) {
    Write-Host ("::error::package gate: {0}" -f $What)
    exit 2
}

# --- states 1-3: both files must exist as REGULAR files (a directory or a
# non-leaf reparse target is not a distribution package) ---
$zipOk = $false
$instOk = $false
try {
    $zipOk = [bool](Test-Path -LiteralPath $Zip -PathType Leaf)
} catch {
    Fail ("portable ZIP path is not evaluable '{0}': {1}" -f $Zip, $_.Exception.Message)
}
try {
    $instOk = [bool](Test-Path -LiteralPath $Installer -PathType Leaf)
} catch {
    Fail ("installer path is not evaluable '{0}': {1}" -f $Installer, $_.Exception.Message)
}

if (-not $zipOk -and -not $instOk) {
    Fail ("neither distribution package exists — portable ZIP missing at '{0}' AND installer missing at '{1}'; the upstream build/package steps did not run or failed" -f $Zip, $Installer)
}
if (-not $zipOk) {
    Fail ("portable ZIP missing at '{0}' while the installer exists at '{1}' — packaging drifted" -f $Zip, $Installer)
}
if (-not $instOk) {
    Fail ("installer missing at '{0}' while the portable ZIP exists at '{1}' — packaging drifted" -f $Installer, $Zip)
}

# --- states 4-5: both files must be non-empty ---
try {
    $zipLen = (Get-Item -LiteralPath $Zip -Force).Length
} catch {
    Fail ("portable ZIP is not readable '{0}': {1}" -f $Zip, $_.Exception.Message)
}
if ($zipLen -le 0) {
    Fail ("portable ZIP exists but is EMPTY (0 bytes): '{0}'" -f $Zip)
}
try {
    $instLen = (Get-Item -LiteralPath $Installer -Force).Length
} catch {
    Fail ("installer is not readable '{0}': {1}" -f $Installer, $_.Exception.Message)
}
if ($instLen -le 0) {
    Fail ("installer exists but is EMPTY (0 bytes): '{0}'" -f $Installer)
}

# --- state 6: every assertion succeeded; and only here may the success
# line appear ---
Write-Host ("package gate: reached (portable ZIP {0} bytes, installer {1} bytes — both distribution packages present and non-empty)" -f $zipLen, $instLen)
exit 0
