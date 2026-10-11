#!/usr/bin/env python3
"""make-e2e-model.py — browser-E2E model fixture (v1.4.0).

Generates a wide-context tiny llama GGUF for the Playwright suite by
REUSING the canonical fixture writer from native/engine/tests/reference
(one source of truth — no duplicated GGUF machinery).

Why a separate model: the committed fixtures declare ctx=64 (f32) and
ctx=256 (slow) — smaller than the orchestrator's own system prompt
(~430 tokens). A real Chat run against them honestly fails the context
preflight ("context budget impossible"), which is correct product
behavior but leaves the browser suite unable to prove the full
send → stream → settle → persist loop. This fixture keeps the same
validated llama graph and tokenizer shape but declares ctx=4096, so the
REAL native engine executes REAL generation end-to-end.

The weights are the same deterministic synthetic functions — generation
is real (real logits, real sampler, real streaming) but not semantically
meaningful, which the E2E suite states explicitly: it proves the
PIPELINE, not model quality.

Usage: python3 e2e/make-e2e-model.py <output.gguf>
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
           "so", "me", "in", "g", "hi"]
    )
    token_types = [2, 3, 3, 1] + [1] * (len(tokens) - 4)
    merges = ["h e", "l l", "o o", "t h", "r e"]

    size = make_fixture.write_gguf(
        out,
        tokens=tokens,
        token_types=token_types,
        merges=merges,
        emb=32,
        layers=2,
        heads=4,
        kv_heads=2,
        head_dim=8,
        ffn=64,
        ctx=4096,
    )
    print(f"wrote {out} ({size} bytes, llama graph, ctx=4096)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
