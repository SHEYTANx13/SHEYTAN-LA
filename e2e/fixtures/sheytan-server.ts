import { spawn, type ChildProcess } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { request } from "@playwright/test";

/**
 * sheytan-server.ts — the REAL application stack for browser E2E.
 *
 * Everything is required and fails loudly when missing (required-gate
 * semantics: a missing dependency must NEVER become a silent pass):
 *
 *   e2e/.build/sheytan-e2e-server        — go build -tags headless of this tree
 *   web/static/index.html                — npm run build (the embedded frontend)
 *   native/engine/build/shtn-engine-host — the real C++ engine (CMake)
 *   python3 + e2e/make-e2e-model.py      — the wide-context fixture generator
 *
 * The server runs against an ISOLATED per-run data directory (a temp
 * dir under e2e/.artifacts), configured with engineBackend=native and
 * the fixture model — the same engine selection a user makes in
 * Settings, so generation is REAL (real logits, real streaming, real
 * persistence), not a mocked SSE stream.
 */

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = path.resolve(HERE, "..", "..");
export const SERVER_BIN = path.join(REPO_ROOT, "e2e", ".build", "sheytan-e2e-server");
export const ENGINE_BIN = (() => {
  const name = process.platform === "win32" ? "shtn-engine-host.exe" : "shtn-engine-host";
  return path.join(REPO_ROOT, "native", "engine", "build", name);
})();

export interface SheytanStack {
  baseURL: string;
  dataDir: string;
  pid: number;
  child?: ChildProcess;
  log(): string;
  stop(): Promise<void>;
}

function requireFile(label: string, file: string): void {
  if (!fs.existsSync(file)) {
    throw new Error(
      `[e2e] missing required ${label}: ${file}\n` +
        (label === "server binary"
          ? "  build it first:  npm run build:e2e\n"
          : label === "embedded frontend"
            ? "  build it first:  npm run build\n"
            : "  build it first:  cmake -S native/engine -B native/engine/build && cmake --build native/engine/build\n") +
        "  The browser suite proves the REAL stack; it never runs green without it.",
    );
  }
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      if (addr && typeof addr === "object") {
        const port = addr.port;
        srv.close(() => resolve(port));
        return;
      }
      reject(new Error("no free port"));
    });
  });
}

async function waitForHealth(baseURL: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr = "";

  while (Date.now() < deadline) {
    try {
      const ctx = await request.newContext({ baseURL });
      const res = await ctx.get("/api/health");
      const body = (await res.json()) as { overall?: string };
      await ctx.dispose();
      if (res.ok() && body.overall === "ok") {
        return;
      }
      lastErr = `health=${res.status()} overall=${body.overall}`;
    } catch (err) {
      lastErr = String(err);
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  throw new Error(`[e2e] the real server never became healthy: ${lastErr}`);
}

export async function startSheytan(options: SheytanOptions = {}): Promise<SheytanStack> {
  requireFile("server binary", SERVER_BIN);
  requireFile("embedded frontend", path.join(REPO_ROOT, "web", "static", "index.html"));
  requireFile("native engine host", ENGINE_BIN);

  const artifacts = path.join(REPO_ROOT, "e2e", ".artifacts");
  fs.mkdirSync(artifacts, { recursive: true });

  const dataDir = fs.mkdtempSync(path.join(artifacts, "stack-"));
  const modelsDir = path.join(dataDir, "models");
  const binDir = path.join(dataDir, "bin");
  fs.mkdirSync(modelsDir, { recursive: true });
  fs.mkdirSync(binDir, { recursive: true });

  // v1.5.0 (run 36083843376): PARTIAL-STARTUP RESOURCE OWNERSHIP. Every
  // resource created from here on is tracked and torn down by this fixture
  // itself when a LATER step fails, so a failed startSheytan() never leaks
  // processes and never surfaces a SECONDARY teardown error — the primary
  // startup failure stays the failure the suite reports.
  let child: ChildProcess | undefined;
  const cleanupCreated = (): void => {
    if (child) {
      stopChild(child);
      child = undefined;
    }
  };

  try {
    return await startSheytanStack({
      dataDir,
      modelsDir,
      binDir,
      noModel: options.noModel,
      model: options.model,
      modelGenerator: options.modelGenerator,
      maxTokens: options.maxTokens,
      onSpawn: (c) => (child = c),
    });
  } catch (err) {
    // Startup failed: clean every resource that was actually created,
    // then re-throw the PRIMARY failure untouched.
    cleanupCreated();
    throw err;
  }
}

interface StartOptions {
  dataDir: string;
  modelsDir: string;
  binDir: string;
  onSpawn: (child: ChildProcess) => void;
  /** When true the generated config carries model:"" — the model-first flow (the app must show the selector and must NOT prewarm an arbitrary GGUF). */
  noModel?: boolean;
  /** v1.8.2: model filename + generator override (see SheytanOptions). */
  model?: string;
  modelGenerator?: string;
  maxTokens?: number;
}

export interface SheytanOptions {
  /** Start with NO selected model: config.model = "" (the fresh-install model-first flow). */
  noModel?: boolean;

  /**
   * v1.8.2: filename of the model to select inside modelsDir (default
   * "e2e-wide.gguf"). The file must have been generated into modelsDir by
   * the caller via modelGenerator (below) before the server boots.
   */
  model?: string;

  /**
   * v1.8.2: python script (under e2e/) that GENERATES the requested model
   * into modelsDir. Default: make-e2e-model.py (the standard fixture).
   */
  modelGenerator?: string;

  /**
   * v1.8.2: override for config llm.maxTokens (the live-visibility suite
   * bounds the fixture generation so the live window is deterministic).
   */
  maxTokens?: number;
}

async function startSheytanStack(opts: StartOptions): Promise<SheytanStack> {
  const { dataDir, modelsDir, binDir, onSpawn } = opts;

  // The wide-context fixture model (real llama graph, ctx=4096).
  //
  // v1.5.0: the generator's stdout/stderr is CAPTURED and attached to
  // the failure when generation fails — the previous single-line
  // "(python3 required)" message masked the real cause (on the
  // ubuntu-24.04 runner it was a missing numpy for the externally
  // managed system python3, not a missing python3). A diagnostic must
  // be collected, never guessed.
  const modelFile = opts.model ?? "e2e-wide.gguf";
  const modelPath = path.join(modelsDir, modelFile);
  const gen = spawn(process.platform === "win32" ? "python" : "python3", [
    path.join(REPO_ROOT, "e2e", opts.modelGenerator ?? "make-e2e-model.py"),
    modelPath,
  ]);
  const genLog: string[] = [];
  gen.stdout?.on("data", (d) => genLog.push(String(d)));
  gen.stderr?.on("data", (d) => genLog.push(String(d)));
  const genOutcome = await new Promise<{ code: number; spawnError?: Error }>((resolve) => {
    gen.on("error", (err) => resolve({ code: -1, spawnError: err }));
    gen.on("exit", (code) => resolve({ code: code ?? -1 }));
  });
  if (genOutcome.code !== 0 || !fs.existsSync(modelPath)) {
    const detail = genLog.join("").trim();
    const cause = genOutcome.spawnError
      ? `python could not be spawned: ${genOutcome.spawnError.message}`
      : genOutcome.code !== 0
        ? `generator exited with code ${genOutcome.code}`
        : "generator exited successfully but wrote no model file";
    throw new Error(
      "[e2e] the fixture model could not be generated.\n" +
        `  cause: ${cause}\n` +
        `  generator output:\n${detail || "    (no output)"}\n` +
        "  the generator needs python3 with numpy (CI provisions both " +
        "via actions/setup-python@v7 + pip install numpy)",
    );
  }

  fs.copyFileSync(ENGINE_BIN, path.join(binDir, path.basename(ENGINE_BIN)));

  const port = await freePort();

  fs.writeFileSync(
    path.join(dataDir, "config.json"),
    JSON.stringify(
      {
        dataDir,
        modelsDir,
        sessionsDir: path.join(dataDir, "sessions"),
        host: "127.0.0.1",
        port,
        provider: "local",
        // v1.5.0: model-first startup — the fixture MAY boot with no
        // selected model (the fresh-install flow). The server must then
        // reach "model selection required" WITHOUT loading an arbitrary
        // first GGUF; the selector flow picks one explicitly.
        // v1.8.2: the model filename and llm.maxTokens are overridable
        // for the live-visibility suite.
        model: opts.noModel ? "" : (opts.model ?? "e2e-wide.gguf"),
        engineBackend: "native",
        nativeEnginePath: path.join(binDir, path.basename(ENGINE_BIN)),
        llamaHost: "127.0.0.1",
        llamaPort: port + 1,
        llamaAutoStart: true,
        // v1.8.2: the live-visibility suite bounds the fixture generation
        // so the live window is deterministic.
        ...(opts.maxTokens !== undefined
          ? { llm: { maxTokens: opts.maxTokens } }
          : {}),
      },
      null,
      2,
    ),
  );

  const child: ChildProcess = spawn(SERVER_BIN, ["serve", "--port", String(port), "--no-browser"], {
    env: { ...process.env, SHEYTAN_DATA_DIR: dataDir },
    stdio: ["ignore", "pipe", "pipe"],
    // Detached process group so the WHOLE tree (server + engine host)
    // can be terminated together.
    detached: process.platform !== "win32",
  });
  onSpawn(child);

  const serverLog: string[] = [];
  child.stdout?.on("data", (d) => serverLog.push(String(d)));
  child.stderr?.on("data", (d) => serverLog.push(String(d)));

  const exited = new Promise<never>((_, reject) => {
    child.on("exit", (code) =>
      reject(
        new Error(
          `[e2e] the real server exited during startup (code ${code}):\n${serverLog.join("").slice(-4000)}`,
        ),
      ),
    );
  });

  const baseURL = `http://127.0.0.1:${port}`;

  try {
    await Promise.race([waitForHealth(baseURL, 90_000), exited]);
  } catch (err) {
    stopChild(child);
    throw err;
  }

  return {
    baseURL,
    dataDir,
    pid: child.pid ?? 0,
    child,
    log: () => serverLog.join(""),
    stop: async () => {
      // v1.5.0: STATE-BASED teardown, not an arbitrary sleep — wait for
      // the server process to actually exit (bounded) so the isolated
      // data dir is consistent and no engine host leaks past the suite.
      stopChild(child);
      await waitForExit(child, 15_000);
    },
  };
}

// waitForExit resolves when the child has exited (state, not a timer).
// A bounded timeout keeps a wedged child from hanging the suite; the
// kill inside stopChild is the backstop.
function waitForExit(child: ChildProcess, timeoutMs: number): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const timer = setTimeout(done, timeoutMs);
    function done(): void {
      child.removeListener("exit", done);
      clearTimeout(timer);
      resolve();
    }
    child.on("exit", done);
  });
}

function stopChild(child: ChildProcess): void {
  if (child.exitCode !== null) {
    return;
  }
  try {
    if (child.pid && process.platform !== "win32") {
      // The server owns its engine subprocesses and tears them down on
      // SIGTERM (Close(): native first, then llama.cpp) — a graceful
      // stop keeps the isolated data dir consistent.
      process.kill(-child.pid, "SIGTERM");
    } else {
      child.kill("SIGTERM");
    }
  } catch {
    try {
      child.kill("SIGKILL");
    } catch {
      /* already gone */
    }
  }
}
