// tensor.h — safe GGUF tensor data access for the native engine (Phase 5).
//
// The Phase 2 loader validates the GGUF container, memory-maps the file
// and parses the tensor TABLE (names, types, shapes, offsets) without
// ever touching tensor DATA. Phase 5 builds the inference-side access
// layer on top of that table:
//
//   - tensor lookup by exact name (validated, never assumed);
//   - tensor type validation — only the types this engine can actually
//     dequantize are accepted; anything else is an EXPLICIT unsupported
//     error (never a silent reinterpretation);
//   - shape validation against the caller's expectation;
//   - byte-range validation before any pointer is handed out;
//   - row-major dequantization of one row into a caller-provided fp32
//     scratch buffer (no tensor copies, no per-row allocations).
//
// SUPPORTED GGML TYPES (documented exactly — do not advertise more):
//   F32 (0), F16 (1), Q4_0 (2), Q4_1 (3), Q5_0 (6), Q5_1 (7), Q8_0 (8)
// Everything else (K-quants, IQ, BF16, MXFP4, integers) fails with
// SHTN_ERR_UNSUPPORTED and the model falls back to llama.cpp.
//
// LAYOUT (matches ggml / the GGUF spec):
//   A tensor with dims [ne0, ne1, ...] is ne1 (or ne2*ne1...) rows of ne0
//   elements, laid out row-major with the FIRST dimension padded up to
//   the quantization block size. Row i therefore starts at element
//   offset i * row_elems_padded and only its first ne0 elements are
//   meaningful (block tail padding is dequantized but discarded).
//
// SECURITY: the mapped model file is UNTRUSTED INPUT. Every access is
// bounds-checked against the file size and the data-section offset; a
// hostile tensor table never yields an out-of-range pointer.

#ifndef SHTN_TENSOR_H
#define SHTN_TENSOR_H

#include "gguf.h"

#include <cstdint>
#include <string>
#include <unordered_map>
#include <vector>

namespace shtn {
namespace tensor {

// GGML type ids this engine can dequantize (the exact supported set).
bool type_supported(uint32_t ggml_type);
const char* type_name(uint32_t ggml_type);

// Dequantized row scratch buffer (owned by the caller / graph runner).
// One row of ne0 floats, reused across the whole generation to avoid
// per-token heap churn.
using RowBuf = std::vector<float>;

// --- resolved tensor record (v1.9.2) ---------------------------------------
//
// ResolvedTensor is the once-validated access record for one 2-D tensor:
// base points at the tensor's first row inside the (stable) file mapping;
// row_bytes/row_elems_padded/block_* come from the same layout logic
// row_f32 uses. The pointer math for row `r` is base + r*row_bytes —
// bounds for EVERY row were validated at resolve() time (the whole
// tensor's byte range), so the per-row access needs only the trivial
// row < rows check.
struct ResolvedTensor {
    const uint8_t* base = nullptr;      // first row (inside the mapping)
    uint32_t ne0 = 0;                   // elements per row (unpadded)
    uint32_t rows = 0;                  // ne1 (2-D row count)
    uint32_t type = 0;                  // GGML type id
    uint32_t row_bytes = 0;             // padded row stride in bytes
    uint32_t row_elems_padded = 0;      // row stride in elements (padded)
    uint32_t block_size = 0;            // elements per quant block
    uint32_t block_bytes = 0;           // bytes per quant block
    bool valid() const { return base != nullptr && ne0 > 0 && rows > 0; }
};

// row_ptr returns the (bounds-checked) pointer to row `row` of a resolved
// tensor, or nullptr when the row index is out of range.
const uint8_t* row_ptr(const ResolvedTensor& t, uint64_t row);

// dequant_row loads row `row` of a resolved tensor into out as fp32.
// out must hold at least max(ne0, row_elems_padded) floats for quantized
// types (block tail padding is written, then discarded by the caller's
// dot over ne0). Same numerics as row_f32 — the switch exists ONCE here.
// Returns SHTN_OK or a negative error code (never a crash).
int32_t dequant_row(const ResolvedTensor& t, uint64_t row, float* out,
                    std::string& error);

// Weights is a read-only, bounds-checked view over the tensor data
// section of ONE memory-mapped GGUF file. It does NOT own the mapping
// (Model owns it); the view is valid while the model stays loaded.
class Weights {
public:
    Weights() = default;

    // Build the name → index map over the parsed tensor table. O(n) once
    // per load; lookups afterwards are O(1). Data access requires a
    // separate bind_data call from the mapping owner.
    explicit Weights(const gguf::GgufHeader& header);

    // bind_data attaches the raw file mapping so data access becomes
    // possible. The header's data_start is trusted because the parser
    // already validated it against the file size.
    void bind_data(const uint8_t* file_base, uint64_t file_size);

    // --- lookup ---------------------------------------------------------
    //
    // find returns the TensorInfo for an exact tensor name, or nullptr
    // when the model has no such tensor (never an assumption).
    const gguf::TensorInfo* find(const std::string& name) const;

    // --- row dequantization ---------------------------------------------
    //
    // row_f32 dequantizes row `row` of tensor `name` into out (resized to
    // ne0). Returns:
    //   SHTN_OK                row dequantized;
    //   SHTN_ERR_UNSUPPORTED   tensor type is not dequantizable here;
    //   SHTN_ERR_MODEL_FORMAT  tensor missing, shape not 2-D, row out of
    //                          range, or the row's byte range leaves the
    //                          mapped data section.
    //
    // expected_ne0/expected_rows, when > 0, are validated against the
    // tensor's actual shape BEFORE any data is read (an incompatible
    // reinterpretation is rejected, never guessed).
    int32_t row_f32(const std::string& name, uint64_t row,
                    uint32_t expected_ne0, uint32_t expected_rows,
                    RowBuf& out, std::string& error) const;

    // Convenience: 1-D tensor (a weight vector) read fully into out.
    // expected_elems > 0 validates the length.
    int32_t vec_f32(const std::string& name, uint32_t expected_elems,
                    RowBuf& out, std::string& error) const;

    // --- raw block access (used by the embedding lookup fast path) ------
    //
    // row_bytes returns the byte size of one row (padded) and row_offset
    // the absolute file offset of row `row`. Validation identical to
    // row_f32. Exposed for the F32 fast paths that dot products consume
    // directly from the mapping without an intermediate copy.
    int32_t row_span(const std::string& name, uint64_t row,
                     uint32_t expected_ne0, uint32_t expected_rows,
                     const uint8_t*& data, uint64_t& nbytes,
                     std::string& error) const;

    // row_count / ne0 / ggml type of a tensor (validated 2-D access).
    int32_t shape2d(const std::string& name, uint32_t& ne0, uint32_t& ne1,
                    uint32_t& type, std::string& error) const;

    // --- resolved tensor views (v1.9.2 prefill/decode fast path) ---------
    //
    // resolve validates a 2-D tensor ONCE (name lookup, shape expectations,
    // type support, byte range of the WHOLE tensor against the mapped data
    // section) and returns the stable access record: the row-0 base pointer
    // inside the mapping, the row layout, and the shape. The record stays
    // valid while the model stays loaded (the mapping never moves; Model
    // owns it). This removes the per-row name resolution that dominated the
    // measured per-token forward cost (measured 2026-10: ~8064 name
    // resolutions per token ≈ 19 ms/token — the math itself is ~1 ms).
    //
    // Failure modes are the same as row_f32's (explicit errors, never
    // guessed pointers).
    int32_t resolve(const std::string& name, uint32_t expected_ne0,
                    uint32_t expected_rows, ResolvedTensor& out,
                    std::string& error) const;

private:
    const gguf::GgufHeader* header_ = nullptr;
    const uint8_t* data_ = nullptr; // data section base
    uint64_t data_size_ = 0;
    std::unordered_map<std::string, uint32_t> by_name_;
};

// --- dequantization primitives (unit-testable, no state) -----------------
//
// dequant_block converts ONE quantization block (block_size elements) at
// `src` into `out` (block_size floats). Returns false for an unsupported
// type. These are the exact reference formulas from the ggml
// specification; test_tensor pins them against hand-computed values.

bool dequant_block(uint32_t ggml_type, const uint8_t* src, float* out);

// fp16 row → fp32 row (exact widening, block_size 1).
void f16_row_to_f32(const uint16_t* src, float* dst, uint32_t count);

} // namespace tensor
} // namespace shtn

#endif /* SHTN_TENSOR_H */
