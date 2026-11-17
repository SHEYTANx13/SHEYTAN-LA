<#
.SYNOPSIS
  Fixtures for the NSIS 3.13.0 version gate (v1.11.1, run 43883548198
  repair) — deterministic, table-driven, no Pester, no network, no NSIS.

.DESCRIPTION
  Drives scripts/ci/NsisVersion.ps1 (Get-NsisVersionVerdict) — the SAME
  function the workflow's "Install NSIS" step runs against the real
  pinned makensis — with the positive and negative fixture matrices the
  work order §2.2 requires.

  Accepted (the run-43883548198 defect class and its neighbors):
    v3.13, 3.13, v3.13.0, 3.13.0, a valid 3.13 patch value (3.13.1),
    and the Out-String materialization "v3.13\r\n".

  Rejected (fail closed):
    3.12, v3.12.9, 3.130, 3.14, v4.0, v3.13-preview, empty output,
    whitespace-only output, multiline garbage, and a nonzero process
    exit carrying an otherwise-valid version string (the exit code
    dominates the text).

  The workflow step additionally asserts the executable identity (the
  exact pinned C:\Program Files (x86)\NSIS\makensis.exe path) and logs
  the path, raw output and exit code — that part needs the real runner
  and is NOT reproducible here; these fixtures pin the PARSER contract.

  Exit codes: 0 all fixtures hold; 1 fixture contract violated.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot "NsisVersion.ps1")

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

# Human-visible form of a fixture (CR/LF escaped) for stable output.
function Show {
    param([string]$S)
    return (($S -replace "`r", '\r') -replace "`n", '\n')
}

# Positive matrix: the pinned 3.13 line in every legal spelling.
$accept = @(
    @{ Raw = 'v3.13';       Exit = 0 }   # the EXACT run-43883548198 output
    @{ Raw = '3.13';        Exit = 0 }   # no leading v
    @{ Raw = 'v3.13.0';     Exit = 0 }   # explicit zero patch
    @{ Raw = '3.13.0';      Exit = 0 }   # no v, explicit patch
    @{ Raw = '3.13.1';      Exit = 0 }   # a valid 3.13 patch value
    @{ Raw = "v3.13`r`n";   Exit = 0 }   # Out-String trailing newline
)

# Negative matrix: wrong versions, malformed shapes, no output, bad exit.
$reject = @(
    @{ Raw = '3.12';           Exit = 0 }
    @{ Raw = 'v3.12.9';        Exit = 0 }
    @{ Raw = '3.130';          Exit = 0 }   # the classic prefix-match trap
    @{ Raw = '3.14';           Exit = 0 }
    @{ Raw = 'v4.0';           Exit = 0 }
    @{ Raw = 'v3.13-preview';  Exit = 0 }   # suffix garbage after the version
    @{ Raw = '';               Exit = 0 }   # empty output
    @{ Raw = '   ';            Exit = 0 }   # whitespace-only output
    @{ Raw = "v3.13`nboom";    Exit = 0 }   # multiline garbage
    @{ Raw = 'v3.13';          Exit = 1 }   # nonzero exit dominates the text
)

foreach ($c in $accept) {
    $v = Get-NsisVersionVerdict -RawOutput $c.Raw -ExitCode $c.Exit
    Assert-True $v.Accepted "accept fixture raw='$(Show $c.Raw)' exit=$($c.Exit) (reason: $($v.Reason))"
    if ($v.Accepted) {
        Assert-True ($v.Major -eq 3 -and $v.Minor -eq 13) "parsed major.minor is 3.13 for raw='$(Show $c.Raw)' (got $($v.Major).$($v.Minor))"
        Assert-True ($null -eq $v.Patch -or $v.Patch -is [int]) "patch is null or an int for raw='$(Show $c.Raw)'"
        Assert-True ($v.Reason -eq '') "an accepted fixture carries no failure reason (raw='$(Show $c.Raw)')"
    }
}

foreach ($c in $reject) {
    $v = Get-NsisVersionVerdict -RawOutput $c.Raw -ExitCode $c.Exit
    Assert-True (-not $v.Accepted) "reject fixture raw='$(Show $c.Raw)' exit=$($c.Exit) must be REJECTED"
    Assert-True ($v.Reason -ne '') "a rejected fixture carries a named reason (raw='$(Show $c.Raw)')"
}

# The verdict object shape the workflow relies on (fail-closed contract:
# a shape drift cannot silently turn a rejection into an acceptance).
$probe = Get-NsisVersionVerdict -RawOutput 'v3.13' -ExitCode 0
foreach ($prop in @('Accepted', 'Reason', 'Raw', 'Normalized', 'ExitCode', 'Major', 'Minor', 'Patch')) {
    Assert-True ($null -ne ($probe.PSObject.Properties[$prop])) "verdict object exposes property '$prop' (the workflow and fixtures depend on it)"
}
Assert-True ($probe.Normalized -eq 'v3.13') "normalization trims only whitespace (got '$($probe.Normalized)')"

if ($failures.Count -gt 0) {
    Write-Host ("nsis-version fixtures: {0} FAILURE(S)" -f $failures.Count)
    $failures | ForEach-Object { Write-Host "  FAIL: $_" }
    exit 1
}

Write-Host "nsis-version fixtures: all fixtures hold (accept + reject matrices + verdict shape)"
exit 0
