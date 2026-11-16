# Go-Gui POC (v1.11.1 UI-engine evaluation slice)

Status: **POC_ONLY — NOT PRODUCTION_READY.** The v1.11.1 production
renderer is Fyne (`ui/fyne`). Decision record and promotion gate:
[`docs/architecture/UI-ENGINE-DECISION.md`](../../../docs/architecture/UI-ENGINE-DECISION.md).

## What this is

An isolated, reversible proof-of-concept for the Go-Gui renderer
candidate, required by the v1.11.1 integrity-first work order. It is a
**nested Go module** — the production module does not depend on it and
`go list ./...` from the repository root does not include it (verified).

Pinned candidate (verified through the Go module proxy, 2026-11-16):

```
github.com/go-gui-org/go-gui v0.96.0
  tag  refs/tags/v0.96.0
  hash 6f2d4e16e58b25fc2bec3d642e6552433a4e7ae4
  sum  h1:VIHHfFXiMJ43Ru9L16GLqbR10k3y2TJuhKowSS62agI=
  go   1.26.0   license MIT   tagged 2026-11-13
```

The version discrepancy in the research inputs (v0.91.0 vs v0.96.0) was
resolved by verification: both tags exist; v0.96.0 is the latest and is
what this POC compiles and tests against.

## The slice

Responsive window + navigation sidebar; chat surface with a scrolling
long-content message list; Persian/Arabic mixed-direction fixtures with
embedded LTR URL and code; a composer with keyboard focus; a resizable
Splitter inspector pane; a table artifact view. Headless app-level tests
(type/click/render — wired behavior, not render-without-panic) plus a
software-rendered screenshot.

## Run it

```sh
cd experiments/ui-engine-poc/go-gui
go test ./...              # 5 headless app tests
go run .                   # real window (needs a desktop session)
go run . -screenshot out.png
```

## Explicitly not covered

Real window-system input, IME/CJK, accessibility trees, native dialogs,
physical devices, frame-time benchmarks — all NOT RUN; the promotion gate
in the decision document lists them. The candidate's own published
platform matrix (Windows a11y not implemented, iOS a11y/URI opening not
implemented, native dialogs unavailable on Android/iOS) is the standing
blocker list.
