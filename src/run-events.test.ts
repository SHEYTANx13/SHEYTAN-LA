// run-events.test.ts — v1.2.5 unit tests for the wire-event vocabulary
// mapping, control normalization and the manual-tool policy helpers.
import test from "node:test";
import assert from "node:assert/strict";

import {
  isRunEvidence,
  normalizeEventKind,
  normalizeThinkingControl,
  normalizeToolAllowlist,
  normalizeToolPolicyMode,
  THINKING_OPTIONS,
} from "./run-events.ts";

test("legacy event names keep their canonical meaning", () => {
  assert.equal(normalizeEventKind("response"), "assistant_delta");
  assert.equal(normalizeEventKind("reasoning"), "thinking_delta");
  assert.equal(normalizeEventKind("tool_start"), "tool_start");
  assert.equal(normalizeEventKind("tool_end"), "tool_end");
  assert.equal(normalizeEventKind("verification"), "verification");
  assert.equal(normalizeEventKind("done"), "done");
  assert.equal(normalizeEventKind("error"), "error");
  assert.equal(normalizeEventKind("session"), "session");
});

test("v1.2.5 event names alias the same semantics", () => {
  assert.equal(normalizeEventKind("assistant_delta"), "assistant_delta");
  assert.equal(normalizeEventKind("thinking_delta"), "thinking_delta");
  assert.equal(normalizeEventKind("thinking_start"), "thinking_open");
  assert.equal(normalizeEventKind("thinking_end"), "thinking_close");
  assert.equal(normalizeEventKind("complete"), "complete");
  assert.equal(normalizeEventKind("status"), "status");
  assert.equal(normalizeEventKind("escalation"), "escalation");
});

test("legacy thinking captions and idle map through (nothing dropped)", () => {
  assert.equal(normalizeEventKind("thinking"), "progress");
  assert.equal(normalizeEventKind("idle"), "idle");
  assert.equal(normalizeEventKind("context"), "context");
  assert.equal(normalizeEventKind("perf"), "perf");
  assert.equal(normalizeEventKind("failure"), "failure");
});

test("unknown names degrade to unknown, never a guess", () => {
  assert.equal(normalizeEventKind("nonsense"), "unknown");
  assert.equal(normalizeEventKind(""), "unknown");
});

test("run evidence covers every live-proof event kind", () => {
  for (const kind of [
    "assistant_delta",
    "thinking_delta",
    "thinking_open",
    "thinking_close",
    "tool_start",
    "tool_end",
    "done",
    "complete",
    "status",
    "escalation",
  ] as const) {
    assert.ok(isRunEvidence(kind), `${kind} must be run evidence`);
  }

  assert.equal(isRunEvidence("context"), false);
  assert.equal(isRunEvidence("unknown"), false);
});

test("thinking control normalization (v1.8.5 four-level ladder)", () => {
  // The ladder normalizes onto itself.
  assert.equal(normalizeThinkingControl("low"), "low");
  assert.equal(normalizeThinkingControl("mid"), "mid");
  assert.equal(normalizeThinkingControl("high"), "high");
  assert.equal(normalizeThinkingControl("ultra"), "ultra");

  // The legacy v1.2.5 vocabulary migrates and is never re-emitted.
  assert.equal(normalizeThinkingControl("fast"), "low");
  assert.equal(normalizeThinkingControl("auto"), "mid");
  assert.equal(normalizeThinkingControl("thinking"), "high");
  assert.equal(normalizeThinkingControl("deep"), "high");

  // Unknown / absent values degrade to the balanced default.
  assert.equal(normalizeThinkingControl("AUTO"), "mid");
  assert.equal(normalizeThinkingControl(null), "mid");
  assert.equal(normalizeThinkingControl(undefined), "mid");
  assert.equal(normalizeThinkingControl("bogus"), "mid");

  // The option surface IS the ladder (labels render from it).
  assert.deepEqual(
    THINKING_OPTIONS.map((o) => o.value),
    ["low", "mid", "high", "ultra"],
  );
});

test("tool policy mode normalization", () => {
  assert.equal(normalizeToolPolicyMode("manual"), "manual");
  assert.equal(normalizeToolPolicyMode("AUTO"), "auto");
  assert.equal(normalizeToolPolicyMode("nonsense"), "auto");
  assert.equal(normalizeToolPolicyMode(null), "auto");
});

test("tool allow-list normalization dedupes and trims", () => {
  assert.deepEqual(
    normalizeToolAllowlist(["files", " files ", "Shell", ""]),
    ["files", "Shell"],
  );

  assert.deepEqual(normalizeToolAllowlist(null), []);
  assert.deepEqual(normalizeToolAllowlist("files"), []);
  assert.deepEqual(
    normalizeToolAllowlist([1, true, undefined, "git"]),
    ["git"],
  );
});

// --- v1.2.6 continuation: the transport-control frame kinds ---------------
//
// `attached` (the deterministic attach acknowledgement) and `run_snapshot`
// (the authoritative replay) are TRANSPORT-CONTROL frames, not run
// evidence: they must never advance the run-phase machine or count as
// proof a generation is live (that proof is a sequenced activity event).

test("transport-control frames are not run evidence", () => {
  assert.equal(normalizeEventKind("attached"), "unknown");
  assert.equal(normalizeEventKind("run_snapshot"), "unknown");
  assert.equal(isRunEvidence(normalizeEventKind("attached")), false);
  assert.equal(isRunEvidence(normalizeEventKind("run_snapshot")), false);
});

test("run evidence kinds remain recognized", () => {
  for (const kind of [
    "response",
    "reasoning",
    "thinking_start",
    "tool_start",
    "tool_end",
    "done",
    "complete",
    "status",
    "escalation",
  ]) {
    assert.equal(
      isRunEvidence(normalizeEventKind(kind)),
      true,
      `${kind} must remain run evidence`,
    );
  }
});
