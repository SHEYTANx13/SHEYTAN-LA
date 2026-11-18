//go:build linux

// sysinfo_fast_linux.go — fast RAM facts on Linux (v1.2.6): /proc/meminfo
// is an in-process read — no external command is spawned.
package sysinfo

import (
	"os"
	"strconv"
	"strings"
)

func fastRAM() RAMInfo {
	r := RAMInfo{}

	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.TrimSuffix(val, " kB")
			n, _ := strconv.ParseUint(val, 10, 64)
			switch key {
			case "MemTotal":
				r.TotalBytes = n * 1024
			case "MemFree":
				r.FreeBytes = n * 1024
			case "MemAvailable":
				r.Available = n * 1024
			}
		}
	}

	return r
}
