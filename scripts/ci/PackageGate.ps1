<#
.SYNOPSIS
  Fail-closed Windows package gate (ALPHA portable-only) — stable
  machine-readable outcome contract.

  MASTER WORK ORDER §10 (installer retirement): the NSIS installer and
  every other installer format are retired from the supported build
  workflow. The gate validates the PORTABLE ZIP ONLY — there is no
  -Installer parameter anymore, and an installer remnant in the
  distribution directory is itself a violation (INSTALLER_REMNANT),
  so a stale installer artifact can never ship under a portable label.

.DESCRIPTION
  Independently requires the portable ZIP to be present, non-empty and
  structurally valid before the job may report "package gate: reached".

  The stable contract (carried over from run 42928792246) does NOT
  depend on any free-form text, stream routing, terminal formatting,
  locale or host behavior:

    1. ONE machine-readable token line on the SUCCESS stream (capturable
       by any of *, 2>&1, Tee-Object, or a file redirect):

           PACKAGE_GATE_RESULT <TOKEN>

       emitted EXACTLY ONCE per invocation, on every outcome, in every
       state — before any human-readable line.

    2. A structured JSON result file (optional -ResultFile parameter)
       with the affected paths, byte counts, entry inventories,
       installer-remnant inventory, dependency verdict and SHA-256
       digest.

    3. Human-readable `::error::` diagnostics via Write-Host (real CI
       annotations on hosted runs) that name the affected path — the
       fixtures treat these as inert data, never as assertions.

  Truth table (token -> exit code; 0 ONLY for OK):

    ZIP_MISSING                2   the portable ZIP is absent
    ZIP_EMPTY                  2   ZIP present, 0 bytes
    ZIP_CORRUPT                2   ZIP non-empty but not a readable archive
    ZIP_BAD_ROOT               2   archive does not hold exactly one
                                   top-level directory named -ExpectedRoot
    ZIP_MISSING_ENTRIES        2   contracted required entries absent
    ZIP_FORBIDDEN_ENTRIES      2   data/Mesa/Qt/placeholder residue present
    INSTALLER_REMNANT          2   -DistDir contains a retired installer
                                   artifact (*-installer.exe / setup.exe /
                                   *-setup.exe) — master work order §10.2:
                                   no stale installer artifact under a
                                   portable label
    DEPENDENCY_CLOSURE_FAILURE 2   -DependencyReport absent/unreadable/
                                   unrecognized, or its audit verdict is
                                   MISSING/ERROR (closure never assumed)
    STALE_PACKAGE              2   -StalenessReference names a file the
                                   package must not predate: the ZIP was
                                   written BEFORE that reference — a stale
                                   package from a previous run masquerading
                                   as this run's output (run 44457547289
                                   §1.1 regression coverage)
    BLOCKED_BY_UPSTREAM_TEST_FAILURE
                               2   the -UpstreamFailureMarker file exists:
                                   an upstream verification gate FAILED
                                   and the package-producing steps never
                                   ran — the report names the FIRST
                                   failing gate instead of inventing a
                                   second root error. Still fail-closed.
    INVALID_ARGUMENTS          1   required arguments missing/invalid
                                   (deterministic: explicit validation,
                                   never the interactive prompt machinery)
    EVALUATION_ERROR           3   the gate could not evaluate (never 0)
    OK                         0   every assertion succeeded

  The success line "package gate: reached (...)" is emitted ONLY in the
  OK state, AFTER every assertion succeeds, and carries the ACTUAL
  on-disk byte count and SHA-256 digest (never fixture constants).

  Exit codes: 0 = OK; 1 = invalid arguments; 2 = package contract
  violated (named state); 3 = gate could not evaluate. Every failure
  path is non-zero: fail-closed.
#>

[CmdletBinding()]
param(
    [string]$Zip,
    [string]$DistDir,
    [string]$ResultFile,
    [string[]]$RequiredEntries,
    [string]$ExpectedRoot = "SHEYTAN-LA",
    [string]$DependencyReport,
    [string]$UpstreamFailureMarker,
    [string]$ManifestFile,
    [string]$StalenessReference
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

# Retired installer artifact shapes (master work order §10): any of these
# in the distribution directory is a stale installer remnant and fails
# the gate — installers are no longer part of the supported build.
$script:InstallerRemnantPatterns = @("*-installer.exe", "*-setup.exe", "setup.exe")

$script:Result = [ordered]@{
    token             = "EVALUATION_ERROR"
    exitCode          = 3
    zipPath           = $Zip
    distDir           = $DistDir
    zipBytes          = $null
    zipEntryCount     = $null
    missingEntries    = @()
    forbiddenEntries  = @()
    installerRemnants = @()
    dependencyReport  = $DependencyReport
    dependencyVerdict = $null
    upstreamFailure   = $null
    zipSha256         = $null
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
    if (-not $Zip) {
        Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: -Zip is required (got zip='{0}')" -f $Zip)
    }
    if (-not $ExpectedRoot) {
        Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message "invalid arguments: -ExpectedRoot must not be empty"
    }

    # ------------------------------------------------------------------
    # 0a. Manifest contract (run 44457547289 §1.1): -ManifestFile names a
    #     JSON document carrying the required-entry list (and optionally
    #     the expected root). A MALFORMED manifest is an explicit invalid
    #     argument — never silently ignored, never partially honored.
    # ------------------------------------------------------------------
    if ($ManifestFile) {
        if (-not (Test-Path -LiteralPath $ManifestFile -PathType Leaf)) {
            Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: -ManifestFile '{0}' does not exist" -f $ManifestFile)
        }

        $manifest = $null
        try {
            $manifest = Get-Content -Raw -LiteralPath $ManifestFile | ConvertFrom-Json
        } catch {
            Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: -ManifestFile '{0}' is not valid JSON: {1}" -f $ManifestFile, $_.Exception.Message)
        }

        $manifestEntries = $null
        try { $manifestEntries = @($manifest.requiredEntries) } catch { $manifestEntries = $null }

        if (-not $manifestEntries -or $manifestEntries.Count -eq 0) {
            Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: -ManifestFile '{0}' carries no requiredEntries array" -f $ManifestFile)
        }

        foreach ($entry in $manifestEntries) {
            if ([string]::IsNullOrWhiteSpace([string]$entry)) {
                Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: -ManifestFile '{0}' requiredEntries must not contain empty values" -f $ManifestFile)
            }
        }

        $RequiredEntries = @($manifestEntries | ForEach-Object { [string]$_ })

        $manifestRoot = $null
        try { $manifestRoot = [string]$manifest.expectedRoot } catch { $manifestRoot = $null }

        if ($manifestRoot) {
            $ExpectedRoot = $manifestRoot
            $script:Result.expectedRoot = $ExpectedRoot
        }
    }

    foreach ($entry in $RequiredEntries) {
        if ([string]::IsNullOrWhiteSpace($entry)) {
            Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message "invalid arguments: -RequiredEntries must not contain empty values"
        }
    }

    # ------------------------------------------------------------------
    # 0b. Blocked-by-upstream reporting (run 43601915826 §3.5): when an
    #     upstream verification gate failed, the package-producing steps
    #     were skipped and the package is legitimately absent — that is
    #     ONE root cause, not a second package defect. The marker file
    #     (written by the FIRST failing gate) names it; the gate stays
    #     fail-closed (exit 2) but reports the true upstream failure
    #     instead of ZIP_MISSING. A missing marker changes nothing: a
    #     completed packaging path that fails to emit its output still
    #     fails hard with the package truth table.
    # ------------------------------------------------------------------
    if ($UpstreamFailureMarker) {
        if (Test-Path -LiteralPath $UpstreamFailureMarker -PathType Leaf) {
            $gate = "<unreadable marker>"
            try { $gate = ([System.IO.File]::ReadAllText((Resolve-Path -LiteralPath $UpstreamFailureMarker).Path)).Trim() } catch { }
            if (-not $gate) { $gate = "<empty marker>" }
            $script:Result.upstreamFailure = $gate
            Fail-State -Token "BLOCKED_BY_UPSTREAM_TEST_FAILURE" -Code 2 -Message ("packaging was BLOCKED BY UPSTREAM TEST FAILURE — the first failing gate is '{0}' (marker '{1}'); the package was never produced by that failed run, so there is no second package defect to report" -f $gate, $UpstreamFailureMarker)
        }
    }

    # ------------------------------------------------------------------
    # 1. Existence: the ZIP must be a REGULAR file (a directory or a
    #    non-leaf reparse target is not a distribution package).
    # ------------------------------------------------------------------
    $zipOk = $false
    try {
        $zipOk = [bool](Test-Path -LiteralPath $Zip -PathType Leaf)
    } catch {
        Fail-State -Token "ZIP_MISSING" -Code 2 -Message ("portable ZIP path is not evaluable '{0}': {1}" -f $Zip, $_.Exception.Message)
    }
    if (-not $zipOk) {
        Fail-State -Token "ZIP_MISSING" -Code 2 -Message ("portable ZIP missing at '{0}' — the upstream build/package steps did not run or failed" -f $Zip)
    }

    # ------------------------------------------------------------------
    # 1a. Stale-package contract (run 44457547289 §1.1): when a
    #     staleness reference is supplied, the package must NOT predate
    #     it — a package written before the reference file is residue
    #     from a previous run masquerading as this run's output.
    # ------------------------------------------------------------------
    if ($StalenessReference) {
        if (-not (Test-Path -LiteralPath $StalenessReference -PathType Leaf)) {
            Fail-State -Token "INVALID_ARGUMENTS" -Code 1 -Message ("invalid arguments: -StalenessReference '{0}' does not exist" -f $StalenessReference)
        }

        $zipWritten = (Get-Item -LiteralPath $Zip -Force).LastWriteTimeUtc
        $refWritten = (Get-Item -LiteralPath $StalenessReference -Force).LastWriteTimeUtc

        if ($zipWritten -lt $refWritten) {
            Fail-State -Token "STALE_PACKAGE" -Code 2 -Message ("portable ZIP '{0}' predates the staleness reference '{1}' (zip written {2:o}, reference {3:o}) — a stale package from a previous run is not this run's output" -f $Zip, $StalenessReference, $zipWritten, $refWritten)
        }
    }

    # ------------------------------------------------------------------
    # 1b. Installer remnants (master work order §10.2): the distribution
    #     directory must not contain any retired installer artifact.
    # ------------------------------------------------------------------
    if ($DistDir) {
        $remnants = @()
        if (Test-Path -LiteralPath $DistDir -PathType Container) {
            foreach ($pattern in $script:InstallerRemnantPatterns) {
                $hits = @(Get-ChildItem -LiteralPath $DistDir -Filter $pattern -File -ErrorAction SilentlyContinue | ForEach-Object { $_.Name })
                $remnants += $hits
            }
            $remnants = @($remnants | Sort-Object -Unique)
        }
        $script:Result.installerRemnants = $remnants
        if ($remnants.Count -gt 0) {
            Fail-State -Token "INSTALLER_REMNANT" -Code 2 -Message ("distribution directory '{0}' contains {1} retired installer artifact(s) ({2}) — installer production is retired (master work order §10); remove the stale artifact instead of uploading it" -f $DistDir, (Format-Invariant $remnants.Count), ($remnants -join ', '))
        }
    }

    # ------------------------------------------------------------------
    # 2. Non-empty: the ZIP must carry actual bytes.
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

    # ------------------------------------------------------------------
    # 3. ZIP integrity: the archive must actually open (a non-empty
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

        # 4. Root layout: exactly ONE top-level directory, named -ExpectedRoot.
        $topSegments = @($entryNames |
            ForEach-Object { ($_ -split '/')[0] } |
            Where-Object { $_ -and $_.Trim() -ne "" } |
            Sort-Object -Unique)
        $rootOk = ($topSegments.Count -eq 1) -and
                  ($topSegments[0] -eq $ExpectedRoot)
        if (-not $rootOk) {
            Fail-State -Token "ZIP_BAD_ROOT" -Code 2 -Message ("portable ZIP must contain exactly one top-level directory named '{0}' (found: {1}) — extract/validate the package layout" -f $ExpectedRoot, ($(if ($topSegments.Count -eq 0) { "<none>" } else { $topSegments -join ', ' })))
        }

        # 5. Required entries (exact path, case-insensitive, ordinal-safe).
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

        # 6. Forbidden entries: data residue, Mesa software-GL DLLs, Qt
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
    # 7. Dependency closure (optional but fail-closed when requested):
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
    # 8. Digest (actual on-disk bytes, invariant hex).
    # ------------------------------------------------------------------
    $zipHash = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
    $script:Result.zipSha256 = $zipHash

    # ------------------------------------------------------------------
    # 9. OK — the ONLY state that may print the success line, after
    #    every assertion succeeded, with the ACTUAL byte count and
    #    digest.
    # ------------------------------------------------------------------
    $script:Result.token = "OK"
    $script:Result.exitCode = 0
    Write-Output "PACKAGE_GATE_RESULT OK"
    Write-ResultFile
    Write-Host ("package gate: reached (portable ZIP {0} bytes — the ONE supported package; installers are retired per master work order §10; sha256 zip={1})" -f (Format-Invariant $zipLen), $zipHash)
    exit 0
} catch {
    # The gate could not evaluate — never a silent pass, never exit 0.
    $diag = $_.Exception.Message
    $stack = ($_.ScriptStackTrace -replace "\r?\n", " | ")
    Write-Host "::error::package gate: the gate could not evaluate (internal error): $diag"
    Write-Host ("::error::package gate: stack: {0}" -f $stack)
    Complete-Gate -Token "EVALUATION_ERROR" -Code 3
}
