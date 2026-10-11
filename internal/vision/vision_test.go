package vision

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x+y)%2 == 0 {
				img.Set(x, y, color.NRGBA{R: 255, G: 90, B: 38, A: 255})
			}
		}
	}
	p := filepath.Join(t.TempDir(), "img.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIsMMProjByFilename(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"mmproj-gemma-4-E2B-it-BF16.gguf", true},
		{"MMProj-model-qwen3.gguf", true},
		{"gemma-4-E2B-it-Q4_K_M.gguf", false},
		{"mmproj-gemma-4-E2B-it-BF16.bin", false},
		{"/x/y/mmproj-model.gguf", true},
		{"readme.md", false},
	}
	for _, c := range cases {
		if got := IsMMProj(c.path); got != c.want {
			t.Errorf("IsMMProj(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// A minimal GGUF header whose only kv entries are clip.* strings.
func clipGGUF(t *testing.T, keys []string) string {
	t.Helper()
	var b []byte
	b = append(b, 'G', 'G', 'U', 'F')
	b = append(b, 3, 0, 0, 0)             // version 3
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0) // tensor count 0
	b = append(b, byte(len(keys)), 0, 0, 0, 0, 0, 0, 0)
	for _, k := range keys {
		b = append(b, byte(len(k)), 0, 0, 0, 0, 0, 0, 0)
		b = append(b, k...)
		b = append(b, 8, 0, 0, 0)             // type string
		b = append(b, 1, 0, 0, 0, 0, 0, 0, 0) // value length 1
		b = append(b, 'x')
	}
	p := filepath.Join(t.TempDir(), "weird-name.gguf")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIsMMProjByContent(t *testing.T) {
	clip := clipGGUF(t, []string{"clip.has_vision_encoder", "clip.projector_type", "clip.vision.image_size"})
	if !IsMMProj(clip) {
		t.Error("GGUF with 3 clip.* keys should be detected as mmproj")
	}
	plain := clipGGUF(t, []string{"general.architecture", "general.name", "llama.context_length"})
	if IsMMProj(plain) {
		t.Error("GGUF without clip.* keys must not be detected as mmproj")
	}
}

func TestFindProjectorPairing(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"mmproj-gemma-4-E2B-it-BF16.gguf",
		"mmproj-gemma-3-4b.gguf",
		"mmproj-qwen3-vl.gguf",
		"gemma-4-E2B-it-Q4_K_M.gguf",
		"qwen3-8b.gguf",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Exact-family pairing: the E2B projector wins for the E2B model.
	got := FindProjector(dir, "gemma-4-E2B-it-Q4_K_M.gguf", "")
	if got == "" || !strings.Contains(got, "gemma-4-E2B") {
		t.Errorf("expected gemma-4-E2B projector, got %q", got)
	}
	// Different family: qwen model never pairs with a gemma projector.
	if got := FindProjector(dir, "qwen3-8b.gguf", ""); got == "" || !strings.Contains(got, "qwen") {
		t.Errorf("expected qwen projector, got %q", got)
	}
	// No projector family at all.
	if got := FindProjector(dir, "llama-3-8b.gguf", ""); got != "" {
		t.Errorf("expected no projector for llama, got %q", got)
	}
	// Explicit override wins even when pairing would disagree.
	ov := FindProjector(dir, "qwen3-8b.gguf", "mmproj-gemma-3-4b.gguf")
	if ov == "" || !strings.Contains(ov, "gemma-3") {
		t.Errorf("explicit override should win, got %q", ov)
	}
	// Missing explicit override → "" (never a silent substitute).
	if got := FindProjector(dir, "qwen3-8b.gguf", "does-not-exist.gguf"); got != "" {
		t.Errorf("missing override must return empty, got %q", got)
	}
}

func TestListProjectorsSortedAndFiltered(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"mmproj-b.gguf", "mmproj-a.gguf", "model.gguf", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := ListProjectors(dir)
	if len(got) != 2 || !strings.HasSuffix(got[0], "mmproj-a.gguf") || !strings.HasSuffix(got[1], "mmproj-b.gguf") {
		t.Errorf("ListProjectors = %v", got)
	}
}

func TestEncodeImageProducesDataURL(t *testing.T) {
	p := mustPNG(t, 300, 200)
	url, err := EncodeImage(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("bad prefix: %.40s", url)
	}
	if len(url) < 100 {
		t.Fatal("suspiciously tiny payload")
	}
}

func TestEncodeImageDownscalesHugePNG(t *testing.T) {
	// 3000x2400 noise → must be downscaled to ≤2048 side.
	p := mustPNG(t, 3000, 2400)
	url, err := EncodeImage(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(url) > EncodedBudget {
		t.Fatalf("payload %d exceeds budget %d", len(url), EncodedBudget)
	}
}

func TestIsImageFile(t *testing.T) {
	for p, want := range map[string]bool{
		"a.png": true, "b.JPG": true, "c.webp": true, "d.gif": true, "e.bmp": true,
		"f.txt": false, "g.md": false, "h.gguf": false, "noext": false,
	} {
		if got := IsImageFile(p); got != want {
			t.Errorf("IsImageFile(%q) = %v, want %v", p, got, want)
		}
	}
}
