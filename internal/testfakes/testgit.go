// Package testfakes implements the cross-platform subprocess-fake
// mechanism used by the Go test suites.
//
// HISTORY: before v1.3.5 the gitclone / api / llm / native-engine tests
// manufactured "fake git" / "fake engine" executables as POSIX shell
// scripts ("#!/bin/sh …"). Those are not executables on Windows, so the
// Windows CI job reported the fake binary as unavailable and the whole
// release pipeline stalled at Go verification. This package replaces
// every shell-script fake with ONE reusable mechanism: the test binary
// re-executes ITSELF as the subprocess (a real executable on every
// supported platform — no shell, no network), dispatching on a
// test-selected mode variable.
//
// USAGE (producer side — the test):
//
//	func fakeGit(t *testing.T, mode string) string {
//		t.Helper()
//		exe, err := os.Executable()
//		if err != nil { t.Fatal(err) }
//		t.Setenv(GitModeEnv, mode) // inherited by the spawned subprocess
//		return exe                  // a real executable, including Windows .exe
//	}
//
// USAGE (consumer side — the package's TestMain):
//
//	func TestMain(m *testing.M) {
//		if testfakes.RunFakeGit(os.Args[1:]) {
//			return // the helper ran and terminated the process
//		}
//		os.Exit(m.Run())
//	}
//
// The mode variable travels through the production spawn environment
// (gitclone's gitEnv() and the llm device enumeration pass the parent
// environment through). The dispatch is additionally gated on a
// git-shaped argv, so a leaked environment variable can never turn a
// normal test-binary run into a fake subprocess. Behaviors are
// deterministic, shell-free, network-free, and safe under temporary
// paths containing spaces (argv is passed directly to exec, never
// interpolated into a command string).
package testfakes

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GitModeEnv is the environment variable that turns a re-executed test
// binary into a fake git subprocess. Values are the scripted scenarios
// below ("success", "branch-release", "not-found", "auth", "network",
// "hang").
const GitModeEnv = "SHEYTAN_FAKE_GIT_MODE"

// fakeHeadCommit is the deterministic 40-character commit the fake git
// reports for every successful clone.
const fakeHeadCommit = "0123456789abcdef0123456789abcdef01234567"

// RunFakeGit executes the fake-git behavior when this process was
// started as one: the mode variable is set AND the argv is git-shaped
// (first argument after the binary is one of the git subcommands the
// production code invokes). When it handles the invocation it NEVER
// returns normally — the scripted behavior ends the process with the
// scripted exit code. When the invocation is not a fake-git one (a
// normal test run: unset mode, or a go-test flag argv) it returns false
// and the caller continues as a test binary.
func RunFakeGit(argv []string) bool {
	mode := os.Getenv(GitModeEnv)
	if mode == "" || len(argv) == 0 {
		return false
	}

	// Gate on the git-shaped argv: `go test` never invokes the binary
	// with a git subcommand as the first flag, so a stray mode variable
	// in the outer environment cannot hijack a real test run.
	switch argv[0] {
	case "clone", "-C", "rev-parse":
	default:
		return false
	}

	switch mode {
	case "success":
		runGitSuccess(argv)
	case "branch-release":
		runGitBranchRelease(argv)
	case "not-found":
		fatalGit(128, "fatal: repository 'https://github.com/owner/repository.git/' not found")
	case "auth":
		fatalGit(128, "fatal: could not read Username for 'https://github.com': terminal prompts disabled")
	case "network":
		fatalGit(128, "fatal: unable to access 'https://github.com/owner/repository.git/': Could not resolve host: github.com")
	case "hang":
		runGitHang(argv)
	default:
		fmt.Fprintf(os.Stderr, "fake git: unknown mode %q\n", mode)
		os.Exit(64)
	}

	return true // unreachable: every behavior ends the process
}

// lastArg returns the final argv element — the destination directory for
// `git clone … <dest>` (the production argument vector ends with it).
func lastArg(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[len(argv)-1]
}

// seedWorktree materializes the representative clone result: the
// destination, a .git directory with HEAD, and a worktree file.
func seedWorktree(dest, headRef string) {
	_ = os.MkdirAll(filepath.Join(dest, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(dest, "README.md"), []byte("readme\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dest, ".git", "HEAD"), []byte(headRef+"\n"), 0o644)
}

// runGitSuccess emulates a well-behaved git: `clone` emits realistic
// --progress stderr lines and creates the destination; `-C <dest>
// rev-parse HEAD` (the production verification step) prints the
// deterministic commit.
func runGitSuccess(argv []string) {
	switch argv[0] {
	case "clone":
		fmt.Fprintln(os.Stderr, "Cloning into 'dest'...")
		fmt.Fprintln(os.Stderr, "remote: Enumerating objects: 26, done.")
		fmt.Fprintln(os.Stderr, "Receiving objects:  50% (13/26)")
		time.Sleep(50 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "Receiving objects: 100% (26/26), done.")
		fmt.Fprintln(os.Stderr, "Resolving deltas: 100% (10/10), done.")
		seedWorktree(lastArg(argv), "ref: refs/heads/main")
		os.Exit(0)
	case "-C":
		// git -C <dest> rev-parse HEAD
		fmt.Println(fakeHeadCommit)
		os.Exit(0)
	case "rev-parse":
		fmt.Println(fakeHeadCommit)
		os.Exit(0)
	}
	os.Exit(1)
}

// runGitBranchRelease emulates the branch contract: the fake git
// INSPECTS the real production argv and refuses unless --branch release
// was passed, so the test proves the flag travelled through the real
// process boundary instead of asserting it in-process.
func runGitBranchRelease(argv []string) {
	switch argv[0] {
	case "clone":
		branch := ""
		for i := 0; i < len(argv)-1; i++ {
			if argv[i] == "--branch" {
				branch = argv[i+1]
			}
		}
		if branch != "release" {
			fmt.Fprintf(os.Stderr, "fake git: --branch release not received (got %q)\n", branch)
			os.Exit(64)
		}
		fmt.Fprintln(os.Stderr, "Cloning into 'dest'...")
		seedWorktree(lastArg(argv), "ref: refs/heads/release")
		os.Exit(0)
	case "-C":
		fmt.Println(fakeHeadCommit)
		os.Exit(0)
	}
	os.Exit(1)
}

// runGitHang emulates a clone that genuinely stays alive mid-transfer
// until the caller's cancellation (proc.CommandContext tree termination)
// kills it — the process-tree teardown contract needs a live victim, not
// a process that exits on its own.
func runGitHang(argv []string) {
	if argv[0] != "clone" {
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "Cloning into 'dest'...")
	fmt.Fprintln(os.Stderr, "Receiving objects:  10% (1/26)")

	// Remain alive well past every test deadline; the supervisor's
	// cancellation is expected to terminate this process tree. There is
	// deliberately no self-exit before the bound below.
	for i := 0; i < 600; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	os.Exit(0)
}

// fatalGit prints the scripted git failure to stderr and exits with the
// scripted code — the deterministic classification fixtures.
func fatalGit(code int, message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(code)
}
