// test_tokenizer.cpp — Phase 4 tokenizer tests (dependency-free asserts).
//
// Builds a synthetic GGUF file with a small BPE vocab + merges, then
// verifies encode/decode round-trips, special token handling, BOS/EOS
// insertion, bounded output, and unsupported-model rejection.
//
// Uses the same gguf_writer.h fixture helper the other tests use, and
// the shared cross-platform temp-dir helper (temp_dir.h) — v1.3.5: no
// test may assume a POSIX temp location.

#include "shtn/engine.h"
#include "shtn/types.h"

#include "gguf_writer.h"
#include "temp_dir.h"
#include "tokenizer.h"

#include <cstdio>
#include <cstring>
#include <fstream>
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

// Build a tiny BPE GGUF model with a known vocab + merges.
//
// Vocab (8 tokens, llama-style BPE with U+2581 space marker):
//   0: "<unk>"   type=unknown
//   1: "<s>"     type=control (BOS)
//   2: "</s>"    type=control (EOS)
//   3: "▁"       type=normal (leading space)
//   4: "h"       type=normal
//   5: "e"       type=normal
//   6: "▁he"     type=normal (merge of ▁ + h + e)
//   7: "llo"     type=normal
//
// Merges (BPE rank order):
//   0: "▁ h"    → ▁h (not in vocab, but rank 0)
//   1: "▁h e"   → ▁he (in vocab)
//   2: "l l"    → ll
//   3: "ll o"   → llo (in vocab)
//
// bos_token_id=1, eos_token_id=2, unknown_token_id=0
struct TinyBPE {
    std::string path;
};

static TinyBPE make_tiny_bpe(const std::string& path) {
    TinyBPE m;
    m.path = path;

    gguf_test::BuildOptions o;
    o.version = 3;
    o.alignment = 32;

    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_str(b, "general.architecture", "llama");
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_str(b, "tokenizer.ggml.model", "llama");
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_string_array(b, "tokenizer.ggml.tokens",
            std::vector<std::string>{
                "<unk>", "<s>", "</s>",
                "\xE2\x96\x81",         // U+2581 (▁)
                "h", "e",
                "\xE2\x96\x81""he",     // ▁he
                "llo",
            });
    });
    // token_type: 1=normal, 2=unknown, 3=control
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        // Write a u32 array of int32 token_type values.
        // The kv_string_array helper writes strings; we need a typed
        // array. Build it inline.
        gguf_test::put_str(b, "tokenizer.ggml.token_type");
        gguf_test::put_u32(b, 9); // array
        gguf_test::put_u32(b, 5); // int32
        gguf_test::put_u64(b, 8); // count
        // types: unknown, control, control, normal, normal, normal, normal, normal
        uint32_t types[8] = {2, 3, 3, 1, 1, 1, 1, 1};
        for (uint32_t t : types) {
            gguf_test::put_u32(b, t);
        }
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_string_array(b, "tokenizer.ggml.merges",
            std::vector<std::string>{
                "\xE2\x96\x81 h",        // ▁ h
                "\xE2\x96\x81""h e",     // ▁h e
                "l l",
                "ll o",
            });
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "tokenizer.ggml.bos_token_id", 1);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "tokenizer.ggml.eos_token_id", 2);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "tokenizer.ggml.unknown_token_id", 0);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "llama.context_length", 64);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "llama.embedding_length", 8);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "llama.block_count", 1);
    });
    o.extra_kv.push_back([](std::vector<uint8_t>& b) {
        gguf_test::kv_u32(b, "llama.vocab_size", 8);
    });

    // One dummy tensor so the file isn't rejected as "no tensors".
    o.tensors.push_back([](std::vector<uint8_t>& b) {
        gguf_test::tensor_info(b, "token_embd.weight", {8, 8}, 0, 0);
    });
    o.data_bytes = 8 * 8 * 4; // F32 8x8

    auto image = gguf_test::build(o);
    gguf_test::write_file(path, image);
    return m;
}

int main() {
    // All synthetic fixtures live under one cross-platform temporary
    // directory (Windows/POSIX portable; removed on exit). v1.3.5 root
    // fix for run 35583009466: the previous getenv(TMPDIR) plus literal
    // /tmp fallback does not exist on Windows and the model load failed
    // before every assertion below cascaded.
    shtn_test::TempDir tmpdir("tokenizer");
    const std::string path = tmpdir.file("tiny-bpe.gguf");

    TinyBPE m = make_tiny_bpe(path);

    // --- create engine, load model, init tokenizer ----------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_model_load_options lopts{};
        int32_t rc = shtn_engine_load_model(engine, path.c_str(), &lopts);
        CHECK(rc == SHTN_OK);

        shtn_tokenizer_info ti{};
        rc = shtn_engine_tokenizer_init(engine, &ti);
        CHECK(rc == SHTN_OK);
        CHECK(ti.initialized == 1);
        CHECK(ti.vocab_size == 8);
        CHECK(ti.merge_count == 4);
        CHECK(ti.has_bos == 1);
        CHECK(ti.has_eos == 1);
        CHECK(ti.has_unknown == 1);
        CHECK(ti.bos_id == 1);
        CHECK(ti.eos_id == 2);
        CHECK(ti.unknown_id == 0);
        CHECK(std::strcmp(ti.model, "bpe") == 0);
        CHECK(std::strcmp(ti.model_name, "llama") == 0);

        // --- encode "hello" → should produce ▁he + llo (after merge) ---
        // Note: input "hello" has no leading space, but the BPE
        // preprocessor adds ▁ at the start for Llama-style models.
        // So "hello" → "▁hello" → split → [▁,h,e,l,l,o] → merge ▁+h+e=▁he,
        // l+l=ll, ll+o=llo → [▁he, llo] → ids [6, 7].
        {
            std::string text = "hello";
            std::vector<uint32_t> ids(64, 0);

            shtn_encode_options eopts{};
            eopts.add_bos = 0;
            eopts.add_eos = 0;
            eopts.max_tokens = 64;
            eopts.reserved = 0;

            shtn_encode_result result{};
            result.ids = ids.data();
            result.ids_count = 0;
            result.truncated = 0;

            rc = shtn_engine_tokenizer_encode(engine, text.c_str(),
                                              text.size(), &eopts, &result);
            CHECK(rc == SHTN_OK);
            CHECK(result.ids_count == 2);
            CHECK(ids[0] == 6); // ▁he
            CHECK(ids[1] == 7); // llo
        }

        // --- encode with BOS + EOS -------------------------------------
        {
            std::string text = "hello";
            std::vector<uint32_t> ids(64, 0);

            shtn_encode_options eopts{};
            eopts.add_bos = 1;
            eopts.add_eos = 1;
            eopts.max_tokens = 64;
            eopts.reserved = 0;

            shtn_encode_result result{};
            result.ids = ids.data();
            result.ids_count = 0;
            result.truncated = 0;

            rc = shtn_engine_tokenizer_encode(engine, text.c_str(),
                                              text.size(), &eopts, &result);
            CHECK(rc == SHTN_OK);
            CHECK(result.ids_count == 4);
            CHECK(ids[0] == 1); // BOS
            CHECK(ids[1] == 6); // ▁he
            CHECK(ids[2] == 7); // llo
            CHECK(ids[3] == 2); // EOS
        }

        // --- decode round-trip: [6, 7] → " hello" (with leading space) -
        // The ▁ marker converts back to a space on decode.
        {
            uint32_t ids_in[] = {6, 7};

            shtn_decode_options dopts{};
            dopts.skip_special = 1;
            dopts.max_bytes = 1024;
            dopts.reserved = 0;

            std::vector<char> text(1024, 0);
            shtn_decode_result result{};
            result.text = text.data();
            result.text_count = 0;
            result.truncated = 0;

            rc = shtn_engine_tokenizer_decode(engine, ids_in, 2, &dopts, &result);
            CHECK(rc == SHTN_OK);
            std::string decoded(result.text, result.text_count);
            CHECK(decoded == " hello");
        }

        // --- decode with BOS/EOS skipped (skip_special=true) -----------
        {
            uint32_t ids_in[] = {1, 6, 7, 2};

            shtn_decode_options dopts{};
            dopts.skip_special = 1;
            dopts.max_bytes = 1024;
            dopts.reserved = 0;

            std::vector<char> text(1024, 0);
            shtn_decode_result result{};
            result.text = text.data();
            result.text_count = 0;
            result.truncated = 0;

            rc = shtn_engine_tokenizer_decode(engine, ids_in, 4, &dopts, &result);
            CHECK(rc == SHTN_OK);
            std::string decoded(result.text, result.text_count);
            CHECK(decoded == " hello"); // BOS/EOS skipped
        }

        // --- bounded output: max_tokens=1 truncates --------------------
        {
            std::string text = "hello";
            std::vector<uint32_t> ids(64, 0);

            shtn_encode_options eopts{};
            eopts.add_bos = 0;
            eopts.add_eos = 0;
            eopts.max_tokens = 1;
            eopts.reserved = 0;

            shtn_encode_result result{};
            result.ids = ids.data();
            result.ids_count = 0;
            result.truncated = 0;

            rc = shtn_engine_tokenizer_encode(engine, text.c_str(),
                                              text.size(), &eopts, &result);
            CHECK(rc == SHTN_OK);
            CHECK(result.ids_count == 1);
            CHECK(result.truncated == 1);
        }

        // --- decode with max_bytes too small truncates -----------------
        {
            uint32_t ids_in[] = {6, 7};

            shtn_decode_options dopts{};
            dopts.skip_special = 1;
            dopts.max_bytes = 4; // " hello" is 6 chars
            dopts.reserved = 0;

            std::vector<char> text(4, 0);
            shtn_decode_result result{};
            result.text = text.data();
            result.text_count = 0;
            result.truncated = 0;

            rc = shtn_engine_tokenizer_decode(engine, ids_in, 2, &dopts, &result);
            CHECK(rc == SHTN_OK);
            CHECK(result.truncated == 1);
            CHECK(result.text_count == 3); // cap - 1 for NUL
        }

        // --- idempotent init -------------------------------------------
        {
            shtn_tokenizer_info ti{};
            rc = shtn_engine_tokenizer_init(engine, &ti);
            CHECK(rc == SHTN_OK);
            CHECK(ti.initialized == 1);
        }

        // --- info without re-init reports initialized ------------------
        {
            shtn_tokenizer_info ti{};
            rc = shtn_engine_tokenizer_info(engine, &ti);
            CHECK(rc == SHTN_OK);
            CHECK(ti.initialized == 1);
        }

        shtn_engine_destroy(engine);
    }

    // --- unsupported tokenizer model kind -------------------------------
    {
        // Build a GGUF with an unsupported tokenizer.ggml.model.
        std::string upath = tmpdir.file("unsupported.gguf");
        {
            gguf_test::BuildOptions o;
            o.version = 3;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "general.architecture", "llama");
            });
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "tokenizer.ggml.model", "rwkv-world");
            });
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_string_array(b, "tokenizer.ggml.tokens",
                    std::vector<std::string>{"a", "b"});
            });
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.context_length", 64);
            });
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.embedding_length", 8);
            });
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.block_count", 1);
            });
            o.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "token_embd.weight", {8, 8}, 0, 0);
            });
            o.data_bytes = 8 * 8 * 4;
            auto image = gguf_test::build(o);
            gguf_test::write_file(upath, image);
        }

        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_model_load_options lopts{};
        CHECK(shtn_engine_load_model(engine, upath.c_str(), &lopts) == SHTN_OK);

        shtn_tokenizer_info ti{};
        int32_t rc = shtn_engine_tokenizer_init(engine, &ti);
        CHECK(rc == SHTN_ERR_UNSUPPORTED);
        CHECK(ti.initialized == 0);

        shtn_engine_destroy(engine);
    }

    // --- encode/decode before tokenizer init returns NO_MODEL -----------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_encode_options eopts{};
        eopts.max_tokens = 16;
        eopts.reserved = 0;
        std::vector<uint32_t> ids(16, 0);
        shtn_encode_result result{};
        result.ids = ids.data();

        std::string text = "x";
        int32_t rc = shtn_engine_tokenizer_encode(engine, text.c_str(),
                                                  text.size(), &eopts, &result);
        CHECK(rc == SHTN_ERR_NO_MODEL);

        shtn_engine_destroy(engine);
    }

    // --- NULL-argument rejection ----------------------------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        CHECK(shtn_engine_tokenizer_init(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_tokenizer_init(engine, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_tokenizer_info(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_tokenizer_encode(nullptr, nullptr, 0, nullptr, nullptr) == SHTN_ERR_INVALID_ARG);

        shtn_engine_destroy(engine);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_tokenizer: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_tokenizer: all checks passed\n");
    return 0;
}
