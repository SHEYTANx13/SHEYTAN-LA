// scroll-contract.test.ts — v1.3.0 regression tests for the workspace
// scroll contract.
//
// The v1.2.9 defect: .view-transition was a plain block under four
// overflow:hidden ancestors, so System (and tall Workspace/Research
// content) was clipped with NO scrollable viewport. The fix is a LAYOUT
// CONTRACT: the view wrapper becomes a shrinkable flex column and every
// tab owns a scroll viewport (simple surfaces scroll themselves; IDE
// surfaces keep internal scroll regions).
//
// These tests pin the contract in the actual shipped CSS + component
// markup so a refactor cannot silently remove it.
import { test } from "node:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const read = (name: string): string =>
  readFileSync(join(import.meta.dirname, name), "utf8");

const styles = read("styles.css");
const settingsCss = read("settings.css");

function assertContains(
  source: string,
  needle: string,
  label: string,
): void {
  if (!source.includes(needle)) {
    throw new Error(
      `scroll contract broken: ${label} — expected to find "${needle.trim()}"`,
    );
  }
}

test("view wrapper is a shrinkable flex column (the general contract)", () => {
  assertContains(
    styles,
    ".workspace > .view-transition",
    "general wrapper rule",
  );
  assertContains(styles, "min-height: 0;", "wrapper min-height");
  assertContains(styles, "flex: 1 1 auto;", "wrapper flex sizing");
});

test("system panel owns a vertical scroll viewport", () => {
  const idx = styles.indexOf(".system-panel");
  if (idx < 0) throw new Error("system-panel rule missing");
  const rule = styles.slice(idx, styles.indexOf("}", idx));
  if (!/overflow-y:\s*auto/.test(rule)) {
    throw new Error(`system-panel must scroll: ${rule}`);
  }
  if (!/flex:\s*1 1 auto/.test(rule)) {
    throw new Error(`system-panel must flex-fill: ${rule}`);
  }
});

test("workspace panel owns a vertical scroll viewport", () => {
  const idx = styles.indexOf(".workspace-panel");
  if (idx < 0) throw new Error("workspace-panel rule missing");
  const rule = styles.slice(idx, styles.indexOf("}", idx));
  if (!/overflow-y:\s*auto/.test(rule)) {
    throw new Error(`workspace-panel must scroll: ${rule}`);
  }
});

// v1.3.6: the "research panel owns a vertical scroll viewport" contract
// was removed with the Research tab itself (spec §21/§22) — Net Search
// renders inside the composers and no .research-panel rule exists.

test("settings page owns a vertical scroll viewport", () => {
  const idx = settingsCss.indexOf(".settings-page");
  if (idx < 0) throw new Error("settings-page rule missing");
  const rule = settingsCss.slice(idx, settingsCss.indexOf("}", idx));
  if (!/overflow-y:\s*auto/.test(rule)) {
    throw new Error(`settings-page must scroll: ${rule}`);
  }
});

test("coding lab keeps its internal scroll regions (IDE-style surface)", () => {
  const labIdx = styles.indexOf(".lab-panel");
  if (labIdx < 0) throw new Error("lab-panel rule missing");
  const labRule = styles.slice(labIdx, styles.indexOf("}", labIdx));
  if (!/overflow:\s*hidden/.test(labRule)) {
    throw new Error("lab-panel must clip and delegate to internal regions");
  }

  // The internal scroll regions exist.
  for (const region of [".lab-task-list", ".lab-detail"]) {
    const idx = styles.indexOf(region);
    if (idx < 0) throw new Error(`${region} rule missing`);
    const rule = styles.slice(idx, styles.indexOf("}", idx));
    if (!/overflow(-y)?:\s*auto/.test(rule)) {
      throw new Error(`${region} must scroll internally: ${rule}`);
    }
  }
});

test("agent surface keeps its internal scroll regions (IDE-style surface)", () => {
  // The conversation stream is the agent's scroll region.
  const idx = styles.indexOf(".conversation-stream");
  if (idx < 0) throw new Error("conversation-stream rule missing");
  const rule = styles.slice(idx, styles.indexOf("}", idx));
  if (!/overflow-y:\s*auto/.test(rule)) {
    throw new Error(`conversation-stream must scroll: ${rule}`);
  }
});

test("no per-tab :has() special cases remain (one general contract)", () => {
  if (/\.view-transition:has\(/.test(settingsCss) || /\.view-transition:has\(/.test(styles)) {
    throw new Error(
      "per-tab :has() special cases must not return — the general " +
        ".workspace > .view-transition contract covers every tab",
    );
  }
});

test("reduce-motion class contract (accessibility)", () => {
  assertContains(
    read("motion.css"),
    "html.reduce-motion",
    "reduce-motion class rule",
  );
});
