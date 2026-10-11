// sampler.cpp — native engine sampling primitives implementation (Phase 4).
//
// Implements greedy, temperature, top-k, top-p, and repetition penalty.
// The sampling is deterministic given (logits, recent_tokens, config, rng)
// — the same inputs always produce the same token. This is verified by
// the test suite.

#include "sampler.h"

#include "shtn/engine.h"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <limits>

namespace shtn {
namespace sampler {

namespace {

// Apply repetition penalty to logits in-place (CTRL formulation).
void apply_repetition_penalty(float* logits, uint32_t vocab_size,
                              const uint32_t* recent, size_t recent_count,
                              float penalty) {
    if (penalty == 1.0f || recent == nullptr || recent_count == 0) {
        return;
    }

    // For each unique token in recent, scale its logit.
    // (Duplicates in recent only apply the penalty once — the standard
    // interpretation. We use a small sorted-unique pass.)
    std::vector<uint32_t> unique_recent(recent, recent + recent_count);
    std::sort(unique_recent.begin(), unique_recent.end());
    unique_recent.erase(std::unique(unique_recent.begin(), unique_recent.end()),
                        unique_recent.end());

    for (uint32_t tok : unique_recent) {
        if (tok >= vocab_size) continue;
        float& l = logits[tok];
        if (l > 0.0f) {
            l /= penalty;
        } else {
            l *= penalty;
        }
    }
}

// Apply temperature scaling: divide every logit by T.
void apply_temperature(float* logits, uint32_t vocab_size, float t) {
    if (t == 1.0f || t <= 0.0f) {
        return;
    }
    float inv = 1.0f / t;
    for (uint32_t i = 0; i < vocab_size; ++i) {
        logits[i] *= inv;
    }
}

// Index entry for top-k / top-p sorting.
struct Idx {
    uint32_t id;
    float val;
};

// Apply top-k: zero out (set to -inf) every logit except the top K.
void apply_top_k(float* logits, uint32_t vocab_size, int32_t k) {
    if (k <= 0 || static_cast<uint32_t>(k) >= vocab_size) {
        return;
    }

    // Partial sort: copy, sort descending, find the threshold.
    std::vector<Idx> idx(vocab_size);
    for (uint32_t i = 0; i < vocab_size; ++i) {
        idx[i] = {i, logits[i]};
    }

    // nth_element gives us the top-k in O(n) average.
    std::nth_element(idx.begin(), idx.begin() + (k - 1), idx.end(),
                     [](const Idx& a, const Idx& b) { return a.val > b.val; });

    // The threshold is the k-th largest value.
    float threshold = idx[k - 1].val;

    // Zero out everything below threshold.
    for (uint32_t i = 0; i < vocab_size; ++i) {
        if (logits[i] < threshold) {
            logits[i] = -std::numeric_limits<float>::infinity();
        }
    }
}

// Apply top-p (nucleus): keep the smallest set whose cumulative
// probability >= top_p, zero out the rest. Requires softmax first.
void apply_top_p(float* logits, uint32_t vocab_size, float top_p,
                 float* probs_workspace) {
    if (top_p >= 1.0f || top_p <= 0.0f) {
        return;
    }

    // Compute softmax into probs_workspace.
    softmax(logits, vocab_size, probs_workspace);

    // Sort indices by probability descending.
    std::vector<Idx> idx(vocab_size);
    for (uint32_t i = 0; i < vocab_size; ++i) {
        idx[i] = {i, probs_workspace[i]};
    }
    std::sort(idx.begin(), idx.end(),
              [](const Idx& a, const Idx& b) { return a.val > b.val; });

    // Walk the sorted list, accumulating prob, until we hit top_p.
    double cum = 0.0;
    std::vector<bool> keep(vocab_size, false);
    for (uint32_t i = 0; i < vocab_size; ++i) {
        cum += idx[i].val;
        keep[idx[i].id] = true;
        if (cum >= top_p) {
            break;
        }
    }

    // Zero out non-kept probabilities, then write back to logits as
    // -inf for filtered tokens (so the subsequent softmax produces 0
    // prob for them).
    for (uint32_t i = 0; i < vocab_size; ++i) {
        if (!keep[i]) {
            logits[i] = -std::numeric_limits<float>::infinity();
        }
    }
}

// Sample one index from a probability distribution (cumulative method).
uint32_t sample_from_probs(const float* probs, uint32_t vocab_size, RNG& rng) {
    // Compute the sum (in case the probs are not normalized — top-p may
    // leave them slightly off after filtering).
    double sum = 0.0;
    for (uint32_t i = 0; i < vocab_size; ++i) {
        if (probs[i] > 0.0f) {
            sum += probs[i];
        }
    }

    if (sum <= 0.0) {
        // Degenerate: return 0 (the host should treat this as an error).
        return 0;
    }

    double r = rng.next_double() * sum;
    double cum = 0.0;
    for (uint32_t i = 0; i < vocab_size; ++i) {
        if (probs[i] > 0.0f) {
            cum += probs[i];
            if (r < cum) {
                return i;
            }
        }
    }

    // Floating-point fallback: return the last non-zero index.
    for (int32_t i = static_cast<int32_t>(vocab_size) - 1; i >= 0; --i) {
        if (probs[i] > 0.0f) {
            return static_cast<uint32_t>(i);
        }
    }

    return 0;
}

} // namespace

void softmax(const float* logits, uint32_t vocab_size, float* out) {
    if (vocab_size == 0) {
        return;
    }

    // Find max for numerical stability.
    float max_val = logits[0];
    for (uint32_t i = 1; i < vocab_size; ++i) {
        if (logits[i] > max_val) {
            max_val = logits[i];
        }
    }

    double sum = 0.0;
    for (uint32_t i = 0; i < vocab_size; ++i) {
        double e = std::exp(static_cast<double>(logits[i] - max_val));
        out[i] = static_cast<float>(e);
        sum += e;
    }

    if (sum <= 0.0) {
        // All -inf or NaN — uniform fallback.
        float u = 1.0f / static_cast<float>(vocab_size);
        for (uint32_t i = 0; i < vocab_size; ++i) {
            out[i] = u;
        }
        return;
    }

    float inv = static_cast<float>(1.0 / sum);
    for (uint32_t i = 0; i < vocab_size; ++i) {
        out[i] *= inv;
    }
}

int32_t sample(const float* logits, uint32_t vocab_size,
               const uint32_t* recent_tokens, size_t recent_count,
               const Config& config, RNG& rng, uint32_t& out_token,
               std::string& error) {
    out_token = 0;

    if (logits == nullptr || vocab_size == 0) {
        error = "sampler: logits is null or empty";
        return SHTN_ERR_INVALID_ARG;
    }

    // Copy logits into a mutable buffer (we modify in place).
    std::vector<float> buf(logits, logits + vocab_size);

    // 1. Repetition penalty (applied to raw logits, before temperature).
    apply_repetition_penalty(buf.data(), vocab_size, recent_tokens,
                             recent_count, config.repetition_penalty);

    // 2. Temperature == 0 or any non-positive → greedy (argmax).
    if (config.temperature <= 0.0f) {
        uint32_t best = 0;
        float best_val = buf[0];
        for (uint32_t i = 1; i < vocab_size; ++i) {
            if (buf[i] > best_val) {
                best_val = buf[i];
                best = i;
            }
        }
        out_token = best;
        return SHTN_OK;
    }

    // 3. Temperature scaling.
    apply_temperature(buf.data(), vocab_size, config.temperature);

    // 4. Top-k filtering.
    if (config.top_k > 0) {
        apply_top_k(buf.data(), vocab_size, config.top_k);
    }

    // 5. Top-p filtering (requires softmax internally).
    std::vector<float> probs(vocab_size);
    if (config.top_p < 1.0f && config.top_p > 0.0f) {
        apply_top_p(buf.data(), vocab_size, config.top_p, probs.data());
    }

    // 6. Final softmax → probabilities.
    softmax(buf.data(), vocab_size, probs.data());

    // 7. Sample.
    out_token = sample_from_probs(probs.data(), vocab_size, rng);

    return SHTN_OK;
}

} // namespace sampler
} // namespace shtn
