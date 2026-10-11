// version.h — SHEYTAN Native Engine ABI and protocol versioning.
//
// The Go core (internal/native/engine) checks BOTH values in the
// handshake: a mismatch is a hard startup failure (fail closed) so a
// stale engine build can never serve a newer Go core, or vice versa.
//
// Bump SHTN_ABI_VERSION whenever the C ABI (engine.h / types.h) changes
// shape. Bump SHTN_PROTOCOL_VERSION whenever the wire protocol
// (framing / ops / payload shapes) changes.
//
// Phase 1 (v1.1.5): both were 1.
// Phase 2 (v1.1.5): both are 2 — added the model surface
// (load_model / unload_model / model_info) to the wire protocol and
// shtn_engine_load_model / shtn_engine_unload_model /
// shtn_engine_model_info / shtn_engine_memory_plan to the C ABI.
// Pre-existing ops and structs kept their shapes (additive change).
//
// Phase 4 (v1.1.5): both are 3 — added the tokenizer / KV-cache /
// scheduler surface to the wire protocol (tokenizer_init /
// tokenizer_info / tokenizer_encode / tokenizer_decode / kv_cache_info /
// scheduler_info) and the matching C ABI functions. Pre-existing ops
// and structs keep their shapes (additive change — a v2 host can still
// be built against this header by ignoring the new functions).
//
// Phase 5 (v1.1.5): both are 4 — REAL native generation. Wire: the
// generate op streams event frames ({"id":N,"ok":true,"event":"chunk",
// "result":{...}}) followed by one final frame; the cancel op now
// addresses real generation requests. C ABI:
// shtn_engine_generate / shtn_engine_cancel_generation /
// shtn_engine_generation_stats; shtn_model_info extended with
// generation_capable + generation_reason (appended fields); new error
// codes -9..-11. Pre-existing op RESULT shapes stay compatible (the
// generate/cancel semantics changed from Phase 4's honest "no generation"
// stubs to real behaviour — that is the point of this phase).

#ifndef SHTN_VERSION_H
#define SHTN_VERSION_H

#define SHTN_ABI_VERSION 4u
#define SHTN_PROTOCOL_VERSION 4

/* Engine identity reported by the ping op. */
#define SHTN_ENGINE_NAME "shtn-native-engine"

#endif /* SHTN_VERSION_H */
