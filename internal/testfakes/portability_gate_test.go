package testfakes

// portability_gate_test.go — the lightweight regression gate that keeps
// POSIX-only fake executables out of the Go test suites.
//
// v1.3.5 history: four test packages manufactured subprocess fixtures as
// shell scripts ("#!/bin/sh …"). They passed on Linux, silently became
// "not an executable" on Windows, and stalled the Windows release
// pipeline at Go verification. All of them now use the re-exec helper in
// this package; this gate fails the build the moment a shell-backed
// fake executable reappears in any *_test.go file.
//
// Two strictness tiers:
//
//   - SCRIPT FIXTURES (shebangs, /bin/sh, /bin/bash) are banned in every
//     test file without exception — the only reason a test file needs a
//     shell interpreter path or a shebang is to hand-craft a fake
//     executable, and that is exactly what this package's re-exec
//     mechanism replaces.
//   - SHELL COMMAND STRINGS ("sh -c", "cmd /c", …) are banned in every
//     test file. v1.4.0: there are NO allowlisted files — the previous
//     two entries (repair_test.go, lab_test.go) are gone: the repair
//     fixture now uses the testfakes re-exec helper (a real subprocess
//     outcome, no shell string), and lab_test.go carries no banned
//     pattern at all. A future test that genuinely needs a shell
//     command string through production machinery must add an entry
//     here with a written justification — a conscious, reviewable
//     decision, and the lookup is path-separator-safe on every
//     platform (v1.4.0: run 35996462352 proved a backslash rel path
//     silently defeated the forward-slash keys on Windows).
//
// Production shell usage outside _test.go files (internal/lab/runner.go,
// termshell sandbox profile, packaging scripts) is deliberate and out of
// scope.

import (
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"
)

// scriptFixturePatterns are banned in EVERY test file. Each literal is
// SPLIT here so this gate's own source never matches itself.
func scriptFixturePatterns() []string {
        return []string{
                "#!" + "/bin/sh",
                "#!" + "/bin/bash",
                "#!" + "/usr/bin/env",
                "/bin/" + "sh",
                "/bin/" + "bash",
        }
}

// shellCommandPatterns are banned in every test file EXCEPT the
// allowlisted ones below. Fragments are concatenated for the same
// self-match immunity.
func shellCommandPatterns() []string {
        return []string{
                "bash " + "-c",
                "sh " + "-c",
                "cmd " + "/c",
                "cmd.exe " + "/c",
                "powershell " + "-Command",
                "powershell " + "-command",
        }
}

// allowedShellCommandFiles documents every test file that legitimately
// carries shell command strings, with the reason. Adding an entry here
// is a conscious, reviewable decision.
//
// v1.4.0: EMPTY by design (see the header comment). Keys use forward
// slashes on every platform; the scanner normalizes with
// filepath.ToSlash before lookup so the same key works on Windows.
var allowedShellCommandFiles = map[string]string{}

func TestGoTestsContainNoShellBackedFakes(t *testing.T) {
        root := repoRootForGate(t)
        self, selfErr := os.Executable()

        var violations []string

        err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
                if err != nil {
                        return err
                }

                name := d.Name()
                if d.IsDir() {
                        switch name {
                        case ".git", "node_modules", "dist", "build", "web", "frontend", "testresults":
                                return filepath.SkipDir
                        }
                        return nil
                }

                if !strings.HasSuffix(name, "_test.go") {
                        return nil
                }

                // Never flag this gate's own file: its pattern tables are built
                // from concatenated fragments precisely so the scanner cannot
                // match its own source.
                if strings.HasSuffix(path, "portability_gate_test.go") {
                        return nil
                }

                data, readErr := os.ReadFile(path)
                if readErr != nil {
                        return readErr
                }

                rel, relErr := filepath.Rel(root, path)
                if relErr != nil {
                        rel = path
                }

                // v1.4.0: normalize separators before the allowlist lookup.
                // On Windows filepath.Rel yields backslash paths, which never
                // matched the forward-slash keys — the run 35996462352 defect
                // class that let an allowlisted file be flagged (and the gate
                // test fail) only on Windows.
                rel = filepath.ToSlash(rel)

                text := string(data)

                for _, pattern := range scriptFixturePatterns() {
                        if strings.Contains(text, pattern) {
                                violations = append(violations,
                                        rel+": script-fixture pattern "+quoteForGate(pattern))
                        }
                }

                if _, allowed := allowedShellCommandFiles[rel]; !allowed {
                        for _, pattern := range shellCommandPatterns() {
                                if strings.Contains(text, pattern) {
                                        violations = append(violations,
                                                rel+": shell-command pattern "+quoteForGate(pattern)+" — use the testfakes re-exec mechanism or allowlist with justification")
                                }
                        }
                }

                return nil
        })
        if err != nil {
                t.Fatalf("portability gate scan failed: %v", err)
        }

        // The helper binary must be a REAL executable: the running test
        // binary itself. On every supported platform — including Windows
        // .exe — os.Executable() resolves and is directly spawnable, which
        // is exactly why the re-exec mechanism cannot rot into a shell
        // fixture.
        if selfErr != nil {
                t.Fatalf("the re-exec fake mechanism requires os.Executable(): %v", selfErr)
        }
        if info, statErr := os.Stat(self); statErr != nil || info.IsDir() {
                t.Fatalf("test binary %q is not a spawnable executable: %v", self, statErr)
        }

        if len(violations) > 0 {
                t.Fatalf("POSIX-only fake executables are banned from Go tests — use the testfakes re-exec mechanism (internal/testfakes):\n%s",
                        strings.Join(violations, "\n"))
        }
}

func repoRootForGate(t *testing.T) string {
        t.Helper()

        _, thisFile, _, ok := runtime.Caller(0)
        if !ok {
                t.Fatal("cannot locate the gate source file")
        }

        // this file lives at <root>/internal/testfakes/portability_gate_test.go
        root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

        info, err := os.Stat(filepath.Join(root, "go.mod"))
        if err != nil || info.IsDir() {
                t.Fatalf("gate root %q does not look like the repository (go.mod missing)", root)
        }
        return root
}

func quoteForGate(s string) string {
        return "\"" + s + "\""
}
