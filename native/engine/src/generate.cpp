// generate.cpp — the native generation runner implementation (Phase 5).

#include "generate.h"

#include "shtn/engine.h"
#include "sampler.h"
#include "util.h"
#include "tokenizer.h"

#include <algorithm>
#include <chrono>
#include <cstring>

namespace shtn {
namespace gen {

namespace {

using Clock = std::chrono::steady_clock;

// find_eos resolves the model's EOS token (0 / has_eos=false when the
// tokenizer has none).
struct EosRef {
    bool has = false;
    uint32_t id = 0;
};

EosRef eos_of(const tokenizer::Vocab& v) {
    EosRef e;
    if (v.special.has_eos && v.special.eos < v.size()) {
        e.has = true;
        e.id = v.special.eos;
    }
    return e;
}

} // namespace

uint32_t utf8_complete_len(const char* buf, uint32_t len) {
    if (len == 0) return 0;

    uint32_t i = 0;
    while (i < len) {
        const unsigned char b = static_cast<unsigned char>(buf[i]);

        uint32_t need = 1;
        if (b < 0x80) {
            need = 1;
        } else if ((b & 0xE0) == 0xC0) {
            need = 2;
        } else if ((b & 0xF0) == 0xE0) {
            need = 3;
        } else if ((b & 0xF8) == 0xF0) {
            need = 4;
        } else {
            // Invalid lead byte: it is not part of a valid sequence that
            // can complete — treat as complete (emit as-is; the tokenizer
            // is the authority on token bytes).
            need = 1;
        }

        if (i + need > len) {
            // Incomplete sequence at the tail → hold back from here.
            return i;
        }

        // Continuation bytes must be 10xxxxxx.
        for (uint32_t k = 1; k < need; ++k) {
            const unsigned char c =
                static_cast<unsigned char>(buf[i + k]);
            if ((c & 0xC0) != 0x80) {
                // Malformed: treat the lead byte as a single unit.
                need = 1;
                break;
            }
        }

        i += need;
    }

    return len;
}

shtn_generation_stats Runner::stats() const {
    std::lock_guard<std::mutex> lock(mu_);
    return stats_;
}

kv::Stats Runner::kv_stats() const {
    std::lock_guard<std::mutex> lock(mu_);
    return forward_.cache().stats();
}

void Runner::abort_model_cycle() {
    std::lock_guard<std::mutex> lock(mu_);
    binding_valid_ = false;
    bound_epoch_ = 0;
    stats_ = shtn_generation_stats{};
}

int32_t Runner::ensure_binding(model::Model& model, std::string& detail) {
    std::lock_guard<std::mutex> lock(mu_);

    if (binding_valid_ && bound_epoch_ == model.epoch()) {
        return SHTN_OK;
    }

    // (Re)bind to the current model.
    if (model.state() != SHTN_MODEL_STATE_LOADED) {
        detail = "generation: no model loaded";
        return SHTN_ERR_NO_MODEL;
    }
    if (!model.generation_capable()) {
        detail = "generation: model is not natively executable — " +
                 model.generation_reason();
        return SHTN_ERR_UNSUPPORTED;
    }

    const tensor::Weights* w = model.weights();
    if (w == nullptr) {
        detail = "generation: model weights not bound";
        return SHTN_ERR_MODEL_STATE;
    }

    const uint64_t avail = model.available_ram_bytes();
    std::string error;
    const int32_t rc = forward_.init(w, model.hyper(), avail, error);
    if (rc != SHTN_OK) {
        detail = "generation: forward init failed — " + error;
        binding_valid_ = false;
        return rc;
    }

    bound_epoch_ = model.epoch();
    binding_valid_ = true;
    return SHTN_OK;
}

int32_t Runner::execute(model::Model& model, const Spec& spec,
                        const std::atomic<bool>& cancel,
                        shtn_generation_emit_fn emit, void* user,
                        shtn_generation_result* result, std::string& detail) {
    // Every request the runner processes counts exactly once, including
    // rejected ones (arg/model/context validation failures) — the totals
    // are real, never silently dropped.
    {
        std::lock_guard<std::mutex> lock(mu_);
        stats_.active_requests = 1;
        stats_.total_requests += 1;
    }

    const int32_t rc =
        execute_impl(model, spec, cancel, emit, user, result, detail);

    {
        std::lock_guard<std::mutex> lock(mu_);
        stats_.active_requests = 0;
        if (rc == SHTN_OK) {
            stats_.total_completed += 1;
        } else if (rc == SHTN_ERR_CANCELLED) {
            stats_.total_cancelled += 1;
        } else {
            stats_.total_failed += 1;
        }
        if (result != nullptr) {
            stats_.ttft_seconds = result->metrics.ttft_seconds;
            stats_.tokens_per_second = result->metrics.tokens_per_second;
            stats_.prompt_tokens_per_second =
                result->metrics.prompt_tokens_per_second;
            stats_.last_prompt_tokens = result->metrics.prompt_tokens;
            stats_.last_generated_tokens = result->metrics.generated_tokens;
        }
    }

    return rc;
}

int32_t Runner::execute_impl(model::Model& model, const Spec& spec,
                             const std::atomic<bool>& cancel,
                             shtn_generation_emit_fn emit, void* user,
                             shtn_generation_result* result,
                             std::string& detail) {
    const Clock::time_point t0 = Clock::now();

    // --- request validation --------------------------------------------------
    if (spec.prompt.empty()) {
        detail = "generation: prompt is empty";
        return SHTN_ERR_INVALID_ARG;
    }
    if (spec.max_tokens == 0 || spec.max_tokens > (1u << 20)) {
        detail = "generation: max_tokens out of range";
        return SHTN_ERR_INVALID_ARG;
    }

    // --- model + capability ---------------------------------------------------
    if (model.state() != SHTN_MODEL_STATE_LOADED) {
        detail = "generation: no model loaded";
        return SHTN_ERR_NO_MODEL;
    }
    if (!model.generation_capable()) {
        detail = "generation: model is not natively executable — " +
                 model.generation_reason();
        return SHTN_ERR_UNSUPPORTED;
    }

    // --- tokenizer (materialize on demand; idempotent) -------------------------
    {
        std::string err;
        const int32_t rc = model.init_tokenizer(err);
        if (rc != SHTN_OK) {
            detail = "generation: tokenizer unavailable — " + err +
                     " (llama.cpp fallback remains the generation backend)";
            return SHTN_ERR_UNSUPPORTED;
        }
    }

    const tokenizer::Vocab* vocab = model.tokenizer_vocab();
    if (vocab == nullptr || !vocab->initialized()) {
        detail = "generation: tokenizer not initialized";
        return SHTN_ERR_UNSUPPORTED;
    }

    // --- prompt encoding (the engine's own tokenizer) ---------------------------
    tokenizer::EncodeResult enc;
    {
        tokenizer::EncodeOptions eopts;
        eopts.add_bos = vocab->special.has_bos; // standard llama priming
        eopts.add_eos = false;
        eopts.max_tokens = static_cast<uint32_t>(tokenizer::kMaxEncodeOutput);

        std::string err;
        const int32_t rc =
            tokenizer::encode(*vocab, spec.prompt, eopts, enc, err);
        if (rc != SHTN_OK) {
            detail = "generation: prompt encoding failed — " + err;
            return rc;
        }
    }

    if (enc.ids.empty()) {
        detail = "generation: prompt encoded to zero tokens";
        return SHTN_ERR_INVALID_ARG;
    }

    const uint64_t context = model.hyper().context;

    // --- context bound (reject policy — documented, no silent truncation) ------
    {
        const uint64_t needed =
            static_cast<uint64_t>(enc.ids.size()) + spec.max_tokens;
        if (needed > context) {
            detail = "generation: prompt (" + std::to_string(enc.ids.size()) +
                     " tokens) + max_tokens (" +
                     std::to_string(spec.max_tokens) +
                     ") exceeds the model context window (" +
                     std::to_string(context) + " tokens); request rejected";
            return SHTN_ERR_CONTEXT_OVERFLOW;
        }
    }

    // --- forward binding (KV cache + scratch, once per model) -------------------
    {
        const int32_t rc = ensure_binding(model, detail);
        if (rc != SHTN_OK) {
            return rc;
        }
    }

    // Per-request boundary: the KV cache resets (no stale state from a
    // previous request can leak into this one).
    {
        std::lock_guard<std::mutex> lock(mu_);
        forward_.reset();
    }

    // --- generation state -------------------------------------------------------
    const EosRef eos = eos_of(*vocab);

    sampler::Config scfg;
    scfg.temperature = spec.temperature;
    scfg.top_k = spec.top_k;
    scfg.top_p = spec.top_p;
    scfg.repetition_penalty = spec.repetition_penalty;
    scfg.seed = spec.seed;
    scfg.max_tokens = spec.max_tokens;
    scfg.eos_token_id = eos.id;
    scfg.has_eos = eos.has;

    sampler::RNG rng(spec.seed);

    std::vector<uint32_t> history(enc.ids.begin(), enc.ids.end());
    std::vector<uint32_t> generated;

    std::string pending;             // text awaiting emission
    uint32_t tokens_since_emit = 0;  // tokens in the pending window
    int32_t emit_rc = 0;             // consumer abort signal

    uint64_t pos = 0;                // next cache write position

    const auto emit_chunk = [&](bool final_chunk) -> bool {
        if (emit == nullptr) {
            return true; // no consumer: events dropped, generation runs on
        }

        // Hold back an incomplete UTF-8 tail unless this is the final
        // flush (the model terminated; the remaining bytes belong to the
        // output as-is).
        std::string text;
        if (final_chunk) {
            text = pending;
        } else {
            const uint32_t complete =
                utf8_complete_len(pending.data(),
                                  static_cast<uint32_t>(pending.size()));
            text.assign(pending, 0, complete);
        }

        shtn_generation_chunk chunk{};
        chunk.text = text.data();
        chunk.text_len = static_cast<uint32_t>(text.size());
        chunk.token_id = generated.empty() ? 0 : generated.back();
        chunk.final = final_chunk ? 1 : 0;

        emit_rc = emit(user, &chunk);

        // Advance the pending buffer past what was emitted.
        pending.erase(0, text.size());
        tokens_since_emit = 0;

        return emit_rc == 0;
    };

    const auto want_emit = [&]() {
        return tokens_since_emit >= kEmitTokenWindow ||
               pending.size() >= kEmitMinBytes;
    };

    // Final result bookkeeping. (Timestamps declared before the lambda
    // that captures them by reference.)
    Clock::time_point t_prefill_end = t0;
    Clock::time_point t_first_token = t0;

    std::string finish_reason = SHTN_FINISH_LENGTH;
    int32_t rc = SHTN_OK;

    const auto fill_result = [&](shtn_generation_result* out) {
        if (out == nullptr) return;
        std::memset(out, 0, sizeof(*out));
        copy_cstr(out->finish_reason, sizeof(out->finish_reason),
                  finish_reason.c_str());

        const Clock::time_point t_end = Clock::now();
        auto dur = [](Clock::time_point a, Clock::time_point b) {
            return std::chrono::duration<double>(b - a).count();
        };
        out->metrics.prompt_tokens =
            static_cast<uint32_t>(enc.ids.size());
        out->metrics.generated_tokens =
            static_cast<uint32_t>(generated.size());
        out->metrics.prompt_seconds = dur(t0, t_prefill_end);
        out->metrics.ttft_seconds =
            generated.empty() ? 0.0 : dur(t0, t_first_token);
        out->metrics.decode_seconds =
            generated.empty() ? 0.0 : dur(t_first_token, t_end);
        out->metrics.total_seconds = dur(t0, t_end);
        if (!generated.empty() && out->metrics.decode_seconds > 0) {
            out->metrics.tokens_per_second =
                static_cast<double>(generated.size()) /
                out->metrics.decode_seconds;
        }
        if (out->metrics.prompt_seconds > 0) {
            out->metrics.prompt_tokens_per_second =
                static_cast<double>(enc.ids.size()) /
                out->metrics.prompt_seconds;
        }
        out->metrics.kv_positions_used = pos;
    };

    // --- prefill ------------------------------------------------------------------
    //
    // v1.9.2 BATCHED PREFILL (the measured P0 TTFT boundary): the prompt
    // runs through Forward::prefill_span in bounded 16-token chunks —
    // the same causal layer math as the serial path (KV written per
    // position, attention over [0, pos]), with the vocabulary logits
    // projection ONLY for the final prompt token (intermediate prompt
    // logits were never consumed). Cancellation stays observed at the
    // documented 16-token cadence — between chunks. The chunking bounds
    // the un-cancellable window; no sleeps, no busy waits.
    {
        std::string error;
        constexpr uint32_t kPrefillChunk = 16; // cancel-observation cadence
        size_t i = 0;
        while (i < enc.ids.size()) {
            if (cancel.load(std::memory_order_acquire)) {
                finish_reason = SHTN_FINISH_CANCELLED;
                rc = SHTN_ERR_CANCELLED;
                break;
            }

            const uint32_t chunk = static_cast<uint32_t>(
                std::min<size_t>(kPrefillChunk, enc.ids.size() - i));
            // Logits only from the FINAL prompt token (the decode loop's
            // first sample reads them from forward_.logits_buf()).
            const bool last = (i + chunk == enc.ids.size());
            int32_t frc = SHTN_OK;
            {
                // Same lock discipline as the serial path and the decode
                // feed step: forward state is touched under mu_ (bounded
                // per chunk; unload is refused while a generation is in
                // flight, so the mapping cannot move under this call).
                std::lock_guard<std::mutex> lock(mu_);
                frc = forward_.prefill_span(
                    enc.ids.data() + i, chunk, pos, last,
                    last ? forward_.logits_buf() : nullptr, error);
            }
            if (frc != SHTN_OK) {
                detail = "generation: prefill failed — " + error;
                finish_reason = frc == SHTN_ERR_CANCELLED
                                    ? SHTN_FINISH_CANCELLED
                                    : "error";
                rc = frc;
                break;
            }
            i += chunk;
            pos += chunk;
        }
        t_prefill_end = Clock::now();
    }

    // --- decode loop ----------------------------------------------------------------
    if (rc == SHTN_OK) {
        // First token from the last prefill logits (still in
        // forward_.logits_buf()).
        for (;;) {
            if (cancel.load(std::memory_order_acquire)) {
                finish_reason = SHTN_FINISH_CANCELLED;
                rc = SHTN_ERR_CANCELLED;
                break;
            }

            uint32_t tok = 0;
            {
                std::string error;
                const uint32_t recent_n = spec.repeat_last_n == 0
                                              ? kDefaultRepeatLastN
                                              : std::min(spec.repeat_last_n,
                                                         kMaxRecentTokens);
                const uint32_t skip = history.size() > recent_n
                                          ? static_cast<uint32_t>(
                                                history.size() - recent_n)
                                          : 0;
                const int32_t src = sampler::sample(
                    forward_.logits_buf(), forward_.hyper().vocab_size,
                    history.data() + skip,
                    history.size() - skip, scfg, rng, tok, error);
                if (src != SHTN_OK) {
                    detail = "generation: sampling failed — " + error;
                    finish_reason = "error";
                    rc = SHTN_ERR_GENERATION;
                    break;
                }
            }

            // EOS: stop WITHOUT emitting the EOS token into the text.
            if (eos.has && tok == eos.id) {
                finish_reason = SHTN_FINISH_EOS;
                rc = SHTN_OK;
                break;
            }

            if (generated.empty()) {
                t_first_token = Clock::now();
            }

            generated.push_back(tok);
            history.push_back(tok);

            // Decode the token to text (control/special tokens skipped by
            // the tokenizer's decode).
            {
                tokenizer::DecodeOptions dopts;
                dopts.skip_special = true;
                dopts.max_bytes = 1u << 16;
                tokenizer::DecodeResult dr;
                std::string error;
                const int32_t drc =
                    tokenizer::decode(*vocab, &tok, 1, dopts, dr, error);
                if (drc != SHTN_OK) {
                    detail = "generation: token decode failed — " + error;
                    finish_reason = "error";
                    rc = SHTN_ERR_GENERATION;
                    break;
                }
                pending += dr.text;
                ++tokens_since_emit;
            }

            // First token: immediate emission (honest TTFT for the
            // consumer). Afterwards: batched windows.
            if (generated.size() == 1 || want_emit()) {
                if (!emit_chunk(false)) {
                    detail = "generation: consumer aborted the stream";
                    finish_reason = "error";
                    rc = SHTN_ERR_INTERNAL;
                    break;
                }
            }

            // Hard generation cap.
            if (generated.size() >= spec.max_tokens) {
                finish_reason = SHTN_FINISH_LENGTH;
                rc = SHTN_OK;
                break;
            }

            // Context exhaustion: no room for another position.
            if (pos >= context) {
                finish_reason = SHTN_FINISH_LENGTH;
                rc = SHTN_OK;
                break;
            }

            // Feed the sampled token back through the transformer.
            {
                std::string error;
                std::lock_guard<std::mutex> lock(mu_);
                const int32_t frc =
                    forward_.token(tok, pos, forward_.logits_buf(), error);
                if (frc != SHTN_OK) {
                    detail = "generation: decode step failed — " + error;
                    finish_reason = "error";
                    rc = SHTN_ERR_GENERATION;
                    break;
                }
            }
            ++pos;
        }
    }

    // --- final emission + result -------------------------------------------------------
    // Flush any remaining text (final marker set).
    (void)emit_chunk(true);

    fill_result(result);

    // A cancelled request reports SHTN_ERR_CANCELLED with a valid result
    // (finishReason "cancelled"); everything else per rc.
    return rc;
}

} // namespace gen
} // namespace shtn
