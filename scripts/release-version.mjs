#!/usr/bin/env node
/**
 * release-version.mjs — Single-source-of-truth release metadata gate for
 * SHEYTAN-Local-Agent.
 *
 * The canonical version lives in package.json ("version", e.g. "1.3.2").
 * Every other release surface is derived from it and must stay in sync:
 *
 *   - internal/config/config.go            ->  AppVersion = "1.3.2"
 *   - build/config.yml                     ->  productVersion: "1.3.2"
 *   - SIGNATURE                            ->  SHEYTAN-Local-Agent v1.3.2
 *
 * The product has a version-only identity: there is no codename dimension,
 * no release suffix and no second version variable. The workflow derives
 * the release identity at runtime from package.json via `--env`, so version
 * metadata has exactly one source and can never drift between files.
 *
 * v1.3.2: this script is the ONE canonical release-metadata authority for
 * every platform. It replaced the three per-shell reimplementations the
 * v1.3.1 pipeline carried (bash greps in the audit + Linux jobs, and the
 * PowerShell Select-String block in the Windows job whose -SimpleMatch
 * flag treated the regex escapes in 'AppVersion\s*=\s*"…"' as literal
 * text — the root cause of CI run 35542000811). Shell steps now only
 * ORCHESTRATE this validator (`node scripts/release-version.mjs --check`);
 * they must never re-implement release semantics. The workflow-contract
 * checks below enforce exactly that, in both directions:
 *
 *   - every build job must run `--check` (the canonical verification), and
 *   - no per-shell release-metadata matching may reappear (banned
 *     fragments: the regex-in-PowerShell pattern class and the bash
 *     grep reimplementations of the four surface checks).
 *
 * Usage:
 *   node scripts/release-version.mjs            # sync mode (default): repair drift in place
 *   node scripts/release-version.mjs --check    # verify only: exit 1 on any drift
 *   node scripts/release-version.mjs --env      # print APP_VERSION=… (CI consumption)
 *
 * The identity is one-dimensional: --env emits exactly ONE variable
 * (APP_VERSION). The former APP_VERSION_FULL compatibility alias (always
 * equal to APP_VERSION) was removed in v1.3.2 after an audit found no
 * consumer that distinguished it from APP_VERSION.
 *
 * Designed for CI: zero dependencies, CRLF-safe, never rewrites a file that
 * is already correct, and emits GitHub Actions ::error:: annotations in
 * check mode so drift is visible directly on the run summary.
 *
 * The logic is exported (pure functions over an explicit repository root)
 * so scripts/release-version.test.mjs can regression-test every rule
 * against fixture trees without spawning the CLI.
 */

import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** Repository root derived from this script's location (CLI mode). */
export const repoRoot = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
);

// ---------------------------------------------------------------------------
// Derived-surface targets: each is (prefix)(value)(suffix) in its file, the
// value being the canonical version. Whitespace-flexible, CRLF-safe.
// ---------------------------------------------------------------------------

export const surfaceTargets = [
  {
    label: "internal/config/config.go",
    file: "internal/config/config.go",
    // gofmt-aligned const, e.g. `      AppVersion = "1.3.2"` (whitespace-
    // flexible: the alignment depends on the identifiers in the block)
    pattern: /((?:^|\r?\n)[ \t]*AppVersion[ \t]*=[ \t]*")([^"]*)(")/,
    expected: (version) => version,
    describe: (v) => `AppVersion = "${v}"`,
  },
  {
    label: "build/config.yml",
    file: join("build", "config.yml"),
    pattern: /((?:^|\r?\n)[ \t]*productVersion:[ \t]*")([^"]*)(")/,
    expected: (version) => version,
    describe: (v) => `productVersion: "${v}"`,
  },
  {
    label: "SIGNATURE",
    file: "SIGNATURE",
    // First line only: "SHEYTAN-Local-Agent v1.3.2"
    pattern: /(^[ \t]*SHEYTAN-Local-Agent[ \t]+v)([^\r\n]*)/,
    multiline: true,
    // group(2) holds only the version token after the "SHEYTAN-Local-Agent v"
    // prefix, so the splice value is the bare canonical version.
    expected: (version) => version,
    describe: (v) => `first line = "SHEYTAN-Local-Agent v${v}"`,
  },
];

// ---------------------------------------------------------------------------
// Workflow contract: the CI file must consume THIS script for every release
// metadata decision and must not carry a per-shell reimplementation.
// ---------------------------------------------------------------------------

/** The workflow file this contract audits. */
export const workflowFile = join(".github", "workflows", "build-desktop.yml");

/** Number of build jobs that must run the canonical check. The pipeline has
 * exactly three repository-consuming build jobs (audit, build-windows,
 * build-linux); each must verify release metadata through this script so a
 * metadata regression cannot pass on one platform while failing another.
 */
export const requiredCheckInvocations = 3;

/**
 * The retired product codename is NEVER spelled literally in any tracked
 * file — this contract included. The CI codename-removal gate scans every
 * tracked file, so a literal token here (or in the regression suite) would
 * re-trigger the very gate it enforces — the exact self-triggering
 * regression of run 35571331850. The token is assembled at runtime; the
 * scanned source text contains only inert fragments.
 */
const RETIRED_CODENAME = ["Ze", "ta"].join("");

/**
 * Banned workflow fragments — every one is a per-shell REIMPLEMENTATION of
 * release metadata semantics that previously drifted (or broke: the
 * regex-in-PowerShell class is the run 35542000811 root cause). Release
 * semantics live here, not in grep/Select-String pipelines.
 */
export const bannedWorkflowFragments = [
  {
    fragment: "AppVersion\\s*",
    reason:
      "regex escapes inside a PowerShell -SimpleMatch pattern (the CI run " +
      "35542000811 failure class); release verification must go through " +
      "release-version.mjs --check",
  },
  {
    fragment: 'grep -E "AppVersion',
    reason:
      "bash reimplementation of the config.go check; use release-version.mjs --check",
  },
  {
    fragment: 'grep -F "\\"version\\"',
    reason:
      "bash reimplementation of the package.json check; use release-version.mjs --check",
  },
  {
    fragment: 'grep -F "productVersion',
    reason:
      "bash reimplementation of the build/config.yml check; use release-version.mjs --check",
  },
  {
    fragment: 'grep -F "SHEYTAN-Local-Agent v$APP_VERSION" SIGNATURE',
    reason:
      "bash reimplementation of the SIGNATURE check; use release-version.mjs --check",
  },
  {
    // v1.3.3 version-only release identity: the GitHub Release title is
    // the PLAIN canonical version, never the tag or a product prefix.
    fragment: "name: SHEYTAN-LA ${{",
    reason:
      "release title derives from the tag/product name instead of the " +
      "plain canonical version — the title must be `name: ${{ env.APP_VERSION }}` " +
      "(version-only identity, v1.3.3 contract)",
  },
  {
    // v1.3.3: no codename may appear anywhere in the workflow. The
    // fragment is the dynamically assembled retired token — never a
    // literal (see RETIRED_CODENAME above).
    fragment: RETIRED_CODENAME,
    reason:
      "codename fragment in the workflow — release identity is version-only " +
      "(no codename, no release suffix, v1.3.3 contract)",
  },
  {
    // v1.3.3: exactly ONE identity variable; the retired alias must stay
    // dead in the workflow too (outputs, envs, steps).
    fragment: "APP_VERSION_FULL",
    reason:
      "second release-identity variable in the workflow — --env emits exactly " +
      "APP_VERSION and no consumer may reintroduce the retired alias " +
      "(v1.3.3 contract)",
  },
];

/**
 * Required workflow fragments — the v1.3.3 version-only tag/release
 * contract, enforced structurally so a future edit cannot silently
 * regress the release identity:
 *   - the GitHub Release title is the PLAIN canonical version
 *     (`name: ${{ env.APP_VERSION }}` → "1.3.3"), not the tag, not a
 *     product prefix, not a codename;
 *   - the release job verifies the pushed tag equals v${APP_VERSION}
 *     before anything is published;
 *   - the published release is re-verified with the tag AND the
 *     version-only title.
 */
export const requiredWorkflowFragments = [
  {
    fragment: "name: ${{ env.APP_VERSION }}",
    reason:
      "GitHub Release title must be the plain canonical version " +
      "(version-only identity)",
  },
  {
    fragment: 'test "${GITHUB_REF_NAME}" = "v${APP_VERSION}"',
    reason:
      "release job must verify the pushed tag equals v${APP_VERSION} " +
      "before publishing",
  },
  {
    fragment: "tag_name: ${{ github.ref_name }}",
    reason:
      "the published release must carry the pushed tag itself",
  },
];

// ---------------------------------------------------------------------------
// Pure validation helpers (exported for tests).
// ---------------------------------------------------------------------------

/**
 * validateSemver checks the plain MAJOR.MINOR.PATCH form. Returns
 * { ok, error } — error carries a human-readable reason on failure.
 * Leading-zero components (e.g. "1.03.2") are rejected: semver forbids
 * them and a permissive pattern would accept ambiguous versions.
 */
export function validateSemver(version) {
  if (typeof version !== "string" || version.length === 0) {
    return { ok: false, error: "version is missing or empty" };
  }
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
    return {
      ok: false,
      error:
        `"${version}" is not plain semver ` +
        `(expected exactly "MAJOR.MINOR.PATCH", e.g. "1.3.2")`,
    };
  }
  return { ok: true, error: null };
}

/**
 * readCanonicalVersion reads and semver-validates package.json's version
 * field. Throws { message } shaped like the CLI failure on bad input so the
 * CLI and the tests share one behavior.
 */
export function readCanonicalVersion(root) {
  let pkg;
  try {
    pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  } catch (error) {
    throw new Error(`unable to read package.json: ${error.message}`);
  }

  const version = pkg?.version;
  if (typeof version !== "string" || version.length === 0) {
    throw new Error('package.json has no usable "version" field.');
  }

  const verdict = validateSemver(version);
  if (!verdict.ok) {
    throw new Error(`package.json version ${verdict.error}.`);
  }

  return version;
}

/**
 * workflowContractViolations audits the workflow file content against the
 * canonical-verification contract:
 *   - the runtime identity derivation (`release-version.mjs --env`) exists;
 *   - `release-version.mjs --check` is invoked by every build job
 *     (>= requiredCheckInvocations occurrences);
 *   - no banned per-shell reimplementation fragment survives.
 * Returns an array of human-readable violation strings (empty when clean).
 */
export function workflowContractViolations(content) {
  const violations = [];

  if (!/release-version\.mjs[ \t]+--env/.test(content)) {
    violations.push(
      ".github/workflows/build-desktop.yml: runtime identity derivation is " +
        "missing (expected 'release-version.mjs --env' — the workflow must " +
        "resolve APP_VERSION from package.json, not a hardcoded constant).",
    );
  }

  const checkInvocations = content.match(
    /node scripts\/release-version\.mjs[ \t]+--check/g,
  );
  if ((checkInvocations?.length ?? 0) < requiredCheckInvocations) {
    violations.push(
      `.github/workflows/build-desktop.yml: canonical release-metadata check ` +
        `invoked ${checkInvocations?.length ?? 0}x, want >= ` +
        `${requiredCheckInvocations} (every build job — audit, windows, ` +
        `linux — must run 'node scripts/release-version.mjs --check').`,
    );
  }

  for (const { fragment, reason } of requiredWorkflowFragments) {
    if (!content.includes(fragment)) {
      violations.push(
        `.github/workflows/build-desktop.yml: required release-identity ` +
          `fragment ${JSON.stringify(fragment)} is missing — ${reason}.`,
      );
    }
  }

  for (const { fragment, reason } of bannedWorkflowFragments) {
    if (content.includes(fragment)) {
      violations.push(
        `.github/workflows/build-desktop.yml contains banned fragment ` +
          `${JSON.stringify(fragment)} — ${reason}.`,
      );
    }
  }

  return violations;
}

/**
 * inspectRepository runs the full canonical validation against an explicit
 * repository root and returns a structured report (no process exits, no
 * writes):
 *
 *   { version, ok, drift: [{label, found, wanted}], missing: [...],
 *     workflowViolations: [...] }
 *
 * `drift` lists derived surfaces whose value differs from the canonical
 * version; `missing` lists surfaces whose marker could not be found at all.
 */
export function inspectRepository(root) {
  const version = readCanonicalVersion(root);

  const report = {
    version,
    ok: true,
    drift: [],
    missing: [],
    workflowViolations: [],
  };

  for (const target of surfaceTargets) {
    const path = join(root, target.file);

    let content;
    try {
      content = readFileSync(path, "utf8");
    } catch (error) {
      report.missing.push(
        `${target.label}: unable to read (${error.message})`,
      );
      report.ok = false;
      continue;
    }

    const pattern = target.multiline
      ? new RegExp(target.pattern.source, "m")
      : target.pattern;

    const match = content.match(pattern);

    if (!match) {
      report.missing.push(
        `${target.label}: release metadata marker not found ` +
          `(expected something matching ${target.describe("<version>")}).`,
      );
      report.ok = false;
      continue;
    }

    const found = match[2];
    const wanted = target.expected(version);

    if (found !== wanted) {
      report.drift.push({
        label: target.label,
        found,
        wanted,
        describe: target.describe,
      });
      report.ok = false;
    }
  }

  // Workflow contract (shape only — the workflow carries no version value).
  const workflowPath = join(root, workflowFile);
  if (!existsSync(workflowPath)) {
    report.workflowViolations.push(`${workflowFile}: file is missing.`);
    report.ok = false;
  } else {
    const violations = workflowContractViolations(
      readFileSync(workflowPath, "utf8"),
    );
    if (violations.length > 0) {
      report.workflowViolations.push(...violations);
      report.ok = false;
    }
  }

  return report;
}

/**
 * repairDrift splices the canonical version into every drifted surface
 * (sync mode). Only the value span is replaced — indentation, alignment and
 * line endings are preserved byte-for-byte. Returns the repaired count.
 * Throws on missing markers (a missing marker is never invented).
 */
export function repairDrift(root, report) {
  let repaired = 0;

  for (const d of report.drift) {
    const target = surfaceTargets.find((t) => t.label === d.label);
    const path = join(root, target.file);
    const content = readFileSync(path, "utf8");

    const pattern = target.multiline
      ? new RegExp(target.pattern.source, "m")
      : target.pattern;

    const match = content.match(pattern);
    if (!match) {
      // inspectRepository just matched this — a concurrent edit removed it.
      throw new Error(`${target.label}: marker vanished before repair.`);
    }

    const start = match.index + match[1].length;
    const end = start + match[2].length;
    const updated = content.slice(0, start) + d.wanted + content.slice(end);

    writeFileSync(path, updated, "utf8");
    repaired += 1;

    console.log(
      `[sheytan-release] ${target.label}: repaired ` +
        `${target.describe(d.found)} -> ${target.describe(d.wanted)}`,
    );
  }

  return repaired;
}

// ---------------------------------------------------------------------------
// CLI modes.
// ---------------------------------------------------------------------------

const CHECK_ONLY = process.argv.includes("--check");
const PRINT_ENV = process.argv.includes("--env");

function cliFail(message) {
  if (CHECK_ONLY) {
    // GitHub Actions annotation so the drift shows up on the run summary.
    console.error(`::error::${message}`);
  }
  console.error(`[sheytan-release] ERROR: ${message}`);
  process.exit(1);
}

const isCli = process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1];

if (isCli) {
  // The CLI operates on the CURRENT WORKING DIRECTORY (CI always invokes
  // `node scripts/release-version.mjs` from the repository root). Resolving
  // from cwd — not from this file's location — also lets the regression
  // suite drive the CLI against fixture trees.
  const cliRoot = process.cwd();

  if (!existsSync(join(cliRoot, "package.json"))) {
    cliFail(
      `no package.json in ${cliRoot} — run this script from the ` +
        `repository root`,
    );
  }

  let report;
  try {
    report = inspectRepository(cliRoot);
  } catch (error) {
    cliFail(error.message);
  }

  // --env mode: emit the release identity for CI consumption. One
  // VAR=value line per variable so both bash ($GITHUB_ENV) and PowerShell
  // consumers can parse it with a naive split on the first "=". The output
  // is PURELY machine-readable — no informational lines.
  if (PRINT_ENV) {
    console.log(`APP_VERSION=${report.version}`);
    process.exit(0);
  }

  console.log(
    `[sheytan-release] source of truth: package.json = ${report.version}`,
  );

  for (const m of report.missing) {
    cliFail(m);
  }

  for (const v of report.workflowViolations) {
    console.error(`[sheytan-release] CONTRACT VIOLATION: ${v}`);
  }

  if (CHECK_ONLY) {
    for (const d of report.drift) {
      console.error(
        `[sheytan-release] DRIFT in ${d.label}: expected ` +
          `${d.describe(d.wanted)}, found ${d.describe(d.found)}`,
      );
    }

    if (!report.ok) {
      console.error(
        `[sheytan-release] release metadata inconsistent. Run ` +
          `"node scripts/release-version.mjs" (sync mode) to repair ` +
          `versioned surfaces, then fix any contract violations by hand.`,
      );
      process.exit(1);
    }

    console.log(
      "[sheytan-release] release metadata consistent " +
        "(check mode; canonical surfaces + workflow contract).",
    );
    process.exit(0);
  }

  // Sync mode.
  let repaired = 0;
  try {
    repaired = repairDrift(cliRoot, report);
  } catch (error) {
    cliFail(error.message);
  }

  if (report.workflowViolations.length > 0) {
    // Repairs happened, but the workflow contract needs human action.
    process.exit(1);
  }

  if (repaired === 0) {
    console.log(
      "[sheytan-release] release metadata consistent (sync mode).",
    );
  } else {
    console.log(
      `[sheytan-release] ${repaired} file(s) repaired from package.json ` +
        `version ${report.version}.`,
    );
  }
  process.exit(0);
}
