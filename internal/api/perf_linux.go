//go:build linux

package api

// perf_linux.go — cheap live CPU/RAM samples for /api/perf on Linux,
// read straight from /proc. No new dependencies.

import (
	"os"
	"strconv"
	"strings"
)

// cpuTimesSnapshot reads cumulative idle/busy CPU times from /proc/stat.
func cpuTimesSnapshot() (idle, busy float64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}

	// First line: cpu  user nice system idle iowait irq softirq steal ...
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}

		fields := strings.Fields(line)[1:]
		if len(fields) < 4 {
			return 0, 0, false
		}

		var nums []float64
		for _, f := range fields {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return 0, 0, false
			}
			nums = append(nums, v)
		}

		var total float64
		for _, v := range nums {
			total += v
		}

		idle := nums[3]
		if len(nums) > 4 { // iowait counts as idle
			idle += nums[4]
		}

		return idle, total - idle, true
	}

	return 0, 0, false
}

// sampleRAM reads live memory pressure from /proc/meminfo.
func sampleRAM() (ramSample, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return ramSample{}, false
	}

	var total, avail uint64
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = meminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = meminfoKB(line)
		}
	}

	if total == 0 {
		return ramSample{}, false
	}

	used := total - avail

	out := ramSample{
		TotalBytes:     total,
		AvailableBytes: avail,
		UsedBytes:      used,
	}
	if total > 0 {
		out.UsedPercent = 100 * float64(used) / float64(total)
	}

	return out, true
}

// meminfoKB parses one "MemTotal:  16384000 kB" line into bytes.
func meminfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	kb, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}
