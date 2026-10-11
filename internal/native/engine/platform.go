package engine

// platform.go — hardware capability abstraction (v1.1.5 Phase 1).
//
// HardwareResult mirrors what the C++ engine actually detects (its own
// view: CPU, RAM, architecture). MergeHardware combines that native view
// with the Go-side sysinfo probe (which owns GPU/VRAM detection today) into
// the platform-neutral llm.HardwareInfo profile.
//
// RULES:
//   - only real detected values are filled in;
//   - no vendor assumptions are hardcoded anywhere;
//   - fields without a detector (NPU/AI accelerators, shared-memory GPUs)
//     remain empty until a probe exists — the profile is *capable* of
//     representing them, it does not invent them.

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// HardwareResult is the JSON shape of the C++ "hwinfo" op result.
// Values the native engine cannot detect are zero/empty — never guessed.
type HardwareResult struct {
	// Architecture as seen by the native binary (x86_64, aarch64, ...).
	Architecture string `json:"architecture"`

	CPU struct {
		Name          string `json:"name,omitempty"`
		PhysicalCores int    `json:"physicalCores,omitempty"`
		LogicalCores  int    `json:"logicalCores,omitempty"`
		FrequencyMHz  int    `json:"frequencyMHz,omitempty"`
	} `json:"cpu"`

	RAM struct {
		TotalBytes     uint64 `json:"totalBytes,omitempty"`
		AvailableBytes uint64 `json:"availableBytes,omitempty"`
	} `json:"ram"`

	// GPUs as detected by the native engine (empty in Phase 1 — the C++
	// skeleton does not enumerate graphics devices; the sysinfo merge
	// supplies real GPU facts).
	GPUs []llm.GPUHardware `json:"gpus,omitempty"`

	// Accelerators (NPU/AI accelerator) as detected by the native engine
	// (empty until a detector exists).
	Accelerators []llm.AcceleratorHardware `json:"accelerators,omitempty"`
}

// MergeHardware builds the unified hardware profile from the native
// probe plus the cached sysinfo probe. Sources are recorded in DetectedBy
// so diagnostics can tell exactly where each fact came from.
func MergeHardware(native HardwareResult) llm.HardwareInfo {
	hw := llm.HardwareInfo{
		Backend:      "native",
		Architecture: native.Architecture,
		CPU: llm.CPUHardware{
			Name:          native.CPU.Name,
			PhysicalCores: native.CPU.PhysicalCores,
			LogicalCores:  native.CPU.LogicalCores,
			FrequencyMHz:  native.CPU.FrequencyMHz,
		},
		RAM: llm.RAMHardware{
			TotalBytes:     native.RAM.TotalBytes,
			AvailableBytes: native.RAM.AvailableBytes,
		},
	}

	if hw.Architecture != "" || hw.CPU.LogicalCores > 0 || hw.RAM.TotalBytes > 0 {
		hw.DetectedBy = append(hw.DetectedBy, "shtn-engine-host")
	}

	// The C++ host's own GPU/accelerator results (Phase 1: none) would be
	// appended here.
	hw.GPUs = append(hw.GPUs, native.GPUs...)
	hw.Accelerators = append(hw.Accelerators, native.Accelerators...)

	// sysinfo owns GPU/VRAM detection today; merge its real values.
	si := sysinfo.Probe()
	if si != nil {
		hw.OS = si.OS

		// Fill CPU gaps (e.g. model name on platforms where the C++ side
		// does not read it) — never overwrite measured native values.
		if hw.CPU.Name == "" && si.CPU.Name != "" && si.CPU.Name != "Unknown" {
			hw.CPU.Name = si.CPU.Name
		}

		if hw.RAM.AvailableBytes == 0 {
			hw.RAM.AvailableBytes = si.RAM.Available
		}

		for _, gpu := range si.GPU {
			hw.GPUs = append(hw.GPUs, llm.GPUHardware{
				Vendor:        gpu.Vendor,
				Name:          gpu.Name,
				VRAMBytes:     gpu.VRAMBytes,
				DriverVersion: gpu.DriverVer,
			})
		}

		hw.DetectedBy = append(hw.DetectedBy, "sysinfo")
	}

	return hw
}
