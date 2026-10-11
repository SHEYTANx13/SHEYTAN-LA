//go:build !windows && !linux

package api

// perf_other.go — /api/perf on platforms without a native CPU/RAM sampler.
// Both surfaces report not-known so the UI renders N/A (never a guess).

// cpuTimesSnapshot is unavailable here.
func cpuTimesSnapshot() (idle, busy float64, ok bool) {
	return 0, 0, false
}

// sampleRAM is unavailable here.
func sampleRAM() (ramSample, bool) {
	return ramSample{}, false
}
