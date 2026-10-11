#!/usr/bin/env python3
"""make_fixture.py — deterministic tiny llama GGUF + reference forward values.

Generates (into tests/fixtures/):
  tiny-llama-f32.gguf   a complete, valid, llama.cpp-loadable GGUF v3 model
                        (llama architecture, F32 tensors, real BPE tokenizer)
  tiny-llama-ref.json   reference values computed by THIS independent Python
                        implementation: per-stage tensors (embedding, rmsnorm
                        output, q/k/v projections, layer-0 attention output,
                        hidden state after each layer) and the final logits
                        for a fixed prompt.

The reference implementation does NOT call the C++ engine — it parses the
GGUF file it just wrote and computes the llama math with numpy, including
the EXACT fp16 K/V storage semantics of the native engine (values are
cast to float16 and back before use).

Deterministic: fixed weight functions, no RNG. Re-run to regenerate; the
C++ forward tests and the Go integration tests consume the outputs.

Model: emb=32, layers=2, heads=4, kv_heads=2 (GQA 2:1), head_dim=8,
ffn=64, ctx=64, vocab=32, rms_eps=1e-5, rope_base=10000, untied output.
"""

import json
import math
import os
import struct
import sys

import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
FIXTURES = os.path.join(os.path.dirname(HERE), "fixtures")

# --- model definition ---------------------------------------------------------

EMB = 32
LAYERS = 2
HEADS = 4
KV_HEADS = 2
HEAD_DIM = 8
FFN = 64
CTX = 64
VOCAB = 32
RMS_EPS = 1e-5
ROPE_BASE = 10000.0
ATTN_OUT = HEADS * HEAD_DIM  # 32
KV_DIM = KV_HEADS * HEAD_DIM  # 16

TOKENS = (
    ["<unk>", "<s>", "</s>", "\u2581"]  # ▁ (llama-style space marker)
    + [chr(ord("a") + i) for i in range(26)]
    + ["he", "ll"]
)
TOKEN_TYPES = [2, 3, 3, 1] + [1] * 28
MERGES = ["h e", "l l"]
BOS = 1
EOS = 2

PROMPT_TOKENS = [BOS, TOKENS.index("▁"), TOKENS.index("he"), TOKENS.index("ll"), TOKENS.index("o")]

assert len(TOKENS) == VOCAB, f"vocab mismatch: {len(TOKENS)} tokens vs VOCAB={VOCAB}"
assert len(TOKEN_TYPES) == VOCAB
PROMPT_TEXT = "hello"


def w_emb(i, j):
    return 0.5 * math.sin(i * 0.9 + j * 0.31)


def w_norm(i):
    return 1.0 + 0.05 * math.sin(i * 0.77)


def w_matrix(row, col, seed):
    return 0.08 * math.sin(row * 0.9 + col * 0.5 + seed) + 0.04 * math.cos(
        row * 0.31 + col * 1.1 + seed * 2.0
    )


# --- weight tables (numpy, float32 — exactly what the file stores) ------------


def matrix(rows, cols, seed):
    m = np.zeros((rows, cols), dtype=np.float32)
    for r in range(rows):
        for c in range(cols):
            m[r, c] = w_matrix(r, c, seed)
    return m


def vec_norm(n):
    return np.array([w_norm(i) for i in range(n)], dtype=np.float32)


TOKEN_EMBD = np.zeros((VOCAB, EMB), dtype=np.float32)  # row t = embedding
for t in range(VOCAB):
    for j in range(EMB):
        TOKEN_EMBD[t, j] = w_emb(t, j)

OUTPUT = np.zeros((VOCAB, EMB), dtype=np.float32)  # row v = logits weights
for v in range(VOCAB):
    for j in range(EMB):
        OUTPUT[v, j] = w_matrix(v, j, 7.7)

OUTPUT_NORM = vec_norm(EMB)

LAYER_W = []
for l in range(LAYERS):
    LAYER_W.append(
        {
            "attn_norm": vec_norm(EMB),
            "ffn_norm": vec_norm(EMB),
            "wq": matrix(ATTN_OUT, EMB, 0.1 + l),  # rows = output elements
            "wk": matrix(KV_DIM, EMB, 0.2 + l),
            "wv": matrix(KV_DIM, EMB, 0.3 + l),
            "wo": matrix(EMB, ATTN_OUT, 0.4 + l),
            "wg": matrix(FFN, EMB, 0.5 + l),
            "wu": matrix(FFN, EMB, 0.6 + l),
            "wd": matrix(EMB, FFN, 0.8 + l),
        }
    )

# --- GGUF writer ----------------------------------------------------------------


def put_u32(b, v):
    b.extend(struct.pack("<I", v))


def put_u64(b, v):
    b.extend(struct.pack("<Q", v))


def put_f32(b, v):
    b.extend(struct.pack("<f", v))


def put_str(b, s):
    enc = s.encode("utf-8")
    put_u64(b, len(enc))
    b.extend(enc)


def kv_str(b, k, v):
    put_str(b, k)
    put_u32(b, 8)
    put_str(b, v)


def kv_u32(b, k, v):
    put_str(b, k)
    put_u32(b, 4)
    put_u32(b, v)


def kv_f32(b, k, v):
    put_str(b, k)
    put_u32(b, 6)
    put_f32(b, v)


def kv_str_array(b, k, vals):
    put_str(b, k)
    put_u32(b, 9)
    put_u32(b, 8)
    put_u64(b, len(vals))
    for v in vals:
        put_str(b, v)


def kv_i32_array(b, k, vals):
    put_str(b, k)
    put_u32(b, 9)
    put_u32(b, 5)
    put_u64(b, len(vals))
    for v in vals:
        b.extend(struct.pack("<i", v))


def kv_f32_array(b, k, vals):
    put_str(b, k)
    put_u32(b, 9)
    put_u32(b, 6)
    put_u64(b, len(vals))
    for v in vals:
        put_f32(b, v)


def write_gguf(path, tokens=None, token_types=None, merges=None,
                emb=EMB, layers=LAYERS, heads=HEADS, kv_heads=KV_HEADS,
                head_dim=HEAD_DIM, ffn=FFN, ctx=CTX, rms_eps=RMS_EPS,
                rope_base=ROPE_BASE, eos_token_id=EOS):
    """Write one llama GGUF. Defaults reproduce the reference fixture; the
    slow variant (bigger dims) serves cancellation/latency tests and needs
    no reference values."""
    tokens = tokens if tokens is not None else TOKENS
    token_types = token_types if token_types is not None else TOKEN_TYPES
    merges = merges if merges is not None else MERGES
    vocab = len(tokens)
    attn_out = heads * head_dim
    kv_dim = kv_heads * head_dim

    token_embd = np.zeros((vocab, emb), dtype=np.float32)
    for t in range(vocab):
        for j in range(emb):
            token_embd[t, j] = w_emb(t, j)
    output = np.zeros((vocab, emb), dtype=np.float32)
    for v in range(vocab):
        for j in range(emb):
            output[v, j] = w_matrix(v, j, 7.7)
    out_norm = vec_norm(emb)

    layer_w = []
    for l in range(layers):
        layer_w.append({
            "attn_norm": vec_norm(emb),
            "ffn_norm": vec_norm(emb),
            "wq": matrix(attn_out, emb, 0.1 + l),
            "wk": matrix(kv_dim, emb, 0.2 + l),
            "wv": matrix(kv_dim, emb, 0.3 + l),
            "wo": matrix(emb, attn_out, 0.4 + l),
            "wg": matrix(ffn, emb, 0.5 + l),
            "wu": matrix(ffn, emb, 0.6 + l),
            "wd": matrix(emb, ffn, 0.8 + l),
        })

    # Tensors: (name, ne, rows). ne follows the GGUF convention ne0 = row
    # length; a tensor [ne0, ne1] stores ne1 rows of ne0 floats. The
    # numpy matrices are (rows=ne1, cols=ne0).
    def mat_rows(m):
        return [list(map(float, m[r, :])) for r in range(m.shape[0])]

    tensors = []
    tensors.append(("token_embd.weight", (emb, vocab), mat_rows(token_embd)))
    tensors.append(("output.weight", (emb, vocab), mat_rows(output)))
    tensors.append(("output_norm.weight", (emb,), [list(map(float, out_norm))]))
    for l in range(layers):
        w = layer_w[l]
        tensors.append((f"blk.{l}.attn_norm.weight", (emb,), [list(map(float, w["attn_norm"]))]))
        tensors.append((f"blk.{l}.attn_q.weight", (emb, attn_out), mat_rows(w["wq"])))
        tensors.append((f"blk.{l}.attn_k.weight", (emb, kv_dim), mat_rows(w["wk"])))
        tensors.append((f"blk.{l}.attn_v.weight", (emb, kv_dim), mat_rows(w["wv"])))
        tensors.append((f"blk.{l}.attn_output.weight", (attn_out, emb), mat_rows(w["wo"])))
        tensors.append((f"blk.{l}.ffn_norm.weight", (emb,), [list(map(float, w["ffn_norm"]))]))
        tensors.append((f"blk.{l}.ffn_gate.weight", (emb, ffn), mat_rows(w["wg"])))
        tensors.append((f"blk.{l}.ffn_up.weight", (emb, ffn), mat_rows(w["wu"])))
        tensors.append((f"blk.{l}.ffn_down.weight", (ffn, emb), mat_rows(w["wd"])))

    kv = {
        "general.architecture": ("str", "llama"),
        "general.name": ("str", "tiny-llama-fixture"),
        "general.file_type": ("u32", 0),  # F32
        "llama.context_length": ("u32", ctx),
        "llama.embedding_length": ("u32", emb),
        "llama.block_count": ("u32", layers),
        "llama.feed_forward_length": ("u32", ffn),
        "llama.attention.head_count": ("u32", heads),
        "llama.attention.head_count_kv": ("u32", kv_heads),
        "llama.attention.key_length": ("u32", head_dim),
        "llama.attention.layer_norm_rms_epsilon": ("f32", rms_eps),
        "llama.rope.freq_base": ("f32", rope_base),
        "llama.vocab_size": ("u32", vocab),
        "tokenizer.ggml.model": ("str", "llama"),
        "tokenizer.ggml.tokens": ("str_array", tokens),
        "tokenizer.ggml.token_type": ("i32_array", token_types),
        "tokenizer.ggml.scores": ("f32_array", [0.0] * vocab),
        "tokenizer.ggml.merges": ("str_array", merges),
        "tokenizer.ggml.bos_token_id": ("u32", BOS),
        "tokenizer.ggml.eos_token_id": ("u32", eos_token_id),
        "tokenizer.ggml.unknown_token_id": ("u32", 0),
    }

    b = bytearray()
    b.extend(b"GGUF")
    put_u32(b, 3)  # version
    put_u64(b, len(tensors))
    put_u64(b, len(kv))

    for k, (kind, v) in kv.items():
        if kind == "str":
            kv_str(b, k, v)
        elif kind == "u32":
            kv_u32(b, k, v)
        elif kind == "f32":
            kv_f32(b, k, v)
        elif kind == "str_array":
            kv_str_array(b, k, v)
        elif kind == "i32_array":
            kv_i32_array(b, k, v)
        elif kind == "f32_array":
            kv_f32_array(b, k, v)
        else:
            raise ValueError(kind)

    # Tensor table (offsets relative to the data section, file order).
    offset = 0
    for name, ne, rows in tensors:
        put_str(b, name)
        put_u32(b, len(ne))
        for d in ne:
            put_u64(b, d)
        put_u32(b, 0)  # F32
        put_u64(b, offset)
        offset += sum(len(r) for r in rows) * 4

    while len(b) % 32 != 0:
        b.extend(b"\x00")

    for _name, _ne, rows in tensors:
        for r in rows:
            for v in r:
                put_f32(b, v)

    with open(path, "wb") as f:
        f.write(b)
    return len(b)


# --- independent reference forward ---------------------------------------------

F16 = np.float16


def rms_norm(x, w, eps):
    ss = np.sum(np.asarray(x, dtype=np.float64) ** 2)
    mean = ss / len(x)
    scale = 1.0 / math.sqrt(mean + eps)
    out = np.asarray(x, dtype=np.float64) * scale * np.asarray(w, dtype=np.float64)
    return out.astype(np.float32)


def rope_pair(x, pos, head_dim, freq_base):
    half = head_dim // 2
    out = np.array(x, dtype=np.float64)
    for j in range(half):
        f = freq_base ** (-2.0 * j / head_dim)
        theta = pos * f
        c, s = math.cos(theta), math.sin(theta)
        x0 = out[j]
        x1 = out[j + half]
        out[j] = x0 * c - x1 * s
        out[j + half] = x0 * s + x1 * c
    return out.astype(np.float32)


def to_fp16(x):
    """EXACT fp16 round-trip (matches the engine's KV cache storage)."""
    return np.asarray(x, dtype=np.float32).astype(F16).astype(np.float32)


def softmax(v):
    m = np.max(v)
    e = np.exp(v - m)
    return e / np.sum(e)


def silu(z):
    return z / (1.0 + np.exp(-z))


def matvec(m, x):
    """out[i] = sum_j m[i,j]*x[j] in float64 — mirrors the C++ dot()."""
    return (np.asarray(m, dtype=np.float64) @ np.asarray(x, dtype=np.float64)).astype(
        np.float32
    )


def reference_forward(tokens):
    """Stage captures at position 0, layer 0 (the full pass runs in main
    through run_layer)."""
    stages = {}

    cur = np.array(TOKEN_EMBD[tokens[0]], dtype=np.float32)
    stages["embedding_t0"] = [float(v) for v in cur]

    xb0 = rms_norm(cur, LAYER_W[0]["attn_norm"], RMS_EPS)
    stages["rmsnorm_after_emb"] = [float(v) for v in xb0]
    q0 = matvec(LAYER_W[0]["wq"], xb0)
    k0 = matvec(LAYER_W[0]["wk"], xb0)
    v0 = matvec(LAYER_W[0]["wv"], xb0)
    stages["q_proj_l0_p0"] = [float(t) for t in q0]
    stages["k_proj_l0_p0"] = [float(t) for t in k0]
    stages["v_proj_l0_p0"] = [float(t) for t in v0]

    # head 0, single position: attention output = V (softmax of one
    # score); the engine reads the fp16-round-tripped V from its cache.
    stages["attn_l0_h0_p0"] = [float(t) for t in to_fp16(v0)[0:HEAD_DIM]]

    return stages


def run_layer(l, pos, tok, x, cache_k, cache_v):
    """One layer for one position, recomputing K/V for THIS position and
    reusing cached K/V of EARLIER positions (written by the main pass)."""
    w = LAYER_W[l]
    xb = rms_norm(x, w["attn_norm"], RMS_EPS)
    q = matvec(w["wq"], xb)
    k = matvec(w["wk"], xb)
    v = matvec(w["wv"], xb)
    for h in range(HEADS):
        q[h * HEAD_DIM : (h + 1) * HEAD_DIM] = rope_pair(
            q[h * HEAD_DIM : (h + 1) * HEAD_DIM], pos, HEAD_DIM, ROPE_BASE
        )
    for hv in range(KV_HEADS):
        k[hv * HEAD_DIM : (hv + 1) * HEAD_DIM] = rope_pair(
            k[hv * HEAD_DIM : (hv + 1) * HEAD_DIM], pos, HEAD_DIM, ROPE_BASE
        )
    cache_k[pos][l] = to_fp16(k)
    cache_v[pos][l] = to_fp16(v)

    attn = np.zeros(ATTN_OUT, dtype=np.float64)
    for h in range(HEADS):
        kv_h = h // (HEADS // KV_HEADS)
        qh = q[h * HEAD_DIM : (h + 1) * HEAD_DIM].astype(np.float64)
        scores = np.zeros(pos + 1)
        for j in range(pos + 1):
            kj = cache_k[j][l][kv_h * HEAD_DIM : (kv_h + 1) * HEAD_DIM]
            acc = 0.0
            for d in range(HEAD_DIM):
                acc += float(qh[d]) * float(kj[d])
            scores[j] = acc / math.sqrt(HEAD_DIM)
        p = softmax(scores)
        for j in range(pos + 1):
            vj = cache_v[j][l][kv_h * HEAD_DIM : (kv_h + 1) * HEAD_DIM]
            attn[h * HEAD_DIM : (h + 1) * HEAD_DIM] += p[j] * np.asarray(
                vj, dtype=np.float64
            )

    o = matvec(w["wo"], attn)
    x = (np.asarray(x, dtype=np.float64) + np.asarray(o, dtype=np.float64)).astype(
        np.float32
    )
    xb3 = rms_norm(x, w["ffn_norm"], RMS_EPS)
    g = matvec(w["wg"], xb3)
    u = matvec(w["wu"], xb3)
    act = (silu(g.astype(np.float64)) * np.asarray(u, dtype=np.float64)).astype(
        np.float32
    )
    d = matvec(w["wd"], act)
    return (np.asarray(x, dtype=np.float64) + np.asarray(d, dtype=np.float64)).astype(
        np.float32
    )


def run_layer_full(l, pos, tokens, x, cache_k, cache_v):
    return run_layer(l, pos, tokens[pos], x, cache_k, cache_v)


def main():
    os.makedirs(FIXTURES, exist_ok=True)

    gguf_path = os.path.join(FIXTURES, "tiny-llama-f32.gguf")
    size = write_gguf(gguf_path)
    print(f"wrote {gguf_path} ({size} bytes)")

    # Slow fixture: bigger dims so one token takes ~milliseconds — used
    # by the cancellation / mid-flight tests (no reference values needed).
    slow_tokens = (
        ["<unk>", "<s>", "</s>", "\u2581"]
        + [chr(ord("a") + i) for i in range(26)]
        + ["he", "ll", "oo", "ab", "lo", "el", "or", "th", "in", "an",
           "er", "es", "re", "st", "ar", "ur", "en", "at"]
    )
    slow_types = [2, 3, 3, 1] + [1] * (len(slow_tokens) - 4)
    slow_merges = ["h e", "l l", "o o"]
    slow_path = os.path.join(FIXTURES, "tiny-llama-slow.gguf")
    size = write_gguf(slow_path, tokens=slow_tokens, token_types=slow_types,
                      merges=slow_merges, emb=128, layers=6, heads=8,
                      kv_heads=4, head_dim=16, ffn=512, ctx=256)
    print(f"wrote {slow_path} ({size} bytes)")

    stages = reference_forward(PROMPT_TOKENS)
    stages.pop("hidden_after_l0_last", None)  # recomputed below properly
    stages.pop("hidden_after_l1_last", None)

    # Recompute the full sequence cleanly with run_layer only.
    n = len(PROMPT_TOKENS)
    cache_k = [[None] * LAYERS for _ in range(n)]
    cache_v = [[None] * LAYERS for _ in range(n)]
    states = {}
    for l in range(LAYERS):
        for pos, tok in enumerate(PROMPT_TOKENS):
            if l == 0:
                x = np.array(TOKEN_EMBD[tok], dtype=np.float32)
            else:
                x = states[(l - 1, pos)]
            x2 = run_layer(l, pos, tok, x, cache_k, cache_v)
            states[(l, pos)] = x2
    final = states[(LAYERS - 1, n - 1)]
    stages["hidden_after_l0_last"] = [float(t) for t in states[(0, n - 1)]]
    stages["hidden_after_l1_last"] = [float(t) for t in final]

    xb_final = rms_norm(final, OUTPUT_NORM, RMS_EPS)
    logits = matvec(OUTPUT, xb_final)
    stages["logits_final"] = [float(t) for t in logits]

    ref = {
        "prompt": PROMPT_TOKENS,
        "prompt_text": PROMPT_TEXT,
        "hyper": {
            "emb": EMB,
            "layers": LAYERS,
            "heads": HEADS,
            "kv_heads": KV_HEADS,
            "head_dim": HEAD_DIM,
            "ffn": FFN,
            "ctx": CTX,
            "vocab": VOCAB,
            "rms_eps": RMS_EPS,
            "rope_base": ROPE_BASE,
        },
        "stages": stages,
        "argmax_token": int(np.argmax(stages["logits_final"])),
        "argmax_token_text": TOKENS[int(np.argmax(stages["logits_final"]))],
    }

    ref_path = os.path.join(FIXTURES, "tiny-llama-ref.json")
    with open(ref_path, "w") as f:
        json.dump(ref, f, indent=1)
    print(f"wrote {ref_path}")

    # Flat-text variant for the C++ tests (trivial to parse).
    txt_path = os.path.join(FIXTURES, "tiny-llama-ref.txt")
    with open(txt_path, "w") as f:
        def row(name, vals):
            f.write(name + " " + " ".join(f"{v:.9g}" for v in vals) + "\n")

        row("hyper", [EMB, LAYERS, HEADS, KV_HEADS, HEAD_DIM, FFN, CTX, VOCAB,
                      RMS_EPS, ROPE_BASE])
        row("prompt", PROMPT_TOKENS)
        for k, v in stages.items():
            row(k, v)
        row("argmax_token", [int(np.argmax(stages["logits_final"]))])
    print(f"wrote {txt_path}")
    print(f"argmax token: {ref['argmax_token']} ({ref['argmax_token_text']!r})")

    # App fixture (v1.1.5 repair): the F32 fixture dims (greedy decode
    # deterministically runs to max_tokens — pinned by test_generate) but
    # with a 2048-token context so a REAL application prompt (system
    # briefing + user turn) fits the native engine's honest
    # context-reject bound. Used by the HTTP-level native end-to-end
    # regression test (internal/api TestEngineToggleNativeServesGeneration).
    app_path = os.path.join(FIXTURES, "tiny-llama-app.gguf")
    size = write_gguf(app_path, ctx=2048)
    print(f"wrote {app_path} ({size} bytes)")


if __name__ == "__main__":
    sys.exit(main())
