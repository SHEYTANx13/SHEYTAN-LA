// temp_dir.h — cross-platform temporary-directory helper for the native
// test suite (single shared implementation; v1.3.5).
//
// WHY THIS EXISTS (Actions run 35583009466, Windows job):
//   test_tokenizer fell back to a literal "/tmp" when TMPDIR was unset
//   (TMPDIR does not exist on Windows), and test_generate hard-coded
//   "/tmp/shtn-not-llama.gguf"; fopen() then returned nullptr on the
//   Windows runner and both tests failed. Three other tests each carried
//   a private #ifdef _WIN32 GetTempPathA / TMPDIR+/tmp copy of the same
//   logic. This header replaces every one of those copies with ONE
//   reusable RAII helper so no test ever re-introduces a platform
//   assumption.
//
// CONTRACT (all properties are proven by the portable-temp contract
// section in test_gguf.cpp):
//   * works on Windows, Linux and macOS/POSIX;
//   * resolves the platform temp root via std::filesystem::
//     temp_directory_path() (GetTempPath on Windows; TMPDIR/TMP/TEMP/
//     /tmp semantics on POSIX) — never a hard-coded platform path in
//     the caller;
//   * does NOT depend on the TMPDIR environment variable being set;
//   * generates a UNIQUE directory per instance (process id + in-
//     process counter + monotonic clock + random suffix), so repeated
//     and parallel test executions never collide;
//   * returns a NATIVE filesystem path (backslashes on Windows) that
//     round-trips through fopen/ofstream exactly as produced — paths
//     containing spaces are fully supported;
//   * cleans up after itself: the destructor removes the directory
//     tree recursively (no-throw; best effort, like every temp-file
//     cleanup);
//   * never uses tmpnam()/tempnam() (unsafe by definition) and never
//     invents a fixed C:\Temp-style location.
//
// TEST ONLY — not part of the engine.

#ifndef SHTN_TEST_TEMP_DIR_H
#define SHTN_TEST_TEMP_DIR_H

#include <atomic>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <random>
#include <string>

#if defined(_WIN32)
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h> // GetCurrentProcessId()
#else
#include <unistd.h> // ::getpid()
#endif

namespace shtn_test {

namespace temp_detail {

// process_id() — the OS process identifier, used only as a readable
// component of the unique directory name (never as a security token).
inline uint64_t process_id() {
#if defined(_WIN32)
    return static_cast<uint64_t>(::GetCurrentProcessId());
#else
    return static_cast<uint64_t>(::getpid());
#endif
}

// fallback_root() — the LAST-RESORT temp root used only when
// temp_directory_path() itself reports an error (which in practice
// requires an unusable environment on a broken machine). On Windows
// this consults TEMP/TMP (the same variables GetTempPath honors);
// on POSIX it is the platform temp directory. It never invents a
// fixed C:\Temp-style path.
inline std::filesystem::path fallback_root() {
#if defined(_WIN32)
    for (const char* name : {"TEMP", "TMP"}) {
        if (const char* v = std::getenv(name); v != nullptr && *v != '\0') {
            return std::filesystem::path(v);
        }
    }
    return std::filesystem::path(".");
#else
    return std::filesystem::path("/tmp");
#endif
}

// resolve_temp_root() — the canonical writable temp root.
//
// Primary: std::filesystem::temp_directory_path() (GetTempPath on
// Windows; TMPDIR, TMP, TEMP, TEMPDIR, then the platform default on
// POSIX). It does not require TMPDIR to exist as a variable, and on
// POSIX libstdc++/libc++ only consult it when set. If the call errors
// (damaged environment), the fallback chain above runs.
inline std::filesystem::path resolve_temp_root() {
    std::error_code ec;
    std::filesystem::path root = std::filesystem::temp_directory_path(ec);
    if (ec || root.empty()) {
        root = fallback_root();
    }
    return root;
}

// next_counter() — monotonic in-process counter. Distinct TempDir
// instances created back-to-back inside one test binary can never
// collide even if the clock and the RNG seed repeat.
inline uint64_t next_counter() {
    static std::atomic<uint64_t> counter{0};
    return counter.fetch_add(1, std::memory_order_relaxed) + 1;
}

// random_hex64() — 16 hex characters of randomness. Combined with the
// process id, the counter and the clock this makes cross-process
// collisions (parallel ctest slots, repeated runs) astronomically
// unlikely even on platforms where process ids recycle quickly.
inline std::string random_hex64() {
    static std::mt19937_64 rng([] {
        std::random_device rd;
        std::seed_seq seq{rd(), rd(), rd(), rd()};
        return std::mt19937_64(seq);
    }());
    char buf[17];
    const uint64_t v = rng();
    std::snprintf(buf, sizeof(buf), "%016llx",
                  static_cast<unsigned long long>(v));
    return std::string(buf);
}

} // namespace temp_detail

// TempDir — RAII owner of one unique temporary directory.
//
//   shtn_test::TempDir tmp("tokenizer");
//   std::string model = tmp.file("tiny-bpe.gguf");   // native path
//   ... fopen(model.c_str(), "wb") ...
//   // scope exit: the whole tree is removed recursively.
//
// The directory name embeds the tag, the process id, an in-process
// counter, a clock tick and a random suffix, so concurrent test
// binaries and repeated runs never share a location.
class TempDir {
public:
    explicit TempDir(const std::string& tag = "test")
        : path_(make_unique_path(tag)) {
        std::error_code ec;
        std::filesystem::create_directories(path_, ec); // parents allowed
        if (ec) {
            // Unreachable on healthy runners (the temp root is writable);
            // if it ever fires, tests fail loudly on the first file write
            // rather than silently passing.
            std::fprintf(stderr,
                         "shtn_test::TempDir: create_directories(%s) failed: %s\n",
                         path_.string().c_str(), ec.message().c_str());
        }
    }

    // Non-copyable (single owner of the on-disk location).
    TempDir(const TempDir&) = delete;
    TempDir& operator=(const TempDir&) = delete;

    // Movable (ownership transfers; the moved-from object no longer
    // cleans anything up).
    TempDir(TempDir&& other) noexcept
        : path_(std::move(other.path_)), armed_(other.armed_) {
        other.armed_ = false;
    }
    TempDir& operator=(TempDir&& other) noexcept {
        if (this != &other) {
            cleanup();
            path_ = std::move(other.path_);
            path_str_ = path_.string();
            armed_ = other.armed_;
            other.armed_ = false;
        }
        return *this;
    }

    ~TempDir() { cleanup(); }

    // path — the directory itself, in native form.
    const std::string& path() const { return path_str_; }

    // file — a path INSIDE the directory, in native form. The name may
    // contain spaces and any other legal filename characters; it must
    // be a single component (no separators) because the directory is
    // the collision-free unit.
    std::string file(const std::string& name) const {
        return (path_ / name).string();
    }

    // exists — whether the directory is currently present on disk.
    bool exists() const {
        std::error_code ec;
        return std::filesystem::is_directory(path_, ec);
    }

    // remove — manual recursive cleanup (idempotent; also runs from
    // the destructor).
    void remove() {
        std::error_code ec;
        std::filesystem::remove_all(path_, ec); // best effort, no-throw
        armed_ = false;
    }

    // release — keep the directory after destruction (debugging aid).
    void release() { armed_ = false; }

private:
    static std::filesystem::path make_unique_path(const std::string& tag) {
        using namespace std::chrono;
        const auto now = steady_clock::now().time_since_epoch();
        const uint64_t tick =
            static_cast<uint64_t>(duration_cast<nanoseconds>(now).count());

        std::string name = "shtn-" + sanitize(tag) + "-" +
                           std::to_string(temp_detail::process_id()) + "-" +
                           std::to_string(temp_detail::next_counter()) + "-" +
                           std::to_string(tick) + "-" +
                           temp_detail::random_hex64();
        return temp_detail::resolve_temp_root() / name;
    }

    // sanitize — keeps the tag filesystem-safe (letters, digits, dash,
    // underscore, dot; anything else becomes '-').
    static std::string sanitize(const std::string& tag) {
        std::string out;
        out.reserve(tag.size());
        for (char c : tag) {
            const bool ok = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
                            (c >= '0' && c <= '9') || c == '-' || c == '_' ||
                            c == '.';
            out.push_back(ok ? c : '-');
            if (out.size() >= 48) break; // bounded tag length
        }
        if (out.empty()) out = "test";
        return out;
    }

    void cleanup() {
        if (!armed_) return;
        std::error_code ec;
        std::filesystem::remove_all(path_, ec); // best effort, no-throw
        armed_ = false;
    }

    std::filesystem::path path_;
    std::string path_str_ = path_.string();
    bool armed_ = true;
};

} // namespace shtn_test

#endif /* SHTN_TEST_TEMP_DIR_H */
