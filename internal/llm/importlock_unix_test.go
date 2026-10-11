//go:build !windows

package llm

// importlock_unix_test.go — the CROSS-PROCESS half of the import-lock
// contract on Unix: an independent file descriptor holding flock(2)
// (exactly what another importer process looks like to the kernel) must
// make ImportModel refuse to run un-serialized, and a completed import
// must have released the lock.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestImportRefusesWhileCrossProcessLockHeld(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Hold the flock from an independent descriptor (simulates another
	// importer process).
	holder, err := os.OpenFile(filepath.Join(modelsDir, importLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("test holder flock: %v", err)
	}
	defer syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)

	// Shrink the bounded wait so the test is fast.
	prevTimeout := importLockAcquireTimeout
	importLockAcquireTimeout = 300 * time.Millisecond
	t.Cleanup(func() { importLockAcquireTimeout = prevTimeout })

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "blocked.gguf")
	writeTestGGUF(t, src, "BLOCKED0", 64<<10)

	start := time.Now()
	_, err = ImportModel(modelsDir, src, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("an import must refuse to run while the cross-process lock is held elsewhere")
	}
	if !strings.Contains(err.Error(), "import") {
		t.Fatalf("the refusal must be actionable, got: %v", err)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("the refusal must come from the bounded wait, not an unrelated fast failure (elapsed %v)", elapsed)
	}
	// Nothing was placed while refused.
	if n := countModelFiles(t, modelsDir); n != 0 {
		t.Errorf("a refused import must place nothing, found %d model files", n)
	}
}

func TestImportLockReleasedAfterCompletion(t *testing.T) {
	srcDir := t.TempDir()
	modelsDir := t.TempDir()

	src := filepath.Join(srcDir, "first.gguf")
	writeTestGGUF(t, src, "FIRSTONE", 64<<10)

	if _, err := ImportModel(modelsDir, src, nil); err != nil {
		t.Fatalf("first import: %v", err)
	}

	// An independent descriptor must be able to take the flock right
	// away — proof the importer released it.
	probe, err := os.OpenFile(filepath.Join(modelsDir, importLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the import lock was not released after completion: %v", err)
	}
	_ = syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
}
