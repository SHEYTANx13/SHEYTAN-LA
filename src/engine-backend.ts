// engine-backend.ts — v1.6.2: the pure state derivation behind the
// Settings → Performance "Engine backend" card (the real Settings
// surface for engine-variant provisioning — API → updater transaction →
// restart → health/backend verification → UI state).
//
// The card itself (SettingsPerformance.tsx) only renders what these
// functions derive, so the logic is unit-testable without a DOM.

export type BackendAction = {
  /** The variant value to POST to /api/engine/provision. */
  variant: string;
  /** The button label. */
  label: string;
  /** One honest sentence about what the action does. */
  hint: string;
};

export type BackendSummary = {
  /** True when exactly the installed variant is offered (no action). */
  singleVariant: boolean;
  /** The actionable provisioning offers (never includes the installed one). */
  actions: BackendAction[];
  /** Honest summary line for the current state. */
  summary: string;
};

const VARIANT_LABELS: Record<string, string> = {
  cpu: "CPU",
  vulkan: "Vulkan",
};

const VARIANT_HINTS: Record<string, string> = {
  cpu: "Swap back to the portable CPU-only engine package.",
  vulkan:
    "Download the real Vulkan engine package and swap it in transactionally. GPU offload still requires a Vulkan-capable device — the accelerator surface stays evidence-gated.",
};

export function variantLabel(variant: string): string {
  return VARIANT_LABELS[variant] ?? variant;
}

// engineBackendSummary derives the card's state from the authoritative
// GET /api/engine/provision answer. Rules:
//   - the INSTALLED variant is displayed, never offered as an action;
//   - only SUPPORTED variants are offered (the platform matrix is the
//     backend's truth — the UI never invents capability);
//   - an unknown/empty installed value renders honestly (it cannot
//     happen through the strict API, but the UI stays defensive).
export function engineBackendSummary(
  installed: string,
  supported: string[],
): BackendSummary {
  const clean = (supported ?? []).filter(
    (v) => typeof v === "string" && v.trim() !== "",
  );

  const actions: BackendAction[] = clean
    .filter((v) => v !== installed)
    .map((v) => ({
      variant: v,
      label: `Provision ${variantLabel(v)}`,
      hint: VARIANT_HINTS[v] ?? `Provision the ${variantLabel(v)} engine package.`,
    }));

  const singleVariant = clean.length <= 1;

  let summary: string;
  if (clean.length === 0) {
    summary = "No engine backend variant is provisionable on this platform.";
  } else if (actions.length === 0) {
    summary = singleVariant
      ? `The ${variantLabel(installed)} engine package is installed. This platform offers no other backend variant.`
      : `The ${variantLabel(installed)} engine package is installed and up to date for every offered variant.`;
  } else {
    summary = `The ${variantLabel(installed)} engine package is installed. Swapping is transactional: the engine stops, the package is staged and verified (startup health + runtime backend enumeration), and a failure rolls back to this package.`;
  }

  return { singleVariant, actions, summary };
}

// provisionBusyLabel is the honest progress caption while the updater
// transaction runs (the POST can take minutes — download, staged swap,
// restart, verification).
export function provisionBusyLabel(variant: string): string {
  return `Provisioning the ${variantLabel(variant)} engine — downloading, stopping the engine, staging and verifying the package (a failure rolls back)…`;
}
