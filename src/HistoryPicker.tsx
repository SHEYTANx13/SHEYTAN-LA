// HistoryPicker.tsx — v1.2.8 cross-mode history picker.
//
// A searchable, mode-filterable dialog that attaches OTHER sessions as
// HISTORY REFERENCES to the active conversation:
//
//   - search covers titles + rolling summaries (server-side, bounded);
//   - the mode filter (All / Chat / Agent) is how one space browses the
//     OTHER space's history;
//   - each hit shows title, mode badge, date, message count and the
//     concise summary preview (objective / current state);
//   - multi-select with an explicit "Attach" action;
//   - the active session is excluded (its transcript is already here).
//
// Attached references are DATA: each run retrieves only the relevant
// portions, the source conversations are never merged or modified, and
// every retrieved block carries provenance labels.

import { memo, useEffect, useRef, useState } from "react";

import type { HistoryHit } from "./api";
import { useRuntimeStore } from "./store";

function formatDate(iso: string): string {
  if (!iso) {
    return "";
  }

  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return "";
  }

  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

const HitRow = memo(function HitRow({
  hit,
  selected,
  onToggle,
}: {
  hit: HistoryHit;
  selected: boolean;
  onToggle: (id: string) => void;
}) {
  const modeBadge = hit.mode === "chat" ? "CHAT" : "AGENT";

  const preview = hit.objective || hit.currentState || hit.snippet || "";

  return (
    <button
      type="button"
      className={`history-hit ${selected ? "selected" : ""}`}
      onClick={() => onToggle(hit.sessionId)}
      aria-pressed={selected}
    >
      <span className="history-hit-select" aria-hidden="true">
        {selected ? "✓" : ""}
      </span>

      <span className="history-hit-body">
        <span className="history-hit-top">
          <strong className="history-hit-title">
            {hit.title || "Untitled session"}
          </strong>

          <span className={`history-hit-mode mode-${hit.mode}`}>
            {modeBadge}
          </span>

          <span className="history-hit-date">{formatDate(hit.updatedAt)}</span>

          {hit.msgCount > 0 ? (
            <span className="history-hit-count">
              {hit.msgCount} msg{hit.msgCount === 1 ? "" : "s"}
            </span>
          ) : null}
        </span>

        {preview ? (
          <span className="history-hit-preview">{preview}</span>
        ) : null}
      </span>
    </button>
  );
});

function HistoryPicker() {
  const open = useRuntimeStore((state) => state.historyPickerOpen);
  const setOpen = useRuntimeStore((state) => state.setHistoryPickerOpen);
  const hits = useRuntimeStore((state) => state.historyHits);
  const loading = useRuntimeStore((state) => state.historySearchLoading);
  const modeFilter = useRuntimeStore((state) => state.historySearchMode);
  const setModeFilter = useRuntimeStore((state) => state.setHistorySearchMode);
  const search = useRuntimeStore((state) => state.searchHistory);
  const attach = useRuntimeStore((state) => state.attachHistoryRefs);
  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);

  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string[]>([]);

  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (open) {
      setSelected([]);
      setQuery("");
      // Focus the search box once the dialog mounts.
      window.requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  if (!open) {
    return null;
  }

  const toggle = (id: string) => {
    setSelected((current) =>
      current.includes(id)
        ? current.filter((item) => item !== id)
        : [...current, id],
    );
  };

  const attachSelected = () => {
    if (selected.length === 0 || !activeSessionId) {
      return;
    }

    void attach(
      selected.map((sessionId) => {
        const hit = hits.find((item) => item.sessionId === sessionId);

        return {
          sessionId,
          mode: hit?.mode,
          summaryVersion: hit?.summaryVersion,
        };
      }),
    );
  };

  return (
    <div
      className="history-picker-backdrop"
      onClick={() => setOpen(false)}
      role="presentation"
    >
      <div
        className="history-picker"
        role="dialog"
        aria-modal="true"
        aria-label="Attach history"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="history-picker-heading">
          <div>
            <span className="eyebrow">HISTORY</span>
            <strong>Attach another conversation</strong>
          </div>

          <button
            type="button"
            className="icon-button"
            onClick={() => setOpen(false)}
            aria-label="Close"
          >
            ×
          </button>
        </div>

        <div className="history-picker-search">
          <input
            ref={inputRef}
            type="search"
            value={query}
            placeholder="Search titles and summaries…"
            onChange={(event) => {
              setQuery(event.target.value);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                void search(query);
              }
            }}
          />

          <button
            type="button"
            className="secondary-button"
            onClick={() => void search(query)}
            disabled={loading}
          >
            {loading ? "Searching…" : "Search"}
          </button>
        </div>

        <div className="history-picker-filters" role="group">
          {(
            [
              ["all", "All"],
              ["chat", "Chat"],
              ["agent", "Agent"],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              className={`filter-chip ${modeFilter === value ? "active" : ""}`}
              onClick={() => setModeFilter(value)}
            >
              {label}
            </button>
          ))}
        </div>

        <div className="history-picker-results">
          {hits.length === 0 && !loading ? (
            <div className="history-hit-empty">
              No sessions match this search yet.
            </div>
          ) : (
            hits
              .filter((hit) => hit.sessionId !== activeSessionId)
              .map((hit) => (
                <HitRow
                  key={hit.sessionId}
                  hit={hit}
                  selected={selected.includes(hit.sessionId)}
                  onToggle={toggle}
                />
              ))
          )}
        </div>

        <div className="history-picker-footer">
          <span className="history-picker-hint">
            Attached sessions are references — only the relevant turns are
            retrieved per request, and the source conversations are never
            modified.
          </span>

          <button
            type="button"
            className="primary-button"
            onClick={attachSelected}
            disabled={selected.length === 0}
          >
            {selected.length > 0
              ? `Attach ${selected.length} conversation${selected.length === 1 ? "" : "s"}`
              : "Attach"}
          </button>
        </div>
      </div>
    </div>
  );
}

export default HistoryPicker;
