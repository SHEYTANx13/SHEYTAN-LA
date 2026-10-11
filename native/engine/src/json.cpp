// json.cpp — minimal JSON parse/serialize implementation.

#include "json.h"

#include <cstdio>
#include <cstring>

namespace shtn {
namespace json {

Parsed parse_request(const std::string& bytes) {
    Parsed result;
    result.valid = false;
    result.id = 0;

    detail::Scanner scanner(bytes.data(), bytes.size());

    scanner.skip_ws();

    if (!scanner.consume('{')) {
        result.error = "request is not a JSON object";
        return result;
    }

    bool have_id = false;
    bool have_op = false;

    scanner.skip_ws();

    if (scanner.consume('}')) {
        result.error = "request object is empty";
        return result;
    }

    for (;;) {
        scanner.skip_ws();

        std::string key;

        if (!scanner.read_string(key)) {
            result.error = "malformed object key";
            return result;
        }

        scanner.skip_ws();

        if (!scanner.consume(':')) {
            result.error = "expected ':' after key";
            return result;
        }

        scanner.skip_ws();

        if (key == "id") {
            double value = 0;
            if (!scanner.read_number(value)) {
                result.error = "id is not a number";
                return result;
            }
            result.id = static_cast<int64_t>(value);
            have_id = true;
        } else if (key == "op") {
            if (!scanner.read_string(result.op)) {
                result.error = "op is not a string";
                return result;
            }
            have_op = true;
        } else if (key == "payload") {
            // Capture the raw payload member so op handlers can parse
            // their own shape with the same scanner machinery.
            const size_t start = scanner.position();
            if (!scanner.skip_value()) {
                result.error = "malformed value for member 'payload'";
                return result;
            }
            result.payload_raw.assign(bytes.data() + start,
                                      scanner.position() - start);
            result.has_payload = true;
        } else {
            if (!scanner.skip_value()) {
                result.error = "malformed value for member '" + key + "'";
                return result;
            }
        }

        scanner.skip_ws();

        if (scanner.consume(',')) {
            continue;
        }

        scanner.skip_ws();

        if (scanner.consume('}')) {
            break;
        }

        result.error = "expected ',' or '}' in object";
        return result;
    }

    if (!have_id) {
        result.error = "request has no id";
        return result;
    }

    if (!have_op || result.op.empty()) {
        result.error = "request has no op";
        return result;
    }

    // Trailing garbage after the object is rejected (one frame = one
    // value).
    scanner.skip_ws();

    if (!scanner.at_end()) {
        result.error = "trailing data after request object";
        return result;
    }

    result.valid = true;
    return result;
}

bool extract_load_payload(const std::string& bytes, std::string& path,
                          uint32_t& context_length, std::string& error) {
    path.clear();
    context_length = 0;

    detail::Scanner scanner(bytes.data(), bytes.size());

    scanner.skip_ws();

    if (!scanner.consume('{')) {
        error = "payload is not a JSON object";
        return false;
    }

    bool have_path = false;

    scanner.skip_ws();

    if (scanner.consume('}')) {
        error = "payload has no path";
        return false;
    }

    for (;;) {
        scanner.skip_ws();

        std::string key;

        if (!scanner.read_string(key)) {
            error = "malformed payload key";
            return false;
        }

        scanner.skip_ws();

        if (!scanner.consume(':')) {
            error = "expected ':' after payload key";
            return false;
        }

        scanner.skip_ws();

        if (key == "path") {
            if (!scanner.read_string(path)) {
                error = "payload path is not a string";
                return false;
            }
            have_path = true;
        } else if (key == "contextLength") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > static_cast<double>(UINT32_MAX)) {
                error = "payload contextLength is not a uint32";
                return false;
            }
            context_length = static_cast<uint32_t>(value);
        } else {
            if (!scanner.skip_value()) {
                error = "malformed value for payload member '" + key + "'";
                return false;
            }
        }

        scanner.skip_ws();

        if (scanner.consume(',')) {
            continue;
        }

        scanner.skip_ws();

        if (scanner.consume('}')) {
            break;
        }

        error = "expected ',' or '}' in payload object";
        return false;
    }

    if (!have_path || path.empty()) {
        error = "payload has no path";
        return false;
    }

    return true;
}

// extract_encode_payload parses {"text": "...", "addBos": bool, "addEos":
// bool, "maxTokens": uint}. text is required; defaults: false/false/256.
bool extract_encode_payload(const std::string& bytes, std::string& text,
                            int32_t& add_bos, int32_t& add_eos,
                            uint32_t& max_tokens, std::string& error) {
    text.clear();
    add_bos = 0;
    add_eos = 0;
    max_tokens = 256;

    detail::Scanner scanner(bytes.data(), bytes.size());

    scanner.skip_ws();
    if (!scanner.consume('{')) {
        error = "encode payload is not a JSON object";
        return false;
    }

    bool have_text = false;
    scanner.skip_ws();
    if (scanner.consume('}')) {
        error = "encode payload has no text";
        return false;
    }

    for (;;) {
        scanner.skip_ws();
        std::string key;
        if (!scanner.read_string(key)) {
            error = "malformed encode payload key";
            return false;
        }
        scanner.skip_ws();
        if (!scanner.consume(':')) {
            error = "expected ':' after encode payload key";
            return false;
        }
        scanner.skip_ws();

        if (key == "text") {
            if (!scanner.read_string(text)) {
                error = "encode payload text is not a string";
                return false;
            }
            have_text = true;
        } else if (key == "addBos") {
            std::string sink;
            // Read a value and check it's true/false.
            size_t start = scanner.position();
            double num;
            if (scanner.read_number(num)) {
                add_bos = (num != 0.0) ? 1 : 0;
            } else {
                // Re-scan from start as a value (true/false/null).
                // Simplest: re-position and skip_value, capturing nothing,
                // then check the substring.
                // We implement a tiny literal check.
                size_t p = scanner.position();
                // The scanner's skip_value handles bool literals; we
                // detect "true" vs "false" by peeking.
                // Re-walk: read 4 chars and see if "true".
                // (This is a simplification but bounded and safe.)
                // We use skip_value to consume it correctly.
                if (!scanner.skip_value()) {
                    error = "malformed addBos value";
                    return false;
                }
                // Look at the original bytes between start and current
                // position to decide true/false.
                size_t end = scanner.position();
                std::string lit(bytes.data() + start, end - start);
                if (lit == "true") add_bos = 1;
                else if (lit == "false") add_bos = 0;
                else {
                    error = "addBos is not a boolean";
                    return false;
                }
                (void)p;
            }
        } else if (key == "addEos") {
            size_t start = scanner.position();
            double num;
            if (scanner.read_number(num)) {
                add_eos = (num != 0.0) ? 1 : 0;
            } else {
                if (!scanner.skip_value()) {
                    error = "malformed addEos value";
                    return false;
                }
                size_t end = scanner.position();
                std::string lit(bytes.data() + start, end - start);
                if (lit == "true") add_eos = 1;
                else if (lit == "false") add_eos = 0;
                else {
                    error = "addEos is not a boolean";
                    return false;
                }
            }
        } else if (key == "maxTokens") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > static_cast<double>(UINT32_MAX)) {
                error = "maxTokens is not a uint32";
                return false;
            }
            max_tokens = static_cast<uint32_t>(value);
            if (max_tokens == 0) {
                max_tokens = 256;
            }
        } else {
            if (!scanner.skip_value()) {
                error = "malformed value for encode payload member '" + key + "'";
                return false;
            }
        }

        scanner.skip_ws();
        if (scanner.consume(',')) {
            continue;
        }
        scanner.skip_ws();
        if (scanner.consume('}')) {
            break;
        }
        error = "expected ',' or '}' in encode payload";
        return false;
    }

    if (!have_text) {
        error = "encode payload has no text";
        return false;
    }

    return true;
}

// extract_decode_payload parses {"ids": [int,...], "skipSpecial": bool,
// "maxBytes": uint}. ids is required (may be empty).
bool extract_decode_payload(const std::string& bytes,
                            std::vector<uint32_t>& ids,
                            int32_t& skip_special, uint32_t& max_bytes,
                            std::string& error) {
    ids.clear();
    skip_special = 1;
    max_bytes = 1u << 20;

    detail::Scanner scanner(bytes.data(), bytes.size());

    scanner.skip_ws();
    if (!scanner.consume('{')) {
        error = "decode payload is not a JSON object";
        return false;
    }

    bool have_ids = false;
    scanner.skip_ws();
    if (scanner.consume('}')) {
        error = "decode payload has no ids";
        return false;
    }

    for (;;) {
        scanner.skip_ws();
        std::string key;
        if (!scanner.read_string(key)) {
            error = "malformed decode payload key";
            return false;
        }
        scanner.skip_ws();
        if (!scanner.consume(':')) {
            error = "expected ':' after decode payload key";
            return false;
        }
        scanner.skip_ws();

        if (key == "ids") {
            if (!scanner.consume('[')) {
                error = "ids is not an array";
                return false;
            }
            scanner.skip_ws();
            if (scanner.consume(']')) {
                have_ids = true;
            } else {
                for (;;) {
                    scanner.skip_ws();
                    double value = 0;
                    if (!scanner.read_number(value) || value < 0 ||
                        value > static_cast<double>(UINT32_MAX)) {
                        error = "ids element is not a uint32";
                        return false;
                    }
                    ids.push_back(static_cast<uint32_t>(value));
                    scanner.skip_ws();
                    if (scanner.consume(',')) continue;
                    scanner.skip_ws();
                    if (scanner.consume(']')) break;
                    error = "expected ',' or ']' in ids array";
                    return false;
                }
                have_ids = true;
            }
        } else if (key == "skipSpecial") {
            size_t start = scanner.position();
            double num;
            if (scanner.read_number(num)) {
                skip_special = (num != 0.0) ? 1 : 0;
            } else {
                if (!scanner.skip_value()) {
                    error = "malformed skipSpecial value";
                    return false;
                }
                size_t end = scanner.position();
                std::string lit(bytes.data() + start, end - start);
                if (lit == "true") skip_special = 1;
                else if (lit == "false") skip_special = 0;
                else {
                    error = "skipSpecial is not a boolean";
                    return false;
                }
            }
        } else if (key == "maxBytes") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > static_cast<double>(UINT32_MAX)) {
                error = "maxBytes is not a uint32";
                return false;
            }
            max_bytes = static_cast<uint32_t>(value);
            if (max_bytes == 0) {
                max_bytes = 1u << 20;
            }
        } else {
            if (!scanner.skip_value()) {
                error = "malformed value for decode payload member '" + key + "'";
                return false;
            }
        }

        scanner.skip_ws();
        if (scanner.consume(',')) continue;
        scanner.skip_ws();
        if (scanner.consume('}')) break;
        error = "expected ',' or '}' in decode payload";
        return false;
    }

    if (!have_ids) {
        error = "decode payload has no ids";
        return false;
    }

    return true;
}

std::string quote(const std::string& src) {
    std::string out;
    out.reserve(src.size() + 2);
    out.push_back('"');

    char buf[8];

    for (const char raw : src) {
        const unsigned char c = static_cast<unsigned char>(raw);

        switch (c) {
        case '"': out += "\\\""; break;
        case '\\': out += "\\\\"; break;
        case '\b': out += "\\b"; break;
        case '\f': out += "\\f"; break;
        case '\n': out += "\\n"; break;
        case '\r': out += "\\r"; break;
        case '\t': out += "\\t"; break;
        default:
            if (c < 0x20) {
                std::snprintf(buf, sizeof(buf), "\\u%04x", static_cast<unsigned>(c));
                out += buf;
            } else {
                out.push_back(static_cast<char>(c));
            }
        }
    }

    out.push_back('"');
    return out;
}

} // namespace json
} // namespace shtn

// --- Phase 5: generate / cancel payload extraction -----------------------------
// (definitions live back inside the shtn::json namespace)

namespace shtn {
namespace json {

bool extract_generate_payload(const std::string& bytes,
                              std::string& request_id, std::string& prompt,
                              uint32_t& max_tokens, float& temperature,
                              int32_t& top_k, float& top_p,
                              float& repetition_penalty,
                              uint32_t& repeat_last_n, uint64_t& seed,
                              std::string& error) {
    request_id.clear();
    prompt.clear();
    max_tokens = 0;
    temperature = 1.0f;
    top_k = 0;
    top_p = 1.0f;
    repetition_penalty = 1.0f;
    repeat_last_n = 0;
    seed = 0;

    detail::Scanner scanner(bytes.data(), bytes.size());

    scanner.skip_ws();
    if (!scanner.consume('{')) {
        error = "payload is not a JSON object";
        return false;
    }

    bool have_prompt = false;
    bool have_max_tokens = false;

    scanner.skip_ws();
    if (scanner.consume('}')) {
        error = "payload is empty";
        return false;
    }

    for (;;) {
        scanner.skip_ws();

        std::string key;
        if (!scanner.read_string(key)) {
            error = "malformed payload key";
            return false;
        }

        scanner.skip_ws();
        if (!scanner.consume(':')) {
            error = "expected ':' after payload key";
            return false;
        }

        scanner.skip_ws();

        if (key == "requestId") {
            if (!scanner.read_string(request_id) || request_id.size() > 128) {
                error = "payload requestId is not a string (or too long)";
                return false;
            }
        } else if (key == "prompt") {
            if (!scanner.read_string(prompt) || prompt.empty() ||
                prompt.size() > (1u << 20)) {
                error = "payload prompt is not a string (or empty/oversized)";
                return false;
            }
            have_prompt = true;
        } else if (key == "maxTokens") {
            double value = 0;
            if (!scanner.read_number(value) || value < 1 ||
                value > static_cast<double>(UINT32_MAX)) {
                error = "payload maxTokens is out of range";
                return false;
            }
            max_tokens = static_cast<uint32_t>(value);
            have_max_tokens = true;
        } else if (key == "temperature") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > 100.0) {
                error = "payload temperature is out of range";
                return false;
            }
            temperature = static_cast<float>(value);
        } else if (key == "topK") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > static_cast<double>(INT32_MAX)) {
                error = "payload topK is out of range";
                return false;
            }
            top_k = static_cast<int32_t>(value);
        } else if (key == "topP") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > 1.0) {
                error = "payload topP is out of range";
                return false;
            }
            top_p = static_cast<float>(value);
        } else if (key == "repetitionPenalty") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > 100.0) {
                error = "payload repetitionPenalty is out of range";
                return false;
            }
            repetition_penalty = static_cast<float>(value);
        } else if (key == "repeatLastN") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > static_cast<double>(UINT32_MAX)) {
                error = "payload repeatLastN is out of range";
                return false;
            }
            repeat_last_n = static_cast<uint32_t>(value);
        } else if (key == "seed") {
            double value = 0;
            if (!scanner.read_number(value) || value < 0 ||
                value > 1.8e19) {
                error = "payload seed is out of range";
                return false;
            }
            seed = static_cast<uint64_t>(value);
        } else {
            if (!scanner.skip_value()) {
                error = "malformed payload value";
                return false;
            }
        }

        scanner.skip_ws();
        if (scanner.consume(',')) {
            continue;
        }
        scanner.skip_ws();
        if (scanner.consume('}')) {
            break;
        }
        error = "expected ',' or '}' in payload";
        return false;
    }

    if (!have_prompt) {
        error = "payload has no prompt";
        return false;
    }
    if (!have_max_tokens) {
        error = "payload has no maxTokens";
        return false;
    }

    return true;
}

bool extract_request_id_payload(const std::string& bytes,
                                std::string& request_id, std::string& error) {
    request_id.clear();

    detail::Scanner scanner(bytes.data(), bytes.size());

    scanner.skip_ws();
    if (!scanner.consume('{')) {
        error = "payload is not a JSON object";
        return false;
    }

    bool have_id = false;

    scanner.skip_ws();
    if (scanner.consume('}')) {
        error = "payload has no requestId";
        return false;
    }

    for (;;) {
        scanner.skip_ws();

        std::string key;
        if (!scanner.read_string(key)) {
            error = "malformed payload key";
            return false;
        }

        scanner.skip_ws();
        if (!scanner.consume(':')) {
            error = "expected ':' after payload key";
            return false;
        }

        scanner.skip_ws();

        if (key == "requestId") {
            if (!scanner.read_string(request_id) || request_id.empty() ||
                request_id.size() > 128) {
                error = "payload requestId is not a string (or wrong size)";
                return false;
            }
            have_id = true;
        } else {
            if (!scanner.skip_value()) {
                error = "malformed payload value";
                return false;
            }
        }

        scanner.skip_ws();
        if (scanner.consume(',')) {
            continue;
        }
        scanner.skip_ws();
        if (scanner.consume('}')) {
            break;
        }
        error = "expected ',' or '}' in payload";
        return false;
    }

    if (!have_id) {
        error = "payload has no requestId";
        return false;
    }

    return true;
}

} // namespace json
} // namespace shtn
