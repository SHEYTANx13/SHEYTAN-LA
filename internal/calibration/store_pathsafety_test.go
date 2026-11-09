package calibration

// store_pathsafety_test.go — v1.11.1 path-injection remediation (alert
// at ModelKey's os.Stat): the model path is a user-selected file (a
// desktop install may point at any local GGUF — the resolved selection
// travels through llm.ResolveModelPath), so it is a user-owned absolute
// path class sink: the hostile spellings FAIL CLOSED before the Stat —
// empty paths and NUL bytes are rejected — and the fingerprint is always
// taken over the cleaned ABSOLUTE form. These tests also prove a real
// model file still fingerprints deterministically.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelKeyHostilePathsFailClosed(t *testing.T) {
	for _, path := range []string{
		"",                   // empty
		"   ",                // whitespace
		"model\x00evil.gguf", // NUL
	} {
		if key, err := ModelKey(path); err == nil {
			t.Errorf("ModelKey(%q) must fail closed, got key %q", path, key)
		}
	}
}

// TestModelKeyFingerprintsCleanAbsoluteForm pins the positive path and
// the normalization contract: the key always carries the cleaned
// absolute form, so a relative spelling of the same file fingerprints
// identically (no CWD-dependent divergence).
func TestModelKeyFingerprintsCleanAbsoluteForm(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(model, []byte("fake gguf bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	mk, err := ModelKey(model)
	if err != nil {
		t.Fatalf("ModelKey(%q): %v", model, err)
	}
	if !strings.HasPrefix(mk, model+"|") {
		t.Fatalf("key %q must start with the cleaned absolute path", mk)
	}

	// A dot-path spelling of the same file resolves to the same key.
	dot := filepath.Join(dir, ".", "model.gguf")
	mk2, err := ModelKey(dot)
	if err != nil {
		t.Fatalf("ModelKey(%q): %v", dot, err)
	}
	if mk != mk2 {
		t.Fatalf("same file must fingerprint identically: %q vs %q", mk, mk2)
	}
}

// TestModelKeyMissingFileIsAnError keeps the old honest behavior: a
// path that does not exist yields an error, never a fabricated key.
func TestModelKeyMissingFileIsAnError(t *testing.T) {
	if _, err := ModelKey(filepath.Join(t.TempDir(), "missing.gguf")); err == nil {
		t.Fatal("a missing model file must yield an error")
	}
}
