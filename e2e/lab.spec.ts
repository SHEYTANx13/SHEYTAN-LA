import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * lab.spec.ts — REAL TOOL EXECUTION through the production Coding Lab
 * boundary (§6's genuinely-useful-Agent-task requirement, driven at the
 * tool layer the Agent itself uses).
 *
 * The Coding Lab is the production tool path for workspace execution:
 * every step below runs the REAL machinery — workspace creation from a
 * real source tree, real subprocess execution through the platform
 * shell under the lab policy, real output capture, real verification
 * with explicit checks, and real task lifecycle settlement.
 *
 * The commands are per-platform shell commands (cat/type) — the same
 * per-platform reality the production lab runner itself embraces.
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan();
});

test.afterAll(async () => {
  // v1.5.0: a failed beforeAll leaves stack undefined — the afterAll must
  // never mask the PRIMARY startup failure with a secondary teardown error.
  await stack?.stop();
});

function api(pathName: string): string {
  return `${stack.baseURL}${pathName}`;
}

async function labAction(body: Record<string, unknown>): Promise<Record<string, unknown>> {
  const res = await test.step("lab action", async () => {
    const r = await fetch(api("/api/lab"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return { status: r.status, json: (await r.json()) as Record<string, unknown> };
  });
  return res.json;
}

test("inspect source → run a real command → observe result → verify → finish", async () => {
  // A real source tree the task operates on (the "inspect repository"
  // fixture of the acceptance flow).
  const sourceDir = path.join(stack.dataDir, "e2e-source");
  fs.mkdirSync(sourceDir, { recursive: true });
  fs.writeFileSync(
    path.join(sourceDir, "README.md"),
    "# E2E Lab Source\n\nThe real tool path proves: execution, capture, verification.\n",
  );
  fs.writeFileSync(
    path.join(sourceDir, "notes.txt"),
    "SHEYTAN lab e2e: the command executes through the real runner.\n",
  );

  // 1. start_task: the lab creates a REAL workspace from the source.
  const started = (await labAction({
    action: "start_task",
    title: "E2E: prove the real tool path",
    description: "inspect the source, run a command, verify the outcome",
    source: sourceDir,
  })) as {
    ok?: boolean;
    error?: string;
    result?: { task?: { id?: string } };
  };

  expect(started.ok, `start_task failed: ${started.error}`).toBe(true);
  const taskId = started.result?.task?.id ?? "";
  expect(taskId).toBeTruthy();

  // 2. read_file: real content through the production read path.
  const read = (await labAction({
    action: "read_file",
    taskId,
    path: "notes.txt",
  })) as {
    ok?: boolean;
    error?: string;
    result?: { file?: { content?: string } };
  };
  expect(read.ok, `read_file failed: ${read.error}`).toBe(true);
  expect(read.result?.file?.content ?? "").toContain("the real runner");

  // 3. run: a REAL subprocess through the platform shell under the lab
  //    policy and sandbox, with captured output.
  const showCommand =
    process.platform === "win32" ? "type notes.txt" : "cat notes.txt";
  const run = (await labAction({
    action: "run",
    taskId,
    command: showCommand,
  })) as {
    ok?: boolean;
    error?: string;
    result?: { result?: { output?: string; exitCode?: number; success?: boolean } };
  };

  expect(run.ok, `run failed: ${run.error}`).toBe(true);
  expect(run.result?.result?.success).toBe(true);
  expect(run.result?.result?.exitCode).toBe(0);
  expect(run.result?.result?.output ?? "").toContain("the real runner");

  // 4. verify: the objective check runs the SAME real command as its
  //    evidence — a passing verification is earned, not assumed.
  const verified = (await labAction({
    action: "verify",
    taskId,
    checks: [
      { name: "notes present", command: showCommand, required: true },
    ],
  })) as {
    ok?: boolean;
    error?: string;
    result?: { verification?: { passed?: boolean } };
  };

  expect(verified.ok, `verify failed: ${verified.error}`).toBe(true);
  expect(verified.result?.verification?.passed).toBe(true);

  // 5. promote: the verified workspace content is mirrored back to the
  //    source tree through the REAL promotion path (the earn-it gate
  //    for finish).
  const promoted = (await labAction({
    action: "promote",
    taskId,
  })) as { ok?: boolean; error?: string };
  expect(promoted.ok, `promote failed: ${promoted.error}`).toBe(true);

  // 6. finish: only allowed after verified + promoted — the lifecycle
  //    gate is real.
  const finished = (await labAction({
    action: "finish",
    taskId,
  })) as { ok?: boolean; error?: string };
  expect(finished.ok, `finish failed: ${finished.error}`).toBe(true);
});

test("an unverified finish is refused (the lifecycle gate is real)", async () => {
  const sourceDir = path.join(stack.dataDir, "e2e-source-unverified");
  fs.mkdirSync(sourceDir, { recursive: true });
  fs.writeFileSync(path.join(sourceDir, "placeholder.txt"), "no verification yet\n");

  const started = (await labAction({
    action: "start_task",
    title: "E2E: finish must require verification",
    source: sourceDir,
  })) as { ok?: boolean; result?: { task?: { id?: string } }; error?: string };
  expect(started.ok).toBe(true);
  const taskId = started.result?.task?.id ?? "";

  const finished = (await labAction({
    action: "finish",
    taskId,
  })) as { ok?: boolean; error?: string };

  // The REAL gate: without verification the finish is rejected with the
  // production error, never a fake success.
  expect(finished.ok).toBe(false);
  expect(finished.error ?? "").toMatch(/verif|promot/i);
});
