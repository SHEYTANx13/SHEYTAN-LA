//go:build windows

// cim_windows.go — the ONE batched Windows CIM invocation (v1.2.6).
//
// v1.2.5 spawned ~7 separate PowerShell processes sequentially to collect
// CPU name, core counts, RAM, GPU adapters and disk size — measured
// 5,399 ms on the target machine (each spawn pays ~700-900 ms of
// PowerShell startup). This file collects EVERYTHING in a single
// invocation, emitting a stable KEY=VALUE block Go parses offline.
package sysinfo

import (
	"fmt"
)

// cimScript is the single batched query. Rules:
//   - $ErrorActionPreference keeps partial failures silent per-section
//     (a missing class must not kill the rest);
//   - every fact is emitted as one `KEY=value` line;
//   - repeated lines (GPU=, NPU=) denote multiple devices.
const cimScript = `$ErrorActionPreference = 'SilentlyContinue'
$cpu = Get-CimInstance -ClassName Win32_Processor | Select-Object -First 1
if ($cpu) {
  if ($cpu.Name) { 'CPU_NAME=' + $cpu.Name.Trim() }
  if ($cpu.NumberOfCores) { 'CPU_CORES=' + $cpu.NumberOfCores }
  if ($cpu.NumberOfLogicalProcessors) { 'CPU_LOGICAL=' + $cpu.NumberOfLogicalProcessors }
  if ($cpu.MaxClockSpeed) { 'CPU_MHZ=' + $cpu.MaxClockSpeed }
}
$osq = Get-CimInstance -ClassName Win32_OperatingSystem
if ($osq) {
  if ($osq.TotalVisibleMemorySize) { 'RAM_TOTAL_KB=' + $osq.TotalVisibleMemorySize }
  if ($osq.FreePhysicalMemory) { 'RAM_FREE_KB=' + $osq.FreePhysicalMemory }
}
Get-CimInstance -ClassName Win32_VideoController | ForEach-Object {
  if ($_.Name) { 'GPU=' + $_.Name + '|' + $_.AdapterRAM + '|' + $_.DriverVersion }
}
Get-CimInstance -ClassName Win32_PnPEntity | Where-Object {
  ($_.Name -match 'AI Boost|Neural Processing|NPU') -or ($_.PNPClass -eq 'ND')
} | ForEach-Object {
  if ($_.Name) { 'NPU=' + $_.Name + '|' + $_.Manufacturer + '|' + $_.DriverVersion + '|' + $_.Status }
}
$disk = Get-CimInstance -ClassName Win32_LogicalDisk -Filter ("DeviceID='" + $env:SystemDrive + "'")
if ($disk) {
  if ($disk.Size) { 'DISK_TOTAL=' + $disk.Size }
  if ($disk.FreeSpace) { 'DISK_FREE=' + $disk.FreeSpace }
}`

// cimBatch runs the single batched PowerShell/CIM invocation and parses
// the KEY=VALUE output. Every value is best-effort; a failed or partial
// probe yields the facts that did parse (never an error the caller must
// guess about — missing facts stay zero).
func cimBatch() (cimFacts, error) {
	out, err := probeOutput("powershell", "-NoProfile", "-NonInteractive", "-Command", cimScript)
	if err != nil && len(out) == 0 {
		return cimFacts{}, fmt.Errorf("batched CIM probe: %w", err)
	}

	facts := parseCIMBatch(string(out))

	// A response with zero recognized keys is a failed probe (PowerShell
	// error text, locale issues) — surface it so the caller logs honestly.
	if facts.cpuName == "" && len(facts.gpus) == 0 && facts.ramTotalKB == 0 && !facts.npu.present {
		if err != nil {
			return facts, fmt.Errorf("batched CIM probe produced no facts: %w", err)
		}
		return facts, fmt.Errorf("batched CIM probe produced no facts")
	}

	return facts, nil
}
