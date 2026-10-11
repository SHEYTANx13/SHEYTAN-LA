import {
  type FormEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import {
  api,
  type AutomationRunReport,
  type AutomationScheduleKind,
  type AutomationTask,
  type AutomationTaskInput,
  type AutomationTaskTool,
  type AutomationTrigger,
} from "./api";
import {
  formatNextRun,
  formatRelativeTime,
  formatRunDuration,
  formatSchedule,
  formatWhen,
  runState,
  RUN_STATE_CHIP,
  RUN_STATE_LABEL,
  toRFC3339Local,
  triggerLabel,
  fromRFC3339ToLocalInput,
} from "./automation-view";
import { ArtifactSection } from "./AutomationArtifactViewer";

// AutomationPanel — v1.7.0: the Automation & Tasks surface.
//
//   left  → the task list (live: polled every 15 s while mounted) with
//           per-task controls and the inline create form
//   right → the selected task: overview, the chronological RUN TIMELINE
//           (newest first), the task's scoped tools with the approval
//           gate, and the task's artifacts (AutomationArtifactViewer)
//
// Every value comes from /api/automation/* through the typed api client;
// nothing is fabricated client-side and unknown values render as "—".
// The panel is self-contained (local state, no store slice) exactly like
// the Workspace panel.

const POLL_INTERVAL_MS = 15_000;

const TRIGGER_OPTIONS: readonly AutomationTrigger[] = [
  "manual",
  "timer",
  "scheduled_maintenance",
  "startup",
  "file_change",
  "git_change",
  "test_failure",
  "ci_failure",
  "build_failure",
];

// SCHEDULED_TRIGGERS accept the extended v1.7.0 schedule model
// (interval / once / daily / weekly); every other trigger fires on its
// event and needs no schedule controls.
const SCHEDULED_TRIGGERS: readonly string[] = ["timer", "scheduled_maintenance"];

const SCHEDULE_KIND_OPTIONS: readonly {
  value: AutomationScheduleKind;
  label: string;
}[] = [
  { value: "interval", label: "Every interval" },
  { value: "once", label: "Once at a time" },
  { value: "daily", label: "Daily at a time" },
  { value: "weekly", label: "Weekly on a weekday" },
];

const WEEKDAY_OPTIONS: readonly { value: number; label: string }[] = [
  { value: 0, label: "Sunday" },
  { value: 1, label: "Monday" },
  { value: 2, label: "Tuesday" },
  { value: 3, label: "Wednesday" },
  { value: 4, label: "Thursday" },
  { value: 5, label: "Friday" },
  { value: 6, label: "Saturday" },
];

function TaskStatusBadges({ task }: { task: AutomationTask }) {
  return (
    <span className="aut-badges">
      {task.running ? (
        <span className="aut-status aut-status-running">RUNNING</span>
      ) : null}

      {task.paused ? <span className="aut-status aut-status-paused">PAUSED</span> : null}

      {!task.enabled ? (
        <span className="aut-status aut-status-disabled">DISABLED</span>
      ) : null}
    </span>
  );
}

function LastRunLine({ task }: { task: AutomationTask }) {
  if (!task.lastRun) {
    return <span className="aut-lastrun muted">No runs yet</span>;
  }

  const state = runState(task.lastRun);

  return (
    <span className="aut-lastrun">
      <span className={`settings-chip ${RUN_STATE_CHIP[state]}`}>
        {RUN_STATE_LABEL[state]}
      </span>

      <span>
        {formatRunDuration(task.lastRun.durationMs)} ·{" "}
        {formatRelativeTime(task.lastRun.startedAt)}
      </span>
    </span>
  );
}

function LinkedChips({ task }: { task: AutomationTask }) {
  const skills = task.linkedSkills ?? [];
  const tools = task.taskTools ?? [];
  const types = task.taskTypes ?? [];

  if (skills.length === 0 && tools.length === 0 && types.length === 0) {
    return null;
  }

  return (
    <span className="aut-linked">
      {skills.map((skill) => (
        <span className="aut-chip" key={`skill-${skill}`} title="Linked skill">
          ✦ {skill}
        </span>
      ))}

      {tools.map((tool) => (
        <span className="aut-chip" key={`tool-${tool}`} title="Task tool">
          ⚒ {tool}
        </span>
      ))}

      {types.map((type) => (
        <span className="aut-chip" key={`type-${type}`} title="Task type">
          {type}
        </span>
      ))}
    </span>
  );
}

function TaskListItem({
  task,
  active,
  busy,
  onSelect,
  onRun,
  onPause,
  onResume,
  onCancel,
  onDelete,
}: {
  task: AutomationTask;
  active: boolean;
  busy: boolean;
  onSelect: () => void;
  onRun: () => void;
  onPause: () => void;
  onResume: () => void;
  onCancel: () => void;
  onDelete: () => void;
}) {
  return (
    <article className={`aut-task-item ${active ? "active" : ""}`}>
      <button type="button" className="aut-task-select" onClick={onSelect}>
        <span className="aut-task-item-top">
          <strong>{task.name || "Untitled task"}</strong>

          <TaskStatusBadges task={task} />
        </span>

        <span className="aut-task-item-schedule">
          {triggerLabel(task.trigger)} · {formatSchedule(task)}
        </span>

        <span className="aut-task-item-next">
          Next run · {formatNextRun(task.nextDue)}
        </span>

        <LastRunLine task={task} />

        <LinkedChips task={task} />

        <span className="aut-task-item-id" title={task.id}>
          {task.id.slice(0, 22)}
        </span>
      </button>

      <span className="aut-task-actions">
        <button
          type="button"
          className="aut-mini-button"
          disabled={busy || task.running || task.paused}
          onClick={onRun}
          title="Run this task now"
        >
          Run now
        </button>

        {task.paused ? (
          <button
            type="button"
            className="aut-mini-button"
            disabled={busy}
            onClick={onResume}
            title="Resume scheduled firing"
          >
            Resume
          </button>
        ) : (
          <button
            type="button"
            className="aut-mini-button"
            disabled={busy}
            onClick={onPause}
            title="Hold scheduled firing"
          >
            Pause
          </button>
        )}

        {task.running ? (
          <button
            type="button"
            className="aut-mini-button"
            disabled={busy}
            onClick={onCancel}
            title="Cancel the running execution"
          >
            Cancel
          </button>
        ) : null}

        <button
          type="button"
          className="aut-mini-button danger"
          disabled={busy}
          onClick={onDelete}
          title="Delete this task"
        >
          Delete
        </button>
      </span>
    </article>
  );
}

function NewTaskForm({ onCreated }: { onCreated: (task: AutomationTask) => void }) {
  const [name, setName] = useState("");
  const [trigger, setTrigger] = useState<AutomationTrigger>("manual");
  const [prompt, setPrompt] = useState("");
  const [scheduleKind, setScheduleKind] =
    useState<AutomationScheduleKind>("interval");
  const [intervalMinutes, setIntervalMinutes] = useState("15");
  const [at, setAt] = useState(() =>
    fromRFC3339ToLocalInput(
      new Date(Date.now() + 60 * 60_000).toISOString(),
    ),
  );
  const [timeOfDay, setTimeOfDay] = useState("09:00");
  const [weekday, setWeekday] = useState("1");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const scheduledTrigger = SCHEDULED_TRIGGERS.includes(trigger);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);

    const trimmedName = name.trim();
    const trimmedPrompt = prompt.trim();

    if (!trimmedName || !trimmedPrompt) {
      return;
    }

    const input: AutomationTaskInput = {
      name: trimmedName,
      trigger,
      prompt: trimmedPrompt,
    };

    if (scheduledTrigger) {
      if (scheduleKind === "interval") {
        const minutes = Number(intervalMinutes);

        if (!Number.isFinite(minutes) || minutes < 5) {
          setFormError("Interval tasks need at least 5 minutes between runs.");
          return;
        }

        input.intervalSeconds = Math.round(minutes * 60);
      } else if (scheduleKind === "once") {
        const atRFC3339 = toRFC3339Local(at);

        if (!atRFC3339) {
          setFormError("Pick the date and time the task should run once.");
          return;
        }

        input.scheduleKind = "once";
        input.at = atRFC3339;
      } else if (scheduleKind === "daily") {
        input.scheduleKind = "daily";
        input.timeOfDay = timeOfDay;
      } else {
        input.scheduleKind = "weekly";
        input.timeOfDay = timeOfDay;
        input.weekday = Number(weekday);
      }
    }

    setSubmitting(true);

    try {
      const task = await api.createAutomationTask(input);

      setName("");
      setPrompt("");
      onCreated(task);
    } catch (err) {
      setFormError(
        err instanceof Error ? err.message : "The task could not be created.",
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="aut-start-form" onSubmit={(event) => void handleSubmit(event)}>
      <div className="aut-section-heading">
        <div>
          <span className="eyebrow">NEW TASK</span>

          <strong>Create an automation task</strong>
        </div>
      </div>

      <label>
        <span>Name</span>

        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Nightly dependency report"
          disabled={submitting}
          required
        />
      </label>

      <label>
        <span>Trigger</span>

        <select
          value={trigger}
          onChange={(event) => setTrigger(event.target.value as AutomationTrigger)}
          disabled={submitting}
        >
          {TRIGGER_OPTIONS.map((option) => (
            <option key={option} value={option}>
              {triggerLabel(option)}
            </option>
          ))}
        </select>
      </label>

      {scheduledTrigger ? (
        <>
          <label>
            <span>Schedule</span>

            <select
              value={scheduleKind}
              onChange={(event) =>
                setScheduleKind(event.target.value as AutomationScheduleKind)
              }
              disabled={submitting}
            >
              {SCHEDULE_KIND_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>

          {scheduleKind === "interval" ? (
            <label>
              <span>Interval (minutes, min 5)</span>

              <input
                type="number"
                min={5}
                step={1}
                value={intervalMinutes}
                onChange={(event) => setIntervalMinutes(event.target.value)}
                disabled={submitting}
              />
            </label>
          ) : null}

          {scheduleKind === "once" ? (
            <label>
              <span>Run at (local time)</span>

              <input
                type="datetime-local"
                value={at}
                onChange={(event) => setAt(event.target.value)}
                disabled={submitting}
              />
            </label>
          ) : null}

          {scheduleKind === "daily" || scheduleKind === "weekly" ? (
            <label>
              <span>Time of day</span>

              <input
                type="time"
                value={timeOfDay}
                onChange={(event) => setTimeOfDay(event.target.value)}
                disabled={submitting}
              />
            </label>
          ) : null}

          {scheduleKind === "weekly" ? (
            <label>
              <span>Weekday</span>

              <select
                value={weekday}
                onChange={(event) => setWeekday(event.target.value)}
                disabled={submitting}
              >
                {WEEKDAY_OPTIONS.map((option) => (
                  <option key={option.value} value={String(option.value)}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
        </>
      ) : null}

      <label>
        <span>Prompt</span>

        <textarea
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          placeholder="Describe what the task should do on every run…"
          rows={4}
          disabled={submitting}
          required
        />
      </label>

      {formError ? <div className="aut-form-error">{formError}</div> : null}

      <button
        type="submit"
        className="send-button"
        disabled={submitting || !name.trim() || !prompt.trim()}
      >
        Create task →
      </button>
    </form>
  );
}

function RunTimeline({ runs }: { runs: AutomationRunReport[] | null }) {
  return (
    <section className="aut-section">
      <div className="aut-section-heading">
        <div>
          <span className="eyebrow">RUN HISTORY</span>

          <strong>Run timeline</strong>
        </div>

        <span className="aut-count">{runs?.length ?? 0}</span>
      </div>

      {runs === null ? (
        <div className="aut-empty">Loading run history…</div>
      ) : runs.length === 0 ? (
        <div className="aut-empty">No runs yet — Run now to see the first entry.</div>
      ) : (
        <ol className="aut-timeline">
          {runs.map((report, index) => {
            const state = runState(report);

            return (
              <li
                key={`${report.startedAt}-${index}`}
                className={`aut-timeline-entry state-${state}`}
              >
                <span className="aut-timeline-dot" aria-hidden="true" />

                <div className="aut-timeline-body">
                  <div className="aut-timeline-head">
                    <span
                      className="aut-timeline-when"
                      title={report.startedAt}
                    >
                      {formatWhen(report.startedAt)} ·{" "}
                      {formatRelativeTime(report.startedAt)}
                    </span>

                    <span className={`settings-chip ${RUN_STATE_CHIP[state]}`}>
                      {RUN_STATE_LABEL[state]}
                    </span>
                  </div>

                  <div className="aut-timeline-meta">
                    <span>{triggerLabel(report.trigger)}</span>

                    <span>{formatRunDuration(report.durationMs)}</span>
                  </div>

                  {report.err ? (
                    <p className="aut-inline-error">{report.err}</p>
                  ) : null}

                  {report.output ? (
                    <details className="aut-timeline-output">
                      <summary>Output</summary>

                      <pre>{report.output}</pre>
                    </details>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
}

function TaskTools({
  tools,
  busy,
  onApprove,
}: {
  tools: AutomationTaskTool[] | null;
  busy: boolean;
  onApprove: (tool: AutomationTaskTool) => void;
}) {
  return (
    <section className="aut-section">
      <div className="aut-section-heading">
        <div>
          <span className="eyebrow">TASK TOOLS</span>

          <strong>Scoped tools</strong>
        </div>

        <span className="aut-count">{tools?.length ?? 0}</span>
      </div>

      {tools === null ? (
        <div className="aut-empty">Loading task tools…</div>
      ) : tools.length === 0 ? (
        <div className="aut-empty">
          No task-scoped tools — tools a run creates stay scoped to this task
          and disappear with it unless promoted.
        </div>
      ) : (
        <div className="aut-tools">
          {tools.map((tool) => (
            <article className="aut-tool" key={tool.id}>
              <div className="aut-tool-head">
                <strong>{tool.name}</strong>

                <span className="aut-badges">
                  {tool.permission ? (
                    <span className="settings-chip chip-neutral">
                      {tool.permission}
                    </span>
                  ) : null}

                  <span
                    className={`settings-chip ${
                      tool.approved ? "chip-good" : "chip-warn"
                    }`}
                  >
                    {tool.approved ? "approved" : "unapproved"}
                  </span>

                  <span
                    className={`settings-chip ${
                      tool.enabled ? "chip-good" : "chip-neutral"
                    }`}
                  >
                    {tool.enabled ? "enabled" : "disabled"}
                  </span>
                </span>
              </div>

              {tool.shortDescription ? (
                <p className="aut-tool-desc">{tool.shortDescription}</p>
              ) : null}

              {!tool.approved ? (
                <div className="aut-tool-actions">
                  <button
                    type="button"
                    className="aut-mini-button"
                    disabled={busy}
                    onClick={() => onApprove(tool)}
                    title="Approve this tool (enables execution for this task)"
                  >
                    Approve
                  </button>

                  <span className="aut-tool-note">
                    Unapproved tools are never executed.
                  </span>
                </div>
              ) : null}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function TaskOverview({ task }: { task: AutomationTask }) {
  return (
    <div className="aut-overview">
      <div className="aut-section-heading">
        <div>
          <span className="eyebrow">TASK</span>

          <strong>{task.name || "Untitled task"}</strong>
        </div>

        <TaskStatusBadges task={task} />
      </div>

      <p className="aut-prompt">{task.prompt}</p>

      <div className="aut-facts">
        <div className="aut-fact">
          <span>Trigger</span>
          <strong>{triggerLabel(task.trigger)}</strong>
        </div>

        <div className="aut-fact">
          <span>Schedule</span>
          <strong>{formatSchedule(task)}</strong>
        </div>

        <div className="aut-fact">
          <span>Next run</span>
          <strong>{formatNextRun(task.nextDue)}</strong>
        </div>

        <div className="aut-fact">
          <span>Max runtime</span>
          <strong>
            {task.maxRuntimeSeconds
              ? formatRunDuration(task.maxRuntimeSeconds * 1000)
              : "—"}
          </strong>
        </div>

        <div className="aut-fact">
          <span>Created</span>
          <strong title={task.created}>{formatWhen(task.created)}</strong>
        </div>

        <div className="aut-fact">
          <span>ID</span>
          <strong className="aut-hash" title={task.id}>
            {task.id}
          </strong>
        </div>
      </div>

      <LinkedChips task={task} />
    </div>
  );
}

export default function AutomationPanel() {
  const [tasks, setTasks] = useState<AutomationTask[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [artifactsRevision, setArtifactsRevision] = useState(0);

  // The selected task's run history + scoped tools (artifacts are owned
  // by the viewer section, keyed by taskId + revision).
  const [runs, setRuns] = useState<AutomationRunReport[] | null>(null);
  const [tools, setTools] = useState<AutomationTaskTool[] | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);

  // v1.2.2 pattern: navigating away mid-request must never mutate
  // unmounted state.
  const aliveRef = useRef(true);

  // "Latest request wins" sequencing for the detail loads — a slow runs
  // answer for task A can never overwrite a newer load for task B.
  const detailRequestRef = useRef(0);

  useEffect(() => {
    aliveRef.current = true;

    return () => {
      aliveRef.current = false;
    };
  }, []);

  const refreshTasks = useCallback(async () => {
    setRefreshing(true);

    try {
      const list = await api.automationTasks();

      if (aliveRef.current) {
        setTasks(list);
        setLoadError(null);
      }
    } catch (err) {
      if (aliveRef.current) {
        setLoadError(
          err instanceof Error
            ? err.message
            : "Automation tasks could not be loaded.",
        );
      }
    } finally {
      if (aliveRef.current) {
        setRefreshing(false);
      }
    }
  }, []);

  const loadDetail = useCallback(async (taskId: string, reset: boolean) => {
    const seq = detailRequestRef.current + 1;
    detailRequestRef.current = seq;

    if (reset) {
      setRuns(null);
      setTools(null);
    }

    setDetailError(null);

    try {
      const [runList, toolList] = await Promise.all([
        api.automationTaskRuns(taskId),
        api.automationTaskTools(taskId),
      ]);

      if (!aliveRef.current || detailRequestRef.current !== seq) {
        return;
      }

      setRuns(runList);
      setTools(toolList);
    } catch (err) {
      if (!aliveRef.current || detailRequestRef.current !== seq) {
        return;
      }

      setDetailError(
        err instanceof Error ? err.message : "The task history could not be loaded.",
      );
    }
  }, []);

  // Initial load + the 15 s live poll (cleared on unmount) so the run
  // badges, next-due times and the timeline stay honest while mounted.
  useEffect(() => {
    void refreshTasks();

    const timer = window.setInterval(() => {
      void refreshTasks();

      if (selectedId) {
        void loadDetail(selectedId, false);
      }
    }, POLL_INTERVAL_MS);

    return () => {
      window.clearInterval(timer);
    };
  }, [refreshTasks, loadDetail, selectedId]);

  // Selection change → the detail reloads from scratch.
  useEffect(() => {
    if (selectedId) {
      void loadDetail(selectedId, true);
    }
  }, [selectedId, loadDetail]);

  // A task deleted outside this panel (or teardown by the backend) must
  // not leave a selection pointing at nothing — fall back honestly.
  useEffect(() => {
    if (tasks && selectedId && !tasks.some((task) => task.id === selectedId)) {
      setSelectedId(null);
    }
  }, [tasks, selectedId]);

  const withAction = useCallback(
    async (
      actionId: string,
      action: () => Promise<unknown>,
      options?: { bumpArtifacts?: boolean; forget?: boolean },
    ) => {
      setBusyId(actionId);
      setActionError(null);

      try {
        await action();
      } catch (err) {
        if (aliveRef.current) {
          setActionError(
            err instanceof Error ? err.message : "The task action failed.",
          );
        }
      } finally {
        if (aliveRef.current) {
          setBusyId(null);
        }
      }

      if (!aliveRef.current) {
        return;
      }

      void refreshTasks();

      if (options?.forget) {
        setSelectedId((current) => (current === actionId ? null : current));
        return;
      }

      void loadDetail(actionId, false);

      if (options?.bumpArtifacts) {
        setArtifactsRevision((revision) => revision + 1);
      }
    },
    [loadDetail, refreshTasks],
  );

  const selectedTask = selectedId
    ? (tasks?.find((task) => task.id === selectedId) ?? null)
    : null;

  function handleCreated(task: AutomationTask) {
    setFormOpen(false);
    setSelectedId(task.id);
    void refreshTasks();
  }

  function removeTask(task: AutomationTask) {
    if (
      !window.confirm(
        `Delete task "${task.name}"? Its run history, scoped tools and artifacts are removed too.`,
      )
    ) {
      return;
    }

    void withAction(task.id, () => api.deleteAutomationTask(task.id), {
      forget: true,
    });
  }

  return (
    <section className="aut-panel">
      <div className="aut-panel-header">
        <div>
          <span className="eyebrow">AUTOMATION</span>

          <h2>Automation &amp; Tasks</h2>
        </div>

        <div className="aut-header-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={() => setFormOpen((open) => !open)}
          >
            {formOpen ? "Close form" : "New task"}
          </button>

          <button
            type="button"
            className="text-button"
            onClick={() => void refreshTasks()}
            disabled={refreshing}
          >
            {refreshing ? "Refreshing…" : "Refresh"}
          </button>
        </div>
      </div>

      {loadError ? (
        <div className="error-banner" role="alert">
          {loadError}
        </div>
      ) : null}

      {actionError ? (
        <div className="error-banner" role="alert">
          {actionError}
        </div>
      ) : null}

      <div className="aut-layout">
        <aside className="aut-task-sidebar">
          <div className="aut-sidebar-heading">
            <div>
              <span className="eyebrow">TASKS</span>

              <strong>Scheduled &amp; event tasks</strong>
            </div>

            <span className="aut-count">{tasks?.length ?? 0}</span>
          </div>

          <div className="aut-task-list">
            {tasks === null ? (
              <div className="aut-empty">Loading tasks…</div>
            ) : tasks.length === 0 ? (
              <div className="aut-empty">
                No automation tasks yet. Tasks are created here and can run
                manually, on a schedule, or in reaction to events — test
                failures, git changes, build results.
              </div>
            ) : (
              tasks.map((task) => (
                <TaskListItem
                  key={task.id}
                  task={task}
                  active={task.id === selectedId}
                  busy={busyId === task.id}
                  onSelect={() => setSelectedId(task.id)}
                  onRun={() =>
                    void withAction(task.id, () => api.runAutomationTask(task.id), {
                      bumpArtifacts: true,
                    })
                  }
                  onPause={() =>
                    void withAction(task.id, () => api.pauseAutomationTask(task.id))
                  }
                  onResume={() =>
                    void withAction(task.id, () => api.resumeAutomationTask(task.id))
                  }
                  onCancel={() =>
                    void withAction(task.id, () => api.cancelAutomationTask(task.id))
                  }
                  onDelete={() => removeTask(task)}
                />
              ))
            )}
          </div>

          {formOpen ? <NewTaskForm onCreated={handleCreated} /> : null}
        </aside>

        <div className="aut-detail">
          {selectedTask ? (
            <>
              <TaskOverview task={selectedTask} />

              <section className="aut-section">
                <div className="aut-section-heading">
                  <div>
                    <span className="eyebrow">LIFECYCLE</span>

                    <strong>Task controls</strong>
                  </div>
                </div>

                <div className="aut-actions">
                  <button
                    type="button"
                    className="secondary-button"
                    disabled={
                      busyId === selectedTask.id ||
                      selectedTask.running ||
                      selectedTask.paused
                    }
                    onClick={() =>
                      void withAction(
                        selectedTask.id,
                        () => api.runAutomationTask(selectedTask.id),
                        { bumpArtifacts: true },
                      )
                    }
                  >
                    Run now
                  </button>

                  {selectedTask.paused ? (
                    <button
                      type="button"
                      className="secondary-button"
                      disabled={busyId === selectedTask.id}
                      onClick={() =>
                        void withAction(selectedTask.id, () =>
                          api.resumeAutomationTask(selectedTask.id),
                        )
                      }
                    >
                      Resume
                    </button>
                  ) : (
                    <button
                      type="button"
                      className="secondary-button"
                      disabled={busyId === selectedTask.id}
                      onClick={() =>
                        void withAction(selectedTask.id, () =>
                          api.pauseAutomationTask(selectedTask.id),
                        )
                      }
                    >
                      Pause
                    </button>
                  )}

                  {selectedTask.running ? (
                    <button
                      type="button"
                      className="secondary-button"
                      disabled={busyId === selectedTask.id}
                      onClick={() =>
                        void withAction(selectedTask.id, () =>
                          api.cancelAutomationTask(selectedTask.id),
                        )
                      }
                    >
                      Cancel
                    </button>
                  ) : null}

                  <button
                    type="button"
                    className="secondary-button aut-danger"
                    disabled={busyId === selectedTask.id}
                    onClick={() => removeTask(selectedTask)}
                  >
                    Delete
                  </button>
                </div>
              </section>

              <RunTimeline runs={runs} />

              <TaskTools
                tools={tools}
                busy={busyId !== null}
                onApprove={(tool) =>
                  void withAction(tool.id, () =>
                    api.approveAutomationTaskTool(tool.id),
                  )
                }
              />

              <ArtifactSection
                taskId={selectedTask.id}
                revision={artifactsRevision}
              />
            </>
          ) : (
            <div className="aut-detail-empty">
              <div className="activity-empty-mark">✦</div>

              <strong>Select a task</strong>

              <span>
                The run timeline, scoped tools, artifacts and lifecycle
                controls will appear here. The list stays live — status
                refreshes every 15 seconds.
              </span>
            </div>
          )}

          {detailError ? (
            <div className="error-banner" role="alert">
              {detailError}
            </div>
          ) : null}
        </div>
      </div>
    </section>
  );
}
