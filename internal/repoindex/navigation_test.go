package repoindex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func navTestStore(t *testing.T, files map[string]string) (*NavTool, string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	store := NewStore(filepath.Join(root, ".idx"))
	return NewNavTool(store, func() string { return root }), root
}

func runNav(t *testing.T, tool *NavTool, args map[string]any) (navResult, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, err := tool.Run(context.Background(), raw)
	if err != nil {
		return navResult{}, err
	}
	var res navResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal result: %v (%s)", err, out)
	}
	return res, nil
}

var navSample = "package main\n\nimport \"fmt\"\n\nfunc main() {\n" +
	"\tfmt.Println(\"hello nav\")\n}\n"

func TestOpenBoundedHead(t *testing.T) {
	var body strings.Builder
	body.WriteString("package main\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&body, "// filler line %d\n", i)
	}
	tool, _ := navTestStore(t, map[string]string{"src/big.go": body.String()})

	res, err := runNav(t, tool, map[string]any{"action": "open", "path": "src/big.go"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if res.StartLine != 1 || res.EndLine != navMaxLines {
		t.Fatalf("open must be the bounded head: %d..%d", res.StartLine, res.EndLine)
	}
	if !res.Truncated {
		t.Fatalf("a 301-line file must report truncation")
	}
	if res.TotalLines != 301 {
		t.Fatalf("total lines must be honest: %d", res.TotalLines)
	}
	if !strings.Contains(res.Provenance, "src/big.go") {
		t.Fatalf("provenance must identify the source: %q", res.Provenance)
	}
}

func TestReadExactRange(t *testing.T) {
	tool, _ := navTestStore(t, map[string]string{"a.go": "l1\nl2\nl3\nl4\nl5\n"})
	res, err := runNav(t, tool, map[string]any{"action": "read", "path": "a.go", "line": 2, "endLine": 4})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if res.StartLine != 2 || res.EndLine != 4 {
		t.Fatalf("exact range violated: %d..%d", res.StartLine, res.EndLine)
	}
	if !strings.Contains(res.Content, "2\tl2") || !strings.Contains(res.Content, "4\tl4") {
		t.Fatalf("content must carry numbered lines: %q", res.Content)
	}
}

func TestNavigateByLineAndSymbol(t *testing.T) {
	tool, _ := navTestStore(t, map[string]string{"m.go": navSample})
	res, err := runNav(t, tool, map[string]any{"action": "navigate", "path": "m.go", "symbol": "Println", "context": 2})
	if err != nil {
		t.Fatalf("navigate symbol: %v", err)
	}
	if !strings.Contains(res.Content, "func main()") {
		t.Fatalf("symbol navigation must land on main: %q", res.Content)
	}
	res2, err := runNav(t, tool, map[string]any{"action": "navigate", "path": "m.go", "line": 6, "context": 1})
	if err != nil {
		t.Fatalf("navigate line: %v", err)
	}
	if res2.StartLine != 5 || res2.EndLine != 7 {
		t.Fatalf("line navigation window wrong: %d..%d", res2.StartLine, res2.EndLine)
	}
}

func TestGrepExactPatternSingleFile(t *testing.T) {
	tool, _ := navTestStore(t, map[string]string{
		"x.go": "alpha\nbeta alpha\nGamma\nalpha beta gamma\n",
	})
	res, err := runNav(t, tool, map[string]any{"action": "grep", "path": "x.go", "pattern": "alpha"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if res.Matches != 3 {
		t.Fatalf("exact-substring matches: got %d want 3", res.Matches)
	}
	// Case-sensitive: "Gamma" is not a match for "alpha".
	res2, err := runNav(t, tool, map[string]any{"action": "grep", "path": "x.go", "pattern": "ALPHA"})
	if err != nil {
		t.Fatalf("grep2: %v", err)
	}
	if res2.Matches != 0 {
		t.Fatalf("grep must be case-sensitive: got %d", res2.Matches)
	}
}

func TestGrepTruncationHonest(t *testing.T) {
	var b strings.Builder
	for i := 0; i < navMaxGrepHits+10; i++ {
		fmt.Fprintf(&b, "hit %d target\n", i)
	}
	tool, _ := navTestStore(t, map[string]string{"many.go": b.String()})
	res, err := runNav(t, tool, map[string]any{"action": "grep", "path": "many.go", "pattern": "target"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if res.Matches != navMaxGrepHits+10 || !res.Truncated {
		t.Fatalf("grep must report the TRUE match count and truncation: %d %v", res.Matches, res.Truncated)
	}
}

func TestPathSafetyTraversalRefused(t *testing.T) {
	tool, _ := navTestStore(t, map[string]string{"ok.go": "fine\n"})
	for _, hostile := range []string{"../secret.txt", "..\\secret.txt", "/etc/passwd", "src/../../etc/passwd"} {
		if _, err := runNav(t, tool, map[string]any{"action": "open", "path": hostile}); err == nil {
			t.Fatalf("hostile path %q must be refused", hostile)
		}
	}
	if _, err := runNav(t, tool, map[string]any{"action": "open", "path": "ok.go"}); err != nil {
		t.Fatalf("the honest file must open: %v", err)
	}
}

func TestUnknownActionRefused(t *testing.T) {
	tool, _ := navTestStore(t, map[string]string{"a.go": "x\n"})
	if _, err := runNav(t, tool, map[string]any{"action": "delete", "path": "a.go"}); err == nil {
		t.Fatalf("nav is read-only: unknown/write actions must be refused")
	}
}
