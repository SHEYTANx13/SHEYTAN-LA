// gguf.h — GGUF file reader for the SHEYTAN Native Engine (Phase 2).
//
// Reads and validates GGUF v2/v3 container files (the versions llama.cpp
// produces and consumes) directly from a MEMORY-MAPPED file: metadata
// parsing touches only the header pages, tensor data is never read.
//
// Security posture — model files are UNTRUSTED INPUT:
//   - every read is bounds-checked against the mapped file size;
//   - every count/length is bounded before use (hostile headers fail
//     fast with a readable error, never a crash and never a huge
//     allocation);
//   - every arithmetic that could overflow (offset + size, dims
//     products, cache estimates) goes through checked helpers;
//   - metadata values are NEVER trusted: tensor offsets must land inside
//     the data section and per-tensor byte sizes (GGML type table) must
//     fit the file or the file is rejected.
//
// Format reference: https://github.com/ggml-org/ggml/blob/master/docs/gguf.md

#ifndef SHTN_GGUF_H
#define SHTN_GGUF_H

#include <cstdint>
#include <string>
#include <vector>

namespace shtn {
namespace gguf {

// Supported container versions. GGUF v1 (32-bit counts, pre-2023) and
// anything newer than v3 are rejected — llama.cpp reads v2/v3 only.
constexpr uint32_t kMinVersion = 2;
constexpr uint32_t kMaxVersion = 3;

// Hostile-input bounds. Real models sit far below every one of these;
// a header claiming more is corrupt or malicious and is rejected.
constexpr uint64_t kMaxKvCount = 16384;             // metadata pairs
constexpr uint64_t kMaxStringBytes = 16u << 20;     // 16 MiB one string
constexpr uint64_t kMaxArrayElements = 100u << 20;  // 100M elements
constexpr uint32_t kMaxTensorDims = 8;              // real models: 1..4
constexpr uint64_t kMaxTensorCount = 1u << 20;      // 1M tensor infos
constexpr uint32_t kMaxAlignment = 4096;            // GGUF alignment cap

// GGUF metadata value types (stable format constants).
enum ValueType : uint32_t {
    kTypeUint8 = 0,
    kTypeInt8 = 1,
    kTypeUint16 = 2,
    kTypeInt16 = 3,
    kTypeUint32 = 4,
    kTypeInt32 = 5,
    kTypeFloat32 = 6,
    kTypeBool = 7,
    kTypeString = 8,
    kTypeArray = 9,
    kTypeUint64 = 10,
    kTypeInt64 = 11,
    kTypeFloat64 = 12,
};

// One scalar metadata value (strings only; arrays are validated then
// skipped — their bytes never enter RAM, but their element count is
// recorded for vocabulary-size derivation).
struct Scalar {
    uint32_t type = 0;
    bool bool_v = false;
    uint64_t u64_v = 0;
    int64_t i64_v = 0;
    double f64_v = 0.0;
    std::string str_v;
    uint64_t array_count = 0; // kTypeArray only: element count
};

using Metadata = std::vector<std::pair<std::string, Scalar>>;

// Metadata lookup helpers (first match wins; GGUF keys are unique).
const Scalar* find_scalar(const Metadata& md, const std::string& key);
bool scalar_u64(const Scalar& s, uint64_t& out);   // int/uint/bool → u64
bool scalar_str(const Scalar& s, std::string& out);

// One validated tensor-table entry.
struct TensorInfo {
    std::string name;
    uint32_t type = 0;             // GGML type id
    uint32_t n_dims = 0;
    uint64_t dims[8] = {0};
    uint64_t offset = 0;           // relative to the data section
    uint64_t nbytes_estimate = 0;  // 0 = type size unknown (lenient check)
};

// Parsed model header: everything Phase 2 needs, nothing more.
struct GgufHeader {
    uint32_t version = 0;
    uint64_t tensor_count = 0;
    uint64_t kv_count = 0;
    Metadata metadata;
    std::vector<TensorInfo> tensors;

    uint64_t alignment = 32;       // general.alignment (validated)
    uint64_t data_start = 0;       // aligned offset where tensor data begins
    uint64_t data_bytes = 0;       // file_size - data_start (tensor span)

    uint64_t derived_parameter_count = 0; // Σ Π(dims) over the tensor table
    bool has_file_type = false;
    uint32_t file_type = 0;               // general.file_type (raw)
};

// Checked arithmetic (uint64). Returns false on overflow — callers turn
// that into a validation error, never into wrap-around.
bool checked_add_u64(uint64_t a, uint64_t b, uint64_t& out);
bool checked_mul_u64(uint64_t a, uint64_t b, uint64_t& out);

// Human-readable quantization name for a general.file_type value
// (matches the llama.cpp file-type enum; unknown values render as
// "type-N").
std::string file_type_name(uint32_t ft);

// Estimated bytes of one tensor: exact for fixed-size types, block
// arithmetic (ggml_nbytes formula: last dim padded to the block size)
// for quantized types, 0 when the type is unknown to this build.
uint64_t tensor_bytes_estimate(uint32_t type, const uint64_t* dims,
                               uint32_t n_dims);

// MappedFile is a read-only memory mapping of a whole file (RAII,
// move-only). Mapping (not reading) means loading a multi-GB model does
// not copy it into RAM: pages are faulted in lazily, only the header is
// touched by the parser.
class MappedFile {
public:
    MappedFile() = default;
    ~MappedFile();

    MappedFile(const MappedFile&) = delete;
    MappedFile& operator=(const MappedFile&) = delete;
    MappedFile(MappedFile&& other) noexcept;
    MappedFile& operator=(MappedFile&& other) noexcept;

    // Opens and maps `path` whole-file read-only. On failure returns
    // false and fills `error` (open errors, empty files, mapping
    // failures).
    bool open(const std::string& path, std::string& error);

    const uint8_t* data() const { return data_; }
    uint64_t size() const { return size_; }

    // Releases the mapping and the file handle (idempotent).
    void close();

private:
    void* handle_ = nullptr;   // platform mapping handle (fd / section)
    void* map_ = nullptr;      // mapping base
    uint8_t* data_ = nullptr;
    uint64_t size_ = 0;
};

// Parse and validate a GGUF header from an open mapping. Every field of
// `out` is only filled with values actually read or derived. On failure
// returns false and fills `error`; `out` is left valid-but-empty.
bool parse_header(const MappedFile& file, GgufHeader& out, std::string& error);

} // namespace gguf
} // namespace shtn

#endif /* SHTN_GGUF_H */
