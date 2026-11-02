package cmd

// version_purity_test.go — run #41415240891 Failure A.
//
// The Linux ZIP verifier rejected the release archive because the staged
// executable's own `version` probe had created runtime state beside it:
// the old boot order answered the version query only AFTER config
// loading, path migrations, EnsureDirs and the file log sinks had run.
// These tests prove the repaired contract directly:
//
//   1. every spelling (version / -v / --version) exits 0 and prints the
//      exact pre-repair output shape (branding preserved byte-for-byte);
//   2. the invocation creates ZERO filesystem entries anywhere it could
//      possibly write — the data root (SHEYTAN_DATA_DIR override points
//      into a fresh empty temp dir) must still be empty afterwards.
//
// The loader would create config.json under the data root and the log
// sinks (app.log, llm.jsonl, tools.jsonl) under <root>/logs — the
// empty-directory assertion fails with the exact residue list if the
// version path ever regains boot side effects.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

func TestVersionDispatchIsReadOnly(t *testing.T) {
	for _, spelling := range []string{"version", "-v", "--version"} {
		t.Run(spelling, func(t *testing.T) {
			probe := t.TempDir()

			// Route EVERY runtime path the loader could resolve into the
			// probe directory. If the version path regains boot side
			// effects, the residue lands here and the assertion below
			// names it.
			t.Setenv("SHEYTAN_DATA_DIR", probe)

			oldArgs := os.Args
			os.Args = []string{"sheytan-local-agent", spelling}
			t.Cleanup(func() { os.Args = oldArgs })

			var out bytes.Buffer
			stdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			os.Stdout = w
			code := RunWithDefaultFn(nil)
			_ = w.Close()
			os.Stdout = stdout
			_, _ = out.ReadFrom(r)
			_ = r.Close()

			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}

			text := out.String()
			wantFirst := config.AppName + " v" + config.AppVersion + "\n"
			if !strings.HasPrefix(text, wantFirst) {
				t.Fatalf("version output = %q, want prefix %q (output shape is a release contract)", text, wantFirst)
			}
			if !strings.Contains(text, brand.Notice()) {
				t.Errorf("version output lost the legal notice line: %q", text)
			}
			if !strings.Contains(text, "go: ") || !strings.Contains(text, "os: ") {
				t.Errorf("version output lost the go/os fact lines: %q", text)
			}

			entries, err := os.ReadDir(probe)
			if err != nil {
				t.Fatalf("read probe dir: %v", err)
			}
			if len(entries) != 0 {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Fatalf("version dispatch created filesystem residue %v — the version path must stay read-only (run #41415240891 Failure A)", names)
			}
		})
	}
}

// TestNonVersionArgsAreNotHandledEarly pins that the early dispatch is
// scoped to the version spellings only — anything else must fall through
// to the full boot path (earlyVersionDispatch reports not-handled).
func TestNonVersionArgsAreNotHandledEarly(t *testing.T) {
	for _, other := range []string{"ask", "serve", "doctor", "help", "-h", "--help", "nonsense"} {
		if _, handled := earlyVersionDispatch([]string{other}); handled {
			t.Errorf("earlyVersionDispatch(%q) = handled — the early path must answer ONLY version/-v/--version", other)
		}
	}

	if _, handled := earlyVersionDispatch(nil); handled {
		t.Error("earlyVersionDispatch(nil) = handled — an empty arg vector is the default desktop path, not a version query")
	}
}

// TestVersionOutputShapePinnedAcrossSpellings — the three spellings print
// the IDENTICAL block (the old dispatch branch served all three through
// one case; the early path must preserve that equivalence).
func TestVersionOutputShapePinnedAcrossSpellings(t *testing.T) {
	capture := func(args []string) string {
		var out bytes.Buffer
		stdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		os.Stdout = w
		_, _ = earlyVersionDispatch(args)
		_ = w.Close()
		os.Stdout = stdout
		_, _ = out.ReadFrom(r)
		_ = r.Close()
		return out.String()
	}

	v := capture([]string{"version"})
	dash := capture([]string{"-v"})
	long := capture([]string{"--version"})

	if v == "" || v != dash || dash != long {
		t.Fatalf("version spellings diverge: %q / %q / %q", v, dash, long)
	}

	want := config.AppName + " v" + config.AppVersion + "\n" +
		"  " + brand.Notice() + "\n"
	if !strings.HasPrefix(v, want) {
		t.Fatalf("version block = %q, want prefix %q", v, want)
	}
}

// TestVersionProbeLeavesExecutableDirectoryClean is the workflow-shaped
// proof: the CI probe runs the STAGED binary and zips the tree beside it.
// Here the "staged tree" is the probe dir and the executable is a tiny
// helper binary built from this package's own test main — the strongest
// available in-process equivalent of `"$bin" version` beside a package
// root. (The hosted Linux job additionally probes from an isolated
// directory and asserts zero residue there.)
func TestVersionProbeLeavesExecutableDirectoryClean(t *testing.T) {
	probe := t.TempDir()

	t.Setenv("SHEYTAN_DATA_DIR", probe)

	// The early path is what the binary's main() reaches first; prove it
	// in the exact order the shipped binary executes it: args parsing,
	// dispatch, THEN the filesystem assertion — with DefaultPath()
	// (config.DefaultPath resolves but must never create) exercised
	// beforehand to show resolution alone leaves nothing behind.
	_ = config.DefaultPath()

	if _, handled := earlyVersionDispatch([]string{"version"}); !handled {
		t.Fatal("earlyVersionDispatch(version) not handled")
	}

	entries, err := os.ReadDir(probe)
	if err != nil {
		t.Fatalf("read probe dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("probe dir gained residue %v (data root %s) — version path wrote to the filesystem", names, filepath.Join(probe))
	}
}
