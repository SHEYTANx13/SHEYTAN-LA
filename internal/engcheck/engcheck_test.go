package engcheck

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSniffArchDetectsHostBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	arch, format := SniffArch(exe)

	if format == "" || arch == "" {
		t.Fatalf("the test binary (%s) must sniff as a known format, got arch=%q format=%q", runtime.GOOS, arch, format)
	}

	if !ArchMatchesHost(arch) {
		t.Fatalf("the test binary's architecture %q must match host %q", arch, runtime.GOARCH)
	}
}

func TestSniffArchRejectsTextFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-an-engine.txt")
	if err := os.WriteFile(path, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}

	arch, format := SniffArch(path)
	if arch != "" || format != "" {
		t.Fatalf("a text file must not sniff as executable, got %q/%q", arch, format)
	}
}

func TestStaticValidateGates(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := StaticValidate(exe); err != nil {
		t.Fatalf("the running test binary must pass the static gate: %v", err)
	}

	// Missing file.
	if _, err := StaticValidate(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("missing file must fail the static gate")
	}

	// Empty file.
	empty := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(empty, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := StaticValidate(empty); err == nil {
		t.Fatalf("empty file must fail the static gate")
	}

	// Non-executable real binary (Unix only).
	if runtime.GOOS != "windows" {
		noExec := filepath.Join(t.TempDir(), "noexec.bin")

		in, err := os.Open(exe)
		if err != nil {
			t.Fatal(err)
		}

		st, _ := in.Stat()
		data := make([]byte, st.Size())
		if _, err := in.Read(data); err != nil && len(data) == 0 {
			t.Fatal(err)
		}
		_ = in.Close()

		if err := os.WriteFile(noExec, data, 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := StaticValidate(noExec); err == nil {
			t.Fatalf("a non-executable binary must fail the static gate on Unix")
		}
	}
}

func TestHashFileIsStableAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte("hello sheytan"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}

	h2, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("hash must be a stable sha-256 hex digest, got %q vs %q", h1, h2)
	}
}

func TestCheckDependenciesOnNonPEIsEmpty(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("the test binary is a PE on Windows — imports are checked")
	}

	rep, err := CheckDependencies(exe)
	if err != nil {
		t.Fatal(err)
	}

	// ELF host: no import table to walk — the report carries format only.
	if len(rep.Missing) != 0 || len(rep.Present) != 0 {
		t.Fatalf("non-PE reports no import evidence, got %+v", rep)
	}
}
