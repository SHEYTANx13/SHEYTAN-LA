// ai-systems-view.ts — v1.9.0 pure view-model helpers for the AI System
// selector and the Goal state surfaces. No I/O, no store access: these
// are deterministic functions over backend payloads, unit-tested in
// ai-systems-view.test.ts.

import type { AISystem, Goal, SystemsPayload } from "./api";

export interface SystemRow {
  system: AISystem;
  active: boolean;
  label: string;
}

// systemsView derives the selector rows: deterministic order (default
// system first, then name), active flag, and a stable label carrying the
// revision (the revision is the identity users must see).
export function systemsView(payload: SystemsPayload | null): SystemRow[] {
  if (!payload || !Array.isArray(payload.systems)) {
    return [];
  }
  const activeId = payload.activeSystemId;
  const rows = payload.systems.map((system) => ({
    system,
    active: system.systemId === activeId,
    label: `${system.name} · rev ${system.revision}`,
  }));
  rows.sort((a, b) => {
    const aDef = a.system.systemId === "default" ? 0 : 1;
    const bDef = b.system.systemId === "default" ? 0 : 1;
    if (aDef !== bDef) return aDef - bDef;
    return a.system.name.localeCompare(b.system.name);
  });
  return rows;
}

export const GOAL_ACTIVE_STATUSES = [
  "active",
  "waiting_for_approval",
  "waiting_for_resource",
] as const;

export function isGoalLive(goal: Goal): boolean {
  return (GOAL_ACTIVE_STATUSES as readonly string[]).includes(goal.status);
}

export function isGoalTerminal(goal: Goal): boolean {
  return ["completed", "failed", "cancelled"].includes(goal.status);
}

// goalProgress derives progress from ACTUAL plan state — never a
// decorative percentage. A goal with no plan reports 0/0 (unknown by
// design, not 0%).
export function goalProgress(goal: Goal): { done: number; total: number } {
  const plan = Array.isArray(goal.plan) ? goal.plan : [];
  return {
    done: plan.filter((step) => step.status === "completed").length,
    total: plan.length,
  };
}

// describeGoalState renders the one honest status line: what is being
// done / what is needed. Evidence-derived, no invented optimism.
export function describeGoalState(goal: Goal): string {
  switch (goal.status) {
    case "active":
      return goal.phase === "acting" && goal.currentStep >= 0
        ? `executing step ${goal.currentStep}`
        : goal.phase;
    case "waiting_for_approval":
      return `awaiting approval: ${goal.pendingApproval?.toolName ?? "unknown call"}`;
    case "waiting_for_resource":
      return "waiting for resources";
    case "paused":
      return "paused at a checkpoint — resumable";
    case "blocked":
      return goal.lastAction || "blocked";
    case "failed":
      return goal.verification || "failed";
    case "completed":
      return goal.verificationPassed
        ? `verified: ${goal.verification || "objectively met"}`
        : "completed without verification";
    case "cancelled":
      return "cancelled";
    default:
      return goal.status;
  }
}
