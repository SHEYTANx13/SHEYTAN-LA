// workspace-v136.test.ts — v1.6.0 REPAIR contract for the top-level
// Chat/Agent navigation:
//
//   - Chat is the DEFAULT landing surface (root/empty hash → Chat)
//   - every view owns an explicit #hash (no agent→empty special case)
//   - the initial view resolves through ONE deterministic path:
//     explicit hash > remembered workspace view > Chat
//   - the conversation-space mode follows the view (chat/agent bind,
//     shared layers bind nothing)
//   - the v1.3.6 Research-layer removal regression stays pinned: no
//     Research layer exists and a stale #research hash resolves through
//     the established invalid-view fallback (now the Chat default).
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  WORKSPACE_LAYERS,
  getWorkspaceLayer,
  isWorkspaceView,
  parseWorkspaceHash,
  resolveInitialView,
  viewModeBinding,
  visibleWorkspaceLayers,
  workspaceHash,
  type WorkspaceView,
} from "./workspace.ts";

test("the research layer is removed from the workspace vocabulary", () => {
  assert.equal(
    WORKSPACE_LAYERS.some((l) => l.id === "research"),
    false,
    "no Research layer may exist in WORKSPACE_LAYERS",
  );
  assert.equal(isWorkspaceView("research"), false);
});

test("no layer may claim the removed surface", () => {
  for (const layer of WORKSPACE_LAYERS) {
    assert.notEqual(layer.label, "Research");
    assert.notEqual(layer.icon, "⌕");
  }
});

// v1.6.0 repair (spec §11): the old #research bookmark resolves through
// the established invalid-view fallback — which is now the Chat default
// (the landing surface changed; the fallback MECHANISM is preserved).
test("an old #research hash resolves to the Chat default, never to a hidden panel", () => {
  assert.equal(parseWorkspaceHash("#research"), "chat");
  assert.equal(getWorkspaceLayer("research" as WorkspaceView).id, "agent");
});

test("both modes still expose the surviving layers", () => {
  for (const mode of ["chat", "agent"] as const) {
    const layers = visibleWorkspaceLayers(mode);
    assert.ok(layers.length > 0);
    // v1.6.0 (spec §6): CHAT leads the navigation — Chat and Agent are
    // both TOP-LEVEL views (the internal segmented selector is gone).
    assert.equal(layers[0].id, "chat");
    assert.equal(layers[1].id, "agent");
  }
});

// v1.6.0 (spec §6): the top-level Chat/Agent navigation contract. Chat
// and Agent are real views with their own hashes; the conversation-space
// mode follows the view; shared layers never rebind the mode.
test("chat and agent are top-level views with view→mode binding", () => {
  assert.equal(isWorkspaceView("chat"), true);
  assert.equal(getWorkspaceLayer("chat").id, "chat");
  assert.equal(getWorkspaceLayer("chat").label, "Chat");

  assert.equal(viewModeBinding("chat"), "chat");
  assert.equal(viewModeBinding("agent"), "agent");
  assert.equal(viewModeBinding("workspace"), null);
  assert.equal(viewModeBinding("system"), null);
  assert.equal(viewModeBinding("settings"), null);
  assert.equal(viewModeBinding("lab"), null);
});

// v1.6.0 repair (spec §11): every view owns an EXPLICIT hash — the old
// agent→empty special case (root ambiguous between Agent and the
// default landing page) is gone.
test("every view maps to an explicit hash", () => {
  for (const view of [
    "chat",
    "agent",
    "workspace",
    "lab",
    "system",
    "settings",
  ] as const) {
    assert.equal(workspaceHash(view), `#${view}`);
  }
});

// parseWorkspaceHash resolves every explicit route; the EMPTY hash (root)
// now resolves to Chat — the clean-install landing surface (spec §10/§11).
// The hash source is window.location — stubbed here for the Node runner.
test("hash routing: root→Chat, #chat→Chat, #agent→Agent, stale→Chat", () => {
  const originalWindow = (globalThis as Record<string, unknown>).window;

  for (const [hash, expected] of [
    ["#chat", "chat"],
    ["#agent", "agent"],
    ["#workspace", "workspace"],
    ["#lab", "lab"],
    ["#system", "system"],
    ["#settings", "settings"],
    ["#research", "chat"],
    ["#nope", "chat"],
    ["", "chat"],
  ] as const) {
    (globalThis as Record<string, unknown>).window = {
      location: { hash },
    };

    assert.equal(parseWorkspaceHash(), expected, `hash ${hash || "(empty)"}`);
  }

  (globalThis as Record<string, unknown>).window = originalWindow;
});

// resolveInitialView (spec §10): ONE deterministic resolution path —
// explicit URL hash > remembered workspace view > Chat default. The
// remembered view wins ONLY when the URL carries no explicit hash, and a
// stale explicit hash never falls through to remembered state.
test("initial view resolution: hash > remembered > Chat", () => {
  const originalWindow = (globalThis as Record<string, unknown>).window;

  const stub = (
    hash: string,
    remembered: string | null,
  ): { location: { hash: string }; localStorage: { getItem: (k: string) => string | null } } => ({
    location: { hash },
    localStorage: {
      getItem: (key: string) =>
        key === "sheytan.workspace.view" ? remembered : null,
    },
  });

  // Fresh install: no hash, nothing remembered → Chat.
  (globalThis as Record<string, unknown>).window = stub("", null);
  assert.equal(resolveInitialView(), "chat");

  // Remembered Agent view + no explicit hash → Agent (the restart
  // restores the last visited surface; the store binds agent mode).
  (globalThis as Record<string, unknown>).window = stub("", "agent");
  assert.equal(resolveInitialView(), "agent");

  // Remembered Chat view + no explicit hash → Chat.
  (globalThis as Record<string, unknown>).window = stub("", "chat");
  assert.equal(resolveInitialView(), "chat");

  // An EXPLICIT hash always wins over remembered state — including the
  // stale #research bookmark, which resolves to the Chat default.
  (globalThis as Record<string, unknown>).window = stub("#chat", "agent");
  assert.equal(resolveInitialView(), "chat");

  (globalThis as Record<string, unknown>).window = stub("#agent", "chat");
  assert.equal(resolveInitialView(), "agent");

  (globalThis as Record<string, unknown>).window = stub("#research", "agent");
  assert.equal(resolveInitialView(), "chat");

  (globalThis as Record<string, unknown>).window = originalWindow;
});
