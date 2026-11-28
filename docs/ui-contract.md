# UI Design Contract

**Binding for every UI/UX change.** This is the single, documented design
contract for the adaptive web shell (`internal/webshell`) — the ONE
desktop UI surface, rendered in the primary window by the web workspace
host (`internal/webworkspace`, Wails v3.1.0) on every desktop platform.
The Fyne shell generation (`ui/fyne`, retired by the run-45634345922
flip; see [`retired.md`](retired.md)) executed the original phased
contracts #1–15; its contract text lives in git history, and the rules
below carry the surviving invariants forward onto the web shell. The
Go-Gui mirror (`ui/gogui`) stays the Android mobile shell and the native
IA mirror the shell contract test pins against.

Provenance: created by the run #123 fix session (2026-11-01) as Phase 0
of the operator-authorized UI/UX research; phases 1–2 and the v1.11.1
remediation pass executed on the Fyne shell (history in git). Rewritten
2026-11-29 (run 46131092469) as the web-shell-era contract, merging the
former TOUCH-RESPONSIVE-MATRIX, DESIGN-SYSTEM and ACCESSIBILITY topics
(the three `docs/ui/` pages) into this one document — consolidation:
same category, one home; the merged pages are retired from the tree.

---

## 1. Token source of truth

- The ONE token authority is CODE: `internal/theme` — schema-versioned,
  WCAG-contrast-validated (the catalog test gates every theme on every
  build), JSON-serializable via `Theme.JSON()`.
- The web shell consumes the tokens through `internal/webshell/assets/css/tokens.css`
  — the CSS custom properties (`--bg`, `--fg`, `--accent`, `--border`,
  `--radius*`, `--space-1..5`, `--touch-floor: 44px`) mirror the product
  token roles. A view may change density and arrangement, never the
  token language; no element file hardcodes a role outside a token.
- The per-element cascade (`assets/css/*.css`: tokens, base, layout,
  controls, rail, views, chat, panels, loading, system, adaptive,
  motion) is entered through ONE `app.css` of comments + ordered
  `@import`s — the entry shape and the same-origin CSP scan across every
  element file are pinned by the `internal/webshell` contract suite.
- Dark is the identity default; the light variant is the same role
  system re-expressed via `prefers-color-scheme`, and
  `prefers-contrast: more` raises the border floor.

## 2. Touch contract

- **The touch floor is 44px** (`--touch-floor`); 48px preferred for
  primary actions. Rail items already carry the full floor; buttons,
  inputs, icon buttons and tappable list rows reach it on coarse
  pointers via the `@media (pointer: coarse)` floors in
  `controls.css`/`views.css` — the pointer-era compact min-heights stay
  for mouse-precision surfaces.
- `touch-action: manipulation` on every interactive control: no
  double-tap-zoom ambiguity on the WebView.
- Pressed feedback is a visible `:active` state on every control class —
  essential feedback never depends on hover, which sticky-hovers or
  never fires on touch.
- Text inputs carry the 16px font floor on coarse pointers, so WebView
  focus auto-zoom can never jolt the layout on tap.
- Safe areas: `env(safe-area-inset-*)` pads the topbar, status bar and
  rail so system insets never cover controls.
- 8–12px practical spacing between adjacent targets; no essential
  action is hover-only; disabled reasons render as visible labels;
  double-submit prevention stays with the core-owned single-flight
  patterns (the UI is never the authority).

## 3. Responsive policy

ONE adaptive system in `adaptive.css`, driven by actual viewport width
(never device labels), recomposed on resize:

| Band (viewport width) | Class | Navigation |
|---|---|---|
| <600 | compact | fixed drawer rail, opened by the topbar toggle (`aria-expanded` managed) |
| 600–899 | medium | 64px icon rail |
| 900–1279 | desktop | wide rail (200px) |
| ≥1280 | wide | wide rail (240px) |

Views reflow inside the bands (`.chat-grid`, `.grid.two` collapse to one
column when needed); scroll containers carry `overscroll-behavior:
contain` and `-webkit-overflow-scrolling: touch`; clipping, overlap and
accidental horizontal scrolling are defects, not styles.

## 4. Interaction contracts (carried onto the web shell)

1. **Composer: Enter sends, Shift+Enter breaks the line** (hint rendered
   beside the send action).
2. **One destination renders once**: the route registry drives exactly
   one rail entry per view; `show()` resolves the loader through the
   typed `Map.get` allowlist (the CodeQL #366 unvalidated-dynamic-call
   class is structurally absent), `aria-current="page"` marks the active
   destination, and the view host takes focus after navigation.
3. **Boot never traps the operator**: the splash overlay is dismissed by
   the first healthy same-origin core read and by a 6s safety timeout;
   the web host performs that read BEFORE the window boots and fails
   boot typed on anything but success.
4. **The status line stays truthful**: connection state renders
   connecting / live / failed as real state; unknown renders unknown;
   no fabricated telemetry.
5. **Busy actions are visible**: in-flight actions set `aria-busy` and
   render progress; a second activation during flight is a no-op.
6. **Keyboard + palette**: Ctrl+1..9 reach the registry views, Ctrl+K
   opens the command palette (dialog role, modal focus, Escape closes),
   every control is reachable by keyboard and carries `:focus-visible`
   outlines; the skip link reaches the view host first.
7. **Reduced motion wins**: `motion.css` is the LAST cascade layer and
   kills every transition/animation under `prefers-reduced-motion:
   reduce`.

## 5. Accessibility matrix (honest rows)

- Semantic structure: landmarks (`header/nav/main/footer`), labelled
  views (`aria-labelledby`), live regions (`aria-live="polite"` on
  status/chat/log surfaces), progressbar roles with real values.
- Accessible names: icon-only actions carry `aria-label` + tooltip text.
- Screen-reader/AT verification on hardware (Narrator/NVDA/UIA,
  AT-SPI/VoiceOver): **NOT RUN** — the WebView content follows the host
  engine's accessibility implementation; no AT pass has been executed in
  this repository. AT verification of the production shell stays a
  roadmap row with the hosted/physical gates.
- The Go-Gui Windows-accessibility correction (UI Automation serves
  Narrator and NVDA on amd64/arm64) remains the recorded upstream fact
  for the mobile-shell engine; it is tripwired against return by
  `internal/renderer.TestGoGuiAccessibilityCorrection`.
- IME/CJK composition and RTL rendering on the web shell: NOT RUN on
  hardware; recorded, never claimed.

## 6. Evidence ladder

- `node --check internal/webshell/assets/app.js` and the
  `internal/webshell` contract suite (bundle assembly, entry shape,
  per-file same-origin CSP scan) on every change.
- `go test ./internal/webshell/ ./internal/webworkspace/ ./internal/desktop/`
  locally; the hosted lanes own the real WebView runtimes
  (WebView2/WebKit/WKWebView), the Xvfb linux launch smoke and the
  macOS/Windows launch-and-shutdown smokes.
- Every "smoother or better" claim is backed by a test or an observable;
  evidence classes never inflate (`docs/UPDATE.md` honesty rules).

## 7. Contract governance

- This document is amended only deliberately, in the same change as the
  behavior it describes; token renames update section 1 in the same
  change; status flips only with evidence.
- The dark token values are the identity root: any change to them is a
  deliberate identity decision recorded in [`changelog.md`](changelog.md),
  never a side effect.
- Superseded UI-contract text (Fyne era) lives in git history and in
  [`retired.md`](retired.md) — never duplicated here.

---

*Current work order: [`Dev-Notes.md`](Dev-Notes.md) (human-owned). Plan: [`ROADMAP.md`](ROADMAP.md).*
