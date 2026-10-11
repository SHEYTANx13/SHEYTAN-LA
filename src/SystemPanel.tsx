import { useCallback, useEffect, useRef, useState } from "react";

import {
  api,
  type EnvironmentPayload,
  type GovernorPayload,
  type HealthPayload,
} from "./api";
import { useRuntimeStore } from "./store";
import { useResource } from "./useResource";
import { DownloadProgressPanel } from "./DownloadProgress";
import { AISystemsCard } from "./AISystemsCard";
import { GoalsCard } from "./GoalsCard";

// v1.2.0 — the Environment Centre (System Centre): one honest view over
// device, runtime, verified health and the recommendation engine. Every
// value comes from /api/environment + /api/health (existing telemetry);
// unknown values render as "—".
//
// v1.2.2 hardening: this panel previously crashed the whole app when the
// backend payload was malformed or partial (a nil Go GPU slice marshals
// as JSON `null`, and `device.gpus.length` then threw inside render —
// with no error boundary that blanked the window). Every optional field
// is now read defensively AND the fetches are AbortController-cancellable
// so navigating away mid-request can never mutate unmounted state.

function fmtBytes(bytes: number | undefined): string {
  if (!bytes || bytes <= 0) return "—";
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
  return `${Math.round(bytes / 1024 ** 2)} MB`;
}

const HEALTH_TONE: Record<string, string> = {
  ok: "good",
  warn: "warn",
  fail: "bad",
  unknown: "neutral",
  na: "neutral",
};

const HEALTH_SYMBOL: Record<string, string> = {
  ok: "✓",
  warn: "!",
  fail: "✕",
  unknown: "?",
  na: "–",
};

function DeviceCard({
  env,
  status,
  onRetry,
}: {
  env: EnvironmentPayload | null;
  status: "loading" | "ready" | "error";
  onRetry: () => void;
}) {
  // v1.2.5: honest states — loading says MEASURING, error says UNAVAILABLE
  // with a retry. The old fallback rendered "Measuring this machine…"
  // forever when the probe failed, which lied about the state.
  if (status === "error" || (status === "ready" && (!env || !env.device))) {
    return (
      <div className="settings-card">
        <span className="eyebrow">DEVICE</span>
        <p className="env-loading">
          Device information is unavailable right now (the hardware probe
          failed or timed out).
        </p>
        <div className="header-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={onRetry}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!env || !env.device) {
    return (
      <div className="settings-card">
        <span className="eyebrow">DEVICE</span>
        <p className="env-loading">Measuring this machine…</p>
      </div>
    );
  }

  // v1.2.2: defensive reads — a nil Go slice arrives as JSON null and
  // must never throw during render (the historical System-tab crash).
  const device = env.device;
  const gpus = Array.isArray(device.gpus) ? device.gpus : [];
  const npu = device.npu ?? null;
  const cpu = device.cpu ?? { name: "" };
  const ram = device.ram ?? { totalBytes: 0, availableBytes: 0 };
  const storage = device.storage ?? {};
  const identity = device.identity ?? {
    product: "",
    shortName: "",
    platform: "",
  };
  const deepReady = device.deepReady !== false;

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">DEVICE</span>
          <strong>{identity.shortName || "This machine"}</strong>
        </div>
        <span className="settings-chip chip-neutral">
          {device.os || "—"} · {device.arch || "—"}
        </span>
      </div>

      <div className="env-grid">
        <div className="session-detail">
          <span>CPU</span>
          <strong title={cpu.name}>
            {cpu.name || "—"}
            {cpu.physicalCores
              ? ` · ${cpu.physicalCores}C/${cpu.logicalCores ?? "?"}T`
              : ""}
          </strong>
        </div>

        <div className="session-detail">
          <span>RAM</span>
          <strong>
            {fmtBytes(ram.totalBytes)}
            {ram.availableBytes
              ? ` · ${fmtBytes(ram.availableBytes)} available`
              : ""}
          </strong>
        </div>

        <div className="session-detail">
          <span>GPU</span>
          <strong>
            {gpus.length > 0
              ? gpus
                  .map(
                    (g) =>
                      `${g.name || g.vendor || "GPU"}${g.vramBytes ? ` (${fmtBytes(g.vramBytes)})` : ""}`,
                  )
                  .join(", ")
                  : deepReady
                    ? "— none detected"
                    : "— detecting…"}
          </strong>
        </div>

        <div className="session-detail">
          <span>NPU</span>
          <strong title={npu ? `${npu.vendor || ""} ${npu.driverVersion || ""} (${npu.detectedBy || ""})`.trim() : undefined}>
            {npu
              ? `${npu.name || "Neural accelerator"}${npu.status ? ` · ${npu.status}` : ""}`
              : deepReady
                ? "— none detected"
                : "— detecting…"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Storage</span>
          <strong>
            {storage.freeBytes
              ? `${fmtBytes(storage.freeBytes)} free of ${fmtBytes(storage.totalBytes)}`
              : "—"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Engine backend</span>
          <strong>
            {device.backend?.engineBinary
              ? `provisioned${device.backend.vulkan ? " · Vulkan" : ""}`
              : "not yet provisioned (auto-download on first run)"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Identity</span>
          <strong title={identity.appUserModelId}>
            {identity.product || "SHEYTAN"} ·{" "}
            {identity.appUserModelId || identity.platform || "—"}
          </strong>
        </div>
      </div>
    </div>
  );
}

function RuntimeCard({ env }: { env: EnvironmentPayload | null }) {
  // v1.2.3: live engine-asset download progress from the shared engine
  // snapshot (polled by the runtime store when this surface is owned).
  const engineDownload = useRuntimeStore((state) => state.engine?.download);
  if (!env || !env.runtime) return null;

  const { runtime } = env;
  const vision = runtime.vision ?? { state: "" };
  const recommendation = env.recommendation;

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">RUNTIME</span>
          <strong>{runtime.backend || "engine"}</strong>
        </div>
        <span
          className={`settings-chip chip-${runtime.verified ? "good" : "warn"}`}
        >
          {runtime.verified ? "✓ verified" : "not verified"}
        </span>
      </div>

      {engineDownload ? (
        <DownloadProgressPanel progress={engineDownload} compact />
      ) : null}

      <div className="env-grid">
        <div className="session-detail">
          <span>Engine</span>
          <strong>
            {runtime.engineState || "—"}
            {runtime.enginePhase && runtime.enginePhase !== "ready"
              ? ` · ${runtime.enginePhase}`
              : ""}
          </strong>
        </div>

        <div className="session-detail">
          <span>Model</span>
          <strong>{runtime.model || "—"}</strong>
        </div>

        <div className="session-detail">
          <span>Context</span>
          <strong>
            {runtime.context ? `${runtime.context.toLocaleString()} tok` : "—"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Vision</span>
          <strong
            title={vision.reason}
            className={`vision-value tone-${
              vision.state === "ready"
                ? "good"
                : vision.state === "failed"
                  ? "bad"
                  : "warn"
            }`}
          >
            {vision.state ? `${vision.active ? "✓ " : ""}${vision.state}` : "—"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Engine tag</span>
          <strong>{runtime.engineTag || "—"}</strong>
        </div>

        <div className="session-detail">
          <span>Profile</span>
          <strong>{recommendation?.task || "chat"}</strong>
        </div>
      </div>
    </div>
  );
}

function RecommendationCard({ env }: { env: EnvironmentPayload | null }) {
  const [applying, setApplying] = useState(false);
  const [appliedAt, setAppliedAt] = useState<string | null>(null);

  // v1.2.2: applying a recommendation races navigation — a component
  // unmounted mid-apply must never re-enter its dead state setters.
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;

    return () => {
      aliveRef.current = false;
    };
  }, []);

  // v1.2.5 REPAIR: every hook must run on EVERY render. The early return
  // used to sit ABOVE the useCallback below — when the environment payload
  // arrived, React saw one more hook than on the first (null-env) render
  // and threw "Rendered more hooks than during the previous render", which
  // crashed the System Centre on every load that had data. The optional
  // data is now read BEFORE the hook and the early return comes after ALL
  // hooks; the defensive reads stay.
  const rec = env?.recommendation ?? null;
  const task = rec?.task || "chat";

  const apply = useCallback(async () => {
    setApplying(true);

    try {
      const payload = await api.recommendation("", task);
      const r = payload.recommended;
      if (!r) return;

      await api.updateConfig({
        llm: {
          numCtx: r.context,
          numThread: r.threads,
          numGpu: r.gpuLayers,
          ubatchSize: r.ubatchSize,
        },
        gpuAutoOffload: r.gpuAutoOffload,
        flashAttention: r.flashAttention,
        kvCacheQuant: r.kvCacheQuant,
        visionMmprojOffload: r.mmprojOffload,
        runtimeProfile: r.task,
      });

      if (!aliveRef.current) return;

      setAppliedAt(new Date().toLocaleTimeString());
    } catch {
      // Config errors surface through the config surfaces elsewhere.
    } finally {
      if (aliveRef.current) {
        setApplying(false);
      }
    }
  }, [task]);

  // The early return now lives AFTER every hook (hooks-order contract).
  if (!rec) return null;

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">RECOMMENDATION</span>
          <strong>{rec.summary}</strong>
        </div>
        <span
          className={`settings-chip chip-${rec.applied || appliedAt ? "good" : "neutral"}`}
        >
          {appliedAt
            ? `Applied ✓ ${appliedAt}`
            : rec.applied
              ? "✓ optimal"
              : "available"}
        </span>
      </div>

      {rec.reasons && rec.reasons.length > 0 ? (
        <details className="env-reasons">
          <summary>Why ({rec.reasons.length})</summary>

          <ul>
            {rec.reasons.map((reason, index) => (
              <li key={index}>{reason}</li>
            ))}
          </ul>
        </details>
      ) : null}

      {!rec.applied ? (
        <div className="header-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={() => void apply()}
            disabled={applying}
          >
            {applying ? "Applying…" : "Apply recommendations"}
          </button>

          <span className="runtime-hint">
            Predicted values — measure the effect afterwards in Settings →
            Performance → Live metrics.
          </span>
        </div>
      ) : null}
    </div>
  );
}

// v1.8.0: Runtime Governor — the adaptive runtime intelligence surface.
// Every value comes from /api/governor (one composed read model over the
// EXISTING authorities). A fact the platform cannot measure renders as
// "—" and is NAMED in the unknowns list — the surface never invents a
// number, and the reasons always state WHY the policy is what it is.
const GOVERNOR_LEVEL_TONE: Record<string, string> = {
  ok: "good",
  warning: "warn",
  high_pressure: "bad",
  critical_pressure: "bad",
};

const GOVERNOR_LEVEL_LABEL: Record<string, string> = {
  ok: "GREEN — nominal",
  warning: "YELLOW — elevated",
  high_pressure: "RED — high pressure",
  critical_pressure: "EMERGENCY — critical",
};

function GovernorCard() {
  const govResource = useResource<GovernorPayload>("governor", (signal) =>
    api.governor(signal),
  );

  const gov = govResource.data;

  if (!gov) {
    return (
      <div className="settings-card">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">RUNTIME GOVERNOR</span>
            <strong>Reading runtime policy…</strong>
          </div>
        </div>
      </div>
    );
  }

  if (!gov.governor.available || !gov.governor.state || !gov.governor.envelope) {
    return (
      <div className="settings-card">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">RUNTIME GOVERNOR</span>
            <strong>Unavailable</strong>
          </div>
        </div>
        <p className="env-loading">
          {gov.governor.reason ??
            "The runtime governor is not wired in this process."}
        </p>
      </div>
    );
  }

  const { state, envelope } = gov.governor;
  const tone = GOVERNOR_LEVEL_TONE[state.level] ?? "neutral";
  const levelLabel = GOVERNOR_LEVEL_LABEL[state.level] ?? state.level;
  const sustained =
    state.sustained > 0 ? `${Math.round(state.sustained / 1000)}s` : "—";
  const reasons = Array.isArray(envelope.reasons) ? envelope.reasons : [];
  const unknowns = Array.isArray(state.unknowns) ? state.unknowns : [];

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">RUNTIME GOVERNOR</span>
          <strong>Adaptive runtime policy</strong>
        </div>
        <span className={`settings-chip chip-${tone}`}>
          {levelLabel}
          {state.sustained > 0 ? ` · ${sustained}` : ""}
        </span>
      </div>

      <div className="health-checks">
        <details className={`health-check state-ok`}>
          <summary>
            <span className="health-check-label">Resource state</span>
            <span className="health-check-state">
              {state.availableKnown
                ? `${fmtBytes(state.ramAvailableBytes)} free / ${fmtBytes(state.ramTotalBytes)}`
                : "RAM unknown"}
              {state.cpuLoadKnown &&
              typeof state.cpuRollingAvg === "number" &&
              state.cpuRollingAvg > 0
                ? ` · CPU ${Math.round(state.cpuRollingAvg)}% rolling`
                : " · CPU —"}
            </span>
          </summary>
          <p className="health-check-evidence">
            Process RSS{" "}
            {typeof state.procRssBytes === "number" && state.procRssBytes > 0
              ? fmtBytes(state.procRssBytes)
              : "—"}
            {" · "}
            Engine{" "}
            {state.engineRunning
              ? `running${state.engineRssKnown ? ` (${fmtBytes(state.engineRssBytes)} RSS)` : ""}`
              : "idle"}
            {" · "}
            Active runs {state.activeRuns}
          </p>
        </details>

        <details className={`health-check state-ok`}>
          <summary>
            <span className="health-check-label">Execution envelope</span>
            <span className="health-check-state">
              {envelope.adjustmentClass === "none"
                ? "no adjustment"
                : `${envelope.adjustmentClass} adjustment`}
            </span>
          </summary>
          <p className="health-check-evidence">
            Heavyweight admission {envelope.admitHeavyweight ? "open" : "deferred"}
            {" · "}
            Background {envelope.reduceBackground ? "reduced" : "normal"}
            {" · "}
            Context work {envelope.reduceContextWork ? "minimal next run" : "normal"}
            {" · "}
            Tool concurrency{" "}
            {envelope.reduceToolConcurrency ? "reduced" : "normal"}
            {envelope.residentMemoryBudgetBytes &&
            envelope.residentMemoryBudgetBytes > 0
              ? ` · resident budget ${fmtBytes(envelope.residentMemoryBudgetBytes)}`
              : ""}
          </p>
        </details>

        {reasons.map((reason, i) => (
          <p key={i} className="health-check-evidence">
            {reason}
          </p>
        ))}

        {unknowns.length > 0 ? (
          <details className="health-check state-warn">
            <summary>
              <span className="health-check-label">Unknowns</span>
              <span className="health-check-state">{unknowns.length}</span>
            </summary>
            {unknowns.map((u, i) => (
              <p key={i} className="health-check-evidence">
                {u}
              </p>
            ))}
          </details>
        ) : null}
      </div>

      <span className="runtime-hint">
        The governor owns POLICY only — the pressure protection path, engine
        lifecycle and scheduler keep executing. Maximum useful capability
        while keeping the host responsive.
      </span>
    </div>
  );
}

function HealthCard() {
  // v1.2.6 continuation: HealthCard now rides the SHARED resource layer
  // ("health") instead of its own uncached fetch. The old code issued a
  // fresh /api/health request on every System Centre mount even when
  // another surface (or a previous visit seconds ago) already had the
  // data — the exact duplicate-request pattern the resource layer exists
  // to remove. Dedup + TTL + last-known-good + eventual cancellation now
  // apply; the Refresh button force-refreshes through the same layer.
  const healthResource = useResource<HealthPayload>("health", (signal) =>
    api.health(signal),
  );

  const health = healthResource.data;

  const load = useCallback(() => {
    healthResource.refresh();
  }, [healthResource]);

  // v1.2.2: defensive checks — a malformed/absent checks array renders
  // honestly instead of throwing.
  const checks = Array.isArray(health?.checks) ? health.checks : [];
  const overall = health?.overall ?? "unknown";

  if (!health) {
    return (
      <div className="settings-card">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">SYSTEM HEALTH</span>
            <strong>Verifying…</strong>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">SYSTEM HEALTH</span>
          <strong>
            {overall === "ok"
              ? "All verified checks pass"
              : overall === "warn"
                ? "Verified, with warnings"
                : "Attention required"}
          </strong>
        </div>

        <div className="panel-heading-actions">
          <button type="button" className="text-button" onClick={load}>
            Refresh
          </button>

          <span
            className={`settings-chip chip-${HEALTH_TONE[overall] ?? "neutral"}`}
          >
            {checks.filter((c) => c.state === "ok").length}/{checks.length} ok
          </span>
        </div>
      </div>

      <div className="health-checks">
        {checks.map((check) => (
          <details
            key={check.id}
            className={`health-check state-${check.state}`}
          >
            <summary>
              <span className="health-check-label">
                <span
                  className={`health-check-symbol tone-${HEALTH_TONE[check.state] ?? "neutral"}`}
                  aria-hidden="true"
                >
                  {HEALTH_SYMBOL[check.state] ?? "?"}
                </span>
                {check.label}
              </span>

              <span
                className={`health-check-state tone-${HEALTH_TONE[check.state] ?? "neutral"}`}
              >
                {check.state}
              </span>
            </summary>

            <p className="health-check-evidence">{check.evidence}</p>
          </details>
        ))}
      </div>

      <span className="runtime-hint">
        Every state is evidence-backed — open any check to see the underlying
        proof. Green never means assumed.
      </span>
    </div>
  );
}

const SystemPanel = function SystemPanel() {
  // v1.2.6 PROGRESSIVE SYSTEM CENTRE — the environment arrives as a FAST
  // snapshot (backend hardware probe no longer blocks the response) and
  // fills in GPU/NPU/driver facts when the background deep probe lands.
  // The health probe loads AFTER the environment (staggered, independent,
  // cancellable) so a slow health check can never stall the device card.
  const envResource = useResource<EnvironmentPayload>(
    "environment",
    (signal) => api.environment(signal),
  );

  const env = envResource.data;
  const status: "loading" | "ready" | "error" =
    envResource.state === "error"
      ? "error"
      : envResource.state === "ready" || envResource.state === "stale"
        ? "ready"
        : "loading";

  // deepReady=false → the backend is still probing GPU/NPU/driver facts in
  // the background; poll the shared resource until it lands (bounded).
  const deepReady = env?.device?.deepReady ?? false;

  useEffect(() => {
    if (status !== "ready" || deepReady) {
      return;
    }

    let cancelled = false;

    const timer = window.setInterval(() => {
      if (cancelled) {
        return;
      }

      void envResource.refresh();
    }, 2000);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [status, deepReady, envResource]);

  const load = useCallback(() => {
    envResource.refresh();
  }, [envResource]);

  return (
    <section className="system-panel">
      {/* v1.9.0: the AI System selector/editor and the durable Goal
          surface — user-owned configuration and long-horizon state live
          in the System Centre, reusing the existing card grammar. */}
      <AISystemsCard />
      <GoalsCard />
      <DeviceCard env={env} status={status} onRetry={load} />
      <RuntimeCard env={env} />
      <RecommendationCard env={env} />
      <GovernorCard />
      <HealthCard />
    </section>
  );
};

export default SystemPanel;
