import { memo } from "react";

import { useRuntimeStore } from "./store";

// formatTok renders the compact token figure the header shows:
// 15.2K / 16K.
function formatTok(tokens: number): string {
  if (tokens >= 1024) {
    const k = tokens / 1024;
    return `${k >= 10 ? Math.round(k) : Math.round(k * 10) / 10}K`;
  }
  return String(tokens);
}

// v1.6.0 (spec §7): the context-window UX concept is AUTOMATIC long
// context. The physical window stays engine/model-bounded (contextplan,
// continuum, chunking, summaries, memory, histref do the continuity),
// but the user no longer manages it: there is NO editable context-size
// choice anywhere. This read-only indicator states exactly that.
function LongContextPill() {
  const sessionContext = useRuntimeStore((state) => state.sessionContext);

  const effective = sessionContext?.effective;

  return (
    <span
      className="ctx-pill"
      title={
        effective
          ? `Automatic long context — the runtime manages the window (currently serving ${formatTok(effective)} tokens physically). Conversation continuity is unbounded through history, summaries, retrieval and memory.`
          : "Automatic long context — the runtime manages the physical window; conversation continuity is unbounded through history, summaries, retrieval and memory."
      }
    >
      <span className="ctx-pill-label">LONG CONTEXT</span>
      <span className="ctx-usage-value">AUTOMATIC</span>
    </span>
  );
}

const AgentHeader = memo(function AgentHeader() {
  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);

  const sessions = useRuntimeStore((state) => state.sessions);

  const running = useRuntimeStore((state) => state.running);

  const mode = useRuntimeStore((state) => state.mode);

  const sessionContext = useRuntimeStore((state) => state.sessionContext);
  const sessionContextError = useRuntimeStore((state) => state.sessionContextError);

  const engine = useRuntimeStore((state) => state.engine);

  const activeSession =
    sessions.find((session) => session.id === activeSessionId) ?? null;

  // v1.1.8: Chat shows plain conversation wording; Agent keeps the
  // engineering voice. Same session, different framing.
  const defaultTitle = mode === "chat" ? "New chat" : "Forge a new task";

  const usage = sessionContext
    ? `${formatTok(sessionContext.used)} / ${formatTok(sessionContext.effective)}`
    : null;
  const pressurePct = sessionContext
    ? Math.round(sessionContext.pressure * 100)
    : null;

  return (
    <>
      <h1>{activeSession?.title || defaultTitle}</h1>

      <div className="header-actions">
        {/* v1.6.0 (spec §6): the Chat|Agent segmented selector is REMOVED
            — Chat and Agent are top-level views in the navigation. */}

        {/* v1.6.0 (spec §7): read-only automatic long-context indicator.
            The per-session context-size SELECT is gone; the backend keeps
            resolving the physical window automatically. */}
        <LongContextPill />

        {usage && (
          <span
            className={`ctx-pill ctx-usage${
              pressurePct !== null && pressurePct >= 90 ? " ctx-critical" : ""
            }`}
            title={
              sessionContext?.resourceReason ??
              `Effective ${sessionContext?.effective} tok · input budget ${sessionContext?.usableInput} · output reserve ${sessionContext?.outputReserve}`
            }
          >
            <span className="ctx-pill-label">USED</span>
            <span className="ctx-usage-value">
              {usage}
              {pressurePct !== null ? ` · ${pressurePct}%` : ""}
            </span>
          </span>
        )}

        {sessionContextError && (
          <span className="ctx-pill ctx-usage ctx-critical" title={sessionContextError}>
            <span className="ctx-pill-label">CONTEXT</span>
            <span className="ctx-usage-value">unavailable</span>
          </span>
        )}

        {/* v1.1.6 §11: the status pill reflects the backend phase and the
            verified-readiness proof, not a local guess. Kept in BOTH
            modes — one glance answers "is the engine ready?". */}
        <span
          className={`runtime-pill${
            engine?.degraded ? " runtime-pill-degraded" : ""
          }`}
          title={
            engine?.degraded
              ? "Engine reports ready but the serving model could not be verified — degraded startup"
              : engine?.verifiedModel
                ? `Verified: ${engine.verifiedModel}`
                : undefined
          }
        >
          {running
            ? "RUNNING"
            : engine?.phase === "ready"
              ? "READY"
              : engine?.download && engine.download.bytesTotal > 0
                ? `DOWNLOADING ${Math.round(
                    (engine.download.bytesDone / engine.download.bytesTotal) * 100,
                  )}%`
                : (engine?.phase ?? "READY").toUpperCase().replace(/-/g, " ")}
        </span>
      </div>
    </>
  );
});

export default AgentHeader;
