package llm

// devices_missing_test.go — run #124 slice: the engine device probe must
// report an ABSENT engine executable as the TYPED ErrEngineBinaryMissing
// state (the expected pending state while the startup maintenance gate is
// installing/updating the engine artifact), while an existing-but-failing
// binary keeps its generic error. Deterministic: real filesystem + real
// exec against fixture files — no fakes prove nothing here.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestEnumerateEngineDevicesMissingBinaryIsTyped(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "llama-server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	devices, supported, err := EnumerateEngineDevices(bin)

	if !errors.Is(err, ErrEngineBinaryMissing) {
		t.Fatalf("missing engine must wrap ErrEngineBinaryMissing, got %v", err)
	}
	if devices != nil {
		t.Fatalf("devices = %v, want none for a missing engine", devices)
	}
	if supported {
		t.Fatal("supported must be false when the engine could not be probed")
	}

	// The failure is cached for the bounded failure window: a second call
	// inside that window answers the same typed verdict (no probe storm).
	devices2, _, err2 := EnumerateEngineDevices(bin)
	if !errors.Is(err2, ErrEngineBinaryMissing) || devices2 != nil {
		t.Fatalf("cached failure drifted: devices=%v err=%v", devices2, err2)
	}
}

func TestEnumerateEngineDevicesExistingFailingBinaryStaysGeneric(t *testing.T) {
	// "Exists but fails" must NOT fold into the missing-engine class.
	// Cross-platform deterministic fixture: a plain data file with no
	// execute permission. Linux refuses it with EACCES, Windows with
	// ERROR_BAD_EXE_FORMAT — both are "present but cannot execute"
	// failures, and neither matches the missing-engine classifier (the
	// llm test binary itself is scripted as a working fake engine and
	// cannot serve as the failing subject).
	bin := filepath.Join(t.TempDir(), "not-an-engine")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if err := os.WriteFile(bin, []byte("this is data, not an executable\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	devices, supported, enumErr := EnumerateEngineDevices(bin)

	if enumErr == nil {
		t.Fatal("an existing but non-executable engine must produce an error")
	}
	if errors.Is(enumErr, ErrEngineBinaryMissing) {
		t.Fatalf("a present-but-failing engine must not be reported as missing: %v", enumErr)
	}
	if devices != nil {
		t.Fatalf("devices = %v, want none", devices)
	}
	if supported {
		t.Fatal("supported must be false for a failed probe")
	}
}

func TestIsEngineBinaryMissingErrorClass(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"nil":                {nil, false},
		"exec not found":     {exec.ErrNotFound, true},
		"fs not exist":       {fs.ErrNotExist, true},
		"wrapped path error": {fmt.Errorf("fork/exec %s: %w", "C:\\data\\bin\\llama-server.exe", fs.ErrNotExist), true},
		"windows createproc": {fmt.Errorf("fork/exec C:\\data\\bin\\llama-server.exe: The system cannot find the file specified."), true},
		"exit status":        {fmt.Errorf("exit status 3"), false},
		"other failure":      {errors.New("permission denied"), false},
	}
	for name, tc := range cases {
		if got := isEngineBinaryMissingError(tc.err); got != tc.want {
			t.Fatalf("%s: isEngineBinaryMissingError = %t, want %t", name, got, tc.want)
		}
	}
}

func TestDeviceFailureTTLBoundsMissingEngineCache(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "engine-under-ttl")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	if _, _, err := EnumerateEngineDevices(bin); !errors.Is(err, ErrEngineBinaryMissing) {
		t.Fatalf("setup probe: %v", err)
	}

	// Advance the clock seam past the failure TTL: the probe must re-run
	// (fresh typed failure), never serve a stale SUCCESS shape.
	prev := timeNow
	timeNow = func() time.Time { return prev().Add(deviceFailureTTL + time.Minute) }
	t.Cleanup(func() { timeNow = prev })

	if _, _, err := EnumerateEngineDevices(bin); !errors.Is(err, ErrEngineBinaryMissing) {
		t.Fatalf("post-TTL probe: %v", err)
	}
}
