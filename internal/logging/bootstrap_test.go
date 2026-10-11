package logging

// bootstrap_test.go — v1.7.4: the bootstrap sink records pre-canonical
// boot lines (stderr + bounded memory buffer) WITHOUT creating or opening
// any file — the runtime-data migrations must be able to create or rename
// the very sink files a canonical manager would hold open (Windows denies
// renaming onto an open path). ReplayInto re-emits the buffered lines
// through a canonical manager's normal Info/Warn/Error pipeline.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr runs fn with os.Stderr replaced by a pipe and returns
// everything written during fn.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	old := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = old
	_ = w.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	_ = r.Close()

	return string(data)
}

// TestBootstrapWritesNoFilesAndBuffers: stderr + memory only — no file may
// be created anywhere, and the buffer must keep level/category/message.
func TestBootstrapWritesNoFilesAndBuffers(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp) // any file the bootstrap created would land here

	var boot *Manager
	stderr := captureStderr(t, func() {
		boot = NewBootstrap()
		boot.Info("paths", "bootstrap note %d", 1)
		boot.Warn("config", "sampling value repaired")
		boot.Error("paths", "legacy AppData migration incomplete: access denied")
	})

	if boot == nil || !boot.Enabled() {
		t.Fatal("the bootstrap manager must be enabled although it owns no directory")
	}

	for _, want := range []string{
		"bootstrap note 1",
		"sampling value repaired",
		"legacy AppData migration incomplete: access denied",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the bootstrap manager created files: %v", entries)
	}

	boot.mu.Lock()
	defer boot.mu.Unlock()

	if len(boot.buf) != 3 {
		t.Fatalf("buffer = %d entries, want 3", len(boot.buf))
	}
	if boot.buf[0].Level != "INFO" || boot.buf[0].Category != "paths" || boot.buf[0].Message != "bootstrap note 1" {
		t.Fatalf("entry 0 = %+v", boot.buf[0])
	}
	if boot.buf[1].Level != "WARN" || boot.buf[1].Category != "config" || boot.buf[1].Message != "sampling value repaired" {
		t.Fatalf("entry 1 = %+v", boot.buf[1])
	}
	if boot.buf[2].Level != "ERROR" || boot.buf[2].Category != "paths" || boot.buf[2].Message != "legacy AppData migration incomplete: access denied" {
		t.Fatalf("entry 2 = %+v", boot.buf[2])
	}
}

// TestNoopManagerStaysDisabled: the distinct bootstrap field must not
// change the long-standing no-op contract (dir == "", disabled).
func TestNoopManagerStaysDisabled(t *testing.T) {
	if noop.Enabled() {
		t.Fatal("the true noop manager must stay disabled")
	}
	if (&Manager{}).Enabled() {
		t.Fatal("a zero-value manager must stay disabled")
	}
	m, err := New("")
	if err != nil || m != noop {
		t.Fatalf(`New("") must return the shared noop manager, got %v`, m)
	}
}

// TestReplayIntoCanonicalPreservesLevelCategoryMessage: the buffered lines
// must land in app.log through dst's normal pipeline, in order, exactly
// once (the replay drains the buffer).
func TestReplayIntoCanonicalPreservesLevelCategoryMessage(t *testing.T) {
	dir := t.TempDir()

	boot := NewBootstrap()
	boot.Info("paths", "folded application root into the canonical data root")
	boot.Warn("config", "sampling value repaired by the loader: repeatPenalty")
	boot.Error("paths", "legacy AppData migration incomplete (will retry on next start): access denied")

	canonical, err := New(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = canonical.Close() })

	boot.ReplayInto(canonical)

	data, err := os.ReadFile(filepath.Join(dir, "logs", "app.log"))
	if err != nil {
		t.Fatalf("read app.log: %v", err)
	}
	log := string(data)

	for _, want := range []string{
		"INFO  [paths] folded application root into the canonical data root",
		"WARN  [config] sampling value repaired by the loader: repeatPenalty",
		"ERROR [paths] legacy AppData migration incomplete (will retry on next start): access denied",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("app.log missing %q:\n%s", want, log)
		}
	}

	iInfo := strings.Index(log, "folded application root")
	iErr := strings.Index(log, "legacy AppData migration incomplete")
	if iInfo < 0 || iErr < 0 || iInfo > iErr {
		t.Fatalf("replay reordered the buffered lines:\n%s", log)
	}

	// The replay drained the buffer: a second call writes nothing.
	before, err := os.Stat(filepath.Join(dir, "logs", "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	boot.ReplayInto(canonical)
	after, err := os.Stat(filepath.Join(dir, "logs", "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatalf("second replay appended lines (size %d -> %d)", before.Size(), after.Size())
	}
}

// TestBootstrapBufferIsBounded: the buffer is a bounded ring — 300 lines
// leave exactly the newest bootstrapBufferCap entries, oldest dropped
// first.
func TestBootstrapBufferIsBounded(t *testing.T) {
	boot := NewBootstrap()
	for i := 0; i < 300; i++ {
		boot.Info("boot", "line %d", i)
	}

	boot.mu.Lock()
	defer boot.mu.Unlock()

	if len(boot.buf) != bootstrapBufferCap {
		t.Fatalf("buffer = %d entries, want exactly cap %d", len(boot.buf), bootstrapBufferCap)
	}
	if got, want := boot.buf[0].Message, fmt.Sprintf("line %d", 300-bootstrapBufferCap); got != want {
		t.Fatalf("oldest retained entry = %q, want %q (ring must drop the oldest)", got, want)
	}
	if got := boot.buf[len(boot.buf)-1].Message; got != "line 299" {
		t.Fatalf("newest entry = %q, want %q", got, "line 299")
	}
}
