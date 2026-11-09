// host_entry.cpp — the shtn-engine-host process entry point.
//
// ONLY main() lives here. The dispatch host itself (shtn_host_run and
// everything below it) is implemented in host_main.cpp and compiled
// ONCE — the resulting object is linked into this executable AND into
// the native test suite (tests/test_host.cpp drives the same loop
// through host_main.h, never by including an implementation source).

#include "host_main.h"

#include "shtn/version.h"

#include <iostream>

#if defined(_WIN32)
#include <fcntl.h>
#include <io.h>
#endif

int main() {
#if defined(_WIN32)
    // Binary-safe stdio on Windows.
    _setmode(_fileno(stdin), _O_BINARY);
    _setmode(_fileno(stdout), _O_BINARY);
    _setmode(_fileno(stderr), _O_BINARY);
#endif

    std::ios::sync_with_stdio(false);

    std::cerr << "[shtn-host] "
              << "engine=" << SHTN_ENGINE_NAME
              << " abi=" << SHTN_ABI_VERSION
              << " protocol=" << SHTN_PROTOCOL_VERSION
              << " (phase 5: lifecycle, health, hardware, metrics, GGUF model"
                 " loading, tokenizer, KV cache, scheduler, REAL native"
                 " generation with streaming + cancellation)"
              << std::endl;

    return shtn_host_run(std::cin, std::cout);
}
