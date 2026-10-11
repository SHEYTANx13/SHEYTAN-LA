// test_kv_cache.cpp — Phase 5 KV cache tests (dependency-free asserts).
//
// Phase 5 additions: the byte-accounting regression suite pins the Phase 4
// defect fix — the physical representation (uint16_t fp16 bits), the
// reported capacity_bytes and the actual allocation size must be IDENTICAL,
// layer offsets must not overlap, and used_bytes must be mathematically
// consistent with the positions written.

#include "shtn/engine.h"
#include "shtn/types.h"

#include "fp16.h"
#include "kv_cache.h"

#include <cstdio>
#include <cstring>
#include <string>

static int failures = 0;

#define CHECK(cond)                                                        \
    do {                                                                   \
        if (!(cond)) {                                                     \
            std::fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__,    \
                         #cond);                                           \
            ++failures;                                                    \
        }                                                                   \
    } while (0)

int main() {
    using namespace shtn::kv;

    // --- basic allocate / stats / release -------------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 4;
        l.embedding_length = 64;
        l.head_count = 8;
        l.head_dim = 8;
        l.kv_head_count = 8; // MHA (no GQA)
        l.kv_dim = 8 * 8;    // 64
        l.context_length = 128;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        Stats s = c.stats();
        CHECK(s.allocated == true);
        CHECK(s.layer_count == 4);
        CHECK(s.kv_dim == 64);
        CHECK(s.capacity_positions == 128);
        CHECK(s.used_positions == 0);
        CHECK(s.used_bytes == 0);
        // 2 (K+V) * 4 layers * 128 ctx * 64 kv_dim * 2 bytes (f16) = 131072
        CHECK(s.capacity_bytes == 2ull * 4 * 128 * 64 * 2);
        CHECK(s.quantization == "f16");
    }

    // --- PHASE 5 REGRESSION: expected bytes == actual allocation size
    //     == reported capacity_bytes ------------------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 4;
        l.embedding_length = 64;
        l.head_count = 8;
        l.head_dim = 8;
        l.kv_head_count = 8;
        l.kv_dim = 64;
        l.context_length = 128;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        const uint64_t expected =
            2ull /* K+V */ * 4 /* layers */ * 128 /* ctx */ *
            64 /* kv_dim */ * sizeof(uint16_t);

        Stats s = c.stats();
        CHECK(expected == s.capacity_bytes); // reporting == math
        // The allocation itself is sizeof(uint16_t) * total elements.
        // layer_elem_count() exposes the per-layer element span; the
        // physical allocation is 2 * layers * layer_elems * 2 bytes.
        CHECK(c.layer_elem_count() == 128ull * 64ull);
        const uint64_t actual_alloc =
            2ull * l.layer_count * c.layer_elem_count() * sizeof(uint16_t);
        CHECK(actual_alloc == s.capacity_bytes); // reporting == physical
    }

    // --- PHASE 5 REGRESSION: layer offsets do not overlap --------------
    {
        Cache c;
        Layout l;
        l.layer_count = 4;
        l.embedding_length = 64;
        l.head_count = 8;
        l.head_dim = 8;
        l.kv_head_count = 8;
        l.kv_dim = 64;
        l.context_length = 128;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        // Distinct layers must address disjoint spans.
        uint16_t* k0 = c.k_layer(0);
        uint16_t* k1 = c.k_layer(1);
        uint16_t* k3 = c.k_layer(3);
        uint16_t* v0 = c.v_layer(0);
        uint16_t* v3 = c.v_layer(3);
        CHECK(k0 != nullptr && k1 != nullptr && k3 != nullptr);
        CHECK(v0 != nullptr && v3 != nullptr);

        const uint64_t span = c.layer_elem_count();
        CHECK(static_cast<uint64_t>(k1 - k0) == span);       // exact stride
        CHECK(static_cast<uint64_t>(k3 - k1) == 2 * span);
        CHECK(static_cast<uint64_t>(v3 - v0) == 3 * span);
        // V block starts after the whole K block.
        CHECK(static_cast<uint64_t>(v0 - k0) == span * 4);
        // Layer 0's K span must not reach into layer 1's K span.
        CHECK(static_cast<uint64_t>(k1 - k0) >= span);
    }

    // --- PHASE 5 REGRESSION: k_at / v_at bounds + position addressing ---
    {
        Cache c;
        Layout l;
        l.layer_count = 2;
        l.embedding_length = 16;
        l.head_count = 2;
        l.head_dim = 8;
        l.kv_head_count = 2;
        l.kv_dim = 16;
        l.context_length = 32;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        CHECK(c.k_at(0, 0) == c.k_layer(0));
        CHECK(c.k_at(1, 0) == c.k_layer(1));
        CHECK(static_cast<uint64_t>(c.k_at(0, 1) - c.k_at(0, 0)) == 16);
        CHECK(static_cast<uint64_t>(c.v_at(0, 1) - c.v_at(0, 0)) == 16);
        // Bounds: out-of-range position / layer → nullptr.
        CHECK(c.k_at(0, 32) == nullptr);
        CHECK(c.k_at(2, 0) == nullptr);
        CHECK(c.v_at(0, 999) == nullptr);
    }

    // --- PHASE 5 REGRESSION: fp16 round-trip through the real storage ---
    {
        Cache c;
        Layout l;
        l.layer_count = 1;
        l.embedding_length = 8;
        l.head_count = 1;
        l.head_dim = 8;
        l.kv_head_count = 1;
        l.kv_dim = 8;
        l.context_length = 4;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        uint16_t* k = c.k_at(0, 2);
        CHECK(k != nullptr);
        const float in[8] = {0.5f,  -1.25f, 3.0f, 0.001f,
                             42.0f, -0.7f,  8.0f, 1e-4f};
        for (int i = 0; i < 8; ++i) {
            k[i] = shtn::fp16::fp32_to_fp16_bits(in[i]);
        }
        for (int i = 0; i < 8; ++i) {
            const float out = shtn::fp16::fp16_bits_to_fp32(k[i]);
            const float rel = out == 0.0f && in[i] == 0.0f
                                  ? 0.0f
                                  : (out - in[i]) / in[i];
            CHECK(rel > -0.001f && rel < 0.001f); // fp16 precision
        }
    }

    // --- PHASE 5 REGRESSION: reset() preserves memory, clears counters ---
    {
        Cache c;
        Layout l;
        l.layer_count = 1;
        l.embedding_length = 8;
        l.head_count = 1;
        l.head_dim = 8;
        l.kv_head_count = 1;
        l.kv_dim = 8;
        l.context_length = 4;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        uint16_t* k = c.k_at(0, 1);
        k[0] = shtn::fp16::fp32_to_fp16_bits(7.0f);
        c.advance(3);
        CHECK(c.used_positions() == 3);
        CHECK(c.stats().used_bytes == 3ull * c.bytes_per_position());

        c.reset();
        Stats s = c.stats();
        CHECK(s.used_positions == 0);
        CHECK(s.used_bytes == 0);
        // The buffer was NOT cleared: the written value survives reset
        // (reset is a counter operation, not a memset).
        uint16_t* k2 = c.k_at(0, 1);
        CHECK(k2 == k);
        CHECK(shtn::fp16::fp16_bits_to_fp32(k2[0]) == 7.0f);
    }

    // --- PHASE 5 REGRESSION: used_bytes math across requests -----------
    {
        Cache c;
        Layout l;
        l.layer_count = 2;
        l.embedding_length = 32;
        l.head_count = 4;
        l.head_dim = 8;
        l.kv_head_count = 4;
        l.kv_dim = 32;
        l.context_length = 64;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        // First request: 10 positions.
        c.advance(10);
        Stats s = c.stats();
        CHECK(s.used_positions == 10);
        CHECK(s.used_bytes == 10ull * 2 * 2 * 32 * 2); // pos*KV*layers*kv_dim*2B

        // New request boundary: reset, then a different count.
        c.reset();
        c.advance(7);
        s = c.stats();
        CHECK(s.used_positions == 7);
        CHECK(s.used_bytes == 7ull * 2 * 2 * 32 * 2);
        // used can never exceed capacity.
        CHECK(s.used_bytes <= s.capacity_bytes);
    }

    // --- advance updates used_positions honestly ------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 2;
        l.embedding_length = 32;
        l.head_count = 4;
        l.head_dim = 8;
        l.kv_head_count = 4;
        l.kv_dim = 32;
        l.context_length = 64;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        c.advance(10);
        Stats s = c.stats();
        CHECK(s.used_positions == 10);
        CHECK(s.used_bytes > 0); // proportional to used_positions

        c.advance(100); // overshoots capacity — clamps
        s = c.stats();
        CHECK(s.used_positions == 64);
        CHECK(s.used_bytes == s.capacity_bytes);

        c.reset();
        s = c.stats();
        CHECK(s.used_positions == 0);
        CHECK(s.used_bytes == 0);
    }

    // --- GQA: kv_head_count < head_count -------------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 4;
        l.embedding_length = 64;
        l.head_count = 8;
        l.head_dim = 8;
        l.kv_head_count = 2; // GQA 4:1
        l.kv_dim = 2 * 8;    // 16 (NOT 64)
        l.context_length = 128;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        Stats s = c.stats();
        CHECK(s.kv_dim == 16);
        // 2 (K+V) * 4 layers * 128 ctx * 16 kv_dim * 2 bytes = 32768
        CHECK(s.capacity_bytes == 2ull * 4 * 128 * 16 * 2);
    }

    // --- invalid layout rejected ----------------------------------------
    {
        Cache c;
        Layout l; // all zeros
        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_ERR_INVALID_ARG);
    }

    // --- context length cap enforced ------------------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 1;
        l.embedding_length = 8;
        l.head_count = 1;
        l.head_dim = 8;
        l.kv_head_count = 1;
        l.kv_dim = 8;
        l.context_length = (1ull << 21); // > kMaxContextLength

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_ERR_UNSUPPORTED);
    }

    // --- available RAM check honored -----------------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 4;
        l.embedding_length = 64;
        l.head_count = 8;
        l.head_dim = 8;
        l.kv_head_count = 8;
        l.kv_dim = 64;
        l.context_length = 128;

        std::string err;
        // Need 131072 bytes; give 1024 → reject.
        CHECK(c.allocate(l, 1024, err) == SHTN_ERR_UNSUPPORTED);
        // Give enough → succeed.
        CHECK(c.allocate(l, 1ull << 20, err) == SHTN_OK);
    }

    // --- release clears state ------------------------------------------
    {
        Cache c;
        Layout l;
        l.layer_count = 1;
        l.embedding_length = 8;
        l.head_count = 1;
        l.head_dim = 8;
        l.kv_head_count = 1;
        l.kv_dim = 8;
        l.context_length = 16;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);
        CHECK(c.allocated() == true);
        c.release();
        CHECK(c.allocated() == false);
        Stats s = c.stats();
        CHECK(s.allocated == false);
        CHECK(s.capacity_bytes == 0);
    }

    // --- move semantics -------------------------------------------------
    {
        Cache a;
        Layout l;
        l.layer_count = 1;
        l.embedding_length = 8;
        l.head_count = 1;
        l.head_dim = 8;
        l.kv_head_count = 1;
        l.kv_dim = 8;
        l.context_length = 16;

        std::string err;
        CHECK(a.allocate(l, 0, err) == SHTN_OK);

        Cache b = std::move(a);
        CHECK(b.allocated() == true);
        CHECK(a.allocated() == false); // NOLINT(bugprone-use-after-move)
        Stats s = b.stats();
        CHECK(s.capacity_positions == 16);
    }

    // --- k_layer / v_layer return non-null after allocate --------------
    {
        Cache c;
        Layout l;
        l.layer_count = 2;
        l.embedding_length = 16;
        l.head_count = 2;
        l.head_dim = 8;
        l.kv_head_count = 2;
        l.kv_dim = 16;
        l.context_length = 32;

        std::string err;
        CHECK(c.allocate(l, 0, err) == SHTN_OK);

        CHECK(c.k_layer(0) != nullptr);
        CHECK(c.k_layer(1) != nullptr);
        CHECK(c.v_layer(0) != nullptr);
        CHECK(c.v_layer(1) != nullptr);
        // K and V for the same layer must be different pointers.
        CHECK(c.k_layer(0) != c.v_layer(0));
        // Out-of-range layer returns nullptr.
        CHECK(c.k_layer(99) == nullptr);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_kv_cache: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_kv_cache: all checks passed\n");
    return 0;
}
