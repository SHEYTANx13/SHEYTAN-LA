// model.h — the native engine's model concern (Phase 2).
//
// Owns the model lifecycle: unloaded → loading → loaded | failed →
// unloaded. A "load" validates the GGUF file, memory-maps it (lazy —
// tensor data is NOT copied into RAM) and computes the memory plan; an
// "unload" releases the mapping and every cached fact. No inference, no
// KV-cache allocation, no workspace allocation — those are later phases;
// the plan below is arithmetic on metadata only.

#ifndef SHTN_MODEL_H
#define SHTN_MODEL_H

#include "shtn/types.h"

#include "gguf.h"
#include "llama.h"
#include "tensor.h"
#include "tokenizer.h"

#include <mutex>
#include <string>

namespace shtn {
namespace model {

// Fixed engine bookkeeping allowance factored into the memory plan:
// thread stacks, allocator arenas, mmap page-table pressure. It is a
// documented planning constant — nothing is pre-allocated for it.
constexpr uint64_t kRuntimeOverheadBytes = 64u << 20; // 64 MiB

// Model is the single model slot of one engine instance. Every public
// call is thread-safe (internal mutex).
class Model {
public:
    Model() = default;
    ~Model() = default;

    Model(const Model&) = delete;
    Model& operator=(const Model&) = delete;

    // Load `path` (GGUF): validate, map, extract metadata, plan. Replace
    // semantics: a currently loaded model is unloaded first, and a
    // failed load leaves NOTHING loaded. Returns SHTN_OK, or a negative
    // error code with `error` filled for the caller's response.
    int32_t load(const std::string& path,
                 const shtn_model_load_options& opts, std::string& error);

    // Unload and release everything. Idempotent. Returns SHTN_OK.
    int32_t unload();

    // Snapshot into the ABI structs. Never fails (NULL out is guarded by
    // the caller layer); reflects the current state exactly.
    void fill_info(shtn_model_info* out) const;
    void fill_plan(shtn_memory_plan* out) const;

    // Current state string (SHTN_MODEL_STATE_*).
    std::string state() const;

    // --- Phase 4: tokenizer concern -------------------------------------
    // init_tokenizer materializes the GGUF tokenizer arrays into the
    // owned Vocab. Returns SHTN_OK, or a negative error code with
    // `error` filled (SHTN_ERR_UNSUPPORTED is the common case for an
    // unimplemented tokenizer model — the host reports it and the
    // llama.cpp fallback remains the generation backend).
    int32_t init_tokenizer(std::string& error);

    // tokenizer_access returns a const pointer to the materialized vocab
    // (nullptr when not initialized). The pointer is valid until the
    // next model unload or model destroy; callers must not retain it
    // across those boundaries.
    const tokenizer::Vocab* tokenizer_vocab() const;

    // tokenizer_initialized reports whether init_tokenizer has succeeded
    // for the current model.
    bool tokenizer_initialized() const;

private:
    // Guards every field below. `load` holds the lock for the whole
    // attempt: loads are serialized (a load in flight rejects a second
    // load with SHTN_ERR_MODEL_STATE), while inspections may run
    // concurrently against the last stable snapshot.
    mutable std::mutex mu_;

    std::string state_ = SHTN_MODEL_STATE_UNLOADED;
    std::string error_;        // last load failure detail
    std::string path_;

    gguf::MappedFile mapping_; // owns the mmap + fd
    gguf::GgufHeader header_;  // parsed + validated header facts

    shtn_memory_plan plan_{};  // last computed plan (0s before load)

    // Phase 5: llama graph binding (weights view + hyper + capability),
    // rebuilt on every successful load (the header is replaced, so the
    // view must be rebuilt with it).
    tensor::Weights weights_;
    llama::Hyper hyper_{};
    bool generation_capable_ = false;
    std::string generation_reason_;

    // epoch increments on every successful load; the generation runner
    // rebinds its KV/scratch when it changes.
    uint64_t epoch_ = 0;

    // Available RAM measured at load time (0 = unknown) — bounds the KV
    // cache allocation at generation time.
    uint64_t available_ram_ = 0;

    // Phase 4: materialized tokenizer vocab (empty until init_tokenizer
    // succeeds; cleared on unload). Owned here so its lifetime is bound
    // to the model, not the host process.
    tokenizer::Vocab vocab_;
    bool vocab_initialized_ = false;

public:
    // --- Phase 5 accessors (stable under the model mutex) -----------------

    // epoch identifies the current load generation.
    uint64_t epoch() const;

    // weights returns the tensor access view bound to the CURRENT
    // mapping (nullptr when nothing is loaded).
    const tensor::Weights* weights() const;

    // hyper returns the derived llama hyper parameters (valid only when
    // generation_capable()).
    llama::Hyper hyper() const;

    // generation_capable: the load-time native-inference verdict.
    bool generation_capable() const;

    // generation_reason names WHY the model is not natively executable
    // (empty when capable).
    std::string generation_reason() const;

    // available_ram_bytes measured at load time (0 = unknown).
    uint64_t available_ram_bytes() const;
};

} // namespace model
} // namespace shtn

#endif /* SHTN_MODEL_H */
