// tensor.cpp — GGUF tensor data access implementation (Phase 5).

#include "tensor.h"

#include "shtn/engine.h"
#include "fp16.h"

#include <cstring>

namespace shtn {
namespace tensor {

namespace {

inline float load_fp16_le(const uint8_t* p) {
    const uint16_t bits = static_cast<uint16_t>(p[0]) |
                          (static_cast<uint16_t>(p[1]) << 8);
    return fp16::fp16_bits_to_fp32(bits);
}

inline float load_f32_le(const uint8_t* p) {
    const uint32_t raw = static_cast<uint32_t>(p[0]) |
                         (static_cast<uint32_t>(p[1]) << 8) |
                         (static_cast<uint32_t>(p[2]) << 16) |
                         (static_cast<uint32_t>(p[3]) << 24);
    float f = 0.0f;
    std::memcpy(&f, &raw, sizeof(f));
    return f;
}

// nibble_lo returns the 4-bit value at nibble index j (low nibbles are
// elements 0..15, high nibbles elements 16..31) — the ggml packing order.
inline int nibble_at(const uint8_t* qs, uint32_t j) {
    const uint8_t byte = qs[j / 2];
    return (j % 2 == 0) ? (byte & 0x0F) : (byte >> 4);
}

// RowLayout describes the physical row layout of one tensor type.
struct RowLayout {
    uint64_t row_elems_padded; // elements per row incl. block padding
    uint64_t row_bytes;        // bytes per row
    uint32_t block_size;       // elements per block (1 for F32/F16)
    uint32_t block_bytes;      // bytes per block
};

bool row_layout_for(uint32_t type, uint64_t ne0, RowLayout& out) {
    uint32_t block = 0;
    uint32_t block_bytes = 0;
    switch (type) {
    case 0: block = 1;  block_bytes = 4;  break; // F32
    case 1: block = 1;  block_bytes = 2;  break; // F16
    case 2: block = 32; block_bytes = 18; break; // Q4_0
    case 3: block = 32; block_bytes = 20; break; // Q4_1
    case 6: block = 32; block_bytes = 22; break; // Q5_0
    case 7: block = 32; block_bytes = 24; break; // Q5_1
    case 8: block = 32; block_bytes = 34; break; // Q8_0
    default: return false;
    }

    const uint64_t padded = (ne0 + block - 1) / block * block;
    out.row_elems_padded = padded;
    out.block_size = block;
    out.block_bytes = block_bytes;
    out.row_bytes = padded / block * block_bytes;
    return true;
}

} // namespace

bool type_supported(uint32_t ggml_type) {
    switch (ggml_type) {
    case 0: case 1: case 2: case 3: case 6: case 7: case 8:
        return true;
    default:
        return false;
    }
}

const char* type_name(uint32_t ggml_type) {
    switch (ggml_type) {
    case 0:  return "F32";
    case 1:  return "F16";
    case 2:  return "Q4_0";
    case 3:  return "Q4_1";
    case 6:  return "Q5_0";
    case 7:  return "Q5_1";
    case 8:  return "Q8_0";
    default: return "unsupported";
    }
}

bool dequant_block(uint32_t ggml_type, const uint8_t* src, float* out) {
    switch (ggml_type) {
    case 0: // F32
        out[0] = load_f32_le(src);
        return true;

    case 1: // F16
        out[0] = load_fp16_le(src);
        return true;

    case 2: { // Q4_0 (18 bytes): v = (q4 - 8) * d
        const float d = load_fp16_le(src);
        for (uint32_t j = 0; j < 32; ++j) {
            const int q = nibble_at(src + 2, j);
            out[j] = static_cast<float>(q - 8) * d;
        }
        return true;
    }

    case 3: { // Q4_1 (20 bytes): v = q4 * d + m
        const float d = load_fp16_le(src);
        const float m = load_fp16_le(src + 2);
        for (uint32_t j = 0; j < 32; ++j) {
            const int q = nibble_at(src + 4, j);
            out[j] = static_cast<float>(q) * d + m;
        }
        return true;
    }

    case 6: { // Q5_0 (22 bytes): v = (q5 - 16) * d, q5 = 16*hi + lo
        const float d = load_fp16_le(src);
        for (uint32_t j = 0; j < 32; ++j) {
            const int hi = (src[2 + j / 8] >> (j % 8)) & 0x01;
            const int lo = nibble_at(src + 6, j);
            const int q = hi * 16 + lo;
            out[j] = static_cast<float>(q - 16) * d;
        }
        return true;
    }

    case 7: { // Q5_1 (24 bytes): v = q5 * d + m
        const float d = load_fp16_le(src);
        const float m = load_fp16_le(src + 2);
        for (uint32_t j = 0; j < 32; ++j) {
            const int hi = (src[4 + j / 8] >> (j % 8)) & 0x01;
            const int lo = nibble_at(src + 8, j);
            const int q = hi * 16 + lo;
            out[j] = static_cast<float>(q) * d + m;
        }
        return true;
    }

    case 8: { // Q8_0 (34 bytes): v = q8 * d
        const float d = load_fp16_le(src);
        for (uint32_t j = 0; j < 32; ++j) {
            out[j] = static_cast<float>(static_cast<int8_t>(src[2 + j])) * d;
        }
        return true;
    }

    default:
        return false;
    }
}

void f16_row_to_f32(const uint16_t* src, float* dst, uint32_t count) {
    for (uint32_t i = 0; i < count; ++i) {
        dst[i] = fp16::fp16_bits_to_fp32(src[i]);
    }
}

// --- Weights ----------------------------------------------------------------

Weights::Weights(const gguf::GgufHeader& header) {
    header_ = &header;
    by_name_.reserve(header.tensors.size());
    for (uint32_t i = 0; i < static_cast<uint32_t>(header.tensors.size());
         ++i) {
        by_name_.emplace(header.tensors[i].name, i);
    }
    data_ = nullptr;
    data_size_ = 0;
}

void Weights::bind_data(const uint8_t* file_base, uint64_t file_size) {
    // The data section starts at header.data_start (already validated to
    // be <= file_size by the parser). bind_data is only called by the
    // model owner after a successful parse of the SAME header.
    if (header_ == nullptr || file_base == nullptr) {
        data_ = nullptr;
        data_size_ = 0;
        return;
    }
    data_ = file_base + header_->data_start;
    data_size_ = file_size > header_->data_start
                     ? file_size - header_->data_start
                     : 0;
}

const gguf::TensorInfo* Weights::find(const std::string& name) const {
    if (header_ == nullptr) return nullptr;
    const auto it = by_name_.find(name);
    if (it == by_name_.end()) return nullptr;
    if (it->second >= header_->tensors.size()) return nullptr;
    return &header_->tensors[it->second];
}

int32_t Weights::shape2d(const std::string& name, uint32_t& ne0, uint32_t& ne1,
                         uint32_t& type, std::string& error) const {
    const gguf::TensorInfo* t = find(name);
    if (t == nullptr) {
        error = "tensor not found: " + name;
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (t->n_dims != 2) {
        error = "tensor '" + name + "' has " + std::to_string(t->n_dims) +
                " dims, expected 2";
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (t->dims[0] > UINT32_MAX || t->dims[1] > UINT32_MAX) {
        error = "tensor '" + name + "' dims exceed 32-bit range";
        return SHTN_ERR_MODEL_FORMAT;
    }
    ne0 = static_cast<uint32_t>(t->dims[0]);
    ne1 = static_cast<uint32_t>(t->dims[1]);
    type = t->type;
    return SHTN_OK;
}

int32_t Weights::row_span(const std::string& name, uint64_t row,
                          uint32_t expected_ne0, uint32_t expected_rows,
                          const uint8_t*& data, uint64_t& nbytes,
                          std::string& error) const {
    data = nullptr;
    nbytes = 0;

    uint32_t ne0 = 0, ne1 = 0, type = 0;
    const int32_t rc = shape2d(name, ne0, ne1, type, error);
    if (rc != SHTN_OK) return rc;

    if (expected_ne0 > 0 && ne0 != expected_ne0) {
        error = "tensor '" + name + "' ne0=" + std::to_string(ne0) +
                ", expected " + std::to_string(expected_ne0);
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (expected_rows > 0 && ne1 != expected_rows) {
        error = "tensor '" + name + "' rows=" + std::to_string(ne1) +
                ", expected " + std::to_string(expected_rows);
        return SHTN_ERR_MODEL_FORMAT;
    }

    if (row >= ne1) {
        error = "tensor '" + name + "' row " + std::to_string(row) +
                " out of range (" + std::to_string(ne1) + " rows)";
        return SHTN_ERR_MODEL_FORMAT;
    }

    RowLayout layout{};
    if (!row_layout_for(type, ne0, layout)) {
        error = "tensor '" + name + "' type id " + std::to_string(type) +
                " is not supported for native inference";
        return SHTN_ERR_UNSUPPORTED;
    }

    const gguf::TensorInfo* t = find(name);

    // Absolute offset: data section start + tensor offset + row offset.
    uint64_t row_off = 0;
    if (!gguf::checked_mul_u64(row, layout.row_bytes, row_off)) {
        error = "tensor '" + name + "' row offset overflow";
        return SHTN_ERR_MODEL_FORMAT;
    }
    uint64_t abs = 0;
    if (!gguf::checked_add_u64(header_->data_start, t->offset, abs) ||
        !gguf::checked_add_u64(abs, row_off, abs)) {
        error = "tensor '" + name + "' absolute offset overflow";
        return SHTN_ERR_MODEL_FORMAT;
    }

    if (data_ == nullptr) {
        error = "tensor data section not bound (model not mapped)";
        return SHTN_ERR_MODEL_STATE;
    }
    if (abs > header_->data_start + data_size_ ||
        layout.row_bytes > header_->data_start + data_size_ - abs) {
        error = "tensor '" + name + "' row " + std::to_string(row) +
                " leaves the mapped data section";
        return SHTN_ERR_MODEL_FORMAT;
    }

    data = (data_ - header_->data_start) + abs;
    nbytes = layout.row_bytes;
    return SHTN_OK;
}

// --- resolved tensor views (v1.9.2) -------------------------------------------

int32_t Weights::resolve(const std::string& name, uint32_t expected_ne0,
                         uint32_t expected_rows, ResolvedTensor& out,
                         std::string& error) const {
    out = ResolvedTensor{};

    const gguf::TensorInfo* t = find(name);
    if (t == nullptr) {
        error = "tensor not found: " + name;
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (t->n_dims != 2) {
        error = "tensor '" + name + "' has " + std::to_string(t->n_dims) +
                " dims, expected 2";
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (t->dims[0] > UINT32_MAX || t->dims[1] > UINT32_MAX) {
        error = "tensor '" + name + "' shape exceeds 32-bit range";
        return SHTN_ERR_MODEL_FORMAT;
    }

    const uint32_t ne0 = static_cast<uint32_t>(t->dims[0]);
    const uint32_t ne1 = static_cast<uint32_t>(t->dims[1]);

    if (expected_ne0 > 0 && ne0 != expected_ne0) {
        error = "tensor '" + name + "' ne0=" + std::to_string(ne0) +
                ", expected " + std::to_string(expected_ne0);
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (expected_rows > 0 && ne1 != expected_rows) {
        error = "tensor '" + name + "' rows=" + std::to_string(ne1) +
                ", expected " + std::to_string(expected_rows);
        return SHTN_ERR_MODEL_FORMAT;
    }

    RowLayout layout{};
    if (!row_layout_for(t->type, ne0, layout)) {
        error = "tensor '" + name + "' type id " + std::to_string(t->type) +
                " is not supported for native inference";
        return SHTN_ERR_UNSUPPORTED;
    }

    if (data_ == nullptr) {
        error = "tensor data section not bound (model not mapped)";
        return SHTN_ERR_MODEL_STATE;
    }

    // Whole-tensor byte range: base + rows*row_bytes must stay inside the
    // mapped data section. Validated ONCE here — row_ptr() afterwards only
    // re-checks the (already covered) row index against `rows`.
    uint64_t span = 0;
    if (!gguf::checked_mul_u64(ne1, layout.row_bytes, span)) {
        error = "tensor '" + name + "' byte span overflow";
        return SHTN_ERR_MODEL_FORMAT;
    }
    uint64_t base_abs = 0;
    if (!gguf::checked_add_u64(header_->data_start, t->offset, base_abs)) {
        error = "tensor '" + name + "' absolute offset overflow";
        return SHTN_ERR_MODEL_FORMAT;
    }
    const uint64_t data_end = header_->data_start + data_size_;
    if (base_abs > data_end || span > data_end - base_abs) {
        error = "tensor '" + name + "' leaves the mapped data section";
        return SHTN_ERR_MODEL_FORMAT;
    }

    out.base = data_ + t->offset;
    out.ne0 = ne0;
    out.rows = ne1;
    out.type = t->type;
    out.row_bytes = layout.row_bytes;
    out.row_elems_padded = layout.row_elems_padded;
    out.block_size = layout.block_size;
    out.block_bytes = layout.block_bytes;
    return SHTN_OK;
}

const uint8_t* row_ptr(const ResolvedTensor& t, uint64_t row) {
    if (!t.valid() || row >= t.rows) return nullptr;
    return t.base + row * static_cast<uint64_t>(t.row_bytes);
}

int32_t dequant_row(const ResolvedTensor& t, uint64_t row, float* out,
                    std::string& error) {
    const uint8_t* src = row_ptr(t, row);
    if (src == nullptr) {
        error = "resolved tensor row " + std::to_string(row) +
                " out of range (" + std::to_string(t.rows) + " rows)";
        return SHTN_ERR_MODEL_FORMAT;
    }

    switch (t.type) {
    case 0: { // F32
        const uint8_t* p = src;
        for (uint32_t i = 0; i < t.ne0; ++i, p += 4) {
            out[i] = load_f32_le(p);
        }
        return SHTN_OK;
    }
    case 1: { // F16
        const uint8_t* p = src;
        for (uint32_t i = 0; i < t.ne0; ++i, p += 2) {
            out[i] = load_fp16_le(p);
        }
        return SHTN_OK;
    }
    default: {
        // Quantized: dequantize whole blocks DIRECTLY into `out` (the
        // caller sizes it to row_elems_padded), keeping only the first
        // ne0 elements meaningful (block tail padding discarded by the
        // caller's dot over ne0). Same switch as row_f32 — one numerics
        // authority.
        const uint32_t blocks =
            static_cast<uint32_t>(t.row_bytes / t.block_bytes);
        for (uint32_t b = 0; b < blocks; ++b) {
            if (!dequant_block(t.type, src + b * t.block_bytes,
                               out + static_cast<size_t>(b) * t.block_size)) {
                error = "dequantization failed (resolved row)";
                return SHTN_ERR_INTERNAL;
            }
        }
        return SHTN_OK;
    }
    }
}

int32_t Weights::row_f32(const std::string& name, uint64_t row,
                         uint32_t expected_ne0, uint32_t expected_rows,
                         RowBuf& out, std::string& error) const {
    const uint8_t* src = nullptr;
    uint64_t nbytes = 0;
    const int32_t rc = row_span(name, row, expected_ne0, expected_rows, src,
                                nbytes, error);
    if (rc != SHTN_OK) return rc;

    const gguf::TensorInfo* t = find(name);
    const uint32_t ne0 = static_cast<uint32_t>(t->dims[0]);

    RowLayout layout{};
    if (!row_layout_for(t->type, ne0, layout)) {
        error = "unsupported tensor type";
        return SHTN_ERR_UNSUPPORTED;
    }

    if (out.size() < ne0) {
        out.resize(ne0);
    }

    switch (t->type) {
    case 0: { // F32
        const uint8_t* p = src;
        for (uint32_t i = 0; i < ne0; ++i, p += 4) {
            out[i] = load_f32_le(p);
        }
        return SHTN_OK;
    }
    case 1: { // F16
        const uint8_t* p = src;
        for (uint32_t i = 0; i < ne0; ++i, p += 2) {
            out[i] = load_fp16_le(p);
        }
        return SHTN_OK;
    }
    default: {
        // Quantized: dequantize whole blocks DIRECTLY into `out` (no
        // intermediate allocation — out is padded to the block multiple),
        // keeping only the first ne0 elements (block tail padding
        // discarded).
        const uint32_t blocks = static_cast<uint32_t>(layout.row_bytes /
                                                      layout.block_bytes);
        if (out.size() < layout.row_elems_padded) {
            out.resize(layout.row_elems_padded);
        }
        for (uint32_t b = 0; b < blocks; ++b) {
            if (!dequant_block(t->type, src + b * layout.block_bytes,
                               out.data() + b * layout.block_size)) {
                error = "dequantization failed for tensor '" + name + "'";
                return SHTN_ERR_INTERNAL;
            }
        }
        return SHTN_OK;
    }
    }
}

int32_t Weights::vec_f32(const std::string& name, uint32_t expected_elems,
                         RowBuf& out, std::string& error) const {
    const gguf::TensorInfo* t = find(name);
    if (t == nullptr) {
        error = "tensor not found: " + name;
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (t->n_dims != 1) {
        error = "tensor '" + name + "' has " + std::to_string(t->n_dims) +
                " dims, expected 1";
        return SHTN_ERR_MODEL_FORMAT;
    }
    if (t->dims[0] > UINT32_MAX) {
        error = "tensor '" + name + "' length exceeds 32-bit range";
        return SHTN_ERR_MODEL_FORMAT;
    }
    const uint32_t n = static_cast<uint32_t>(t->dims[0]);
    if (expected_elems > 0 && n != expected_elems) {
        error = "tensor '" + name + "' length " + std::to_string(n) +
                ", expected " + std::to_string(expected_elems);
        return SHTN_ERR_MODEL_FORMAT;
    }

    // 1-D validation + dequant through the 2-D row machinery (a 1-D
    // tensor is one row).
    if (out.size() < n) out.resize(n);

    switch (t->type) {
    case 0: {
        const uint8_t* src = nullptr;
        // Manual span computation for the 1-D case.
        RowLayout layout{};
        if (!row_layout_for(t->type, n, layout)) {
            error = "unsupported tensor type";
            return SHTN_ERR_UNSUPPORTED;
        }
        uint64_t abs = 0;
        if (!gguf::checked_add_u64(header_->data_start, t->offset, abs)) {
            error = "offset overflow";
            return SHTN_ERR_MODEL_FORMAT;
        }
        if (data_ == nullptr) {
            error = "data section not bound";
            return SHTN_ERR_MODEL_STATE;
        }
        if (abs > header_->data_start + data_size_ ||
            layout.row_bytes > header_->data_start + data_size_ - abs) {
            error = "tensor '" + name + "' leaves the mapped data section";
            return SHTN_ERR_MODEL_FORMAT;
        }
        src = (data_ - header_->data_start) + abs;
        const uint8_t* p = src;
        for (uint32_t i = 0; i < n; ++i, p += 4) {
            out[i] = load_f32_le(p);
        }
        return SHTN_OK;
    }
    default:
        // 1-D quantized vectors (norm weights are never quantized in
        // practice; still fail explicitly rather than reinterpret).
        error = "tensor '" + name + "' type id " + std::to_string(t->type) +
                " not supported for 1-D native access";
        return SHTN_ERR_UNSUPPORTED;
    }
}

} // namespace tensor
} // namespace shtn
