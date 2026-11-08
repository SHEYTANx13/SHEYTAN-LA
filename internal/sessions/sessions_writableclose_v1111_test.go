package sessions

// sessions_writableclose_v1111_test.go — v1.11.1 E6 remediation proof
// (CodeQL go/unhandled-writable-file-close #247): the write+close pair
// on the activity feed must preserve BOTH the primary write failure and
// the cleanup close failure. The deterministic "writer whose Close
// fails" fixture is an already-closed file: every subsequent Write AND
// Close fails, so the joined error must carry both.

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAppendLineCloseJoinsWriteAndCloseFailures(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "acts-")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	path := f.Name()

	// Close FIRST: this is the writer whose Close fails — and whose
	// Write fails too, exercising the join on both components.
	if err := f.Close(); err != nil {
		t.Fatalf("pre-close: %v", err)
	}

	err = appendLineClose(f, []byte("activity line\n"))
	if err == nil {
		t.Fatal("appendLineClose on a closed file must fail")
	}

	// Both component failures must be observable in the joined error:
	// os.ErrClosed flows through both the Write and the Close branch.
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("joined error must wrap os.ErrClosed (write and close both failed), got: %v", err)
	}

	// errors.Join renders one line per component: the joined error must
	// carry TWO distinct failure lines, never just the first.
	if lines := strings.Count(err.Error(), "\n") + 1; lines != 2 {
		t.Errorf("joined error must preserve BOTH failures (2 lines), got %d: %q", lines, err.Error())
	}

	// The failed write must not have landed on disk.
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read back: %v", rerr)
	}
	if strings.Contains(string(data), "activity line") {
		t.Error("the failed write must not have materialized on disk")
	}
}

func TestAppendLineCloseHappyPath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "acts-ok-")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())

	if err := appendLineClose(f, []byte("activity line\n")); err != nil {
		t.Fatalf("happy path must be nil, got: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "activity line\n" {
		t.Errorf("content = %q, want the written line", data)
	}
}
