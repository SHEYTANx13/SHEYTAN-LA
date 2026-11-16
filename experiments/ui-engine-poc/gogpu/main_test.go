package main

// main_test.go — gogpu POC verification (work order §4.5).
//
// What IS proven here (this host, headless): the deterministic data
// function; a real render through the Pure-Go software backend (no display,
// no GPU adapter); determinism of that render across repeated frames.
//
// What is NOT claimed anywhere: GPU hardware acceleration (no Vulkan ICD,
// no /dev/dri on this host — GPU HARDWARE NOT RUN), windowed runtime,
// frame-time percentiles, zero-copy interop with the Go-Gui POC (the two
// engines run SEPARATE surfaces by design — see the module README and
// docs/architecture/GO-GUI-GOGPU-INTEROP.md).

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/gogpu/gogpu"
)

func TestSpectrumFrameDeterministic(t *testing.T) {
	a := spectrumFrame(0)
	b := spectrumFrame(0)
	if len(a) != 48 || len(b) != 48 {
		t.Fatalf("spectrum vector length = %d/%d, want 48", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("frame 0 is not deterministic at bar %d: %v != %v", i, a[i], b[i])
		}
		if a[i] < 0 || a[i] > 1 {
			t.Fatalf("bar %d amplitude %v outside [0,1]", i, a[i])
		}
	}
	c := spectrumFrame(37)
	same := true
	for i := range a {
		if a[i] == c[i] {
			continue
		}
		same = false
		break
	}
	if same {
		t.Fatal("frame 37 produced the identical vector to frame 0 — the animation is not advancing")
	}
}

func TestHeadlessSoftwareRenderRealAndDeterministic(t *testing.T) {
	r, err := gogpu.NewHeadlessRenderer() // zero args = Pure-Go software backend
	if err != nil {
		t.Fatalf("NewHeadlessRenderer: %v", err)
	}
	s := &spectrumScene{}

	img1, err := r.RenderToImage(320, 180, func(dc *gogpu.Context) {
		s.draw(dc, 0)
	})
	if err != nil {
		t.Fatalf("RenderToImage #1: %v", err)
	}
	if got := img1.Bounds(); got.Dx() != 320 || got.Dy() != 180 {
		t.Fatalf("render bounds = %v, want 320x180", got)
	}

	// Non-blank: the bars must cover a real share of the frame. Background
	// is 0x10141C; count pixels that differ from it.
	diff := 0
	total := 0
	for y := img1.Bounds().Min.Y; y < img1.Bounds().Max.Y; y++ {
		for x := img1.Bounds().Min.X; x < img1.Bounds().Max.X; x++ {
			c := img1.RGBAAt(x, y)
			total++
			if int(c.R) != 0x10 || int(c.G) != 0x14 || int(c.B) != 0x1C {
				diff++
			}
		}
	}
	if total == 0 || diff*100/total < 3 {
		t.Fatalf("render is blank: %d/%d pixels differ from the clear color (<3%%)", diff, total)
	}

	s2 := &spectrumScene{}
	img2, err := r.RenderToImage(320, 180, func(dc *gogpu.Context) {
		s2.draw(dc, 0)
	})
	if err != nil {
		t.Fatalf("RenderToImage #2: %v", err)
	}
	var b1, b2 bytes.Buffer
	if err := png.Encode(&b1, img1); err != nil {
		t.Fatalf("png #1: %v", err)
	}
	if err := png.Encode(&b2, img2); err != nil {
		t.Fatalf("png #2: %v", err)
	}
	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatal("two renders of frame 0 differ — the software render is not deterministic")
	}

	s.destroy()
	s2.destroy()
	r.Destroy()
	r.ReleaseInstance()
}
