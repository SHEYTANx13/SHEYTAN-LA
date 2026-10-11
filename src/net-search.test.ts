// net-search.test.ts — v1.3.6 (spec §24/§25) contract tests for the
// composer Net Search control's pure state machinery: wire-event
// evidence classification and the honest result-count extraction.
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  isResearchToolStart,
  isResearchToolEnd,
  researchEndFailed,
  extractNetSearchResultCount,
} from "./run-events.ts";

test("a research tool_start is recognized from the wire detail", () => {
  assert.equal(
    isResearchToolStart({
      detail: { function: { name: "research", arguments: '{"query":"x"}' } },
    }),
    true,
  );
  assert.equal(isResearchToolStart({ detail: { name: "research" } }), true);
});

test("non-research tool starts are never misclassified", () => {
  assert.equal(
    isResearchToolStart({
      detail: { function: { name: "files", arguments: "{}" } },
    }),
    false,
  );
  assert.equal(isResearchToolStart(null), false);
  assert.equal(isResearchToolStart("research"), false);
  // A tool whose name merely CONTAINS "research" is not the research tool.
  assert.equal(
    isResearchToolStart({ detail: { name: "researchify" } }),
    false,
  );
});

test("a research tool_end is recognized from the caption", () => {
  assert.equal(isResearchToolEnd("Tool research → 8 results"), true);
  assert.equal(isResearchToolEnd('Tool "research" → error'), true);
  assert.equal(isResearchToolEnd("Tool files → wrote 3 files"), false);
  assert.equal(isResearchToolEnd(undefined), false);
});

test("failed research ends are detected from the result text", () => {
  assert.equal(researchEndFailed("Error: provider unavailable"), true);
  assert.equal(researchEndFailed("8 results for query"), false);
  assert.equal(researchEndFailed(null), false);
});

test("the result count is extracted only when STATED", () => {
  assert.equal(extractNetSearchResultCount("8 results for query"), 8);
  assert.equal(extractNetSearchResultCount("1 result found"), 1);
  assert.equal(extractNetSearchResultCount("search finished"), null);
  assert.equal(extractNetSearchResultCount(undefined), null);
});
