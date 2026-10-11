package sysinfo

// cim_parse_test.go — v1.2.6 unit tests for the batched CIM parser.
// The parser is pure string → struct, so it is testable on every platform
// with FIXTURE outputs modelled on real CIM responses. (The live probe
// itself requires a Windows host — see the honest limitations in the
// release notes: these tests verify the parsing contract, not the probe.)

import (
	"strings"
	"testing"
)

const intelArcNPUFixture = `CPU_NAME=Intel(R) Core(TM) Ultra 7 155H
CPU_CORES=16
CPU_LOGICAL=22
CPU_MHZ=3800
RAM_TOTAL_KB=32505544
RAM_FREE_KB=18432000
GPU=Intel(R) Arc(TM) A770M Graphics|4293918720|32.0.101.5768
GPU=Intel(R) Iris(R) Xe Graphics|1073741824|32.0.101.5768
NPU=Intel(R) AI Boost|Intel|32.0.100.3104|OK
DISK_TOTAL=1000202276864
DISK_FREE=483183820800
`

func TestParseCIMBatchIntelArcNPUMachine(t *testing.T) {
	facts := parseCIMBatch(intelArcNPUFixture)

	if facts.cpuName != "Intel(R) Core(TM) Ultra 7 155H" {
		t.Fatalf("cpuName = %q", facts.cpuName)
	}

	if facts.cpuCores != 16 || facts.cpuLogical != 22 || facts.cpuMHz != 3800 {
		t.Fatalf("cpu counts: %d/%d/%d", facts.cpuCores, facts.cpuLogical, facts.cpuMHz)
	}

	if facts.ramTotalKB != 32505544 || facts.ramFreeKB != 18432000 {
		t.Fatalf("ram: %d/%d", facts.ramTotalKB, facts.ramFreeKB)
	}

	if len(facts.gpus) != 2 {
		t.Fatalf("gpus = %d, want 2", len(facts.gpus))
	}

	// CIM AdapterRAM saturates near 4 GB (uint32) — the documented caveat;
	// presence + vendor + driver are the reliable facts.
	if facts.gpus[0].Vendor != "Intel" || facts.gpus[0].Name != "Intel(R) Arc(TM) A770M Graphics" {
		t.Fatalf("primary gpu = %+v", facts.gpus[0])
	}

	if !facts.npu.present {
		t.Fatal("NPU not detected on the Intel AI Boost fixture")
	}

	if facts.npu.info.Vendor != "Intel" || facts.npu.info.Name != "Intel(R) AI Boost" {
		t.Fatalf("npu = %+v", facts.npu.info)
	}

	if facts.npu.info.Status != "OK" || facts.npu.info.DriverVer != "32.0.100.3104" {
		t.Fatalf("npu status/driver = %q/%q", facts.npu.info.Status, facts.npu.info.DriverVer)
	}

	if facts.diskTotal != 1000202276864 || facts.diskFree != 483183820800 {
		t.Fatalf("disk: %d/%d", facts.diskTotal, facts.diskFree)
	}
}

func TestParseCIMBatchSkipsVirtualAdapters(t *testing.T) {
	facts := parseCIMBatch(`GPU=Microsoft Basic Display Adapter|0|
GPU=Intel(R) Arc(TM) Graphics|4293918720|31.0.101.5333
`)

	if len(facts.gpus) != 1 {
		t.Fatalf("gpus = %d, want 1 (virtual display adapter must be skipped)", len(facts.gpus))
	}

	if !strings.Contains(facts.gpus[0].Name, "Arc") {
		t.Fatalf("kept the wrong adapter: %+v", facts.gpus[0])
	}
}

func TestParseCIMBatchPartialAndGarbageInput(t *testing.T) {
	// PowerShell error text, empty sections, malformed numbers — every
	// value falls back to zero, nothing panics, partial facts survive.
	facts := parseCIMBatch(`CPU_NAME=AMD Ryzen 9
some random error text without equals key
RAM_TOTAL_KB=not-a-number
GPU=|0|
`)

	if facts.cpuName != "AMD Ryzen 9" {
		t.Fatalf("cpuName = %q", facts.cpuName)
	}

	if facts.ramTotalKB != 0 {
		t.Fatalf("malformed RAM must parse as 0, got %d", facts.ramTotalKB)
	}

	if len(facts.gpus) != 0 {
		t.Fatalf("empty GPU name must be skipped, got %+v", facts.gpus)
	}

	if facts.npu.present {
		t.Fatal("no NPU on this fixture")
	}
}

func TestParseCIMBatchNoNPU(t *testing.T) {
	facts := parseCIMBatch("CPU_NAME=Intel(R) Core(TM) i7-9750H\n")

	if facts.npu.present {
		t.Fatal("NPU must be absent on a machine without one")
	}
}

func TestFastSnapshotNeverSpawnsAndIsInstant(t *testing.T) {
	// On every platform the fast snapshot must be cheap: no external
	// process is spawned (in-process APIs only). We assert it returns
	// promptly with sane core counts; the timing assertion is generous to
	// stay stable on slow CI machines.
	info := ProbeFast()

	if info.CPU.LogicalCores <= 0 {
		t.Fatalf("logical cores = %d, want > 0", info.CPU.LogicalCores)
	}

	if info.RAM.TotalBytes == 0 && info.OS == "linux" {
		t.Fatal("linux fast snapshot must read /proc/meminfo")
	}
}

func TestProbeFastMergesDeepAfterProbe(t *testing.T) {
	if !DeepReady() {
		Probe() // warm (Linux: fast, in-process)
	}

	info := ProbeFast()

	if !DeepReady() {
		t.Fatal("DeepReady after Probe")
	}

	if info.DeepProbedAt.IsZero() {
		t.Fatal("DeepProbedAt must be set on a deep-merged snapshot")
	}
}
