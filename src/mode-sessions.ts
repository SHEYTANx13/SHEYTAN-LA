// mode-sessions.ts — v1.2.8 mode-separated history helpers (PURE module).
//
// Chat and Agent are two independent conversation spaces. The store keeps
// ONE active session PER MODE so switching modes never silently switches
// (or merges) the active conversation. These helpers are the deterministic
// core of that behavior, extracted so they are unit-testable without a
// store or browser API.

export type WorkspaceMode = "chat" | "agent";

export function normalizeMode(v: unknown): WorkspaceMode {
  return v === "chat" ? "chat" : "agent";
}

export type SessionModeInfo = {
  id: string;
  mode?: string;
};

/**
 * sessionMode resolves the conversation space of a session. Sessions that
 * predate the mode field are "agent" (the deterministic v1.2.8 migration
 * rule — matches the backend's sessions.DefaultMode).
 */
export function sessionMode(session: SessionModeInfo | null | undefined): WorkspaceMode {
  return session?.mode === "chat" ? "chat" : "agent";
}

/**
 * filterSessionsByMode keeps only the sessions of one space. mode "agent"
 * also catches legacy sessions without a mode field.
 */
export function filterSessionsByMode<T extends SessionModeInfo>(
  sessions: T[],
  mode: WorkspaceMode,
): T[] {
  return sessions.filter((s) => sessionMode(s) === mode);
}

/**
 * resolveActiveForMode returns the active session id FOR one mode: the
 * remembered per-mode selection when it still exists in that space,
 * otherwise the newest session of that space, otherwise null.
 */
export function resolveActiveForMode(
  sessions: SessionModeInfo[],
  mode: WorkspaceMode,
  remembered: string | null,
): string | null {
  const inMode = filterSessionsByMode(sessions, mode);

  if (remembered && inMode.some((s) => s.id === remembered)) {
    return remembered;
  }

  return inMode[0]?.id ?? null;
}

/**
 * resolveModeSwitchTarget (v1.2.8.1) returns the active session id for the
 * TARGET mode when switching: the PER-MODE remembered selection.
 *
 * v1.2.8 REGRESSION NOTE: setMode resolved this against the CURRENT
 * (still previous-mode) `sessions` array. Since v1.2.8 that array is
 * single-mode, so the target-mode subset was always empty and the switch
 * ALWAYS landed on null — no transcript load, no activity socket, no
 * context policy: the deterministic empty-conversation-after-switch bug.
 * The per-mode memory is the synchronous authority; refreshSessions()
 * re-validates it asynchronously (and loads the re-resolved conversation
 * when the remembered session no longer exists).
 */
export function resolveModeSwitchTarget(
  remembered: Record<WorkspaceMode, string | null>,
  mode: WorkspaceMode,
): string | null {
  return remembered[mode] ?? null;
}

/**
 * crossModePickerFilter (v1.2.8.1) returns the history picker's default
 * filter for a workspace mode: the OTHER space. Cross-mode references are
 * the picker's purpose, and the backend rejects same-mode references
 * outright (server-side enforcement, v1.2.8.1).
 */
export function crossModePickerFilter(mode: WorkspaceMode): WorkspaceMode {
  return mode === "chat" ? "agent" : "chat";
}

/**
 * rememberActiveForMode records the selection into the per-mode map
 * (immutable update).
 */
export function rememberActiveForMode(
  map: Record<WorkspaceMode, string | null>,
  mode: WorkspaceMode,
  id: string | null,
): Record<WorkspaceMode, string | null> {
  return { ...map, [mode]: id };
}

/**
 * sessionTitleLine renders the sidebar line: title (or fallback) plus the
 * id prefix — presentation-only, shared by the sidebar and the picker.
 */
export function sessionTitleLine(title: string | undefined, id: string): string {
  return title && title.trim() !== "" ? title : `Untitled ${id.slice(0, 8)}`;
}
