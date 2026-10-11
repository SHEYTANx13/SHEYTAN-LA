// clone-url.test.ts — v1.3.0 regression tests for the client-side
// GitHub URL pre-validation (mirrors the backend's
// gitclone.ValidateGitHubURL; the backend remains the authority — this
// layer only gives instant form feedback).
import { test } from "node:test";
import { parseGitHubUrl } from "./clone-url.ts";

test("accepts the normal https forms", () => {
  const cases: [string, string, string][] = [
    ["https://github.com/owner/repository", "owner", "repository"],
    ["https://github.com/owner/repository.git", "owner", "repository"],
    ["https://github.com/owner/repository/", "owner", "repository"],
    ["https://GitHub.com/Owner/Repository", "owner", "repository"],
  ];
  for (const [raw, owner, repo] of cases) {
    const parsed = parseGitHubUrl(raw);
    if (!parsed || parsed.owner !== owner || parsed.repo !== repo) {
      throw new Error(
        `parseGitHubUrl(${raw}) = ${JSON.stringify(parsed)}, want ${owner}/${repo}`,
      );
    }
    if (parsed.ssh) {
      throw new Error(`${raw} misclassified as SSH`);
    }
  }
});

test("accepts the scp ssh form", () => {
  const parsed = parseGitHubUrl("git@github.com:owner/repository.git");
  if (!parsed || parsed.owner !== "owner" || parsed.repo !== "repository") {
    throw new Error(`scp form misparsed: ${JSON.stringify(parsed)}`);
  }
  if (!parsed.ssh) {
    throw new Error("scp form must be recognized as SSH");
  }
});

test("accepts the bare host form", () => {
  const parsed = parseGitHubUrl("github.com/owner/repository");
  if (!parsed || parsed.owner !== "owner" || parsed.repo !== "repository") {
    throw new Error(`bare form misparsed: ${JSON.stringify(parsed)}`);
  }
});

test("rejects invalid and non-GitHub URLs", () => {
  const rejected = [
    "",
    "   ",
    "https://gitlab.com/owner/repository",
    "https://github.com/onlyowner",
    "https://github.com/owner/repo/extra",
    "https://user:token@github.com/owner/repo",
    "file:///C:/x",
    "C:\\local\\path",
    "/local/path",
    "just a sentence",
    "http://github.com/owner/repo",
  ];
  for (const raw of rejected) {
    if (parseGitHubUrl(raw) !== null) {
      throw new Error(`parseGitHubUrl(${JSON.stringify(raw)}) accepted, want rejection`);
    }
  }
});
