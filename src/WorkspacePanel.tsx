import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  api,
  type CloneStatus,
  type WorkspaceSummary as WorkspaceSummaryData,
} from "./api";
import { parseGitHubUrl } from "./clone-url";
import RepositoryIndexCard from "./RepositoryIndexCard";
import { useRuntimeStore } from "./store";

// v1.2.4: the Workspace panel — the whole work environment in one glance:
// Project → files → active task → model → runtime → current state, plus
// the quick actions. Technical controls (change root, terminal) stay
// secondary; the state summary and the common actions lead.
//
// v1.3.0 adds the first-class CLONE GITHUB REPOSITORY workflow:
//
//   Enter repository URL → choose destination → validate → clone →
//   verify → open as active workspace (automatic switch)
//
// The flow mirrors the app-update "Download Manager" contract: POST
// starts the job server-side (structured git execution, tree-kill
// cancellation, bounded output), the UI polls status at 1.2 s and can
// cancel at any time. On success the workspace switches automatically
// — no manual path editing for the common workflow.

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatWhen(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  return t.toLocaleString();
}

function WorkspaceLoading() {
  return (
    <div className="workspace-panel">
      <div className="wb-summary-card wb-card">
        <div className="wb-summary-line">
          <span className="wb-chip skeleton">loading…</span>
          <span className="wb-chip skeleton">loading…</span>
        </div>
      </div>
    </div>
  );
}

// cloneErrorMessage renders the classified, actionable error — never
// raw shell noise as the only explanation (the git output tail rides
// along under "details").
function cloneErrorMessage(status: CloneStatus): string {
  if (status.errorMessage) return status.errorMessage;
  switch (status.errorKind) {
    case "git-unavailable":
      return "Git is not installed on this machine. Install it from https://git-scm.com/download and try again.";
    case "invalid-url":
      return "That does not look like a GitHub repository URL.";
    case "destination-exists":
      return "The destination folder already exists and is not empty — choose a different location.";
    default:
      return "The clone failed. See the output below for git's own diagnostics.";
  }
}

function CloneCard({
  onSwitched,
}: {
  onSwitched: (summary: WorkspaceSummaryData) => void;
}) {
  const [url, setUrl] = useState("");
  const [destination, setDestination] = useState("");
  const [branch, setBranch] = useState("");
  const [showOptions, setShowOptions] = useState(false);
  const [status, setStatus] = useState<CloneStatus | null>(null);
  const [starting, setStarting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const pollRef = useRef<number | null>(null);

  const running = status?.state === "running";

  const parsed = useMemo(() => parseGitHubUrl(url), [url]);

  // Stop polling on unmount.
  useEffect(() => {
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
  }, []);

  // Poll while the clone runs (1.2 s — same cadence as the update flow).
  useEffect(() => {
    if (!running) {
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }

    pollRef.current = window.setInterval(() => {
      void api
        .cloneStatus()
        .then((next) => {
          setStatus(next);
          if (next.state !== "running" && next.summary && next.switched) {
            onSwitched(next.summary);
          }
        })
        .catch(() => {
          // transient — keep the last honest view
        });
    }, 1200);

    return () => {
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [running, onSwitched]);

  async function startClone() {
    setFormError(null);

    const trimmed = url.trim();
    if (!trimmed) {
      setFormError("Enter a GitHub repository URL first.");
      return;
    }

    // Instant client-side feedback; the backend remains the authority.
    if (!parseGitHubUrl(trimmed)) {
      setFormError(
        "That does not look like a GitHub URL — use https://github.com/owner/repository or git@github.com:owner/repository.git",
      );
      return;
    }

    setStarting(true);
    try {
      const initial = await api.cloneStart({
        url: trimmed,
        destination: destination.trim() || undefined,
        branch: branch.trim() || undefined,
      });
      setStatus(initial);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "The clone could not be started.";
      setFormError(message);
    } finally {
      setStarting(false);
    }
  }

  async function cancelClone() {
    try {
      await api.cloneCancel();
    } catch {
      // status polling reflects the outcome either way
    }
  }

  function reset() {
    setStatus(null);
    setUrl("");
    setDestination("");
    setBranch("");
    setShowOptions(false);
  }

  return (
    <div className="wb-card wb-clone-card">
      <div className="wb-card-title">Clone GitHub repository</div>

      {running ? (
        <div className="wb-clone-progress">
          <div className="wb-clone-statusline">
            <span className="wb-clone-repo">
              {status?.owner
                ? `${status.owner}/${status.repoName}`
                : status?.url}
            </span>
            <span className="wb-clone-phase">
              {status?.phase === "verifying"
                ? "Verifying repository…"
                : status?.phase === "cloning"
                  ? `Cloning ${status?.percent ?? 0}%`
                  : "Validating…"}
            </span>
          </div>
          <div
            className="dl-track"
            role="progressbar"
            aria-valuenow={status?.percent ?? 0}
          >
            <div
              className="dl-bar"
              style={{ width: `${Math.max(2, status?.percent ?? 0)}%` }}
            />
          </div>
          <div className="wb-clone-meta">
            <span title={status?.destination}>{status?.destination}</span>
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => void cancelClone()}
            >
              Cancel
            </button>
          </div>
        </div>
      ) : status && status.state === "succeeded" ? (
        <div className="wb-clone-result wb-clone-ok">
          <div>
            <strong>
              Cloned {status.owner}/{status.repoName}
            </strong>
            {status.head ? (
              <span className="wb-muted"> at {status.head.slice(0, 12)}</span>
            ) : null}
          </div>
          <div className="wb-muted" title={status.destination}>
            {status.switched
              ? `Workspace switched → ${status.destination}`
              : status.switchNote || status.destination}
          </div>
          <div className="wb-clone-actions">
            {status.switched ? null : (
              <button
                type="button"
                className="btn btn-sm"
                onClick={() =>
                  status.destination && void onSwitchedTo(status.destination)
                }
              >
                Open as workspace
              </button>
            )}
            <button type="button" className="btn btn-sm" onClick={reset}>
              Clone another
            </button>
          </div>
        </div>
      ) : status &&
        (status.state === "failed" || status.state === "canceled") ? (
        <div className="wb-clone-result wb-clone-fail">
          <div>
            <strong>
              {status.state === "canceled" ? "Clone canceled" : "Clone failed"}
            </strong>
          </div>
          <div>{cloneErrorMessage(status)}</div>
          {status.outputTail && status.outputTail.length > 0 ? (
            <details className="wb-clone-output">
              <summary>git output</summary>
              <pre>{status.outputTail.join("\n")}</pre>
            </details>
          ) : null}
          <div className="wb-clone-actions">
            <button type="button" className="btn btn-sm" onClick={reset}>
              Try again
            </button>
          </div>
        </div>
      ) : (
        <form
          className="wb-clone-form"
          onSubmit={(event) => {
            event.preventDefault();
            void startClone();
          }}
        >
          <label htmlFor="wb-clone-url">Repository URL</label>
          <input
            id="wb-clone-url"
            type="text"
            value={url}
            placeholder="https://github.com/owner/repository"
            onChange={(event) => setUrl(event.target.value)}
            spellCheck={false}
            autoFocus
          />

          {showOptions ? (
            <>
              <label htmlFor="wb-clone-dest">Destination (optional)</label>
              <input
                id="wb-clone-dest"
                type="text"
                value={destination}
                placeholder={
                  parsed
                    ? `default: workspace/${parsed.repo}`
                    : "default: the SHEYTAN workspace folder"
                }
                onChange={(event) => setDestination(event.target.value)}
                spellCheck={false}
              />
              <label htmlFor="wb-clone-branch">Branch (optional)</label>
              <input
                id="wb-clone-branch"
                type="text"
                value={branch}
                placeholder="default branch"
                onChange={(event) => setBranch(event.target.value)}
                spellCheck={false}
              />
            </>
          ) : null}

          {formError ? <div className="wb-clone-error">{formError}</div> : null}

          <div className="wb-clone-actions">
            <button
              type="submit"
              className="btn btn-primary"
              disabled={starting || !url.trim()}
            >
              {starting ? "Starting…" : "Clone"}
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => setShowOptions((v) => !v)}
            >
              {showOptions ? "Hide options" : "Destination & branch…"}
            </button>
          </div>
        </form>
      )}
    </div>
  );

  async function onSwitchedTo(path: string) {
    try {
      const result = await api.workspaceSwitch(path);
      onSwitched(result.summary);
    } catch {
      // The switch failure surfaces through the workspace summary refresh.
    }
  }
}

function WorkspacePanel() {
  const [summary, setSummary] = useState<WorkspaceSummaryData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionNote, setActionNote] = useState<string | null>(null);
  const [pickPath, setPickPath] = useState("");
  const [showSwitch, setShowSwitch] = useState(false);

  const running = useRuntimeStore((s) => s.running);
  const activeSessionId = useRuntimeStore((s) => s.activeSessionId);
  const createSession = useRuntimeStore((s) => s.createSession);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await api.workspace();
      setSummary(data);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to load workspace.",
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Refresh the summary when a run settles — the agent state chip and the
  // recent-file list may both change as a side effect of the run.
  useEffect(() => {
    if (!loading) {
      void refresh();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running, activeSessionId]);

  async function reveal(target: "files" | "terminal") {
    setActionNote(null);
    try {
      await api.workspaceReveal(target);
      setActionNote(
        target === "files"
          ? "Opened the folder in your file manager."
          : "Opened a terminal at the workspace folder.",
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reveal failed.");
    }
  }

  async function switchRoot(path: string) {
    setError(null);
    setActionNote(null);
    try {
      const result = await api.workspaceSwitch(path);
      setSummary(result.summary);
      setActionNote(
        `Workspace changed → ${result.summary.root}${
          result.changed && result.changed.length > 0
            ? ` (${result.changed.join(", ")})`
            : ""
        }`,
      );
      setShowSwitch(false);
      setPickPath("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Workspace switch failed.");
    }
  }

  // v1.3.0: the clone flow hands the post-clone workspace summary back
  // through this callback (clone → verified → switched → summary).
  const onCloneSwitched = useCallback((next: WorkspaceSummaryData) => {
    setSummary(next);
    setActionNote(`Workspace switched → ${next.root} (cloned repository)`);
  }, []);

  const summaryLine = useMemo(() => {
    if (!summary) return [];
    const chips: { label: string; value: string }[] = [
      {
        label: "Project",
        value:
          summary.root.split(/[\\/]/).filter(Boolean).pop() ?? summary.root,
      },
      {
        label: "Files",
        value: summary.recentFiles?.length
          ? `${summary.recentFiles.length} recent`
          : summary.exists
            ? "—"
            : "missing",
      },
      {
        label: "Task",
        value: summary.activeSession?.title
          ? summary.activeSession.title
          : summary.activeSession
            ? `session ${summary.activeSession.id}`
            : "no session",
      },
      {
        label: "Model",
        value: summary.runtime?.model || "none",
      },
      {
        label: "Runtime",
        value: summary.runtime?.engineState || "unknown",
      },
      {
        label: "State",
        value: summary.agent?.running ? "running" : "idle",
      },
    ];
    return chips;
  }, [summary]);

  if (loading && !summary) {
    return <WorkspaceLoading />;
  }

  return (
    <div className="workspace-panel">
      {error ? (
        <div className="error-banner" role="alert">
          <span>{error}</span>
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => void refresh()}
          >
            Retry
          </button>
        </div>
      ) : null}

      {actionNote ? (
        <div className="wb-action-note" role="status">
          {actionNote}
        </div>
      ) : null}

      {/* Project → files → task → model → runtime → state */}
      <div className="wb-card wb-summary-card">
        <div className="wb-card-title">Current environment</div>
        <div className="wb-summary-line">
          {summaryLine.map((chip) => (
            <span
              className="wb-chip"
              key={chip.label}
              title={`${chip.label}: ${chip.value}`}
            >
              <span className="wb-chip-label">{chip.label}</span>
              <span className="wb-chip-value">{chip.value}</span>
            </span>
          ))}
        </div>
        <div className="wb-summary-path" title={summary?.root}>
          {summary?.root}
        </div>
      </div>

      {/* Quick actions */}
      <div className="wb-card">
        <div className="wb-card-title">Quick actions</div>
        <div className="wb-actions">
          <button
            type="button"
            className="btn"
            onClick={() => void reveal("files")}
            disabled={!summary?.exists}
            title="Open this folder in your file manager"
          >
            Open in Explorer
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => void reveal("terminal")}
            disabled={!summary?.exists}
            title="Open a terminal here"
          >
            Open terminal
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => {
              createSession();
              void refresh();
            }}
            title="Start a fresh session"
          >
            New session
          </button>
          <button type="button" className="btn" onClick={() => void refresh()}>
            Refresh
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => setShowSwitch((v) => !v)}
            title="Point SHEYTAN at a different project folder"
          >
            Change workspace
          </button>
        </div>

        {showSwitch ? (
          <form
            className="wb-switch-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (pickPath.trim()) {
                void switchRoot(pickPath.trim());
              }
            }}
          >
            <label htmlFor="wb-path-input">Project folder path</label>
            <input
              id="wb-path-input"
              type="text"
              value={pickPath}
              placeholder="e.g. C:\dev\my-project or /home/me/my-project"
              onChange={(event) => setPickPath(event.target.value)}
              autoFocus
            />
            <div className="wb-switch-actions">
              <button
                type="submit"
                className="btn btn-primary"
                disabled={!pickPath.trim()}
              >
                Switch
              </button>
              <button
                type="button"
                className="btn"
                onClick={() => setShowSwitch(false)}
              >
                Cancel
              </button>
            </div>
          </form>
        ) : null}

        {summary?.recentWorkspaces && summary.recentWorkspaces.length > 0 ? (
          <div className="wb-recents">
            <span className="wb-muted">Recent:</span>
            {summary.recentWorkspaces.map((recent) => (
              <button
                type="button"
                key={recent}
                className="wb-chip wb-chip-btn"
                title={recent}
                onClick={() => void switchRoot(recent)}
              >
                {recent.split(/[\\/]/).filter(Boolean).pop() ?? recent}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {/* v1.3.0: first-class GitHub clone workflow */}
      <CloneCard onSwitched={onCloneSwitched} />

      {/* Project health */}
      <div className="wb-grid">
        <div className="wb-card">
          <div className="wb-card-title">Project health</div>
          {summary?.project && Object.keys(summary.project).length > 1 ? (
            <dl className="wb-facts">
              {summary.project.languages &&
              summary.project.languages.length > 0 ? (
                <div className="wb-fact">
                  <dt>Languages</dt>
                  <dd>{summary.project.languages.join(", ")}</dd>
                </div>
              ) : null}
              {summary.project.buildCmd ? (
                <div className="wb-fact">
                  <dt>Build</dt>
                  <dd>
                    <code>{summary.project.buildCmd}</code>
                    {summary.project.buildVerifiedAt ? (
                      <span className="wb-verified"> verified</span>
                    ) : null}
                  </dd>
                </div>
              ) : null}
              {summary.project.testCmd ? (
                <div className="wb-fact">
                  <dt>Test</dt>
                  <dd>
                    <code>{summary.project.testCmd}</code>
                    {summary.project.testVerifiedAt ? (
                      <span className="wb-verified"> verified</span>
                    ) : null}
                  </dd>
                </div>
              ) : null}
              {summary.project.lessons && summary.project.lessons.length > 0 ? (
                <div className="wb-fact">
                  <dt>Lessons</dt>
                  <dd>{summary.project.lessons.length} recorded</dd>
                </div>
              ) : null}
              {summary.project.conventions &&
              summary.project.conventions.length > 0 ? (
                <div className="wb-fact">
                  <dt>Conventions</dt>
                  <dd>{summary.project.conventions.length} recorded</dd>
                </div>
              ) : null}
              {summary.project.lastObservedAt ? (
                <div className="wb-fact">
                  <dt>Observed</dt>
                  <dd>{formatWhen(summary.project.lastObservedAt)}</dd>
                </div>
              ) : null}
            </dl>
          ) : (
            <p className="wb-muted">
              No measured facts yet for this folder. Run a task or a Coding Lab
              job here and SHEYTAN will record languages, build/test commands
              and lessons automatically.
            </p>
          )}
        </div>

        {/* Recent files */}
        <div className="wb-card">
          <div className="wb-card-title">Recent files</div>
          {summary?.recentFiles && summary.recentFiles.length > 0 ? (
            <ul className="wb-files">
              {summary.recentFiles.map((file) => (
                <li key={file.name} title={file.name}>
                  <span className="wb-file-name">{file.name}</span>
                  <span className="wb-file-meta">
                    {formatBytes(file.size)} · {formatWhen(file.modTime)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="wb-muted">No files in this workspace yet.</p>
          )}
        </div>
      </div>

      {/* v1.3.4 (ROADMAP v1.4 slice 1): persistent repository index +
          the repository search entry point. */}
      <RepositoryIndexCard />
    </div>
  );
}

export default WorkspacePanel;
