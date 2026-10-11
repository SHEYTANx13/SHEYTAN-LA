// hardware.cpp — platform hardware detection implementation.
//
// v1.3.3 Windows compile hygiene (the CI compiler failure): every header
// this translation unit actually uses is included EXPLICITLY — nothing
// relies on transitive includes that happen to hold on one platform or
// toolchain and silently break on another:
//   * <vector>     — std::vector for the GLPI processor info (previously
//                    pulled transitively; missing on MSVC).
//   * <cstdlib>    — std::strtod / std::strtoll (previously transitive).
//   * <intrin.h>   — __cpuid on MSVC/MinGW (previously transitive via
//                    windows.h on some SDK versions only).
//   * NOMINMAX     — windows.h must never define the min/max macros:
//                    WIN32_LEAN_AND_MEAN does NOT suppress them, and the
//                    macro expands even after "::" — `std::min(a, b)`
//                    became `std::(((a)<(b))?(a):(b)))` and failed to
//                    compile. gguf.cpp already carried this guard; this
//                    file did not.

#include "hardware.h"

#include <algorithm>
#include <cerrno>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

#if defined(_WIN32)
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#include <psapi.h>
#include <intrin.h> // __cpuid
#elif defined(__APPLE__)
#include <mach/mach.h>
#include <mach/mach_host.h>
#include <sys/sysctl.h>
#include <unistd.h>
#else
// Linux and other POSIX systems.
#include <unistd.h>
#endif

namespace shtn {
namespace {

// read_first_value reads the value of "key<TAB><sep>value" lines from a
// /proc-style file (Linux only callers guard the usage).
#if !defined(_WIN32) && !defined(__APPLE__)
std::string proc_first_value(const char* path, const char* key) {
    std::FILE* f = std::fopen(path, "r");
    if (!f) {
        return {};
    }

    std::string result;
    char line[512];

    while (std::fgets(line, sizeof(line), f)) {
        const char* tab = std::strchr(line, '\t');
        if (!tab) {
            continue;
        }

        const size_t key_len = std::strlen(key);
        if (std::strlen(line) < key_len || std::strncmp(line, key, key_len) != 0) {
            continue;
        }

        // Trim trailing whitespace/newline.
        const char* value = tab + 1;
        std::string v(value);
        while (!v.empty() && (v.back() == '\n' || v.back() == '\r' || v.back() == ' ')) {
            v.pop_back();
        }
        result = v;
        break;
    }

    std::fclose(f);
    return result;
}
#endif

void copy_str(char* dst, size_t cap, const std::string& src) {
    if (cap == 0) {
        return;
    }
    // Parenthesized (std::min): immune to any min macro that a consumer's
    // include order may still have defined (defense in depth on top of
    // NOMINMAX above).
    const size_t n = (std::min)(cap - 1, src.size());
    std::memcpy(dst, src.data(), n);
    dst[n] = '\0';
}

} // namespace

void detect_cpu(shtn_cpu_info& out) {
    std::memset(&out, 0, sizeof(out));

    const unsigned hw_concurrency = []() -> unsigned {
#if defined(_WIN32)
        SYSTEM_INFO si{};
        GetSystemInfo(&si);
        return si.dwNumberOfProcessors;
#else
        long n = sysconf(_SC_NPROCESSORS_ONLN);
        return n > 0 ? static_cast<unsigned>(n) : 0u;
#endif
    }();

    out.logical_cores = static_cast<int32_t>(hw_concurrency);

#if defined(__linux__)
    out.physical_cores = static_cast<int32_t>(hw_concurrency);

    // Physical cores: count unique (physical id, core id) pairs when
    // /proc/cpuinfo exposes them; fall back to logical count.
    if (std::FILE* f = std::fopen("/proc/cpuinfo", "r")) {
        std::string current_physical;
        std::string current_core;
        std::string last_pair;

        // Collect unique pairs in a small vector.
        static thread_local std::string pairs[512];
        size_t pair_count = 0;

        char line[512];
        std::string physical;
        std::string core;

        while (std::fgets(line, sizeof(line), f)) {
            const char* tab = std::strchr(line, '\t');
            if (!tab) {
                continue;
            }

            std::string key(line, static_cast<size_t>(tab - line));
            std::string value(tab + 1);
            while (!value.empty() && (value.back() == '\n' || value.back() == '\r' || value.back() == ' ')) {
                value.pop_back();
            }

            if (key == "physical id") {
                physical = value;
            } else if (key == "core id") {
                core = value;
            } else if (key == "model name") {
                copy_str(out.name, sizeof(out.name), value);
            } else if (key == "cpu MHz") {
                errno = 0;
                char* end = nullptr;
                const double mhz = std::strtod(value.c_str(), &end);
                if (end && end != value.c_str() && errno == 0 && mhz > 0.0) {
                    out.frequency_hz = static_cast<int64_t>(mhz * 1e6);
                }
            }

            if (!physical.empty() && !core.empty()) {
                const std::string pair = physical + ":" + core;
                if (pair != last_pair) {
                    last_pair = pair;
                    if (pair_count < sizeof(pairs) / sizeof(pairs[0])) {
                        pairs[pair_count++] = pair;
                    }
                }
                physical.clear();
                core.clear();
            }
        }

        std::fclose(f);

        if (pair_count > 0) {
            out.physical_cores = static_cast<int32_t>(pair_count);
        }
    }
#elif defined(_WIN32)
    // Physical cores via logical processor relationships.
    using GLPI = BOOL (WINAPI*)(PSYSTEM_LOGICAL_PROCESSOR_INFORMATION, PDWORD);
    if (GLPI glpi = reinterpret_cast<GLPI>(GetProcAddress(GetModuleHandleW(L"kernel32.dll"), "GetLogicalProcessorInformation"))) {
        DWORD bytes = 0;
        if (!glpi(nullptr, &bytes) && GetLastError() == ERROR_INSUFFICIENT_BUFFER && bytes > 0) {
            const auto count = bytes / sizeof(SYSTEM_LOGICAL_PROCESSOR_INFORMATION);
            std::vector<SYSTEM_LOGICAL_PROCESSOR_INFORMATION> info(count);
            if (glpi(info.data(), &bytes)) {
                DWORD cores = 0;
                for (const auto& i : info) {
                    if (i.Relationship == RelationProcessorCore) {
                        ++cores;
                    }
                }
                out.physical_cores = static_cast<int32_t>(cores);
            }
        }
    }

    // CPU brand string via cpuid leaf 0x80000002..0x80000004.
    int regs[4] = {0, 0, 0, 0};
    __cpuid(regs, 0x80000000);
    if (static_cast<unsigned>(regs[0]) >= 0x80000004u) {
        char brand[49];
        brand[48] = '\0';
        for (unsigned leaf = 0; leaf < 3; ++leaf) {
            __cpuid(regs, static_cast<int>(0x80000002u + leaf));
            std::memcpy(brand + leaf * 16, regs, 16);
        }
        std::string b(brand);
        const size_t first = b.find_first_not_of(" \t");
        if (first != std::string::npos) {
            b = b.substr(first);
            copy_str(out.name, sizeof(out.name), b);
        }
    }
#elif defined(__APPLE__)
    out.physical_cores = static_cast<int32_t>(hw_concurrency);

    char brand[128] = {0};
    size_t size = sizeof(brand) - 1;
    if (sysctlbyname("machdep.cpu.brand_string", brand, &size, nullptr, 0) == 0) {
        copy_str(out.name, sizeof(out.name), std::string(brand));
    }
#endif
}

void detect_ram(shtn_ram_info& out) {
    std::memset(&out, 0, sizeof(out));

#if defined(_WIN32)
    MEMORYSTATUSEX status{};
    status.dwLength = sizeof(status);
    if (GlobalMemoryStatusEx(&status)) {
        out.total_bytes = status.ullTotalPhys;
        out.available_bytes = status.ullAvailPhys;
    }
#elif defined(__APPLE__)
    uint64_t memsize = 0;
    size_t len = sizeof(memsize);
    if (sysctlbyname("hw.memsize", &memsize, &len, nullptr, 0) == 0) {
        out.total_bytes = memsize;
    }
    // Available memory: not exposed portably on macOS; stays 0.
#else
    const long page_size = sysconf(_SC_PAGESIZE);
    const long pages = sysconf(_SC_PHYS_PAGES);

    if (page_size > 0 && pages > 0) {
        out.total_bytes = static_cast<uint64_t>(page_size) * static_cast<uint64_t>(pages);
    }

    if (const std::string avail = proc_first_value("/proc/meminfo", "MemAvailable"); !avail.empty()) {
        errno = 0;
        char* end = nullptr;
        const long long kb = std::strtoll(avail.c_str(), &end, 10);
        if (end && end != avail.c_str() && errno == 0 && kb >= 0) {
            out.available_bytes = static_cast<uint64_t>(kb) * 1024ull;
        }
    }
#endif
}

const char* architecture_name() {
#if defined(__x86_64__) || defined(_M_X64)
    return "x86_64";
#elif defined(__aarch64__) || defined(_M_ARM64)
    return "aarch64";
#elif defined(__i386__) || defined(_M_IX86)
    return "i386";
#elif defined(__arm__) || defined(_M_ARM)
    return "arm";
#else
    return "unknown";
#endif
}

uint64_t current_process_rss() {
#if defined(_WIN32)
    PROCESS_MEMORY_COUNTERS pmc{};
    if (GetProcessMemoryInfo(GetCurrentProcess(), &pmc, sizeof(pmc))) {
        return pmc.WorkingSetSize;
    }
    return 0;
#elif defined(__APPLE__)
    task_basic_info_data_t info;
    mach_msg_type_number_t count = TASK_BASIC_INFO_COUNT;
    if (task_info(mach_task_self(), TASK_BASIC_INFO,
                  reinterpret_cast<task_info_t>(&info), &count) == KERN_SUCCESS) {
        return static_cast<uint64_t>(info.resident_size);
    }
    return 0;
#else
    if (std::FILE* f = std::fopen("/proc/self/statm", "r")) {
        unsigned long long total_pages = 0;
        unsigned long long resident_pages = 0;
        const int n = std::fscanf(f, "%llu %llu", &total_pages, &resident_pages);
        std::fclose(f);

        if (n == 2) {
            const long page_size = sysconf(_SC_PAGESIZE);
            if (page_size > 0) {
                return resident_pages * static_cast<uint64_t>(page_size);
            }
        }
    }
    return 0;
#endif
}

} // namespace shtn
