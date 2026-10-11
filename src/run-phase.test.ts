import assert from "node:assert/strict";
import { test } from "node:test";

import {
  isLivePhase,
  isTerminalPhase,
  nextPhase,
  PHASE_LABELS,
  type RunPhase,
} from "./run-phase.ts";

// v1.2.2 regression tests — the generation lifecycle state machine.
//
// These pin the transitions the live timeline depends on:
//   Preparing → Thinking → Generating → Finalising → Complete
// with error/abort safety and no illegal demotions.

test("fresh run starts in Preparing", () => {
  assert.equal(nextPhase("idle", "run_started"), "preparing");
  assert.equal(nextPhase("complete", "run_started"), "preparing");
  assert.equal(nextPhase("error", "run_started"), "preparing");
  assert.equal(nextPhase("aborted", "run_started"), "preparing");
});

test("reasoning moves Preparing to Thinking, never backwards from Generating", () => {
  assert.equal(nextPhase("preparing", "reasoning_delta"), "thinking");
  assert.equal(nextPhase("thinking", "reasoning_delta"), "thinking");
  // A late reasoning event after the answer started must NOT demote
  // Generating back to Thinking.
  assert.equal(nextPhase("generating", "reasoning_delta"), "generating");
});

test("first response token moves Preparing/Thinking to Generating", () => {
  assert.equal(nextPhase("preparing", "response_delta"), "generating");
  assert.equal(nextPhase("thinking", "response_delta"), "generating");
  assert.equal(nextPhase("generating", "response_delta"), "generating");
});

test("thinking/tool activity advances Preparing but never demotes", () => {
  assert.equal(nextPhase("preparing", "thinking_activity"), "thinking");
  assert.equal(nextPhase("generating", "thinking_activity"), "generating");
  assert.equal(nextPhase("thinking", "thinking_activity"), "thinking");
});

test("done settles any live phase into Finalising", () => {
  for (const phase of ["preparing", "thinking", "generating"] as RunPhase[]) {
    assert.equal(nextPhase(phase, "done"), "finalising");
  }
});

test("history confirmation completes a Finalising run", () => {
  assert.equal(nextPhase("finalising", "history_confirmed"), "complete");
  // History confirmation outside Finalising is a no-op.
  assert.equal(nextPhase("complete", "history_confirmed"), "complete");
  assert.equal(nextPhase("idle", "history_confirmed"), "idle");
});

test("error is terminal from every live phase and ignored after settling", () => {
  for (const phase of [
    "preparing",
    "thinking",
    "generating",
    "finalising",
  ] as RunPhase[]) {
    assert.equal(nextPhase(phase, "error"), "error");
  }
  assert.equal(nextPhase("complete", "error"), "complete");
  assert.equal(nextPhase("aborted", "error"), "aborted");
});

test("abort is terminal from every live phase", () => {
  for (const phase of [
    "preparing",
    "thinking",
    "generating",
    "finalising",
  ] as RunPhase[]) {
    assert.equal(nextPhase(phase, "aborted"), "aborted");
  }
  assert.equal(nextPhase("complete", "aborted"), "complete");
});

test("done arriving after abort does not resurrect the run", () => {
  // The backend emits done(abortCaption) AFTER the user pressed Stop —
  // the phase must stay Aborted, not flip to Finalising/Complete.
  assert.equal(nextPhase("aborted", "done"), "aborted");
});

test("live and terminal classification", () => {
  assert.equal(isLivePhase("preparing"), true);
  assert.equal(isLivePhase("thinking"), true);
  assert.equal(isLivePhase("generating"), true);
  assert.equal(isLivePhase("finalising"), true);

  assert.equal(isLivePhase("idle"), false);
  assert.equal(isLivePhase("complete"), false);
  assert.equal(isLivePhase("error"), false);
  assert.equal(isLivePhase("aborted"), false);

  assert.equal(isTerminalPhase("complete"), true);
  assert.equal(isTerminalPhase("error"), true);
  assert.equal(isTerminalPhase("aborted"), true);
  assert.equal(isTerminalPhase("idle"), false);
  assert.equal(isTerminalPhase("generating"), false);
});

test("every phase has a user-facing label", () => {
  const phases: RunPhase[] = [
    "idle",
    "preparing",
    "thinking",
    "generating",
    "finalising",
    "complete",
    "error",
    "aborted",
  ];

  for (const phase of phases) {
    assert.ok(PHASE_LABELS[phase].length > 0, `label missing for ${phase}`);
  }
});

// ---------------------------------------------------------------------------
// v1.7.4 run control — Pause / Edit / Resume sequences.
//
// The machine must express the ONE authoritative backend contract:
// RUNNING → PAUSING → PAUSED → RESUMING → RUNNING → terminal, with
// idempotent duplicates and no illegal transitions from idle/terminal.
// ---------------------------------------------------------------------------

test("pause_requested moves live phases to pausing, never idle/terminal", () => {
  for (const phase of ["preparing", "thinking", "generating", "finalising"] as RunPhase[]) {
    assert.equal(nextPhase(phase, "pause_requested"), "pausing");
  }
  // A pause of an idle or terminal run is a no-op (double-click, stale
  // client) — the UI never fabricates a pausing state.
  assert.equal(nextPhase("idle", "pause_requested"), "idle");
  assert.equal(nextPhase("complete", "pause_requested"), "complete");
  assert.equal(nextPhase("error", "pause_requested"), "error");
  assert.equal(nextPhase("aborted", "pause_requested"), "aborted");
});

test("paused confirms from any live or pausing phase", () => {
  for (const phase of ["preparing", "thinking", "generating", "finalising", "pausing"] as RunPhase[]) {
    assert.equal(nextPhase(phase, "paused"), "paused");
  }
  assert.equal(nextPhase("idle", "paused"), "idle");
});

test("resume runs only from paused", () => {
  assert.equal(nextPhase("paused", "resume_requested"), "resuming");
  assert.equal(nextPhase("generating", "resume_requested"), "generating");
  assert.equal(nextPhase("idle", "resume_requested"), "idle");
});

test("resumed returns to generating; deltas keep it there", () => {
  assert.equal(nextPhase("resuming", "resumed"), "generating");
  assert.equal(nextPhase("paused", "resumed"), "generating");
  assert.equal(nextPhase("resuming", "response_delta"), "generating");
  assert.equal(nextPhase("resuming", "reasoning_delta"), "thinking");
});

test("double pause is a no-op; double resume is a no-op", () => {
  // double pause
  const afterFirst = nextPhase("generating", "pause_requested");
  assert.equal(nextPhase(afterFirst, "pause_requested"), "pausing");
  // double resume
  const paused = nextPhase(afterFirst, "paused");
  assert.equal(nextPhase(paused, "resume_requested"), "resuming");
  assert.equal(nextPhase("resuming", "resume_requested"), "resuming");
});

test("start → pause → reconnect → edit → resume → done sequence", () => {
  let phase: RunPhase = "idle";
  phase = nextPhase(phase, "run_started"); // preparing
  phase = nextPhase(phase, "response_delta"); // generating
  phase = nextPhase(phase, "pause_requested"); // pausing
  assert.equal(phase, "pausing");
  // reconnect: the run_snapshot re-renders the paused state
  phase = nextPhase(phase, "paused");
  assert.equal(phase, "paused");
  // resume after the edit
  phase = nextPhase(phase, "resume_requested");
  assert.equal(phase, "resuming");
  phase = nextPhase(phase, "response_delta");
  assert.equal(phase, "generating");
  phase = nextPhase(phase, "done");
  assert.equal(phase, "finalising");
  phase = nextPhase(phase, "history_confirmed");
  assert.equal(phase, "complete");
});

test("start → pause → stop sequence", () => {
  let phase: RunPhase = "idle";
  phase = nextPhase(phase, "run_started");
  phase = nextPhase(phase, "pause_requested");
  phase = nextPhase(phase, "paused");
  // Stop after pause settles the run.
  phase = nextPhase(phase, "aborted");
  assert.equal(phase, "aborted");
});

test("a pause racing the resume window still settles paused", () => {
  // The backend may confirm a NEW pause while the frontend is still in
  // resuming (the user paused between resume and the first delta) — the
  // machine follows the authoritative backend event.
  const phase = nextPhase("resuming", "paused");
  assert.equal(phase, "paused");
});

test("paused phase classification helpers", () => {
  assert.equal(isLivePhase("paused"), false);
  assert.equal(isLivePhase("pausing"), true);
  assert.equal(isLivePhase("resuming"), true);
  assert.equal(isTerminalPhase("paused"), false);
});

test("phase labels cover the run-control phases", () => {
  assert.equal(PHASE_LABELS.pausing, "Pausing…");
  assert.equal(PHASE_LABELS.paused, "Paused");
  assert.equal(PHASE_LABELS.resuming, "Resuming…");
});
