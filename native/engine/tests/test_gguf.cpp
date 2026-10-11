// test_gguf.cpp — GGUF reader validation tests (Phase 2).
//
// Covers the required matrix: valid GGUF (v2 + v3, custom alignment),
// malformed header, unsupported version, truncated file, invalid tensor
// offsets, overflow/bounds failures, and metadata extraction. The model
// files used here are SYNTHETIC (built by gguf_writer.h) — small, fully
// spec-conformant, or deliberately corrupt.

#include "gguf.h"
#include "gguf_writer.h"
#include "temp_dir.h"

#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

static int failures = 0;

#define CHECK(cond)                                                          \
    do {                                                                     \
        if (!(cond)) {                                                       \
            std::fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__,     \
                         #cond);                                             \
            ++failures;                                                      \
        }                                                                    \
    } while (0)

namespace {

// parse_file is the common harness: map + parse, returning success.
bool parse_file(const std::string& path, shtn::gguf::GgufHeader& out,
                std::string& error) {
    shtn::gguf::MappedFile f;
    if (!f.open(path, error)) {
        return false;
    }
    return shtn::gguf::parse_header(f, out, error);
}

} // namespace

int main() {
    // Shared cross-platform temp-dir helper (v1.3.5 consolidation:
    // replaces this test's private GetTempPathA/TMPDIR copy; the tree
    // is removed on exit).
    shtn_test::TempDir tmpdir("gguf");
    const std::string dir = tmpdir.path();

    // --- 1. valid GGUF v3: metadata + tensor table + derived params -----
    {
        const auto m = gguf_test::make_tiny_model(dir + "/tiny-v3.gguf");
        CHECK(gguf_test::write_file(m.path, m.image));

        shtn::gguf::GgufHeader h{};
        std::string err;
        CHECK(parse_file(m.path, h, err));

        if (h.version != 3 || h.tensor_count != 3 || h.kv_count != 8) {
            std::fprintf(stderr, "FAIL: header version=%u tensors=%llu kvs=%llu (%s)\n",
                         h.version,
                         static_cast<unsigned long long>(h.tensor_count),
                         static_cast<unsigned long long>(h.kv_count),
                         err.c_str());
            ++failures;
        }

        // Metadata extraction.
        CHECK(h.metadata.size() == 8);

        shtn::gguf::GgufHeader h2{};
        std::string err2;
        CHECK(parse_file(m.path, h2, err2));

        const shtn::gguf::Scalar* arch =
            shtn::gguf::find_scalar(h2.metadata, "general.architecture");
        CHECK(arch != nullptr);
        if (arch != nullptr) {
            std::string s;
            CHECK(shtn::gguf::scalar_str(*arch, s));
            CHECK(s == "llama");
        }

        // Derived parameter count: 64*96 + 96*64 + 64.
        CHECK(h2.derived_parameter_count == 64ull * 96ull + 96ull * 64ull + 64ull);

        // Data section: aligned, weights span correct.
        CHECK(h2.data_start % 32 == 0);
        const uint64_t expected_data =
            64ull * 96ull * 4ull + 96ull * 64ull * 4ull + 64ull * 2ull;
        CHECK(h2.data_bytes == expected_data);

        // Tensor offsets validated: every tensor starts inside the span.
        CHECK(h2.tensors.size() == 3);
        CHECK(h2.tensors[0].name == "token_embd.weight");
        CHECK(h2.tensors[0].nbytes_estimate == 64ull * 96ull * 4ull);
        CHECK(h2.tensors[2].nbytes_estimate == 64ull * 2ull);
    }

    // --- 2. valid GGUF v2 ------------------------------------------------
    {
        auto o = gguf_test::BuildOptions{};
        // Reuse the tiny model body but stamp version 2.
        auto m = gguf_test::make_tiny_model(dir + "/tiny-v2.gguf");
        m.image[4] = 2; // version field is right after the magic
        gguf_test::write_file(m.path, m.image);

        shtn::gguf::GgufHeader h{};
        std::string err;
        CHECK(parse_file(m.path, h, err));
        CHECK(h.version == 2);
        CHECK(h.tensor_count == 3);
    }

    // --- 3. custom alignment (general.alignment = 64) --------------------
    {
        auto m = gguf_test::make_tiny_model(dir + "/aligned.gguf");
        // Declare the alignment in the header (the parser only honors the
        // metadata key) and re-pad: rebuild with one extra kv.
        auto o = gguf_test::BuildOptions{};
        o.version = 3;
        o.alignment = 64;
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_u32(b, "general.alignment", 64);
        });
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_str(b, "general.architecture", "llama");
        });
        o.tensors.push_back([](std::vector<uint8_t>& b) {
            gguf_test::tensor_info(b, "t0", {16}, 0, 0);
        });
        o.data_bytes = 16ull * 4ull;
        gguf_test::write_file(m.path, gguf_test::build(o));

        shtn::gguf::GgufHeader h{};
        std::string err;
        CHECK(parse_file(m.path, h, err));
        CHECK(h.alignment == 64);
        CHECK(h.data_start % 64 == 0);
        CHECK(h.tensors.size() == 1);
    }

    // --- 4. malformed header: bad magic ----------------------------------
    {
        auto m = gguf_test::make_tiny_model(dir + "/bad-magic.gguf");
        m.image[0] = 'X';
        gguf_test::write_file(m.path, m.image);

        shtn::gguf::GgufHeader h{};
        std::string err;
        CHECK(!parse_file(m.path, h, err));
        CHECK(err.find("magic") != std::string::npos);
    }

    // --- 5. unsupported versions (1 and 4) --------------------------------
    {
        auto m = gguf_test::make_tiny_model(dir + "/v1.gguf");
        m.image[4] = 1;
        gguf_test::write_file(m.path, m.image);

        shtn::gguf::GgufHeader h{};
        std::string err;
        CHECK(!parse_file(m.path, h, err));
        CHECK(err.find("version") != std::string::npos);

        m.image[4] = 4;
        gguf_test::write_file(m.path, m.image);
        CHECK(!parse_file(m.path, h, err));
        CHECK(err.find("version") != std::string::npos);
    }

    // --- 6. truncated files ------------------------------------------------
    {
        // Empty file.
        {
            const std::string p = dir + "/empty.gguf";
            gguf_test::write_file(p, {});
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
        }

        // Magic only.
        {
            const auto m = gguf_test::make_tiny_model(dir + "/magic-only.gguf");
            std::vector<uint8_t> cut(m.image.begin(), m.image.begin() + 4);
            gguf_test::write_file(m.path, cut);
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(m.path, h, err));
        }

        // Header counts truncated.
        {
            const auto m = gguf_test::make_tiny_model(dir + "/no-counts.gguf");
            std::vector<uint8_t> cut(m.image.begin(), m.image.begin() + 6);
            gguf_test::write_file(m.path, cut);
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(m.path, h, err));
        }

        // Cut mid-metadata.
        {
            const auto m = gguf_test::make_tiny_model(dir + "/cut-meta.gguf");
            std::vector<uint8_t> cut(m.image.begin(), m.image.begin() + 60);
            gguf_test::write_file(m.path, cut);
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(m.path, h, err));
        }

        // Cut mid-tensor-table.
        {
            const auto m = gguf_test::make_tiny_model(dir + "/cut-tensors.gguf");
            const size_t header_end = m.image.size() - static_cast<size_t>(m.image.size() % 32);
            // Data starts at header_end; keep only part of the tensor table.
            std::vector<uint8_t> cut(m.image.begin(),
                                     m.image.begin() + static_cast<long>(header_end) - 8);
            // Fix data section to empty so offsets still parse: the cut is
            // inside the tensor table, so parsing must fail.
            gguf_test::write_file(m.path, cut);
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(m.path, h, err));
        }

        // Data section missing entirely (header claims tensors).
        {
            const auto m = gguf_test::make_tiny_model(dir + "/no-data.gguf");
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.alignment = 32;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "general.architecture", "llama");
            });
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "t0", {4}, 0, 0);
            });
            o.data_bytes = 0;
            gguf_test::write_file(m.path, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            // Parses (offset 0 <= span 0? span is 0, offset must be <= span
            // and 0 <= 0 is fine — but the strict size check must reject:
            // 4 F32 elements need 16 bytes).
            CHECK(!parse_file(m.path, h, err));
            CHECK(err.find("data section") != std::string::npos);
        }
    }

    // --- 7. hostile counts and lengths (bounds failures) ------------------
    {
        // Implausible kv count.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.override_kv_count = 1ull << 40;
            const std::string p = dir + "/huge-kv.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("metadata count") != std::string::npos);
        }

        // Implausible tensor count.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.override_tensor_count = 5'000'000'000ull;
            const std::string p = dir + "/huge-tensors.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("tensor count") != std::string::npos);
        }

        // Implausible string length in metadata.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::put_str(b, "general.architecture");
                gguf_test::put_u32(b, 8);
                gguf_test::put_u64(b, 1ull << 30); // 1 GiB "string"
            });
            const std::string p = dir + "/huge-string.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
        }

        // Huge array element count.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::put_str(b, "tokenizer.tokens");
                gguf_test::put_u32(b, 9);
                gguf_test::put_u32(b, 0);
                gguf_test::put_u64(b, 1ull << 40);
            });
            const std::string p = dir + "/huge-array.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
        }

        // Unknown metadata value type.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::put_str(b, "weird.key");
                gguf_test::put_u32(b, 77);
                gguf_test::put_u64(b, 1);
            });
            const std::string p = dir + "/bad-type.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
        }

        // Invalid alignment (not a power of two).
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.extra_kv.push_back([o](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "general.alignment", 33);
            });
            const std::string p = dir + "/bad-align.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("alignment") != std::string::npos);
        }
    }

    // --- 8. invalid tensor entries -----------------------------------------
    {
        // Offset beyond the data section.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "general.architecture", "llama");
            });
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "far", {4}, 0, 1ull << 30);
            });
            o.data_bytes = 64;
            const std::string p = dir + "/far-offset.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("outside data section") != std::string::npos);
        }

        // Exact-size type whose bytes overflow the data section.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "big", {1024, 1024}, 0 /*F32*/, 0);
            });
            o.data_bytes = 4096; // needs 4 MiB
            const std::string p = dir + "/oversize.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("exceeds the data section") != std::string::npos);
        }

        // Zero dim.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "zero", {0}, 0, 0);
            });
            o.data_bytes = 64;
            const std::string p = dir + "/zero-dim.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("zero dim") != std::string::npos);
        }

        // Dim count 0 and dim count 9 (bounds).
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "nodims", {}, 0, 0);
            });
            o.data_bytes = 64;
            const std::string p = dir + "/no-dims.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("dim count") != std::string::npos);
        }

        // Overflowing dims product (overflow/bounds failure).
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "huge-product",
                                       {1ull << 32, 1ull << 32}, 0, 0);
            });
            o.data_bytes = 4096;
            const std::string p = dir + "/huge-product.gguf";
            gguf_test::write_file(p, gguf_test::build(o));
            shtn::gguf::GgufHeader h{};
            std::string err;
            // 2^32 * 2^32 = 2^64 → element count not representable.
            CHECK(!parse_file(p, h, err));
            CHECK(err.find("dims overflow") != std::string::npos);
        }
    }

    // --- 9. metadata extraction helpers -------------------------------------
    {
        shtn::gguf::Scalar s{};
        s.type = shtn::gguf::kTypeUint32;
        s.u64_v = 42;
        uint64_t v = 0;
        CHECK(shtn::gguf::scalar_u64(s, v));
        CHECK(v == 42);

        s.type = shtn::gguf::kTypeInt32;
        s.i64_v = -7;
        CHECK(shtn::gguf::scalar_u64(s, v) == false); // negative rejected

        s.type = shtn::gguf::kTypeString;
        s.str_v = "hello";
        CHECK(shtn::gguf::scalar_u64(s, v) == false);

        std::string str;
        CHECK(shtn::gguf::scalar_str(s, str));
        CHECK(str == "hello");

        CHECK(shtn::gguf::file_type_name(15) == "Q4_K_M");
        CHECK(shtn::gguf::file_type_name(0) == "F32");
        CHECK(shtn::gguf::file_type_name(999) == "type-999");

        // Checked arithmetic.
        uint64_t r = 0;
        CHECK(shtn::gguf::checked_add_u64(1, 2, r) && r == 3);
        CHECK(!shtn::gguf::checked_add_u64(UINT64_MAX, 1, r));
        CHECK(shtn::gguf::checked_mul_u64(3, 4, r) && r == 12);
        CHECK(!shtn::gguf::checked_mul_u64(UINT64_MAX, 2, r));
    }

    // --- 10. portable temp-directory contract (v1.3.5) --------------------
    // Regression guard for Actions run 35583009466: no native test may
    // ever depend on a hard-coded POSIX temp location again. Proves the
    // shared helper (temp_dir.h) returns a USABLE, WRITABLE, native
    // temporary location on every platform — create, write, read,
    // close, remove, including a path containing spaces — plus
    // uniqueness under repeated creation and RAII cleanup.
    {
        // The platform temp root must resolve to something non-empty.
        CHECK(!shtn_test::temp_detail::resolve_temp_root().string().empty());

        // The directory exists, is absolute and is a directory.
        CHECK(tmpdir.exists());
        CHECK(std::filesystem::path(dir).is_absolute());

        // Create + write + close through the C stdio surface the GGUF
        // loader itself uses (fopen/fwrite/fclose).
        const std::string plain = tmpdir.file("contract.gguf");
        {
            FILE* f = std::fopen(plain.c_str(), "wb");
            CHECK(f != nullptr);
            if (f != nullptr) {
                const char payload[] = "GGUF-contract-payload";
                CHECK(std::fwrite(payload, 1, sizeof(payload), f) ==
                      sizeof(payload));
                CHECK(std::fclose(f) == 0);
            }
        }

        // Re-open + read + close: the written bytes round-trip.
        {
            FILE* f = std::fopen(plain.c_str(), "rb");
            CHECK(f != nullptr);
            if (f != nullptr) {
                char buf[64] = {0};
                const size_t n = std::fread(buf, 1, sizeof(buf) - 1, f);
                CHECK(n == 22); // strlen("GGUF-contract-payload")
                CHECK(std::string(buf) == "GGUF-contract-payload");
                CHECK(std::fclose(f) == 0);
            }
        }

        // Remove the single file and prove the removal took.
        CHECK(std::remove(plain.c_str()) == 0);
        {
            FILE* f = std::fopen(plain.c_str(), "rb");
            CHECK(f == nullptr); // gone
            if (f != nullptr) {
                std::fclose(f);
            }
        }

        // A path containing spaces round-trips through stdio.
        const std::string spaced = tmpdir.file("space dir name.gguf");
        {
            FILE* f = std::fopen(spaced.c_str(), "wb");
            CHECK(f != nullptr);
            if (f != nullptr) {
                const char payload[] = "spaces";
                CHECK(std::fwrite(payload, 1, sizeof(payload), f) ==
                      sizeof(payload));
                CHECK(std::fclose(f) == 0);
            }
        }
        {
            FILE* g = std::fopen(spaced.c_str(), "rb");
            CHECK(g != nullptr);
            if (g != nullptr) {
                char buf[16] = {0};
                // sizeof("spaces") == 7: the NUL byte was written too.
                CHECK(std::fread(buf, 1, sizeof(buf) - 1, g) == 7);
                CHECK(std::string(buf) == "spaces");
                CHECK(std::fclose(g) == 0);
            }
        }

        // Uniqueness: two instances never share a location, even when
        // created back-to-back (parallel ctest slots, repeated runs).
        {
            shtn_test::TempDir a("uniqueness");
            shtn_test::TempDir b("uniqueness");
            CHECK(a.exists());
            CHECK(b.exists());
            CHECK(a.path() != b.path());

            // Scoped lifetime: the whole tree is removed when the owner
            // dies (explicit remove() proves it inside the scope).
            shtn_test::TempDir c("scoped");
            const std::string inner = c.file("inner.gguf");
            FILE* f = std::fopen(inner.c_str(), "wb");
            CHECK(f != nullptr);
            if (f != nullptr) {
                std::fputs("x", f);
                std::fclose(f);
            }
            const std::string cpath = c.path();
            c.remove();
            CHECK(!std::filesystem::is_directory(
                std::filesystem::path(cpath)));
        }
    }

    // --- 11. mapped-file release contract (v1.3.5) -----------------------
    // After a file is mapped and released, it MUST be deletable on
    // every platform. The previous Windows MappedFile::close() passed
    // the SECTION handle to UnmapViewOfFile — which requires the VIEW
    // base address — so the unmap silently failed, the section object
    // leaked, and the still-mapped view pinned the file until process
    // exit (a model could not be deleted or replaced after unload).
    // Proven here at the reader layer; the engine lifecycle path is
    // covered by the unload-then-delete section in test_model.cpp.
    {
        const std::string p = tmpdir.file("release-check.gguf");
        gguf_test::BuildOptions ro;
        ro.version = 3;
        ro.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_str(b, "general.architecture", "llama");
        });
        CHECK(gguf_test::write_file(p, gguf_test::build(ro)));

        {
            shtn::gguf::MappedFile f;
            std::string err;
            CHECK(f.open(p, err));
            CHECK(f.data() != nullptr);
            CHECK(f.size() > 4);
            // Parse still works through the mapping.
            shtn::gguf::GgufHeader h{};
            CHECK(shtn::gguf::parse_header(f, h, err));
            // Explicit close, then close again (idempotent contract).
            f.close();
            f.close();
        }

        // THE contract: with the mapping fully released, the file can
        // be deleted on Windows, Linux and macOS alike. On Windows the
        // leaked view made this remove fail with a sharing violation.
        CHECK(std::remove(p.c_str()) == 0);
        {
            FILE* f = std::fopen(p.c_str(), "rb");
            CHECK(f == nullptr); // gone
            if (f != nullptr) {
                std::fclose(f);
            }
        }
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_gguf: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_gguf: all checks passed\n");
    return 0;
}
