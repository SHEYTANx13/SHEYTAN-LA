// automation-view.test.ts — regression contract for the Automation
// panel's pure view helpers (formatting + viewer decisions; no I/O).
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  artifactViewMode,
  formatArtifactSize,
  formatInterval,
  formatNextRun,
  formatRelativeTime,
  formatRunDuration,
  formatSchedule,
  prettyJsonIfNeeded,
  runState,
  toRFC3339Local,
  fromRFC3339ToLocalInput,
  triggerLabel,
  weekdayLabel,
} from "./automation-view.ts";

// 2025-06-15T12:00:00Z — the fixed "now" for relative-time tests.
const NOW = Date.parse("2025-06-15T12:00:00Z");

test("unset times render as — (missing, invalid, Go zero time)", () => {
  assert.equal(formatNextRun(undefined), "—");
  assert.equal(formatNextRun(null), "—");
  assert.equal(formatNextRun(""), "—");
  assert.equal(formatNextRun("0001-01-01T00:00:00Z"), "—");
  assert.equal(formatNextRun("not-a-time"), "—");
  assert.equal(formatNextRun("2025-06-15T12:00:00Z"), new Date("2025-06-15T12:00:00Z").toLocaleString());
});

test("relative time buckets are honest and deterministic", () => {
  assert.equal(formatRelativeTime(undefined, NOW), "—");
  assert.equal(formatRelativeTime("0001-01-01T00:00:00Z", NOW), "—");
  assert.equal(formatRelativeTime("2025-06-15T12:00:00Z", NOW), "just now");
  assert.equal(formatRelativeTime("2025-06-15T11:59:30Z", NOW), "30s ago");
  assert.equal(formatRelativeTime("2025-06-15T11:30:00Z", NOW), "30m ago");
  assert.equal(formatRelativeTime("2025-06-15T09:00:00Z", NOW), "3h ago");
  assert.equal(formatRelativeTime("2025-06-13T12:00:00Z", NOW), "2d ago");
  // Future timestamps never go negative — they clamp to "just now".
  assert.equal(formatRelativeTime("2025-06-15T12:00:10Z", NOW), "just now");
});

test("run durations render in human units", () => {
  assert.equal(formatRunDuration(undefined), "—");
  assert.equal(formatRunDuration(-5), "—");
  assert.equal(formatRunDuration(0), "0 ms");
  assert.equal(formatRunDuration(940), "940 ms");
  assert.equal(formatRunDuration(12_340), "12.34 s");
  assert.equal(formatRunDuration(90_000), "1m 30s");
  assert.equal(formatRunDuration(3_600_000), "1h");
  assert.equal(formatRunDuration(5_400_000), "1h 30m");
});

test("intervals and weekday/trigger labels render compactly", () => {
  assert.equal(formatInterval(undefined), "");
  assert.equal(formatInterval(0), "");
  assert.equal(formatInterval(300), "5m");
  assert.equal(formatInterval(3600), "1h");
  assert.equal(formatInterval(7200), "2h");
  assert.equal(formatInterval(90), "90s");
  assert.equal(triggerLabel("git_change"), "Git change");
  assert.equal(triggerLabel("scheduled_maintenance"), "Maintenance");
  assert.equal(triggerLabel("mystery"), "mystery");
  assert.equal(triggerLabel(undefined), "—");
  assert.equal(weekdayLabel(0), "Sunday");
  assert.equal(weekdayLabel(6), "Saturday");
  assert.equal(weekdayLabel(7), "");
});

test("schedule summaries prefer the v1.7.0 schedule", () => {
  assert.equal(formatSchedule(null), "—");
  assert.equal(
    formatSchedule({ trigger: "manual" }),
    "Manual",
  );
  assert.equal(
    formatSchedule({
      trigger: "timer",
      intervalSeconds: 900,
      scheduleV17: { kind: "daily", timeOfDay: "08:30" },
    }),
    "Daily at 08:30",
  );
  assert.equal(
    formatSchedule({
      trigger: "timer",
      intervalSeconds: 900,
      scheduleV17: { kind: "weekly", timeOfDay: "08:30", weekday: 1 },
    }),
    "Weekly · Monday at 08:30",
  );
  assert.equal(
    formatSchedule({
      trigger: "timer",
      intervalSeconds: 3600,
      scheduleV17: { kind: "once", at: "0001-01-01T00:00:00Z" },
    }),
    "Once · —",
  );
  assert.equal(
    formatSchedule({ trigger: "timer", intervalSeconds: 3600 }),
    "Every 1h",
  );
  assert.equal(
    formatSchedule({ trigger: "scheduled_maintenance", intervalSeconds: 3600 }),
    "Every 1h",
  );
});

test("run state classifies canceled above ok", () => {
  assert.equal(runState(undefined), "failed");
  assert.equal(runState({ ok: true }), "ok");
  assert.equal(runState({ ok: true, canceled: true }), "canceled");
  assert.equal(runState({ ok: false }), "failed");
});

test("artifact view mode never executes HTML and never inlines SVG", () => {
  assert.equal(
    artifactViewMode({ kind: "doc", relPath: "report.md" }),
    "markdown",
  );
  assert.equal(
    artifactViewMode({ kind: "doc", relPath: "notes.txt" }),
    "source",
  );
  assert.equal(
    artifactViewMode({ kind: "code", relPath: "main.go" }),
    "source",
  );
  assert.equal(
    artifactViewMode({ kind: "code", relPath: "page.html" }),
    "source",
  );
  assert.equal(
    artifactViewMode({ kind: "chart", relPath: "chart.svg" }),
    "svg",
  );
  assert.equal(
    artifactViewMode({ kind: "chart", relPath: "chart.txt" }),
    "source",
  );
  assert.equal(
    artifactViewMode({ kind: "image", relPath: "shot.png" }),
    "image",
  );
  assert.equal(
    artifactViewMode({ kind: "image", relPath: "raw.bin" }),
    "source",
  );
  assert.equal(
    artifactViewMode({ kind: "data", relPath: "data.json" }),
    "source",
  );
});

test("data artifacts named .json pretty-print; invalid JSON stays raw", () => {
  assert.equal(
    prettyJsonIfNeeded("data", "out.json", '{"b":1,"a":[1,2]}'),
    '{\n  "b": 1,\n  "a": [\n    1,\n    2\n  ]\n}',
  );
  assert.equal(prettyJsonIfNeeded("data", "out.json", "{broken"), "{broken");
  assert.equal(prettyJsonIfNeeded("data", "out.csv", "a,b"), "a,b");
  assert.equal(prettyJsonIfNeeded("code", "x.json", "{}"), "{}");
});

test("artifact sizes render with compact units", () => {
  assert.equal(formatArtifactSize(undefined), "—");
  assert.equal(formatArtifactSize(-1), "—");
  assert.equal(formatArtifactSize(512), "512 B");
  assert.equal(formatArtifactSize(2048), "2.0 KB");
  assert.equal(formatArtifactSize(5 * 1024 * 1024), "5.0 MB");
  assert.equal(formatArtifactSize(1.5 * 1024 ** 3), "1.50 GB");
});

test("datetime-local ↔ RFC3339 round-trips through the local clock", () => {
  const local = "2025-06-15T10:30";
  const rfc3339 = toRFC3339Local(local);
  assert.ok(rfc3339);
  assert.equal(fromRFC3339ToLocalInput(rfc3339 as string), local);

  assert.equal(toRFC3339Local(""), null);
  assert.equal(toRFC3339Local("garbage"), null);
  assert.equal(fromRFC3339ToLocalInput("0001-01-01T00:00:00Z"), "");
  assert.equal(fromRFC3339ToLocalInput(undefined), "");
});
