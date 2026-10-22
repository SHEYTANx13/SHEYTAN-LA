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

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
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

test("every build job uploads its complete ci-logs set with if: always() (D18, I18)", () => {
  const jobs = jobBlocks();
  for (const name of ["preflight", "audit", "build-windows", "build-linux"]) {
    const job = jobs.find((j) => j.name === name);
    assert.ok(job, `job ${name} exists`);
    const steps = jobStepBodies(job);
    const allLogs = steps.find((s) =>
      new RegExp(`uses: actions/upload-artifact@`).test(s.body.join("\n")) &&
      new RegExp(`name: ${name}-ci-logs-attempt`).test(s.body.join("\n")),
    );
    assert.ok(
      allLogs,
      `${name} uploads a ${name}-ci-logs-attempt$\{{ github.run_attempt }} artifact`,
    );
    const body = allLogs.body.join("\n");
    assert.match(body, /if: always\(\)/, `${name}'s all-logs upload runs on EVERY outcome`);
    assert.match(body, /ci-logs/, `${name}'s all-logs upload covers the ci-logs directory`);
  }
});

test("every actions pin is at or above its verified latest major (R4/D19, re-derived 2026-10-23)", () => {
  // Verified against the primary sources (GitHub releases pages) at P1:
  //   actions/cache v6.1.0 (2026-06-26, Node 24 line),
  //   actions/upload-artifact v7.0.2 (2026-10-07),
  //   actions/checkout v7.0.1, actions/setup-node v7.1.0,
  //   actions/setup-go v7.0.0, actions/setup-python v7,
  //   actions/download-artifact v7.
  // install-qt-action stays frozen (charter: no Qt/aqtinstall changes).
  const floors = {
    "actions/cache": 6,
    "actions/upload-artifact": 7,
    "actions/checkout": 7,
    "actions/setup-node": 7,
    "actions/setup-go": 7,
    "actions/setup-python": 7,
    "actions/download-artifact": 7,
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

test("green gates emit one ::notice success line each (D17: content or success, never silence)", () => {
  const cases = [
    // Windows: the Invoke-Gate helper carries the green notice for every
    // gate it drives; the inline gofmt gate emits its own literal notice.
    ["build-windows", "Go verification", /Write-Host "::notice::gate OK: \$Label"/],
    ["build-windows", "Go verification", /Write-Host "::notice::gate OK: gofmt -l \."/],
    // Linux: the bash gates emit one literal notice each.
    ["build-linux", "Go verification", /echo "::notice::gate OK: go vet \.\/\.\.\."/],
    ["build-linux", "Go verification", /echo "::notice::gate OK: gofmt -l \."/],
    // The NSIS compile gate emits its own literal notice.
    ["build-windows", "Build Windows NSIS installer", /::notice::gate OK: NSIS makensis/],
  ];
  for (const [job, step, pattern] of cases) {
    const body = findJobStep(job, step);
    assert.match(
      body,
      pattern,
      `${job}/${step} emits a green-gate success notice`,
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
