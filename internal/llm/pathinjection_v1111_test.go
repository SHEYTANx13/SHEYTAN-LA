package llm

// pathinjection_v1111_test.go — v1.11.1 CodeQL go/path-injection repair:
// adversarial coverage for the two model-path classes.
//
//   - USER-SELECTED external paths (import source, model refs, image
//     attachments) keep their capability but are validated at the point
//     of use: absolute (once resolved), NUL-free, never reassembled from
//     untrusted components;
//   - APP-MANAGED model storage (the models root) is only ever written
//     through the managedModelPath chokepoint: hostile names, traversal,
//     NUL bytes and Windows-reserved names produce errors — never a path
//     outside the models root — and a refused malicious operation leaves
//     files outside the models root untouched.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// TestManagedModelPathChokepoint proves managedModelPath accepts ordinary
// model names INSIDE the models root and fails closed on every hostile
// spelling: traversal, separators, NUL, colons, dotfiles, trailing
// dot/space, control characters, Windows-reserved devices, oversized names.
func TestManagedModelPathChokepoint(t *testing.T) {
	modelsDir := t.TempDir()

	ok, err := managedModelPath(modelsDir, "Tiny-Llama-1.1B-Q4_K_M (1).gguf")
	if err != nil {
		t.Fatalf("ordinary model name must be accepted: %v", err)
	}
	if rel, relErr := filepath.Rel(modelsDir, ok); relErr != nil ||
		rel != "Tiny-Llama-1.1B-Q4_K_M (1).gguf" {
		t.Fatalf("managed path escaped the models root: %q (rel=%q err=%v)", ok, rel, relErr)
	}

	hostile := []string{
		"",
		".",
		"..",
		".hidden.gguf",
		".import.lock",
		"../evil.gguf",
		"sub/dir.gguf",
		`sub\dir.gguf`,
		"evil\x00.gguf",
		"drive:C.gguf",
		"trail.gguf ",
		"trail.gguf.",
		"   ",
		"model\x07.gguf",
		"model\x1b.gguf",
		"CON.gguf",
		"nul.gguf",
		"AUX.gguf",
		"PRN.gguf",
		"COM1.gguf",
		"lpt9.gguf",
		"con.x.gguf",
		strings.Repeat("x", 256) + ".gguf",
	}

	for _, name := range hostile {
		if got, err := managedModelPath(modelsDir, name); err == nil {
			t.Fatalf("hostile model name %q must be rejected, got %q", name, got)
		}
	}
}

// TestNextFreeModelNameStaysInsideModelsRoot: the collision search joins
// every candidate through the chokepoint — a hostile base name fails the
// whole search closed instead of producing any path.
func TestNextFreeModelNameStaysInsideModelsRoot(t *testing.T) {
	modelsDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(modelsDir, "collide-1.gguf"), []byte("taken"), 0o644); err != nil {
		t.Fatal(err)
	}

	name, path, err := nextFreeModelName(modelsDir, "collide.gguf")
	if err != nil {
		t.Fatalf("collision search: %v", err)
	}
	if name != "collide-2.gguf" || path != filepath.Join(modelsDir, name) {
		t.Fatalf("collision name = %q path = %q", name, path)
	}
	if rel, relErr := filepath.Rel(modelsDir, path); relErr != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("collision path escaped the models root: %q", path)
	}

	name, path, err = nextFreeModelName(modelsDir, "../evil.gguf")
	if err == nil {
		t.Fatalf("hostile base must fail the search closed, got name=%q path=%q", name, path)
	}
	if path != "" {
		t.Fatalf("a failed collision search must not produce a path, got %q", path)
	}
}

// TestImportModelRefusesNULSourceAndLeavesOutsideUntouched: a NUL-bearing
// source is refused at the point of use, before any directory is created,
// and files outside the models root are untouched.
func TestImportModelRefusesNULSourceAndLeavesOutsideUntouched(t *testing.T) {
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("DO NOT TOUCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}

	modelsDir := filepath.Join(outside, "models")

	for _, src := range []string{"bad\x00name.gguf", "a\x00", "\x00"} {
		_, err := ImportModel(modelsDir, src, nil)
		if err == nil {
			t.Fatalf("NUL source %q must be refused", src)
		}
		if !strings.Contains(err.Error(), "NUL") {
			t.Fatalf("NUL refusal must name the defect, got: %v", err)
		}
	}

	after, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused import modified a file outside the models root")
	}
	if _, statErr := os.Stat(modelsDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a refused import must not create the models dir, stat err=%v", statErr)
	}
}

// TestImportModelRefusesHostileManagedNames: a REAL file whose name would
// be unsafe inside managed storage (reserved device, dotfile, trailing
// space) is refused end-to-end through the public API — the models root
// receives nothing.
func TestImportModelRefusesHostileManagedNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hostile file names (trailing space, reserved devices) cannot exist on Windows")
	}

	srcDir := t.TempDir()
	modelsDir := filepath.Join(t.TempDir(), "models")

	for _, base := range []string{"nul.gguf", ".hidden-import.gguf", "trail.gguf ", "con.x.gguf"} {
		src := filepath.Join(srcDir, base)
		writeTestGGUF(t, src, "HOSTILE0", 32<<10)

		if _, err := ImportModel(modelsDir, src, nil); err == nil {
			t.Fatalf("hostile managed name %q must be refused", base)
		}
	}

	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gguf") {
			t.Fatalf("a refused import placed a model in the models root: %s", e.Name())
		}
	}
}

// TestImportModelSourceValidationAtPointOfUse: the external-source
// capability is kept (a readable absolute path still imports; a relative
// path is still resolved against the CWD), but validation runs first.
func TestImportModelSourceValidationAtPointOfUse(t *testing.T) {
	modelsDir := filepath.Join(t.TempDir(), "models")

	if _, err := ImportModel(modelsDir, filepath.Join("does", "not", "exist.gguf"), nil); err == nil {
		t.Fatal("missing source must fail")
	} else if !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("unexpected refusal: %v", err)
	}

	if _, err := ImportModel(modelsDir, "", nil); err == nil {
		t.Fatal("empty source must fail")
	}
}

// TestReadModelCardRefusesUnsafePaths: the GGUF reader fails closed on
// empty and NUL-contaminated paths.
func TestReadModelCardRefusesUnsafePaths(t *testing.T) {
	if _, err := ReadModelCard(""); err == nil {
		t.Fatal("empty model path must be refused")
	}
	_, err := ReadModelCard("bad\x00path.gguf")
	if err == nil {
		t.Fatal("NUL model path must be refused")
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL refusal must name the defect, got: %v", err)
	}
}

// TestResolveModelCapabilitiesRefusesUnsafePaths: an unusable model path
// degrades to nil ("unknown model"), never to a file-system probe.
func TestResolveModelCapabilitiesRefusesUnsafePaths(t *testing.T) {
	cfg := config.Default()

	if caps := ResolveModelCapabilities(cfg, ""); caps != nil {
		t.Fatal("empty model path must yield nil caps")
	}
	if caps := ResolveModelCapabilities(cfg, "bad\x00path.gguf"); caps != nil {
		t.Fatal("NUL model path must yield nil caps")
	}
	if caps := ResolveModelCapabilities(cfg, filepath.Join(t.TempDir(), "nope.gguf")); caps != nil {
		t.Fatal("missing model path must yield nil caps")
	}
}

// TestResolveModelPathHardening: NUL-bearing refs fail closed, relative
// names are NEVER resolved against the process CWD anymore (the old
// filepath.Abs(name) escape hatch), and the absolute-path capability is
// kept intact.
func TestResolveModelPathHardening(t *testing.T) {
	dir := t.TempDir() // an empty models dir

	_, err := ResolveModelPath(dir, "bad\x00model.gguf")
	if err == nil {
		t.Fatal("NUL model ref must be refused")
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL refusal must name the defect, got: %v", err)
	}

	// A relative name that EXISTS relative to the CWD must not resolve.
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.WriteFile(filepath.Join(cwd, "cwdmodel.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if path, err := ResolveModelPath(dir, "cwdmodel.gguf"); err == nil {
		t.Fatalf("relative name must not resolve against the CWD, got %q", path)
	}

	// The explicit absolute-path capability is kept.
	outside := filepath.Join(cwd, "outside.gguf")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := ResolveModelPath(dir, outside)
	if err != nil {
		t.Fatalf("absolute model path must still resolve: %v", err)
	}
	if filepath.Clean(path) != filepath.Clean(outside) {
		t.Fatalf("absolute resolve returned %q, want %q", path, outside)
	}
}

// TestImageDataURLPathValidation: the vision attachment reader keeps the
// absolute-path capability but refuses empty, relative and NUL paths, and
// still encodes a real image.
func TestImageDataURLPathValidation(t *testing.T) {
	c := &Client{}

	if _, err := c.imageDataURL(""); err == nil {
		t.Fatal("empty image path must be refused")
	}
	if _, err := c.imageDataURL("relative/pic.png"); err == nil {
		t.Fatal("relative image path must be refused")
	}
	_, err := c.imageDataURL("bad\x00pic.png")
	if err == nil {
		t.Fatal("NUL image path must be refused")
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL refusal must name the defect, got: %v", err)
	}

	// Capability: a real image at an absolute path still encodes.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	pic := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(pic, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	url, err := c.imageDataURL(pic)
	if err != nil {
		t.Fatalf("real image must still encode: %v", err)
	}
	if !strings.HasPrefix(url, "data:image/") {
		t.Fatalf("unexpected data URL prefix: %q", url[:min(len(url), 32)])
	}
}
