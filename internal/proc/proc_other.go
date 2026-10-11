//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// newSysProcAttr starts the child as the leader of its own process group.
func newSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// applyHidden is a no-op outside Windows, but preserve any existing process
// attributes and ensure the child owns a distinct process group.
func applyHidden(attr *syscall.SysProcAttr) {
	if attr != nil {
		attr.Setpgid = true
	}
}

// killProcessTree terminates the complete Unix process group created for the
// command. The negative PID targets the process group rather than only the
// shell process.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	err := syscall.Kill(
		-cmd.Process.Pid,
		syscall.SIGKILL,
	)

	if err == syscall.ESRCH {
		return nil
	}

	return err
}
