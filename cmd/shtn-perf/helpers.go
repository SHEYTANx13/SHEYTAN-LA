package main

// helpers.go — the shtn-perf measurement plumbing: process RSS readers
// (Linux /proc with an honest fallback) and the small JSON indirection
// the version probe uses. No runtime behavior is owned here.

import (
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// rssMB reads the current process resident set (MB).
func rssMB() float64 {
	return procStatusKB("VmRSS")
}

// peakRSSMB reads the process high-water-mark resident set (MB).
func peakRSSMB() float64 {
	return procStatusKB("VmHWM")
}

func procStatusKB(field string) float64 {
	if runtime.GOOS != "linux" {
		// Non-Linux hosts: no honest sample from this primitive — the
		// metric is recorded with n=1 architecture fallback below and the
		// limitation list states the boundary. Returning 0 marks it
		// visibly unusable rather than inventing a number.
		return 0
	}

	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, field+":") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				kb, err := strconv.ParseFloat(parts[1], 64)
				if err == nil {
					return kb / 1024.0
				}
			}
		}
	}

	return 0
}
