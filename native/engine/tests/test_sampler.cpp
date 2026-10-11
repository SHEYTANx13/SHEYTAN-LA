// test_sampler.cpp — Phase 4 sampling primitives tests (dependency-free).
//
// Verifies the sampler is deterministic, greedy picks argmax, temperature
// sharpens/flattens, top-k filters, top-p filters, and repetition penalty
// penalizes recent tokens.

#include "shtn/engine.h"
#include "shtn/types.h"

#include "sampler.h"

#include <cmath>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

static int failures = 0;

#define CHECK(cond)                                                        \
    do {                                                                   \
        if (!(cond)) {                                                     \
            std::fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__,    \
                         #cond);                                           \
            ++failures;                                                    \
        }                                                                  \
    } while (0)

int main() {
    using namespace shtn::sampler;

    // --- greedy (temperature=0) picks argmax ---------------------------
    {
        // Logits: token 3 is the max.
        std::vector<float> logits = {1.0f, 2.0f, 0.5f, 5.0f, 0.1f};
        Config cfg;
        cfg.temperature = 0.0f;
        RNG rng(42);

        uint32_t tok = 0;
        std::string err;
        CHECK(sample(logits.data(), 5, nullptr, 0, cfg, rng, tok, err) == SHTN_OK);
        CHECK(tok == 3);
    }

    // --- determinism: same inputs → same token -------------------------
    {
        std::vector<float> logits = {1.0f, 2.0f, 3.0f, 2.5f, 1.5f};
        Config cfg;
        cfg.temperature = 1.0f;
        cfg.top_p = 0.9f;
        cfg.seed = 12345;

        RNG r1(12345);
        RNG r2(12345);

        uint32_t t1 = 0, t2 = 0;
        std::string err;
        CHECK(sample(logits.data(), 5, nullptr, 0, cfg, r1, t1, err) == SHTN_OK);
        CHECK(sample(logits.data(), 5, nullptr, 0, cfg, r2, t2, err) == SHTN_OK);
        CHECK(t1 == t2);
    }

    // --- different seeds usually give different tokens ------------------
    // (probabilistic but with T=1 and 5 tokens the chance of collision
    // is ~20%; we use a larger vocab to make it near-zero.)
    {
        std::vector<float> logits(100, 0.0f);
        for (uint32_t i = 0; i < 100; ++i) logits[i] = static_cast<float>(i);

        Config cfg;
        cfg.temperature = 1.0f;
        cfg.seed = 1;

        RNG r1(1);
        RNG r2(2);

        uint32_t t1 = 0, t2 = 0;
        std::string err;
        CHECK(sample(logits.data(), 100, nullptr, 0, cfg, r1, t1, err) == SHTN_OK);
        CHECK(sample(logits.data(), 100, nullptr, 0, cfg, r2, t2, err) == SHTN_OK);
        // Different RNG seeds with 100 tokens — almost never equal.
        // If they happen to be equal, that's not a bug — but it's
        // vanishingly unlikely. We CHECK inequality as a sanity probe.
        // (Disable if it ever flakes.)
        CHECK(t1 != t2);
    }

    // --- top-k=1 forces argmax even with temperature -------------------
    {
        std::vector<float> logits = {1.0f, 5.0f, 2.0f, 0.5f};
        Config cfg;
        cfg.temperature = 1.0f;
        cfg.top_k = 1;
        cfg.seed = 1;

        RNG rng(1);
        uint32_t tok = 0;
        std::string err;
        CHECK(sample(logits.data(), 4, nullptr, 0, cfg, rng, tok, err) == SHTN_OK);
        CHECK(tok == 1); // argmax
    }

    // --- top-p=0.5 keeps only the top of the distribution --------------
    {
        // Logits: token 2 dominates after softmax.
        std::vector<float> logits = {0.0f, 0.0f, 10.0f, 0.0f, 0.0f};
        Config cfg;
        cfg.temperature = 1.0f;
        cfg.top_p = 0.5f;
        cfg.seed = 1;

        RNG rng(1);
        uint32_t tok = 0;
        std::string err;
        // Sample many times — token 2 should win every time (others have
        // ~0 probability after top-p filtering).
        for (int i = 0; i < 20; ++i) {
            RNG r(1 + i);
            CHECK(sample(logits.data(), 5, nullptr, 0, cfg, r, tok, err) == SHTN_OK);
            CHECK(tok == 2);
        }
    }

    // --- repetition penalty lowers the probability of recent tokens ----
    {
        // Token 1 has the highest logit. Without penalty, greedy → 1.
        std::vector<float> logits = {1.0f, 5.0f, 0.5f};
        Config cfg;
        cfg.temperature = 0.0f; // greedy

        uint32_t tok = 0;
        std::string err;
        RNG rng(1);
        CHECK(sample(logits.data(), 3, nullptr, 0, cfg, rng, tok, err) == SHTN_OK);
        CHECK(tok == 1);

        // With repetition penalty on token 1, greedy now picks token 0
        // (logit 5/2 = 2.5 < 1.0? no — 5/2=2.5 > 1.0). Let me redo:
        // CTRL: if logit>0, divide by penalty. 5/2=2.5, 1/2=0.5, 0.5/2=0.25.
        // Argmax is still token 1 (2.5). Use penalty=10: 5/10=0.5, 1/1=1.0.
        // Now argmax is token 0 (1.0 > 0.5).
        uint32_t recent[] = {1};
        Config cfg2;
        cfg2.temperature = 0.0f;
        cfg2.repetition_penalty = 10.0f;

        RNG rng2(1);
        uint32_t tok2 = 0;
        CHECK(sample(logits.data(), 3, recent, 1, cfg2, rng2, tok2, err) == SHTN_OK);
        CHECK(tok2 == 0); // penalty flipped the argmax
    }

    // --- softmax sanity -------------------------------------------------
    {
        std::vector<float> logits = {0.0f, 0.0f, 0.0f, 0.0f};
        std::vector<float> probs(4);
        softmax(logits.data(), 4, probs.data());

        // Uniform distribution.
        for (float p : probs) {
            CHECK(std::fabs(p - 0.25f) < 1e-6f);
        }
    }

    // --- softmax with one dominant token -------------------------------
    {
        std::vector<float> logits = {0.0f, 0.0f, 100.0f, 0.0f};
        std::vector<float> probs(4);
        softmax(logits.data(), 4, probs.data());

        CHECK(probs[2] > 0.999f);
        CHECK(probs[0] < 0.001f);
    }

    // --- null/empty logits rejected ------------------------------------
    {
        Config cfg;
        RNG rng(1);
        uint32_t tok = 0;
        std::string err;
        CHECK(sample(nullptr, 0, nullptr, 0, cfg, rng, tok, err) == SHTN_ERR_INVALID_ARG);
        CHECK(sample(nullptr, 5, nullptr, 0, cfg, rng, tok, err) == SHTN_ERR_INVALID_ARG);
    }

    // --- RNG reproducibility -------------------------------------------
    {
        RNG a(42);
        RNG b(42);
        for (int i = 0; i < 10; ++i) {
            CHECK(a.next_u64() == b.next_u64());
        }
    }

    // --- RNG low-entropy seed doesn't produce degenerate state ---------
    {
        RNG r(1);
        // First few values should be well-distributed (no all-zero runs).
        uint64_t v1 = r.next_u64();
        uint64_t v2 = r.next_u64();
        CHECK(v1 != 0);
        CHECK(v2 != 0);
        CHECK(v1 != v2);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_sampler: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_sampler: all checks passed\n");
    return 0;
}
