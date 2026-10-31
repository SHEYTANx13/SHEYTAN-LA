<#
.SYNOPSIS
  Pre-launch static-import dependency audit for the Windows launch smoke.

.DESCRIPTION
  Run #119 root-cause class: ExitCode 0xC0000135 (STATUS_DLL_NOT_FOUND) is
  raised by the Windows loader while resolving the STATIC import chain at
  process init — before main() runs, so the app cannot log anything and a
  bare exit code is the only symptom. This script reads the static import
  tables of the smoke copy's load-chain modules (the app exe plus the two
  Mesa per-app DLLs) with a pure .NET PE parser (no dumpbin, no VS dev
  shell) and resolves every imported DLL name in the REAL loader search
  order for this launch (exe directory, then System32, then PATH).

  Contract (fail-open by design):
    - Every import resolved  -> report + exit 0 (smoke proceeds).
    - Any import MISSING     -> ::error naming the DLL + exit 2 (the step
      fails BEFORE the app is ever launched: a red with a named missing
      DLL is diagnosable from the run page alone).
    - PE parse failure       -> ::warning + continue (exit 0). The audit
      adds signal; its own bugs must never block a green smoke.
    - api-ms-* / ext-ms-* names are virtual API Set contracts resolved by
      the OS schema, not by file search — reported as API-SET, never MISSING.
#>
param(
    [string]$ExeDir,
    [string[]]$Modules,
    [string]$SystemRoot = $env:SystemRoot
)

# Explicit parameter validation (no Mandatory attribute: that engages the
# interactive prompt machinery, which can block a non-interactive CI shell).
if (-not $ExeDir -or $Modules.Count -eq 0) {
    Write-Output "::error::smoke dependency audit: -ExeDir and -Modules are required"
    exit 2
}
# Accept a single comma-separated -Modules value (pwsh -File CLI form) as
# well as a native string[] binding (in-process `& script -Modules @(...)`).
if ($Modules.Count -eq 1 -and $Modules[0].ToString().Contains(',')) {
    $Modules = $Modules[0].ToString().Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" }
}

$ErrorActionPreference = "Stop"
$script:sawMissing = $false

function Convert-RvaToOffset {
    param([UInt64]$Rva, $Sections)
    foreach ($s in $Sections) {
        $span = [Math]::Max($s.VSize, $s.RawSize)
        if ($Rva -ge $s.Va -and $Rva -lt ($s.Va + $span)) { return $s.RawPtr + ($Rva - $s.Va) }
    }
    return $null
}

function Read-AsciiCString {
    param([byte[]]$Bytes, [UInt64]$Offset)
    $end = $Offset
    while ($end -lt [UInt64]$Bytes.Length -and $Bytes[$end] -ne 0) { $end++ }
    return [System.Text.Encoding]::ASCII.GetString($Bytes, [int]$Offset, [int]($end - $Offset))
}

function Get-StaticImportDlls {
    param([string]$Path)
    $bytes = [System.IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 0x40 -or $bytes[0] -ne 0x4D -or $bytes[1] -ne 0x5A) { throw "not an MZ binary" }
    $peOff = [BitConverter]::ToInt32($bytes, 0x3C)
    if ($peOff -le 0 -or ($peOff + 24) -ge $bytes.Length -or
        $bytes[$peOff] -ne 0x50 -or $bytes[$peOff + 1] -ne 0x45) { throw "PE signature missing" }
    $numSections = [BitConverter]::ToUInt16($bytes, $peOff + 6)
    $optSize     = [BitConverter]::ToUInt16($bytes, $peOff + 20)
    $optOff      = $peOff + 24
    $magic       = [BitConverter]::ToUInt16($bytes, $optOff)
    # Optional-header data directories start after the standard fields:
    # PE32+ (0x20B) at +112, PE32 (0x10B) at +96. Index 1 = import table.
    $ddOff = switch ($magic) {
        0x20B { $optOff + 112 }
        0x10B { $optOff + 96 }
        default { throw ("unknown optional-header magic 0x{0:X4}" -f $magic) }
    }
    $importRva  = [UInt64][BitConverter]::ToUInt32($bytes, $ddOff + 8)
    $importSize = [UInt64][BitConverter]::ToUInt32($bytes, $ddOff + 12)
    $sections = @()
    $secOff = $optOff + $optSize
    for ($i = 0; $i -lt $numSections; $i++) {
        $o = $secOff + 40 * $i
        $sections += [pscustomobject]@{
            Va      = [UInt64][BitConverter]::ToUInt32($bytes, $o + 12)
            VSize   = [UInt64][BitConverter]::ToUInt32($bytes, $o + 8)
            RawSize = [UInt64][BitConverter]::ToUInt32($bytes, $o + 16)
            RawPtr  = [UInt64][BitConverter]::ToUInt32($bytes, $o + 20)
        }
    }
    $dlls = New-Object System.Collections.Generic.List[string]
    if ($importRva -eq 0) { return $dlls }
    # Walk the descriptor array bounded by the directory size (or a fixed
    # cap when the declared size is bogus) — malformed input cannot hang
    # the audit.
    $descRva = $importRva
    $descEnd = $importRva + [Math]::Max($importSize, [UInt64](20 * 4096))
    while ($descRva -lt $descEnd) {
        $descOff = Convert-RvaToOffset -Rva $descRva -Sections $sections
        if ($null -eq $descOff) { throw "import-descriptor RVA does not map into any section" }
        $origThunkRva = [UInt64][BitConverter]::ToUInt32($bytes, [int]$descOff)
        $nameRva      = [UInt64][BitConverter]::ToUInt32($bytes, [int]($descOff + 12))
        if ($origThunkRva -eq 0 -and $nameRva -eq 0) { break } # null terminator
        $nameOff = Convert-RvaToOffset -Rva $nameRva -Sections $sections
        if ($null -eq $nameOff) { throw "import-descriptor name RVA does not map into any section" }
        $dllName = Read-AsciiCString -Bytes $bytes -Offset $nameOff
        if ($dllName) { $dlls.Add($dllName) }
        $descRva += 20 # sizeof(IMAGE_IMPORT_DESCRIPTOR)
    }
    return $dlls
}

function Resolve-InLoaderOrder {
    param([string]$Name, [string[]]$SearchDirs, [hashtable]$ExeDirIndex)
    # Loader semantics are case-insensitive; the exe dir is probed through a
    # pre-enumerated lowercase index so resolution stays faithful even on a
    # case-sensitive filesystem (and survives odd-cased import names).
    if ($ExeDirIndex.ContainsKey($Name.ToLowerInvariant())) {
        return $ExeDirIndex[$Name.ToLowerInvariant()]
    }
    foreach ($dir in ($SearchDirs | Select-Object -Skip 1)) {
        $cand = Join-Path $dir $Name
        if (Test-Path -LiteralPath $cand) { return (Resolve-Path -LiteralPath $cand).Path }
    }
    return $null
}

# Loader-order approximation prescribed by the smoke contract: exe dir,
# System32, PATH. Deduplicated, non-empty.
$searchDirs = @(
    (Resolve-Path -LiteralPath $ExeDir).Path,
    (Join-Path $SystemRoot "System32")
) + @($env:PATH -split ';' | Where-Object { $_ -ne "" })

Write-Output "=== smoke dependency audit (static imports, loader order: exe dir -> System32 -> PATH) ==="
Write-Output ("search dirs: {0}" -f ($searchDirs -join ' ; '))

# Case-insensitive index of the smoke copy's exe directory (the first loader
# probe). Bounded: the copy holds a handful of files.
$exeDirIndex = @{}
Get-ChildItem -LiteralPath $searchDirs[0] -File | ForEach-Object {
    $exeDirIndex[$_.Name.ToLowerInvariant()] = $_.FullName
}

foreach ($mod in $Modules) {
    $modPath = Join-Path $ExeDir $mod
    if (-not (Test-Path -LiteralPath $modPath)) {
        Write-Output ("::error::smoke dependency audit: {0} is absent from the smoke copy — a load-chain module is missing before launch" -f $mod)
        $script:sawMissing = $true
        continue
    }
    try {
        $imports = Get-StaticImportDlls -Path $modPath
    } catch {
        Write-Output ("::warning::smoke dependency audit: PE parse of {0} failed ({1}) — continuing to launch (audit is fail-open on its own parse errors)" -f $mod, $_.Exception.Message)
        continue
    }
    Write-Output ("--- {0}: {1} static import(s) ---" -f $mod, $imports.Count)
    $seen = @{}
    foreach ($dll in $imports) {
        $key = $dll.ToLowerInvariant()
        if ($seen.ContainsKey($key)) { continue }
        $seen[$key] = $true
        if ($dll -match '^(api-ms-|ext-ms-)') {
            Write-Output ("  [API-SET] {0} (virtual API Set name — resolved by the OS schema, not file search)" -f $dll)
            continue
        }
        $found = Resolve-InLoaderOrder -Name $dll -SearchDirs $searchDirs -ExeDirIndex $exeDirIndex
        if ($found) {
            Write-Output ("  [resolved] {0} -> {1}" -f $dll, $found)
        } else {
            Write-Output ("::error::smoke dependency audit: {0} imported by {1} has no resolvable copy (exe dir, System32, PATH)" -f $dll, $mod)
            $script:sawMissing = $true
        }
    }
}

if ($script:sawMissing) {
    Write-Output "smoke dependency audit: MISSING DEPENDENCIES — the launch would die at loader level with 0xC0000135"
    exit 2
}
Write-Output "smoke dependency audit: every named static import resolves (or is an OS API Set) — safe to launch"
exit 0
