package releasecontract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// version_v1111_contract_test.go — run #41415240891 Failure C repair.
//
// The observed commit 4e8c2b8 was TITLED v1.11.1 while every built
// identity surface still reported 1.11.0: the commit title was bumped
// without synchronizing internal/config.AppVersion and its consumers.
// This suite pins the CURRENT release identity fail-closed so the next
// bump cannot ship the same lineage split:
//
//   - internal/config.AppVersion           (THE runtime authority)
//   - build/config.yml  productVersion     (packaging metadata)
//   - SIGNATURE first line                 (release identity, v-spelled)
//   - workflow APP_VERSION env literals    (cross-checked live against
//     printversion on every run — they must equal the authority HERE too,
//     or the hosted cross-check fires after the artifacts are built)
//   - cmd/printversion                     (the CGO-free resolver; its
//     --plain output IS the workflow/packaging version)
//
// A bump edits AppVersion (and regenerates SIGNATURE via
// `go run scripts/gen-signature.go`); this test fails with the exact
// surfaces that still disagree.
//
// Historical 1.11.0 references (changelog entries, migration comments,
// retirement notes) are HISTORY and are deliberately NOT scanned here —
// only current identity declarations are.

// TestCurrentIdentityIsV1111 pins the exact shipped version. A bump must
// consciously update this constant together with AppVersion — the whole
// point of Failure C is that an unsynced bump went unnoticed.
func TestCurrentIdentityIsV1111(t *testing.T) {
	const want = "1.11.1"

	if config.AppVersion != want {
		t.Fatalf("internal/config.AppVersion = %q, want %q — bump the ONE authority and every consumer that this suite lists, then update this pin", config.AppVersion, want)
	}

	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(config.AppVersion) {
		t.Fatalf("AppVersion %q is not a strict MAJOR.MINOR.PATCH release shape", config.AppVersion)
	}
}

// TestWorkflowAppVersionTracksAuthority reads build-desktop.yml and
// asserts every `APP_VERSION: "X"` declaration equals the Go authority,
// and that the resolution step still derives the live value from
// cmd/printversion (never from the literal alone).
func TestWorkflowAppVersionTracksAuthority(t *testing.T) {
	root := findRepoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "build-desktop.yml"))
	if err != nil {
		t.Fatalf("read build-desktop.yml: %v", err)
	}

	text := string(raw)

	decl := regexp.MustCompile(`APP_VERSION:\s*"([^"]+)"`)
	matches := decl.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		t.Fatal("no APP_VERSION declaration found in build-desktop.yml — the version chain is broken")
	}

	for _, m := range matches {
		if m[1] != config.AppVersion {
			t.Errorf("workflow APP_VERSION = %q, want the Go authority %q (the hosted cross-check would fail AFTER the artifacts were built)", m[1], config.AppVersion)
		}
	}

	// The live resolution must still come from the ONE CGO-free helper.
	if !strings.Contains(text, "go run ./cmd/printversion --plain") {
		t.Error("build-desktop.yml no longer resolves APP_VERSION through cmd/printversion — a second hardcoded authority has appeared")
	}
}

// TestPrintversionPlainEmitsAuthority compiles nothing: it asserts the
// resolver contract at the source level (the resolver takes the authority
// verbatim and prints it unprefixed). The exact-output behavior is unit
// pinned in cmd/printversion; HERE the contract is that the workflow
// consumption shape (`--plain`, regex `v?[0-9]+.[0-9]+.[0-9]+`) keeps
// working against the shipped value.
func TestPrintversionPlainEmitsAuthority(t *testing.T) {
	main, err := os.ReadFile(filepath.Join(findRepoRoot(t), "cmd", "printversion", "main.go"))
	if err != nil {
		t.Fatalf("read printversion main.go: %v", err)
	}

	if !strings.Contains(string(main), "config.AppVersion") {
		t.Error("cmd/printversion must resolve internal/config.AppVersion — the ONE authority — not a local literal")
	}
}
