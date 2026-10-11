// parse-ended-at.test.ts — v1.2.8 wire-contract test for the recovery
// fast-path root-cause fix: the backend sends runOutcome.endedAt as an
// RFC3339 STRING (pinned by internal/api/runregistry_test.go); the
// recovery parser previously accepted only epoch-ms numbers, so the
// authoritative lastRun fast path could never fire.

import assert from "node:assert/strict";
import { test } from "node:test";

import { parseEndedAt } from "./run-recovery.ts";

test("parseEndedAt accepts the RFC3339 string the backend actually sends", () => {
  const parsed = parseEndedAt("2026-09-19T18:43:47.047145672Z");
  assert.ok(parsed !== null);
  assert.equal(new Date(parsed).toISOString().slice(0, 4), "2026");
});

test("parseEndedAt still accepts epoch-ms numbers (older wire shape)", () => {
  assert.equal(parseEndedAt(1737420000000), 1737420000000);
});

test("parseEndedAt rejects non-timestamps without throwing", () => {
  assert.equal(parseEndedAt("not-a-timestamp"), null);
  assert.equal(parseEndedAt(undefined), null);
  assert.equal(parseEndedAt(null), null);
  assert.equal(parseEndedAt({}), null);
  assert.equal(parseEndedAt(Number.NaN), null);
});
