# Features

This document is the single feature inventory. A feature is listed under **Implemented** only when its source lives in the tracked tree at HEAD **and** a deterministic test or CI gate in this repository evidences it. Documentation alone never implements a feature. `changelog.md` stays the only release history and `ROADMAP.md` stays the only direction document.

Hardware claims use five distinct classes everywhere: hardware **detected** (a probe found it) ≠ **available** (usable) ≠ **selected** (chosen for a run) ≠ **executed** (a run actually drove it) ≠ **verified** (a hosted green gate proves the outcome).

---

## Implemented features

### Core runtime & agent
- Long-horizon Goal engine — durable checkpointed UNDERSTAND → PLAN → ACT → VERIFY loop with turn budget and bounded replans
- Goal store durability — atomic file store, reload contract, boot recovery (never falsely running)
- Terminal settlement guard — a terminal goal can never be re-classified by a late drive settlement
- Drive registry — atomic single-flight admission, generation-owned cleanup, reservation retention across cancel
- Approval parking — ask-all systems park at the acting boundary with a durable pending call; decisions are one-use and bound to the exact canonical action identity
- Frozen AI-System revision binding — goals run against their frozen system revision; revised systems fail typed instead of silently substituting
- One-authority runtime: single engine lifecycle, run registry, session store, tool registry, scheduler, downloader, memory manager, Runtime Governor

### Desktop (one-Go-UI)
- Single Go + Fyne desktop application running in-process over secured local IPC
- Responsive three-zone navigation (wide rail / medium compact rail / narrow scrollable strip)
- Zero-session Send — deleting the final session leaves a usable zero-session state; Send creates and activates a session
- Session-list generation guard — every list write goes through one monotonic generation
- Live streaming that cannot wedge (triple-boundary flush + server-side conflation-aware queue)
- Four-level reasoning-depth control (Low / Mid / High / Ultra) with real numeric thinking-token budgets
- Show/Hide Thinking control (presentation only)
- Optional 3D Studio — read-only live system topology map (loopback-only, per-launch token, strict CSP, embedded Three.js)

### Inference & hardware truth
- Managed llama.cpp inference with modern CLI contract and strict numeric-argument validation before spawn
- Native C++ engine path with real generation for validated llama-architecture models and honest fallback
- Execution/evidence ladder: `detected → backend-available → device-selected → model-loaded → generation-executed → execution-evidence → verified`
- AUTO GPU posture contract with honest separation of detection from execution
- Runtime Governor policy loop with resource state, envelope, admission, and self-model

### Data, tools & memory
- Deterministic `dataAnalysis` authority (analyze / aggregate / join / quality / export) — pure Go, in-process, 256 MB bound
- Skills system (JSON + Markdown SKILL.md packages with progressive disclosure)
- Repository indexing and deterministic navigation (`repo_nav`)
- Net Search, browser, sandbox, custom tools, Coding Lab, automations
- Memory, recall, and continuum machinery

### CI & packaging
- Full Go build/test/smoke gates on Windows (MSYS2 UCRT64 CGO) and Linux
- Fyne UI suite + real-window Xvfb smoke on Linux
- Consolidated one-file run-log artifact on every CI run
- Portable ZIPs and NSIS installer (Windows)

---

## Coming features

Only items tracked in [`ROADMAP.md`](ROADMAP.md) at HEAD, in its wording and order, with its evidence labels.

**Current / future boundary (D-planned)**
- Automatic sub-agent fan-out from the goal runner
- Protected-evaluation anti-hack guard
- MCP runtime wiring into the tool registry
- Oversized-output spool handles
- Native-engine forward-pass arithmetic optimization (SIMD batched)

**Future — not implemented**
- Universal Agent / controlled computer-use layer
- AI Learning & Evaluation framework (suites, baselines, promotion by measured evidence only)
- Advanced Model Intelligence (multi-model routing, specialist models, dynamic pools)
- Full SHEYTAN Local AI Platform end-state (scalable workers, mature multi-run orchestration, portable systems)

---

## Verification map and maintenance rule

- **Go evidence**: `go test -race` on critical packages, broad `go test ./internal/...`, `go vet`, `gofmt -l` — all green at HEAD.
- **CI evidence**: `.github/workflows/build-desktop.yml` gates; the annotations and accounting line are the anonymous run-page contract.
- **Hardware claims**: the engine pipeline reports detected/available, the config selects, a run executes, and only a hosted green gate verifies.
- **Maintenance rule**: a line moves from “Coming” to “Implemented” only when the source **and** the test/CI evidence exist at HEAD. This file is never a second roadmap and never carries release history.
