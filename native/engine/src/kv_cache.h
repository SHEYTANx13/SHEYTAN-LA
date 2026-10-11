// kv_cache.h — the SHEYTAN native engine KV cache (Phase 4 → Phase 5).
//
// A real K/V cache sized from the model's actual dimensions
// (layer_count, kv_head_count, head_dim, context_length). The cache stores
// per-layer K and V tensors as contiguous 16-bit float buffers, indexed by
// position [0, capacity).
//
// Phase 5 CORRECTION (the Phase 4 defect): the storage is now genuinely
// 16-bit — a uint16_t array holding IEEE 754 binary16 bit patterns (see
// fp16.h). Before Phase 5 the buffer was a float[] (4 bytes/element) while
// the code documented and accounted for fp16 (2 bytes/element): the actual
// allocation was exactly 2x the reported capacity_bytes and the per-layer
// pointer math used sizeof(float) against a byte count computed for 2-byte
// elements, so adjacent layers overlapped. The physical representation,
// the reported capacity and the layer offsets are now identical by
// construction and pinned by regression tests that compare expected bytes
// vs actual allocation size vs reported capacity_bytes.
//
// What this is:
//   - real allocations, sized from real model dims;
//   - TRUE fp16 physical storage (2 bytes per element, uint16_t bits);
//   - explicit capacity (context length);
//   - measured bytes (returned to the host and reported through metrics)
//     where capacity_bytes == the actual allocation size, always;
//   - used_bytes == positions written * bytes per position (K+V, all
//     layers) — mathematically consistent with the write positions;
//   - reset() marks positions unused WITHOUT clearing memory (the forward
//     pass overwrites positions before reading them — full-buffer clearing
//     between requests is wasted work);
//   - explicit ownership: the cache OWNS its buffers and frees them on
//     destruction (RAII; move-only).
//
// What this is NOT:
//   - this is NOT a paged cache (no virtual memory mapping);
//   - this is NOT a quantized cache (f16 only in this phase);
//   - this is NOT thread-safe by itself: the single-slot generation
//     scheduler serializes all writers; info readers take the engine's
//     stats lock (the forward pass writes from the scheduler worker only).
//
// SECURITY / BOUNDS:
//   - every dimension comes from the parsed GGUF header (already
//     validated by gguf.cpp);
//   - capacity is bounded by kMaxContextLength (1M positions);
//   - total bytes is checked against kMaxKVBytes (16 GiB) and against
//     the available RAM passed in by the host;
//   - allocation failure returns SHTN_ERR_INTERNAL with a clear error
//     (never a nullptr dereference);
//   - every position write is bounds-checked against the capacity.

#ifndef SHTN_KV_CACHE_H
#define SHTN_KV_CACHE_H

#include "shtn/types.h"

#include <cstdint>
#include <memory>
#include <string>

namespace shtn {
namespace kv {

// Hard bounds (defensive — real models are far below these).
constexpr uint64_t kMaxContextLength = 1u << 20;  // 1M positions
constexpr uint64_t kMaxKVBytes       = 16ull << 30; // 16 GiB total

// Layout describes the per-layer K/V tensor shape derived from the model.
struct Layout {
    uint32_t layer_count = 0;       // <arch>.block_count
    uint32_t embedding_length = 0;  // <arch>.embedding_length
    uint32_t head_count = 0;        // <arch>.attention.head_count
    uint32_t head_dim = 0;          // derived: embedding/head_count or key_length
    uint32_t kv_head_count = 0;     // <arch>.attention.head_count_kv (GQA)
    uint32_t kv_dim = 0;            // kv_head_count * head_dim
    uint64_t context_length = 0;    // effective context (planned)

    bool valid() const {
        return layer_count > 0 && embedding_length > 0 &&
               head_count > 0 && head_dim > 0 && kv_head_count > 0 &&
               kv_dim > 0 && context_length > 0;
    }
};

// Stats is the measurable snapshot reported through the metrics op.
// Every field is a real measured value — never a guess.
struct Stats {
    bool allocated = false;          // true after a successful alloc
    uint64_t capacity_bytes = 0;     // K + V total allocated bytes (actual)
    uint64_t used_bytes = 0;         // bytes for positions actually written
    uint64_t capacity_positions = 0; // context_length
    uint64_t used_positions = 0;     // positions written this request
    uint32_t layer_count = 0;
    uint32_t kv_dim = 0;
    std::string quantization;        // "f16"
};

// Cache is the owned KV cache of one model. Not copyable; move-only.
class Cache {
public:
    Cache() = default;
    ~Cache();

    Cache(const Cache&) = delete;
    Cache& operator=(const Cache&) = delete;
    Cache(Cache&& other) noexcept;
    Cache& operator=(Cache&& other) noexcept;

    // Allocate the K and V buffers from the given layout. Returns:
    //   SHTN_OK              allocated (capacity_bytes == allocation size);
    //   SHTN_ERR_INVALID_ARG layout invalid;
    //   SHTN_ERR_UNSUPPORTED layout exceeds kMaxContextLength or
    //                         kMaxKVBytes, or exceeds available_ram;
    //   SHTN_ERR_INTERNAL    allocation failed.
    //
    // available_ram_bytes is the host's view of free RAM (0 = unknown;
    // the check is skipped when unknown).
    int32_t allocate(const Layout& layout, uint64_t available_ram_bytes,
                     std::string& error);

    // Release the buffers. Idempotent.
    void release();

    // Reset marks all positions as unused (used_positions_ = 0). The
    // buffers stay allocated; this is the per-request-boundary reset.
    // It does NOT clear memory — the forward pass overwrites each
    // position before reading it, and it only reads positions < used.
    void reset();

    // advance records that `n` positions have been written (overflow-safe,
    // clamped to capacity). Used by the forward pass after each decode step.
    void advance(uint64_t n);

    // Stats returns the current measurable snapshot.
    Stats stats() const;

    // --- accessors used by the transformer forward pass ------------------
    //
    // The K and V blocks are laid out as:
    //   K: layer-major — layer l occupies elements [l*ctx_elems,
    //      (l+1)*ctx_elems) where ctx_elems = context_length * kv_dim;
    //      within a layer, position p occupies [p*kv_dim, (p+1)*kv_dim);
    //   V: same shape, in a contiguous block after all K layers.
    //
    // Each element is ONE uint16_t = one IEEE 754 binary16 value. The
    // forward pass converts through fp16.h when writing/reading.
    //
    // Returns nullptr when not allocated or layer is out of range.
    uint16_t* k_layer(uint32_t layer) const;
    uint16_t* v_layer(uint32_t layer) const;

    // Pointer to the first element of position `pos` within layer
    // `layer` of the K (or V) block. Bounds-checked: pos must be <
    // context_length, layer < layer_count; nullptr otherwise.
    uint16_t* k_at(uint32_t layer, uint64_t pos) const;
    uint16_t* v_at(uint32_t layer, uint64_t pos) const;

    bool allocated() const { return buffers_ != nullptr; }
    uint64_t capacity_positions() const { return layout_.context_length; }
    uint64_t used_positions() const { return used_positions_; }
    uint64_t layer_elem_count() const { return layer_elems_; }
    const Layout& layout() const { return layout_; }

    // bytes_per_position is the K+V footprint of ONE position across ALL
    // layers (used for consistent used_bytes accounting).
    uint64_t bytes_per_position() const {
        if (layer_elems_ == 0) return 0;
        return 2ull /* K+V */ * layout_.layer_count * layout_.kv_dim *
               sizeof(uint16_t);
    }

private:
    Layout layout_{};
    // One contiguous allocation holding all layers' K then all layers' V.
    // Each element is one uint16_t = one fp16 value. The allocation size
    // in bytes IS total_bytes_ == stats().capacity_bytes — pinned by tests.
    std::unique_ptr<uint16_t[]> buffers_;
    uint64_t layer_elems_ = 0;   // elements per layer per K or V block
    uint64_t total_elems_ = 0;   // total uint16 elements (K and V)
    uint64_t total_bytes_ = 0;   // capacity_bytes == allocation size
    uint64_t used_positions_ = 0;
};

} // namespace kv
} // namespace shtn

#endif /* SHTN_KV_CACHE_H */
