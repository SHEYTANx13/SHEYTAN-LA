// settings-sections.test.ts — v1.3.0 regression tests for the Settings
// information architecture.
//
// Contracts under test (release contract §6/§7/§11):
//   1. exactly EIGHT coherent sections with the agreed ids;
//   2. the MM projector path field is NOT an editable control in any
//      normal section (it reads "Automatically managed");
//   3. technical engine fields (host/port/bin path/extra args, cache
//      tuning, batch/thread controls, storage limits) live ONLY in
//      Advanced;
//   4. legacy config values remain represented (fields removed from the
//      UI are still persisted — the backend struct keeps them).
import { test } from "node:test";
import {
  SETTINGS_SECTIONS,
  FIELD_PLACEMENT,
  REMOVED_FROM_NORMAL_UI,
  isNormalSection,
} from "./settings-sections.ts";

test("exactly eight coherent sections", () => {
  const ids = SETTINGS_SECTIONS.map((s) => s.id);
  if (ids.length !== 8) {
    throw new Error(`expected 8 sections, got ${ids.length}`);
  }
  const expected = [
    "general",
    "models",
    "performance",
    "agent",
    "network",
    "updates",
    "diagnostics",
    "advanced",
  ];
  for (const id of expected) {
    if (!ids.includes(id as (typeof ids)[number])) {
      throw new Error(`section "${id}" missing`);
    }
  }
});

test("every section carries a label and a hint", () => {
  for (const section of SETTINGS_SECTIONS) {
    if (!section.label || !section.hint) {
      throw new Error(`section ${section.id} is incomplete: ${JSON.stringify(section)}`);
    }
  }
});

test("the MM projector field is never an editable normal control", () => {
  const placement = FIELD_PLACEMENT["visionMmproj"];
  if (placement !== "advanced") {
    throw new Error(
      `visionMmproj must live ONLY in Advanced (got ${placement}) — the projector is detected, matched, loaded and verified automatically`,
    );
  }
});

test("technical engine fields live only in Advanced", () => {
  const expertOnly = [
    "llamaBinPath",
    "llamaHost",
    "llamaPort",
    "llamaExtraArgs",
    "kvCacheQuant",
    "cacheReuse",
    "ubatchSize",
    "threadsBatch",
    "mlock",
    "draftModel",
    "sandboxMemory",
    "sandboxCPU",
    "maxWorkspaceMb",
    "maxSessionsKept",
    "maxLogMb",
  ];
  for (const field of expertOnly) {
    const placement = FIELD_PLACEMENT[field];
    if (placement !== "advanced") {
      throw new Error(`${field} must be Advanced-only (got ${placement})`);
    }
  }
});

test("fields removed from the normal UI are tracked", () => {
  if (REMOVED_FROM_NORMAL_UI.length === 0) {
    throw new Error("the removal audit list must not be empty");
  }
  for (const field of REMOVED_FROM_NORMAL_UI) {
    if (!(field in FIELD_PLACEMENT)) {
      throw new Error(`${field} is listed as removed but has no placement record`);
    }
  }
});

test("user-facing outcome sections exist and are distinct from Advanced", () => {
  // Performance exposes the high-level postures — verified through the
  // placement of its controls.
  if (FIELD_PLACEMENT["numCtx"] !== "performance") {
    throw new Error("context belongs to Performance");
  }
  if (FIELD_PLACEMENT["thinkingMode"] !== "agent") {
    throw new Error("thinking mode belongs to Agent & Tools");
  }
  if (FIELD_PLACEMENT["researchEnabled"] !== "network") {
    throw new Error("research belongs to Network");
  }
  if (FIELD_PLACEMENT["updateSchedule"] !== "updates") {
    throw new Error("update policy belongs to Updates");
  }
});

test("isNormalSection distinguishes the expert surface", () => {
  if (isNormalSection("advanced")) {
    throw new Error("advanced is the expert surface");
  }
  if (!isNormalSection("general")) {
    throw new Error("general is a normal surface");
  }
});
