// memory-evidence.test.ts — v1.8.2 proofs for the live memory indicator:
// the UI renders ONLY backend truth, never a fabricated count.

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  extractMemoryEvidence,
  memoryEvidenceLine,
  type MemoryEvidenceState,
} from "./memory-evidence.ts";

test("extractMemoryEvidence reads the context detail memory block", () => {
  const evidence = extractMemoryEvidence({
    detail: {
      memory: {
        summaryInjected: true,
        summaryTokens: 180,
        recalledExchanges: 2,
        historyRefs: 0,
        recallAttempted: true,
      },
    },
  });

  assert.ok(evidence);
  assert.equal(evidence.summaryInjected, true);
  assert.equal(evidence.summaryTokens, 180);
  assert.equal(evidence.recalledExchanges, 2);
  assert.equal(evidence.recallAttempted, true);
});

test("extractMemoryEvidence returns null when the frame carries none", () => {
  assert.equal(extractMemoryEvidence({ detail: {} }), null);
  assert.equal(extractMemoryEvidence(null), null);
  assert.equal(extractMemoryEvidence("nope"), null);
  assert.equal(extractMemoryEvidence({ detail: { memory: "junk" } }), null);
});

test("memoryEvidenceLine: summary + recalled exchanges", () => {
  const line = memoryEvidenceLine({
    summaryInjected: true,
    recalledExchanges: 2,
  });

  assert.equal(line, "Memory: session summary · 2 recalled exchanges");
});

test("memoryEvidenceLine: singular exchange", () => {
  const line = memoryEvidenceLine({
    summaryInjected: false,
    recalledExchanges: 1,
  });

  assert.equal(line, "Memory: 1 recalled exchange");
});

test("memoryEvidenceLine: recall attempted but nothing matched", () => {
  const line = memoryEvidenceLine({
    summaryInjected: true,
    recalledExchanges: 0,
    recallAttempted: true,
  });

  assert.equal(line, "Memory: session summary · no recall matches");
});

test("memoryEvidenceLine: recall not attempted is stated as such", () => {
  const line = memoryEvidenceLine({
    summaryInjected: false,
    recalledExchanges: 0,
    recallAttempted: false,
  });

  assert.equal(line, "Memory: recall not used");
});

test("memoryEvidenceLine: history references counted", () => {
  const line = memoryEvidenceLine({
    summaryInjected: false,
    historyRefs: 2,
  } satisfies MemoryEvidenceState);

  assert.equal(line, "Memory: 2 history references");
});

test("memoryEvidenceLine: empty evidence degrades to the honest minimum", () => {
  const line = memoryEvidenceLine({
    summaryInjected: false,
  });

  assert.equal(line, "Memory: session context only");
});

test("memoryEvidenceLine: null evidence shows NOTHING (no guess)", () => {
  assert.equal(memoryEvidenceLine(null), null);
});
