//go:build !headless

package goguiui

// renderBridge connects the internal/render service (GoGPU execution
// path) to the go-gui shell through the toolkit's DOCUMENTED live-frame
// surface: gui.NewPixelSurface + ImageCfg.Surface (run 44457547289 §5.2).
// This is the bounded integration fallback the work order names — an
// app-owned surface fed from a goroutine — not a zero-copy interop claim.
//
// Boundedness contract (§5.2): at most one frame in flight, an explicit
// publish cap for low-frequency surfaces, no full-GPU-frame readback on
// every display frame (a frame is produced ONLY when Publish is called),
// and the UI thread is never blocked (PixelSurface.Update is
// documented safe for concurrent use).

import (
	"fmt"
	"image"
	"image/color"
	"sync"
	"sync/atomic"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderstate"
	"github.com/go-gui-org/go-gui/gui"
)

// renderBridge owns one PixelSurface and the render service link.
type renderBridge struct {
	surface *gui.PixelSurface
	width   int
	height  int

	publishing atomic.Bool // one frame in flight
	frames     atomic.Uint64
	dropped    atomic.Uint64

	lastErrMu sync.Mutex
	lastErr   string
}

func newRenderBridge(width, height int) *renderBridge {
	s, err := gui.NewPixelSurface(gui.PixelSurfaceCfg{Width: width, Height: height})
	if err != nil {
		// A surface of sane size cannot fail; an error here is a real
		// toolkit limitation and is surfaced by the status card.
		return &renderBridge{lastErr: fmt.Sprintf("pixel surface: %v", err)}
	}

	return &renderBridge{surface: s, width: width, height: height}
}

// Surface exposes the toolkit surface for the Image widget.
func (b *renderBridge) Surface() *gui.PixelSurface {
	if b == nil {
		return nil
	}

	return b.surface
}

// PublishOne renders ONE frame (jobIndex selects the deterministic
// content) and pushes it into the surface. Non-blocking: a publish while
// another is in flight is dropped and counted — the preview stays
// low-frequency by contract.
func (b *renderBridge) PublishOne(jobIndex int) {
	if b == nil || b.surface == nil {
		return
	}

	if !b.publishing.CompareAndSwap(false, true) {
		b.dropped.Add(1)

		return
	}

	go func() {
		defer b.publishing.Store(false)

		// The gogpu render path lives in builds that link the gogpu stack
		// (the fakecgo collision keeps it out of THIS binary); the preview
		// here publishes the CPU rasterization of the same deterministic
		// frame so the surface is real without pretending to be GPU.
		pix := renderReferenceFrame(jobIndex, b.width, b.height)

		if err := b.surface.Update(pix, b.width*4); err != nil {
			b.setErr(err.Error())

			return
		}

		b.frames.Add(1)
		b.setErr("")
	}()
}

// StatusText renders the honest render-service observation for the
// Engine Center status card, from the toolkit-neutral channel.
func (b *renderBridge) StatusText() string {
	snap := renderstate.Latest()

	line := fmt.Sprintf("GPU stack: state=%s backend=%s jobs=%d verified=%d failed=%d",
		snap.State, snap.Backend, snap.JobsSubmitted, snap.JobsVerified, snap.JobsFailed)

	if snap.FallbackReason != "" {
		line += " fallback=" + snap.FallbackReason
	}

	if !renderstate.Wired() {
		line += " [" + renderstate.ErrNotWired.Error() + "]"
	}

	if b != nil && b.lastErr != "" {
		line += " surface=" + b.lastErr
	}

	return line
}

func (b *renderBridge) setErr(msg string) {
	b.lastErrMu.Lock()
	b.lastErr = msg
	b.lastErrMu.Unlock()
}

// renderReferenceFrame is the presentation-side rasterization of the SAME
// deterministic frame content the render service verifies (LCG amplitudes
// -> 12 bars). It exists here so the preview surface shows real pixels in
// binaries that cannot link the gogpu stack; the verification authority
// remains internal/render (checksum + profile against its own reference).
func renderReferenceFrame(jobIndex, width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	const (
		margin = float32(8)
		gap    = float32(2)
		bars   = 12
	)

	// Background 0x10141C.
	bg := color.NRGBA{R: 0x10, G: 0x14, B: 0x1C, A: 255}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, bg)
		}
	}

	amps := renderAmplitudes(jobIndex, bars)

	bw := (float32(width) - 2*margin - gap*(bars-1)) / bars
	if bw < 1 {
		bw = 1
	}

	for i := 0; i < bars; i++ {
		a := amps[i]
		if a < 0.05 {
			a = 0.05
		}

		bh := (float32(height) - 2*margin) * float32(a)
		if bh < 2 {
			bh = 2
		}

		x0 := int(margin + float32(i)*(bw+gap))
		x1 := int(margin + float32(i)*(bw+gap) + bw)
		y0 := height - int(margin) - int(bh)
		y1 := height - int(margin)

		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if x >= 0 && x < width && y >= 0 && y < height {
					img.SetNRGBA(x, y, white)
				}
			}
		}
	}

	return img.Pix
}

// renderAmplitudes is the deterministic LCG amplitude vector (the same
// constant schedule internal/render verifies against).
func renderAmplitudes(jobIndex, bars int) []float64 {
	out := make([]float64, bars)

	x := uint64(0x9E3779B97F4A7C15) ^ (uint64(jobIndex) * 0xBF58476D1CE4E5B9)
	x |= 1

	for i := 0; i < bars; i++ {
		x = 0x5851F42D4C957F2D*x + 0x14057B7EF767814F

		out[i] = float64((x>>33)&0xFFFF) / 65535.0
	}

	return out
}
