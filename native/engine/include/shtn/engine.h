// engine.h — the SHEYTAN Native Engine C ABI (v1.1.5 Phase 4).
//
// This is the NARROW boundary the Go core sees. Only C types, only
// coarse operations — no C++ classes, templates or exceptions cross this
// line. The Go core does NOT link this ABI directly; it supervises the
// shtn-engine-host subprocess which wraps exactly these functions behind
// the length-prefixed JSON IPC protocol (see doc.go in
// internal/native/engine for the boundary rationale).
//
// Phase 1 surface (implemented, unchanged):
//   shtn_engine_create / shtn_engine_destroy   engine lifecycle
//   shtn_engine_health                          active health probe
//   shtn_engine_hardware_info                   detected hardware profile
//   shtn_engine_metrics                         measured metrics
//   shtn_abi_version                            ABI negotiation
//
// Phase 2 surface (implemented — native GGUF model loading):
//   shtn_engine_load_model                      validate + map + plan
//   shtn_engine_unload_model                    release everything
//   shtn_engine_model_info                      real metadata snapshot
//   shtn_engine_memory_plan                     load-time budget
//
// Phase 4 surface (implemented — foundation primitives; NO inference):
//   shtn_engine_tokenizer_init                  materialize GGUF tokenizer
//   shtn_engine_tokenizer_info                  vocab snapshot
//   shtn_engine_tokenizer_encode                UTF-8 → token ids
//   shtn_engine_tokenizer_decode                token ids → UTF-8
//   shtn_engine_kv_cache_info                   measured KV cache snapshot
//   shtn_engine_scheduler_info                  measured scheduler snapshot
//
// NOT implemented (later phases — the honest answer is an error code):
//   inference / token generation / GPU kernels / forward pass.
//
// The Phase 4 surface is real but does NOT produce generated text:
// the tokenizer, KV cache, scheduler and sampler are implemented and
// measured, but the transformer forward pass is not. Generation requests
// still return SHTN_ERR_UNSUPPORTED and the llama.cpp fallback remains
// the production generation backend.
//
// Conventions:
//   - every call returns int32_t: 0 = success, negative = error code;
//   - structs are filled only with values this build can actually
//     detect, measure, read or derive;
//   - all calls are safe under concurrency per engine instance (internal
//     mutex); NULL arguments are rejected, never dereferenced.

#ifndef SHTN_ENGINE_H
#define SHTN_ENGINE_H

#include "types.h"
#include "version.h"

#ifdef __cplusplus
extern "C" {
#endif

/* Error codes (stable ABI values; Phase 4/5 additions are appended —
 * existing values are never renumbered). */
enum {
    SHTN_OK = 0,
    SHTN_ERR_INVALID_ARG = -1,   /* NULL or otherwise invalid argument */
    SHTN_ERR_ABI = -2,           /* caller ABI does not match engine ABI */
    SHTN_ERR_UNSUPPORTED = -3,   /* operation reserved for a later phase */
    SHTN_ERR_INTERNAL = -4,      /* unexpected internal failure */
    SHTN_ERR_MODEL_FORMAT = -5,  /* malformed / unsupported GGUF file */
    SHTN_ERR_MODEL_STATE = -6,   /* model state rejects the operation */
    SHTN_ERR_NO_MODEL = -7,      /* operation requires a loaded model */
    SHTN_ERR_QUEUE_FULL = -8,    /* Phase 4: scheduler queue is full */
    SHTN_ERR_CANCELLED = -9,     /* Phase 5: request cancelled (not error) */
    SHTN_ERR_CONTEXT_OVERFLOW = -10, /* Phase 5: prompt+max_tokens exceeds
                                        the model context */
    SHTN_ERR_GENERATION = -11    /* Phase 5: generation failed mid-flight */
};

/* Opaque engine handle. */
typedef struct shtn_engine shtn_engine;

/* shtn_engine_options configures engine creation. The caller MUST set
 * abi_version = SHTN_ABI_VERSION; a mismatch fails with SHTN_ERR_ABI so
 * incompatible builds fail closed at creation, not mid-flight. */
typedef struct shtn_engine_options {
    uint32_t abi_version;
    uint32_t reserved; /* must be 0 */
} shtn_engine_options;

/* Create an engine instance. On success *out holds the handle and the
 * engine reports healthy/ready. Destroy the handle with
 * shtn_engine_destroy. */
int32_t shtn_engine_create(const shtn_engine_options* opts, shtn_engine** out);

/* Destroy an engine instance. NULL is accepted and ignored. Any loaded
 * model (memory mapping, file handle) is released. */
void shtn_engine_destroy(shtn_engine* engine);

/* Fill out with the engine's current health. A created engine is healthy
 * until destroyed (a FAILED model load does not make the engine
 * unhealthy — the failure is reported on the model surface). The detail
 * string summarizes the model concern. */
int32_t shtn_engine_health(const shtn_engine* engine, shtn_health_status* out);

/* Fill out with the detected hardware profile of this machine (as seen
 * by the native binary). Only detectable values are filled; see
 * types.h. */
int32_t shtn_engine_hardware_info(shtn_engine* engine, shtn_hardware_info* out);

/* Fill out with measured engine metrics (state, uptime, own process RSS,
 * active request count). */
int32_t shtn_engine_metrics(shtn_engine* engine, shtn_metrics* out);

/* Load the GGUF model at path: validate the file (magic, version,
 * metadata, tensor table, bounds), map it read-only (memory-mapped, lazy
 * page-in — no eager copy of the tensor data), derive the metadata
 * snapshot and compute the memory plan. Replaces any previously loaded
 * model (replace semantics: unload, then load — all-or-nothing; a failed
 * load leaves NO model loaded and every resource released). Concurrent
 * load/unload calls are serialized; a load while another load is in
 * flight fails with SHTN_ERR_MODEL_STATE. */
int32_t shtn_engine_load_model(shtn_engine* engine, const char* path,
                               const shtn_model_load_options* opts);

/* Unload the current model and release all of its resources (mapping,
 * file handle, cached metadata). Idempotent: unloading an engine with no
 * model succeeds and leaves the state "unloaded". */
int32_t shtn_engine_unload_model(shtn_engine* engine);

/* Fill out with the model concern snapshot (state, metadata, plan
 * summary). Always succeeds for a valid engine; with no model attempted
 * the state is "unloaded" and every metadata field stays 0/empty. */
int32_t shtn_engine_model_info(const shtn_engine* engine, shtn_model_info* out);

/* Fill out with the memory plan of the CURRENT model (a failed or
 * finished load keeps the last computed plan for inspection; before any
 * load attempt every field is 0). Does not allocate. */
int32_t shtn_engine_memory_plan(const shtn_engine* engine, shtn_memory_plan* out);

/* --- Phase 4: tokenizer / KV / scheduler surface ----------------------- *
 *
 * These functions expose the Phase 4 foundation primitives to the host.
 * They are REAL: the tokenizer reads GGUF arrays, the KV cache is a real
 * allocation sized from model dims, the scheduler is a real bounded
 * queue. They are NOT inference — no forward pass exists, no token is
 * ever generated. The llama.cpp fallback remains the generation backend.
 */

/* Materialize the GGUF tokenizer for the currently loaded model. Safe
 * to call repeatedly (idempotent — a second call returns SHTN_OK with
 * the existing vocab). Returns SHTN_ERR_UNSUPPORTED for an
 * unimplemented tokenizer model kind; SHTN_ERR_NO_MODEL when no model
 * is loaded. */
int32_t shtn_engine_tokenizer_init(shtn_engine* engine, shtn_tokenizer_info* out);

/* Fill out with the tokenizer snapshot (initialized flag, vocab size,
 * special token ids, model kind). Always succeeds for a valid engine;
 * an uninitialized tokenizer leaves initialized=0. */
int32_t shtn_engine_tokenizer_info(const shtn_engine* engine,
                                   shtn_tokenizer_info* out);

/* Encode UTF-8 text to token ids. The caller allocates result->ids with
 * capacity opts->max_tokens. Returns SHTN_OK or a negative error code;
 * result->ids_count holds the number of ids written. */
int32_t shtn_engine_tokenizer_encode(const shtn_engine* engine,
                                     const char* text, uint64_t text_len,
                                     const shtn_encode_options* opts,
                                     shtn_encode_result* result);

/* Decode token ids to UTF-8 text. The caller allocates result->text with
 * capacity opts->max_bytes. Returns SHTN_OK or a negative error code;
 * result->text_count holds the number of bytes written (excl. NUL). */
int32_t shtn_engine_tokenizer_decode(const shtn_engine* engine,
                                     const uint32_t* ids, uint64_t ids_count,
                                     const shtn_decode_options* opts,
                                     shtn_decode_result* result);

/* Fill out with the measured KV-cache snapshot. In Phase 4 the cache is
 * NOT allocated automatically — it exists as a data structure sized
 * from model dims but is not populated until a forward pass exists.
 * This op reports the honest zero-state (allocated=0) unless a future
 * op explicitly allocates the cache. */
int32_t shtn_engine_kv_cache_info(const shtn_engine* engine,
                                  shtn_kv_cache_info* out);

/* Fill out with the measured scheduler snapshot. Counts are real
 * (queued requests, totals since create); active is 0 in Phase 4. */
int32_t shtn_engine_scheduler_info(const shtn_engine* engine,
                                   shtn_scheduler_info* out);

/* --- Phase 5: REAL native generation surface ---------------------------- *
 *
 * shtn_engine_generate performs a REAL transformer forward pass (llama
 * architecture: RMSNorm, RoPE, GQA causal self-attention over the fp16
 * KV cache, SwiGLU FFN, logits) and generates tokens with the Phase 4
 * sampler consuming the REAL logits. The call BLOCKS until generation
 * completes (EOS / max_tokens / context bound / cancellation / error)
 * and streams coarse-grained chunks through `emit` (one frame per
 * emission window, never one per token). It executes on the engine's
 * single-slot scheduler (queued behind any earlier request; bounded
 * queue). Cancellation: shtn_engine_cancel_generation sets the active
 * request's flag; the generation loop observes it at every token and at
 * every prefill window boundary.
 */

/* Generate. opts->prompt (UTF-8) is tokenized by the engine's own GGUF
 * tokenizer (materialized on demand). out (optional) receives the final
 * result (finish reason + measured metrics). Returns:
 *   SHTN_OK                    generation completed (any finish reason);
 *   SHTN_ERR_INVALID_ARG       NULL/bad options (prompt, max_tokens...);
 *   SHTN_ERR_NO_MODEL          no model loaded;
 *   SHTN_ERR_UNSUPPORTED       model not natively executable / tokenizer
 *                              unsupported (reason in `detail`);
 *   SHTN_ERR_CONTEXT_OVERFLOW  prompt+max_tokens exceeds the context;
 *   SHTN_ERR_CANCELLED         cancelled by request;
 *   SHTN_ERR_QUEUE_FULL        scheduler queue full;
 *   SHTN_ERR_MODEL_STATE       unload/reload in flight;
 *   SHTN_ERR_GENERATION        mid-flight inference failure;
 *   SHTN_ERR_INTERNAL          internal failure.
 *
 * `detail` (optional, 256 bytes) receives a human-readable reason on any
 * non-OK return. Thread-safe: concurrent callers queue on the scheduler. */
int32_t shtn_engine_generate(shtn_engine* engine,
                             const shtn_generation_options* opts,
                             shtn_generation_emit_fn emit, void* user,
                             shtn_generation_result* out, char* detail);

/* Cancel the in-flight (or queued) generation request with this id.
 * Returns SHTN_OK for a valid engine (arguments validated); *cancelled is
 * 1 when a matching request was found and marked for cancellation (the
 * blocked generate call then returns SHTN_ERR_CANCELLED), 0 when no such
 * request exists, with `reason` filled (bounded 128 bytes). Always safe
 * to call; a miss never disturbs the engine. */
int32_t shtn_engine_cancel_generation(shtn_engine* engine,
                                       const char* request_id,
                                       int32_t* cancelled, char* reason);

/* Fill out with the generation concern snapshot (active request count,
 * totals, last request's measured metrics). Always succeeds for a valid
 * engine. */
int32_t shtn_engine_generation_stats(const shtn_engine* engine,
                                      shtn_generation_stats* out);

/* Report the ABI version this engine was built with. */
uint32_t shtn_abi_version(void);

#ifdef __cplusplus
} /* extern "C" */
#endif

#endif /* SHTN_ENGINE_H */
