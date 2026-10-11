package updater

// v1.3.6 (spec §11/§12/§31): DLL/PACKAGE MIXING regressions.
//
// The previous mergeForeignFiles() moved EVERY file from the previous
// package into the new one — the exact mechanism that could reintroduce
// a stale llama.dll / ggml.dll beside a fresh llama-server.exe (the
// 0xC0000139 STATUS_ENTRYPOINT_NOT_FOUND class). The new contract:
//
//   - mergeCompanionFiles carries ONLY the allowlisted companions
//     (native engine host, license/metadata files);
//   - stale DLLs, foreign executables and unknown files are DROPPED
//     (and reported);
//   - copyEngineClosure imports only the candidate's validated closure
//     (entry binary + beside-the-binary runtime DLLs + metadata), never
//     the directory.

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMergeCompanionFilesDropsStaleDLLs(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()

	// The OLD package's stale DLLs — the poison this test exists for.
	write(t, filepath.Join(oldDir, "llama.dll"), "STALE LLAMA DLL")
	write(t, filepath.Join(oldDir, "ggml.dll"), "STALE GGML DLL")
	write(t, filepath.Join(oldDir, "ggml-cuda.dll"), "STALE CUDA BACKEND")

	// Allowlisted companions.
	write(t, filepath.Join(oldDir, "shtn-engine-host.exe"), "native host")
	write(t, filepath.Join(oldDir, "LICENSE"), "license text")
	write(t, filepath.Join(oldDir, "README.md"), "readme")

	// The new package's own file — never overwritten.
	write(t, filepath.Join(newDir, "llama-server.exe"), "NEW ENGINE")
	write(t, filepath.Join(newDir, "ggml-base.dll"), "NEW BASE DLL")

	dropped := mergeCompanionFiles(oldDir, newDir)

	// Stale DLLs must NOT have been carried over.
	for _, stale := range []string{"llama.dll", "ggml.dll", "ggml-cuda.dll"} {
		if _, err := os.Stat(filepath.Join(newDir, stale)); err == nil {
			t.Fatalf("stale %s must never enter the new package (0xC0000139 contract)", stale)
		}
	}

	// Allowlisted companions must have moved.
	for _, kept := range []string{"shtn-engine-host.exe", "LICENSE", "README.md"} {
		if _, err := os.Stat(filepath.Join(newDir, kept)); err != nil {
			t.Fatalf("companion %s must survive the package swap: %v", kept, err)
		}
	}

	// The new package's own files stay untouched.
	if b, err := os.ReadFile(filepath.Join(newDir, "ggml-base.dll")); err != nil || string(b) != "NEW BASE DLL" {
		t.Fatal("the new package's own DLL must never be replaced")
	}

	// The dropped list reports the stale DLLs (diagnostics, spec §21).
	if len(dropped) < 3 {
		t.Fatalf("dropped list must report the stale DLLs, got %v", dropped)
	}
}

func TestMergeCompanionFilesDropsForeignExecutables(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()

	write(t, filepath.Join(oldDir, "quantize.exe"), "FOREIGN TOOL")
	write(t, filepath.Join(oldDir, "llama-bench.exe"), "FOREIGN TOOL")
	write(t, filepath.Join(oldDir, "server-logs.txt"), "some log") // not metadata-named → dropped

	mergeCompanionFiles(oldDir, newDir)

	for _, foreign := range []string{"quantize.exe", "llama-bench.exe", "server-logs.txt"} {
		if _, err := os.Stat(filepath.Join(newDir, foreign)); err == nil {
			t.Fatalf("foreign file %s must not survive the swap", foreign)
		}
	}
}

func TestCopyEngineClosureImportsClosureOnly(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	binRel := filepath.Join("release", "bin", "llama-server.exe")

	// The candidate's package closure.
	write(t, filepath.Join(src, "release", "bin", "llama-server.exe"), "ENGINE ENTRY")
	write(t, filepath.Join(src, "release", "bin", "llama.dll"), "RUNTIME DLL")
	write(t, filepath.Join(src, "release", "bin", "ggml.dll"), "RUNTIME DLL")
	write(t, filepath.Join(src, "release", "bin", "ggml-cuda.dll"), "DYNAMIC BACKEND DLL")
	write(t, filepath.Join(src, "release", "bin", "LICENSE"), "license")

	// Foreign neighbors that must NEVER be imported (spec §11).
	write(t, filepath.Join(src, "release", "bin", "quantize.exe"), "FOREIGN EXE")
	write(t, filepath.Join(src, "release", "bin", "model.gguf"), "FOREIGN MODEL")
	write(t, filepath.Join(src, "other-engine", "llama.dll"), "ANOTHER ENGINE'S DLL")
	write(t, filepath.Join(src, "docs", "manual.pdf"), "UNRELATED TREE")

	copied, _, err := copyEngineClosure(src, dst, binRel)
	if err != nil {
		t.Fatalf("closure copy: %v", err)
	}

	// Exactly the closure: entry exe + 3 runtime DLLs + LICENSE.
	if copied != 5 {
		t.Fatalf("closure copy must import exactly 5 files, got %d", copied)
	}

	for _, must := range []string{
		filepath.Join("release", "bin", "llama-server.exe"),
		filepath.Join("release", "bin", "llama.dll"),
		filepath.Join("release", "bin", "ggml.dll"),
		filepath.Join("release", "bin", "ggml-cuda.dll"),
		filepath.Join("release", "bin", "LICENSE"),
	} {
		if _, err := os.Stat(filepath.Join(dst, must)); err != nil {
			t.Fatalf("closure file %s missing: %v", must, err)
		}
	}

	for _, foreign := range []string{
		filepath.Join("release", "bin", "quantize.exe"),
		filepath.Join("release", "bin", "model.gguf"),
		filepath.Join("other-engine", "llama.dll"),
		filepath.Join("docs", "manual.pdf"),
	} {
		if _, err := os.Stat(filepath.Join(dst, foreign)); err == nil {
			t.Fatalf("foreign file %s must never be imported", foreign)
		}
	}
}

func TestCopyEngineClosureFlatPackage(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// A flat candidate (exe at the root — the common release layout).
	write(t, filepath.Join(src, "llama-server"), "ENGINE")
	write(t, filepath.Join(src, "ggml-base.dll"), "DLL")
	write(t, filepath.Join(src, "subdir", "nested.dll"), "NESTED DLL")

	copied, skipped, err := copyEngineClosure(src, dst, "llama-server")
	if err != nil {
		t.Fatalf("closure copy: %v", err)
	}

	if copied != 2 {
		t.Fatalf("flat closure = exe + beside DLLs → 2 files, got %d (skipped %d)", copied, skipped)
	}

	if _, err := os.Stat(filepath.Join(dst, "subdir", "nested.dll")); err == nil {
		t.Fatal("nested foreign DLLs must not be imported")
	}
}

func TestIsCompanionFileAllowlist(t *testing.T) {
	allowed := []string{
		"shtn-engine-host.exe", "shtn-engine-host", "shtn-engine-host-dbg.exe",
		"LICENSE", "LICENSE.txt", "README", "README.md", "NOTICE",
		"CHANGELOG.md",
	}
	for _, name := range allowed {
		if !isCompanionFile(name) {
			t.Fatalf("%s must be an allowlisted companion", name)
		}
	}

	denied := []string{
		"llama.dll", "ggml.dll", "ggml-cuda.dll", "cudart64_12.dll",
		"quantize.exe", "llama-bench.exe", "server.log", "model.gguf", "script.py",
	}
	for _, name := range denied {
		if isCompanionFile(name) {
			t.Fatalf("%s must NEVER be an allowlisted companion", name)
		}
	}
}
