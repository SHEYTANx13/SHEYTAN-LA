package proc

import (
	"context"
	"os/exec"
)

// Hide marks the command so Windows never creates a console window for
// it. It is safe to call on every platform (no-op outside Windows).
func Hide(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = newSysProcAttr()
		return
	}

	applyHidden(cmd.SysProcAttr)
}

// Command builds an exec.Cmd that never opens a console window on Windows.
// Use it instead of exec.Command everywhere the app spawns a subprocess.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	Hide(cmd)
	return cmd
}

// CommandContext is the context-aware variant of Command.
//
// Cancellation is configured to terminate the whole process tree rather than
// only the immediate child. This is important for shell commands that spawn
// compilers, test runners, interpreters, or other descendants.
func CommandContext(
	ctx context.Context,
	name string,
	args ...string,
) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	Hide(cmd)

	cmd.Cancel = func() error {
		return killProcessTree(cmd)
	}

	return cmd
}
