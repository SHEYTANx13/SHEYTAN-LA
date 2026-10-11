import { useCallback, useEffect, useRef, useState } from "react";

import {
  api,
  type Model,
  type RuntimeConfig,
  type RuntimeRecommendation,
  type AppUpdateStatus,
  type TaskProfileInfo,
} from "./api";
import { DownloadProgressPanel } from "./DownloadProgress";
import { useRuntimeStore } from "./store";
import { visionBadge } from "./vision";

// v1.2.0 — Settings surfaces for the productisation release:
//
//   SimplePerformanceCard — the Simple level: three postures (Quiet /
//     Balanced / Maximum) + recommended context + automatic vision/GPU.
//     Each posture is a REAL task profile resolved by the recommendation
//     engine and applied through the existing config save path.
//   TaskProfileCard       — the full task-aware profile picker.
//   VisionCard            — the dedicated Vision section: real controls
//     plus the DETECTED projector evidence card.
//   UpdatesCard           — the application update surface (manifest-
//     verified check + stage; the staged installer is applied by the user
//     or OS — never executed behind the user's back).

type SaveFn = (patch: Record<string, unknown>) => Promise<void>;

const POSTURES: {
  id: string;
  label: string;
  description: string;
  task: string;
}[] = [
  {
    id: "quiet",
    label: "Quiet",
    description:
      "Cool and silent — half the cores, smaller context, projector on CPU.",
    task: "low-power",
  },
  {
    id: "balanced",
    label: "Balanced",
    description:
      "The everyday posture — recommended context, GPU when available.",
    task: "chat",
  },
  {
    id: "maximum",
    label: "Maximum",
    description: "Everything the machine has verified it can run.",
    task: "maximum",
  },
];

export function SimplePerformanceCard({
  config,
  save,
}: {
  config: RuntimeConfig;
  save: SaveFn;
}) {
  const [busy, setBusy] = useState<string | null>(null);
  const [applied, setApplied] = useState<string | null>(
    config.runtimeProfile || null,
  );
  const [rec, setRec] = useState<RuntimeRecommendation | null>(null);
  const [reasons, setReasons] = useState<string[]>([]);

  // The recommended-context chip comes from the CURRENT profile's engine
  // output (evidence-based, not invented).
  useEffect(() => {
    let cancelled = false;

    api
      .recommendation("", rec?.task ?? config.runtimeProfile ?? "chat")
      .then((payload) => {
        if (cancelled) return;
        setRec(payload.recommended ?? null);
        setReasons(payload.recommended?.reasons ?? []);
      })
      .catch(() => {
        if (!cancelled) setRec(null);
      });

    return () => {
      cancelled = true;
    };
  }, [rec?.task, config.runtimeProfile]);

  const applyPosture = useCallback(
    async (posture: (typeof POSTURES)[number]) => {
      setBusy(posture.id);

      try {
        const payload = await api.recommendation("", posture.task);
        const r = payload.recommended;

        if (r) {
          await save({
            llm: {
              numCtx: r.context,
              numThread: r.threads,
              numGpu: r.gpuLayers,
            },
            // v1.3.0 fix: ubatchSize is a TOP-LEVEL config field
            // (--ubatch-size); the v1.2.9 apply wrote it under llm where
            // no such field exists — a silent no-op.
            ubatchSize: r.ubatchSize,
            gpuAutoOffload: r.gpuAutoOffload,
            flashAttention: r.flashAttention,
            kvCacheQuant: r.kvCacheQuant,
            visionMmprojOffload: r.mmprojOffload,
            runtimeProfile: posture.task,
          });
          setApplied(posture.task);
          setRec(r);
          setReasons(r.reasons);
        }
      } catch {
        // save() surfaces errors through the shared save state.
      } finally {
        setBusy(null);
      }
    },
    [save],
  );

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">SIMPLE</span>
          <h3>Performance posture</h3>
        </div>
        {applied ? (
          <span className="settings-chip chip-good">profile: {applied}</span>
        ) : null}
      </div>

      <div className="posture-grid">
        {POSTURES.map((posture) => (
          <button
            key={posture.id}
            type="button"
            className={`posture-card${applied === posture.task ? " active" : ""}`}
            onClick={() => void applyPosture(posture)}
            disabled={busy !== null}
          >
            <strong>{busy === posture.id ? "Applying…" : posture.label}</strong>
            <span>{posture.description}</span>
          </button>
        ))}
      </div>

      <div className="env-grid">
        <div className="session-detail">
          <span>Context</span>
          <strong>
            {rec?.context
              ? `Recommended: ${(rec.context / 1024).toLocaleString()}K`
              : config.llm.numCtx
                ? `Current: ${(config.llm.numCtx / 1024).toLocaleString()}K`
                : "—"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Vision</span>
          <strong>Automatic</strong>
        </div>

        <div className="session-detail">
          <span>GPU</span>
          <strong>{config.gpuAutoOffload ? "Automatic" : "Off"}</strong>
        </div>

        <div className="session-detail">
          <span>Predicted effect</span>
          <strong>
            {rec?.predicted?.speed
              ? `Speed ${rec.predicted.speed} · ${
                  rec.predicted.class ?? "unassessed"
                }`
              : "—"}
          </strong>
        </div>
      </div>

      {reasons.length > 0 ? (
        <details className="env-reasons">
          <summary>Why these values ({reasons.length})</summary>

          <ul>
            {reasons.map((reason, index) => (
              <li key={index}>{reason}</li>
            ))}
          </ul>
        </details>
      ) : null}

      <span className="runtime-hint">
        Predicted, never measured — Live metrics below verifies the effect after
        the engine restarts.
      </span>
    </section>
  );
}

export function TaskProfileCard({
  config,
  save,
}: {
  config: RuntimeConfig;
  save: SaveFn;
}) {
  const [tasks, setTasks] = useState<TaskProfileInfo[]>([]);
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    api
      .recommendation("", "")
      .then((payload) => {
        if (!cancelled) setTasks(payload.available);
      })
      .catch(() => {
        /* picker stays empty; the Simple card remains usable */
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const apply = useCallback(
    async (task: TaskProfileInfo) => {
      setBusy(task.id);

      try {
        const payload = await api.recommendation("", task.id);
        const r = payload.recommended;

        if (r) {
          await save({
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
            runtimeProfile: task.id,
          });
        }
      } catch {
        // save() surfaces errors through the shared save state.
      } finally {
        setBusy(null);
      }
    },
    [save],
  );

  if (tasks.length === 0) {
    return null;
  }

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">TASK PROFILES</span>
          <h3>What are you using SHEYTAN for?</h3>
        </div>
        <span className="settings-chip chip-neutral">
          active: {config.runtimeProfile || "chat"}
        </span>
      </div>

      <div className="profile-grid">
        {tasks.map((task) => (
          <button
            key={task.id}
            type="button"
            className={`profile-card${
              (config.runtimeProfile || "chat") === task.id ? " active" : ""
            }`}
            onClick={() => void apply(task)}
            disabled={busy !== null}
            title={task.description}
          >
            <strong>{busy === task.id ? "Applying…" : task.label}</strong>
            <span>{task.description}</span>
          </button>
        ))}
      </div>

      <span className="runtime-hint">
        A profile changes ONLY the settings that matter for that task. The
        engine restarts when an engine-affecting value changed.
      </span>
    </section>
  );
}

// VisionCard renders the vision surface in ONE of two variants
// (v1.3.0 settings restructure):
//
//   variant="status"  (Models tab) — user-facing capability view:
//     "Vision ✓ Available — Projector: Automatically managed", the
//     detected-projector evidence, and the enable toggle. NO raw path
//     fields, no projector file pickers.
//   variant="control" (Advanced tab) — the explicit projector override
//     select + GPU offload control for experts.
export function VisionCard({
  config,
  variant = "status",
}: {
  config: RuntimeConfig;
  variant?: "status" | "control";
}) {
  const models = useRuntimeStore((state) => state.models);
  const refreshModels = useRuntimeStore((state) => state.refreshModels);
  const [saving, setSaving] = useState(false);
  const [projectorChoice, setProjectorChoice] = useState(
    config.visionMmproj || "",
  );

  // The serving model's vision evidence drives the status card.
  const serving: Model | undefined = (models?.local ?? []).find(
    (m) => m.serving,
  );
  const vision = visionBadge(
    serving?.visionState,
    serving?.visionReason,
    serving?.mmprojName,
  );

  const saveVision = useCallback(
    async (patch: Record<string, unknown>) => {
      setSaving(true);
      try {
        await api.updateConfig(patch);
        await refreshModels();
      } catch {
        // surfaced via config surfaces
      } finally {
        setSaving(false);
      }
    },
    [refreshModels],
  );

  return (
    <>
      <section className="settings-card">
        <div className="settings-card-heading">
          <div>
            <span className="eyebrow">
              {variant === "control" ? "VISION — ADVANCED" : "VISION"}
            </span>
            <h3>
              {variant === "control"
                ? "Multimodal runtime"
                : "Vision capability"}
            </h3>
          </div>
          <span
            className={`settings-chip chip-${vision.tone === "good" ? "good" : vision.tone === "bad" ? "bad" : "warn"}`}
          >
            {vision.symbol} {vision.label}
          </span>
        </div>

        <div className="settings-form-grid">
          <label className="inline-toggle">
            <input
              type="checkbox"
              checked={config.visionEnabled}
              onChange={(event) =>
                void saveVision({ visionEnabled: event.target.checked })
              }
              disabled={saving}
            />
            <span>Enable vision</span>
          </label>

          {variant === "status" ? (
            <div className="session-detail">
              <span>Projector</span>
              <strong>Automatically managed</strong>
              <span className="runtime-hint">
                The mmproj projector is detected, matched to the model family,
                loaded and verified automatically — no path configuration
                needed.
              </span>
            </div>
          ) : (
            <>
              <label className="settings-field">
                <span>Projector override</span>
                <select
                  value={projectorChoice}
                  onChange={(event) => {
                    setProjectorChoice(event.target.value);
                    void saveVision({ visionMmproj: event.target.value });
                  }}
                  disabled={saving}
                >
                  <option value="">Automatic (pair by model family)</option>
                  {(models?.local ?? [])
                    .filter((m) => m.mmprojName)
                    .map((m) => (
                      <option key={m.mmprojPath} value={m.mmprojName}>
                        {m.mmprojName}
                      </option>
                    ))}
                </select>
              </label>

              <label className="settings-field">
                <span>Projector GPU offload</span>
                <select
                  value={config.visionMmprojOffload || "auto"}
                  onChange={(event) =>
                    void saveVision({ visionMmprojOffload: event.target.value })
                  }
                  disabled={saving}
                >
                  <option value="auto">Automatic (recommended)</option>
                  <option value="on">On</option>
                  <option value="off">Off (CPU projector)</option>
                </select>
              </label>
            </>
          )}
        </div>

        <span className="runtime-hint">
          Image handling: max 4 images per message, 2048 px per side, re-encoded
          to fit a 6 MB budget — enforced by the backend vision package
          (constants, not tunables).
        </span>
      </section>

      <section className="settings-card">
        <div className="settings-card-heading">
          <div>
            <span className="eyebrow">DETECTED PROJECTOR</span>
            <h3>
              {serving?.mmprojName ?? "None paired with the serving model"}
            </h3>
          </div>
        </div>

        {serving?.mmprojName ? (
          <div className="env-grid">
            <div className="session-detail">
              <span>File</span>
              <strong title={serving.mmprojPath}>{serving.mmprojName}</strong>
            </div>
            <div className="session-detail">
              <span>Size</span>
              <strong>
                {serving.mmprojSizeBytes
                  ? `${(serving.mmprojSizeBytes / 1024 ** 3).toFixed(2)} GB`
                  : "—"}
              </strong>
            </div>
            <div className="session-detail">
              <span>Model family</span>
              <strong>{serving.architecture ?? "—"}</strong>
            </div>
            <div className="session-detail">
              <span>Status</span>
              <strong title={vision.title}>{vision.label}</strong>
            </div>
          </div>
        ) : (
          <span className="runtime-hint">
            {vision.title} — place the model family's mmproj file in the models
            folder and restart the engine.
          </span>
        )}
      </section>
    </>
  );
}

export function UpdatesCard() {
  const appVersion = useRuntimeStore((state) => state.app?.appVersion);
  const [status, setStatus] = useState<AppUpdateStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // v1.2.3: the download endpoint returns immediately now; progress is
  // observed by polling /update/status while the state is "downloading".
  const pollRef = useRef<number | null>(null);

  const load = useCallback(() => {
    api
      .updateStatus()
      .then(setStatus)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "status unavailable");
      });
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Poll while a staging download runs; stop the moment it settles.
  useEffect(() => {
    if (status?.state === "downloading") {
      pollRef.current = window.setInterval(load, 1200);
      return () => {
        if (pollRef.current !== null) {
          window.clearInterval(pollRef.current);
          pollRef.current = null;
        }
      };
    }
    return undefined;
  }, [status?.state, load]);

  const check = useCallback(async () => {
    setBusy(true);
    setError(null);

    try {
      setStatus(await api.updateCheck());
    } catch (err) {
      setError(err instanceof Error ? err.message : "check failed");
    } finally {
      setBusy(false);
    }
  }, []);

  // v1.2.3: START staging; the response arrives immediately and the
  // poller above tracks live progress (bytes, speed, ETA, source).
  const stage = useCallback(async () => {
    setBusy(true);
    setError(null);

    try {
      setStatus(await api.updateDownload());
    } catch (err) {
      setError(err instanceof Error ? err.message : "download failed");
      load();
    } finally {
      setBusy(false);
    }
  }, [load]);

  const cancelDownload = useCallback(async () => {
    setBusy(true);
    try {
      setStatus(await api.updateCancel());
    } catch (err) {
      setError(err instanceof Error ? err.message : "cancel failed");
    } finally {
      setBusy(false);
      load();
    }
  }, [load]);

  const state = status?.state ?? "unknown";

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">UPDATES</span>
          <h3>SHEYTAN-LA updates</h3>
        </div>
        <span
          className={`settings-chip chip-${
            state === "up-to-date"
              ? "good"
              : state === "update-available" || state === "ready"
                ? "accent"
                : state === "failed" || state === "check-failed"
                  ? "bad"
                  : "neutral"
          }`}
        >
          {status?.message ?? state}
        </span>
      </div>

      <div className="env-grid">
        <div className="session-detail">
          <span>Current version</span>
          <strong>v{status?.version ?? appVersion ?? "\u2014"}</strong>
        </div>

        <div className="session-detail">
          <span>Latest</span>
          <strong>{status?.latest ? `v${status.latest}` : "\u2014"}</strong>
        </div>

        <div className="session-detail">
          <span>Channel</span>
          <strong>{status?.channel ?? "stable"}</strong>
        </div>

        <div className="session-detail">
          <span>Checked</span>
          <strong>
            {status?.checkedAt
              ? new Date(status.checkedAt).toLocaleString()
              : "\u2014"}
          </strong>
        </div>
      </div>

      {status?.notes ? <p className="runtime-hint">{status.notes}</p> : null}

      {status?.stagedPath ? (
        <span className="runtime-hint">
          Verified artifact staged at <code>{status.stagedPath}</code> — run it
          to apply the update. Models, sessions and configuration are preserved.
        </span>
      ) : null}

      {state === "downloading" || status?.download ? (
        <DownloadProgressPanel
          progress={status?.download}
          onCancel={() => void cancelDownload()}
          busy={busy}
        />
      ) : null}

      {error ? <p className="feedback-error">{error}</p> : null}

      <div className="header-actions">
        <button
          type="button"
          className="secondary-button"
          onClick={() => void check()}
          disabled={busy}
        >
          {busy ? "Working\u2026" : "Check now"}
        </button>

        {state === "update-available" || state === "failed" ? (
          <button
            type="button"
            className="primary-button"
            onClick={() => void stage()}
            disabled={busy}
          >
            {busy
              ? "Starting\u2026"
              : state === "failed"
                ? "Retry download"
                : "Download & verify"}
          </button>
        ) : null}
      </div>

      <span className="runtime-hint">
        Updates are verified against the release manifest's SHA-256 before
        staging. Downloads stream to disk, resume when interrupted, and never
        stage a byte that fails verification. This build never applies an update
        silently and never touches your models.
      </span>
    </section>
  );
}
