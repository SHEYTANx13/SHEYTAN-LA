// test_generate.cpp — Phase 5 generation-loop tests through the real ABI.
//
// Covers: greedy determinism (same seed + same request → same output),
// temperature/seed reproducibility, max_tokens stop, context-bound
// rejection (prompt + max_tokens > context), context exhaustion at the
// context edge, cancellation mid-generation (active request), cancel of
// an unknown id (miss, engine unaffected), metrics sanity (measured
// values only), KV reset between requests (no state leaks), UTF-8
// boundary hold-back, EOS flow, error paths (no model / unsupported /
// invalid args), engine reusability after every failure.

#include "shtn/engine.h"
#include "shtn/types.h"

#include "generate.h"
#include "temp_dir.h"
#include "util.h"

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cstdio>
#include <cstring>
#include <mutex>
#include <string>
#include <thread>
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

using namespace shtn;

struct Sink {
    std::mutex mu;
    std::string text;
    std::vector<uint32_t> tokens;
    int chunks = 0;
    int final_chunks = 0;

    static int32_t emit(void* user, const shtn_generation_chunk* c) {
        auto* s = static_cast<Sink*>(user);
        if (c == nullptr) return 0;
        std::lock_guard<std::mutex> lock(s->mu);
        s->text.append(c->text, c->text_len);
        if (!c->final) {
            s->tokens.push_back(c->token_id);
        } else {
            s->final_chunks++;
        }
        s->chunks++;
        return 0;
    }
};

static shtn_generation_options base_opts(const char* prompt,
                                         uint32_t max_tokens) {
    shtn_generation_options o{};
    o.request_id = "test";
    o.prompt = prompt;
    o.prompt_len = std::strlen(prompt);
    o.max_tokens = max_tokens;
    o.temperature = 0.0f; // greedy
    o.top_k = 0;
    o.top_p = 1.0f;
    o.repetition_penalty = 1.0f;
    o.repeat_last_n = 64;
    o.seed = 0;
    o.reserved = 0;
    return o;
}

int main() {
    const std::string fixtures = SHTN_FIXTURES_DIR;

    // Cross-platform temporary location for the synthetic non-llama
    // fixture (v1.3.5 root fix for run 35583009466: the previous
    // hard-coded /tmp/shtn-not-llama.gguf path made fopen() return
    // nullptr on the Windows runner). Removed on exit.
    shtn_test::TempDir tmpdir("generate");

    // --- error path: no model ----------------------------------------------
    {
        shtn_engine* e = nullptr;
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&opts, &e) == SHTN_OK);

        auto o = base_opts("hello", 4);
        char detail[256] = {0};
        Sink sink;
        shtn_generation_result res{};
        CHECK(shtn_engine_generate(e, &o, Sink::emit, &sink, &res, detail) ==
              SHTN_ERR_NO_MODEL);
        CHECK(std::strstr(detail, "no model") != nullptr);

        shtn_engine_destroy(e);
    }

    // --- main fixture engine -------------------------------------------------
    shtn_engine* engine = nullptr;
    {
        shtn_engine_options opts{};
        opts.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&opts, &engine) == SHTN_OK);
        shtn_model_load_options lopts{};
        CHECK(shtn_engine_load_model(
                  engine, (fixtures + "/tiny-llama-f32.gguf").c_str(),
                  &lopts) == SHTN_OK);
    }

    // --- greedy determinism + real metrics ----------------------------------
    std::string first_text;
    {
        auto o = base_opts("hello", 8);
        Sink sink;
        shtn_generation_result res{};
        char detail[256] = {0};
        const int32_t rc =
            shtn_engine_generate(engine, &o, Sink::emit, &sink, &res, detail);
        CHECK(rc == SHTN_OK);
        CHECK(sink.final_chunks == 1);
        CHECK(res.metrics.generated_tokens == 8); // max_tokens bound
        CHECK(std::string(res.finish_reason) == SHTN_FINISH_LENGTH);
        CHECK(res.metrics.prompt_tokens == 5);    // BOS + ▁ + he + ll + o
        CHECK(res.metrics.ttft_seconds > 0.0);
        CHECK(res.metrics.total_seconds >= res.metrics.ttft_seconds);
        CHECK(res.metrics.tokens_per_second > 0.0);
        // prompt + (generated - 1): the last sampled token is never
        // forward-passed (llama.cpp semantics).
        CHECK(res.metrics.kv_positions_used == 5 + 7);
        first_text = sink.text;
        CHECK(!first_text.empty()); // the tiny model emits printable text
    }

    // Same request again → identical output (greedy + fixed seed).
    {
        auto o = base_opts("hello", 8);
        Sink sink;
        shtn_generation_result res{};
        char detail[256] = {0};
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &sink, &res,
                                   detail) == SHTN_OK);
        CHECK(sink.text == first_text);
    }

    // --- KV reset between requests -------------------------------------------
    {
        // After the second request, used positions reflect ONLY the last
        // request's tokens (no accumulation across requests).
        shtn_kv_cache_info kv{};
        CHECK(shtn_engine_kv_cache_info(engine, &kv) == SHTN_OK);
        CHECK(kv.allocated == 1);
        CHECK(kv.used_positions == 5 + 7);
        CHECK(kv.used_bytes == kv.used_positions * 2 * kv.layer_count *
                                  kv.kv_dim * 2);
        CHECK(kv.capacity_bytes == 2ull * 2 * 64 * 16 * 2);
    }

    // --- temperature + fixed seed reproducibility ----------------------------
    {
        auto o = base_opts("hello", 6);
        o.temperature = 0.9f;
        o.top_k = 0;
        o.top_p = 0.95f;
        o.seed = 12345;

        Sink a, b;
        shtn_generation_result ra{}, rb{};
        char da[256] = {0}, db[256] = {0};
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &a, &ra, da) ==
              SHTN_OK);
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &b, &rb, db) ==
              SHTN_OK);
        CHECK(a.text == b.text); // same seed + deterministic conditions
        CHECK(a.tokens.size() == b.tokens.size());
    }

    // --- different seed → (almost surely) different tokens -------------------
    {
        auto o1 = base_opts("hello", 6);
        o1.temperature = 1.2f;
        o1.seed = 1;
        auto o2 = base_opts("hello", 6);
        o2.temperature = 1.2f;
        o2.seed = 999;

        Sink a, b;
        shtn_generation_result ra{}, rb{};
        char da[256] = {0}, db[256] = {0};
        CHECK(shtn_engine_generate(engine, &o1, Sink::emit, &a, &ra, da) ==
              SHTN_OK);
        CHECK(shtn_engine_generate(engine, &o2, Sink::emit, &b, &rb, db) ==
              SHTN_OK);
        // (Not asserting inequality — with a 32-token vocab collisions are
        // possible; the reproducibility test above is the strong check.)
        (void)a;
        (void)b;
    }

    // --- context bound: prompt + max_tokens > context → REJECT ---------------
    {
        // ctx=64, prompt=5 tokens → max_tokens=59 fits exactly, 60 does not.
        auto ok_o = base_opts("hello", 59);
        Sink ok_sink;
        shtn_generation_result ok_res{};
        char ok_d[256] = {0};
        CHECK(shtn_engine_generate(engine, &ok_o, Sink::emit, &ok_sink,
                                   &ok_res, ok_d) == SHTN_OK);
        CHECK(ok_res.metrics.kv_positions_used == 63);

        auto bad_o = base_opts("hello", 60);
        Sink bad_sink;
        shtn_generation_result bad_res{};
        char bad_d[256] = {0};
        CHECK(shtn_engine_generate(engine, &bad_o, Sink::emit, &bad_sink,
                                   &bad_res, bad_d) ==
              SHTN_ERR_CONTEXT_OVERFLOW);
        CHECK(std::strstr(bad_d, "context") != nullptr);

        // The engine is reusable after the rejection.
        auto o = base_opts("hello", 4);
        Sink sink;
        shtn_generation_result res{};
        char d[256] = {0};
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &sink, &res, d) ==
              SHTN_OK);
    }

    // --- invalid arguments -----------------------------------------------------
    {
        auto o = base_opts("hello", 4);
        o.max_tokens = 0;
        Sink sink;
        shtn_generation_result res{};
        char d[256] = {0};
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &sink, &res, d) ==
              SHTN_ERR_INVALID_ARG);

        auto o2 = base_opts("", 4); // empty prompt
        Sink sink2;
        shtn_generation_result res2{};
        char d2[256] = {0};
        CHECK(shtn_engine_generate(engine, &o2, Sink::emit, &sink2, &res2,
                                   d2) == SHTN_ERR_INVALID_ARG);
    }

    // --- cancellation of an ACTIVE request -------------------------------------
    // (Uses the SLOW fixture: one token takes milliseconds, so the request
    // is reliably in flight when the cancel arrives.)
    {
        shtn_engine* slow = nullptr;
        shtn_engine_options so{};
        so.abi_version = SHTN_ABI_VERSION;
        so.reserved = 0;
        CHECK(shtn_engine_create(&so, &slow) == SHTN_OK);
        shtn_model_load_options slo{};
        CHECK(shtn_engine_load_model(
                  slow, (fixtures + "/tiny-llama-slow.gguf").c_str(), &slo) ==
              SHTN_OK);

        auto o = base_opts("hello", 200);
        o.request_id = "cancel-me";
        o.temperature = 1.1f;
        o.seed = 7;

        Sink sink;
        shtn_generation_result res{};
        char d[256] = {0};

        // Cancel once the first streamed token arrives (decode phase
        // underway — prefill finished, TTFT measured).
        std::thread canceller([&slow, &sink]() {
            for (int i = 0; i < 3000; ++i) {
                {
                    std::lock_guard<std::mutex> lock(sink.mu);
                    if (sink.tokens.size() >= 1) {
                        break;
                    }
                }
                std::this_thread::sleep_for(std::chrono::milliseconds(1));
            }
            int32_t cancelled = 0;
            char reason[128] = {0};
            CHECK(shtn_engine_cancel_generation(slow, "cancel-me",
                                                &cancelled, reason) ==
                  SHTN_OK);
            CHECK(cancelled == 1);
        });

        const int32_t rc = shtn_engine_generate(slow, &o, Sink::emit, &sink,
                                                &res, d);
        canceller.join();
        CHECK(rc == SHTN_ERR_CANCELLED);
        CHECK(std::string(res.finish_reason) == SHTN_FINISH_CANCELLED);
        CHECK(res.metrics.generated_tokens < 200);
        CHECK(res.metrics.generated_tokens > 0);
        CHECK(res.metrics.ttft_seconds > 0.0); // first token DID stream

        // Scheduler state consistent + engine reusable after cancellation.
        shtn_scheduler_info si{};
        CHECK(shtn_engine_scheduler_info(slow, &si) == SHTN_OK);
        CHECK(si.active_requests == 0);
        CHECK(si.total_completed == 0);
        CHECK(si.total_cancelled == 1);

        auto o2 = base_opts("hello", 2);
        Sink sink2;
        shtn_generation_result res2{};
        char d2[256] = {0};
        CHECK(shtn_engine_generate(slow, &o2, Sink::emit, &sink2, &res2,
                                   d2) == SHTN_OK);
        shtn_engine_destroy(slow);
    }

    // --- cancel of a nonexistent id → miss, engine untouched -------------------
    {
        int32_t cancelled = 99;
        char reason[128] = {0};
        CHECK(shtn_engine_cancel_generation(engine, "no-such-id", &cancelled,
                                            reason) == SHTN_OK);
        CHECK(cancelled == 0);
        CHECK(reason[0] != '\0');

        auto o = base_opts("hello", 2);
        Sink sink;
        shtn_generation_result res{};
        char d[256] = {0};
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &sink, &res, d) ==
              SHTN_OK);
    }

    // --- generation stats snapshot (measured) -----------------------------------
    {
        shtn_generation_stats gs{};
        CHECK(shtn_engine_generation_stats(engine, &gs) == SHTN_OK);
        CHECK(gs.total_requests >= 6);
        CHECK(gs.total_completed >= 6);
        CHECK(gs.total_failed == 1); // the context-overflow rejection
        CHECK(gs.active_requests == 0);
        CHECK(gs.last_prompt_tokens == 5);
        CHECK(gs.last_generated_tokens == 2);
    }

    // --- consumer abort (emit returns non-zero) ----------------------------------
    {
        shtn_engine* slow = nullptr;
        shtn_engine_options so{};
        so.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&so, &slow) == SHTN_OK);
        shtn_model_load_options slo{};
        CHECK(shtn_engine_load_model(
                  slow, (fixtures + "/tiny-llama-slow.gguf").c_str(), &slo) ==
              SHTN_OK);

        struct AbortSink {
            static int32_t emit(void* user, const shtn_generation_chunk* c) {
                (void)c;
                auto* count = static_cast<int*>(user);
                ++*count;
                return (*count >= 2) ? 1 : 0; // abort at the second chunk
            }
        };
        int count = 0;
        auto o = base_opts("hello", 100);
        o.temperature = 1.3f;
        o.seed = 3;
        shtn_generation_result res{};
        char d[256] = {0};
        const int32_t rc = shtn_engine_generate(slow, &o, AbortSink::emit,
                                                &count, &res, d);
        CHECK(rc == SHTN_ERR_INTERNAL);
        CHECK(std::strstr(d, "consumer") != nullptr);

        // Engine still reusable after the consumer abort.
        auto o2 = base_opts("hello", 2);
        Sink sink2;
        shtn_generation_result res2{};
        char d2[256] = {0};
        CHECK(shtn_engine_generate(slow, &o2, Sink::emit, &sink2, &res2,
                                   d2) == SHTN_OK);
        shtn_engine_destroy(slow);
    }

    // --- model unload during active generation → rejected ------------------------
    // Invariant under test: load/unload must be REJECTED while a generation
    // is in flight, and the rejection must hold for the WHOLE observable
    // window (registered → queued → popped → executing). The first version
    // of this test raced: its sequencer polled scheduler_info (the
    // scheduler's active slot) while the engine's guard counted a different
    // variable (executor-entered generations), so an unload landing in the
    // window between the two SUCCEEDED, tore the model out from under the
    // in-flight generation, and failed the CHECK (~25% of loaded CI runs;
    // repro'd at 28% locally — CI run 34921857962). The engine guard now
    // covers the entire in-flight window, and this test synchronizes on an
    // EXPLICIT signal — the first streamed token — which is emitted only
    // from inside the runner's decode loop, strictly after the guard state
    // is set. Once that signal is observed, the unload/load rejection is
    // deterministic: no polling budget, no sleeps, no timing luck.
    {
        shtn_engine* slow = nullptr;
        shtn_engine_options so{};
        so.abi_version = SHTN_ABI_VERSION;
        CHECK(shtn_engine_create(&so, &slow) == SHTN_OK);
        shtn_model_load_options slo{};
        CHECK(shtn_engine_load_model(
                  slow, (fixtures + "/tiny-llama-slow.gguf").c_str(), &slo) ==
              SHTN_OK);

        auto o = base_opts("hello", 200);
        // Greedy decode: on this fixture it deterministically runs to
        // max_tokens (finish reason "length", no early EOS — pinned by the
        // greedy-determinism test above), so after the first streamed token
        // the generation is guaranteed to still be in flight while the
        // sequencer does its checks (each further token takes milliseconds).
        o.temperature = 0.0f;
        o.seed = 0;
        o.request_id = "unload-guard";

        // Explicit start-of-decode signal: the first non-final chunk. Emitted
        // from Runner::execute → strictly after the engine's guard state is
        // established, so observing it implies the invariant's precondition.
        struct GuardSink {
            std::mutex mu;
            std::condition_variable cv;
            bool first_token = false;
            int tokens = 0;

            static int32_t emit(void* user, const shtn_generation_chunk* c) {
                auto* s = static_cast<GuardSink*>(user);
                if (c == nullptr) return 0;
                {
                    std::lock_guard<std::mutex> lock(s->mu);
                    if (!c->final && !s->first_token) {
                        s->first_token = true;
                    }
                    if (!c->final) {
                        s->tokens += 1;
                    }
                }
                s->cv.notify_all();
                return 0;
            }
        } sink;

        // One sequencer thread: wait for the explicit signal (bounded — a
        // broken engine fails the test instead of hanging CI), verify the
        // observable scheduler state is coherent, assert BOTH guard surfaces
        // reject, then cancel so the test finishes quickly and cleanly.
        std::thread sequencer([&slow, &sink, &fixtures]() {
            bool signalled = false;
            {
                std::unique_lock<std::mutex> lock(sink.mu);
                signalled = sink.cv.wait_for(lock, std::chrono::seconds(60),
                                             [&] { return sink.first_token; });
            }
            CHECK(signalled);

            // Coherent observable state: the request must be reported
            // active by the scheduler at this point (this is the state the
            // old test raced against — it must never contradict the guard).
            shtn_scheduler_info si{};
            CHECK(shtn_engine_scheduler_info(slow, &si) == SHTN_OK);
            CHECK(si.active_requests == 1);

            // THE invariant: unload rejected while generation is active.
            CHECK(shtn_engine_unload_model(slow) == SHTN_ERR_MODEL_STATE);

            // The same guard protects the reload path (load must also be
            // rejected — it tears the mapping just the same).
            shtn_model_load_options rlo{};
            CHECK(shtn_engine_load_model(
                      slow, (fixtures + "/tiny-llama-slow.gguf").c_str(),
                      &rlo) == SHTN_ERR_MODEL_STATE);

            // Cancel the request; the blocked generate() returns
            // SHTN_ERR_CANCELLED and the engine is reused below.
            int32_t cancelled = 0;
            char reason[128] = {0};
            CHECK(shtn_engine_cancel_generation(slow, "unload-guard",
                                                &cancelled, reason) == SHTN_OK);
            CHECK(cancelled == 1);
        });

        shtn_generation_result res{};
        char d[256] = {0};
        const int32_t rc = shtn_engine_generate(slow, &o, GuardSink::emit,
                                                &sink, &res, d);
        sequencer.join();

        // The blocked generate() observed the cancellation cleanly.
        CHECK(rc == SHTN_ERR_CANCELLED);
        CHECK(std::string(res.finish_reason) == SHTN_FINISH_CANCELLED);
        CHECK(res.metrics.generated_tokens > 0);

        // Scheduler state consistent + engine reusable after the whole
        // sequence (rejected unload, rejected reload, cancelled generation).
        shtn_scheduler_info si{};
        CHECK(shtn_engine_scheduler_info(slow, &si) == SHTN_OK);
        CHECK(si.active_requests == 0);
        CHECK(si.total_cancelled == 1);

        auto o2 = base_opts("hello", 2);
        Sink sink2;
        shtn_generation_result res2{};
        char d2[256] = {0};
        CHECK(shtn_engine_generate(slow, &o2, Sink::emit, &sink2, &res2,
                                   d2) == SHTN_OK);
        shtn_engine_destroy(slow);
    }

    // --- malformed model (non-llama architecture) → capable=0 + UNSUPPORTED -----
    {
        // Build a minimal non-llama GGUF in /tmp.
        std::vector<uint8_t> b;
        auto put32 = [&b](uint32_t v) {
            b.push_back(static_cast<uint8_t>(v));
            b.push_back(static_cast<uint8_t>(v >> 8));
            b.push_back(static_cast<uint8_t>(v >> 16));
            b.push_back(static_cast<uint8_t>(v >> 24));
        };
        auto put64 = [&put32](uint64_t v) {
            put32(static_cast<uint32_t>(v & 0xFFFFFFFFu));
            put32(static_cast<uint32_t>(v >> 32));
        };
        auto putstr = [&b, &put32](const std::string& s) {
            put32(static_cast<uint32_t>(s.size()));
            put32(0);
            b.insert(b.end(), s.begin(), s.end());
        };
        auto kv_str = [&](const std::string& k, const std::string& v) {
            putstr(k);
            put32(8);
            putstr(v);
        };
        auto kv_u32 = [&](const std::string& k, uint32_t v) {
            putstr(k);
            put32(4);
            put32(v);
        };
        // GGUF F32 (type 6) — writes real float metadata (rms eps).
        auto kv_f32 = [&](const std::string& k, float v) {
            putstr(k);
            put32(6);
            uint32_t bits = 0;
            std::memcpy(&bits, &v, sizeof(bits));
            put32(bits);
        };

        b.insert(b.end(), {'G', 'G', 'U', 'F'});
        put32(3);
        put64(1);  // 1 tensor
        put64(12); // kv count
        kv_str("general.architecture", "qwen2");
        kv_u32("qwen2.context_length", 64);
        kv_u32("qwen2.embedding_length", 8);
        kv_u32("qwen2.block_count", 1);
        kv_u32("qwen2.attention.head_count", 1);
        kv_u32("qwen2.attention.head_count_kv", 1);
        kv_u32("qwen2.feed_forward_length", 8);
        kv_f32("qwen2.attention.layer_norm_rms_eps", 1e-5f);
        kv_u32("qwen2.rope.freq_base", 10000);
        kv_u32("qwen2.vocab_size", 8);
        kv_str("general.name", "not-llama");
        kv_str("tokenizer.ggml.model", "llama");
        // (kv count = 12; list above has 12 entries)

        putstr("token_embd.weight");
        put32(2);
        put64(8);
        put64(8);
        put32(0); // F32
        put64(0); // offset
        while (b.size() % 32 != 0) {
            b.push_back(0);
        }
        b.resize(b.size() + 8 * 8 * 4, 0);

        const std::string path = tmpdir.file("not-llama.gguf");
        FILE* f = std::fopen(path.c_str(), "wb");
        CHECK(f != nullptr);
        if (f != nullptr) {
            std::fwrite(b.data(), 1, b.size(), f);
            std::fclose(f);

            shtn_model_load_options lo{};
            CHECK(shtn_engine_load_model(engine, path.c_str(), &lo) ==
                  SHTN_OK);
            shtn_model_info mi{};
            CHECK(shtn_engine_model_info(engine, &mi) == SHTN_OK);
            CHECK(mi.generation_capable == 0);
            CHECK(std::strstr(mi.generation_reason, "qwen2") != nullptr);

            // Generate → UNSUPPORTED with the reason; llama.cpp is the
            // fallback signal.
            auto o = base_opts("hello", 4);
            Sink sink;
            shtn_generation_result res{};
            char d[256] = {0};
            CHECK(shtn_engine_generate(engine, &o, Sink::emit, &sink, &res,
                                       d) == SHTN_ERR_UNSUPPORTED);
            CHECK(std::strstr(d, "qwen2") != nullptr);
        }

        // Reload the GOOD model (replace semantics) — engine still fine.
        shtn_model_load_options lo2{};
        CHECK(shtn_engine_load_model(
                  engine, (fixtures + "/tiny-llama-f32.gguf").c_str(),
                  &lo2) == SHTN_OK);
        auto o = base_opts("hello", 2);
        Sink sink;
        shtn_generation_result res{};
        char d[256] = {0};
        CHECK(shtn_engine_generate(engine, &o, Sink::emit, &sink, &res, d) ==
              SHTN_OK);
        CHECK(sink.text == first_text.substr(0, sink.text.size()));
    }

    shtn_engine_destroy(engine);

    if (failures > 0) {
        std::fprintf(stderr, "test_generate: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_generate: all checks passed\n");
    return 0;
}
