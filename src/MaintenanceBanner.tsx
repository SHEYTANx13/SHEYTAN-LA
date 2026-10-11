// MaintenanceBanner — v1.6.0 P0/P4 (spec §12): the engine maintenance UX
// matches the REAL lifecycle. The startup maintenance gate exposes its
// explicit phase through /api/maintenance; this surface renders the
// honest sequence instead of a generic "Updating…":
//
//   Preparing engine update → Checking release → Downloading →
//   Verifying → Installing → Starting engine → Verifying engine + model
//   → Ready
//
// The banner polls ONLY while a maintenance decision is in flight (the
// gate is single-shot at startup); terminal states render once with
// their outcome — including honest failure/blocked diagnostics — and
// then fade out. Nothing is fabricated: bytes/speed/ETA come from the
// engine's own Download Manager surface (DownloadProgressPanel), never
// from here.
import { useEffect, useState } from "react";

import { api, type MaintenanceStatus } from "./api";

// The user-understandable sequence (spec §12) mapped from the gate's
// authoritative phases. Every label describes what is ACTUALLY
// happening; the engine restart is part of the transaction and is never
// presented as a crash.
const PHASE_LABELS: Record<MaintenanceStatus["phase"], string> = {
  CHECKING: "Checking release",
  MAINTENANCE_REQUIRED: "Preparing engine update",
  DOWNLOADING: "Downloading",
  VERIFYING: "Verifying",
  INSTALLING: "Installing",
  READY_FOR_PREWARM: "Starting engine",
  DEFERRED: "No maintenance required",
  BLOCKED: "Engine maintenance blocked",
  FAILED: "Engine maintenance failed",
};

const TERMINAL: MaintenanceStatus["phase"][] = [
  "READY_FOR_PREWARM",
  "DEFERRED",
  "BLOCKED",
  "FAILED",
];

const POLL_MS = 1500;

export function MaintenanceBanner() {
  const [status, setStatus] = useState<MaintenanceStatus | null>(null);

  useEffect(() => {
    let alive = true;
    let timer: number | undefined;

    async function poll() {
      try {
        const next = await api.maintenance();
        if (!alive) return;
        setStatus(next);
      } catch {
        // The endpoint may briefly 404/503 before the server finishes
        // booting — retry silently.
      }

      if (!alive) return;

      // Keep polling while the gate is mid-decision; park once terminal.
      const terminal =
        status !== null && TERMINAL.includes(status.phase) && status.finishedAt;

      if (!terminal) {
        timer = window.setTimeout(() => void poll(), POLL_MS);
      }
    }

    void poll();

    return () => {
      alive = false;
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
    // status is intentionally NOT a dependency: the poll loop owns the
    // cadence and closes over fresh state through setStatus.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Nothing to render: no maintenance decision observed yet.
  if (!status) {
    return null;
  }

  const isDef = status.phase === "DEFERRED";

  // A no-op gate is not engine news — only surfaced in the banner when
  // maintenance actually ran (or failed/blocked).
  if (isDef && !status.updated) {
    return null;
  }

  const done = Boolean(status.finishedAt);
  const failed = status.phase === "FAILED" || status.phase === "BLOCKED";

  const label = PHASE_LABELS[status.phase];

  return (
    <div
      className={`maintenance-banner ${
        failed ? "maintenance-banner-failed" : ""
      } ${done ? "maintenance-banner-done" : ""}`}
      role="status"
    >
      <span className="maintenance-banner-phase">
        {failed ? "" : done ? "✓ " : "… "}
        {label}
        {status.targetTag ? ` · ${status.targetTag}` : ""}
      </span>

      {status.detail ? (
        <span className="maintenance-banner-detail" title={status.reason ?? ""}>
          {status.detail}
        </span>
      ) : null}

      {status.phase === "READY_FOR_PREWARM" && status.updated ? (
        <span className="maintenance-banner-detail">
          engine verified — starting engine, then verifying engine + model
        </span>
      ) : null}

      {status.phase === "BLOCKED" ? (
        <span className="maintenance-banner-detail">
          the installed engine is not usable and the update failed — the
          engine will not start until the issue is resolved
        </span>
      ) : null}
    </div>
  );
}

export default MaintenanceBanner;
