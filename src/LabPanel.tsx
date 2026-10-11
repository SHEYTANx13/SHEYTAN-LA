import { type FormEvent, useEffect, useMemo, useState } from "react";

import { type LabTaskSessionSnapshot } from "./api";
import { useRuntimeStore } from "./store";

function formatDate(value?: string): string {
  if (!value) {
    return "—";
  }

  const date = new Date(value);

  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return date.toLocaleString();
}

function formatDuration(value?: number): string {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "—";
  }

  if (value < 1000) {
    return `${Math.round(value)} ms`;
  }

  return `${(value / 1000).toFixed(2)} s`;
}

function statusClass(status: LabTaskSessionSnapshot["task"]["status"]): string {
  return `lab-status lab-status-${status}`;
}

function verificationLabel(task: LabTaskSessionSnapshot["task"]): string {
  if (task.verificationPassed) {
    return "VERIFIED";
  }

  if (task.lastVerification) {
    return "NOT VERIFIED";
  }

  return "UNVERIFIED";
}

function verificationClass(task: LabTaskSessionSnapshot["task"]): string {
  if (task.verificationPassed) {
    return "lab-verification verified";
  }

  if (task.lastVerification) {
    return "lab-verification failed";
  }

  return "lab-verification pending";
}

function actionPayload(
  action: string,
  taskId?: string,
): Record<string, unknown> {
  return {
    action,
    ...(taskId
      ? {
          taskId,
        }
      : {}),
  };
}

function TaskListItem({
  item,
  active,
  onSelect,
}: {
  item: LabTaskSessionSnapshot;
  active: boolean;
  onSelect: () => void;
}) {
  const task = item.task;

  return (
    <button
      type="button"
      className={`lab-task-item ${active ? "active" : ""}`}
      onClick={onSelect}
    >
      <div className="lab-task-item-top">
        <strong>{task.title || "Untitled task"}</strong>

        <span className={statusClass(task.status)}>{task.status}</span>
      </div>

      <div className="lab-task-item-bottom">
        <span>{task.id.slice(0, 12)}</span>

        <span className={verificationClass(task)}>
          {verificationLabel(task)}
        </span>
      </div>
    </button>
  );
}

function TaskOverview({ task }: { task: LabTaskSessionSnapshot }) {
  const data = task.task;

  return (
    <div className="lab-overview">
      <div className="lab-section-heading">
        <div>
          <span className="eyebrow">TASK</span>

          <strong>{data.title || "Untitled task"}</strong>
        </div>

        <span className={statusClass(data.status)}>{data.status}</span>
      </div>

      {data.description ? (
        <p className="lab-description">{data.description}</p>
      ) : (
        <p className="lab-description muted">No task description.</p>
      )}

      <div className="lab-metrics">
        <div className="lab-metric">
          <span>Verification</span>
          <strong>{verificationLabel(data)}</strong>
        </div>

        <div className="lab-metric">
          <span>Commands</span>
          <strong>{data.commands?.length ?? 0}</strong>
        </div>

        <div className="lab-metric">
          <span>Results</span>
          <strong>{data.results?.length ?? 0}</strong>
        </div>

        <div className="lab-metric">
          <span>Created</span>
          <strong>{formatDate(data.createdAt)}</strong>
        </div>
      </div>

      {data.workspace ? (
        <div className="lab-workspace">
          <div className="lab-section-heading compact">
            <div>
              <span className="eyebrow">WORKSPACE</span>
            </div>
          </div>

          <div className="lab-detail-row">
            <span>ID</span>
            <strong>{data.workspace.id}</strong>
          </div>

          <div className="lab-detail-row">
            <span>Source</span>
            <strong>{data.workspace.source}</strong>
          </div>

          <div className="lab-detail-row">
            <span>Path</span>
            <strong>{data.workspace.path}</strong>
          </div>

          <div className="lab-detail-row">
            <span>Created</span>
            <strong>{formatDate(data.workspace.createdAt)}</strong>
          </div>
        </div>
      ) : null}

      {data.error ? (
        <div className="lab-error">
          <span className="eyebrow">ERROR</span>

          <p>{data.error}</p>
        </div>
      ) : null}
    </div>
  );
}

function CommandHistory({ task }: { task: LabTaskSessionSnapshot }) {
  const commands = task.task.commands ?? [];
  const results = task.task.results ?? [];

  return (
    <section className="lab-section">
      <div className="lab-section-heading">
        <div>
          <span className="eyebrow">EXECUTION</span>

          <strong>Command history</strong>
        </div>

        <span className="lab-count">{results.length}</span>
      </div>

      {results.length === 0 ? (
        <div className="lab-empty">No commands have been executed yet.</div>
      ) : (
        <div className="lab-command-list">
          {results.map((result, index) => {
            const command = commands[index]?.command ?? result.command;

            return (
              <article className="lab-command" key={`${command}-${index}`}>
                <div className="lab-command-header">
                  <code>{command}</code>

                  <span
                    className={
                      result.success
                        ? "lab-result-success"
                        : "lab-result-failure"
                    }
                  >
                    {result.success ? "PASS" : `EXIT ${result.exitCode}`}
                  </span>
                </div>

                <div className="lab-command-meta">
                  <span>{formatDuration(result.duration)}</span>

                  <span>
                    {result.timedOut
                      ? "Timed out"
                      : result.canceled
                        ? "Canceled"
                        : result.outputLimit
                          ? "Output limited"
                          : "Completed"}
                  </span>
                </div>

                {result.output ? (
                  <pre className="lab-output">{result.output}</pre>
                ) : null}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}

function VerificationPanel({ task }: { task: LabTaskSessionSnapshot }) {
  const verification = task.task.lastVerification;

  return (
    <section className="lab-section">
      <div className="lab-section-heading">
        <div>
          <span className="eyebrow">OBJECTIVE</span>

          <strong>Verification</strong>
        </div>

        {verification ? (
          <span
            className={
              verification.passed
                ? "lab-verification verified"
                : "lab-verification failed"
            }
          >
            {verification.passed ? "PASSED" : "FAILED"}
          </span>
        ) : (
          <span className="lab-verification pending">NOT RUN</span>
        )}
      </div>

      {verification ? (
        <>
          <div className="lab-verification-grid">
            <div className="lab-metric">
              <span>Required</span>

              <strong>
                {verification.requiredPassed}/{verification.requiredTotal}
              </strong>
            </div>

            <div className="lab-metric">
              <span>Required failed</span>

              <strong>{verification.requiredFailed}</strong>
            </div>

            <div className="lab-metric">
              <span>Optional</span>

              <strong>
                {verification.optionalPassed}/{verification.optionalTotal}
              </strong>
            </div>

            <div className="lab-metric">
              <span>Duration</span>

              <strong>{formatDuration(verification.duration)}</strong>
            </div>
          </div>

          <div className="lab-verification-list">
            {verification.results.map((result, index) => (
              <article
                className="lab-verification-item"
                key={`${result.name}-${index}`}
              >
                <div className="lab-command-header">
                  <strong>{result.name || `Check ${index + 1}`}</strong>

                  <span
                    className={
                      result.status === "passed"
                        ? "lab-result-success"
                        : "lab-result-failure"
                    }
                  >
                    {result.status}
                  </span>
                </div>

                <code>{result.result.command}</code>

                {result.error ? (
                  <p className="lab-inline-error">{result.error}</p>
                ) : null}
              </article>
            ))}
          </div>

          {verification.error ? (
            <div className="lab-error">
              <span className="eyebrow">VERIFICATION ERROR</span>

              <p>{verification.error}</p>
            </div>
          ) : null}
        </>
      ) : (
        <div className="lab-empty">
          Verification has not been run for this task.
        </div>
      )}
    </section>
  );
}

function LabActions({
  task,
  loading,
  onAction,
}: {
  task: LabTaskSessionSnapshot;
  loading: boolean;
  onAction: (payload: Record<string, unknown>) => Promise<void>;
}) {
  const status = task.task.status;
  const verified = task.task.verificationPassed;

  const canVerify = status === "running";

  const canPromote = status === "running" && verified;

  const canFinish = status === "running" && verified;

  const canCancel = status === "running";

  const canBlock = status === "running";

  const canClose = status !== "pending" && status !== "running";

  return (
    <section className="lab-section">
      <div className="lab-section-heading">
        <div>
          <span className="eyebrow">LIFECYCLE</span>

          <strong>Task controls</strong>
        </div>
      </div>

      <div className="lab-actions">
        <button
          type="button"
          className="secondary-button"
          disabled={loading || !canVerify}
          onClick={() => void onAction(actionPayload("verify", task.id))}
        >
          Verify
        </button>

        <button
          type="button"
          className="secondary-button"
          disabled={loading || !canPromote}
          onClick={() => void onAction(actionPayload("promote", task.id))}
        >
          Promote
        </button>

        <button
          type="button"
          className="secondary-button"
          disabled={loading || !canFinish}
          onClick={() => void onAction(actionPayload("finish", task.id))}
        >
          Finish
        </button>

        <button
          type="button"
          className="secondary-button"
          disabled={loading || !canCancel}
          onClick={() => void onAction(actionPayload("cancel", task.id))}
        >
          Cancel
        </button>

        <button
          type="button"
          className="secondary-button"
          disabled={loading || !canBlock}
          onClick={() => void onAction(actionPayload("block", task.id))}
        >
          Block
        </button>

        <button
          type="button"
          className="secondary-button"
          disabled={loading || !canClose}
          onClick={() => void onAction(actionPayload("close", task.id))}
        >
          Close
        </button>
      </div>
    </section>
  );
}

function StartTaskForm({
  loading,
  onStart,
}: {
  loading: boolean;
  onStart: (payload: Record<string, unknown>) => Promise<void>;
}) {
  const [title, setTitle] = useState("");

  const [description, setDescription] = useState("");

  const [source, setSource] = useState("");

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const trimmedSource = source.trim();

    if (!trimmedSource) {
      return;
    }

    await onStart({
      action: "start_task",
      title: title.trim(),
      description: description.trim(),
      source: trimmedSource,
    });

    setTitle("");
    setDescription("");
    setSource("");
  }

  return (
    <form
      className="lab-start-form"
      onSubmit={(event) => void handleSubmit(event)}
    >
      <div className="lab-section-heading">
        <div>
          <span className="eyebrow">NEW</span>

          <strong>Start Coding Lab task</strong>
        </div>
      </div>

      <label>
        <span>Title</span>

        <input
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          placeholder="Fix compiler failure"
          disabled={loading}
        />
      </label>

      <label>
        <span>Description</span>

        <textarea
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          placeholder="Describe the coding objective..."
          rows={3}
          disabled={loading}
        />
      </label>

      <label>
        <span>Local source path</span>

        <input
          value={source}
          onChange={(event) => setSource(event.target.value)}
          placeholder="C:\\Projects\\my-project"
          disabled={loading}
          required
        />
      </label>

      <button
        type="submit"
        className="send-button"
        disabled={loading || !source.trim()}
      >
        Start task →
      </button>
    </form>
  );
}

export default function LabPanel() {
  // v1.2.4: explicit selectors — subscribing to the whole store made the
  // Lab panel re-render on every streaming flush and every 2.5 s engine
  // poll even though it reads none of that state.
  const lab = useRuntimeStore((s) => s.lab);
  const labLoading = useRuntimeStore((s) => s.labLoading);
  const labError = useRuntimeStore((s) => s.labError);
  const activeLabTaskId = useRuntimeStore((s) => s.activeLabTaskId);
  const activeLabTask = useRuntimeStore((s) => s.activeLabTask);
  const refreshLab = useRuntimeStore((s) => s.refreshLab);
  const loadLabTask = useRuntimeStore((s) => s.loadLabTask);
  const runLabAction = useRuntimeStore((s) => s.runLabAction);

  useEffect(() => {
    void refreshLab();
  }, [refreshLab]);

  const selectedTask = useMemo(() => {
    if (activeLabTask) {
      return activeLabTask;
    }

    if (!activeLabTaskId) {
      return null;
    }

    return lab?.tasks.find((item) => item.id === activeLabTaskId) ?? null;
  }, [activeLabTask, activeLabTaskId, lab?.tasks]);

  async function handleAction(payload: Record<string, unknown>) {
    await runLabAction(payload);
  }

  async function handleStart(payload: Record<string, unknown>) {
    const result = await runLabAction(payload);

    if (result && typeof result === "object") {
      const value = result as Record<string, unknown>;

      const task = value.task;

      if (task && typeof task === "object") {
        const taskValue = task as Record<string, unknown>;

        if (typeof taskValue.id === "string") {
          await loadLabTask(taskValue.id);
        }
      }
    }
  }

  return (
    <section className="lab-panel">
      <div className="lab-panel-header">
        <div>
          <span className="eyebrow">CODING LAB</span>

          <h2>Autonomous engineering</h2>
        </div>

        <button
          type="button"
          className="text-button"
          onClick={() => void refreshLab()}
          disabled={labLoading}
        >
          {labLoading ? "Refreshing…" : "Refresh"}
        </button>
      </div>

      {labError ? (
        <div className="error-banner" role="alert">
          {labError}
        </div>
      ) : null}

      <div className="lab-layout">
        <aside className="lab-task-sidebar">
          <div className="lab-section-heading">
            <div>
              <span className="eyebrow">TASKS</span>

              <strong>Active Lab sessions</strong>
            </div>

            <span className="lab-count">{lab?.tasks.length ?? 0}</span>
          </div>

          <div className="lab-task-list">
            {lab?.tasks.length ? (
              lab.tasks.map((item) => (
                <TaskListItem
                  key={item.id}
                  item={item}
                  active={item.id === activeLabTaskId}
                  onSelect={() => void loadLabTask(item.id)}
                />
              ))
            ) : (
              <div className="lab-empty">No active Coding Lab tasks.</div>
            )}
          </div>

          <StartTaskForm loading={labLoading} onStart={handleStart} />
        </aside>

        <div className="lab-detail">
          {selectedTask ? (
            <>
              <TaskOverview task={selectedTask} />

              <LabActions
                task={selectedTask}
                loading={labLoading}
                onAction={handleAction}
              />

              <VerificationPanel task={selectedTask} />

              <CommandHistory task={selectedTask} />
            </>
          ) : (
            <div className="lab-detail-empty">
              <div className="activity-empty-mark">✦</div>

              <strong>Select a Lab task</strong>

              <span>
                Task state, verification evidence, command history, and
                lifecycle controls will appear here.
              </span>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
