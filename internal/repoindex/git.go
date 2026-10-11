package repoindex

// git.go — the first Git-aware relevance layer of repository
// intelligence (ROADMAP v1.4: "Git-aware context").
//
// Contract: Git signals are RELEVANCE METADATA, never an authority for
// correctness. Git is entirely optional — an absent git binary or a
// non-git workspace leaves every signal empty and indexing proceeds
// normally. All git output is line-bounded (gitOutputCap) so even a
// huge repository cannot balloon the update.

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// applyGitSignals stamps GitState onto records when the workspace is a
// git working tree:
//
//	"modified"  — tracked AND changed in the working tree (including
//	              staged changes and deletions pending)
//	"untracked" — present on disk, unknown to the index
//	"tracked"   — known to git, clean in the working tree
//
// Everything else stays "" (unknown). Deterministic, bounded output.
func applyGitSignals(ctx context.Context, idx *Index, report *UpdateReport) {
	idx.GitAvailable = false

	if !isGitWorktree(idx.Root) {
		return
	}

	tracked, trackedOK := gitListSet(idx.Root, []string{"ls-files"}, report)
	if !trackedOK {
		return
	}

	statusMap, statusOK := gitStatusMap(idx.Root, report)
	if !statusOK {
		return
	}

	idx.GitAvailable = true
	report.GitAvailable = true

	for _, rec := range idx.Files {
		switch {
		case statusMap[rec.Path] == "modified":
			rec.GitState = "modified"
		case statusMap[rec.Path] == "untracked":
			rec.GitState = "untracked"
		case tracked[rec.Path]:
			rec.GitState = "tracked"
		default:
			rec.GitState = ""
		}
	}
}

// isGitWorktree reports whether root looks like a git working tree.
// The check is deliberately cheap: a .git entry (directory or file —
// worktrees/submodules use a file) at the root. Real git availability
// is proven by the commands below succeeding.
func isGitWorktree(root string) bool {
	return fileExists(root + "/.git")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gitListSet runs one git command with bounded output and returns the
// set of non-empty lines (paths). ok=false means git failed/unavailable
// — the caller leaves all signals empty.
func gitListSet(root string, args []string, report *UpdateReport) (map[string]bool, bool) {
	out, ok := runGitBounded(root, args)
	if !ok {
		return nil, false
	}

	set := make(map[string]bool, 512)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() && len(set) < gitOutputCap {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			set[strings.TrimPrefix(line, `"`)] = true
		}
	}
	return set, true
}

// gitStatusMap parses `git status --porcelain=v1 -z`-free plain output
// into path -> state ("modified" | "untracked"). Bounded line count.
func gitStatusMap(root string, report *UpdateReport) (map[string]string, bool) {
	out, ok := runGitBounded(root, []string{"status", "--porcelain"})
	if !ok {
		return nil, false
	}

	m := make(map[string]string, 128)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() && len(m) < gitOutputCap {
		line := scanner.Text()
		if len(line) < 4 {
			continue
		}
		code, pathField := line[:2], strings.TrimSpace(line[3:])
		if pathField == "" {
			continue
		}
		// Rename entries carry "old -> new"; both endpoints are
		// relevant (the old path is being removed, the new one
		// added) — record the new side; the old side appears as a
		// deletion the next full pass catches via the walk.
		if arrow := strings.Index(pathField, " -> "); arrow > 0 {
			pathField = pathField[arrow+4:]
		}
		if code == "??" {
			m[strings.Trim(pathField, `"`)] = "untracked"
		} else {
			m[strings.Trim(pathField, `"`)] = "modified"
		}
	}
	return m, true
}

// runGitBounded executes git in root with a context timeout and
// bounded output capture. ok=false on ANY failure (no git, not a repo,
// timeout) — signals are optional metadata, failures are silent.
func runGitBounded(root string, args []string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	if len(out) == 0 {
		return out, true
	}
	if len(out) > gitMaxOutputBytes {
		out = out[:gitMaxOutputBytes]
	}
	return out, true
}

const (
	// gitTimeout bounds each git invocation (relevance metadata must
	// never dominate an update).
	gitTimeout = 2 * time.Second

	// gitMaxOutputBytes caps captured git output before parsing.
	gitMaxOutputBytes = 4 << 20
)
