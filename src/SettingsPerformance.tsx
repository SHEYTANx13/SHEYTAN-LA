import { useEffect, useState } from "react";

import { api, type LLMConfig, type NetDiagResult, type PerfSnapshot, type RuntimeConfig } from "./api";
import {
  engineBackendSummary,
  provisionBusyLabel,
  variantLabel,
  type BackendAction,
} from "./engine-backend";
import {
  Chip,
  FieldLabel,
  RestartChip,
  fmtBytes,
  fmtDelta,
  fmtNA,
  fmtPct,
} from "./settings-shared";
import LogViewer from "./LogViewer";

// SettingsPerformance.tsx — v1.1.7 Performance / Network / Logs cards.
//
// Every displayed number is a REAL measurement from /api/perf, /api/logs
// or /api/netcheck; anything unmeasurable renders as N/A. Recommendations
// are derived from detected hardware + model + engine capability and are
// applied ONLY through explicit Apply buttons.

export interface PerfBaseline {
  label: string;
  at: string;
  genTokPerSec?: number;
  promptTokPerSec?: number;
  ttftSeconds?: number;
  cpuPercent?: number | null;
  gpuPercent?: number | null;
}

export interface PerformanceCardProps {
  config: RuntimeConfig;
  perf: PerfSnapshot | null;
  baseline: PerfBaseline | null;
  save: (patch: Record<string, unknown>) => Promise<void>;
  updateLocalLLM: <K extends keyof LLMConfig>(key: K, value: LLMConfig[K]) => void;
  clearBaseline: () => void;
}

// ---------------------------------------------------------------------------
// v1.5.0 — Performance mode (AUTO / MANUAL)
// ---------------------------------------------------------------------------

// PerformanceModeCard is the single runtime concept that decides who owns
// the performance settings:
//
//   AUTO   — SHEYTAN calculates and applies the profile from measured
//            hardware + the selected model (the ONE recommendation
//            engine), recalculated on every model change, with a bounded
//            real calibration whose verified winner is retained.
//   MANUAL — explicit user values are preserved verbatim; model changes
//            never overwrite them.
//
// The card states the honest auto-tuned summary ("Auto tuned — based on
// measured hardware + selected model", or "not benchmarked" when no real
// calibration ran) and offers Re-analyze, which drives the SAME backend
// selection flow for the CURRENT model (recalculating the profile). It
// never claims "optimal" without measured evidence.
export function PerformanceModeCard({
  config,
  save,
  onReanalyzed,
}: {
  config: RuntimeConfig;
  save: (patch: Record<string, unknown>) => Promise<void>;
  onReanalyzed?: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mode = config.performanceMode === "manual" ? "manual" : "auto";

  async function switchMode(next: "auto" | "manual") {
    if (busy || next === mode) {
      return;
    }

    setBusy(true);
    setError(null);

    try {
      await save({ performanceMode: next });
    } catch (modeError) {
      setError(
        modeError instanceof Error ? modeError.message : "Unable to save the performance mode.",
      );
    } finally {
      setBusy(false);
    }
  }

  async function reanalyze() {
    if (busy || !config.model) {
      return;
    }

    setBusy(true);
    setError(null);

    try {
      // The SAME backend selection flow for the CURRENT model — the
      // profile is recalculated from measured evidence (AUTO only).
      // v1.5.1: the CURRENTLY INTENDED task is preserved (sent back
      // verbatim) — re-analysis must never silently retune a different
      // surface's profile than the one the user is on (spec §5).
      await api.selectModel(config.model, config.runtimeProfile || undefined);
      onReanalyzed?.();
    } catch (reanalyzeError) {
      setError(
        reanalyzeError instanceof Error
          ? reanalyzeError.message
          : "Unable to re-analyze the current model.",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">PERFORMANCE MODE</span>
          <h3>Who owns the runtime settings</h3>
        </div>
        <Chip tone={mode === "auto" ? "good" : "warn"}>
          {mode === "auto" ? "Auto tuned" : "Manual"}
        </Chip>
      </div>

      <div className="settings-rows">
        <div className="settings-row">
          <div>
            <FieldLabel
              name="Performance mode"
              tip="AUTO calculates and applies settings automatically from measured hardware + the selected model. MANUAL preserves your explicit values."
            />
            <div className="segmented" role="radiogroup" aria-label="Performance mode">
              <button
                type="button"
                className={mode === "auto" ? "active" : ""}
                onClick={() => void switchMode("auto")}
                disabled={busy}
                aria-pressed={mode === "auto"}
              >
                AUTO
              </button>
              <button
                type="button"
                className={mode === "manual" ? "active" : ""}
                onClick={() => void switchMode("manual")}
                disabled={busy}
                aria-pressed={mode === "manual"}
              >
                MANUAL
              </button>
            </div>
          </div>

          <div className="settings-row-actions">
            {mode === "auto" ? (
              <>
                <span className="runtime-hint">
                  Auto tuned — based on measured hardware + selected model
                </span>
                <button
                  type="button"
                  className="secondary-button"
                  onClick={() => void reanalyze()}
                  disabled={busy || !config.model}
                  title="Recalculate the automatic profile for the current model (measured hardware + engine capabilities)."
                >
                  Re-analyze
                </button>
              </>
            ) : (
              <span className="runtime-hint">
                Manual — your explicit settings are preserved on model changes.
              </span>
            )}
          </div>
        </div>
      </div>

      {error ? <p className="settings-error">{error}</p> : null}
    </section>
  );
}

// ---------------------------------------------------------------------------
// Engine profile status — "why compatibility mode?"
// ---------------------------------------------------------------------------

export function EngineProfileCard({ perf }: { perf: PerfSnapshot | null }) {
  const compat = perf?.compat;

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">ENGINE PROFILE</span>
          <h3>Active engine configuration</h3>
        </div>
        {compat?.optimised ? (
          <Chip tone="good">Optimised</Chip>
        ) : (
          <Chip tone="warn">
            {compat ? `Compatibility mode ${compat.level}` : "Unknown"}
          </Chip>
        )}
      </div>

      {!perf ? (
        <span className="settings-note">Engine profile unavailable.</span>
      ) : (
        <div className="engine-profile">
          <div className="profile-facts">
            <div>
              <span>Backend</span>
              <strong>{perf.backend}</strong>
            </div>
            <div>
              <span>State</span>
              <strong>{perf.engineState}</strong>
            </div>
            <div>
              <span>Profile</span>
              <strong>{compat?.name ?? "unknown"}</strong>
            </div>
            <div>
              <span>Model</span>
              <strong>{perf.model || "N/A"}</strong>
            </div>
          </div>

          {!compat?.optimised && compat?.reason ? (
            <div className="profile-explain">
              <strong>Why compatibility mode?</strong>
              <p>
                A launch option was rejected by this engine build
                ({compat.reason}). Only that option was repaired; the rest of
                the speed profile stays active. The engine retries the
                full-speed profile automatically once the capability profile
                moves on.
              </p>
            </div>
          ) : null}

          {!compat?.optimised && compat?.changes?.length ? (
            <div className="profile-changes">
              <strong>Removed options</strong>
              {compat.changes.map((change) => (
                <div key={change}>{change}</div>
              ))}
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}

// ---------------------------------------------------------------------------
// Recommended settings — explicit apply, never silent overwrite
// ---------------------------------------------------------------------------

export function RecommendedCard({
  config,
  perf,
  save,
}: {
  config: RuntimeConfig;
  perf: PerfSnapshot | null;
  save: PerformanceCardProps["save"];
}) {
  const rec = perf?.recommended;
  const [applied, setApplied] = useState<string | null>(null);

  if (!rec) {
    return (
      <section className="settings-card">
        <div className="settings-card-heading">
          <div>
            <span className="eyebrow">RECOMMENDED</span>
            <h3>Suggested for this machine</h3>
          </div>
        </div>
        <span className="settings-note">
          Hardware recommendations unavailable right now.
        </span>
      </section>
    );
  }

  async function apply(key: string, patch: Record<string, unknown>) {
    await save(patch);
    setApplied(key);
    window.setTimeout(() => setApplied((current) => (current === key ? null : current)), 4000);
  }

  const rows: {
    key: string;
    name: string;
    tip: string;
    current: string;
    recommended: string;
    patch: Record<string, unknown> | null;
  }[] = [
    {
      key: "threads",
      name: "CPU threads",
      tip: "Generation threads. Physical cores are fastest for text generation; one thread per physical core avoids SMT contention.",
      current: String(config.llm.numThread || "Auto"),
      recommended: String(rec.threads || "Auto"),
      patch: rec.threads ? { llm: { ...config.llm, numThread: rec.threads } } : null,
    },
    {
      key: "gpu",
      name: "GPU offload",
      tip: "Runs model layers on the GPU when one is available. The single biggest speed win — a GPU with enough VRAM is several times faster than CPU.",
      current: config.gpuAutoOffload ? "Automatic" : `Fixed (${config.llm.numGpu || 0})`,
      recommended: rec.gpuAutoOffload
        ? rec.gpuLayers
          ? `Automatic (up to ${rec.gpuLayers})`
          : "Automatic"
        : "Off (no GPU detected)",
      patch: { gpuAutoOffload: rec.gpuAutoOffload },
    },
    {
      key: "flashAttention",
      name: "Flash attention",
      tip: "Speeds up attention when supported and may reduce memory use.",
      current: config.flashAttention ? "On" : "Off",
      recommended: rec.flashAttention ? "On" : `Off (${rec.flashAttnState})`,
      patch: { flashAttention: rec.flashAttention },
    },
    {
      key: "ubatch",
      name: "U-Batch size",
      tip: "How many tokens are processed per physical batch during prompt ingestion. Larger batches speed up long prompts at the cost of more memory.",
      current: String(config.llm.numBatch || "Auto"),
      recommended: String(rec.ubatchSize || "Auto"),
      patch: rec.ubatchSize ? { llm: { ...config.llm, numBatch: rec.ubatchSize } } : null,
    },
    {
      key: "cacheReuse",
      name: "Cache reuse",
      tip: "Reuses the cached prompt prefix between agent turns, collapsing repeated prompt processing — dramatically lower wait time per follow-up message.",
      current: String(config.cacheReuse || 0),
      recommended: String(rec.cacheReuse || 0),
      patch: { cacheReuse: rec.cacheReuse },
    },
    // v1.6.0 (spec §7): the "Context size" recommendation row is REMOVED
    // from the user-facing profile table. The physical context remains
    // engine/model-bounded and is derived AUTOMATICALLY by the selection
    // and recommendation engine (measured evidence, applied with the
    // runtime profile) — the user never manages a context number. Logical
    // conversation continuity is unbounded through history, summaries,
    // retrieval, chunking, rollover and memory.
  ];

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">RECOMMENDED</span>
          <h3>Suggested for this machine</h3>
        </div>
        <span className="settings-card-value">
          detected hardware + model + engine
        </span>
      </div>

      <div className="recommend-list">
        {rows.map((row) => {
          const differs = row.current !== row.recommended;

          return (
            <div key={row.key} className="recommend-row">
              <div className="recommend-info">
                <strong>
                  {row.name}
                  <span className="settings-tip" data-tip={row.tip} tabIndex={0} aria-label={row.tip}>?</span>
                </strong>
                <span className="recommend-values">
                  Current: <em>{row.current}</em>
                  <span className="recommend-arrow">→</span>
                  Recommended: <em>{row.recommended}</em>
                </span>
              </div>
              {row.patch && differs ? (
                <button
                  type="button"
                  className="secondary-button"
                  onClick={() => void apply(row.key, row.patch as Record<string, unknown>)}
                >
                  {applied === row.key ? "Applied ✓" : "Apply"}
                </button>
              ) : (
                <Chip tone={differs ? "neutral" : "good"}>
                  {differs ? "Manual" : "OK"}
                </Chip>
              )}
            </div>
          );
        })}
      </div>

      {rec.notes?.length ? (
        <div className="hardware-warnings">
          {rec.notes.map((note) => (
            <div key={note}>{note}</div>
          ))}
        </div>
      ) : null}

      <span className="settings-note">
        Applying a recommendation saves the setting and restarts the local
        engine when required. Nothing is changed silently.
      </span>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Live metrics — real measurements only
// ---------------------------------------------------------------------------

function MetricRow({
  name,
  value,
}: {
  name: string;
  value: string;
}) {
  return (
    <div className="metric-row">
      <span>{name}</span>
      <strong>{value}</strong>
    </div>
  );
}

export function LiveMetricsCard({
  perf,
  baseline,
  clearBaseline,
}: {
  perf: PerfSnapshot | null;
  baseline: PerfBaseline | null;
  clearBaseline: () => void;
}) {
  const gen = perf?.generation;

  const cpu = perf?.cpuPercent ?? null;
  const gpu = perf?.gpuPercent ?? null;
  const ram = perf?.ram ?? null;
  const vram = perf?.vram ?? null;

  const genTps = gen?.hasSample ? gen.genTokPerSec ?? null : null;
  const promptTps = gen?.hasSample ? gen.promptTokPerSec ?? null : null;
  const ttft = gen?.hasSample ? gen.ttftSeconds ?? null : null;
  const ctx = perf?.context ?? null;

  return (
    <section className="settings-card">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">LIVE METRICS</span>
          <h3>Measured performance</h3>
        </div>
        <span className="settings-card-value">
          {perf ? perf.generatedAt.slice(11, 19) + " UTC" : "—"}
        </span>
      </div>

      <div className="metrics-grid">
        <MetricRow name="CPU" value={fmtPct(cpu)} />
        <MetricRow name="GPU" value={fmtPct(gpu)} />
        <MetricRow
          name="RAM"
          value={ram ? `${fmtBytes(ram.usedBytes)} / ${fmtBytes(ram.totalBytes)}` : "N/A"}
        />
        <MetricRow
          name="VRAM"
          value={vram ? `${vram.usedMb.toFixed(0)} / ${vram.totalMb.toFixed(0)} MB` : "N/A"}
        />
        <MetricRow
          name="Prompt"
          value={promptTps !== null ? `${promptTps.toFixed(1)} tok/s` : "N/A"}
        />
        <MetricRow
          name="Generate"
          value={genTps !== null ? `${genTps.toFixed(1)} tok/s` : "N/A"}
        />
        <MetricRow
          name="TTFT"
          value={ttft !== null ? `${ttft.toFixed(2)} s` : "N/A"}
        />
        <MetricRow
          name="Context"
          value={ctx ? `${ctx.usedTokens} / ${ctx.total} tok` : "N/A"}
        />
        <MetricRow name="Engine" value={perf?.compat.optimised ? "Optimised" : `Compat ${perf?.compat.level ?? "?"}`} />
        <MetricRow name="Backend" value={perf?.backend ?? "N/A"} />
      </div>

      <span className="settings-note">
        N/A means the value was not measurable on this machine — nothing is
        estimated. tok/s and TTFT come from the most recent generation.
      </span>

      {baseline ? (
        <div className="before-after">
          <div className="before-after-heading">
            <strong>Before / after — {baseline.label}</strong>
            <button type="button" className="secondary-button" onClick={clearBaseline}>
              Clear
            </button>
          </div>
          <div className="metrics-grid">
            <MetricRow
              name="Generate"
              value={
                baseline.genTokPerSec && genTps
                  ? `${baseline.genTokPerSec.toFixed(1)} → ${genTps.toFixed(1)} (${fmtDelta(baseline.genTokPerSec, genTps)})`
                  : `${fmtNA(baseline.genTokPerSec ?? null)} → ${fmtNA(genTps)}`
              }
            />
            <MetricRow
              name="Prompt"
              value={
                baseline.promptTokPerSec && promptTps
                  ? `${baseline.promptTokPerSec.toFixed(1)} → ${promptTps.toFixed(1)} (${fmtDelta(baseline.promptTokPerSec, promptTps)})`
                  : `${fmtNA(baseline.promptTokPerSec ?? null)} → ${fmtNA(promptTps)}`
              }
            />
            <MetricRow
              name="TTFT"
              value={
                baseline.ttftSeconds && ttft
                  ? `${baseline.ttftSeconds.toFixed(2)} → ${ttft.toFixed(2)} s (${fmtDelta(ttft, baseline.ttftSeconds)})`
                  : `${fmtNA(baseline.ttftSeconds ?? null)} → ${fmtNA(ttft)}`
              }
            />
            <MetricRow
              name="CPU"
              value={
                baseline.cpuPercent !== null && baseline.cpuPercent !== undefined && cpu !== null
                  ? `${baseline.cpuPercent.toFixed(0)} → ${cpu.toFixed(0)}%`
                  : "N/A"
              }
            />
            <MetricRow
              name="GPU"
              value={
                baseline.gpuPercent !== null && baseline.gpuPercent !== undefined && gpu !== null
                  ? `${baseline.gpuPercent.toFixed(0)} → ${gpu.toFixed(0)}%`
                  : "N/A"
              }
            />
          </div>
          <span className="settings-note">
            Comparison values appear once a new generation has run after the
            change.
          </span>
        </div>
      ) : null}
    </section>
  );
}

// ---------------------------------------------------------------------------
// Network diagnostics — local-first, informational only
// ---------------------------------------------------------------------------

export function NetworkCard() {
  const [result, setResult] = useState<NetDiagResult | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run() {
    setChecking(true);
    setError(null);
    try {
      setResult(await api.netcheck());
    } catch (diagError) {
      setError(diagError instanceof Error ? diagError.message : "Connection check failed.");
    } finally {
      setChecking(false);
    }
  }

  const tone =
    result?.state === "Excellent" || result?.state === "Good"
      ? "good"
      : result?.state === "Offline"
        ? "bad"
        : "warn";

  return (
    <section className="settings-card">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">NETWORK</span>
          <h3>Connection diagnostics</h3>
        </div>
        {result ? <Chip tone={tone}>{result.state}</Chip> : null}
      </div>

      <div className="settings-form-grid">
        <div className="settings-field">
          <FieldLabel
            name="Connection check"
            tip="Runs one bounded check: DNS, HTTPS reachability, latency and a short stability sample. SHEYTAN works fully offline — this only reports status."
          />
          <button
            type="button"
            className="secondary-button"
            onClick={() => void run()}
            disabled={checking}
          >
            {checking ? "Checking…" : result ? "Run again" : "Run check"}
          </button>
        </div>
      </div>

      {error ? <div className="settings-status error">{error}</div> : null}

      {result ? (
        <div className="metrics-grid">
          <MetricRow name="State" value={result.state} />
          <MetricRow
            name="Latency"
            value={result.latencyMs ? `${result.latencyMs.toFixed(0)} ms` : "N/A"}
          />
          <MetricRow name="DNS" value={result.dnsOk ? "OK" : "Failed"} />
          <MetricRow name="HTTPS" value={result.httpsOk ? "OK" : "Failed"} />
          <MetricRow
            name="Endpoint"
            value={result.endpointOk ? "Reachable" : "Unreachable"}
          />
          <MetricRow name="Check took" value={`${result.totalMs} ms`} />
        </div>
      ) : null}

      {result?.reason ? (
        <div className="hardware-warnings">
          <div>{result.reason}</div>
        </div>
      ) : null}

      <span className="settings-note">
        Local-first: an offline connection never blocks the agent — only web
        tools and remote providers are affected.
      </span>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Logs / Diagnostics
// ---------------------------------------------------------------------------

export function LogsCard() {
  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">LOGS / DIAGNOSTICS</span>
          <h3>Live application log</h3>
        </div>
      </div>
      <LogViewer />
    </section>
  );
}

// ---------------------------------------------------------------------------
// Engine card (advanced) — capability-aware speed options
// ---------------------------------------------------------------------------

export function EngineCard({
  config,
  perf,
  save,
  updateLocalLLM,
  updateConfigLocal,
}: {
  config: RuntimeConfig;
  perf: PerfSnapshot | null;
  save: PerformanceCardProps["save"];
  updateLocalLLM: PerformanceCardProps["updateLocalLLM"];
  updateConfigLocal: <K extends keyof RuntimeConfig>(
    key: K,
    value: RuntimeConfig[K],
  ) => void;
}) {
  const flashState = perf?.recommended?.flashAttnState ?? null;

  const flashChips = (
    <>
      {flashState ? (
        <Chip tone={flashState === "Supported" ? "good" : "bad"}>
          {flashState}
        </Chip>
      ) : null}
      <RestartChip />
    </>
  );

  return (
    <section className="settings-card">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">ENGINE — ADVANCED</span>
          <h3>llama.cpp</h3>
        </div>
      </div>

      <div className="settings-form-grid">
        <label className="settings-field">
          <FieldLabel
            name="Binary path"
            tip="Which llama.cpp server binary to launch. Leave empty to use the managed engine that SHEYTAN downloads and keeps up to date."
          />
          <input
            value={config.llamaBinPath}
            onChange={(event) =>
              updateConfigLocal("llamaBinPath", event.target.value)
            }
            onBlur={() => void save({ llamaBinPath: config.llamaBinPath })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="Host"
            tip="Address the engine listens on. 127.0.0.1 keeps it local to this machine."
            chips={<RestartChip />}
          />
          <input
            value={config.llamaHost}
            onChange={(event) =>
              updateConfigLocal("llamaHost", event.target.value)
            }
            onBlur={() => void save({ llamaHost: config.llamaHost })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="Port"
            tip="Port the engine listens on. Change it only if another program already uses this port."
            chips={<RestartChip />}
          />
          <input
            type="number"
            value={config.llamaPort}
            onChange={(event) =>
              updateConfigLocal("llamaPort", Number(event.target.value) || 0)
            }
            onBlur={() => void save({ llamaPort: config.llamaPort })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="Extra arguments"
            tip="Raw command-line flags passed to llama.cpp. For advanced users — invalid flags stop the engine from starting."
            chips={<RestartChip />}
          />
          <input
            value={config.llamaExtraArgs}
            onChange={(event) =>
              updateConfigLocal("llamaExtraArgs", event.target.value)
            }
            onBlur={() => void save({ llamaExtraArgs: config.llamaExtraArgs })}
          />
        </label>

        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={config.llamaAutoStart}
            onChange={(event) => {
              const value = event.target.checked;
              updateConfigLocal("llamaAutoStart", value);
              void save({ llamaAutoStart: value });
            }}
          />
          <span>Auto-start local engine</span>
        </label>

        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={config.gpuAutoOffload}
            onChange={(event) => {
              const value = event.target.checked;
              updateConfigLocal("gpuAutoOffload", value);
              void save({ gpuAutoOffload: value });
            }}
          />
          <span>
            Automatic GPU offload
            <span
              className="settings-tip"
              data-tip="Lets SHEYTAN put as many model layers on the GPU as fit in its memory — the biggest speed win when a GPU is present."
              tabIndex={0}
              aria-label="GPU offload tooltip"
            >
              ?
            </span>
          </span>
          <RestartChip />
        </label>

        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={config.flashAttention}
            onChange={(event) => {
              const value = event.target.checked;
              updateConfigLocal("flashAttention", value);
              void save({ flashAttention: value });
            }}
          />
          <span>Flash attention</span>
          {flashChips}
        </label>

        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={config.mlock}
            onChange={(event) => {
              const value = event.target.checked;
              updateConfigLocal("mlock", value);
              void save({ mlock: value });
            }}
          />
          <span>
            Pin model in RAM
            <span
              className="settings-tip"
              data-tip="Keeps the model in physical memory so the OS cannot evict it mid-chat. Costs a large, permanent RAM reservation."
              tabIndex={0}
              aria-label="mlock tooltip"
            >
              ?
            </span>
          </span>
          <RestartChip />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="Cache reuse"
            tip="How much of the previous prompt prefix to reuse between turns. Higher values make follow-up agent turns much faster; 0 disables reuse."
            chips={<RestartChip />}
          />
          <input
            type="number"
            min="0"
            step="512"
            value={config.cacheReuse}
            onChange={(event) =>
              updateConfigLocal("cacheReuse", Number(event.target.value) || 0)
            }
            onBlur={() => void save({ cacheReuse: config.cacheReuse })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="U-Batch size"
            tip="Tokens processed per physical batch while reading a prompt. Larger batches speed up long prompts and use more memory."
            chips={<RestartChip />}
          />
          <input
            type="number"
            min="32"
            step="32"
            value={config.llm.numBatch}
            onChange={(event) =>
              updateLocalLLM("numBatch", Number(event.target.value) || 0)
            }
            onBlur={() => void save({ llm: config.llm })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="GPU layers"
            tip="How many model layers run on the GPU. 0 with automatic offload lets SHEYTAN decide; a fixed number overrides that."
            chips={<RestartChip />}
          />
          <input
            type="number"
            min="0"
            value={config.llm.numGpu}
            onChange={(event) =>
              updateLocalLLM("numGpu", Number(event.target.value) || 0)
            }
            onBlur={() => void save({ llm: config.llm })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="Threads"
            tip="CPU threads for generation. Physical cores are fastest; 0 picks the best detected count automatically."
            chips={<RestartChip />}
          />
          <input
            type="number"
            min="0"
            value={config.llm.numThread}
            onChange={(event) =>
              updateLocalLLM("numThread", Number(event.target.value) || 0)
            }
            onBlur={() => void save({ llm: config.llm })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="Prefill threads"
            tip="Extra CPU threads used while reading a prompt (batch phase). Logical cores work well here, unlike generation."
            chips={<RestartChip />}
          />
          <input
            type="number"
            min="0"
            value={config.threadsBatch}
            onChange={(event) =>
              updateConfigLocal("threadsBatch", Number(event.target.value) || 0)
            }
            onBlur={() => void save({ threadsBatch: config.threadsBatch })}
          />
        </label>

        <label className="settings-field">
          <FieldLabel
            name="KV cache quant"
            tip="Compresses the model's memory for conversation state. q8_0 roughly halves it with a tiny speed cost; keep f16 if output quality degrades."
            chips={<RestartChip />}
          />
          <select
            value={config.kvCacheQuant}
            onChange={(event) => {
              updateConfigLocal("kvCacheQuant", event.target.value);
              void save({ kvCacheQuant: event.target.value });
            }}
          >
            <option value="">f16 (default)</option>
            <option value="q8_0">q8_0 — half memory</option>
          </select>
        </label>
      </div>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Context card — v1.6.0 (spec §7): AUTOMATIC LONG CONTEXT.
//
// The editable "Context size" input and the "History window %" control
// are REMOVED: the user no longer manages the physical model context.
// The runtime stays physically bounded (the selection/recommendation
// engine derives the window from measured evidence and the engine's
// verified limit — internally, automatically), while LOGICAL conversation
// continuity is unbounded through history, summaries, retrieval,
// chunking, rollover, memory and cross-mode references.
//
// Historical saved values (config.llm.numCtx, session context policies)
// keep loading as internal legacy values — never user controls again.
// ---------------------------------------------------------------------------

export function ContextCard({
  config,
  perf,
  save,
  updateConfigLocal,
}: {
  config: RuntimeConfig;
  perf: PerfSnapshot | null;
  save: PerformanceCardProps["save"];
  updateConfigLocal: <K extends keyof RuntimeConfig>(
    key: K,
    value: RuntimeConfig[K],
  ) => void;
}) {
  const verified = perf?.context?.total;

  return (
    <section className="settings-card">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">CONTEXT</span>
          <h3>Long context</h3>
        </div>
        <span className="settings-card-value">AUTOMATIC</span>
      </div>

      <div className="settings-form-grid">
        <div className="settings-field">
          <FieldLabel
            name="Conversation continuity"
            tip="SHEYTAN manages the context automatically: the physical window follows the model and engine limits, and long conversations continue without interruption through automatic summaries, retrieval, memory and cross-mode references. There is nothing to tune."
          />
          <div className="ctx-automatic-indicator">
            <strong>LONG CONTEXT · AUTOMATIC</strong>
            <span className="runtime-hint">
              {verified
                ? `The engine currently serves ${verified} tokens physically — managed automatically, no user tuning.`
                : "Start the engine to see the verified window (read-only)."}
            </span>
          </div>
        </div>

        <label className="inline-toggle">
          <input
            type="checkbox"
            checked={config.continuumEnabled}
            onChange={(event) => {
              const value = event.target.checked;
              updateConfigLocal("continuumEnabled", value);
              void save({ continuumEnabled: value });
            }}
          />
          <span>
            Automatic chapter rollover
            <span
              className="settings-tip"
              data-tip="Automatically rolls long conversations into chapters so very long sessions keep flowing — part of the automatic long-context system."
              tabIndex={0}
              aria-label="Automatic chapter rollover tooltip"
            >
              ?
            </span>
          </span>
        </label>
      </div>
    </section>
  );
}


// ---------------------------------------------------------------------------
// v1.6.2 — Engine backend (variant) provisioning card
// ---------------------------------------------------------------------------

// EngineBackendCard is the REAL Settings surface for engine-variant
// provisioning (previously API-only — ROADMAP/AI-CONTEXT disagreed on
// this; v1.6.2 wires the surface completely instead of a UI claim
// without a backend transaction):
//
//   Settings → API (GET/POST /api/engine/provision) → updater
//   transaction (stop → staged install → startup/health → runtime
//   backend verification → commit / rollback) → UI state.
//
// The card derives every label from engine-backend.ts (unit-tested);
// it never invents capability — the platform support matrix comes from
// the backend, and the GPU/Vulkan execution truth stays evidence-gated
// in the accelerator surface regardless of which package is installed.
export function EngineBackendCard() {
  const [installed, setInstalled] = useState<string | null>(null);
  const [supported, setSupported] = useState<string[]>([]);
  const [platform, setPlatform] = useState<string>("");
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busyVariant, setBusyVariant] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api
      .engineProvisionState()
      .then((state) => {
        if (!alive) return;
        setInstalled(state.installedVariant);
        setSupported(state.supportedVariants ?? []);
        setPlatform(state.platform ?? "");
        setLoadError(null);
      })
      .catch((err: unknown) => {
        if (!alive) return;
        setLoadError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      alive = false;
    };
  }, []);

  async function provision(action: BackendAction) {
    if (busyVariant) return;
    setBusyVariant(action.variant);
    setActionError(null);
    setOutcome(null);
    try {
      const result = await api.engineProvision(action.variant);
      setOutcome(result.outcome);
      const state = await api.engineProvisionState();
      setInstalled(state.installedVariant);
      setSupported(state.supportedVariants ?? []);
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyVariant(null);
    }
  }

  const summary =
    installed === null ? null : engineBackendSummary(installed, supported);

  return (
    <section className="settings-card">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">ENGINE — BACKEND</span>
          <h3>Engine backend</h3>
        </div>
      </div>

      <p className="settings-note">
        The managed llama.cpp engine ships as backend packages. The swap is a
        verified transaction: the engine stops, the new package is staged and
        verified (startup health plus the engine&rsquo;s own backend/device
        enumeration), and any failure rolls back to the current package. GPU
        offload claims stay evidence-gated — installing the Vulkan package
        does not by itself claim a working GPU.
      </p>

      {loadError ? (
        <p className="settings-error">
          Engine backend state unavailable: {loadError}
        </p>
      ) : installed === null ? (
        <p className="settings-note">Reading the installed engine backend…</p>
      ) : (
        <>
          <div className="settings-form-grid">
            <div className="settings-field">
              <FieldLabel
                name="Installed backend"
                tip="The backend family of the engine package currently installed (recorded in the install manifest)."
              />
              <Chip tone={installed === "vulkan" ? "good" : "neutral"}>
                {variantLabel(installed)}
              </Chip>
            </div>
            <div className="settings-field">
              <FieldLabel
                name="Platform"
                tip="The OS/architecture the support matrix is evaluated for."
              />
              <Chip tone="neutral">{platform || "—"}</Chip>
            </div>
          </div>

          <p className="settings-note">
            {summary ? summary.summary : ""}
          </p>

          {summary
            ? summary.actions.map((action) => (
                <div key={action.variant} className="settings-field">
                  <button
                    type="button"
                    className="settings-inline-action"
                    disabled={busyVariant !== null}
                    onClick={() => void provision(action)}
                  >
                    {busyVariant === action.variant
                      ? provisionBusyLabel(action.variant)
                      : action.label}
                  </button>
                  <p className="settings-note">{action.hint}</p>
                </div>
              ))
            : null}

          {outcome ? (
            <p className="settings-note">Result: {outcome}</p>
          ) : null}

          {actionError ? (
            <p className="settings-error">Provisioning failed: {actionError}</p>
          ) : null}
        </>
      )}
    </section>
  );
}
