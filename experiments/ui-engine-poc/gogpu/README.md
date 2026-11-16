# GoGPU spectrum POC (v1.11.1 hybrid-native-UI milestone)

Status: **POC_ONLY — NOT PRODUCTION_READY.** Isolated nested module; the
production module does not depend on it (`go list ./...` from the repo root
excludes it). GoGPU is the optional high-performance graphics-surface layer
of the hybrid stack — never a second copy of the conversation or persistence
engine (work order §4.1).

## Pinned candidate (module proxy, verified 2026-11-16)

```
github.com/gogpu/gogpu v0.54.1
  tag refs/tags/v0.54.1 @ 01b7d29ce5678a034483bcaee6c6b5822110a08a
  tagged 2026-11-08, go 1.24 min, Apache-2.0
module graph pins (resolved by go get, NOT hand-forced):
  github.com/gogpu/wgpu     v0.34.9   (latest standalone tag v0.34.15 of
                                       2026-11-14 exists; overriding the
                                       pin gogpu v0.54.1 requires would be
                                       an unaudited change — refused)
  github.com/gogpu/gputypes v0.8.2
  github.com/gogpu/naga     v0.21.4
```

## The slice

A deterministic spectrum visualization: 48 bar amplitudes as a pure
function of the frame index (LCG — no time source), drawn as positioned
bars through one solid texture and `Context.DrawTextureScaled`. Explicit
resource cleanup on close. Two paths:

- `go run .` — the real windowed App. **GoGPU owns its own native
  window**: a separate surface, never an embedded bridge into the Go-Gui
  workspace (work order §4.5.5). No zero-copy or GPU-interop claim is made
  anywhere.
- `go run . -screenshot out.png` — one frame through the Pure-Go software
  backend (`gogpu.NewHeadlessRenderer`): no display, no GPU adapter.
- `go run . -frames N` — bounded soak that closes after N frames.

## Verified on this host (2026-11-16, Linux headless)

- `go build ./...` green; `go test ./...` green (determinism + real
  non-blank software render + render determinism).
- `-screenshot` run produced a real 960x600 software-rendered frame.

## Explicitly NOT RUN / NOT claimed on this host

- **GPU HARDWARE NOT RUN**: no Vulkan ICD (`/usr/share/vulkan/icd.d/`
  absent), no `/dev/dri`. Software rendering is not proof of GPU
  acceleration.
- Windowed runtime under a real desktop session.
- Frame-time P50/P95/P99, dropped frames, VRAM: NOT MEASURED (no present
  loop exercised here; measurement is a promotion-gate item).
- Any Go-Gui PixelSurface/frame-streaming bridge: STAGED_WITH_GATE —
  see `docs/architecture/GO-GUI-GOGPU-INTEROP.md`. Both engines keep
  separate surfaces until a documented, tested frame-transfer path exists.
