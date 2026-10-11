// repoindex-view.ts — pure formatting helpers for the Workspace panel's
// Repository Index card (v1.3.4, ROADMAP v1.4 slice 1).
//
// Pure module: no React, no fetch — the testable pattern this codebase
// uses for view logic (see clone-url.ts, resources.ts).

// RepoIndexStatus mirrors the GET /api/repo/index payload.
export interface RepoIndexStatus {
  root: string;
  state: "empty" | "ready" | "stale";
  files?: number;
  symbols?: number;
  depEdges?: number;
  testLinks?: number;
  languages?: Record<string, number>;
  updatedAt?: string;
  gitAvailable?: boolean;
  truncated?: boolean;
  staleRoot?: boolean;
  partial?: boolean;
}

// RepoIndexResult is one bounded search hit (POST /api/repo/search).
export interface RepoIndexResult {
  path: string;
  language?: string;
  role?: string;
  score: number;
  evidence: string;
  symbols?: string[];
  line?: number;
  gitState?: string;
  recent?: boolean;
}

// RepoSearchReport is the bounded search response.
export interface RepoSearchReport {
  root: string;
  totalHits: number;
  returned: number;
  truncated?: boolean;
  results: RepoIndexResult[];
  indexAge?: string;
  durationMs?: number;
}

// indexStateTone maps the index state to a settings-chip tone.
export function indexStateTone(
  status: Pick<RepoIndexStatus, "state"> | null | undefined,
): "good" | "warn" | "bad" {
  if (!status) return "warn";
  switch (status.state) {
    case "ready":
      return "good";
    case "stale":
      return "warn";
    default:
      return "warn";
  }
}

// indexStateLabel renders the honest human state line.
export function indexStateLabel(
  status: Pick<
    RepoIndexStatus,
    "state" | "truncated" | "partial" | "gitAvailable"
  > | null | undefined,
): string {
  if (!status) return "Not built yet";
  switch (status.state) {
    case "ready":
      return status.truncated
        ? "Ready (bounded)"
        : status.partial
          ? "Ready (partial pass)"
          : "Ready";
    case "stale":
      return "Stale — refresh";
    default:
      return "Not built yet";
  }
}

// topLanguages renders the language distribution as bounded
// "name ×count" fragments, largest first, ties alphabetical.
export function topLanguages(
  languages: Record<string, number> | null | undefined,
  max = 6,
): string[] {
  if (!languages) return [];
  return Object.entries(languages)
    .filter(([name, count]) => name.length > 0 && count > 0)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, max)
    .map(([name, count]) => `${name} ×${count}`);
}

// formatScore renders a two-decimal evidence score.
export function formatScore(score: number): string {
  if (!Number.isFinite(score)) return "0.00";
  return score.toFixed(2);
}

// resultSummary renders one search hit's one-line summary: the matched
// symbols when present, otherwise the language/role tags.
export function resultSummary(result: RepoIndexResult): string {
  const parts: string[] = [];
  if (result.symbols && result.symbols.length > 0) {
    parts.push(result.symbols.join(", "));
  }
  if (result.language) parts.push(result.language);
  if (result.role && result.role !== "source") parts.push(result.role);
  if (result.line && result.line > 0) parts.push(`:${result.line}`);
  return parts.join(" · ");
}

// evidenceLabel marks the evidence class honestly: structural facts
// (imports, test links) vs inferred relevance (keyword/path scoring).
export function evidenceLabel(evidence: string | undefined): string {
  if (!evidence) return "";
  return evidence;
}

// isStructuralEvidence reports whether an evidence string carries
// structural (dependency/test) facts rather than pure inference.
export function isStructuralEvidence(evidence: string | undefined): boolean {
  if (!evidence) return false;
  return (
    evidence.includes("dependency") ||
    evidence.includes("imported by") ||
    evidence.includes("test relationship") ||
    evidence.includes("test file of")
  );
}
