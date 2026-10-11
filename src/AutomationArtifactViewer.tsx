import { memo, useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";

import { api, type AutomationArtifact } from "./api";
import {
  artifactViewMode,
  formatArtifactSize,
  formatRelativeTime,
  formatWhen,
  prettyJsonIfNeeded,
} from "./automation-view";

// AutomationArtifactViewer — v1.7.0: the task artifact surface.
//
// The registry (GET /api/automation/artifacts?task=) lists one task's
// artifacts newest-first; clicking one opens the VIEWER: provenance
// (source, run, hash, version), the version strip, and the content —
//
//   markdown  → rendered (react-markdown + GFM + highlight)
//   image/svg → <img> against the sandboxed content endpoint (the
//               content is NEVER inlined into the application DOM —
//               the backend answers with a deny-all CSP and the SVG
//               stays an isolated document; XSS containment by design)
//   source    → <pre> text; HTML artifacts are shown as SOURCE (the
//               backend already serves them as text/plain) and data
//               artifacts named *.json pretty-print when parseable
//
// Nothing here ever evaluates artifact content: fetch wrapper + text or
// <img src> only.

const ART_KIND_GLYPH: Record<string, string> = {
  doc: "≡",
  code: "{ }",
  data: "#",
  chart: "◔",
  image: "▣",
  archive: "▦",
  diagnostics: "⚠",
  other: "·",
};

const ART_KIND_LABEL: Record<string, string> = {
  doc: "doc",
  code: "code",
  data: "data",
  chart: "chart",
  image: "image",
  archive: "archive",
  diagnostics: "diagnostics",
  other: "other",
};

// ArtifactMarkdown reuses the chat surface's markdown styles (the
// .message-markdown rule set in styles.css) so rendered reports look
// like first-class documents, not pasted text.
const ArtifactMarkdown = memo(function ArtifactMarkdown({
  content,
}: {
  content: string;
}) {
  return (
    <div className="aut-markdown message-markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
        {content}
      </ReactMarkdown>
    </div>
  );
});

function isHtmlArtifact(artifact: AutomationArtifact): boolean {
  return artifact.relPath.toLowerCase().endsWith(".html");
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function ArtifactViewer({
  artifact,
  onClose,
}: {
  artifact: AutomationArtifact;
  onClose: () => void;
}) {
  // The version strip loads for the ORIGINAL artifact's task+path; the
  // viewer's `viewing` tracks which version's content is on display
  // (clicking an older version swaps content AND provenance).
  const [versions, setVersions] = useState<AutomationArtifact[] | null>(null);
  const [viewing, setViewing] = useState<AutomationArtifact>(artifact);
  const [content, setContent] = useState<string | null>(null);
  const [contentError, setContentError] = useState<string | null>(null);
  const [loadingContent, setLoadingContent] = useState(false);

  const mode = artifactViewMode(viewing);

  useEffect(() => {
    // Keep the viewer in sync when the parent opens a different artifact
    // without unmounting (openId swap).
    setViewing(artifact);
    setVersions(null);
  }, [artifact]);

  useEffect(() => {
    let cancelled = false;

    api
      .automationArtifactVersions(artifact.id)
      .then((list) => {
        if (!cancelled) setVersions(list);
      })
      .catch(() => {
        // The strip is supplementary — a failed version lookup leaves it
        // hidden instead of faking an empty history.
        if (!cancelled) setVersions(null);
      });

    return () => {
      cancelled = true;
    };
  }, [artifact.id]);

  useEffect(() => {
    // Image-family artifacts render straight from the sandboxed content
    // endpoint — no text fetch, nothing to parse.
    if (mode === "image" || mode === "svg") {
      setContent(null);
      setContentError(null);
      setLoadingContent(false);
      return;
    }

    const controller = new AbortController();

    setLoadingContent(true);
    setContentError(null);

    api
      .automationArtifactContent(viewing.id, controller.signal)
      .then((text) => {
        if (!controller.signal.aborted) {
          setContent(text);
          setLoadingContent(false);
        }
      })
      .catch((err) => {
        if (controller.signal.aborted) return;
        setContent(null);
        setContentError(
          errorMessage(err, "The artifact content could not be loaded."),
        );
        setLoadingContent(false);
      });

    return () => controller.abort();
  }, [viewing.id, mode]);

  const filename = viewing.relPath.split("/").pop() ?? viewing.relPath;
  const contentURL = api.automationArtifactContentURL(viewing.id);

  return (
    <div className="aut-viewer">
      <div className="aut-viewer-head">
        <div className="aut-viewer-title">
          <span className={`aut-art-kind kind-${viewing.kind}`}>
            {ART_KIND_GLYPH[viewing.kind] ?? "·"}
          </span>

          <div>
            <strong>{viewing.title || viewing.relPath}</strong>

            <span className="aut-viewer-sub">
              {viewing.relPath} · run {viewing.runId || "—"}
            </span>
          </div>
        </div>

        <div className="aut-viewer-chiprow">
          <span className="settings-chip chip-neutral">
            {ART_KIND_LABEL[viewing.kind] ?? viewing.kind}
          </span>

          <span className="settings-chip chip-accent">
            v{viewing.version}
            {viewing.isCurrent ? " · current" : ""}
          </span>

          <a
            className="aut-download"
            href={contentURL}
            download={filename}
            title="Download this version"
          >
            Download
          </a>

          <button
            type="button"
            className="text-button"
            onClick={onClose}
            aria-label="Close artifact viewer"
          >
            Close
          </button>
        </div>
      </div>

      <div className="aut-viewer-meta">
        <div className="session-detail">
          <span>Created</span>
          <strong title={viewing.created}>{formatWhen(viewing.created)}</strong>
        </div>

        <div className="session-detail">
          <span>Source</span>
          <strong>{viewing.source || "—"}</strong>
        </div>

        <div className="session-detail">
          <span>Size</span>
          <strong>{formatArtifactSize(viewing.size)}</strong>
        </div>

        <div className="session-detail">
          <span>Hash</span>
          <strong title={viewing.hash || undefined} className="aut-hash">
            {viewing.hash || "—"}
          </strong>
        </div>
      </div>

      {versions && versions.length > 1 ? (
        <div className="aut-versions" role="list" aria-label="Version history">
          <span className="eyebrow">VERSIONS</span>

          <div className="aut-version-strip">
            {versions.map((version) => (
              <button
                type="button"
                key={version.id}
                role="listitem"
                className={`aut-version-btn ${
                  version.id === viewing.id ? "active" : ""
                } ${version.isCurrent ? "current" : ""}`}
                onClick={() => setViewing(version)}
                title={`${formatWhen(version.created)} · ${formatArtifactSize(version.size)}`}
              >
                v{version.version}
              </button>
            ))}
          </div>
        </div>
      ) : null}

      <div className="aut-viewer-body">
        {mode === "image" || mode === "svg" ? (
          <img
            className="aut-art-image"
            src={contentURL}
            alt={viewing.title || viewing.relPath}
            loading="lazy"
          />
        ) : loadingContent ? (
          <div className="aut-empty">Loading content…</div>
        ) : contentError ? (
          <div className="aut-empty">
            The content could not be loaded — {contentError}
          </div>
        ) : mode === "markdown" && content !== null ? (
          <ArtifactMarkdown content={content} />
        ) : content !== null ? (
          <>
            {isHtmlArtifact(viewing) ? (
              <p className="aut-sandbox-note">
                HTML artifacts are shown as source (sandboxed) — they are never
                rendered or executed inside the app.
              </p>
            ) : null}

            <pre className="aut-art-source">
              {prettyJsonIfNeeded(viewing.kind, viewing.relPath, content)}
            </pre>
          </>
        ) : null}
      </div>
    </div>
  );
}

export function ArtifactSection({
  taskId,
  revision,
}: {
  taskId: string;
  revision: number;
}) {
  const [artifacts, setArtifacts] = useState<AutomationArtifact[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [openId, setOpenId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    setError(null);

    api
      .automationArtifacts(taskId)
      .then((list) => {
        if (!cancelled) setArtifacts(list);
      })
      .catch((err) => {
        if (!cancelled) {
          setError(errorMessage(err, "The artifact list could not be loaded."));
        }
      });

    return () => {
      cancelled = true;
    };
  }, [taskId, revision]);

  // A deleted/absent artifact can no longer stay open in the viewer.
  useEffect(() => {
    if (
      openId &&
      artifacts &&
      !artifacts.some((artifact) => artifact.id === openId)
    ) {
      setOpenId(null);
    }
  }, [artifacts, openId]);

  const openArtifact = openId
    ? (artifacts?.find((artifact) => artifact.id === openId) ?? null)
    : null;

  return (
    <section className="aut-section">
      <div className="aut-section-heading">
        <div>
          <span className="eyebrow">ARTIFACTS</span>

          <strong>Task artifacts</strong>
        </div>

        <span className="aut-count">{artifacts?.length ?? 0}</span>
      </div>

      {error ? (
        <div className="error-banner" role="alert">
          {error}
        </div>
      ) : null}

      {openArtifact ? (
        <ArtifactViewer
          artifact={openArtifact}
          onClose={() => setOpenId(null)}
        />
      ) : null}

      {artifacts === null ? (
        error ? null : <div className="aut-empty">Loading artifacts…</div>
      ) : artifacts.length === 0 ? (
        <div className="aut-empty">
          No artifacts yet — files a run produces (reports, charts, code, data)
          appear here with provenance and version history.
        </div>
      ) : (
        <div className="aut-art-grid">
          {artifacts.map((artifact) => (
            <button
              type="button"
              key={artifact.id}
              className={`aut-art-card ${
                artifact.id === openId ? "active" : ""
              }`}
              onClick={() => setOpenId(artifact.id)}
              title={`${artifact.relPath} · ${formatArtifactSize(artifact.size)}`}
            >
              <span className={`aut-art-kind kind-${artifact.kind}`}>
                {ART_KIND_GLYPH[artifact.kind] ?? "·"}
              </span>

              <span className="aut-art-copy">
                <strong>{artifact.title || artifact.relPath}</strong>

                <span>
                  {artifact.relPath} · {formatArtifactSize(artifact.size)}
                </span>

                <span>
                  v{artifact.version}
                  {artifact.isCurrent ? "" : " · archived"} ·{" "}
                  {formatRelativeTime(artifact.created)}
                </span>
              </span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}
