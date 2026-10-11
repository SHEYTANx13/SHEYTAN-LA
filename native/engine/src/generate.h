// generate.h — the native generation runner (Phase 5 — REAL).
//
// One generation request: tokenize (engine's own GGUF tokenizer) →
// context-bound check → KV reset → prefill → decode loop (forward pass →
// sampler over REAL logits → stop conditions) → streamed chunks →
// measured metrics.
//
// Stop conditions (all REAL):
//   EOS token sampled (not emitted into the text);
//   max_tokens reached;
//   context exhausted (prompt + generated == context);
//   cooperative cancellation (observed every token during decode and
//   every prefill window during prompt processing);
//   mid-flight inference error (explicit error return).
//
// Chunk emission (coarse-grained by design): the FIRST sampled token is
// emitted immediately (honest TTFT measurement at the consumer), then
// text accumulates and flushes when ≥ kEmitMinBytes bytes or
// ≥ kEmitTokenWindow tokens are pending; the final chunk flushes the
// remainder. Emitted text never splits an incomplete UTF-8 sequence —
// trailing partial bytes are held back until the sequence completes (or
// flushed at the end when the model truly terminates mid-sequence).
//
// The sampler is the Phase 4 sampler consuming the REAL logits row
// produced by the transformer forward pass (never synthetic logits).
//
// Metrics: monotonic (steady) clock only; every reported number is
// measured; zero means not measured (never a guess).

#ifndef SHTN_GENERATE_H
#define SHTN_GENERATE_H

#include "shtn/types.h"

#include "forward.h"
#include "model.h"

#include <atomic>
#include <cstdint>
#include <mutex>
#include <string>
#include <vector>

namespace shtn {
namespace gen {

// Emission batching bounds (coarse-grained streaming).
constexpr uint32_t kEmitTokenWindow = 8;   // tokens per chunk (max)
constexpr uint32_t kEmitMinBytes = 24;     // flush threshold (bytes)
constexpr uint32_t kDefaultRepeatLastN = 64;
constexpr uint32_t kMaxRecentTokens = 4096; // repetition window cap

// Spec is one fully-parsed generation request.
struct Spec {
    std::string request_id;
    std::string prompt;
    uint32_t max_tokens = 64;
    float temperature = 1.0f;
    int32_t top_k = 0;
    float top_p = 1.0f;
    float repetition_penalty = 1.0f;
    uint32_t repeat_last_n = kDefaultRepeatLastN;
    uint64_t seed = 0;
};

// Runner owns the per-engine generation state: the Forward binding
// (KV cache + scratch, rebound when the model changes) and the measured
// stats. Execution is serialized by the scheduler's single worker slot.
class Runner {
public:
    Runner() = default;

    Runner(const Runner&) = delete;
    Runner& operator=(const Runner&) = delete;

    // execute runs one generation synchronously (called from the
    // scheduler worker). cancel is observed cooperatively. Returns
    // SHTN_OK for any terminal finish reason (eos/length/cancelled) with
    // result filled; negative error codes for failures (detail filled).
    int32_t execute(model::Model& model, const Spec& spec,
                    const std::atomic<bool>& cancel,
                    shtn_generation_emit_fn emit, void* user,
                    shtn_generation_result* result, std::string& detail);

    // stats snapshots the generation concern (thread-safe).
    shtn_generation_stats stats() const;

    // kv_stats snapshots the REAL KV cache of the current forward
    // binding (allocated at first generate; populated by the forward
    // pass). Honest zero-state before any generation.
    kv::Stats kv_stats() const;

    // abort_model_cycle invalidates the forward binding when the model
    // changes/unloads (called by the engine's model surface).
    void abort_model_cycle();

private:
    // ensure_binding (re)binds the forward state to the current model.
    int32_t ensure_binding(model::Model& model, std::string& detail);

    // execute_impl is the request body (stats accounting lives in
    // execute's single exit point).
    int32_t execute_impl(model::Model& model, const Spec& spec,
                         const std::atomic<bool>& cancel,
                         shtn_generation_emit_fn emit, void* user,
                         shtn_generation_result* result,
                         std::string& detail);

    mutable std::mutex mu_;
    fwd::Forward forward_;
    uint64_t bound_epoch_ = 0; // model load epoch the binding belongs to
    bool binding_valid_ = false;
    shtn_generation_stats stats_{};
};

// utf8_complete_len returns the length of the longest COMPLETE UTF-8
// sequence prefix of buf[0..len) — used to hold back partial sequences
// at chunk boundaries.
uint32_t utf8_complete_len(const char* buf, uint32_t len);

} // namespace gen
} // namespace shtn

#endif /* SHTN_GENERATE_H */
