<#
.SYNOPSIS
  Fail-closed Windows package gate (v1.11.1) — stable machine-readable
  outcome contract (run 42928792246 work order §3.2).

.DESCRIPTION
  Independently requires BOTH distribution files — the portable ZIP and
  the NSIS installer — to be present, non-empty and structurally valid
  before the job may report "package gate: reached".

  Run 42928792246 root cause (REPRODUCED locally on PowerShell 7.4): the
  v1 fixture harness captured the gate with `2>&1` only, while the gate
  emitted every diagnostic through `Write-Host` (information stream 6).
  `2>&1` merges stream 2 into 1 — it NEVER merges the information
  stream — so the harness's captured output was empty, the exit codes
  were correct, and all seven diagnostic-output assertions failed while
  the raw `::error::` lines leaked to the console and became live
  workflow annotations. The stable contract below therefore does NOT
  depend on any free-form text, stream routing, terminal formatting,
  locale or host behavior:

    1. ONE machine-readable token line on the SUCCESS stream (capturable
       by any of *, 2>&1, Tee-Object, or a file redirect):

           PACKAGE_GATE_RESULT <TOKEN>

       emitted EXACTLY ONCE per invocation, on every outcome, in every
       state — before any human-readable line.

    2. A structured JSON result file (optional -ResultFile parameter)
       with the affected paths, byte counts, entry inventories, installer
       PE machine type, dependency verdict and SHA-256 digests.

    3. Human-readable `::error::` diagnostics via Write-Host (real CI
       annotations on hosted runs) that name the affected path — the
       fixtures treat these as inert data, never as assertions.

  Truth table (token -> exit code; 0 ONLY for OK):

    BOTH_MISSING               2   neither package present
    ZIP_MISSING                2   ZIP absent, installer present
    INSTALLER_MISSING          2   installer absent, ZIP present
    ZIP_EMPTY                  2   ZIP present, 0 bytes
    INSTALLER_EMPTY            2   installer present, 0 bytes
    ZIP_CORRUPT                2   ZIP non-empty but not a readable archive
    ZIP_BAD_ROOT               2   archive does not hold exactly one
                                   top-level directory named -ExpectedRoot
    ZIP_MISSING_ENTRIES        2   contracted required entries absent
    ZIP_FORBIDDEN_ENTRIES      2   data/Mesa/Qt/placeholder residue present
    INSTALLER_NOT_PE           2   installer lacks the MZ/PE signature
    INSTALLER_BAD_ARCH         2   installer PE machine type is neither
                                   i386 (NSIS 3.x stub) nor x86-64
    DEPENDENCY_CLOSURE_FAILURE 2   -DependencyReport absent/unreadable/
                                   unrecognized, or its audit verdict is
                                   MISSING/ERROR (closure never assumed)
    INVALID_ARGUMENTS          1   required arguments missing/invalid
                                   (deterministic: explicit validation,
                                   never the interactive prompt machinery)
    EVALUATION_ERROR           3   the gate could not evaluate (never 0)
    OK                         0   every assertion succeeded

  The success line "package gate: reached (...)" is emitted ONLY in the
  OK state, AFTER every assertion succeeds, and carries the ACTUAL
  on-disk byte counts and SHA-256 digests (never fixture constants).

  Exit codes: 0 = OK; 1 = invalid arguments; 2 = package contract
  violated (named state); 3 = gate could not evaluate. Every failure
  path is non-zero: fail-closed.
#>

[CmdletBinding()]
param(
    [string]$Zip,
    [string]$Installer,
    [string]$ResultFile,
    [string[]]$RequiredEntries,
    [string]$ExpectedRoot = "SHEYTAN-LA",
    [string]$DependencyReport
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

# Contracted default required entries (relative to the archive root) —
# mirrors the "Verify Windows ZIP" step's required-entry list exactly.
if (-not $RequiredEntries -or $RequiredEntries.Count -eq 0) {
    $RequiredEntries = @(
        "SHEYTAN-LA.exe",
        "SHEYTAN-LA.bat",
        "resources/docs/README.txt",
        "resources/docs/BUILD-INFO.txt",
        "resources/docs/LICENSE.md",
        "resources/brand/sheytan.ico"
    )
}

$script:Result = [ordered]@{
    token             = "EVALUATION_ERROR"
    exitCode          = 3
    zipPath           = $Zip
    installerPath     = $Installer
    zipBytes          = $null
    installerBytes    = $null
    zipEntryCount     = $null
    missingEntries    = @()
    forbiddenEntries  = @()
    installerMachine  = $null
    dependencyReport  = $DependencyReport
    dependencyVerdict = $null
    zipSha256         = $null
    installerSha256   = $null
    expectedRoot      = $ExpectedRoot
    timestampUtc      = [System.DateTime]::UtcNow.ToString("o", [System.Globalization.CultureInfo]::InvariantCulture)
}

function Format-Invariant {
    param([object]$Value)
    if ($null -eq $Value) { return "" }
    return $Value.ToString([System.Globalization.CultureInfo]::InvariantCulture)
}

function Write-ResultFile {
    # Structured side-channel: the fixtures and the consolidated log
    # consume this instead of parsing free-form text. A failure to write
    # it must not mask the gate's real verdict, so it is best-effort.
    if (-not $ResultFile) { return }
    try {
        $json = $script:Result | ConvertTo-Json -Depth 4
        [System.IO.File]::WriteAllText($ResultFile, $json, (New-Object System.Text.UTF8Encoding($false)))
    } catch {
        Write-Host ("::error::package gate: could not write the JSON result file '{0}': {1}" -f $ResultFile, $_.Exception.Message)
    }
}

function Complete-Gate {
    param([string]$Token, [int]$Code)
    $script:Result.token = $Token
    $script:Result.exitCode = $Code
    # The ONE machine-readable outcome line: success stream, invariant
    # token, exactly once, in every state.
    Write-Output ("PACKAGE_GATE_RESULT {0}" -f $Token)
    Write-ResultFile
    exit $Code
}

function Fail-State {
    param([string]$Token, [int]$Code, [string]$Message)
    Write-Host ("::error::package gate: {0}" -f $Message)
    Complete-Gate -Token $Token -Code $Code
}

try {
    # ------------------------------------------------------------------
    # 0. Invalid arguments (deterministic, never the interactive prompt
    #    machinery that a Mandatory-parameter binding failure engages).
    # ------------------------------------------------------------------
    if (-not $Zip -or -not $Installer) {
        Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: both -Zip and -Installer are required (got zip='{0}', installer='{1}')" -f $Zip, $Installer)
    }
    if (-not $ExpectedRoot) {
        Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message "invalid arguments: -ExpectedRoot must not be empty"
    }
    foreach ($entry in $RequiredEntries) {
        if ([string]::IsNullOrWhiteSpace($entry)) {
            Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message "invalid arguments: -RequiredEntries must not contain empty values"
        }
    }

    # ------------------------------------------------------------------
    # 1-3. Existence: both files must exist as REGULAR files (a directory
    # or a non-leaf reparse target is not a distribution package).
    # ------------------------------------------------------------------
    $zipOk = $false
    $instOk = $false
    try {
        $zipOk = [bool](Test-Path -LiteralPath $Zip -PathType Leaf)
    } catch {
        Fail-State -Token "ZIP_MISSING" -Code 2 -Message ("portable ZIP path is not evaluable '{0}': {1}" -f $Zip, $_.Exception.Message)
    }
    try {
        $instOk = [bool](Test-Path -LiteralPath $Installer -PathType Leaf)
    } catch {
        Fail-State -Token "INSTALLER_MISSING" -Code 2 -Message ("installer path is not evaluable '{0}': {1}" -f $Installer, $_.Exception.Message)
    }

    if (-not $zipOk -and -not $instOk) {
        Fail-State -Token "BOTH_MISSING" -Code 2 -Message ("neither distribution package exists — portable ZIP missing at '{0}' AND installer missing at '{1}'; the upstream build/package steps did not run or failed" -f $Zip, $Installer)
    }
    if (-not $zipOk) {
        Fail-State -Token "ZIP_MISSING" -Code 2 -Message ("portable ZIP missing at '{0}' while the installer exists at '{1}' — packaging drifted" -f $Zip, $Installer)
    }
    if (-not $instOk) {
        Fail-State -Token "INSTALLER_MISSING" -Code 2 -Message ("installer missing at '{0}' while the portable ZIP exists at '{1}' — packaging drifted" -f $Installer, $Zip)
    }

    # ------------------------------------------------------------------
    # 4-5. Non-empty: both files must carry actual bytes.
    # ------------------------------------------------------------------
    try {
        $zipLen = (Get-Item -LiteralPath $Zip -Force).Length
    } catch {
        Fail-State -Token "ZIP_EMPTY" -Code 2 -Message ("portable ZIP is not readable '{0}': {1}" -f $Zip, $_.Exception.Message)
    }
    $script:Result.zipBytes = $zipLen
    if ($zipLen -le 0) {
        Fail-State -Token "ZIP_EMPTY" -Code 2 -Message ("portable ZIP exists but is EMPTY (0 bytes): '{0}'" -f $Zip)
    }
    try {
        $instLen = (Get-Item -LiteralPath $Installer -Force).Length
    } catch {
        Fail-State -Token "INSTALLER_EMPTY" -Code 2 -Message ("installer is not readable '{0}': {1}" -f $Installer, $_.Exception.Message)
    }
    $script:Result.installerBytes = $instLen
    if ($instLen -le 0) {
        Fail-State -Token "INSTALLER_EMPTY" -Code 2 -Message ("installer exists but is EMPTY (0 bytes): '{0}'" -f $Installer)
    }

    # ------------------------------------------------------------------
    # 6. ZIP integrity: the archive must actually open (a non-empty
    #    non-ZIP file or a truncated central directory fails here).
    # ------------------------------------------------------------------
    Add-Type -AssemblyName System.IO.Compression.FileSystem -ErrorAction SilentlyContinue
    $archive = $null
    $entryNames = $null
    try {
        $archive = [System.IO.Compression.ZipFile]::OpenRead((Resolve-Path -LiteralPath $Zip).Path)
    } catch {
        Fail-State -Token "ZIP_CORRUPT" -Code 2 -Message ("portable ZIP cannot be opened as an archive ('{0}', {1} bytes): {2}" -f $Zip, (Format-Invariant $zipLen), $_.Exception.Message)
    }
    try {
        $entryNames = @($archive.Entries | ForEach-Object { $_.FullName })
        $script:Result.zipEntryCount = $entryNames.Count

        # 7. Root layout: exactly ONE top-level directory, named -ExpectedRoot.
        $topSegments = @($entryNames |
            ForEach-Object { ($_ -split '/')[0] } |
            Where-Object { $_ -and $_.Trim() -ne "" } |
            Sort-Object -Unique)
        $rootOk = ($topSegments.Count -eq 1) -and
                  ($topSegments[0] -eq $ExpectedRoot)
        if (-not $rootOk) {
            Fail-State -Token "ZIP_BAD_ROOT" -Code 2 -Message ("portable ZIP must contain exactly one top-level directory named '{0}' (found: {1}) — extract/validate the package layout" -f $ExpectedRoot, ($(if ($topSegments.Count -eq 0) { "<none>" } else { $topSegments -join ', ' })))
        }

        # 8. Required entries (exact path, case-insensitive, ordinal-safe).
        $missing = @()
        foreach ($rel in $RequiredEntries) {
            $wanted = "$ExpectedRoot/$rel"
            $hit = @($entryNames | Where-Object { $_ -ieq $wanted })
            if ($hit.Count -eq 0) { $missing += $wanted }
        }
        $script:Result.missingEntries = $missing
        if ($missing.Count -gt 0) {
            Fail-State -Token "ZIP_MISSING_ENTRIES" -Code 2 -Message ("portable ZIP is missing {0} required payload entr(y/ies): {1}" -f (Format-Invariant $missing.Count), ($missing -join ', '))
        }

        # 9. Forbidden entries: data residue, Mesa software-GL DLLs, Qt
        #    runtime DLLs and retired root-level placeholders (mirrors the
        #    "Verify Windows ZIP" purity contract — the gate re-checks it
        #    on the package file itself).
        $forbidden = @()
        foreach ($name in $entryNames) {
            $leaf = if ($name.Contains('/')) { $name.Substring($name.LastIndexOf('/') + 1) } else { $name }
            if ($name.StartsWith("$ExpectedRoot/data/", [System.StringComparison]::OrdinalIgnoreCase) -or
                $name.StartsWith("$ExpectedRoot/models/", [System.StringComparison]::OrdinalIgnoreCase) -or
                $name.StartsWith("$ExpectedRoot/workspace/", [System.StringComparison]::OrdinalIgnoreCase)) {
                $forbidden += $name
            } elseif ($leaf -ieq "opengl32.dll" -or $leaf -ieq "libgallium_wgl.dll") {
                $forbidden += $name
            } elseif ($leaf -like 'Qt5*.dll' -or $leaf -like 'Qt6*.dll' -or $leaf -like 'libQt*.dll') {
                $forbidden += $name
            }
        }
        $script:Result.forbiddenEntries = $forbidden
        if ($forbidden.Count -gt 0) {
            Fail-State -Token "ZIP_FORBIDDEN_ENTRIES" -Code 2 -Message ("portable ZIP carries {0} forbidden entr(y/ies) (data residue / Mesa software-GL DLLs / Qt runtime / retired placeholders): {1}" -f (Format-Invariant $forbidden.Count), ($forbidden -join ', '))
        }
    } finally {
        if ($archive) { $archive.Dispose() }
    }

    # ------------------------------------------------------------------
    # 10. Installer PE identity: MZ + PE signature + machine type. NSIS
    #     3.x installer stubs are i386 PEs even when the payload is
    #     x64-only, so i386 (0x014C) or x86-64 (0x8664) are accepted; any
    #     other machine type is a wrong-architecture package. The PAYLOAD
    #     x64 identity is enforced on SHEYTAN-LA.exe by the build job.
    # ------------------------------------------------------------------
    try {
        $stream = [System.IO.File]::OpenRead((Resolve-Path -LiteralPath $Installer).Path)
        try {
            $head = New-Object byte[] 4096
            $read = $stream.Read($head, 0, $head.Length)
            $headLen = $read
        } finally {
            $stream.Dispose()
        }
    } catch {
        Fail-State -Token "INSTALLER_NOT_PE" -Code 2 -Message ("installer is not readable as a PE ('{0}'): {1}" -f $Installer, $_.Exception.Message)
    }
    if ($headLen -lt 2 -or $head[0] -ne 0x4D -or $head[1] -ne 0x5A) {
        Fail-State -Token "INSTALLER_NOT_PE" -Code 2 -Message ("installer is not a PE binary (missing MZ header): '{0}'" -f $Installer)
    }
    if ($headLen -lt 0x40) {
        Fail-State -Token "INSTALLER_NOT_PE" -Code 2 -Message ("installer is too small to carry a PE header: '{0}'" -f $Installer)
    }
    $peOffset = [BitConverter]::ToInt32($head, 0x3C)
    if ($peOffset -le 0 -or ($peOffset + 6) -gt $headLen -or $head[$peOffset] -ne 0x50 -or $head[$peOffset + 1] -ne 0x45) {
        Fail-State -Token "INSTALLER_NOT_PE" -Code 2 -Message ("installer PE signature missing or out of range (peOffset={0}): '{1}'" -f (Format-Invariant $peOffset), $Installer)
    }
    $machine = [BitConverter]::ToUInt16($head, $peOffset + 4)
    $script:Result.installerMachine = ("0x{0:X4}" -f $machine)
    if ($machine -ne 0x014C -and $machine -ne 0x8664) {
        Fail-State -Token "INSTALLER_BAD_ARCH" -Code 2 -Message ("installer PE machine = 0x{0:X4}, want i386 (0x014C, NSIS stub) or x86-64 (0x8664): '{1}'" -f $machine, $Installer)
    }

    # ------------------------------------------------------------------
    # 11. Dependency closure (optional but fail-closed when requested):
    #     the pre-launch audit's structured verdict lines are the contract.
    #     An absent/unreadable/unrecognized report means the closure is
    #     UNVERIFIED — and unverified is never a pass.
    # ------------------------------------------------------------------
    if ($DependencyReport) {
        $verdict = "UNRECOGNIZED"
        if (Test-Path -LiteralPath $DependencyReport -PathType Leaf) {
            $raw = [System.IO.File]::ReadAllText((Resolve-Path -LiteralPath $DependencyReport).Path)
            if ($raw.Contains("smoke dependency audit: MISSING DEPENDENCIES")) {
                $verdict = "FAIL"
            } elseif ($raw.Contains("smoke dependency audit: ERROR")) {
                $verdict = "ERROR"
            } elseif ($raw.Contains("smoke dependency audit: PASS")) {
                $verdict = "PASS"
            }
        } else {
            $verdict = "REPORT_MISSING"
        }
        $script:Result.dependencyVerdict = $verdict
        if ($verdict -ne "PASS") {
            Fail-State -Token "DEPENDENCY_CLOSURE_FAILURE" -Code 2 -Message ("dependency closure is not proven (verdict: {0}; report: '{1}') — MISSING/ERROR/unverifiable audit results are never a pass" -f $verdict, $DependencyReport)
        }
    }

    # ------------------------------------------------------------------
    # 12. Digests (actual on-disk bytes, invariant hex).
    # ------------------------------------------------------------------
    $zipHash = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
    $instHash = (Get-FileHash -LiteralPath $Installer -Algorithm SHA256).Hash.ToLowerInvariant()
    $script:Result.zipSha256 = $zipHash
    $script:Result.installerSha256 = $instHash

    # ------------------------------------------------------------------
    # 13. OK — the ONLY state that may print the success line, after
    #     every assertion succeeded, with the ACTUAL byte counts and
    #     digests.
    # ------------------------------------------------------------------
    $script:Result.token = "OK"
    $script:Result.exitCode = 0
    Write-Output "PACKAGE_GATE_RESULT OK"
    Write-ResultFile
    Write-Host ("package gate: reached (portable ZIP {0} bytes, installer {1} bytes — both distribution packages present and non-empty; sha256 zip={2} installer={3})" -f (Format-Invariant $zipLen), (Format-Invariant $instLen), $zipHash, $instHash)
    exit 0
} catch {
    # The gate could not evaluate — never a silent pass, never exit 0.
    $diag = $_.Exception.Message
    $stack = ($_.ScriptStackTrace -replace "\r?\n", " | ")
    Write-Host "::error::package gate: the gate could not evaluate (internal error): $diag"
    Write-Host ("::error::package gate: stack: {0}" -f $stack)
    Complete-Gate -Token "EVALUATION_ERROR" -Code 3
}
