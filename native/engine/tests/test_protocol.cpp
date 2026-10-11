// test_protocol.cpp — framing + minimal JSON tests.

#include "json.h"
#include "protocol.h"

#include <cstdio>
#include <sstream>
#include <string>

static int failures = 0;

#define CHECK(cond)                                                        \
    do {                                                                   \
        if (!(cond)) {                                                     \
            std::fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__,    \
                         #cond);                                           \
            ++failures;                                                    \
        }                                                                  \
    } while (0)

static std::string frame(const std::string& payload) {
    std::ostringstream out;
    CHECK(shtn::protocol::write_frame(out, payload));
    return out.str();
}

int main() {
    using shtn::protocol::FrameStatus;

    // --- framing round trip ----------------------------------------------
    {
        std::istringstream in(frame("{\"id\":1,\"op\":\"ping\"}"));
        std::string payload;

        CHECK(shtn::protocol::read_frame(in, payload) == FrameStatus::Ok);
        CHECK(payload == "{\"id\":1,\"op\":\"ping\"}");
    }

    // --- empty payload rejected -------------------------------------------
    {
        std::istringstream in(std::string("\x00\x00\x00\x00", 4));
        std::string payload;

        CHECK(shtn::protocol::read_frame(in, payload) == FrameStatus::Invalid);
    }

    // --- oversized frame rejected ------------------------------------------
    {
        std::string head;
        head.push_back(static_cast<char>(0xFF));
        head.push_back(static_cast<char>(0xFF));
        head.push_back(static_cast<char>(0x00));
        head.push_back(static_cast<char>(0x00)); // 0xFFFF = 65535 < 1MiB -> valid size but short body
        std::istringstream in(head + "short");
        std::string payload;

        // Header says 65535 bytes, body is 5 -> truncated.
        CHECK(shtn::protocol::read_frame(in, payload) == FrameStatus::Truncated);
    }

    // --- frame exceeding the 1 MiB cap --------------------------------------
    {
        const uint32_t big = (1u << 20) + 1;

        std::string head;
        head.push_back(static_cast<char>(big & 0xFF));
        head.push_back(static_cast<char>((big >> 8) & 0xFF));
        head.push_back(static_cast<char>((big >> 16) & 0xFF));
        head.push_back(static_cast<char>((big >> 24) & 0xFF));

        std::istringstream in(head);
        std::string payload;

        CHECK(shtn::protocol::read_frame(in, payload) == FrameStatus::TooLarge);
    }

    // --- EOF is a clean close -----------------------------------------------
    {
        std::istringstream in("");
        std::string payload;

        CHECK(shtn::protocol::read_frame(in, payload) == FrameStatus::Eof);
    }

    // --- JSON request parsing ------------------------------------------------
    {
        const auto p = shtn::json::parse_request("{\"id\":7,\"op\":\"health\"}");
        CHECK(p.valid);
        CHECK(p.id == 7);
        CHECK(p.op == "health");
    }

    // Extra members and nested payloads are tolerated.
    {
        const auto p = shtn::json::parse_request(
            "{\"id\":2,\"op\":\"cancel\",\"payload\":{\"requestId\":\"abc-123\",\"nested\":[1,2,{\"x\":true}]}}");
        CHECK(p.valid);
        CHECK(p.id == 2);
        CHECK(p.op == "cancel");
    }

    // Escapes in op strings.
    {
        const auto p = shtn::json::parse_request(
            "{\"id\":3,\"op\":\"op\\u0041\\n\\\"quoted\\\"\"}");
        CHECK(p.valid);
        CHECK(p.id == 3);
        CHECK(p.op == "opA\n\"quoted\"");
    }

    // --- malformed input is reported, never crashes ---------------------------
    {
        CHECK(!shtn::json::parse_request("").valid);
        CHECK(!shtn::json::parse_request("{").valid);
        CHECK(!shtn::json::parse_request("not json").valid);
        CHECK(!shtn::json::parse_request("{\"id\":1}").valid);            // no op
        CHECK(!shtn::json::parse_request("{\"op\":\"ping\"}").valid);     // no id
        CHECK(!shtn::json::parse_request("{\"id\":\"x\",\"op\":\"ping\"}").valid);
        CHECK(!shtn::json::parse_request("{\"id\":1,\"op\":42}").valid);
        CHECK(!shtn::json::parse_request("{\"id\":1,\"op\":\"ping\"} trailing").valid);
        CHECK(!shtn::json::parse_request("[1,2,3]").valid);
        CHECK(!shtn::json::parse_request("{\"id\":1,\"op\":\"ping\",}").valid);
    }

    // --- string quoting/escaping ------------------------------------------------
    {
        using shtn::json::quote;

        CHECK(quote("") == "\"\"");
        CHECK(quote("plain") == "\"plain\"");
        CHECK(quote("a\"b") == "\"a\\\"b\"");
        CHECK(quote("back\\slash") == "\"back\\\\slash\"");
        CHECK(quote("nl\n") == "\"nl\\n\"");
        CHECK(quote("ctl\x01") == "\"ctl\\u0001\"");
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_protocol: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_protocol: all checks passed\n");
    return 0;
}
