export type WorkspaceView =
  | "chat"
  | "agent"
  | "workspace"
  | "lab"
  | "automation"
  | "system"
  | "settings";

export type WorkspaceMode = "chat" | "agent";

export type WorkspaceLayer = {
  id: WorkspaceView;
  label: string;
  eyebrow: string;
  title: string;
  description: string;
  icon: string;
  // v1.1.9: which top-level modes surface this layer in the navigation.
  // Coding Lab is Agent machinery — Chat stays a calm conversation
  // surface and does not offer it. Everything else is available in both
  // modes; the underlying runtime is shared either way.
  modes: readonly WorkspaceMode[];
};

const AGENT_LAYER: WorkspaceLayer = {
  // v1.2.4: the agent surface is labelled honestly ("Agent") — the
  // Workspace word now belongs to the dedicated work-environment layer.
  id: "agent",
  label: "Agent",
  eyebrow: "AGENT",
  title: "SHEYTAN",
  description: "Interactive local intelligence",
  icon: "◈",
  modes: ["chat", "agent"],
};

// v1.6.0 (spec §6): CHAT is a real TOP-LEVEL view — not a segmented
// selector inside the Agent workspace. Chat and Agent are two visible
// surfaces over the SAME runtime (engine, memory, tools, sessions
// infrastructure); only the presentation differs (Chat stays a calm,
// lightweight conversation surface; Agent carries the engineering
// machinery).
const CHAT_LAYER: WorkspaceLayer = {
  id: "chat",
  label: "Chat",
  eyebrow: "CHAT",
  title: "Chat",
  description: "Lightweight conversation with the local model",
  icon: "◇",
  modes: ["chat", "agent"],
};

export const WORKSPACE_LAYERS: readonly WorkspaceLayer[] = [
  // v1.6.0 navigation contract (spec §6): CHAT | AGENT | WORKSPACE/LAB |
  // SYSTEM | SETTINGS. The internal Chat|Agent segmented selector is
  // GONE — the views themselves are the switch.
  CHAT_LAYER,
  AGENT_LAYER,
  {
    // v1.2.4: the Workspace work-environment layer — current project,
    // recent files, active session, model/runtime state, project health
    // and the quick actions, in one view.
    id: "workspace",
    label: "Workspace",
    eyebrow: "WORKSPACE",
    title: "Work Environment",
    description: "Project, sessions, runtime state, quick actions",
    icon: "⌂",
    modes: ["chat", "agent"],
  },
  {
    id: "lab",
    label: "Coding Lab",
    eyebrow: "CODING LAB",
    title: "Autonomous engineering",
    description: "Execute, verify, and repair",
    icon: "◆",
    modes: ["agent"],
  },
  {
    // v1.7.0: the Automation layer — scheduled/event tasks, the
    // chronological run timeline, task-scoped tools and artifacts.
    // Agent machinery (tasks execute agent runs), so Chat stays a calm
    // conversation surface without it (same rule as Coding Lab).
    id: "automation",
    label: "Automation",
    eyebrow: "AUTOMATION",
    title: "Automation & Tasks",
    description: "Schedules, runs, tools, artifacts",
    icon: "⏱",
    modes: ["agent"],
  },
  // v1.3.6 (spec §21/§22): the Research workspace LAYER is removed —
  // Net Search lives where it is used, as a per-request control in both
  // Chat and Agent composers (ComposerControls), backed by the same
  // research service. No dead route remains; an old #research hash
  // resolves to Agent via parseWorkspaceHash's invalid-view fallback.
  {
    id: "system",
    label: "System",
    eyebrow: "SYSTEM CENTRE",
    title: "Environment Centre",
    description: "Device, runtime, health, and recommendations",
    icon: "▤",
    modes: ["chat", "agent"],
  },
  {
    id: "settings",
    label: "Settings",
    eyebrow: "SETTINGS",
    title: "Settings",
    description: "Models, engine, agent, and integrations",
    icon: "⚙",
    modes: ["chat", "agent"],
  },
] as const;

// v1.1.9: the navigation is mode-aware — Chat hides Agent machinery
// (Coding Lab) without touching the layers themselves. v1.6.0: with Chat
// and Agent as top-level views the mode follows the active view (the
// App binds chat-view → chat mode, agent-view → agent mode), so the
// layer visibility rule still holds for every other surface.
export function visibleWorkspaceLayers(
  mode: WorkspaceMode,
): readonly WorkspaceLayer[] {
  return WORKSPACE_LAYERS.filter((layer) => layer.modes.includes(mode));
}

export function isWorkspaceView(value: string): value is WorkspaceView {
  return WORKSPACE_LAYERS.some((layer) => layer.id === value);
}

export function getWorkspaceLayer(view: WorkspaceView): WorkspaceLayer {
  return WORKSPACE_LAYERS.find((layer) => layer.id === view) ?? AGENT_LAYER;
}

export function parseWorkspaceHash(): WorkspaceView {
  // v1.6.0 repair (spec §11): the default landing surface is CHAT — the
  // old empty-hash → Agent special case made root ambiguous and left
  // the store's chat mode fighting the agent view at boot. Unknown or
  // stale hashes (e.g. the removed #research) resolve to the same
  // default through the invalid-view fallback.
  if (typeof window === "undefined") {
    return "chat";
  }

  const hash = window.location.hash.replace(/^#/, "").trim().toLowerCase();

  return isWorkspaceView(hash) ? hash : "chat";
}

// resolveInitialView (v1.6.0 repair, spec §10): the ONE deterministic
// initial-view resolution path — no asynchronous effect may move the
// view after first paint:
//
//     explicit URL hash  >  remembered workspace view  >  Chat default
//
// The App's first render AND the store's boot conversation-space mode
// both resolve through this function, so the view and the mode can
// never disagree at boot (fresh install → Chat view + chat mode).
// A stale EXPLICIT hash (e.g. the removed #research) resolves to the
// Chat default — never to remembered state.
export function resolveInitialView(): WorkspaceView {
  if (typeof window === "undefined") {
    return "chat";
  }

  const hash = window.location.hash.replace(/^#/, "").trim().toLowerCase();

  if (hash !== "") {
    return isWorkspaceView(hash) ? hash : "chat";
  }

  return restoreView() ?? "chat";
}

// viewModeBinding (v1.6.0, spec §6): the conversation-space mode that a
// top-level view implies. The Chat VIEW is the chat conversation space;
// the Agent VIEW is the agent conversation space; every other layer
// leaves the current space untouched (workspace/system/settings are
// shared surfaces over the same runtime).
export function viewModeBinding(
  view: WorkspaceView,
): WorkspaceMode | null {
  if (view === "chat") {
    return "chat";
  }

  if (view === "agent") {
    return "agent";
  }

  return null;
}

// v1.2.4: persisted view state — the last visited layer is remembered and
// restored intelligently on restart (an invalid stored value falls back to
// the hash/default instead of breaking the app).
const VIEW_STORAGE_KEY = "sheytan.workspace.view";

export function rememberView(view: WorkspaceView): void {
  try {
    window.localStorage.setItem(VIEW_STORAGE_KEY, view);
  } catch {
    // storage unavailable (private mode) — view memory is best-effort
  }
}

export function restoreView(): WorkspaceView | null {
  try {
    const stored = window.localStorage.getItem(VIEW_STORAGE_KEY);
    return stored && isWorkspaceView(stored) ? stored : null;
  } catch {
    return null;
  }
}

export function workspaceHash(view: WorkspaceView): string {
  // v1.6.0 repair (spec §11): EVERY view has an explicit hash. The old
  // agent → empty-hash special case made root ambiguous between "Agent"
  // and "default landing page". Root now resolves through
  // resolveInitialView (hash > remembered > Chat) and is normalized to
  // the resolved view's explicit hash at boot.
  return `#${view}`;
}

export function getWorkspaceHref(view: WorkspaceView): string {
  const hash = workspaceHash(view);

  return `${window.location.pathname}${window.location.search}${hash}`;
}
