# Accessibility Matrix (v1.11.1)

> What is verified, what is NOT RUN, and the Go-Gui correction that the
> v1.11.1 milestone incorporated. Hardware screen-reader verification is
> owned by hosted/physical gates — nothing here inflates a structural
> guarantee into an assistive-technology guarantee.

## 1. Fyne (the production shell)

- Keyboard navigation: Ctrl+1..9 switch the nine primary views in
  registry order (pinned by `TestShortcutKeyMapCoversAllPrimaryViews`);
  Ctrl+N new session; every destination pointer-reachable in every
  viewport class (the responsive matrices).
- Focus and activation: the custom window controls are Focusable with a
  visible focus ring and Enter/Space activation
  (`TestWindowControlsCloseFunnel` fires on all three paths).
- Accessible labeling: controls carry visible text labels (icon-only
  actions are avoided); disabled controls render their reason as a
  VISIBLE label (never a tooltip-only affordance).
- Screen-reader coverage: **NOT RUN on hardware** this milestone. Fyne's
  accessibility flag remains an early opt-in implementation; no UIA /
  AT-SPI / VoiceOver pass has been executed here. AT verification of
  the production shell is a roadmap row with the hosted/physical gates.

## 2. Go-Gui — the corrected matrix (2026-11-16, Mike Ward)

**The correction (binding for all docs):** Windows accessibility IS
implemented — a Windows UI Automation provider serves **Narrator and
NVDA on amd64 and arm64**; `UiaRaiseNotificationEvent` requires
Windows 10 1709+ for announcements; Windows desktop dialogs are
supported. The earlier claim that Windows accessibility was
unimplemented is STALE and is retired from this repository
(`UI-PLATFORM-AUDIT.md` §2, `UI-ENGINE-DECISION.md`, and the renderer
registry — tripwired against return by
`internal/renderer.TestGoGuiAccessibilityCorrection`).

**Remaining documented upstream gaps (still promotion blockers):**
iOS accessibility tree sync/announcements; iOS URI opening; native
open/save/folder/message/confirm dialogs on Android and iOS; iOS
autocorrect, marked-text (CJK) composition and clipboard wiring.

**Evidence class:** upstream matrix correction incorporated — no
hardware Narrator/NVDA run has been executed in this repository; the
promotion gate still requires it.

## 3. GoGPU UI / Wails WebView

- GoGPU UI: upstream documents accessibility roles; **unverified** —
  no screen-reader pass here.
- Wails: WebView-content accessibility follows the HOST engine
  (WebView2/WebKit) — **unverified**; the workspace's own controls keep
  visible labels and fallbacks regardless.

## 4. Input-method matrix (recorded, honest)

- IME/CJK composition: NOT RUN on any engine (POC text input exercises
  the framework test driver only).
- Persian/Arabic RTL + mixed LTR code/URL: exercised in the Go-Gui POC
  as state round-trip + software-rendered screenshot (shaping
  correctness not reference-verified); the Fyne shell renders long
  mixed-direction session titles under the same contract.
- Paste/selection/clipboard: engine-owned behavior, recorded per
  engine in the renderer registry's capability rows.

## 5. Roadmap rows

Narrator/NVDA smoke on the Windows hosted lane (Fyne + Go-Gui);
AT-SPI on Linux; VoiceOver on macOS; TalkBack/VoiceOver only after
mobile targets exist; IME/CJK fixtures on physical input hardware.
