package repoindex

// navigation.go — v1.9.0 DETERMINISTIC REPOSITORY NAVIGATION (spec §7).
//
// The complex retrieval workflow over the ONE index:
//
//      search → inspect → refine → open → navigate → read/grep → verify
//
// repo_search (tool.go) owns the SEARCH half. This file adds the
// navigation half as ONE tool (`repo_nav`) with deterministic actions:
//
//      open     bounded head of an identified source file
//      navigate line/range/symbol context around an identified location
//      read     exact bounded range
//      grep     exact pattern inside ONE identified source file
//
// Every result carries path/source identity, an exact line range,
// workspace-scoped provenance and an explicit truncation state. Entire
// files are NEVER returned — every action is bounded in lines and bytes.
// All paths go through the SAME path-safety authority as repo_search
// (relWithinRoot): traversal, absolute paths, Windows volumes and UNC
// forms are refused identically on Linux and Windows.

import (
        "context"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
)

// NavToolName is the LLM-visible navigation tool name.
const NavToolName = "repo_nav"

// Navigation bounds — never an entire file.
const (
        navMaxLines     = 120
        navMaxBytes     = 8 << 10
        navMaxFileBytes = 1 << 20
        navMaxGrepHits  = 40
        navDefaultCtx   = 10
)

// NavTool implements the agent tool contract for repository navigation.
type NavTool struct {
        Store *Store
        // Root provides the LIVE workspace root (re-read per call, the same
        // discipline as the repo_search tool).
        Root func() string
}

// NewNavTool builds the repo_nav tool.
func NewNavTool(store *Store, root func() string) *NavTool {
        return &NavTool{Store: store, Root: root}
}

// Name returns the tool name.
func (t *NavTool) Name() string { return NavToolName }

// Description returns the LLM-facing description.
func (t *NavTool) Description() string {
        return "Navigate identified source files with deterministic, bounded " +
                "actions: open (bounded head), navigate (context around a line or " +
                "symbol), read (exact line range) and grep (exact pattern inside one " +
                "file). Use after repo_search to inspect evidence precisely — " +
                "results carry exact ranges and never exceed the bound."
}

// ShortDescription is the compact UI-facing summary.
func (t *NavTool) ShortDescription() string {
        return "Open, navigate, read or grep inside identified workspace files (bounded)"
}

type navParams struct {
        Action  string `json:"action" jsonschema:"one of: open, navigate, read, grep"`
        Path    string `json:"path" jsonschema:"workspace-relative (or previously reported) path of the identified source file"`
        Line    int    `json:"line,omitempty" jsonschema:"line number (1-based) for navigate/read ranges"`
        EndLine int    `json:"endLine,omitempty" jsonschema:"inclusive end line for read (bounded)"`
        Symbol  string `json:"symbol,omitempty" jsonschema:"symbol to navigate to (first occurrence wins)"`
        Pattern string `json:"pattern,omitempty" jsonschema:"exact substring pattern for grep (case-sensitive)"`
        Context int    `json:"context,omitempty" jsonschema:"lines of context for navigate (default 10, max 40)"`
}

// Parameters returns the JSON schema.
func (t *NavTool) Parameters() any { return navParams{} }

type navResult struct {
        Action     string `json:"action"`
        Path       string `json:"path"`
        StartLine  int    `json:"startLine"`
        EndLine    int    `json:"endLine"`
        TotalLines int    `json:"totalLines"`
        Truncated  bool   `json:"truncated"`
        Content    string `json:"content"`
        Matches    int    `json:"matches,omitempty"`
        Provenance string `json:"provenance"`
}

// Run executes one navigation action.
func (t *NavTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
        root := strings.TrimSpace(t.Root())
        if root == "" {
                return "", fmt.Errorf("repo_nav: no workspace root is active")
        }

        var p navParams
        if err := json.Unmarshal(args, &p); err != nil {
                return "", fmt.Errorf("repo_nav: invalid arguments: %w", err)
        }
        p.Action = strings.ToLower(strings.TrimSpace(p.Action))

        rel, err := relWithinRoot(root, p.Path)
        if err != nil {
                return "", err
        }
        if rel == "" {
                return "", fmt.Errorf("repo_nav: path is required")
        }

        abs := filepath.Join(root, filepath.FromSlash(rel))
        info, err := os.Stat(abs)
        if err != nil {
                return "", fmt.Errorf("repo_nav: cannot stat %s: %w", rel, err)
        }
        if info.IsDir() {
                return "", fmt.Errorf("repo_nav: %s is a directory — identify a source file", rel)
        }
        if info.Size() > navMaxFileBytes {
                return "", fmt.Errorf("repo_nav: %s exceeds the %d byte navigation bound", rel, navMaxFileBytes)
        }

        data, err := os.ReadFile(abs)
        if err != nil {
                return "", fmt.Errorf("repo_nav: cannot read %s: %w", rel, err)
        }
        lines := splitLines(string(data))

        res := navResult{
                Action:     p.Action,
                Path:       rel,
                TotalLines: len(lines),
                Provenance: fmt.Sprintf("workspace:%s#%s", filepath.Base(root), rel),
        }

        switch p.Action {
        case "open":
                end := navMaxLines
                if len(lines) < end {
                        end = len(lines)
                }
                res.StartLine, res.EndLine = 1, end
                res.Truncated = len(lines) > end
                res.Content = renderRange(lines, 1, end)

        case "navigate":
                start := p.Line
                if start <= 0 && strings.TrimSpace(p.Symbol) != "" {
                        start = findSymbolLine(lines, p.Symbol)
                        if start <= 0 {
                                return "", fmt.Errorf("repo_nav: symbol %q not found in %s", p.Symbol, rel)
                        }
                }
                if start <= 0 {
                        return "", fmt.Errorf("repo_nav: navigate needs a line or a symbol")
                }
                ctxLines := p.Context
                if ctxLines <= 0 {
                        ctxLines = navDefaultCtx
                }
                if ctxLines > 40 {
                        ctxLines = 40
                }
                from := start - ctxLines
                if from < 1 {
                        from = 1
                }
                to := start + ctxLines
                if to > len(lines) {
                        to = len(lines)
                }
                if to-from+1 > navMaxLines {
                        to = from + navMaxLines - 1
                }
                res.StartLine, res.EndLine = from, to
                res.Truncated = to < len(lines) && (p.Context > 40 || to == from+navMaxLines-1)
                res.Content = renderRange(lines, from, to)

        case "read":
                if p.Line <= 0 {
                        return "", fmt.Errorf("repo_nav: read needs a start line")
                }
                from := p.Line
                to := p.EndLine
                if to < from {
                        to = from + navMaxLines - 1
                }
                if to-from+1 > navMaxLines {
                        to = from + navMaxLines - 1
                }
                if from > len(lines) {
                        return "", fmt.Errorf("repo_nav: start line %d beyond end of file (%d lines)", from, len(lines))
                }
                if to > len(lines) {
                        to = len(lines)
                }
                res.StartLine, res.EndLine = from, to
                res.Truncated = to < len(lines)
                res.Content = renderRange(lines, from, to)

        case "grep":
                pattern := p.Pattern
                if pattern == "" {
                        return "", fmt.Errorf("repo_nav: grep needs a pattern")
                }
                var b strings.Builder
                matches := 0
                truncated := false
                for i, line := range lines {
                        if strings.Contains(line, pattern) {
                                matches++
                                // The match COUNT stays honest (total); the CONTENT is
                                // bounded — the explicit truncation flag covers the rest.
                                if matches <= navMaxGrepHits {
                                        fmt.Fprintf(&b, "%d: %s\n", i+1, line)
                                } else {
                                        truncated = true
                                }
                        }
                }
                res.StartLine, res.EndLine = 0, 0
                res.Matches = matches
                res.Truncated = truncated
                res.Content = b.String()

        default:
                return "", fmt.Errorf("repo_nav: unknown action %q (open | navigate | read | grep)", p.Action)
        }

        // Byte bound on the rendered content (the line bounds already bound
        // the common case; this catches pathological long lines).
        if len(res.Content) > navMaxBytes {
                res.Content = res.Content[:navMaxBytes]
                res.Truncated = true
        }

        out, err := json.Marshal(res)
        if err != nil {
                return "", err
        }
        return string(out), nil
}

// splitLines splits without dropping the final line's newline state.
func splitLines(s string) []string {
        s = strings.ReplaceAll(s, "\r\n", "\n")
        lines := strings.Split(s, "\n")
        // A trailing newline produces one empty last element — it is not a
        // real line for numbering purposes.
        if len(lines) > 0 && lines[len(lines)-1] == "" {
                lines = lines[:len(lines)-1]
        }
        return lines
}

func renderRange(lines []string, from, to int) string {
        var b strings.Builder
        for i := from; i <= to && i-1 < len(lines); i++ {
                fmt.Fprintf(&b, "%d\t%s\n", i, lines[i-1])
        }
        return b.String()
}

// findSymbolLine locates the first line containing the symbol as a
// standalone token (deterministic scan; the index's symbol list guided
// the SEARCH, this pins the LOCATION).
func findSymbolLine(lines []string, symbol string) int {
        if strings.TrimSpace(symbol) == "" {
                return 0
        }
        for i, line := range lines {
                if containsToken(line, symbol) {
                        return i + 1
                }
        }
        return 0
}

func containsToken(line, sym string) bool {
        idx := 0
        for {
                j := strings.Index(line[idx:], sym)
                if j < 0 {
                        return false
                }
                start := idx + j
                end := start + len(sym)
                beforeOK := start == 0 || !isWordByte(line[start-1])
                afterOK := end >= len(line) || !isWordByte(line[end])
                if beforeOK && afterOK {
                        return true
                }
                idx = start + 1
                if idx >= len(line) {
                        return false
                }
        }
}

func isWordByte(b byte) bool {
        return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
