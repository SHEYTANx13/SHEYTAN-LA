// ComposerControls.tsx — per-request composer controls.
//
// REAL controls (not visual-only):
//
//   Thinking ▾    low | mid | high | ultra — sent with every run request;
//                 each level carries a REAL numeric thinking-token budget
//                 that reaches the generation request (llama.cpp
//                 `reasoning_budget_tokens`), plus the tier posture
//                 (v1.8.5 four-level ladder; see agent.WithThinkingMode).
//
//   Show/Hide Thinking — VISIBILITY ONLY (v1.8.5). Never changes
//                 generation settings: the reasoning level and budget
//                 travel with every request regardless, the store keeps
//                 folding reasoning snapshots, and only the presentation
//                 is gated. Actual displayed reasoning always comes from
//                 backend-reported reasoning — never fabricated.
//
//   Tools ▾       AUTO | MANUAL + per-tool checkboxes — in MANUAL only
//                 the selected tools are offered and executable; the
//                 restriction is enforced server-side and is never
//                 silently re-enabled (see agent.WithToolPolicy).
//
//   Net Search    v1.3.6 (spec §23/§25): explicit per-request Net Search
//                 intent, enforced server-side (agent.WithNetSearch). The
//                 state chip reflects the REAL wire evidence: searching →
//                 results / failed — external results are provenance,
//                 never model-invented.
//
// The live status chip (backend "status" events: measured tier/token
// telemetry) rides beside them so the user sees what the controls did.

import { useMemo, useState } from "react";

import { useRuntimeStore } from "./store";
import {
  THINKING_OPTIONS,
  type ThinkingControl,
  type NetSearchState,
} from "./run-events";

function ComposerControls() {
  const thinkingControl = useRuntimeStore((s) => s.thinkingControl);
  const setThinkingControl = useRuntimeStore((s) => s.setThinkingControl);
  const showThinking = useRuntimeStore((s) => s.showThinking);
  const setShowThinking = useRuntimeStore((s) => s.setShowThinking);
  const toolPolicyMode = useRuntimeStore((s) => s.toolPolicyMode);
  const setToolPolicyMode = useRuntimeStore((s) => s.setToolPolicyMode);
  const toolAllowlist = useRuntimeStore((s) => s.toolAllowlist);
  const setToolAllowed = useRuntimeStore((s) => s.setToolAllowed);
  const tools = useRuntimeStore((s) => s.tools);
  const liveStatus = useRuntimeStore((s) => s.liveStatus);

  // v1.3.6: Net Search control state.
  const netSearch = useRuntimeStore((s) => s.netSearch);
  const setNetSearch = useRuntimeStore((s) => s.setNetSearch);
  const netSearchState = useRuntimeStore((s) => s.netSearchState);
  const netSearchResultCount = useRuntimeStore((s) => s.netSearchResultCount);

  const [thinkingOpen, setThinkingOpen] = useState(false);
  const [toolsOpen, setToolsOpen] = useState(false);

  const thinkingLabel = useMemo(() => {
    const found = THINKING_OPTIONS.find((o) => o.value === thinkingControl);
    return found ? found.label : "Mid";
  }, [thinkingControl]);

  const allowedCount = toolAllowlist.length;

  return (
    <div className="composer-controls" role="group" aria-label="Request controls">
      {/* Thinking control */}
      <div className="control-menu">
        <button
          type="button"
          className={`control-button${thinkingControl !== "mid" ? " active" : ""}`}
          aria-haspopup="listbox"
          aria-expanded={thinkingOpen}
          onClick={() => {
            setThinkingOpen((v) => !v);
            setToolsOpen(false);
          }}
          title="Reasoning depth for this request: every level carries a real thinking-token budget on the generation request"
        >
          Thinking · {thinkingLabel} ▾
        </button>

        {thinkingOpen ? (
          <div className="control-menu-panel" role="listbox">
            {THINKING_OPTIONS.map((option) => (
              <button
                key={option.value}
                type="button"
                role="option"
                aria-selected={thinkingControl === option.value}
                className={`control-option${
                  thinkingControl === option.value ? " selected" : ""
                }`}
                onClick={() => {
                  setThinkingControl(option.value as ThinkingControl);
                  setThinkingOpen(false);
                }}
              >
                <span className="control-option-label">{option.label}</span>
                <span className="control-option-hint">{option.hint}</span>
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {/* v1.8.5 SHOW/HIDE THINKING — visibility ONLY. Never changes the
          generation settings: the reasoning level and its budget travel
          with every request regardless of this toggle, and the store keeps
          folding reasoning snapshots — only the presentation is gated. */}
      <div className="control-menu">
        <button
          type="button"
          className={`control-button${showThinking ? "" : " active"}`}
          aria-pressed={!showThinking}
          onClick={() => setShowThinking(!showThinking)}
          title="Shows or hides the model's backend-reported reasoning. Visibility only — it never changes the reasoning level, the thinking budget or any generation setting."
        >
          {showThinking ? "Hide Thinking" : "Show Thinking"}
        </button>
      </div>

      {/* Tool policy control */}
      <div className="control-menu">
        <button
          type="button"
          className={`control-button${toolPolicyMode === "manual" ? " active" : ""}`}
          aria-haspopup="listbox"
          aria-expanded={toolsOpen}
          onClick={() => {
            setToolsOpen((v) => !v);
            setThinkingOpen(false);
          }}
          title="AUTO lets the agent choose tools; MANUAL restricts execution to your selection"
        >
          Tools · {toolPolicyMode === "manual" ? `Manual (${allowedCount})` : "Auto"} ▾
        </button>

        {toolsOpen ? (
          <div className="control-menu-panel control-tools-panel" role="listbox">
            <div className="control-policy-row">
              <button
                type="button"
                className={`control-policy${toolPolicyMode === "auto" ? " selected" : ""}`}
                onClick={() => setToolPolicyMode("auto")}
              >
                AUTO
              </button>
              <button
                type="button"
                className={`control-policy${toolPolicyMode === "manual" ? " selected" : ""}`}
                onClick={() => setToolPolicyMode("manual")}
              >
                MANUAL
              </button>
            </div>

            {toolPolicyMode === "manual" ? (
              <div className="control-tool-list">
                {tools.length === 0 ? (
                  <span className="runtime-hint">No tools registered</span>
                ) : (
                  tools.map((tool) => {
                    const checked = toolAllowlist.some(
                      (n) => n.toLowerCase() === tool.name.toLowerCase(),
                    );

                    return (
                      <label key={tool.name} className="control-tool-row">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={(e) =>
                            setToolAllowed(tool.name, e.target.checked)
                          }
                        />
                        <span className="control-tool-name">{tool.name}</span>
                        {tool.description ? (
                          <span className="control-tool-desc">
                            {tool.description}
                          </span>
                        ) : null}
                      </label>
                    );
                  })
                )}

                {allowedCount === 0 ? (
                  <span className="runtime-hint control-tools-hint">
                    Nothing selected — manual mode allows no tools (pure chat).
                  </span>
                ) : null}
              </div>
            ) : (
              <span className="control-option-hint control-tools-hint">
                The agent selects the tools each task needs; restrictions can
                be imposed per request below.
              </span>
            )}
          </div>
        ) : null}
      </div>

      {/* v1.3.6 (spec §23/§25): Net Search control — REAL per-request
          action, enforced server-side. The evidence chip renders the
          measured states from wire events (never invented). */}
      <div className="control-menu">
        <button
          type="button"
          className={`control-button${netSearch ? " active" : ""}`}
          aria-pressed={netSearch}
          onClick={() => setNetSearch(!netSearch)}
          title="External search for this request: results are evidence with provenance, fetched by the Net Search backend — not invented by the model"
        >
          Net Search{netSearch ? netSearchStateSuffix(netSearchState, netSearchResultCount) : ""}
        </button>
      </div>

      {/* Live status chip — backend "status" events only (measured values) */}
      {liveStatus ? (
        <span className="control-status-chip" role="status">
          {liveStatus}
        </span>
      ) : null}
    </div>
  );
}

// netSearchStateSuffix renders the live evidence states (spec §25):
// SEARCHING / RESULTS AVAILABLE (with the stated count when known) /
// SEARCH FAILED. OFF and ENABLED are carried by the button itself.
function netSearchStateSuffix(
  state: NetSearchState,
  resultCount: number | null,
): string {
  switch (state) {
    case "searching":
      return " · Searching…";
    case "results":
      return resultCount !== null
        ? ` · ${resultCount} results`
        : " · Results";
    case "failed":
      return " · Failed";
    default:
      return "";
  }
}

export default ComposerControls;
