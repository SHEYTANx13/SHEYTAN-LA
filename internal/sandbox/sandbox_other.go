//go:build !windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// prlimitGovernor wraps commands with prlimit(1) when available:
//   - --cpu=<seconds>: hard CPU-time budget per run (scaled by cpuPct —
//     a 25% sandbox on a 60s timeout gets 15 CPU seconds)
//   - --as is deliberately NOT applied: node/V8 and modern JVMs reserve
//     multi-GB virtual address spaces at startup and would die instantly
//     even though their RSS is tiny. Without cgroups (root) there is no
//     honest RSS cap on Linux, so CPU + timeout carry the protection.
type prlimitGovernor struct {
	cpuPct int
	// prlimit availability is probed once per process.
	once   sync.Once
	hasPrl bool
}

// newGovernor returns the Unix prlimit governor, or nil when prlimit is
// missing (timeout-only degradation — the sandbox stays functional).
func newGovernor(memMB, cpuPct int) resourceGovernor {
	g := &prlimitGovernor{cpuPct: cpuPct}
	g.once.Do(func() {
		_, err := exec.LookPath("prlimit")
		g.hasPrl = err == nil
	})
	if !g.hasPrl {
		return nil
	}
	return g
}

// prepare rewrites the command as
// prlimit --cpu=<budget> -- <cmd> <args...> so the kernel enforces the
// CPU budget even if the parent dies.
func (g *prlimitGovernor) prepare(timeoutSec int, name string, args []string) (string, []string) {
	cpu := timeoutSec * g.cpuPct / 100
	if cpu < 1 {
		cpu = 1
	}
	wrapped := append([]string{
		fmt.Sprintf("--cpu=%d", cpu),
		"--",
		name,
	}, args...)
	return "prlimit", wrapped
}

// attach is a no-op on Unix (caps ride the exec wrapper).
func (g *prlimitGovernor) attach(p *os.Process) error { return nil }

// close is a no-op on Unix (children die with the ctx timeout).
func (g *prlimitGovernor) close() error { return nil }

// resolvePython finds python3 (or python) on Unix.
func resolvePython() (string, error) {
	for _, cand := range []string{"python3", "python"} {
		if path, err := exec.LookPath(cand); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("python3 not found")
}
