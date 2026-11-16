# UI Engine Stack — The Hybrid Layering (v1.11.1)

> How the native layers of the v1.11.1 hybrid milestone fit together:
> who owns which surface, who owns state, and what is production versus
> preview. Decision records: [`UI-ENGINE-DECISION.md`](UI-ENGINE-DECISION.md)
> (Fyne stays production). Audit base: [`UI-PLATFORM-AUDIT.md`](UI-PLATFORM-AUDIT.md).
> Verified against the repository 2026-11-16 (run-43862893851 follow-up
> milestone).

## 1. The one layering rule

The Go application core owns every authority exactly once. Every renderer
is a CLIENT surface that dispatches typed commands through the existing
permission/service boundary — no renderer writes to a store, ever.

```
             Existing SHEYTAN-LA Go application core
    conversation · projects · providers · agents · tools
       permissions · context · persistence · artifacts
                         │
              typed commands / events / DTOs
                         │
                Workspace / Host Router
        ┌────────────────┼─────────────────┐
        │                │                 │
   Fyne Shell       Go-Gui Workspace    Native WebAppHost
   default UI       advanced native     actual web content
   (production)     workspace (POC)     (contract; adapters gated)
        │                │                 │
        │                └───────┬─────────┘
        │                        │
        │                 GoGPU / WGPU
        │           GPU visual surfaces only (POC)
        │           — separate native window, by design
        │
        └── shared permission, diagnostics and storage services
```

## 2. Surface ownership (one renderer owner per native surface)

| Layer | Status in v1.11.1 | Owns its own native window? | Store access |
|---|---|---|---|
| Fyne shell (`ui/fyne`, v2.8.2) | PRODUCTION | yes | none — client of the local API over `internal/localipc` |
| Go-Gui workspace (`experiments/ui-engine-poc/go-gui`, v0.96.0) | POC_ONLY, isolated nested module | yes (own window/process; never embedded in the Fyne window) | none — deterministic sample state only; no app data path exists in the POC |
| GoGPU surface (`experiments/ui-engine-poc/gogpu`, gogpu v0.54.1) | POC_ONLY, isolated nested module | yes — a SEPARATE surface; never an embedded bridge into Go-Gui (see [`GO-GUI-GOGPU-INTEROP.md`](GO-GUI-GOGPU-INTEROP.md)) | none — it renders a deterministic spectrum; it is not a second copy of any engine |
| Native WebView (`internal/webapphost` contract) | FOUNDATION: typed contract + enforced security policy; adapters STAGED_WITH_GATE | adapters will own their own window (WebView2 / WKWebView / WebKitGTK) | none — pages are untrusted content; only a trusted `app://` origin may receive a named, capability-granted bridge ([`WEBVIEW-SECURITY.md`](WEBVIEW-SECURITY.md)) |

## 3. Launch and lifecycle posture

- Optional engines are opt-in and lazy: nothing in this milestone starts
  Go-Gui, GoGPU or a WebView adapter on the production startup path. The
  Fyne shell starts and works with none of them installed.
- A crash or refusal in any optional layer must never take down the Fyne
  shell; every optional layer fails visibly with a reason.
- The Fyne-side launch action for the Go-Gui workspace is
  STAGED_WITH_GATE: it must be added and compile-verified on a host with
  the Fyne CGO prerequisites (the authoring environment of this milestone
  could not compile `ui/fyne`, so no blind edit was made — the no-placeholder
  rule outranks the feature).

## 4. Diagnostics vocabulary (binding)

For every optional layer the runtime reports the states of
[`../diagnostics/RUNTIME-DIAGNOSTICS.md`](../diagnostics/RUNTIME-DIAGNOSTICS.md) §3:
detected ≠ available ≠ selected ≠ executed ≠ verified. A GoGPU software
render is reported as software — never as GPU acceleration.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
