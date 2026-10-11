// test_host.cpp — dispatch loop tests: malformed native requests never
// crash the host; every valid op round-trips; shutdown exits cleanly.

#define SHTN_HOST_NO_MAIN
#include "../src/host_main.cpp"

#include "gguf_writer.h"
#include "temp_dir.h"

#include <cstdio>
#include <fstream>
#include <map>
#include <sstream>
#include <vector>
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

// gguf_test_quote wraps a path as a JSON string (paths are plain ASCII in
// tests; the host's quote() is available via host_main.cpp's includes).
static std::string gguf_test_quote(const std::string& s) {
    std::string out = "\"";
    for (char c : s) {
        if (c == '\\' || c == '"') {
            out.push_back('\\');
        }
        out.push_back(c);
    }
    out.push_back('"');
    return out;
}

// run feeds raw bytes to the host loop and returns its output + exit code.
static std::pair<std::string, int> run(const std::string& input) {
    std::istringstream in(input);
    std::ostringstream out;

    const int rc = shtn_host_run(in, out);

    return {out.str(), rc};
}

// last_frame decodes the final frame of a framed stream.
static std::string last_frame(const std::string& framed) {
    if (framed.size() < 4) {
        return {};
    }

    size_t off = 0;
    std::string last;

    while (off + 4 <= framed.size()) {
        const uint32_t size =
            static_cast<uint32_t>(static_cast<unsigned char>(framed[off])) |
            (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 1])) << 8) |
            (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 2])) << 16) |
            (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 3])) << 24);

        if (off + 4 + size > framed.size()) {
            break; // truncated tail
        }

        last = framed.substr(off + 4, size);
        off += 4 + size;
    }

    return last;
}

static std::string frame(const std::string& payload) {
    const uint32_t size = static_cast<uint32_t>(payload.size());

    std::string out;
    out.push_back(static_cast<char>(size & 0xFF));
    out.push_back(static_cast<char>((size >> 8) & 0xFF));
    out.push_back(static_cast<char>((size >> 16) & 0xFF));
    out.push_back(static_cast<char>((size >> 24) & 0xFF));
    out += payload;

    return out;
}

// phase5_generate_tests (defined at the bottom of this file): streaming
// generation through the real dispatch loop.
static void phase5_generate_tests(const std::string& fixtures);

int main() {
    // --- valid ops round-trip -------------------------------------------------
    {
        const auto [out, rc] = run(frame("{\"id\":1,\"op\":\"ping\"}"));

        CHECK(rc == 0);

        const std::string resp = last_frame(out);
        CHECK(resp.find("\"id\":1") != std::string::npos);
        CHECK(resp.find("\"ok\":true") != std::string::npos);
        CHECK(resp.find("\"protocolVersion\":4") != std::string::npos);
        CHECK(resp.find("\"abiVersion\":4") != std::string::npos);
    }

    {
        const auto [out, rc] = run(frame("{\"id\":2,\"op\":\"health\"}"));

        CHECK(rc == 0);

        const std::string resp = last_frame(out);
        CHECK(resp.find("\"healthy\":true") != std::string::npos);
        CHECK(resp.find("\"state\":\"ready\"") != std::string::npos);
    }

    {
        const auto [out, rc] = run(frame("{\"id\":3,\"op\":\"hwinfo\"}"));

        CHECK(rc == 0);

        const std::string resp = last_frame(out);
        CHECK(resp.find("\"architecture\":") != std::string::npos);
        CHECK(resp.find("\"totalBytes\":") != std::string::npos);
    }

    {
        const auto [out, rc] = run(frame("{\"id\":4,\"op\":\"metrics\"}"));

        CHECK(rc == 0);

        const std::string resp = last_frame(out);
        CHECK(resp.find("\"engineState\":\"ready\"") != std::string::npos);
        CHECK(resp.find("\"scheduler\"") != std::string::npos);
        CHECK(resp.find("\"maxConcurrentRequests\":1") != std::string::npos);
        CHECK(resp.find("\"nativeRssBytes\":") != std::string::npos);
    }

    {
        const auto [out, rc] = run(frame("{\"id\":5,\"op\":\"cancel\",\"payload\":{\"requestId\":\"req-9\"}}"));

        CHECK(rc == 0);

        const std::string resp = last_frame(out);
        CHECK(resp.find("\"cancelled\":false") != std::string::npos);
        CHECK(resp.find("\"reason\"") != std::string::npos);
    }

    // --- unknown op: bounded error response, loop keeps serving ---------------
    {
        std::string input;
        input += frame("{\"id\":10,\"op\":\"make-coffee\"}");
        input += frame("{\"id\":11,\"op\":\"ping\"}");

        const auto [out, rc] = run(input);

        CHECK(rc == 0);

        // First: error for the unknown op.
        const std::string err = last_frame(out.substr(0, out.size()));
        // (last_frame returns the LAST frame; decode both by splitting.)
        std::istringstream stream(out);
        std::string first;
        std::string second;

        {
            std::string framed(out);
            size_t off = 0;
            std::vector<std::string> frames;
            while (off + 4 <= framed.size()) {
                const uint32_t size =
                    static_cast<uint32_t>(static_cast<unsigned char>(framed[off])) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 1])) << 8) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 2])) << 16) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 3])) << 24);
                if (off + 4 + size > framed.size()) {
                    break;
                }
                frames.push_back(framed.substr(off + 4, size));
                off += 4 + size;
            }

            CHECK(frames.size() == 2);
            if (frames.size() == 2) {
                first = frames[0];
                second = frames[1];
            }
        }

        CHECK(first.find("\"ok\":false") != std::string::npos);
        CHECK(first.find("unknown op") != std::string::npos);
        CHECK(first.find("\"id\":10") != std::string::npos);

        CHECK(second.find("\"ok\":true") != std::string::npos);
        CHECK(second.find("\"id\":11") != std::string::npos);
    }

    // --- malformed requests: error response, host survives ---------------------
    {
        const char* garbage[] = {
            "not json at all",
            "{",
            "{\"id\":\"NaN\"}",
            "{\"op\":true}",
            "[]",
            "{\"id\":1,\"op\":\"ping\",\"payload\":{broken}}",
            "\x01\x02\x03",
        };

        for (const char* g : garbage) {
            std::string input;
            input += frame(g);
            input += frame("{\"id\":99,\"op\":\"ping\"}");

            const auto [out, rc] = run(input);

            CHECK(rc == 0); // never crashes, never exits

            // Final frame must be the healthy ping response.
            const std::string resp = last_frame(out);
            CHECK(resp.find("\"ok\":true") != std::string::npos);
            CHECK(resp.find("\"id\":99") != std::string::npos);
        }
    }

    // --- model ops: load / info / unload round trip ----------------------------
    {
        // Shared cross-platform temp-dir helper (v1.3.5 consolidation:
        // replaces this test's private GetTempPathA/TMPDIR copy).
        shtn_test::TempDir tmpdir("host");
        const std::string dir = tmpdir.path();

        const auto tiny = gguf_test::make_tiny_model(dir + "/tiny.gguf");
        {
            std::ofstream f(tiny.path, std::ios::binary | std::ios::trunc);
            f.write(reinterpret_cast<const char*>(tiny.image.data()),
                    static_cast<std::streamsize>(tiny.image.size()));
            CHECK(static_cast<bool>(f));
        }

        // Fresh host: model_info reports unloaded, no model object.
        {
            const auto [out, rc] = run(frame("{\"id\":20,\"op\":\"model_info\"}"));
            CHECK(rc == 0);
            const std::string resp = last_frame(out);
            CHECK(resp.find("\"loaded\":false") != std::string::npos);
            CHECK(resp.find("\"state\":\"unloaded\"") != std::string::npos);
            CHECK(resp.find("\"model\"") == std::string::npos);
        }

        // load_model without payload → bounded error, host survives.
        {
            std::string input;
            input += frame("{\"id\":21,\"op\":\"load_model\"}");
            input += frame("{\"id\":22,\"op\":\"ping\"}");

            const auto [out, rc] = run(input);
            CHECK(rc == 0);

            std::istringstream stream(out);
            // Two frames out: error + ping-ok.
            const std::string framed = out;
            size_t off = 0;
            std::vector<std::string> frames;
            while (off + 4 <= framed.size()) {
                const uint32_t size =
                    static_cast<uint32_t>(static_cast<unsigned char>(framed[off])) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 1])) << 8) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 2])) << 16) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(framed[off + 3])) << 24);
                if (off + 4 + size > framed.size()) {
                    break;
                }
                frames.push_back(framed.substr(off + 4, size));
                off += 4 + size;
            }
            CHECK(frames.size() == 2);
            if (frames.size() == 2) {
                CHECK(frames[0].find("\"ok\":false") != std::string::npos);
                CHECK(frames[0].find("payload") != std::string::npos);
                CHECK(frames[1].find("\"ok\":true") != std::string::npos);
            }
        }

        // load_model with malformed payload → bounded error.
        {
            std::string input;
            input += frame("{\"id\":23,\"op\":\"load_model\",\"payload\":{\"bad\":1}}");
            input += frame("{\"id\":24,\"op\":\"load_model\",\"payload\":{}}");
            input += frame("{\"id\":25,\"op\":\"ping\"}");

            const auto [out, rc] = run(input);
            CHECK(rc == 0);

            const std::string resp = last_frame(out);
            CHECK(resp.find("\"id\":25") != std::string::npos); // still serving
        }

        // load_model → model_info → unload_model → model_info.
        {
            std::string input;
            input += frame("{\"id\":30,\"op\":\"load_model\",\"payload\":{\"path\":" +
                           gguf_test_quote(tiny.path) + "}}");
            input += frame("{\"id\":31,\"op\":\"model_info\"}");
            input += frame("{\"id\":32,\"op\":\"unload_model\"}");
            input += frame("{\"id\":33,\"op\":\"model_info\"}");

            const auto [out, rc] = run(input);
            CHECK(rc == 0);

            // Decode all four frames.
            std::vector<std::string> frames;
            size_t off = 0;
            while (off + 4 <= out.size()) {
                const uint32_t size =
                    static_cast<uint32_t>(static_cast<unsigned char>(out[off])) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(out[off + 1])) << 8) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(out[off + 2])) << 16) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(out[off + 3])) << 24);
                if (off + 4 + size > out.size()) {
                    break;
                }
                frames.push_back(out.substr(off + 4, size));
                off += 4 + size;
            }

            CHECK(frames.size() == 4);
            if (frames.size() == 4) {
                CHECK(frames[0].find("\"id\":30") != std::string::npos);
                CHECK(frames[0].find("\"ok\":true") != std::string::npos);
                CHECK(frames[0].find("\"loaded\":true") != std::string::npos);
                CHECK(frames[0].find("\"state\":\"loaded\"") != std::string::npos);
                CHECK(frames[0].find("\"architecture\":\"llama\"") != std::string::npos);
                CHECK(frames[0].find("\"contextLength\":256") != std::string::npos);
                CHECK(frames[0].find("\"vocabularySize\":96") != std::string::npos);
                CHECK(frames[0].find("\"tensorCount\":3") != std::string::npos);
                CHECK(frames[0].find("\"kvCacheBytes\":") != std::string::npos);
                CHECK(frames[0].find("\"weightsBytes\":") != std::string::npos);

                CHECK(frames[1].find("\"id\":31") != std::string::npos);
                CHECK(frames[1].find("\"loaded\":true") != std::string::npos);
                CHECK(frames[1].find("\"ggufVersion\":3") != std::string::npos);

                CHECK(frames[2].find("\"id\":32") != std::string::npos);
                CHECK(frames[2].find("\"loaded\":false") != std::string::npos);
                CHECK(frames[2].find("\"state\":\"unloaded\"") != std::string::npos);

                CHECK(frames[3].find("\"id\":33") != std::string::npos);
                CHECK(frames[3].find("\"state\":\"unloaded\"") != std::string::npos);
            }
        }

        // load_model with a garbage model → error response naming the failure.
        {
            std::vector<uint8_t> garbage(256, 0x42);
            const std::string bad = dir + "/garbage.gguf";
            {
                std::ofstream f(bad, std::ios::binary | std::ios::trunc);
                f.write(reinterpret_cast<const char*>(garbage.data()),
                        static_cast<std::streamsize>(garbage.size()));
            }

            std::string input;
            input += frame("{\"id\":40,\"op\":\"load_model\",\"payload\":{\"path\":" +
                           gguf_test_quote(bad) + "}}");
            input += frame("{\"id\":41,\"op\":\"ping\"}");

            const auto [out, rc] = run(input);
            CHECK(rc == 0);

            const std::string resp = last_frame(out);
            CHECK(resp.find("\"id\":41") != std::string::npos); // host alive
        }

        // Phase 4: tokenizer end-to-end through the IPC protocol.
        // Build a small BPE fixture, load_model → tokenizer_init →
        // tokenizer_encode → tokenizer_decode, verifying the host
        // correctly dispatches the new ops and the round-trips work.
        {
            // Build a tiny BPE GGUF in the test dir.
            gguf_test::BuildOptions bo;
            bo.version = 3;
            bo.alignment = 32;

            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "general.architecture", "llama");
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_str(b, "tokenizer.ggml.model", "llama");
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_string_array(b, "tokenizer.ggml.tokens",
                    std::vector<std::string>{
                        "<unk>", "<s>", "</s>",
                        "\xE2\x96\x81", "h", "e",
                        "\xE2\x96\x81""he", "llo",
                    });
            });
            // token_type as int32 array (1=normal, 2=unknown, 3=control)
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::put_str(b, "tokenizer.ggml.token_type");
                gguf_test::put_u32(b, 9); // array
                gguf_test::put_u32(b, 5); // int32
                gguf_test::put_u64(b, 8);
                uint32_t types[8] = {2, 3, 3, 1, 1, 1, 1, 1};
                for (uint32_t t : types) gguf_test::put_u32(b, t);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_string_array(b, "tokenizer.ggml.merges",
                    std::vector<std::string>{
                        "\xE2\x96\x81 h",
                        "\xE2\x96\x81""h e",
                        "l l",
                        "ll o",
                    });
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "tokenizer.ggml.bos_token_id", 1);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "tokenizer.ggml.eos_token_id", 2);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "tokenizer.ggml.unknown_token_id", 0);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.context_length", 64);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.embedding_length", 8);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.block_count", 1);
            });
            bo.extra_kv.push_back([](std::vector<uint8_t>& b) {
                gguf_test::kv_u32(b, "llama.vocab_size", 8);
            });
            bo.tensors.push_back([](std::vector<uint8_t>& b) {
                gguf_test::tensor_info(b, "token_embd.weight", {8, 8}, 0, 0);
            });
            bo.data_bytes = 8 * 8 * 4;

            const std::string bpe_path = dir + "/bpe.gguf";
            auto image = gguf_test::build(bo);
            gguf_test::write_file(bpe_path, image);

            std::string input;
            // Load the model.
            input += frame("{\"id\":50,\"op\":\"load_model\",\"payload\":{\"path\":" +
                           gguf_test_quote(bpe_path) + "}}");
            // Init the tokenizer.
            input += frame("{\"id\":51,\"op\":\"tokenizer_init\"}");
            // Query tokenizer_info.
            input += frame("{\"id\":52,\"op\":\"tokenizer_info\"}");
            // Encode "hello" with BOS+EOS.
            input += frame("{\"id\":53,\"op\":\"tokenizer_encode\",\"payload\":"
                           "{\"text\":\"hello\",\"addBos\":true,\"addEos\":true,"
                           "\"maxTokens\":64}}");
            // Decode [6,7] (▁he + llo) — should give " hello".
            input += frame("{\"id\":54,\"op\":\"tokenizer_decode\",\"payload\":"
                           "{\"ids\":[6,7],\"skipSpecial\":true,\"maxBytes\":1024}}");
            // kv_cache_info — should report allocated=false (no auto-alloc).
            input += frame("{\"id\":55,\"op\":\"kv_cache_info\"}");
            // scheduler_info — should report queued=0, max_concurrent=1.
            input += frame("{\"id\":56,\"op\":\"scheduler_info\"}");

            const auto [out, rc] = run(input);
            CHECK(rc == 0);

            // Decode all frames.
            std::vector<std::string> frames;
            size_t off = 0;
            while (off + 4 <= out.size()) {
                const uint32_t size =
                    static_cast<uint32_t>(static_cast<unsigned char>(out[off])) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(out[off + 1])) << 8) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(out[off + 2])) << 16) |
                    (static_cast<uint32_t>(static_cast<unsigned char>(out[off + 3])) << 24);
                if (off + 4 + size > out.size()) break;
                frames.push_back(out.substr(off + 4, size));
                off += 4 + size;
            }

            CHECK(frames.size() == 7);
            if (frames.size() == 7) {
                // load_model → ok, loaded.
                CHECK(frames[0].find("\"id\":50") != std::string::npos);
                CHECK(frames[0].find("\"ok\":true") != std::string::npos);
                CHECK(frames[0].find("\"loaded\":true") != std::string::npos);

                // tokenizer_init → ok, initialized=true, vocab_size=8.
                CHECK(frames[1].find("\"id\":51") != std::string::npos);
                CHECK(frames[1].find("\"ok\":true") != std::string::npos);
                CHECK(frames[1].find("\"initialized\":true") != std::string::npos);
                CHECK(frames[1].find("\"vocabSize\":8") != std::string::npos);
                CHECK(frames[1].find("\"model\":\"bpe\"") != std::string::npos);

                // tokenizer_info → initialized=true, bos/eos ids.
                CHECK(frames[2].find("\"id\":52") != std::string::npos);
                CHECK(frames[2].find("\"hasBos\":true") != std::string::npos);
                CHECK(frames[2].find("\"bosId\":1") != std::string::npos);
                CHECK(frames[2].find("\"eosId\":2") != std::string::npos);
                CHECK(frames[2].find("\"mergeCount\":4") != std::string::npos);

                // tokenizer_encode → ids [1, 6, 7, 2] (BOS, ▁he, llo, EOS).
                CHECK(frames[3].find("\"id\":53") != std::string::npos);
                CHECK(frames[3].find("\"ids\":[1,6,7,2]") != std::string::npos);
                CHECK(frames[3].find("\"count\":4") != std::string::npos);

                // tokenizer_decode → " hello" (▁he + llo → " hello").
                CHECK(frames[4].find("\"id\":54") != std::string::npos);
                CHECK(frames[4].find("\"text\":\" hello\"") != std::string::npos);

                // kv_cache_info → allocated=false, capacity_bytes=0.
                CHECK(frames[5].find("\"id\":55") != std::string::npos);
                CHECK(frames[5].find("\"allocated\":false") != std::string::npos);

                // scheduler_info → queued=0, max_concurrent=1.
                CHECK(frames[6].find("\"id\":56") != std::string::npos);
                CHECK(frames[6].find("\"queuedRequests\":0") != std::string::npos);
                CHECK(frames[6].find("\"maxConcurrent\":1") != std::string::npos);
            }
        }
    }

    // --- oversized frame: protocol violation exit (supervisor restarts) -------
    {
        const uint32_t big = (1u << 20) + 1;

        std::string input;
        input.push_back(static_cast<char>(big & 0xFF));
        input.push_back(static_cast<char>((big >> 8) & 0xFF));
        input.push_back(static_cast<char>((big >> 16) & 0xFF));
        input.push_back(static_cast<char>((big >> 24) & 0xFF));

        const auto [out, rc] = run(input);

        CHECK(rc == 2); // protocol violation
    }

    // --- shutdown: acknowledge and clean exit ---------------------------------
    {
        std::string input;
        input += frame("{\"id\":50,\"op\":\"health\"}");
        input += frame("{\"id\":51,\"op\":\"shutdown\"}");
        input += frame("{\"id\":52,\"op\":\"ping\"}"); // must NOT be served

        const auto [out, rc] = run(input);

        CHECK(rc == 0);

        const std::string resp = last_frame(out);
        CHECK(resp.find("\"id\":51") != std::string::npos);
        CHECK(resp.find("\"ok\":true") != std::string::npos);
    }

    // --- EOF: clean exit -------------------------------------------------------
    {
        const auto [out, rc] = run(frame("{\"id\":60,\"op\":\"ping\"}"));

        CHECK(rc == 0);
    }

    // --- Phase 5: streaming generation through the real dispatch loop ---
    //
    // Phase 6 repair: this block used to run AFTER the `failures > 0`
    // exit gate, so every phase5 CHECK failure was invisible to the
    // process exit code — CI stayed green while the streaming tests
    // silently failed (and a separate reader bug, fixed below, made
    // blocks 1-2 fail on every run). The gate now runs LAST; the exit
    // code is honest about ALL executed checks.
    {
        // v1.3.5 fix: the fixtures directory is the configure-time
        // SHTN_FIXTURES_DIR macro (already provided by CMake for this
        // target), exactly like test_forward and test_generate. The
        // previous getenv("SHTN_FIXTURES_DIR") plus "../tests/fixtures"
        // fallback made the test CWD-DEPENDENT: it only passed under
        // ctest because CTest's default working directory is the build
        // dir; running the binary from anywhere else silently loaded
        // no model and failed every streaming assertion.
        std::string fixtures = SHTN_FIXTURES_DIR;
        phase5_generate_tests(fixtures);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_host: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_host: all checks passed\n");
    return 0;
}

// --- Phase 5: generate streaming through the REAL host loop -----------------
//
// The in-memory run() helper cannot keep stdin open while reading
// streamed output, so these tests use OS pipes: the driver writes
// request frames, then reads event + final frames until the request
// completes, then closes.

#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#define SHTN_READ  _read
#define SHTN_WRITE _write
#define SHTN_CLOSE _close
using ShtnPipe = int;
#else
#include <fcntl.h>
#include <unistd.h>
#define SHTN_READ  read
#define SHTN_WRITE write
#define SHTN_CLOSE close
using ShtnPipe = int;
#endif

#include <cerrno>
#include <thread>

namespace hostgen {

#ifdef _WIN32
inline bool make_pipe(ShtnPipe fds[2]) {
    return _pipe(fds, 65536, _O_BINARY) == 0;
}
#else
inline bool make_pipe(ShtnPipe fds[2]) {
    return ::pipe(fds) == 0;
}
#endif

// pipe istream: a streambuf reading raw bytes from a file descriptor.
class PipeInBuf : public std::streambuf {
public:
    explicit PipeInBuf(ShtnPipe fd) : fd_(fd) {
        setg(buf_, buf_, buf_);
    }

protected:
    int_type underflow() override {
        const auto n = SHTN_READ(fd_, buf_, sizeof(buf_));
        if (n <= 0) {
            return traits_type::eof();
        }
        setg(buf_, buf_, buf_ + static_cast<size_t>(n));
        return traits_type::to_int_type(buf_[0]);
    }

private:
    ShtnPipe fd_;
    char buf_[4096];
};

// pipe ostream: a streambuf writing raw bytes to a file descriptor.
class PipeOutBuf : public std::streambuf {
public:
    explicit PipeOutBuf(ShtnPipe fd) : fd_(fd) {
        setp(buf_, buf_ + sizeof(buf_));
    }
    ~PipeOutBuf() override { sync(); }

protected:
    int_type overflow(int_type c) override {
        if (!traits_type::eq_int_type(c, traits_type::eof())) {
            *pptr() = static_cast<char>(c);
            pbump(1);
        }
        return sync() == 0 ? traits_type::not_eof(c) : traits_type::eof();
    }

    int sync() override {
        const size_t n = static_cast<size_t>(pptr() - pbase());
        if (n > 0) {
            size_t done = 0;
            while (done < n) {
                const auto w = SHTN_WRITE(fd_, pbase() + done, n - done);
                if (w <= 0) return -1;
                done += static_cast<size_t>(w);
            }
        }
        setp(buf_, buf_ + sizeof(buf_));
        return 0;
    }

private:
    ShtnPipe fd_;
    char buf_[4096];
};

// FrameReader parses length-prefixed frames from a descriptor.
class FrameReader {
public:
    explicit FrameReader(ShtnPipe fd) : fd_(fd) {}

    // next returns the next frame payload (blocking); false on EOF/error.
    bool next(std::string& out) {
        uint8_t hdr[4];
        if (!read_exact(hdr, 4)) {
            return false;
        }
        const uint32_t size =
            static_cast<uint32_t>(hdr[0]) |
            (static_cast<uint32_t>(hdr[1]) << 8) |
            (static_cast<uint32_t>(hdr[2]) << 16) |
            (static_cast<uint32_t>(hdr[3]) << 24);
        out.resize(size);
        if (size > 0 && !read_exact(reinterpret_cast<uint8_t*>(&out[0]),
                                    size)) {
            return false;
        }
        return true;
    }

private:
    bool read_exact(uint8_t* dst, size_t n) {
        size_t done = 0;
        while (done < n) {
            const auto r = SHTN_READ(fd_, dst + done, n - done);
            if (r <= 0) {
                return false;
            }
            done += static_cast<size_t>(r);
        }
        return true;
    }

    ShtnPipe fd_;
};

} // namespace hostgen

static bool run_host_generate_test(const std::string& fixture_path,
                                   const std::string& generate_frame,
                                   std::vector<std::string>& frames_out,
                                   const std::string& extra_frames = "") {
    using namespace hostgen;

    // fixture_path documents the fixture dependency of every caller; the
    // model path itself is embedded in the scripted frames.
    (void)fixture_path;

    ShtnPipe req_pipe[2];
    ShtnPipe resp_pipe[2];
    if (!make_pipe(req_pipe) || !make_pipe(resp_pipe)) {
        return false;
    }

    PipeInBuf in_buf(req_pipe[0]);
    PipeOutBuf out_buf(resp_pipe[1]);
    std::istream in(&in_buf);
    std::ostream out(&out_buf);

    std::thread host([&in, &out]() { shtn_host_run(in, out); });

    // Write the generate request, keep stdin open while frames stream.
    SHTN_WRITE(req_pipe[1], generate_frame.data(), generate_frame.size());

    if (!extra_frames.empty()) {
        SHTN_WRITE(req_pipe[1], extra_frames.data(), extra_frames.size());
    }

    FrameReader reader(resp_pipe[0]);
    std::string frame;
    while (reader.next(frame)) {
        frames_out.push_back(frame);
        // Phase 6 repair: stop at the GENERATE terminal frame, not at the
        // first non-chunk frame. The old condition broke at the load_model
        // response (always the first non-chunk frame), so the driver
        // collected exactly one frame and the streaming assertions never
        // had a chance to run. A generate terminal either carries a
        // finishReason (success/cancelled) or is an ok:false error frame
        // (rejected request); load/ping responses match neither.
        const bool is_generate_terminal =
            frame.find("\"finishReason\":") != std::string::npos ||
            frame.find("\"ok\":false") != std::string::npos;
        if (is_generate_terminal) {
            break;
        }
    }

    // Close stdin (EOF) → host cancels any leftovers and exits.
    SHTN_CLOSE(req_pipe[1]);
    host.join();

    SHTN_CLOSE(req_pipe[0]);
    SHTN_CLOSE(resp_pipe[0]);
    SHTN_CLOSE(resp_pipe[1]);
    return true;
}

static void phase5_generate_tests(const std::string& fixtures) {
    // 1. Load a model + generate → event frames + final frame with REAL
    //    generated text and metrics.
    {
        std::string script;
        script += frame("{\"id\":1,\"op\":\"load_model\",\"payload\":{\"path\":\"" +
                       fixtures + "/tiny-llama-f32.gguf\"}}");
        script += frame("{\"id\":2,\"op\":\"generate\",\"payload\":{" +
                       std::string("\"requestId\":\"go-1\",\"prompt\":\"hello\",") +
                       "\"maxTokens\":6,\"temperature\":0.0}}");

        std::vector<std::string> frames;
        CHECK(run_host_generate_test(fixtures, script, frames));

        // Frame 0: load_model response.
        CHECK(frames.size() >= 2);
        if (frames.size() >= 1) {
            CHECK(frames[0].find("\"id\":1") != std::string::npos);
            CHECK(frames[0].find("\"ok\":true") != std::string::npos);
            CHECK(frames[0].find("\"loaded\":true") != std::string::npos);
            CHECK(frames[0].find("\"generationCapable\":true") !=
                  std::string::npos);
        }

        // Then generate: at least one chunk event + the final frame.
        bool saw_chunk = false;
        bool saw_final = false;
        for (size_t i = 1; i < frames.size(); ++i) {
            const std::string& f = frames[i];
            CHECK(f.find("\"id\":2") != std::string::npos);
            CHECK(f.find("\"ok\":true") != std::string::npos);
            if (f.find("\"event\":\"chunk\"") != std::string::npos) {
                saw_chunk = true;
                CHECK(f.find("\"requestId\":\"go-1\"") != std::string::npos);
            } else {
                saw_final = true;
                CHECK(f.find("\"finishReason\":\"length\"") !=
                      std::string::npos);
                CHECK(f.find("\"generatedTokens\":6") != std::string::npos);
                CHECK(f.find("\"promptTokens\":5") != std::string::npos);
                CHECK(f.find("\"metrics\"") != std::string::npos);
                CHECK(f.find("\"tokensPerSecond\"") != std::string::npos);
            }
        }
        CHECK(saw_chunk);
        CHECK(saw_final);
    }

    // 2. Cancel mid-generation (slow fixture): event frames stop, the
    //    final frame reports "cancelled", the host stays alive.
    //
    //    Phase 6 repair: the original driver preloaded the cancel frame
    //    together with the generate frame, so the dispatch loop could
    //    process the cancel BEFORE the lane thread submitted the request
    //    to the engine ("cancelled":false, generation then ran to
    //    completion). The cancel is now issued only after the FIRST
    //    streamed chunk arrives — at that point the request is provably
    //    registered and active in the engine, which is the deterministic
    //    mid-flight condition the test means to exercise (and what the
    //    Go-side TestRealCppHostPhase5Cancellation has always done).
    {
        using namespace hostgen;
        ShtnPipe req_pipe[2];
        ShtnPipe resp_pipe[2];
        CHECK(make_pipe(req_pipe) && make_pipe(resp_pipe));

        PipeInBuf in_buf(req_pipe[0]);
        PipeOutBuf out_buf(resp_pipe[1]);
        std::istream in(&in_buf);
        std::ostream out(&out_buf);

        std::thread host([&in, &out]() { shtn_host_run(in, out); });

        std::string script;
        script += frame("{\"id\":1,\"op\":\"load_model\",\"payload\":{\"path\":\"" +
                       fixtures + "/tiny-llama-slow.gguf\"}}");
        script += frame("{\"id\":2,\"op\":\"generate\",\"payload\":{" +
                       std::string("\"requestId\":\"go-2\",\"prompt\":\"hello\",") +
                       "\"maxTokens\":200,\"temperature\":1.1,\"seed\":5}}");
        SHTN_WRITE(req_pipe[1], script.data(), script.size());

        FrameReader reader(resp_pipe[0]);
        std::vector<std::string> frames;
        std::string f;

        bool generation_active = false;
        while (!generation_active && reader.next(f)) {
            frames.push_back(f);
            if (f.find("\"id\":2") != std::string::npos &&
                f.find("\"event\":\"chunk\"") != std::string::npos) {
                generation_active = true;
            }
        }
        CHECK(generation_active);

        // Mid-flight: the generation is streaming. Cancel it now.
        const std::string cancel =
            frame("{\"id\":3,\"op\":\"cancel\",\"payload\":{\"requestId\":\"go-2\"}}");
        SHTN_WRITE(req_pipe[1], cancel.data(), cancel.size());

        // Read through the cancelled final frame.
        while (reader.next(f)) {
            frames.push_back(f);
            if (f.find("\"finishReason\":\"cancelled\"") != std::string::npos ||
                f.find("\"ok\":false") != std::string::npos) {
                break;
            }
        }

        SHTN_CLOSE(req_pipe[1]);
        host.join();
        SHTN_CLOSE(req_pipe[0]);
        SHTN_CLOSE(resp_pipe[0]);
        SHTN_CLOSE(resp_pipe[1]);

        bool saw_cancel_ok = false;
        bool saw_cancelled_final = false;
        for (const std::string& fr : frames) {
            if (fr.find("\"id\":3") != std::string::npos) {
                CHECK(fr.find("\"cancelled\":true") != std::string::npos);
                saw_cancel_ok = true;
            }
            if (fr.find("\"id\":2") != std::string::npos &&
                fr.find("\"event\"") == std::string::npos) {
                CHECK(fr.find("\"finishReason\":\"cancelled\"") !=
                      std::string::npos);
                saw_cancelled_final = true;
            }
        }
        CHECK(saw_cancel_ok);
        CHECK(saw_cancelled_final);
    }

    // 3. Malformed generate payload → bounded error frame; host alive.
    {
        std::string script;
        script += frame("{\"id\":1,\"op\":\"load_model\",\"payload\":{\"path\":\"" +
                       fixtures + "/tiny-llama-f32.gguf\"}}");
        script += frame("{\"id\":2,\"op\":\"generate\",\"payload\":{\"prompt\":123}}");

        std::vector<std::string> frames;
        CHECK(run_host_generate_test(fixtures, script, frames));

        if (frames.size() >= 2) {
            CHECK(frames[1].find("\"id\":2") != std::string::npos);
            CHECK(frames[1].find("\"ok\":false") != std::string::npos);
            CHECK(frames[1].find("malformed generate payload") !=
                  std::string::npos);
        }
    }

    // 4. Generate with NO model → error frame (fallback signal).
    {
        std::string script;
        script += frame("{\"id\":1,\"op\":\"generate\",\"payload\":{" +
                       std::string("\"requestId\":\"go-x\",\"prompt\":\"hello\",") +
                       "\"maxTokens\":4}}");

        std::vector<std::string> frames;
        CHECK(run_host_generate_test(fixtures, script, frames));

        if (frames.size() >= 1) {
            CHECK(frames[0].find("\"id\":1") != std::string::npos);
            CHECK(frames[0].find("\"ok\":false") != std::string::npos);
            CHECK(frames[0].find("no model") != std::string::npos);
        }
    }

    // 5. Phase 6 regression — lane prompt OWNERSHIP (CI run 34703102794):
    //    op_generate used to leave opts.prompt pointing at its stack-local
    //    std::string; pipelined generate frames made the dispatch loop
    //    reuse that dead stack slot while lanes were still starting, and
    //    the corrupted prompt silently dropped the BPE merges ("hello"
    //    → 7 tokens instead of 5 on a loaded runner; the context-bound
    //    Go test then rejected a fitting request). The lane now owns a
    //    copy. This test pipelines DISTINCT prompts — every request must
    //    report the token count of ITS OWN prompt, so any cross-request
    //    contamination or stack corruption fails an assertion.
    //
    //    Expected counts on tiny-llama-f32.gguf (vocab: ▁ + a-z singles +
    //    "he"/"ll"; merges "h e" and "l l"; BOS auto-prepended):
    //      "z"       → 3   (BOS + ▁ + z)
    //      "ab"      → 4
    //      "abcd"    → 6
    //      "abcdef"  → 8
    //      "hello"   → 5   (REQUIRES both merges to apply)
    //      "abcdefgh"→ 10  (BOS + ▁ + 8 letters)
    {
        struct BurstReq {
            int id;
            const char* prompt;
            int expected_prompt_tokens;
        };
        const BurstReq burst[] = {
            {10, "z", 3},
            {11, "ab", 4},
            {12, "abcd", 6},
            {13, "abcdef", 8},
            {14, "hello", 5},
            {15, "abcdefgh", 10},
        };
        const size_t kBurst = sizeof(burst) / sizeof(burst[0]);

        std::string script;
        script += frame("{\"id\":1,\"op\":\"load_model\",\"payload\":{\"path\":\"" +
                       fixtures + "/tiny-llama-f32.gguf\"}}");
        for (const BurstReq& r : burst) {
            script += frame("{\"id\":" + std::to_string(r.id) +
                           ",\"op\":\"generate\",\"payload\":{" +
                           "\"requestId\":\"burst-" + std::to_string(r.id) +
                           "\",\"prompt\":\"" + r.prompt +
                           "\",\"maxTokens\":2,\"temperature\":0.0}}");
        }

        // Drive the host with ALL frames preloaded (pipelined — the
        // dispatch loop never blocks on stdin, which is exactly the
        // stack-reuse pressure that exposed the dangling pointer).
        using namespace hostgen;
        ShtnPipe req_pipe[2];
        ShtnPipe resp_pipe[2];
        CHECK(make_pipe(req_pipe) && make_pipe(resp_pipe));

        PipeInBuf in_buf(req_pipe[0]);
        PipeOutBuf out_buf(resp_pipe[1]);
        std::istream in(&in_buf);
        std::ostream out(&out_buf);

        std::thread host([&in, &out]() { shtn_host_run(in, out); });

        SHTN_WRITE(req_pipe[1], script.data(), script.size());

        FrameReader reader(resp_pipe[0]);
        std::map<int, std::string> finals; // generate id → terminal frame
        std::string f;
        while (finals.size() < kBurst && reader.next(f)) {
            // A generate terminal carries finishReason (success) or is
            // an error frame (ok:false). Counting BOTH keeps the loop's
            // termination guaranteed even under rejection.
            const bool is_terminal =
                f.find("\"finishReason\":") != std::string::npos ||
                f.find("\"ok\":false") != std::string::npos;
            if (!is_terminal) {
                continue;
            }
            for (const BurstReq& r : burst) {
                if (f.find("\"id\":" + std::to_string(r.id) + ",") !=
                    std::string::npos) {
                    finals[r.id] = f;
                    break;
                }
            }
        }

        SHTN_CLOSE(req_pipe[1]);
        host.join();
        SHTN_CLOSE(req_pipe[0]);
        SHTN_CLOSE(resp_pipe[0]);
        SHTN_CLOSE(resp_pipe[1]);

        CHECK(finals.size() == kBurst);
        for (const BurstReq& r : burst) {
            const auto it = finals.find(r.id);
            CHECK(it != finals.end());
            if (it == finals.end()) {
                continue;
            }
            CHECK(it->second.find("\"ok\":true") != std::string::npos);
            CHECK(it->second.find("\"requestId\":\"burst-" +
                                  std::to_string(r.id) + "\"") !=
                  std::string::npos);
            const std::string want =
                "\"promptTokens\":" + std::to_string(r.expected_prompt_tokens);
            if (it->second.find(want) == std::string::npos) {
                std::fprintf(stderr,
                             "[burst %d] prompt mismatch for prompt \"%s\": "
                             "expected %s — frame: %s\n",
                             r.id, r.prompt, want.c_str(),
                             it->second.c_str());
            }
            CHECK(it->second.find(want) != std::string::npos);
            CHECK(it->second.find("\"finishReason\":\"length\"") !=
                  std::string::npos);
        }
    }

    // 6. Phase 6 regression — lane RECYCLING: finished generation lanes
    //    must be retired (join + free). Before the sweep, lanes stayed in
    //    the host's list for its whole lifetime, so kMaxLanes (16) capped
    //    the host at 16 generations TOTAL — the 17th request of a long
    //    session was rejected with "too many concurrent requests" while
    //    nothing was running. 20 SEQUENTIAL round-trip generations (never
    //    more than one in flight) served by one host must ALL succeed.
    //    (A pipelined burst of 20 is different: it legitimately exceeds
    //    the CONCURRENT lane bound and is honestly rejected.)
    {
        using namespace hostgen;
        ShtnPipe req_pipe[2];
        ShtnPipe resp_pipe[2];
        CHECK(make_pipe(req_pipe) && make_pipe(resp_pipe));

        PipeInBuf in_buf(req_pipe[0]);
        PipeOutBuf out_buf(resp_pipe[1]);
        std::istream in(&in_buf);
        std::ostream out(&out_buf);

        std::thread host([&in, &out]() { shtn_host_run(in, out); });

        const std::string load =
            frame("{\"id\":1,\"op\":\"load_model\",\"payload\":{\"path\":\"" +
                  fixtures + "/tiny-llama-f32.gguf\"}}");
        SHTN_WRITE(req_pipe[1], load.data(), load.size());

        FrameReader reader(resp_pipe[0]);
        std::string f;

        // Consume the load response.
        CHECK(reader.next(f));
        CHECK(f.find("\"ok\":true") != std::string::npos);

        int successes = 0;
        bool saw_reject = false;
        for (int i = 0; i < 20; ++i) {
            const std::string gen =
                frame("{\"id\":" + std::to_string(100 + i) +
                       ",\"op\":\"generate\",\"payload\":{" +
                       "\"requestId\":\"recycle-" + std::to_string(i) +
                       "\",\"prompt\":\"hello\",\"maxTokens\":2," +
                       "\"temperature\":0.0}}");
            SHTN_WRITE(req_pipe[1], gen.data(), gen.size());

            // Read through THIS request's terminal frame (chunks first,
            // then the final).
            bool terminal = false;
            while (!terminal && reader.next(f)) {
                if (f.find("too many concurrent requests") !=
                    std::string::npos) {
                    saw_reject = true;
                }
                terminal =
                    f.find("\"finishReason\":") != std::string::npos ||
                    f.find("\"ok\":false") != std::string::npos;
                if (terminal) {
                    CHECK(f.find("\"ok\":true") != std::string::npos);
                    CHECK(f.find("\"promptTokens\":5") != std::string::npos);
                    ++successes;
                }
            }
            CHECK(terminal);
        }

        SHTN_CLOSE(req_pipe[1]);
        host.join();
        SHTN_CLOSE(req_pipe[0]);
        SHTN_CLOSE(resp_pipe[0]);
        SHTN_CLOSE(resp_pipe[1]);

        CHECK(!saw_reject);
        CHECK(successes == 20);
    }
}
