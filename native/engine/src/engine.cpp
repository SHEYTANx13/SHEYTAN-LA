// engine.cpp — SHEYTAN Native Engine core implementation (Phase 5).
//
// Phase 5 adds REAL native generation:
//   shtn_engine_generate          full transformer forward pass + decode
//                                 loop + streaming chunks + measured
//                                 metrics, executed on the scheduler's
//                                 single-slot worker;
//   shtn_engine_cancel_generation cooperative cancellation of the queued
//                                 or active request;
//   shtn_engine_generation_stats  the measured generation snapshot;
//   shtn_engine_metrics           reports the scheduler's REAL active
//                                 count; kv_cache_info reads the runner's
//                                 REAL populated cache.
// Model loads now validate the llama graph and report
// generation_capable + reason (metadata-level; the Go core selects the
// llama.cpp fallback for models this engine cannot execute).
//
// Phase 4 surface (tokenizer / KV / scheduler) is unchanged; Phase 2
// (model loading) is unchanged except the capability verdict.

#include "shtn/engine.h"

#include "forward.h"
#include "generate.h"
#include "hardware.h"
#include "kv_cache.h"
#include "model.h"
#include "scheduler.h"
#include "tokenizer.h"
#include "util.h"

#include <atomic>
#include <chrono>
#include <cstring>
#include <memory>
#include <mutex>
#include <new>
#include <string>
#include <unordered_map>

// --- the engine object --------------------------------------------------------

// GenWork is the per-request generation payload the scheduler executor
// consumes (defined before the engine struct that stores it).
struct GenWork {
    shtn::gen::Spec spec;
    shtn_generation_emit_fn emit = nullptr;
    void* user = nullptr;
    shtn_generation_result* result = nullptr; // caller's slot
    int32_t rc = SHTN_ERR_INTERNAL;
    std::string detail;
};

// The error-code enum lives at global scope in engine.h (C linkage); the
// GenWork default above uses the global name.

namespace shtn {
namespace gen {
// detail_runner_execute is defined at the bottom of this file (after the
// engine internals are known); it runs one scheduled generation.
int32_t detail_runner_execute(shtn_engine* engine,
                              shtn::sched::RequestPtr req);
} // namespace gen
} // namespace shtn

struct shtn_engine {
    std::mutex mu;
    std::chrono::steady_clock::time_point started_at;
    shtn_hardware_info hardware;
    bool hardware_cached;

    // Model concern (guarded by its own mutex inside Model). The Model
    // owns the tokenizer vocab (Phase 4) and the llama graph binding
    // (Phase 5).
    shtn::model::Model model;

    // Phase 5: the scheduler (real single-slot worker) + generation
    // runner (KV cache binding, scratch, measured stats).
    shtn::sched::Scheduler scheduler;
    shtn::gen::Runner generator;

    // Generation lifecycle guards (one mutex, two jobs):
    //   - work registry: request id → pending GenWork (executor lookup);
    //   - active_generations: unload/reload rejection while a forward
    //     pass holds the mapping.
    std::mutex gen_mu;
    std::unordered_map<std::string, std::shared_ptr<GenWork>> work;
    uint32_t active_generations = 0;

    // --- internal helpers (engine.cpp use only) -------------------------
    bool insert_work(const std::string& id, std::shared_ptr<GenWork> w) {
        return work.emplace(id, std::move(w)).second;
    }
    void erase_work(const std::string& id) { work.erase(id); }
    std::shared_ptr<GenWork> find_work(const std::string& id) {
        const auto it = work.find(id);
        return it == work.end() ? nullptr : it->second;
    }
    int32_t submit_scheduled(shtn::sched::RequestPtr req, std::string& err) {
        return scheduler.submit(std::move(req), err);
    }
    bool cancel_scheduled(const std::string& id) { return scheduler.cancel(id); }

private:
    // The executor closure needs engine internals; friended via the
    // free function below (detail_runner_execute).
    friend int32_t shtn::gen::detail_runner_execute(shtn_engine*,
                                                    shtn::sched::RequestPtr);
};

namespace {

using shtn::copy_cstr;

// generation_in_flight — the single authority for the model-state guard.
//
// A generation is in flight across THREE windows, and every one of them
// must reject load/unload (tearing the model out under a forward pass is
// undefined behaviour):
//
//   1. registered → queued: shtn_engine_generate() inserted the work entry
//      (work map non-empty) but the scheduler has not popped it yet;
//   2. popped → executor-entered: the worker set its active slot
//      (scheduler stats active_requests == 1) but the executor has not
//      incremented active_generations yet — this is exactly the window
//      the unload-guard test used to race over (CI run 34921857962):
//      observers see the scheduler slot, the old guard counted only
//      active_generations, and an unload landing in between SUCCEEDED
//      while a request was observably active, tearing the model from
//      under the generation (it then failed with SHTN_ERR_NO_MODEL);
//   3. executor-entered → exited: active_generations > 0, the window the
//      old guard covered.
//
// With all three covered, the guard is coherent with what every observer
// can see: if scheduler_info/metrics report an active request, or a
// generate() call is blocked in flight, load/unload returns
// SHTN_ERR_MODEL_STATE.
bool generation_in_flight(shtn_engine* engine) {
    {
        // Windows 1 + 3 under the generation mutex.
        std::lock_guard<std::mutex> lock(engine->gen_mu);
        if (engine->active_generations > 0 || !engine->work.empty()) {
            return true;
        }
    }
    // Window 2 under the scheduler mutex (never nested with gen_mu: the
    // executor takes gen_mu outside the scheduler lock, so no cycle).
    const shtn::sched::Stats s = engine->scheduler.stats();
    return s.active_requests > 0;
}

void summarize_model(const shtn::model::Model& model, char* dst, size_t cap) {
    shtn_model_info info{};
    model.fill_info(&info);

    if (info.state[0] == '\0' ||
        std::strcmp(info.state, SHTN_MODEL_STATE_UNLOADED) == 0) {
        copy_cstr(dst, cap, "no model loaded");
        return;
    }

    if (std::strcmp(info.state, SHTN_MODEL_STATE_LOADED) == 0) {
        std::string s = "model loaded: ";
        s += info.architecture;
        s += " (";
        s += info.quantization[0] != '\0' ? info.quantization : "unknown quant";
        s += ")";
        if (info.generation_capable) {
            s += " — native generation capable";
        } else if (info.generation_reason[0] != '\0') {
            s += " — native generation unavailable: ";
            s += info.generation_reason;
        }
        copy_cstr(dst, cap, s.c_str());
        return;
    }

    if (std::strcmp(info.state, SHTN_MODEL_STATE_FAILED) == 0) {
        std::string s = "model load failed: ";
        s += info.error;
        copy_cstr(dst, cap, s.c_str());
        return;
    }

    copy_cstr(dst, cap, "model loading");
}

} // namespace

extern "C" {

uint32_t shtn_abi_version(void) {
    return SHTN_ABI_VERSION;
}

int32_t shtn_engine_create(const shtn_engine_options* opts, shtn_engine** out) {
    if (opts == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    if (opts->abi_version != SHTN_ABI_VERSION) {
        return SHTN_ERR_ABI;
    }

    if (opts->reserved != 0) {
        return SHTN_ERR_INVALID_ARG;
    }

    auto* engine = new (std::nothrow) shtn_engine();
    if (engine == nullptr) {
        return SHTN_ERR_INTERNAL;
    }

    engine->started_at = std::chrono::steady_clock::now();
    engine->hardware_cached = false;

    // Phase 5: start the REAL scheduler worker (single slot). Every
    // submitted generation request executes through the runner on this
    // worker; the executor records the outcome for the blocked submitter.
    {
        std::string err;
        const int32_t rc = engine->scheduler.start_worker(
            [engine](shtn::sched::RequestPtr req) -> int32_t {
                return shtn::gen::detail_runner_execute(engine,
                                                        std::move(req));
            });
        if (rc != SHTN_OK) {
            delete engine;
            return rc;
        }
    }

    *out = engine;
    return SHTN_OK;
}

void shtn_engine_destroy(shtn_engine* engine) {
    // The scheduler shutdown joins the worker (cooperatively cancelling
    // the active generation) BEFORE the model mapping disappears.
    if (engine != nullptr) {
        engine->scheduler.shutdown();
        engine->generator.abort_model_cycle();
        engine->model.unload();
    }
    delete engine;
}

int32_t shtn_engine_health(const shtn_engine* engine, shtn_health_status* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    std::memset(out, 0, sizeof(*out));

    // The engine is healthy while it exists; a FAILED model load is a
    // model-concern failure (visible in the detail), not an engine fault.
    out->healthy = 1;
    copy_cstr(out->state, sizeof(out->state), "ready");
    summarize_model(engine->model, out->detail, sizeof(out->detail));

    return SHTN_OK;
}

int32_t shtn_engine_hardware_info(shtn_engine* engine, shtn_hardware_info* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    std::lock_guard<std::mutex> lock(engine->mu);

    if (!engine->hardware_cached) {
        shtn_hardware_info& hw = engine->hardware;
        std::memset(&hw, 0, sizeof(hw));

        copy_cstr(hw.architecture, sizeof(hw.architecture), shtn::architecture_name());
        shtn::detect_cpu(hw.cpu);
        shtn::detect_ram(hw.ram);

        // No GPU / accelerator detection on the C++ side (the Go core
        // merges the sysinfo probe for real GPU facts). Counts stay
        // zero — represented, not invented.
        hw.gpu_count = 0;
        hw.accelerator_count = 0;

        copy_cstr(hw.detected_by, sizeof(hw.detected_by), "shtn-engine-cpp");

        engine->hardware_cached = true;
    }

    *out = engine->hardware;
    return SHTN_OK;
}

int32_t shtn_engine_metrics(shtn_engine* engine, shtn_metrics* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    std::lock_guard<std::mutex> lock(engine->mu);

    std::memset(out, 0, sizeof(*out));

    const auto uptime = std::chrono::steady_clock::now() - engine->started_at;
    out->uptime_seconds = std::chrono::duration<double>(uptime).count();
    out->process_rss_bytes = shtn::current_process_rss();
    {
        // Measured: the scheduler's active slot (1 while a generation
        // executes, 0 otherwise) — real, never an artificial count.
        const shtn::sched::Stats s = engine->scheduler.stats();
        out->active_requests = s.active_requests;
    }
    copy_cstr(out->state, sizeof(out->state), "ready");

    return SHTN_OK;
}

// --- Phase 2: model surface ----------------------------------------------

int32_t shtn_engine_load_model(shtn_engine* engine, const char* path,
                               const shtn_model_load_options* opts) {
    if (engine == nullptr || path == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    // A load while a generation is in flight would tear the mapping out
    // from under the forward pass — reject it (the caller retries after
    // the generation finishes). The guard covers the WHOLE in-flight
    // window; see generation_in_flight().
    if (generation_in_flight(engine)) {
        return SHTN_ERR_MODEL_STATE;
    }

    shtn_model_load_options defaults{};
    const shtn_model_load_options& options =
        opts != nullptr ? *opts : defaults;

    std::string error;
    const int32_t rc = engine->model.load(path, options, error);
    if (rc != SHTN_OK && error.empty()) {
        error = "model load failed with error code " + std::to_string(rc);
    }
    // The forward binding belongs to the previous load — invalidate it.
    engine->generator.abort_model_cycle();
    return rc;
}

int32_t shtn_engine_unload_model(shtn_engine* engine) {
    if (engine == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    // Same guard as load: the mapping cannot disappear under an active
    // forward pass. The guard covers the WHOLE in-flight window — queued,
    // popped, and executor-entered — so an unload can never slip between
    // the observable scheduler state and the executor counter.
    if (generation_in_flight(engine)) {
        return SHTN_ERR_MODEL_STATE;
    }

    const int32_t rc = engine->model.unload();
    engine->generator.abort_model_cycle();
    return rc;
}

int32_t shtn_engine_model_info(const shtn_engine* engine,
                               shtn_model_info* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    engine->model.fill_info(out);
    return SHTN_OK;
}

int32_t shtn_engine_memory_plan(const shtn_engine* engine,
                                shtn_memory_plan* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    engine->model.fill_plan(out);
    return SHTN_OK;
}

// --- Phase 4: tokenizer / KV / scheduler surface --------------------------

namespace {

void fill_tokenizer_info(const shtn_engine* engine,
                         shtn_tokenizer_info* out) {
    std::memset(out, 0, sizeof(*out));

    const shtn::tokenizer::Vocab* v = engine->model.tokenizer_vocab();
    if (v == nullptr) {
        return;
    }

    shtn::tokenizer::InfoSnapshot snap;
    shtn::tokenizer::fill_info(*v, snap);

    out->initialized = snap.initialized ? 1 : 0;
    out->vocab_size = snap.vocab_size;
    out->merge_count = snap.merge_count;
    out->has_bos = snap.has_bos ? 1 : 0;
    out->has_eos = snap.has_eos ? 1 : 0;
    out->has_unknown = snap.has_unknown ? 1 : 0;
    out->bos_id = snap.bos_id;
    out->eos_id = snap.eos_id;
    out->unknown_id = snap.unknown_id;

    const char* model_str = "unsupported";
    switch (snap.model) {
    case shtn::tokenizer::Model::kBPE:     model_str = "bpe"; break;
    case shtn::tokenizer::Model::kUnigram: model_str = "unigram"; break;
    case shtn::tokenizer::Model::kWPM:     model_str = "wpm"; break;
    default: break;
    }
    shtn::copy_cstr(out->model, sizeof(out->model), model_str);
    shtn::copy_cstr(out->model_name, sizeof(out->model_name),
                    snap.model_name.c_str());
}

void fill_kv_cache_info(const shtn_engine* engine,
                        shtn_kv_cache_info* out) {
    std::memset(out, 0, sizeof(*out));

    // Phase 5: the REAL cache lives in the generation runner's forward
    // binding — allocated at the first generate (sized from the model
    // dims, f16) and populated by the forward pass. Before any generate
    // this is the honest zero-state.
    const shtn::kv::Stats s = engine->generator.kv_stats();
    out->allocated = s.allocated ? 1 : 0;
    out->capacity_bytes = s.capacity_bytes;
    out->used_bytes = s.used_bytes;
    out->capacity_positions = s.capacity_positions;
    out->used_positions = s.used_positions;
    out->layer_count = s.layer_count;
    out->kv_dim = s.kv_dim;
    shtn::copy_cstr(out->quantization, sizeof(out->quantization),
                    s.quantization.c_str());
}

void fill_scheduler_info(const shtn_engine* engine,
                         shtn_scheduler_info* out) {
    std::memset(out, 0, sizeof(*out));

    const shtn::sched::Stats s = engine->scheduler.stats();
    out->active_requests = s.active_requests;
    out->queued_requests = s.queued_requests;
    out->max_concurrent = s.max_concurrent;
    out->queue_depth_limit = s.queue_depth_limit;
    out->total_submitted = s.total_submitted;
    out->total_completed = s.total_completed;
    out->total_cancelled = s.total_cancelled;
    out->total_failed = s.total_failed;
    out->shutting_down = s.shutting_down ? 1 : 0;
}

} // namespace

int32_t shtn_engine_tokenizer_init(shtn_engine* engine,
                                   shtn_tokenizer_info* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    std::string error;
    const int32_t rc = engine->model.init_tokenizer(error);

    fill_tokenizer_info(engine, out);
    if (rc != SHTN_OK && !error.empty()) {
        shtn::copy_cstr(out->error, sizeof(out->error), error.c_str());
    }
    return rc;
}

int32_t shtn_engine_tokenizer_info(const shtn_engine* engine,
                                   shtn_tokenizer_info* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    fill_tokenizer_info(engine, out);
    return SHTN_OK;
}

int32_t shtn_engine_tokenizer_encode(const shtn_engine* engine,
                                     const char* text, uint64_t text_len,
                                     const shtn_encode_options* opts,
                                     shtn_encode_result* result) {
    if (engine == nullptr || text == nullptr || opts == nullptr ||
        result == nullptr || result->ids == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    if (opts->reserved != 0) {
        return SHTN_ERR_INVALID_ARG;
    }
    if (opts->max_tokens == 0) {
        return SHTN_ERR_INVALID_ARG;
    }

    result->ids_count = 0;
    result->truncated = 0;

    const shtn::tokenizer::Vocab* v = engine->model.tokenizer_vocab();
    if (v == nullptr) {
        return SHTN_ERR_NO_MODEL;
    }

    shtn::tokenizer::EncodeOptions eopts;
    eopts.add_bos = opts->add_bos != 0;
    eopts.add_eos = opts->add_eos != 0;
    eopts.max_tokens = opts->max_tokens;

    std::string input(text, static_cast<size_t>(text_len));

    shtn::tokenizer::EncodeResult er;
    std::string error;
    const int32_t rc = shtn::tokenizer::encode(*v, input, eopts, er, error);
    if (rc != SHTN_OK) {
        return rc;
    }

    uint32_t cap = opts->max_tokens;
    uint32_t n = static_cast<uint32_t>(er.ids.size());
    if (n > cap) {
        n = cap;
        result->truncated = 1;
    } else if (er.truncated) {
        result->truncated = 1;
    }
    for (uint32_t i = 0; i < n; ++i) {
        result->ids[i] = er.ids[i];
    }
    result->ids_count = n;

    return SHTN_OK;
}

int32_t shtn_engine_tokenizer_decode(const shtn_engine* engine,
                                     const uint32_t* ids, uint64_t ids_count,
                                     const shtn_decode_options* opts,
                                     shtn_decode_result* result) {
    if (engine == nullptr || opts == nullptr || result == nullptr ||
        result->text == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    if (opts->reserved != 0) {
        return SHTN_ERR_INVALID_ARG;
    }
    if (opts->max_bytes == 0) {
        return SHTN_ERR_INVALID_ARG;
    }
    if (ids == nullptr && ids_count > 0) {
        return SHTN_ERR_INVALID_ARG;
    }

    result->text_count = 0;
    result->truncated = 0;
    result->text[0] = '\0';

    const shtn::tokenizer::Vocab* v = engine->model.tokenizer_vocab();
    if (v == nullptr) {
        return SHTN_ERR_NO_MODEL;
    }

    shtn::tokenizer::DecodeOptions dopts;
    dopts.skip_special = opts->skip_special != 0;
    dopts.max_bytes = opts->max_bytes;

    shtn::tokenizer::DecodeResult dr;
    std::string error;
    const int32_t rc = shtn::tokenizer::decode(*v, ids,
        static_cast<size_t>(ids_count), dopts, dr, error);
    if (rc != SHTN_OK) {
        return rc;
    }

    uint32_t cap = opts->max_bytes;
    uint32_t n = static_cast<uint32_t>(dr.text.size());
    if (n >= cap) {
        n = cap - 1;
        result->truncated = 1;
    } else if (dr.truncated) {
        result->truncated = 1;
    }
    std::memcpy(result->text, dr.text.data(), n);
    result->text[n] = '\0';
    result->text_count = n;

    return SHTN_OK;
}

int32_t shtn_engine_kv_cache_info(const shtn_engine* engine,
                                  shtn_kv_cache_info* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    fill_kv_cache_info(engine, out);
    return SHTN_OK;
}

int32_t shtn_engine_scheduler_info(const shtn_engine* engine,
                                   shtn_scheduler_info* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    fill_scheduler_info(engine, out);
    return SHTN_OK;
}

// --- Phase 5: REAL native generation -----------------------------------------

int32_t shtn_engine_generate(shtn_engine* engine,
                             const shtn_generation_options* opts,
                             shtn_generation_emit_fn emit, void* user,
                             shtn_generation_result* out, char* detail) {
    if (engine == nullptr || opts == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    if (detail != nullptr) {
        detail[0] = '\0';
    }
    if (out != nullptr) {
        std::memset(out, 0, sizeof(*out));
    }
    if (opts->prompt == nullptr || opts->prompt_len == 0 ||
        opts->prompt_len > (1ull << 24)) {
        if (detail != nullptr) {
            copy_cstr(detail, 256, "generation: prompt missing or oversized");
        }
        return SHTN_ERR_INVALID_ARG;
    }
    if (opts->max_tokens == 0) {
        if (detail != nullptr) {
            copy_cstr(detail, 256, "generation: max_tokens must be > 0");
        }
        return SHTN_ERR_INVALID_ARG;
    }
    if (opts->reserved != 0) {
        return SHTN_ERR_INVALID_ARG;
    }

    // Build the work + request.
    auto work = std::make_shared<GenWork>();
    work->spec.request_id =
        opts->request_id != nullptr ? opts->request_id : "";
    work->spec.prompt.assign(opts->prompt,
                             static_cast<size_t>(opts->prompt_len));
    work->spec.max_tokens = opts->max_tokens;
    work->spec.temperature = opts->temperature;
    work->spec.top_k = opts->top_k;
    work->spec.top_p = opts->top_p;
    work->spec.repetition_penalty = opts->repetition_penalty;
    work->spec.repeat_last_n =
        opts->repeat_last_n == 0 ? shtn::gen::kDefaultRepeatLastN
                                 : opts->repeat_last_n;
    work->spec.seed = opts->seed;
    work->emit = emit;
    work->user = user;
    work->result = out;

    // Request id: caller-supplied or a deterministic synthetic one.
    std::string req_id = work->spec.request_id;
    if (req_id.empty()) {
        static std::atomic<uint64_t> next_anon{1};
        req_id = "gen-" + std::to_string(next_anon.fetch_add(1));
        work->spec.request_id = req_id;
    }
    if (req_id.size() > 128) {
        if (detail != nullptr) {
            copy_cstr(detail, 256, "generation: request id too long (>128)");
        }
        return SHTN_ERR_INVALID_ARG;
    }

    auto req = std::make_shared<shtn::sched::Request>(req_id, nullptr);

    // Register the work so the executor can find it. A duplicate
    // in-flight id would orphan the older entry — rejected.
    {
        std::lock_guard<std::mutex> lock(engine->gen_mu);
        if (!engine->insert_work(req_id, work)) {
            if (detail != nullptr) {
                copy_cstr(detail, 256,
                          "generation: request id already in flight");
            }
            return SHTN_ERR_MODEL_STATE;
        }
    }

    // Queue on the scheduler (bounded).
    {
        std::string err;
        const int32_t rc = engine->submit_scheduled(req, err);
        if (rc != SHTN_OK) {
            std::lock_guard<std::mutex> lock(engine->gen_mu);
            engine->erase_work(req_id);
            if (detail != nullptr) {
                copy_cstr(detail, 256, ("generation: " + err).c_str());
            }
            return rc;
        }
    }

    // Block until terminal (the executor records the outcome in the work
    // entry, then the request state becomes terminal and wakes us).
    req->wait_terminal();

    // Collect the outcome.
    int32_t rc = SHTN_ERR_INTERNAL;
    std::string det;
    {
        std::lock_guard<std::mutex> lock(engine->gen_mu);
        if (auto w = engine->find_work(req_id)) {
            rc = w->rc;
            det = w->detail;
        }
        engine->erase_work(req_id);
    }

    if (detail != nullptr && !det.empty()) {
        copy_cstr(detail, 256, det.c_str());
    }

    return rc;
}

int32_t shtn_engine_cancel_generation(shtn_engine* engine,
                                       const char* request_id,
                                       int32_t* cancelled, char* reason) {
    if (engine == nullptr || request_id == nullptr || cancelled == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }

    *cancelled = 0;
    if (reason != nullptr) {
        reason[0] = '\0';
    }

    // The scheduler resolves both QUEUED (removed + cancelled) and ACTIVE
    // (cooperative flag observed every token) requests.
    const bool found = engine->cancel_scheduled(request_id);

    if (!found) {
        if (reason != nullptr) {
            copy_cstr(reason, 128, "no queued or active request with this id");
        }
        return SHTN_OK;
    }

    *cancelled = 1;
    return SHTN_OK;
}

int32_t shtn_engine_generation_stats(const shtn_engine* engine,
                                     shtn_generation_stats* out) {
    if (engine == nullptr || out == nullptr) {
        return SHTN_ERR_INVALID_ARG;
    }
    *out = engine->generator.stats();
    return SHTN_OK;
}

} // extern "C"

namespace shtn {
namespace gen {

// detail_runner_execute is the scheduler executor body: it resolves the
// request's work, runs the REAL generation through the runner and
// records the outcome for the blocked submitter.
int32_t detail_runner_execute(shtn_engine* engine,
                              shtn::sched::RequestPtr req) {
    std::shared_ptr<GenWork> work;
    {
        std::lock_guard<std::mutex> lock(engine->gen_mu);
        work = engine->find_work(req->id);
    }
    if (work == nullptr) {
        // Submitted but unregistered (should not happen; defensive).
        return SHTN_ERR_INTERNAL;
    }

    {
        std::lock_guard<std::mutex> lock(engine->gen_mu);
        engine->active_generations += 1;
    }

    const int32_t rc = engine->generator.execute(
        engine->model, work->spec, req->cancel_requested, work->emit,
        work->user, work->result, work->detail);

    {
        std::lock_guard<std::mutex> lock(engine->gen_mu);
        engine->active_generations -= 1;
        work->rc = rc;
    }

    return rc;
}

} // namespace gen
} // namespace shtn
