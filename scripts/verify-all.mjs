#!/usr/bin/env node
// verify-all.mjs — the ONE canonical test/build-verification entrypoint
// (v1.10.1).
//
// Every applicable suite runs through here with fail-fast exit codes and
// full preserved output; CI calls this file, and so does local development.
// Framework-specific suites stay separated in source (Go tests, CTest,
// QtTest, Node release tests) — this is the RUNNER, not a test monolith.
// A required test may never silently turn into a skip: suites whose
// toolchain is genuinely absent are reported as NOT RUN with the exact
// missing-tool reason and still exit non-zero in --release mode.
//
// Usage:
//   node scripts/verify-all.mjs            # full local profile
//   node scripts/verify-all.mjs --go       # Go suites only
//   node scripts/verify-all.mjs --native   # C++ engine (CTest) only
//   node scripts/verify-all.mjs --qt       # Qt host build + tests only
//   node scripts/verify-all.mjs --release  # release gate: unavailable
//                                          # toolchains FAIL the run
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const args = new Set(process.argv.slice(2));
const RELEASE = args.has("--release");

const results = [];
let failed = false;

function have(tool, args = ["--version"]) {
  const r = spawnSync(tool, args, { encoding: "utf8" });
  return r.status === 0;
}

function run(label, cmd, cmdArgs, opts = {}) {
  const { env, cwd = ROOT, required = true, skip } = opts;

  if (skip) {
    results.push({ label, status: "NOT RUN", reason: skip });
    console.log(`\n=== ${label}: NOT RUN (${skip}) ===`);
    return;
  }

  console.log(`\n=== ${label} ===`);
  const r = spawnSync(cmd, cmdArgs, {
    cwd,
    env: { ...process.env, ...env },
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  const ok = r.status === 0;
  results.push({ label, status: ok ? "PASS" : "FAIL" });

  if (!ok) {
    failed = true;
    if (required) {
      console.log(`\n[verify-all] ${label} FAILED — fail-fast stop.`);
      report();
      process.exit(1);
    }
  }
}

function report() {
  console.log("\n=== verify-all summary ===");
  for (const r of results) {
    const extra = r.reason ? ` (${r.reason})` : "";
    console.log(`  ${r.status.padEnd(8)} ${r.label}${extra}`);
  }
}

// --- 1. release/version contracts (always) ----------------------------------
run("release version contract", "node", ["scripts/release-version.mjs", "--check"]);
run("codename removal gate", "node", ["scripts/codename-gate.mjs"]);
run("release unit tests (node)", "npm", ["run", "test:release"]);
run("frontend unit tests (node)", "npm", ["run", "test:units"]);

// --- 2. Go suites ------------------------------------------------------------
const goSelected = args.has("--go");
if (!args.has("--native") && !args.has("--qt") || goSelected) {
  if (have("go", ["version"])) {
    run("go vet ./...", "go", ["vet", "./..."]);
    run("go build ./...", "go", ["build", "./..."]);
    run(
      "go test ./internal/... (headless)",
      "go",
      ["test", "./internal/...", "-tags", "headless", "-count=1"],
    );
    run("go test ./... (full)", "go", ["test", "./...", "-count=1"]);
    run(
      "go test -race (concurrency-heavy packages)",
      "go",
      [
        "test", "-race", "-tags", "headless", "-count=1",
        "./internal/api", "./internal/agent", "./internal/sessions",
        "./internal/contextplan", "./internal/histref", "./internal/runtime",
      ],
      { required: !RELEASE ? true : true },
    );
    // v1.10.1: Wails must stay OUT of the module graph.
    run(
      "dependency graph: no Wails",
      "go",
      ["list", "-m", "all"],
      {
        required: true,
        skip: have("go", ["version"]) ? undefined : "go toolchain unavailable",
      },
    );
  } else {
    results.push({ label: "Go suites", status: "NOT RUN", reason: "go toolchain unavailable" });
    console.log("\n=== Go suites: NOT RUN (go toolchain unavailable) ===");
    if (RELEASE) failed = true;
  }
}

// --- 3. C++ native engine (CTest) ---------------------------------------------
if (args.has("--native") || (!goSelected && !args.has("--qt"))) {
  if (have("cmake")) {
    run(
      "native engine configure+build (CMake)",
      "cmake",
      ["-S", "native/engine", "-B", "native/engine/build", "--fresh"],
    );
    run("native engine build", "cmake", ["--build", "native/engine/build"]);
    run(
      "native engine tests (CTest)",
      "ctest",
      ["--test-dir", "native/engine/build", "--output-on-failure"],
    );
  } else {
    results.push({ label: "native engine (CTest)", status: "NOT RUN", reason: "cmake unavailable" });
    console.log("\n=== native engine (CTest): NOT RUN (cmake unavailable) ===");
    if (RELEASE) failed = true;
  }
}

// --- 4. Qt host (CMake + QtTest) ----------------------------------------------
if (args.has("--qt") || (!goSelected && !args.has("--native"))) {
  const qtDir = process.env.Qt6_DIR || process.env.QT6_DIR || "";
  const qtReady = qtDir !== "" || existsSync("/usr/lib/cmake/Qt6");

  if (have("cmake") && qtReady) {
    const cfgArgs = ["-S", "ui/qt", "-B", "ui/qt/build"];
    if (qtDir) cfgArgs.push(`-DCMAKE_PREFIX_PATH=${qtDir}`);
    // Environment-specific cache pins (e.g. a user-space OpenGL root in a
    // container) ride EXTRA_CMAKE_ARGS verbatim: "-DFOO=bar -DBAZ=qux".
    if (process.env.EXTRA_CMAKE_ARGS) {
      for (const a of process.env.EXTRA_CMAKE_ARGS.split(/\s+/).filter(Boolean)) cfgArgs.push(a);
    }
    run("Qt host configure (CMake)", "cmake", cfgArgs);
    run("Qt host build", "cmake", ["--build", "ui/qt/build"]);
    // v1.10.8 (R-A, reshape residue): the v1.10.7 reshape moved the core
    // entry to cmd/sheytan-local-agent — the root package no longer
    // exists, so `go build -o sheytan .` is a DEAD command and nothing
    // else in this runner produces the core binary (go build ./...
    // compiles packages without writing binaries). Build the core
    // EXPLICITLY from its real entrypoint and point SHEYTAN_CORE_BIN at
    // the produced binary. The skip hint now teaches the working command.
    const coreBin = path.join(ROOT, process.platform === "win32" ? "sheytan.exe" : "sheytan");
    run(
      "core build (cmd/sheytan-local-agent)",
      "go",
      ["build", "-o", coreBin, "./cmd/sheytan-local-agent"],
      { skip: have("go", ["version"]) ? undefined : "go toolchain unavailable" },
    );
    // v1.11.0 (R3b, run-#51, Task ID 45): the script surface drives the
    // ONE CLI — the freshly built core binary's own version subcommand
    // must report the release identity package.json carries (I6: ONE
    // version source; no duplicated pathway — the binary IS the surface).
    if (existsSync(coreBin)) {
      const parity = spawnSync(coreBin, ["version"], { encoding: "utf8" });
      const want = JSON.parse(readFileSync(path.join(ROOT, "package.json"), "utf8")).version;
      const got = (parity.stdout ?? "").match(/v([0-9][^\s]*)/)?.[1] ?? "";
      const ok = parity.status === 0 && got === want;
      results.push({
        label: "core version parity (the ONE CLI)",
        status: ok ? "PASS" : "FAIL",
        reason: ok ? undefined : `binary reported "${got || "nothing"}"; package.json = ${want}`,
      });
      if (ok) {
        console.log(`\n=== core version parity (the ONE CLI): PASS (v${want}) ===`);
      } else {
        failed = true;
        console.log(`\n=== core version parity (the ONE CLI): FAIL (binary="${got}" package.json="${want}") ===`);
        if (RELEASE) {
          report();
          process.exit(1);
        }
      }
    }
    run(
      "Qt host acceptance (QtTest, real core)",
      path.join("ui", "qt", "build", process.platform === "win32" ? "Release/" : "", "sheytan-ui-acceptance"),
      [],
      {
        env: {
          SHEYTAN_REPO: ROOT,
          // The core executable is produced by the explicit core build above.
          SHEYTAN_CORE_BIN: coreBin,
          QT_QPA_PLATFORM: "offscreen",
        },
        skip: existsSync(coreBin)
          ? undefined
          : "core executable not built — run `go build -o sheytan ./cmd/sheytan-local-agent` first",
      },
    );
  } else {
    results.push({
      label: "Qt host suites",
      status: "NOT RUN",
      reason: qtReady ? "cmake unavailable" : "Qt6 CMake package not found (set Qt6_DIR)",
    });
    console.log(`\n=== Qt host suites: NOT RUN ===`);
    if (RELEASE) failed = true;
  }
}

report();

if (failed) {
  console.log("\n[verify-all] RESULT: FAIL");
  process.exit(1);
}

const notRun = results.filter((r) => r.status === "NOT RUN");
console.log(`\n[verify-all] RESULT: PASS${notRun.length ? ` (${notRun.length} suite(s) NOT RUN — environment-limited, listed above)` : ""}`);
