# SHEYTAN Native AI Engine — C++ engine core (v1.1.5 Phase 5)
#
# Build with CMake (preferred):
#
#   cmake -S native/engine -B native/engine/build
#   cmake --build native/engine/build
#   ctest --test-dir native/engine/build
#
# Or with plain make (no CMake required):
#
#   make -C native/engine
#   make -C native/engine test
#
# Artifacts:
#   shtn-engine-host        the supervised IPC host executable
#   libshtn_engine.a        the engine core with the C ABI
#
# Install location for runtime use: {DataDir}/bin/shtn-engine-host(.exe)
# (see internal/native/engine.DefaultHostPath in the Go core).

CXX ?= g++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Wpedantic -O2
BUILD := build

.PHONY: all clean test host library

all: host library

library: $(BUILD)/libshtn_engine.a

ENGINE_SRCS := src/engine.cpp src/model.cpp src/gguf.cpp src/hardware.cpp \
	src/tokenizer.cpp src/kv_cache.cpp src/scheduler.cpp src/sampler.cpp \
	src/tensor.cpp src/llama.cpp src/forward.cpp src/generate.cpp

ENGINE_OBJS := $(patsubst src/%.cpp,$(BUILD)/%.o,$(ENGINE_SRCS))

ENGINE_HDRS := include/shtn/engine.h include/shtn/types.h include/shtn/version.h \
	src/model.h src/gguf.h src/util.h src/tokenizer.h src/kv_cache.h \
	src/scheduler.h src/sampler.h src/tensor.h src/llama.h src/forward.h \
	src/generate.h src/fp16.h

$(BUILD)/%.o: src/%.cpp $(ENGINE_HDRS)
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -c $< -o $@

$(BUILD)/libshtn_engine.a: $(ENGINE_OBJS)
	ar rcs $@ $(ENGINE_OBJS)

$(BUILD)/libshtn_protocol.a: src/protocol.cpp src/json.cpp src/protocol.h src/json.h
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -c src/protocol.cpp -o $(BUILD)/protocol.o
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -c src/json.cpp -o $(BUILD)/json.o
	ar rcs $@ $(BUILD)/protocol.o $(BUILD)/json.o

host: $(BUILD)/shtn-engine-host

$(BUILD)/shtn-engine-host: src/host_main.cpp $(BUILD)/libshtn_engine.a $(BUILD)/libshtn_protocol.a
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc src/host_main.cpp $(BUILD)/libshtn_engine.a $(BUILD)/libshtn_protocol.a -o $@

TESTS := test_engine test_protocol test_host test_gguf test_model \
	test_tokenizer test_kv_cache test_scheduler test_sampler \
	test_tensor test_forward test_generate

test: $(patsubst %,$(BUILD)/%,$(TESTS))
	@for t in $(TESTS); do echo "== $$t"; $(BUILD)/$$t || exit 1; done

$(BUILD)/test_engine: tests/test_engine.cpp $(BUILD)/libshtn_engine.a
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_engine.cpp $(BUILD)/libshtn_engine.a -o $@

$(BUILD)/test_protocol: tests/test_protocol.cpp $(BUILD)/libshtn_protocol.a
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_protocol.cpp $(BUILD)/libshtn_protocol.a -o $@

$(BUILD)/test_host: tests/test_host.cpp $(BUILD)/libshtn_engine.a $(BUILD)/libshtn_protocol.a
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -Itests -DSHTN_FIXTURES_DIR='"$(CURDIR)/tests/fixtures"' tests/test_host.cpp $(BUILD)/libshtn_engine.a $(BUILD)/libshtn_protocol.a -lpthread -o $@

$(BUILD)/test_gguf: tests/test_gguf.cpp $(BUILD)/libshtn_engine.a
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -Itests tests/test_gguf.cpp $(BUILD)/libshtn_engine.a -o $@

$(BUILD)/test_model: tests/test_model.cpp $(BUILD)/libshtn_engine.a
	mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -Itests tests/test_model.cpp $(BUILD)/libshtn_engine.a -o $@

clean:
	rm -rf $(BUILD)

$(BUILD)/test_tokenizer: tests/test_tokenizer.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_tokenizer.cpp $(BUILD)/libshtn_engine.a -o $@

$(BUILD)/test_kv_cache: tests/test_kv_cache.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_kv_cache.cpp $(BUILD)/libshtn_engine.a -o $@

$(BUILD)/test_scheduler: tests/test_scheduler.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_scheduler.cpp $(BUILD)/libshtn_engine.a -lpthread -o $@

$(BUILD)/test_sampler: tests/test_sampler.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_sampler.cpp $(BUILD)/libshtn_engine.a -o $@

$(BUILD)/test_tensor: tests/test_tensor.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc tests/test_tensor.cpp $(BUILD)/libshtn_engine.a -o $@

$(BUILD)/test_forward: tests/test_forward.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -DSHTN_FIXTURES_DIR='"$(CURDIR)/tests/fixtures"' tests/test_forward.cpp $(BUILD)/libshtn_engine.a -lpthread -o $@

$(BUILD)/test_generate: tests/test_generate.cpp $(BUILD)/libshtn_engine.a
	$(CXX) $(CXXFLAGS) -Iinclude -Isrc -DSHTN_FIXTURES_DIR='"$(CURDIR)/tests/fixtures"' tests/test_generate.cpp $(BUILD)/libshtn_engine.a -lpthread -o $@
