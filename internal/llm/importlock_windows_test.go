//go:build windows

package llm

// importlock_windows_test.go — the CROSS-PROCESS half of the import-lock
// contract on Windows: an independent file handle holding a LockFileEx
// exclusive byte range (exactly what another importer process looks like
// to the kernel) must make ImportModel refuse to run un-serialized, and
// a completed import must have released the lock.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportRefusesWhileCrossProcessLockHeld(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	holder, err := os.OpenFile(filepath.Join(modelsDir, importLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	if err := lockImportFile(holder); err != nil {
		t.Fatalf("test holder LockFileEx: %v", err)
	}
	defer func() { _ = unlockImportFile(holder) }()

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

	probe, err := os.OpenFile(filepath.Join(modelsDir, importLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	if err := lockImportFile(probe); err != nil {
		t.Fatalf("the import lock was not released after completion: %v", err)
	}
	_ = unlockImportFile(probe)
}
