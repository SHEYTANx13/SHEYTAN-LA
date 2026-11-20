# Rendering — Go-Gui Native Path, Optional GoGPU Specialist Subsystem

Run 44547031698 §9.4/§10.1 evidence model, consolidated with the run
44547031698 §3.2 link-graph findings.

## One window, one owner, one renderer class

- The production desktop window is owned by ONE presentation shell
  (today: Fyne via internal/desktop; the Go-Gui candidate: ui/gogui).
- GoGPU (github.com/gogpu/gogpu via goffi) is an OPTIONAL specialist
  graphics/compute subsystem. It never owns the product window, never
  starts an application loop, and never blocks core or shell startup
  (internal/desktop/renderboot*.go: the nogogpu build profile publishes
  the honest not-wired observation and boots clean).
- The Go-Gui shell never links the gogpu stack; it reads observations
  through the gogpu-free internal/renderstate channel (BuildNote carries
  the honest availability reason; ErrNotWired is the typed refusal).

## Link-graph reality (goffi v0.6.8, the pinned dependency)

| Build profile | CGO | Tags | Link graph | Status |
|---|---|---|---|---|
| Linux desktop | 1 | nogogpu | fyne/go-gl cgo, NO goffi | required on Linux: goffi issue #72 (SDYNIMPORT beside other cgo code) |
| macOS / Windows desktop | 1 | (default) | fyne + goffi + gogpu | upstream-supported combination; per-run combined-link proof |
| cgo-free combined | 0 | nofakecgo | go-gui (purego) + gogpu (goffi) | proven: cmd/sheytan-renderprobe links and runs; without the tag the linker reports the verbatim duplicate `_cgo_init` |

`cmd/sheytan-renderprobe` is the permanent regression target: ONE binary
importing BOTH stacks, built by the CodeQL manual build and the Linux lane
with the exact documented configuration. Its optional `-render` mode
executes the real software-path verification job (headless).

## Separate evidence domains (never merged)

- graphics/UI renderer evidence: device/API, backend, surface created,
  command submitted, frame verified — `backend=gogpu-software,
  state=execution_verified` is a CPU/software execution fact.
- AI inference evidence: provider/backend, model load, execution, output —
  a GoGPU render proves NOTHING about llama.cpp/OpenVINO/Vulkan/NPU
  inference. Telemetry, VRAM budgets and status ladders stay separate.

## GPU claims policy

No backend is claimed until it was executed and verified on the target
platform (device/API/backend created, command submitted, frame verified).
Marketing capability lists never become runtime status.
