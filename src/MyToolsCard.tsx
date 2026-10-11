import { useCallback, useEffect, useMemo, useState } from "react";

import {
  api,
  type CustomToolDefinition,
  type CustomToolParam,
  type CustomToolTestResult,
} from "./api";
import { useResource } from "./useResource";

// MyToolsCard — v1.6.0 P3 (spec §8/§10): the custom-tools surface.
//
// Settings → Agent & Tools → My Tools → + Create Tool.
//
// The card implements the REAL end-to-end path: define → validate →
// save → register → test-execute. A tool created here becomes a
// first-class citizen of the SAME registry the agent loop offers
// built-in tools from (source=custom in /api/tools).
//
// Security posture mirrored from the backend:
//   - new tools are created DISABLED (enabling is explicit)
//   - HTTP tools require the network permission; command tools the
//     local-execution permission (the acknowledge checkbox)
//   - bounded timeout + bounded output are always shown, never optional

const PARAM_TYPES: CustomToolParam["type"][] = [
  "string",
  "number",
  "integer",
  "boolean",
  "array",
  "object",
];

type DraftParam = CustomToolParam & { defaultText?: string };

type Draft = {
  name: string;
  shortDescription: string;
  description: string;
  params: DraftParam[];
  executionType: "http" | "command";
  httpMethod: string;
  httpUrl: string;
  httpHeaders: string;
  bodyTemplate: string;
  executable: string;
  commandArgs: string;
  workingDir: string;
  timeoutSeconds: number;
  outputLimitBytes: number;
};

function emptyDraft(): Draft {
  return {
    name: "",
    shortDescription: "",
    description: "",
    params: [],
    executionType: "command",
    httpMethod: "GET",
    httpUrl: "",
    httpHeaders: "",
    bodyTemplate: "",
    executable: "",
    commandArgs: "",
    workingDir: "",
    timeoutSeconds: 30,
    outputLimitBytes: 32768,
  };
}

function definitionFromDraft(draft: Draft): Omit<CustomToolDefinition, "id" | "enabled"> {
  const headers: Record<string, string> = {};

  for (const line of draft.httpHeaders.split("\n")) {
    const idx = line.indexOf(":");
    if (idx > 0) {
      const key = line.slice(0, idx).trim();
      const value = line.slice(idx + 1).trim();
      if (key && value) {
        headers[key] = value;
      }
    }
  }

  const args = draft.commandArgs
    .split(/\s+/)
    .map((a) => a.trim())
    .filter((a) => a.length > 0);

  const params: CustomToolParam[] = draft.params.map((p) => ({
    name: p.name,
    type: p.type,
    required: p.required,
    description: p.description,
  }));

  return {
    name: draft.name.trim(),
    shortDescription: draft.shortDescription.trim(),
    description: draft.description.trim(),
    inputParameters: params,
    executionType: draft.executionType,
    http:
      draft.executionType === "http"
        ? {
            method: draft.httpMethod as CustomToolDefinition["http"] extends
              | { method: infer M }
              | undefined
              ? M
              : never,
            url: draft.httpUrl.trim(),
            headers,
            bodyTemplate: draft.bodyTemplate.trim() || undefined,
          }
        : undefined,
    command:
      draft.executionType === "command"
        ? {
            executable: draft.executable.trim(),
            args,
            workingDir: draft.workingDir.trim() || undefined,
          }
        : undefined,
    // The permission is DERIVED from the execution type + the explicit
    // acknowledgement the UI collects — the backend re-verifies it.
    permission: draft.executionType === "http" ? "network" : "local",
    timeoutSeconds: draft.timeoutSeconds,
    outputLimitBytes: draft.outputLimitBytes,
  };
}

function MyToolsCard() {
  const toolsResource = useResource<CustomToolDefinition[]>(
    "custom-tools",
    (signal) => api.customTools(signal),
  );

  const tools = toolsResource.data ?? [];

  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const [testResults, setTestResults] = useState<Record<string, CustomToolTestResult>>({});
  const [testing, setTesting] = useState<string | null>(null);

  const [permissionAck, setPermissionAck] = useState(false);

  const refresh = useCallback(() => {
    void toolsResource.refresh();
  }, [toolsResource]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 4000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const validation = useMemo(() => validateDraft(draft), [draft]);

  function update<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  function updateParam(index: number, patch: Partial<DraftParam>) {
    setDraft((current) => {
      const params = current.params.map((p, i) =>
        i === index ? { ...p, ...patch } : p,
      );
      return { ...current, params };
    });
  }

  async function saveNewTool() {
    setError(null);

    if (!validation.ok) {
      setError(validation.error);
      return;
    }

    if (!permissionAck) {
      setError(
        "Acknowledge the execution permission first — custom tools are executable capabilities.",
      );
      return;
    }

    setBusy(true);

    try {
      await api.createCustomTool(definitionFromDraft(draft));
      setCreating(false);
      setDraft(emptyDraft());
      setPermissionAck(false);
      setNotice(
        "Tool created — disabled by default. Enable it to offer it to the agent.",
      );
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "save failed");
    } finally {
      setBusy(false);
    }
  }

  async function toggleEnabled(tool: CustomToolDefinition, enabled: boolean) {
    setBusy(true);
    setError(null);

    try {
      await api.updateCustomTool(tool.id, { ...tool, enabled });
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "update failed");
    } finally {
      setBusy(false);
    }
  }

  async function removeTool(tool: CustomToolDefinition) {
    setBusy(true);
    setError(null);

    try {
      await api.deleteCustomTool(tool.id);
      setNotice(`Tool "${tool.name}" deleted.`);
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "delete failed");
    } finally {
      setBusy(false);
    }
  }

  async function testTool(tool: CustomToolDefinition) {
    setTesting(tool.id);
    setError(null);

    // Test with each parameter's example value (empty string default).
    const args: Record<string, unknown> = {};
    for (const p of tool.inputParameters) {
      if (p.type === "number" || p.type === "integer") {
        args[p.name] = 1;
      } else if (p.type === "boolean") {
        args[p.name] = true;
      } else {
        args[p.name] = "test";
      }
    }

    try {
      const result = await api.testCustomTool(tool.id, args);
      setTestResults((current) => ({ ...current, [tool.id]: result }));
    } catch (err) {
      setTestResults((current) => ({
        ...current,
        [tool.id]: {
          ok: false,
          error: err instanceof Error ? err.message : "test failed",
        },
      }));
    } finally {
      setTesting(null);
    }
  }

  return (
    <section className="settings-card settings-card-wide">
      <div className="settings-card-heading">
        <div>
          <span className="eyebrow">MY TOOLS</span>
          <h3>Custom tools</h3>
        </div>
        <button
          type="button"
          className="btn btn-sm"
          onClick={() => {
            setCreating((open) => !open);
            setError(null);
          }}
        >
          {creating ? "Close" : "+ Create Tool"}
        </button>
      </div>

      {toolsResource.error && !tools.length ? (
        <span className="runtime-hint">Custom tools unavailable: {toolsResource.error}</span>
      ) : null}

      {creating ? (
        <div className="custom-tool-form">
          <div className="settings-form-grid">
            <label className="settings-field">
              <span>Tool name</span>
              <input
                value={draft.name}
                placeholder="myTool"
                onChange={(e) => update("name", e.target.value)}
              />
              <span className="runtime-hint">
                Letters, digits, - and _ — cannot shadow a built-in tool.
              </span>
            </label>

            <label className="settings-field">
              <span>Short description</span>
              <input
                value={draft.shortDescription}
                placeholder="One line the tool list shows"
                onChange={(e) => update("shortDescription", e.target.value)}
              />
            </label>
          </div>

          <label className="settings-field">
            <span>Full description</span>
            <textarea
              rows={3}
              value={draft.description}
              placeholder="What the tool does, when to use it — the model reads this."
              onChange={(e) => update("description", e.target.value)}
            />
          </label>

          <div className="settings-form-grid">
            <label className="settings-field">
              <span>Execution type</span>
              <select
                value={draft.executionType}
                onChange={(e) =>
                  update("executionType", e.target.value as Draft["executionType"])
                }
              >
                <option value="command">Local command / script</option>
                <option value="http">HTTP request (HTTPS only)</option>
              </select>
            </label>

            <label className="settings-field">
              <span>Timeout (seconds)</span>
              <input
                type="number"
                min={1}
                max={300}
                value={draft.timeoutSeconds}
                onChange={(e) =>
                  update("timeoutSeconds", Number(e.target.value) || 30)
                }
              />
            </label>

            <label className="settings-field">
              <span>Output limit (bytes)</span>
              <input
                type="number"
                min={256}
                max={1048576}
                value={draft.outputLimitBytes}
                onChange={(e) =>
                  update("outputLimitBytes", Number(e.target.value) || 32768)
                }
              />
            </label>
          </div>

          {draft.executionType === "http" ? (
            <div className="settings-form-grid">
              <label className="settings-field">
                <span>Method</span>
                <select
                  value={draft.httpMethod}
                  onChange={(e) => update("httpMethod", e.target.value)}
                >
                  {["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </label>

              <label className="settings-field">
                <span>HTTPS URL</span>
                <input
                  value={draft.httpUrl}
                  placeholder="https://api.example.com/lookup/$query"
                  onChange={(e) => update("httpUrl", e.target.value)}
                />
                <span className="runtime-hint">
                  $param in the URL is replaced with the parameter value.
                </span>
              </label>

              <label className="settings-field">
                <span>Headers (one per line)</span>
                <textarea
                  rows={3}
                  value={draft.httpHeaders}
                  placeholder={"Authorization: Bearer …\nX-Api-Key: …"}
                  onChange={(e) => update("httpHeaders", e.target.value)}
                />
                <span className="runtime-hint">
                  Header values are stored locally and NEVER shown to the model.
                </span>
              </label>

              <label className="settings-field">
                <span>Body template (JSON, POST/PUT/PATCH)</span>
                <textarea
                  rows={3}
                  value={draft.bodyTemplate}
                  placeholder={'{"query":"$query"}'}
                  onChange={(e) => update("bodyTemplate", e.target.value)}
                />
              </label>
            </div>
          ) : (
            <div className="settings-form-grid">
              <label className="settings-field">
                <span>Executable</span>
                <input
                  value={draft.executable}
                  placeholder="python / node / my-script"
                  onChange={(e) => update("executable", e.target.value)}
                />
              </label>

              <label className="settings-field">
                <span>Arguments</span>
                <input
                  value={draft.commandArgs}
                  placeholder="script.py --input $text"
                  onChange={(e) => update("commandArgs", e.target.value)}
                />
                <span className="runtime-hint">
                  $param is substituted with the parameter value.
                </span>
              </label>

              <label className="settings-field">
                <span>Working directory (optional)</span>
                <input
                  value={draft.workingDir}
                  placeholder="empty = private per-run scratch dir"
                  onChange={(e) => update("workingDir", e.target.value)}
                />
              </label>
            </div>
          )}

          <div className="custom-tool-params">
            <div className="settings-card-heading">
              <div>
                <span className="eyebrow">INPUT</span>
                <h4>Parameters</h4>
              </div>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() =>
                  setDraft((current) => ({
                    ...current,
                    params: [
                      ...current.params,
                      {
                        name: "",
                        type: "string",
                        required: false,
                        description: "",
                      },
                    ],
                  }))
                }
              >
                + Parameter
              </button>
            </div>

            {draft.params.map((param, index) => (
              <div key={index} className="custom-tool-param-row">
                <input
                  className="custom-tool-param-name"
                  value={param.name}
                  placeholder="name"
                  onChange={(e) => updateParam(index, { name: e.target.value })}
                />

                <select
                  value={param.type}
                  onChange={(e) =>
                    updateParam(index, {
                      type: e.target.value as CustomToolParam["type"],
                    })
                  }
                >
                  {PARAM_TYPES.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>

                <input
                  className="custom-tool-param-desc"
                  value={param.description ?? ""}
                  placeholder="description"
                  onChange={(e) => updateParam(index, { description: e.target.value })}
                />

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={param.required}
                    onChange={(e) => updateParam(index, { required: e.target.checked })}
                  />
                  <span>required</span>
                </label>

                <button
                  type="button"
                  className="btn btn-sm"
                  onClick={() =>
                    setDraft((current) => ({
                      ...current,
                      params: current.params.filter((_, i) => i !== index),
                    }))
                  }
                >
                  Remove
                </button>
              </div>
            ))}
          </div>

          <label className="inline-toggle">
            <input
              type="checkbox"
              checked={permissionAck}
              onChange={(e) => setPermissionAck(e.target.checked)}
            />
            <span>
              I understand this tool can{" "}
              {draft.executionType === "http"
                ? "make network requests"
                : "run a local program on this machine"}
              . It stays disabled until I enable it.
            </span>
          </label>

          {error ? <p className="settings-error">{error}</p> : null}

          <div className="custom-tool-actions">
            <button
              type="button"
              className="btn"
              disabled={busy || !validation.ok || !permissionAck}
              onClick={() => void saveNewTool()}
            >
              {busy ? "Saving…" : "Save tool (disabled)"}
            </button>
          </div>
        </div>
      ) : null}

      {notice ? <p className="settings-notice">{notice}</p> : null}

      {tools.length === 0 && !creating ? (
        <span className="runtime-hint">
          No custom tools yet — create one to extend the agent with your own
          capabilities. Custom tools are first-class: they appear in the tool
          list and the agent can call them like built-ins.
        </span>
      ) : null}

      <div className="control-tool-list">
        {tools.map((tool) => {
          const result = testResults[tool.id];

          return (
            <div key={tool.id} className="custom-tool-card">
              <div className="custom-tool-card-head">
                <div>
                  <strong>{tool.name}</strong>
                  <span className="custom-tool-desc">{tool.shortDescription}</span>
                  <span className="runtime-hint">
                    {tool.executionType === "http"
                      ? `HTTP ${tool.http?.method ?? ""} · ${tool.http?.url ?? ""}`
                      : `command · ${tool.command?.executable ?? ""}`}{" "}
                    · timeout {tool.timeoutSeconds}s · output ≤{" "}
                    {tool.outputLimitBytes} B
                  </span>
                </div>

                <div className="custom-tool-card-actions">
                  <label className="inline-toggle">
                    <input
                      type="checkbox"
                      checked={tool.enabled}
                      disabled={busy}
                      onChange={(e) => void toggleEnabled(tool, e.target.checked)}
                    />
                    <span>{tool.enabled ? "enabled" : "disabled"}</span>
                  </label>

                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={testing === tool.id}
                    onClick={() => void testTool(tool)}
                  >
                    {testing === tool.id ? "Testing…" : "Test"}
                  </button>

                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={busy}
                    onClick={() => void removeTool(tool)}
                  >
                    Delete
                  </button>
                </div>
              </div>

              {result ? (
                <div
                  className={`custom-tool-test-result ${
                    result.ok ? "custom-tool-test-ok" : "custom-tool-test-fail"
                  }`}
                >
                  {result.ok
                    ? `OK (${result.duration ?? 0} ms): ${
                        result.output?.slice(0, 500) ?? ""
                      }`
                    : `FAILED: ${result.error ?? "unknown error"}`}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </section>
  );
}

// validateDraft mirrors the backend's client-side-visible contract —
// the backend remains the authority (it re-validates every save).
function validateDraft(draft: Draft): { ok: boolean; error: string | null } {
  if (!/^[a-zA-Z][a-zA-Z0-9_-]{1,39}$/.test(draft.name.trim())) {
    return {
      ok: false,
      error: "Tool name must start with a letter (letters, digits, - and _, 2-40 chars).",
    };
  }

  if (!draft.shortDescription.trim()) {
    return { ok: false, error: "Short description is required." };
  }

  if (!draft.description.trim()) {
    return { ok: false, error: "Full description is required." };
  }

  if (draft.executionType === "http") {
    if (!draft.httpUrl.trim().toLowerCase().startsWith("https://")) {
      return { ok: false, error: "HTTP tools require an https:// URL." };
    }
  } else if (!draft.executable.trim()) {
    return { ok: false, error: "Command tools require an executable." };
  }

  for (const p of draft.params) {
    if (!/^[a-zA-Z][a-zA-Z0-9_-]{1,39}$/.test(p.name.trim())) {
      return { ok: false, error: `Parameter name "${p.name}" is invalid.` };
    }
  }

  if (draft.timeoutSeconds < 1 || draft.timeoutSeconds > 300) {
    return { ok: false, error: "Timeout must be between 1 and 300 seconds." };
  }

  if (draft.outputLimitBytes < 256 || draft.outputLimitBytes > 1048576) {
    return { ok: false, error: "Output limit must be between 256 B and 1 MiB." };
  }

  return { ok: true, error: null };
}

export default MyToolsCard;
