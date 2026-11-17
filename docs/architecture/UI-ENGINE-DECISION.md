# UI Engine Decision — v1.11.1

## Decision: KEEP_FYNE_FOR_1.11.1

The production renderer for v1.11.1 is **Fyne v2.8.2** (`ui/fyne`),
unchanged. The Go-Gui candidate is **POC_ONLY — NOT PRODUCTION_READY**.
Gio remains a contingency with no implementation. Rationale and evidence:
[`UI-PLATFORM-AUDIT.md`](UI-PLATFORM-AUDIT.md).

## Why (one paragraph)

v1.11.1 is an integrity-first maintenance milestone: the highest-priority
work was recoverable content mutation (see
[`CONTENT-INTEGRITY.md`](CONTENT-INTEGRITY.md)), the Windows named-pipe
typed-error repair and gate hygiene. The Fyne tree carries the landed
v1.11.1 UI contracts (15 items in [`../ui-contract.md`](../ui-contract.md))
with a hosted-green gate and passing local matrices; replacing a renderer
in this pass would have traded verified behavior for an unevaluated one —
exactly the destabilization the work order forbids. The Go-Gui POC
isolates the evaluation so the decision can be made on evidence next
milestone, with zero production risk today.

## POC results (v0.96.0, 2026-11-16)

Exercised and green (`experiments/ui-engine-poc/go-gui`, `go test ./...`
5/5 PASS):

- window + navigation sidebar with state-driven view switching;
- chat surface: scrolling long-content message list + fixed composer;
- keyboard entry through the framework's own test driver (`TestType`,
  `TestClick`) — wired behavior asserted, not just render-without-panic;
- resizable `Splitter` inspector pane with per-pane minimums;
- table artifact view (`TableCfgFromData`);
- Persian/Arabic mixed-direction fixture messages with embedded LTR
  URL/code (state round-trip pinned; shaping correctness is a screenshot
  claim only — see limits);
- software-rendered screenshot (`soft.RenderToPNG`, 126,746-byte PNG).

v1.11.1 hybrid milestone extension (run-43862893851 follow-up): the POC
suite is now **10/10** — the mandated responsive matrix (1280x720,
1024x768, 800x600, 640x480) renders contained on both surfaces with
per-size screenshots, a 2x sampling proxy (a sampling parameter, NOT a
device-scaling claim), close/reopen, and typing focus at the 640x480
floor. A REAL containment defect was found and fixed in POC code (never
by weakening the test): at 640x480 the Send button lost its visible area
inside the starved splitter pane, so the workspace now renders one
content column below a 900-unit breakpoint, mirroring the Fyne shell's
compact contract. The Fyne-side launch action stays STAGED_WITH_GATE (see
[`UI-ENGINE-STACK.md`](UI-ENGINE-STACK.md) §3): no blind `ui/fyne` edit
was made from a host that could not compile it.

Notable framework finding (recorded for the next evaluation round): the
widget-identity model scopes child ids under id-bearing ancestors with
`:` (`gui/id_resolve.go`); the POC pins its interactive controls with
absolute ids (containing `:`) so focus/test addressing stays stable
inside Splitter panes.

## Measured gaps / NOT RUN (never inflated)

- Real window-system input, IME, CJK composition, clipboard: NOT RUN.
- Accessibility trees (Windows UIA, Linux AT-SPI, macOS VoiceOver,
  TalkBack/VoiceOver on mobile): NOT RUN on hardware in this pass. The
  corrected upstream Native Platform Matrix (2026-11-16, Mike Ward) marks
  Windows accessibility IMPLEMENTED through a UI Automation provider for
  Narrator/NVDA on amd64/arm64 (announcements need Windows 10 1709+);
  the stale pre-correction claim that Windows a11y was unimplemented is
  retired. iOS a11y/URI opening remain unimplemented upstream, and
  native dialogs remain unavailable on Android/iOS.
- Physical devices, high-DPI, safe-area, touch: NOT RUN.
- Frame-time P50/P95/P99, typing latency, 10k-item virtualization soak,
  same-hardware comparison vs Fyne: NOT RUN (no comparable hardware
  budget in this pass; the hosted/physical gates own those numbers).
- Bidi/shaping correctness of the Persian fixture: rendered and
  screenshot-captured only, not verified against a reference.

## Promotion gate (all must pass on the SAME pinned candidate)

Go-Gui may replace Fyne only after, on one pinned version/commit:

1. Windows/macOS/Linux/Android/iOS builds AND runtime launch;
2. native file selection, permissions and lifecycle on all five;
3. keyboard, paste, IME, CJK marked text, Persian/Arabic RTL + mixed LTR
   verified with native speakers' fixtures;
4. accessible semantic tree + screen-reader tests (UIA, AT-SPI,
   VoiceOver, TalkBack, VoiceOver) — including the matrix gaps above
   CLOSED or explicitly waived by the operator;
5. focus, touch, window/safe-area behavior;
6. responsive layout, virtualization at 10k items, artifact rendering,
   dialogs;
7. stability soak + same-hardware performance benchmarks vs Fyne
   (cold start, first frame, P50/P95/P99, typing latency, memory);
8. security + dependency + license review of the full module graph.

Until every item is green, Fyne stays production and the candidate stays
labeled POC_ONLY.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
