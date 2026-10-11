// clone-url.ts — v1.3.0 client-side GitHub URL pre-validation.
//
// A PURE module (no imports) so it is testable under node:test directly.
// It mirrors the backend's gitclone.ValidateGitHubURL; the backend
// remains the authority — this layer only gives instant feedback in the
// clone form.

export interface ParsedGitHubUrl {
  owner: string;
  repo: string;
  ssh: boolean;
}

export function parseGitHubUrl(raw: string): ParsedGitHubUrl | null {
  const value = raw.trim().toLowerCase();
  if (!value) return null;

  const scp = /^git@github\.com:([a-z0-9_.-]+)\/([a-z0-9_.-]+?)(\.git)?\/?$/.exec(
    value,
  );
  if (scp && scp[1] && scp[2]) {
    return { owner: scp[1], repo: scp[2], ssh: true };
  }

  const https = /^(?:https:\/\/github\.com\/|github\.com\/)([a-z0-9_.-]+)\/([a-z0-9_.-]+?)(?:\.git)?\/?$/.exec(
    value,
  );
  if (https && https[1] && https[2]) {
    return { owner: https[1], repo: https[2], ssh: false };
  }

  return null;
}
