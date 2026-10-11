// forward.cpp — the llama transformer forward pass implementation
// (Phase 5 — REAL).
//
// Correctness notes:
//   - accumulation in double for dot products and RMS means (reference
//     parity with the Python reference within tight tolerances);
//   - K/V stored as REAL fp16 bits (uint16_t) — the conversion goes
//     through fp16.h exactly as the Python reference models it;
//   - attention reads only positions [0, pos] (causal by construction);
//   - GQA mapping: query head h attends through kv head h / (heads /
//     kv_heads) — the llama.cpp relationship, validated at derive time
//     (heads % kv_heads == 0);
//   - the logits row for token v comes from output.weight (row v) or,
//     for tied models, token_embd (row v).

#include "forward.h"

#include "shtn/engine.h"

#include <algorithm>
#include <cmath>
#include <cstring>

namespace shtn {
namespace fwd {

// --- numerics ----------------------------------------------------------------

void rms_norm(const float* x, const float* w, uint32_t n, double eps,
              float* out) {
    double ss = 0.0;
    for (uint32_t i = 0; i < n; ++i) {
        const double xi = static_cast<double>(x[i]);
        ss += xi * xi;
    }
    const double mean = ss / static_cast<double>(n);
    const double scale = 1.0 / std::sqrt(mean + eps);

    for (uint32_t i = 0; i < n; ++i) {
        out[i] = static_cast<float>(static_cast<double>(x[i]) * scale *
                                    static_cast<double>(w[i]));
    }
}

void rope_freqs(uint32_t head_dim, double freq_base,
                std::vector<float>& out) {
    const uint32_t half = head_dim / 2;
    out.assign(half, 0.0f);
    for (uint32_t j = 0; j < half; ++j) {
        // inv_freq = base^(-2j/head_dim)
        const double f =
            std::pow(freq_base, -2.0 * static_cast<double>(j) /
                                    static_cast<double>(head_dim));
        out[j] = static_cast<float>(f);
    }
}

void rope(float* head, uint32_t head_dim, uint64_t pos,
          const float* freqs) {
    const uint32_t half = head_dim / 2;

    for (uint32_t j = 0; j < half; ++j) {
        const float theta = static_cast<float>(
            static_cast<double>(pos) * static_cast<double>(freqs[j]));
        const float cos_t = std::cos(theta);
        const float sin_t = std::sin(theta);

        const float x0 = head[j];
        const float x1 = head[j + half];

        head[j] = x0 * cos_t - x1 * sin_t;
        head[j + half] = x0 * sin_t + x1 * cos_t;
    }
}

void softmax_inplace(float* v, uint32_t n) {
    float max_v = v[0];
    for (uint32_t i = 1; i < n; ++i) {
        if (v[i] > max_v) max_v = v[i];
    }

    double sum = 0.0;
    for (uint32_t i = 0; i < n; ++i) {
        const double e = std::exp(static_cast<double>(v[i] - max_v));
        v[i] = static_cast<float>(e);
        sum += e;
    }

    const double inv = 1.0 / sum;
    for (uint32_t i = 0; i < n; ++i) {
        v[i] = static_cast<float>(static_cast<double>(v[i]) * inv);
    }
}

float dot(const float* a, const float* b, uint32_t n) {
    double acc = 0.0;
    for (uint32_t i = 0; i < n; ++i) {
        acc += static_cast<double>(a[i]) * static_cast<double>(b[i]);
    }
    return static_cast<float>(acc);
}

// --- Forward -----------------------------------------------------------------

int32_t Forward::init(const tensor::Weights* w, const llama::Hyper& h,
                      uint64_t available_ram_bytes, std::string& error) {
    w_ = w;
    h_ = h;
    initialized_ = false;

    kv_.release();

    if (w == nullptr) {
        error = "forward: null weights";
        return SHTN_ERR_INVALID_ARG;
    }

    // --- KV cache from the REAL hyper parameters ------------------------
    kv::Layout layout;
    layout.layer_count = h.layers;
    layout.embedding_length = h.emb;
    layout.head_count = h.heads;
    layout.head_dim = h.head_dim;
    layout.kv_head_count = h.kv_heads;
    layout.kv_dim = h.kv_dim;
    layout.context_length = h.context;

    const int32_t rc = kv_.allocate(layout, available_ram_bytes, error);
    if (rc != SHTN_OK) {
        kv_.release();
        return rc;
    }

    // --- scratch buffers (bounded, reused) --------------------------------
    x_.assign(h.emb, 0.0f);
    xb_.assign(h.emb, 0.0f);
    q_.assign(h.attn_out_dim, 0.0f);
    k_.assign(h.kv_dim, 0.0f);
    v_.assign(h.kv_dim, 0.0f);
    attn_.assign(h.attn_out_dim, 0.0f);
    ffn_g_.assign(h.ffn, 0.0f);
    ffn_u_.assign(h.ffn, 0.0f);
    ffn_d_.assign(h.emb, 0.0f);
    logits_.assign(h.vocab_size, 0.0f);
    scores_.assign(static_cast<size_t>(h.context) < 4096
                       ? static_cast<size_t>(h.context)
                       : 4096,
                   0.0f);

    rope_freqs(h.head_dim, h.rope_freq_base, rope_freqs_);

    // --- norm weights: dequantize ONCE (bounded, reused) ------------------
    attn_norm_.assign(h.layers, {});
    ffn_norm_.assign(h.layers, {});
    for (uint32_t l = 0; l < h.layers; ++l) {
        if (w->vec_f32(llama::tensor_attn_norm(l), h.emb, attn_norm_[l],
                       error) != SHTN_OK ||
            w->vec_f32(llama::tensor_ffn_norm(l), h.emb, ffn_norm_[l],
                       error) != SHTN_OK) {
            error = "forward: " + error + " (layer " + std::to_string(l) + ")";
            kv_.release();
            return SHTN_ERR_MODEL_FORMAT;
        }
    }
    if (w->vec_f32(llama::tensor_output_norm(), h.emb, out_norm_, error) !=
        SHTN_OK) {
        error = "forward: " + error;
        kv_.release();
        return SHTN_ERR_MODEL_FORMAT;
    }

    // --- resolved weight table (v1.9.2): once per binding -----------------
    if (const int32_t rc = resolve_model(error); rc != SHTN_OK) {
        kv_.release();
        return rc;
    }

    initialized_ = true;
    return SHTN_OK;
}

void Forward::reset() {
    kv_.reset();
}

// --- resolved-model setup (v1.9.2) --------------------------------------------

int32_t Forward::resolve_model(std::string& error) {
    lw_.clear();
    resolved_ = false;
    if (w_ == nullptr) {
        error = "forward: null weights";
        return SHTN_ERR_MODEL_STATE;
    }

    lw_.resize(h_.layers);
    for (uint32_t l = 0; l < h_.layers; ++l) {
        LayerWeights& lw = lw_[l];
        if (w_->resolve(llama::tensor_attn_q(l), h_.emb, h_.attn_out_dim,
                        lw.q, error) != SHTN_OK ||
            w_->resolve(llama::tensor_attn_k(l), h_.emb, h_.kv_dim,
                        lw.k, error) != SHTN_OK ||
            w_->resolve(llama::tensor_attn_v(l), h_.emb, h_.kv_dim,
                        lw.v, error) != SHTN_OK ||
            w_->resolve(llama::tensor_attn_out(l), h_.attn_out_dim, h_.emb,
                        lw.out, error) != SHTN_OK ||
            w_->resolve(llama::tensor_ffn_gate(l), h_.emb, h_.ffn,
                        lw.gate, error) != SHTN_OK ||
            w_->resolve(llama::tensor_ffn_up(l), h_.emb, h_.ffn,
                        lw.up, error) != SHTN_OK ||
            w_->resolve(llama::tensor_ffn_down(l), h_.ffn, h_.emb,
                        lw.down, error) != SHTN_OK) {
            error = "forward: " + error + " (layer " + std::to_string(l) + ")";
            return SHTN_ERR_MODEL_FORMAT;
        }
    }

    // Logits projection: output.weight, or token_embd for tied models.
    // rows == vocab_size (validated by llama::validate at load; re-checked
    // here so the resolved record carries the real bound).
    const std::string out_name = h_.output_tied
                                     ? llama::tensor_token_embd()
                                     : llama::tensor_output();
    if (w_->resolve(out_name, h_.emb, h_.vocab_size, out_proj_, error) !=
        SHTN_OK) {
        error = "forward: " + error;
        return SHTN_ERR_MODEL_FORMAT;
    }

    // The shared row buffer must hold any resolved row (padded stride).
    uint32_t max_padded = 0;
    for (const LayerWeights& lw : lw_) {
        max_padded = std::max({max_padded, lw.q.row_elems_padded,
                               lw.k.row_elems_padded, lw.v.row_elems_padded,
                               lw.out.row_elems_padded,
                               lw.gate.row_elems_padded,
                               lw.up.row_elems_padded,
                               lw.down.row_elems_padded});
    }
    max_padded = std::max(max_padded, out_proj_.row_elems_padded);
    if (row_buf_.size() < max_padded) {
        row_buf_.resize(max_padded);
    }

    resolved_ = true;
    return SHTN_OK;
}

int32_t Forward::matvec_resolved(const tensor::ResolvedTensor& m,
                                 const float* x, float* out,
                                 std::string& error) {
    for (uint32_t i = 0; i < m.rows; ++i) {
        if (tensor::dequant_row(m, i, row_buf_.data(), error) != SHTN_OK) {
            return SHTN_ERR_MODEL_FORMAT;
        }
        out[i] = dot(row_buf_.data(), x, m.ne0);
    }
    return SHTN_OK;
}

int32_t Forward::embed(uint32_t token_id, float* out,
                       std::string& error) const {
    if (!initialized_) {
        error = "forward: not initialized";
        return SHTN_ERR_MODEL_STATE;
    }
    if (token_id >= h_.vocab_size) {
        error = "forward: token id " + std::to_string(token_id) +
                " out of vocabulary range " + std::to_string(h_.vocab_size);
        return SHTN_ERR_INVALID_ARG;
    }

    tensor::RowBuf row;
    const int32_t rc = w_->row_f32(llama::tensor_token_embd(), token_id,
                                   h_.emb, 0, row, error);
    if (rc != SHTN_OK) {
        return rc;
    }
    std::memcpy(out, row.data(), h_.emb * sizeof(float));
    return SHTN_OK;
}

int32_t Forward::token(uint32_t token_id, uint64_t pos, float* logits_out,
                       std::string& error) {
    if (!initialized_) {
        error = "forward: not initialized";
        return SHTN_ERR_MODEL_STATE;
    }
    if (token_id >= h_.vocab_size) {
        error = "forward: token id " + std::to_string(token_id) +
                " out of vocabulary range";
        return SHTN_ERR_INVALID_ARG;
    }
    if (pos >= h_.context) {
        error = "forward: position " + std::to_string(pos) +
                " exceeds context " + std::to_string(h_.context);
        return SHTN_ERR_CONTEXT_OVERFLOW;
    }
    if (scores_.size() < pos + 1) {
        scores_.resize(static_cast<size_t>(pos + 1), 0.0f);
    }

    if (const int32_t rc = run_layers(token_id, pos, error); rc != SHTN_OK) {
        return rc;
    }

    // --- final norm + logits -------------------------------------------------
    rms_norm(x_.data(), out_norm_.data(), h_.emb, h_.rms_eps, xb_.data());
    return project_logits(logits_out, error);
}

int32_t Forward::prefill_span(const uint32_t* ids, uint32_t count,
                              uint64_t pos, bool with_last_logits,
                              float* logits_out, std::string& error) {
    if (!initialized_) {
        error = "forward: not initialized";
        return SHTN_ERR_MODEL_STATE;
    }
    if (count == 0) {
        return SHTN_OK; // empty span: a no-op, not an error
    }
    if (ids == nullptr) {
        error = "forward: null prompt token span";
        return SHTN_ERR_INVALID_ARG;
    }
    if (count > h_.context || pos > h_.context - count) {
        error = "forward: positions [" + std::to_string(pos) + ", " +
                std::to_string(pos + count) + ") exceed context " +
                std::to_string(h_.context);
        return SHTN_ERR_CONTEXT_OVERFLOW;
    }
    if (with_last_logits && logits_out == nullptr) {
        error = "forward: prefill_span requested last-token logits with a "
                "null logits buffer";
        return SHTN_ERR_INVALID_ARG;
    }
    for (uint32_t i = 0; i < count; ++i) {
        const uint32_t tok = ids[i];
        if (tok >= h_.vocab_size) {
            error = "forward: token id " + std::to_string(tok) +
                    " (span index " + std::to_string(i) + ")" +
                    " out of vocabulary range";
            return SHTN_ERR_INVALID_ARG;
        }
        const uint64_t p = pos + i;
        if (scores_.size() < p + 1) {
            scores_.resize(static_cast<size_t>(p + 1), 0.0f);
        }
        if (const int32_t rc = run_layers(tok, p, error); rc != SHTN_OK) {
            return rc;
        }
    }

    if (with_last_logits) {
        rms_norm(x_.data(), out_norm_.data(), h_.emb, h_.rms_eps, xb_.data());
        return project_logits(logits_out, error);
    }
    return SHTN_OK;
}

int32_t Forward::run_layers(uint32_t token_id, uint64_t pos,
                            std::string& error) {
    // --- embedding ---------------------------------------------------------
    {
        const int32_t rc = w_->row_f32(llama::tensor_token_embd(), token_id,
                                       h_.emb, 0, row_buf_, error);
        if (rc != SHTN_OK) return rc;
        std::memcpy(x_.data(), row_buf_.data(), h_.emb * sizeof(float));
    }

    const float attn_scale = 1.0f / std::sqrt(static_cast<float>(h_.head_dim));
    const uint32_t heads_per_kv = h_.heads / h_.kv_heads;

    for (uint32_t l = 0; l < h_.layers; ++l) {
        const LayerWeights& lw = lw_[l];

        // --- attention block ------------------------------------------------
        rms_norm(x_.data(), attn_norm_[l].data(), h_.emb, h_.rms_eps,
                 xb_.data());

        if (int32_t rc = matvec_resolved(lw.q, xb_.data(), q_.data(), error);
            rc != SHTN_OK) {
            return rc;
        }
        if (int32_t rc = matvec_resolved(lw.k, xb_.data(), k_.data(), error);
            rc != SHTN_OK) {
            return rc;
        }
        if (int32_t rc = matvec_resolved(lw.v, xb_.data(), v_.data(), error);
            rc != SHTN_OK) {
            return rc;
        }

        // RoPE on every query head and every kv head.
        for (uint32_t hh = 0; hh < h_.heads; ++hh) {
            rope(q_.data() + hh * h_.head_dim, h_.head_dim, pos,
                 rope_freqs_.data());
        }
        for (uint32_t hv = 0; hv < h_.kv_heads; ++hv) {
            rope(k_.data() + hv * h_.head_dim, h_.head_dim, pos,
                 rope_freqs_.data());
        }

        // K/V into the fp16 cache at position pos (every layer writes its
        // own slice; the POSITION advances once per token, below).
        {
            uint16_t* kdst = kv_.k_at(l, pos);
            uint16_t* vdst = kv_.v_at(l, pos);
            if (kdst == nullptr || vdst == nullptr) {
                error = "forward: KV cache position out of range";
                return SHTN_ERR_CONTEXT_OVERFLOW;
            }
            for (uint32_t i = 0; i < h_.kv_dim; ++i) {
                kdst[i] = fp16::fp32_to_fp16_bits(k_[i]);
                vdst[i] = fp16::fp32_to_fp16_bits(v_[i]);
            }
        }

        // Attention per query head over all cached positions.
        for (uint32_t hh = 0; hh < h_.heads; ++hh) {
            const float* qh = q_.data() + hh * h_.head_dim;
            const uint32_t kv_h = hh / heads_per_kv;

            // Read K from the cache (widened to fp32 into k_ buffer is NOT
            // safe — k_ holds the current projections; widen per element
            // inline instead to avoid another buffer).
            // scores[j] = dot(qh, K[l][j] + kv_h*hd) * scale, j <= pos.
            for (uint64_t j = 0; j <= pos; ++j) {
                const uint16_t* kj =
                    kv_.k_at(l, j) + kv_h * h_.head_dim;
                double acc = 0.0;
                for (uint32_t d = 0; d < h_.head_dim; ++d) {
                    acc += static_cast<double>(qh[d]) *
                           static_cast<double>(fp16::fp16_bits_to_fp32(kj[d]));
                }
                scores_[j] = static_cast<float>(acc) * attn_scale;
            }

            // Causal mask: positions > pos are not read at all (loop bound
            // is pos), so no explicit masking is required.
            softmax_inplace(scores_.data(), static_cast<uint32_t>(pos + 1));

            // out_h = Σ_j p_j * V[l][j][kv_h]
            float* outh = attn_.data() + hh * h_.head_dim;
            std::memset(outh, 0, h_.head_dim * sizeof(float));
            for (uint64_t j = 0; j <= pos; ++j) {
                const float p = scores_[j];
                if (p == 0.0f) continue;
                const uint16_t* vj = kv_.v_at(l, j) + kv_h * h_.head_dim;
                for (uint32_t d = 0; d < h_.head_dim; ++d) {
                    outh[d] += p * fp16::fp16_bits_to_fp32(vj[d]);
                }
            }
        }

        // Output projection + residual.
        if (int32_t rc = matvec_resolved(lw.out, attn_.data(), ffn_d_.data(),
                                         error);
            rc != SHTN_OK) {
            return rc;
        }
        for (uint32_t i = 0; i < h_.emb; ++i) {
            x_[i] += ffn_d_[i];
        }

        // --- feed-forward block (SwiGLU) ------------------------------------
        rms_norm(x_.data(), ffn_norm_[l].data(), h_.emb, h_.rms_eps,
                 xb_.data());

        if (int32_t rc = matvec_resolved(lw.gate, xb_.data(), ffn_g_.data(),
                                         error);
            rc != SHTN_OK) {
            return rc;
        }
        if (int32_t rc = matvec_resolved(lw.up, xb_.data(), ffn_u_.data(),
                                         error);
            rc != SHTN_OK) {
            return rc;
        }
        for (uint32_t i = 0; i < h_.ffn; ++i) {
            ffn_g_[i] = silu(ffn_g_[i]) * ffn_u_[i];
        }
        if (int32_t rc = matvec_resolved(lw.down, ffn_g_.data(), ffn_d_.data(),
                                         error);
            rc != SHTN_OK) {
            return rc;
        }
        for (uint32_t i = 0; i < h_.emb; ++i) {
            x_[i] += ffn_d_[i];
        }
    }

    // One POSITION consumed (all layers wrote their K/V slices).
    kv_.advance(1);
    return SHTN_OK;
}

int32_t Forward::project_logits(float* logits_out, std::string& error) {
    for (uint32_t v = 0; v < h_.vocab_size; ++v) {
        if (tensor::dequant_row(out_proj_, v, row_buf_.data(), error) !=
            SHTN_OK) {
            return SHTN_ERR_MODEL_FORMAT;
        }
        logits_[v] = dot(row_buf_.data(), xb_.data(), h_.emb);
    }

    std::memcpy(logits_out, logits_.data(), h_.vocab_size * sizeof(float));
    return SHTN_OK;
}

} // namespace fwd
} // namespace shtn
