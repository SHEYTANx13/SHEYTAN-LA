#!/usr/bin/env node
/**
 * consolidate-logs.mjs — the D21/I18 deterministic log merge (run-#41 R2).
 *
 * Merges the per-job log parts (<job>-logs-part-<attempt>.txt) into ONE
 * all-logs.txt — the single file inside the run's single SHEYTAN-LA-
 * v<version>-all-logs artifact. The part files are plain-text
 * concatenations each job builds under log-parts/ (ci-logs content with
 * per-file headers plus a ci-summary line); this script is the merge
 * point and nothing else: it never rewrites part content, never drops a
 * section, never reorders anything that is not the job ordering itself.
 *
 * Contract:
 *   - FIXED job order: preflight → audit → build-windows → build-linux
 *     (the pipeline's own gate order); any unexpected job part is appended
 *     after, sorted by name, so the output is deterministic.
 *   - Section headers carry the job name, the outcome and the attempt
 *     plus the part's mtime — the reader sees WHAT ran, HOW it ended and
 *     WHEN, without opening a second file.
 *   - A missing part is a NAMED instrumentation failure: the section is
 *     written with the MISSING PART marker (never silence, I18) and the
 *     script exits 1 so the consolidate-logs job fails loudly. All four
 *     parts present → exit 0.
 *   - If several attempts of one job are present (defensive: a hosted run
 *     only downloads its own attempt), the HIGHEST attempt wins and every
 *     discarded file is named in the accounting block.
 *
 * Usage:
 *   node scripts/consolidate-logs.mjs --parts <dir> --out <file>
 *
 * Pure node: no dependencies, no network, no filesystem writes outside
 * the --out file.
 */

import { readdirSync, readFileSync, statSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import process from "node:process";

// The pipeline's own gate order (build-desktop.yml): preflight first,
// audit second, then the two platform jobs. This is the ONLY ordering
// authority in this file.
const JOB_ORDER = ["preflight", "audit", "build-windows", "build-linux"];

const PART_RE = /^(.+)-logs-part-(\d+)\.txt$/;

function parseArgs(argv) {
  const args = { parts: "", out: "" };
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--parts") args.parts = argv[++i] ?? "";
    else if (argv[i] === "--out") args.out = argv[++i] ?? "";
  }
  if (!args.parts || !args.out) {
    process.stderr.write(
      "usage: consolidate-logs.mjs --parts <dir> --out <file>\n",
    );
    process.exit(2);
  }
  return args;
}

// Collect part files: flat directory (the download-artifact@v8
// merge-multiple shape). Returns [{job, attempt, path, mtime}].
function collectParts(dir) {
  let entries = [];
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const parts = [];
  for (const e of entries) {
    if (!e.isFile()) continue;
    const m = PART_RE.exec(e.name);
    if (!m) continue;
    const full = join(dir, e.name);
    let mtime = null;
    try {
      mtime = statSync(full).mtime;
    } catch {
      // stat on a just-listed file failing is a broken dir; treat as
      // unknown time — the merge still proceeds (never silence: the
      // accounting block lists it).
    }
    parts.push({
      job: m[1],
      attempt: Number(m[2]),
      path: full,
      name: e.name,
      mtime,
    });
  }
  return parts;
}

// Deterministic ordering: the fixed job order first; unknown jobs after,
// sorted by name. Within a job: highest attempt wins, lower attempts are
// recorded as discarded.
function orderParts(parts) {
  const byJob = new Map();
  for (const p of parts) {
    if (!byJob.has(p.job)) byJob.set(p.job, []);
    byJob.get(p.job).push(p);
  }
  const jobs = [...byJob.keys()].sort((a, b) => {
    const ia = JOB_ORDER.indexOf(a);
    const ib = JOB_ORDER.indexOf(b);
    if (ia !== -1 && ib !== -1) return ia - ib;
    if (ia !== -1) return -1;
    if (ib !== -1) return 1;
    return a.localeCompare(b);
  });
  const kept = [];
  const discarded = [];
  for (const job of jobs) {
    const list = byJob.get(job).sort((a, b) => b.attempt - a.attempt);
    kept.push(list[0]);
    for (const d of list.slice(1)) discarded.push(d);
  }
  return { kept, discarded };
}

function utc(mtime) {
  if (!mtime) return "unknown-time";
  return mtime.toISOString().replace("T", " ").replace(/\..+/, " UTC");
}

const RULE = "=".repeat(80);

function main() {
  const args = parseArgs(process.argv.slice(2));
  const parts = collectParts(args.parts);
  const { kept, discarded } = orderParts(parts);
  const keptJobs = new Set(kept.map((p) => p.job));
  const missing = JOB_ORDER.filter((j) => !keptJobs.has(j));

  const out = [];
  out.push("#".repeat(79));
  out.push("# SHEYTAN-LA all-logs — the ONE consolidated run log (D21/I18)");
  out.push(`# generated: ${utc(new Date())}`);
  out.push(
    `# parts read: ${kept.length} of ${JOB_ORDER.length} expected (${JOB_ORDER.join(", ")})`,
  );
  out.push(`# missing: ${missing.length ? missing.join(", ") : "none"}`);
  if (discarded.length) {
    out.push(
      `# discarded (lower attempts): ${discarded.map((d) => d.name).join(", ")}`,
    );
  }
  out.push(`# job order: fixed — ${JOB_ORDER.join(" → ")}`);
  out.push("#".repeat(79));
  out.push("");

  // Sections: every expected job in order (missing ones as named
  // instrumentation failures), then any unexpected jobs (defensive —
  // future jobs must never be dropped).
  const sectionJobs = [
    ...JOB_ORDER,
    ...kept.map((p) => p.job).filter((j) => !JOB_ORDER.includes(j)),
  ];

  for (const job of sectionJobs) {
    const part = kept.find((p) => p.job === job);
    if (!part) {
      out.push(RULE);
      out.push(
        `== JOB: ${job} — MISSING PART (instrumentation failure: no ${job}-logs-part-*.txt was present at consolidate time — the ${job} job's part upload (if: always()) is expected on EVERY outcome; I18)`,
      );
      out.push(RULE);
      out.push("");
      continue;
    }
    out.push(RULE);
    out.push(
      `== JOB: ${job} — outcome: ${partHeaderField(part, "outcome") || "unknown"} — attempt: ${part.attempt} — part mtime: ${utc(part.mtime)}`,
    );
    out.push(`== source: ${part.name}`);
    out.push(RULE);
    out.push(readFileSync(part.path, "utf8"));
    if (!out[out.length - 1].endsWith("\n")) out.push("");
  }

  out.push("");
  out.push(
    `+ accounting: sections=${sectionJobs.length} present=${kept.length} missing=${missing.length} discarded=${discarded.length} inputs=${parts.length}`,
  );
  out.push("");

  mkdirSync(dirname(args.out), { recursive: true });
  writeFileSync(args.out, out.join("\n"), "utf8");

  if (missing.length) {
    process.stderr.write(
      `consolidate-logs: MISSING PART for ${missing.join(", ")} — named in the output; exit 1 (I18 instrumentation failure)\n`,
    );
    process.exit(1);
  }
  process.stdout.write(
    `consolidate-logs: ${kept.length} parts merged into ${args.out} (all sections present)\n`,
  );
  process.exit(0);
}

// Read a header field (job:/attempt:/outcome:/run:) from a part file's
// first lines — the part builders write these verbatim at the top.
function partHeaderField(part, field) {
  try {
    const head = readFileSync(part.path, "utf8").split("\n", 8);
    const line = head.find((l) => l.startsWith(`${field}: `));
    return line ? line.slice(field.length + 2).trim() : "";
  } catch {
    return "";
  }
}

main();
