// automation-view.ts — pure formatting/decision helpers for the
// Automation panel (v1.7.0): next-run times, run durations, schedule
// summaries, artifact viewer mode and JSON pretty-printing.
//
// Pure module: no React, no fetch, no DOM — the testable pattern this
// codebase uses for view logic (see repoindex-view.ts, clone-url.ts).
// Types are structural re-declarations (NOT imports of api.ts) so the
// node:test runner can load this file without the browser-only config
// module underneath api.ts.

// ScheduleLike mirrors api.AutomationSchedule (structural subset).
export interface ScheduleLike {
  kind?: string;
  at?: string;
  timeOfDay?: string;
  weekday?: number;
}

// TaskLike mirrors the api.AutomationTask fields the helpers read.
export interface TaskLike {
  trigger?: string;
  intervalSeconds?: number;
  scheduleV17?: ScheduleLike;
}

// RunLike mirrors api.AutomationRunReport (structural subset).
export interface RunLike {
  startedAt?: string;
  durationMs?: number;
  ok?: boolean;
  canceled?: boolean;
}

// ArtifactLike mirrors api.AutomationArtifact (structural subset).
export interface ArtifactLike {
  kind: string;
  relPath: string;
}

// ZERO_TIME_PREFIX is how a Go zero time.Time marshals to RFC3339. The
// scheduler's `nextDue`/scheduleV17.at use json omitempty, which does not
// apply to structs — an unset next due time arrives as year 0001, not as
// a missing field. The UI renders it as "—" (unset), never as a date.
export const ZERO_TIME_PREFIX = "0001-01-01";

const WEEKDAYS = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
] as const;

const TRIGGER_LABELS: Record<string, string> = {
  manual: "Manual",
  timer: "Timer",
  startup: "On startup",
  file_change: "File change",
  git_change: "Git change",
  test_failure: "Test failure",
  ci_failure: "CI failure",
  build_failure: "Build failure",
  scheduled_maintenance: "Maintenance",
};

export function triggerLabel(trigger: string | undefined): string {
  if (!trigger) return "—";
  return TRIGGER_LABELS[trigger] ?? trigger;
}

export function weekdayLabel(weekday: number | undefined): string {
  if (
    typeof weekday !== "number" ||
    !Number.isInteger(weekday) ||
    weekday < 0 ||
    weekday > 6
  ) {
    return "";
  }

  return WEEKDAYS[weekday] ?? "";
}

// isUnsetTime reports whether an RFC3339 string is absent, unparseable or
// the Go zero time — all three render as "—" (honest unset).
export function isUnsetTime(value: string | undefined | null): boolean {
  if (!value) return true;
  if (value.startsWith(ZERO_TIME_PREFIX)) return true;
  return Number.isNaN(new Date(value).getTime());
}

// formatWhen renders an RFC3339 timestamp in the viewer's locale, or "—"
// for absent/zero/invalid values.
export function formatWhen(value: string | undefined | null): string {
  if (isUnsetTime(value)) return "—";
  return new Date(value as string).toLocaleString();
}

// formatNextRun renders the task's next scheduled firing ("—" when unset).
export function formatNextRun(nextDue: string | undefined | null): string {
  return formatWhen(nextDue);
}

// formatRelativeTime renders "n units ago" relative to `now` (ms epoch,
// defaulting to Date.now()). Deterministic when `now` is passed — the
// unit tests rely on that.
export function formatRelativeTime(
  value: string | undefined | null,
  now: number = Date.now(),
): string {
  if (isUnsetTime(value)) return "—";

  const diff = Math.max(0, now - new Date(value as string).getTime());
  const seconds = Math.floor(diff / 1000);

  if (seconds < 10) return "just now";
  if (seconds < 60) return `${seconds}s ago`;

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;

  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

// formatRunDuration renders one run's wall-clock duration ("—" when the
// duration is missing; the same honesty rules as the Lab panel's).
export function formatRunDuration(ms: number | undefined): string {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "—";

  if (ms < 1000) return `${Math.round(ms)} ms`;

  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(2)} s`;

  const wholeMinutes = Math.floor(seconds / 60);
  const restSeconds = Math.round(seconds - wholeMinutes * 60);
  if (wholeMinutes < 60) {
    return restSeconds > 0
      ? `${wholeMinutes}m ${restSeconds}s`
      : `${wholeMinutes}m`;
  }

  const hours = Math.floor(wholeMinutes / 60);
  const restMinutes = wholeMinutes - hours * 60;
  return restMinutes > 0 ? `${hours}h ${restMinutes}m` : `${hours}h`;
}

// formatInterval renders a timer interval in seconds as a compact human
// string (the scheduler floor is 5 minutes).
export function formatInterval(seconds: number | undefined): string {
  if (
    typeof seconds !== "number" ||
    !Number.isFinite(seconds) ||
    seconds <= 0
  ) {
    return "";
  }

  if (seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return hours === 1 ? "1h" : `${hours}h`;
  }

  if (seconds % 60 === 0) {
    return `${seconds / 60}m`;
  }

  return `${seconds}s`;
}

// formatSchedule renders the task's firing rule in one line. Timer-ish
// tasks prefer the extended v1.7.0 schedule (once/daily/weekly) and fall
// back to the raw interval; non-scheduled triggers just say what they are.
export function formatSchedule(task: TaskLike | null | undefined): string {
  if (!task) return "—";

  const schedule = task.scheduleV17;
  const kind = schedule?.kind;

  if (kind === "once") {
    return `Once · ${formatWhen(schedule?.at)}`;
  }

  if (kind === "daily") {
    return `Daily at ${schedule?.timeOfDay || "—"}`;
  }

  if (kind === "weekly") {
    const day = weekdayLabel(schedule?.weekday);
    return `Weekly · ${day || "—"} at ${schedule?.timeOfDay || "—"}`;
  }

  if (task.trigger === "timer" || task.trigger === "scheduled_maintenance") {
    const interval = formatInterval(task.intervalSeconds);
    return interval ? `Every ${interval}` : triggerLabel(task.trigger);
  }

  return triggerLabel(task.trigger);
}

// runState classifies a settled report for badge colouring: canceled
// outranks ok (a canceled run is not a pass), then ok/failed.
export function runState(
  report: RunLike | null | undefined,
): "ok" | "failed" | "canceled" {
  if (!report) return "failed";
  if (report.canceled) return "canceled";
  return report.ok ? "ok" : "failed";
}

// RUN_STATE_CHIP maps run state → settings-chip tone classes.
export const RUN_STATE_CHIP: Record<"ok" | "failed" | "canceled", string> = {
  ok: "chip-good",
  failed: "chip-bad",
  canceled: "chip-neutral",
};

// RUN_STATE_LABEL is the honest human word for each state.
export const RUN_STATE_LABEL: Record<"ok" | "failed" | "canceled", string> = {
  ok: "OK",
  failed: "FAILED",
  canceled: "CANCELED",
};

export type ArtifactViewMode = "markdown" | "image" | "svg" | "source";

const MARKDOWN_EXTENSIONS = [".md", ".markdown"] as const;

const IMAGE_EXTENSIONS = [
  ".png",
  ".jpg",
  ".jpeg",
  ".webp",
  ".gif",
  ".bmp",
] as const;

function hasExtension(filename: string, extensions: readonly string[]): boolean {
  const lower = filename.toLowerCase();
  return extensions.some((ext) => lower.endsWith(ext));
}

// artifactViewMode decides HOW an artifact renders:
//
//   markdown → rendered through react-markdown (Markdown is first-class)
//   image    → <img src> to the sandboxed content endpoint
//   svg      → <img src> (NEVER inlined into the DOM — XSS containment)
//   source   → <pre> text (HTML is shown as source, never executed)
//
// The decision is pure so the viewer can never drift from the tests.
export function artifactViewMode(artifact: ArtifactLike): ArtifactViewMode {
  if (artifact.kind === "doc" && hasExtension(artifact.relPath, MARKDOWN_EXTENSIONS)) {
    return "markdown";
  }

  if (artifact.kind === "image" && hasExtension(artifact.relPath, IMAGE_EXTENSIONS)) {
    return "image";
  }

  if (artifact.kind === "chart" && artifact.relPath.toLowerCase().endsWith(".svg")) {
    return "svg";
  }

  return "source";
}

// prettyJsonIfNeeded returns indented JSON for data artifacts named *.json
// and the raw content unchanged for everything else (invalid JSON falls
// back to the raw body — the viewer never fabricates content).
export function prettyJsonIfNeeded(
  kind: string,
  relPath: string,
  content: string,
): string {
  if (kind !== "data" || !relPath.toLowerCase().endsWith(".json")) {
    return content;
  }

  try {
    return JSON.stringify(JSON.parse(content), null, 2);
  } catch {
    return content;
  }
}

// formatArtifactSize renders bytes with the same compact rules as the
// Workspace panel's file list.
export function formatArtifactSize(bytes: number | undefined): string {
  if (
    typeof bytes !== "number" ||
    !Number.isFinite(bytes) ||
    bytes < 0
  ) {
    return "—";
  }

  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

// toRFC3339Local converts a datetime-local input value
// ("YYYY-MM-DDTHH:MM", interpreted as LOCAL time by <input>) into an
// RFC3339 instant. Returns null when the value is empty/invalid — the
// caller decides whether that is a form error.
export function toRFC3339Local(value: string): string | null {
  if (!value) return null;

  const date = new Date(value);

  if (Number.isNaN(date.getTime())) return null;

  return date.toISOString();
}

// pad2 is the zero-padding shared by the local-input formatters.
function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}

// fromRFC3339ToLocalInput renders an RFC3339 instant as a
// datetime-local value in the viewer's local wall clock (used to prefill
// the "once" schedule picker when editing).
export function fromRFC3339ToLocalInput(value: string | undefined): string {
  if (isUnsetTime(value)) return "";

  const date = new Date(value as string);

  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(
    date.getDate(),
  )}T${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}
