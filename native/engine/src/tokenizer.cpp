// tokenizer.cpp — native engine tokenizer implementation (Phase 4).
//
// Reads tokenizer arrays from the memory-mapped GGUF file, materializes
// them into a Vocab (bounded), and provides encode/decode for BPE,
// Unigram (greedy longest-match) and WPM (greedy WordPiece) models.
//
// Bounds posture (mirrors gguf.cpp — the file is UNTRUSTED INPUT):
//   - every length read from the file is bounded before allocation;
//   - every string copy is bounded by kMaxTokenBytes;
//   - the vocab size is bounded by kMaxTokens;
//   - encode output is bounded by max_tokens (default 64k);
//   - decode output is bounded by max_bytes (default 1 MiB);
//   - no arithmetic that could overflow is done without a check.

#include "tokenizer.h"

#include "shtn/engine.h"
#include "util.h"

#include <cstring>
#include <sstream>

namespace shtn {
namespace tokenizer {

namespace {

// ---------------------------------------------------------------------------
// File view: a tiny bounds-checked reader over the mapped GGUF file.
// The GGUF parser already validated the header structure; this re-walks
// the metadata section starting from a known KV index to MATERIALIZE the
// tokenizer arrays (the parser skips arrays on the first pass — see
// gguf.cpp read_value, which only records element counts for arrays).
// ---------------------------------------------------------------------------

// find_meta finds a metadata entry by key (linear scan; metadata is
// bounded by kMaxKvCount = 16384 so this is cheap enough on init).
const gguf::Scalar* find_meta(const gguf::Metadata& md,
                              const std::string& key) {
    for (const auto& kv : md) {
        if (kv.first == key) {
            return &kv.second;
        }
    }
    return nullptr;
}

bool read_u64_scalar(const gguf::Metadata& md, const std::string& key,
                     uint64_t& out, bool& present) {
    const gguf::Scalar* s = find_meta(md, key);
    if (s == nullptr) {
        present = false;
        return true;
    }
    if (!gguf::scalar_u64(*s, out)) {
        return false;
    }
    present = true;
    return true;
}

// MaterializeStringArray re-walks the mapped file to read a string-array
// metadata value. GGUF array layout: [type:u32=9][elem_type:u32=8]
// [count:u64] then count * [len:u64][bytes]. We re-parse from the raw
// bytes of the Scalar — but the GGUF parser does NOT retain raw bytes.
//
// Instead, we re-parse the entire metadata block: walk every KV pair from
// the start (the mapping is still open), skipping values we don't want,
// and MATERIALIZE the target string array when we reach it. This is the
// same posture as the first pass — bounded, validated, never trusting
// counts.
//
// We do this by re-implementing the parser walk with a callback. To keep
// the code honest we use the SAME Cursor + read_value helpers the main
// parser uses (declared in gguf.cpp's anonymous namespace — we duplicate
// the minimal cursor here because gguf.cpp keeps it file-local).

// FileCursor: a minimal bounds-checked byte reader (matches gguf.cpp's
// Cursor interface; we duplicate it because the original is anonymous).
class FileCursor {
public:
    FileCursor(const uint8_t* data, uint64_t size)
        : data_(data), size_(size), pos_(0) {}

    uint64_t pos() const { return pos_; }
    uint64_t remaining() const { return size_ - pos_; }

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
        if (!take(1, p)) return false;
        out = p[0];
        return true;
    }
    bool u32(uint32_t& out) {
        const uint8_t* p = nullptr;
        if (!take(4, p)) return false;
        out = static_cast<uint32_t>(p[0]) |
              (static_cast<uint32_t>(p[1]) << 8) |
              (static_cast<uint32_t>(p[2]) << 16) |
              (static_cast<uint32_t>(p[3]) << 24);
        return true;
    }
    bool u64(uint64_t& out) {
        uint32_t lo = 0, hi = 0;
        if (!u32(lo) || !u32(hi)) return false;
        out = static_cast<uint64_t>(lo) | (static_cast<uint64_t>(hi) << 32);
        return true;
    }
    bool f32(double& out) {
        uint32_t raw = 0;
        if (!u32(raw)) return false;
        float f = 0.0f;
        std::memcpy(&f, &raw, sizeof(f));
        out = static_cast<double>(f);
        return true;
    }

    bool read_string(std::string& out) {
        uint64_t len = 0;
        if (!u64(len)) return false;
        if (len > gguf::kMaxStringBytes) return false;
        const uint8_t* p = nullptr;
        if (!take(len, p)) return false;
        out.assign(reinterpret_cast<const char*>(p), static_cast<size_t>(len));
        return true;
    }

private:
    const uint8_t* data_;
    uint64_t size_;
    uint64_t pos_;
};

// skip_value walks one typed value (matching gguf.cpp read_value) WITHOUT
// materializing arrays — used to skip past KV pairs we don't care about.
bool skip_value(FileCursor& c) {
    uint32_t type = 0;
    if (!c.u32(type)) return false;

    switch (type) {
    case gguf::kTypeUint8: { uint8_t v; return c.u8(v); }
    case gguf::kTypeInt8:  { uint8_t v; return c.u8(v); }
    case gguf::kTypeUint16: { uint8_t a, b; return c.u8(a) && c.u8(b); }
    case gguf::kTypeInt16:  { uint8_t a, b; return c.u8(a) && c.u8(b); }
    case gguf::kTypeUint32: { uint32_t v; return c.u32(v); }
    case gguf::kTypeInt32:  { uint32_t v; return c.u32(v); }
    case gguf::kTypeFloat32: { double v; return c.f32(v); }
    case gguf::kTypeBool:   { uint8_t v; return c.u8(v); }
    case gguf::kTypeString: { std::string s; return c.read_string(s); }
    case gguf::kTypeUint64:
    case gguf::kTypeInt64:  { uint64_t v; return c.u64(v); }
    case gguf::kTypeFloat64: { uint64_t v; return c.u64(v); }
    case gguf::kTypeArray: {
        uint32_t elem_type = 0;
        uint64_t count = 0;
        if (!c.u32(elem_type) || !c.u64(count)) return false;
        if (count > gguf::kMaxArrayElements) return false;
        if (elem_type == gguf::kTypeString) {
            for (uint64_t i = 0; i < count; ++i) {
                uint64_t len = 0;
                if (!c.u64(len) || len > gguf::kMaxStringBytes) return false;
                if (!c.skip(len)) return false;
            }
        } else {
            uint64_t elem_size = 0;
            switch (elem_type) {
            case gguf::kTypeUint8: case gguf::kTypeInt8: case gguf::kTypeBool:
                elem_size = 1; break;
            case gguf::kTypeUint16: case gguf::kTypeInt16:
                elem_size = 2; break;
            case gguf::kTypeUint32: case gguf::kTypeInt32: case gguf::kTypeFloat32:
                elem_size = 4; break;
            case gguf::kTypeUint64: case gguf::kTypeInt64: case gguf::kTypeFloat64:
                elem_size = 8; break;
            default: return false;
            }
            uint64_t span = 0;
            if (!gguf::checked_mul_u64(elem_size, count, span)) return false;
            if (!c.skip(span)) return false;
        }
        return true;
    }
    default:
        return false;
    }
}

// MaterializeTarget walks the metadata section looking for ONE specific
// string array key, and materializes it. Returns false on any bounds
// violation or type mismatch.
bool materialize_string_array(const gguf::GgufHeader& header,
                              const uint8_t* file_data, uint64_t file_size,
                              const std::string& target_key,
                              std::vector<std::string>& out,
                              std::string& error) {
    // Re-walk the header from the start: magic(4) + version(4) +
    // tensor_count(8) + kv_count(8) = 24 bytes, then KV pairs.
    FileCursor c(file_data, file_size);

    const uint8_t* magic = nullptr;
    if (!c.take(4, magic)) { error = "tokenizer: file truncated at magic"; return false; }
    static const uint8_t kMagic[4] = {'G', 'G', 'U', 'F'};
    if (std::memcmp(magic, kMagic, 4) != 0) {
        error = "tokenizer: bad magic on re-walk";
        return false;
    }

    uint32_t version = 0;
    uint64_t tc = 0, kc = 0;
    if (!c.u32(version) || !c.u64(tc) || !c.u64(kc)) {
        error = "tokenizer: header truncated on re-walk";
        return false;
    }

    if (kc != header.kv_count) {
        // Defensive: the header should be the same one we parsed.
        error = "tokenizer: kv_count mismatch on re-walk";
        return false;
    }

    out.clear();
    bool found = false;

    for (uint64_t i = 0; i < kc; ++i) {
        std::string key;
        if (!c.read_string(key)) {
            error = "tokenizer: malformed key on re-walk";
            return false;
        }

        if (key == target_key) {
            // Materialize this array.
            uint32_t type = 0;
            if (!c.u32(type) || type != gguf::kTypeArray) {
                error = "tokenizer: target " + target_key + " is not an array";
                return false;
            }
            uint32_t elem_type = 0;
            uint64_t count = 0;
            if (!c.u32(elem_type) || !c.u64(count)) {
                error = "tokenizer: array header truncated";
                return false;
            }
            if (count > kMaxTokens) {
                error = "tokenizer: vocab exceeds kMaxTokens";
                return false;
            }
            if (elem_type != gguf::kTypeString) {
                error = "tokenizer: target " + target_key + " is not a string array";
                return false;
            }

            out.reserve(static_cast<size_t>(count < 4096 ? count : 4096));

            for (uint64_t j = 0; j < count; ++j) {
                std::string s;
                if (!c.read_string(s)) {
                    error = "tokenizer: string element truncated";
                    return false;
                }
                if (s.size() > kMaxTokenBytes) {
                    // Truncate over-long token strings defensively (real
                    // models never exceed a few dozen bytes; a hostile
                    // file gets a bounded read, not a crash).
                    s.resize(kMaxTokenBytes);
                }
                out.push_back(std::move(s));
            }

            found = true;
        } else {
            if (!skip_value(c)) {
                error = "tokenizer: malformed value on re-walk at key " + key;
                return false;
            }
        }
    }

    if (!found) {
        // Not an error — caller decides whether the array is required.
        return true;
    }
    return true;
}

// Materialize a u32 array (used for token_type; GGUF stores it as int32).
bool materialize_u32_array(const gguf::GgufHeader& header,
                           const uint8_t* file_data, uint64_t file_size,
                           const std::string& target_key,
                           std::vector<uint32_t>& out,
                           std::string& error) {
    FileCursor c(file_data, file_size);

    const uint8_t* magic = nullptr;
    if (!c.take(4, magic)) { error = "tokenizer: file truncated"; return false; }
    static const uint8_t kMagic[4] = {'G', 'G', 'U', 'F'};
    if (std::memcmp(magic, kMagic, 4) != 0) {
        error = "tokenizer: bad magic";
        return false;
    }

    uint32_t version = 0;
    uint64_t tc = 0, kc = 0;
    if (!c.u32(version) || !c.u64(tc) || !c.u64(kc)) {
        error = "tokenizer: header truncated";
        return false;
    }
    if (kc != header.kv_count) {
        error = "tokenizer: kv_count mismatch";
        return false;
    }

    out.clear();

    for (uint64_t i = 0; i < kc; ++i) {
        std::string key;
        if (!c.read_string(key)) {
            error = "tokenizer: malformed key";
            return false;
        }

        if (key == target_key) {
            uint32_t type = 0;
            if (!c.u32(type) || type != gguf::kTypeArray) {
                error = "tokenizer: " + target_key + " is not an array";
                return false;
            }
            uint32_t elem_type = 0;
            uint64_t count = 0;
            if (!c.u32(elem_type) || !c.u64(count)) {
                error = "tokenizer: array header truncated";
                return false;
            }
            if (count > kMaxTokens) {
                error = "tokenizer: array exceeds kMaxTokens";
                return false;
            }
            // token_type is stored as int32 in GGUF.
            if (elem_type != gguf::kTypeInt32 && elem_type != gguf::kTypeUint32) {
                error = "tokenizer: " + target_key + " is not int32";
                return false;
            }

            out.reserve(static_cast<size_t>(count < 4096 ? count : 4096));
            for (uint64_t j = 0; j < count; ++j) {
                uint32_t v = 0;
                if (!c.u32(v)) {
                    error = "tokenizer: token_type truncated";
                    return false;
                }
                out.push_back(v);
            }
            return true;
        } else {
            if (!skip_value(c)) {
                error = "tokenizer: malformed value";
                return false;
            }
        }
    }

    return true; // not found — caller decides
}

// Materialize a float array (used for scores).
bool materialize_f32_array(const gguf::GgufHeader& header,
                           const uint8_t* file_data, uint64_t file_size,
                           const std::string& target_key,
                           std::vector<float>& out,
                           std::string& error) {
    FileCursor c(file_data, file_size);

    const uint8_t* magic = nullptr;
    if (!c.take(4, magic)) { error = "tokenizer: file truncated"; return false; }
    static const uint8_t kMagic[4] = {'G', 'G', 'U', 'F'};
    if (std::memcmp(magic, kMagic, 4) != 0) {
        error = "tokenizer: bad magic";
        return false;
    }

    uint32_t version = 0;
    uint64_t tc = 0, kc = 0;
    if (!c.u32(version) || !c.u64(tc) || !c.u64(kc)) {
        error = "tokenizer: header truncated";
        return false;
    }
    if (kc != header.kv_count) {
        error = "tokenizer: kv_count mismatch";
        return false;
    }

    out.clear();

    for (uint64_t i = 0; i < kc; ++i) {
        std::string key;
        if (!c.read_string(key)) {
            error = "tokenizer: malformed key";
            return false;
        }

        if (key == target_key) {
            uint32_t type = 0;
            if (!c.u32(type) || type != gguf::kTypeArray) {
                error = "tokenizer: " + target_key + " is not an array";
                return false;
            }
            uint32_t elem_type = 0;
            uint64_t count = 0;
            if (!c.u32(elem_type) || !c.u64(count)) {
                error = "tokenizer: array header truncated";
                return false;
            }
            if (count > kMaxTokens) {
                error = "tokenizer: array exceeds kMaxTokens";
                return false;
            }
            if (elem_type != gguf::kTypeFloat32) {
                error = "tokenizer: " + target_key + " is not float32";
                return false;
            }

            out.reserve(static_cast<size_t>(count < 4096 ? count : 4096));
            for (uint64_t j = 0; j < count; ++j) {
                double v = 0;
                if (!c.f32(v)) {
                    error = "tokenizer: score truncated";
                    return false;
                }
                out.push_back(static_cast<float>(v));
            }
            return true;
        } else {
            if (!skip_value(c)) {
                error = "tokenizer: malformed value";
                return false;
            }
        }
    }

    return true;
}

// Resolve a special-token id from a scalar key.
void resolve_special(const gguf::Metadata& md, const std::string& key,
                     uint32_t& id_out, bool& has_out) {
    uint64_t v = 0;
    bool present = false;
    if (read_u64_scalar(md, key, v, present) && present && v <= UINT32_MAX) {
        id_out = static_cast<uint32_t>(v);
        has_out = true;
    }
}

// utf8_decode_one decodes one UTF-8 codepoint starting at offset `i` of
// `s`. Returns the codepoint and the number of bytes consumed; on
// invalid UTF-8 it consumes one byte and returns U+FFFD.
struct Utf8Unit { uint32_t cp; size_t len; };
Utf8Unit utf8_decode_one(const std::string& s, size_t i) {
    Utf8Unit u{0xFFFDu, 1};
    if (i >= s.size()) {
        u.len = 0;
        return u;
    }

    unsigned char b0 = static_cast<unsigned char>(s[i]);

    // 1-byte (ASCII)
    if (b0 < 0x80u) {
        u.cp = b0;
        u.len = 1;
        return u;
    }

    auto next = [&](size_t k) -> unsigned char {
        return (i + k < s.size()) ? static_cast<unsigned char>(s[i + k]) : 0u;
    };

    // 2-byte
    if ((b0 & 0xE0u) == 0xC0u) {
        unsigned char b1 = next(1);
        if ((b1 & 0xC0u) == 0x80u) {
            u.cp = ((b0 & 0x1Fu) << 6) | (b1 & 0x3Fu);
            u.len = 2;
            return u;
        }
        return u; // invalid → replacement, 1 byte
    }

    // 3-byte
    if ((b0 & 0xF0u) == 0xE0u) {
        unsigned char b1 = next(1), b2 = next(2);
        if ((b1 & 0xC0u) == 0x80u && (b2 & 0xC0u) == 0x80u) {
            u.cp = ((b0 & 0x0Fu) << 12) | ((b1 & 0x3Fu) << 6) | (b2 & 0x3Fu);
            u.len = 3;
            return u;
        }
        return u;
    }

    // 4-byte
    if ((b0 & 0xF8u) == 0xF0u) {
        unsigned char b1 = next(1), b2 = next(2), b3 = next(3);
        if ((b1 & 0xC0u) == 0x80u && (b2 & 0xC0u) == 0x80u && (b3 & 0xC0u) == 0x80u) {
            u.cp = ((b0 & 0x07u) << 18) | ((b1 & 0x3Fu) << 12) |
                   ((b2 & 0x3Fu) << 6) | (b3 & 0x3Fu);
            u.len = 4;
            return u;
        }
        return u;
    }

    return u; // invalid lead byte
}

// preprocess_bpe_input transforms raw input text into the symbol stream
// the BPE merge algorithm expects. For Llama-style BPE models, every
// word boundary becomes a U+2581 (▁) marker — including the implicit
// boundary at the start of the input. So:
//   "hello world" → "▁hello▁world"
//   " hello"      → "▁hello"  (leading space collapses to one ▁)
//
// For gpt2-style BPE (model_name == "gpt2"), spaces stay as-is — gpt2's
// vocab encodes spaces inside tokens directly via byte-pair encoding.
std::string preprocess_bpe_input(const std::string& text, Model m,
                                 const std::string& model_name) {
    if (m == Model::kBPE && model_name != "gpt2") {
        // Llama-style: replace every ' ' with U+2581, and prepend a
        // leading ▁ if the input doesn't already start with whitespace
        // (mirrors llama.cpp's tokenizer behavior for instruct models).
        std::string out;
        out.reserve(text.size() + 3);

        bool starts_with_space = !text.empty() &&
            (text[0] == ' ' || text[0] == '\t' || text[0] == '\n');
        if (!starts_with_space) {
            out.push_back('\xE2');
            out.push_back('\x96');
            out.push_back('\x81');
        }

        for (size_t i = 0; i < text.size(); ++i) {
            unsigned char c = static_cast<unsigned char>(text[i]);
            if (c == ' ') {
                out.push_back('\xE2');
                out.push_back('\x96');
                out.push_back('\x81');
            } else {
                out.push_back(text[i]);
            }
        }
        return out;
    }
    return text;
}

// Split text into UTF-8 codepoint symbols for BPE merging. Each symbol
// is one UTF-8 codepoint (we operate on codepoints, not bytes, to keep
// the merge lookup correct for multi-byte scripts).
struct Symbol { std::string text; };

void split_utf8(const std::string& s, std::vector<Symbol>& out) {
    size_t i = 0;
    while (i < s.size()) {
        Utf8Unit u = utf8_decode_one(s, i);
        if (u.len == 0) break;
        out.push_back({s.substr(i, u.len)});
        i += u.len;
    }
}

// bpe_encode: the canonical merge-rank BPE algorithm.
//   1. preprocess input (replace spaces with U+2581 for Llama-style);
//   2. split into UTF-8 symbols;
//   3. iteratively merge the highest-priority adjacent pair until none
//      remains;
//   4. look up each merged symbol in token_to_id; unknown symbols become
//      the unknown token id (or byte tokens when present).
int32_t bpe_encode(const Vocab& v, const std::string& text,
                   const EncodeOptions& opts, EncodeResult& out,
                   std::string& error) {
    std::string pre = preprocess_bpe_input(text, v.model, v.model_name);
    std::vector<Symbol> syms;
    split_utf8(pre, syms);

    if (syms.empty()) {
        return SHTN_OK;
    }

    // Iteratively apply the best merge across the whole sequence.
    // This is O(n^2 * merges) worst-case but bounded by kMaxEncodeOutput
    // symbols (we cap input length there before merging).
    if (syms.size() > kMaxEncodeOutput) {
        syms.resize(kMaxEncodeOutput);
        out.truncated = true;
    }

    bool merged_any = true;
    while (merged_any && syms.size() > 1) {
        merged_any = false;
        uint32_t best_rank = UINT32_MAX;
        size_t best_pos = 0;

        for (size_t i = 0; i + 1 < syms.size(); ++i) {
            std::string key = syms[i].text + " " + syms[i + 1].text;
            auto it = v.merge_ranks.find(key);
            if (it != v.merge_ranks.end() && it->second < best_rank) {
                best_rank = it->second;
                best_pos = i;
            }
        }

        if (best_rank == UINT32_MAX) {
            break;
        }

        // Apply the merge at best_pos.
        syms[best_pos].text = syms[best_pos].text + syms[best_pos + 1].text;
        syms.erase(syms.begin() + best_pos + 1);
        merged_any = true;
    }

    // Resolve each symbol to a token id.
    for (const Symbol& sym : syms) {
        auto it = v.token_to_id.find(sym.text);
        if (it != v.token_to_id.end()) {
            out.ids.push_back(it->second);
        } else if (v.special.has_unknown) {
            out.ids.push_back(v.special.unknown);
        } else {
            // Fallback: emit one byte token per byte if the vocab has
            // byte tokens (token_type == kByte). We linear-scan the
            // token table for the matching byte token; this is bounded
            // by vocab size and only runs on the rare unknown-symbol
            // path.
            bool emitted = false;
            for (uint32_t j = 0; j < v.tokens.size(); ++j) {
                if (v.has_token_types && v.types[j] == TokenType::kByte) {
                    // The byte token's string is one byte (0x00..0xFF as
                    // a single-byte literal in GGUF).
                    if (v.tokens[j].size() == 1 && !sym.text.empty() &&
                        static_cast<unsigned char>(v.tokens[j][0]) ==
                            static_cast<unsigned char>(sym.text[0])) {
                        out.ids.push_back(j);
                        emitted = true;
                        break;
                    }
                }
            }
            if (!emitted) {
                // Hard fail: no unknown token, no byte token — the
                // input cannot be encoded. Surface this honestly.
                error = "tokenizer: cannot encode symbol (no unknown/byte token)";
                return SHTN_ERR_UNSUPPORTED;
            }
        }

        if (out.ids.size() >= opts.max_tokens) {
            out.truncated = true;
            break;
        }
    }

    return SHTN_OK;
}

// unigram_encode: greedy longest-match against the vocab.
// This is a deterministic simplification of SentencePiece Viterbi — it
// is NOT a full lattice, and we document that explicitly. It produces
// the same tokens as Viterbi for the common case where greedy matches
// the unique maximum; for ambiguous inputs Viterbi would differ.
int32_t unigram_encode(const Vocab& v, const std::string& text,
                       const EncodeOptions& opts, EncodeResult& out,
                       std::string& error) {
    size_t i = 0;
    while (i < text.size()) {
        // Try the longest match first (bounded by kMaxTokenBytes).
        size_t max_try = std::min(text.size() - i, static_cast<size_t>(kMaxTokenBytes));
        bool matched = false;

        for (size_t len = max_try; len >= 1; --len) {
            std::string sub = text.substr(i, len);
            auto it = v.token_to_id.find(sub);
            if (it != v.token_to_id.end()) {
                out.ids.push_back(it->second);
                i += len;
                matched = true;
                break;
            }
        }

        if (!matched) {
            if (v.special.has_unknown) {
                out.ids.push_back(v.special.unknown);
                // Advance one UTF-8 codepoint so we make progress.
                Utf8Unit u = utf8_decode_one(text, i);
                i += (u.len == 0 ? 1 : u.len);
            } else {
                error = "tokenizer: unigram: no match and no unknown token";
                return SHTN_ERR_UNSUPPORTED;
            }
        }

        if (out.ids.size() >= opts.max_tokens) {
            out.truncated = true;
            break;
        }
    }

    return SHTN_OK;
}

// wpm_encode: WordPiece greedy longest-match (BERT-style). Splits on
// whitespace first, then for each word tries longest-prefix match with
// the "##" continuation marker for non-initial subwords.
int32_t wpm_encode(const Vocab& v, const std::string& text,
                   const EncodeOptions& opts, EncodeResult& out,
                   std::string& error) {
    // Split on whitespace (BERT uses basic whitespace tokenization).
    std::vector<std::string> words;
    std::string cur;
    for (char c : text) {
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
            if (!cur.empty()) {
                words.push_back(std::move(cur));
                cur.clear();
            }
        } else {
            cur.push_back(c);
        }
    }
    if (!cur.empty()) words.push_back(std::move(cur));

    for (const std::string& word : words) {
        size_t i = 0;
        bool is_first = true;
        while (i < word.size()) {
            size_t max_try = std::min(word.size() - i, static_cast<size_t>(kMaxTokenBytes));
            bool matched = false;
            for (size_t len = max_try; len >= 1; --len) {
                std::string sub = word.substr(i, len);
                std::string key = is_first ? sub : ("##" + sub);
                auto it = v.token_to_id.find(key);
                if (it != v.token_to_id.end()) {
                    out.ids.push_back(it->second);
                    i += len;
                    matched = true;
                    is_first = false;
                    break;
                }
            }
            if (!matched) {
                if (v.special.has_unknown) {
                    out.ids.push_back(v.special.unknown);
                    // Skip the rest of the word on unknown.
                    i = word.size();
                } else {
                    error = "tokenizer: wpm: no match and no unknown token";
                    return SHTN_ERR_UNSUPPORTED;
                }
            }
            if (out.ids.size() >= opts.max_tokens) {
                out.truncated = true;
                return SHTN_OK;
            }
        }
    }

    return SHTN_OK;
}

} // namespace

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

int32_t init(const gguf::GgufHeader& header, Vocab& vocab, std::string& error) {
    vocab = Vocab{};

    if (header.metadata.empty()) {
        error = "tokenizer: no metadata in header";
        return SHTN_ERR_NO_MODEL;
    }

    // Resolve the tokenizer model kind.
    const gguf::Scalar* model_scalar = find_meta(header.metadata, "tokenizer.ggml.model");
    if (model_scalar == nullptr || model_scalar->type != gguf::kTypeString) {
        error = "tokenizer: tokenizer.ggml.model is missing or not a string";
        return SHTN_ERR_MODEL_FORMAT;
    }

    vocab.model_name = model_scalar->str_v;

    if (vocab.model_name == "llama" || vocab.model_name == "gpt2") {
        vocab.model = Model::kBPE;
    } else if (vocab.model_name == "t5" || vocab.model_name == "unigram" ||
               vocab.model_name == "sentencepiece") {
        vocab.model = Model::kUnigram;
    } else if (vocab.model_name == "bert") {
        vocab.model = Model::kWPM;
    } else {
        // Unknown tokenizer model kind: refuse to initialize rather than
        // guess at semantics. The host reports unsupported; llama.cpp
        // remains the generation backend.
        error = "tokenizer: unsupported tokenizer.ggml.model '" +
                vocab.model_name + "'";
        return SHTN_ERR_UNSUPPORTED;
    }

    // The vocab arrays live in the mapped file. The header metadata tells
    // us they EXIST (find_meta succeeds and reports array_count), but the
    // parser skipped the element bytes. We re-walk the mapping here.
    //
    // To do that we need the raw mapping pointer. The header does NOT
    // retain it (by design — gguf.cpp is decoupled from the mapping).
    // So we expose the mapping through a static thread-local pointer set
    // by Model::load just before calling init(). This is the same pattern
    // the model uses for fill_info (it re-reads metadata via the header).
    //
    // ACTUALLY — the simpler, cleaner approach is to have the host pass
    // us the mapping. We expose init_with_mapping below and the Model
    // class calls it with its MappedFile.
    error = "tokenizer: init called without a mapping (use init_with_mapping)";
    return SHTN_ERR_INTERNAL;
}

// init_with_mapping is the real entry point the Model calls — it has
// access to the still-open memory mapping so the tokenizer arrays can
// be materialized.
int32_t init_with_mapping(const gguf::GgufHeader& header,
                          const uint8_t* file_data, uint64_t file_size,
                          Vocab& vocab, std::string& error) {
    vocab = Vocab{};

    if (file_data == nullptr || file_size == 0) {
        error = "tokenizer: no mapping";
        return SHTN_ERR_NO_MODEL;
    }

    if (header.metadata.empty()) {
        error = "tokenizer: no metadata in header";
        return SHTN_ERR_NO_MODEL;
    }

    // Resolve the tokenizer model kind.
    const gguf::Scalar* model_scalar = find_meta(header.metadata, "tokenizer.ggml.model");
    if (model_scalar == nullptr || model_scalar->type != gguf::kTypeString) {
        error = "tokenizer: tokenizer.ggml.model is missing or not a string";
        return SHTN_ERR_MODEL_FORMAT;
    }

    vocab.model_name = model_scalar->str_v;

    if (vocab.model_name == "llama" || vocab.model_name == "gpt2") {
        vocab.model = Model::kBPE;
    } else if (vocab.model_name == "t5" || vocab.model_name == "unigram" ||
               vocab.model_name == "sentencepiece") {
        vocab.model = Model::kUnigram;
    } else if (vocab.model_name == "bert") {
        vocab.model = Model::kWPM;
    } else {
        error = "tokenizer: unsupported tokenizer.ggml.model '" +
                vocab.model_name + "'";
        return SHTN_ERR_UNSUPPORTED;
    }

    // Materialize the tokens array (required).
    const gguf::Scalar* tokens_scalar = find_meta(header.metadata, "tokenizer.ggml.tokens");
    if (tokens_scalar == nullptr || tokens_scalar->type != gguf::kTypeArray ||
        tokens_scalar->array_count == 0) {
        error = "tokenizer: tokenizer.ggml.tokens is missing or empty";
        return SHTN_ERR_MODEL_FORMAT;
    }

    if (!materialize_string_array(header, file_data, file_size,
                                  "tokenizer.ggml.tokens", vocab.tokens, error)) {
        return SHTN_ERR_MODEL_FORMAT;
    }

    if (vocab.tokens.empty()) {
        error = "tokenizer: tokenizer.ggml.tokens array is empty";
        return SHTN_ERR_MODEL_FORMAT;
    }

    // Materialize token_type (optional; default to kNormal).
    std::vector<uint32_t> raw_types;
    if (materialize_u32_array(header, file_data, file_size,
                              "tokenizer.ggml.token_type", raw_types, error)) {
        if (raw_types.size() == vocab.tokens.size()) {
            vocab.has_token_types = true;
            vocab.types.reserve(raw_types.size());
            for (uint32_t t : raw_types) {
                switch (t) {
                case 1: vocab.types.push_back(TokenType::kNormal); break;
                case 2: vocab.types.push_back(TokenType::kUnknown); break;
                case 3: vocab.types.push_back(TokenType::kControl); break;
                case 4: vocab.types.push_back(TokenType::kUserDefined); break;
                case 5: vocab.types.push_back(TokenType::kUnused); break;
                case 6: vocab.types.push_back(TokenType::kByte); break;
                default: vocab.types.push_back(TokenType::kNormal); break;
                }
            }
        }
    }
    if (!vocab.has_token_types) {
        vocab.types.assign(vocab.tokens.size(), TokenType::kNormal);
    }

    // Materialize scores (optional; used by unigram for likelihood).
    materialize_f32_array(header, file_data, file_size,
                          "tokenizer.ggml.scores", vocab.scores, error);
    // Reset error: scores are optional; a failed read is not fatal.
    error.clear();
    if (vocab.scores.size() != vocab.tokens.size()) {
        vocab.scores.clear();
    }

    // Materialize merges (BPE only).
    if (vocab.model == Model::kBPE) {
        std::vector<std::string> merges;
        if (!materialize_string_array(header, file_data, file_size,
                                      "tokenizer.ggml.merges", merges, error)) {
            return SHTN_ERR_MODEL_FORMAT;
        }
        for (uint32_t r = 0; r < merges.size(); ++r) {
            // merges are stored as "a b" — store directly as the key.
            vocab.merge_ranks[merges[r]] = r;
        }
    }

    // Build reverse lookup. For unigram/WPM we use the raw token string.
    // For BPE the same — the GGUF token strings already include the
    // U+2581 marker where the model expects it.
    vocab.token_to_id.reserve(vocab.tokens.size() * 2);
    for (uint32_t i = 0; i < vocab.tokens.size(); ++i) {
        // Skip control/unused tokens in the reverse map (they cannot
        // appear in encoded text).
        if (vocab.has_token_types) {
            if (vocab.types[i] == TokenType::kControl ||
                vocab.types[i] == TokenType::kUnused) {
                continue;
            }
        }
        // Last-wins is the GGUF convention (later tokens override).
        vocab.token_to_id[vocab.tokens[i]] = i;
    }

    // Resolve special token ids from scalars.
    resolve_special(header.metadata, "tokenizer.ggml.bos_token_id",
                    vocab.special.bos, vocab.special.has_bos);
    resolve_special(header.metadata, "tokenizer.ggml.eos_token_id",
                    vocab.special.eos, vocab.special.has_eos);
    resolve_special(header.metadata, "tokenizer.ggml.unknown_token_id",
                    vocab.special.unknown, vocab.special.has_unknown);
    // Note: GGUF spells this "seperator" (typo in the original spec) —
    // we honor both spellings defensively.
    resolve_special(header.metadata, "tokenizer.ggml.seperator_token_id",
                    vocab.special.separator, vocab.special.has_separator);
    resolve_special(header.metadata, "tokenizer.ggml.separator_token_id",
                    vocab.special.separator, vocab.special.has_separator);
    resolve_special(header.metadata, "tokenizer.ggml.padding_token_id",
                    vocab.special.padding, vocab.special.has_padding);
    resolve_special(header.metadata, "tokenizer.ggml.eot_token_id",
                    vocab.special.eot, vocab.special.has_eot);

    return SHTN_OK;
}

int32_t encode(const Vocab& v, const std::string& text,
               const EncodeOptions& opts, EncodeResult& out,
               std::string& error) {
    out = EncodeResult{};

    if (!v.initialized()) {
        error = "tokenizer: vocab is not initialized";
        return SHTN_ERR_MODEL_STATE;
    }

    if (opts.max_tokens == 0 || opts.max_tokens > kMaxEncodeOutput) {
        error = "tokenizer: max_tokens out of range";
        return SHTN_ERR_INVALID_ARG;
    }

    int32_t rc = SHTN_OK;
    switch (v.model) {
    case Model::kBPE:
        rc = bpe_encode(v, text, opts, out, error);
        break;
    case Model::kUnigram:
        rc = unigram_encode(v, text, opts, out, error);
        break;
    case Model::kWPM:
        rc = wpm_encode(v, text, opts, out, error);
        break;
    default:
        error = "tokenizer: unknown model kind";
        return SHTN_ERR_UNSUPPORTED;
    }

    if (rc != SHTN_OK) {
        return rc;
    }

    // BOS / EOS bookkeeping.
    std::vector<uint32_t>& ids = out.ids;
    if (opts.add_bos && v.special.has_bos) {
        ids.insert(ids.begin(), v.special.bos);
        if (ids.size() > opts.max_tokens) {
            ids.resize(opts.max_tokens);
            out.truncated = true;
        }
    }
    if (opts.add_eos && v.special.has_eos) {
        if (ids.size() < opts.max_tokens) {
            ids.push_back(v.special.eos);
        } else {
            out.truncated = true;
        }
    }

    return SHTN_OK;
}

int32_t decode(const Vocab& v, const uint32_t* ids, size_t count,
               const DecodeOptions& opts, DecodeResult& out,
               std::string& error) {
    out = DecodeResult{};

    if (!v.initialized()) {
        error = "tokenizer: vocab is not initialized";
        return SHTN_ERR_MODEL_STATE;
    }

    if (ids == nullptr && count > 0) {
        error = "tokenizer: null ids with non-zero count";
        return SHTN_ERR_INVALID_ARG;
    }

    out.text.reserve(count * 4); // rough UTF-8 expansion estimate

    for (size_t i = 0; i < count; ++i) {
        uint32_t id = ids[i];

        if (id >= v.tokens.size()) {
            error = "tokenizer: token id out of range";
            return SHTN_ERR_INVALID_ARG;
        }

        TokenType type = v.has_token_types ? v.types[id] : TokenType::kNormal;

        // Skip control tokens (unless they're BOS/EOS and we keep specials).
        if (type == TokenType::kControl) {
            if (opts.skip_special) {
                continue;
            }
            // If we're keeping specials, only BOS/EOS emit anything; other
            // control tokens still skip (they have no useful text).
            bool is_bos = v.special.has_bos && id == v.special.bos;
            bool is_eos = v.special.has_eos && id == v.special.eos;
            if (!is_bos && !is_eos) {
                continue;
            }
            // BOS/EOS as control tokens: emit nothing (they're structural).
            continue;
        }

        if (type == TokenType::kUnused) {
            continue;
        }

        // Special tokens (BOS/EOS/UNK/PAD/SEP) when skip_special=true.
        bool is_special = false;
        if (v.special.has_bos && id == v.special.bos) is_special = true;
        if (v.special.has_eos && id == v.special.eos) is_special = true;
        if (v.special.has_unknown && id == v.special.unknown) is_special = true;
        if (v.special.has_padding && id == v.special.padding) is_special = true;
        if (v.special.has_separator && id == v.special.separator) is_special = true;
        if (is_special && opts.skip_special) {
            continue;
        }

        // Byte token: emit the raw byte value (the token string is one byte).
        if (type == TokenType::kByte) {
            if (!v.tokens[id].empty()) {
                out.text.push_back(v.tokens[id][0]);
            }
        } else {
            // Normal/user-defined/unknown: append the token string verbatim.
            // For Llama-style BPE the token string already contains U+2581
            // markers — convert them back to ASCII space here.
            const std::string& tok = v.tokens[id];
            for (size_t k = 0; k < tok.size(); ++k) {
                // U+2581 in UTF-8 is E2 96 81.
                if (k + 2 < tok.size() &&
                    static_cast<unsigned char>(tok[k]) == 0xE2u &&
                    static_cast<unsigned char>(tok[k + 1]) == 0x96u &&
                    static_cast<unsigned char>(tok[k + 2]) == 0x81u) {
                    out.text.push_back(' ');
                    k += 2;
                } else {
                    out.text.push_back(tok[k]);
                }
            }
        }

        if (out.text.size() >= opts.max_bytes) {
            out.text.resize(opts.max_bytes);
            out.truncated = true;
            break;
        }
    }

    return SHTN_OK;
}

void fill_info(const Vocab& vocab, InfoSnapshot& out) {
    out = InfoSnapshot{};
    out.initialized = vocab.initialized();
    out.model = vocab.model;
    out.vocab_size = vocab.size();
    out.merge_count = static_cast<uint32_t>(vocab.merge_ranks.size());
    out.has_bos = vocab.special.has_bos;
    out.has_eos = vocab.special.has_eos;
    out.has_unknown = vocab.special.has_unknown;
    out.bos_id = vocab.special.bos;
    out.eos_id = vocab.special.eos;
    out.unknown_id = vocab.special.unknown;
    out.model_name = vocab.model_name;
}

} // namespace tokenizer
} // namespace shtn
