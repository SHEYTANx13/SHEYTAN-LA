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
