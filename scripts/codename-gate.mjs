#!/usr/bin/env node
// codename-gate.mjs — v1.7.6 TOKEN-AWARE codename removal gate.
//
// History: the v1.3.1 gate was a `git grep -inE` one-liner. It false-positived
// on the legitimate native identifier MaterializeTarget (the character
// sequence appears across the compound's lower/upper boundary once the line
// is case-folded), which forced boundary-aware regex rules that in turn could
// not see camelCase spellings of the retired name (a leading-capital compound
// of it passed the old pattern: an alpha character followed the match).
// v1.7.6 replaces the regex with deterministic tokenization so that BOTH
// failure classes are gone:
//
//   - a real retired-codename TOKEN fails in every spelling (bare, quoted
//     prose, snake_case, kebab-case, camelCase, digit-joined, any case);
//   - a larger identifier that merely CONTAINS the character sequence across
//     a compound boundary (MaterializeTarget -> materialize + target) is a
//     legitimate English identifier and is ALLOWED.
//
// Tokenization: split each line into identifiers (maximal alphanumeric runs),
// then split every identifier into case-folded sub-tokens at snake/kebab
// boundaries (non-alphanumeric separators), camelCase humps, acronym runs and
// letter<->digit transitions. A hit is any sub-token equal to the retired
// token, the whole identifier case-folding to it, the joined legacy form, or
// the separated two-word legacy pair. The retired token and the pair's second
// word are CONSTRUCTED AT RUNTIME so this gate's own source never contains
// its own target as a scannable token sequence.
//
// The retired token itself is CONSTRUCTED AT RUNTIME ("z" + "eta"): this
// gate's own source — and its test — must never contain the literal it bans,
// exactly like the bracket trick (z[e]ta) the regex version used.
//
// File discovery mirrors the CI semantics (the tracked tree): `git ls-files`
// inside a checkout; an .gitignore-respecting walk as the deterministic
// fallback for clean-room source drops without a .git directory.
//
// Exit codes: 0 = zero retired-codename references; 1 = references remain
// (each printed as file:line with the offending identifier). The gate is
// never disabled or weakened: detection precision improves, the invariant
// (zero references) does not change.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// The retired codename token (constructed — never a literal in this file).
export const BANNED_TOKEN = "z" + "eta";
// The legacy pair form banned since v1.3.1: the two words below, joined or
// separated (underscore, hyphen, space, any casing). The words are
// constructed on SEPARATE lines so this gate never contains its own target
// as adjacent tokens (the pair check is line-scoped).
const PAIR_WORD_2 = "codename";
export const BANNED_JOINED = "app" + PAIR_WORD_2;
export const BANNED_PAIR = ["app", PAIR_WORD_2];

// Directories never scanned by the clean-room fallback walk. The git-tracked
// path (`git ls-files`) already excludes these; the fallback mirrors it.
const EXCLUDED_DIRS = new Set([
  ".git",
  "node_modules",
  "dist",
  "coverage",
  ".build",
  "test-results",
  "playwright-report",
]);

// Binary-ish suffixes the fallback walk skips without reading.
const BINARY_SUFFIXES = new Set([
  ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".icns", ".bmp", ".tiff",
  ".woff", ".woff2", ".ttf", ".otf", ".eot",
  ".zip", ".gz", ".tgz", ".tar", ".br", ".zst", ".7z", ".rar",
  ".exe", ".dll", ".so", ".dylib", ".a", ".lib", ".obj", ".o",
  ".pdf", ".mp3", ".mp4", ".wav", ".ogg", ".webm", ".mov", ".avi",
  ".gguf", ".bin", ".wasm", ".pdb", ".syso", ".sum-check",
]);

// tokenizeIdentifier splits one identifier into case-folded sub-tokens.
// Boundaries: lower/digit -> upper (camelCase humps and acronym tails),
// upper -> upper+lower (HTTPServer -> http | server), letter <-> digit.
export function tokenizeIdentifier(identifier) {
  const parts = identifier.split(
    /(?<=[a-z0-9])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])|(?<=[a-z])(?=[0-9])|(?<=[0-9])(?=[a-z])/,
  );
  return parts.map((p) => p.toLowerCase());
}

// codenameHitsInLine returns the offending TRIGGERS on one line (empty array
// when the line is clean). Each trigger is either the offending identifier
// itself or, for the separated legacy pair, the joined pair label. The pair
// check spans the line's whole token stream, because the separator may split
// one identifier into two — exactly what the old regex's optional-underscore
// form caught.
export function codenameHitsInLine(line) {
  const hits = [];
  // Identifiers: maximal alphanumeric runs — separators (spaces, quotes,
  // underscores, hyphens, dots, brackets) never join two tokens. The
  // line-wide case-folded token stream (below) carries the pair evidence.
  const identifiers = line.match(/[A-Za-z0-9]+/g) ?? [];
  const stream = [];

  for (const id of identifiers) {
    const tokens = tokenizeIdentifier(id);
    stream.push(...tokens);

    // Detection levels per identifier:
    //   (a) any case-folded sub-token EQUALS the retired token — snake,
    //       kebab, camel and digit-joined compounds (the retired name in
    //       snake, camel and digit-suffixed compounds);
    //   (b) the whole identifier case-folds to the retired token — pure
    //       case-variant spellings that fragment under tokenization
    //       (upper-case and mixed-case one-word forms) but are the
    //       codename nonetheless;
    //   (c) the legacy joined form.
    if (
      (tokens.includes(BANNED_TOKEN) ||
        id.toLowerCase() === BANNED_TOKEN ||
        id.toLowerCase() === BANNED_JOINED) &&
      !hits.includes(id)
    ) {
      hits.push(id);
    }
  }

  for (let i = 0; i + 1 < stream.length; i++) {
    if (stream[i] === BANNED_PAIR[0] && stream[i + 1] === BANNED_PAIR[1]) {
      const label = BANNED_PAIR.join("+");
      if (!hits.includes(label)) {
        hits.push(label);
      }
      i++; // a token completes at most one pair
    }
  }

  return hits;
}

// scanText scans one whole text body; returns [{ line, hits, text }].
export function scanText(text) {
  const out = [];
  const lines = text.split(/\r\n|\r|\n/); // CRLF/CR/LF normalized

  for (let i = 0; i < lines.length; i++) {
    const hits = codenameHitsInLine(lines[i]);
    if (hits.length > 0) {
      out.push({ line: i + 1, hits, text: lines[i] });
    }
  }

  return out;
}

function isScannable(file) {
  const lower = file.toLowerCase();
  for (const suffix of BINARY_SUFFIXES) {
    if (lower.endsWith(suffix)) {
      return false;
    }
  }
  return true;
}

// listFiles returns every file to scan, relative to root. Primary: the
// tracked tree (`git ls-files`, the CI semantics). Fallback: a deterministic
// walk honoring EXCLUDED_DIRS for source drops without a .git directory.
export function listFiles(root) {
  try {
    const out = execFileSync("git", ["ls-files", "-z", "--", "."], {
      cwd: root,
      maxBuffer: 64 * 1024 * 1024,
      stdio: ["ignore", "pipe", "ignore"],
    });
    const files = out.toString().split("\0").filter(Boolean);
    if (files.length > 0) {
      return files;
    }
  } catch {
    // no git — fall through to the walk
  }

  const files = [];
  const visit = (dir) => {
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (!EXCLUDED_DIRS.has(entry.name)) {
          visit(full);
        }
        continue;
      }
      if (entry.isFile() && isScannable(entry.name)) {
        files.push(path.relative(root, full));
      }
    }
  };
  visit(root);
  return files.sort();
}

// scanRepo scans every discovered file under root; returns
// [{ file, line, hits, text }] sorted by file then line.
export function scanRepo(root) {
  const results = [];

  for (const rel of listFiles(root)) {
    if (!isScannable(rel)) {
      continue;
    }
    const full = path.join(root, rel);
    let data;
    try {
      data = fs.readFileSync(full);
    } catch {
      continue; // raced away between listing and read — nothing to scan
    }
    if (data.includes(0)) {
      continue; // binary
    }
    if (data.length > 8 * 1024 * 1024) {
      continue; // generated bulk — source is far below this bound
    }
    for (const hit of scanText(data.toString("utf8"))) {
      results.push({ file: rel, ...hit });
    }
  }

  return results;
}

function main() {
  const root = path.resolve(process.argv[2] ?? process.cwd());
  const hits = scanRepo(root);

  if (hits.length > 0) {
    console.error("Retired product-codename references remain:");
    for (const h of hits) {
      console.error(`${h.file}:${h.line}: ${h.hits.join(", ")} :: ${h.text.trim()}`);
    }
    process.exit(1);
  }

  console.log("Codename removal gate passed: zero codename references (token-aware).");
}

// Only run as a CLI (the test file imports the exported functions).
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(import.meta.url.replace(/^file:\/\//, ""))) {
  main();
}
