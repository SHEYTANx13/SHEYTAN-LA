package llm

// importmodel_test.go — v1.6.1: the first-class GGUF import, proven with
// the REAL in-repo GGUF fixture (native/engine/tests/fixtures/
// tiny-llama-f32.gguf — a genuine llama/F32 model file, not a fake).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realGGUFFixture returns the absolute path of the in-repo real GGUF
// fixture (llama arch, F32, 85 KB). Tests skip loudly when the fixture is
// absent — never fake a pass.
func realGGUFFixture(t *testing.T) string {
	t.Helper()

	// The fixture lives at <repo>/native/engine/tests/fixtures/.
	candidates := []string{
		filepath.Join("..", "..", "native", "engine", "tests", "fixtures", "tiny-llama-f32.gguf"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}

	t.Skip("real GGUF fixture (native/engine/tests/fixtures/tiny-llama-f32.gguf) not present in this checkout")
	return ""
}

// TestImportModelRealGGUF proves the full import contract against a REAL
// GGUF file: header validation, streaming copy, atomic placement, byte
// identity, and an intact source.
func TestImportModelRealGGUF(t *testing.T) {
	src := realGGUFFixture(t)

	external := t.TempDir() // the user's "Downloads folder" stand-in
	srcCopy := filepath.Join(external, "tiny-llama-f32.gguf")
	if err := copyFileForTest(t, src, srcCopy); err != nil {
		t.Fatal(err)
	}

	modelsDir := filepath.Join(t.TempDir(), "models")

	var progressCalls int
	var lastCopied, lastTotal int64

	result, err := ImportModel(modelsDir, srcCopy, func(copied, total int64) {
		progressCalls++
		lastCopied, lastTotal = copied, total
	})
	if err != nil {
		t.Fatalf("import real GGUF: %v", err)
	}

	if result.Duplicate {
		t.Fatal("first import must not be a duplicate")
	}
	if result.Name != "tiny-llama-f32.gguf" {
		t.Fatalf("imported name = %q", result.Name)
	}
	if result.Path != filepath.Join(modelsDir, "tiny-llama-f32.gguf") {
		t.Fatalf("imported path = %q", result.Path)
	}

	// Byte identity: the imported copy is the source, bit for bit.
	same, err := sameContent(srcCopy, result.Path)
	if err != nil || !same {
		t.Fatalf("imported copy must be byte-identical to the source (same=%t err=%v)", same, err)
	}

	// Progress reporting fired and finished at the full size.
	if progressCalls == 0 {
		t.Fatal("streaming copy must report progress")
	}
	if lastCopied != lastTotal || lastTotal != result.SizeBytes {
		t.Fatalf("progress must reach the full size: copied=%d total=%d size=%d",
			lastCopied, lastTotal, result.SizeBytes)
	}

	// The parsed card carries the real fixture facts.
	if result.Card == nil || result.Card.Arch != "llama" || result.Card.Quant != "F32" {
		t.Fatalf("imported card must carry the real GGUF facts, got %+v", result.Card)
	}

	// The SOURCE is preserved verbatim (external paths stay the user's).
	if _, err := os.Stat(srcCopy); err != nil {
		t.Fatalf("source must never be removed: %v", err)
	}

	// No staging artifacts remain.
	entries, _ := os.ReadDir(modelsDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".import-") {
			t.Fatalf("staging artifact leaked into the models dir: %s", e.Name())
		}
	}
}

// TestImportModelDuplicateHandling: importing the identical file twice
// reports a duplicate and never rewrites anything.
func TestImportModelDuplicateHandling(t *testing.T) {
	src := realGGUFFixture(t)

	external := t.TempDir()
	srcCopy := filepath.Join(external, "dup.gguf")
	if err := copyFileForTest(t, src, srcCopy); err != nil {
		t.Fatal(err)
	}

	modelsDir := filepath.Join(t.TempDir(), "models")

	first, err := ImportModel(modelsDir, srcCopy, nil)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}

	second, err := ImportModel(modelsDir, srcCopy, nil)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}

	if !second.Duplicate {
		t.Fatal("identical re-import must be reported as a duplicate")
	}
	if second.Path != first.Path {
		t.Fatalf("duplicate must point at the existing file: %q vs %q", second.Path, first.Path)
	}
}

// TestImportModelRenamesDifferentCollision: a DIFFERENT file with the same
// name gets a fresh suffixed name — the existing model is never overwritten.
func TestImportModelRenamesDifferentCollision(t *testing.T) {
	src := realGGUFFixture(t)

	external := t.TempDir()
	srcCopy := filepath.Join(external, "collide.gguf")
	if err := copyFileForTest(t, src, srcCopy); err != nil {
		t.Fatal(err)
	}

	modelsDir := filepath.Join(t.TempDir(), "models")

	if _, err := ImportModel(modelsDir, srcCopy, nil); err != nil {
		t.Fatal(err)
	}

	// A different "model" under the same name: mutate one byte of a second
	// copy of the fixture.
	other := filepath.Join(external, "other.gguf")
	if err := copyFileForTest(t, src, other); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(other)
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(other, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, srcCopy); err != nil {
		t.Fatal(err)
	}

	result, err := ImportModel(modelsDir, srcCopy, nil)
	if err != nil {
		t.Fatalf("collision import: %v", err)
	}

	if result.Duplicate {
		t.Fatal("different content must not be classified as a duplicate")
	}
	if result.RenamedFrom != "collide.gguf" {
		t.Fatalf("renamedFrom = %q", result.RenamedFrom)
	}
	if result.Name != "collide-1.gguf" {
		t.Fatalf("collision must rename to collide-1.gguf, got %q", result.Name)
	}

	// The ORIGINAL file is untouched (still the real fixture bytes).
	same, _ := sameContent(src, filepath.Join(modelsDir, "collide.gguf"))
	if !same {
		t.Fatal("the pre-existing model must never be overwritten")
	}
}

// TestImportModelRejectsInvalidSources: the error surface is actionable.
func TestImportModelRejectsInvalidSources(t *testing.T) {
	modelsDir := filepath.Join(t.TempDir(), "models")

	cases := []struct {
		name  string
		setup func(t *testing.T) string
		want  string
	}{
		{
			name: "empty path",
			setup: func(t *testing.T) string { return "" },
			want:  "empty",
		},
		{
			name: "missing file",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nope.gguf")
			},
			want: "not reachable",
		},
		{
			name: "directory",
			setup: func(t *testing.T) string {
				return t.TempDir() + ".gguf"
			},
			want: "directory",
		},
		{
			name: "not a gguf extension",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "model.txt")
				_ = os.WriteFile(p, []byte("junk"), 0o644)
				return p
			},
			want: "not a .gguf file",
		},
		{
			name: "corrupt gguf payload",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "fake.gguf")
				_ = os.WriteFile(p, []byte("this is definitely not a gguf file payload"), 0o644)
				return p
			},
			want: "GGUF validation",
		},
		{
			name: "truncated gguf header",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "trunc.gguf")
				_ = os.WriteFile(p, []byte("GGUF"), 0o644) // magic and nothing else
				return p
			},
			want: "GGUF validation",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.setup(t)

			result, err := ImportModel(modelsDir, src, nil)
			if err == nil {
				t.Fatalf("must be rejected, got result %+v", result)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error must mention %q, got: %v", tc.want, err)
			}

			// Nothing was placed in the models directory.
			if entries, _ := os.ReadDir(modelsDir); len(entries) > 0 {
				t.Fatalf("rejected import must leave the models dir empty, found %d entries", len(entries))
			}
		})
	}
}

func copyFileForTest(t *testing.T, src, dst string) error {
	t.Helper()

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
