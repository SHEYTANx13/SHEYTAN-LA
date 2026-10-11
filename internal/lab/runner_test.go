package lab

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunnerCombinedOutputLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	workspaceRoot := filepath.Join(root, "workspaces")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	manager, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := manager.Create(
		context.Background(),
		source,
	)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(
		5*time.Second,
		1024,
	)

	result, err := runner.Run(
		context.Background(),
		workspace,
		Command{
			Command: outputFloodCommand(4096),
		},
	)

	if err == nil {
		t.Fatal("expected combined output limit to return an error")
	}

	if !strings.Contains(err.Error(), ErrOutputLimit.Error()) {
		t.Fatalf(
			"expected ErrOutputLimit, got %v",
			err,
		)
	}

	if !result.OutputLimit {
		t.Fatal("result.OutputLimit = false, want true")
	}

	if result.Success {
		t.Fatal("output-limited command must not be successful")
	}

	captured := int64(len(result.Stdout) + len(result.Stderr))

	if captured > 1024 {
		t.Fatalf(
			"captured stdout+stderr = %d bytes, want <= 1024",
			captured,
		)
	}

	if !strings.Contains(
		result.Output,
		"[LAB OUTPUT TRUNCATED]",
	) {
		t.Fatalf(
			"truncation marker missing from output: %q",
			result.Output,
		)
	}
}

func outputFloodCommand(size int) string {
	n := strconv.Itoa(size)

	if runtime.GOOS == "windows" {
		// cmd.exe: emit enough stdout to exceed the shared 1 KiB budget.
		return "for /L %i in (1,1," + n + ") do @echo 1234567890"
	}

	// Use the shell's printf rather than assuming Python is installed.
	return "i=0; while [ $i -lt " + n + " ]; do printf '1234567890'; i=$((i+1)); done"
}
