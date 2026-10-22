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
