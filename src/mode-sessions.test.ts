// mode-sessions.test.ts — v1.2.8 mode-separated history helpers (PURE
// module tests, node:test).

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  filterSessionsByMode,
  normalizeMode,
  rememberActiveForMode,
  resolveActiveForMode,
  sessionMode,
  sessionTitleLine,
} from "./mode-sessions.ts";

const CHAT_A = { id: "chat-a", mode: "chat" };
const CHAT_B = { id: "chat-b", mode: "chat" };
const AGENT_A = { id: "agent-a", mode: "agent" };
const LEGACY = { id: "legacy-1" }; // pre-v1.2.8 session: no mode field

test("sessionMode migrates legacy sessions to agent deterministically", () => {
  assert.equal(sessionMode(LEGACY), "agent");
  assert.equal(sessionMode(CHAT_A), "chat");
  assert.equal(sessionMode(AGENT_A), "agent");
  assert.equal(sessionMode(null), "agent");
});

test("filterSessionsByMode keeps exactly one conversation space", () => {
  const all = [CHAT_A, AGENT_A, LEGACY, CHAT_B];

  assert.deepEqual(
    filterSessionsByMode(all, "chat").map((s) => s.id),
    ["chat-a", "chat-b"],
  );

  assert.deepEqual(
    filterSessionsByMode(all, "agent").map((s) => s.id),
    ["agent-a", "legacy-1"],
  );
});

test("resolveActiveForMode keeps the remembered selection inside its space", () => {
  const all = [CHAT_A, AGENT_A, CHAT_B];

  // Remembered chat session still exists in chat space → kept.
  assert.equal(resolveActiveForMode(all, "chat", "chat-b"), "chat-b");

  // Remembered session from ANOTHER space → falls to the space's newest,
  // never silently switches the active conversation across spaces.
  assert.equal(resolveActiveForMode(all, "chat", "agent-a"), "chat-a");

  // Empty space → null (a fresh session is created lazily).
  assert.equal(resolveActiveForMode([AGENT_A], "chat", null), null);
});

test("rememberActiveForMode updates one slot without touching the other", () => {
  const map = rememberActiveForMode(
    { chat: "chat-a", agent: "agent-a" },
    "chat",
    "chat-b",
  );

  assert.deepEqual(map, { chat: "chat-b", agent: "agent-a" });
});

test("normalizeMode is total", () => {
  assert.equal(normalizeMode("chat"), "chat");
  assert.equal(normalizeMode("agent"), "agent");
  assert.equal(normalizeMode("nonsense"), "agent");
  assert.equal(normalizeMode(undefined), "agent");
});

test("sessionTitleLine falls back for untitled sessions", () => {
  assert.equal(sessionTitleLine("Deploy fix", "s123456abcdef"), "Deploy fix");
  assert.equal(sessionTitleLine("", "s123456abcdef"), "Untitled s123456a");
  assert.equal(sessionTitleLine(undefined, "s123456abcdef"), "Untitled s123456a");
});

// ---------------------------------------------------------------------------
// v1.2.8.1 regressions: mode-switch target authority + cross-mode picker
// filter. These pin the repair of the deterministic
// empty-conversation-after-switch bug (the v1.2.8 setMode resolved the
// target mode's active session against the PREVIOUS mode's single-mode
// session list — always empty — so nextActive was always null).
// ---------------------------------------------------------------------------

import {
  crossModePickerFilter,
  resolveModeSwitchTarget,
} from "./mode-sessions.ts";

test("resolveModeSwitchTarget: the per-mode memory is the switch authority", () => {
  const remembered: Record<string, string | null> = {
    chat: "chat-a",
    agent: "agent-b",
  };

  assert.equal(resolveModeSwitchTarget(remembered, "agent"), "agent-b");
  assert.equal(resolveModeSwitchTarget(remembered, "chat"), "chat-a");
});

test("resolveModeSwitchTarget: switching with NO memory in the target mode resolves to null (refreshSessions re-resolves asynchronously)", () => {
  const remembered: Record<string, string | null> = {
    chat: "chat-a",
    agent: null,
  };

  // The CURRENT session list is intentionally NOT an input — that was the
  // v1.2.8 bug. A null target resolves to null synchronously and
  // refreshSessions() promotes the newest session of the space afterwards.
  assert.equal(resolveModeSwitchTarget(remembered, "agent"), null);
});

test("resolveModeSwitchTarget: round trip Chat A → Agent A → Chat keeps Chat A", () => {
  let remembered: Record<string, string | null> = {
    chat: "chat-a",
    agent: null,
  };

  // Chat → Agent (remember chat-a, take agent memory: null)
  const agentTarget = resolveModeSwitchTarget(remembered, "agent");
  assert.equal(agentTarget, null);

  // Agent space creates/uses agent-a
  remembered = { ...remembered, agent: "agent-a" };

  // Agent → Chat (remember agent-a, take chat memory)
  assert.equal(resolveModeSwitchTarget(remembered, "chat"), "chat-a");
});

test("crossModePickerFilter: the picker defaults to the OTHER space", () => {
  assert.equal(crossModePickerFilter("chat"), "agent");
  assert.equal(crossModePickerFilter("agent"), "chat");
});
