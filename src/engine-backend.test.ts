// engine-backend.test.ts — v1.6.2 unit coverage for the Settings
// engine-backend card's state derivation (the UI added in v1.6.2; the
// Go API tests cover the transaction itself).
import { test } from "node:test";
import {
  engineBackendSummary,
  provisionBusyLabel,
  variantLabel,
} from "./engine-backend.ts";

test("windows x64: cpu installed → vulkan offered", () => {
  const s = engineBackendSummary("cpu", ["cpu", "vulkan"]);
  if (s.actions.length !== 1) {
    throw new Error(`expected 1 action, got ${s.actions.length}`);
  }
  if (s.actions[0].variant !== "vulkan") {
    throw new Error(`expected vulkan action, got ${s.actions[0].variant}`);
  }
  if (s.actions[0].label !== "Provision Vulkan") {
    throw new Error(`unexpected label: ${s.actions[0].label}`);
  }
  if (!s.summary.includes("CPU")) {
    throw new Error(`summary must name the installed variant: ${s.summary}`);
  }
  if (!s.summary.includes("rolls back")) {
    throw new Error(`summary must state the rollback guarantee: ${s.summary}`);
  }
});

test("vulkan installed → cpu offered (the way back)", () => {
  const s = engineBackendSummary("vulkan", ["cpu", "vulkan"]);
  if (s.actions.length !== 1 || s.actions[0].variant !== "cpu") {
    throw new Error(`expected the cpu action, got ${JSON.stringify(s.actions)}`);
  }
  if (!s.actions[0].hint.includes("CPU-only")) {
    throw new Error(`cpu hint must describe the package: ${s.actions[0].hint}`);
  }
});

test("single-variant platform (win/arm64, linux, darwin): no actions offered", () => {
  const s = engineBackendSummary("cpu", ["cpu"]);
  if (s.actions.length !== 0) {
    throw new Error(`no action may be offered on a single-variant platform`);
  }
  if (!s.singleVariant) {
    throw new Error("singleVariant must be true");
  }
  if (!s.summary.includes("no other backend variant")) {
    throw new Error(`honest single-variant summary expected: ${s.summary}`);
  }
});

test("empty support list renders honestly", () => {
  const s = engineBackendSummary("cpu", []);
  if (s.actions.length !== 0) {
    throw new Error("no actions without support");
  }
  if (!s.summary.includes("No engine backend variant")) {
    throw new Error(`unexpected summary: ${s.summary}`);
  }
});

test("the installed variant is never offered as an action", () => {
  for (const installed of ["cpu", "vulkan"]) {
    const s = engineBackendSummary(installed, ["cpu", "vulkan"]);
    for (const a of s.actions) {
      if (a.variant === installed) {
        throw new Error(`installed variant ${installed} offered as action`);
      }
    }
  }
});

test("vulkan hint states the evidence-gated GPU truth", () => {
  const s = engineBackendSummary("cpu", ["cpu", "vulkan"]);
  const vulkan = s.actions.find((a) => a.variant === "vulkan");
  if (!vulkan) throw new Error("vulkan action missing");
  if (!vulkan.hint.includes("evidence-gated")) {
    throw new Error(`the hint must keep GPU claims evidence-gated: ${vulkan.hint}`);
  }
});

test("labels and busy caption", () => {
  if (variantLabel("cpu") !== "CPU" || variantLabel("vulkan") !== "Vulkan") {
    throw new Error("variant labels");
  }
  if (variantLabel("banana") !== "banana") {
    throw new Error("unknown variants render verbatim (defensive)");
  }
  const busy = provisionBusyLabel("vulkan");
  if (!busy.includes("Vulkan") || !busy.includes("rolls back")) {
    throw new Error(`busy caption must be honest: ${busy}`);
  }
});
