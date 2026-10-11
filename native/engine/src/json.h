// json.h — minimal JSON parsing/serialization for the IPC host.
//
// The wire protocol needs exactly three things:
//   1. parse a request object: {"id": <int>, "op": "<string>", "payload": {...}}
//      (tolerantly — extra members, nested values and malformed input
//      must never crash the host);
//   2. extract typed members from a payload object (the model ops carry
//      {"path": "...", "contextLength": N});
//   3. serialize response objects with correctly escaped strings.
//
// No third-party dependency, no allocations beyond the result. This is
// deliberately NOT a general-purpose JSON library.

#ifndef SHTN_JSON_H
#define SHTN_JSON_H

#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <string>
#include <vector>

namespace shtn {
namespace json {

// Parsed is the extracted request envelope.
struct Parsed {
    bool valid;       // syntactically a JSON object with id/op readable
    int64_t id;
    std::string op;
    std::string error; // human-readable parse failure detail

    // Raw bytes of the "payload" member when present (op-specific;
    // extracted verbatim so ops can parse their own shape).
    bool has_payload = false;
    std::string payload_raw;
};

// Parse extracts "id" and "op" (and the raw "payload" member when
// present) from a top-level JSON object. Unknown or malformed members
// are ignored unless they break structure; structural breakage is
// reported via valid=false + error.
Parsed parse_request(const std::string& bytes);

// ExtractLoadPayload parses the model-load payload:
// {"path": "<file>", "contextLength": <optional uint>} — path is
// required; contextLength optional (0 when absent). Returns false with
// `error` filled on any structural problem.
bool extract_load_payload(const std::string& bytes, std::string& path,
                          uint32_t& context_length, std::string& error);

// ExtractEncodePayload parses the tokenizer_encode payload:
// {"text": "...", "addBos": bool, "addEos": bool, "maxTokens": uint}.
// text is required; the rest optional (defaults: false/false/256).
bool extract_encode_payload(const std::string& bytes, std::string& text,
                            int32_t& add_bos, int32_t& add_eos,
                            uint32_t& max_tokens, std::string& error);

// ExtractDecodePayload parses the tokenizer_decode payload:
// {"ids": [int,...], "skipSpecial": bool, "maxBytes": uint}.
bool extract_decode_payload(const std::string& bytes,
                            std::vector<uint32_t>& ids,
                            int32_t& skip_special, uint32_t& max_bytes,
                            std::string& error);

// ExtractGeneratePayload parses the generate op payload:
// {"requestId": "...", "prompt": "...", "maxTokens": N, "temperature": f,
//  "topK": n, "topP": f, "repetitionPenalty": f, "repeatLastN": n,
//  "seed": n}. prompt and maxTokens are REQUIRED; the rest optional.
bool extract_generate_payload(const std::string& bytes,
                              std::string& request_id, std::string& prompt,
                              uint32_t& max_tokens, float& temperature,
                              int32_t& top_k, float& top_p,
                              float& repetition_penalty,
                              uint32_t& repeat_last_n, uint64_t& seed,
                              std::string& error);

// ExtractRequestIdPayload parses {"requestId": "..."} (cancel op).
bool extract_request_id_payload(const std::string& bytes,
                                std::string& request_id, std::string& error);

// Escape returns src quoted and escaped as a JSON string literal
// (control characters, quotes, backslashes, and invalid UTF-8 bytes).
std::string quote(const std::string& src);

// detail — shared scanner, exposed so op handlers can parse their own
// payload shapes with the same bounds-checked machinery.
namespace detail {

class Scanner {
public:
    Scanner(const char* data, size_t size)
        : data_(data), size_(size), pos_(0) {}

    void skip_ws() {
        while (pos_ < size_) {
            const char c = data_[pos_];
            if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                ++pos_;
            } else {
                break;
            }
        }
    }

    bool at_end() const { return pos_ >= size_; }
    char peek() const { return pos_ < size_ ? data_[pos_] : '\0'; }
    size_t position() const { return pos_; }

    bool consume(char c) {
        if (pos_ < size_ && data_[pos_] == c) {
            ++pos_;
            return true;
        }
        return false;
    }

    // read_string parses a JSON string literal (including escapes).
    bool read_string(std::string& out) {
        out.clear();

        if (!consume('"')) {
            return false;
        }

        while (pos_ < size_) {
            const char c = data_[pos_++];

            if (c == '"') {
                return true;
            }

            if (static_cast<unsigned char>(c) < 0x20) {
                return false; // raw control character: invalid JSON
            }

            if (c != '\\') {
                out.push_back(c);
                continue;
            }

            if (pos_ >= size_) {
                return false;
            }

            const char esc = data_[pos_++];

            switch (esc) {
            case '"': out.push_back('"'); break;
            case '\\': out.push_back('\\'); break;
            case '/': out.push_back('/'); break;
            case 'b': out.push_back('\b'); break;
            case 'f': out.push_back('\f'); break;
            case 'n': out.push_back('\n'); break;
            case 'r': out.push_back('\r'); break;
            case 't': out.push_back('\t'); break;
            case 'u': {
                if (pos_ + 4 > size_) {
                    return false;
                }

                unsigned code = 0;
                for (int i = 0; i < 4; ++i) {
                    code <<= 4;
                    const char h = data_[pos_++];
                    if (h >= '0' && h <= '9') {
                        code |= static_cast<unsigned>(h - '0');
                    } else if (h >= 'a' && h <= 'f') {
                        code |= static_cast<unsigned>(h - 'a' + 10);
                    } else if (h >= 'A' && h <= 'F') {
                        code |= static_cast<unsigned>(h - 'A' + 10);
                    } else {
                        return false;
                    }
                }

                // Encode BMP scalar as UTF-8 (surrogates pass through
                // paired; a lone surrogate is tolerated as replacement).
                if (code >= 0xD800u && code <= 0xDFFFu) {
                    out.push_back('\xEF'); out.push_back('\xBF'); out.push_back('\xBD');
                } else if (code < 0x80u) {
                    out.push_back(static_cast<char>(code));
                } else if (code < 0x800u) {
                    out.push_back(static_cast<char>(0xC0u | (code >> 6)));
                    out.push_back(static_cast<char>(0x80u | (code & 0x3Fu)));
                } else {
                    out.push_back(static_cast<char>(0xE0u | (code >> 12)));
                    out.push_back(static_cast<char>(0x80u | ((code >> 6) & 0x3Fu)));
                    out.push_back(static_cast<char>(0x80u | (code & 0x3Fu)));
                }
                break;
            }
            default:
                return false; // invalid escape
            }
        }

        return false; // unterminated string
    }

    // skip_value skips any JSON value (object/array/string/number/bool/
    // null) without retaining it. Objects are key:value sequences; arrays
    // are bare value sequences — both handled recursively.
    bool skip_value() {
        skip_ws();

        if (at_end()) {
            return false;
        }

        const char c = peek();

        if (c == '"') {
            std::string sink;
            return read_string(sink);
        }

        if (c == '{' || c == '[') {
            const char open = c;
            const char close = (open == '{') ? '}' : ']';
            const bool is_object = (open == '{');

            if (!consume(open)) {
                return false;
            }

            skip_ws();

            if (consume(close)) {
                return true; // empty container
            }

            for (;;) {
                if (is_object) {
                    // object member: string key, ':', value
                    std::string key;

                    skip_ws();

                    if (!read_string(key)) {
                        return false;
                    }

                    skip_ws();

                    if (!consume(':')) {
                        return false;
                    }
                }

                if (!skip_value()) {
                    return false;
                }

                skip_ws();

                if (consume(',')) {
                    continue;
                }

                skip_ws();

                if (consume(close)) {
                    return true;
                }

                return false;
            }
        }

        // number / true / false / null: consume a run of legal bytes.
        size_t start = pos_;

        while (pos_ < size_) {
            const char b = data_[pos_];

            const bool legal =
                (b >= '0' && b <= '9') || b == '-' || b == '+' || b == '.' ||
                b == 'e' || b == 'E' || b == 't' || b == 'r' || b == 'u' ||
                b == 'f' || b == 'a' || b == 'l' || b == 's' || b == 'n';

            if (!legal) {
                break;
            }
            ++pos_;
        }

        return pos_ > start;
    }

    // read_number consumes a numeric literal into out.
    bool read_number(double& out) {
        skip_ws();

        const size_t start = pos_;

        while (pos_ < size_) {
            const char b = data_[pos_];
            const bool legal =
                (b >= '0' && b <= '9') || b == '-' || b == '+' || b == '.' ||
                b == 'e' || b == 'E';
            if (!legal) {
                break;
            }
            ++pos_;
        }

        if (pos_ == start) {
            return false;
        }

        out = std::strtod(std::string(data_ + start, pos_ - start).c_str(), nullptr);
        return true;
    }

private:
    const char* data_;
    size_t size_;
    size_t pos_;
};

} // namespace detail

} // namespace json
} // namespace shtn

#endif /* SHTN_JSON_H */
