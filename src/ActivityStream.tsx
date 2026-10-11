import { memo, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";

import type { ActivityEvent } from "./store";
import { useRuntimeStore } from "./store";

const MAX_VISIBLE_EVENTS = 50;

const activityTimeFormatter = new Intl.DateTimeFormat([], {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

function formatActivity(activity: ActivityEvent): string {
  const data = activity.data;

  // v1.1.4: the backend agent.Activity contract carries the caption in
  // `caption` — this formatter previously probed message/content/text/error
  // (fields the backend never populates), so every event rendered as a bare
  // type label like "tool_start" instead of the actual caption.
  if (typeof data.caption === "string" && data.caption) {
    return data.caption;
  }

  if (typeof data.message === "string") {
    return data.message;
  }

  if (typeof data.error === "string") {
    return data.error;
  }

  return activity.type;
}

// v1.2.0 — progressive disclosure: the flat event list is grouped into the
// run's PHASES the way the agent actually works (plan → execute → repair →
// verify). The mapping is derived from the event types the backend already
// emits; nothing new is invented, and unknown types land in "Run".
type PhaseId = "plan" | "execution" | "repair" | "verification" | "run";

const PHASES: { id: PhaseId; label: string }[] = [
  { id: "plan", label: "Plan" },
  { id: "execution", label: "Execution" },
  { id: "repair", label: "Repair" },
  { id: "verification", label: "Verification" },
  { id: "run", label: "Run" },
];

function phaseFor(item: ActivityEvent): PhaseId {
  const type = item.type;
  const caption = typeof item.data.caption === "string" ? item.data.caption.toLowerCase() : "";

  // Verification evidence: build/test/diff/verify confirmations.
  if (
    type === "verify" ||
    type === "verification" ||
    caption.includes("verif") ||
    caption.includes("test") ||
    caption.includes("build passed") ||
    caption.includes("diff")
  ) {
    return "verification";
  }

  // Repair: fixing/rolling back/retry after failure.
  if (
    type === "repair" ||
    caption.includes("repair") ||
    caption.includes("fix") ||
    caption.includes("retry") ||
    caption.includes("rollback")
  ) {
    return "repair";
  }

  // Execution: tool calls and file modifications.
  if (type === "tool_start" || type === "tool_end" || type === "tool") {
    return "execution";
  }

  // Planning: context/session/engine preparation events that precede work.
  // v1.2.5: tier escalations and live status lines are context-preparation
  // signals — they group under Plan.
  if (
    type === "context" ||
    type === "session" ||
    type === "plan" ||
    type === "escalation" ||
    type === "status" ||
    type === "thinking_start" ||
    type === "thinking_end"
  ) {
    return "plan";
  }

  return "run";
}

interface PhaseGroup {
  id: PhaseId;
  label: string;
  items: ActivityEvent[];
  failed: boolean;
  done: boolean;
}

// groupByPhase keeps order stable and merges consecutive same-phase runs,
// so a long execution reads as one section instead of 30 single items.
function groupByPhase(events: ActivityEvent[]): PhaseGroup[] {
  const groups: PhaseGroup[] = [];

  for (const item of events) {
    const id = phaseFor(item);
    const last = groups[groups.length - 1];

    const group =
      last && last.id === id
        ? last
        : (() => {
            const next: PhaseGroup = {
              id,
              label: PHASES.find((p) => p.id === id)?.label ?? id,
              items: [],
              failed: false,
              done: false,
            };
            groups.push(next);
            return next;
          })();

    group.items.push(item);
    if (item.type.includes("fail") || item.type === "error") {
      group.failed = true;
    }
    if (item.type.includes("end") || item.type.includes("done") || item.type.includes("ok")) {
      group.done = true;
    }
  }

  return groups;
}

const ActivityItem = memo(function ActivityItem({
  item,
}: {
  item: ActivityEvent;
}) {
  const failed = item.type.includes("fail") || item.type === "error";

  return (
    <article className={`activity-item${failed ? " activity-item-failed" : ""}`}>
      <div className="activity-marker">
        <span>{failed ? "✕" : item.type.includes("end") || item.type.includes("ok") ? "✓" : ""}</span>
      </div>

      <div className="activity-content">
        <div className="activity-meta">
          <span>{item.type}</span>

          <time>{activityTimeFormatter.format(item.timestamp)}</time>
        </div>

        <p>{formatActivity(item)}</p>
      </div>
    </article>
  );
});

const PhaseSection = memo(function PhaseSection({
  group,
}: {
  group: PhaseGroup;
}) {
  const open = group.id === "run" || group.failed;

  return (
    <details className={`activity-phase phase-${group.id}${group.failed ? " phase-failed" : ""}`} open={open}>
      <summary>
        <span className="activity-phase-title">{group.label}</span>

        <span className="activity-phase-status">
          {group.failed ? "✕" : group.done ? "✓" : `${group.items.length}`}
        </span>
      </summary>

      {group.items.map((item) => (
        <ActivityItem key={item.id} item={item} />
      ))}
    </details>
  );
});

function ActivityStream() {
  const activity = useRuntimeStore((state) => state.activity);

  const clearActivity = useRuntimeStore((state) => state.clearActivity);

  const activityEndRef = useRef<HTMLDivElement | null>(null);

  const scrollFrameRef = useRef<number | null>(null);

  const deferredActivity = useDeferredValue(activity);

  const visibleActivity = useMemo(
    () =>
      deferredActivity.length > MAX_VISIBLE_EVENTS
        ? deferredActivity.slice(-MAX_VISIBLE_EVENTS)
        : deferredActivity,
    [deferredActivity],
  );

  // v1.2.0: grouped progressive disclosure. The raw list stays available
  // (toggle) for anyone who wants the unfiltered feed.
  const [grouped, setGrouped] = useState<boolean>(true);

  const groups = useMemo(
    () => (grouped ? groupByPhase(visibleActivity) : []),
    [grouped, visibleActivity],
  );

  useEffect(() => {
    if (scrollFrameRef.current !== null) {
      cancelAnimationFrame(scrollFrameRef.current);
    }

    scrollFrameRef.current = requestAnimationFrame(() => {
      scrollFrameRef.current = null;

      activityEndRef.current?.scrollIntoView({
        behavior: "auto",
        block: "nearest",
      });
    });

    return () => {
      if (scrollFrameRef.current !== null) {
        cancelAnimationFrame(scrollFrameRef.current);

        scrollFrameRef.current = null;
      }
    };
  }, [deferredActivity]);

  return (
    <div className="activity-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">LIVE</span>

          <strong>Activity</strong>
        </div>

        <div className="panel-heading-actions">
          <button
            type="button"
            className="text-button"
            onClick={() => setGrouped((value) => !value)}
            title={grouped ? "Show the raw event feed" : "Group events into run phases"}
          >
            {grouped ? "Phases" : "Raw"}
          </button>

          <button
            type="button"
            className="text-button"
            onClick={clearActivity}
            disabled={activity.length === 0}
          >
            Clear
          </button>
        </div>
      </div>

      <div className="activity-stream">
        {deferredActivity.length === 0 ? (
          <div className="activity-empty">
            <div className="activity-empty-mark">✦</div>

            <strong>Awaiting the first operation</strong>

            <span>Send a task below to begin a local agent run.</span>
          </div>
        ) : grouped ? (
          groups.map((group) => <PhaseSection key={`${group.id}-${group.items[0]?.id ?? ""}`} group={group} />)
        ) : (
          visibleActivity.map((item) => <ActivityItem key={item.id} item={item} />)
        )}

        <div ref={activityEndRef} />
      </div>
    </div>
  );
}

export default memo(ActivityStream);
