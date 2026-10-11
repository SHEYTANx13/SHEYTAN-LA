// v1.2.4: global keyboard shortcuts + the "?" help overlay.
//
// The app previously had exactly two shortcuts (Enter-to-send and
// Ctrl+Shift+P for the perf HUD). This module adds a small, discoverable
// set — every layer reachable from the keyboard, new session, and a help
// overlay — without touching the composer's own key handling.
//
// Design rules: no heavy animation (the overlay is a simple card), no
// re-render storms (a single window listener registered once), and Escape
// always closes the overlay.

import type { WorkspaceView } from "./workspace";

export type ShortcutSpec = {
  id: string;
  keys: string;
  description: string;
};

// Layer order mirrors the sidebar navigation.
// v1.3.6: the Research layer is removed — Net Search lives in the
// composers. Digit shortcuts renumber accordingly (Ctrl+4 = System).
const VIEW_ORDER: readonly WorkspaceView[] = [
  "agent",
  "workspace",
  "lab",
  "system",
  "settings",
];

export const SHORTCUTS: readonly ShortcutSpec[] = [
  { id: "views", keys: "Ctrl + 1…5", description: "Switch workspace layers" },
  { id: "new-session", keys: "Ctrl + N", description: "New session" },
  { id: "focus-composer", keys: "/", description: "Focus the message composer" },
  { id: "perf-hud", keys: "Ctrl + Shift + P", description: "Toggle performance HUD" },
  { id: "help", keys: "?", description: "Show this help" },
  { id: "close", keys: "Esc", description: "Close dialogs" },
];

export function viewForDigit(digit: number): WorkspaceView | null {
  if (digit < 1 || digit > VIEW_ORDER.length) return null;
  return VIEW_ORDER[digit - 1] ?? null;
}

export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    target.isContentEditable
  );
}

export type ShortcutHandlers = {
  onView: (view: WorkspaceView) => void;
  onNewSession: () => void;
  onFocusComposer: () => void;
  onToggleHelp: () => void;
};

// installShortcuts registers the global listener and returns its teardown.
// The "?" help toggle and layer switching are intentionally inert while
// the user types into inputs — "/" focuses the composer only from a
// non-typing context for the same reason.
export function installShortcuts(handlers: ShortcutHandlers): () => void {
  function onKeyDown(event: KeyboardEvent) {
    if (event.defaultPrevented) return;

    const mod = event.ctrlKey || event.metaKey;

    // Ctrl/Cmd + digit → layer switch (works while typing too — it never
    // collides with text entry).
    if (mod && !event.shiftKey && !event.altKey) {
      if (event.key >= "1" && event.key <= "9") {
        const view = viewForDigit(Number(event.key));
        if (view) {
          event.preventDefault();
          handlers.onView(view);
        }
        return;
      }
      if (event.key.toLowerCase() === "n") {
        event.preventDefault();
        handlers.onNewSession();
        return;
      }
      // Ctrl+Shift+P (perf HUD) is owned by perf-hud.ts — untouched here.
      return;
    }

    if (isTypingTarget(event.target)) {
      return; // plain keys never steal from text entry
    }

    if (event.key === "/") {
      event.preventDefault();
      handlers.onFocusComposer();
      return;
    }

    if (event.key === "?") {
      event.preventDefault();
      handlers.onToggleHelp();
    }
  }

  window.addEventListener("keydown", onKeyDown);
  return () => window.removeEventListener("keydown", onKeyDown);
}
