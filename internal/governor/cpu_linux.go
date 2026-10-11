//go:build linux

// cpu_linux.go — v1.8.0 CPU load seam (Linux).
//
// The platform CPU signal is derived from /proc/loadavg — the kernel's own
// rolling load average (1m window), normalized by the logical core count.
// This is a MEASURED fact from the OS, not a fabricated utilization: where
// /proc is unavailable the sampler reports "not measurable" and every CPU
// policy branch stays OFF (unknown never drives policy).
package governor

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// CPULoadPlatform is the Linux CPUSampler: the 1-minute load average
// normalized to 0..100 percent of total core capacity.
func CPULoadPlatform() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}

	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}

	cores := runtime.NumCPU()
	if cores <= 0 {
		return 0, false
	}

	pct := load1 / float64(cores) * 100
	if pct > 100*10 {
		// A heavily oversubscribed load average is still a real fact,
		// but policy caps the signal so reasons stay readable.
		pct = 1000
	}

	return pct, true
}
