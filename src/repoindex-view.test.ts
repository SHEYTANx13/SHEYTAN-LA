// repoindex-view.test.ts — regression contract for the Repository Index
// view helpers (pure formatting; no I/O).
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  evidenceLabel,
  formatScore,
  indexStateLabel,
  indexStateTone,
  isStructuralEvidence,
  resultSummary,
  topLanguages,
} from "./repoindex-view.ts";

test("index state tone maps honestly", () => {
  assert.equal(indexStateTone({ state: "ready" }), "good");
  assert.equal(indexStateTone({ state: "stale" }), "warn");
  assert.equal(indexStateTone({ state: "empty" }), "warn");
  assert.equal(indexStateTone(null), "warn");
});

test("index state label covers the bounded/partial flags", () => {
  assert.equal(indexStateLabel({ state: "ready" }), "Ready");
  assert.equal(
    indexStateLabel({ state: "ready", truncated: true }),
    "Ready (bounded)",
  );
  assert.equal(
    indexStateLabel({ state: "ready", partial: true }),
    "Ready (partial pass)",
  );
  assert.equal(indexStateLabel({ state: "stale" }), "Stale — refresh");
  assert.equal(indexStateLabel({ state: "empty" }), "Not built yet");
  assert.equal(indexStateLabel(null), "Not built yet");
});

test("top languages sort by count then name and stay bounded", () => {
  assert.deepEqual(
    topLanguages({ go: 30, typescript: 12, javascript: 12, json: 40 }, 3),
    ["json ×40", "go ×30", "javascript ×12"],
  );
  assert.deepEqual(topLanguages(undefined), []);
  assert.deepEqual(topLanguages({ "": 5, go: 0, rust: 2 }), ["rust ×2"]);
});

test("score formatting is two-decimal and safe", () => {
  assert.equal(formatScore(5), "5.00");
  assert.equal(formatScore(2.345), "2.35");
  assert.equal(formatScore(Number.NaN), "0.00");
});

test("result summary prefers symbols and appends tags", () => {
  assert.equal(
    resultSummary({
      path: "a.go",
      score: 1,
      evidence: "",
      symbols: ["Helper", "Version"],
      language: "go",
    }),
    "Helper, Version · go",
  );
  assert.equal(
    resultSummary({
      path: "a_test.go",
      score: 1,
      evidence: "",
      language: "go",
      role: "test",
      line: 12,
    }),
    "go · test · :12",
  );
  assert.equal(
    resultSummary({ path: "a.go", score: 1, evidence: "" }),
    "",
  );
});

test("structural evidence is distinguishable from inference", () => {
  assert.equal(
    isStructuralEvidence("dependency of main.go"),
    true,
  );
  assert.equal(isStructuralEvidence("imported by server.go"), true);
  assert.equal(
    isStructuralEvidence("test file of internal/util/util.go"),
    true,
  );
  assert.equal(isStructuralEvidence('symbol match "helper"'), false);
  assert.equal(isStructuralEvidence(undefined), false);
  assert.equal(evidenceLabel(undefined), "");
  assert.equal(evidenceLabel("path match"), "path match");
});
