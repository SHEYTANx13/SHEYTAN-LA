# Go-Gui / GoGPU Interop — Surface Separation Decision (v1.11.1)

> Why the Go-Gui workspace POC and the GoGPU surface POC run as SEPARATE
> surfaces in v1.11.1, and what a future bridge would have to prove.
> Verified against the repository 2026-11-16.

## Decision: SEPARATE_SURFACES (no bridge in this milestone)

The Go-Gui workspace (`experiments/ui-engine-poc/go-gui`, v0.96.0) and the
GoGPU graphics surface (`experiments/ui-engine-poc/gogpu`, gogpu v0.54.1)
each own their own native window. No PixelSurface/frame-streaming bridge,
no shared device, no native-handle sharing exists — and none is claimed.

## Why (the honest reasons)

1. **"Both are Go libraries" is not interop.** The work order's §1.2 rule:
   never claim Go-Gui and GoGPU have direct interop just because both are
   Go. A real bridge needs a proven pixel/texture path, and none was
   tested here.
2. **The candidate APIs were not exercised for embedding.** The Go-Gui POC
   verified its soft backend (headless renders, screenshots) — the
   `PixelSurface` embedding path was never driven in this pass. Asserting
   it would have been a fabricated claim.
3. **Zero-copy would be a lie today.** Any Go-Gui↔GoGPU frame exchange in
   the currently verifiable form is a CPU-side image copy at best. The
   work order forbids labeling that zero-copy or GPU-to-GPU. We do not
   label it at all — there is no bridge.
4. **One renderer owner per native surface** (work order §1.2). Two
   independent GPU contexts/event loops attached to one window is exactly
   the unsupported configuration the work order prohibits. Separate windows
   are the safe, supported shape today.

## What a future bridge must prove (STAGED_WITH_GATE)

A GoGPU-in-Go-Gui embedded surface may only exist after, on one pinned
candidate pair:

1. the exact PixelSurface (or equivalent public API) path is exercised in
   the candidate's own documentation AND in our tests — with a measured
   frame transfer cost (CPU readback must be stated as CPU readback);
2. bounded double/triple buffering with latest-frame-wins backpressure,
   explicit stride/format/size and frame timestamps — never unbounded
   queues, never a layout rebuild per frame;
3. resize, device-loss, rapid show/hide and teardown tests pass;
4. the label is honest: CPU-copy bridges are documented as CPU-copy.

Until then: the GoGPU surface stays its own window, launched from wherever
the operator wires it, linked to the app only through the shared
session/request model.

## What was verified in this milestone (neither overstated)

- Go-Gui POC: 10/10 tests — including the mandated responsive matrix
  (1280x720, 1024x768, 800x600, 640x480), per-size screenshots, a 2x
  sampling proxy (NOT a device-scaling claim), close/reopen, and typing
  focus at the 640x480 floor after a real containment defect was found
  and fixed (the narrow-width breakpoint in the POC's `mainView`).
- GoGPU POC: deterministic spectrum (LCG data, pure function of the frame
  index); real render through the Pure-Go software backend (headless,
  no display, no GPU adapter — `adapter selected name="Software Renderer"
  type=CPU`); a bounded 3-frame windowed soak under a manual Xvfb display
  selecting the software adapter; GPU HARDWARE NOT RUN (no Vulkan ICD, no
  /dev/dri on the authoring host). Software rendering is not proof of GPU
  acceleration and is never reported as such.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
