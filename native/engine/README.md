# SHEYTAN Native AI Engine — C++ Engine Core (v1.7.1)

This is the C++ side of the **SHEYTAN Native AI Engine architecture**:

```text
Qt Quick/QML host (ui/qt — the native desktop UI)
      ↓
   Local IPC  (internal/localipc — per-user socket/pipe, keyed handshake)
      ↓
   Go Core  (internal/ — application + runtime engine)
      ↓
SHEYTAN Native API  (internal/native/engine — supervised host + IPC)
      ↓
C++ Native Engine   (THIS tree — the supervised heavy-compute/AI execution engine)
```

## Promotion status (R&D 2026-10-11 — read literally)

The R&D decision (`Parsaetak/Contents` → `Research-and-Development` →
`SHEYTAN-LA-2026-10-11.md`) names THIS tree the future PRIMARY inference
backend, with managed llama.cpp as the automatic honest fallback. The
runtime mechanism already exists (`NativeBackendEnabled` selects
native-first in `internal/api/selection.go`, falling back to llama on
unsupported models/capabilities or verified failure) — but the DEFAULT
is NOT flipped in v1.10.0 (and was not flipped in v1.9.6). v1.10.0
evidence status: the CTest suites pass 13/13 on the hosted Linux CI-class
host (including bit-for-bit prefill/KV parity), and the browser E2E
stack exercises this engine end-to-end with the canonical fixture model.
The REMAINING gate for the default flip is specific: the warm-TTFT
comparison against managed llama.cpp on physical Windows target
hardware through the committed Phase 0 harness (`internal/perf` +
`cmd/shtn-perf`), alongside the full supported-model / request /
streaming / cancellation / sampling coverage evidence. Until that
evidence exists this engine is an ALTERNATIVE backend with proven
parity tests — never described as primary in docs or claims.

## Status (read literally — ARCHITECTURE.md Part III truth standard)

**IMPLEMENTED + TESTED in Phase 1:**

- a narrow, stable **C ABI** (`include/shtn/engine.h`):
  `shtn_engine_create` / `shtn_engine_destroy` / `shtn_engine_health` /
  `shtn_engine_hardware_info` / `shtn_engine_metrics` / `shtn_abi_version`
- real platform hardware detection (`src/hardware.cpp`): CPU
  (name/cores/frequency), RAM (total/available), compile-time
  architecture — detected values only, never guessed, no hardcoded vendor
  assumptions
- real process metrics: engine uptime and its own RSS
- `shtn-engine-host`: the supervised subprocess the Go core spawns — a
  length-prefixed JSON IPC protocol over stdin/stdout (4-byte
  little-endian frame length, 1 MiB cap, protocol+ABI handshake, bounded
  error responses for malformed input — the host never crashes on bad
  input)

**IMPLEMENTED + TESTED in Phase 2 (native GGUF model loading):**

- a bounds-checked, overflow-safe **GGUF reader** (`src/gguf.*`): magic +
  version validation (v2/v3), metadata parsing with hostile-input bounds
  (counts, string lengths, array element counts), tensor-table parsing
  (dims bounds, overflow-checked element products, per-tensor offsets
  validated against the data section, exact byte-size checks for known
  GGML types), alignment handling — model metadata is NEVER trusted
- **memory-mapped model access** (`MappedFile`): whole-file read-only
  mapping on POSIX (`mmap`) and Windows (`CreateFileMapping`); loading a
  multi-GB model does NOT copy it into RAM — only the header pages are
  touched
- **`shtn_engine_load_model` / `shtn_engine_unload_model` /
  `shtn_engine_model_info` / `shtn_engine_memory_plan`** (ABI v2):
  all-or-nothing loading with replace semantics, idempotent unload,
  clean release of the mapping/handle on every path (verified under
  AddressSanitizer)
- real **metadata extraction**: architecture, name, parameter count
  (explicit `general.parameter_count` or derived from the tensor
  table), context length, vocabulary size, embedding length, layer
  count, quantization (from `general.file_type`), tensor count, file
  size — only values actually read or derived; absent keys stay zero
- a load-time **memory plan** (computed, never allocated): model file /
  mapped bytes / weights (tensor-data span) / workspace estimate
  (context·vocab·4 logits row) / KV-cache estimate
  (2·K/V·layers·context·embedding·2 bytes f16) / fixed 64 MiB runtime
  allowance / overflow-checked total / fit-vs-detected-RAM verdict
- the model lifecycle states `unloaded` / `loading` / `loaded` /
  `failed`, mirrored on the Go side (protocol v2 ops: `load_model`,
  `unload_model`, `model_info`)

**HISTORICAL — Phases 1–4 (superseded by Phase 5, kept for the record):**
through Phase 4 this host had NO inference: no generation, no KV cache
allocation, no compute buffers, no scheduler activity — llama.cpp was
the only generation engine, and the `cancel` op honestly answered "no
active generation requests". Phase 5 (next section) implemented REAL
native transformer inference + generation; the paragraph above is the
record of how the engine got there, not the current state.

**IMPLEMENTED + TESTED in Phase 5 (REAL native transformer inference + generation):**

- a safe **tensor access layer** (`src/tensor.*`): name lookup,
  type/shape/byte-range validation and row dequantization for EXACTLY
  these GGML types: F32, F16, Q4_0, Q4_1, Q5_0, Q5_1, Q8_0 (anything
  else — K-quants, IQ, BF16 — fails with an explicit unsupported error,
  never a silent reinterpretation)
- **llama-architecture graph derivation + validation** (`src/llama.*`):
  hyper parameters from REAL GGUF metadata (missing required keys —
  rms_eps, rope.freq_base — fail clearly; no defaults are invented; both
  historical rms_eps key spellings accepted, ambiguity fails closed),
  GQA divisibility checks, and full tensor presence/shape/type
  validation at LOAD time (the `generationCapable` + reason verdict in
  model_info)
- a REAL **transformer forward pass** (`src/forward.*`): token
  embeddings → per-layer [RMSNorm → Q/K/V matvec → RoPE (NORM pairing,
  freq from the model's rope base) → causal GQA attention over the fp16
  KV cache → output projection + residual → RMSNorm → SwiGLU FFN +
  residual] → final RMSNorm → logits (output.weight; token_embd when
  tied); double accumulators; scratch reused across steps (no per-token
  allocations); the KV cache is TRUE fp16 storage (uint16 bits —
  Phase 5 corrected the Phase 4 float[]-but-reported-f16 defect, pinned
  by byte-accounting regression tests)
- a REAL **generation runner** (`src/generate.*`): prompt tokenized by
  the engine's own GGUF tokenizer; context-bound REJECT policy
  (prompt + max_tokens > context fails explicitly — no silent
  truncation); per-request KV reset; prefill + decode loop with
  per-token cancellation observation; the Phase 4 sampler consuming
  REAL logits; stops: EOS (never emitted into text), max_tokens,
  context exhaustion, cancellation, error; UTF-8-complete chunk
  emission (first token immediate for honest TTFT, then batched
  windows — never one frame per token); measured metrics from the
  monotonic clock (prompt/decode/TTFT/tok/s/KV positions — zero means
  not measured, never a guess)
- a REAL **scheduler worker** (`src/scheduler.*`): single-slot thread
  executing queued generation requests; active/completed/cancelled/
  failed counts are real; cancel addresses queued (removed) and active
  (cooperative flag) requests
- **host generation lanes** (`src/host_main.cpp`): the `generate` op
  streams event frames (same request id, `"event":"chunk"`) before its
  final frame; bounded lanes (≤16; the engine queue bounds
  acceptance); every frame write under a shared mutex so responses
  never interleave mid-frame; `cancel` is REAL; EOF/shutdown cancels
  in-flight lanes and joins before exit
- **numerical correctness pinned against an independent Python
  reference** (`tests/reference/make_fixture.py` + `tests/fixtures/`):
  the tiny llama GGUF's staged values (embedding, rmsnorm output, q/k/v
  projections, attention) and final logits are computed by a numpy
  implementation that never calls the C++ code; `test_forward` compares
  within documented tolerances (1e-5 staged, 1e-3 logits after fp16
  K/V + accumulation-order differences)

Fixtures (checked in, deterministic — regenerate with
`python3 tests/reference/make_fixture.py`):
- `tests/fixtures/tiny-llama-f32.gguf` — the reference-verified model
  (llama.cpp-loadable: the same file runs under llama-bench)
- `tests/fixtures/tiny-llama-ref.json` / `.txt` — the reference values
- `tests/fixtures/tiny-llama-slow.gguf` — bigger dims (deterministic
  cancellation windows for the mid-flight tests)

**Phase 5 honest limits (read literally):** architecture llama only;
tensor types F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 only; rope.freq_scale 1.0
only; no chat-template interpretation (plain prompts); the forward pass
is portable scalar C++ — measured SLOWER than llama.cpp on the Phase-5
fixtures (see worklog.md Phase 5 performance table) — correctness was
the priority; optimization remains future work and is never claimed
without fresh comparable measurements.

## Build

CMake (preferred):

```bash
cmake -S native/engine -B native/engine/build
cmake --build native/engine/build
ctest --test-dir native/engine/build          # 12 suites: engine, protocol, host, gguf, model,
                                               # tokenizer, kv_cache, scheduler, sampler,
                                               # tensor, forward (vs the Python reference), generate
```

Plain make (no CMake required):

```bash
make -C native/engine
make -C native/engine test
```

Artifacts:

- `build/shtn-engine-host` — the supervised host executable the Go core
  spawns
- `build/libshtn_engine.a` — the engine core behind the C ABI

## Runtime use

Copy `shtn-engine-host` to `{DataDir}/bin/` (or configure
`nativeEnginePath`), set `engineBackend: "native"` in `config.json`, and
the Go core will supervise it. Without the binary the native path reports
unavailable and llama.cpp remains the engine (the default anyway).

## Protocol (v4)

Frame: `[4-byte LE length][JSON payload]` (cap 1 MiB), over the host's
stdin/stdout. Ops: `ping`, `health`, `hwinfo`, `metrics`, `cancel`
(payload `{"requestId": "..."}` — real cooperative cancellation),
`load_model`, `unload_model`, `model_info`, `tokenizer_init`,
`tokenizer_info`, `tokenizer_encode`, `tokenizer_decode`,
`kv_cache_info`, `scheduler_info`,
`generate` (payload
`{"requestId": "...", "prompt": "...", "maxTokens": N, "temperature": f,
"topK": n, "topP": f, "repetitionPenalty": f, "repeatLastN": n, "seed": n}` —
streams `{"id":N,"ok":true,"event":"chunk","result":{"requestId":...,
"text":...,"token":...,"final":...}}` frames before its final
`{"id":N,"ok":true,"result":{"requestId":...,"finishReason":...,
"promptTokens":...,"generatedTokens":...,"metrics":{...}}}`), and
`shutdown`. A request id receives one or more frames: every frame with
an `"event"` member is intermediate; the frame without it is final.
Bump `SHTN_PROTOCOL_VERSION` / `SHTN_ABI_VERSION`
(`include/shtn/version.h`) whenever the wire or ABI changes — the Go
core fails closed on mismatch (v4 was bumped together on both sides in
Phase 5: generate/cancel + streamed event frames; v3 was Phase 4; v2 was
Phase 2).
