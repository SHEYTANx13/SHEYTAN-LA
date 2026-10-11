//go:build !windows

package llm

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// exitSignalPart reports the terminating signal for a signaled process
// (unix). Windows has no signal concept — see exitdetail_windows.go.
func exitSignalPart(err error) string {
	var exitErr *exec.ExitError

	if !errors.As(err, &exitErr) {
		return ""
	}

	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf("signal %s", ws.Signal().String())
	}

	return ""
}
