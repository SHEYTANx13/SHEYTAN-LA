// target.go — v1.3.0 GitHub clone target parsing and validation.
//
// The accepted forms (everything a user realistically pastes):
//
//	https://github.com/owner/repository
//	https://github.com/owner/repository.git
//	https://github.com/owner/repository/
//	github.com/owner/repository            (normalized to https)
//	git@github.com:owner/repository.git     (SSH, scp syntax)
//	ssh://git@github.com/owner/repository   (SSH URL)
//
// Everything else — other hosts, URLs with embedded credentials, local
// paths, file:// URLs, ports, query/fragment suffixes — is rejected with
// a specific, user-actionable error. Validation happens BEFORE any
// process is spawned; user input is never interpolated into a shell
// string (the clone itself runs a structured git invocation with
// explicit arguments — see clone.go).
package gitclone

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// GitHubHost is the only accepted remote host.
const GitHubHost = "github.com"

// Target is a validated clone target.
type Target struct {
	// Raw is the input exactly as the user provided it.
	Raw string
	// NormalizedURL is the URL handed to git (https form for https
	// targets, the original ssh form for SSH targets).
	NormalizedURL string
	// SSH is true for SSH targets (authentication via the user's keys).
	SSH bool
	// Owner and Repo identify the repository ("owner/repository").
	Owner string
	// Repo is the repository name WITHOUT the .git suffix.
	Repo string
	// Name is "owner/repo".
	Name string
}

// scpPattern matches git@github.com:owner/repo(.git) — the scp-like SSH
// shorthand users copy from GitHub's "Code" button.
var scpPattern = regexp.MustCompile(`(?i)^git@github\.com:([A-Za-z0-9_.\-]+)/([A-Za-z0-9_.\-]+?)(?:\.git)?/?$`)

// ValidateGitHubURL parses raw into a Target or rejects it with a
// classified error (see ErrorKind).
func ValidateGitHubURL(raw string) (Target, error) {
	trimmed := strings.TrimSpace(raw)

	if trimmed == "" {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "enter a GitHub repository URL, e.g. https://github.com/owner/repository"}
	}

	// SSH scp shorthand.
	if m := scpPattern.FindStringSubmatch(trimmed); m != nil {
		return Target{
			Raw:           trimmed,
			NormalizedURL: "git@" + GitHubHost + ":" + m[1] + "/" + m[2] + ".git",
			SSH:           true,
			Owner:         m[1],
			Repo:          m[2],
			Name:          m[1] + "/" + m[2],
		}, nil
	}

	// ssh:// URL form.
	if strings.HasPrefix(strings.ToLower(trimmed), "ssh://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return Target{}, &Error{Kind: ErrInvalidURL, Message: "the SSH URL could not be parsed: " + err.Error()}
		}
		if strings.ToLower(u.Hostname()) != GitHubHost || u.User == nil || u.User.Username() != "git" {
			return Target{}, &Error{Kind: ErrInvalidURL, Message: "SSH URLs must point at git@github.com"}
		}
		owner, repo, err := ownerRepoFromPath(u.Path)
		if err != nil {
			return Target{}, err
		}
		return Target{
			Raw:           trimmed,
			NormalizedURL: u.String(),
			SSH:           true,
			Owner:         owner,
			Repo:          repo,
			Name:          owner + "/" + repo,
		}, nil
	}

	// Bare host/path form ("github.com/owner/repo") → normalize to https.
	if strings.HasPrefix(strings.ToLower(trimmed), GitHubHost+"/") {
		trimmed = "https://" + trimmed
	}

	// Everything else must be a well-formed https URL.
	if !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "not a GitHub URL — use https://github.com/owner/repository or git@github.com:owner/repository.git"}
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "the URL could not be parsed: " + err.Error()}
	}

	if u.Hostname() == "" {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "the URL has no host"}
	}

	if !strings.EqualFold(u.Hostname(), GitHubHost) {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: fmt.Sprintf("only %s repositories can be cloned here (got %q)", GitHubHost, u.Hostname())}
	}

	if u.User != nil {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "URLs with embedded credentials are not accepted"}
	}

	if u.Port() != "" {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "URLs with an explicit port are not accepted"}
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return Target{}, &Error{Kind: ErrInvalidURL, Message: "URLs with a query or fragment are not accepted"}
	}

	owner, repo, err := ownerRepoFromPath(u.Path)
	if err != nil {
		return Target{}, err
	}

	return Target{
		Raw:           raw,
		NormalizedURL: "https://" + GitHubHost + "/" + owner + "/" + repo + ".git",
		Owner:         owner,
		Repo:          repo,
		Name:          owner + "/" + repo,
	}, nil
}

// ownerRepoFromPath extracts owner/repo from "/owner/repo(.git)/".
func ownerRepoFromPath(path string) (string, string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(path), "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")

	if trimmed == "" {
		return "", "", &Error{Kind: ErrInvalidURL, Message: "the URL has no repository path — use https://github.com/owner/repository"}
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", "", &Error{Kind: ErrInvalidURL, Message: "expected owner/repository in the URL path, got \"" + trimmed + "\""}
	}

	owner, repo := parts[0], parts[1]

	for _, part := range []string{owner, repo} {
		if part == "" || part == "." || part == ".." {
			return "", "", &Error{Kind: ErrInvalidURL, Message: "invalid repository path"}
		}
	}

	return owner, repo, nil
}
