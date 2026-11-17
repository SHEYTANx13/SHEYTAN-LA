// file_catalog_contract_test.go — the FILE_CATALOG coverage contract
// (v1.11.1 §8.2): every tracked file appears in the catalog EXCEPT the
// catalog's own two files (explicit policy exclusion) — a new file added
// without a catalog regeneration fails HERE, not silently. Every entry
// must carry purpose/type/layer/subsystem/status; statuses come from the
// documented vocabulary.
package releasecontract

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// catalogPolicyExclusions are the catalog's own files: the catalog does
// not describe itself (self-reference would churn every regeneration).
var catalogPolicyExclusions = map[string]bool{
	"docs/architecture/FILE_CATALOG.json": true,
	"docs/architecture/FILE_CATALOG.md":   true,
}

// catalogEntryStatusVocabulary — the documented status set.
var catalogEntryStatusVocabulary = map[string]bool{
	"active":                           true,
	"test":                             true,
	"fixture":                          true,
	"generated":                        true,
	"poc (not built into the product)": true,
	"needs review":                     true,
}

func TestFileCatalogCoversEveryTrackedFile(t *testing.T) {
	raw := readTreeFile(t, "docs/architecture/FILE_CATALOG.json")

	var entries []struct {
		Path      string `json:"path"`
		Type      string `json:"type"`
		Purpose   string `json:"purpose"`
		Layer     string `json:"layer"`
		Subsystem string `json:"subsystem"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("FILE_CATALOG.json does not decode: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("FILE_CATALOG.json is empty — the catalog was never generated (go run ./scripts/gen-filecatalog)")
	}

	// The inventory authority: git ls-files (tracked files ONLY — caches
	// and build products never appear), executed from the REPO ROOT so
	// the listing covers the whole tree with root-relative paths (the
	// test's own working directory is the package dir, where a bare
	// `git ls-files` would list only that subtree — the exact cwd-scoping
	// defect this contract would otherwise miss).
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files failed (the coverage contract needs the tracked-file inventory): %v", err)
	}
	tracked := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			tracked[line] = true
		}
	}

	inCatalog := map[string]bool{}
	for _, e := range entries {
		if strings.TrimSpace(e.Path) == "" {
			t.Fatal("a catalog entry carries no path")
		}
		if inCatalog[e.Path] {
			t.Errorf("catalog lists %q twice — regenerate the catalog", e.Path)
		}
		inCatalog[e.Path] = true

		if strings.TrimSpace(e.Purpose) == "" {
			t.Errorf("catalog entry %q has no purpose — a row must say what the file is, never just echo its name", e.Path)
		}
		if strings.TrimSpace(e.Type) == "" || strings.TrimSpace(e.Layer) == "" || strings.TrimSpace(e.Subsystem) == "" {
			t.Errorf("catalog entry %q is missing type/layer/subsystem fields", e.Path)
		}
		if !catalogEntryStatusVocabulary[e.Status] {
			t.Errorf("catalog entry %q has undocumented status %q", e.Path, e.Status)
		}
	}

	// Coverage: every tracked file (minus the catalog's own two files)
	// must be listed; every listed file must be tracked (no ghosts).
	var missing []string
	for p := range tracked {
		if catalogPolicyExclusions[p] {
			continue
		}
		if !inCatalog[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d tracked file(s) missing from the catalog (regenerate with `go run ./scripts/gen-filecatalog` after staging them): %v",
			len(missing), missing)
	}

	var ghosts []string
	for p := range inCatalog {
		if !tracked[p] && !catalogPolicyExclusions[p] {
			ghosts = append(ghosts, p)
		}
	}
	if len(ghosts) > 0 {
		t.Fatalf("%d catalog entries reference files git does not track (stale catalog — regenerate): %v", len(ghosts), ghosts)
	}
}

// TestFileCatalogMarkdownIndexExists pins the human-readable companion's
// presence and the generator pointer (both artifacts regenerate together).
func TestFileCatalogMarkdownIndexExists(t *testing.T) {
	md := readTreeFile(t, "docs/architecture/FILE_CATALOG.md")
	if !strings.Contains(md, "FILE_CATALOG.json") {
		t.Error("the Markdown index must point at its machine-readable companion (FILE_CATALOG.json)")
	}
	if !strings.Contains(md, "gen-filecatalog") {
		t.Error("the Markdown index must document the regeneration command (go run ./scripts/gen-filecatalog)")
	}
}
