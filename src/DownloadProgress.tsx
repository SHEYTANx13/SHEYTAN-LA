// DownloadProgress — the shared visual language for every remote asset
// the app pulls (v1.2.3 Download Manager): a phase chain
// (Resolving → Connecting → Downloading → Verifying → Installing →
// Ready), a determinate progress bar, measured speed / ETA / bytes, the
// active source, the verification state and retry reasons. Used by the
// Updates card (app installer) and the engine/runtime surfaces (llama.cpp
// archive, future model packages).
import { useMemo } from "react";
import type { DownloadProgress as DlProgress } from "./api";
import { fmtBytes } from "./settings-shared";

// Phase chain in display order; the active phase highlights, completed
// phases tick.
const PHASE_ORDER = [
  "resolving",
  "connecting",
  "downloading",
  "verifying",
  "installing",
  "ready",
] as const;

const PHASE_LABELS: Record<string, string> = {
  resolving: "Resolving",
  connecting: "Connecting",
  downloading: "Downloading",
  verifying: "Verifying",
  installing: "Installing",
  ready: "Ready",
};

const TRUST_LABELS: Record<string, string> = {
  primary: "authoritative source",
  mirror: "mirror",
  fallback: "fallback source",
};

function formatSpeed(bps?: number): string | null {
  if (!bps || bps <= 0) return null;
  if (bps >= 1024 * 1024) return `${(bps / (1024 * 1024)).toFixed(1)} MB/s`;
  return `${Math.max(1, Math.round(bps / 1024))} KB/s`;
}

function formatEta(seconds?: number): string | null {
  if (seconds === undefined || seconds < 0 || !Number.isFinite(seconds)) {
    return null;
  }
  if (seconds < 5) return "a few seconds";
  if (seconds < 90) return `${Math.round(seconds)}s`;
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return `${m}m ${s.toString().padStart(2, "0")}s`;
}

export interface DownloadProgressProps {
  /** Live progress snapshot; undefined renders nothing. */
  progress?: DlProgress;
  /** Compact single-row variant for header pills / runtime cards. */
  compact?: boolean;
  /** Optional actions; each renders only when provided. */
  onCancel?: () => void;
  cancelLabel?: string;
  onRetry?: () => void;
  retryLabel?: string;
  onReveal?: () => void;
  revealLabel?: string;
  /** Set while an action request is in flight (buttons go busy). */
  busy?: boolean;
}

export function DownloadProgressPanel({
  progress,
  compact = false,
  onCancel,
  cancelLabel = "Cancel",
  onRetry,
  retryLabel = "Retry",
  onReveal,
  revealLabel = "Reveal",
  busy = false,
}: DownloadProgressProps) {
  const pct = useMemo(() => {
    if (!progress || progress.bytesTotal <= 0) return null;
    const p = (progress.bytesDone / progress.bytesTotal) * 100;
    return Math.max(0, Math.min(100, p));
  }, [progress]);

  if (!progress) return null;

  const activeIndex = PHASE_ORDER.indexOf(
    progress.phase as (typeof PHASE_ORDER)[number],
  );
  const speed = formatSpeed(progress.bytesPerSec);
  const eta = formatEta(progress.etaSeconds);
  const trust =
    progress.sourceTrust != null ? TRUST_LABELS[progress.sourceTrust] : null;
  const stateLabel = progress.paused
    ? "Paused"
    : progress.phase === "ready"
      ? "Ready"
      : PHASE_LABELS[progress.phase] ?? progress.phase;

  return (
    <div
      className={`dl-panel${compact ? " dl-panel-compact" : ""}`}
      role="status"
      aria-live="polite"
      aria-busy={progress.phase !== "ready" && !progress.paused}
      aria-label={`Download ${stateLabel}${
        pct !== null ? `, ${Math.round(pct)} percent` : ""
      }`}
    >
      {/* phase chain */}
      {!compact && (
        <ol className="dl-phases" aria-label="Download phases">
          {PHASE_ORDER.map((phase, i) => (
            <li
              key={phase}
              className={
                "dl-phase" +
                (i < activeIndex ? " done" : "") +
                (i === activeIndex ? " active" : "") +
                (i > activeIndex ? " todo" : "")
              }
              aria-current={i === activeIndex ? "step" : undefined}
            >
              <span className="dl-phase-dot" aria-hidden="true" />
              <span className="dl-phase-label">{PHASE_LABELS[phase]}</span>
            </li>
          ))}
        </ol>
      )}

      {/* progress bar + headline */}
      <div className="dl-row">
        <span className="dl-state">{stateLabel}</span>
        <div
          className="m-progress-track dl-track"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={pct !== null ? Math.round(pct) : undefined}
        >
          <div
            className={
              "dl-bar" + (pct === null ? " indeterminate" : "")
            }
            style={
              pct !== null
                ? { width: `${pct}%` }
                : undefined
            }
          />
        </div>
        <span className="dl-pct">
          {pct !== null ? `${Math.round(pct)}%` : "—"}
        </span>
      </div>

      {/* measured facts */}
      {!compact && (
        <div className="dl-facts">
          <span>
            {fmtBytes(progress.bytesDone)}
            {progress.bytesTotal > 0
              ? ` / ${fmtBytes(progress.bytesTotal)}`
              : ""}
          </span>
          {speed && <span>{speed}</span>}
          {eta && !progress.paused && <span>ETA {eta}</span>}
          {progress.sourceLabel && (
            <span className="dl-source" title={progress.sourceUrl}>
              {trust ? `${progress.sourceLabel} · ${trust}` : progress.sourceLabel}
            </span>
          )}
          {progress.verified && (
            <span className="dl-verified" title="SHA-256 / size verified before activation">
              ✓ verified
            </span>
          )}
          {(progress.retries ?? 0) > 0 && (
            <span className="dl-retries">
              {progress.retries} retr{progress.retries === 1 ? "y" : "ies"}
            </span>
          )}
          {progress.resumable && <span className="dl-resumed">resumed</span>}
        </div>
      )}

      {progress.message && !compact && (
        <p className="dl-message" role={progress.phase === "ready" ? undefined : "status"}>
          {progress.message}
        </p>
      )}

      {(onCancel || onRetry || onReveal) && (
        <div className="dl-actions">
          {onCancel && progress.phase !== "ready" && (
            <button
              type="button"
              className="text-button dl-action"
              onClick={onCancel}
              disabled={busy}
            >
              {busy ? "Stopping…" : cancelLabel}
            </button>
          )}
          {onRetry && (
            <button
              type="button"
              className="text-button dl-action"
              onClick={onRetry}
              disabled={busy}
            >
              {busy ? "Working…" : retryLabel}
            </button>
          )}
          {onReveal && (
            <button
              type="button"
              className="text-button dl-action"
              onClick={onReveal}
              disabled={busy}
            >
              {revealLabel}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
