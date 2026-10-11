//go:build !windows

package resources

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func procRAMImpl(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return kb * 1024, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("VmRSS not found for pid %d", pid)
}
