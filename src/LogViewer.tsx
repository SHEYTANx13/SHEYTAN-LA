import { useEffect, useMemo, useRef, useState } from "react";

import { api, type LogEntry } from "./api";

// LogViewer — v1.1.7 in-app log surface over the EXISTING logging ring.
//
//   live tail        — polls /api/logs while mounted (2 s cadence)
//   pause            — stops polling (frozen view, resumes cleanly)
//   auto-scroll      — sticks to the newest line, user-scrollable
//   search           — substring match over time/level/category/message
//   severity filter  — All / Info / Warning / Error
//   subsystem filter — Engine / Agent / Network / Tools / Context /
//                      Research / System (+ Any)
//   copy             — the filtered view to the clipboard
//   clear view       — hides everything currently displayed WITHOUT
//                      touching the real log file; lines that arrive after
//                      the clear render again
//
// Memory is bounded by construction: each poll REPLACES the view with the
// server's bounded ring (≤ 512 lines), so the client never accumulates
// unbounded history. Secrets are redacted by the backend BEFORE they reach
// this component.

const POLL_MS = 2000;

type SeverityFilter = "All" | "Info" | "Warning" | "Error";
type SubsystemFilter =
  | "Any"
  | "Engine"
  | "Agent"
  | "Network"
  | "Tools"
  | "Context"
  | "Net Search"
  | "System";

const SEVERITIES: SeverityFilter[] = ["All", "Info", "Warning", "Error"];
const SUBSYSTEMS: SubsystemFilter[] = [
  "Any",
  "Engine",
  "Agent",
  "Network",
  "Tools",
  "Context",
  "Net Search",
  "System",
];

// severityMatches groups DEBUG under Info and maps the log levels onto the
// three filter buckets.
function severityMatches(level: string, filter: SeverityFilter): boolean {
  switch (filter) {
    case "All":
      return true;
    case "Info":
      return level === "INFO" || level === "DEBUG";
    case "Warning":
      return level === "WARN";
    case "Error":
      return level === "ERROR";
  }
}

// subsystemMatches maps the canonical log categories onto the viewer's
// subsystem buckets. Unknown categories stay visible under "Any" only.
function subsystemMatches(category: string, filter: SubsystemFilter): boolean {
  if (filter === "Any") return true;

  const c = category.toLowerCase();
  switch (filter) {
    case "Engine":
      return (
        c === "engine" || c === "llm" || c === "native"
      );
    case "Agent":
      return (
        c === "agent" ||
        c === "orchestrator" ||
        c === "multiagent" ||
        c === "pipeline" ||
        c === "lab" ||
        c === "tool"
      );
    case "Network":
      return (
        c === "network" ||
        c === "netcheck" ||
        c === "server" ||
        c === "api" ||
        c === "ws"
      );
    case "Tools":
      return (
        c === "tools" ||
        c === "browser" ||
        c === "shell" ||
        c === "git"
      );
    case "Context":
      return (
        c === "context" ||
        c === "contextplan" ||
        c === "continuum" ||
        c === "memory" ||
        c === "recall" ||
        c === "attachments"
      );
    case "Net Search":
      return (
        c === "research" ||
        c === "web" ||
        c === "github" ||
        c === "reddit" ||
        c === "searxng"
      );
    case "System":
      return (
        c === "system" ||
        c === "app" ||
        c === "config" ||
        c === "updater" ||
        c === "crash" ||
        c === "desktop"
      );
  }
}

// clearMarker remembers the last line shown before "Clear view" so lines
// that rotate through the ring can be recognised even as indices shift.
interface ClearMarker {
  raw: string;
}

function LogViewer() {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [autoscroll, setAutoscroll] = useState(true);
  const [query, setQuery] = useState("");
  const [severity, setSeverity] = useState<SeverityFilter>("All");
  const [subsystem, setSubsystem] = useState<SubsystemFilter>("Any");
  const [clearMarker, setClearMarker] = useState<ClearMarker | null>(null);

  const listRef = useRef<HTMLDivElement | null>(null);
  const clearMarkerRef = useRef<ClearMarker | null>(null);

  async function poll() {
    try {
      const res = await api.logs(512);

      let fresh = res.entries;

      const marker = clearMarkerRef.current;
      if (marker) {
        // Show only lines strictly AFTER the marker line. If the marker
        // has already rotated out of the ring, everything in the response
        // is newer than what was cleared.
        let markerAt = -1;
        for (let i = fresh.length - 1; i >= 0; i--) {
          const entry: LogEntry | undefined = fresh[i];
          if (entry && entry.raw === marker.raw) {
            markerAt = i;
            break;
          }
        }

        fresh = markerAt >= 0 ? fresh.slice(markerAt + 1) : fresh;

        if (markerAt < 0) {
          clearMarkerRef.current = null;
          setClearMarker(null);
        }
      }

      // Replace, never accumulate: the ring is the bounded source of truth.
      setEntries(fresh);
    } catch {
      // Backend briefly unavailable (engine restart) — keep the view.
    }
  }

  useEffect(() => {
    void poll();

    if (paused) {
      return;
    }

    const timer = window.setInterval(() => void poll(), POLL_MS);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paused]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();

    return entries.filter((e) => {
      if (!severityMatches(e.level, severity)) {
        return false;
      }
      if (!subsystemMatches(e.category, subsystem)) {
        return false;
      }
      if (
        q &&
        !e.raw.toLowerCase().includes(q) &&
        !e.message.toLowerCase().includes(q)
      ) {
        return false;
      }
      return true;
    });
  }, [entries, query, severity, subsystem]);

  // Auto-scroll sticks to the newest line unless the user scrolled up.
  useEffect(() => {
    if (!autoscroll || !listRef.current) {
      return;
    }
    listRef.current.scrollTop = listRef.current.scrollHeight;
  }, [visible, autoscroll]);

  async function copyView() {
    const text = visible.map((e) => e.raw).join("\n");
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Clipboard unavailable (permissions) — the view stays usable.
    }
  }

  function clearView() {
    const last = entries[entries.length - 1];
    const marker = last ? { raw: last.raw } : null;
    clearMarkerRef.current = marker;
    setClearMarker(marker);
    setEntries([]);
  }

  const levelClass = (level: string) =>
    level === "ERROR"
      ? "log-line error"
      : level === "WARN"
        ? "log-line warn"
        : level === "DEBUG"
          ? "log-line debug"
          : "log-line";

  return (
    <div className="logviewer">
      <div className="logviewer-controls">
        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={!paused}
            onChange={(event) => setPaused(!event.target.checked)}
          />
          <span>Live tail</span>
        </label>

        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={autoscroll}
            onChange={(event) => setAutoscroll(event.target.checked)}
          />
          <span>Auto-scroll</span>
        </label>

        <select
          className="logviewer-select"
          value={severity}
          onChange={(event) => setSeverity(event.target.value as SeverityFilter)}
          aria-label="Severity filter"
        >
          {SEVERITIES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>

        <select
          className="logviewer-select"
          value={subsystem}
          onChange={(event) =>
            setSubsystem(event.target.value as SubsystemFilter)
          }
          aria-label="Subsystem filter"
        >
          {SUBSYSTEMS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>

        <input
          className="logviewer-search"
          placeholder="Search logs…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />

        <button
          type="button"
          className="secondary-button"
          onClick={() => void copyView()}
        >
          Copy
        </button>

        <button type="button" className="secondary-button" onClick={clearView}>
          Clear view
        </button>
      </div>

      <div className="logviewer-list" ref={listRef}>
        {visible.length === 0 ? (
          <div className="logviewer-empty">
            {clearMarker
              ? "View cleared — lines arriving after the clear will appear here. The real log file is untouched."
              : paused
                ? "Paused — no entries match the current filters."
                : "Waiting for log lines…"}
          </div>
        ) : (
          visible.map((entry, i) => (
            <div key={`${entry.index}-${i}`} className={levelClass(entry.level)}>
              <span className="log-time">{entry.time}</span>
              <span className="log-level">{entry.level}</span>
              <span className="log-category">
                [{entry.category || "app"}]
              </span>
              <span className="log-message">{entry.message}</span>
            </div>
          ))
        )}
      </div>

      <div className="logviewer-footnote">
        {visible.length} of {entries.length} entries shown · redacted before
        display · the log file itself is never modified by this view
      </div>
    </div>
  );
}

export default LogViewer;
