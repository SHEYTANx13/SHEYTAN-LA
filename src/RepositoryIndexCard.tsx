// RepositoryIndexCard.tsx — v1.3.4 (ROADMAP v1.4 slice 1): the Workspace
// panel's repository-intelligence card.
//
// Shows the persistent repository index (files, languages, symbols,
// dependency edges, state, last update) and provides the repository
// search entry point over POST /api/repo/search. Lightweight by
// contract: the status endpoint never walks the tree; search and
// refresh are bounded server-side.

import { useCallback, useEffect, useState } from "react";

import {
  api,
  type RepoIndexStatus,
  type RepoSearchReport,
} from "./api";
import {
  formatScore,
  indexStateLabel,
  indexStateTone,
  resultSummary,
  topLanguages,
} from "./repoindex-view";

function formatWhen(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  return t.toLocaleString();
}

export function RepositoryIndexCard() {
  const [status, setStatus] = useState<RepoIndexStatus | null>(null);
  const [report, setReport] = useState<RepoSearchReport | null>(null);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refreshStatus = useCallback(async () => {
    try {
      setStatus(await api.repoIndex());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Index status failed.");
    }
  }, []);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  // A first view of a not-yet-built index triggers ONE bounded refresh
  // so the card is useful immediately — without polling.
  useEffect(() => {
    if (status?.state !== "empty") return;
    let cancelled = false;
    void (async () => {
      try {
        const result = await api.repoIndexRefresh();
        if (!cancelled) setStatus(result.status);
      } catch {
        // Status stays honest ("Not built yet"); the Refresh button
        // remains the manual path.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [status?.state]);

  async function onRefresh() {
    setBusy(true);
    setError(null);
    try {
      const result = await api.repoIndexRefresh();
      setStatus(result.status);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Refresh failed.");
    } finally {
      setBusy(false);
    }
  }

  async function onSearch(e: React.FormEvent) {
    e.preventDefault();
    const q = query.trim();
    if (!q) return;
    setSearching(true);
    setError(null);
    try {
      // Free text rides the task dimension (keyword relevance over the
      // indexed metadata); the backend is the authority for scoring.
      setReport(await api.repoSearch({ task: q, text: q, limit: 12 }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Search failed.");
    } finally {
      setSearching(false);
    }
  }

  const tone = indexStateTone(status);
  const languages = topLanguages(status?.languages);

  return (
    <div className="wb-card">
      <div className="wb-card-title">Repository index</div>

      <div className="wb-summary-line">
        <span className={`settings-chip chip-${tone}`}>
          {indexStateLabel(status)}
        </span>
        {status?.files ? (
          <span className="wb-chip">
            <span className="wb-chip-label">Files</span>
            <span className="wb-chip-value">{status.files}</span>
          </span>
        ) : null}
        {status?.symbols ? (
          <span className="wb-chip">
            <span className="wb-chip-label">Symbols</span>
            <span className="wb-chip-value">{status.symbols}</span>
          </span>
        ) : null}
        {status?.depEdges ? (
          <span className="wb-chip">
            <span className="wb-chip-label">Deps</span>
            <span className="wb-chip-value">{status.depEdges}</span>
          </span>
        ) : null}
        {status?.testLinks ? (
          <span className="wb-chip">
            <span className="wb-chip-label">Tests</span>
            <span className="wb-chip-value">{status.testLinks}</span>
          </span>
        ) : null}
        {status?.gitAvailable ? (
          <span className="wb-chip">
            <span className="wb-chip-label">Git</span>
            <span className="wb-chip-value">signals on</span>
          </span>
        ) : null}
      </div>

      {languages.length > 0 ? (
        <p className="wb-muted repo-langs">{languages.join(" · ")}</p>
      ) : null}

      {status?.updatedAt ? (
        <p className="wb-muted">Last update: {formatWhen(status.updatedAt)}</p>
      ) : (
        <p className="wb-muted">
          No index yet for this folder. Refresh builds a bounded index of
          files, symbols and dependency edges.
        </p>
      )}

      <form
        className="wb-clone-form repo-search-form"
        onSubmit={(e) => void onSearch(e)}
      >
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search the repository — symbol, file, or task…"
          aria-label="Repository search"
        />
        <div className="wb-clone-actions">
          <button
            className="btn btn-sm"
            type="submit"
            disabled={searching || query.trim() === ""}
          >
            {searching ? "Searching…" : "Search"}
          </button>
          <button
            className="btn btn-sm"
            type="button"
            onClick={() => void onRefresh()}
            disabled={busy}
          >
            {busy ? "Refreshing…" : "Refresh index"}
          </button>
        </div>
      </form>

      {error ? (
        <div className="error-banner" role="alert">
          <span>{error}</span>
        </div>
      ) : null}

      {report ? (
        report.results.length > 0 ? (
          <>
            <ul className="wb-files repo-results">
              {report.results.map((res) => (
                <li key={res.path} title={res.path}>
                  <span className="wb-file-name">{res.path}</span>
                  <span className="wb-file-meta">
                    {resultSummary(res)} · score {formatScore(res.score)}
                  </span>
                  {res.evidence ? (
                    <span className="repo-evidence">{res.evidence}</span>
                  ) : null}
                </li>
              ))}
            </ul>
            {report.truncated ? (
              <p className="wb-muted">
                Showing top {report.returned} of {report.totalHits} hits —
                narrow the query.
              </p>
            ) : null}
          </>
        ) : (
          <p className="wb-muted">No matches in the index for that query.</p>
        )
      ) : null}
    </div>
  );
}

export default RepositoryIndexCard;
