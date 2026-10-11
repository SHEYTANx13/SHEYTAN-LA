// sampler.h — the SHEYTAN native engine sampling primitives (Phase 4).
//
// Real, deterministic, configurable sampling that the future native
// inference loop will call after computing logits. The sampler is pure:
// it takes logits + config + RNG state and returns a token id. It does
// NOT depend on the model, the KV cache, or the scheduler — it operates
// on the logits row the (future) forward pass produces.
//
// What this is:
//   - greedy (argmax);
//   - temperature scaling (T > 0);
//   - top-k filtering;
//   - top-p (nucleus) filtering;
//   - repetition penalty applied to the recent token history;
//   - seedable deterministic RNG (xorshift64* — bounded, no std::mt19937
//     to keep the binary lean and the sequence reproducible);
//   - EOS honored when sampled.
//
// What this is NOT:
//   - this is NOT wired into the inference loop (no logits come from
//     anywhere yet — tests supply synthetic logits);
//   - this does NOT fake a token (an empty logits row returns
//     SHTN_ERR_INVALID_ARG; the host reports the error honestly).

#ifndef SHTN_SAMPLER_H
#define SHTN_SAMPLER_H

#include <cstdint>
#include <string>
#include <vector>

namespace shtn {
namespace sampler {

// Config mirrors the llm request's sampling controls. Only fields the
// engine actually consumes are present (no "mirostat" / "min_p" stubs).
struct Config {
    // Temperature: 1.0 = unchanged; <1.0 = sharper; >1.0 = flatter;
    // 0.0 = greedy (argmax) regardless of other settings.
    float temperature = 1.0f;

    // TopK: keep only the K highest-probability tokens. 0 = disabled.
    int32_t top_k = 0;

    // TopP: keep the smallest set whose cumulative prob >= top_p.
    // 1.0 = disabled (keep all). Range (0, 1].
    float top_p = 1.0f;

    // RepetitionPenalty: multiplier applied to the logits of tokens in
    // recent_tokens. 1.0 = disabled; >1.0 = penalize repeats; <1.0 =
    // encourage repeats. Standard formulation (CTRL paper): if logit > 0,
    // divide by penalty; else multiply by penalty.
    float repetition_penalty = 1.0f;

    // Seed: deterministic RNG seed. 0 = use a fixed default seed (NOT
    // time-based — we keep sampling deterministic for reproducibility).
    uint64_t seed = 0;

    // MaxTokens: hard cap on generation length (informational here; the
    // future generation loop enforces it).
    uint32_t max_tokens = 256;

    // EOS token id: when sampled, generation stops (informational here).
    uint32_t eos_token_id = 0;
    bool has_eos = false;

    // Stop strings: when the decoded text contains one, generation stops
    // (informational here; enforced by the future generation loop).
    std::vector<std::string> stop_strings;
};

// RNG: a seedable, deterministic, reproducible RNG (xorshift64*). The
// state is explicit so test fixtures can reproduce exact sequences.
struct RNG {
    uint64_t state = 0x9E3779B97F4A7C15ull; // golden ratio constant default

    explicit RNG(uint64_t seed = 0) {
        if (seed == 0) {
            state = 0x9E3779B97F4A7C15ull;
        } else {
            // SplitMix64 step to avoid low-entropy seeds.
            state = seed;
            state += 0x9E3779B97F4A7C15ull;
            state = (state ^ (state >> 30)) * 0xBF58476D1CE4E5B9ull;
            state = (state ^ (state >> 27)) * 0x94D049BB133111EBull;
            state = state ^ (state >> 31);
            if (state == 0) {
                state = 0x9E3779B97F4A7C15ull;
            }
        }
    }

    // next returns a uniform uint64.
    uint64_t next_u64() {
        uint64_t x = state;
        x ^= x >> 12;
        x ^= x << 25;
        x ^= x >> 27;
        state = x;
        return x * 0x2545F4914F6CDD1Dull;
    }

    // next_double returns a uniform double in [0, 1).
    double next_double() {
        // Use the top 53 bits for full double precision.
        return static_cast<double>(next_u64() >> 11) *
               (1.0 / static_cast<double>(1ull << 53));
    }
};

// Sample picks one token id from a logits row.
//
// Inputs:
//   logits  — pointer to vocab_size floats (the raw model output);
//   vocab_size — the size of the logits array;
//   recent_tokens — token ids in the recent context (for repetition
//                   penalty); may be empty;
//   config  — sampling configuration;
//   rng     — RNG state (advanced in place; pass the same RNG across
//             decode steps for a single sequence).
//
// Output:
//   out_token — the sampled token id;
//   error     — human-readable reason on failure.
//
// Returns SHTN_OK or a negative error code. Never crashes on a hostile
// logits array (nullptr / zero size → SHTN_ERR_INVALID_ARG).
int32_t sample(const float* logits, uint32_t vocab_size,
               const uint32_t* recent_tokens, size_t recent_count,
               const Config& config, RNG& rng, uint32_t& out_token,
               std::string& error);

// Softmax computes softmax(logits) in-place into `out` (out has
// vocab_size floats). Used by tests; the sampler uses it internally.
void softmax(const float* logits, uint32_t vocab_size, float* out);

} // namespace sampler
} // namespace shtn

#endif /* SHTN_SAMPLER_H */
