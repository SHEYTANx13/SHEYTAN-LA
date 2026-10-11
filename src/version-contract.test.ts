// version-contract.test.ts — v1.3.6 (spec §29/§39) regression contract:
// the visible frontend version comes from canonical build metadata
// (package.json, injected as __APP_VERSION__), and the stale v1.2.2
// fallback can never return to shipped source.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const srcDir = fileURLToPath(new URL("./", import.meta.url));
const rootDir = fileURLToPath(new URL("../", import.meta.url));

const read = (p: string) => readFileSync(srcDir + p, "utf-8");
const readRoot = (p: string) => readFileSync(rootDir + p, "utf-8");

// Strip JS/JSX comments so HISTORICAL annotations (e.g. "v1.2.2: every
// lazy panel…") do not false-positive: the contract bans version
// fallbacks in CODE, not changelog notes.
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "");
}

test("no stale hard-coded version fallback exists in shipped source", () => {
  // The v1.2.2 literal was the original defect (App.tsx displayed a
  // stale version when the backend hadn't reported yet).
  const appTsx = stripComments(read("App.tsx"));

  assert.equal(
    appTsx.includes("1.2.2"),
    false,
    "the stale v1.2.2 literal must never return to App.tsx",
  );

  // No hard-coded version literal may be displayed in ANY component —
  // scan the render surface for version-shaped string literals.
  for (const file of ["App.tsx", "SettingsVisionUpdates.tsx", "ComposerControls.tsx"]) {
    const src = stripComments(read(file));
    const displayLiterals = [...src.matchAll(/["'`]v\d+\.\d+\.\d+["'`]/g)];

    assert.deepEqual(
      displayLiterals,
      [],
      `${file} must not hard-code a visible version literal`,
    );
  }
});

test("the version display path uses the canonical build-time constant", () => {
  const appTsx = read("App.tsx");

  assert.ok(
    appTsx.includes("__APP_VERSION__"),
    "App.tsx must fall back to the build-time __APP_VERSION__ constant",
  );
  assert.ok(
    appTsx.includes("appVersion"),
    "the backend runtime appVersion stays authoritative once loaded",
  );
});

test("vite injects __APP_VERSION__ from package.json", () => {
  const config = readRoot("vite.config.ts");
  const pkg = JSON.parse(readRoot("package.json")) as { version: string };

  assert.ok(
    config.includes("__APP_VERSION__"),
    "vite.config.ts must define __APP_VERSION__",
  );
  assert.ok(
    config.includes("package.json"),
    "the injected version must derive from package.json (single source of truth)",
  );
  assert.ok(/^\d+\.\d+\.\d+$/.test(pkg.version), "package.json version is semver");
});

test("env.dts declares the ambient constant so tsc -b passes", () => {
  const env = read("vite-env.d.ts");

  assert.ok(
    env.includes("declare const __APP_VERSION__"),
    "vite-env.d.ts must declare __APP_VERSION__ for the typecheck build",
  );
});
