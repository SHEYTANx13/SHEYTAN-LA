//go:build !linux && !windows

// cpu_other.go — v1.8.0 CPU load seam (platforms without a live CPU
// authority: macOS/BSDs today). The codebase's CPU measurement
// authorities are the /proc/stat-family readers; until a live load
// authority exists for one of these platforms, the CPU seam reports
// "not measurable" and every CPU policy branch stays OFF. The self-model
// reports CPU as unknown — honestly. (Windows gained its GetSystemTimes
// delta seam in v1.8.6: cpu_windows.go; Linux keeps cpu_linux.go.)
package governor

// CPULoadPlatform reports that CPU load is not measurable on this
// platform for live policy purposes.
func CPULoadPlatform() (float64, bool) {
        return 0, false
}
