// Package sysinfo probes the host machine for CPU, RAM, GPU, VRAM, OS,
// accelerators (GPU/NPU) and recommends llama.cpp runtime knobs.
//
// v1.2.6 probe architecture (fixes the measured 5,399 ms Windows probe):
//
//	FAST SNAPSHOT (never blocks, never spawns a process on Windows):
//	    OS, arch, hostname, CPU counts (runtime), RAM (in-process Win32 API),
//	    disk (in-process Win32 API). Serves the UI immediately.
//
//	DEEP PROBE (background, ONE batched PowerShell/CIM invocation):
//	    CPU model name, frequency, GPU adapters + VRAM + driver, NPU
//	    (Intel AI Boost / Neural devices), WSL2, Docker. Bounded by
//	    probeTimeout per source; every source records its own duration and
//	    success/failure in the log.
//
// The v1.2.5 implementation spawned ~7 SEPARATE PowerShell processes
// sequentially (~700-900 ms each ≈ the measured 5.4 s). All CIM facts now
// ride ONE invocation; the interactive path never waits for them at all.
package sysinfo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

var _ = proc.Command // used below on windows/darwin probes

// probeTimeout is the HARD per-call bound for one external probe command.
// A hung PowerShell/CIM/wmic call used to block the probe forever — and
// because Probe() is a sync.Once, every later caller (engine start GPU
// checks, /api/environment, /api/health) would block behind it too. Every
// probe is bounded; a timed-out probe yields "unknown" facts, never a hang.
const probeTimeout = 8 * time.Second

// probeOutput runs one external probe with the bounded timeout.
func probeOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := proc.CommandContext(ctx, name, args...)

	return cmd.Output()
}

// SysInfo is the full hardware/software snapshot of the host.
type SysInfo struct {
	OS       string    `json:"os"`
	Arch     string    `json:"arch"`
	Hostname string    `json:"hostname"`
	CPU      CPUInfo   `json:"cpu"`
	RAM      RAMInfo   `json:"ram"`
	Disk     DiskInfo  `json:"disk"`
	GPU      []GPUInfo `json:"gpus"`
	NPU      *NPUInfo  `json:"npu,omitempty"`
	WSL2     bool      `json:"wsl2"`
	Docker   bool      `json:"docker"`

	// OSBuild (v1.2.6 continuation) is the MEASURED OS build number
	// (Windows: the RtlGetVersion build, e.g. 26200; 0 = not measured).
	// The build number is the AUTHORITATIVE identity — marketing labels
	// are derived from it (see OSDisplay), never the reverse. A stale
	// manually-labelled "Windows 10" must never override build 26200
	// (a Windows 11 25H2 build).
	OSBuild int `json:"osBuild,omitempty"`

	// OSDisplay (v1.2.6 continuation) is the honest human-readable OS
	// label DERIVED from the measured build (e.g. "Windows 11 25H2
	// (build 26200)"); "unknown" when the build was not measured.
	OSDisplay string `json:"osDisplay,omitempty"`

	Recommended Recommended `json:"recommended"`

	// DeepProbedAt (v1.2.6) is when the deep facts (CPU name, GPU, NPU,
	// driver) were actually measured — empty when only fast facts exist.
	DeepProbedAt time.Time `json:"deepProbedAt,omitempty"`

	// DeepProbeMs (v1.2.6) is the measured duration of the deep probe
	// (0 when the deep probe never ran — a fast snapshot only).
	DeepProbeMs int64 `json:"deepProbeMs,omitempty"`
}

type CPUInfo struct {
	Name          string `json:"name"`
	PhysicalCores int    `json:"physicalCores"`
	LogicalCores  int    `json:"logicalCores"`
	FrequencyMHz  int    `json:"frequencyMHz"`
}

type RAMInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	Available  uint64 `json:"availableBytes"`
}

type DiskInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	Path       string `json:"path"`
}

type GPUInfo struct {
	Vendor    string `json:"vendor"`
	Name      string `json:"name"`
	VRAMBytes uint64 `json:"vramBytes"`
	DriverVer string `json:"driverVersion"`
}

// NPUInfo (v1.2.6) describes a measured neural accelerator (Intel AI
// Boost or a generic NPU PnP device). Detection is presence/identity
// evidence ONLY — it never implies any inference backend can use it
// (that decision belongs to the accelerator package, from measured
// capability, never from hardware presence alone).
type NPUInfo struct {
	Vendor     string `json:"vendor"`
	Name       string `json:"name"`
	DriverVer  string `json:"driverVersion,omitempty"`
	Status     string `json:"status,omitempty"` // PnP device status, e.g. "OK"
	DetectedBy string `json:"detectedBy"`       // probe source identity
}

type Recommended struct {
	NumThread int      `json:"numThread"`
	NumGPU    int      `json:"numGPU"`
	NumCtx    int      `json:"numCtx"`
	NumBatch  int      `json:"numBatch"`
	MaxTokens int      `json:"maxTokens"`
	CanRunCPU bool     `json:"canRunCPU"`
	CanRunGPU bool     `json:"canRunGPU"`
	Warnings  []string `json:"warnings"`
}

// --- cache ---------------------------------------------------------------
//
// One shared cache: the fast snapshot is (re)computed on demand (it is
// in-process-cheap), the deep facts are warmed by the FIRST full Probe
// (sync.Once) — usually the background probe started at boot.

var (
	probeOnce  sync.Once
	probeCache *SysInfo
)

// fastReadyOnce (v1.3.0) guarantees the fast-environment summary is
// logged EXACTLY once per process. fastSnapshot() itself is silent — it
// is a cache/read event requested by many UI surfaces (environment
// endpoint, system panel, perf poll, engine checks); a measurement event
// (the one-time ready summary, the one-time deep probe) is the only
// thing that belongs in the log. The v1.2.9 defect: fastSnapshot logged
// INFO on EVERY call, so a single UI session produced dozens of
// identical "fast snapshot in 0 ms" lines.
var fastReadyOnce sync.Once

// resetFastLogForTest clears the one-shot fast-environment summary state
// (tests only).
func resetFastLogForTest() {
	fastReadyOnce = sync.Once{}
}

// deepProbeFn is the seam the startup test uses to prove New()/ProbeFast
// never invoke (or wait for) the deep probe: the test swaps it with a
// slow stub and resets the sync.Once. Production always runs probeUncached.
var deepProbeFn = probeUncached

// resetDeepProbeForTest clears the single-flight state (tests only).
func resetDeepProbeForTest() {
	probeOnce = sync.Once{}
	probeCache = nil
	deepMu.Lock()
	deepSnapshot = nil
	deepMu.Unlock()
}

// fastMu guards the fast-snapshot cache (deep facts are merged in).
var fastMu sync.Mutex

// Probe collects the FULL system information (fast + deep). The deep part
// runs ONCE per process (hardware does not change inside a session) and is
// bounded by probeTimeout. On Windows this is ONE batched PowerShell/CIM
// invocation, not one process per fact.
func Probe() *SysInfo {
	probeOnce.Do(func() {
		probeCache = deepProbeFn()
	})
	return probeCache
}

// ProbeFast returns the best information available WITHOUT ever blocking
// on a deep probe: the in-process fast facts always, plus the measured
// deep facts when the (background) deep probe has already completed.
// This is what interactive endpoints (/api/environment, /api/sysinfo)
// serve — the UI must never wait for CIM.
func ProbeFast() *SysInfo {
	fast := fastSnapshot()

	deepMu.Lock()
	deep := deepSnapshot
	deepMu.Unlock()

	if deep == nil {
		return fast
	}

	// Merge: deep facts win (they are the measured enrichments).
	merged := *fast
	merged.CPU.Name = deep.CPU.Name
	if deep.CPU.FrequencyMHz > 0 {
		merged.CPU.FrequencyMHz = deep.CPU.FrequencyMHz
	}
	merged.GPU = deep.GPU
	merged.NPU = deep.NPU
	merged.WSL2 = deep.WSL2
	merged.Docker = deep.Docker
	merged.DeepProbedAt = deep.DeepProbedAt
	merged.DeepProbeMs = deep.DeepProbeMs

	return &merged
}

// DeepReady reports whether the deep facts are already measured.
func DeepReady() bool {
	deepMu.Lock()
	defer deepMu.Unlock()

	return deepSnapshot != nil
}

// deepSnapshot is the shared deep-facts holder (guarded by deepMu).
var (
	deepMu       sync.Mutex
	deepSnapshot *SysInfo
)

// storeDeep publishes measured deep facts (caller: probeUncached only).
func storeDeep(info *SysInfo) {
	deepMu.Lock()
	deepSnapshot = info
	deepMu.Unlock()
}

// fastSnapshot assembles the always-cheap facts. No process spawn on
// Windows (in-process Win32 APIs); /proc reads on Linux; sysctl on darwin
// is acceptable but falls back to runtime values when it fails.
//
// v1.3.0: this function is a READ/CACHE event and is silent by design
// (see fastReadyOnce above). The single startup summary — the
// measurement event — is emitted once, the first time the fast
// environment is actually assembled.
func fastSnapshot() *SysInfo {
	started := time.Now()

	info := &SysInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: hostname(),
		CPU: CPUInfo{
			PhysicalCores: runtime.NumCPU(),
			LogicalCores:  runtime.NumCPU(),
		},
	}

	// v1.2.6 continuation: the MEASURED OS identity (in-process, no
	// spawn): the real build number and the display label derived from
	// it — the honest replacement for trusting any manually-labelled
	// version string. fastOSIdentity is per-OS; unmeasured stays 0
	// ("unknown") — never a guess.
	build, display := fastOSIdentity()
	info.OSBuild = build
	info.OSDisplay = display

	info.RAM = fastRAM()
	info.Disk = fastDisk(".")
	info.Recommended = recommend(info)

	// v1.3.0: ONE concise summary per process — not one line per UI poll.
	fastReadyOnce.Do(func() {
		logging.Default().Info(
			"sysinfo",
			"fast environment ready (os=%s arch=%s ramMB=%d, assembled in %d ms)",
			info.OS, info.Arch, info.RAM.TotalBytes>>20,
			time.Since(started).Milliseconds(),
		)
	})

	return info
}

// probeUncached runs the deep probe and merges it with the fast snapshot.
func probeUncached() *SysInfo {
	// v1.2.5 measured the FIRST full probe cost (the "hardware probe: X ms"
	// log line). v1.2.6 splits it: fast (above) + deep (here), each with its
	// own measured duration, and every source records success/failure.
	started := time.Now()

	info := fastSnapshot()
	info.CPU.Name = "Unknown"

	var probeSources []string

	// --- CPU identity ---------------------------------------------------
	cpuStarted := time.Now()

	switch runtime.GOOS {
	case "linux":
		info.CPU.Name = readFirstLine("/proc/cpuinfo", "model name")

		if coreSockets := readFirstLine("/proc/cpuinfo", "cpu cores"); coreSockets != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(coreSockets)); err == nil {
				info.CPU.PhysicalCores = n
			}
		}

		if freq := readFirstLine("/proc/cpuinfo", "cpu MHz"); freq != "" {
			if f, err := strconv.ParseFloat(strings.TrimSpace(freq), 64); err == nil {
				info.CPU.FrequencyMHz = int(f)
			}
		}

		probeSources = append(probeSources, "linux-proc")
	case "darwin":
		if out, err := probeOutput("sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
			info.CPU.Name = strings.TrimSpace(string(out))
		}

		if out, err := probeOutput("sysctl", "-n", "hw.physicalcpu"); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
				info.CPU.PhysicalCores = n
			}
		}

		if out, err := probeOutput("sysctl", "-n", "hw.logicalcpu"); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
				info.CPU.LogicalCores = n
			}
		}

		if out, err := probeOutput("sysctl", "-n", "hw.cpufrequency"); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
				info.CPU.FrequencyMHz = int(n / 1_000_000)
			}
		}

		probeSources = append(probeSources, "darwin-sysctl")
	case "windows":
		// v1.2.6: ONE batched CIM invocation replaces the v1.2.5 chain of
		// ~7 separate PowerShell processes (CPU x3, RAM x2, GPU x1, disk
		// wmic) that measured 5,399 ms on the target machine.
		facts, err := cimBatch()
		if err != nil {
			logging.Default().Warn("sysinfo", "batched CIM probe failed: %v", err)
			probeSources = append(probeSources, "windows-cim:FAILED")
		} else {
			probeSources = append(probeSources, "windows-cim:OK")

			if facts.cpuName != "" {
				info.CPU.Name = facts.cpuName
			}

			if facts.cpuCores > 0 {
				info.CPU.PhysicalCores = facts.cpuCores
			}

			if facts.cpuLogical > 0 {
				info.CPU.LogicalCores = facts.cpuLogical
			}

			if facts.cpuMHz > 0 {
				info.CPU.FrequencyMHz = facts.cpuMHz
			}

			if facts.ramTotalKB > 0 {
				info.RAM.TotalBytes = facts.ramTotalKB * 1024
			}

			if facts.ramFreeKB > 0 {
				info.RAM.FreeBytes = facts.ramFreeKB * 1024
				info.RAM.Available = facts.ramFreeKB * 1024
			}

			for _, g := range facts.gpus {
				info.GPU = append(info.GPU, g)
			}

			if facts.npu.present {
				npu := facts.npu.info
				info.NPU = &npu
			}

			if facts.diskTotal > 0 {
				info.Disk.TotalBytes = facts.diskTotal
				info.Disk.FreeBytes = facts.diskFree
			}
		}
	}

	logging.Default().Debug(
		"sysinfo",
		"cpu probe in %d ms",
		time.Since(cpuStarted).Milliseconds(),
	)

	// GPU probes for non-Windows platforms.
	gpuStarted := time.Now()
	info.GPU = append(info.GPU, probeGPUs()...)

	logging.Default().Debug(
		"sysinfo",
		"gpu probe in %d ms (devices=%d)",
		time.Since(gpuStarted).Milliseconds(), len(info.GPU),
	)

	if info.GPU == nil {
		info.GPU = []GPUInfo{}
	}

	envStarted := time.Now()
	info.WSL2 = detectWSL2()
	info.Docker = detectDocker()

	logging.Default().Debug(
		"sysinfo",
		"environment probe in %d ms (wsl2=%t docker=%t)",
		time.Since(envStarted).Milliseconds(), info.WSL2, info.Docker,
	)

	info.DeepProbedAt = time.Now().UTC()
	info.DeepProbeMs = time.Since(started).Milliseconds()
	info.Recommended = recommend(info)

	logging.Default().Info(
		"sysinfo",
		"deep probe completed in %d ms (sources: %s)",
		info.DeepProbeMs,
		strings.Join(probeSources, ", "),
	)

	// Publish for ProbeFast readers.
	storeDeep(info)

	return info
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// fastRAM reads memory without spawning a process (Windows: in-process
// GlobalMemoryStatusEx; Linux: /proc/meminfo; others: unknown).
//fastRAM is defined per-OS in sysinfo_fast_*.go files.

// fastDisk reads disk usage without spawning a process (Windows: in-process
// GetDiskFreeSpaceEx; Linux/darwin: statfs).
// fastDisk is defined per-OS in sysinfo_fast_*.go files.

func probeDisk(path string) DiskInfo {
	d := DiskInfo{Path: path}
	abs, _ := filepath.Abs(path)
	d.Path = abs
	// Use 'df' on linux/darwin; wmic on windows
	if runtime.GOOS == "windows" {
		if out, err := probeOutput("wmic", "logicaldisk", "where", "DeviceID='"+filepath.VolumeName(abs)+"'", "get", "FreeSpace,Size"); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(strings.TrimSpace(lines[1]))
				if len(fields) == 2 {
					d.FreeBytes, _ = strconv.ParseUint(fields[0], 10, 64)
					d.TotalBytes, _ = strconv.ParseUint(fields[1], 10, 64)
				}
			}
		}
	} else {
		if out, err := probeOutput("df", "-k", abs); err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 4 {
					total, _ := strconv.ParseUint(fields[1], 10, 64)
					free, _ := strconv.ParseUint(fields[3], 10, 64)
					d.TotalBytes = total * 1024
					d.FreeBytes = free * 1024
				}
			}
		}
	}
	return d
}

func probeGPUs() []GPUInfo {
	var gpus []GPUInfo
	// NVIDIA via nvidia-smi
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		if out, err := probeOutput("nvidia-smi",
			"--query-gpu=name,driver_version,memory.total",
			"--format=csv,noheader,nounits"); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				fields := strings.Split(line, ", ")
				if len(fields) < 3 {
					continue
				}
				vramMB, _ := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
				gpus = append(gpus, GPUInfo{
					Vendor:    "NVIDIA",
					Name:      strings.TrimSpace(fields[0]),
					DriverVer: strings.TrimSpace(fields[1]),
					VRAMBytes: vramMB * 1024 * 1024,
				})
			}
		}
	}
	// Apple Metal via system_profiler
	if runtime.GOOS == "darwin" {
		if out, err := probeOutput("system_profiler", "SPDisplaysDataType"); err == nil {
			text := string(out)
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "Chipset Model") {
					gpus = append(gpus, GPUInfo{
						Vendor: "Apple",
						Name:   strings.TrimSpace(strings.SplitN(line, ":", 2)[1]),
					})
				}
				if strings.Contains(line, "VRAM") && len(gpus) > 0 {
					v := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
					if strings.HasSuffix(v, " MB") {
						if n, err := strconv.ParseUint(strings.TrimSuffix(v, " MB"), 10, 64); err == nil {
							gpus[len(gpus)-1].VRAMBytes = n * 1024 * 1024
						}
					}
				}
			}
		}
	}
	// v1.0.3/v1.2.6: Windows adapters arrive through the ONE batched CIM
	// invocation (see cimBatch) — not through a second process spawn here.
	return gpus
}

func detectWSL2() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

func detectDocker() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func readFirstLine(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// recommend derives llama.cpp runtime knobs from the host's hardware.
func recommend(info *SysInfo) Recommended {
	r := Recommended{
		NumThread: info.CPU.PhysicalCores,
		NumGPU:    0,
		NumCtx:    8192,
		NumBatch:  512,
		MaxTokens: 1024,
		CanRunCPU: info.RAM.TotalBytes >= 4*1024*1024*1024,
		CanRunGPU: false,
	}
	for _, gpu := range info.GPU {
		if gpu.VRAMBytes >= 2*1024*1024*1024 {
			r.CanRunGPU = true
			r.NumGPU = 99 // offload all layers
			break
		}
	}
	// Scale context window to RAM
	if info.RAM.TotalBytes >= 32*1024*1024*1024 {
		r.NumCtx = 16384
	} else if info.RAM.TotalBytes >= 16*1024*1024*1024 {
		r.NumCtx = 8192
	} else if info.RAM.TotalBytes >= 8*1024*1024*1024 {
		r.NumCtx = 4096
	}
	// Warnings
	if !r.CanRunCPU {
		r.Warnings = append(r.Warnings, "Insufficient RAM (<4 GB) — CPU inference will fail")
	}
	if r.NumThread < 4 {
		r.Warnings = append(r.Warnings, "Few CPU cores — expect slow CPU inference")
	}
	if !r.CanRunGPU && len(info.GPU) == 0 {
		r.Warnings = append(r.Warnings, "No GPU detected — falling back to CPU")
	}
	return r
}

// RecommendThreads returns (generationThreads, prefillThreads):
// token generation prefers PHYSICAL cores (SMT siblings fight for the
// same execution units and usually cost 5-15% tok/s), while prompt
// prefill parallelizes well across every LOGICAL core. This is the llama.cpp
// tuning consensus as of 2026 (v1.0.4 Speed Pack).
func RecommendThreads() (gen int, batch int) {
	info := Probe()
	gen = info.CPU.PhysicalCores
	batch = info.CPU.LogicalCores
	if gen <= 0 {
		gen = runtime.NumCPU()
	}
	if batch <= 0 {
		batch = runtime.NumCPU()
	}
	if gen > batch {
		gen = batch // defensive: never claim more gen threads than logical cores
	}
	return gen, batch
}

// FormatBytes pretty-prints byte counts.
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
