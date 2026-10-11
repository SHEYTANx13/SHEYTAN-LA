// host_main.cpp — the supervised native engine host (shtn-engine-host).
//
// The Go core spawns this process and speaks the length-prefixed JSON
// protocol over stdin/stdout (see internal/native/engine/protocol.go).
// stderr carries human-readable diagnostics (ring-buffered by Go).
//
// Dispatch loop rules:
//   - one request frame in → one or more response frames out (generate
//     streams EVENT frames with the same id before its final frame);
//   - malformed input (bad JSON, unknown op, oversized frame) gets a
//     bounded ERROR response — the host NEVER crashes and never exits
//     on bad input;
//   - "shutdown" acknowledges and exits cleanly;
//   - stdin EOF cancels in-flight generation, waits for the lanes and
//     exits cleanly (the Go side closes stdin on stop);
//   - every op is coarse-grained; generation streams coarse chunks
//     (never one frame per token — the engine batches emissions).
//
// Phase 2 ops: load_model / unload_model / model_info — the native GGUF
// loading surface (validate + memory-map + metadata + memory plan).
//
// Phase 4 ops: tokenizer_init / tokenizer_info / tokenizer_encode /
// tokenizer_decode / kv_cache_info / scheduler_info — the foundation
// primitives surface.
//
// Phase 5 ops: generate / cancel — REAL native generation.
//   generate: {"id":N,"op":"generate","payload":{requestId, prompt,
//             maxTokens, temperature, topK, topP, repetitionPenalty,
//             repeatLastN, seed}} →
//             {"id":N,"ok":true,"event":"chunk","result":{requestId,
//              text, token, tokensSoFar}}* then
//             {"id":N,"ok":true,"result":{requestId, finishReason,
//              generatedTokens, promptTokens, metrics{...}}}
//             (or {"id":N,"ok":false,"error":"..."} as the final frame).
//             The generation runs on a bounded LANE thread through the
//             engine's single-slot scheduler; the dispatch loop stays
//             responsive (cancel / metrics / shutdown while generating).
//   cancel: {"id":N,"op":"cancel","payload":{requestId}} →
//           {"id":N,"ok":true,"result":{cancelled, reason}} — real
//           cooperative cancellation of the queued or active request.

#include "shtn/engine.h"
#include "shtn/types.h"
#include "shtn/version.h"

#include "json.h"
#include "protocol.h"

#include <atomic>
#include <cstdio>
#include <cstring>
#include <iostream>
#include <list>
#include <memory>
#include <mutex>
#include <sstream>
#include <string>
#include <thread>
#include <vector>

#if defined(_WIN32)
#include <fcntl.h>
#include <io.h>
#endif

namespace {

using shtn::json::quote;

// Forward declarations (definitions live further down; the Phase 5
// generation lane code references them).
std::string error_response(int64_t id, const std::string& message);
std::string success_response(int64_t id, const std::string& result);

// json_num renders a double without trailing garbage for integral values.
std::string json_num(double v) {
    std::ostringstream oss;
    oss << v;
    return oss.str();
}

std::string json_uint(unsigned long long v) {
    std::ostringstream oss;
    oss << v;
    return oss.str();
}

// --- op handlers ------------------------------------------------------------

std::string op_ping() {
    std::ostringstream oss;
    oss << "{"
        << "\"protocolVersion\":" << SHTN_PROTOCOL_VERSION
        << ",\"abiVersion\":" << SHTN_ABI_VERSION
        << ",\"engine\":" << quote(SHTN_ENGINE_NAME)
        << "}";
    return oss.str();
}

std::string op_health(shtn_engine* engine) {
    shtn_health_status health{};

    const int32_t rc = shtn_engine_health(engine, &health);

    if (rc != SHTN_OK) {
        throw std::string("health failed with error code ") + std::to_string(rc);
    }

    std::ostringstream oss;
    oss << "{"
        << "\"healthy\":" << (health.healthy ? "true" : "false")
        << ",\"state\":" << quote(health.state)
        << ",\"detail\":" << quote(health.detail)
        << "}";
    return oss.str();
}

std::string op_hardware(shtn_engine* engine) {
    shtn_hardware_info hw{};

    const int32_t rc = shtn_engine_hardware_info(engine, &hw);

    if (rc != SHTN_OK) {
        throw std::string("hwinfo failed with error code ") + std::to_string(rc);
    }

    std::ostringstream oss;
    oss << "{"
        << "\"architecture\":" << quote(hw.architecture)
        << ",\"cpu\":{"
        << "\"name\":" << quote(hw.cpu.name)
        << ",\"physicalCores\":" << hw.cpu.physical_cores
        << ",\"logicalCores\":" << hw.cpu.logical_cores
        << ",\"frequencyMHz\":" << (hw.cpu.frequency_hz / 1000000)
        << "},\"ram\":{"
        << "\"totalBytes\":" << json_uint(hw.ram.total_bytes)
        << ",\"availableBytes\":" << json_uint(hw.ram.available_bytes)
        << "},\"gpus\":[],\"accelerators\":[]"
        << "}";
    return oss.str();
}

std::string op_metrics(shtn_engine* engine) {
    shtn_metrics m{};

    const int32_t rc = shtn_engine_metrics(engine, &m);

    if (rc != SHTN_OK) {
        throw std::string("metrics failed with error code ") + std::to_string(rc);
    }

    // The model concern rides along as an honest snapshot (state only;
    // full metadata is the model_info op's job).
    shtn_model_info mi{};
    shtn_engine_model_info(engine, &mi);

    // Phase 5: REAL generation + KV measurements (zero before any
    // generation — never fabricated).
    shtn_generation_stats gs{};
    shtn_engine_generation_stats(engine, &gs);
    shtn_kv_cache_info kv{};
    shtn_engine_kv_cache_info(engine, &kv);
    shtn_scheduler_info si{};
    shtn_engine_scheduler_info(engine, &si);

    std::ostringstream oss;
    oss << "{"
        << "\"engineState\":" << quote(m.state)
        << ",\"uptimeSeconds\":" << json_num(m.uptime_seconds)
        << ",\"processRssBytes\":" << json_uint(m.process_rss_bytes)
        << ",\"modelState\":" << quote(mi.state)
        << ",\"scheduler\":{"
        << "\"activeRequests\":" << si.active_requests
        << ",\"queuedRequests\":" << si.queued_requests
        << ",\"maxConcurrentRequests\":" << si.max_concurrent
        << ",\"totalSubmitted\":" << json_uint(si.total_submitted)
        << ",\"totalCompleted\":" << json_uint(si.total_completed)
        << ",\"totalCancelled\":" << json_uint(si.total_cancelled)
        << ",\"totalFailed\":" << json_uint(si.total_failed)
        << "},\"memory\":{"
        << "\"nativeRssBytes\":" << json_uint(m.process_rss_bytes)
        << "},\"kv\":{"
        << "\"allocated\":" << (kv.allocated ? "true" : "false")
        << ",\"strategy\":\"f16\""
        << ",\"capacityBytes\":" << json_uint(kv.capacity_bytes)
        << ",\"usedBytes\":" << json_uint(kv.used_bytes)
        << ",\"capacityPositions\":" << json_uint(kv.capacity_positions)
        << ",\"usedPositions\":" << json_uint(kv.used_positions)
        << "},\"generation\":{"
        << "\"activeRequests\":" << gs.active_requests
        << ",\"totalRequests\":" << json_uint(gs.total_requests)
        << ",\"totalCompleted\":" << json_uint(gs.total_completed)
        << ",\"totalCancelled\":" << json_uint(gs.total_cancelled)
        << ",\"totalFailed\":" << json_uint(gs.total_failed)
        << ",\"ttftSeconds\":" << json_num(gs.ttft_seconds)
        << ",\"tokensPerSecond\":" << json_num(gs.tokens_per_second)
        << ",\"promptTokensPerSecond\":"
        << json_num(gs.prompt_tokens_per_second)
        << ",\"lastPromptTokens\":" << gs.last_prompt_tokens
        << ",\"lastGeneratedTokens\":" << gs.last_generated_tokens
        << "}"
        << "}";
    return oss.str();
}

// --- Phase 5: generation lane ------------------------------------------------
//
// The host runs at most kMaxLanes generation lanes at a time (each lane
// blocks in shtn_engine_generate while the engine's single-slot
// scheduler serializes execution). A generate request beyond the engine
// queue bound is rejected with SHTN_ERR_QUEUE_FULL by the scheduler, so
// the lane count is structurally bounded.
constexpr size_t kMaxLanes = 16;

// GenLane is one in-flight generate request.
//
// Phase 6 repair (CI run 34703102794, Linux): the lane owns COPIES of
// every string the engine reads through opts. Previously opts.prompt
// pointed into op_generate's stack-local std::string — a dangling
// pointer the moment op_generate returned. The lane thread usually won
// the read race (prompt read at the top of shtn_engine_generate) and
// the tests passed, but on a fully-loaded runner the dispatch loop
// reused the dead stack frame first and the corrupted prompt silently
// dropped the BPE merges: "hello" encoded as 7 tokens instead of 5 and
// the context-bound test rejected a fitting request. The fix is
// ownership, not timing: the prompt lives in the lane, which outlives
// the generation.
struct GenLane {
    int64_t id = 0;
    std::string request_id;
    std::string prompt; // own copy — opts.prompt points here
    shtn_generation_options opts{};
    std::thread thread;
    // Set by lane_body as its LAST action, after the final frame is
    // written. host_loop sweeps finished lanes (join + retire) so the
    // lane list tracks IN-FLIGHT work, not lifetime totals — without
    // the sweep, kMaxLanes would cap the host at 16 generations TOTAL
    // and then reject everything while idle.
    std::atomic<bool> finished{false};
};

// HostCtx carries everything the lanes + dispatch loop share.
struct HostCtx {
    shtn_engine* engine = nullptr;
    std::ostream* out = nullptr;
    std::mutex out_mu;        // serializes every write_frame
    std::mutex lanes_mu;      // guards the lane list
    std::list<std::unique_ptr<GenLane>> lanes;
};

// generation_result_json renders the final generate frame's result.
std::string generation_result_json(const std::string& request_id,
                                   const shtn_generation_result& res) {
    std::ostringstream oss;
    oss << "{"
        << "\"requestId\":" << quote(request_id)
        << ",\"finishReason\":" << quote(res.finish_reason)
        << ",\"promptTokens\":" << res.metrics.prompt_tokens
        << ",\"generatedTokens\":" << res.metrics.generated_tokens
        << ",\"metrics\":{"
        << "\"promptSeconds\":" << json_num(res.metrics.prompt_seconds)
        << ",\"ttftSeconds\":" << json_num(res.metrics.ttft_seconds)
        << ",\"decodeSeconds\":" << json_num(res.metrics.decode_seconds)
        << ",\"totalSeconds\":" << json_num(res.metrics.total_seconds)
        << ",\"tokensPerSecond\":" << json_num(res.metrics.tokens_per_second)
        << ",\"promptTokensPerSecond\":"
        << json_num(res.metrics.prompt_tokens_per_second)
        << ",\"kvPositionsUsed\":" << json_uint(res.metrics.kv_positions_used)
        << "}"
        << "}";
    return oss.str();
}

// lane_body is the thread body of one generate lane.
void lane_body(HostCtx* ctx, GenLane* lane) {
    // The emit callback runs on the ENGINE's scheduler worker thread (the
    // lane itself blocks in shtn_engine_generate). Event frames are
    // written under the shared out mutex so they never interleave
    // mid-frame with dispatch-loop responses.
    struct EmitCtx {
        HostCtx* ctx;
        GenLane* lane;
    } emit_ctx{ctx, lane};

    auto emit = [](void* user, const shtn_generation_chunk* c) -> int32_t {
        auto* e = static_cast<EmitCtx*>(user);
        if (c == nullptr) {
            return 0;
        }

        std::ostringstream oss;
        oss << "{"
            << "\"id\":" << e->lane->id
            << ",\"ok\":true"
            << ",\"event\":\"chunk\""
            << ",\"result\":{"
            << "\"requestId\":" << quote(e->lane->request_id)
            << ",\"text\":" << quote(std::string(c->text, c->text_len))
            << ",\"token\":" << c->token_id
            << ",\"final\":" << (c->final ? "true" : "false")
            << "}"
            << "}";

        const std::string payload = oss.str();
        std::lock_guard<std::mutex> lock(e->ctx->out_mu);
        shtn::protocol::write_frame(*e->ctx->out, payload);
        return 0;
    };

    shtn_generation_result res{};
    char detail[256] = {0};
    const int32_t rc = shtn_engine_generate(ctx->engine, &lane->opts, emit,
                                            &emit_ctx, &res, detail);

    // The final frame for this request id.
    std::string payload;
    if (rc == SHTN_OK || rc == SHTN_ERR_CANCELLED) {
        payload = "{\"id\":" + std::to_string(lane->id) +
                  ",\"ok\":true,\"result\":" +
                  generation_result_json(lane->request_id, res) + "}";
    } else {
        std::string msg = detail[0] != '\0' ? detail
                                             : "generation failed (code " +
                                                   std::to_string(rc) + ")";
        payload = "{\"id\":" + std::to_string(lane->id) +
                  ",\"ok\":false,\"error\":" + quote(msg) + "}";
    }

    {
        std::lock_guard<std::mutex> lock(ctx->out_mu);
        shtn::protocol::write_frame(*ctx->out, payload);
    }

    // LAST action: mark the lane finished so host_loop can join and
    // retire it. No lock is taken after this point.
    lane->finished.store(true, std::memory_order_release);
}

// op_generate schedules a generation lane. The dispatch loop returns
// WITHOUT writing a response — the lane owns this request id's frames
// (event frames + the final frame).
void op_generate(HostCtx* ctx, const shtn::json::Parsed& req,
                 const std::string& payload_raw, bool has_payload) {
    if (!has_payload) {
        std::lock_guard<std::mutex> lock(ctx->out_mu);
        shtn::protocol::write_frame(
            *ctx->out, error_response(req.id, "generate requires a payload"));
        return;
    }

    std::string request_id, prompt, err;
    uint32_t max_tokens = 0, repeat_last_n = 0;
    float temperature = 1.0f, top_p = 1.0f, repetition_penalty = 1.0f;
    int32_t top_k = 0;
    uint64_t seed = 0;

    if (!shtn::json::extract_generate_payload(
            payload_raw, request_id, prompt, max_tokens, temperature, top_k,
            top_p, repetition_penalty, repeat_last_n, seed, err)) {
        std::lock_guard<std::mutex> lock(ctx->out_mu);
        shtn::protocol::write_frame(
            *ctx->out,
            error_response(req.id, "malformed generate payload: " + err));
        return;
    }

    if (request_id.empty()) {
        request_id = "go-" + std::to_string(req.id);
    }

    {
        std::lock_guard<std::mutex> lock(ctx->lanes_mu);
        if (ctx->lanes.size() >= kMaxLanes) {
            std::lock_guard<std::mutex> olock(ctx->out_mu);
            shtn::protocol::write_frame(
                *ctx->out,
                error_response(req.id,
                               "generate: too many concurrent requests"));
            return;
        }
    }

    auto lane = std::make_unique<GenLane>();
    lane->id = req.id;
    lane->request_id = request_id;
    lane->prompt = prompt; // own copy — MUST outlive the generation
    lane->opts.request_id = lane->request_id.c_str();
    lane->opts.prompt = lane->prompt.c_str();
    lane->opts.prompt_len = lane->prompt.size();
    lane->opts.max_tokens = max_tokens;
    lane->opts.temperature = temperature;
    lane->opts.top_k = top_k;
    lane->opts.top_p = top_p;
    lane->opts.repetition_penalty = repetition_penalty;
    lane->opts.repeat_last_n = repeat_last_n;
    lane->opts.seed = seed;
    lane->opts.reserved = 0;

    GenLane* raw = lane.get();
    {
        std::lock_guard<std::mutex> lock(ctx->lanes_mu);
        ctx->lanes.push_back(std::move(lane));
    }

    raw->thread = std::thread(
        [ctx, raw]() { lane_body(ctx, raw); });
}

// sweep_finished_lanes retires lanes whose lane_body has returned.
// Runs on the dispatch (host_loop) thread only — the same thread that
// assigns lane->thread inside op_generate, so a lane can never be swept
// between thread creation and handle assignment. Joins happen without
// lanes_mu held (the finished lane is already at its end; join is
// instantaneous) and the unique_ptr frees the lane only after its
// thread was joined.
void sweep_finished_lanes(HostCtx* ctx) {
    std::list<std::unique_ptr<GenLane>> done;
    {
        std::lock_guard<std::mutex> lock(ctx->lanes_mu);
        for (auto it = ctx->lanes.begin(); it != ctx->lanes.end();) {
            if ((*it)->finished.load(std::memory_order_acquire)) {
                done.splice(done.end(), ctx->lanes, it++);
            } else {
                ++it;
            }
        }
    }

    for (auto& lane : done) {
        if (lane->thread.joinable()) {
            lane->thread.join();
        }
    }
    // `done` destruction frees the retired lanes — after their threads
    // were joined.
}

// join_lanes cancels every in-flight generation and joins the lane
// threads (call before destroying the engine / leaving host_loop).
void join_lanes(HostCtx* ctx) {
    std::list<std::unique_ptr<GenLane>> lanes;
    {
        std::lock_guard<std::mutex> lock(ctx->lanes_mu);
        lanes = std::move(ctx->lanes);
    }

    // Cancel each request (queued or active) so lanes unblock promptly.
    for (auto& l : lanes) {
        int32_t cancelled = 0;
        char reason[128] = {0};
        shtn_engine_cancel_generation(ctx->engine, l->request_id.c_str(),
                                      &cancelled, reason);
    }

    for (auto& l : lanes) {
        if (l->thread.joinable()) {
            l->thread.join();
        }
    }
}

// op_cancel is the REAL Phase 5 cancel: cooperative cancellation of the
// queued or active generation request.
std::string op_cancel(HostCtx* ctx, const std::string& payload_raw,
                      bool has_payload) {
    std::string request_id;
    std::string err;

    if (!has_payload ||
        !shtn::json::extract_request_id_payload(payload_raw, request_id,
                                                err)) {
        return "{\"cancelled\":false,\"reason\":\"" +
               (has_payload ? err : std::string("cancel requires a payload")) +
               "\"}";
    }

    int32_t cancelled = 0;
    char reason[128] = {0};
    const int32_t rc = shtn_engine_cancel_generation(
        ctx->engine, request_id.c_str(), &cancelled, reason);

    if (rc != SHTN_OK) {
        return "{\"cancelled\":false,\"reason\":\"cancel failed (code " +
               std::to_string(rc) + ")\"}";
    }

    std::ostringstream oss;
    oss << "{"
        << "\"cancelled\":" << (cancelled ? "true" : "false")
        << ",\"reason\":" << quote(cancelled ? "" : reason)
        << "}";
    return oss.str();
}

// model_info_json renders one shtn_model_info snapshot (only fields the
// engine actually read or derived; zeros/empties stay zero/empty).
std::string model_info_json(const shtn_model_info& mi) {
    std::ostringstream oss;
    oss << "{"
        << "\"path\":" << quote(mi.path)
        << ",\"architecture\":" << quote(mi.architecture)
        << ",\"name\":" << quote(mi.name)
        << ",\"quantization\":" << quote(mi.quantization)
        << ",\"state\":" << quote(mi.state)
        << ",\"error\":" << quote(mi.error)
        << ",\"fileSizeBytes\":" << json_uint(mi.file_size_bytes)
        << ",\"parameterCount\":" << json_uint(mi.parameter_count)
        << ",\"contextLength\":" << json_uint(mi.context_length)
        << ",\"vocabularySize\":" << json_uint(mi.vocabulary_size)
        << ",\"embeddingLength\":" << json_uint(mi.embedding_length)
        << ",\"layerCount\":" << json_uint(mi.layer_count)
        << ",\"tensorCount\":" << mi.tensor_count
        << ",\"ggufVersion\":" << mi.gguf_version
        << ",\"fileType\":" << mi.general_file_type
        << ",\"hasFileType\":" << (mi.has_file_type ? "true" : "false")
        << ",\"kvCacheBytes\":" << json_uint(mi.kv_cache_bytes)
        << ",\"workspaceBytes\":" << json_uint(mi.workspace_bytes)
        << ",\"totalPlanBytes\":" << json_uint(mi.total_plan_bytes)
        << ",\"generationCapable\":"
        << (mi.generation_capable ? "true" : "false")
        << ",\"generationReason\":" << quote(mi.generation_reason)
        << "}";
    return oss.str();
}

std::string memory_plan_json(const shtn_memory_plan& mp) {
    std::ostringstream oss;
    oss << "{"
        << "\"modelFileBytes\":" << json_uint(mp.model_file_bytes)
        << ",\"mappedBytes\":" << json_uint(mp.mapped_bytes)
        << ",\"weightsBytes\":" << json_uint(mp.weights_bytes)
        << ",\"workspaceBytes\":" << json_uint(mp.workspace_bytes)
        << ",\"kvCacheBytes\":" << json_uint(mp.kv_cache_bytes)
        << ",\"runtimeOverheadBytes\":" << json_uint(mp.runtime_overhead_bytes)
        << ",\"totalBytes\":" << json_uint(mp.total_bytes)
        << ",\"availableRamBytes\":" << json_uint(mp.available_ram_bytes)
        << ",\"fitsInRam\":" << mp.fits_in_ram
        << "}";
    return oss.str();
}

// op_load_model validates the payload, runs the native load and renders
// the post-load model snapshot. Errors are thrown as readable strings
// (the dispatch turns them into bounded error frames).
std::string op_load_model(shtn_engine* engine, const std::string& payload_raw,
                          bool has_payload) {
    std::string path;
    uint32_t context_length = 0;
    std::string err;

    if (!has_payload) {
        throw std::string("load_model requires a payload");
    }

    if (!shtn::json::extract_load_payload(payload_raw, path, context_length,
                                          err)) {
        throw std::string("malformed load_model payload: ") + err;
    }

    shtn_model_load_options opts{};
    opts.context_length = context_length;
    opts.reserved = 0;

    const int32_t rc = shtn_engine_load_model(engine, path.c_str(), &opts);

    if (rc != SHTN_OK) {
        // The model snapshot carries the failure detail — surface it in
        // the error response instead of a bare code.
        shtn_model_info mi{};
        shtn_engine_model_info(engine, &mi);

        std::string msg = "load failed";
        if (mi.error[0] != '\0') {
            msg = mi.error;
        } else {
            msg += " (error code " + std::to_string(rc) + ")";
        }
        throw msg;
    }

    shtn_model_info mi{};
    shtn_memory_plan mp{};

    shtn_engine_model_info(engine, &mi);
    shtn_engine_memory_plan(engine, &mp);

    std::ostringstream oss;
    oss << "{"
        << "\"loaded\":true"
        << ",\"state\":" << quote(mi.state)
        << ",\"model\":" << model_info_json(mi)
        << ",\"memory\":" << memory_plan_json(mp)
        << "}";
    return oss.str();
}

std::string op_unload_model(shtn_engine* engine) {
    const int32_t rc = shtn_engine_unload_model(engine);

    if (rc != SHTN_OK) {
        throw std::string("unload failed (error code ") + std::to_string(rc) + ")";
    }

    std::ostringstream oss;
    oss << "{"
        << "\"loaded\":false"
        << ",\"state\":" << quote(SHTN_MODEL_STATE_UNLOADED)
        << "}";
    return oss.str();
}

std::string op_model_info(shtn_engine* engine) {
    shtn_model_info mi{};
    shtn_memory_plan mp{};

    const int32_t rc = shtn_engine_model_info(engine, &mi);
    if (rc != SHTN_OK) {
        throw std::string("model_info failed (error code ") +
            std::to_string(rc) + ")";
    }

    shtn_engine_memory_plan(engine, &mp);

    const bool loaded = std::strcmp(mi.state, SHTN_MODEL_STATE_LOADED) == 0;

    std::ostringstream oss;
    oss << "{"
        << "\"loaded\":" << (loaded ? "true" : "false")
        << ",\"state\":" << quote(mi.state);

    // With nothing attempted, model/memory stay absent — the snapshot is
    // honest about "nothing to report".
    if (mi.state[0] != '\0' &&
        std::strcmp(mi.state, SHTN_MODEL_STATE_UNLOADED) != 0) {
        oss << ",\"model\":" << model_info_json(mi)
            << ",\"memory\":" << memory_plan_json(mp);
    }

    oss << "}";
    return oss.str();
}

// --- Phase 4: tokenizer / KV / scheduler ops ------------------------------

// tokenizer_info_json renders one shtn_tokenizer_info snapshot.
std::string tokenizer_info_json(const shtn_tokenizer_info& ti) {
    std::ostringstream oss;
    oss << "{"
        << "\"initialized\":" << (ti.initialized ? "true" : "false")
        << ",\"model\":" << quote(ti.model)
        << ",\"modelName\":" << quote(ti.model_name)
        << ",\"vocabSize\":" << ti.vocab_size
        << ",\"mergeCount\":" << ti.merge_count
        << ",\"hasBos\":" << (ti.has_bos ? "true" : "false")
        << ",\"hasEos\":" << (ti.has_eos ? "true" : "false")
        << ",\"hasUnknown\":" << (ti.has_unknown ? "true" : "false")
        << ",\"bosId\":" << ti.bos_id
        << ",\"eosId\":" << ti.eos_id
        << ",\"unknownId\":" << ti.unknown_id
        << ",\"error\":" << quote(ti.error)
        << "}";
    return oss.str();
}

std::string op_tokenizer_init(shtn_engine* engine) {
    shtn_tokenizer_info ti{};
    const int32_t rc = shtn_engine_tokenizer_init(engine, &ti);

    std::ostringstream oss;
    oss << "{";
    if (rc == SHTN_OK) {
        oss << "\"initialized\":" << (ti.initialized ? "true" : "false")
            << ",\"info\":" << tokenizer_info_json(ti);
    } else {
        oss << "\"initialized\":false"
            << ",\"info\":" << tokenizer_info_json(ti)
            << ",\"error\":" << quote(rc == SHTN_ERR_UNSUPPORTED
                ? std::string("tokenizer model not supported (llama.cpp fallback remains the generation backend)")
                : std::string("tokenizer init failed (error code ") +
                  std::to_string(rc) + ")");
    }
    oss << "}";
    return oss.str();
}

std::string op_tokenizer_info(shtn_engine* engine) {
    shtn_tokenizer_info ti{};
    const int32_t rc = shtn_engine_tokenizer_info(engine, &ti);
    if (rc != SHTN_OK) {
        throw std::string("tokenizer_info failed (error code ") +
            std::to_string(rc) + ")";
    }
    return tokenizer_info_json(ti);
}

std::string op_tokenizer_encode(shtn_engine* engine,
                                const std::string& payload_raw,
                                bool has_payload) {
    if (!has_payload) {
        throw std::string("tokenizer_encode requires a payload");
    }

    std::string text;
    int32_t add_bos = 0, add_eos = 0;
    uint32_t max_tokens = 256;
    std::string err;

    if (!shtn::json::extract_encode_payload(payload_raw, text,
                                            add_bos, add_eos, max_tokens, err)) {
        throw std::string("malformed tokenizer_encode payload: ") + err;
    }

    std::vector<uint32_t> ids(max_tokens, 0);

    shtn_encode_options opts{};
    opts.add_bos = add_bos;
    opts.add_eos = add_eos;
    opts.max_tokens = max_tokens;
    opts.reserved = 0;

    shtn_encode_result result{};
    result.ids = ids.data();
    result.ids_count = 0;
    result.truncated = 0;

    const int32_t rc = shtn_engine_tokenizer_encode(
        engine, text.c_str(), text.size(), &opts, &result);

    if (rc != SHTN_OK) {
        throw std::string("tokenizer_encode failed (error code ") +
            std::to_string(rc) + ")";
    }

    std::ostringstream oss;
    oss << "{"
        << "\"ids\":[";
    for (uint32_t i = 0; i < result.ids_count; ++i) {
        if (i > 0) oss << ",";
        oss << result.ids[i];
    }
    oss << "]"
        << ",\"count\":" << result.ids_count
        << ",\"truncated\":" << (result.truncated ? "true" : "false")
        << "}";
    return oss.str();
}

std::string op_tokenizer_decode(shtn_engine* engine,
                                const std::string& payload_raw,
                                bool has_payload) {
    if (!has_payload) {
        throw std::string("tokenizer_decode requires a payload");
    }

    std::vector<uint32_t> ids;
    int32_t skip_special = 1;
    uint32_t max_bytes = 1u << 20;
    std::string err;

    if (!shtn::json::extract_decode_payload(payload_raw, ids,
                                            skip_special, max_bytes, err)) {
        throw std::string("malformed tokenizer_decode payload: ") + err;
    }

    std::vector<char> text(max_bytes, 0);

    shtn_decode_options opts{};
    opts.skip_special = skip_special;
    opts.max_bytes = max_bytes;
    opts.reserved = 0;

    shtn_decode_result result{};
    result.text = text.data();
    result.text_count = 0;
    result.truncated = 0;

    const int32_t rc = shtn_engine_tokenizer_decode(
        engine, ids.empty() ? nullptr : ids.data(), ids.size(), &opts, &result);

    if (rc != SHTN_OK) {
        throw std::string("tokenizer_decode failed (error code ") +
            std::to_string(rc) + ")";
    }

    std::ostringstream oss;
    oss << "{"
        << "\"text\":" << quote(std::string(text.data(), result.text_count))
        << ",\"count\":" << result.text_count
        << ",\"truncated\":" << (result.truncated ? "true" : "false")
        << "}";
    return oss.str();
}

std::string op_kv_cache_info(shtn_engine* engine) {
    shtn_kv_cache_info info{};
    const int32_t rc = shtn_engine_kv_cache_info(engine, &info);
    if (rc != SHTN_OK) {
        throw std::string("kv_cache_info failed (error code ") +
            std::to_string(rc) + ")";
    }

    std::ostringstream oss;
    oss << "{"
        << "\"allocated\":" << (info.allocated ? "true" : "false")
        << ",\"quantization\":" << quote(info.quantization)
        << ",\"capacityBytes\":" << json_uint(info.capacity_bytes)
        << ",\"usedBytes\":" << json_uint(info.used_bytes)
        << ",\"capacityPositions\":" << json_uint(info.capacity_positions)
        << ",\"usedPositions\":" << json_uint(info.used_positions)
        << ",\"layerCount\":" << info.layer_count
        << ",\"kvDim\":" << info.kv_dim
        << "}";
    return oss.str();
}

std::string op_scheduler_info(shtn_engine* engine) {
    shtn_scheduler_info info{};
    const int32_t rc = shtn_engine_scheduler_info(engine, &info);
    if (rc != SHTN_OK) {
        throw std::string("scheduler_info failed (error code ") +
            std::to_string(rc) + ")";
    }

    std::ostringstream oss;
    oss << "{"
        << "\"activeRequests\":" << info.active_requests
        << ",\"queuedRequests\":" << info.queued_requests
        << ",\"maxConcurrent\":" << info.max_concurrent
        << ",\"queueDepthLimit\":" << info.queue_depth_limit
        << ",\"totalSubmitted\":" << json_uint(info.total_submitted)
        << ",\"totalCompleted\":" << json_uint(info.total_completed)
        << ",\"totalCancelled\":" << json_uint(info.total_cancelled)
        << ",\"totalFailed\":" << json_uint(info.total_failed)
        << ",\"shuttingDown\":" << (info.shutting_down ? "true" : "false")
        << "}";
    return oss.str();
}

// --- dispatch ----------------------------------------------------------------

// handle_request processes one parsed request and returns the RESULT JSON
// (success) or throws a std::string error message (failure). The generate
// op is special: it schedules a lane and returns an EMPTY string (the
// caller detects this and skips writing a direct response — the lane owns
// the frames for this id).
std::string handle_request(HostCtx* ctx, const shtn::json::Parsed& req) {
    shtn_engine* engine = ctx->engine;
    if (req.op == "ping") {
        return op_ping();
    }
    if (req.op == "health") {
        return op_health(engine);
    }
    if (req.op == "hwinfo") {
        return op_hardware(engine);
    }
    if (req.op == "metrics") {
        return op_metrics(engine);
    }
    if (req.op == "cancel") {
        return op_cancel(ctx, req.payload_raw, req.has_payload);
    }
    if (req.op == "generate") {
        op_generate(ctx, req, req.payload_raw, req.has_payload);
        return {}; // the lane writes the event + final frames
    }
    if (req.op == "load_model") {
        return op_load_model(engine, req.payload_raw, req.has_payload);
    }
    if (req.op == "unload_model") {
        return op_unload_model(engine);
    }
    if (req.op == "model_info") {
        return op_model_info(engine);
    }
    if (req.op == "tokenizer_init") {
        return op_tokenizer_init(engine);
    }
    if (req.op == "tokenizer_info") {
        return op_tokenizer_info(engine);
    }
    if (req.op == "tokenizer_encode") {
        return op_tokenizer_encode(engine, req.payload_raw, req.has_payload);
    }
    if (req.op == "tokenizer_decode") {
        return op_tokenizer_decode(engine, req.payload_raw, req.has_payload);
    }
    if (req.op == "kv_cache_info") {
        return op_kv_cache_info(engine);
    }
    if (req.op == "scheduler_info") {
        return op_scheduler_info(engine);
    }
    if (req.op == "shutdown") {
        // Acknowledged by the caller specially (respond, then exit).
        return "{}";
    }

    throw std::string("unknown op");
}

// error_response builds the bounded failure frame for a request id.
std::string error_response(int64_t id, const std::string& message) {
    std::ostringstream oss;
    oss << "{"
        << "\"id\":" << id
        << ",\"ok\":false"
        << ",\"error\":" << quote(message)
        << "}";
    return oss.str();
}

// success_response wraps a result payload.
std::string success_response(int64_t id, const std::string& result) {
    std::ostringstream oss;
    oss << "{"
        << "\"id\":" << id
        << ",\"ok\":true"
        << ",\"result\":" << result
        << "}";
    return oss.str();
}

// host_loop is the testable core: read frames from in, write response
// frames to out, until EOF or shutdown. Returns the process exit code.
int host_loop(std::istream& in, std::ostream& out, shtn_engine* engine) {
    HostCtx ctx;
    ctx.engine = engine;
    ctx.out = &out;

    for (;;) {
        std::string payload;

        switch (shtn::protocol::read_frame(in, payload)) {
        case shtn::protocol::FrameStatus::Ok:
            break;
        case shtn::protocol::FrameStatus::Eof:
            // Clean close: cancel in-flight generation, wait for the
            // lanes (bounded — cancellation unblocks them), then exit.
            join_lanes(&ctx);
            return 0;
        case shtn::protocol::FrameStatus::TooLarge:
            // Cannot correlate to an id (frame unreadable): log and exit —
            // the supervisor restarts us. Cancel lanes first so nothing
            // outlives the streams.
            std::fprintf(stderr, "[shtn-host] protocol violation: frame exceeds cap\n");
            join_lanes(&ctx);
            return 2;
        case shtn::protocol::FrameStatus::Truncated:
            std::fprintf(stderr, "[shtn-host] protocol violation: truncated frame\n");
            join_lanes(&ctx);
            return 2;
        case shtn::protocol::FrameStatus::Invalid:
            std::fprintf(stderr, "[shtn-host] protocol violation: invalid frame\n");
            join_lanes(&ctx);
            return 2;
        }

        const shtn::json::Parsed parsed = shtn::json::parse_request(payload);

        if (!parsed.valid) {
            // Malformed request: bounded error response, keep serving.
            const std::string resp =
                error_response(0, "malformed request: " + parsed.error);
            std::lock_guard<std::mutex> lock(ctx.out_mu);
            shtn::protocol::write_frame(out, resp);
            continue;
        }

        try {
            const std::string result = handle_request(&ctx, parsed);

            if (parsed.op == "shutdown") {
                // Respond first, then cancel lanes and exit cleanly.
                {
                    std::lock_guard<std::mutex> lock(ctx.out_mu);
                    shtn::protocol::write_frame(
                        out, success_response(parsed.id, result));
                }
                join_lanes(&ctx);
                return 0;
            }

            if (!result.empty()) {
                std::lock_guard<std::mutex> lock(ctx.out_mu);
                shtn::protocol::write_frame(
                    out, success_response(parsed.id, result));
            }
            // An empty result means the op wrote its own frames (generate)
            // or owns them via a lane — nothing to write here.
        } catch (const std::string& err) {
            const std::string resp = error_response(parsed.id, err);
            std::lock_guard<std::mutex> lock(ctx.out_mu);
            shtn::protocol::write_frame(out, resp);
        }

        // Retire finished generation lanes after every dispatched frame so
        // the lane list tracks in-flight work (bounded by kMaxLanes as a
        // CONCURRENCY bound, not a lifetime total).
        sweep_finished_lanes(&ctx);
    }
}

} // namespace

// Exposed for tests (tests/test_host.cpp).
int shtn_host_run(std::istream& in, std::ostream& out) {
    shtn_engine* engine = nullptr;

    shtn_engine_options opts{};
    opts.abi_version = SHTN_ABI_VERSION;
    opts.reserved = 0;

    const int32_t rc = shtn_engine_create(&opts, &engine);

    if (rc != SHTN_OK || engine == nullptr) {
        std::fprintf(stderr, "[shtn-host] engine create failed: %d\n", rc);
        return 1;
    }

    const int exit_code = host_loop(in, out, engine);

    shtn_engine_destroy(engine);

    return exit_code;
}

#ifndef SHTN_HOST_NO_MAIN

int main() {
#if defined(_WIN32)
    // Binary-safe stdio on Windows.
    _setmode(_fileno(stdin), _O_BINARY);
    _setmode(_fileno(stdout), _O_BINARY);
    _setmode(_fileno(stderr), _O_BINARY);
#endif

    std::ios::sync_with_stdio(false);

    std::cerr << "[shtn-host] "
              << "engine=" << SHTN_ENGINE_NAME
              << " abi=" << SHTN_ABI_VERSION
              << " protocol=" << SHTN_PROTOCOL_VERSION
              << " (phase 5: lifecycle, health, hardware, metrics, GGUF model"
                 " loading, tokenizer, KV cache, scheduler, REAL native"
                 " generation with streaming + cancellation)"
              << std::endl;

    return shtn_host_run(std::cin, std::cout);
}

#endif /* SHTN_HOST_NO_MAIN */
