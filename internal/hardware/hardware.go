// Package hardware is SHEYTAN's unified hardware intelligence layer
// (v1.2.0). It consolidates the hardware facts the product reasons about —
// CPU, memory, storage, GPU/VRAM, backend capability — from the EXISTING
// measured sources (internal/sysinfo probes, engine binary inspection) and
// adds nothing unmeasured.
//
// Design rules:
//
//   - sysinfo.Probe() remains the single measurement point; this package
//     never re-probes or duplicates OS-specific code.
//   - Unavailable values stay zero/empty — the recommendation engine and
//     the Environment Centre render them as unknown, never as guesses.
//   - The pool calculation (available RAM + VRAM) mirrors
//     llm.AssessContextResource so every consumer agrees on one number.
package hardware

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// CPU summarizes processor capacity (measured).
type CPU struct {
	Name          string `json:"name"`
	PhysicalCores int    `json:"physicalCores,omitempty"`
	LogicalCores  int    `json:"logicalCores,omitempty"`
	FrequencyMHz  int    `json:"frequencyMHz,omitempty"`
}

// RAM summarizes system memory (measured). AvailableBytes is 0 when the
// OS probe could not measure it — consumers must treat 0 as unknown.
type RAM struct {
	TotalBytes     int64 `json:"totalBytes"`
	FreeBytes      int64 `json:"freeBytes,omitempty"`
	AvailableBytes int64 `json:"availableBytes,omitempty"`
}

// Storage summarizes the data-disk (measured).
type Storage struct {
	TotalBytes int64  `json:"totalBytes,omitempty"`
	FreeBytes  int64  `json:"freeBytes,omitempty"`
	Path       string `json:"path,omitempty"`
}

// GPU describes one detected graphics adapter (measured). VRAMBytes stays
// 0 when the driver refuses to report it — notably Windows CIM
// AdapterRAM saturates near 4 GB, so the recommendation engine treats
// small values with documented caution.
type GPU struct {
	Vendor    string `json:"vendor,omitempty"`
	Name      string `json:"name,omitempty"`
	VRAMBytes int64  `json:"vramBytes,omitempty"`
	DriverVer string `json:"driverVer,omitempty"`
}

// Backend summarizes the local inference backend's capability evidence.
type Backend struct {
	// EngineBinary is the resolved llama-server path ("" when not yet
	// provisioned — first run downloads it).
	EngineBinary string `json:"engineBinary,omitempty"`
	// Vulkan reports a Vulkan backend sitting beside the engine binary.
	Vulkan bool `json:"vulkan"`
	// NativeEnabled reports the user's native-engine selection.
	NativeEnabled bool `json:"nativeEnabled"`
	// EngineTag is the installed llama.cpp release tag, when known.
	EngineTag string `json:"engineTag,omitempty"`
}

// Profile is the unified hardware snapshot served to /api/environment,
// /api/health and the recommendation engine.
type Profile struct {
	OS       string  `json:"os"`
	Arch     string  `json:"arch"`
	Hostname string  `json:"hostname,omitempty"`
	CPU      CPU     `json:"cpu"`
	RAM      RAM     `json:"ram"`
	Storage  Storage `json:"storage"`
	// GPUs MUST marshal as [] (never null): a nil Go slice marshals to
	// JSON null, and the System Centre frontend historically read
	// gpus.length off it - the v1.2.2 black-screen crash. Collect
	// guarantees non-nil; JSON consumers still defend in depth.
	GPUs        []GPU   `json:"gpus"`
	Backend     Backend `json:"backend"`
	CollectedAt string  `json:"collectedAt"`

	// NPU (v1.2.6): the measured neural accelerator identity when a PnP
	// probe found one (Intel AI Boost etc.). Presence NEVER implies a usable
	// inference backend — that verdict belongs to the accelerator package
	// and requires runtime capability evidence.
	NPU *sysinfo.NPUInfo `json:"npu,omitempty"`

	// DeepReady (v1.2.6): whether GPU/NPU/driver facts are measured yet
	// (false = this snapshot carries fast facts only — the UI renders the
	// device card progressively and fills it when the deep probe lands).
	DeepReady bool `json:"deepReady"`
}

// updaterEngineTag reads the installed llama.cpp tag through the existing
// updater state file (never guesses from the binary name).
func updaterEngineTag(cfg *config.Config) string {
	return updater.InstalledEngineTag(cfg)
}

// engineBinaryName mirrors llm's default engine file name without
// importing the server type (the llm package already depends on this
// file's package through the API layer only — no cycle: hardware imports
// llm, and llm never imports hardware).
func engineBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

// Snapshot returns the best currently-available profile WITHOUT ever
// blocking on a deep OS probe (v1.2.6): fast facts always; GPU/NPU/driver
// facts when the background deep probe has completed. Interactive
// endpoints (/api/environment, /api/health, /api/sysinfo) serve THIS —
// the UI must never wait for CIM.
func Snapshot(cfg *config.Config) Profile {
	return collectFrom(sysinfo.ProbeFast(), cfg)
}

// WarmDeep starts the deep hardware probe in the background (idempotent,
// single-flight via sysinfo's sync.Once). Call it at startup so the deep
// facts (GPU/NPU/driver — ONE batched CIM invocation on Windows) are
// ready before any interactive surface needs them.
func WarmDeep() {
	go func() {
		_ = sysinfo.Probe()
	}()
}

// Collect assembles the FULL profile (fast + deep, blocking on the deep
// probe the FIRST time only). Non-interactive owners (engine launch GPU
// decisions, recommendations) call THIS; interactive surfaces call
// Snapshot.
func Collect(cfg *config.Config) Profile {
	return collectFrom(sysinfo.Probe(), cfg)
}

func collectFrom(si *sysinfo.SysInfo, cfg *config.Config) Profile {
	p := Profile{
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		// v1.2.2: non-nil from the start - a machine whose GPU probe
		// returns nothing (CPU-only, or a WMI/PowerShell failure under
		// load) marshals as "gpus": [] instead of null.
		GPUs:      []GPU{},
		DeepReady: si != nil && !si.DeepProbedAt.IsZero(),
	}

	if si != nil {
		p.OS = si.OS
		p.Arch = si.Arch
		p.Hostname = si.Hostname
		p.CPU = CPU{
			Name:          si.CPU.Name,
			PhysicalCores: si.CPU.PhysicalCores,
			LogicalCores:  si.CPU.LogicalCores,
			FrequencyMHz:  si.CPU.FrequencyMHz,
		}
		p.RAM = RAM{
			TotalBytes:     int64(si.RAM.TotalBytes),
			FreeBytes:      int64(si.RAM.FreeBytes),
			AvailableBytes: int64(si.RAM.Available),
		}
		p.Storage = Storage{
			TotalBytes: int64(si.Disk.TotalBytes),
			FreeBytes:  int64(si.Disk.FreeBytes),
			Path:       si.Disk.Path,
		}
		for _, g := range si.GPU {
			p.GPUs = append(p.GPUs, GPU{
				Vendor:    g.Vendor,
				Name:      g.Name,
				VRAMBytes: int64(g.VRAMBytes),
				DriverVer: g.DriverVer,
			})
		}

		if si.NPU != nil {
			p.NPU = si.NPU
		}
	}

	if cfg != nil {
		p.Backend = Backend{
			Vulkan:        llm.VulkanAvailable(cfg),
			NativeEnabled: cfg.NativeBackendEnabled(),
			EngineTag:     updaterEngineTag(cfg),
		}

		bin := cfg.LlamaBinPath
		if bin == "" {
			bin = filepath.Join(cfg.DataDir, "bin", engineBinaryName())
		}
		if _, err := os.Stat(bin); err == nil {
			p.Backend.EngineBinary = bin
		}
	}

	// Stale-while-revalidate note (v1.2.6): the sysinfo layer keeps deep
	// facts once measured (its guarded snapshot is never unset), so a later
	// probe failure can never blank the interactive view — Snapshot serves
	// the last measured facts, marked DeepReady.
	return p
}

// PrimaryGPU returns the first discrete GPU, or nil.
func (p Profile) PrimaryGPU() *GPU {
	if len(p.GPUs) == 0 {
		return nil
	}
	return &p.GPUs[0]
}

// VRAMBytes returns the measured VRAM of the primary GPU (0 = unknown).
func (p Profile) VRAMBytes() int64 {
	if g := p.PrimaryGPU(); g != nil {
		return g.VRAMBytes
	}
	return 0
}

// HasGPU reports whether any graphics adapter was measured.
func (p Profile) HasGPU() bool {
	return len(p.GPUs) > 0
}

// PoolBytes is the memory pool recommendations budget against: available
// RAM when measured (total otherwise) plus VRAM when known. It mirrors
// llm.AssessContextResource's pool so every layer agrees.
func (p Profile) PoolBytes() int64 {
	pool := p.RAM.TotalBytes
	if p.RAM.AvailableBytes > 0 {
		pool = p.RAM.AvailableBytes
	} else if p.RAM.FreeBytes > 0 {
		pool = p.RAM.FreeBytes
	}
	if vram := p.VRAMBytes(); vram > 0 {
		pool += vram
	}
	return pool
}
