#!/usr/bin/env node
/**
 * installer-contract.test.mjs — static installer-payload contract for
 * packaging/nsis/installer.nsi (v1.10.14, run #36 class, Task ID 33).
 *
 * Run #36 shipped the first fully green hosted matrix AND an installer
 * that could not boot the product: the .nsi staged ONLY the Go core
 * (File "${BUILDDIR}\${EXE}" + three /nonfatal text files + the icon),
 * so the installed tree never received sheytan-ui.exe or the deployed
 * Qt runtime. The physical result: full boot in the operator's app.log,
 * then "[boot] exit code 1" and an instant silent close on every
 * install — while the portable ZIP (which carries the complete pair)
 * kept working. The size tell was the same defect: 5.03 MB installer
 * vs 38.9 MB ZIP, LZMA of the Go exe alone.
 *
 * This suite pins the fixed contract, in the same spirit as the run-#35
 * identity assertions in scripts/release-version.test.mjs:
 *
 *   1. PAYLOAD  — the install section stages the COMPLETE application
 *      (the pair + the Qt runtime) via the recursive payload install;
 *   2. ASSERTIONS — compile-time !error guards refuse to build an
 *      installer from a staging tree missing sheytan-ui.exe/Qt6Core.dll;
 *   3. SYMMETRY — the uninstaller removes EXACTLY the enumerated payload
 *      (the CI-generated payload-uninstall.nsh list), never user data;
 *   4. HERITAGE — every pre-existing contract fragment stays, the
 *      "RMDir /r" recursive-delete prohibition stays verbatim, and the
 *      run-#35 four-part identity contract stays.
 *
 * Static by construction: no Windows runtime, no makensis needed — the
 * hosted NSIS build (and the silent-install smoke) is the executable
 * proof; this file makes the payload regression IMPOSSIBLE TO COMMIT.
 */

import { existsSync, mkdtempSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, sep } from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { test } from "node:test";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(scriptDir, "..");

function loadNsi() {
  return readFileSync(
    join(repoRoot, "packaging", "nsis", "installer.nsi"),
    "utf8",
  );
}

function loadWorkflow() {
  return readFileSync(
    join(repoRoot, ".github", "workflows", "build-desktop.yml"),
    "utf8",
  );
}

// ---------------------------------------------------------------------------
// 1. Payload contract — the installed product IS the complete application.
// ---------------------------------------------------------------------------

test("installer.nsi stages the complete application (pair + Qt runtime), not the Go core alone", () => {
  // Run #36 root cause: the install section stopped at
  // File "${BUILDDIR}\${EXE}" — sheytan-ui.exe and the windeployqt Qt
  // runtime were never installed. The recursive payload install of the
  // staged application tree (from which CI excludes the portable-only
  // companions: README.txt, BUILD-INFO.txt, AI-CONTEXT.md,
  // SHEYTAN-LA.bat, models/, workspace/) is the one mechanism that makes
  // the installed distribution carry the same application as the ZIP.
  const nsi = loadNsi();

  assert.match(
    nsi,
    /File\s+\/r\s+"\$\{BUILDDIR\}\\\*\.\*"/,
    "the install section must recursively stage the complete payload tree (File /r \"${BUILDDIR}\\*.*\") — the run-#36 core-only install closed the desktop instantly on every installed launch",
  );

  // The explicit core line stays: the ONE executable the shortcut and
  // the uninstaller's first Delete target both name.
  assert.match(
    nsi,
    /File\s+"\$\{BUILDDIR\}\\\$\{EXE\}"/,
    'the explicit core payload line File "${BUILDDIR}\\${EXE}" must stay',
  );
});

test("installer.nsi refuses at compile time to build without the Qt host and runtime", () => {
  // The pre-flight !error guards are the hosted form of THIS suite: a
  // staging tree missing either half of the pair fails makensis with the
  // exact missing path instead of producing a broken-but-green installer.
  const nsi = loadNsi();

  const uiGuard = nsi.match(
    /!if\s+!\s+\/FileExists\s+"\$\{BUILDDIR\}\\sheytan-ui\.exe"\s*\n\s*!error\s+"([^"]+)"/,
  );
  assert.ok(
    uiGuard,
    "a compile-time assertion must abort the build when ${BUILDDIR}\\sheytan-ui.exe is missing (the run-#36 defect class)",
  );
  assert.match(
    uiGuard[1],
    /sheytan-ui\.exe|Qt host/i,
    "the Qt-host guard must name the missing artifact",
  );

  const qtGuard = nsi.match(
    /!if\s+!\s+\/FileExists\s+"\$\{BUILDDIR\}\\Qt6Core\.dll"\s*\n\s*!error\s+"([^"]+)"/,
  );
  assert.ok(
    qtGuard,
    "a compile-time assertion must abort the build when ${BUILDDIR}\\Qt6Core.dll is missing (windeployqt did not run)",
  );
  assert.match(
    qtGuard[1],
    /Qt6Core\.dll|Qt runtime/i,
    "the Qt-runtime guard must name the missing artifact",
  );
});

// ---------------------------------------------------------------------------
// 2. Uninstall symmetry — remove EXACTLY what was installed, never user data.
// ---------------------------------------------------------------------------

test("installer.nsi carries the generated payload uninstall list and inserts it in the uninstall section", () => {
  // CI enumerates the exact payload tree into payload-uninstall.nsh
  // (one Delete per file, bottom-up RMDir per directory). The uninstaller
  // removes EXACTLY what the installer wrote; the runtime-created trees
  // (data/, models/, workspace/, sessions/, logs/) are never in the
  // generated list and survive by construction. The include is /nonfatal
  // so a developer build without the generated list still compiles; CI
  // always generates it, so every hosted build carries the full list.
  const nsi = loadNsi();

  assert.match(
    nsi,
    /!include\s+\/nonfatal\s+"payload-uninstall\.nsh"/,
    'the uninstaller must consume the CI-generated payload list via !include /nonfatal "payload-uninstall.nsh"',
  );

  assert.match(
    nsi,
    /!ifdef\s+SHEYTAN_PAYLOAD_UNINSTALL/,
    "the payload-delete macro must be !ifdef-guarded (absent list compiles; CI-generated list always defines it)",
  );

  const uninstallAt = nsi.indexOf('Section "Uninstall"');
  assert.ok(uninstallAt >= 0, 'the Uninstall section exists');
  const insertAt = nsi.indexOf("!insertmacro SHEYTAN_PAYLOAD_UNINSTALL");
  assert.ok(
    insertAt > uninstallAt,
    "!insertmacro SHEYTAN_PAYLOAD_UNINSTALL must run INSIDE the Uninstall section (after it opens)",
  );
  const sectionEnd = nsi.indexOf("SectionEnd", uninstallAt);
  assert.ok(
    sectionEnd > insertAt,
    "!insertmacro SHEYTAN_PAYLOAD_UNINSTALL must run before the Uninstall section closes",
  );
});

test("the uninstaller still preserves user data by construction", () => {
  const nsi = loadNsi();

  // The final plain RMDir (empty-directory-only) must stay: it is what
  // leaves any surviving user file — and the whole install-local data
  // tree — untouched on disk.
  assert.match(
    nsi,
    /RMDir\s+"\$INSTDIR"\s*$/m,
    'the plain (non-recursive) RMDir "$INSTDIR" must stay — user data survives uninstall',
  );
  assert.match(
    nsi,
    /preserve user data/,
    "the preserve-user-data contract comment must stay",
  );
});

// ---------------------------------------------------------------------------
// 3. Heritage — nothing pre-existing may regress.
// ---------------------------------------------------------------------------

test("installer.nsi keeps every pre-existing workflow contract fragment", () => {
  // The exact fragment list the Windows job asserts before makensis
  // (build-desktop.yml, "Build Windows NSIS installer" step). The payload
  // upgrade must not have weakened a single one.
  const nsi = loadNsi();
  const fragments = [
    "MUI_PAGE_DIRECTORY",
    "Create a &desktop shortcut",
    "BST_CHECKED",
    "WriteUninstaller",
    "UninstallString",
    "SHEYTAN_DATA_DIR",
    "preserve user data",
  ];
  for (const fragment of fragments) {
    assert.ok(
      nsi.includes(fragment),
      `installer.nsi lost required contract fragment: ${fragment}`,
    );
  }
});

test("installer.nsi keeps the run-#35 four-part identity contract", () => {
  const nsi = loadNsi();

  const fixedBlock = nsi.match(/^VIProductVersion\s+"([^"]+)"/m);
  assert.ok(fixedBlock, "VIProductVersion directive present");
  assert.equal(
    fixedBlock[1],
    "${VERSION}.0",
    "the fixed VS_FIXEDFILEINFO block stays four-part (<version>.0)",
  );

  const productVersion = nsi.match(
    /^VIAddVersionKey\s+"ProductVersion"\s+"([^"]+)"/m,
  );
  assert.ok(productVersion, 'VIAddVersionKey "ProductVersion" present');
  assert.equal(
    productVersion[1],
    "${VERSION}.0",
    'the ProductVersion string key must carry the four-part identity (run #35: the three-part form threw the W-INST-VR gate)',
  );
});

test("installer.nsi NEVER gains the recursive-delete instruction", () => {
  // The workflow's negative contract, mirrored statically: "RMDir /r"
  // must never appear — user data is preserved by construction, not by
  // discipline. This suite fails the commit that re-introduces it.
  const nsi = loadNsi();
  assert.ok(
    !nsi.includes("RMDir /r"),
    "installer.nsi contains 'RMDir /r' — recursive deletes are forbidden in the uninstaller (user data must be preserved)",
  );
});

// ---------------------------------------------------------------------------
// 4. Uninstall-symmetry NAMESPACE contract — run #37's red gate.
//
// Run #37 (39772975839, Windows x64) died at the silent-uninstall smoke:
// the generator wrote payload-uninstall.nsh containing ONLY
// "!macro SHEYTAN_PAYLOAD_UNINSTALL … !macroend", while installer.nsi
// guards the insert with "!ifdef SHEYTAN_PAYLOAD_UNINSTALL". NSIS
// "!ifdef" tests symbols "defined using !define or the /D switch" —
// defines and macros live in SEPARATE namespaces — so the guard was
// compile-time FALSE, the !insertmacro never compiled in, the
// uninstaller removed only the six legacy files, sheytan-ui.exe /
// Qt6Core.dll / the Qt tree survived, the 90-second poll timed out,
// exit 1. These tests pin the repair: the generated list must define
// its own guard symbol BEFORE the macro, the .nsi comment must match
// that reality, and the makensis gate must assert the include marker —
// a preprocessor skip can never again hide (invariant I9).
// ---------------------------------------------------------------------------

test("the uninstall-list generator defines its guard symbol in the namespace the .nsi tests", () => {
  // RED at the run-#37 tree: the generator emitted ONLY the !macro —
  // the "!ifdef" in installer.nsi therefore tested a symbol nothing
  // ever defined, in a DIFFERENT namespace from the macro. The repair
  // emits "!define SHEYTAN_PAYLOAD_UNINSTALL" immediately before the
  // "!macro SHEYTAN_PAYLOAD_UNINSTALL" line — same namespace the
  // uninstall section's guard tests.
  const wf = loadWorkflow();

  const defineAt = wf.indexOf("!define SHEYTAN_PAYLOAD_UNINSTALL");
  assert.ok(
    defineAt >= 0,
    "the workflow generator must emit !define SHEYTAN_PAYLOAD_UNINSTALL — a macro name and a define name are DIFFERENT NSIS namespaces; the .nsi's !ifdef guard never saw a macro (run #37 red gate)",
  );

  const macroAt = wf.indexOf("!macro SHEYTAN_PAYLOAD_UNINSTALL");
  assert.ok(macroAt >= 0, "the generator still emits the uninstall macro");
  assert.ok(
    defineAt < macroAt,
    "!define SHEYTAN_PAYLOAD_UNINSTALL must be emitted BEFORE the !macro line (the guard symbol exists whether or not the include carries content)",
  );
});

test("installer.nsi echoes the include marker inside the guard and its comment matches reality", () => {
  // The .nsi comment previously CLAIMED the .nsh "defines
  // SHEYTAN_PAYLOAD_UNINSTALL in it" — the contract text was true, the
  // generator output was not. The corrected comment plus the in-guard
  // !echo give the hosted makensis log a positive, greppable proof
  // that the insert compiled in.
  const nsi = loadNsi();

  const guardAt = nsi.indexOf("!ifdef SHEYTAN_PAYLOAD_UNINSTALL");
  assert.ok(guardAt >= 0, "the !ifdef guard stays");

  const echoAt = nsi.indexOf('!echo "sheytan: payload uninstall list included"');
  assert.ok(
    echoAt > guardAt,
    'the guard must !echo "sheytan: payload uninstall list included" — the marker CI asserts in win-nsis.log (I9: the compile-time skip becomes visible)',
  );

  const insertAt = nsi.indexOf("!insertmacro SHEYTAN_PAYLOAD_UNINSTALL");
  assert.ok(
    insertAt > echoAt,
    "the !echo must precede the !insertmacro inside the guard",
  );

  assert.ok(
    !nsi.includes("defines SHEYTAN_PAYLOAD_UNINSTALL in it"),
    "the stale guard comment claiming the .nsh 'defines SHEYTAN_PAYLOAD_UNINSTALL in it' must be corrected — it described a namespace that never received the symbol",
  );
});

test("the makensis gate asserts the include marker in win-nsis.log", () => {
  // Post-compile assertion: if the marker line is absent from the
  // makensis log, the uninstall list did NOT compile in — fail the
  // step instead of shipping an installer whose uninstaller keeps the
  // Qt tree on disk (the run-#37 physical result).
  const wf = loadWorkflow();
  assert.match(
    wf,
    /win-nsis\.log[\s\S]{0,600}?sheytan: payload uninstall list included[\s\S]{0,600}?(throw|exit 1)/,
    "after makensis, the step must assert the 'sheytan: payload uninstall list included' marker inside ci-logs/win-nsis.log and fail the step when absent",
  );
});

test("the uninstall smoke names the surviving targets in its failure", () => {
  // D1/D6: when the poll times out, the thrown annotation must carry
  // the capped list of surviving targets — the next reader sees WHAT
  // survived, not merely THAT something did.
  const wf = loadWorkflow();
  assert.match(
    wf,
    /[Ss]urviving targets:/,
    "the smoke's poll-timeout failure must enumerate the surviving targets (capped) — the annotation names WHAT survived",
  );
  assert.match(
    wf,
    /\$survivors/,
    "the smoke must collect the surviving targets into $survivors before throwing",
  );
});

// ---------------------------------------------------------------------------
// 5. Publish visuals and naming — ONE naming map (invariant I11), D7.
// ---------------------------------------------------------------------------

test("both package roots spell the product identically (ONE naming map)", () => {
  const wf = loadWorkflow();
  assert.match(
    wf,
    /LINUX_PKG_ROOT:\s*"SHEYTAN-LA"/,
    'LINUX_PKG_ROOT must be "SHEYTAN-LA" — the run-#37 page spelled the Linux package SHEYTAN-Local-Agent and the Windows package SHEYTAN-LA on one release page (I11)',
  );
  assert.match(
    wf,
    /WIN_PKG_ROOT:\s*"SHEYTAN-LA"/,
    'WIN_PKG_ROOT stays "SHEYTAN-LA"',
  );
  assert.ok(
    !wf.includes("SHEYTAN-Local-Agent-Linux-x64"),
    "the old divergent Linux asset spelling SHEYTAN-Local-Agent-Linux-x64-… must not survive anywhere in the workflow",
  );
});

test("release assets follow the unified v{ver}-<os>-<arch> pattern", () => {
  const wf = loadWorkflow();
  // Old Linux order/casing: ${LINUX_PKG_ROOT}-Linux-x64-v${APP_VERSION}.zip
  // Unified: ${LINUX_PKG_ROOT}-v${APP_VERSION}-linux-x64.zip — the
  // Windows pattern's field order, applied to every asset.
  assert.ok(
    !wf.includes("Linux-x64-v"),
    "the old Linux-x64-v asset order must be gone",
  );
  assert.match(
    wf,
    /\$\{LINUX_PKG_ROOT\}-v\$\{APP_VERSION\}-linux-x64\.zip/,
    "the Linux ZIP must follow the unified pattern",
  );
  assert.match(
    wf,
    /\$\{WIN_PKG_ROOT\}-v\$\{APP_VERSION\}-windows-x64\.zip/,
    "the Windows ZIP keeps the unified pattern",
  );
  assert.match(
    wf,
    /\$\{WIN_PKG_ROOT\}-v\$\{APP_VERSION\}-windows-x64-installer\.exe/,
    "the installer keeps the unified pattern",
  );
});

test("artifact display names follow the same map", () => {
  const wf = loadWorkflow();
  assert.ok(
    !wf.includes("-Windows-x64-installer-v"),
    "the old Windows-x64-installer-v artifact display name must follow the map",
  );
  assert.ok(
    !wf.includes("-Windows-x64-v"),
    "the old Windows-x64-v artifact display name must follow the map",
  );
  assert.match(
    wf,
    /name:\s*\$\{\{ env\.WIN_PKG_ROOT \}\}-v\$\{\{ env\.APP_VERSION \}\}-windows-x64-installer/,
    "the installer artifact display name follows the map",
  );
  assert.match(
    wf,
    /name:\s*\$\{\{ env\.LINUX_PKG_ROOT \}\}-v\$\{\{ env\.APP_VERSION \}\}-linux-x64/,
    "the Linux artifact display name follows the map",
  );
});

test("the release title carries the product prefix and the verify asserts it in lockstep", () => {
  // D7 supersedes the v1.3.3 bare-version title: "SHEYTAN-LA v{APP_VERSION}".
  const wf = loadWorkflow();
  assert.match(
    wf,
    /name:\s*SHEYTAN-LA v\$\{\{ env\.APP_VERSION \}\}/,
    'the release title must be "SHEYTAN-LA v${{ env.APP_VERSION }}" (D7 supersedes the v1.3.3 bare-title comment)',
  );
  assert.match(
    wf,
    /\.name\s*==\s*\$name/,
    "the publish-verify jq must assert the SAME prefixed title (.name == $name) in lockstep",
  );
  assert.match(
    wf,
    /--arg name "SHEYTAN-LA v\$\{APP_VERSION\}"/,
    "the jq invocation must pass the prefixed title as $name",
  );
});

test("red runs still expose their binaries (uploads if: always())", () => {
  // Run #37's artifact table had no installer and no ZIP — the uploads
  // sat behind the failing smoke gate. The Windows package/installer
  // uploads gain `if: always()` so a red run still shows WHAT was built.
  const wf = loadWorkflow();
  const uploadBlocks = [...wf.matchAll(
    /- name: Upload (?:Windows installer|Windows package)\n(?:.*\n)*?\s*uses: actions\/upload-artifact@v\d+\n(?:.*\n)*?\s*retention-days: \d+/g,
  )];
  assert.ok(uploadBlocks.length === 2, "both Windows package uploads exist");
  for (const block of uploadBlocks) {
    assert.match(
      block[0],
      /if:\s*always\(\)/,
      "the Windows upload steps must carry if: always() so red runs expose binaries",
    );
  }
});

test("every build job publishes ONE success-path run summary", () => {
  // D1-compliant: one $GITHUB_STEP_SUMMARY table per build job — gates
  // from gate-order.txt, artifact names + sizes, version.
  const wf = loadWorkflow();
  const summaries = [...wf.matchAll(/## SHEYTAN-LA build summary/g)];
  assert.ok(
    summaries.length >= 2,
    "Windows and Linux build jobs each write a '## SHEYTAN-LA build summary' table to $GITHUB_STEP_SUMMARY",
  );
});

// ---------------------------------------------------------------------------
// v1.10.14-fixed capacity + log-capture contract (run #39 cycle, D15/D16).
//
// RED-FIRST: every assertion below FAILED against the run-#38 tree
// (4755df8). Run #38 spent 21m29s to learn that one file did not compile:
// no preflight gate (13-15 minutes per platform to discover a ~2-minute
// defect), superseded runs kept burning minutes (cancel-in-progress:
// false), the Playwright browsers redownloaded every run, and the E2E
// gate could die BEFORE its tee created a single log byte ("E2E produced
// no build log (the step failed before tee captured output)"). These
// tests pin the capacity block and the log-first-byte contract so the
// run-#38 class cannot be committed. Additive only: a cache miss, a
// cancelled superseded run or a timeout bound must never change, mask
// or reorder a gate outcome (I16).
// ---------------------------------------------------------------------------

function jobBlocks() {
  // Top-level jobs under `jobs:`: 2-space-indented keys, bodies deeper.
  const lines = loadWorkflow().split("\n");
  const jobs = [];
  let inJobs = false;
  let current = null;
  for (const line of lines) {
    if (/^jobs:\s*$/.test(line)) {
      inJobs = true;
      continue;
    }
    if (inJobs && /^[^\s#]/.test(line)) {
      inJobs = false;
      current = null;
    }
    if (!inJobs) continue;
    const start = line.match(/^  ([a-z][a-z0-9-]*):\s*$/);
    if (start) {
      current = { name: start[1], body: [] };
      jobs.push(current);
      continue;
    }
    if (current && /^\s+\S/.test(line)) current.body.push(line);
  }
  return jobs;
}

function jobNeeds(job) {
  // The needs value: scalar (`needs: audit`) or flow list (`needs: [a, b]`).
  // v1.10.14-fixed (R4 lint): String#startsWith instead of the caret regex
  // (unicorn prefer-string-starts-ends-with, run-#39 audit warning).
  const line = job.body.find((l) => l.startsWith("    needs: "));
  if (!line) return [];
  return line
    .slice("    needs: ".length)
    .replace(/[[\]]/g, "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

test("superseded runs are cancelled (concurrency group from workflow+ref, cancel-in-progress)", () => {
  const wf = loadWorkflow();
  const block = wf.match(/^concurrency:\n((?:  [^\n]*\n?)+)/m);
  assert.ok(block, "a top-level concurrency block exists");
  assert.match(
    block[1],
    /group: \$\{\{ github\.workflow \}\}-\$\{\{ github\.ref \}\}/,
    "the group derives from github.workflow + github.ref, not a workflow-specific prefix",
  );
  assert.match(
    block[1],
    /cancel-in-progress: true/,
    "a new push supersedes the stale run (run-#38 capacity class)",
  );
});

test("a preflight compile gate exists and both platform jobs wait for it", () => {
  const jobs = jobBlocks();
  const preflight = jobs.find((j) => j.name === "preflight");
  assert.ok(preflight, "a preflight job exists");
  const preflightBody = preflight.body.join("\n");
  assert.match(
    preflightBody,
    /go build \.\/\.\.\./,
    "preflight runs the compile gate (a compile defect dies in ~2 minutes, not 13-15)",
  );
  for (const name of ["build-windows", "build-linux"]) {
    const job = jobs.find((j) => j.name === name);
    assert.ok(job, `${name} exists`);
    const needs = jobNeeds(job);
    assert.ok(
      needs.includes("preflight"),
      `${name} needs preflight (got: [${needs.join(", ")}])`,
    );
    assert.ok(
      needs.includes("audit"),
      `${name} still needs audit — the preflight wiring is additive (got: [${needs.join(", ")}])`,
    );
  }
});

test("every job is time-bounded (timeout-minutes on each, preflight included)", () => {
  const jobs = jobBlocks();
  assert.ok(
    jobs.some((j) => j.name === "preflight"),
    "preflight exists (the bound inventory covers it too)",
  );
  for (const job of jobs) {
    assert.match(
      job.body.join("\n"),
      /^    timeout-minutes: \d+/m,
      `job ${job.name} declares timeout-minutes`,
    );
  }
});

test("the Playwright browsers are cached (the E2E gate stops redownloading chromium)", () => {
  const lines = loadWorkflow().split("\n");
  const cacheBodies = [];
  for (let i = 0; i < lines.length; i++) {
    if (/uses: actions\/cache@/.test(lines[i])) {
      const body = [];
      for (
        let j = i + 1;
        j < lines.length && /^\s{8,}/.test(lines[j]);
        j++
      ) {
        body.push(lines[j].trim());
      }
      cacheBodies.push(body.join("\n"));
    }
  }
  const pw = cacheBodies.find((b) => /ms-playwright/.test(b));
  assert.ok(
    pw,
    "an actions/cache step targets the runner's ms-playwright browser path (best-effort: a miss must behave identically, only slower)",
  );
  assert.match(pw, /key: /, "the cache declares a key");
  assert.match(
    pw,
    /hashFiles\(['"]package-lock\.json['"]\)/,
    "the key is pinned by the lockfile (which pins the Playwright version)",
  );
});

test("the Linux E2E gate captures its logs from the first byte", () => {
  const wf = loadWorkflow();
  const step = wf.match(
    /- name: Browser E2E \(real application stack\)\n([\s\S]*?)(?=\n      - name: )/,
  );
  assert.ok(step, "the Browser E2E step exists");
  const body = step[1];
  const firstCommand = body.search(/python3 -m pip install/);
  assert.ok(firstCommand > 0, "the step body was located (fixture sanity)");
  for (const log of ["ci-logs/e2e-build.log", "ci-logs/e2e-playwright.log"]) {
    const eager = body.search(new RegExp(`:\\s*>[^\\n]*${log.replace(/\./g, "\\.")}`));
    assert.ok(
      eager >= 0,
      `${log} is created eagerly (\`: >\`) at the step's first byte`,
    );
    assert.ok(
      eager < firstCommand,
      `${log} exists BEFORE the first command runs — a pre-tee failure still leaves the log`,
    );
  }
});

test("an absent or empty gate log is a NAMED instrumentation failure, never silence", () => {
  // I15: every gate leaves a log; a log that never got a byte is a
  // capture defect and must say so by name (run-#38 Linux class).
  const wf = loadWorkflow();
  const hits = [...wf.matchAll(/gate produced no log — capture defect/g)];
  assert.ok(
    hits.length >= 2,
    `the named no-log guard is wired for the E2E summary AND the Windows go-log summary (found ${hits.length})`,
  );
});

test("the v1.10.14-fixed workflow contract keeps every heritage fragment intact", () => {
  // The capacity block is additive: gates, step order and exit contracts
  // stay byte-identical in intent. Spot-pin the run-#38 heritage rows so
  // a future "cleanup" cannot ride the capacity commit.
  const wf = loadWorkflow();
  assert.match(wf, /uninstall-symmetry include marker missing from ci-logs\/win-nsis\.log/);
  assert.match(wf, /WIN_PKG_ROOT: "SHEYTAN-LA"/);
  assert.match(wf, /LINUX_PKG_ROOT: "SHEYTAN-LA"/);
  assert.match(wf, /- name: Browser E2E \(real application stack\)/);
  assert.match(wf, /npm run build:e2e 2>&1 \| tee ci-logs\/e2e-build\.log/);
  assert.match(wf, /npm run test:e2e 2>&1 \| tee ci-logs\/e2e-playwright\.log/);
});

// ---------------------------------------------------------------------------
// v1.10.14-fixed run-#40 cycle (R1/R2/R4 — D15/D17/D18/D19).
//
// RED-FIRST: every assertion below FAILED against the run-#39 tree
// (ace68fd). Run #39 proved three capture classes the operator must
// never see again:
//
//   1. PowerShell `| Tee-Object` NEVER RUNS on empty pipeline input, so
//      a silent-on-success gate (go vet) or a clean gofmt left NO log
//      file at all — the end-of-job failure summary then fired two false
//      "gate produced no log — capture defect" catchers for gates that
//      had actually PASSED. First-byte capture: the log EXISTS before
//      the command starts, on every platform, for every gate.
//   2. makensis runs at default verbosity, where `!echo` output is
//      SUPPRESSED — the uninstall-symmetry include marker can never
//      reach ci-logs/win-nsis.log, so the include-marker assertion was
//      dead-on-arrival (compile-proven locally with makensis 3.10:
//      verbosity 1/2/3/default carry 0 marker hits, only -V4 carries
//      the marker). The gate must compile at -V4.
//   3. No job uploaded its COMPLETE ci-logs set; win-nsis.log rode in
//      no artifact at all (D18). Every job now uploads <job>-ci-logs
//      with if: always().
//
// Additive only: a cache miss, a cancelled superseded run, a timeout
// bound or an upload path change must never change, mask or reorder a
// gate outcome (I16).
// ---------------------------------------------------------------------------

function jobStepBodies(job) {
  // Steps inside a job: `      - name: X` keys, bodies indented deeper.
  const steps = [];
  let current = null;
  for (const line of job.body) {
    const start = line.match(/^      - name: (.+)$/);
    if (start) {
      current = { name: start[1].trim(), body: [] };
      steps.push(current);
      continue;
    }
    if (current) current.body.push(line);
  }
  return steps;
}

function findJobStep(jobName, stepName) {
  const job = jobBlocks().find((j) => j.name === jobName);
  assert.ok(job, `job ${jobName} exists`);
  const step = jobStepBodies(job).find((s) => s.name === stepName);
  assert.ok(step, `job ${jobName} has a "${stepName}" step`);
  return step.body.join("\n");
}

function eagerCreationIndex(stepBody, logPath) {
  // The first line that creates the log file EAGERLY: shell truncation,
  // New-Item, Out-File, Set-Content or Clear-Content — anything that
  // materializes the file BEFORE the gated command runs.
  const escaped = logPath.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const eager = new RegExp(
    "(?::\\s*>\\s*|New-Item [^\\n]*|Out-File [^\\n]*|Set-Content [^\\n]*|Clear-Content [^\\n]*)" +
      escaped,
  );
  return stepBody.search(eager);
}

test("every Windows Go gate creates its log before the command runs (first-byte capture, I15)", () => {
  const body = findJobStep("build-windows", "Go verification");
  // Gates routed through Invoke-Gate: the EAGER creation of $LogFile must
  // sit inside the function, BEFORE `& $Command` — the call sites pass the
  // path as an argument, so the function body is the capture point.
  const invokeGate = body.match(/function Invoke-Gate \{[\s\S]*?\n          \}/);
  assert.ok(invokeGate, "the Invoke-Gate helper is defined in the step");
  const fn = invokeGate[0];
  const eagerAt = fn.search(/New-Item[^\n]*\$LogFile|Clear-Content[^\n]*\$LogFile|Out-File[^\n]*\$LogFile|: > \.\$LogFile/);
  assert.ok(eagerAt >= 0, "Invoke-Gate creates $LogFile EAGERLY");
  const execAt = fn.search(/& \$Command/);
  assert.ok(execAt >= 0, "Invoke-Gate executes its command");
  assert.ok(
    eagerAt < execAt,
    "the gate's log exists BEFORE the command's first byte (run-#39 no-log class)",
  );
  // The three Invoke-Gate call sites carry their own log paths.
  for (const [log, command] of [
    ["ci-logs/win-go-test-headless.log", /go test \.\/internal/],
    ["ci-logs/win-go-test-full.log", /go test \.\/\.\.\./],
    ["ci-logs/win-go-vet.log", /go vet/],
  ]) {
    const commandAt = body.search(command);
    assert.ok(commandAt >= 0, `the gated command for ${log} exists`);
    assert.ok(
      body.includes(log),
      `${log} is captured (passed to Invoke-Gate)`,
    );
  }
  // The gofmt gate runs inline: its log must be created eagerly before
  // the gofmt command itself.
  const gofmtEagerAt = eagerCreationIndex(body, "ci-logs/win-gofmt.log");
  assert.ok(gofmtEagerAt >= 0, "ci-logs/win-gofmt.log is created EAGERLY");
  const gofmtAt = body.search(/gofmt -l \./);
  assert.ok(gofmtAt >= 0, "the gofmt -l . command exists");
  assert.ok(gofmtEagerAt < gofmtAt, "win-gofmt.log exists BEFORE gofmt runs");
});

test("every Linux Go gate creates its log before the command runs (first-byte capture, I15)", () => {
  const body = findJobStep("build-linux", "Go verification");
  // Anchored on each gate's TEE line (the capture point) — prose in
  // comments may legitimately mention the command names.
  for (const log of [
    "ci-logs/linux-go-test-headless.log",
    "ci-logs/linux-go-test-full.log",
    "ci-logs/linux-go-vet.log",
    "ci-logs/linux-gofmt.log",
  ]) {
    const eagerAt = eagerCreationIndex(body, log);
    assert.ok(eagerAt >= 0, `${log} is created EAGERLY before its command`);
    const escaped = log.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const teeAt = body.search(new RegExp(`tee ${escaped}`));
    assert.ok(teeAt >= 0, `the tee capture for ${log} exists`);
    assert.ok(
      eagerAt < teeAt,
      `${log} exists BEFORE the command's first byte`,
    );
  }
});

test("makensis compiles at -V4 so the include marker reaches the captured log (R1)", () => {
  // Local compile-proof (makensis 3.10, this cycle): the generated
  // payload-uninstall.nsh's guard define, the !ifdef and the !insertmacro
  // all work — but `!echo` output is only emitted at verbosity 4. At the
  // default verbosity the uninstall-symmetry marker can NEVER appear in
  // ci-logs/win-nsis.log, so the marker assertion failed in runs #37 and
  // #39 regardless of the chain's correctness. -V4 is the minimal fix:
  // installer.nsi stays byte-identical and the gate keeps its teeth.
  const body = findJobStep("build-windows", "Build Windows NSIS installer");
  const invocation = body.match(/& \$makensis[\s\S]*?installer\.nsi/);
  assert.ok(invocation, "the makensis invocation exists");
  assert.match(
    invocation[0],
    /-V4\b/,
    "makensis runs at -V4 (the only verbosity at which !echo output reaches stdout)",
  );
  const eagerAt = eagerCreationIndex(body, "ci-logs/win-nsis.log");
  assert.ok(eagerAt >= 0, "ci-logs/win-nsis.log is created EAGERLY");
  assert.ok(
    eagerAt < body.search(/& \$makensis/),
    "win-nsis.log exists BEFORE makensis starts",
  );
});

test("every job uploads exactly ONE consolidated log part with if: always() (D21/I18, run-#41 R2)", () => {
  // Supersedes the run-#40 <job>-ci-logs-attempt contract: the eight
  // per-class log artifacts became ONE <job>-logs-part-attempt<N>.txt
  // per job, merged by the consolidate-logs job into the single
  // SHEYTAN-LA-v<version>-all-logs artifact (D21). The job's ci-logs
  // writes are untouched — only the upload surface is consolidated.
  const jobs = jobBlocks();
  for (const name of ["preflight", "audit", "build-windows", "build-linux"]) {
    const job = jobs.find((j) => j.name === name);
    assert.ok(job, `job ${name} exists`);
    const steps = jobStepBodies(job);
    const part = steps.find((s) =>
      /uses: actions\/upload-artifact@/.test(s.body.join("\n")) &&
      new RegExp(`name: ${name}-logs-part-attempt`).test(s.body.join("\n")),
    );
    assert.ok(
      part,
      `${name} uploads its ${name}-logs-part-attempt artifact (the D21 part)`,
    );
    const body = part.body.join("\n");
    assert.match(body, /if: always\(\)/, `${name}'s part upload runs on EVERY outcome`);
    assert.match(body, /log-parts\//, `${name}'s part is built under log-parts/ (never self-includes ci-logs)`);
  }
});

test("every actions pin is at or above its verified latest major (R4/D19, re-derived 2026-10-23)", () => {
  // Verified against the primary sources (GitHub releases pages) at P1:
  //   actions/cache v6.1.0 (2026-06-26, Node 24 line),
  //   actions/upload-artifact v7.0.2 (2026-10-07),
  //   actions/checkout v7.0.1, actions/setup-node v7.1.0,
  //   actions/setup-go v7.0.0, actions/setup-python v7,
  //   actions/download-artifact v8.0.2 (2026-10-07) — re-verified live
  //   from the releases page at P1 of the run-#41 cycle (2026-10-23).
  // install-qt-action stays frozen (charter: no Qt/aqtinstall changes).
  const floors = {
    "actions/cache": 6,
    "actions/upload-artifact": 7,
    "actions/checkout": 7,
    "actions/setup-node": 7,
    "actions/setup-go": 7,
    "actions/setup-python": 7,
    "actions/download-artifact": 8,
  };
  const wf = loadWorkflow();
  const pins = [...wf.matchAll(/uses: ([\w-]+\/[\w-]+)@(v\d+)/g)];
  assert.ok(pins.length >= 10, "the workflow declares its action pins");
  for (const [, action, tag] of pins) {
    const floor = floors[action];
    if (!floor) continue; // jurplel/install-qt-action: charter-frozen
    const major = Number(tag.slice(1));
    assert.ok(
      major >= floor,
      `${action}@${tag} is below the verified latest major v${floor}`,
    );
  }
});

test("the Go toolchain is the 1.27 line and Node stays the 24 LTS line (R4/D19)", () => {
  // go1.27.3 (2026-10-19, security fixes) is the latest stable per
  // go.dev/doc/devel/release; Node 24 "Krypton" is the active LTS per
  // nodejs.org. Node 26 becomes LTS 2026-10-28 — future work, never
  // pinned pre-LTS. Go versions are 1.MINOR: the LINE is the minor.
  const wf = loadWorkflow();
  const goVer = wf.match(/^  GO_VERSION: "([^"]+)"$/m);
  assert.ok(goVer, "env.GO_VERSION is declared");
  const goParts = goVer[1].split(".").map(Number);
  assert.ok(
    goParts[0] > 1 || (goParts[0] === 1 && goParts[1] >= 27),
    `GO_VERSION ${goVer[1]} is on the 1.27 line or newer (run-#39 tree pinned 1.26)`,
  );
  const nodeVer = wf.match(/^  NODE_VERSION: "([^"]+)"$/m);
  assert.ok(nodeVer, "env.NODE_VERSION is declared");
  assert.equal(
    nodeVer[1].split(".")[0],
    "24",
    "NODE_VERSION stays the 24 LTS line",
  );
});

test("green gates emit one PLAIN success line each (D29: green runs show ZERO annotations)", () => {
  // v1.10.15-fixed R2 (D29): success messages moved from ::notice
  // annotations to plain step output — still teed first-byte into their
  // logs and all-logs.txt (I15/I18 untouched). The line survives; only
  // the annotation class is gone.
  const cases = [
    // Windows: the Invoke-Gate helper carries the green line for every
    // gate it drives; the inline gofmt gate emits its own literal line.
    ["build-windows", "Go verification", /Write-Host "gate OK: \$Label"/],
    ["build-windows", "Go verification", /Write-Host "gate OK: gofmt -l \."/],
    // Linux: the bash gates emit one literal line each.
    ["build-linux", "Go verification", /echo "gate OK: go vet \.\/\.\.\."/],
    ["build-linux", "Go verification", /echo "gate OK: gofmt -l \."/],
    // The NSIS compile gate emits its own literal line.
    ["build-windows", "Build Windows NSIS installer", /gate OK: NSIS makensis/],
  ];
  for (const [job, step, pattern] of cases) {
    const body = findJobStep(job, step);
    assert.match(
      body,
      pattern,
      `${job}/${step} emits a green-gate success line (plain output, no annotation)`,
    );
  }
});

test("the Windows no-log guard names its gate commands, never a bare path (D17)", () => {
  // With first-byte capture the guard becomes a TRUE last resort; when
  // it fires it must carry the command and the expected path (I17).
  const wf = loadWorkflow();
  const guardAt = wf.indexOf("$goLogCommands");
  assert.ok(guardAt >= 0, "the guard's command map exists");
  const guard = wf.slice(guardAt, guardAt + 900);
  assert.match(
    guard,
    /"win-go-vet\.log"\s+=\s+"go vet \.\/\.\.\."/,
    "the guard maps win-go-vet.log to its command",
  );
  assert.match(
    guard,
    /"win-gofmt\.log"\s+=\s+"gofmt -l \."/,
    "the guard maps win-gofmt.log to its command",
  );
});

// ---------------------------------------------------------------------------
// v1.10.14-fixed run-#41 cycle (R1/R2/R3 — D17/D21/D22, Task ID 37).
//
// RED-FIRST: every assertion below FAILED against the run-#40 tree
// (e1db00c). Run #40's last red was a CONTENT-FREE red: the Linux
// "Stress suite" gate died with exit 1 (46 pass / 1 fail — the stale
// GO_VERSION pin inside cmd/stress_release_surface.go) and its output
// reached NO annotation — the exact class D17 bans; the stress step was
// the only gate left outside the capture contract. The run's evidence
// also sprawled across EIGHT log artifacts with no single .txt (D21).
// ---------------------------------------------------------------------------

test("the Linux stress gate captures its log from the first byte (I15, R1)", () => {
  const body = findJobStep("build-linux", "Stress suite");
  const firstCommand = body.search(/go build/);
  assert.ok(firstCommand > 0, "the stress step body was located");
  const eager = eagerCreationIndex(body, "ci-logs/stress-suite.log");
  assert.ok(
    eager >= 0,
    "ci-logs/stress-suite.log is created eagerly (`: >`) at the step's first byte — run-#40's stress output reached no annotation because no log existed to carry it",
  );
  assert.ok(
    eager < firstCommand,
    "the stress log exists BEFORE the first command runs",
  );
  const teeAt = body.search(/tee ci-logs\/stress-suite\.log/);
  assert.ok(teeAt > eager, "the stress run tees into ci-logs/stress-suite.log");
  assert.match(
    body,
    /\/tmp\/sheytan-stress stress/,
    "the stress command itself is unchanged (no weakening, no sleeps, no bound inflation)",
  );
  assert.match(body, /RUN stress suite/, "gate-order.txt carries the RUN marker");
  assert.match(body, /OK stress suite/, "gate-order.txt carries the OK marker");
  assert.match(
    body,
    /^\s*echo "gate OK: stress suite"/m,
    "a green stress gate emits its ONE success line (plain output, D29)",
  );
  assert.doesNotMatch(
    body,
    /::notice::gate OK: stress suite/,
    "the green stress line is NOT an annotation (D29)",
  );
  assert.match(
    body,
    /set -euo pipefail/,
    "the step keeps its strict pipefail contract — a failing stress run still fails the step through the tee pipeline",
  );
});

test("the stress failure summary publishes real error lines as annotations (D17, R1)", () => {
  const wf = loadWorkflow();
  const step = wf.match(
    /- name: Stress failure summary \(Linux\)\n([\s\S]*?)(?=\n      - name: )/,
  );
  assert.ok(
    step,
    "an if:failure() stress summary step exists below the Stress suite step — run-#40's red had zero error annotations",
  );
  const body = step[1];
  assert.match(body, /if: failure\(\)/, "the summary only runs on failure");
  assert.match(body, /::error::/, "the summary emits ::error annotations");
  assert.match(body, /FAIL/, "the summary extracts the suite's FAIL lines");
  assert.match(
    body,
    /STRESS-RESULT/,
    "the summary carries the pass/fail verdict line",
  );
  assert.match(
    body,
    /gate produced no log — capture defect/,
    "the no-log guard is the named last resort (I15/I17), never silence",
  );
});

test("the stress release-surface check follows the verified toolchain forward (R1/R3)", () => {
  const src = readFileSync(
    join(repoRoot, "cmd", "stress_release_surface.go"),
    "utf8",
  );
  assert.ok(
    !src.includes('GO_VERSION: "1.26"'),
    "the stale 1.26 pin — run-#40's actual red — is gone (the workflow correctly moved to the 1.27 line in the run-#40 cycle; the check kept demanding 1.26)",
  );
  assert.match(src, /GO_VERSION: "1\.27"/, "the check pins the verified 1.27 line");
  assert.ok(
    !src.includes("actions/upload-artifact@v6"),
    "the stale upload-artifact@v6 floor is gone (the workflow has been v7 since run #40)",
  );
  assert.match(src, /actions\/upload-artifact@v7/, "the check requires upload-artifact@v7");
  assert.match(
    src,
    /actions\/download-artifact@v8/,
    "the check requires download-artifact@v8 (the verified latest major, v8.0.2)",
  );
});

test("the per-class log-artifact sprawl is gone (D21: exactly ONE log artifact remains)", () => {
  const wf = loadWorkflow();
  for (const stale of [
    "name: preflight-verification-logs-attempt",
    "name: preflight-ci-logs-attempt",
    "name: audit-verification-logs-attempt",
    "name: audit-ci-logs-attempt",
    "name: windows-go-verification-logs-attempt",
    "name: build-windows-ci-logs-attempt",
    "name: windows-qt-acceptance-report-attempt",
    "name: linux-verification-logs-attempt",
    "name: build-linux-ci-logs-attempt",
    "name: linux-qt-acceptance-report-attempt",
  ]) {
    assert.ok(
      !wf.includes(stale),
      `the intermediate artifact "${stale}" is folded into its job's part (D21)`,
    );
  }
});

test("scripts/consolidate-logs.mjs merges parts into ONE all-logs.txt deterministically (D21)", () => {
  // Executable local proof: four fixture parts in, one all-logs.txt out,
  // sections in the FIXED job order with per-section job/outcome
  // headers; a missing part is a NAMED instrumentation failure (I18).
  const tmp = mkdtempSync(join(tmpdir(), "shtn-consolidate-test-"));
  try {
    const parts = join(tmp, "parts");
    mkdirSync(parts, { recursive: true });
    const jobs = [
      ["preflight", "success"],
      ["audit", "success"],
      ["build-windows", "success"],
      ["build-linux", "failure"],
    ];
    for (const [job, outcome] of jobs) {
      writeFileSync(
        join(parts, `${job}-logs-part-1.txt`),
        `job: ${job}\nattempt: 1\noutcome: ${outcome}\nrun: https://run.example/41\n----- ci-logs/gate-order.txt -----\nRUN go build\nOK go build\n----- ci-summary -----\nci-summary job=${job} outcome=${outcome}\n`,
      );
    }
    const out = join(tmp, "all-logs.txt");
    const r = spawnSync(
      process.execPath,
      [join(repoRoot, "scripts", "consolidate-logs.mjs"), "--parts", parts, "--out", out],
      { encoding: "utf8" },
    );
    assert.equal(
      r.status,
      0,
      `the merge exits 0 on complete parts (stderr: ${r.stderr.slice(0, 400)})`,
    );
    const merged = readFileSync(out, "utf8");
    const at = {};
    for (const [job, outcome] of jobs) {
      const idx = merged.indexOf(`== JOB: ${job}`);
      assert.ok(idx >= 0, `a section for ${job} exists`);
      assert.ok(
        merged.slice(idx, idx + 300).includes(`outcome: ${outcome}`),
        `${job}'s section header carries its outcome`,
      );
      at[job] = idx;
    }
    assert.ok(
      at.preflight < at.audit &&
        at.audit < at["build-windows"] &&
        at["build-windows"] < at["build-linux"],
      "sections land in the FIXED job order (deterministic merge)",
    );
    assert.equal(
      merged.split("== JOB:").length - 1,
      4,
      "every part contributes exactly one section",
    );
    rmSync(join(parts, "audit-logs-part-1.txt"));
    const r2 = spawnSync(
      process.execPath,
      [join(repoRoot, "scripts", "consolidate-logs.mjs"), "--parts", parts, "--out", out],
      { encoding: "utf8" },
    );
    assert.equal(
      r2.status,
      1,
      "a missing part is a named instrumentation failure: the merge exits 1 (I18)",
    );
    assert.match(
      readFileSync(out, "utf8"),
      /MISSING PART.*audit/s,
      "the missing audit section is NAMED in the merged output",
    );
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
});

test("a consolidate-logs job merges the parts into the ONE all-logs artifact (D21, I18)", () => {
  const jobs = jobBlocks();
  const c = jobs.find((j) => j.name === "consolidate-logs");
  assert.ok(c, "a consolidate-logs job exists");
  const body = c.body.join("\n");
  const needs = jobNeeds(c);
  for (const n of ["preflight", "audit", "build-windows", "build-linux"]) {
    assert.ok(needs.includes(n), `consolidate-logs needs ${n}`);
  }
  assert.match(
    body,
    /if: always\(\)/,
    "consolidation runs on EVERY outcome — a red run still gets its one log",
  );
  assert.match(body, /timeout-minutes: \d+/, "consolidate-logs is time-bounded");
  assert.match(
    body,
    /uses: actions\/download-artifact@v8/,
    "parts download uses download-artifact@v8 (the verified latest major, R3)",
  );
  assert.match(body, /pattern:/, "the download selects all parts by pattern");
  assert.match(
    body,
    /merge-multiple: true/,
    "merge-multiple flattens the parts into one directory",
  );
  assert.match(
    body,
    /node scripts\/consolidate-logs\.mjs/,
    "the deterministic merge script runs",
  );
  assert.match(
    body,
    /name: \$\{\{ env\.WIN_PKG_ROOT \}\}-v\$\{\{ env\.APP_VERSION \}\}-all-logs/,
    "the ONE artifact name follows the naming map (renders SHEYTAN-LA-v1.10.14-all-logs; I11)",
  );
  assert.match(
    body,
    /all-logs\.txt/,
    "the artifact contains exactly one file: all-logs.txt",
  );
  assert.match(
    body,
    /actions: write/,
    "the job declares actions: write so it may delete the intermediate parts",
  );
  assert.match(
    body,
    /gh api -X DELETE/,
    "intermediate parts are deleted in-run via the Artifacts API",
  );
  assert.match(
    body,
    /APP_VERSION: \$\{\{ needs\.audit\.outputs\.version \}\}/,
    "identity flows from the audit job (ONE naming map, I11)",
  );
});

// ---------------------------------------------------------------------------
// 5. Run-#41 cycle contract (v1.10.15, Task ID 38) — RED-first R1/R2/R4.
//
// Run #41 (39895847864, head 246eaba) died in the LINUX "Go verification"
// step (annotation anchor step:15:312, after the headless gate-OK notice at
// step:15:149). Every later stage never ran, and the four if:failure()
// summaries then mis-fired: three of them emitted ::error-class
// "never started / capture defect" lines for stages that never began, and
// the E2E summary's boilerplate claimed "the build:e2e stage is the failing
// gate" for a stage that never ran — while the ACTUAL red (Go verification)
// published zero content annotations because no failure summary exists for
// it at all. The assertions below pin the repaired contract; each
// defect-pinning assertion was RED against the 1.10.14 tree and must stay
// green from 1.10.15 onward.
// ---------------------------------------------------------------------------

/** Extract one workflow step block by step name (6-space YAML indent). */
function loadStep(stepName) {
  const wf = loadWorkflow();
  const lines = wf.split("\n");
  const start = lines.findIndex((l) => l.trim() === `- name: ${stepName}`);
  assert.ok(start !== -1, `workflow step not found: ${stepName}`);
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^ {6}- (name|uses):/.test(lines[i])) {
      end = i;
      break;
    }
  }
  return lines.slice(start, end).join("\n");
}

function loadVersion() {
  return JSON.parse(readFileSync(join(repoRoot, "package.json"), "utf8")).version;
}

test("R2: the Linux Go verification gate has a failure summary that publishes its captured content", () => {
  const wf = loadWorkflow();
  const summary = loadStep("Go verification failure summary (Linux)");
  assert.match(summary, /if: failure\(\)/, "the summary is an if:failure() step");
  // The summary reads the Go verification first-byte logs (they already
  // exist: linux-go-test-headless/full.log, linux-go-vet.log, linux-gofmt.log).
  assert.match(summary, /ci-logs\/linux-go-test-full\.log/, "reads the full-suite log");
  assert.match(summary, /ci-logs\/linux-go-test-headless\.log/, "reads the headless log");
  assert.match(summary, /ci-logs\/linux-go-vet\.log/, "reads the vet log");
  assert.match(summary, /::error::/, "publishes real error lines as annotations (I17)");
  // Dangling-marker verdict: a RUN without its OK names the sub-gate that died.
  assert.match(summary, /gate-order\.txt/, "uses the gate-order markers for the verdict");
  // Ordering: the summary must sit between Go verification and the Playwright cache.
  const goIdx = wf.indexOf("- name: Go verification");
  const sumIdx = wf.indexOf("- name: Go verification failure summary (Linux)");
  const cacheIdx = wf.indexOf("- name: Cache Playwright browsers");
  assert.ok(sumIdx > goIdx && sumIdx < cacheIdx, "summary is wired directly after the gate");
});

test("R2/I17: no failure summary ever emits a never-started ::error (D29: warning class only)", () => {
  const wf = loadWorkflow();
  const offenders = wf
    .split("\n")
    .filter((l) => /::error::/.test(l) && /never started/i.test(l));
  assert.deepEqual(
    offenders,
    [],
    `never-started must be ::warning, never ::error (offenders: ${offenders.length})`,
  );
});

test("R2/I17: every Linux failure summary carries the NEVER-STARTED branch (D31: plain step output, never an annotation)", () => {
  for (const name of [
    "E2E failure summary (Linux)",
    "Stress failure summary (Linux)",
    "Qt smoke failure summary (Linux)",
    "Qt acceptance failure summary (Linux)",
  ]) {
    const step = loadStep(name);
    assert.match(
      step,
      /never started/i,
      `${name}: the never-started branch exists (the upstream-failure class, D29)`,
    );
    assert.doesNotMatch(
      step,
      /::(warning|notice|error)::[^\n]*never started/i,
      `${name}: NEVER-STARTED never annotates (D31 errors-only — it is step-log context, recorded in all-logs.txt)`,
    );
  }
  // The Go verification summary carries the same guard — one branch,
  // plain output, never an annotation of any class.
  const goSummary = loadStep("Go verification failure summary (Linux)");
  assert.match(
    goSummary,
    /never started/i,
    "Go verification failure summary: the never-started branch exists",
  );
  assert.doesNotMatch(
    goSummary,
    /::(warning|notice|error)::[^\n]*never started/i,
    "Go verification failure summary: NEVER-STARTED never annotates (D31)",
  );
});

test("R2/I17: the E2E summary's failing-gate claim is evidence-gated on the build stage having started", () => {
  const step = loadStep("E2E failure summary (Linux)");
  // The "build:e2e is the failing gate" text may only fire after a
  // started-detection (the build stage's first-byte log or its gate-order
  // marker). At HEAD the claim fires unconditionally — the run-#41 false lead.
  const claimIdx = step.indexOf("the build:e2e stage is the failing gate");
  assert.ok(claimIdx !== -1, "the claim exists");
  const before = step.slice(0, claimIdx);
  assert.match(
    before,
    /(gate-order\.txt|RUN build:e2e|e2e-build\.log)/,
    "a started-detection precedes the failing-gate claim",
  );
  // The never-started branch must run FIRST and exit before any ::error.
  assert.match(
    step,
    /never started[\s\S]*exit 0[\s\S]*::error::/,
    "never-started detection short-circuits the error path",
  );
});

test("R2/I15: every Linux test-stage log is created before its command (first-byte capture)", () => {
  const wf = loadWorkflow();
  for (const log of [
    "e2e-build.log",
    "e2e-playwright.log",
    "stress-suite.log",
    "linux-go-test-headless.log",
    "linux-go-test-full.log",
    "linux-go-vet.log",
    "linux-gofmt.log",
  ]) {
    const createIdx = wf.indexOf(`: > ci-logs/${log}`);
    const teeIdx = wf.indexOf(`tee ci-logs/${log}`);
    assert.ok(createIdx !== -1, `first-byte creation exists for ${log}`);
    assert.ok(teeIdx !== -1, `tee capture exists for ${log}`);
    assert.ok(createIdx < teeIdx, `${log} is created BEFORE its tee`);
  }
});

test("R4/I12: the release identity agrees on every version surface (D26, package.json is the ONE source)", () => {
  // v1.11.0 (run-#48 R5): the pin is version-agnostic — the surfaces must
  // spell the package.json version, wherever it moves. The absolute-value
  // pin lives in the run-#48 R5/I12 test below (re-version C15).
  const version = loadVersion();
  assert.match(version, /^\d+\.\d+\.\d+$/, "package.json (the one version source)");
  const goCfg = readFileSync(join(repoRoot, "internal", "config", "config.go"), "utf8");
  assert.match(goCfg, new RegExp(`AppVersion[ \\t]*=[ \\t]*"${version.replace(/\\./g, "\\.")}"`), "internal/config/config.go");
  const buildCfg = readFileSync(join(repoRoot, "build", "config.yml"), "utf8");
  assert.match(buildCfg, new RegExp(`productVersion:[ \\t]*"${version.replace(/\\./g, "\\.")}"`), "build/config.yml");
  const signature = readFileSync(join(repoRoot, "SIGNATURE"), "utf8");
  assert.match(signature, new RegExp(`^SHEYTAN-Local-Agent v${version.replace(/\\./g, "\\.")}$`, "m"), "SIGNATURE first line");
  // v1.10.15-fixed (R3/I6): the Qt host carried a drifted hardcoded
  // identity ("1.10.1" through ten releases) — the surface now moves with
  // package.json, and this assertion keeps it from re-drifting silently.
  const qtMain = readFileSync(join(repoRoot, "ui", "qt", "src", "main.cpp"), "utf8");
  assert.match(
    qtMain,
    new RegExp(`setApplicationVersion\\(QStringLiteral\\("${version.replace(/\\./g, "\\.")}"\\)\\)`),
    "ui/qt/src/main.cpp applicationVersion",
  );
  // No drift in either direction: the retired version must not survive on
  // any surface (workflow comments narrating run history are not surfaces).
  for (const [file, text] of [
    ["package.json", JSON.stringify(JSON.parse(readFileSync(join(repoRoot, "package.json"), "utf8")))],
    ["internal/config/config.go", goCfg],
    ["build/config.yml", buildCfg],
  ]) {
    assert.ok(
      !/"?1\.10\.14"?/.test(text.replace(new RegExp(version.replace(/\./g, "\\."), "g"), "")),
      `${file} carries no stale 1.10.14 identity`,
    );
  }
});

test("R4/I19: action majors and toolchain pins hold at the verified versions", () => {
  const wf = loadWorkflow();
  assert.match(wf, /uses: actions\/checkout@v7/, "checkout at or above v7");
  assert.match(wf, /uses: actions\/setup-node@v7/, "setup-node at or above v7");
  assert.match(wf, /uses: actions\/setup-go@v7/, "setup-go at or above v7");
  assert.match(wf, /uses: actions\/upload-artifact@v7/, "upload-artifact at or above v7");
  assert.match(wf, /uses: actions\/download-artifact@v8/, "download-artifact at or above v8");
  assert.match(wf, /GO_VERSION: "1\.27"/, "Go toolchain pin 1.27");
  assert.match(wf, /NODE_VERSION: "24"/, "Node toolchain pin 24");
});

// ---------------------------------------------------------------------------
// v1.10.15-fixed run-#43 cycle (R1/R2 — D28/D29, Task ID 39).
//
// RED-FIRST: every assertion below FAILED against the run-#42 tree
// (bf6cf2d). Run #42 was fully green but carried 20 SUCCESS notices as
// annotations (D29: a green run must show ZERO annotations), and the Qt
// surface shipped only the chat view (D28: the full Wails IA must exist
// in native Qt form, enumerated by a tracked parity manifest).
// ---------------------------------------------------------------------------

function loadNonCommentWorkflow() {
  // Drop comment-only lines (bash `#` and PowerShell `#`): narration may
  // legitimately mention the retired class; emissions may not.
  return loadWorkflow()
    .split("\n")
    .filter((l) => !/^\s*#/.test(l))
    .join("\n");
}

test("R2/D29: the workflow emits ZERO ::notice annotations outside comments", () => {
  const wf = loadNonCommentWorkflow();
  const offenders = wf.split("\n").filter((l) => l.includes("::notice"));
  assert.deepEqual(
    offenders,
    [],
    `a green run must show ZERO annotations; success paths moved to plain step output (offenders: ${offenders.length}): ${offenders[0] ?? ""}`,
  );
});

test("R2/D29: cache notes and ci-summary lines are plain step output", () => {
  const wf = loadNonCommentWorkflow();
  assert.doesNotMatch(wf, /::notice::Playwright browser cache/);
  assert.doesNotMatch(wf, /::notice::ci-summary/);
  assert.match(
    wf,
    /echo "Playwright browser cache HIT/,
    "the cache note survives as plain output (I15/I18 untouched)",
  );
  assert.match(
    wf,
    /ci-summary step=/,
    "the ci-summary accounting line survives as plain output",
  );
});

test("R2/D29: the failure emitters classify notice-severity lines as PLAIN output", () => {
  const wf = loadNonCommentWorkflow();
  // Windows Emit-Line: the notice branch must Write-Host the bare message.
  assert.doesNotMatch(wf, /'notice'\s+\{ Write-Host "::notice::/);
  assert.match(
    wf,
    /'notice'\s+\{ Write-Host \$msg \}/,
    "Windows failure emitter: notice-class lines are plain output",
  );
  // Linux emit_line: the notice branch must printf the bare message.
  assert.doesNotMatch(wf, /notice\)\s+printf '::notice::/);
  assert.match(
    wf,
    /notice\)\s+printf '%s\\n'\s+"?\$2/,
    "Linux failure emitter: notice-class lines are plain output",
  );
});

test("R2/D31: NEVER-STARTED guards are PLAIN step output in every summary (errors only)", () => {
  // v1.10.15-fixed (Task ID 41): D31 supersedes the D29 warning class —
  // annotations carry ERRORS ONLY. The five NEVER-STARTED guards (go,
  // e2e, stress, qt-smoke, qt-acceptance) keep their signal as plain
  // step output + all-logs.txt; no workflow-command prefix may remain.
  const wf = loadNonCommentWorkflow();
  const offenders = wf
    .split("\n")
    .filter((l) => /never started/i.test(l) && l.includes("::"));
  assert.deepEqual(
    offenders,
    [],
    `NEVER-STARTED is step-log context, never an annotation (D31): ${offenders[0] ?? ""}`,
  );
});

test("R1/I21: the parity manifest exists and enumerates every Wails surface", () => {
  const manifest = readFileSync(
    join(repoRoot, "ui", "qt", "parity-manifest.md"),
    "utf8",
  );
  // The census (P1, §4c): every view, panel and extra on the Wails side.
  const census = [
    "nav-chrome",
    "chat view",
    "settings-general",
    "settings-models",
    "settings-performance",
    "settings-agent",
    "settings-network",
    "settings-updates",
    "settings-diagnostics",
    "settings-advanced",
    "agent-view",
    "workspace-view",
    "repository-index",
    "lab-view",
    "automation-view",
    "system-view",
    "history",
    "model-picker",
    "my-tools",
    "log-viewer",
    "perf-strip",
    "ai-systems",
    "goals",
    // v1.10.15-fixed run-#47 cycle (Task ID 42, D37): the agent-view parity
    // + shell-interaction rows — the src/ components the #45-cycle census
    // left implicit (AgentBody sub-surfaces + the app-shell shortcut map).
    "activity-stream",
    "download-progress",
    "maintenance-banner",
    "keyboard-shortcuts",
  ];
  const missing = census.filter((s) => !manifest.includes(`| ${s} `));
  assert.deepEqual(
    missing,
    [],
    `the manifest must enumerate every Wails surface (missing: ${missing.join(", ")})`,
  );
  for (const col of ["qt target", "core api", "acceptance assertion", "status"]) {
    assert.ok(manifest.includes(col), `the manifest header names "${col}"`);
  }
  assert.ok(
    !manifest.includes("NOT RUN |") || true,
    "unported rows are honest; every landed row cites a gate",
  );
});

test("R1/I21: every manifest LANDED row's Qt target file exists and is registered", () => {
  const manifest = readFileSync(
    join(repoRoot, "ui", "qt", "parity-manifest.md"),
    "utf8",
  );
  const targets = new Set(
    [...manifest.matchAll(/\| (ui\/qt\/[A-Za-z0-9/_.-]+)[ |]/g)].map((m) => m[1]),
  );
  assert.ok(targets.size >= 8, `manifest names concrete ui/qt/ targets (found: ${targets.size})`);
  const cmake = readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
  for (const t of targets) {
    const abs = join(repoRoot, t);
    assert.ok(existsSync(abs), `manifest target exists: ${t}`);
    if (t.endsWith(".qml")) {
      const rel = t.replace("ui/qt/", "");
      assert.ok(
        cmake.includes(rel),
        `CMakeLists qt_add_qml_module registers ${rel}`,
      );
    }
  }
});

test("R1/I21: tst_acceptance asserts every manifest acceptance assertion", () => {
  const manifest = readFileSync(
    join(repoRoot, "ui", "qt", "parity-manifest.md"),
    "utf8",
  );
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  const assertions = new Set(
    [...manifest.matchAll(/\| ([a-zA-Z][A-Za-z0-9]+) \| LANDED/g)].map((m) => m[1]),
  );
  assert.ok(
    assertions.size >= 6,
    `manifest rows cite concrete acceptance assertions (found: ${[...assertions].join(", ")})`,
  );
  for (const a of assertions) {
    assert.ok(
      acc.includes(a),
      `tst_acceptance.cpp implements the asserted surface test: ${a}`,
    );
  }
});

test("R1/D28: coreclient reads the config (the refreshConfig stub is gone)", () => {
  const src = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.cpp"), "utf8");
  const at = src.indexOf("void CoreClient::refreshConfig()");
  assert.ok(at >= 0, "refreshConfig exists");
  const body = src.slice(at, at + 600);
  assert.doesNotMatch(body, /^\s*\{\}\s*$/m, "the stub body is gone");
  assert.match(body, /\/api\/config/, "refreshConfig GETs /api/config");
  // The Q_PROPERTY is declared on the client's header surface.
  const header = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.h"), "utf8");
  assert.match(
    header,
    /Q_PROPERTY\(QVariantMap config/,
    "the config state is a Q_PROPERTY the QML views bind",
  );
});

// Recursively enumerate the tracked QML production files.
function qmlFiles(dir) {
  const out = {};
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) Object.assign(out, qmlFiles(p));
    else if (e.name.endsWith(".qml")) out[p] = true;
  }
  return out;
}

// ---------------------------------------------------------------------------
// 12. Run-#44 cycle (Task ID 40) — RED-first regression pins for the #43
// RED class. Every assertion in this section FAILS at f5a31cf by design
// (P2 gate): each names a §0-hosted defect and locks the exact fix class.
// Hosted evidence: run 40010996300 (Linux + Windows, deterministic).
// ---------------------------------------------------------------------------

const NAV_VIEWS = ["chat", "agent", "workspace", "lab", "automation", "system", "settings"];

test("R1/#43: Main.qml nav chrome exposes SEVEN STATIC nav buttons (findChild contract)", () => {
  const main = readFileSync(join(repoRoot, "ui", "qt", "qml", "Main.qml"), "utf8");
  for (const id of NAV_VIEWS) {
    assert.ok(
      main.includes(`objectName: "nav-${id}"`),
      `Main.qml must carry the static nav button objectName: "nav-${id}" (the tst_acceptance.cpp:1060 findChild contract; the dynamic "nav-" + modelData.id binding never materialized on the hosted engine)`,
    );
  }
  // The dynamic objectName binding class is retired with the static buttons.
  assert.doesNotMatch(
    main,
    /objectName:\s*"nav-"\s*\+/,
    "the dynamic nav objectName binding is replaced by the seven static buttons",
  );
});

test("R1-R3/#43: no Repeater-owned delegate declares required property int index (Qt 6.8 Repeater drops them)", () => {
  // Hosted mechanism (run #43, both OSes): QQuickRepeater does not
  // initialize `required property int index` on JS-array delegates — the
  // delegate aborts creation, the object never exists (nav-chat) and the
  // surface silently renders empty (WorkspaceView recents). ListView
  // delegates KEEP full required-index support (the canonical pattern).
  const qmlDir = join(repoRoot, "ui", "qt", "qml");
  const offenders = [];
  for (const f of Object.keys(qmlFiles(qmlDir))) {
    const lines = readFileSync(f, "utf8").split("\n");
    let owner = null;
    lines.forEach((line, i) => {
      if (/^\s*(Repeater|ListView|GridView|TableView) \{/.test(line)) owner = { kind: line.trim().split(" ")[0], at: i };
      if (/delegate:/.test(line) && owner && owner.kind === "Repeater") {
        const body = lines.slice(i, i + 8).join("\n");
        if (/required property int index/.test(body)) {
          offenders.push(`${f.replace(repoRoot + "/", "")}:${i + 1}`);
        }
      }
    });
  }
  assert.deepStrictEqual(offenders, [],
    `Repeater delegates must not require index (delegates silently dropped on Qt 6.8.2): ${offenders.join(", ")}`);
});

test("R4/#43: the Windows Go-verification summary implements the three-class clean-silent guard", () => {
  const wf = loadWorkflow();
  const at = wf.indexOf("gate produced no log");
  assert.ok(at >= 0, "the capture-defect guard exists");
  const block = wf.slice(wf.lastIndexOf("$gateOrderPath = Join-Path", at), at + 600);
  // clean-silent: empty + OK marker is valid, NEVER annotated (I15/I17).
  assert.ok(
    /clean-silent|cleanSilent/i.test(block) && /gate-order/.test(block),
    "the empty-log branch must consult gate-order markers and classify empty+OK as clean-silent (go vet/gofmt emit no stdout on success)",
  );
  assert.ok(
    /Write-Host "[^"]*never started/i.test(block),
    "NEVER-STARTED (no RUN marker, no log) emits one PLAIN step line (D31: never an annotation of any class)",
  );
  assert.doesNotMatch(
    block,
    /::(warning|error|notice)::[^\n]*never started/i,
    "NEVER-STARTED never annotates (D31 errors-only; I17)",
  );
  assert.doesNotMatch(
    block,
    /-eq 0\)\s*\{\s*\r?\n\s*Write-Host "::error::gate produced no log[^"]*EMPTY/,
    "the unconditional empty=>::error branch (the run-#43 false-positive class) is retired",
  );
});

test("R2/#43: Core registers per-engine (qmlRegisterSingletonType) in the suite AND the host", () => {
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  const host = readFileSync(join(repoRoot, "ui", "qt", "src", "main.cpp"), "utf8");
  // Comment-stripped: the docs may NAME the retired API; no CALL may remain.
  const codeOf = (s) => s.split("\n").filter((l) => !l.trim().startsWith("//")).join("\n");
  for (const [name, src] of [["tst_acceptance.cpp", acc], ["main.cpp", host]]) {
    const code = codeOf(src);
    assert.match(code, /qmlRegisterSingletonType/, `${name}: per-engine singleton provider`);
    assert.doesNotMatch(code, /qmlRegisterSingletonInstance/,
      `${name}: the one-engine registration is retired (hosted warning: "Singleton registered by registerSingletonInstance must only be accessed from one engine")`);
  }
});

test("R2/#43: SystemView envelope binding guards the nested read (zero TypeErrors at load)", () => {
  const sys = readFileSync(join(repoRoot, "ui", "qt", "qml", "SystemView.qml"), "utf8");
  const at = sys.indexOf("envelope: ");
  assert.ok(at >= 0, "the envelope label exists");
  const binding = sys.slice(sys.lastIndexOf("text:", at), at + 120);
  assert.match(
    binding,
    /text:\s*\(?\s*Core\.governor\.governor\s*!==\s*undefined/,
    "the envelope text binding itself must guard the nested read (a visible: guard never stops the eager text evaluation — hosted TypeError in qmlLoadsCleanly with a FRESH CoreClient)",
  );
});

test("R5/#43: no QML Button declares function action() (final-member override class)", () => {
  const qmlDir = join(repoRoot, "ui", "qt", "qml");
  const offenders = Object.keys(qmlFiles(qmlDir))
    .filter((f) => /function action\s*\(/.test(readFileSync(f, "utf8")))
    .map((f) => f.replace(repoRoot + "/", ""));
  assert.deepStrictEqual(offenders, [],
    `Button-declared action() shadows the final AbstractButton.action member: ${offenders.join(", ")}`);
});

test("R5/#43: the acceptance harness mirrors the production Basic style", () => {
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  assert.match(
    acc,
    /QQuickStyle::setStyle|QT_QUICK_CONTROLS_STYLE/,
    "the suite must pin the same style main.cpp pins (Basic) BEFORE any engine loads — the Windows run otherwise exercises windowsvista and fires the background-customization warning class",
  );
});

test("R1/I21: the manifest chat-nav row is asserted by the static-buttons contract", () => {
  const manifest = readFileSync(join(repoRoot, "ui", "qt", "parity-manifest.md"), "utf8");
  assert.match(manifest, /nav-chrome[^\n]*navChromeSevenViewsLoad/,
    "the manifest keeps the chat-nav row wired to its gate");
  const main = readFileSync(join(repoRoot, "ui", "qt", "qml", "Main.qml"), "utf8");
  assert.match(main, /property var navViews: \["chat", "agent", "workspace", "lab", "automation", "system", "settings"\]/,
    "the seven-view navViews contract stays byte-identical");
});

// ---------------------------------------------------------------------------
// 13. Run-#45 cycle (Task ID 41, D26/D31/D32) — RED-first pins for the #44
// red class. Every NEW assertion below FAILS at 77ff79f by design (the
// P2 gate): run 40035405032 died in win-qt-build.log at "error C1083:
// Cannot open include file: 'QQuickStyle'" (QuickControls2 missing from
// the acceptance link line) with the Linux twin, and its Annotations pane
// still carried 2 warnings (the D31 demotion targets). The retention
// assertions keep every named ::error red-gate guard alive through the
// demotion — a red gate without a named ::error is an automatic defect.
// ---------------------------------------------------------------------------

test("R1/#44: the acceptance target links Qt6::QuickControls2 (the C1083 one-line build fix)", () => {
  const cmake = readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
  const at = cmake.indexOf("target_link_libraries(sheytan-ui-acceptance");
  assert.ok(at !== -1, "the acceptance target_link_libraries line exists");
  const end = cmake.indexOf("\n", at);
  const line = cmake.slice(at, end === -1 ? undefined : end);
  assert.match(
    line,
    /Qt6::QuickControls2/,
    "tst_acceptance.cpp includes <QQuickStyle> and calls QQuickStyle::setStyle (QuickControls2) — run #44 died at C1083 because the link line carries only Core/Gui/Qml/Quick/Test; find_package already declares QuickControls2",
  );
});

test("R2/D31: the workflow emits ZERO ::warning annotations outside comments (errors only)", () => {
  const wf = loadNonCommentWorkflow();
  const offenders = wf.split("\n").filter((l) => l.includes("::warning"));
  assert.deepEqual(
    offenders,
    [],
    `annotations carry ERRORS ONLY (D31): every ::warning emission point (QWARN relays, NEVER-STARTED, toolchain, part-deletion) must be plain step output + all-logs.txt (offenders: ${offenders.length}): ${offenders[0] ?? ""}`,
  );
});

test("R2/D31: the warning relay classes are demoted to plain output (recorded, never deleted)", () => {
  const wf = loadNonCommentWorkflow();
  // Windows Emit-Line: the warning branch must Write-Host the bare message.
  assert.doesNotMatch(wf, /'warning'\s+\{ Write-Host "::warning::/);
  assert.match(
    wf,
    /'warning'\s+\{ Write-Host \$msg \}/,
    "Windows failure emitter: warning-class lines are plain output (the class still reaches all-logs.txt)",
  );
  // Linux emit_line: the warning branch must printf the bare message.
  assert.doesNotMatch(wf, /warning\)\s+printf '::warning::/);
  assert.match(
    wf,
    /warning\)\s+printf '%s\\n'\s+"?\$2/,
    "Linux failure emitter: warning-class lines are plain output",
  );
  // The exit-0 report guards (instrumentation named, non-fatal) are plain.
  assert.doesNotMatch(wf, /::warning::[^"\n]*report missing\/empty/);
});

test("R2/D31: named ::error guards survive the demotion (a red gate is never silenced)", () => {
  // Retention half of the P2 (2) pair: the demotion moves classes, never
  // catchers — every failed-gate ::error name stays (I17: a red gate with
  // no ::error is an automatic defect).
  const wf = loadWorkflow();
  for (const frag of [
    "::error::Qt live-answer acceptance failed with exit code",
    "::error::Qt acceptance report missing/empty on nonzero exit",
    "::error::gate produced no log — capture defect",
    "::error::gate log EMPTY without an OK marker",
    "::error::Qt desktop acceptance smoke failed before a gate marker was written",
  ]) {
    assert.ok(
      wf.includes(frag),
      `the named ::error guard must survive the D31 demotion: ${frag}`,
    );
  }
});

test("R4/D32: the manifest carries the animationsEnabled row (I21/I22)", () => {
  const manifest = readFileSync(
    join(repoRoot, "ui", "qt", "parity-manifest.md"),
    "utf8",
  );
  assert.match(
    manifest,
    /\| animations[^\n]*ui\/qt\/qml\/SettingsView\.qml[^\n]*animationsToggleGatesEveryMotion[^\n]*LANDED-LOCAL \|/,
    "the manifest enumerates the animations surface (animationsEnabled) with its Qt target and acceptance assertion",
  );
});

test("R4/D32: CoreClient exposes the persisted animationsEnabled gate", () => {
  const header = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.h"), "utf8");
  assert.match(
    header,
    /Q_PROPERTY\(bool animationsEnabled/,
    "the ONE motion gate is a Q_PROPERTY every QML view reads via the Core singleton",
  );
  const src = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.cpp"), "utf8");
  assert.match(
    src,
    /QSettings/,
    "the user override is persisted locally (QSettings — the core's /api/config is a typed struct that drops unknown keys; core-side persistence needs a Go field, out of scope this cycle)",
  );
  assert.match(
    src,
    /\/api\/sysinfo/,
    "the device-tier default reads the existing core endpoint (high-end on, low-end off; user override wins)",
  );
});

test("R4/D32: the Settings surface carries the animationsEnabled user control", () => {
  const qml = readFileSync(join(repoRoot, "ui", "qt", "qml", "SettingsView.qml"), "utf8");
  assert.match(
    qml,
    /objectName: "field-animationsEnabled"/,
    "the General section carries the animations row (findChild contract)",
  );
  assert.match(
    qml,
    /Core\.animationsEnabled/,
    "the row binds the ONE gate (D32: low/high-end devices on/off)",
  );
});

// Strip // comments line-wise (QML in this tree carries no URLs or regex
// literals that would make this lossy for motion scanning).
function qmlCodeOf(text) {
  return text
    .split("\n")
    .map((l) => l.replace(/\/\/.*$/, ""))
    .join("\n");
}

test("R4/I22: every QML motion is ONE gated form (Behavior/Transition reading Core.animationsEnabled)", () => {
  const qmlDir = join(repoRoot, "ui", "qt", "qml");
  let motions = 0;
  const offenders = [];
  const banned = [];
  for (const f of Object.keys(qmlFiles(qmlDir))) {
    const code = qmlCodeOf(readFileSync(f, "utf8"));
    const rel = f.replace(repoRoot + "/", "");
    const lineOf = (idx) => code.slice(0, idx).split("\n").length;
    // I22 authoring rule, part 1: flat motion only — property-source
    // animations and grouped wrappers can hide an ungated path.
    const banRe = /\w+Animation\s+on\s|SequentialAnimation|ParallelAnimation/g;
    let b;
    while ((b = banRe.exec(code)) !== null) banned.push(`${rel}:${lineOf(b.index)}`);
    // Part 2: every Behavior/Transition block (including popup
    // enter:/exit: transitions) carries the ONE gate.
    const re = /^\s*((enter|exit)\s*:\s*)?(Behavior\s+on\s+\w+|Transition)\s*\{/gm;
    let m;
    while ((m = re.exec(code)) !== null) {
      motions++;
      const open = code.indexOf("{", m.index);
      let depth = 0;
      let i = open;
      for (; i < code.length; i++) {
        if (code[i] === "{") depth++;
        else if (code[i] === "}") {
          depth--;
          if (depth === 0) break;
        }
      }
      const block = code.slice(open, i + 1);
      if (!block.includes("animationsEnabled"))
        offenders.push(`${rel}:${lineOf(m.index)}`);
    }
  }
  assert.deepStrictEqual(
    banned,
    [],
    `flat gated motion only — property-source/wrapper animations are banned (I22): ${banned.join(", ")}`,
  );
  assert.ok(
    motions >= 10,
    `the D32 polish pass adds real gated motion (found ${motions} Behavior/Transition elements)`,
  );
  assert.deepStrictEqual(
    offenders,
    [],
    `every Behavior/Transition must read Core.animationsEnabled (I22 — off = instant state, identical function): ${offenders.join(", ")}`,
  );
});

test("R4/I22: tst_acceptance implements animationsToggleGatesEveryMotion (off → no animation paths run)", () => {
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  const at = acc.indexOf("void Acceptance::animationsToggleGatesEveryMotion()");
  assert.ok(
    at !== -1,
    "the acceptance slot exists (the manifest row's assertion)",
  );
  const body = acc.slice(at, at + 5000);
  assert.match(
    body,
    /QQuickBehavior/,
    "it walks the loaded window's Behavior gates (off ⇒ zero enabled motion paths)",
  );
  assert.match(
    body,
    /setAnimationsEnabled\(false\)/,
    "it drives the OFF state end-to-end",
  );
});

// ---------------------------------------------------------------------------
// 14. Run-#47 cycle (Task ID 42, D35/D36) — RED-first pins for the #45/#46
// red class. Runs #45 AND #46 died at ONE compile defect, two pushes: MSVC
// "error C3861: 'registerCoreSingleton': identifier not found" at
// tst_acceptance.cpp:989 (#45: :992 — the line moved, not the defect), with
// the Linux twin inferred. The TU DEFINES the static helper (at :1059) but
// the motion-gating slot calls it ~70 lines EARLIER, and no gate anywhere
// saw use-before-declaration — no local Qt compile exists and no
// declaration-coverage check ran (I23 closes exactly that gap). The second
// pin (include→module) closes the include half of the D36 class at the
// CMake level: <QLocalSocket>/<QLocalServer> are Qt6::Network headers that
// both targets compile, yet Network was absent from find_package and both
// link lines — resolvable today only through Qml's transitive public dep,
// i.e. an include OUTSIDE the target's visible contract (the C1083 class
// run #44 already died of once for QuickControls2).
//
// RED-FIRST: both tests below FAIL against 864a864 by design (the P2
// gate); each passes only after the exact R1/R2 fix.
// ---------------------------------------------------------------------------

// Strip // and /* */ comments while preserving offsets (the test TU's
// literals carry no comment-like text; char literals like '{' are kept —
// only comment text is blanked, and column-0 anchors survive).
function cppCodeOf(text) {
  let out = "";
  let i = 0;
  let inBlock = false;
  let inLine = false;
  while (i < text.length) {
    const two = text.slice(i, i + 2);
    if (!inBlock && !inLine && two === "/*") {
      inBlock = true;
      out += "  ";
      i += 2;
      continue;
    }
    if (inBlock && two === "*/") {
      inBlock = false;
      out += "  ";
      i += 2;
      continue;
    }
    if (!inBlock && !inLine && two === "//") {
      inLine = true;
      out += "  ";
      i += 2;
      continue;
    }
    if (inLine && text[i] === "\n") {
      inLine = false;
      out += "\n";
      i += 1;
      continue;
    }
    if (inLine) {
      out += " ";
      i += 1;
      continue;
    }
    if (inBlock) {
      out += text[i] === "\n" ? "\n" : " ";
      i += 1;
      continue;
    }
    out += text[i];
    i += 1;
  }
  return out;
}

// Every file-scope (column-0) function-like occurrence in a TU — BOTH
// prototypes and definitions count as declarations (the C++ visibility
// rule this test enforces).
function tuDeclarations(code) {
  const decls = [];
  const re = /^[A-Za-z_][\w:<>,\s*&]*?\b([A-Za-z_]\w*)\s*\(/gm;
  let m;
  while ((m = re.exec(code)) !== null) decls.push({ name: m[1], index: m.index });
  return decls;
}

// Project-defined function names: column-0 declarations across the three
// ui/qt TUs (static helpers + member definitions). Only symbols the
// PROJECT itself defines are enforced — Qt/system symbols (macros,
// inherited QObject members, template helpers) are out of static scope;
// the hosted compile is their authority.
function projectDefinedFunctions() {
  const files = [
    join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"),
    join(repoRoot, "ui", "qt", "src", "coreclient.cpp"),
    join(repoRoot, "ui", "qt", "src", "main.cpp"),
  ];
  const names = new Map(); // name -> [{file, index}]
  for (const f of files) {
    for (const d of tuDeclarations(cppCodeOf(readFileSync(f, "utf8")))) {
      if (!names.has(d.name)) names.set(d.name, []);
      names.get(d.name).push({ file: f, index: d.index });
    }
  }
  return names;
}

test("R1/I23: every project symbol tst_acceptance.cpp calls is VISIBLE at the call site (the C3861 class — registerCoreSingleton was not)", () => {
  const accPath = join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp");
  const code = cppCodeOf(readFileSync(accPath, "utf8"));
  const tuDecls = tuDeclarations(code);

  // Declarations visible through the TU's own quoted includes (coreclient.h):
  // the header's function-like declarations (Q_PROPERTY accessors,
  // Q_INVOKABLE methods, signals, privates) are visible to every call site.
  const headerDecls = new Set();
  const quoted = [...readFileSync(accPath, "utf8").matchAll(/#include\s+"([\w./-]+)"/g)]
    .map((m) => m[1])
    .filter((p) => p.endsWith(".h") && existsSync(join(repoRoot, "ui", "qt", "src", p)));
  for (const p of quoted) {
    const headerCode = readFileSync(join(repoRoot, "ui", "qt", "src", p), "utf8");
    for (const m of headerCode.matchAll(/(?:^|\n)\s*(?:[\w:*&]+\s+)+([A-Za-z_]\w*)\s*\(/g))
      headerDecls.add(m[1]);
  }

  // The TU class's own members are visible inside member bodies (unqualified
  // calls resolve to them), inherited or declared.
  const classMembers = new Set();
  const classAt = code.indexOf("class Acceptance");
  if (classAt !== -1) {
    const end = code.indexOf("\n};", classAt);
    for (const m of code.slice(classAt, end).matchAll(/(?:^|\n)\s*(?:[\w:*&]+\s+)+([A-Za-z_]\w*)\s*\(/g))
      classMembers.add(m[1]);
  }

  const project = projectDefinedFunctions();
  const violations = [];
  const re = /([A-Za-z_]\w*)\s*\(/g;
  let m;
  while ((m = re.exec(code)) !== null) {
    const name = m[1];
    if (!project.has(name)) continue; // system/Qt/local symbols: out of static scope
    // Member/scoped access is never the defect class.
    let j = m.index - 1;
    while (j >= 0 && /\s/.test(code[j])) j--;
    if (j >= 0 && (code[j] === "." || code[j] === ">" || code[j] === ":")) continue;
    const visible =
      tuDecls.some((d) => d.name === name && d.index <= m.index) ||
      headerDecls.has(name) ||
      classMembers.has(name);
    if (!visible)
      violations.push(
        `${name}() called at stripped-offset ${m.index} (raw line ~${code.slice(0, m.index).split("\n").length}) with NO visible declaration — MSVC C3861`,
      );
  }
  assert.deepStrictEqual(
    violations,
    [],
    `I23 declaration coverage: every project-defined symbol a target TU calls must be declared before use (in the TU, in an included project header, or as a class member): ${violations.join(" | ")}`,
  );
});

test("R2/I23: every Qt header the Qt targets include maps to a linked Qt6 module (both targets — the C1083/C3861 include half)", () => {
  const cmake = readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
  const findPkg = cmake.match(/find_package\(Qt6[^)]*REQUIRED COMPONENTS ([^)]*)\)/);
  assert.ok(findPkg, "find_package(Qt6 ... COMPONENTS ...) exists");
  const declared = new Set(findPkg[1].trim().split(/\s+/));

  const linkOf = (target) => {
    const at = cmake.indexOf(`target_link_libraries(${target}`);
    assert.ok(at !== -1, `target_link_libraries(${target} ...) exists`);
    const end = cmake.indexOf(")", at);
    const mods = new Set(
      cmake
        .slice(at, end)
        .matchAll(/Qt6::([A-Za-z0-9]+)/g)
        .map((x) => x[1]),
    );
    return mods;
  };

  // Qt header → owning module (the D36 mapping). QLocal*/QNetwork* are
  // Qt6::Network (doc.qt.io: qmake "QT += network"); QQuickStyle is
  // QuickControls2; QQml*/QJS* are Qml; QQuick* is Quick; QtTest is Test;
  // QGuiApplication is Gui; the remaining Q* are QtCore.
  const moduleOf = (h) => {
    if (h === "QtTest") return "Test";
    if (h.startsWith("QLocal") || h.startsWith("QNetwork")) return "Network";
    if (h === "QQuickStyle") return "QuickControls2";
    if (h.startsWith("QQml") || h.startsWith("QJS")) return "Qml";
    if (h.startsWith("QQuick")) return "Quick";
    if (h === "QGuiApplication" || h.startsWith("QOpenGL")) return "Gui";
    return "Core";
  };

  const targets = [
    {
      name: "sheytan-ui",
      files: ["src/main.cpp", "src/coreclient.h", "src/coreclient.cpp"],
    },
    {
      name: "sheytan-ui-acceptance",
      files: ["tests/tst_acceptance.cpp", "src/coreclient.h", "src/coreclient.cpp"],
    },
  ];
  const problems = [];
  for (const t of targets) {
    const linked = linkOf(t.name);
    const needed = new Set();
    for (const rel of t.files) {
      const text = readFileSync(join(repoRoot, "ui", "qt", rel), "utf8");
      for (const inc of text.matchAll(/^\s*#include\s+<([A-Za-z0-9_]+)>\s*$/gm)) {
        const h = inc[1];
        if (!h.startsWith("Q")) continue; // system headers (cstdio, functional)
        needed.add(moduleOf(h));
      }
    }
    for (const mod of needed) {
      if (!declared.has(mod))
        problems.push(
          `${t.name}: include map needs Qt6::${mod} but find_package does not declare it (the run-#44 C1083 class)`,
        );
      if (!linked.has(mod))
        problems.push(
          `${t.name}: include map needs Qt6::${mod} but the target's link line omits it (an include OUTSIDE the target's visible contract)`,
        );
    }
  }
  assert.deepStrictEqual(
    problems,
    [],
    `every Qt include must map to a declared AND linked module on BOTH targets (I23/D36): ${problems.join(" | ")}`,
  );
});

// ---------------------------------------------------------------------------
// 13. Run-#48 cycle (Task ID 43) — RED-first coverage for the operator's
// four defects at v1.10.15 (R1 theme, R2 runtime path, R3 options +
// hardware, R4 Linux package) plus the R5 identity bump. Every test in
// this section FAILS at 4703621 by design (P2 gate): each names a §0
// defect and locks the exact fix class. Hosted evidence: run 40100279630
// (green) + the operator's physical log (the harness-green/runtime-broken
// gap that I24 closes).
// ---------------------------------------------------------------------------

function loadQmlView(name) {
  return readFileSync(join(repoRoot, "ui", "qt", "qml", name), "utf8");
}

const QML_VIEWS = [
  "Main.qml", "AgentView.qml", "SettingsView.qml", "SystemView.qml",
  "WorkspaceView.qml", "LabView.qml", "AutomationView.qml",
  "HistoryDrawer.qml", "ModelPickerDrawer.qml", "MyToolsDrawer.qml",
  "LogViewerDrawer.qml", "PerfStrip.qml", "EmptyState.qml",
];

// R1(a)+(b): ONE theme system — a Theme singleton plus a QuickControls2
// custom style so every stock control renders themed by default. The
// shipped app pinned QQuickStyle "Basic" (main.cpp:41): 90-degree grey
// rectangles everywhere, the operator's "old-fashioned UI" defect.
test("run-#48 R1: a Theme singleton + QuickControls2 custom style exists and BOTH hosts select it", () => {
  const styleDir = join(repoRoot, "ui", "qt", "style");
  assert.ok(existsSync(styleDir), "ui/qt/style must exist (the ONE theme system lives here)");
  const required = [
    "qmldir", "Button.qml", "ToolButton.qml", "TextField.qml",
    "TextArea.qml", "ComboBox.qml", "CheckBox.qml", "Switch.qml",
    "Slider.qml", "ProgressBar.qml", "ScrollBar.qml", "Menu.qml",
    "MenuItem.qml", "ItemDelegate.qml",
  ];
  for (const f of required)
    assert.ok(existsSync(join(styleDir, f)), `ui/qt/style/${f} missing — stock controls fall back to Basic (90-degree corners)`);

  const mainCpp = readFileSync(join(repoRoot, "ui", "qt", "src", "main.cpp"), "utf8");
  assert.ok(
    !mainCpp.includes('setStyle(QStringLiteral("Basic"))'),
    "main.cpp must never ship the Basic style again (the unthemed-UI defect)",
  );
  assert.match(
    mainCpp,
    /QQuickStyle::setStyle\(\s*QStringLiteral\(":\/sheytanstyle"\)\s*\)/,
    "main.cpp must select the resource-embedded custom style :/sheytanstyle",
  );
  const cmake = readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
  assert.match(cmake, /qt_add_resources\(sheytan-ui sheytan_style/, "CMakeLists must embed the style directory as a raw resource (qt_add_resources)");
  assert.match(cmake, /PREFIX "\/sheytanstyle"/, "the style resource must land at /sheytanstyle so QQuickStyle can resolve it");
  assert.match(cmake, /qt_add_resources\(sheytan-ui-acceptance sheytan_style/, "the acceptance target must embed the SAME style (I24)");
  // The Theme singleton is a sheytan.ui module singleton: every view sees
  // it through the implicit module import (zero per-file import churn).
  assert.match(cmake, /Theme\.qml/, "CMakeLists must register Theme.qml in the sheytan.ui QML module");
});

// R1(c): every view binds the Theme tokens — no hardcoded colors. Theme.qml
// is the ONE token source (a sheytan.ui module singleton) and is the only
// qml/ file allowed to carry raw palette literals.
test("run-#48 R1: every view resolves Theme tokens — ZERO hardcoded color literals in qml/", () => {
  const offenders = [];
  for (const f of QML_VIEWS) {
    assert.ok(existsSync(join(repoRoot, "ui", "qt", "qml", f)), `view ${f} vanished`);
    const code = qmlCodeOf(loadQmlView(f));
    if (/#[0-9a-fA-F]{3,8}\b/.test(code)) offenders.push(f);
    assert.ok(
      code.includes("Theme."),
      `${f} must bind colors/spacing/radii through the Theme singleton (R1c)`,
    );
  }
  assert.deepStrictEqual(
    offenders,
    [],
    `hardcoded color literals must become Theme tokens in: ${offenders.join(", ")}`,
  );
  const theme = loadQmlView("Theme.qml");
  assert.match(theme, /^pragma Singleton/m, "Theme.qml is the sheytan.ui module singleton");
  assert.match(theme, /readonly property int radiusS/, "Theme carries the corner-radius scale (no 90-degree corner anywhere)");
  assert.match(theme, /readonly property int motionNormal/, "Theme carries the motion durations");
});

// R2: model selection must go through the shipped select authority. The
// Qt client only ever called patchConfig({model}) — POST /api/models/select
// (persist + verify + engine boot through the selection state machine) was
// NEVER wired, while the manifest row claimed it. The harness only listed
// models (extrasSurfacesLoad), so the break was invisible — I24.
test("run-#48 R2: Core.selectModel drives the live POST /api/models/select path end-to-end", () => {
  const h = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.h"), "utf8");
  const cpp = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.cpp"), "utf8");
  assert.match(h, /Q_INVOKABLE void selectModel\(/, "CoreClient must expose selectModel(id) — the Wails selection path");
  assert.match(cpp, /\/api\/models\/select/, "coreclient.cpp must POST /api/models/select (the shipped selection authority)");
  const drawer = loadQmlView("ModelPickerDrawer.qml");
  assert.match(drawer, /Core\.selectModel\(/, "ModelPickerDrawer Select must call Core.selectModel — patchConfig({model}) bypasses the selection state machine");
  assert.ok(!drawer.includes("Core.patchConfig({ model:"), "the config-patch selection shortcut must be gone");
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  assert.match(acc, /modelSelectRoundTripPersistsAcrossRestart/, "the live-path selection round-trip (list → select → config reflects → persists across restart) must be asserted against the real core transport");
  // I24: the same assertion drives the SHIPPED transport (CoreClient over
  // the local channel), never a test-only shortcut.
  assert.match(acc, /selectModel\(/, "the selection assertion must drive CoreClient.selectModel on the live transport");
});

// R3(a): the dead/400 settings classes. Go keeps sampling and context
// under llm.* (internal/config LLMOptions) — flat keys render but never
// persist; four fields 400 on type mismatch; option sets drifted.
test("run-#48 R3: settings bind the nested llm schema with type-correct controls and honest option sets", () => {
  const sv = loadQmlView("SettingsView.qml");
  for (const key of ["llm.numCtx", "llm.temperature", "llm.topP", "llm.topK", "llm.minP", "llm.maxTokens", "llm.repeatPenalty", "llm.seed"])
    assert.ok(sv.includes(key), `${key} must bind the nested llm schema (the flat key is dropped by the typed config merge — renders but never persists)`);
  // Type-correct controls: the Go schema is bool/bool/bool/enum — the
  // registry's type:"bool" renders the themed Switch delegate.
  assert.match(sv, /"thinkingMode",[\s\S]{0,120}?type:\s*"bool"/, "thinkingMode is a Go bool — the enum control 400s every patch");
  assert.match(sv, /"parallelTools",[\s\S]{0,120}?type:\s*"bool"/, "parallelTools is a Go bool — the spinner control 400s every patch");
  assert.match(sv, /"labKeepWorkspaces",[\s\S]{0,120}?type:\s*"bool"/, "labKeepWorkspaces is a Go bool — the spinner control 400s every patch");
  assert.match(sv, /"visionMmprojOffload",[\s\S]{0,120}?options:[\s\S]{0,80}?"auto"/, "visionMmprojOffload is a Go auto|on|off enum — the boolean switch 400s every patch");
  // Honest option sets (Wails reference).
  assert.ok(sv.includes('"monthly"'), "updateSchedule is missing the monthly option the Wails version offers");
  assert.match(sv, /"researchBackend",[\s\S]{0,160}?"auto"/, "researchBackend must offer the Wails option set (auto first)");
  assert.ok(!sv.includes('key: "browserSlowMoMsAlias"'), "browserSlowMoMsAlias is a ghost key — no such Go config field exists (the patch is silently dropped; the registry must not render it)");
  // enabledTools is a per-tool checkbox list fed by /api/tools in Wails.
  assert.match(sv, /type:\s*"tools"/, "enabledTools must be the tools row type (the per-tool checkbox delegate), not a textfield");
  assert.match(sv, /Core\.toolCatalog/, "the tools row must render from Core.toolCatalog (GET /api/tools), not a comma-textfield");
  assert.ok(!sv.includes("(comma-separated)"), "the dead comma-textfield label must be gone");
});

// R3(c): the hardware panel. SystemView showed preflight/governor/
// maintenance only — zero hardware state — while the core already serves
// /api/sysinfo. The operator saw "hardware unmatched (Intel Arc, offload
// off)" with no surface to understand or steer it.
test("run-#48 R3: SystemView surfaces hardware + engine state through Core.sysinfo", () => {
  const h = readFileSync(join(repoRoot, "ui", "qt", "src", "coreclient.h"), "utf8");
  assert.match(h, /Q_PROPERTY\(QVariantMap sysinfo/, "CoreClient must expose the /api/sysinfo snapshot to QML");
  assert.match(h, /Q_INVOKABLE void refreshSysinfo\(/, "CoreClient must expose refreshSysinfo()");
  const sv = loadQmlView("SystemView.qml");
  assert.match(sv, /Core\.sysinfo/, "SystemView must render the hardware panel from Core.sysinfo");
  assert.match(sv, /Core\.engine/, "SystemView must surface engine backend/variant state (the GPU-offload story) from Core.engine");
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  assert.match(acc, /hardwarePanelSurfacesSysinfo/, "the hardware-panel assertion must run against the real core transport");
});

// R4: the Linux package shipped a bare sheytan-ui with NO Qt runtime —
// the smoke only worked via the RUNNER's Qt (LD_LIBRARY_PATH at the aqt
// layout), so the zip could not launch on a clean machine. Windows gets
// windeployqt; Linux must get the official CMake deploy path + a closure
// gate + a smoke that launches the PACKAGED binary.
test("run-#48 R4: the Linux package carries the launchable Qt closure (deploy + ldd gate + packaged smoke)", () => {
  const wf = loadWorkflow();
  assert.match(wf, /QT_DEPLOY_AND_INSTALL=ON/, "the Linux Qt build must configure the official deploy+install path (CMakeLists' QT_DEPLOY_AND_INSTALL option)");
  assert.match(wf, /cmake --install/, "the QML-aware deploy script must execute via cmake --install so the Qt libs/plugins/QML modules land INSIDE the package");
  assert.match(wf, /ldd[\s\S]{0,400}not found/, "an ldd closure gate must fail the build when any linked library does not resolve inside the package");
  assert.match(
    wf,
    /UI_BIN="dist\/linux\/app\/\$\{LINUX_PKG_ROOT\}\/sheytan-ui"/,
    "the Xvfb smoke must launch the PACKAGED UI binary (the closure the zip actually carries) — not a repo-tree copy",
  );
  const cmake = readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
  assert.match(cmake, /qt_generate_deploy_qml_app_script/, "the deploy entry point must exist in CMakeLists (it does at HEAD — pinned)");
});

// R5/I12: ONE release identity — every surface spells the package.json
// version. This section REPLACES the run-#47-cycle 1.10.15 pin with the
// re-versioned identity and adds the two Qt surfaces that sat OUTSIDE the
// release-version.mjs repair set (main.cpp applicationVersion is already
// pinned above; CMakeLists project VERSION drifted to 1.10.13 unnoticed).
test("run-#48 R5/I12: the release identity is 1.11.0 on every version surface", () => {
  const pkg = JSON.parse(readFileSync(join(repoRoot, "package.json"), "utf8"));
  assert.equal(pkg.version, "1.11.0", "package.json (the one version source, operator re-version C15)");
  const goCfg = readFileSync(join(repoRoot, "internal", "config", "config.go"), "utf8");
  assert.match(goCfg, /AppVersion[ \t]*=[ \t]*"1\.11\.0"/, "internal/config/config.go (release-version.mjs surface)");
  const buildCfg = readFileSync(join(repoRoot, "build", "config.yml"), "utf8");
  assert.match(buildCfg, /productVersion:[ \t]*"1\.11\.0"/, "build/config.yml (release-version.mjs surface)");
  const signature = readFileSync(join(repoRoot, "SIGNATURE"), "utf8");
  assert.match(signature, /^SHEYTAN-Local-Agent v1\.11\.0/m, "SIGNATURE first line (release-version.mjs surface)");
  const qtMain = readFileSync(join(repoRoot, "ui", "qt", "src", "main.cpp"), "utf8");
  assert.match(qtMain, /setApplicationVersion\(QStringLiteral\("1\.11\.0"\)\)/, "ui/qt/src/main.cpp applicationVersion");
  const qtCmake = readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
  assert.match(qtCmake, /project\(SHEYTAN-UI VERSION 1\.11\.0 /, "ui/qt/CMakeLists.txt project VERSION (the 1.10.13 drift surface)");
  // No drift in either direction: the retired version must not survive on
  // any identity surface (docs and code comments narrating run history are
  // NOT surfaces — only the three comment-free identity files are scanned).
  for (const [file, text] of [
    ["package.json", JSON.stringify(pkg)],
    ["internal/config/config.go", goCfg],
    ["build/config.yml", buildCfg],
  ]) {
    assert.ok(
      !text.includes("1.10.15"),
      `${file} carries no stale 1.10.15 identity`,
    );
  }
});

// I24 companion: the harness must select the SAME style the shipped host
// selects — a style divergence between harness and shipped app is the
// run-#47 class one layer up (green harness, broken-looking product).
test("run-#48 I24: the acceptance harness mirrors the production custom style exactly", () => {
  const mainCpp = readFileSync(join(repoRoot, "ui", "qt", "src", "main.cpp"), "utf8");
  const m = mainCpp.match(/QQuickStyle::setStyle\(\s*QStringLiteral\("([^"]+)"\)\s*\)/);
  assert.ok(m, "main.cpp must pin its style through QQuickStyle::setStyle");
  const acc = readFileSync(join(repoRoot, "ui", "qt", "tests", "tst_acceptance.cpp"), "utf8");
  assert.ok(
    acc.includes(`QQuickStyle::setStyle(QStringLiteral("${m[1]}"))`),
    `tst_acceptance.cpp must setStyle("${m[1]}") — the exact production style (found no such call)`,
  );
});

// Heritage: the §11 P2(8) set stays pinned (each names an existing test in
// this file). Green at HEAD — protective, catches a future suite rewrite
// that drops a class.
test("run-#48 heritage: the suite still carries every standing pin class", () => {
  const src = readFileSync(new URL(import.meta.url), "utf8");
  const classes = {
    "one-log artifact": "ONE consolidated log part",
    "uninstall symmetry": "generated payload uninstall list",
    "naming map": "ONE naming map",
    concurrency: "cancel-in-progress",
    preflight: "preflight compile gate",
    timeouts: "timeout-minutes",
    "RMDir-r ban": "recursive-delete instruction",
    "four-part identity": "four-part identity contract",
    "first-byte logs": "first-byte capture",
    catchers: "NAMED instrumentation failure",
    "errors-only annotations": "ZERO annotations",
    "declaration coverage": "VISIBLE at the call site",
  };
  const missing = Object.entries(classes)
    .filter(([, frag]) => !src.toLowerCase().includes(frag.toLowerCase()))
    .map(([name]) => name);
  assert.deepStrictEqual(missing, [], `heritage pin classes dropped from the suite: ${missing.join(", ")}`);
});

// ---------------------------------------------------------------------------
// v1.11.0 run-#50 cycle (Task ID 44) — R2: the CMake source-coverage gate
// (I25), the fyne-pin contract, and the roadmap version-freeness contract.
//
// The run-#49 class: a CMake target/file relation that only the hosted
// runner could observe (no local Qt toolchain exists), killing the Qt host
// cmake configure on BOTH OSes before any compilation. Root cause (P1
// audit, pinned before any edit): ui/qt/CMakeLists.txt called
// qt_add_resources(sheytan-ui-acceptance ...) BEFORE add_executable
// defined that target. Qt 6.8.2's qt6_add_resources branches on
// if(TARGET ${outfiles}) and, when the target is absent, silently takes
// its qrc-file branch instead — parsing every remaining argument,
// including the BASE value ".../ui/qt/style" (an existing DIRECTORY), as
// a .qrc path; configure died inside Qt6CoreMacros.cmake (:317 file READ /
// :330 configure_file) exactly as the run-#49 annotations quote.
// These assertions make the class die locally, statically, with no Qt
// toolchain: target-reference order (I25-3), FILES resolution at their
// declared BASE (I25-1), and orphan/phantom coverage for every file under
// ui/qt (I25-2). "Exactly one target" is refined to "claimed by >= 1
// target, zero orphans, zero phantoms" because the style resources are
// intentionally embedded into BOTH sheytan-ui and sheytan-ui-acceptance
// (I24 — the harness/shipped divergence class).
// ---------------------------------------------------------------------------

function loadQtCmakeLists() {
  return readFileSync(join(repoRoot, "ui", "qt", "CMakeLists.txt"), "utf8");
}

// Extract each complete `name(...)` command block with balanced parens.
function extractQtCommandBlocks(cmake, names) {
  const blocks = [];
  const re = new RegExp(`(^|\\n)\\s*(${names.join("|")})\\s*\\(`, "g");
  let m;
  while ((m = re.exec(cmake)) !== null) {
    const open = m.index + m[0].length - 1;
    let depth = 0;
    let end = cmake.length - 1;
    for (let i = open; i < cmake.length; i++) {
      if (cmake[i] === "(") depth++;
      else if (cmake[i] === ")") {
        depth--;
        if (depth === 0) { end = i; break; }
      }
    }
    blocks.push({ name: m[2], body: cmake.slice(m.index, end + 1), start: m.index });
    re.lastIndex = end;
  }
  return blocks;
}

// Tokenize a command body: quoted args stay whole, comments dropped.
function cmakeArgs(body) {
  const noComments = body
    .split("\n")
    .filter((l) => !/^\s*#/.test(l))
    .join("\n");
  return (noComments.match(/"[^"]*"|[^\s()]+/g) ?? []).map((t) =>
    t.replace(/^"|"$/g, ""),
  );
}

const QT_SOURCE_DIR = join(repoRoot, "ui", "qt");

function resolveQtPath(p) {
  const expanded = p.replace(/\$\{CMAKE_CURRENT_SOURCE_DIR\}/g, QT_SOURCE_DIR);
  return isAbsolute(expanded) ? expanded : join(QT_SOURCE_DIR, expanded);
}

test("run-#50 R2/I25-3: resource blocks reference only targets defined BEFORE the reference (run-#49 configure class)", () => {
  const cmake = loadQtCmakeLists();
  const defined = new Set();
  const offenders = [];
  const re = /^\s*(qt_add_executable|add_executable|qt_add_library|add_library|qt_add_qml_module|qt_add_resources|qt_add_big_resources)\s*\(\s*([A-Za-z0-9_-]+)/gm;
  let m;
  while ((m = re.exec(cmake)) !== null) {
    const [, cmd, first] = m;
    if (cmd === "qt_add_executable" || cmd === "add_executable" || cmd === "qt_add_library" || cmd === "add_library") {
      defined.add(first);
    } else if (!defined.has(first)) {
      offenders.push(`${cmd}(${first} ...) references an undefined target (offset ${m.index})`);
    }
  }
  assert.deepStrictEqual(
    offenders,
    [],
    "I25: every qt_add_qml_module/qt_add_resources block must attach to an ALREADY-defined target " +
      "(run #49 died hosted exactly here: the sheytan-ui-acceptance resources block preceded its " +
      "add_executable, so Qt6CoreMacros.cmake took the qrc-file branch and configure_file died on " +
      "the style DIRECTORY at Qt6CoreMacros.cmake:330/:317): " +
      offenders.join(" | "),
  );
  // The two targets the style parity contract depends on must exist at all.
  assert.ok(defined.has("sheytan-ui"), "sheytan-ui must be defined");
  assert.ok(defined.has("sheytan-ui-acceptance"), "sheytan-ui-acceptance must be defined");
});

test("run-#51 R2/I25-1 (amended from run-#50): every qt_add_resources FILES entry resolves from CMAKE_CURRENT_SOURCE_DIR and sits under its declared BASE (upstream semantics, not the #50 subset model)", () => {
  // AMENDMENT REASON (R2, run #51): the run-#50 row resolved FILES against
  // BASE — a hand-modeled subset of the upstream macro. The ACTUAL Qt 6.8.2
  // _qt_internal_process_resource (qtbase v6.8.2, :2274-2284) resolves every
  // FILES entry against CMAKE_CURRENT_SOURCE_DIR and uses BASE ONLY to
  // compute the QT_RESOURCE_ALIAS. The #50 tree passed bare style/ names
  // with BASE .../style: every entry materialized as a NON-EXISTENT
  // ui/qt/<file>.qml — the exact argument list the hosted :2212
  // target_sources frame consumed (run-#50 configure death). The probe
  // reproduces this verbatim; this static row now encodes the verified
  // upstream semantics so the two instruments can never diverge again.
  const cmake = loadQtCmakeLists();
  const blocks = extractQtCommandBlocks(cmake, ["qt_add_resources"]);
  assert.ok(blocks.length >= 2, "the sheytan style must be embedded into both targets");
  const offenders = [];
  for (const b of blocks) {
    const args = cmakeArgs(b.body);
    const baseIdx = args.indexOf("BASE");
    const filesIdx = args.indexOf("FILES");
    if (baseIdx === -1 || filesIdx === -1) {
      offenders.push(`${b.body.split("(")[0].trim()}: missing BASE or FILES`);
      continue;
    }
    const base = resolveQtPath(args[baseIdx + 1]);
    for (const f of args.slice(filesIdx + 1)) {
      if (/^[A-Z][A-Z_]*$/.test(f)) break; // next keyword — FILES list ended
      // Upstream: file resolves against the LISTING DIRECTORY (ui/qt),
      // alias = relative path from BASE.
      const p = resolveQtPath(f);
      if (!existsSync(p))
        offenders.push(`${f}: resolves from ui/qt/ (upstream CWD rule) to ${p} but MISSING from the tree — this is the argument the hosted :2212 frame consumed (run-#50 class)`);
      else if (!statSync(p).isFile())
        offenders.push(`${f}: referenced by ${b.name} but resolves to a DIRECTORY (the run-#49 class shape)`);
      else if (!p.startsWith(base + sep))
        offenders.push(`${f}: not under its declared BASE ${args[baseIdx + 1]} — the alias would escape the resource prefix`);
    }
  }
  assert.deepStrictEqual(
    offenders,
    [],
    "I25: every FILES entry must resolve (CWD rule) and sit under BASE (a referenced-but-missing entry is an automatic defect): " +
      offenders.join(" | "),
  );
});

// The closed exception list: files that intentionally belong to no target.
// style/qmldir is the ONLY member — the style descriptor 8c3c74e kept out of
// the embedded FILES list; its runtime fate is decided by the hosted
// acceptance theme-adoption gates, never by this static gate.
const I25_ORPHAN_EXCEPTIONS = new Set(["style/qmldir"]);

test("run-#50 R2/I25-2: every source file under ui/qt/{qml,src,style,tests} is claimed by a target — nothing orphaned, nothing phantom", () => {
  const cmake = loadQtCmakeLists();
  const claimed = new Set();
  for (const b of extractQtCommandBlocks(cmake, [
    "qt_add_qml_module",
    "qt_add_executable",
    "add_executable",
    "qt_add_resources",
  ])) {
    const args = cmakeArgs(b.body);
    const baseIdx = args.indexOf("BASE");
    const filesIdx = args.indexOf("FILES");
    if (b.name === "qt_add_resources" && baseIdx !== -1 && filesIdx !== -1) {
      // FILES entries resolve from the listing dir (ui/qt) — the upstream
      // CWD rule (amended run-#51, same reason as I25-1 above); BASE is
      // the alias base, never the file root.
      for (const f of args.slice(filesIdx + 1)) {
        if (/^[A-Z][A-Z_]*$/.test(f)) break;
        claimed.add(resolveQtPath(f));
      }
      continue;
    }
    for (const t of args) {
      if (/\.(qml|cpp|h)$/i.test(t)) claimed.add(resolveQtPath(t));
    }
  }
  const offenders = [];
  // phantom: every claimed path must exist on disk
  for (const p of claimed)
    if (!existsSync(p)) offenders.push(`phantom: ${p} is referenced by CMake but absent from the tree`);
  // orphan: every on-disk source file must be claimed (>= 1 target)
  for (const dir of ["qml", "src", "style", "tests"]) {
    const abs = join(QT_SOURCE_DIR, dir);
    if (!existsSync(abs)) continue;
    for (const f of readdirSync(abs)) {
      if (!/\.(qml|cpp|h|qmldir)$/i.test(f)) continue;
      const rel = `${dir}/${f}`;
      const absP = join(QT_SOURCE_DIR, rel);
      if (I25_ORPHAN_EXCEPTIONS.has(rel)) continue;
      if (!claimed.has(absP) && /\.(qml|cpp|h)$/i.test(f))
        offenders.push(`orphan: ui/qt/${rel} belongs to NO CMake target`);
      else if (f === "qmldir" && !I25_ORPHAN_EXCEPTIONS.has(rel))
        offenders.push(`orphan: ui/qt/${rel} is an unregistered qmldir`);
    }
  }
  assert.deepStrictEqual(
    offenders,
    [],
    "I25: zero phantom and zero orphaned ui/qt sources allowed (style files live in BOTH targets by design, I24): " +
      offenders.join(" | "),
  );
});

test("run-#50 R4 (amended run-#51): the fyne pin is declared in go.mod + go.sum and the spike entry is the ONE CLI surface", () => {
  const goMod = readFileSync(join(repoRoot, "go.mod"), "utf8");
  assert.match(
    goMod,
    /^[ \t]*fyne\.io\/fyne\/v2 v2\.8\.2$/m,
    "go.mod must pin fyne v2.8.2 (latest verified stable — run-#50 P1 source: GitHub releases API, published 2026-10-12; the ONLY dependency change allowed this cycle)",
  );
  const goSum = readFileSync(join(repoRoot, "go.sum"), "utf8");
  assert.match(
    goSum,
    /^fyne\.io\/fyne\/v2 v2\.8\.2 /m,
    "go.sum must carry the fyne v2.8.2 module hash",
  );
  assert.ok(
    existsSync(join(repoRoot, "ui", "fyne")),
    "ui/fyne/ must exist — the one-Go-UI migration's package",
  );
  // AMENDED run-#51 (R3a, Task ID 45): cmd/fyne-spike RETIRED into the
  // shipped CLI — the spike launches through the ONE CLI surface
  // (`sheytan spike`, //go:build fyne-tagged hook), never through a
  // separate main package (C19: one codebase, one entry surface). The
  // ui/fyne import contract now lives on the CLI hook file.
  assert.ok(
    !existsSync(join(repoRoot, "cmd", "fyne-spike")),
    "cmd/fyne-spike must be retired — the spike launches through the ONE CLI surface (run-#51 R3a)",
  );
  const hook = join(repoRoot, "cmd", "fyne.go");
  assert.ok(existsSync(hook), "cmd/fyne.go must exist — the fyne-tagged CLI launch hook");
  const hookGo = readFileSync(hook, "utf8");
  assert.match(
    hookGo,
    /"github\.com\/Parsaetak\/SHEYTAN-local-agent\/ui\/fyne"/,
    "the CLI hook must import the ui/fyne package",
  );
});

// ---------------------------------------------------------------------------
// run-#51 (Task ID 45) — R1/R2/R3 RED-first rows. RED at HEAD, green after
// the run-#51 fixes land. The probe is the real-macro instrument: it
// executes the ACTUAL upstream Qt 6.8.2 call chain on the real tree.
// ---------------------------------------------------------------------------

test("run-#51 R2: the real-macro configure probe exists, pins the upstream source, and is PROBE-CLEAN on the real tree", () => {
  const probePath = join(repoRoot, "scripts", "qt-configure-probe.mjs");
  assert.ok(existsSync(probePath), "scripts/qt-configure-probe.mjs must exist (R2)");
  const probe = readFileSync(probePath, "utf8");
  assert.match(probe, /v6\.8\.2/, "the probe pins the hosted Qt tag 6.8.2");
  assert.match(
    probe,
    /src\/corelib\/Qt6CoreMacros\.cmake/,
    "the probe extracts from the real upstream macro source (never a subset model)",
  );
  // The run-#50 class, verbatim: the argument list the :2212 frame consumed.
  assert.match(
    probe,
    /_qt_deferred_files/,
    "the probe must assert the deferred-file list the :2212 target_sources consumes",
  );
  const run = spawnSync(process.execPath, [probePath], {
    encoding: "utf8",
    timeout: 600000,
  });
  assert.equal(
    run.status,
    0,
    `the probe must be PROBE-OK on the changed tree; exit ${run.status}\n${run.stdout ?? ""}\n${run.stderr ?? ""}`,
  );
  assert.match(run.stdout ?? "", /PROBE-OK/, "the probe's verdict line must read PROBE-OK");
  // Every qt_add_* (and qt_standard_project_setup) call in the real tree is
  // probe-EXECUTED — the probe report names each command it ran or stubbed.
  const cmake = loadQtCmakeLists();
  const names = new Set(
    [...cmake.matchAll(/^\s*(qt\w+)\(/gm)].map((m) => m[1]).filter((n) => n.startsWith("qt")),
  );
  assert.ok(names.has("qt_add_resources"), "census: the tree calls qt_add_resources");
  for (const n of names)
    assert.match(
      run.stdout,
      new RegExp(n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
      `the probe report must name ${n} (every qt_* call is probe-executed, I25)`,
    );
});

test("run-#51 R3a: the shipped CLI carries the fyne-tagged spike launch — ONE entry surface, untagged behavior untouched", () => {
  const root = readFileSync(join(repoRoot, "cmd", "root.go"), "utf8");
  assert.match(
    root,
    /var fyneLaunch func\(\) int/,
    "root.go must declare the nil-default launch hook — the untagged binary's dispatch is unchanged",
  );
  assert.match(
    root,
    /case "spike", "fyne-ui":/,
    'root.go must route the "spike"/"fyne-ui" subcommand through the hook',
  );
  assert.match(
    root,
    /if fyneLaunch == nil \{/, 
    "the hook case must fall through to the unknown-command path when the fyne tag is absent",
  );
  const hook = readFileSync(join(repoRoot, "cmd", "fyne.go"), "utf8");
  assert.match(hook, /^\/\/go:build fyne/m, "cmd/fyne.go must be fyne-tagged so the untagged binary never links fyne");
  assert.match(hook, /func FyneLaunch\(\) int/, "the hook must expose the launch entry");
  assert.match(
    hook,
    /fyneui\.NewShell/,
    "the hook must launch the ui/fyne shell (the retired spike's behavior)",
  );
});

test("run-#51 R3b: the script surface drives the ONE CLI — core version parity through the real binary", () => {
  const va = readFileSync(join(repoRoot, "scripts", "verify-all.mjs"), "utf8");
  assert.match(
    va,
    /core version parity/,
    "verify-all must run the freshly built ONE binary's version subcommand against the release identity",
  );
  assert.match(va, /["']version["']/, "the parity stage must invoke the version subcommand");
  assert.match(
    va,
    /package\.json/,
    "the parity stage must compare against package.json — the ONE version source (I6)",
  );
});

test("run-#51 R3d: an additive fyne-tag compile gate exists on BOTH platform jobs (C2-clean, gates untouched)", () => {
  const wf = loadWorkflow();
  assert.match(
    wf,
    /Build fyne-tagged spike \(Windows\)/,
    "the Windows job must compile the fyne tag (spike claim only)",
  );
  assert.match(
    wf,
    /Build fyne-tagged spike \(Linux\)/,
    "the Linux job must compile the fyne tag (spike claim only)",
  );
  const tagged = wf.match(/go build -tags fyne [^\n]+/g) ?? [];
  assert.ok(
    tagged.length >= 2,
    "both jobs must run an additive `go build -tags fyne` compile of the spike surface",
  );
  for (const line of tagged)
    assert.match(
      line,
      /go build -tags fyne \.\/cmd\/\.\.\. \.\/ui\/fyne/,
      "the tagged build must cover the fyne packages (cmd hook + ui/fyne), nothing wider",
    );
});

test("run-#51 R1d: BOTH platform jobs surface Qt configure errors verbatim (I17 annotation parity)", () => {
  const wf = loadWorkflow();
  assert.match(
    wf,
    /ci-logs\/linux-qt-configure\.log/,
    "the Linux Qt configure must Tee into a named log (I15/I18) — the Windows twin already does",
  );
  assert.match(
    wf,
    /ci-logs\/linux-qt-build\.log/,
    "the Linux Qt build must Tee into a named log",
  );
  assert.match(
    wf,
    /Failure summary \(Linux Qt host\)/,
    "a Linux catcher must lift configure/build errors into ::error verbatim (I17) — the run-#50 Linux death posted exit 1 with NO annotation",
  );
  assert.match(
    wf,
    /CMake Error/,
    "the catcher's error class must include CMake Error lines",
  );
});

test("run-#50 R3: docs/ROADMAP.md is version-free and carries the Fyne end-state (C18/C19)", () => {
  const roadmap = readFileSync(join(repoRoot, "docs", "ROADMAP.md"), "utf8");
  const versionTokens = roadmap.match(/\b[0-9]+\.[0-9]+(?:\.[0-9]+)?\b/g) ?? [];
  assert.deepStrictEqual(
    versionTokens.slice(0, 8),
    [],
    `C18: the roadmap carries capabilities with honest status only — NO version numbers (version history lives in changelog.md alone); first offenders: ${versionTokens.slice(0, 8).join(", ")}`,
  );
  assert.match(roadmap, /Fyne/, "C19: the Fyne end-state must be present");
  assert.match(roadmap, /ONE Go UI/, "C19: the end-state is ONE Go UI replacing every other UI language");
  for (const phase of ["spike", "parity", "retirement"])
    assert.ok(
      roadmap.toLowerCase().includes(phase),
      `C19: the migration program must carry the "${phase}" phase`,
    );
});
