// cim_parse.go — the platform-independent parser for the batched CIM
// KEY=VALUE block (v1.2.6). Pure function: unit-testable on every OS with
// fixture outputs, no Windows required.
package sysinfo

import (
	"strconv"
	"strings"
)

// cimFacts is the parsed result of the batched CIM invocation. Defined
// here (platform-independent) so the parser is unit-testable everywhere;
// the Windows-only cimBatch() runs the actual probe.
type cimFacts struct {
	cpuName    string
	cpuCores   int
	cpuLogical int
	cpuMHz     int

	ramTotalKB uint64
	ramFreeKB  uint64

	gpus []GPUInfo

	npu struct {
		present bool
		info    NPUInfo
	}

	diskTotal uint64
	diskFree  uint64
}

// parseCIMBatch parses the batched probe output into cimFacts. Unknown
// keys are ignored (forward compatibility); malformed values fall back
// to zero — never an error, the caller logs what actually arrived.
func parseCIMBatch(out string) cimFacts {
	var facts cimFacts

	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "CPU_NAME":
			facts.cpuName = strings.Trim(value, "\"")

		case "CPU_CORES":
			facts.cpuCores = atoiOrZero(value)

		case "CPU_LOGICAL":
			facts.cpuLogical = atoiOrZero(value)

		case "CPU_MHZ":
			facts.cpuMHz = atoiOrZero(value)

		case "RAM_TOTAL_KB":
			facts.ramTotalKB = parseUintOrZero(value)

		case "RAM_FREE_KB":
			facts.ramFreeKB = parseUintOrZero(value)

		case "DISK_TOTAL":
			facts.diskTotal = parseUintOrZero(value)

		case "DISK_FREE":
			facts.diskFree = parseUintOrZero(value)

		case "GPU":
			// GPU=name|adapterRAM|driverVersion
			parts := strings.SplitN(value, "|", 3)
			if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
				continue
			}

			name := strings.TrimSpace(parts[0])

			// Skip non-hardware display adapters (remote-session devices).
			lower := strings.ToLower(name)
			if strings.Contains(lower, "basic display") || strings.Contains(lower, "remote display") ||
				strings.Contains(lower, "paravirtual") || strings.Contains(lower, "virtual display") {
				continue
			}

			g := GPUInfo{Name: name}

			if len(parts) > 1 {
				g.VRAMBytes = parseUintOrZero(strings.TrimSpace(parts[1]))
			}

			if len(parts) > 2 {
				g.DriverVer = strings.TrimSpace(parts[2])
			}

			switch {
			case strings.Contains(lower, "nvidia") || strings.Contains(lower, "geforce") || strings.Contains(lower, "quadro"):
				g.Vendor = "NVIDIA"
			case strings.Contains(lower, "amd") || strings.Contains(lower, "radeon"):
				g.Vendor = "AMD"
			case strings.Contains(lower, "intel") || strings.Contains(lower, "arc") || strings.Contains(lower, "iris"):
				g.Vendor = "Intel"
			default:
				g.Vendor = "Unknown"
			}

			facts.gpus = append(facts.gpus, g)

		case "NPU":
			// NPU=name|manufacturer|driverVersion|status
			parts := strings.SplitN(value, "|", 4)
			if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
				continue
			}

			facts.npu.present = true
			facts.npu.info = NPUInfo{
				Name:       strings.TrimSpace(parts[0]),
				DetectedBy: "win32-pnp-cim",
			}

			if len(parts) > 1 {
				facts.npu.info.Vendor = strings.TrimSpace(parts[1])
			}

			if len(parts) > 2 {
				facts.npu.info.DriverVer = strings.TrimSpace(parts[2])
			}

			if len(parts) > 3 {
				facts.npu.info.Status = strings.TrimSpace(parts[3])
			}
		}
	}

	return facts
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func parseUintOrZero(s string) uint64 {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
