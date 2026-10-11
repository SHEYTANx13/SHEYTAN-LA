// AgentTaskPanel.tsx — v1.2.8 agent pipeline panel.
//
// Renders the BOUNDED task state the backend maintains from the run's REAL
// tool traffic (agent.TaskState): goal, current step, files
// inspected/changed, commands and tests run, failures/repairs, the
// objective verification verdict, artifacts and the next step.
//
// This is the visible form of the actual runtime workflow (UNDERSTAND →
// INSPECT → … → VERIFY → REPORT) — not a decorative stage strip: every
// line comes from observed backend evidence, and nothing renders when the
// run has produced no task state. Activity summaries stay concise; no
// internal reasoning content is exposed here.

import { memo } from "react";

import { useRuntimeStore } from "./store";

type TaskListProps = {
  label: string;
  items: unknown;
  max?: number;
};

function asStringArray(value: unknown, max = 4): string[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .filter((item): item is string => typeof item === "string")
    .slice(0, max);
}

const TaskList = memo(function TaskList({ label, items, max = 4 }: TaskListProps) {
  const values = asStringArray(items, max);

  if (values.length === 0) {
    return null;
  }

  return (
    <div className="task-row">
      <span className="task-row-label">{label}</span>

      <span className="task-row-items">
        {values.map((item, index) => (
          <span key={`${index}-${item}`} className="task-item" title={item}>
            {item}
          </span>
        ))}
      </span>
    </div>
  );
});

function AgentTaskPanel() {
  const task = useRuntimeStore((state) => state.agentTask);

  if (!task || typeof task !== "object") {
    return null;
  }

  const goal = typeof task.goal === "string" ? task.goal : "";
  const step = typeof task.currentStep === "string" ? task.currentStep : "";
  const verification =
    typeof task.verification === "string" ? task.verification : "";
  const nextStep = typeof task.nextStep === "string" ? task.nextStep : "";

  const hasAnything =
    goal !== "" ||
    step !== "" ||
    verification !== "" ||
    asStringArray(task.filesInspected).length > 0 ||
    asStringArray(task.filesChanged).length > 0 ||
    asStringArray(task.commandsRun).length > 0 ||
    asStringArray(task.testsRun).length > 0 ||
    asStringArray(task.failures).length > 0;

  if (!hasAnything) {
    return null;
  }

  return (
    <aside className="agent-task-panel" aria-label="Agent task state">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">TASK</span>
          <strong>Agent pipeline</strong>
        </div>

        {step ? <span className="task-step">{step}</span> : null}
      </div>

      {goal ? <p className="task-goal">{goal}</p> : null}

      <TaskList label="Inspecting" items={task.filesInspected} />
      <TaskList label="Changed" items={task.filesChanged} />
      <TaskList label="Commands" items={task.commandsRun} max={3} />
      <TaskList label="Tests" items={task.testsRun} max={3} />
      <TaskList label="Failures" items={task.failures} max={2} />

      {verification ? (
        <div className="task-row">
          <span className="task-row-label">Verification</span>

          <span className={`task-verification value-${verification.startsWith("verified") ? "good" : verification === "failed" ? "bad" : "neutral"}`}>
            {verification}
          </span>
        </div>
      ) : null}

      {nextStep ? (
        <div className="task-row">
          <span className="task-row-label">Next</span>
          <span className="task-next">{nextStep}</span>
        </div>
      ) : null}
    </aside>
  );
}

export default memo(AgentTaskPanel);
