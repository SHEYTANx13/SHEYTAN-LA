package render

// gogpu execution path: real render/compute jobs through the gogpu stack
// (github.com/gogpu/gogpu v0.54.1 — the module pin the repository's POC
// validated; the SAME release the product now links), with checksum
// verified readback. The software renderer is a real path of the stack
// (no display, no native adapter required); native Vulkan/Metal/D3D
// adapters stay OUT of the claims until a backend was executed and
// verified on the target platform (work order §5.3: never report GPU
// execution unless a real job was submitted, completed and verified).

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gogpu/gmath"
)

// probeSoftwareRenderer answers whether the gogpu software path can be
// created on this host — a REAL stack initialization (renderer object
// creation), not a capability guess from environment variables.
func probeSoftwareRenderer() error {
	r, err := gogpu.NewHeadlessRenderer()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRendererUnavailable, err)
	}

	defer r.Destroy()

	return nil
}

// jobFrame renders one deterministic verification frame: a 12-slot
// amplitude spectrum driven by an LCG (pure function of the job index —
// no wall clock, no seeded math/rand), so the checksum of a healthy
// backend is stable across hosts and runs.
func jobFrame(jobIndex int, width, height int) func(*gogpu.Context) {
	return func(dc *gogpu.Context) {
		dc.ClearColor(gmath.Hex(0x10141C))

		r := dc.Renderer()

		// 2x2 white source texture, scaled into bars (the POC's proven
		// draw path — one solid texture, Context.DrawTextureScaled).
		px := []byte{
			255, 255, 255, 255, 255, 255, 255, 255,
			255, 255, 255, 255, 255, 255, 255, 255,
		}
		tex, texErr := r.NewTextureFromRGBA(2, 2, px)
		if texErr != nil {
			return
		}

		defer tex.Destroy()

		const margin = float32(8)
		const gap = float32(2)
		const bars = 12

		amplitudes := jobAmplitudes(jobIndex, bars)

		bw := (float32(width) - 2*margin - gap*(bars-1)) / bars
		if bw < 1 {
			bw = 1
		}

		for i := 0; i < bars; i++ {
			a := amplitudes[i]
			if a < 0.05 {
				a = 0.05
			}

			bh := (float32(height) - 2*margin) * float32(a)
			if bh < 2 {
				bh = 2
			}

			x := margin + float32(i)*(bw+gap)
			y := float32(height) - margin - bh

			_ = dc.DrawTextureScaled(tex, x, y, bw, bh)
		}
	}
}

// jobAmplitudes is the pure LCG amplitude vector for a job index.
func jobAmplitudes(jobIndex int, bars int) []float64 {
	out := make([]float64, bars)

	x := uint64(0x9E3779B97F4A7C15) ^ (uint64(jobIndex) * 0xBF58476D1CE4E5B9)
	x |= 1

	for i := 0; i < bars; i++ {
		x = 0x5851F42D4C957F2D*x + 0x14057B7EF767814F

		out[i] = float64((x>>33)&0xFFFF) / 65535.0
	}

	return out
}

// referenceFrameRGBA is the CPU reference implementation of jobFrame:
// the same amplitude vector rasterized in pure Go. The service verifies a
// gogpu frame against this reference (structural equality of the
// non-background pixel mask) so a verification result means the backend
// actually produced the expected image.
func referenceFrameRGBA(jobIndex, width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	const margin = 8
	const gap = 2
	const bars = 12

	amplitudes := jobAmplitudes(jobIndex, bars)

	bw := (float32(width) - 2*margin - gap*(bars-1)) / bars
	if bw < 1 {
		bw = 1
	}

	// Background (matches gmath.Hex(0x10141C)).
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, bgRGBA)
		}
	}

	white := imageNewRGBAColor(255, 255, 255, 255)

	for i := 0; i < bars; i++ {
		a := amplitudes[i]
		if a < 0.05 {
			a = 0.05
		}

		bh := (float32(height) - 2*margin) * float32(a)
		if bh < 2 {
			bh = 2
		}

		x0 := int(margin + float32(i)*(bw+gap))
		x1 := int(margin + float32(i)*(bw+gap) + bw)
		y0 := height - margin - int(bh)
		y1 := height - margin

		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if x >= 0 && x < width && y >= 0 && y < height {
					img.SetRGBA(x, y, white)
				}
			}
		}
	}

	return img.Pix
}

// draw.Copy is referenced to keep the image/draw import honest for the
// CPU rasterizer's future blit paths.
var _ = draw.Draw
