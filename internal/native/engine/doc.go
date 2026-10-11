// Package engine implements the Go side of the SHEYTAN Native AI Engine
// (v1.1.5 Phase 2).
//
// # Architecture position
//
// The target stack is:
//
//	React/TypeScript
//	      ↓
//	    Wails
//	      ↓
//	   Go Core
//	      ↓
//	SHEYTAN Native API   ← this package (Go side)
//	      ↓
//	C++ Native Engine    ← native/engine/ (C ABI + host subprocess)
//
// Go remains the main application/runtime engine. In Phase 2 the native
// engine implements the supervised lifecycle (start / health-check / mark
// ready / stop / detect failure / bounded restart), hardware reporting,
// metrics AND native GGUF model loading (validate → memory-map →
// metadata extraction → memory planning) with the model states unloaded /
// loading / loaded / failed. NOT inference: Generate / StreamGenerate
// still return ErrNotImplemented and GenerationCapable stays false, so
// generation requests keep running on the llama.cpp backend, which
// remains fully functional as the fallback.
//
// # Go ↔ C++ boundary decision
//
// Two candidate boundaries were evaluated:
//
//	A) cgo / shared native library
//	B) supervised native subprocess + IPC
//
// Decision: **B — a supervised native subprocess (shtn-engine-host)
// speaking a length-prefixed JSON protocol over stdin/stdout.**
//
// Rationale:
//
//   - Crash isolation: a native engine crash (segfault, bad driver call,
//     OOM inside the allocator) must never take down the application
//     process. A subprocess dies and is restarted by the bounded watchdog;
//     an in-process cgo crash terminates the whole Go runtime. SHEYTAN's
//     Windows-first audience makes this decisive.
//   - Cross-compilation: the Windows build is a CGO_ENABLED=0 cross-build
//     today. cgo would require a Windows C++ toolchain for every build
//     host and break that property.
//   - Future Android: a subprocess maps to an Android service process;
//     the C ABI + protocol stay identical either way.
//   - Maintainability: the boundary is an explicit protocol with its own
//     version, testable from both sides (Go tests use a fake host; C++
//     tests run the dispatch loop directly).
//   - Performance: the boundary only carries COARSE operations (lifecycle,
//     health, hardware, metrics, and in later phases whole generation
//     requests streaming chunks). It is never used for tiny
//     high-frequency calls, so IPC overhead is irrelevant at this
//     granularity.
//
// # Concern layout
//
// The package separates the engine's concerns as files:
//
//	protocol.go   wire protocol (framing, ops, validation; v2 includes
//	              the model ops)
//	runtime.go    subprocess supervision + authoritative native state
//	backend.go    llm.Backend adapter (selection + fallback seam)
//	platform.go   hardware profile assembly (native probe + sysinfo)
//	metrics.go    native metrics snapshot
//	model.go      model lifecycle (REAL in Phase 2: load/unload/info,
//	              state machine, host-restart resets)
//	memory.go     memory plan types (plan values flow through model.go;
//	              the KV/compute detail types are for later phases)
//	kv.go         KV-cache types (types only — later phases)
//	generation.go generation request types (types only — later phases)
//	scheduler.go  request scheduler types (types only — later phases)
//
// The types-only files define the future data model so later phases extend
// instead of invent; they are wired into the metrics result where honest
// (measured fields only) and deliberately contain no fake inference.
package engine
