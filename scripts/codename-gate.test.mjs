// codename-gate.test.mjs — v1.7.6 deterministic proofs for the token-aware
// codename removal gate (scripts/codename-gate.mjs).
//
// §4 release contract: the gate must
//   - FAIL on the real retired token in every spelling (bare, quoted prose,
//     snake_case, kebab-case, camelCase, digit-joined, any letter case);
//   - FAIL on the legacy joined/pair forms (the two-word app/name family);
//   - ALLOW ordinary identifiers that merely CONTAIN the same character
//     sequence across a compound boundary (the historical false positive:
//     MaterializeTarget) and any unrelated text.
//
// This file never contains ANY banned literal — not the retired token in
// any casing and not the legacy pair forms: every fixture constructs its
// strings at runtime, so the tracked tree stays clean while the
// assertions stay real (the gate scans this file too, once committed).

import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import {
  BANNED_PAIR,
  BANNED_TOKEN,
  codenameHitsInLine,
  scanRepo,
  scanText,
  tokenizeIdentifier,
} from "./codename-gate.mjs";

// The retired token, built at runtime (never a literal in this file).
const RETIRED = BANNED_TOKEN;
// Case-variant helpers, also runtime-constructed.
const Cased = RETIRED[0].toUpperCase() + RETIRED.slice(1);
const Upper = RETIRED.toUpperCase();
const Mixed = "zE" + "tA";
const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("tokenizer splits camelCase, acronym and digit boundaries", () => {
  assert.deepEqual(tokenizeIdentifier("MaterializeTarget"), ["materialize", "target"]);
  assert.deepEqual(tokenizeIdentifier("HTTPServer"), ["http", "server"]);
  assert.deepEqual(tokenizeIdentifier(`${RETIRED}Foo`), [RETIRED, "foo"]);
  assert.deepEqual(tokenizeIdentifier(`${Cased}Foo`), [RETIRED, "foo"]);
  assert.deepEqual(tokenizeIdentifier(`${RETIRED}1`), [RETIRED, "1"]);
  assert.deepEqual(tokenizeIdentifier(RETIRED), [RETIRED]);
});

test("real codename token fails in every spelling", () => {
  const spellings = [
    RETIRED, // bare
    `"${RETIRED}"`, // quoted prose
    `'${RETIRED}'`,
    `${RETIRED}_test`, // snake
    `app_${RETIRED}`,
    `version-${RETIRED}`, // kebab
    `the ${RETIRED} gate`, // prose
    `(${RETIRED})`, // bracketed
  ];

  for (const line of spellings) {
    const hits = codenameHitsInLine(line);
    assert.equal(hits.length, 1, `line must fail: ${line}`);
    assert.match(hits[0], new RegExp(RETIRED, "i"));
  }
});

test("case variants fail", () => {
  for (const line of [Cased, Upper, Mixed, `${Cased} gate passed`]) {
    assert.equal(codenameHitsInLine(line).length, 1, `case variant must fail: ${line}`);
  }
});

test("camelCase spellings fail (the class the old regex missed)", () => {
  for (const line of [`${Cased}Foo`, `${RETIRED}Bar`, `new ${Cased}Target()`, `${Upper}Options`]) {
    assert.equal(codenameHitsInLine(line).length, 1, `camelCase must fail: ${line}`);
  }
});

test("digit-joined spellings fail", () => {
  for (const line of [`${RETIRED}1`, `${Cased}2Gate`, `7${RETIRED}`]) {
    assert.equal(codenameHitsInLine(line).length, 1, `digit-joined must fail: ${line}`);
  }
});

test("legacy joined and pair forms fail", () => {
  const second = BANNED_PAIR[1]; // the pair's second word, built apart in the gate
  const joined = "app" + second;
  const forms = [
    joined, // one identifier
    joined.toUpperCase(), // any case
    `app_${second}`, // separated by underscore
    `app-${second}`, // separated by hyphen
    `app ${second}`, // separated by a space
  ];

  for (const line of forms) {
    const hits = codenameHitsInLine(line);
    assert.ok(hits.length > 0, `legacy form must fail: ${line}`);
    assert.ok(
      hits.some((h) => h.toLowerCase().includes(BANNED_PAIR[1])),
      `legacy form must report the codename trigger: ${line} -> ${hits.join(", ")}`,
    );
  }
});

test("ordinary identifiers containing the sequence are ALLOWED", () => {
  const clean = [
    "func MaterializeTarget(t *testing.T) {",
    "MaterializeTarget compiles the tier plan",
    "materializetarget", // raw compound, no token boundary
    "materialize the plan",
    "ZephyrTest", // different letter entirely
    "azimuth",
    `${RETIRED.slice(0, 3)}toot`, // sequence continues into a larger token (historical: allowed)
    "the token-aware gate scans the tracked tree",
    `grep -c "${RETIRED.slice(0, 1)}[e]${RETIRED.slice(2)}" file`, // bracket trick
  ];

  for (const line of clean) {
    assert.deepEqual(codenameHitsInLine(line), [], `legitimate line must pass: ${line}`);
  }
});

test("scanText reports line numbers and normalizes line endings", () => {
  const body = [
    "package agent",
    `var retired = "${RETIRED}"`,
    "func MaterializeTarget() {}",
    `const ${Upper} = 3`,
  ].join("\r\n");

  const hits = scanText(body);

  assert.equal(hits.length, 2, "exactly the two offending lines");
  assert.equal(hits[0].line, 2);
  assert.equal(hits[1].line, 4);
});

test("scanText on a clean file yields zero hits", () => {
  const body = [
    "// The product identity is version-only; no codename dimension remains.",
    "func MaterializeTarget(t *testing.T) { t.Helper() }",
    'const APP_VERSION = "1.7.6"',
  ].join("\n");

  assert.deepEqual(scanText(body), []);
});

test("scanRepo finds zero references in the LIVE tracked tree", () => {
  // The gate as a permanent regression test: the repository itself must
  // always pass — this file included (no banned literals above).
  const hits = scanRepo(REPO_ROOT);
  assert.deepEqual(
    hits.map((h) => `${h.file}:${h.line}`),
    [],
    "the tracked tree must contain zero retired-codename references",
  );
});
