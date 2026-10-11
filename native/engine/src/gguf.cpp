// gguf.cpp — GGUF reader implementation (bounds-checked, mmap-backed).

#include "gguf.h"

#include <cstring>
#include <string>

#if defined(_WIN32)
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#else
#include <fcntl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>
#endif

namespace shtn {
namespace gguf {

// --- checked arithmetic ------------------------------------------------------

bool checked_add_u64(uint64_t a, uint64_t b, uint64_t& out) {
    if (a > UINT64_MAX - b) {
        return false;
    }
    out = a + b;
    return true;
}

bool checked_mul_u64(uint64_t a, uint64_t b, uint64_t& out) {
    if (a != 0 && b > UINT64_MAX / a) {
        return false;
    }
    out = a * b;
    return true;
}

// --- GGML type table ---------------------------------------------------------
//
// (block_size, type_size) pairs from the public ggml type traits. Used
// ONLY for tensor byte-size estimates and offset validation — never for
// allocating anything. Types unknown to this build yield a 0 estimate
// and fall back to the offset-in-range check.

namespace {

struct TypeTraits {
    uint32_t block_size;
    uint32_t type_size;
};

bool type_traits(uint32_t type, TypeTraits& out) {
    // GGML type ids mirror the public ggml.h enum. Only types whose
    // (block_size, type_size) pairs are unambiguous are listed; anything
    // else falls back to the offset-in-range check (never a guess).
    switch (type) {
    case 0:  out = {1, 4};   return true;   // F32
    case 1:  out = {1, 2};   return true;   // F16
    case 2:  out = {32, 18}; return true;   // Q4_0
    case 3:  out = {32, 20}; return true;   // Q4_1
    case 6:  out = {32, 22}; return true;   // Q5_0
    case 7:  out = {32, 24}; return true;   // Q5_1
    case 8:  out = {32, 34}; return true;   // Q8_0
    case 9:  out = {32, 36}; return true;   // Q8_1
    case 10: out = {256, 84};  return true; // Q2_K
    case 11: out = {256, 110}; return true; // Q3_K
    case 12: out = {256, 144}; return true; // Q4_K
    case 13: out = {256, 176}; return true; // Q5_K
    case 14: out = {256, 210}; return true; // Q6_K
    case 16: out = {256, 66};  return true; // IQ2_XXS
    case 17: out = {256, 74};  return true; // IQ2_XS
    case 18: out = {256, 98};  return true; // IQ3_XXS
    case 19: out = {256, 50};  return true; // IQ1_S
    case 20: out = {32, 20};   return true; // IQ4_NL
    case 21: out = {256, 110}; return true; // IQ3_S
    case 22: out = {256, 98};  return true; // IQ2_S
    case 23: out = {256, 56};  return true; // IQ4_XS
    case 24: out = {1, 1};   return true;   // I8
    case 25: out = {1, 2};   return true;   // I16
    case 26: out = {1, 4};   return true;   // I32
    case 27: out = {1, 8};   return true;   // I64
    case 28: out = {1, 8};   return true;   // F64
    case 29: out = {256, 56};  return true; // IQ1_M
    case 30: out = {1, 2};   return true;   // BF16
    default: return false;
    }
}

} // namespace

uint64_t tensor_bytes_estimate(uint32_t type, const uint64_t* dims,
                               uint32_t n_dims) {
    TypeTraits tt{};
    if (!type_traits(type, tt) || dims == nullptr || n_dims == 0 ||
        n_dims > kMaxTensorDims) {
        return 0;
    }

    // ggml_nbytes formula: round the FIRST dim up to the block size,
    // multiply the rest, divide by the block size, multiply the type
    // size. All steps overflow-checked; overflow yields "unknown".
    uint64_t d0_padded = 0;
    if (!checked_add_u64(dims[0], tt.block_size - 1u, d0_padded)) {
        return 0;
    }
    d0_padded = (d0_padded / tt.block_size) * tt.block_size;

    uint64_t elements = d0_padded;
    for (uint32_t i = 1; i < n_dims; ++i) {
        if (!checked_mul_u64(elements, dims[i], elements)) {
            return 0;
        }
    }

    uint64_t blocks = 0;
    if (!checked_mul_u64(elements, 1, blocks) || elements % tt.block_size != 0) {
        return 0;
    }
    blocks = elements / tt.block_size;

    uint64_t bytes = 0;
    if (!checked_mul_u64(blocks, tt.type_size, bytes)) {
        return 0;
    }

    return bytes;
}

std::string file_type_name(uint32_t ft) {
    switch (ft) {
    case 0:  return "F32";
    case 1:  return "F16";
    case 2:  return "Q4_0";
    case 3:  return "Q4_1";
    case 7:  return "Q5_0";
    case 8:  return "Q5_1";
    case 9:  return "Q8_0";
    case 10: return "Q2_K";
    case 11: return "Q3_K_S";
    case 12: return "Q3_K_M";
    case 13: return "Q3_K_L";
    case 14: return "Q4_K_S";
    case 15: return "Q4_K_M";
    case 16: return "Q5_K_S";
    case 17: return "Q5_K_M";
    case 18: return "Q6_K";
    case 19: return "TQ1_0";
    case 20: return "TQ2_0";
    case 21: return "IQ2_XXS";
    case 22: return "IQ2_XS";
    case 23: return "Q2_K_S";
    case 24: return "IQ3_XS";
    case 25: return "IQ3_S";
    case 26: return "IQ3_M";
    case 27: return "IQ2_S";
    case 28: return "IQ2_M";
    case 29: return "IQ4_XS";
    case 30: return "BF16";
    case 31: return "IQ1_S";
    case 32: return "IQ1_M";
    case 36: return "IQ4_NL";
    case 38: return "MXFP4";
    default: {
        std::string s = "type-";
        s += std::to_string(ft);
        return s;
    }
    }
}

// --- MappedFile ---------------------------------------------------------------

MappedFile::~MappedFile() { close(); }

MappedFile::MappedFile(MappedFile&& other) noexcept
    : handle_(other.handle_), map_(other.map_), data_(other.data_),
      size_(other.size_) {
    other.handle_ = nullptr;
    other.map_ = nullptr;
    other.data_ = nullptr;
    other.size_ = 0;
}

MappedFile& MappedFile::operator=(MappedFile&& other) noexcept {
    if (this != &other) {
        close();
        handle_ = other.handle_;
        map_ = other.map_;
        data_ = other.data_;
        size_ = other.size_;
        other.handle_ = nullptr;
        other.map_ = nullptr;
        other.data_ = nullptr;
        other.size_ = 0;
    }
    return *this;
}

#if defined(_WIN32)

bool MappedFile::open(const std::string& path, std::string& error) {
    close();

    if (path.empty()) {
        error = "model path is empty";
        return false;
    }

    std::wstring wpath;
    const int n = MultiByteToWideChar(
        CP_UTF8, MB_ERR_INVALID_CHARS, path.c_str(), -1, nullptr, 0);
    if (n <= 0) {
        error = "model path is not valid UTF-8";
        return false;
    }
    wpath.resize(static_cast<size_t>(n));
    MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, path.c_str(), -1,
                        wpath.data(), n);

    HANDLE file = CreateFileW(wpath.c_str(), GENERIC_READ, FILE_SHARE_READ,
                              nullptr, OPEN_EXISTING,
                              FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) {
        error = "cannot open model file (win32 error " +
                std::to_string(GetLastError()) + ")";
        return false;
    }

    LARGE_INTEGER sz{};
    if (!GetFileSizeEx(file, &sz)) {
        CloseHandle(file);
        error = "cannot stat model file (win32 error " +
                std::to_string(GetLastError()) + ")";
        return false;
    }

    if (sz.QuadPart <= 0) {
        CloseHandle(file);
        error = "model file is empty";
        return false;
    }

    HANDLE section = CreateFileMappingW(file, nullptr, PAGE_READONLY, 0, 0,
                                        nullptr);
    if (section == nullptr) {
        CloseHandle(file);
        error = "cannot create file mapping (win32 error " +
                std::to_string(GetLastError()) + ")";
        return false;
    }

    void* view = MapViewOfFile(section, FILE_MAP_READ, 0, 0, 0);
    if (view == nullptr) {
        CloseHandle(section);
        CloseHandle(file);
        error = "cannot map view of model file (win32 error " +
                std::to_string(GetLastError()) + ")";
        return false;
    }

    // v1.3.5 fix (found by the portable temp-cleanup contract): store the
    // handles per the class contract — handle_ = the platform mapping
    // handle (the SECTION, mirroring the POSIX fd), map_ = the mapping
    // BASE (the value UnmapViewOfFile requires). The previous code stored
    // the file handle in handle_ and the section handle in map_, so
    // close() called UnmapViewOfFile(section) — which fails (it needs the
    // view address) — and CloseHandle(file) left BOTH the section object
    // and the view alive: every model unload leaked a section + view and
    // pinned the .gguf file for the life of the process (the file could
    // not be deleted or replaced after unload). The explicit file handle
    // is not needed once the view exists (the section holds its own
    // reference to the file), so it is closed here.
    CloseHandle(file);

    handle_ = section; // platform mapping handle (POSIX counterpart: fd)
    map_ = view;      // mapping base — UnmapViewOfFile's argument
    data_ = static_cast<uint8_t*>(view);
    size_ = static_cast<uint64_t>(sz.QuadPart);
    return true;
}

void MappedFile::close() {
    if (map_ != nullptr) {
        UnmapViewOfFile(map_); // the VIEW base address (never a handle)
        map_ = nullptr;
    }
    if (handle_ != nullptr) {
        CloseHandle(handle_); // the SECTION handle (the file handle was
                              // closed at open time; the section's own
                              // reference kept the file alive until now)
        handle_ = nullptr;
    }
    data_ = nullptr;
    size_ = 0;
}

#else // POSIX

bool MappedFile::open(const std::string& path, std::string& error) {
    close();

    if (path.empty()) {
        error = "model path is empty";
        return false;
    }

    const int fd = ::open(path.c_str(), O_RDONLY | O_CLOEXEC);
    if (fd < 0) {
        error = "cannot open model file: " + std::string(strerror(errno));
        return false;
    }

    struct stat st{};
    if (::fstat(fd, &st) != 0) {
        const std::string reason = strerror(errno);
        ::close(fd);
        error = "cannot stat model file: " + reason;
        return false;
    }

    if (st.st_size <= 0) {
        ::close(fd);
        error = "model file is empty";
        return false;
    }

    void* mapped = ::mmap(nullptr, static_cast<size_t>(st.st_size), PROT_READ,
                          MAP_PRIVATE, fd, 0);
    if (mapped == MAP_FAILED) {
        const std::string reason = strerror(errno);
        ::close(fd);
        error = "cannot memory-map model file: " + reason;
        return false;
    }

    handle_ = reinterpret_cast<void*>(static_cast<intptr_t>(fd));
    map_ = mapped;
    data_ = static_cast<uint8_t*>(mapped);
    size_ = static_cast<uint64_t>(st.st_size);
    return true;
}

void MappedFile::close() {
    if (map_ != nullptr) {
        ::munmap(map_, static_cast<size_t>(size_));
        map_ = nullptr;
    }
    if (handle_ != nullptr) {
        ::close(static_cast<int>(reinterpret_cast<intptr_t>(handle_)));
        handle_ = nullptr;
    }
    data_ = nullptr;
    size_ = 0;
}

#endif

// --- bounded cursor over the mapped bytes ------------------------------------

namespace {

// Cursor walks the mapped region with explicit bounds; every read is a
// bounds check first. A failed read is an error, never a partial value.
class Cursor {
public:
    Cursor(const uint8_t* data, uint64_t size)
        : data_(data), size_(size), pos_(0) {}

    uint64_t pos() const { return pos_; }
    uint64_t remaining() const { return size_ - pos_; }
    bool at_end() const { return pos_ >= size_; }

    bool take(uint64_t n, const uint8_t*& ptr) {
        if (n > remaining()) {
            return false;
        }
        ptr = data_ + pos_;
        pos_ += n;
        return true;
    }

    bool skip(uint64_t n) {
        if (n > remaining()) {
            return false;
        }
        pos_ += n;
        return true;
    }

    bool u8(uint8_t& out) {
        const uint8_t* p = nullptr;
        if (!take(1, p)) {
            return false;
        }
        out = p[0];
        return true;
    }

    bool u16(uint16_t& out) {
        const uint8_t* p = nullptr;
        if (!take(2, p)) {
            return false;
        }
        out = static_cast<uint16_t>(p[0]) |
              (static_cast<uint16_t>(p[1]) << 8);
        return true;
    }

    bool u32(uint32_t& out) {
        const uint8_t* p = nullptr;
        if (!take(4, p)) {
            return false;
        }
        out = static_cast<uint32_t>(p[0]) |
              (static_cast<uint32_t>(p[1]) << 8) |
              (static_cast<uint32_t>(p[2]) << 16) |
              (static_cast<uint32_t>(p[3]) << 24);
        return true;
    }

    bool u64(uint64_t& out) {
        uint32_t lo = 0;
        uint32_t hi = 0;
        if (!u32(lo) || !u32(hi)) {
            return false;
        }
        out = static_cast<uint64_t>(lo) |
              (static_cast<uint64_t>(hi) << 32);
        return true;
    }

    bool f32(double& out) {
        uint32_t raw = 0;
        if (!u32(raw)) {
            return false;
        }
        float f = 0.0f;
        std::memcpy(&f, &raw, sizeof(f));
        out = static_cast<double>(f);
        return true;
    }

    bool f64(double& out) {
        uint64_t raw = 0;
        if (!u64(raw)) {
            return false;
        }
        std::memcpy(&out, &raw, sizeof(out));
        return true;
    }

private:
    const uint8_t* data_;
    uint64_t size_;
    uint64_t pos_;
};

bool read_string(Cursor& c, std::string& out) {
    uint64_t len = 0;
    if (!c.u64(len)) {
        return false;
    }
    if (len > kMaxStringBytes) {
        return false;
    }
    const uint8_t* p = nullptr;
    if (!c.take(len, p)) {
        return false;
    }
    out.assign(reinterpret_cast<const char*>(p), static_cast<size_t>(len));
    return true;
}

// read_value parses one typed metadata value. Scalars are captured
// (strings bounded by kMaxStringBytes); array ELEMENTS are never
// materialized — the array is validated element-size-wise and skipped,
// with only its element count recorded.
bool read_value(Cursor& c, Scalar& out) {
    if (!c.u32(out.type)) {
        return false;
    }

    switch (out.type) {
    case kTypeUint8: {
        uint8_t v = 0;
        if (!c.u8(v)) return false;
        out.u64_v = v;
        return true;
    }
    case kTypeInt8: {
        uint8_t v = 0;
        if (!c.u8(v)) return false;
        out.i64_v = static_cast<int8_t>(v);
        return true;
    }
    case kTypeUint16: {
        uint16_t v = 0;
        if (!c.u16(v)) return false;
        out.u64_v = v;
        return true;
    }
    case kTypeInt16: {
        uint16_t v = 0;
        if (!c.u16(v)) return false;
        out.i64_v = static_cast<int16_t>(v);
        return true;
    }
    case kTypeUint32: {
        uint32_t v = 0;
        if (!c.u32(v)) return false;
        out.u64_v = v;
        return true;
    }
    case kTypeInt32: {
        uint32_t v = 0;
        if (!c.u32(v)) return false;
        out.i64_v = static_cast<int32_t>(v);
        return true;
    }
    case kTypeFloat32:
        return c.f32(out.f64_v);
    case kTypeBool: {
        uint8_t v = 0;
        if (!c.u8(v)) return false;
        out.bool_v = v != 0;
        return true;
    }
    case kTypeString:
        return read_string(c, out.str_v);
    case kTypeUint64:
    case kTypeInt64:
        return c.u64(out.u64_v);
    case kTypeFloat64:
        return c.f64(out.f64_v);
    case kTypeArray: {
        uint32_t elem_type = 0;
        uint64_t count = 0;
        if (!c.u32(elem_type) || !c.u64(count)) {
            return false;
        }
        if (count > kMaxArrayElements) {
            return false;
        }

        if (elem_type == kTypeString) {
            // Array of strings: each element has its own length prefix;
            // walk (not read) every element with the same bounds.
            for (uint64_t i = 0; i < count; ++i) {
                uint64_t len = 0;
                if (!c.u64(len) || len > kMaxStringBytes) {
                    return false;
                }
                if (!c.skip(len)) {
                    return false;
                }
            }
        } else {
            uint64_t elem_size = 0;
            switch (elem_type) {
            case kTypeUint8: case kTypeInt8: case kTypeBool:
                elem_size = 1; break;
            case kTypeUint16: case kTypeInt16:
                elem_size = 2; break;
            case kTypeUint32: case kTypeInt32: case kTypeFloat32:
                elem_size = 4; break;
            case kTypeUint64: case kTypeInt64: case kTypeFloat64:
                elem_size = 8; break;
            case kTypeArray:
                return false; // nested arrays are not valid GGUF
            default:
                return false; // unknown element type
            }

            uint64_t span = 0;
            if (!checked_mul_u64(elem_size, count, span)) {
                return false;
            }
            if (!c.skip(span)) {
                return false;
            }
        }

        out.array_count = count;
        return true;
    }
    default:
        return false; // unknown value type
    }
}

// align_up rounds `value` up to `alignment` with overflow protection.
bool align_up(uint64_t value, uint64_t alignment, uint64_t& out) {
    if (alignment == 0) {
        return false;
    }
    uint64_t rem = value % alignment;
    if (rem == 0) {
        out = value;
        return true;
    }
    return checked_add_u64(value, alignment - rem, out);
}

} // namespace

bool parse_header(const MappedFile& file, GgufHeader& out, std::string& error) {
    out = GgufHeader{};

    if (file.data() == nullptr || file.size() == 0) {
        error = "gguf: file is empty or not mapped";
        return false;
    }

    Cursor c(file.data(), file.size());

    // Magic "GGUF".
    static const uint8_t kMagic[4] = {'G', 'G', 'U', 'F'};
    const uint8_t* magic = nullptr;
    if (!c.take(4, magic)) {
        error = "gguf: file too small for magic";
        return false;
    }
    if (std::memcmp(magic, kMagic, 4) != 0) {
        error = "gguf: bad magic (not a GGUF file)";
        return false;
    }

    if (!c.u32(out.version)) {
        error = "gguf: truncated header (no version)";
        return false;
    }
    if (out.version < kMinVersion || out.version > kMaxVersion) {
        error = "gguf: unsupported GGUF version " + std::to_string(out.version) +
                " (supported: 2..3)";
        return false;
    }

    if (!c.u64(out.tensor_count) || !c.u64(out.kv_count)) {
        error = "gguf: truncated header (no counts)";
        return false;
    }
    if (out.tensor_count > kMaxTensorCount) {
        error = "gguf: implausible tensor count " +
                std::to_string(out.tensor_count);
        return false;
    }
    if (out.kv_count > kMaxKvCount) {
        error = "gguf: implausible metadata count " +
                std::to_string(out.kv_count);
        return false;
    }

    // Metadata key/value pairs.
    out.metadata.reserve(static_cast<size_t>(out.kv_count < 256
                                                 ? out.kv_count
                                                 : 256));
    for (uint64_t i = 0; i < out.kv_count; ++i) {
        std::string key;
        if (!read_string(c, key)) {
            error = "gguf: malformed metadata at offset " +
                    std::to_string(c.pos());
            return false;
        }
        if (key.empty()) {
            error = "gguf: empty metadata key at offset " +
                    std::to_string(c.pos());
            return false;
        }

        Scalar value;
        if (!read_value(c, value)) {
            error = "gguf: malformed metadata value for key '" + key +
                    "' at offset " + std::to_string(c.pos());
            return false;
        }

        if (key == "general.alignment" && value.type == kTypeUint32) {
            if (value.u64_v == 0 || value.u64_v > kMaxAlignment ||
                (value.u64_v & (value.u64_v - 1)) != 0) {
                error = "gguf: invalid general.alignment " +
                        std::to_string(value.u64_v) +
                        " (must be a power of two, <= 4096)";
                return false;
            }
            out.alignment = value.u64_v;
        }

        out.metadata.emplace_back(std::move(key), std::move(value));
    }

    if (const Scalar* ft = find_scalar(out.metadata, "general.file_type")) {
        uint64_t v = 0;
        if (scalar_u64(*ft, v) && v <= UINT32_MAX) {
            out.file_type = static_cast<uint32_t>(v);
            out.has_file_type = true;
        }
    }

    // Tensor table.
    out.tensors.reserve(static_cast<size_t>(out.tensor_count < 1024
                                                ? out.tensor_count
                                                : 1024));
    for (uint64_t i = 0; i < out.tensor_count; ++i) {
        TensorInfo t;
        if (!read_string(c, t.name)) {
            error = "gguf: malformed tensor name at offset " +
                    std::to_string(c.pos());
            return false;
        }
        if (t.name.empty()) {
            error = "gguf: empty tensor name at entry " + std::to_string(i);
            return false;
        }

        if (!c.u32(t.n_dims)) {
            error = "gguf: truncated tensor dims for '" + t.name + "'";
            return false;
        }
        if (t.n_dims == 0 || t.n_dims > kMaxTensorDims) {
            error = "gguf: invalid dim count " + std::to_string(t.n_dims) +
                    " for tensor '" + t.name + "'";
            return false;
        }

        for (uint32_t d = 0; d < t.n_dims; ++d) {
            if (!c.u64(t.dims[d])) {
                error = "gguf: truncated tensor dims for '" + t.name + "'";
                return false;
            }
            if (t.dims[d] == 0) {
                error = "gguf: zero dim in tensor '" + t.name + "'";
                return false;
            }
        }

        if (!c.u32(t.type)) {
            error = "gguf: truncated tensor type for '" + t.name + "'";
            return false;
        }

        if (!c.u64(t.offset)) {
            error = "gguf: truncated tensor offset for '" + t.name + "'";
            return false;
        }

        t.nbytes_estimate =
            tensor_bytes_estimate(t.type, t.dims, t.n_dims);

        out.tensors.push_back(std::move(t));
    }

    // Data section: header end, aligned up to general.alignment.
    uint64_t data_start = 0;
    if (!align_up(c.pos(), out.alignment, data_start)) {
        error = "gguf: data section offset overflow";
        return false;
    }
    if (data_start > file.size()) {
        error = "gguf: header exceeds file size (truncated file)";
        return false;
    }

    out.data_start = data_start;
    out.data_bytes = file.size() - data_start;

    // Validate every tensor offset against the data section — model
    // metadata is never trusted. Exact byte sizes are enforced when the
    // type is known to this build; unknown types get the in-range check.
    for (const TensorInfo& t : out.tensors) {
        // The element count must be representable at all: a tensor whose
        // dims product overflows uint64 cannot exist in any real file.
        uint64_t elements = 1;
        for (uint32_t d = 0; d < t.n_dims; ++d) {
            if (!checked_mul_u64(elements, t.dims[d], elements)) {
                error = "gguf: tensor '" + t.name +
                        "' dims overflow (hostile header)";
                return false;
            }
        }

        if (t.offset > out.data_bytes) {
            error = "gguf: tensor '" + t.name +
                    "' offset " + std::to_string(t.offset) +
                    " outside data section (" +
                    std::to_string(out.data_bytes) + " bytes)";
            return false;
        }

        if (t.nbytes_estimate > 0) {
            uint64_t end = 0;
            if (!checked_add_u64(t.offset, t.nbytes_estimate, end) ||
                end > out.data_bytes) {
                error = "gguf: tensor '" + t.name +
                        "' size exceeds the data section";
                return false;
            }
        }

        // Derive the parameter count: Σ Π(dims) over the tensor table
        // (the same derivation the official gguf tooling performs).
        uint64_t total = 0;
        if (checked_add_u64(out.derived_parameter_count, elements, total)) {
            out.derived_parameter_count = total;
        }
    }

    return true;
}

// --- metadata helpers ----------------------------------------------------------

const Scalar* find_scalar(const Metadata& md, const std::string& key) {
    for (const auto& kv : md) {
        if (kv.first == key) {
            return &kv.second;
        }
    }
    return nullptr;
}

bool scalar_u64(const Scalar& s, uint64_t& out) {
    switch (s.type) {
    case kTypeUint8:
    case kTypeUint16:
    case kTypeUint32:
    case kTypeUint64:
        out = s.u64_v;
        return true;
    case kTypeInt8:
    case kTypeInt16:
    case kTypeInt32:
    case kTypeInt64:
        if (s.i64_v < 0) {
            return false;
        }
        out = static_cast<uint64_t>(s.i64_v);
        return true;
    case kTypeBool:
        out = s.bool_v ? 1u : 0u;
        return true;
    default:
        return false;
    }
}

bool scalar_str(const Scalar& s, std::string& out) {
    if (s.type != kTypeString) {
        return false;
    }
    out = s.str_v;
    return true;
}

} // namespace gguf
} // namespace shtn
