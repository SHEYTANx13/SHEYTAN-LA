// gguf_writer.h — dependency-free GGUF file builder for the test suite.
//
// Writes real GGUF v2/v3 files (little-endian, spec-conformant layout)
// plus deliberately-corrupted variants for the validation tests. TEST
// ONLY — not part of the engine.

#ifndef SHTN_TEST_GGUF_WRITER_H
#define SHTN_TEST_GGUF_WRITER_H

#include <cstdint>
#include <cstring>
#include <fstream>
#include <functional>
#include <string>
#include <vector>

namespace gguf_test {

inline void put_u32(std::vector<uint8_t>& b, uint32_t v) {
    b.push_back(static_cast<uint8_t>(v & 0xFF));
    b.push_back(static_cast<uint8_t>((v >> 8) & 0xFF));
    b.push_back(static_cast<uint8_t>((v >> 16) & 0xFF));
    b.push_back(static_cast<uint8_t>((v >> 24) & 0xFF));
}

inline void put_u64(std::vector<uint8_t>& b, uint64_t v) {
    put_u32(b, static_cast<uint32_t>(v & 0xFFFFFFFFu));
    put_u32(b, static_cast<uint32_t>(v >> 32));
}

inline void put_f32(std::vector<uint8_t>& b, float v) {
    uint32_t raw = 0;
    std::memcpy(&raw, &v, 4);
    put_u32(b, raw);
}

inline void put_str(std::vector<uint8_t>& b, const std::string& s) {
    put_u64(b, s.size());
    b.insert(b.end(), s.begin(), s.end());
}

// Typed metadata value writers (subset the tests need).
inline void kv_u32(std::vector<uint8_t>& b, const std::string& k, uint32_t v) {
    put_str(b, k);
    put_u32(b, 4); // uint32
    put_u32(b, v);
}

inline void kv_u64(std::vector<uint8_t>& b, const std::string& k, uint64_t v) {
    put_str(b, k);
    put_u32(b, 10); // uint64
    put_u64(b, v);
}

inline void kv_str(std::vector<uint8_t>& b, const std::string& k,
                   const std::string& v) {
    put_str(b, k);
    put_u32(b, 8); // string
    put_str(b, v);
}

// kv_string_array writes {"type": 9 (array), "elem": 8 (string)}.
inline void kv_string_array(std::vector<uint8_t>& b, const std::string& k,
                            const std::vector<std::string>& vals) {
    put_str(b, k);
    put_u32(b, 9); // array
    put_u32(b, 8); // of strings
    put_u64(b, vals.size());
    for (const std::string& v : vals) {
        put_str(b, v);
    }
}

// Tensor entry: name, n_dims, dims, type, offset.
inline void tensor_info(std::vector<uint8_t>& b, const std::string& name,
                        const std::vector<uint64_t>& dims, uint32_t type,
                        uint64_t offset) {
    put_str(b, name);
    put_u32(b, static_cast<uint32_t>(dims.size()));
    for (uint64_t d : dims) {
        put_u64(b, d);
    }
    put_u32(b, type);
    put_u64(b, offset);
}

// BuildOptions describes one synthetic GGUF file.
struct BuildOptions {
    uint32_t version = 3;
    uint64_t alignment = 32;
    std::vector<std::function<void(std::vector<uint8_t>&)>> extra_kv;
    std::vector<std::function<void(std::vector<uint8_t>&)>> tensors;
    uint64_t data_bytes = 0;          // bytes appended after the header
    bool write_counts = true;         // false → header stops after version
    bool write_tensor_count = true;   // false → kv count only (truncation)
    uint64_t override_tensor_count = 0;
    uint64_t override_kv_count = 0;
    uint32_t override_version = 0;    // 0 = use version
};

// Build assembles a GGUF file image in memory.
inline std::vector<uint8_t> build(const BuildOptions& o) {
    std::vector<uint8_t> b;
    b.push_back('G');
    b.push_back('G');
    b.push_back('U');
    b.push_back('F');

    put_u32(b, o.override_version != 0 ? o.override_version : o.version);

    if (!o.write_counts) {
        return b;
    }

    put_u64(b, o.write_tensor_count
                   ? (o.override_tensor_count != 0 ? o.override_tensor_count
                                                   : o.tensors.size())
                   : 0);
    put_u64(b, o.override_kv_count != 0 ? o.override_kv_count
                                        : o.extra_kv.size());

    for (const auto& kv : o.extra_kv) {
        kv(b);
    }
    for (const auto& t : o.tensors) {
        t(b);
    }

    // Pad the header to the alignment before the data section.
    while (b.size() % o.alignment != 0) {
        b.push_back(0);
    }

    const size_t before = b.size();
    b.resize(before + static_cast<size_t>(o.data_bytes), 0xAB);

    return b;
}

// WriteFile writes the image to disk.
inline bool write_file(const std::string& path,
                       const std::vector<uint8_t>& bytes) {
    std::ofstream f(path, std::ios::binary | std::ios::trunc);
    if (!f) {
        return false;
    }
    f.write(reinterpret_cast<const char*>(bytes.data()),
            static_cast<std::streamsize>(bytes.size()));
    return static_cast<bool>(f);
}

// A small, fully valid llama-architecture model used by many tests.
// 3 tensors (F32 token_embd 64x96, F32 output 96x64, F16 attn norm 64),
// vocabulary array of 96 entries, ctx 256.
struct TinyModel {
    std::string path;
    std::vector<uint8_t> image;

    uint64_t parameter_count() const {
        return 64ull * 96ull + 96ull * 64ull + 64ull;
    }
};

inline TinyModel make_tiny_model(const std::string& path,
                                 uint64_t alignment = 32) {
    TinyModel m;
    m.path = path;

    BuildOptions o;
    o.version = 3;
    o.alignment = alignment;

    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_str(b, "general.architecture", "llama");
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_str(b, "general.name", "tiny-test-model");
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_u32(b, "general.file_type", 1); // F16
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_u32(b, "llama.context_length", 256);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_u32(b, "llama.embedding_length", 64);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_u32(b, "llama.block_count", 2);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_u32(b, "llama.vocab_size", 96);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        kv_string_array(b, "tokenizer.ggml.tokens",
                        std::vector<std::string>(96, "tok"));
    });

    // Offsets within the (aligned) data section.
    o.tensors.push_back([](std::vector<uint8_t>& b) {
        tensor_info(b, "token_embd.weight", {64, 96}, 0 /*F32*/, 0);
    });
    o.tensors.push_back([](std::vector<uint8_t>& b) {
        tensor_info(b, "output.weight", {96, 64}, 0 /*F32*/, 64ull * 96ull * 4ull);
    });
    o.tensors.push_back([](std::vector<uint8_t>& b) {
        tensor_info(b, "blk.0.attn_norm.weight", {64}, 1 /*F16*/,
                    64ull * 96ull * 4ull + 96ull * 64ull * 4ull);
    });

    o.data_bytes = 64ull * 96ull * 4ull + 96ull * 64ull * 4ull + 64ull * 2ull;

    m.image = build(o);
    return m;
}

} // namespace gguf_test

#endif /* SHTN_TEST_GGUF_WRITER_H */
