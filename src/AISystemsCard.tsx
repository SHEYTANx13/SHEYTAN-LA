import { useCallback, useEffect, useState } from "react";

import { api, type AISystem, type SystemsPayload } from "./api";
import { systemsView } from "./ai-systems-view";

// AISystemsCard.tsx — v1.9.0 (spec §3/§15): the compact AI System
// selector + editor inside the System Centre. This is the user-owned
// object's ONLY surface: create, edit (revision increments), activate
// (the NEXT run binds the snapshot), clone, export, import, delete.
// Everything talks to /api/systems; nothing is client-side policy.

const EMPTY_DRAFT = {
  name: "",
  instructions: "",
  model: "",
  reasoning: "",
  approvalPolicy: "ask-risky",
  verificationPolicy: "standard",
};

type Draft = typeof EMPTY_DRAFT;

function toDraft(s: AISystem | null): Draft {
  if (!s) return { ...EMPTY_DRAFT };
  return {
    name: s.name,
    instructions: s.instructions,
    model: s.model ?? "",
    reasoning: s.reasoning ?? "",
    approvalPolicy: s.approvalPolicy || "ask-risky",
    verificationPolicy: s.verificationPolicy || "standard",
  };
}

export function AISystemsCard() {
  const [payload, setPayload] = useState<SystemsPayload | null>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Draft>({ ...EMPTY_DRAFT });
  const [busy, setBusy] = useState(false);
  // v1.9.0: the editor stays open in DRAFT mode after "New system" —
  // hiding it made the create flow unreachable.
  const [draftMode, setDraftMode] = useState(false);

  const reload = useCallback(async () => {
    setStatus((prev) => (prev === "ready" ? "ready" : "loading"));
    try {
      const next = await api.systems();
      setPayload(next);
      setStatus("ready");
      setError(null);
      setSelectedId((prev) => prev ?? next.activeSystemId);
    } catch (e) {
      setStatus("error");
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const selected = payload?.systems.find((s) => s.systemId === selectedId) ?? null;
  const isActive = selected?.systemId === payload?.activeSystemId;

  useEffect(() => {
    setDraft(toDraft(selected));
    if (selectedId) {
      setDraftMode(false);
    }
  }, [selectedId]); // eslint-disable-line react-hooks/exhaustive-deps

  const run = useCallback(
    async (fn: () => Promise<string>, fallback: string) => {
      setBusy(true);
      setNote(null);
      try {
        setNote(await fn());
        await reload();
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
        setNote(fallback);
      } finally {
        setBusy(false);
      }
    },
    [reload],
  );

  const rows = systemsView(payload);

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <h2>
          <span className="eyebrow">V1.9 — AI SYSTEMS</span>
        </h2>
        <span className="settings-chip chip-neutral">
          {status === "ready" && payload
            ? `${payload.systems.length} system${payload.systems.length === 1 ? "" : "s"}`
            : status === "loading"
              ? "loading"
              : "unavailable"}
        </span>
      </div>

      <p className="session-detail">
        The ACTIVE AI System binds the next run: instructions, model, reasoning
        effort, tool surface, approval and verification policy. Editing bumps the
        revision; a running keeps its frozen snapshot.
      </p>

      {status === "error" ? (
        <div className="settings-card">
          <span className="settings-chip chip-warn">unavailable</span>
          <p className="env-loading">{error ?? "the AI System store did not answer"}</p>
          <button className="secondary-button" onClick={() => void reload()}>
            Retry
          </button>
        </div>
      ) : null}

      {status === "ready" && rows.length > 0 ? (
        <div className="header-actions">
          <select
            aria-label="Active AI System"
            value={selectedId ?? ""}
            onChange={(e) => setSelectedId(e.target.value)}
          >
            {rows.map((row) => (
              <option key={row.system.systemId} value={row.system.systemId}>
                {row.label}
                {row.active ? " — ACTIVE" : ""}
              </option>
            ))}
          </select>
        </div>
      ) : null}

      {note ? <p className="session-detail">{note}</p> : null}

      {selected || draftMode ? (
        <div className="settings-card">
          <div className="panel-heading">
            <span className="eyebrow">
              {selected
                ? `EDIT — ${selected.name} (rev ${selected.revision})`
                : "NEW SYSTEM"}
            </span>
          </div>
          <label className="session-detail">
            Name
            <input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </label>
          <label className="session-detail">
            Instructions
            <textarea
              rows={5}
              value={draft.instructions}
              onChange={(e) => setDraft({ ...draft, instructions: e.target.value })}
            />
          </label>
          <div className="header-actions">
            <label className="session-detail">
              Model override
              <input
                placeholder="(runtime default)"
                value={draft.model}
                onChange={(e) => setDraft({ ...draft, model: e.target.value })}
              />
            </label>
            <label className="session-detail">
              Reasoning
              <select
                value={draft.reasoning}
                onChange={(e) => setDraft({ ...draft, reasoning: e.target.value })}
              >
                <option value="">(runtime default)</option>
                <option value="low">low — budget 0</option>
                <option value="mid">mid — 1024</option>
                <option value="high">high — 4096</option>
                <option value="ultra">ultra — engine default</option>
              </select>
            </label>
            <label className="session-detail">
              Approval policy
              <select
                value={draft.approvalPolicy}
                onChange={(e) => setDraft({ ...draft, approvalPolicy: e.target.value })}
              >
                <option value="auto">auto</option>
                <option value="ask-risky">ask for risky calls</option>
                <option value="ask-all">ask for every call</option>
              </select>
            </label>
          </div>
          <div className="header-actions">
            <button
              className="secondary-button"
              disabled={busy || !selected}
              onClick={() =>
                void run(
                  () =>
                    api
                      .updateSystem(selected!.systemId, draft)
                      .then((s) => `saved — now rev ${s.revision}`),
                  "save failed",
                )
              }
            >
              Save (bumps revision)
            </button>
            <button
              className="secondary-button"
              disabled={busy || !selected || isActive}
              onClick={() =>
                void run(
                  () =>
                    api
                      .activateSystem(selected!.systemId)
                      .then((r) => `activated rev ${r.revision} — the next run binds it`),
                  "activation failed",
                )
              }
            >
              {isActive ? "Active" : "Activate"}
            </button>
            <button
              className="secondary-button"
              disabled={busy || !selected}
              onClick={() =>
                void run(
                  () =>
                    api.cloneSystem(selected!.systemId).then((s) => `cloned as ${s.name}`),
                  "clone failed",
                )
              }
            >
              Clone
            </button>
            <button
              className="secondary-button"
              disabled={busy || !selected || selected.systemId === "default"}
              onClick={() =>
                void run(
                  () =>
                    api
                      .deleteSystem(selected!.systemId)
                      .then(() => {
                        setSelectedId(null);
                        return "deleted";
                      }),
                  "delete failed",
                )
              }
            >
              Delete
            </button>
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  const doc = await api.exportSystem(selected!.systemId);
                  const blob = new Blob([doc], { type: "application/json" });
                  const url = URL.createObjectURL(blob);
                  const a = document.createElement("a");
                  a.href = url;
                  a.download = `ai-system-${selected!.systemId}.json`;
                  a.click();
                  URL.revokeObjectURL(url);
                  return "exported";
                }, "export failed")
              }
            >
              Export
            </button>
            <label className="secondary-button" style={{ cursor: "pointer" }}>
              Import
              <input
                type="file"
                accept="application/json"
                style={{ display: "none" }}
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (!file) return;
                  void file
                    .text()
                    .then((doc) =>
                      run(() => api.importSystem(doc).then((s) => `imported as ${s.name}`), "import failed"),
                    );
                  e.target.value = "";
                }}
              />
            </label>
          </div>
          <button
            className="secondary-button"
            disabled={busy}
            onClick={() => {
              setDraft({ ...EMPTY_DRAFT });
              setSelectedId(null);
              setDraftMode(true);
              setNote("new system — fill the fields and Create");
            }}
          >
            New system
          </button>
          <button
            className="secondary-button"
            disabled={busy || !draft.name.trim()}
            onClick={() =>
              void run(
                () =>
                  api
                    .createSystem(draft)
                    .then((s) => {
                      setSelectedId(s.systemId);
                      return `created ${s.name} (rev 1)`;
                    }),
                "create failed",
              )
            }
          >
            Create
          </button>
        </div>
      ) : null}
    </div>
  );
}
