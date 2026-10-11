import { memo, useEffect, useRef, useState } from "react";

import { api, type PerfSnapshot } from "./api";
import { useRuntimeStore } from "./store";

// v1.1.8: compact performance readout for the Agent surface. This reuses
// the existing /api/perf snapshot (same source as the diagnostic HUD and
// the Settings performance page) — it is NOT a second telemetry system.
// Values shown are backend-measured; anything unavailable renders as
// N/A instead of being fabricated.

const POLL_INTERVAL_MS = 4000;

function formatPercent(value: number | null | undefined): string {
  return typeof value === "number" ? `${Math.round(value)}%` : "N/A";
}

function formatBytesCompact(bytes: number | null | undefined): string {
  if (typeof bytes !== "number" || bytes <= 0) {
    return "N/A";
  }

  if (bytes >= 1024 ** 3) {
    return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
  }

  return `${Math.round(bytes / 1024 ** 2)} MB`;
}

function formatTokPerSec(value: number | undefined): string {
  return typeof value === "number" && value > 0
    ? `${value.toFixed(1)} tok/s`
    : "N/A";
}

function formatTTFT(seconds: number | undefined): string {
  if (typeof seconds !== "number" || seconds <= 0) {
    return "N/A";
  }

  return seconds < 1 ? `${Math.round(seconds * 1000)} ms` : `${seconds.toFixed(2)} s`;
}

// compatBadge maps the persisted compatibility level to the concise
// wording used across the UI. The backend owns the level; this only
// labels it. Unknown levels render verbatim (never invented).
function compatBadge(perf: PerfSnapshot): { label: string; title: string } {
  const compat = perf.compat;

  if (!compat || typeof compat.level !== "number") {
    return { label: "N/A", title: "No compatibility record yet." };
  }

  const reason = compat.reason ? ` — ${compat.reason}` : "";

  if (compat.level <= 0 && compat.optimised) {
    return {
      label: "Optimised",
      title: `All requested options verified native${reason}`,
    };
  }

  if (compat.level <= 0) {
    return { label: "Compatible", title: `Verified ready${reason}` };
  }

  return {
    label: `Fallback L${compat.level}`,
    title: `Options repaired by the engine compatibility layer${reason}`,
  };
}

function PerfCell({
  label,
  value,
  title,
}: {
  label: string;
  value: string;
  title?: string;
}) {
  return (
    <div className="perf-cell" title={title}>
      <span className="perf-cell-label">{label}</span>
      <span className="perf-cell-value">{value}</span>
    </div>
  );
}

const PerfStrip = function PerfStrip() {
  const [perf, setPerf] = useState<PerfSnapshot | null>(null);
  const engineModel = useRuntimeStore((state) => state.engine?.model);
  const engineState = useRuntimeStore((state) => state.engine?.state);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      // v1.1.9: skip the fetch while the window is hidden — the strip is
      // invisible anyway, and /api/perf performs real host measurement
      // work. The next visible tick refreshes immediately.
      if (document.hidden) {
        return;
      }

      try {
        const snapshot = await api.perf();

        if (!cancelled) {
          setPerf(snapshot);
        }
      } catch {
        // The strip shows N/A on the next render — polling continues.
      }
    }

    function handleVisibility() {
      if (!document.hidden) {
        void poll();
      }
    }

    void poll();
    timerRef.current = window.setInterval(poll, POLL_INTERVAL_MS);
    document.addEventListener("visibilitychange", handleVisibility);

    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", handleVisibility);

      if (timerRef.current !== null) {
        window.clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };
  }, []);

  const ram = perf?.ram ?? null;
  const vram = perf?.vram ?? null;
  const context = perf?.context ?? null;
  const generation = perf?.generation ?? null;
  const compat = perf ? compatBadge(perf) : null;

  return (
    <div className="perf-strip" aria-label="Live performance telemetry">
      <PerfCell
        label="CPU"
        value={formatPercent(perf?.cpuPercent)}
        title="Total host CPU utilisation (measured)"
      />
      <PerfCell
        label="GPU"
        value={formatPercent(perf?.gpuPercent)}
        title="GPU utilisation (measured; N/A when no usable GPU provider)"
      />
      <PerfCell
        label="RAM"
        value={
          ram ? `${formatBytesCompact(ram.usedBytes)} / ${formatBytesCompact(ram.totalBytes)}` : "N/A"
        }
        title="Host memory used / total (measured)"
      />
      <PerfCell
        label="VRAM"
        value={
          vram
            ? `${Math.round(vram.usedMb)} / ${Math.round(vram.totalMb)} MB`
            : "N/A"
        }
        title="GPU memory used / total (measured; N/A when unavailable)"
      />
      <PerfCell
        label="PROMPT"
        value={formatTokPerSec(generation?.promptTokPerSec)}
        title="Prompt processing speed over the latest sample"
      />
      <PerfCell
        label="GEN"
        value={formatTokPerSec(generation?.genTokPerSec)}
        title="Generation speed over the latest sample"
      />
      <PerfCell
        label="TTFT"
        value={formatTTFT(generation?.ttftSeconds)}
        title="Time to first token over the latest sample"
      />
      <PerfCell
        label="CONTEXT"
        value={
          context && context.total > 0
            ? `${context.usedTokens} / ${context.total}`
            : "N/A"
        }
        title="Tokens used in the serving context window"
      />
      <PerfCell
        label="ENGINE"
        value={engineState ?? perf?.engineState ?? "N/A"}
        title="Engine lifecycle state"
      />
      <PerfCell
        label="BACKEND"
        value={perf?.backend ?? "N/A"}
        title="Active inference backend (llama.cpp or native)"
      />
      {compat ? (
        <PerfCell
          label="COMPAT"
          value={compat.label}
          title={compat.title}
        />
      ) : null}
      <PerfCell
        label="MODEL"
        value={perf?.model ?? engineModel ?? "N/A"}
        title="Model currently served"
      />
    </div>
  );
};

export default memo(PerfStrip);
