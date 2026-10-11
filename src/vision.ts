// v1.2.0 — shared vision state rendering. The UI must never show
// "Vision: Yes" merely because a filename contains mmproj: every visible
// state here maps 1:1 to the backend's evidence-based state machine
// (internal/vision states.go).

export type VisionTone = "good" | "warn" | "bad" | "neutral";

export interface VisionBadge {
  /** Compact label for chips and badges. */
  label: string;
  /** Symbol shown before the label. */
  symbol: string;
  /** Tone used to pick the chip color. */
  tone: VisionTone;
  /** Full explanation (the backend-provided evidence goes here). */
  title: string;
}

export function visionBadge(
  state: string | undefined,
  reason?: string,
  projectorName?: string,
): VisionBadge {
  const evidence = reason ? ` — ${reason}` : "";

  switch (state) {
    case "ready":
    case "projector-verified":
      return {
        label: "Ready",
        symbol: "✓",
        tone: "good",
        title: `Vision ready${projectorName ? ` (${projectorName})` : ""}${evidence}`,
      };
    case "loading":
      return {
        label: "Loading",
        symbol: "…",
        tone: "neutral",
        title: `Verifying the vision projector with the engine boot${evidence}`,
      };
    case "projector-found":
      return {
        label: projectorName ? "mmproj" : "Projector found",
        symbol: "✓",
        tone: "good",
        title: `A compatible projector is paired with this model${evidence}`,
      };
    case "projector-missing":
      return {
        label: "No mmproj",
        symbol: "!",
        tone: "warn",
        title: `Vision-capable model, but no compatible projector is in the models folder${evidence}`,
      };
    case "supported":
      return {
        label: "Supported",
        symbol: "!",
        tone: "warn",
        title: `Vision-capable architecture; add the model family's mmproj file to enable vision${evidence}`,
      };
    case "degraded":
      return {
        label: "Degraded",
        symbol: "!",
        tone: "warn",
        title: `The projector failed with every launch profile; running text-only${evidence}`,
      };
    case "failed":
      return {
        label: "Failed",
        symbol: "✕",
        tone: "bad",
        title: `Vision is not usable right now${evidence}`,
      };
    case "unsupported":
      return {
        label: "Unsupported",
        symbol: "✕",
        tone: "neutral",
        title: `This model has no vision encoder${evidence}`,
      };
    default:
      return {
        label: "—",
        symbol: "",
        tone: "neutral",
        title: "Vision capability not evaluated yet.",
      };
  }
}
