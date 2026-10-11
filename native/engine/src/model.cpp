// model.cpp — model concern implementation (Phase 2).

#include "model.h"

#include "shtn/engine.h"

#include "hardware.h"
#include "util.h"

namespace shtn {
namespace model {

namespace {

// extract_metadata pulls the model facts the engine reports, reading
// ONLY values actually present in the header (missing keys stay 0).
struct ModelFacts {
    std::string architecture;
    std::string name;
    uint64_t context_length = 0;
    uint64_t vocab_size = 0;
    uint64_t embedding_length = 0;
    uint64_t layer_count = 0;
    uint64_t parameter_count = 0; // explicit general.parameter_count
};

ModelFacts extract_metadata(const gguf::Metadata& md) {
    ModelFacts f;

    if (const gguf::Scalar* s = gguf::find_scalar(md, "general.architecture")) {
        gguf::scalar_str(*s, f.architecture);
    }
    if (const gguf::Scalar* s = gguf::find_scalar(md, "general.name")) {
        gguf::scalar_str(*s, f.name);
    }
    if (const gguf::Scalar* s =
            gguf::find_scalar(md, "general.parameter_count")) {
        gguf::scalar_u64(*s, f.parameter_count);
    }

    const std::string& arch = f.architecture;
    const std::string prefix = arch + ".";

    auto lookup_u64 = [&](const std::string& key, uint64_t& out) {
        if (const gguf::Scalar* s = gguf::find_scalar(md, key)) {
            gguf::scalar_u64(*s, out);
        }
    };

    if (!arch.empty()) {
        lookup_u64(prefix + "context_length", f.context_length);
        lookup_u64(prefix + "embedding_length", f.embedding_length);
        lookup_u64(prefix + "block_count", f.layer_count);

        // Vocabulary size: the architecture scalar wins; the tokenizer
        // token array length is the honest fallback when the scalar is
        // absent (the array is never materialized).
        lookup_u64(prefix + "vocab_size", f.vocab_size);
        if (f.vocab_size == 0) {
            if (const gguf::Scalar* s = gguf::find_scalar(
                    md, "tokenizer.ggml.tokens")) {
                if (s->type == gguf::kTypeArray) {
                    f.vocab_size = s->array_count;
                }
            }
        }
    }

    return f;
}

// compute_plan derives the memory budget from parsed facts ONLY —
// nothing here allocates. Overflow at any step zeroes that component
// (an unknown component is an honest 0, never a guess).
shtn_memory_plan compute_plan(const gguf::GgufHeader& h,
                              const ModelFacts& f,
                              uint64_t file_size,
                              uint64_t effective_context) {
    shtn_memory_plan p{};
    p.model_file_bytes = file_size;
    p.mapped_bytes = file_size; // whole-file read-only mapping
    p.weights_bytes = h.data_bytes;

    // KV cache: 2 (K+V) * layers * context * embedding * 2 bytes (f16).
    // Only computed when the model provides every input.
    if (f.layer_count > 0 && effective_context > 0 &&
        f.embedding_length > 0) {
        uint64_t kv = 0;
        if (gguf::checked_mul_u64(f.layer_count, effective_context, kv) &&
            gguf::checked_mul_u64(kv, f.embedding_length, kv) &&
            gguf::checked_mul_u64(kv, 4, kv)) { // 2 (K+V) * 2 (f16 bytes)
            p.kv_cache_bytes = kv;
        }
    }

    // Workspace (temporary compute buffers): the dominant derivable term
    // is the float logits row — context * vocabulary * 4 bytes. Requires
    // both metadata values; otherwise honestly 0.
    if (effective_context > 0 && f.vocab_size > 0) {
        uint64_t ws = 0;
        if (gguf::checked_mul_u64(effective_context, f.vocab_size, ws) &&
            gguf::checked_mul_u64(ws, 4, ws)) {
            p.workspace_bytes = ws;
        }
    }

    p.runtime_overhead_bytes = kRuntimeOverheadBytes;

    uint64_t total = 0;
    if (gguf::checked_add_u64(total, p.model_file_bytes, total) &&
        gguf::checked_add_u64(total, p.kv_cache_bytes, total) &&
        gguf::checked_add_u64(total, p.workspace_bytes, total) &&
        gguf::checked_add_u64(total, p.runtime_overhead_bytes, total)) {
        p.total_bytes = total;
    }

    return p;
}

} // namespace

int32_t Model::load(const std::string& path,
                    const shtn_model_load_options& opts, std::string& error) {
    std::lock_guard<std::mutex> lock(mu_);

    if (state_ == SHTN_MODEL_STATE_LOADING) {
        error = "a model load is already in flight";
        return SHTN_ERR_MODEL_STATE;
    }

    // --- validate the request itself -----------------------------------
    // Caller-argument errors reject the load WITHOUT touching the model
    // state: nothing was attempted, so the previous state stands.
    if (path.empty()) {
        error = "model path is empty";
        return SHTN_ERR_INVALID_ARG;
    }

    if (opts.reserved != 0) {
        error = "load options reserved field must be 0";
        return SHTN_ERR_INVALID_ARG;
    }

    // --- begin the attempt -----------------------------------------------
    // Replace semantics: drop whatever is resident (or failed) first so
    // a failed load can never leave a stale model behind. From here on a
    // real load attempt is in flight and any failure walks the model to
    // the "failed" state with the reason recorded.
    mapping_.close();
    header_ = gguf::GgufHeader{};
    plan_ = shtn_memory_plan{};
    error_.clear();
    state_ = SHTN_MODEL_STATE_LOADING;
    path_ = path;

    // --- validate + map the file ----------------------------------------
    if (!mapping_.open(path, error)) {
        state_ = SHTN_MODEL_STATE_FAILED;
        error_ = error;
        return SHTN_ERR_INVALID_ARG;
    }

    // --- parse + validate the GGUF container -----------------------------
    if (!gguf::parse_header(mapping_, header_, error)) {
        mapping_.close();
        state_ = SHTN_MODEL_STATE_FAILED;
        error_ = error;
        return SHTN_ERR_MODEL_FORMAT;
    }

    if (header_.tensor_count == 0) {
        mapping_.close();
        header_ = gguf::GgufHeader{};
        state_ = SHTN_MODEL_STATE_FAILED;
        error_ = "gguf: file contains no tensors (not a model)";
        error = error_;
        return SHTN_ERR_MODEL_FORMAT;
    }

    // --- metadata + memory plan ------------------------------------------
    const ModelFacts facts = extract_metadata(header_.metadata);

    // Effective planning context: caller override, else trained value.
    const uint64_t effective_context =
        opts.context_length > 0 ? opts.context_length : facts.context_length;

    plan_ = compute_plan(header_, facts, mapping_.size(), effective_context);

    // --- Phase 5: native-inference capability (llama graph validation) ---
    // Metadata-level only: derive hyper parameters and validate every
    // required tensor's presence/shape/type. Nothing is allocated and no
    // tensor data is read. A model that fails any check stays LOADED
    // (Phase 2 semantics) but reports generation_capable=0 with the
    // explicit reason — the Go core then selects llama.cpp.
    hyper_ = llama::Hyper{};
    generation_capable_ = false;
    generation_reason_.clear();
    {
        std::string reason;
        if (llama::derive(header_.metadata, hyper_, reason)) {
            if (llama::validate(header_, hyper_, reason)) {
                generation_capable_ = true;
            } else {
                generation_reason_ = reason;
            }
        } else {
            generation_reason_ = reason;
        }
    }

    // Bind the tensor access view to the (new) header + mapping. The
    // previous view pointed at the replaced header — rebuild ALWAYS.
    weights_ = tensor::Weights(header_);
    weights_.bind_data(mapping_.data(), mapping_.size());

    // Measure available RAM once per load (bounds the KV allocation at
    // generation time; 0 = unknown → check skipped).
    {
        shtn_ram_info ram{};
        shtn::detect_ram(ram);
        available_ram_ = ram.available_bytes;
    }

    epoch_ += 1;

    state_ = SHTN_MODEL_STATE_LOADED;
    path_ = path;
    error.clear();

    return SHTN_OK;
}

int32_t Model::unload() {
    std::lock_guard<std::mutex> lock(mu_);

    mapping_.close();
    header_ = gguf::GgufHeader{};
    plan_ = shtn_memory_plan{};
    vocab_ = tokenizer::Vocab{};
    vocab_initialized_ = false;
    weights_ = tensor::Weights{};
    hyper_ = llama::Hyper{};
    generation_capable_ = false;
    generation_reason_.clear();
    available_ram_ = 0;
    error_.clear();
    path_.clear();
    state_ = SHTN_MODEL_STATE_UNLOADED;

    return SHTN_OK;
}

void Model::fill_info(shtn_model_info* out) const {
    if (out == nullptr) {
        return;
    }

    std::lock_guard<std::mutex> lock(mu_);

    *out = shtn_model_info{};

    copy_cstr(out->path, sizeof(out->path), path_);
    copy_cstr(out->state, sizeof(out->state), state_.c_str());
    copy_cstr(out->error, sizeof(out->error), error_);

    out->file_size_bytes = mapping_.size();

    if (header_.version != 0) {
        const ModelFacts facts = extract_metadata(header_.metadata);

        copy_cstr(out->architecture, sizeof(out->architecture),
                  facts.architecture.c_str());
        copy_cstr(out->name, sizeof(out->name), facts.name.c_str());

        out->parameter_count = facts.parameter_count > 0
                                   ? facts.parameter_count
                                   : header_.derived_parameter_count;
        out->context_length = facts.context_length;
        out->vocabulary_size = facts.vocab_size;
        out->embedding_length = facts.embedding_length;
        out->layer_count = facts.layer_count;

        out->tensor_count = static_cast<uint32_t>(header_.tensor_count);
        out->gguf_version = header_.version;
        out->general_file_type = header_.file_type;
        out->has_file_type = header_.has_file_type ? 1 : 0;

        if (out->has_file_type) {
            const std::string q = gguf::file_type_name(header_.file_type);
            copy_cstr(out->quantization, sizeof(out->quantization), q.c_str());
        }

        // Plan summary (the full plan lives on the memory_plan surface).
        out->kv_cache_bytes = plan_.kv_cache_bytes;
        out->workspace_bytes = plan_.workspace_bytes;
        out->total_plan_bytes = plan_.total_bytes;
    }

    // Phase 5: capability verdict + reason (filled whenever a load was
    // attempted — the zero-state stays 0/empty).
    out->generation_capable = generation_capable_ ? 1 : 0;
    copy_cstr(out->generation_reason, sizeof(out->generation_reason),
              generation_reason_.c_str());
}

void Model::fill_plan(shtn_memory_plan* out) const {
    if (out == nullptr) {
        return;
    }

    std::lock_guard<std::mutex> lock(mu_);
    *out = plan_;
}

std::string Model::state() const {
    std::lock_guard<std::mutex> lock(mu_);
    return state_;
}

// --- Phase 4: tokenizer concern -------------------------------------------

int32_t Model::init_tokenizer(std::string& error) {
    std::lock_guard<std::mutex> lock(mu_);

    if (state_ != SHTN_MODEL_STATE_LOADED) {
        error = "tokenizer: no model loaded";
        return SHTN_ERR_NO_MODEL;
    }

    if (vocab_initialized_) {
        // Idempotent: a second call is a no-op (the vocab is already
        // materialized). Caller can re-query tokenizer_info cheaply.
        return SHTN_OK;
    }

    const int32_t rc = tokenizer::init_with_mapping(
        header_, mapping_.data(), mapping_.size(), vocab_, error);

    if (rc != SHTN_OK) {
        // A failed init leaves NO vocab (mirrors load's all-or-nothing).
        vocab_ = tokenizer::Vocab{};
        vocab_initialized_ = false;
        return rc;
    }

    vocab_initialized_ = true;
    return SHTN_OK;
}

const tokenizer::Vocab* Model::tokenizer_vocab() const {
    std::lock_guard<std::mutex> lock(mu_);
    if (!vocab_initialized_) {
        return nullptr;
    }
    return &vocab_;
}

bool Model::tokenizer_initialized() const {
    std::lock_guard<std::mutex> lock(mu_);
    return vocab_initialized_;
}

// --- Phase 5 accessors -------------------------------------------------------

uint64_t Model::epoch() const {
    std::lock_guard<std::mutex> lock(mu_);
    return epoch_;
}

const tensor::Weights* Model::weights() const {
    std::lock_guard<std::mutex> lock(mu_);
    if (state_ != SHTN_MODEL_STATE_LOADED || weights_.find("token_embd.weight") == nullptr) {
        return nullptr;
    }
    // NOTE: the returned pointer is a member address — stable for the
    // object lifetime, but its CONTENTS (header binding) change on
    // load/unload. Callers must re-check epoch() after acquiring it.
    return &weights_;
}

llama::Hyper Model::hyper() const {
    std::lock_guard<std::mutex> lock(mu_);
    return hyper_;
}

bool Model::generation_capable() const {
    std::lock_guard<std::mutex> lock(mu_);
    return generation_capable_;
}

std::string Model::generation_reason() const {
    std::lock_guard<std::mutex> lock(mu_);
    return generation_reason_;
}

uint64_t Model::available_ram_bytes() const {
    std::lock_guard<std::mutex> lock(mu_);
    return available_ram_;
}

} // namespace model
} // namespace shtn
