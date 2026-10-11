// test_model.cpp — native model lifecycle tests (Phase 2).
//
// Covers: load/unload/reload, replace semantics, failed-load recovery,
// metadata + memory-plan correctness, context override, NULL/invalid
// argument rejection, and concurrent safe inspection.

#include "shtn/engine.h"
#include "gguf_writer.h"
#include "model.h" // src/ is on the engine target's include path (tests)
#include "temp_dir.h"

#include <cstdio>
#include <cstring>
#include <string>
#include <thread>
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

shtn_engine* new_engine() {
    shtn_engine* engine = nullptr;
    shtn_engine_options opts{};
    opts.abi_version = SHTN_ABI_VERSION;
    opts.reserved = 0;

    if (shtn_engine_create(&opts, &engine) != SHTN_OK || engine == nullptr) {
        std::fprintf(stderr, "FAIL: engine create\n");
        std::exit(1);
    }
    return engine;
}

} // namespace

int main() {
    // Shared cross-platform temp-dir helper (v1.3.5 consolidation:
    // replaces this test's private GetTempPathA/TMPDIR copy; the tree
    // is removed on exit).
    shtn_test::TempDir tmpdir("model");
    const std::string dir = tmpdir.path();

    const auto tiny = gguf_test::make_tiny_model(dir + "/tiny.gguf");
    CHECK(gguf_test::write_file(tiny.path, tiny.image));

    // A second, different valid model (different context length).
    {
        auto o = gguf_test::BuildOptions{};
        o.version = 3;
        o.alignment = 32;
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_str(b, "general.architecture", "qwen2");
        });
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_u32(b, "qwen2.context_length", 512);
        });
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_u32(b, "qwen2.embedding_length", 32);
        });
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_u32(b, "qwen2.block_count", 1);
        });
        o.extra_kv.push_back([](std::vector<uint8_t>& b) {
            gguf_test::kv_u32(b, "qwen2.vocab_size", 64);
        });
        o.tensors.push_back([](std::vector<uint8_t>& b) {
            gguf_test::tensor_info(b, "t0", {32, 64}, 0, 0);
        });
        o.data_bytes = 32ull * 64ull * 4ull;
        gguf_test::write_file(dir + "/second.gguf", gguf_test::build(o));
    }

    // A garbage file (valid size, invalid content).
    {
        std::vector<uint8_t> garbage(512, 0x13);
        gguf_test::write_file(dir + "/garbage.gguf", garbage);
    }

    // --- fresh engine: unloaded state -------------------------------------
    {
        shtn_engine* e = new_engine();

        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);
        CHECK(mi.file_size_bytes == 0);
        CHECK(mi.tensor_count == 0);
        CHECK(mi.architecture[0] == '\0');

        shtn_memory_plan mp{};
        CHECK(shtn_engine_memory_plan(e, &mp) == SHTN_OK);
        CHECK(mp.total_bytes == 0);
        CHECK(mp.weights_bytes == 0);

        shtn_engine_destroy(e);
    }

    // --- NULL / invalid argument rejection ----------------------------------
    {
        shtn_engine* e = new_engine();

        shtn_model_info mi{};
        shtn_memory_plan mp{};

        CHECK(shtn_engine_load_model(nullptr, "x", nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_load_model(e, nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_unload_model(nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_model_info(nullptr, &mi) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_model_info(e, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_memory_plan(nullptr, &mp) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_memory_plan(e, nullptr) == SHTN_ERR_INVALID_ARG);

        // Empty path: caller-argument error → state unchanged (still
        // unloaded — nothing was attempted).
        CHECK(shtn_engine_load_model(e, "", nullptr) == SHTN_ERR_INVALID_ARG);

        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);

        // Reserved options field must be 0 (also a caller-argument error).
        shtn_model_load_options bad{};
        bad.context_length = 0;
        bad.reserved = 7;
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), &bad) ==
              SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);

        // A real attempt on a missing file is a failed load.
        CHECK(shtn_engine_load_model(e, (dir + "/missing.gguf").c_str(),
                                     nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_FAILED) == 0);

        shtn_engine_destroy(e);
    }

    // --- basic load: metadata + plan ----------------------------------------
    {
        shtn_engine* e = new_engine();

        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);

        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);

        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0);
        CHECK(std::strcmp(mi.architecture, "llama") == 0);
        CHECK(std::strcmp(mi.name, "tiny-test-model") == 0);
        CHECK(std::strcmp(mi.quantization, "F16") == 0);
        CHECK(mi.gguf_version == 3);
        CHECK(mi.has_file_type == 1);
        CHECK(mi.general_file_type == 1);
        CHECK(mi.tensor_count == 3);
        CHECK(mi.context_length == 256);
        CHECK(mi.vocabulary_size == 96);
        CHECK(mi.embedding_length == 64);
        CHECK(mi.layer_count == 2);
        CHECK(mi.parameter_count == tiny.parameter_count());
        CHECK(mi.file_size_bytes == static_cast<uint64_t>(tiny.image.size()));
        CHECK(mi.error[0] == '\0');

        shtn_memory_plan mp{};
        CHECK(shtn_engine_memory_plan(e, &mp) == SHTN_OK);

        CHECK(mp.model_file_bytes == static_cast<uint64_t>(tiny.image.size()));
        CHECK(mp.mapped_bytes == mp.model_file_bytes);

        // Weights span: the file's data section exactly (the writer pads
        // the header to alignment 32 and appends data_bytes tensor bytes).
        const uint64_t expected_weights =
            64ull * 96ull * 4ull + 96ull * 64ull * 4ull + 64ull * 2ull;
        CHECK(mp.weights_bytes == expected_weights);
        CHECK(mp.runtime_overhead_bytes == shtn::model::kRuntimeOverheadBytes);

        // KV estimate: 2 * layers(2) * ctx(256) * emb(64) * 2 = 131072.
        CHECK(mp.kv_cache_bytes == 2ull * 2ull * 256ull * 64ull * 2ull);

        // Workspace estimate: ctx * vocab * 4 = 256*96*4 = 98304.
        CHECK(mp.workspace_bytes == 256ull * 96ull * 4ull);

        CHECK(mp.total_bytes ==
              mp.model_file_bytes + mp.kv_cache_bytes + mp.workspace_bytes +
                  mp.runtime_overhead_bytes);

        // fits_in_ram is honest: 1/0 with RAM detected, -1 otherwise.
        CHECK(mp.fits_in_ram == -1 || mp.fits_in_ram == 0 || mp.fits_in_ram == 1);

        // Health mentions the model but the engine stays healthy.
        shtn_health_status health{};
        CHECK(shtn_engine_health(e, &health) == SHTN_OK);
        CHECK(health.healthy == 1);
        CHECK(std::strstr(health.detail, "model loaded") != nullptr);

        shtn_engine_destroy(e);
    }

    // --- context override changes the plan -----------------------------------
    {
        shtn_engine* e = new_engine();

        shtn_model_load_options opts{};
        opts.context_length = 1024;
        opts.reserved = 0;

        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), &opts) == SHTN_OK);

        shtn_memory_plan mp{};
        CHECK(shtn_engine_memory_plan(e, &mp) == SHTN_OK);
        CHECK(mp.kv_cache_bytes == 2ull * 2ull * 1024ull * 64ull * 2ull);
        CHECK(mp.workspace_bytes == 1024ull * 96ull * 4ull);

        shtn_engine_destroy(e);
    }

    // --- replace semantics: load A, load A again, load B ---------------------
    {
        shtn_engine* e = new_engine();

        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);
        // Load A again (replace with the same file).
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);

        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0);
        CHECK(std::strcmp(mi.architecture, "llama") == 0);

        // Load B (replace with a different model).
        CHECK(shtn_engine_load_model(e, (dir + "/second.gguf").c_str(),
                                     nullptr) == SHTN_OK);

        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0);
        CHECK(std::strcmp(mi.architecture, "qwen2") == 0);
        CHECK(mi.context_length == 512);
        CHECK(mi.tensor_count == 1);

        shtn_engine_destroy(e);
    }

    // --- unload / reload / idempotent unload ----------------------------------
    {
        shtn_engine* e = new_engine();

        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);
        CHECK(shtn_engine_unload_model(e) == SHTN_OK);

        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);
        CHECK(mi.architecture[0] == '\0');
        CHECK(mi.file_size_bytes == 0);

        // Idempotent.
        CHECK(shtn_engine_unload_model(e) == SHTN_OK);

        // Reload works.
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0);

        shtn_engine_destroy(e);
    }

    // --- failed load: clean failure + recovery --------------------------------
    {
        shtn_engine* e = new_engine();

        // Garbage file → format error.
        const int32_t rc = shtn_engine_load_model(
            e, (dir + "/garbage.gguf").c_str(), nullptr);
        CHECK(rc == SHTN_ERR_MODEL_FORMAT);

        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_FAILED) == 0);
        CHECK(mi.error[0] != '\0');
        CHECK(mi.architecture[0] == '\0'); // nothing cached from the failure
        CHECK(mi.parameter_count == 0);

        // Zero-tensor GGUF → not a model.
        {
            auto o = gguf_test::BuildOptions{};
            o.version = 3;
            o.alignment = 32;
            o.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "general.architecture", "llama");
            });
            o.data_bytes = 0;
            gguf_test::write_file(dir + "/no-tensors.gguf", gguf_test::build(o));
        }
        CHECK(shtn_engine_load_model(e, (dir + "/no-tensors.gguf").c_str(),
                                     nullptr) == SHTN_ERR_MODEL_FORMAT);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_FAILED) == 0);

        // Unload clears the failure.
        CHECK(shtn_engine_unload_model(e) == SHTN_OK);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);
        CHECK(mi.error[0] == '\0');

        // Recovery: a failed engine loads fine afterwards.
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0);

        // Replace with garbage: the previous model is NOT kept.
        CHECK(shtn_engine_load_model(e, (dir + "/garbage.gguf").c_str(),
                                     nullptr) == SHTN_ERR_MODEL_FORMAT);
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_FAILED) == 0);
        CHECK(mi.architecture[0] == '\0');

        // Health stays healthy across model failures.
        shtn_health_status health{};
        CHECK(shtn_engine_health(e, &health) == SHTN_OK);
        CHECK(health.healthy == 1);
        CHECK(std::strstr(health.detail, "failed") != nullptr);

        shtn_engine_destroy(e);
    }

    // --- engine destroy releases a loaded model --------------------------------
    {
        shtn_engine* e = new_engine();
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);
        shtn_engine_destroy(e); // must not leak or crash
    }

    // --- concurrent safe inspection ---------------------------------------------
    {
        shtn_engine* e = new_engine();
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) == SHTN_OK);

        std::vector<std::thread> workers;
        for (int i = 0; i < 4; ++i) {
            workers.emplace_back([&e]() {
                for (int round = 0; round < 50; ++round) {
                    shtn_model_info mi{};
                    shtn_memory_plan mp{};
                    shtn_health_status health{};

                    shtn_engine_model_info(e, &mi);
                    shtn_engine_memory_plan(e, &mp);
                    shtn_engine_health(e, &health);
                }
            });
        }

        // A loader thread exercises load/unload while inspectors run.
        std::thread churn([&e, &dir, &tiny]() {
            for (int i = 0; i < 20; ++i) {
                shtn_engine_load_model(e, tiny.path.c_str(), nullptr);
                shtn_engine_unload_model(e);
                shtn_engine_load_model(e, (dir + "/second.gguf").c_str(),
                                       nullptr);
            }
            shtn_engine_unload_model(e);
        });

        for (auto& w : workers) {
            w.join();
        }
        churn.join();

        // Final state is consistent.
        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);

        shtn_engine_destroy(e);
    }

    // --- unload releases the file mapping: the model file is deletable ---
    // v1.3.5 regression guard for the Windows MappedFile defect: close()
    // passed the SECTION handle to UnmapViewOfFile (which requires the
    // VIEW base address), so the unmap silently failed, the section
    // leaked, and the still-mapped view pinned the model file until
    // process exit — on Windows the file could not be deleted or
    // replaced after unload (a locked .gguf is a user-facing defect:
    // the host process outlives the unload). The engine-level contract:
    // after load + unload, the file must be removable on EVERY platform.
    {
        // A dedicated file (not tiny.gguf) so its deletion proves the
        // release rather than collateral cleanup.
        const std::string victim = tmpdir.file("unload-release.gguf");
        CHECK(gguf_test::write_file(victim, tiny.image));

        shtn_engine* e = new_engine();
        CHECK(shtn_engine_load_model(e, victim.c_str(), nullptr) == SHTN_OK);
        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(e, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0);
        CHECK(shtn_engine_unload_model(e) == SHTN_OK);

        // THE contract: the mapping is gone, so the file deletes.
        CHECK(std::remove(victim.c_str()) == 0);

        // And the engine remains fully reusable afterwards.
        CHECK(shtn_engine_load_model(e, tiny.path.c_str(), nullptr) ==
              SHTN_OK);
        CHECK(shtn_engine_unload_model(e) == SHTN_OK);
        shtn_engine_destroy(e);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_model: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_model: all checks passed\n");
    return 0;
}
