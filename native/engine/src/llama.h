// llama.h — the llama-family model graph for native inference (Phase 5).
//
// The FIRST (and in this phase, the only) architecture the native engine
// executes for real:
//
//   token embeddings → per-layer [RMSNorm → GQA self-attention with RoPE
//   → residual → RMSNorm → SwiGLU FFN → residual] → final RMSNorm →
//   logits projection (output.weight, tied to token_embd when absent).
//
// This module owns:
//   - Hyper: the full set of transformer dimensions derived from real
//     GGUF metadata (llama.* keys). Missing REQUIRED keys fail clearly —
//     no defaults are invented (rope_freq_base and rms_eps MUST be in the
//     file; head_dim is derived as embedding/head_count only when
//     attention.key_length is absent — the documented llama.cpp
//     relationship);
//   - validate(): load-time graph validation — every required tensor
//     present, 2-D/1-D shapes consistent with the hyper parameters, and
//     every type within the supported dequantization set. Metadata-level
//     only: nothing is allocated, no data is read;
//   - TensorName: the exact llama.cpp tensor naming convention
//     (blk.{i}.attn_q.weight, ...).
//
// What is NOT supported in this phase (fails validate → the Go backend
// falls back to llama.cpp with the recorded reason):
//   - any other architecture (qwen2, gemma, mistral-specific variants
//     that deviate from the llama tensor set, ...);
//   - attention/FFN bias tensors (the llama family carries none — a file
//     that has them is rejected as "unexpected tensor", not silently
//     ignored);
//   - non-standard RoPE variants (rope section mappings, yarn scaling,
//     long-rope): freq_base/freq_scale only, the NORM pairing layout;
//   - logit soft-capping (Gemma-style) — plain llama has none.
//
// The exact ordering and formulas are documented next to each function
// in forward.cpp and pinned by the Python reference comparison.

#ifndef SHTN_LLAMA_H
#define SHTN_LLAMA_H

#include "gguf.h"
#include "tensor.h"

#include <cstdint>
#include <string>

namespace shtn {
namespace llama {

// Supported llama hyper parameters (all values READ from GGUF metadata
// or derived by the documented relationships — never guessed).
struct Hyper {
    uint32_t vocab_size = 0;      // token_embd ne1 (padded vocab included)
    uint32_t emb = 0;             // llama.embedding_length
    uint32_t layers = 0;          // llama.block_count
    uint32_t heads = 0;           // llama.attention.head_count
    uint32_t kv_heads = 0;        // llama.attention.head_count_kv
    uint32_t head_dim = 0;        // llama.attention.key_length or emb/heads
    uint32_t ffn = 0;             // llama.feed_forward_length
    uint64_t context = 0;         // llama.context_length
    double rms_eps = 0.0;         // llama.attention.layer_norm_rms_eps
    double rope_freq_base = 0.0;  // llama.rope.freq_base
    double rope_freq_scale = 1.0; // llama.rope.freq_scale (must be 1.0)
    uint32_t attn_out_dim = 0;    // heads * head_dim
    uint32_t kv_dim = 0;          // kv_heads * head_dim
    bool output_tied = false;     // output.weight absent → token_embd
};

// derive pulls the llama hyper parameters out of parsed metadata.
// Returns false with `error` naming the FIRST missing/invalid required
// value — the caller fails clearly, never guesses.
bool derive(const gguf::Metadata& md, Hyper& h, std::string& error);

// validate checks the full tensor graph against the hyper parameters:
// presence, shape and supported type of every tensor the forward pass
// will touch. Returns true when the model is natively executable; false
// with `reason` filled for the model_info surface (the Go side surfaces
// it and selects the llama.cpp fallback). On success h.vocab_size and
// h.output_tied are COMPLETED from the tensor table (validated facts,
// not guesses).
bool validate(const gguf::GgufHeader& header, Hyper& h,
              std::string& reason);

// is_llama_arch reports whether the GGUF declares the llama architecture
// (the exact general.architecture string this engine executes).
bool is_llama_arch(const gguf::Metadata& md);

// --- tensor names (the llama.cpp convention) -------------------------------

std::string tensor_token_embd();                       // token_embd.weight
std::string tensor_output();                           // output.weight
std::string tensor_output_norm();                      // output_norm.weight
std::string tensor_attn_norm(uint32_t layer);          // blk.i.attn_norm
std::string tensor_attn_q(uint32_t layer);             // blk.i.attn_q
std::string tensor_attn_k(uint32_t layer);             // blk.i.attn_k
std::string tensor_attn_v(uint32_t layer);             // blk.i.attn_v
std::string tensor_attn_out(uint32_t layer);           // blk.i.attn_output
std::string tensor_ffn_norm(uint32_t layer);           // blk.i.ffn_norm
std::string tensor_ffn_gate(uint32_t layer);           // blk.i.ffn_gate
std::string tensor_ffn_up(uint32_t layer);             // blk.i.ffn_up
std::string tensor_ffn_down(uint32_t layer);           // blk.i.ffn_down

} // namespace llama
} // namespace shtn

#endif /* SHTN_LLAMA_H */
