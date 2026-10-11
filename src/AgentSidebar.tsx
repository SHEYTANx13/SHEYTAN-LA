import { memo, useMemo, useRef, useState } from "react";

import type { Session } from "./api";
import { useRuntimeStore } from "./store";
import { sessionTitleLine } from "./mode-sessions";

const SessionItem = memo(function SessionItem({
  session,
  active,
  onSelect,
  onRename,
  onDelete,
}: {
  session: Session;
  active: boolean;
  onSelect: (id: string) => void;
  onRename: (id: string, title: string) => void;
  onDelete: (id: string) => void;
}) {
  // v1.2.8: inline rename — the backend PUT existed since 1.1.3; the UI
  // finally uses it. Double-click the title (or its menu entry) to edit.
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title || "");
  const inputRef = useRef<HTMLInputElement | null>(null);

  const startEdit = () => {
    setDraft(session.title || "");
    setEditing(true);
    window.requestAnimationFrame(() => inputRef.current?.select());
  };

  const commit = () => {
    setEditing(false);

    const clean = draft.trim();

    if (clean && clean !== session.title) {
      onRename(session.id, clean);
    }
  };

  return (
    // v1.8.3: data-session-id gives every sidebar row an UNAMBIGUOUS
    // identity (the rendered id text is an 8-char timestamp slice that
    // collides for sessions created within the same bucket — useless for
    // assertions). No behavior change; a stable test hook.
    <div className={`session-item-wrap ${active ? "active" : ""}`} data-session-id={session.id}>
      {editing ? (
        <input
          ref={inputRef}
          className="session-rename-input"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              commit();
            } else if (event.key === "Escape") {
              setEditing(false);
            }
          }}
          aria-label="Rename session"
        />
      ) : (
        <button
          type="button"
          className="session-item"
          onClick={() => onSelect(session.id)}
          onDoubleClick={startEdit}
          title="Open (double-click to rename)"
        >
          <span className="session-icon">◈</span>

          <span className="session-copy">
            <strong>{sessionTitleLine(session.title, session.id)}</strong>

            <span>{session.id.slice(0, 8)}</span>
          </span>
        </button>
      )}

      <span className="session-item-actions">
        {!editing ? (
          <button
            type="button"
            className="session-action"
            onClick={startEdit}
            aria-label={`Rename ${sessionTitleLine(session.title, session.id)}`}
            title="Rename"
          >
            ✎
          </button>
        ) : null}

        <button
          type="button"
          className="session-action danger"
          onClick={() => onDelete(session.id)}
          aria-label={`Delete ${sessionTitleLine(session.title, session.id)}`}
          title="Delete session"
        >
          ×
        </button>
      </span>
    </div>
  );
});

const AgentSidebar = memo(function AgentSidebar() {
  const sessions = useRuntimeStore((state) => state.sessions);

  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);

  const loading = useRuntimeStore((state) => state.loading);

  const selectSession = useRuntimeStore((state) => state.selectSession);

  const renameSession = useRuntimeStore((state) => state.renameSession);

  const deleteSession = useRuntimeStore((state) => state.deleteSession);

  // v1.1.9: the sidebar speaks in the voice of the active mode —
  // v1.2.8: the list IS the mode's conversation space (mode-filtered by
  // the store), not a shared transcript with different framing.
  const mode = useRuntimeStore((state) => state.mode);

  // v1.2.8: history search — a client-side filter over the loaded (already
  // mode-scoped) list. Cross-space search lives in the history picker.
  const [filter, setFilter] = useState("");

  const visibleSessions = useMemo(() => {
    const needle = filter.trim().toLowerCase();

    if (needle === "") {
      return sessions;
    }

    return sessions.filter(
      (session) =>
        (session.title || "").toLowerCase().includes(needle) ||
        session.id.toLowerCase().includes(needle),
    );
  }, [sessions, filter]);

  function requestNewSession() {
    window.dispatchEvent(new CustomEvent("sheytan:new-session"));
  }

  return (
    <>
      <div className="sidebar-heading sidebar-heading-secondary">
        <div>
          <span className="eyebrow">{mode === "chat" ? "CHAT" : "AGENT"}</span>

          <strong>Sessions</strong>
        </div>

        <button
          type="button"
          className="icon-button"
          onClick={requestNewSession}
          title="New session"
          aria-label="New session"
        >
          +
        </button>
      </div>

      <div className="sidebar-search">
        <input
          type="search"
          value={filter}
          placeholder="Search this space…"
          onChange={(event) => setFilter(event.target.value)}
          aria-label="Search sessions"
        />
      </div>

      <div className="session-list">
        {visibleSessions.length === 0 && !loading ? (
          <div className="empty-sidebar">
            {filter.trim() === ""
              ? "No sessions yet."
              : "No sessions match this search."}
          </div>
        ) : (
          visibleSessions.map((session) => (
            <SessionItem
              key={session.id}
              session={session}
              active={session.id === activeSessionId}
              onSelect={selectSession}
              onRename={(id, title) => void renameSession(id, title)}
              onDelete={(id) => void deleteSession(id)}
            />
          ))
        )}
      </div>
    </>
  );
});

export default AgentSidebar;
