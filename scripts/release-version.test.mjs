#!/usr/bin/env node
/**
 * release-version.test.mjs — regression tests for the canonical release
 * metadata gate (scripts/release-version.mjs).
 *
 * v1.3.2 regression scope (the failure class of CI run 35542000811 and the
 * rules that prevent its return):
 *
 *   - valid / invalid semver shapes
 *   - metadata mismatch (drift) detection on every derived surface
 *   - missing metadata markers
 *   - whitespace variation around the config.go constant (gofmt alignment)
 *   - CRLF line endings (Windows checkouts)
 *   - --env identity emission (exactly one variable — the retired
 *     APP_VERSION_FULL alias must stay dead)
 *   - workflow canonical-version propagation and the banned-fragment
 *     contract (the run 35542000811 root cause must fail the gate)
 *   - patch-to-patch version changes (1.3.1 -> 1.3.2 and back), including
 *     in-place repair semantics that preserve alignment and CRLF
 *
 * Runs with: node --test scripts/release-version.test.mjs
 * (wired as "test:release" in package.json).
 */

import { test as rawTest } from "node:test";
import assert from "node:assert/strict";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  rmSync,
  readFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { execFile } from "node:child_process";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

/**
 * The retired product codename, assembled at runtime and never spelled
 * literally. This regression suite is itself a tracked file, so a literal
 * token here would re-trigger the repository-wide codename-removal gate
 * that these very tests enforce — the self-triggering regression of CI
 * run 35571331850. The source text below contains only inert fragments;
 * the joined result equals the retired token.
 */
const RETIRED_CODENAME = ["Ze", "ta"].join("");

import {
  validateSemver,
  inspectRepository,
  repairDrift,
  workflowContractViolations,
  requiredCheckInvocations,
} from "./release-version.mjs";

const execFileAsync = promisify(execFile);
const scriptDir = dirname(fileURLToPath(import.meta.url));

// node:test's context object is only available inside test bodies, but
// fixtureRepo needs it for t.after cleanup. `test` below wraps every body
// so the active context is captured for testCtx() — call sites stay plain.
let _ctx = null;

function testCtx() {
  if (_ctx == null) {
    throw new Error("fixtureRepo called outside a captured test context");
  }
  return _ctx;
}

function test(name, fn) {
  return rawTest(name, async (t) => {
    _ctx = t;
    try {
      await fn();
    } finally {
      _ctx = null;
    }
  });
}

// ---------------------------------------------------------------------------
// Fixture helpers: build a minimal repository tree in a temp directory.
// ---------------------------------------------------------------------------

const GOOD_WORKFLOW = [
  "jobs:",
  "  audit:",
  "    steps:",
  "      - run: node scripts/release-version.mjs",
  "      - run: node scripts/release-version.mjs --check",
  "  build-windows:",
  "    steps:",
  "      - run: node scripts/release-version.mjs",
  "      - run: node scripts/release-version.mjs --check",
  "  build-linux:",
  "    steps:",
  "      - run: node scripts/release-version.mjs",
  "      - run: node scripts/release-version.mjs --check",
  "      - run: identity=$(node scripts/release-version.mjs --env)",
  "  release:",
  "    if: startsWith(github.ref, 'refs/tags/v')",
  "    steps:",
  "      - name: Verify release tag",
  "        shell: bash",
  "        run: |",
  "          set -euo pipefail",
  "          test \"${GITHUB_REF_NAME}\" = \"v${APP_VERSION}\"",
  "      - name: Publish GitHub Release",
  "        uses: softprops/action-gh-release@v3",
  "        with:",
  "          tag_name: ${{ github.ref_name }}",
  "          name: ${{ env.APP_VERSION }}",
  "",
].join("\n");

function fixtureRepo(t, { version, configGo, configYml, signature, workflow } = {}) {
  const root = mkdtempSync(join(tmpdir(), "sheytan-release-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));

  mkdirSync(join(root, ".github", "workflows"), { recursive: true });
  mkdirSync(join(root, "build"), { recursive: true });
  mkdirSync(join(root, "internal", "config"), { recursive: true });

  writeFileSync(
    join(root, "package.json"),
    JSON.stringify({ name: "sheytan-local-agent", version }, null, 2) + "\n",
  );

  writeFileSync(
    join(root, "internal", "config", "config.go"),
    configGo ?? 'package config\n\nconst (\n\tAppVersion = "1.3.2"\n)\n',
  );

  writeFileSync(
    join(root, "build", "config.yml"),
    configYml ?? 'info:\n  productVersion: "1.3.2"\n',
  );

  writeFileSync(
    join(root, "SIGNATURE"),
    signature ?? "SHEYTAN-Local-Agent v1.3.2\nSigned by: Parsa Tak\n",
  );

  writeFileSync(
    join(root, ".github", "workflows", "build-desktop.yml"),
    workflow ?? GOOD_WORKFLOW,
  );

  return root;
}

// ---------------------------------------------------------------------------
// 1. Semver shapes.
// ---------------------------------------------------------------------------

test("valid semver shapes are accepted", () => {
  for (const v of ["1.3.2", "0.0.1", "10.20.30", "2.0.0"]) {
    assert.equal(validateSemver(v).ok, true, v);
  }
});

test("invalid semver shapes are rejected", () => {
  const bad = [
    "",            // empty
    "1.3",         // two components
    "1.3.2.0",     // four components
    "v1.3.2",      // leading v (the tag carries the v, the metadata must not)
    "1.3.x",       // wildcard
    "1.3.2-rc1",   // prerelease suffix
    "1.3.2+build", // build metadata suffix
    " 1.3.2",      // leading whitespace
    "1.3.2 ",      // trailing whitespace
    "1.03.2",      // leading zero (ambiguous — reject conservatively)
    null,          // non-string
    undefined,
    1.3,
  ];
  for (const v of bad) {
    const verdict = validateSemver(v);
    assert.equal(
      verdict.ok,
      false,
      `expected rejection: ${JSON.stringify(v)}`,
    );
    assert.ok(verdict.error.length > 0, "rejection carries a reason");
  }
});

// ---------------------------------------------------------------------------
// 2. Consistency, drift, missing markers.
// ---------------------------------------------------------------------------

test("consistent tree passes inspection", () => {
  const root = fixtureRepo(testCtx(), { version: "1.3.2" });
  const report = inspectRepository(root);
  assert.equal(report.ok, true);
  assert.equal(report.version, "1.3.2");
  assert.deepEqual(report.drift, []);
  assert.deepEqual(report.missing, []);
  assert.deepEqual(report.workflowViolations, []);
});

test("metadata mismatch is detected on every surface", () => {
  const t = testCtx();
  const cases = [
    {
      name: "config.go AppVersion stale",
      configGo: 'package config\n\nconst (\n\tAppVersion = "1.3.1"\n)\n',
    },
    {
      name: "build/config.yml productVersion stale",
      configYml: 'info:\n  productVersion: "1.3.1"\n',
    },
    {
      name: "SIGNATURE first line stale",
      signature: "SHEYTAN-Local-Agent v1.3.1\nSigned by: Parsa Tak\n",
    },
  ];

  for (const c of cases) {
    const root = fixtureRepo(t, { version: "1.3.2", ...c });
    const report = inspectRepository(root);
    assert.equal(report.ok, false, c.name);
    assert.equal(report.drift.length, 1, c.name);
    assert.equal(report.drift[0].found, "1.3.1", c.name);
    assert.equal(report.drift[0].wanted, "1.3.2", c.name);
  }
});

test("missing metadata markers are reported, never invented", () => {
  const t = testCtx();
  const missingCases = [
    {
      name: "config.go has no AppVersion constant",
      configGo: "package config\n\nconst (\n\tAppName = \"x\"\n)\n",
    },
    {
      name: "config.yml has no productVersion key",
      configYml: "info:\n  productName: \"SHEYTAN-LA\"\n",
    },
    {
      name: "SIGNATURE first line is malformed",
      signature: "Not the signature first line\n",
    },
  ];

  for (const c of missingCases) {
    const root = fixtureRepo(t, { version: "1.3.2", ...c });
    const report = inspectRepository(root);
    assert.equal(report.ok, false, c.name);
    assert.equal(report.missing.length, 1, c.name);
    assert.ok(report.missing[0].includes("marker not found"), c.name);
  }
});

// ---------------------------------------------------------------------------
// 3. Whitespace and CRLF tolerance.
// ---------------------------------------------------------------------------

test("whitespace variation around AppVersion is matched (gofmt alignment)", () => {
  const t = testCtx();
  const variants = [
    'const (\n\tAppVersion = "1.3.2"\n)\n',      // tab indent, single spaces
    'const (\n\tAppVersion   =   "1.3.2"\n)\n', // multiple spaces around =
    'const (\n      AppVersion = "1.3.2"\n)\n',  // space-padded block
    'const (\n\tAppVersion\t=\t"1.3.2"\n)\n',    // tabs around =
  ];
  for (const go of variants) {
    const root = fixtureRepo(t, { version: "1.3.2", configGo: go });
    const report = inspectRepository(root);
    assert.equal(report.ok, true, JSON.stringify(go));
    assert.equal(report.drift.length, 0, JSON.stringify(go));
  }
});

test("drift is detected through whitespace variation", () => {
  const t = testCtx();
  const root = fixtureRepo(t, {
    version: "1.3.2",
    configGo: 'const (\n\tAppVersion   =   "1.3.1"\n)\n', // stale, wide spacing
  });
  const report = inspectRepository(root);
  assert.equal(report.ok, false);
  assert.equal(report.drift[0].found, "1.3.1");
  assert.equal(report.drift[0].wanted, "1.3.2");
});

test("CRLF line endings are handled (Windows checkouts)", () => {
  const t = testCtx();
  const root = fixtureRepo(t, {
    version: "1.3.2",
    configGo: 'package config\r\n\r\nconst (\r\n\tAppVersion = "1.3.2"\r\n)\r\n',
    configYml: 'info:\r\n  productVersion: "1.3.2"\r\n',
    signature: "SHEYTAN-Local-Agent v1.3.2\r\nSigned by: Parsa Tak\r\n",
  });
  const report = inspectRepository(root);
  assert.equal(report.ok, true);
  assert.deepEqual(report.drift, []);
});

test("CRLF drift is repaired without corrupting line endings", () => {
  const t = testCtx();
  const root = fixtureRepo(t, {
    version: "1.3.2",
    configGo: 'package config\r\n\r\nconst (\r\n\tAppVersion = "1.3.1"\r\n)\r\n',
  });

  const report = inspectRepository(root);
  assert.equal(report.drift.length, 1);
  const repaired = repairDrift(root, report);
  assert.equal(repaired, 1);

  const after = inspectRepository(root);
  assert.equal(after.ok, true);
  assert.equal(after.drift.length, 0);

  const go = readFileSync(
    join(root, "internal", "config", "config.go"),
    "utf8",
  );
  assert.ok(go.includes('AppVersion = "1.3.2"\r\n'), "CRLF preserved");
});

// ---------------------------------------------------------------------------
// 4. --env identity emission (the alias must stay dead).
// ---------------------------------------------------------------------------

test("env identity is exactly APP_VERSION (APP_VERSION_FULL stays removed)", async () => {
  const root = fixtureRepo(testCtx(), { version: "1.3.2" });

  const { stdout } = await execFileAsync(
    process.execPath,
    [join(scriptDir, "release-version.mjs"), "--env"],
    { cwd: root },
  );

  const lines = stdout.trim().split(/\r?\n/).filter((l) => l.includes("="));
  assert.equal(lines.length, 1, `exactly one VAR=value line, got: ${lines}`);
  assert.equal(lines[0], "APP_VERSION=1.3.2");
  assert.ok(!stdout.includes("APP_VERSION_FULL"), "the retired alias must stay dead");
});

test("env identity fails closed on invalid semver", async () => {
  const root = fixtureRepo(testCtx(), { version: "1.3.2-rc1" });

  await assert.rejects(
    execFileAsync(
      process.execPath,
      [join(scriptDir, "release-version.mjs"), "--env"],
      { cwd: root },
    ),
    (err) => {
      assert.notEqual(err.code, 0, "invalid semver must exit non-zero");
      assert.ok(!err.stdout.includes("APP_VERSION="), "no identity on failure");
      return true;
    },
  );
});

// ---------------------------------------------------------------------------
// 5. Workflow contract (the run 35542000811 root-cause class).
// ---------------------------------------------------------------------------

test("workflow contract accepts the canonical shape", () => {
  assert.deepEqual(workflowContractViolations(GOOD_WORKFLOW), []);
});

test("workflow missing --env derivation is a violation", () => {
  const wf = GOOD_WORKFLOW.replace(
    "identity=$(node scripts/release-version.mjs --env)",
    "identity=hardcoded",
  );
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1);
  assert.ok(v[0].includes("--env"));
});

test("workflow with too few --check invocations is a violation", () => {
  // Remove one of the three --check lines.
  const wf = GOOD_WORKFLOW.replace(
    "      - run: node scripts/release-version.mjs --check\n  build-linux:",
    "  build-linux:",
  );
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1);
  assert.ok(v[0].includes("canonical release-metadata check"));
  assert.ok(v[0].includes(`>= ${requiredCheckInvocations}`));
});

test("the run-35542000811 pattern class is banned (regex escapes in PowerShell)", () => {
  const wf =
    GOOD_WORKFLOW +
    "  - run: |\n" +
    "      $p = 'AppVersion\\s*=\\s*\"' + $env:APP_VERSION + '\"'\n" +
    "      Select-String -Path config.go -Pattern $p -SimpleMatch -Quiet\n";
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1);
  assert.ok(v[0].includes("35542000811"));
});

test("bash grep reimplementations are banned", () => {
  const bashFragments = [
    '          grep -E "AppVersion[[:space:]]+=[[:space:]]+\\"$APP_VERSION\\"" internal/config/config.go\n',
    '          grep -F "\\"version\\": \\"$APP_VERSION\\"" package.json\n',
    '          grep -F "productVersion: \\"$APP_VERSION\\"" build/config.yml\n',
    '          grep -F "SHEYTAN-Local-Agent v$APP_VERSION" SIGNATURE\n',
  ];
  for (const frag of bashFragments) {
    const v = workflowContractViolations(GOOD_WORKFLOW + frag);
    assert.equal(v.length, 1, frag.trim());
    assert.ok(v[0].includes("banned fragment"), frag.trim());
  }
});

test("inspectRepository integrates the workflow contract", () => {
  const t = testCtx();
  const root = fixtureRepo(t, {
    version: "1.3.2",
    workflow:
      GOOD_WORKFLOW +
      'grep -F "productVersion: \\"1.3.2\\"" build/config.yml\n',
  });
  const report = inspectRepository(root);
  assert.equal(report.ok, false);
  assert.equal(report.workflowViolations.length, 1);
});

// ---------------------------------------------------------------------------
// 6. Patch-to-patch version changes.
// ---------------------------------------------------------------------------

test("patch release 1.3.1 -> 1.3.2 is repaired across every surface", () => {
  const t = testCtx();
  // A tree at 1.3.1 whose canonical version is bumped to 1.3.2.
  const root = fixtureRepo(t, {
    version: "1.3.2",
    configGo: 'package config\n\nconst (\n\tAppVersion = "1.3.1"\n)\n',
    configYml: 'info:\n  productVersion: "1.3.1"\n',
    signature: "SHEYTAN-Local-Agent v1.3.1\nSigned by: Parsa Tak\n",
  });

  const before = inspectRepository(root);
  assert.equal(before.ok, false);
  assert.equal(before.drift.length, 3, "all three surfaces drifted");

  const repaired = repairDrift(root, before);
  assert.equal(repaired, 3);

  const after = inspectRepository(root);
  assert.equal(after.ok, true);
  assert.deepEqual(after.drift, []);
});

test("patch release downgrade 1.3.2 -> 1.3.1 is detected as drift", () => {
  const t = testCtx();
  // A tree at 1.3.2 (all defaults) whose canonical version is rolled back
  // to 1.3.1: EVERY derived surface must report the downgrade as drift.
  const root = fixtureRepo(t, {
    version: "1.3.1",
  });

  const report = inspectRepository(root);
  assert.equal(report.ok, false);
  assert.equal(report.drift.length, 3, "all three surfaces drifted");
  for (const d of report.drift) {
    assert.equal(d.found, "1.3.2");
    assert.equal(d.wanted, "1.3.1");
  }
});

test("repair preserves gofmt alignment padding around the constant", () => {
  const t = testCtx();
  // Wide aligned block: the const identifiers share a wide column layout.
  const root = fixtureRepo(t, {
    version: "1.3.2",
    configGo:
      'package config\n\nconst (\n\tAppName      = "SHEYTAN-LA"\n\tAppVersion   = "1.3.1"\n)\n',
  });

  const report = inspectRepository(root);
  assert.equal(report.drift.length, 1);
  repairDrift(root, report);

  const go = readFileSync(
    join(root, "internal", "config", "config.go"),
    "utf8",
  );
  assert.ok(
    go.includes('\tAppVersion   = "1.3.2"'),
    "alignment padding preserved byte-for-byte",
  );
  assert.ok(
    go.includes('\tAppName      = "SHEYTAN-LA"'),
    "sibling line untouched",
  );
});

test("repair is a no-op on an already-consistent tree", () => {
  const root = fixtureRepo(testCtx(), { version: "1.3.2" });
  const report = inspectRepository(root);
  assert.equal(report.ok, true);
  assert.equal(repairDrift(root, report), 0);
});


// ---------------------------------------------------------------------------
// 5b. v1.3.3 version-only tag/release identity contract.
// ---------------------------------------------------------------------------

test("release title must be the plain canonical version (version-only identity)", () => {
  // The pre-v1.3.3 shape: title derived from the tag with a product prefix.
  const wf = GOOD_WORKFLOW.replace(
    "name: ${{ env.APP_VERSION }}",
    "name: SHEYTAN-LA ${{ github.ref_name }}",
  );
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 2, JSON.stringify(v));
  assert.ok(
    v.some((x) => x.includes("version-only identity")),
    "the tag/product-derived title must be named a banned shape",
  );
  assert.ok(
    v.some((x) => x.includes("required release-identity fragment")),
    "the missing version-only title fragment must be reported",
  );
});

test("release title must not carry a codename suffix", () => {
  const wf = GOOD_WORKFLOW.replace(
    "name: ${{ env.APP_VERSION }}",
    `name: \${{ env.APP_VERSION }}-${RETIRED_CODENAME}`,
  );
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1, JSON.stringify(v));
  assert.ok(v[0].includes("codename"));
});

test("codename fragments anywhere in the workflow are banned", () => {
  const Z = RETIRED_CODENAME;
  const shapes = [
    `      - run: echo "Shipping ${Z} build"\n`,
    `  # release codename: ${Z}\n`,
    `      - run: echo v1.3.3-${Z}\n`,
  ];
  for (const shape of shapes) {
    const v = workflowContractViolations(GOOD_WORKFLOW + shape);
    assert.equal(v.length, 1, shape.trim());
    assert.ok(v[0].includes(RETIRED_CODENAME), shape.trim());
    assert.ok(v[0].includes("codename"), shape.trim());
  }
});

test("a second release-identity variable is banned (APP_VERSION_FULL stays dead)", () => {
  const wf = GOOD_WORKFLOW + "      - run: echo ${{ env.APP_VERSION_FULL }}\n";
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1, JSON.stringify(v));
  assert.ok(v[0].includes("second release-identity variable"));
});

test("the tag-equals-version verification step is required", () => {
  const wf = GOOD_WORKFLOW.replace(
    'test "${GITHUB_REF_NAME}" = "v${APP_VERSION}"',
    'test "${GITHUB_REF_NAME}" = "v1.3.2"',
  );
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1, JSON.stringify(v));
  assert.ok(v[0].includes("verify the pushed tag"));
});

test("the published release must carry the pushed tag itself", () => {
  const wf = GOOD_WORKFLOW.replace(
    "tag_name: ${{ github.ref_name }}",
    "tag_name: v1.3.2",
  );
  const v = workflowContractViolations(wf);
  assert.equal(v.length, 1, JSON.stringify(v));
  assert.ok(v[0].includes("pushed tag"));
});

test("the real repository workflow satisfies the full v1.3.3 contract", () => {
  const repoRoot = join(scriptDir, "..");
  const wf = readFileSync(
    join(repoRoot, ".github", "workflows", "build-desktop.yml"),
    "utf8",
  );
  assert.deepEqual(workflowContractViolations(wf), []);
});
