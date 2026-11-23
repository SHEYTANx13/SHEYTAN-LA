// Package mesh implements SHEYTAN-LA's private personal compute mesh
// (blueprint: "SHEYTAN-LA Mesh Computing — Research Paper, Architecture
// Decision and Implementation Blueprint", Parsa Tak, 2026-11-22).
//
// Package boundary (one authority per concern):
//
//   - This package is the SINGLE production authority for distributed
//     AI-inference compute. It owns the typed job model, the durable
//     job ledger with attempt identity and fencing epochs, the
//     allowlisted handler registry, and the manager lifecycle.
//   - It never touches internal/localipc (secured desktop IPC), never
//     re-binds the loopback API onto a LAN/WAN interface, and — in this
//     first phase — opens NO network listener, performs NO discovery,
//     and starts NO background worker as a side effect of construction.
//     The libp2p LAN/WAN transport is the next gated phase.
//
// The one rule that outranks everything else (product requirement):
//
//	UI/UX COMPUTE IS NEVER SHARED. Only AI-inference compute
//	(reasoning) participates in the mesh.
//
// Concretely: the handler allowlist in job.go admits ONLY the typed
// AI-inference job types (embeddings batches, whole-request inference on
// a model-ready peer, bounded deterministic read-only AI batch tasks).
// RegisterHandler structurally refuses any type outside that allowlist,
// so UI/render/layout/animation work can never be registered, dispatched
// or executed remotely, and no handler can ever be invented at runtime
// by a model or a peer. The coordinator additionally refuses envelopes
// whose privacy class would move user-private content off-device.
//
// Phase-one evidence class (blueprint work order §16): typed job model,
// durable ledger, fake-worker interface, deterministic end-to-end tests,
// attempt identity, lease expiry, fencing epoch, stale-result rejection,
// cancellation, retry cap, restart recovery — all local, no network.
package mesh
