// forward.h — the llama transformer forward pass (Phase 5 — REAL).
//
// One token in → vocabulary logits out, with the K/V written to the fp16
// KV cache at the token's position and attention reading all cached
// positions <= the current one (causal).
//
// The exact computation (llama architecture, derived from the GGUF
// hyper parameters — never hard-coded):
//
//   x  = token_embd[token]
//   for layer l in 0..L-1:
//     xb  = rms_norm(x, attn_norm[l], eps)
//     q   = Wq[l]  @ xb          (heads * head_dim outputs)
//     k   = Wk[l]  @ xb          (kv_heads * head_dim outputs)
//     v   = Wv[l]  @ xb
//     RoPE(q, pos), RoPE(k, pos)  (NORM pairing, freq = base^(-2j/hd))
//     K[l][pos] = k ; V[l][pos] = v        (fp16 cache write)
//     for head h: scores_j = (q_h . K[l][j]) / sqrt(head_dim),
//                 j in [0, pos]            (causal mask by construction)
//     attn_h = softmax(scores) @ V[l][j..]  (GQA: head h reads kv head
//                                            h / (heads / kv_heads))
//     a   = concat(attn_h)
//     o   = Wo[l] @ a
//     x   = x + o                 (residual)
//     xb  = rms_norm(x, ffn_norm[l], eps)
//     g   = silu(Wg[l] @ xb) * (Wu[l] @ xb)
//     x   = x + (Wd[l] @ g)       (residual)
//   xb = rms_norm(x, output_norm, eps)
//   logits[v] = output_row[v] . xb   (output.weight; token_embd if tied)
//
// Every unit (RMSNorm, RoPE, attention, matvec, SwiGLU) is a free
// function pinned by reference tests against independently computed
// values (see test_forward.cpp and tests/reference/).

#ifndef SHTN_FORWARD_H
#define SHTN_FORWARD_H

#include "fp16.h"
#include "kv_cache.h"
#include "llama.h"
#include "tensor.h"

#include <cmath>
#include <cstdint>
#include <string>
#include <vector>

namespace shtn {
namespace fwd {

// --- numerics (unit-testable free functions) --------------------------------

// rms_norm: out = x * w / sqrt(mean(x^2) + eps). In-place-safe (out may
// alias x). Numerically stable (mean accumulates in double).
void rms_norm(const float* x, const float* w, uint32_t n, double eps,
              float* out);

// rope: rotate ONE head's q (or k) at position pos, NORM pairing:
//   pairs (x[j], x[j+half]) for j in [0, half);
//   freq[j] = freq_base^(-2j/head_dim);
//   theta = pos * freq[j];
//   x[j]'     = x[j] * cos(theta) - x[j+half] * sin(theta)
//   x[j+half]' = x[j+half] * cos(theta) + x[j] * sin(theta)
// freqs (size head_dim/2) must be precomputed via rope_freqs().
void rope(float* head, uint32_t head_dim, uint64_t pos,
          const float* freqs);

// rope_freqs precomputes the inverse frequencies for one head dimension.
void rope_freqs(uint32_t head_dim, double freq_base, std::vector<float>& out);

// softmax_inplace: numerically stable (max subtraction).
void softmax_inplace(float* v, uint32_t n);

// silu(z) = z / (1 + exp(-z)).
inline float silu(float z) {
    return z / (1.0f + std::exp(-z));
}

// dot: plain dot product (double accumulator for determinism).
float dot(const float* a, const float* b, uint32_t n);

// Forward owns the per-model inference state: the KV cache, scratch
// buffers and the dequantized norm weights. NOT thread-safe by itself —
// the single-slot scheduler serializes execution.
class Forward {
public:
    Forward() = default;
    ~Forward() = default;

    Forward(const Forward&) = delete;
    Forward& operator=(const Forward&) = delete;

    // init binds the weights view + hyper parameters and allocates the
    // KV cache + scratch. available_ram_bytes (0 = unknown) bounds the
    // KV allocation. Idempotent per model binding: re-init releases the
    // previous state.
    int32_t init(const tensor::Weights* w, const llama::Hyper& h,
                 uint64_t available_ram_bytes, std::string& error);

    // reset marks the KV cache unused (per-request boundary). Scratch
    // stays allocated (reuse across requests — no churn).
    void reset();

    // token runs the full forward pass for ONE token at position pos:
    // writes K/V at pos, returns the vocabulary logits in logits_out
    // (capacity >= hyper().vocab_size). Returns SHTN_OK or a negative
    // error code (bounds, tensor access failures — never a crash).
    int32_t token(uint32_t token_id, uint64_t pos, float* logits_out,
                  std::string& error);

    // prefill_span (v1.9.2) processes a CONTIGUOUS span of prompt tokens
    // at successive positions [pos, pos+count) with the SAME layer math
    // as token() — per-token causal attention over all cached positions,
    // K/V written at each position — but WITHOUT the vocabulary logits
    // projection except for the span's LAST token (intermediate prompt
    // logits were never consumed; on real models the output projection is
    // the largest per-token cost). logits_out (capacity >= vocab_size) is
    // written only when with_last_logits is true; it may be nullptr
    // otherwise. Returns SHTN_OK or a negative error code; on error the
    // cache state for THIS span's unconsumed tokens is defined only up to
    // the failing token (the caller resets per request anyway).
    //
    // The generate loop calls this in bounded chunks (cancellation stays
    // observed at the documented cadence between chunks). Numerical
    // parity with token() is pinned by test_prefill_parity: per-position
    // logits through the chunk=1 span cadence (every token is a
    // span-final token there), last-token logits for the production
    // chunk sizes, and full KV bytes — all bit-for-bit.
    int32_t prefill_span(const uint32_t* ids, uint32_t count, uint64_t pos,
                         bool with_last_logits, float* logits_out,
                         std::string& error);

    // embed copies the token's embedding row into out (capacity >= emb)
    // without running the layers (used by tests).
    int32_t embed(uint32_t token_id, float* out, std::string& error) const;

    const llama::Hyper& hyper() const { return h_; }
    kv::Cache& cache() { return kv_; }
    const kv::Cache& cache() const { return kv_; }
    bool initialized() const { return initialized_; }

    // logits scratch exposure for the generator (avoids caller-side
    // allocation per step).
    float* logits_buf() { return logits_.data(); }

private:
    // ResolvedTensorView is one layer weight matrix resolved ONCE per
    // model binding (v1.9.2): name lookup + shape + type + byte-range
    // validation happen at init; the forward pass then walks rows by
    // pointer arithmetic. This removes the per-row tensor-name
    // resolution that dominated the measured per-token cost (19 ms/token
    // → the actual math) for BOTH prefill and decode.
    struct LayerWeights {
        tensor::ResolvedTensor q;    // attn_q      [emb, attn_out_dim]
        tensor::ResolvedTensor k;    // attn_k      [emb, kv_dim]
        tensor::ResolvedTensor v;    // attn_v      [emb, kv_dim]
        tensor::ResolvedTensor out;  // attn_out    [attn_out_dim, emb]
        tensor::ResolvedTensor gate; // ffn_gate    [emb, ffn]
        tensor::ResolvedTensor up;   // ffn_up      [emb, ffn]
        tensor::ResolvedTensor down; // ffn_down    [ffn, emb]
    };

    // matvec_resolved: out[i] = dot(row_i(M), x) for i in [0, M.rows),
    // rows dequantized through the shared row buffer (no name
    // resolution). Same numerics as the name-based matvec.
    int32_t matvec_resolved(const tensor::ResolvedTensor& m, const float* x,
                            float* out, std::string& error);

    // run_layers executes the full layer stack for ONE token at position
    // pos (embedding → attention block → FFN block, K/V written at pos,
    // cache advanced by one). Leaves the residual stream in x_. Both
    // token() and prefill_span run through this — one numerics authority.
    int32_t run_layers(uint32_t token_id, uint64_t pos, std::string& error);

    // resolve_model builds the per-layer resolved weight table + the
    // output projection record. Called once per binding from init(); any
    // failure aborts the binding (explicit error, never a guess).
    int32_t resolve_model(std::string& error);

    // project_logits computes the vocabulary logits from the final-norm
    // residual (xb_) through the resolved output projection and copies
    // them into logits_out (capacity >= vocab_size).
    int32_t project_logits(float* logits_out, std::string& error);

    const tensor::Weights* w_ = nullptr;
    llama::Hyper h_{};
    bool initialized_ = false;

    kv::Cache kv_;

    // Scratch buffers (allocated once per model binding, reused every
    // token — no per-token heap allocations).
    std::vector<float> x_;        // residual stream [emb]
    std::vector<float> xb_;       // normalized [emb]
    std::vector<float> q_;        // query projections [attn_out_dim]
    std::vector<float> k_;        // key projection [kv_dim]
    std::vector<float> v_;        // value projection [kv_dim]
    std::vector<float> attn_;     // attention output concat [attn_out_dim]
    std::vector<float> ffn_g_;    // gate [ffn]
    std::vector<float> ffn_u_;    // up [ffn]
    std::vector<float> ffn_d_;    // down output [emb]
    std::vector<float> logits_;   // [vocab]
    std::vector<float> scores_;   // attention scores [context, grows]
    tensor::RowBuf row_buf_;      // dequantized weight row scratch

    // RoPE frequencies (per head-dim).
    std::vector<float> rope_freqs_;

    // Norm weights dequantized ONCE per model binding (bounded: layers *
    // emb * 2 * 4 bytes — a small fraction of any real model).
    std::vector<std::vector<float>> attn_norm_;
    std::vector<std::vector<float>> ffn_norm_;
    std::vector<float> out_norm_;

    // v1.9.2: per-layer resolved weight matrices + the logits projection
    // (output.weight, or token_embd for tied models) — resolved once per
    // binding (see resolve_model). The projected rows are walked by
    // pointer arithmetic; bounds were validated at resolve time.
    std::vector<LayerWeights> lw_;
    tensor::ResolvedTensor out_proj_;
    bool resolved_ = false;
};

} // namespace fwd
} // namespace shtn

#endif /* SHTN_FORWARD_H */
