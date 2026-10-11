// llama.cpp — llama-family graph derivation + validation (Phase 5).
//
// Metadata is never trusted: every value is bounds-sanity-checked, every
// relationship (heads divide embedding, GQA ratio integral, ffn>0, ...)
// is verified, and the first violation names itself in the error string.

#include "llama.h"

#include "shtn/engine.h"

#include <sstream>

namespace shtn {
namespace llama {

namespace {

bool lookup_u32(const gguf::Metadata& md, const std::string& key,
                uint32_t& out, std::string& error, const char* what) {
    const gguf::Scalar* s = gguf::find_scalar(md, key);
    if (s == nullptr) {
        error = std::string("missing required metadata: ") + key;
        return false;
    }
    uint64_t v = 0;
    if (!gguf::scalar_u64(*s, v) || v == 0 || v > UINT32_MAX) {
        error = std::string("invalid ") + what + " (" + key + ")";
        return false;
    }
    out = static_cast<uint32_t>(v);
    return true;
}

bool lookup_f64(const gguf::Metadata& md, const std::string& key,
                double& out, std::string& error, const char* what) {
    const gguf::Scalar* s = gguf::find_scalar(md, key);
    if (s == nullptr) {
        error = std::string("missing required metadata: ") + key;
        return false;
    }
    if (s->type == gguf::kTypeFloat32 || s->type == gguf::kTypeFloat64) {
        out = s->f64_v;
    } else {
        error = std::string("invalid ") + what + " (" + key +
                ": not a float)";
        return false;
    }
    if (!(out > 0.0) || out > 1e9) {
        error = std::string("invalid ") + what + " (" + key + ": " +
                std::to_string(out) + ")";
        return false;
    }
    return true;
}

} // namespace

std::string tensor_token_embd() { return "token_embd.weight"; }
std::string tensor_output() { return "output.weight"; }
std::string tensor_output_norm() { return "output_norm.weight"; }

std::string tensor_attn_norm(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".attn_norm.weight";
    return oss.str();
}

std::string tensor_attn_q(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".attn_q.weight";
    return oss.str();
}

std::string tensor_attn_k(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".attn_k.weight";
    return oss.str();
}

std::string tensor_attn_v(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".attn_v.weight";
    return oss.str();
}

std::string tensor_attn_out(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".attn_output.weight";
    return oss.str();
}

std::string tensor_ffn_norm(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".ffn_norm.weight";
    return oss.str();
}

std::string tensor_ffn_gate(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".ffn_gate.weight";
    return oss.str();
}

std::string tensor_ffn_up(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".ffn_up.weight";
    return oss.str();
}

std::string tensor_ffn_down(uint32_t layer) {
    std::ostringstream oss;
    oss << "blk." << layer << ".ffn_down.weight";
    return oss.str();
}

bool is_llama_arch(const gguf::Metadata& md) {
    const gguf::Scalar* s = gguf::find_scalar(md, "general.architecture");
    if (s == nullptr) return false;
    std::string arch;
    if (!gguf::scalar_str(*s, arch)) return false;
    return arch == "llama";
}

bool derive(const gguf::Metadata& md, Hyper& h, std::string& error) {
    h = Hyper{};

    if (!is_llama_arch(md)) {
        const gguf::Scalar* s = gguf::find_scalar(md, "general.architecture");
        std::string arch = "<absent>";
        if (s != nullptr) {
            gguf::scalar_str(*s, arch);
        }
        error = "architecture '" + arch +
                "' is not supported for native inference (supported: llama)";
        return false;
    }

    const char* p = "llama.";

    if (!lookup_u32(md, std::string(p) + "embedding_length", h.emb, error,
                    "embedding_length") ||
        !lookup_u32(md, std::string(p) + "block_count", h.layers, error,
                    "block_count") ||
        !lookup_u32(md, std::string(p) + "attention.head_count", h.heads,
                    error, "attention.head_count") ||
        !lookup_u32(md, std::string(p) + "attention.head_count_kv",
                    h.kv_heads, error, "attention.head_count_kv") ||
        !lookup_u32(md, std::string(p) + "feed_forward_length", h.ffn, error,
                    "feed_forward_length")) {
        return false;
    }

    // Context length (u64 in files).
    {
        const gguf::Scalar* s = gguf::find_scalar(md,
                                                  std::string(p) + "context_length");
        if (s == nullptr) {
            error = "missing required metadata: " + std::string(p) +
                    "context_length";
            return false;
        }
        uint64_t v = 0;
        if (!gguf::scalar_u64(*s, v) || v == 0 || v > (1ull << 32)) {
            error = "invalid context_length";
            return false;
        }
        h.context = v;
    }

    // head_dim: attention.key_length when present, else the documented
    // derivation embedding / head_count (llama.cpp relationship).
    {
        const gguf::Scalar* s =
            gguf::find_scalar(md, std::string(p) + "attention.key_length");
        if (s != nullptr) {
            uint64_t v = 0;
            if (!gguf::scalar_u64(*s, v) || v == 0 || v > UINT32_MAX) {
                error = "invalid attention.key_length";
                return false;
            }
            h.head_dim = static_cast<uint32_t>(v);
        } else {
            if (h.emb % h.heads != 0) {
                error = "embedding_length " + std::to_string(h.emb) +
                        " is not divisible by head_count " +
                        std::to_string(h.heads) +
                        " (and attention.key_length is absent)";
                return false;
            }
            h.head_dim = h.emb / h.heads;
        }
    }

    // Normalization epsilon (REQUIRED — no default invented). Real-world
    // GGUFs carry ONE of two historical spellings: the current llama.cpp
    // canonical "layer_norm_rms_epsilon" or the older "layer_norm_rms_eps".
    // Both are accepted; if a file carries BOTH with different values
    // that is an AMBIGUITY and fails closed (never a silent pick).
    {
        const std::string key_canon = std::string(p) + "attention.layer_norm_rms_epsilon";
        const std::string key_legacy = std::string(p) + "attention.layer_norm_rms_eps";
        const gguf::Scalar* canon = gguf::find_scalar(md, key_canon);
        const gguf::Scalar* legacy = gguf::find_scalar(md, key_legacy);

        if (canon == nullptr && legacy == nullptr) {
            error = "missing required metadata: " + key_canon +
                    " (or the legacy " + key_legacy + ")";
            return false;
        }

        const gguf::Scalar& s = canon != nullptr ? *canon : *legacy;
        if (s.type != gguf::kTypeFloat32 && s.type != gguf::kTypeFloat64) {
            error = "invalid rms_eps (not a float)";
            return false;
        }
        h.rms_eps = s.f64_v;

        if (canon != nullptr && legacy != nullptr &&
            canon->f64_v != legacy->f64_v) {
            error = "ambiguous rms_eps: " + key_canon + " and " + key_legacy +
                    " disagree (" + std::to_string(canon->f64_v) + " vs " +
                    std::to_string(legacy->f64_v) + ")";
            return false;
        }
    }
    if (h.rms_eps >= 1.0) {
        error = "invalid rms_eps " + std::to_string(h.rms_eps) +
                " (must be < 1)";
        return false;
    }

    // RoPE frequency base (REQUIRED — no default invented).
    if (!lookup_f64(md, std::string(p) + "rope.freq_base", h.rope_freq_base,
                    error, "rope.freq_base")) {
        return false;
    }

    // RoPE frequency scale: default 1.0 when absent; anything other than
    // 1.0 is unsupported in this phase (fail closed, explicit).
    {
        const gguf::Scalar* s =
            gguf::find_scalar(md, std::string(p) + "rope.freq_scale");
        if (s != nullptr) {
            double v = 1.0;
            if (s->type == gguf::kTypeFloat32 ||
                s->type == gguf::kTypeFloat64) {
                v = s->f64_v;
            } else {
                error = "invalid rope.freq_scale (not a float)";
                return false;
            }
            if (v < 0.999 || v > 1.001) {
                error = "rope.freq_scale " + std::to_string(v) +
                        " is not supported for native inference (only 1.0)";
                return false;
            }
            h.rope_freq_scale = 1.0;
        }
    }

    // Derived dims.
    h.attn_out_dim = h.heads * h.head_dim;
    h.kv_dim = h.kv_heads * h.head_dim;

    // Relationship checks.
    if (h.heads < h.kv_heads) {
        error = "head_count " + std::to_string(h.heads) +
                " < head_count_kv " + std::to_string(h.kv_heads) +
                " (invalid GQA)";
        return false;
    }
    if (h.heads % h.kv_heads != 0) {
        error = "head_count " + std::to_string(h.heads) +
                " not divisible by head_count_kv " +
                std::to_string(h.kv_heads) + " (GQA mapping unsupported)";
        return false;
    }
    if (h.emb == 0 || h.layers == 0 || h.ffn == 0 || h.head_dim == 0) {
        error = "zero dimension in llama hyper parameters";
        return false;
    }

    return true;
}

bool validate(const gguf::GgufHeader& header, Hyper& h,
              std::string& reason) {
    const tensor::Weights w(header);

    // token_embd.weight: [emb, vocab].
    {
        uint32_t ne0 = 0, ne1 = 0, type = 0;
        std::string err;
        if (w.shape2d(tensor_token_embd(), ne0, ne1, type, err) != SHTN_OK) {
            reason = "native inference tensor error: " + err;
            return false;
        }
        if (ne0 != h.emb) {
            reason = "token_embd.weight ne0=" + std::to_string(ne0) +
                     " != embedding_length " + std::to_string(h.emb);
            return false;
        }
        if (!tensor::type_supported(type)) {
            reason = "tensor token_embd.weight has unsupported type " +
                     std::string(tensor::type_name(type)) +
                     " (supported: F32, F16, Q4_0, Q4_1, Q5_0, Q5_1, Q8_0)";
            return false;
        }
        h.vocab_size = ne1;
    }

    // output.weight: [emb, vocab] — optional (tied embeddings).
    {
        const gguf::TensorInfo* t = w.find(tensor_output());
        if (t == nullptr) {
            // Tied: token_embd doubles as the LM head.
            h.output_tied = true;
        } else {
            uint32_t ne0 = 0, ne1 = 0, type = 0;
            std::string err;
            if (w.shape2d(tensor_output(), ne0, ne1, type, err) != SHTN_OK) {
                reason = "native inference tensor error: " + err;
                return false;
            }
            if (ne0 != h.emb || ne1 != h.vocab_size) {
                reason = "output.weight shape [" + std::to_string(ne0) + "," +
                         std::to_string(ne1) + "] incompatible with [emb=" +
                         std::to_string(h.emb) + ", vocab=" +
                         std::to_string(h.vocab_size) + "]";
                return false;
            }
            if (!tensor::type_supported(type)) {
                reason = "tensor output.weight has unsupported type " +
                         std::string(tensor::type_name(type));
                return false;
            }
        }
    }

    // output_norm.weight: [emb].
    {
        const gguf::TensorInfo* t = w.find(tensor_output_norm());
        if (t == nullptr || t->n_dims != 1 || t->dims[0] != h.emb ||
            !tensor::type_supported(t->type)) {
            reason = "output_norm.weight missing, wrong shape or unsupported type";
            return false;
        }
    }

    // Per-layer tensors.
    for (uint32_t l = 0; l < h.layers; ++l) {
        struct Spec {
            std::string name;
            uint32_t ne0;
            uint32_t ne1;
        };

        const Spec specs[] = {
            {tensor_attn_norm(l), h.emb, 0},        // 1-D
            {tensor_ffn_norm(l), h.emb, 0},         // 1-D
            {tensor_attn_q(l), h.emb, h.attn_out_dim},
            {tensor_attn_k(l), h.emb, h.kv_dim},
            {tensor_attn_v(l), h.emb, h.kv_dim},
            {tensor_attn_out(l), h.attn_out_dim, h.emb},
            {tensor_ffn_gate(l), h.emb, h.ffn},
            {tensor_ffn_up(l), h.emb, h.ffn},
            {tensor_ffn_down(l), h.ffn, h.emb},
        };

        for (const Spec& s : specs) {
            const gguf::TensorInfo* t = w.find(s.name);
            if (t == nullptr) {
                reason = "missing required tensor " + s.name;
                return false;
            }
            if (s.ne1 == 0) {
                // 1-D norm weight.
                if (t->n_dims != 1 || t->dims[0] != s.ne0) {
                    reason = "tensor " + s.name + " must be 1-D of length " +
                             std::to_string(s.ne0);
                    return false;
                }
            } else {
                if (t->n_dims != 2 || t->dims[0] != s.ne0 ||
                    t->dims[1] != s.ne1) {
                    reason = "tensor " + s.name + " must be [" +
                             std::to_string(s.ne0) + "," +
                             std::to_string(s.ne1) + "] (got n_dims=" +
                             std::to_string(t->n_dims) + ")";
                    return false;
                }
            }
            if (!tensor::type_supported(t->type)) {
                reason = "tensor " + s.name + " has unsupported type " +
                         std::string(tensor::type_name(t->type)) +
                         " (supported: F32, F16, Q4_0, Q4_1, Q5_0, Q5_1, Q8_0)";
                return false;
            }
        }
    }

    return true;
}

} // namespace llama
} // namespace shtn
