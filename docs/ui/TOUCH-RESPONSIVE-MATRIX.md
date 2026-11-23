# Touch & Responsive Matrix (v1.11.1)

> The touch/viewport contract: hit-area floors, the size-class policy and
> the exercised matrices. The shared touch tokens are CODE:
> `internal/theme` (44/48 DIP + spacing); the responsive policy is CODE:
> `ui/fyne/shell.go` (`sizeClassFor`/`navPolicy`) — this page documents
> both, never re-defines them.

## 1. Touch targets (UX-06)

- **Minimum 44×44 logical DIPs; 48 preferred for primary actions and
  window chrome.** The Engines gallery's window controls GUARANTEE the
  floor structurally (custom widgets whose `MinSize` cannot go below
  44×44 — `ui/fyne/window_controls.go`, pinned by
  `TestWindowControlsHitAreas`).
- **Run 45165536396 UX pass — the floor is now STRUCTURAL for the
  navigation chrome and the composer too**, at every size class (phone
  AND big screens): `floorTouch` (ui/fyne/widgets.go) wraps every rail
  row (primary + utilities), top-strip row, the More button and the
  disconnected-state Retry at 44 DIP (`RowRelaxed` / `TouchTargetMin`),
  Send/Stop at 48 DIP (`TouchTargetPreferred`), and the empty-state
  sample chips at 44 DIP. The wrapper is a one-child container with a
  visibility-aware layout: the child fills the floored area (the whole
  surface is the tap target) and a hidden child reserves no space. The
  structural goldens pin the floored geometry.
- 8–12 DIP practical spacing between adjacent targets.
- Known boundary: standard Fyne popup-menu items (the compact strip's
  More menu) are rendered at the driver's menu metrics and are not
  floored in this pass — recorded, not claimed.
- No essential action is hover-only: disabled reasons render as VISIBLE
  labels (the minimize/tray cards), never tooltips-only.
- Double-submit prevention: state-changing commands are guarded by the
  existing single-flight/funnel patterns (bootstrap guard, shutdown
  coordinator CAS).
- Text selection, clipboard and IME behavior follow the host engine;
  long-press never routes to a destructive action without an affordance.

## 2. Responsive policy (UX-07)

ONE policy in `shell.go`, shared by navigation and views:

| Band (logical width) | Class | Navigation | Notes |
|---|---|---|---|
| <600 | compact | top strip + More menu | one content column; every destination reachable |
| 600–899 | medium | compact rail | rail grows to content minimum (never clips labels) |
| 900–1279 | desktop | wide rail | |
| ≥1280 | wide | wide rail | same desktop policy (documented, not a hidden 4th layout) |

The cross-engine bands (compact/tablet/desktop/wide/ultrawide at
600/900/1400/2200 — `internal/theme.Bands()`) record the full contract
for future engines; the Fyne shell's tested four-class policy maps onto
them without divergence at any exercised size.

## 3. The exercised matrices (evidence in-repo)

- **Structural goldens** (`shell_responsive_test.go` + regenerated
  `testdata/golden-*.txt`): compact-landscape 844×390, compact-portrait
  390×844, medium-tablet 768×1024, desktop 1366×768, wide 2560×1440 —
  the v1.11.1 regeneration diff is EXACTLY the ninth destination
  (Engines) in rail and top strip; nothing else moved.
- **Viewport sweep** (`viewportMatrix`): 320×568 … 3840×2160 — phone
  portrait/landscape, tablets, laptops, desktops, wide/4K.
- **Boundary sweep** (`boundaryMatrix`): one unit below/at/above every
  breakpoint at three heights.
- **Button-fit matrix** (`button_fit_test.go`): every destination,
  every size, every visible button/entry/select/check — no clipped
  labels, nothing escaping the content width, every destination keeps
  ≥1 operable control. The v1.11.1 run caught and fixed a REAL defect
  of this class: long build-time texts (unwrapped bold engine names,
  card titles) inflated the Engines scroll content to 388 DIP inside a
  304 DIP viewport — fixed by wrapping the texts (and by the
  `wrappedLabel` creation-order repair in `widgets.go`), never by
  weakening the gate.
- **Resize smoke** (`SHEYTAN_UI_RESIZE_SMOKE=1`): the real window under
  Xvfb walks a bounded logical-size matrix with stable-settle checks
  and clamps.
- **Degenerate sizes**: 0×0/1×1 and sub-minimum probes — settle stably
  at the 360×320 product minimum, never panic.

## 4. DPI / scaling truth

Layout is driven by LOGICAL canvas units, never physical pixels; Fyne
maps device scale per platform. The local evidence is at scale 1.0;
Windows 100–200% and mixed-DPI multi-display behavior is owned by the
hosted/physical lanes (recorded, not claimed). High-refresh targets are
measurement goals, not promises — no idle render loop exists in the
shell.

## 5. Roadmap rows

Windows scale 100/125/150/175/200% + mixed-DPI dual-display automated
passes; rotation and split-screen coverage on real hardware; touch-input
path verification on real touchscreens (the 44/48 DIP contract is
verified structurally today).
