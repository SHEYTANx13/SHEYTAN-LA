// types.h — plain C data structures crossing the engine boundary.
//
// RULES (see ARCHITECTURE.md Part III):
//   - every value in these structs is DETECTED, MEASURED, READ or
//     DERIVED; unknowns are zero/empty, never guessed;
//   - no vendor assumptions are hardcoded anywhere;
//   - fixed-size buffers keep the ABI stable across compilers; strings
//     are always NUL-terminated.
//
// Phase 2 additions (ABI v2): shtn_model_load_options, shtn_model_info
// and shtn_memory_plan — the model-loading surface. Phase 1 structs are
// UNCHANGED.

#ifndef SHTN_TYPES_H
#define SHTN_TYPES_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct shtn_health_status {
    int32_t healthy;        /* 0 or 1 */
    char state[32];         /* same vocabulary as the Go engine states */
    char detail[256];       /* human-readable note; empty when healthy */
} shtn_health_status;

typedef struct shtn_cpu_info {
    char name[128];         /* empty when not detected */
    int32_t physical_cores; /* 0 when not detected */
    int32_t logical_cores;  /* 0 when not detected */
    int64_t frequency_hz;   /* 0 when not detected */
} shtn_cpu_info;

typedef struct shtn_ram_info {
    uint64_t total_bytes;      /* 0 when not detected */
    uint64_t available_bytes;  /* 0 when not detected */
} shtn_ram_info;

typedef struct shtn_gpu_info {
    char vendor[64];
    char name[128];
    uint64_t vram_bytes;     /* 0 when not detected */
    int32_t shared_memory;   /* 1 = shared/unified memory; 0 when unknown */
    char driver_version[64];
} shtn_gpu_info;

/* NPU / AI accelerator device. The struct EXISTS so the hardware profile
 * can represent accelerators; detection fills none (count 0) until a
 * real probe is implemented. */
typedef struct shtn_accelerator_info {
    char vendor[64];
    char name[128];
    char kind[32];           /* "npu", "tpu", "dsp", "other" */
    uint64_t memory_bytes;
} shtn_accelerator_info;

typedef struct shtn_hardware_info {
    char architecture[16];   /* "x86_64", "aarch64", ... (compile-time) */
    shtn_cpu_info cpu;
    shtn_ram_info ram;
    shtn_gpu_info gpus[8];
    int32_t gpu_count;                 /* 0 (no C++ GPU probe) */
    shtn_accelerator_info accelerators[4];
    int32_t accelerator_count;         /* 0 */
    char detected_by[256];             /* semicolon-joined probe sources */
} shtn_hardware_info;

typedef struct shtn_metrics {
    char state[32];
    double uptime_seconds;     /* measured since engine create */
    uint64_t process_rss_bytes;/* measured engine process RSS */
    int32_t active_requests;   /* measured: 0 (no generation yet) */
} shtn_metrics;

/* --- Phase 2: model loading surface -------------------------------------- */

/* Model lifecycle states (reported by shtn_model_info.state). These are
 * MODEL states — deliberately a small, dedicated vocabulary; the ENGINE
 * states (idle/starting/ready/...) keep living in the shared llm.State*
 * set and are never mixed with these. */
#define SHTN_MODEL_STATE_UNLOADED "unloaded"
#define SHTN_MODEL_STATE_LOADING  "loading"
#define SHTN_MODEL_STATE_LOADED   "loaded"
#define SHTN_MODEL_STATE_FAILED   "failed"

/* shtn_model_load_options configures one load attempt.
 * context_length: 0 = plan with the model's own trained context length;
 * a non-zero value overrides it for the KV-cache / workspace estimates
 * (planning only — Phase 2 allocates no context buffers). */
typedef struct shtn_model_load_options {
    uint32_t context_length; /* 0 = use the model's trained context */
    uint32_t reserved;       /* must be 0 */
} shtn_model_load_options;

/* shtn_memory_plan is the load-time budget the engine computes from the
 * GGUF header + tensor table. NOTHING is allocated to produce this plan
 * — every number is arithmetic on parsed metadata. Bytes semantics:
 *   model_file_bytes       measured on-disk size;
 *   mapped_bytes           bytes mapped read-only (== file size);
 *   weights_bytes          tensor-data span (file_size - data start);
 *   workspace_bytes        estimated temporary compute buffers;
 *   kv_cache_bytes         estimated f16 K+V cache at the planned context;
 *   runtime_overhead_bytes fixed engine bookkeeping allowance;
 *   total_bytes            overflow-checked sum of the above;
 *   available_ram_bytes    detected available RAM (0 = unknown);
 *   fits_in_ram            1 yes / 0 no / -1 unknown. */
typedef struct shtn_memory_plan {
    uint64_t model_file_bytes;
    uint64_t mapped_bytes;
    uint64_t weights_bytes;
    uint64_t workspace_bytes;
    uint64_t kv_cache_bytes;
    uint64_t runtime_overhead_bytes;
    uint64_t total_bytes;
    uint64_t available_ram_bytes;
    int32_t  fits_in_ram;
    int32_t  reserved;       /* always 0 */
} shtn_memory_plan;

/* --- Phase 5: native generation surface -----------------------------------
 *
 * ABI v4: shtn_model_info is EXTENDED IN PLACE (appended fields — the
 * Phase 4 comment block describing it moved up unchanged). The ABI and
 * protocol versions are bumped together on both sides (Go + C++). */

/* shtn_model_info is a snapshot of the engine's model concern. Fields
 * are filled ONLY with values actually read from the GGUF header or
 * derived from the tensor table; unknowns stay 0/empty. `state` uses the
 * SHTN_MODEL_STATE_* vocabulary; `error` carries the last load failure
 * detail (empty when the model did not fail).
 *
 * Phase 5 additions: generation_capable is the load-time native-inference
 * verdict (the llama graph validated against real GGUF metadata —
 * presence/shape/type of every required tensor; metadata only, nothing
 * allocated). 1 = the transformer forward pass can execute this model
 * natively; 0 = the Go core must select the llama.cpp fallback, with
 * generation_reason carrying the explicit, inspectable reason. */
typedef struct shtn_model_info {
    char path[1024];
    char architecture[64];    /* general.architecture */
    char name[256];           /* general.name */
    char quantization[64];    /* human name for general.file_type */
    char state[16];           /* SHTN_MODEL_STATE_* */
    char error[256];          /* last load failure detail; empty otherwise */
    uint64_t file_size_bytes;      /* measured on-disk size */
    uint64_t parameter_count;      /* derived from tensor table, or read */
    uint64_t context_length;       /* <arch>.context_length */
    uint64_t vocabulary_size;      /* <arch>.vocab_size / tokenizer array */
    uint64_t embedding_length;     /* <arch>.embedding_length */
    uint64_t layer_count;          /* <arch>.block_count */
    uint64_t kv_cache_bytes;       /* planned estimate (f16 K+V) */
    uint64_t workspace_bytes;      /* planned estimate (logits row) */
    uint64_t total_plan_bytes;     /* full memory-plan total */
    uint32_t tensor_count;         /* tensor table entries */
    uint32_t gguf_version;         /* 2 or 3 */
    uint32_t general_file_type;    /* raw general.file_type (when present) */
    int32_t  has_file_type;        /* 1 = general.file_type was present */
    int32_t  generation_capable;   /* 1 = native inference validated */
    char generation_reason[256];   /* why not (empty when capable) */
} shtn_model_info;

/* Generation finish reasons (stable values). */
#define SHTN_FINISH_EOS        "eos"
#define SHTN_FINISH_LENGTH     "length"
#define SHTN_FINISH_CANCELLED  "cancelled"
#define SHTN_FINISH_STOP       "stop"

/* shtn_generation_options configures one native generation request. The
 * caller (host worker lane) supplies the prompt as UTF-8 text; the
 * engine tokenizes it with its own materialized GGUF tokenizer. */
typedef struct shtn_generation_options {
    const char* request_id;   /* caller-supplied id (cancel address); NULL ok */
    const char* prompt;       /* UTF-8 prompt text (required) */
    uint64_t prompt_len;      /* prompt bytes (required) */
    uint32_t max_tokens;      /* hard generation cap (> 0) */
    float temperature;        /* 0 = greedy; typical 0..2 */
    int32_t top_k;            /* 0 = disabled */
    float top_p;              /* 1.0 = disabled */
    float repetition_penalty; /* 1.0 = disabled */
    uint32_t repeat_last_n;   /* repetition window (0 = 64 default) */
    uint64_t seed;            /* 0 = fixed default (deterministic) */
    uint32_t reserved;        /* must be 0 */
} shtn_generation_options;

/* shtn_generation_chunk is ONE streamed generation event. text is NUL-
 * terminated with text_len valid bytes; token_id is the sampled token;
 * final != 0 marks the last chunk (metrics then valid). */
typedef struct shtn_generation_chunk {
    const char* text;         /* decoded delta (UTF-8 complete sequences) */
    uint32_t text_len;
    uint32_t token_id;
    int32_t final;            /* 1 = last chunk */
} shtn_generation_chunk;

/* shtn_generation_metrics — every field is a MEASURED value from the
 * monotonic (steady) clock; zero means "not measured" (never a guess). */
typedef struct shtn_generation_metrics {
    uint32_t prompt_tokens;        /* measured: encoded prompt length */
    uint32_t generated_tokens;     /* measured: tokens sampled */
    double prompt_seconds;         /* prefill duration */
    double ttft_seconds;           /* start → first sampled token */
    double decode_seconds;         /* first token → last token */
    double total_seconds;          /* start → completion */
    double tokens_per_second;      /* generated / decode window (0 if n/a) */
    double prompt_tokens_per_second; /* prompt / prefill (0 if n/a) */
    uint64_t kv_positions_used;    /* prompt + generated positions */
} shtn_generation_metrics;

/* shtn_generation_result is the final outcome of one generation. */
typedef struct shtn_generation_result {
    char finish_reason[16];   /* SHTN_FINISH_* */
    shtn_generation_metrics metrics;
} shtn_generation_result;

/* The streaming callback: called once per chunk from the generation
 * worker. Return 0 to continue; non-zero aborts generation with
 * SHTN_ERR_INTERNAL (consumer failure — never a crash). */
typedef int32_t (*shtn_generation_emit_fn)(void* user,
                                           const shtn_generation_chunk* chunk);

/* shtn_generation_stats is the engine-level snapshot of the generation
 * concern (reported through the metrics op). */
typedef struct shtn_generation_stats {
    uint32_t active_requests;      /* in-flight right now */
    uint64_t total_requests;       /* since engine create */
    uint64_t total_completed;      /* finished (any finish reason) */
    uint64_t total_cancelled;
    uint64_t total_failed;
    /* Last completed request's measured stats (zeros before any). */
    double ttft_seconds;
    double tokens_per_second;
    double prompt_tokens_per_second;
    uint32_t last_prompt_tokens;
    uint32_t last_generated_tokens;
} shtn_generation_stats;



/* shtn_tokenizer_info is the materialized tokenizer snapshot. Fields
 * are filled ONLY with values actually read from the GGUF header; a
 * tokenizer that failed to initialize leaves initialized=0. */
typedef struct shtn_tokenizer_info {
    int32_t  initialized;          /* 0 or 1 */
    char     model[32];            /* "bpe", "unigram", "wpm", "unsupported" */
    char     model_name[64];       /* raw tokenizer.ggml.model string */
    uint32_t vocab_size;           /* number of tokens materialized */
    uint32_t merge_count;          /* BPE merges (0 for non-BPE) */
    int32_t  has_bos;              /* 0 or 1 */
    int32_t  has_eos;              /* 0 or 1 */
    int32_t  has_unknown;          /* 0 or 1 */
    uint32_t bos_id;               /* 0 when has_bos == 0 */
    uint32_t eos_id;               /* 0 when has_eos == 0 */
    uint32_t unknown_id;           /* 0 when has_unknown == 0 */
    char     error[256];           /* last init failure detail */
} shtn_tokenizer_info;

/* shtn_kv_cache_info is the measured KV-cache snapshot. Every byte
 * count is the REAL allocation; used_positions is 0 until a forward
 * pass exists (Phase 4 reports the honest 0 — never a fabricated
 * utilization). */
typedef struct shtn_kv_cache_info {
    int32_t  allocated;            /* 0 or 1 */
    char     quantization[16];     /* "f16" when allocated */
    uint64_t capacity_bytes;       /* total K+V bytes allocated */
    uint64_t used_bytes;           /* bytes for written positions */
    uint64_t capacity_positions;   /* context_length */
    uint64_t used_positions;       /* 0 until a forward pass exists */
    uint32_t layer_count;
    uint32_t kv_dim;
} shtn_kv_cache_info;

/* shtn_scheduler_info is the measured scheduler snapshot. Counts are
 * real (queued requests, totals since create); active is 0 in Phase 4
 * (no worker thread — the scheduler exists and is measurable, but
 * executes nothing). */
typedef struct shtn_scheduler_info {
    uint32_t active_requests;       /* 0 in Phase 4 */
    uint32_t queued_requests;       /* current queue depth */
    uint32_t max_concurrent;        /* 1 (single-slot) */
    uint32_t queue_depth_limit;     /* bounded queue cap */
    uint64_t total_submitted;       /* since scheduler create */
    uint64_t total_completed;
    uint64_t total_cancelled;
    uint64_t total_failed;
    int32_t  shutting_down;         /* 0 or 1 */
} shtn_scheduler_info;

/* shtn_encode_options configures one tokenizer encode call. */
typedef struct shtn_encode_options {
    int32_t  add_bos;              /* 0 or 1 (only when vocab has BOS) */
    int32_t  add_eos;              /* 0 or 1 (only when vocab has EOS) */
    uint32_t max_tokens;           /* hard cap on output */
    uint32_t reserved;             /* must be 0 */
} shtn_encode_options;

/* shtn_encode_result holds the encode outcome. ids_count is the number
 * of valid ids in the ids buffer (caller-allocated). */
typedef struct shtn_encode_result {
    uint32_t* ids;                 /* caller-allocated, max_tokens capacity */
    uint32_t  ids_count;           /* number of ids written */
    int32_t   truncated;           /* 0 or 1 */
} shtn_encode_result;

/* shtn_decode_options configures one tokenizer decode call. */
typedef struct shtn_decode_options {
    int32_t  skip_special;         /* 0 or 1 */
    uint32_t max_bytes;            /* hard cap on output */
    uint32_t reserved;             /* must be 0 */
} shtn_decode_options;

/* shtn_decode_result holds the decode outcome. text is caller-allocated
 * with max_bytes capacity; text_count is the number of bytes written
 * (excluding the NUL terminator). */
typedef struct shtn_decode_result {
    char*     text;                /* caller-allocated, max_bytes capacity */
    uint32_t  text_count;          /* bytes written (excl. NUL) */
    int32_t   truncated;           /* 0 or 1 */
} shtn_decode_result;

#ifdef __cplusplus
} /* extern "C" */
#endif

#endif /* SHTN_TYPES_H */
