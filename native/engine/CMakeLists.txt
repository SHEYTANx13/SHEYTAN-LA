cmake_minimum_required(VERSION 3.16)

project(shtn_native_engine
    VERSION 1.1.5
    DESCRIPTION "SHEYTAN Native AI Engine — C++ engine core and IPC host (Phase 4: tokenizer, KV cache, scheduler, sampler foundation)"
    LANGUAGES C CXX)

# v1.9.2 (build hygiene): default to an OPTIMIZED build when the caller did
# not choose a configuration. A bare `cmake -S . -B build` (single-config
# generators) previously produced an engine with NO optimization flags at
# all — an unsupported configuration for inference arithmetic (the measured
# v1.9.2 bottleneck was per-row tensor-name resolution, fixed in the
# forward path itself, but the dequantization/dot loops of quantized real
# models are strongly optimization-sensitive and were never meant to ship
# at -O0). The authoritative Actions run 37869632400 built the engine with
# a bare configure. An explicit -DCMAKE_BUILD_TYPE=... (Debug for a
# debugging session) still wins.
if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
    set(CMAKE_BUILD_TYPE Release CACHE STRING
        "Build type (Release default: unoptimized native inference is not a supported configuration)" FORCE)
    set_property(CACHE CMAKE_BUILD_TYPE PROPERTY STRINGS Debug Release RelWithDebInfo MinSizeRel)
endif()

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

option(SHTN_BUILD_TESTS "Build the native engine test binaries" ON)
option(SHTN_BUILD_SHARED "Build libshtn_engine as a shared library instead of static" OFF)

# ---------------------------------------------------------------------------
# Engine core library: the narrow C ABI every consumer links against.
# ---------------------------------------------------------------------------
if(SHTN_BUILD_SHARED)
    add_library(shtn_engine SHARED
        src/engine.cpp
        src/model.cpp
        src/gguf.cpp
        src/hardware.cpp
        src/tokenizer.cpp
        src/kv_cache.cpp
        src/scheduler.cpp
        src/sampler.cpp
        src/tensor.cpp
        src/llama.cpp
        src/forward.cpp
        src/generate.cpp)
else()
    add_library(shtn_engine STATIC
        src/engine.cpp
        src/model.cpp
        src/gguf.cpp
        src/hardware.cpp
        src/tokenizer.cpp
        src/kv_cache.cpp
        src/scheduler.cpp
        src/sampler.cpp
        src/tensor.cpp
        src/llama.cpp
        src/forward.cpp
        src/generate.cpp)
endif()

target_include_directories(shtn_engine PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/src>
    $<INSTALL_INTERFACE:include>)

# The engine core is dependency-free C++17 by design (no JSON, no network,
# no third-party libraries) so it builds anywhere, including CI cross-builds.
target_compile_features(shtn_engine PUBLIC cxx_std_17)

if(MSVC)
    target_compile_options(shtn_engine PRIVATE /W4)
else()
    target_compile_options(shtn_engine PRIVATE -Wall -Wextra -Wpedantic)
endif()

# ---------------------------------------------------------------------------
# Protocol helpers (framing + minimal JSON) — host-side only.
# ---------------------------------------------------------------------------
add_library(shtn_protocol STATIC
    src/protocol.cpp
    src/json.cpp)
target_include_directories(shtn_protocol PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/src>)
target_compile_features(shtn_protocol PUBLIC cxx_std_17)

# ---------------------------------------------------------------------------
# shtn-engine-host: the supervised subprocess the Go core talks to.
# Reads length-prefixed JSON requests on stdin, writes responses on stdout.
# ---------------------------------------------------------------------------
add_executable(shtn-engine-host src/host_main.cpp)
target_link_libraries(shtn-engine-host PRIVATE shtn_engine shtn_protocol)
if(WIN32)
    # Binary-safe stdio on Windows.
    target_compile_definitions(shtn-engine-host PRIVATE SHTN_WINDOWS_HOST=1)
endif()

# ---------------------------------------------------------------------------
# Tests: dependency-free assert-based binaries registered with CTest.
# ---------------------------------------------------------------------------
if(SHTN_BUILD_TESTS)
    enable_testing()

    add_executable(test_engine tests/test_engine.cpp)
    target_link_libraries(test_engine PRIVATE shtn_engine)
    add_test(NAME engine COMMAND test_engine)

    add_executable(test_protocol tests/test_protocol.cpp)
    target_link_libraries(test_protocol PRIVATE shtn_protocol)
    add_test(NAME protocol COMMAND test_protocol)

    add_executable(test_host tests/test_host.cpp)
    target_link_libraries(test_host PRIVATE shtn_engine shtn_protocol)
    target_compile_definitions(test_host PRIVATE
        SHTN_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/tests/fixtures")
    add_test(NAME host COMMAND test_host)

    add_executable(test_gguf tests/test_gguf.cpp)
    target_link_libraries(test_gguf PRIVATE shtn_engine)
    add_test(NAME gguf COMMAND test_gguf)

    add_executable(test_model tests/test_model.cpp)
    target_link_libraries(test_model PRIVATE shtn_engine)
    add_test(NAME model COMMAND test_model)

    # Phase 4 test binaries.
    add_executable(test_tokenizer tests/test_tokenizer.cpp)
    target_link_libraries(test_tokenizer PRIVATE shtn_engine)
    add_test(NAME tokenizer COMMAND test_tokenizer)

    add_executable(test_kv_cache tests/test_kv_cache.cpp)
    target_link_libraries(test_kv_cache PRIVATE shtn_engine)
    add_test(NAME kv_cache COMMAND test_kv_cache)

    add_executable(test_scheduler tests/test_scheduler.cpp)
    target_link_libraries(test_scheduler PRIVATE shtn_engine)
    add_test(NAME scheduler COMMAND test_scheduler)

    add_executable(test_sampler tests/test_sampler.cpp)
    target_link_libraries(test_sampler PRIVATE shtn_engine)
    add_test(NAME sampler COMMAND test_sampler)

    # Phase 5 test binaries.
    add_executable(test_tensor tests/test_tensor.cpp)
    target_link_libraries(test_tensor PRIVATE shtn_engine)
    add_test(NAME tensor COMMAND test_tensor)

    add_executable(test_forward tests/test_forward.cpp)
    target_link_libraries(test_forward PRIVATE shtn_engine)
    target_compile_definitions(test_forward PRIVATE
        SHTN_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/tests/fixtures")
    add_test(NAME forward COMMAND test_forward)

    add_executable(test_generate tests/test_generate.cpp)
    target_link_libraries(test_generate PRIVATE shtn_engine)
    target_compile_definitions(test_generate PRIVATE
        SHTN_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/tests/fixtures")
    add_test(NAME generate COMMAND test_generate)

    # v1.9.2: the batched-prefill parity gate — v1.9.3 claims corrected to
    # exactly what is proven: per-position logits parity through the
    # chunk=1 span cadence (every token is a span-final token there),
    # final-token logits for the production chunk sizes, full KV bytes
    # identical across every chunking AND the serial path, edge/error
    # bounds, and mixed prefill→decode continuity. Tied output is NOT
    # claimed (no shipped fixture is tied).
    add_executable(test_prefill_parity tests/test_prefill_parity.cpp)
    target_link_libraries(test_prefill_parity PRIVATE shtn_engine)
    target_compile_definitions(test_prefill_parity PRIVATE
        SHTN_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/tests/fixtures")
    add_test(NAME prefill_parity COMMAND test_prefill_parity)
endif()
