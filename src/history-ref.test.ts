// history-ref.test.ts — v1.2.8 cross-mode history reference helpers (PURE
// module tests, node:test).

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  describeRange,
  mergeHistoryRefs,
  normalizeHistoryRefs,
  refLabel,
  removeHistoryRef,
} from "./history-ref.ts";

const ACTIVE = "sess-active";

test("normalizeHistoryRefs drops self-references and empties", () => {
  const refs = normalizeHistoryRefs(
    [
      { sessionId: ACTIVE }, // self — dropped
      { sessionId: "" }, // empty — dropped
      { sessionId: "other-1" },
      { sessionId: "other-1" }, // duplicate — dropped
    ],
    ACTIVE,
  );

  assert.deepEqual(refs.map((r) => r.sessionId), ["other-1"]);
});

test("normalizeHistoryRefs caps at 4 (the backend MaxHistoryRefs)", () => {
  const refs = normalizeHistoryRefs(
    [
      { sessionId: "a" },
      { sessionId: "b" },
      { sessionId: "c" },
      { sessionId: "d" },
      { sessionId: "e" },
      { sessionId: "f" },
    ],
    null,
  );

  assert.equal(refs.length, 4);
  assert.deepEqual(refs.map((r) => r.sessionId), ["a", "b", "c", "d"]);
});

test("mergeHistoryRefs preserves existing order and appends new ones", () => {
  const merged = mergeHistoryRefs(
    [{ sessionId: "a" }],
    [{ sessionId: "b", mode: "chat" }],
    null,
  );

  assert.deepEqual(merged.map((r) => r.sessionId), ["a", "b"]);
  assert.equal(merged[1].mode, "chat");
});

test("removeHistoryRef detaches only the requested session", () => {
  const refs = removeHistoryRef(
    [{ sessionId: "a" }, { sessionId: "b" }],
    "a",
  );

  assert.deepEqual(refs.map((r) => r.sessionId), ["b"]);
});

test("refLabel carries the mode provenance", () => {
  assert.equal(refLabel({ sessionId: "s1234567890", mode: "chat" }), "Chat · s1234567");
  assert.equal(refLabel({ sessionId: "s1234567890" }), "Agent · s1234567");
});

test("describeRange describes whole-session vs focused retrieval", () => {
  assert.equal(
    describeRange({ sessionId: "a" }),
    "Relevant turns retrieved per request (whole session)",
  );

  assert.equal(
    describeRange({ sessionId: "a", ranges: [{ from: 1, to: 5 }] }),
    "Focused on message(s) 2–5",
  );
});
