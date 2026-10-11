// test_engine.cpp — C ABI contract tests (dependency-free asserts).

#include "shtn/engine.h"

#include "temp_dir.h"

#include <cmath>
#include <cstdio>
#include <cstring>

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
    // Cross-platform temp location (v1.3.5): the nonexistent-path
    // fixture below is derived from it instead of a POSIX-style
    // "/definitely/not/here.gguf" literal.
    shtn_test::TempDir tmpdir("engine");

    // ABI version sanity: must match the version the Go core pins.
    CHECK(shtn_abi_version() == SHTN_ABI_VERSION);
    CHECK(shtn_abi_version() == 4u); // Phase 5 (native generation added; v3 was Phase 4)

    // --- create/destroy round trip --------------------------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;
        opts.reserved = 0;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);
        CHECK(engine != nullptr);
        shtn_engine_destroy(engine);
    }

    // --- NULL-argument rejection (never dereferenced) --------------------
    {
        CHECK(shtn_engine_create(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);

        shtn_engine* created = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;

        CHECK(shtn_engine_create(&opts, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_create(nullptr, &created) == SHTN_ERR_INVALID_ARG);
        CHECK(created == nullptr);

        // A valid instance for the (engine, NULL-out) pairs below.
        CHECK(shtn_engine_create(&opts, &created) == SHTN_OK);

        shtn_engine_destroy(nullptr); // must be a safe no-op

        CHECK(shtn_engine_health(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_hardware_info(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_metrics(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);

        // Phase 2 model surface: NULL arguments rejected, never dereferenced.
        CHECK(shtn_engine_load_model(nullptr, "x", nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_load_model(created, nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_unload_model(nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_model_info(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_model_info(created, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_memory_plan(nullptr, nullptr) == SHTN_ERR_INVALID_ARG);
        CHECK(shtn_engine_memory_plan(created, nullptr) == SHTN_ERR_INVALID_ARG);

        shtn_engine_destroy(created);
    }

    // --- ABI mismatch fails closed ---------------------------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION + 1u; // wrong on purpose
        opts.reserved = 0;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_ERR_ABI);
        CHECK(engine == nullptr);
    }

    // --- health -----------------------------------------------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_health_status health{};
        CHECK(shtn_engine_health(engine, &health) == SHTN_OK);
        CHECK(health.healthy == 1);
        CHECK(std::strcmp(health.state, "ready") == 0);

        shtn_engine_destroy(engine);
    }

    // --- hardware info: detected values only ------------------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_hardware_info hw{};
        CHECK(shtn_engine_hardware_info(engine, &hw) == SHTN_OK);

        // Architecture is compile-time knowledge: always present.
        CHECK(hw.architecture[0] != '\0');

        // Logical cores are detectable on every supported platform.
        CHECK(hw.cpu.logical_cores > 0);

        // Total RAM is detectable on every supported platform.
        CHECK(hw.ram.total_bytes > 0);

        // Phase 1: no C++ GPU/accelerator detection — counts must be
        // exactly zero (represented, not invented).
        CHECK(hw.gpu_count == 0);
        CHECK(hw.accelerator_count == 0);

        // The detection source must be recorded.
        CHECK(hw.detected_by[0] != '\0');

        shtn_engine_destroy(engine);
    }

    // --- metrics: measured values only ------------------------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_metrics m{};
        CHECK(shtn_engine_metrics(engine, &m) == SHTN_OK);

        CHECK(m.uptime_seconds >= 0.0);
        CHECK(m.process_rss_bytes > 0);     // RSS is really measured
        CHECK(m.active_requests == 0);      // no generation exists
        CHECK(std::strcmp(m.state, "ready") == 0);

        // Repeated reads must be monotonically non-decreasing in uptime.
        shtn_metrics m2{};
        CHECK(shtn_engine_metrics(engine, &m2) == SHTN_OK);
        CHECK(m2.uptime_seconds >= m.uptime_seconds);

        shtn_engine_destroy(engine);
    }

    // --- model surface: fresh engine reports unloaded -----------------------
    {
        shtn_engine* engine = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;

        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);

        shtn_model_info mi{};
        CHECK(shtn_engine_model_info(engine, &mi) == SHTN_OK);
        CHECK(std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) == 0);

        shtn_memory_plan mp{};
        CHECK(shtn_engine_memory_plan(engine, &mp) == SHTN_OK);
        CHECK(mp.total_bytes == 0);

        // Loading a nonexistent file fails cleanly without crashing.
        // The path is built under the portable temp dir (it is never
        // created), so the fixture works on every platform.
        CHECK(shtn_engine_load_model(
                  engine, tmpdir.file("definitely-not-here.gguf").c_str(),
                  nullptr) == SHTN_ERR_INVALID_ARG);

        shtn_engine_destroy(engine);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_engine: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_engine: all checks passed\n");
    return 0;
}
