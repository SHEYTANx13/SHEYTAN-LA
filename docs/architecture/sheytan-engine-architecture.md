# SHEYTAN-Engine.go — architecture and evidence status

Status: **VERTICAL SLICE DELIVERED AND VERIFIED (CPU reference path) —
real pretrained-model inference NOT YET VERIFIED; GPU/vendor backends NOT
IMPLEMENTED; comparative performance claims DO NOT EXIST.**

`internal/engine` (package `engine`) is SHEYTAN-Engine.go: the genuine Go
inference-engine foundation per master work order section 5 and the
Go_AI_Inference_Engine_Competitive_Architecture_Report. It is NOT a
llama.cpp wrapper and NOT a hard-coded response generator: every generated
token in the verified slice comes from real reference kernels over real
(deterministic) weights.

## Layer map (report layer -> engine file -> status)

| Layer | Implementation | Status |
| --- | --- | --- |
| 1. Public Engine/Session/Generate/Stream API | `engine.go`, `session.go` | implemented, tested |
| 2. Model registry + architecture adapters | `model.go` (Registry), `model_fixture.go` (tiny-fixture-1) | implemented, tested (one architecture) |
| 3. Container adapters | `safetensors.go` (Safetensors); GGUF: typed-unsupported seam; LiteRT-LM: adapter seam marked unavailable | Safetensors = METADATA_INSPECTION_ONLY + complete mapping checks; GGUF/LiteRT-LM NOT IMPLEMENTED |
| 4. Tokenizer + chat template | `model.go` (ByteTokenizer for the fixture) | fixture VERIFIED; real tokenizer/chat-template adapters NOT IMPLEMENTED |
| 5. Tensor metadata + canonical graph IR | `tensor.go`, `graph.go` | implemented, tested |
| 6. Graph optimization + kernel registry | `backend.go` (OpKey registry) | registry implemented; optimization NOT IMPLEMENTED (reference kernels only) |
| 7. Backend capability contract | `backend.go` (Backend interface: Info/Capabilities/Allocate/Upload/Download/Compile/Submit/Synchronize/MemoryInfo/Close) | implemented, tested |
| 8. CPU reference + optimized kernels | `kernels_cpu.go` | scalar reference VERIFIED (31 tests); optimized/vectorized kernels NOT IMPLEMENTED |
| 9. GPU/vendor adapters | typed seam only (Backend contract, `ErrUnsupportedBackend`) | NOT IMPLEMENTED — no GPU claim exists anywhere |
| 10. Memory manager | explicit Allocate/Free/Close, no finalizers | bounded first slice VERIFIED; mmap/streamed weights NOT IMPLEMENTED |
| 11. Admission scheduler | `engine.go` bounded admission queue (typed `ErrBusy`) | bounded first slice VERIFIED; continuous batching NOT IMPLEMENTED |
| 12. Sampling + stop conditions | `sampling.go` | implemented, tested (greedy/temperature/top-k/top-p, seeded) |
| 13. Profiling/correctness/benchmark | `*_test.go`, `bench_test.go` (BenchRecord, section 5.8 fields) | harness VERIFIED; comparative baselines DO NOT EXIST |
| 14. App/API/CLI integration over one engine API | the package is wired as `internal/engine`; application integration is the follow-up slice | NOT WIRED into the desktop app yet (the app's inference surface is unchanged) |

## Evidence classes (the honesty contract)

- **implemented, tested** — behavior pinned by `go test ./internal/engine/`
  (31 tests: tensor validation + overflow, kernel-vs-hand-computed parity,
  seeded sampling reproducibility, container corruption cases, end-to-end
  fixture execution, weight sensitivity, admission/cancellation/ownership).
- **METADATA_INSPECTION_ONLY** — Safetensors: strict header parsing
  (run 44835425398 pass: the data-section bounds check is
  overflow-safe — offsets are compared against the section LENGTH
  instead of an additive check that wrapped near 2^64, and direct
  `TensorFromSafetensors` calls reject reversed/escaped ranges typed —
  regression-tested)
  (dtype/shape/offset/byte-count validation, monotonic non-overlapping
  ranges, hostile-length caps) plus the COMPLETE required-tensor mapping
  against a registered architecture. This is NOT model support: no
  pretrained checkpoint has executed.
- **fixture VERIFIED** — `tiny-fixture-1`: a deterministic 258-vocab byte
  tokenizer, a single-block single-head transformer-shaped graph
  (embedding -> RMSNorm -> causal attention -> residual -> RMSNorm ->
  SiLU MLP -> residual -> RMSNorm -> LM head, 14 graph nodes), weights
  generated from the documented seed 20261121 and shipped as a REAL
  Safetensors container in the test, sampled with a seeded RNG. The
  weight-sensitivity test perturbs one tensor and observes DIFFERENT
  logits — proof of real execution, impossible for a canned generator.
- **NOT YET VERIFIED / NOT IMPLEMENTED** — everything below the CPU
  reference path: real tokenizer families, chat templates, GGUF execution,
  LiteRT-LM, GPU/NPU backends, paged/prefix KV caching, continuous
  batching, optimized kernels, pretrained checkpoints.

## Fixed decisions

1. **Byte order**: the reference kernels use explicit little-endian
   element codecs; container adapters declare/convert, kernels never
   reinterpret bytes.
2. **Write discipline**: `Tensor.Floats()/Ints()` are detached typed
   copies; kernels materialize results back through
   `copy(t.Bytes(), f32AsBytes(v))` — no unsafe aliasing in the
   reference path.
3. **Ties**: argmax/greedy resolve to the LOWEST index (documented,
   tested).
4. **KV state**: the fixture recomputes the FULL prefix attention each
   step — correct, bounded, deterministic. Paged/prefix KV and prefix-cache
   identity are the documented NEXT slice; nothing pretends a cache hit.
5. **Ownership**: no finalizers. Buffers are `Free()`d; backends
   `Close()` everything; engines/sessions answer typed closed-errors
   afterwards (tested).
6. **Bounded everything**: request caps (`MaxNewTokens`), context-length
   enforcement, an admission queue with typed backpressure, allocation
   caps in shape arithmetic (`checkedMul/checkedAdd/Numel` with a
   2^40-element validation cap and a 100 MiB header cap).
7. **Unknown = typed error**: unknown dtypes, quantizations, operators,
   architectures and container records produce `*UnsupportedError` /
   `ErrMalformedContainer` — never silent reinterpretation.

## Prerequisites for the next slices (exact, not vague)

- Real model execution: a pretrained Safetensors checkpoint + its tokenizer
  files + chat template + a complete tensor mapping in an architecture
  adapter (the Registry seam already exists).
- GGUF: version-aware parser behind the same strict-parser pattern;
  quantization types get kernels ONLY with implemented and tested block
  decoders (`ErrUnsupportedQuantization` otherwise).
- LiteRT-LM: the official pinned runtime (C API adapter, CGo isolated) —
  not vendored here; the adapter seam is typed-unavailable.
- GPU: a backend implementation of the existing contract plus the
  Go-Gui/GoGPU link resolution (goffi #72 constraint documented in
  GO-GUI-GOGPU-INTEROP.md) — the renderprobe remains the combined-link
  regression gate.
