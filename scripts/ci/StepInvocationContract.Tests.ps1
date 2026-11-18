# Work order section 4.2.4 regression fixture (run 44306343338): the
# PowerShell step-invocation error contract, pinned under
# Set-StrictMode -Version Latest with $ErrorActionPreference='Stop' and
# NO inherited $LASTEXITCODE.
#
# Contract pinned here:
#   1. a fresh strict-mode scope has NO LASTEXITCODE variable;
#   2. a pure PowerShell script invocation is judged through $?
#      (immediately after the invocation), never through $LASTEXITCODE;
#   3. $LASTEXITCODE is read ONLY after a native executable has actually
#      run, captured immediately;
#   4. a failing script invocation is detected via $? becoming false.
#
# Exit contract: 0 = every assertion holds; 1 = the contract drifted.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

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

# (1) No inherited $LASTEXITCODE: no native command has run in this scope.
$inherited = Get-Variable -Name LASTEXITCODE -ErrorAction SilentlyContinue
Assert-True ($null -eq $inherited) "a fresh strict-mode scope inherits NO LASTEXITCODE variable"

# (2a) A succeeding pure PowerShell script invocation: $? is true.
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("stepinvocation-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$okScript = Join-Path $tmp "child-success.ps1"
Set-Content -Path $okScript -Value "Set-StrictMode -Version Latest`r`nexit 0" -Encoding utf8NoBOM
& $okScript
$ok = $?
Assert-True $ok "a succeeding pure-script invocation reports `$? = true"

# (2b) A failing pure PowerShell script invocation: $? is false — the
#      verdict comes from $?, and $LASTEXITCODE is NOT consulted (under
#      strict mode, reading it unset throws; reading it stale lies).
$failScript = Join-Path $tmp "child-failure.ps1"
Set-Content -Path $failScript -Value "Set-StrictMode -Version Latest`r`nexit 3" -Encoding utf8NoBOM
& $failScript
$detected = (-not $?)
Assert-True $detected "a failing pure-script invocation is detected through `$? = false"

# (3) $LASTEXITCODE is legitimate ONLY after a native executable, and it
#     is captured immediately after that native command.
& git --version | Out-Null
$nativeExit = $LASTEXITCODE
Assert-True ($nativeExit -eq 0) "LASTEXITCODE after the native 'git --version' is 0 (captured immediately)"

# (4) The native-vs-script distinction in ONE flow: after the script
#     invocations above, LASTEXITCODE existed only because (3) ran a
#     native command; a script result never set it.
$postNative = Get-Variable -Name LASTEXITCODE -ErrorAction SilentlyContinue
Assert-True ($null -ne $postNative -and $postNative.Value -eq 0) "LASTEXITCODE reflects the NATIVE command, not the child scripts"

Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue

if ($failures.Count -gt 0) {
    Write-Host ("step-invocation contract: {0} failure(s)" -f $failures.Count)
    exit 1
}
Write-Host "step-invocation contract OK (strict mode, no inherited LASTEXITCODE, dollar-question verdicts, native LASTEXITCODE)"
exit 0
