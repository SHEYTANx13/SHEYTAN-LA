# Renderer Host — the one-authority contract (v1.11.1)

> How multiple renderers coexist with ONE Go-owned state authority. The
> concrete registry: `internal/renderer`. The engines:
> [`RENDERER-ENGINE-MATRIX.md`](RENDERER-ENGINE-MATRIX.md). The audit of
> where this landed: [`UI-ARCHITECTURE-AUDIT.md`](UI-ARCHITECTURE-AUDIT.md).

## 1. The rule

**Go remains the policy, persistence, orchestration, permission,
provider, event and data authority. Renderers are clients.** No renderer
ever reads or writes the database/files directly, owns a duplicate
provider/session/tool registry, or becomes a second state authority.
Every mutation flows through the existing typed Go commands with the
v8 content-integrity contract (exact-target identity, revision guards,
idempotency keys, recoverable deletion) — the Engine Gallery introduced
NO new store.

## 2. Renderer manifests and lifecycle

`internal/renderer` declares each engine's identity, pinned version,
maturity (production/preview/experimental/unavailable), isolation mode,
per-platform verification status, truthful capability matrix, known gaps
and theme-mapping status. The gallery renders the registry verbatim —
the UI cannot invent engines or states. A future adapter EXTENDS the
registry; it does not fork a second list into a UI package.

Lifecycle (future adapters): probe availability → start/open → attach
typed state → health/status → stop/cleanup — bounded, with explicit
event-loop ownership and cancellation; no unbounded retries, no sleeps
as synchronization.

## 3. The embedding boundary (the single-window rule)

The Fyne shell presents ONE unified SHEYTAN-LA window (engine and theme
selection included — the Engines destination, Ctrl+9). An engine renders
INSIDE that window only through a supported host/child-surface/texture
interop path proven by a real POC. Where a framework requires its own
native event loop or window — Go-Gui, GoGPU UI and Wails all do today —
it is launched as a **managed companion surface/window** from the same
Engine Gallery and synchronized through the Go core, clearly labeled;
a static screenshot labeled interactive is FORBIDDEN. In the v1.11.1
package those engines are honestly **not built** (POCs live in
`experiments/`): the gallery shows their state, pin, capabilities and
gaps with disabled actions carrying the reason — no fake launch
buttons.

## 4. State and event flow (when adapters land)

Renderers receive typed read models and incremental events; they submit
typed commands (schema/protocol version, command id, exact target,
expected revision, actor/permission context, deadline/correlation).
Bounded channels, event conflation for high-frequency progress, full
refresh on missed sequence. Worker goroutines never mutate UI widgets
directly — every surface marshals through its own UI loop (the Fyne
seam today: `ui/fyne/ui_marshal.go`).

## 5. Web surfaces (the Wails boundary)

HTML/CSS/JS is allowed ONLY inside the isolated Web Workspace and its
embedded assets — never a second app-state or permission authority. The
WebAppHost bridge is a closed, typed allowlist (the POC:
`theme.tokens`, `engine.status`, `workspace.ping`) with strict schema,
bounded payloads and correlation ids; untrusted origins never receive
it. Navigation policy, downloads, CSP and feature detection contracts:
[`WEBVIEW-SECURITY.md`](WEBVIEW-SECURITY.md) and the POC's tested
`security.go`/`features.go`.

## 6. Theme authority

`internal/theme` is the ONE token catalog (six themes, schema-versioned,
WCAG-contrast-validated, JSON-serializable for web surfaces). Fyne maps
it fully (`ui/fyne/theme.go`); the other engines' mappings are DESIGNED
(`docs/ui-contract.md`) and degrade honestly — a renderer that
cannot implement a token documents the degradation rather than
inventing a different design.
