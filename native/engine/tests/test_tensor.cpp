// test_tensor.cpp — Phase 5 tensor access + dequantization tests.
//
// Verifies: lookup, type validation (supported set exactly F32/F16/Q4_0/
// Q4_1/Q5_0/Q5_1/Q8_0), shape validation, byte-range validation, row
// dequantization against HAND-COMPUTED reference values, and explicit
// unsupported-type failures.

#include "shtn/engine.h"

#include "fp16.h"
#include "gguf.h"
#include "tensor.h"

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
        }                                                                   \
    } while (0)

// --- helpers -----------------------------------------------------------------

using namespace shtn;

static gguf::GgufHeader build_header_with(
    const std::vector<std::pair<std::string, std::pair<uint32_t, uint64_t>>>&
        extra_kv,
    const std::vector<gguf::TensorInfo>& tensors) {
    gguf::GgufHeader h;
    h.version = 3;
    h.tensor_count = tensors.size();
    h.kv_count = extra_kv.size();
    for (const auto& kv : extra_kv) {
        gguf::Scalar s;
        s.type = kv.second.first;
        s.u64_v = kv.second.second;
        h.metadata.emplace_back(kv.first, s);
    }
    h.tensors = tensors;
    h.alignment = 32;
    h.data_start = 0; // bound later
    return h;
}

// A synthetic data section: 4 KiB of bytes we can point tensors into.
struct TestData {
    std::vector<uint8_t> bytes;
    gguf::GgufHeader header;
    tensor::Weights weights;

    TestData(const std::vector<gguf::TensorInfo>& tensors,
             const std::vector<uint8_t>& data)
        : bytes(data) {
        std::vector<gguf::TensorInfo> ts = tensors;
        // Lay tensors out sequentially in the data section.
        uint64_t off = 0;
        for (auto& t : ts) {
            t.offset = off;
            off += t.nbytes_estimate > 0 ? t.nbytes_estimate : 64;
        }
        header = build_header_with({{"general.architecture",
                                     {gguf::kTypeString, 0}}},
                                   ts);
        header.data_start = 0;
        header.data_bytes = bytes.size();
        // Rebuild infos with offsets.
        header.tensors.clear();
        header.tensor_count = ts.size();
        for (auto& t : ts) {
            header.tensors.push_back(t);
        }
        weights = tensor::Weights(header);
        weights.bind_data(bytes.data(), bytes.size());
    }
};

static void put_f32(std::vector<uint8_t>& b, float v) {
    uint32_t raw = 0;
    std::memcpy(&raw, &v, 4);
    b.push_back(static_cast<uint8_t>(raw));
    b.push_back(static_cast<uint8_t>(raw >> 8));
    b.push_back(static_cast<uint8_t>(raw >> 16));
    b.push_back(static_cast<uint8_t>(raw >> 24));
}

static void put_f16(std::vector<uint8_t>& b, float v) {
    const uint16_t h = fp16::fp32_to_fp16_bits(v);
    b.push_back(static_cast<uint8_t>(h));
    b.push_back(static_cast<uint8_t>(h >> 8));
}

int main() {
    // --- supported-type set is EXACT ------------------------------------
    {
        CHECK(tensor::type_supported(0));  // F32
        CHECK(tensor::type_supported(1));  // F16
        CHECK(tensor::type_supported(2));  // Q4_0
        CHECK(tensor::type_supported(3));  // Q4_1
        CHECK(tensor::type_supported(6));  // Q5_0
        CHECK(tensor::type_supported(7));  // Q5_1
        CHECK(tensor::type_supported(8));  // Q8_0
        CHECK(!tensor::type_supported(12)); // Q4_K — NOT supported
        CHECK(!tensor::type_supported(14)); // Q6_K
        CHECK(!tensor::type_supported(30)); // BF16
        CHECK(!tensor::type_supported(99)); // nonsense
        CHECK(std::strcmp(tensor::type_name(2), "Q4_0") == 0);
        CHECK(std::strcmp(tensor::type_name(12), "unsupported") == 0);
    }

    // --- F32 row dequant + shape validation ------------------------------
    {
        std::vector<uint8_t> data;
        for (int i = 0; i < 16; ++i) {
            put_f32(data, static_cast<float>(i) * 0.5f);
        }

        gguf::TensorInfo t;
        t.name = "w";
        t.type = 0; // F32
        t.n_dims = 2;
        t.dims[0] = 4;
        t.dims[1] = 4;
        t.offset = 0;
        t.nbytes_estimate = 4 * 4 * 4;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("w", 2, 4, 4, out, err) == SHTN_OK);
        CHECK(out.size() >= 4);
        CHECK(out[0] == 4.0f); // row 2 starts at element 8 → 8*0.5
        CHECK(out[1] == 4.5f);
        CHECK(out[3] == 5.5f);

        // Wrong ne0 expectation → shape rejection.
        CHECK(td.weights.row_f32("w", 0, 5, 4, out, err) ==
              SHTN_ERR_MODEL_FORMAT);
        // Wrong rows expectation → rejection.
        CHECK(td.weights.row_f32("w", 0, 4, 5, out, err) ==
              SHTN_ERR_MODEL_FORMAT);
        // Out-of-range row → rejection.
        CHECK(td.weights.row_f32("w", 4, 4, 4, out, err) ==
              SHTN_ERR_MODEL_FORMAT);
        // Missing tensor → rejection.
        CHECK(td.weights.row_f32("nope", 0, 0, 0, out, err) ==
              SHTN_ERR_MODEL_FORMAT);
    }

    // --- F16 row dequant --------------------------------------------------
    {
        std::vector<uint8_t> data;
        const float vals[4] = {1.0f, -2.5f, 0.25f, 100.0f};
        for (float v : vals) {
            put_f16(data, v);
        }
        // Second row repeats.
        for (float v : vals) {
            put_f16(data, v);
        }

        gguf::TensorInfo t;
        t.name = "h";
        t.type = 1; // F16
        t.n_dims = 2;
        t.dims[0] = 4;
        t.dims[1] = 2;
        t.offset = 0;
        t.nbytes_estimate = 2 * 4 * 2;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("h", 1, 4, 2, out, err) == SHTN_OK);
        CHECK(out.size() >= 4);
        CHECK(std::fabs(out[0] - 1.0f) < 1e-3);
        CHECK(std::fabs(out[1] + 2.5f) < 1e-2);
        CHECK(std::fabs(out[3] - 100.0f) < 0.05f);
    }

    // --- Q4_0 dequant against HAND-COMPUTED values ------------------------
    // One block: d = 2.0 (fp16). Nibbles: qs[0] = 0x12 (low=2, high=1),
    // the rest 0x55 (both nibbles 5).
    {
        std::vector<uint8_t> data;
        // d as fp16 bits of 2.0 = 0x4000.
        data.push_back(0x00);
        data.push_back(0x40);
        for (int k = 0; k < 16; ++k) {
            data.push_back(k == 0 ? 0x12 : 0x55);
        }

        gguf::TensorInfo t;
        t.name = "q4";
        t.type = 2; // Q4_0
        t.n_dims = 2;
        t.dims[0] = 32; // one full block
        t.dims[1] = 1;
        t.offset = 0;
        t.nbytes_estimate = 18;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("q4", 0, 32, 1, out, err) == SHTN_OK);
        // value = (q - 8) * d. Element 0: q=2 → -12; element 1: q=1 → -14;
        // elements 2..31: q=5 → -6.
        CHECK(std::fabs(out[0] + 12.0f) < 1e-4);
        CHECK(std::fabs(out[1] + 14.0f) < 1e-4);
        for (int j = 2; j < 32; ++j) {
            CHECK(std::fabs(out[j] + 6.0f) < 1e-4);
        }
    }

    // --- Q8_0 dequant -------------------------------------------------------
    {
        std::vector<uint8_t> data;
        // d = 0.5 fp16 = 0x3800.
        data.push_back(0x00);
        data.push_back(0x38);
        // 32 int8 values: j - 16 → j = 0..31.
        for (int j = 0; j < 32; ++j) {
            data.push_back(static_cast<uint8_t>(
                static_cast<int8_t>(j - 16)));
        }

        gguf::TensorInfo t;
        t.name = "q8";
        t.type = 8; // Q8_0
        t.n_dims = 2;
        t.dims[0] = 32;
        t.dims[1] = 1;
        t.offset = 0;
        t.nbytes_estimate = 34;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("q8", 0, 32, 1, out, err) == SHTN_OK);
        for (int j = 0; j < 32; ++j) {
            const float want = static_cast<float>(j - 16) * 0.5f;
            CHECK(std::fabs(out[j] - want) < 1e-4);
        }
    }

    // --- Q5_0 dequant ---------------------------------------------------------
    // d = 1.0; element j: hi bit = j % 2 (so hi = 1 for odd), lo = j/2.
    // q5 = 16*hi + lo → value = (q5 - 16) * 1.0.
    {
        std::vector<uint8_t> data;
        // d = 1.0 fp16 = 0x3C00.
        data.push_back(0x00);
        data.push_back(0x3C);
        // qh[4]: bit j of byte j/8 is element j's high bit. Elements 0..31:
        // set high bit for every odd element → byte value = 0b10101010.
        for (int b = 0; b < 4; ++b) {
            data.push_back(0xAA);
        }
        // qs[16]: nibbles lo(j) = j/2: element j's low nibble = j/2.
        // nibble_at(j) = byte j/2's low nibble if j even, high if odd.
        // We want lo(j) = j/2 → byte k must hold low = k, high = k.
        for (int k = 0; k < 16; ++k) {
            data.push_back(static_cast<uint8_t>(k | (k << 4)));
        }

        gguf::TensorInfo t;
        t.name = "q5";
        t.type = 6; // Q5_0
        t.n_dims = 2;
        t.dims[0] = 32;
        t.dims[1] = 1;
        t.offset = 0;
        t.nbytes_estimate = 22;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("q5", 0, 32, 1, out, err) == SHTN_OK);
        for (int j = 0; j < 32; ++j) {
            const int hi = (0xAA >> (j % 8)) & 1; // matches the packing
            const int lo = j / 2;
            const float want = static_cast<float>(hi * 16 + lo - 16);
            CHECK(std::fabs(out[j] - want) < 1e-4);
        }
    }

    // --- Q4_1 dequant --------------------------------------------------------
    // d = 1.0, m = 0.25, qs byte k = k | (k << 4) → element j has nibble
    // floor(j / 2) (≤ 15, fits).
    {
        std::vector<uint8_t> data;
        // d = 1.0 (0x3C00), m = 0.25 (0x3400).
        data.push_back(0x00);
        data.push_back(0x3C);
        data.push_back(0x00);
        data.push_back(0x34);
        for (int k = 0; k < 16; ++k) {
            data.push_back(static_cast<uint8_t>(k | (k << 4)));
        }

        gguf::TensorInfo t;
        t.name = "q41";
        t.type = 3; // Q4_1
        t.n_dims = 2;
        t.dims[0] = 32;
        t.dims[1] = 1;
        t.offset = 0;
        t.nbytes_estimate = 20;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("q41", 0, 32, 1, out, err) == SHTN_OK);
        // value = q * d + m → floor(j/2) * 1.0 + 0.25.
        for (int j = 0; j < 32; ++j) {
            const float want = static_cast<float>(j / 2) + 0.25f;
            CHECK(std::fabs(out[j] - want) < 1e-3);
        }
    }

    // --- Q5_1 dequant ---------------------------------------------------------
    {
        std::vector<uint8_t> data;
        // d = 2.0, m = -1.0.
        data.push_back(0x00);
        data.push_back(0x40);
        data.push_back(0x00);
        data.push_back(0xBC);
        for (int b = 0; b < 4; ++b) {
            data.push_back(0x00); // all high bits 0
        }
        for (int k = 0; k < 16; ++k) {
            data.push_back(static_cast<uint8_t>(k | (k << 4)));
        }

        gguf::TensorInfo t;
        t.name = "q51";
        t.type = 7; // Q5_1
        t.n_dims = 2;
        t.dims[0] = 32;
        t.dims[1] = 1;
        t.offset = 0;
        t.nbytes_estimate = 24;

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("q51", 0, 32, 1, out, err) == SHTN_OK);
        // value = q * 2 - 1, q = lo = j/2.
        for (int j = 0; j < 32; ++j) {
            const float want = static_cast<float>(j / 2) * 2.0f - 1.0f;
            CHECK(std::fabs(out[j] - want) < 1e-3);
        }
    }

    // --- unsupported type → EXPLICIT failure ---------------------------------
    {
        gguf::TensorInfo t;
        t.name = "qk";
        t.type = 12; // Q4_K — unsupported
        t.n_dims = 2;
        t.dims[0] = 256;
        t.dims[1] = 1;
        t.offset = 0;
        t.nbytes_estimate = 0; // unknown to the estimator

        TestData td({t}, std::vector<uint8_t>(64, 0));

        tensor::RowBuf out;
        std::string err;
        const int32_t rc = td.weights.row_f32("qk", 0, 0, 0, out, err);
        CHECK(rc == SHTN_ERR_UNSUPPORTED);
        CHECK(err.find("not supported") != std::string::npos);
    }

    // --- byte-range validation -------------------------------------------------
    {
        gguf::TensorInfo t;
        t.name = "oob";
        t.type = 0; // F32
        t.n_dims = 2;
        t.dims[0] = 4;
        t.dims[1] = 2;
        t.offset = 0; // auto-layout puts row 1 past the 8-byte data
        t.nbytes_estimate = 32;

        // 16 bytes of data: row 0 (16 bytes) fits exactly, row 1 leaves
        // the section.
        std::vector<uint8_t> data;
        for (int i = 0; i < 16; ++i) {
            data.push_back(0);
        }

        TestData td({t}, data);

        tensor::RowBuf out;
        std::string err;
        CHECK(td.weights.row_f32("oob", 0, 4, 2, out, err) == SHTN_OK);
        CHECK(td.weights.row_f32("oob", 1, 4, 2, out, err) ==
              SHTN_ERR_MODEL_FORMAT);
        CHECK(err.find("leaves the mapped data section") !=
              std::string::npos);
    }

    // --- dequant_block primitives directly -----------------------------------
    {
        // Q4_0 with d = 1.0, all nibbles 5 → value = (5-8)*1 = -3.
        uint8_t blk[18];
        blk[0] = 0x00;
        blk[1] = 0x3C;
        for (int i = 0; i < 16; ++i) {
            blk[2 + i] = 0x55;
        }
        float out[32];
        CHECK(tensor::dequant_block(2, blk, out));
        for (int j = 0; j < 32; ++j) {
            CHECK(std::fabs(out[j] + 3.0f) < 1e-5);
        }
        CHECK(!tensor::dequant_block(12, blk, out)); // unsupported
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_tensor: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_tensor: all checks passed\n");
    return 0;
}
