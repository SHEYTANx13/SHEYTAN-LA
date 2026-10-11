#!/usr/bin/env python3
"""make-e2e-live-model.py — browser-E2E LIVE-VISIBILITY model fixture (v1.8.2).

A wide-context tiny llama GGUF whose generation runs to the token budget
instead of stopping at EOS: the eos_token_id metadata points BEYOND the
vocabulary, so the engine's EOS gate (`eos_of`: has_eos requires
eos < vocab size) stays closed and every turn generates exactly
`max_tokens` tokens of real streamed output.

Why: the standard e2e fixture samples EOS within a few tokens, so the
whole generation completes in ~5 ms — the run settles before any browser
test can observe the live surface. THIS fixture makes the live streaming
window real and deterministic (~160 tokens at v1.9 slow-dims ≈ 1 s on a
fast host, several seconds on CI — spanning many renderer frames on ANY
host), which is what the live-visibility E2E (`e2e/live-stream.spec.ts`)
must prove:

    send → run live → streamed text visible WHILE the run is live →
    more text arrives without user action → settle → one final message.

Generation is still REAL (real logits, real sampler, real streaming, the
validated llama graph executed by the real C++ engine); the token content
is synthetic-weights noise and the EOS suppression is a FIXTURE property,
never a product behavior.

Usage: python3 e2e/make-e2e-live-model.py <output.gguf>
"""

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REFERENCE = os.path.join(
    HERE, "..", "native", "engine", "tests", "reference", "make_fixture.py"
)

sys.path.insert(0, os.path.dirname(REFERENCE))
import make_fixture  # noqa: E402  (the canonical writer)


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__)
        return 2

    out = os.path.abspath(sys.argv[1])
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)

    tokens = (
        ["<unk>", "<s>", "</s>", "▁"]
        + [chr(ord("a") + i) for i in range(26)]
        + ["he", "ll", "o ", "th", "re", " w", "or", "ld", "sa", "y",
           "so", "me", "in", "g", "hi", "er", "an", "es", "at", "en"]
    )
    token_types = [2, 3, 3, 1] + [1] * (len(tokens) - 4)
    merges = ["h e", "l l", "o o", "t h", "r e"]

    # eos_token_id far beyond the vocabulary: the engine resolves
    # has_eos = special.eos < vocab_size → false → generation runs to the
    # request's max_tokens (LENGTH finish), never stops early on EOS.
    # v1.9.0: dims sized so ONE full 160-token generation takes long
    # enough that the streaming window spans MANY renderer frames on
    # EVERY host speed — including fast CI-like hosts where the previous
    # dims (emb=96, layers=4) generated the whole reply in single-digit
    # milliseconds and the live surface legitimately never painted
    # between two animation frames (measured: first response frame and
    # done 6 ms apart; the store's rAF flush then coalesces them into
    # one final commit). The window must be a FIXTURE property, not a
    # property of the host's luck. Rough per-token compute: ~5.2 MMAC
    # (was ~0.25 MMAC) → ~1 s per 160-token generation on a fast host,
    # a few seconds on CI — real generation either way.
    size = make_fixture.write_gguf(
        out,
        tokens=tokens,
        token_types=token_types,
        merges=merges,
        emb=144,
        layers=6,
        heads=6,
        kv_heads=3,
        head_dim=24,
        ffn=384,
        ctx=4096,
        eos_token_id=0x7FFFFFF0,
    )
    print(f"wrote {out} ({size} bytes, llama graph, ctx=4096, EOS unreachable)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
