<#
.SYNOPSIS
  NSIS 3.13.0 version-gate semantics (v1.11.1, run 43883548198 repair) —
  ONE shared implementation for the workflow gate and its fixture test.

.DESCRIPTION
  The Chocolatey nsis 3.13.0 package installs makensis 3.13, whose
  /VERSION output is "v3.13" — a leading v and no patch component. The
  retired gate predicated on "^3\.13" and therefore rejected that VALID
  output, which stopped the Windows installer job in run 43883548198
  (the package-gate "installer missing while portable ZIP exists"
  message downstream was the consequence of this version assertion, not
  a second proven packaging defect).

  The strict parser below accepts an optional leading v and an optional
  patch component while requiring major 3 and minor 13 — exactly the
  semantic the work order pins:

      ^v?(?<major>\d+)\.(?<minor>\d+)(?:\.(?<patch>\d+))?$

  applied to the TRIMMED output. Anything else fails closed: a nonzero
  process exit, empty/whitespace output, multiline garbage, and every
  non-3.13 version (3.12, v3.12.9, 3.130, 3.14, v4.0, v3.13-preview).

  The function is PURE: it never runs makensis. The workflow supplies
  the real executable output and exit code and asserts the executable
  identity separately; the fixture test (NsisVersion.Tests.ps1) drives
  this function with recorded outputs, so the gate semantics have one
  authority and cannot drift between the workflow and the fixtures.
#>

function Get-NsisVersionVerdict {
    [CmdletBinding()]
    param(
        # The RAW makensis /VERSION output (captured with 2>&1 | Out-String
        # upstream — may carry trailing newlines/CRs; never hand-normalized
        # before this call).
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string]$RawOutput,

        # The process exit code of the exact makensis invocation. A nonzero
        # exit fails closed even when the text looks like a valid version.
        [int]$ExitCode = 0
    )

    # Normalize CRs and trailing/leading whitespace only. Embedded newlines
    # survive normalization and reject the multiline-garbage fixture.
    $normalized = ($RawOutput -replace "`r", "").Trim()

    $accepted = $false
    $major = $null
    $minor = $null
    $patch = $null
    $reason = ""

    if ($ExitCode -ne 0) {
        $reason = "nonzero makensis exit code $ExitCode (the version gate requires exit 0 from the exact pinned executable)"
    }
    elseif ([string]::IsNullOrWhiteSpace($normalized)) {
        $reason = "empty version output (makensis produced nothing to parse)"
    }
    elseif ($normalized -match "`n") {
        $reason = "multiline output is not a version string (first line: '$(($normalized -split "`n")[0])')"
    }
    else {
        # Anchored, strict: optional leading v, required major.minor,
        # optional patch. No prefix/suffix garbage, no loose containment.
        $m = [regex]::Match($normalized, '^v?(?<major>\d+)\.(?<minor>\d+)(?:\.(?<patch>\d+))?$')
        if (-not $m.Success) {
            $reason = "output '$normalized' is not a vMAJOR.MINOR[.PATCH] version string (anchored match failed)"
        }
        else {
            $major = [int]$m.Groups['major'].Value
            $minor = [int]$m.Groups['minor'].Value
            if ($m.Groups['patch'].Success) {
                $patch = [int]$m.Groups['patch'].Value
            }
            if ($major -ne 3 -or $minor -ne 13) {
                $reason = "version $major.$minor is not the pinned 3.13 line (want major 3, minor 13)"
            }
            else {
                $accepted = $true
            }
        }
    }

    [pscustomobject]@{
        Accepted   = $accepted
        Reason     = $reason
        Raw        = $RawOutput
        Normalized = $normalized
        ExitCode   = $ExitCode
        Major      = $major
        Minor      = $minor
        Patch      = $patch
    }
}
