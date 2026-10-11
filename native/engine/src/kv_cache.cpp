// kv_cache.cpp — native engine KV cache implementation (Phase 5).
//
// Phase 5 correction: TRUE fp16 storage. The buffer is a uint16_t[] of
// IEEE 754 binary16 bit patterns; capacity_bytes is the exact allocation
// size; per-layer offsets are element-accurate. See kv_cache.h for the
// full Phase 4 defect description.

#include "kv_cache.h"

#include "shtn/engine.h"
#include "gguf.h"
#include "util.h"

namespace shtn {
namespace kv {

Cache::~Cache() {
    release();
}

Cache::Cache(Cache&& other) noexcept
    : layout_(other.layout_),
      buffers_(std::move(other.buffers_)),
      layer_elems_(other.layer_elems_),
      total_elems_(other.total_elems_),
      total_bytes_(other.total_bytes_),
      used_positions_(other.used_positions_) {
    other.layout_ = Layout{};
    other.layer_elems_ = 0;
    other.total_elems_ = 0;
    other.total_bytes_ = 0;
    other.used_positions_ = 0;
}

Cache& Cache::operator=(Cache&& other) noexcept {
    if (this != &other) {
        release();
        layout_ = other.layout_;
        buffers_ = std::move(other.buffers_);
        layer_elems_ = other.layer_elems_;
        total_elems_ = other.total_elems_;
        total_bytes_ = other.total_bytes_;
        used_positions_ = other.used_positions_;
        other.layout_ = Layout{};
        other.layer_elems_ = 0;
        other.total_elems_ = 0;
        other.total_bytes_ = 0;
        other.used_positions_ = 0;
    }
    return *this;
}

int32_t Cache::allocate(const Layout& layout, uint64_t available_ram_bytes,
                        std::string& error) {
    release();
    layout_ = Layout{};

    if (!layout.valid()) {
        error = "kv: layout is invalid (zero dimension)";
        return SHTN_ERR_INVALID_ARG;
    }

    if (layout.context_length > kMaxContextLength) {
        error = "kv: context_length " + std::to_string(layout.context_length) +
                " exceeds cap " + std::to_string(kMaxContextLength);
        return SHTN_ERR_UNSUPPORTED;
    }

    // Elements per layer per K (or V) block: context * kv_dim.
    uint64_t layer_elems = 0;
    if (!gguf::checked_mul_u64(layout.context_length, layout.kv_dim,
                               layer_elems)) {
        error = "kv: per-layer element count overflow";
        return SHTN_ERR_UNSUPPORTED;
    }

    // Total uint16 elements: 2 (K+V) * layers * layer_elems.
    uint64_t total_elems = 0;
    if (!gguf::checked_mul_u64(layer_elems, layout.layer_count, total_elems) ||
        !gguf::checked_mul_u64(total_elems, 2, total_elems)) {
        error = "kv: total element count overflow";
        return SHTN_ERR_UNSUPPORTED;
    }

    // Actual allocation size in bytes: total_elems * sizeof(uint16_t).
    // This IS capacity_bytes — one identity the regression tests pin.
    uint64_t total_bytes = 0;
    if (!gguf::checked_mul_u64(total_elems, sizeof(uint16_t), total_bytes)) {
        error = "kv: total byte count overflow";
        return SHTN_ERR_UNSUPPORTED;
    }

    if (total_bytes > kMaxKVBytes) {
        error = "kv: total " + std::to_string(total_bytes) + " exceeds cap " +
                std::to_string(kMaxKVBytes);
        return SHTN_ERR_UNSUPPORTED;
    }

    if (available_ram_bytes > 0 && total_bytes > available_ram_bytes) {
        error = "kv: total " + std::to_string(total_bytes) +
                " exceeds available RAM " + std::to_string(available_ram_bytes);
        return SHTN_ERR_UNSUPPORTED;
    }

    // Guard the element count against a hostile layout before narrowing to
    // size_t for the array new.
    if (total_elems > SIZE_MAX / sizeof(uint16_t) ||
        total_elems > static_cast<uint64_t>(1) << 40) {
        error = "kv: implausible element count " +
                std::to_string(total_elems);
        return SHTN_ERR_UNSUPPORTED;
    }

    // The REAL fp16 allocation: one uint16 per element, zero-initialized
    // (deterministic first-read behaviour for tests; generation always
    // writes before reading anyway).
    buffers_ = std::unique_ptr<uint16_t[]>(
        new (std::nothrow) uint16_t[static_cast<size_t>(total_elems)]());
    if (buffers_ == nullptr) {
        error = "kv: allocation of " + std::to_string(total_bytes) +
                " bytes failed";
        return SHTN_ERR_INTERNAL;
    }

    layout_ = layout;
    layer_elems_ = layer_elems;
    total_elems_ = total_elems;
    total_bytes_ = total_bytes;
    used_positions_ = 0;

    return SHTN_OK;
}

void Cache::release() {
    buffers_.reset();
    layout_ = Layout{};
    layer_elems_ = 0;
    total_elems_ = 0;
    total_bytes_ = 0;
    used_positions_ = 0;
}

void Cache::reset() {
    used_positions_ = 0;
    // Deliberately NO buffer clearing: positions are overwritten before
    // they are read by the forward pass, so zeroing the whole allocation
    // between requests would be wasted work (pinned by a test that reads
    // back written values after reset()).
}

void Cache::advance(uint64_t n) {
    if (!allocated()) return;
    uint64_t next = 0;
    if (!gguf::checked_add_u64(used_positions_, n, next) ||
        next > layout_.context_length) {
        next = layout_.context_length;
    }
    used_positions_ = next;
}

Stats Cache::stats() const {
    Stats s;
    s.allocated = allocated();
    s.capacity_bytes = total_bytes_;
    s.capacity_positions = layout_.context_length;
    s.used_positions = used_positions_;
    s.layer_count = layout_.layer_count;
    s.kv_dim = layout_.kv_dim;
    if (allocated()) {
        s.quantization = "f16";
        // used_bytes: positions actually written, times the per-position
        // K+V footprint across all layers. Mathematically consistent with
        // the write positions (pinned by regression tests).
        s.used_bytes = used_positions_ * bytes_per_position();
    }
    return s;
}

uint16_t* Cache::k_layer(uint32_t layer) const {
    if (!allocated() || layer >= layout_.layer_count) return nullptr;
    return buffers_.get() + static_cast<size_t>(layer) * layer_elems_;
}

uint16_t* Cache::v_layer(uint32_t layer) const {
    if (!allocated() || layer >= layout_.layer_count) return nullptr;
    const uint64_t k_total = layer_elems_ * layout_.layer_count;
    return buffers_.get() + static_cast<size_t>(k_total) +
           static_cast<size_t>(layer) * layer_elems_;
}

uint16_t* Cache::k_at(uint32_t layer, uint64_t pos) const {
    if (!allocated() || layer >= layout_.layer_count ||
        pos >= layout_.context_length) {
        return nullptr;
    }
    return k_layer(layer) + static_cast<size_t>(pos) * layout_.kv_dim;
}

uint16_t* Cache::v_at(uint32_t layer, uint64_t pos) const {
    if (!allocated() || layer >= layout_.layer_count ||
        pos >= layout_.context_length) {
        return nullptr;
    }
    return v_layer(layer) + static_cast<size_t>(pos) * layout_.kv_dim;
}

} // namespace kv
} // namespace shtn
