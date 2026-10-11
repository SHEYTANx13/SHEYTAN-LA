// model-selection.ts — v1.5.0 the pure model-first selection logic,
// extended in v1.5.1 with the honest per-card state machine.
//
// Extracted from ModelPicker so the CONTRACTS are unit-testable:
//
//   - the backend-authoritative selection phases and which of them lock
//     the picker (the UI never invents a phase);
//   - the fit hint is a MEASURED sizing verdict (estimated footprint vs
//     measured host RAM) and never a "best model" claim;
//   - "Recommended for this machine" requires the recommendation
//     engine's measured evidence — never a filename/size heuristic;
//   - a model card is "Incompatible" ONLY on the backend's measured
//     unsupported verdict — a RAM estimate alone is a warning, never a
//     hard block (spec §4);
//   - there is NO implicit selection path: the picker has no helper
//     that turns evidence or ordering into a model choice (spec §4).

export type SelectionPhase =
  | "select"
  | "analyzing"
  | "configuring"
  | "loading"
  | "calibrating"
  | "ready"
  | "failed";

/** The phases during which a selection flow is in flight (cards lock). */
export const SELECTION_BUSY_PHASES: ReadonlySet<string> = new Set([
  "select",
  "analyzing",
  "configuring",
  "loading",
  "calibrating",
]);

/** The user-facing label for a backend selection phase. */
export const SELECTION_PHASE_LABEL: Record<string, string> = {
  select: "Select",
  analyzing: "Analyzing",
  configuring: "Configuring",
  loading: "Loading",
  calibrating: "Calibrating",
  ready: "Ready",
  failed: "Failed",
};

/** True while the backend selection flow is between select and ready. */
export function selectionBusy(phase: string | null | undefined): boolean {
  return phase !== null && phase !== undefined && SELECTION_BUSY_PHASES.has(phase);
}

/** The fit verdict: a measured sizing hint, NOT a recommendation. */
export type Fit = "fits" | "tight" | "over" | null;

/**
 * classifyFit derives the honest sizing hint from measured values: the
 * backend's estimated footprint against the host's measured total RAM.
 * It answers "can this machine hold it?" — nothing more. Unknown
 * values render no hint (never a guess).
 */
export function classifyFit(
  estimatedMemoryBytes: number | undefined,
  totalRamBytes: number | undefined,
): Fit {
  if (
    !estimatedMemoryBytes ||
    estimatedMemoryBytes <= 0 ||
    !totalRamBytes ||
    totalRamBytes <= 0
  ) {
    return null;
  }

  const ratio = estimatedMemoryBytes / totalRamBytes;

  if (ratio > 1) {
    return "over";
  }

  if (ratio <= 0.85) {
    return "fits";
  }

  return "tight";
}

/** The recommendation engine's per-model evidence shape. */
export interface ModelRecommendationEvidence {
  class?: string;
  speed?: string;
  context?: number;
  gpuLayers?: number;
  gpuAutoOffload?: boolean;
  predictedVramBytes?: number;
  reasons?: string[];
  notes?: string[];
  hardwareMeasured?: boolean;
}

/**
 * recommendedOnThisMachine decides the "Recommended for this machine"
 * label. It is TRUE only when the recommendation engine produced
 * measured evidence (hardwareMeasured) AND classified the model as a
 * safe resource fit — never from the filename, the size, or any
 * client-side heuristic.
 */
export function recommendedOnThisMachine(
  evidence: ModelRecommendationEvidence | null | undefined,
): boolean {
  return evidence?.hardwareMeasured === true && evidence.class === "safe";
}

/** The backend's authoritative resource verdict for one model card. */
export type BackendVerdict = "safe" | "caution" | "unsupported";

/**
 * backendVerdict extracts the recommendation engine's MEASURED resource
 * verdict for a model (hardwareMeasured + class). It is the ONLY signal
 * that may hard-block a model card. Returns null when the backend has
 * no measured verdict for this machine — a sizing estimate is never a
 * verdict.
 */
export function backendVerdict(
  evidence: ModelRecommendationEvidence | null | undefined,
): BackendVerdict | null {
  if (evidence?.hardwareMeasured !== true) {
    return null;
  }

  if (
    evidence.class === "safe" ||
    evidence.class === "caution" ||
    evidence.class === "unsupported"
  ) {
    return evidence.class;
  }

  return null;
}

/** The v1.5.1 per-card state machine. */
export type CardState = "ready" | "loading" | "incompatible" | "available";

export interface CardStateInput {
  /** The engine is serving this model right now. */
  serving: boolean;
  /** A selection/model operation is targeting THIS model. */
  targeting: boolean;
  /** The backend's measured verdict (null = none measured). */
  verdict: BackendVerdict | null;
}

/**
 * modelCardState — the honest card state machine (v1.5.1):
 *
 *   ready         — the engine is serving this model right now;
 *   loading       — a selection targeting this exact model is in flight;
 *   incompatible  — ONLY the backend's authoritative unsupported
 *                   verdict blocks a model (spec §4);
 *   available     — everything else. A RAM estimate above host memory is
 *                   a WARNING (the fit chip), never a hard block: the
 *                   estimate is a sizing signal, not proof of runtime
 *                   impossibility.
 */
export function modelCardState(input: CardStateInput): CardState {
  if (input.serving) {
    return "ready";
  }

  if (input.targeting) {
    return "loading";
  }

  if (input.verdict === "unsupported") {
    return "incompatible";
  }

  return "available";
}
