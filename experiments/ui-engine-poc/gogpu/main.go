// gogpu POC — v1.11.1 hybrid-native-UI milestone (work order §4.5).
// POC_ONLY. NOT PRODUCTION_READY.
//
// An isolated, nested Go module (the production module does not depend on
// it) exercising the GPU graphics-surface candidate:
//
//      github.com/gogpu/gogpu v0.54.1
//        tag refs/tags/v0.54.1 @ 01b7d29ce5678a034483bcaee6c6b5822110a08a
//        tagged 2026-11-08 (module proxy, verified 2026-11-16)
//      module graph pins (go.mod, NOT hand-forced):
//        github.com/gogpu/wgpu    v0.34.9   (latest standalone tag is
//                                            v0.34.15; overriding the
//                                            gogpu-required pin would be
//                                            an unaudited change — refused)
//        github.com/gogpu/gputypes v0.8.2
//        github.com/gogpu/naga     v0.21.4
//
// The slice: a deterministic spectrum visualization. Bar amplitudes are a
// pure function of the frame index (an LCG — no time source, no seeded
// math/rand), rendered as positioned bars through one solid texture and
// Context.DrawTextureScaled. Two execution paths:
//
//   - default: the real windowed App (gogpu owns its own native window —
//     work order §4.5.5: a SEPARATE surface, never an embedded bridge into
//     the Go-Gui workspace; no zero-copy or GPU-interop claim is made);
//   - -screenshot path.png: one headless frame through the Pure-Go
//     software backend (gogpu.NewHeadlessRenderer) — no display, no GPU
//     adapter required;
//   - -frames N: a bounded soak that closes the app after N frames.
package main

import (
        "flag"
        "fmt"
        "image"
        "image/color"
        "image/png"
        "log"
        "math"
        "os"

        "github.com/gogpu/gogpu"
        "github.com/gogpu/gogpu/gmath"
)

const (
        sceneWidth  = 960
        sceneHeight = 600
)

// spectrumFrame returns 48 deterministic bar amplitudes in [0,1] for frame
// index t. Pure function of t: the same frame always renders the same
// spectrum on every host (the golden determinism the tests pin).
func spectrumFrame(t int) []float64 {
        const bars = 48
        out := make([]float64, bars)
        x := uint64(0x9E3779B97F4A7C15) ^ (uint64(t) * 0xBF58476D1CE4E5B9)
        x |= 1
        for i := 0; i < bars; i++ {
                x = 0x5851F42D4C957F2D*x + 0x14057B7EF767814F
                v := float64((x>>11)&0xFFFF) / 65535.0
                env := 0.35 + 0.65*math.Abs(math.Sin(float64(i)/7.0+float64(t)/9.0))
                out[i] = math.Min(1.0, v*env)
        }
        return out
}

// spectrumScene owns the one GPU texture of the slice and draws one frame.
// The texture is created lazily on first draw (the renderer exists only
// then — the upstream texture example's pattern) and destroyed explicitly
// on close (work order §4.5.2: explicit resource cleanup).
type spectrumScene struct {
        tex    *gogpu.Texture
        texErr error
}

func (s *spectrumScene) ensureTex(r *gogpu.Renderer) {
        if s.tex != nil || s.texErr != nil {
                return
        }
        // 2x2 opaque white: bars are this texture scaled per frame.
        px := []byte{255, 255, 255, 255, 255, 255, 255, 255,
                255, 255, 255, 255, 255, 255, 255, 255}
        s.tex, s.texErr = r.NewTextureFromRGBA(2, 2, px)
}

func (s *spectrumScene) draw(dc *gogpu.Context, frame int) {
        dc.ClearColor(gmath.Hex(0x10141C))
        r := dc.Renderer()
        s.ensureTex(r)
        if s.texErr != nil {
                log.Printf("spectrum: texture unavailable, frame not drawn: %v", s.texErr)
                return
        }
        if s.tex == nil {
                return
        }

        w, h := dc.FramebufferSize()
        if w < 80 || h < 80 {
                return // degenerate window: draw nothing rather than divide by zero
        }
        const margin = float32(24)
        const gap = float32(6)
        const minBar = float32(8)
        // Bar count adapts to width so the layout is valid at every size the
        // tests render; the DATA function stays a fixed 48-slot vector.
        n := int((float32(w) - 2*margin + gap) / (minBar + gap))
        if n > 48 {
                n = 48
        }
        if n < 4 {
                n = 4
        }
        amp := spectrumFrame(frame)
        bw := (float32(w) - 2*margin - gap*float32(n-1)) / float32(n)
        if bw < 1 {
                bw = 1
        }
        for i := 0; i < n; i++ {
                a := amp[i]
                if a < 0.05 {
                        a = 0.05 // keep every bar visible: no zero-area widgets/bars
                }
                bh := (float32(h) - 2*margin) * float32(a)
                if bh < 4 {
                        bh = 4
                }
                x := margin + float32(i)*(bw+gap)
                y := float32(h) - margin - bh
                if err := dc.DrawTextureScaled(s.tex, x, y, bw, bh); err != nil {
                        log.Printf("spectrum bar %d: %v", i, err)
                        return
                }
        }
}

func (s *spectrumScene) destroy() {
        if s.tex != nil {
                s.tex.Destroy()
                s.tex = nil
        }
}

func main() {
        screenshot := flag.String("screenshot", "", "render ONE deterministic frame through the headless Pure-Go software backend to this PNG path and exit")
        frames := flag.Int("frames", 0, "windowed mode: close the app after N frames (0 = run until the user closes)")
        flag.Parse()

        if *screenshot != "" {
                r, err := gogpu.NewHeadlessRenderer() // zero args = software backend
                if err != nil {
                        log.Fatalf("headless renderer: %v", err)
                }
                s := &spectrumScene{}
                img, err := r.RenderToImage(sceneWidth, sceneHeight, func(dc *gogpu.Context) {
                        s.draw(dc, 0)
                        s.destroy()
                })
                r.Destroy()
                r.ReleaseInstance()
                if err != nil {
                        log.Fatalf("headless render: %v", err)
                }
                f, err := os.Create(*screenshot)
                if err != nil {
                        log.Fatalf("create %s: %v", *screenshot, err)
                }
                if err := png.Encode(f, img); err != nil {
                        _ = f.Close()
                        log.Fatalf("png encode: %v", err)
                }
                if err := f.Close(); err != nil {
                        log.Fatalf("close: %v", err)
                }
                fmt.Printf("gogpu POC: headless software frame rendered to %s (%dx%d)\n", *screenshot, sceneWidth, sceneHeight)
                os.Exit(0)
        }

        app := gogpu.NewApp(gogpu.DefaultConfig().
                WithTitle("SHEYTAN-LA GoGPU spectrum POC (POC_ONLY — separate surface)").
                WithSize(sceneWidth, sceneHeight))

        s := &spectrumScene{}
        frame := 0
        app.OnDraw(func(dc *gogpu.Context) {
                s.draw(dc, frame)
                frame++
                if *frames > 0 && frame >= *frames {
                        go app.Close() // off-thread: never block the draw callback
                }
        })
        app.OnClose(func() { s.destroy() })

        if err := app.Run(); err != nil {
                log.Fatalf("gogpu POC: %v", err)
        }
        fmt.Printf("gogpu POC: windowed run completed after %d frames\n", frame)
}

// whiteImage is a tiny helper kept for future tinted-bar experiments; the
// current slice uses the raw RGBA path (NewTextureFromRGBA).
func whiteImage() image.Image {
        img := image.NewRGBA(image.Rect(0, 0, 2, 2))
        for y := 0; y < 2; y++ {
                for x := 0; x < 2; x++ {
                        img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
                }
        }
        return img
}
