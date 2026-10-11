// model-selection.test.ts — v1.5.0 unit tests for the model-first
// selection contracts (the logic ModelPicker renders).
import { test } from "node:test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import {
  backendVerdict,
  classifyFit,
  modelCardState,
  recommendedOnThisMachine,
  selectionBusy,
  SELECTION_BUSY_PHASES,
  SELECTION_PHASE_LABEL,
} from "./model-selection.ts";

test("selection busy phases: exactly the in-flight backend phases", () => {
  // The backend-authoritative chain — every in-flight phase locks.
  for (const phase of ["select", "analyzing", "configuring", "loading", "calibrating"]) {
    if (!selectionBusy(phase)) {
      throw new Error(`phase "${phase}" must be busy (a selection is in flight)`);
    }
  }

  // Terminal phases never lock the picker.
  for (const phase of ["ready", "failed"]) {
    if (selectionBusy(phase)) {
      throw new Error(`terminal phase "${phase}" must not be busy`);
    }
  }

  // No selection flow at all (fresh install) is not busy either.
  if (selectionBusy(null) || selectionBusy(undefined)) {
    throw new Error("no selection flow must not be busy");
  }
});

test("fit hint: a measured sizing verdict, never a guess", () => {
  // Unknown values → no hint.
  if (classifyFit(undefined, 16e9) !== null) throw new Error("no estimate → no hint");
  if (classifyFit(4e9, undefined) !== null) throw new Error("no measured RAM → no hint");
  if (classifyFit(0, 16e9) !== null) throw new Error("zero estimate → no hint");

  // Measured ratios → the honest verdicts.
  if (classifyFit(4e9, 16e9) !== "fits") throw new Error("25% of RAM fits");
  if (classifyFit(14e9, 16e9) !== "tight") throw new Error("87.5% of RAM is tight");
  if (classifyFit(20e9, 16e9) !== "over") throw new Error("125% of RAM is over");

  // The 0.85 boundary is exact.
  if (classifyFit(13.6e9, 16e9) !== "fits") throw new Error("85% exactly is fits");
  if (classifyFit(13.7e9, 16e9) !== "tight") throw new Error("85.6% is tight");
});

test("recommended label: ONLY from measured recommendation evidence", () => {
  // No evidence at all → never.
  if (recommendedOnThisMachine(null)) throw new Error("no evidence → never recommended");

  // Evidence WITHOUT measured hardware → never (the engine did not
  // actually assess this machine).
  if (recommendedOnThisMachine({ class: "safe", hardwareMeasured: false })) {
    throw new Error("unmeasured hardware → never recommended");
  }

  // Measured evidence with a NON-safe verdict → never.
  if (recommendedOnThisMachine({ class: "caution", hardwareMeasured: true })) {
    throw new Error("caution verdict → never recommended");
  }
  if (recommendedOnThisMachine({ class: "unsupported", hardwareMeasured: true })) {
    throw new Error("unsupported verdict → never recommended");
  }
  if (recommendedOnThisMachine({ class: "", hardwareMeasured: true })) {
    throw new Error("no verdict (card-less file) → never recommended");
  }

  // Measured + safe → the ONLY shape that earns the label.
  if (!recommendedOnThisMachine({ class: "safe", hardwareMeasured: true })) {
    throw new Error("measured safe evidence must earn the label");
  }
});

test("calibration state is visibly distinct from the final ready state", () => {
  // The calibrating phase locks the picker AND carries its own label —
  // it must never render as (or collapse into) "Ready".
  if (SELECTION_PHASE_LABEL["calibrating"] !== "Calibrating") {
    throw new Error('calibrating must be labeled "Calibrating"');
  }
  if (SELECTION_PHASE_LABEL["ready"] !== "Ready") {
    throw new Error('ready must be labeled "Ready"');
  }
  if (SELECTION_PHASE_LABEL["calibrating"] === SELECTION_PHASE_LABEL["ready"]) {
    throw new Error("calibrating and ready labels must differ");
  }
  // calibrating is an in-flight phase: the picker stays locked.
  if (!SELECTION_BUSY_PHASES.has("calibrating")) {
    throw new Error("calibrating must lock the picker");
  }
});

test("backend verdict: ONLY a measured unsupported verdict exists", () => {
  // No evidence / unmeasured hardware → no verdict at all.
  if (backendVerdict(null) !== null) throw new Error("no evidence → no verdict");
  if (backendVerdict(undefined) !== null) throw new Error("no evidence → no verdict");
  if (backendVerdict({ class: "unsupported", hardwareMeasured: false })) {
    throw new Error("unmeasured hardware → no verdict (a guess is not a verdict)");
  }

  // Measured verdicts pass through.
  if (backendVerdict({ class: "safe", hardwareMeasured: true }) !== "safe") {
    throw new Error("measured safe verdict must pass through");
  }
  if (backendVerdict({ class: "caution", hardwareMeasured: true }) !== "caution") {
    throw new Error("measured caution verdict must pass through");
  }
  if (backendVerdict({ class: "unsupported", hardwareMeasured: true }) !== "unsupported") {
    throw new Error("measured unsupported verdict must pass through");
  }

  // An unknown class is not a verdict.
  if (backendVerdict({ class: "", hardwareMeasured: true }) !== null) {
    throw new Error("empty class → no verdict");
  }
});

test("over-RAM estimate alone does NOT create a hard incompatibility", () => {
  // A huge sizing estimate with NO backend verdict → the card stays
  // available (the estimate renders as a warning chip, the model stays
  // selectable). The estimate is a sizing signal, not proof.
  const hugeEstimateNoVerdict = modelCardState({
    serving: false,
    targeting: false,
    verdict: null,
  });
  if (hugeEstimateNoVerdict !== "available") {
    throw new Error("an estimate alone must never hard-block: got " + hugeEstimateNoVerdict);
  }

  // Even a measured "caution" verdict stays available.
  if (modelCardState({ serving: false, targeting: false, verdict: "caution" }) !== "available") {
    throw new Error("caution is a warning, not a block");
  }

  // ONLY the backend's authoritative unsupported verdict blocks.
  if (modelCardState({ serving: false, targeting: false, verdict: "unsupported" }) !== "incompatible") {
    throw new Error("measured unsupported → incompatible");
  }

  // Serving/targeting keep priority over the verdict.
  if (modelCardState({ serving: true, targeting: false, verdict: "unsupported" }) !== "ready") {
    throw new Error("serving wins over any verdict");
  }
  if (modelCardState({ serving: false, targeting: true, verdict: null }) !== "loading") {
    throw new Error("targeting → loading");
  }
});

test("recommended setup cannot silently select a model (source contract)", () => {
  // v1.5.1 removed the "Use recommended setup" hidden auto-selection.
  // This source contract keeps it removed: the picker must contain no
  // auto-target logic (no sorting by estimated footprint, no
  // first-evidence-safe pick) and no onboarding button that would call
  // onUse outside an explicit user action.
  const picker = readFileSync(
    fileURLToPath(new URL("./ModelPicker.tsx", import.meta.url)),
    "utf8",
  );

  for (const banned of [
    /use recommended setup/i,
    /applying recommended setup/i,
    /estimatedMemoryBytes\s*[-+*/<>=]|sort\(\s*\([^)]*\)\s*=>\s*[^)]*estimatedMemoryBytes/,
    /MAX_SAFE_INTEGER/,
  ]) {
    if (banned.test(picker)) {
      throw new Error(`ModelPicker must not contain an implicit-selection pattern: ${banned}`);
    }
  }

  // v1.6.1: the ALLOWED onUse call sites are enumerated — every one is an
  // EXPLICIT user action, never a heuristic:
  //   1. the model card's "Use model" button click;
  //   2. the GGUF import flow (the user picked/typed a model file to
  //      import — selecting it is the requested outcome of that action,
  //      driven through the same selection API).
  // Adding another call site requires updating this contract with the
  // same justification: user-initiated, no footprint/filename/ordering
  // heuristics involved.
  const onUseCalls = [...picker.matchAll(/onUse\(/g)].length;
  if (onUseCalls !== 2) {
    throw new Error(
      `ModelPicker must call onUse exactly twice (the explicit card click + the explicit import flow), found ${onUseCalls}`,
    );
  }
});
