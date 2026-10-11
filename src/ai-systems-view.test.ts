import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  describeGoalState,
  goalProgress,
  isGoalLive,
  isGoalTerminal,
  systemsView,
} from "./ai-systems-view.ts";
import type { AISystem, Goal, SystemsPayload } from "./api.ts";

function system(id: string, name: string, revision: number): AISystem {
  return {
    systemId: id,
    name,
    revision,
    instructions: "",
    approvalPolicy: "ask-risky",
    verificationPolicy: "standard",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

describe("ai-systems-view (v1.9.0)", () => {
  it("derives deterministic selector rows with the active flag", () => {
    const payload: SystemsPayload = {
      systems: [
        system("sys-b", "Beta", 2),
        system("default", "Default", 1),
        system("sys-a", "Alpha", 7),
      ],
      activeSystemId: "sys-a",
    };
    const rows = systemsView(payload);
    assert.deepEqual(
      rows.map((r) => r.system.systemId),
      ["default", "Alpha".length ? "sys-a" : "", "sys-b"].map((x) =>
        x === "" ? "sys-a" : x,
      ),
    );
    assert.equal(rows[1].active, true);
    assert.equal(rows[1].label, "Alpha · rev 7");
  });

  it("tolerates malformed payloads defensively", () => {
    assert.deepEqual(systemsView(null), []);
    // @ts-expect-error malformed payload tolerance is the contract
    assert.deepEqual(systemsView({}), []);
  });

  it("derives goal progress from actual plan state (no percentages)", () => {
    const goal = {
      plan: [
        { index: 0, objective: "a", status: "completed" },
        { index: 1, objective: "b", status: "pending" },
        { index: 2, objective: "c", status: "completed" },
      ],
    } as unknown as Goal;
    assert.deepEqual(goalProgress(goal), { done: 2, total: 3 });
    assert.deepEqual(goalProgress({ plan: [] } as unknown as Goal), {
      done: 0,
      total: 0,
    });
  });

  it("classifies goal lifecycle honestly", () => {
    const live = { status: "active" } as Goal;
    const parked = { status: "paused" } as Goal;
    const done = { status: "completed" } as Goal;
    assert.equal(isGoalLive(live), true);
    assert.equal(isGoalLive(parked), false);
    assert.equal(isGoalTerminal(done), true);
    assert.equal(isGoalTerminal(parked), false);
  });

  it("describes goal states without inventing optimism", () => {
    assert.equal(
      describeGoalState({
        status: "waiting_for_approval",
        pendingApproval: { toolName: "shell", approvalId: "x", risk: "destructive" },
      } as unknown as Goal),
      "awaiting approval: shell",
    );
    assert.equal(
      describeGoalState({ status: "active", phase: "acting", currentStep: 2 } as Goal),
      "executing step 2",
    );
    assert.equal(
      describeGoalState({ status: "completed", verificationPassed: false } as Goal),
      "completed without verification",
    );
    assert.equal(
      describeGoalState({ status: "paused" } as Goal),
      "paused at a checkpoint — resumable",
    );
  });
});
