// tokenizer.h — the SHEYTAN native engine tokenizer concern (Phase 4).
//
// Real GGUF-backed BPE/Unigram tokenizer: encodes UTF-8 text to token
// ids and decodes token ids back to UTF-8 text, using the tokenizer
// metadata arrays stored inside the GGUF file
// (tokenizer.ggml.tokens / token_type / scores / merges / bos / eos /
// unknown / eot / sep / pad). Implemented WITHOUT llama.cpp: the engine
// reads its own tokenizer arrays from the memory-mapped model file.
//
// What this is NOT:
//   - this is NOT a model forward pass; it produces token ids, not logits;
//   - this does NOT call llama.cpp;
//   - this does NOT fake anything — a tokenizer that cannot be initialized
//     for a given model returns SHTN_ERR_UNSUPPORTED, and the host reports
//     that honestly (the llama.cpp fallback still serves generation).
//
// SECURITY:
//   - the mapped file is UNTRUSTED INPUT — every length is bounded
//     (kMaxTokens, kMaxMerges, kMaxTokenBytes) and every arithmetic is
//     checked;
//   - no unbounded token streams; the encoder caps output at max_tokens
//     and the decoder caps output at max_bytes;
//   - the encode/decode path NEVER copies the model file — it reads
//     strings from the mapping on demand (lazy token-table read).

#ifndef SHTN_TOKENIZER_H
#define SHTN_TOKENIZER_H

#include "shtn/types.h"

#include "gguf.h"

#include <cstdint>
#include <string>
#include <unordered_map>
#include <vector>

namespace shtn {
namespace tokenizer {

// Hostile-input bounds (kept generous for real models, bounded against
// pathological headers — same posture as gguf.cpp).
constexpr uint64_t kMaxTokens = 1u << 20;       // 1M vocab entries
constexpr uint64_t kMaxMerges = 1u << 20;       // 1M BPE merges
constexpr uint64_t kMaxTokenBytes = 256;        // one token string cap
constexpr uint64_t kMaxEncodeOutput = 1u << 16; // 64k tokens per encode

// GGUF tokenizer model kinds (tokenizer.ggml.model).
enum class Model : uint32_t {
    kUnknown = 0,
    kBPE     = 1, // Llama-style BPE: tokens + merges + scores
    kUnigram = 2, // SentencePiece-style (tokens + scores; no merges)
    kWPM     = 3, // WordPiece (BERT) — supported for encode/decode only
};

// TokenType mirrors GGUF tokenizer.ggml.token_type values.
enum class TokenType : uint8_t {
    kNormal       = 1,
    kUnknown      = 2,
    kControl      = 3,
    kUserDefined  = 4,
    kUnused       = 5,
    kByte         = 6,
};

// Special token ids resolved from GGUF metadata (0 = not present).
struct SpecialIds {
    uint32_t bos = 0;          // tokenizer.ggml.bos_token_id
    uint32_t eos = 0;          // tokenizer.ggml.eos_token_id
    uint32_t unknown = 0;      // tokenizer.ggml.unknown_token_id
    uint32_t separator = 0;    // tokenizer.ggml.seperator_token_id (typo in spec)
    uint32_t padding = 0;      // tokenizer.ggml.padding_token_id
    uint32_t eot = 0;          // tokenizer.ggml.eot_token_id
    bool has_bos = false;
    bool has_eos = false;
    bool has_unknown = false;
    bool has_separator = false;
    bool has_padding = false;
    bool has_eot = false;
};

// Vocab is the materialized tokenizer vocabulary. Lazy: constructed only
// when the host asks for tokenizer_init; released on model unload.
//
// Memory model: token strings are owned here (copied out of the mapping
// once, on init — bounded by kMaxTokens * kMaxTokenBytes). This is a
// bounded, deliberate materialization, NOT a copy of the model.
struct Vocab {
    Model model = Model::kUnknown;

    // The vocab table: token_id -> (string, type, score).
    std::vector<std::string> tokens;
    std::vector<TokenType>   types;
    std::vector<float>       scores;   // empty when GGUF had none

    // BPE merges as "a b" -> rank (lower rank = higher priority).
    // Empty for unigram/WPM models.
    std::unordered_map<std::string, uint32_t> merge_ranks;

    // Reverse lookup: token string -> id. For BPE this is the raw token
    // (no leading space marker transformation — the GGUF token bytes are
    // stored verbatim). For unigram, the same.
    std::unordered_map<std::string, uint32_t> token_to_id;

    // Special tokens resolved from GGUF scalars.
    SpecialIds special;

    // Whether token_type array was present (controls whether the encoder
    // treats unknown bytes specially).
    bool has_token_types = false;

    // Raw model name string ("LLaMA", "gpt2", "bert", ...) — informational.
    std::string model_name;

    bool initialized() const { return model != Model::kUnknown && !tokens.empty(); }
    uint32_t size() const { return static_cast<uint32_t>(tokens.size()); }
};

// Init reads the tokenizer arrays out of a parsed GGUF header. Returns:
//   SHTN_OK               vocab materialized;
//   SHTN_ERR_MODEL_FORMAT the GGUF has no tokenizer.ggml.tokens array;
//   SHTN_ERR_UNSUPPORTED  the tokenizer model kind is not implemented;
//   SHTN_ERR_NO_MODEL     header has no parsed metadata;
//   SHTN_ERR_INTERNAL     bounds violation mid-read.
//
// `header` is the already-parsed GGUF header (Model owns it). The vocab
// is BOUND to that header's mapping — caller must keep the model loaded
// for the lifetime of the vocab (unload invalidates both).
//
// init() is the documented entry point but requires the raw mapping; the
// host calls init_with_mapping() (declared below) which has access to
// the still-open MappedFile. init() returns SHTN_ERR_INTERNAL with a
// clear message if called directly — it exists only to keep the API
// surface readable.
int32_t init(const gguf::GgufHeader& header, Vocab& vocab, std::string& error);

// init_with_mapping is the real entry point: it re-walks the memory
// mapping to materialize the tokenizer arrays (the first-pass parser
// skips array element bytes — see gguf.cpp read_value).
int32_t init_with_mapping(const gguf::GgufHeader& header,
                          const uint8_t* file_data, uint64_t file_size,
                          Vocab& vocab, std::string& error);

// EncodeOptions configures one encode call.
struct EncodeOptions {
    // AddBOS: prepend the BOS token (when the vocab has one).
    bool add_bos = false;
    // AddEOS: append the EOS token (when the vocab has one).
    bool add_eos = false;
    // MaxTokens caps the output; overflow returns SHTN_ERR_INVALID_ARG.
    uint32_t max_tokens = static_cast<uint32_t>(kMaxEncodeOutput);
};

// EncodeResult holds the encode outcome.
struct EncodeResult {
    std::vector<uint32_t> ids;
    // Truncated reports whether the input was cut at max_tokens.
    bool truncated = false;
};

// Encode converts UTF-8 text to token ids deterministically.
//   - BPE: byte-pair encoding using merge ranks; unknown bytes become
//     the unknown token id (or byte tokens when the vocab has them);
//   - Unigram: greedy longest-match against the vocab (a faithful
//     simplification of SentencePiece Viterbi — chosen for determinism
//     and bounded compute; documented as such, not claimed as a full
//     Viterbi lattice);
//   - WPM: greedy whitespace-split WordPiece (BERT-style; control/byte
//     handling faithful to the GGUF metadata).
//
// Returns SHTN_OK or a negative error code; never throws.
int32_t encode(const Vocab& vocab, const std::string& text,
               const EncodeOptions& opts, EncodeResult& out,
               std::string& error);

// Decode converts token ids back to UTF-8 text.
//   - control tokens are skipped (TokenType::kControl) UNLESS they are
//     the BOS/EOS and skip_special=false;
//   - byte tokens (TokenType::kByte) are emitted as their raw byte;
//   - unknown tokens emit the unknown token's string when present;
//   - out is capped at max_bytes (default 1 MiB); overflow truncates.
//
// Returns SHTN_OK or a negative error code; out.truncated is set on cap.
struct DecodeOptions {
    bool skip_special = true;
    uint32_t max_bytes = 1u << 20; // 1 MiB cap
};

struct DecodeResult {
    std::string text;
    bool truncated = false;
};

int32_t decode(const Vocab& vocab, const uint32_t* ids, size_t count,
               const DecodeOptions& opts, DecodeResult& out,
               std::string& error);

// Fill the ABI snapshot (used by the tokenizer_info op). Only fields the
// engine actually read are filled; unknowns stay zero/empty.
struct InfoSnapshot {
    bool initialized;
    Model model;
    uint32_t vocab_size;
    uint32_t merge_count;
    bool has_bos;
    bool has_eos;
    bool has_unknown;
    uint32_t bos_id;
    uint32_t eos_id;
    uint32_t unknown_id;
    std::string model_name;
};

void fill_info(const Vocab& vocab, InfoSnapshot& out);

} // namespace tokenizer
} // namespace shtn

#endif /* SHTN_TOKENIZER_H */
