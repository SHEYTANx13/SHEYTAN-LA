package repoindex

// tool.go — the agent-facing repo_search tool and the context-pipeline
// evidence block.
//
// The tool gives the model TARGETED repository evidence on demand:
// find a symbol, find the dependencies of a file, find who depends on
// it, find its tests, or rank files against a task description. All
// results are bounded, deterministic, path-safe and workspace-scoped —
// the model receives evidence lines, never the index itself.

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// ToolName is the LLM-visible tool name.
const ToolName = "repo_search"

// maxToolTextBytes bounds the tool's textual result.
const maxToolTextBytes = 8 << 10

// Tool implements the agent tool contract (Name/Description/
// Parameters/Run — see internal/agent.Tool).
type Tool struct {
	Store *Store

	// Root provides the LIVE workspace root (the agent's current
	// project). Re-read on every call so a workspace switch takes
	// effect immediately.
	Root func() string
}

// NewTool builds the repo_search tool.
func NewTool(store *Store, root func() string) *Tool {
	return &Tool{Store: store, Root: root}
}

// Name returns the tool name.
func (t *Tool) Name() string { return ToolName }

// Description returns the LLM-facing tool description.
func (t *Tool) Description() string {
	return "Search the current workspace's persistent repository index " +
		"(ROADMAP v1.4 Repository Intelligence). Finds files by symbol " +
		"declarations, dependency edges (imports/includes), test/source " +
		"relationships, language/role, path and task relevance — with " +
		"bounded, evidence-backed results. Use it to locate where things " +
		"are implemented, what depends on what, and which tests cover a " +
		"source file BEFORE reading files blindly."
}

// ShortDescription is the compact UI-facing summary.
func (t *Tool) ShortDescription() string {
	return "Query the repository index (symbols, dependencies, tests, relevance)"
}

// repoSearchParams mirrors Query with JSON-schema-friendly tags.
type repoSearchParams struct {
	Text     string `json:"text,omitempty" jsonschema:"free-text keywords to match against paths, symbols and imports"`
	Symbol   string `json:"symbol,omitempty" jsonschema:"symbol name to find (exact, prefix or substring match on declared symbols)"`
	Path     string `json:"path,omitempty" jsonschema:"path or basename substring to match"`
	Language string `json:"language,omitempty" jsonschema:"language filter: go, typescript, javascript, json, c, cpp, ..."`
	Role     string `json:"role,omitempty" jsonschema:"role filter: source, test, config, doc, asset"`
	Task     string `json:"task,omitempty" jsonschema:"task description — files are ranked against its keywords (relevance mode)"`
	DepsOf   string `json:"depsOf,omitempty" jsonschema:"file path — find the files THIS file depends on (imports/includes)"`
	UsedBy   string `json:"usedBy,omitempty" jsonschema:"file path — find the files that depend on THIS file"`
	TestsOf  string `json:"testsOf,omitempty" jsonschema:"file path — find its corresponding test (or source) files"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum results (default 12, max 50)"`
}

// Parameters returns the JSON schema (struct-tag based, house style).
func (t *Tool) Parameters() any {
	return repoSearchParams{}
}

// Run executes the query.
func (t *Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Store == nil || t.Root == nil {
		return "", fmt.Errorf("repo_search: repository index is not available")
	}

	root := t.Root()
	if root == "" {
		return "", fmt.Errorf("repo_search: no workspace root is configured")
	}

	var p repoSearchParams
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("repo_search: invalid arguments: %w", err)
		}
	}

	// Path-safety: file-reference parameters may arrive absolute
	// (the model saw absolute paths earlier); relativize against the
	// workspace root and refuse anything escaping it.
	if rel, err := relWithinRoot(root, p.DepsOf); err != nil {
		return "", err
	} else {
		p.DepsOf = rel
	}
	if rel, err := relWithinRoot(root, p.UsedBy); err != nil {
		return "", err
	} else {
		p.UsedBy = rel
	}
	if rel, err := relWithinRoot(root, p.TestsOf); err != nil {
		return "", err
	} else {
		p.TestsOf = rel
	}

	q := Query{
		Text:     p.Text,
		Symbol:   p.Symbol,
		Path:     p.Path,
		Language: p.Language,
		Role:     p.Role,
		Task:     p.Task,
		DepsOf:   p.DepsOf,
		UsedBy:   p.UsedBy,
		TestsOf:  p.TestsOf,
		Limit:    p.Limit,
	}

	report, err := t.Store.Search(ctx, root, q)
	if err != nil {
		return "", fmt.Errorf("repo_search: %w", err)
	}

	return renderToolReport(root, report), nil
}

// renderToolReport formats the bounded, LLM-readable result block.
func renderToolReport(root string, report SearchReport) string {
	var b strings.Builder

	header := fmt.Sprintf(
		"repoindex: %d hit(s)", report.TotalHits,
	)
	if report.Truncated {
		header += fmt.Sprintf(" (showing top %d — narrow the query)", report.Returned)
	}
	if report.IndexAge != "" {
		header += fmt.Sprintf(" — index updated %s", report.IndexAge)
	}
	b.WriteString(header)
	b.WriteString("\n")

	shown := 0
	for i, res := range report.Results {
		if b.Len() > maxToolTextBytes {
			fmt.Fprintf(&b, "… output cap reached — %d of %d results shown; narrow the query\n",
				shown, report.TotalHits)
			break
		}

		fmt.Fprintf(&b, "%d. %s [%s", i+1, res.Path, res.Language)
		if res.Role != "" && res.Role != "source" {
			b.WriteString(", " + res.Role)
		}
		fmt.Fprintf(&b, "] score %.2f\n", res.Score)

		if len(res.Symbols) > 0 {
			fmt.Fprintf(&b, "   symbols: %s\n", strings.Join(res.Symbols, ", "))
		}
		if res.Line > 0 {
			fmt.Fprintf(&b, "   line: %d\n", res.Line)
		}
		if res.Evidence != "" {
			fmt.Fprintf(&b, "   evidence: %s\n", res.Evidence)
		}
		shown++
	}

	if report.TotalHits == 0 {
		b.WriteString("no matches — try a broader symbol/path fragment, drop the language/role filter, or query the task dimension instead\n")
	}

	_ = root
	return b.String()
}

// relWithinRoot normalizes a user/model-supplied file reference to the
// workspace-relative slash form, refusing any path that escapes the
// root (path safety is non-negotiable for a model-facing surface).
//
// v1.3.5: the classification is CROSS-PLATFORM by design. The previous
// implementation leaned on filepath.IsAbs of the running platform, so on
// Windows a POSIX-rooted input such as /etc/passwd (IsAbs there is
// false) slipped through Clean/ToSlash and was accepted as an ordinary
// relative filename — a real escape on a model-facing tool. The rules
// now are:
//
//   - separators are unified FIRST so every check sees one syntax, and
//     the result is always the workspace-relative slash form;
//   - ANY rooted form is rejected — POSIX absolute (/etc/passwd),
//     Windows rooted (\foo, \foo\bar) and UNC (\\server\share,
//     //server/share) all start with a slash after unification;
//   - ANY Windows volume form is rejected (C:/x, C:\x, C:x, D:/secret)
//     regardless of the running platform — a drive-qualified path is
//     never an ordinary relative filename;
//   - a host-absolute path in the RUNNING platform's own syntax may
//     still point INSIDE the workspace (the model saw absolute paths
//     earlier): it is relativized against the root and then subjected
//     to the same traversal checks;
//   - lexical traversal is rejected after normalization (..\ and ../
//     are equivalent, foo/../../secret collapses to ../secret and is
//     refused).
func relWithinRoot(root, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}

	outside := func() (string, error) {
		return "", fmt.Errorf("repo_search: path %q is outside the workspace", p)
	}

	// Unified slash form: one syntax for every classification below.
	unified := strings.ReplaceAll(p, "\\", "/")

	// Host-absolute paths in the running platform's own syntax may
	// still point inside the workspace (the model saw absolute paths
	// earlier): relativize against the root, then subject the result
	// to the traversal checks below. This covers POSIX-absolute input
	// on Unix and drive/UNC input on Windows.
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return outside()
		}
		unified = strings.ReplaceAll(rel, "\\", "/")
	} else {
		// Host-relative per IsAbs — but the FOREIGN syntaxes the
		// host cannot see must still be refused:
		//   - Windows volume forms ("C:/x", "C:x", "D:\secret"):
		//     drive-qualified paths are rooted at a volume, and
		//     "C:x" is drive-relative (rooted at the drive's own
		//     cwd) — never a workspace-relative filename;
		//   - rooted forms with a leading slash once separators
		//     are unified: POSIX-absolute input on Windows
		//     ("/etc/passwd", "//server/share") and Windows
		//     drive-less rooted input ("\foo", "\foo\bar") both
		//     land here with IsAbs reporting false.
		if isWindowsVolumePath(unified) || strings.HasPrefix(unified, "/") {
			return outside()
		}
	}

	cleaned := path.Clean(unified)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned == "/" {
		return "", fmt.Errorf("repo_search: path escapes the workspace root")
	}

	return cleaned, nil
}

// isWindowsVolumePath reports whether a unified (slash-separated) path
// carries a Windows volume prefix: "C:/x", "C:\\x", "C:x", "d:/secret".
// A single ASCII letter followed by a colon is a volume reference on
// Windows; the model-facing boundary refuses it on every platform
// instead of mistaking it for an ordinary relative filename.
func isWindowsVolumePath(unified string) bool {
	if len(unified) < 2 || unified[1] != ':' {
		return false
	}

	c := unified[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ---------------------------------------------------------------------------
// Context-pipeline evidence block
// ---------------------------------------------------------------------------

// EvidenceBlock renders the top repository evidence for a task as a
// compact, bounded text block for the agent's context (the
// SetRepoEvidence provider). Budget-bounded: maxTokens approximated at
// 4 bytes/token, entries capped so the block never dominates the
// optional-block budget.
func (s *Store) EvidenceBlock(root, task string, maxTokens int) string {
	if task == "" {
		return ""
	}

	keywords := ExtractKeywords(task)
	if len(keywords) == 0 {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), searchTimeBudget)
	defer cancel()

	report, err := s.Search(ctx, root, Query{
		Task:  task,
		Limit: 8,
	})
	if err != nil || len(report.Results) == 0 {
		return ""
	}

	byteBudget := maxTokens * 4
	if byteBudget > maxEvidenceBlockBytes {
		byteBudget = maxEvidenceBlockBytes
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Repository evidence (repoindex, %d files indexed; most relevant files for this task):",
		report.TotalHits)

	for _, res := range report.Results {
		if b.Len() >= byteBudget {
			break
		}

		line := fmt.Sprintf("\n- %s [%s", res.Path, res.Language)
		if res.Role != "" && res.Role != "source" {
			line += ", " + res.Role
		}
		line += "]"
		if len(res.Symbols) > 0 {
			line += " symbols: " + strings.Join(res.Symbols, ", ")
		}
		if res.Evidence != "" {
			line += " — " + res.Evidence
		}

		if b.Len()+len(line) > byteBudget {
			break
		}
		b.WriteString(line)
	}

	if b.Len() == 0 {
		return ""
	}

	b.WriteString("\nUse repo_search for more targeted lookups (depsOf/usedBy/testsOf/symbol).")
	return b.String()
}

// maxEvidenceBlockBytes hard-caps the evidence block.
const maxEvidenceBlockBytes = 4 << 10
