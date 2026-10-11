#!/usr/bin/env node
/**
 * verify-static-assets.mjs — embedded frontend acceptance contract.
 *
 * v1.3.2: the contract is now DERIVED, not heuristic. Two rules, in order:
 *
 *   RULE 1 (build-config contract — the source of determinism): vite.config.ts
 *   must declare the stable no-hash output patterns
 *     entryFileNames:  "assets/[name].js"
 *     chunkFileNames:  "assets/[name].js"
 *     assetFileNames:  "assets/[name][extname]"
 *   A content hash can ONLY enter a filename through a [hash]/[contenthash]/
 *   [chunkhash] token in these patterns, so asserting them asserts the whole
 *   no-hash property. The previous heuristic (rejecting any name containing
 *   "-<6+ chars with a digit>") could false-reject legitimate deterministic
 *   names that carry version digits and is gone.
 *
 *   RULE 2 (reachability — the actual file contract): every file under
 *   web/static must be REACHABLE from index.html through real references:
 *     - index.html src/href attributes,
 *     - JS import specifiers (static, dynamic `import("./x.js")`, and the
 *       Rolldown/Vite `__vite__mapDeps` dependency-map arrays that name
 *       chunks as "assets/<name>.js" strings),
 *     - CSS url(...) references.
 *   A planted stale/hashed file is unreachable by construction, so it fails
 *   this check; a legitimate deterministic name (version digits, hyphens,
 *   whatever the sources are named) passes as long as something references
 *   it — which the build guarantees for every emitted asset.
 *
 * Additional standing rules (unchanged from v1.3.1):
 *
 *   3. web/static/index.html exists and references /assets/index.js and
 *      /assets/index.css.
 *   4. The bundle MUST contain at least one dynamic import: this application
 *      is INTENTIONALLY code-split (the entry lazy-loads the UI panels —
 *      see the rollupOptions output naming and src/App.tsx). Zero dynamic
 *      imports means code splitting was silently disabled, which would
 *      regress both the startup path and the stable per-panel filenames
 *      the agent navigates. This is an application contract, not an
 *      incidental implementation detail.
 *   5. When a fresh dist/ build exists (--dist), the file sets of dist/ and
 *      web/static/ must be IDENTICAL — no stale files from a previous
 *      generation may survive the sync.
 *   6. No .vite/ manifest is generated (no runtime consumer exists).
 *
 * Usage:
 *   node scripts/verify-static-assets.mjs            # verify web/static
 *   node scripts/verify-static-assets.mjs --dist     # also diff vs dist/
 *
 * Exit code 1 on any violation. Designed for CI: zero dependencies.
 */

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const staticDir = join(repoRoot, "web", "static");
const distDir = join(repoRoot, "dist");
const viteConfigPath = join(repoRoot, "vite.config.ts");

const COMPARE_DIST = process.argv.includes("--dist");

const errors = [];

function fail(message) {
  errors.push(message);
}

// ---------------------------------------------------------------------------
// 1. Build-config contract: the stable no-hash output patterns.
// ---------------------------------------------------------------------------

/** The exact output patterns that constitute the stable-filename contract. */
const STABLE_OUTPUT_PATTERNS = {
  entryFileNames: "assets/[name].js",
  chunkFileNames: "assets/[name].js",
  assetFileNames: "assets/[name][extname]",
};

/** Hash tokens that would reintroduce content-hashed filenames. */
const HASH_TOKEN_RE = /\[(?:content-?hash|chunk-?hash|hash)[^\]]*\]/i;

function checkViteOutputContract() {
  if (!existsSync(viteConfigPath)) {
    fail("vite.config.ts is missing — the build-config contract cannot be verified");
    return;
  }

  const config = readFileSync(viteConfigPath, "utf8");

  for (const [key, expected] of Object.entries(STABLE_OUTPUT_PATTERNS)) {
    const re = new RegExp(`${key}\\s*:\\s*["']([^"']+)["']`);
    const match = config.match(re);

    if (!match) {
      fail(
        `vite.config.ts does not declare build.rollupOptions.output.${key} ` +
          `(expected the stable pattern "${expected}")`,
      );
      continue;
    }

    if (match[1] !== expected) {
      fail(
        `vite.config.ts ${key} = "${match[1]}", want the stable pattern ` +
          `"${expected}" — hashed or renamed output breaks the embedded ` +
          `frontend contract`,
      );
    }

    if (HASH_TOKEN_RE.test(match[1])) {
      fail(
        `vite.config.ts ${key} contains a hash token ("${match[1]}") — ` +
          `deterministic filenames are required`,
      );
    }
  }
}

// ---------------------------------------------------------------------------
// 2. Reachability: every file must be referenced through a real edge.
// ---------------------------------------------------------------------------

function walk(dir, out = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(full, out);
    } else {
      out.push(full);
    }
  }
  return out;
}

/** Edges from an index.html file: src= / href= attributes. */
function htmlReferences(html) {
  const refs = [];
  const re = /(?:src|href)=["']([^"']+)["']/g;
  for (const m of html.matchAll(re)) {
    refs.push(m[1]);
  }
  return refs;
}

/** Edges from a JS bundle: import specifiers + __vite__mapDeps arrays. */
function jsReferences(js) {
  const refs = [];

  // Static and dynamic import specifiers, single/double/backtick quoted:
  //   import("./Panel.js")  /  from("./Panel.js")  /  import(`./Panel.js`)
  const importRe = /(?:import|from)\(\s*["'`](\.{1,2}\/[^"'`]+\.js)["'`]\s*\)/g;
  for (const m of js.matchAll(importRe)) {
    refs.push(m[1]);
  }

  // Plain static import statements: import "./x.js" / from "./x.js";
  const staticImportRe = /["'](\.{1,2}\/[^"']+\.js)["']/g;
  for (const m of js.matchAll(staticImportRe)) {
    refs.push(m[1]);
  }

  // Vite/Rolldown preload dependency maps: the entry (and chunks) embed
  // arrays of "assets/<name>.js" strings for __vite__mapDeps preloading.
  const mapDepsRe = /["'](assets\/[A-Za-z0-9._/-]+\.(?:js|css|woff2?|ttf|otf|png|jpe?g|svg|webp))["']/g;
  for (const m of js.matchAll(mapDepsRe)) {
    refs.push("/" + m[1]);
  }

  return refs;
}

/** Edges from a CSS file: url(...) references (fonts, images). */
function cssReferences(css) {
  const refs = [];
  const re = /url\(\s*["']?([^"')]+)["']?\s*\)/g;
  for (const m of css.matchAll(re)) {
    refs.push(m[1]);
  }
  return refs;
}

/** resolveRef maps one reference to a file under staticDir (null = external). */
function resolveRef(fromFile, ref) {
  if (/^(?:[a-z]+:)?\/\//i.test(ref) || ref.startsWith("data:")) {
    return null; // external URL / data URI — not a local asset
  }

  if (ref.startsWith("/")) {
    return join(staticDir, ref.slice(1));
  }

  return resolve(dirname(fromFile), ref);
}

function checkReachability() {
  const allFiles = existsSync(staticDir) ? walk(staticDir) : [];

  const indexPath = join(staticDir, "index.html");
  if (!existsSync(indexPath)) {
    fail("web/static/index.html is missing");
    return;
  }

  const indexHtml = readFileSync(indexPath, "utf8");

  if (!indexHtml.includes('<div id="root"></div>')) {
    fail("index.html does not contain the root mount element");
  }

  for (const required of ["/assets/index.js", "/assets/index.css"]) {
    if (!indexHtml.includes(`"${required}"`)) {
      fail(`index.html does not reference the stable entry ${required}`);
    }
  }

  // BFS over the reference graph starting from index.html.
  const reached = new Set([indexPath]);
  const queue = [indexPath];

  let dynamicImportsFound = 0;

  while (queue.length > 0) {
    const file = queue.shift();
    let refs = [];

    if (file.endsWith(".html")) {
      refs = htmlReferences(readFileSync(file, "utf8"));
    } else if (file.endsWith(".js")) {
      const js = readFileSync(file, "utf8");

      // Count dynamic imports for the code-splitting contract (rule 4).
      const dynRe = /import\(\s*["'`][^"'`]+["'`]\s*\)/g;
      dynamicImportsFound += (js.match(dynRe) ?? []).length;

      refs = jsReferences(js);
    } else if (file.endsWith(".css")) {
      refs = cssReferences(readFileSync(file, "utf8"));
    }

    for (const ref of refs) {
      const target = resolveRef(file, ref);
      if (target && existsSync(target) && statSync(target).isFile()) {
        if (!reached.has(target)) {
          reached.add(target);
          queue.push(target);
        }
      }
      // References to files that do not exist are reported separately
      // below (a missing referenced asset is a build contract break, not
        // an unreachability issue) — collect them here.
      if (target && !existsSync(target)) {
        fail(
          `${relative(repoRoot, file)} references ${ref} but the file does not exist`,
        );
      }
    }
  }

  // Every real file under web/static must have been reached.
  for (const file of allFiles) {
    if (!reached.has(file)) {
      fail(
        `unreachable asset (referenced by nothing — stale or foreign file): ` +
          `${relative(staticDir, file)}`,
      );
    }
  }

  // Rule 4: the app is intentionally code-split (lazy UI panels).
  if (dynamicImportsFound === 0) {
    fail(
      "no dynamic imports found in the bundled JS — code splitting appears " +
        "disabled (the app is intentionally split; see vite.config.ts and " +
        "src/App.tsx)",
    );
  }

  return dynamicImportsFound;
}

// ---------------------------------------------------------------------------
// 3. No .vite manifest.
// ---------------------------------------------------------------------------

if (existsSync(join(staticDir, ".vite", "manifest.json"))) {
  fail("web/static/.vite/manifest.json exists but no runtime consumer does");
}

// ---------------------------------------------------------------------------
// 4. Optional: web/static must exactly mirror the current dist/ build.
// ---------------------------------------------------------------------------

function collectFiles(dir, prefix = "") {
  const out = new Set();
  if (!existsSync(dir)) {
    return out;
  }
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const rel = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) {
      for (const f of collectFiles(join(dir, entry.name), rel)) {
        out.add(f);
      }
    } else if (statSync(join(dir, entry.name)).isFile()) {
      out.add(rel);
    }
  }
  return out;
}

function checkDistMirror() {
  const distFiles = collectFiles(distDir);
  const staticFiles = collectFiles(staticDir);

  if (distFiles.size === 0) {
    fail("--dist given but dist/ is empty — run the frontend build first");
    return;
  }

  const stale = [...staticFiles].filter((f) => !distFiles.has(f));
  const missing = [...distFiles].filter((f) => !staticFiles.has(f));

  for (const f of stale) {
    fail(`web/static contains a file the current build does not produce: ${f}`);
  }
  for (const f of missing) {
    fail(`web/static is missing a file the current build produces: ${f}`);
  }
}

// ---------------------------------------------------------------------------
// Run.
// ---------------------------------------------------------------------------

checkViteOutputContract();
checkReachability();
if (COMPARE_DIST) {
  checkDistMirror();
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`::error::${error}`);
    console.error(`[verify-static-assets] ${error}`);
  }
  console.error(
    `[verify-static-assets] ${errors.length} violation(s) — the embedded frontend does not satisfy the stable-asset contract.`,
  );
  process.exit(1);
}

console.log(
  "[verify-static-assets] stable-asset contract satisfied" +
    (COMPARE_DIST ? " (web/static mirrors the current build)" : "") +
    ".",
);
