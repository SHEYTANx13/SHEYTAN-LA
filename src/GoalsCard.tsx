import { useCallback, useEffect, useState } from "react";

import { api, type Goal, type GoalsPayload } from "./api";
import { describeGoalState, goalProgress, isGoalLive } from "./ai-systems-view";

// GoalsCard.tsx — v1.9.0 (spec §4/§15): the compact durable Goal surface.
// Progress derives from ACTUAL plan state; pause/resume/cancel/approve/
// reject are real backend actions over /api/goals; approval parking is
// durable (the pending call survives a reload). No giant dashboard —
// one honest list with the minimal controls the lifecycle needs.

const STATUS_TONE: Record<string, string> = {
  active: "chip-good",
  completed: "chip-good",
  waiting_for_approval: "chip-warn",
  waiting_for_resource: "chip-warn",
  paused: "chip-neutral",
  blocked: "chip-warn",
  failed: "chip-bad",
  cancelled: "chip-neutral",
};

export function GoalsCard() {
  const [payload, setPayload] = useState<GoalsPayload | null>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    try {
      const next = await api.goals();
      setPayload(next);
      setStatus("ready");
      setError(null);
    } catch (e) {
      setStatus("error");
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  // The goal list refreshes on a light interval ONLY while a goal is
  // live — the drive writes checkpoints to disk and the honest state is
  // one fetch away. No streaming, no fake progress ticks.
  useEffect(() => {
    const anyLive = payload?.goals.some(isGoalLive);
    if (!anyLive) return;
    const timer = window.setInterval(() => void reload(), 3000);
    return () => window.clearInterval(timer);
  }, [payload, reload]);

  const act = useCallback(
    async (fn: () => Promise<unknown>, done: string) => {
      setBusy(true);
      setNote(null);
      try {
        await fn();
        setNote(done);
        await reload();
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setBusy(false);
      }
    },
    [reload],
  );

  const goals: Goal[] = Array.isArray(payload?.goals) ? payload!.goals : [];

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <h2>
          <span className="eyebrow">V1.9 — GOALS</span>
        </h2>
        <span className="settings-chip chip-neutral">
          {status === "ready" ? `${goals.length} goal${goals.length === 1 ? "" : "s"}` : status}
        </span>
      </div>

      <p className="session-detail">
        A Goal is durable long-horizon work: plan → act → verify with
        checkpoints. Paused goals resume from their checkpoint; terminal goals
        stay settled. Completion requires objective verification — a model claim
        never proves it.
      </p>

      <div className="header-actions">
        <input
          placeholder="Describe a long-horizon objective…"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button
          className="secondary-button"
          disabled={busy || !draft.trim()}
          onClick={() =>
            void act(async () => {
              await api.createGoal({ goal: draft.trim(), start: true });
              setDraft("");
            }, "goal created and started")
          }
        >
          Start goal
        </button>
      </div>

      {note ? <p className="session-detail">{note}</p> : null}
      {status === "error" ? (
        <p className="env-loading">{error ?? "the goal store did not answer"}</p>
      ) : null}

      {goals.map((goal) => {
        const { done, total } = goalProgress(goal);
        return (
          <div className="settings-card" key={goal.goalId}>
            <div className="panel-heading">
              <span className="eyebrow">{goal.status.toUpperCase()}</span>
              <span className={`settings-chip ${STATUS_TONE[goal.status] ?? "chip-neutral"}`}>
                {describeGoalState(goal)}
              </span>
            </div>
            <p className="session-detail">{goal.originalGoal}</p>
            <p className="session-detail">
              {total > 0 ? `plan: ${done}/${total} steps completed` : "no plan yet"}
              {goal.replans > 0 ? ` · ${goal.replans} replan${goal.replans === 1 ? "" : "s"}` : ""}
              {goal.turnsUsed > 0 ? ` · ${goal.turnsUsed} turns used` : ""}
              {goal.systemId ? ` · system ${goal.systemId} rev ${goal.systemRevision ?? "?"}` : ""}
            </p>
            {goal.pendingApproval ? (
              <div className="settings-card">
                <span className="settings-chip chip-warn">
                  approval needed: {goal.pendingApproval.toolName} ({goal.pendingApproval.risk})
                </span>
                <div className="header-actions">
                  <button
                    className="secondary-button"
                    disabled={busy}
                    onClick={() =>
                      void act(
                        () =>
                          api.goalAction(
                            goal.goalId,
                            "approve",
                            goal.pendingApproval!.approvalId,
                          ),
                        "approved — the exact call resumes",
                      )
                    }
                  >
                    Approve
                  </button>
                  <button
                    className="secondary-button"
                    disabled={busy}
                    onClick={() =>
                      void act(
                        () =>
                          api.goalAction(
                            goal.goalId,
                            "reject",
                            goal.pendingApproval!.approvalId,
                          ),
                        "rejected — recorded as evidence",
                      )
                    }
                  >
                    Reject
                  </button>
                </div>
              </div>
            ) : null}
            <div className="header-actions">
              {isGoalLive(goal) ? (
                <button
                  className="secondary-button"
                  disabled={busy}
                  onClick={() => void act(() => api.goalAction(goal.goalId, "pause"), "paused at checkpoint")}
                >
                  Pause
                </button>
              ) : null}
              {!isGoalLive(goal) && !["completed", "cancelled", "failed"].includes(goal.status) ? (
                <button
                  className="secondary-button"
                  disabled={busy}
                  onClick={() => void act(() => api.goalAction(goal.goalId, "resume"), "resumed from checkpoint")}
                >
                  Resume
                </button>
              ) : null}
              {!["completed", "cancelled", "failed"].includes(goal.status) ? (
                <button
                  className="secondary-button"
                  disabled={busy}
                  onClick={() => void act(() => api.goalAction(goal.goalId, "cancel"), "cancelled")}
                >
                  Cancel
                </button>
              ) : null}
            </div>
          </div>
        );
      })}
    </div>
  );
}
