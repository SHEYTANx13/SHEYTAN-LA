# Capture-Step.ps1 — first-byte command capture for the consolidated run
# log on the Windows job (pwsh). Mirrors scripts/ci/capture.sh.
#
# Usage:
#   ./scripts/ci/Capture-Step.ps1 -Job "windows" -Step "build" -Log "ci-logs/windows/build.log" -Command { go build ./... }
#
# Contract:
#   - stdout + stderr captured from the first byte into the namespaced log.
#   - $LASTEXITCODE saved IMMEDIATELY after the native command.
#   - Structured STARTED / EXIT_CODE / ENDED records with the command text.
#   - The script rethrows on FAIL so the step terminates; the original
#     exit code is surfaced via `exit $code` semantics by the caller.
param(
  [Parameter(Mandatory = $true)][string]$Job,
  [Parameter(Mandatory = $true)][string]$Step,
  [Parameter(Mandatory = $true)][string]$Log,
  [Parameter(Mandatory = $true)][scriptblock]$Command
)

$ErrorActionPreference = 'Stop'

$dir = Split-Path -Parent $Log
if ($dir) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }

$startUtc = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
$cmdText = $Command.ToString().Trim()

@(
  '=== COMMAND RECORD ==='
  "JOB: $Job"
  "STEP: $Step"
  "STARTED: $startUtc"
  "WORKDIR: $PWD"
  "COMMAND: $cmdText"
) | Set-Content -Path $Log -Encoding utf8NoBOM

try {
  & $Command 2>&1 | ForEach-Object { "$_" } | Add-Content -Path $Log -Encoding utf8NoBOM
} catch {
  "ERROR: $($_.Exception.Message)" | Add-Content -Path $Log -Encoding utf8NoBOM
  "ERRORTYPE: $($_.Exception.GetType().FullName)" | Add-Content -Path $Log -Encoding utf8NoBOM
  "TARGET: $($_.TargetObject)" | Add-Content -Path $Log -Encoding utf8NoBOM
}

# Save the exit status of the last native command IMMEDIATELY.
$code = if (Get-Variable -Name LASTEXITCODE -ErrorAction SilentlyContinue) { $LASTEXITCODE } else { 0 }
$endUtc = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
$verdict = if ($code -eq 0) { 'PASS' } else { 'FAIL' }

@(
  "EXIT_CODE: $code $verdict"
  "ENDED: $endUtc"
  '=== END COMMAND RECORD ==='
) | Add-Content -Path $Log -Encoding utf8NoBOM

if ($code -ne 0) {
  throw "step '$Step' failed with exit code $code (see $Log)"
}
exit $code
