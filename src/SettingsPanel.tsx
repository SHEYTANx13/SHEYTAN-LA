import { useEffect, useMemo, useState, type ChangeEvent } from "react";

import {
  api,
  type LLMConfig,
  type ModelsResponse,
  type PerfSnapshot,
  type Preset,
  type RuntimeConfig,
  type SysInfo,
  type ToolInfo,
} from "./api";
import { ensure as ensureResource, reset as resetResource } from "./resources";
import { useResource } from "./useResource";

// v1.6.0 P3: the My Tools card (Settings → Agent & Tools → My Tools).
import MyToolsCard from "./MyToolsCard";

import { FieldLabel } from "./settings-shared";
import {
  ContextCard,
  EngineBackendCard,
  EngineCard,
  EngineProfileCard,
  LiveMetricsCard,
  LogsCard,
  NetworkCard,
  PerformanceModeCard,
  RecommendedCard,
  type PerfBaseline,
} from "./SettingsPerformance";
import {
  SimplePerformanceCard,
  TaskProfileCard,
  UpdatesCard,
  VisionCard,
} from "./SettingsVisionUpdates";
import { SETTINGS_SECTIONS, type SettingsSectionId } from "./settings-sections";

// v1.3.0: the section registry (settings-sections.ts) is the single
// source of truth — rendering here and the structure regression tests
// both consume it.

type SaveState = "idle" | "loading" | "saved" | "error";

// v1.3.0 SETTINGS RESTRUCTURE — users choose outcomes; SHEYTAN chooses
// implementation details. Eight coherent sections replace the ten
// v1.2.9 tabs that exposed llama.cpp internals (engine host/port,
// projector paths, batch/thread knobs) to ordinary users:
//
//   General        startup, workspace, interaction, accessibility
//   Models         current model, available models, vision capability
//   Performance    Quiet/Balanced/Maximum + measured context/metrics
//   Agent & Tools  tools, permissions, thinking, sandbox posture
//   Network        Net Search + remote provider (internal keys stay research*)
//   Updates        check, policy, version
//   Diagnostics    read-only health, hardware, logs, network
//   Advanced       the ONLY home for expert controls
//
// The technical fields still exist in the backend config (backward
// compatible — legacy files load and save unchanged); they are simply
// no longer ordinary user controls.
type SettingsTab = SettingsSectionId;

const TABS: { id: SettingsTab; label: string }[] = SETTINGS_SECTIONS.map(
  (section) => ({ id: section.id, label: section.label }),
);

// Engine-affecting top-level keys: saving any of these restarts the local
// engine so the option ACTUALLY takes effect (v1.1.7 — the previous list
// silently left speed options inert until a manual restart).
const ENGINE_KEYS = [
  "flashAttention",
  "cacheReuse",
  "ubatchSize",
  "threadsBatch",
  "kvCacheQuant",
  "mlock",
  "gpuAutoOffload",
  "draftModel",
] as const;

function touchesEngine(patch: Record<string, unknown>): boolean {
  if (ENGINE_KEYS.some((key) => patch[key] !== undefined)) {
    return true;
  }
  return (
    patch.model !== undefined ||
    patch.llamaBinPath !== undefined ||
    patch.llamaHost !== undefined ||
    patch.llamaPort !== undefined ||
    patch.llm !== undefined
  );
}

function numberValue(event: ChangeEvent<HTMLInputElement>): number {
  const value = Number(event.target.value);
  return Number.isFinite(value) ? value : 0;
}

function SettingsPanel() {
  // v1.1.7: active settings section (declared FIRST — the lazy resource
  // hooks below depend on which tab is visible).
  const [activeTab, setActiveTab] = useState<SettingsTab>("general");

  // v1.2.6 PROGRESSIVE LOADING — the v1.2.5 panel fetched config + models
  // + presets + tools + sysinfo with ONE global Promise.all: the entire
  // view waited for the slowest endpoint (the Windows hardware probe
  // measured 5.4 s) and one failure failed everything.
  //
  //   IMMEDIATE   config (the critical controls render as soon as it lands)
  //   BACKGROUND  presets, tools (independent, stale-while-revalidate)
  //   LAZY        models (Models tab), sysinfo (Performance tab)
  //
  // Every resource owns its OWN state through the shared resource layer
  // (dedup + TTL + last-known-good + AbortController) — one slow endpoint
  // can never block the others, and Settings/System/Performance mounting
  // together never issue duplicate requests.
  const configResource = useResource<RuntimeConfig>("config", (signal) =>
    api.config(signal),
  );
  const modelsResource = useResource<ModelsResponse>(
    "models",
    (signal) => api.models(signal),
    { enabled: activeTab === "models" || activeTab === "advanced" },
  );
  const presetsResource = useResource<Preset[]>("presets", (signal) =>
    api.presets(signal),
  );
  const toolsResource = useResource<ToolInfo[]>("tools", (signal) =>
    api.tools(signal),
  );
  const sysinfoResource = useResource<SysInfo>(
    "sysinfo",
    (signal) => api.sysinfo(signal),
    // v1.3.0 fix: the hardware card serves Diagnostics (and Advanced)
    // too — the v1.2.9 gate only fetched it on Performance, so the
    // hardware card showed "unavailable" everywhere else.
    {
      enabled:
        activeTab === "performance" ||
        activeTab === "diagnostics" ||
        activeTab === "advanced",
    },
  );

  // The config the panel edits: the shared cache's server truth, with
  // optimistic local edits between load and save (existing call sites
  // keep their plain setConfig updaters).
  const [config, setConfig] = useState<RuntimeConfig | null>(null);

  useEffect(() => {
    if (configResource.data) {
      setConfig(configResource.data);
    }
  }, [configResource.data]);

  // v1.1.5 Phase 6: the backend currently serving generation ("native" |
  // "llama") — reported by /api/models so the local model list can mark the
  // serving model honestly instead of guessing from llamaRunning.
  const models = modelsResource.data?.local ?? [];
  const servingBackend = modelsResource.data?.backend ?? "";
  const presets = presetsResource.data ?? [];
  const tools = toolsResource.data ?? [];
  const sysinfo = sysinfoResource.data;

  const [saveState, setSaveState] = useState<SaveState>("loading");
  const [error, setError] = useState<string | null>(null);
  const [restartAfterSave, setRestartAfterSave] = useState(true);

  // v1.3.0: reduce-motion user preference — mirrors the OS-level
  // prefers-reduced-motion with an explicit per-application switch.
  // Applied as a class on the document root; motion.css carries the
  // equivalent animation-collapse rules for both the media query and
  // this class.
  const [reduceMotion, setReduceMotion] = useState<boolean>(() => {
    try {
      return localStorage.getItem("shtn:reduce-motion") === "1";
    } catch {
      return false;
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem("shtn:reduce-motion", reduceMotion ? "1" : "0");
    } catch {
      // storage unavailable — the toggle stays session-scoped
    }
    document.documentElement.classList.toggle("reduce-motion", reduceMotion);
  }, [reduceMotion]);

  // The save button unlocks the moment the config lands — one slow
  // background resource (models/tools/sysinfo) can no longer lock the
  // whole panel (the v1.2.5 symptom: "Settings still feels slow").
  useEffect(() => {
    if (configResource.state === "ready" || configResource.state === "stale") {
      setSaveState((current) => (current === "loading" ? "idle" : current));
    }

    if (configResource.state === "error") {
      setError(configResource.error ?? "Unable to load runtime settings.");
      setSaveState("error");
    }
  }, [configResource.state, configResource.error]);

  // v1.1.7: live performance snapshot (polled while the Performance tab is
  // open) and the optional before/after comparison baseline.
  const [perf, setPerf] = useState<PerfSnapshot | null>(null);
  const [baseline, setBaseline] = useState<PerfBaseline | null>(null);

  async function load() {
    // v1.2.6: a manual refresh re-fetches every OWNED resource through
    // the shared layer (deduplicated; slow ones refresh in background
    // while the panel keeps showing last-known-good).
    setSaveState("loading");
    setError(null);

    try {
      await Promise.all([
        ensureResource("config", (signal) => api.config(signal), {
          force: true,
        }),
        ensureResource("presets", (signal) => api.presets(signal), {
          force: true,
        }),
        ensureResource("tools", (signal) => api.tools(signal), {
          force: true,
        }),
        ...(activeTab === "models" || activeTab === "advanced"
          ? [
              ensureResource("models", (signal) => api.models(signal), {
                force: true,
              }),
            ]
          : []),
        ...(activeTab === "performance" ||
        activeTab === "diagnostics" ||
        activeTab === "advanced"
          ? [
              ensureResource("sysinfo", (signal) => api.sysinfo(signal), {
                force: true,
              }),
            ]
          : []),
      ]);

      setSaveState("idle");
    } catch {
      // The resource layer keeps last-known-good data and records the
      // per-resource error state; the panel shows the failure without
      // blanking anything already rendered.
      setSaveState("error");
    }
  }

  // v1.1.7: lightweight live-metrics polling — ONLY while the Performance
  // or Diagnostics tab is visible, at a human cadence (3 s), so the app
  // stays quiet everywhere else.
  useEffect(() => {
    if (activeTab !== "performance" && activeTab !== "diagnostics") {
      return;
    }

    let cancelled = false;

    async function tick() {
      try {
        const snapshot = await api.perf();
        if (!cancelled) {
          setPerf(snapshot);
        }
      } catch {
        // Engine restart or backend hiccup — keep the last honest view.
      }
    }

    void tick();
    const timer = window.setInterval(tick, 3000);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeTab]);

  async function save(patch: Record<string, unknown>) {
    if (!config) {
      return;
    }

    // v1.1.7 P1: before/after observation — when a performance-relevant
    // option changes, remember the CURRENT measured metrics so the Live
    // Metrics card can show an honest Before → After comparison once the
    // next generation runs. In-memory only; no benchmark database.
    const perfKeysTouched =
      patch.llm !== undefined ||
      ENGINE_KEYS.some((key) => patch[key] !== undefined);

    if (perfKeysTouched && perf?.generation?.hasSample) {
      setBaseline({
        label: "setting change",
        at: new Date().toLocaleTimeString(),
        genTokPerSec: perf.generation.genTokPerSec,
        promptTokPerSec: perf.generation.promptTokPerSec,
        ttftSeconds: perf.generation.ttftSeconds,
        cpuPercent: perf.cpuPercent ?? null,
        gpuPercent: perf.gpuPercent ?? null,
      });
    }

    setSaveState("loading");
    setError(null);

    try {
      const nextConfig = await api.updateConfig(patch);
      setConfig(nextConfig);

      // v1.2.6: the shared config cache must carry the server's post-save
      // truth (Settings, System and Performance all read it).
      resetResource("config");

      // v1.1.7: the restart condition now covers every engine-affecting
      // option, so speed settings genuinely apply instead of waiting for a
      // manual restart.
      if (restartAfterSave && touchesEngine(patch)) {
        try {
          await api.llama("stop");
        } catch {
          // Engine may already be stopped.
        }

        await api.llama("start");
      }

      setSaveState("saved");
    } catch (saveError) {
      setError(
        saveError instanceof Error
          ? saveError.message
          : "Unable to save settings.",
      );
      setSaveState("error");
    }
  }

  async function selectModel(model: string) {
    if (!model || model === config?.model) {
      return;
    }

    // v1.5.1: a local model change goes through the backend-authoritative
    // model-first flow (resolve → analyze → configure atomically → load →
    // verify → ready) — the SAME path the Model Picker uses — never a
    // bare config write plus a blind engine cycle. The currently intended
    // task profile is preserved (spec §5). Remote providers keep the
    // plain config save (model selection is a local-engine concept).
    if ((config?.provider ?? "local") === "local") {
      setSaveState("loading");
      setError(null);

      try {
        await api.selectModel(model, config?.runtimeProfile || undefined);
        const nextConfig = await api.config();
        setConfig(nextConfig);
        resetResource("config");
        setSaveState("saved");
      } catch (selectError) {
        setError(
          selectError instanceof Error
            ? selectError.message
            : "Unable to switch the model.",
        );
        setSaveState("error");
      }
      return;
    }

    await save({
      model,
    });
  }

  async function selectProvider(provider: string) {
    await save({
      provider,
    });
  }

  async function applyPreset(preset: Preset) {
    const llm: Partial<LLMConfig> = {
      ...(typeof preset.temperature === "number"
        ? { temperature: preset.temperature }
        : {}),
      ...(typeof preset.top_p === "number" ? { topP: preset.top_p } : {}),
      ...(typeof preset.top_k === "number" ? { topK: preset.top_k } : {}),
      ...(typeof preset.max_tokens === "number"
        ? { maxTokens: preset.max_tokens }
        : {}),
      ...(typeof preset.repeat_penalty === "number"
        ? { repeatPenalty: preset.repeat_penalty }
        : {}),
      ...(typeof preset.mirostat === "number"
        ? { mirostat: preset.mirostat }
        : {}),
      ...(typeof preset.num_ctx === "number" ? { numCtx: preset.num_ctx } : {}),
      preset: preset.id,
    };

    await save({
      llm,
    });
  }

  async function toggleTool(name: string, enabled: boolean) {
    const current = config?.enabledTools ?? [];

    const active =
      current.length === 0 ? tools.map((tool) => tool.name) : [...current];

    const next = enabled
      ? Array.from(new Set([...active, name]))
      : active.filter((item) => item !== name);

    await save({
      enabledTools: next,
    });
  }

  function updateLocalLLM<K extends keyof LLMConfig>(
    key: K,
    value: LLMConfig[K],
  ) {
    setConfig((current) =>
      current
        ? {
            ...current,
            llm: {
              ...current.llm,
              [key]: value,
            },
          }
        : current,
    );
  }

  // v1.1.7: top-level config draft updater for the Performance cards.
  function updateConfigLocal<K extends keyof RuntimeConfig>(
    key: K,
    value: RuntimeConfig[K],
  ) {
    setConfig((current) => (current ? { ...current, [key]: value } : current));
  }

  const currentPreset = useMemo(
    () => presets.find((preset) => preset.id === config?.llm.preset),
    [presets, config?.llm.preset],
  );

  const effectiveTools = useMemo(
    () =>
      config?.enabledTools?.length
        ? new Set(config.enabledTools)
        : new Set(tools.map((tool) => tool.name)),
    [config?.enabledTools, tools],
  );

  if (!config) {
    return (
      <div className="settings-page" tabIndex={-1}>
        <div className="settings-loading">
          <div className="panel-loading-mark">✦</div>
          <strong>Loading runtime configuration</strong>
          <span>{error ?? "Reading local runtime state…"}</span>
          {error ? (
            <button
              type="button"
              className="secondary-button"
              onClick={() => void load()}
            >
              Retry
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div className="settings-page">
      <div className="settings-toolbar">
        <div>
          <span className="eyebrow">RUNTIME CONTROL</span>
          <h2>Configure SHEYTAN</h2>
          <p>
            Models, inference, agent behavior, tools, browser automation, Coding
            Lab, Net Search, and performance.
          </p>
        </div>

        <div className="settings-toolbar-actions">
          <label className="inline-toggle">
            <input
              type="checkbox"
              checked={restartAfterSave}
              onChange={(event) => setRestartAfterSave(event.target.checked)}
            />
            <span>Restart engine after engine changes</span>
          </label>

          <button
            type="button"
            className="secondary-button"
            onClick={() => void load()}
            disabled={saveState === "loading"}
          >
            Refresh
          </button>
        </div>
      </div>

      {/* v1.1.7: section navigation — scan first, scroll less. */}
      <div className="settings-tabs" role="tablist">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={activeTab === tab.id}
            className={`settings-tab ${activeTab === tab.id ? "active" : ""}`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {saveState === "saved" ? (
        <div className="settings-status success">
          Settings saved successfully.
        </div>
      ) : null}

      {error ? <div className="settings-status error">{error}</div> : null}

      <div className="settings-grid">
        {/* ======================================================== */}
        {/* GENERAL */}
        {/* ======================================================== */}
        {/* v1.1.8: Models gets its own tab — model/provider identity is
            its own intent, not a "General"Misc item. */}
        {activeTab === "models" ? (
          <>
            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">PROVIDER</span>
                  <h3>Model runtime</h3>
                </div>
                <span className="settings-card-value">{config.provider}</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Provider"
                    tip="Local runs models on this machine with llama.cpp; Remote talks to an OpenAI-compatible API over the internet. Local keeps everything private."
                  />
                  <select
                    value={config.provider}
                    onChange={(event) =>
                      void selectProvider(event.target.value)
                    }
                  >
                    <option value="local">Local GGUF / llama.cpp</option>
                    <option value="remote">Remote OpenAI-compatible API</option>
                  </select>
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Local model"
                    tip="Which GGUF model file to load into the local engine. The currently serving model is marked automatically."
                  />
                  <select
                    value={config.model}
                    onChange={(event) => void selectModel(event.target.value)}
                    disabled={config.provider !== "local"}
                  >
                    <option value="">Select a local model</option>
                    {models.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.name}
                        {model.serving
                          ? ` — currently serving${
                              servingBackend === "native"
                                ? " (native engine)"
                                : " (llama.cpp)"
                            }`
                          : ""}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="settings-field">
                  <span>Models folder</span>
                  <div className="settings-inline-action">
                    <code
                      className="settings-value-path"
                      title={config.modelsDir}
                    >
                      {config.modelsDir}
                    </code>
                    <button
                      type="button"
                      className="secondary-button"
                      onClick={() =>
                        void api.openModelsFolder().catch(() => undefined)
                      }
                    >
                      Open folder
                    </button>
                  </div>
                  <span className="runtime-hint">
                    Drop GGUF model files here — they are discovered
                    automatically on the next start. The folder location itself
                    is managed automatically (changeable in Advanced).
                  </span>
                </label>

                <div className="settings-note">
                  Remote (OpenAI-compatible) endpoints are configured in the
                  Network tab.
                </div>
              </div>
            </section>

            {/* v1.3.0: the Vision tab folds into Models as a capability
                status — "Projector: Automatically managed", evidence
                only, no raw path fields. The expert override lives in
                Advanced. */}
            <VisionCard config={config} variant="status" />
          </>
        ) : null}

        {activeTab === "general" ? (
          <>
            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">STARTUP</span>
                  <h3>Startup &amp; workspace</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <FieldLabel
                    name="Start the local engine automatically"
                    tip="With this on, SHEYTAN launches its local inference engine whenever a model is needed — no manual engine management."
                  />
                  <input
                    type="checkbox"
                    checked={config.llamaAutoStart}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, llamaAutoStart: value }
                          : current,
                      );

                      void save({ llamaAutoStart: value });
                    }}
                  />
                </label>

                <label className="settings-field">
                  <span>Default workspace</span>
                  <div className="settings-inline-action">
                    <code
                      className="settings-value-path"
                      title={config.workspaceRoot || ""}
                    >
                      {config.workspaceRoot || "Managed automatically"}
                    </code>
                  </div>
                  <span className="runtime-hint">
                    The folder your agent, files and git tools work in. Change
                    it from the Workspace tab (or clone a GitHub repository —
                    the workspace switches automatically).
                  </span>
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">INTERACTION</span>
                  <h3>Response pacing</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.smoothStream}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, smoothStream: value } : current,
                      );

                      void save({ smoothStream: value });
                    }}
                  />
                  <span>Smooth token streaming</span>
                </label>

                <label className="settings-field">
                  <span>Target FPS</span>
                  <input
                    type="number"
                    min="30"
                    max="240"
                    value={config.targetFps}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current ? { ...current, targetFps: value } : current,
                      );
                    }}
                    onBlur={() => void save({ targetFps: config.targetFps })}
                  />
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.showPerfHud}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, showPerfHud: value } : current,
                      );

                      void save({ showPerfHud: value });
                    }}
                  />
                  <span>Performance HUD (frame timing, dev tool)</span>
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">ACCESSIBILITY</span>
                  <h3>Motion</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <FieldLabel
                    name="Reduce motion"
                    tip="Cuts interface animations to the minimum. This also follows your operating system's reduced-motion setting automatically."
                  />
                  <input
                    type="checkbox"
                    checked={reduceMotion}
                    onChange={(event) => {
                      setReduceMotion(event.target.checked);
                    }}
                  />
                </label>
                <span className="runtime-hint">
                  Animations follow the OS-level reduced-motion preference
                  automatically; this switch forces it for SHEYTAN only.
                </span>
              </div>
            </section>
          </>
        ) : null}

        {/* ======================================================== */}
        {/* AGENT & TOOLS — behavior, tool access, sandbox posture */}
        {/* ======================================================== */}
        {activeTab === "agent" ? (
          <>
            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">AGENT</span>
                  <h3>Behavior &amp; reliability</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Maximum iterations"
                    tip="How many tool steps the agent may take per task before it must report back. Higher allows longer autonomous work."
                  />
                  <input
                    type="number"
                    min="1"
                    max="1000"
                    value={config.maxIterations}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current
                          ? { ...current, maxIterations: value }
                          : current,
                      );
                    }}
                    onBlur={() =>
                      void save({ maxIterations: config.maxIterations })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Recall Top K"
                    tip="How many past memories the agent may pull into a task. Higher recall helps long projects; lower keeps the context tight."
                  />
                  <input
                    type="number"
                    min="0"
                    max="100"
                    value={config.recallTopK}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current ? { ...current, recallTopK: value } : current,
                      );
                    }}
                    onBlur={() => void save({ recallTopK: config.recallTopK })}
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Thinking mode"
                    tip="Lets the model reason privately before answering — better on hard tasks, slightly slower."
                  />
                  <input
                    type="checkbox"
                    checked={config.thinkingMode}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, thinkingMode: value } : current,
                      );

                      void save({ thinkingMode: value });
                    }}
                  />
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.parallelTools}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, parallelTools: value }
                          : current,
                      );

                      void save({ parallelTools: value });
                    }}
                  />
                  <span>Parallel tools</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.recallEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, recallEnabled: value }
                          : current,
                      );

                      void save({ recallEnabled: value });
                    }}
                  />
                  <span>Memory recall</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.continuumEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, continuumEnabled: value }
                          : current,
                      );

                      void save({ continuumEnabled: value });
                    }}
                  />
                  <span>Continuum context</span>
                </label>
              </div>
            </section>

            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">TOOLS</span>
                  <h3>Tool access</h3>
                </div>
                <span className="settings-card-value">
                  {effectiveTools.size} / {tools.length}
                </span>
              </div>

              <div className="control-tool-list">
                {tools.map((tool) => (
                  <label
                    key={tool.name}
                    className="inline-toggle control-tool-toggle"
                  >
                    <input
                      type="checkbox"
                      checked={effectiveTools.has(tool.name)}
                      onChange={(event) => {
                        void toggleTool(tool.name, event.target.checked);
                      }}
                    />
                    <span title={tool.description}>{tool.name}</span>
                  </label>
                ))}
              </div>
              <span className="runtime-hint">
                Unchecking a tool removes it from the agent's abilities
                immediately — no restart required.
              </span>
            </section>

            {/* v1.6.0 P3 (spec §8): MY TOOLS — the custom tool builder.
                Create → validate → save → register → test, all through
                the backend's single custom-tools authority. */}
            <MyToolsCard />

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">SANDBOX</span>
                  <h3>Execution sandbox</h3>
                </div>
                <span className="settings-chip chip-good">fail closed</span>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <FieldLabel
                    name="Sandbox agent-generated code"
                    tip="Model-spawned processes run with memory and CPU caps. When off, generated code runs unsandboxed — only turn this off for trusted, isolated workflows."
                  />
                  <input
                    type="checkbox"
                    checked={config.sandboxEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, sandboxEnabled: value }
                          : current,
                      );

                      void save({ sandboxEnabled: value });
                    }}
                  />
                </label>
                <span className="runtime-hint">
                  Resource limits are managed automatically (tunable in
                  Advanced).
                </span>
              </div>
            </section>

            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">CODING LAB</span>
                  <h3>Autonomous engineering</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <FieldLabel
                    name="Enable Coding Lab"
                    tip="Isolated workspaces where the agent can build, run and verify code end-to-end."
                  />
                  <input
                    type="checkbox"
                    checked={config.labEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, labEnabled: value } : current,
                      );

                      void save({ labEnabled: value });
                    }}
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Keep lab workspaces"
                    tip="Preserve lab workspaces between runs so you can inspect what the agent built. When off, they are cleaned up after verification."
                  />
                  <input
                    type="checkbox"
                    checked={config.labKeepWorkspaces}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, labKeepWorkspaces: value }
                          : current,
                      );

                      void save({ labKeepWorkspaces: value });
                    }}
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Allow network in lab"
                    tip="Permit network access inside Coding Lab commands (dependency installs, package downloads). Off by default — the lab is isolated."
                  />
                  <input
                    type="checkbox"
                    checked={config.labAllowNetwork}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, labAllowNetwork: value }
                          : current,
                      );

                      void save({ labAllowNetwork: value });
                    }}
                  />
                </label>
              </div>
            </section>
          </>
        ) : null}

        {/* ======================================================== */}
        {/* NETWORK / RESEARCH */}
        {/* ======================================================== */}
        {activeTab === "network" ? (
          <>
            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">NET SEARCH</span>
                  <h3>Net Search</h3>
                </div>
                <span className="settings-card-value">
                  {config.researchEnabled
                    ? config.researchBackend || "auto"
                    : "off"}
                </span>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <FieldLabel
                    name="Net Search enabled"
                    tip="Lets the agent search and cite external sources when a task needs evidence beyond the local machine."
                  />
                  <input
                    type="checkbox"
                    checked={config.researchEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, researchEnabled: value }
                          : current,
                      );

                      void save({ researchEnabled: value });
                    }}
                  />
                </label>

                <label className="settings-field">
                  <span>Search provider</span>
                  <select
                    value={config.researchBackend || "auto"}
                    onChange={(event) => {
                      const value = event.target.value;

                      setConfig((current) =>
                        current
                          ? { ...current, researchBackend: value }
                          : current,
                      );

                      void save({ researchBackend: value });
                    }}
                  >
                    <option value="auto">Automatic</option>
                    <option value="duckduckgo">DuckDuckGo</option>
                    <option value="searxng">SearXNG (self-hosted)</option>
                  </select>
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="SearXNG URL"
                    tip="Your self-hosted SearXNG instance, used when the provider is set to SearXNG."
                  />
                  <input
                    value={config.researchSearxngUrl ?? ""}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchSearxngUrl: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        researchSearxngUrl: config.researchSearxngUrl,
                      })
                    }
                    disabled={(config.researchBackend || "auto") !== "searxng"}
                  />
                </label>

                <label className="settings-field">
                  <span>Results per search</span>
                  <input
                    type="number"
                    min="1"
                    max="100"
                    value={config.researchMaxResults}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current
                          ? { ...current, researchMaxResults: value }
                          : current,
                      );
                    }}
                    onBlur={() =>
                      void save({
                        researchMaxResults: config.researchMaxResults,
                      })
                    }
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Net Search provider: GitHub"
                    tip="Search GitHub issues and pull requests for engineering evidence."
                  />
                  <input
                    type="checkbox"
                    checked={config.researchGitHub}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, researchGitHub: value }
                          : current,
                      );

                      void save({ researchGitHub: value });
                    }}
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Net Search provider: Reddit"
                    tip="Search Reddit for community experience and practical answers."
                  />
                  <input
                    type="checkbox"
                    checked={config.researchReddit}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, researchReddit: value }
                          : current,
                      );

                      void save({ researchReddit: value });
                    }}
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Net Search provider: general web"
                    tip="Fetch and cite ordinary web pages."
                  />
                  <input
                    type="checkbox"
                    checked={config.researchWeb}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, researchWeb: value } : current,
                      );

                      void save({ researchWeb: value });
                    }}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">REMOTE PROVIDER</span>
                  <h3>OpenAI-compatible endpoint</h3>
                </div>
                <span className="settings-card-value">{config.provider}</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Remote base URL"
                    tip="An OpenAI-compatible chat endpoint, used when the provider is set to Remote in the Models tab."
                  />
                  <input
                    value={config.remoteBaseUrl}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, remoteBaseUrl: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ remoteBaseUrl: config.remoteBaseUrl })
                    }
                    disabled={config.provider !== "remote"}
                  />
                </label>

                <label className="settings-field">
                  <span>Remote model</span>
                  <input
                    value={config.remoteModel}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, remoteModel: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ remoteModel: config.remoteModel })
                    }
                    disabled={config.provider !== "remote"}
                  />
                </label>

                <div className="settings-note">
                  API keys stay on the machine: they are stored in the local
                  config and redacted from every UI response.
                </div>
              </div>
            </section>
          </>
        ) : null}

        {/* ======================================================== */}
        {/* UPDATES */}
        {/* ======================================================== */}
        {activeTab === "updates" ? (
          <>
            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">POLICY</span>
                  <h3>Automatic updates</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Check for updates"
                    tip="How often SHEYTAN checks for engine and application updates in the background. Checks never install anything by themselves."
                  />
                  <select
                    value={config.updateSchedule || "daily"}
                    onChange={(event) => {
                      const value = event.target.value;

                      setConfig((current) =>
                        current
                          ? { ...current, updateSchedule: value }
                          : current,
                      );

                      void save({ updateSchedule: value });
                    }}
                  >
                    <option value="daily">Daily (recommended)</option>
                    <option value="weekly">Weekly</option>
                    <option value="monthly">Monthly</option>
                    <option value="off">Off</option>
                  </select>
                </label>
              </div>
            </section>

            <UpdatesCard />
          </>
        ) : null}

        {/* ======================================================== */}
        {/* DIAGNOSTICS — read-only: hardware, network, logs */}
        {/* ======================================================== */}
        {activeTab === "diagnostics" ? (
          <>
            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">HARDWARE</span>
                  <h3>System profile</h3>
                </div>
                <span className="settings-chip chip-good">measured</span>
              </div>

              {sysinfo ? (
                <div className="env-grid">
                  <div className="session-detail">
                    <span>CPU</span>
                    <strong title={sysinfo.cpu.name}>
                      {sysinfo.cpu.name !== "Unknown"
                        ? sysinfo.cpu.name
                        : `${sysinfo.cpu.logicalCores} logical cores`}
                    </strong>
                  </div>
                  <div className="session-detail">
                    <span>Memory</span>
                    <strong>
                      {(sysinfo.ram.totalBytes / 1024 ** 3).toFixed(1)} GB
                    </strong>
                  </div>
                  <div className="session-detail">
                    <span>Storage free</span>
                    <strong>
                      {(sysinfo.disk.freeBytes / 1024 ** 3).toFixed(1)} GB
                    </strong>
                  </div>
                  <div className="session-detail">
                    <span>OS</span>
                    <strong>{sysinfo.osDisplay || sysinfo.os}</strong>
                  </div>
                  <div className="session-detail">
                    <span>Recommended context</span>
                    <strong>
                      {sysinfo.recommended.numCtx.toLocaleString()}
                    </strong>
                  </div>
                </div>
              ) : (
                <span className="runtime-hint">
                  Hardware information is being measured…
                </span>
              )}

              {sysinfo?.recommended.warnings?.length ? (
                <div className="settings-note">
                  {sysinfo.recommended.warnings.join(" · ")}
                </div>
              ) : null}
            </section>

            <NetworkCard />
            <LogsCard />
          </>
        ) : null}

        {/* ======================================================== */}
        {/* PERFORMANCE: postures → recommended → context → measured */}
        {/* ======================================================== */}
        {activeTab === "performance" ? (
          <>
            {/* v1.5.0: the AUTO/MANUAL contract owns the whole tab's
                semantics — it comes first. */}
            <PerformanceModeCard
              config={config}
              save={save}
              onReanalyzed={() => void resetResource("config")}
            />
            <SimplePerformanceCard config={config} save={save} />
            <TaskProfileCard config={config} save={save} />

            <EngineProfileCard perf={perf} />

            {/* v1.6.2: the REAL Settings surface for engine-variant
                provisioning (Settings → API → updater transaction →
                restart → health/backend verification → UI state). */}
            <EngineBackendCard />

            <RecommendedCard config={config} perf={perf} save={save} />

            <ContextCard
              config={config}
              perf={perf}
              save={save}
              updateConfigLocal={updateConfigLocal}
            />

            <LiveMetricsCard
              perf={perf}
              baseline={baseline}
              clearBaseline={() => setBaseline(null)}
            />
          </>
        ) : null}

        {/* ======================================================== */}
        {/* ADVANCED — the only home for expert controls */}
        {/* ======================================================== */}
        {activeTab === "advanced" ? (
          <>
            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">ADVANCED</span>
                  <h3>Expert controls</h3>
                </div>
              </div>
              <span className="runtime-hint">
                Everything here is a genuine expert control: raw engine flags,
                storage limits, resource caps and path overrides. Ordinary use
                never requires this tab — SHEYTAN measures and picks these
                values automatically (Automatic / Recommended / Measured are
                labeled as such where they apply).
              </span>
            </section>

            <EngineCard
              config={config}
              perf={perf}
              save={save}
              updateLocalLLM={updateLocalLLM}
              updateConfigLocal={updateConfigLocal}
            />

            <VisionCard config={config} variant="control" />

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">GENERATION</span>
                  <h3>Sampling</h3>
                </div>
                {currentPreset ? (
                  <span className="settings-card-value">
                    preset: {currentPreset.id}
                  </span>
                ) : null}
              </div>

              <div className="preset-row">
                {presets.map((preset) => (
                  <button
                    key={preset.id}
                    type="button"
                    className={`preset-card ${config.llm.preset === preset.id ? "active" : ""}`}
                    onClick={() => void applyPreset(preset)}
                  >
                    <strong>{preset.label || preset.id}</strong>
                    <span>{preset.description}</span>
                  </button>
                ))}
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <span>Temperature</span>
                  <input
                    type="number"
                    min="0"
                    max="2"
                    step="0.05"
                    value={config.llm.temperature}
                    onChange={(event) =>
                      updateLocalLLM("temperature", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Top P</span>
                  <input
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={config.llm.topP}
                    onChange={(event) =>
                      updateLocalLLM("topP", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Top K</span>
                  <input
                    type="number"
                    min="0"
                    value={config.llm.topK}
                    onChange={(event) =>
                      updateLocalLLM("topK", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Min P</span>
                  <input
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={config.llm.minP ?? 0}
                    onChange={(event) =>
                      updateLocalLLM("minP", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Max tokens</span>
                  <input
                    type="number"
                    min="128"
                    step="128"
                    value={config.llm.maxTokens}
                    onChange={(event) =>
                      updateLocalLLM("maxTokens", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Repeat penalty</span>
                  <input
                    type="number"
                    min="0.5"
                    max="2"
                    step="0.01"
                    value={config.llm.repeatPenalty}
                    onChange={(event) =>
                      updateLocalLLM("repeatPenalty", numberValue(event))
                    }
                    onBlur={() => {
                      // v1.6.1: the engine rejects "--repeat-penalty 0"
                      // outright (must be finite and greater than 0) — a
                      // cleared or malformed field must never be saved as 0.
                      // Normalize to the documented default exactly like
                      // config.Load does, then save that.
                      const rp = config.llm.repeatPenalty;
                      if (!Number.isFinite(rp) || rp <= 0) {
                        updateLocalLLM("repeatPenalty", 1.1);
                        void save({
                          llm: { ...config.llm, repeatPenalty: 1.1 },
                        });
                        return;
                      }
                      void save({ llm: config.llm });
                    }}
                  />
                </label>

                <label className="settings-field">
                  <span>Seed (0 = random)</span>
                  <input
                    type="number"
                    value={config.llm.seed}
                    onChange={(event) =>
                      updateLocalLLM("seed", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">ENGINE ENDPOINTS</span>
                  <h3>Paths &amp; endpoints</h3>
                </div>
                <span className="settings-chip chip-warn">
                  managed by default
                </span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="LLM base URL"
                    tip="Overrides the local engine endpoint for an external, self-managed OpenAI-compatible server. Empty = follow the managed engine."
                  />
                  <input
                    value={config.llmBaseUrl ?? ""}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, llmBaseUrl: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() => void save({ llmBaseUrl: config.llmBaseUrl })}
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Models directory"
                    tip="Where GGUF model files are discovered. Changing it relocates model discovery; existing models are not moved."
                  />
                  <input
                    value={config.modelsDir}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, modelsDir: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() => void save({ modelsDir: config.modelsDir })}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">SANDBOX RESOURCES</span>
                  <h3>Resource caps</h3>
                </div>
                <span className="settings-chip chip-warn">advanced</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Sandbox memory"
                    tip="Memory cap for sandboxed agent-generated processes, e.g. 512m or 1g."
                  />
                  <input
                    value={config.sandboxMemory}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, sandboxMemory: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ sandboxMemory: config.sandboxMemory })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Sandbox CPU"
                    tip="CPU percentage cap for sandboxed processes (1-100)."
                  />
                  <input
                    type="number"
                    min="1"
                    max="100"
                    value={config.sandboxCPU}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, sandboxCPU: numberValue(event) }
                          : current,
                      )
                    }
                    onBlur={() => void save({ sandboxCPU: config.sandboxCPU })}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">STORAGE</span>
                  <h3>Limits &amp; retention</h3>
                </div>
                <span className="settings-chip chip-warn">advanced</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Workspace budget"
                    tip="Maximum workspace size in MB before the agent is warned about the budget."
                  />
                  <input
                    type="number"
                    min="16"
                    value={config.maxWorkspaceMb}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, maxWorkspaceMb: numberValue(event) }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ maxWorkspaceMb: config.maxWorkspaceMb })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Sessions kept"
                    tip="How many sessions are retained before the oldest are archived away."
                  />
                  <input
                    type="number"
                    min="10"
                    value={config.maxSessionsKept}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, maxSessionsKept: numberValue(event) }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ maxSessionsKept: config.maxSessionsKept })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Log budget"
                    tip="Maximum size of the application log before rotation trims it."
                  />
                  <input
                    type="number"
                    min="5"
                    value={config.maxLogMb}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, maxLogMb: numberValue(event) }
                          : current,
                      )
                    }
                    onBlur={() => void save({ maxLogMb: config.maxLogMb })}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">AGENT LIMITS</span>
                  <h3>Timeouts &amp; budgets</h3>
                </div>
                <span className="settings-chip chip-warn">advanced</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Run timeout (minutes)"
                    tip="Maximum wall time for one agent turn (0 = unbounded)."
                  />
                  <input
                    type="number"
                    min="0"
                    max="1440"
                    value={config.runTimeoutMinutes ?? 60}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              runTimeoutMinutes: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ runTimeoutMinutes: config.runTimeoutMinutes })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Attachment budget (KB)"
                    tip="Per-message budget for images and file attachments."
                  />
                  <input
                    type="number"
                    min="64"
                    value={config.attachmentsBudgetKb}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              attachmentsBudgetKb: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        attachmentsBudgetKb: config.attachmentsBudgetKb,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Multi-agent depth"
                    tip="How deep the orchestrator may spawn sub-agents."
                  />
                  <input
                    type="number"
                    min="1"
                    max="8"
                    value={config.multiAgentDepth}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, multiAgentDepth: numberValue(event) }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ multiAgentDepth: config.multiAgentDepth })
                    }
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">CODING LAB</span>
                  <h3>Lab internals</h3>
                </div>
                <span className="settings-chip chip-warn">advanced</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Lab workspace root"
                    tip="Where Coding Lab creates its isolated task workspaces."
                  />
                  <input
                    value={config.labWorkspaceRoot}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, labWorkspaceRoot: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ labWorkspaceRoot: config.labWorkspaceRoot })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Command timeout (seconds)"
                    tip="Maximum runtime for ONE lab command before it is killed."
                  />
                  <input
                    type="number"
                    min="5"
                    value={config.labCommandTimeoutSec}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              labCommandTimeoutSec: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        labCommandTimeoutSec: config.labCommandTimeoutSec,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Lab max iterations"
                    tip="How many build/verify/repair cycles one lab task may take."
                  />
                  <input
                    type="number"
                    min="1"
                    value={config.labMaxIterations}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              labMaxIterations: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ labMaxIterations: config.labMaxIterations })
                    }
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">NET SEARCH</span>
                  <h3>Net Search internals</h3>
                </div>
                <span className="settings-chip chip-warn">advanced</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Request timeout (seconds)"
                    tip="Maximum time for one research HTTP operation."
                  />
                  <input
                    type="number"
                    min="5"
                    value={config.researchTimeoutSec}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchTimeoutSec: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        researchTimeoutSec: config.researchTimeoutSec,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Cache lifetime (minutes)"
                    tip="How long research results are reused before refreshing."
                  />
                  <input
                    type="number"
                    min="0"
                    value={config.researchCacheTtlMin}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchCacheTtlMin: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        researchCacheTtlMin: config.researchCacheTtlMin,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="User agent"
                    tip="Identifier research requests present to websites."
                  />
                  <input
                    value={config.researchUserAgent ?? ""}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchUserAgent: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ researchUserAgent: config.researchUserAgent })
                    }
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">BROWSER</span>
                  <h3>Automation</h3>
                </div>
                <span className="settings-chip chip-warn">advanced</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Browser executable"
                    tip="Path to a Chrome/Edge binary the browser tools drive. Empty = automatic discovery."
                  />
                  <input
                    value={config.browserExecutablePath ?? ""}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              browserExecutablePath: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        browserExecutablePath: config.browserExecutablePath,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Slow motion (ms)"
                    tip="Per-action delay for watchable browser automation runs."
                  />
                  <input
                    type="number"
                    min="0"
                    value={config.browserSlowMoMs}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              browserSlowMoMs: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ browserSlowMoMs: config.browserSlowMoMs })
                    }
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Headless browser"
                    tip="Run automation without a visible browser window."
                  />
                  <input
                    type="checkbox"
                    checked={config.browserHeadless}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, browserHeadless: value }
                          : current,
                      );

                      void save({ browserHeadless: value });
                    }}
                  />
                </label>
              </div>
            </section>
          </>
        ) : null}
      </div>
    </div>
  );
}

export default SettingsPanel;
