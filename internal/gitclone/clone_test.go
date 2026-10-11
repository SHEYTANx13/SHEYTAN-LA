// clone_test.go — v1.3.0 regression tests for the GitHub clone workflow.
//
// The tests inject a FAKE git executable through Options.GitBinary so
// every contract — URL validation, destination collision,
// git-unavailable, cancellation, failed clone, successful clone — is
// exercised against the REAL process machinery (proc.CommandContext,
// tree-kill, bounded capture) without the network.
//
// v1.3.5: the fake git is no longer a POSIX shell script (which is not
// an executable on Windows and stalled the Windows release pipeline).
// The fixture is the TEST BINARY ITSELF, re-executed as a helper via
// internal/testfakes (one reusable cross-platform mechanism for every
// package): a real executable on Windows and Unix, dispatched on a
// mode variable and the production argv.
package gitclone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/testfakes"
)

// TestMain arms the re-exec helper: when the production process
// machinery spawns this test binary as the fake git, the helper runs
// the scripted behavior instead of the test suite.
func TestMain(m *testing.M) {
	if testfakes.RunFakeGit(os.Args[1:]) {
		return // the helper ran and ended the process
	}
	os.Exit(m.Run())
}

// fakeGit returns the fake git executable path for the scripted mode.
// The executable is the test binary itself — a real executable on every
// supported platform — and the mode travels through the spawn
// environment (gitEnv() passes the parent environment through), with
// automatic restore via t.Setenv.
func fakeGit(t *testing.T, mode string) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}

	t.Setenv(testfakes.GitModeEnv, mode)
	return exe
}

// fakeSuccessGit clones by creating the destination with a .git dir and
// a worktree file, emitting realistic --progress lines on stderr.
func fakeSuccessGit(t *testing.T) string {
	return fakeGit(t, "success")
}

// waitJob polls the job until terminal or the deadline expires.
func waitJob(t *testing.T, j *Job, timeout time.Duration) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		st := j.Status()
		if st.State != StateRunning {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never reached a terminal state: %+v", st)
		}
		select {
		case <-j.Done():
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func targetForTest(t *testing.T) Target {
	t.Helper()
	target, err := ValidateGitHubURL("https://github.com/owner/repository.git")
	if err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
	return target
}

func TestValidateGitHubURLForms(t *testing.T) {
	accepted := []struct {
		raw, owner, repo, normalized string
		ssh                          bool
	}{
		{"https://github.com/owner/repository", "owner", "repository", "https://github.com/owner/repository.git", false},
		{"https://github.com/owner/repository.git", "owner", "repository", "https://github.com/owner/repository.git", false},
		{"https://github.com/owner/repository/", "owner", "repository", "https://github.com/owner/repository.git", false},
		{"github.com/owner/repository", "owner", "repository", "https://github.com/owner/repository.git", false},
		{"git@github.com:owner/repository.git", "owner", "repository", "git@github.com:owner/repository.git", true},
		{"git@github.com:owner/repository", "owner", "repository", "git@github.com:owner/repository.git", true},
		{"ssh://git@github.com/owner/repository", "owner", "repository", "ssh://git@github.com/owner/repository", true},
	}

	for _, tc := range accepted {
		target, err := ValidateGitHubURL(tc.raw)
		if err != nil {
			t.Fatalf("ValidateGitHubURL(%q) rejected: %v", tc.raw, err)
		}
		if target.Owner != tc.owner || target.Repo != tc.repo {
			t.Fatalf("%q parsed owner/repo = %s/%s, want %s/%s", tc.raw, target.Owner, target.Repo, tc.owner, tc.repo)
		}
		if target.SSH != tc.ssh {
			t.Fatalf("%q SSH = %v, want %v", tc.raw, target.SSH, tc.ssh)
		}
		if target.NormalizedURL != tc.normalized {
			t.Fatalf("%q normalized = %q, want %q", tc.raw, target.NormalizedURL, tc.normalized)
		}
	}

	rejected := []string{
		"",
		"   ",
		"https://gitlab.com/owner/repository",
		"https://github.com/owner/repository?x=1",
		"https://github.com/owner/repository#frag",
		"https://user:token@github.com/owner/repository",
		"https://github.com:8443/owner/repository",
		"https://github.com/onlyowner",
		"https://github.com/owner/repo/extra/deep",
		"file:///C:/something",
		"C:\\local\\path",
		"/local/path",
		"http://github.com/owner/repository",
		"git@gitlab.com:owner/repo.git",
		"just-a-word",
	}

	for _, raw := range rejected {
		if _, err := ValidateGitHubURL(raw); err == nil {
			t.Fatalf("ValidateGitHubURL(%q) accepted, want rejection", raw)
		}
	}
}

func TestSuccessfulClone(t *testing.T) {
	git := fakeSuccessGit(t)
	parent := t.TempDir()
	dest := filepath.Join(parent, "repository")

	j := Start(targetForTest(t), dest, "", Options{GitBinary: git, Timeout: 30 * time.Second})
	st := waitJob(t, j, 15*time.Second)

	if st.State != StateSucceeded {
		t.Fatalf("state = %s (%s: %s) tail=%v", st.State, st.ErrorKind, st.ErrorMessage, st.OutputTail)
	}
	if st.Head != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("HEAD = %q", st.Head)
	}
	if st.Percent != 100 {
		t.Fatalf("final percent = %d", st.Percent)
	}
	if !strings.Contains(st.Message, "owner/repository") {
		t.Fatalf("message missing repo identity: %q", st.Message)
	}

	// The clone really landed.
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git", "HEAD")); err != nil {
		t.Fatalf(".git missing: %v", err)
	}
}

func TestCloneProgressParsing(t *testing.T) {
	ok, pct := parseProgressLine("Receiving objects:  45% (12/26), 2.31 MiB | 1.20 MiB/s")
	if !ok || pct != 5+45*85/100 {
		t.Fatalf("Receiving 45%% parsed to (%v,%d)", ok, pct)
	}
	ok, pct = parseProgressLine("Resolving deltas: 100% (10/10), done.")
	if !ok || pct != 95 {
		t.Fatalf("Resolving 100%% parsed to (%v,%d)", ok, pct)
	}
	ok, _ = parseProgressLine("remote: Enumerating objects: 26, done.")
	if ok {
		t.Fatal("Enumerating without a percent must not move progress")
	}
	ok, pct = parseProgressLine("Receiving objects: 100% (26/26), done.")
	if !ok || pct != 90 {
		t.Fatalf("Receiving 100%% = %d, want 90", pct)
	}
}

func TestCloneDestinationCollision(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "repository")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	j := Start(targetForTest(t), dest, "", Options{GitBinary: fakeSuccessGit(t)})
	st := waitJob(t, j, 15*time.Second)

	if st.State != StateFailed {
		t.Fatalf("state = %s, want failed", st.State)
	}
	if st.ErrorKind != string(ErrDestExists) {
		t.Fatalf("errorKind = %s, want destination-exists (message: %s)", st.ErrorKind, st.ErrorMessage)
	}

	// The unrelated existing content survives untouched.
	if _, err := os.Stat(filepath.Join(dest, "existing.txt")); err != nil {
		t.Fatalf("existing file disturbed: %v", err)
	}
}

func TestCloneDestinationEmptyDirAllowed(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "repository")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	j := Start(targetForTest(t), dest, "", Options{GitBinary: fakeSuccessGit(t)})
	st := waitJob(t, j, 15*time.Second)
	if st.State != StateSucceeded {
		t.Fatalf("state = %s (%s)", st.State, st.ErrorMessage)
	}
}

func TestCloneGitUnavailable(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "repository")

	j := Start(targetForTest(t), dest, "", Options{GitBinary: filepath.Join(parent, "definitely-missing-git")})
	st := waitJob(t, j, 15*time.Second)

	if st.State != StateFailed {
		t.Fatalf("state = %s", st.State)
	}
	if st.ErrorKind != string(ErrGitUnavailable) {
		t.Fatalf("errorKind = %s, want git-unavailable", st.ErrorKind)
	}
	if !strings.Contains(st.ErrorMessage, "git-scm.com") {
		t.Fatalf("git-unavailable message is not actionable: %q", st.ErrorMessage)
	}
}

func TestCloneFailureClassification(t *testing.T) {
	cases := []struct {
		name string
		mode string
		kind ErrorKind
	}{
		{
			name: "repository not found",
			mode: "not-found",
			kind: ErrNotFound,
		},
		{
			name: "authentication failure",
			mode: "auth",
			kind: ErrAuth,
		},
		{
			name: "network failure",
			mode: "network",
			kind: ErrNetwork,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			j := Start(targetForTest(t), filepath.Join(parent, "repository"), "", Options{
				GitBinary: fakeGit(t, tc.mode),
			})
			st := waitJob(t, j, 15*time.Second)

			if st.State != StateFailed {
				t.Fatalf("state = %s", st.State)
			}
			if st.ErrorKind != string(tc.kind) {
				t.Fatalf("errorKind = %s, want %s (message: %s)", st.ErrorKind, tc.kind, st.ErrorMessage)
			}
			if strings.TrimSpace(st.ErrorMessage) == "" {
				t.Fatal("failure message must never be empty")
			}
		})
	}
}

func TestCloneCancellation(t *testing.T) {
	// A git that genuinely stays alive mid-clone until the real
	// process-tree cancellation terminates it.
	git := fakeGit(t, "hang")

	parent := t.TempDir()
	j := Start(targetForTest(t), filepath.Join(parent, "repository"), "", Options{
		GitBinary: git,
		Timeout:   30 * time.Second,
	})

	// Wait for it to be mid-clone, then cancel.
	deadline := time.Now().Add(5 * time.Second)
	for j.Status().Percent < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	j.Cancel()

	st := waitJob(t, j, 15*time.Second)
	if st.State != StateCanceled {
		t.Fatalf("state = %s, want canceled (kind=%s msg=%s)", st.State, st.ErrorKind, st.ErrorMessage)
	}
}

func TestValidateDestinationCases(t *testing.T) {
	parent := t.TempDir()

	// Non-existent destination under an existing parent: allowed.
	if err := ValidateDestination(filepath.Join(parent, "newrepo")); err != nil {
		t.Fatalf("fresh destination rejected: %v", err)
	}

	// Empty dir: allowed.
	empty := filepath.Join(parent, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDestination(empty); err != nil {
		t.Fatalf("empty destination rejected: %v", err)
	}

	// Non-empty dir: refused.
	busy := filepath.Join(parent, "busy")
	if err := os.MkdirAll(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDestination(busy); err == nil {
		t.Fatal("non-empty destination accepted")
	}

	// Destination is a file: refused.
	file := filepath.Join(parent, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDestination(file); err == nil {
		t.Fatal("file destination accepted")
	}

	// Parent missing: refused.
	if err := ValidateDestination(filepath.Join(parent, "missing", "repo")); err == nil {
		t.Fatal("missing parent accepted")
	}

	// Relative path: refused.
	if err := ValidateDestination("relative/repo"); err == nil {
		t.Fatal("relative destination accepted")
	}
}

func TestGitEnvIsNonInteractive(t *testing.T) {
	env := gitEnv()

	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("GIT_TERMINAL_PROMPT=0 missing — a credential prompt could hang the job")
	}

	// Overrides win: no duplicate keys.
	seen := map[string]int{}
	for _, kv := range env {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		seen[key]++
		if key == "GIT_TERMINAL_PROMPT" && seen[key] > 1 {
			t.Fatal("duplicate GIT_TERMINAL_PROMPT entry")
		}
	}
}

func TestCloneWithBranch(t *testing.T) {
	// The fake git inspects the REAL production argv and refuses
	// unless --branch release arrived through the process boundary.
	git := fakeGit(t, "branch-release")

	parent := t.TempDir()
	j := Start(targetForTest(t), filepath.Join(parent, "repository"), "release", Options{GitBinary: git})
	st := waitJob(t, j, 15*time.Second)

	if st.State != StateSucceeded {
		t.Fatalf("state = %s kind=%s msg=%s tail=%v", st.State, st.ErrorKind, st.ErrorMessage, st.OutputTail)
	}
	if st.Branch != "release" {
		t.Fatalf("status branch = %q", st.Branch)
	}
}

func TestInvalidBranchNameFiltered(t *testing.T) {
	// Branch names that look like flags must never reach the argument
	// vector — they are filtered to "" before the process spawns.
	for _, bad := range []string{"--upload-pack=evil", "-x", "a..b", "bad/"} {
		if validBranchName(bad) {
			t.Fatalf("validBranchName(%q) = true", bad)
		}
	}
	if !validBranchName("release-1.2") {
		t.Fatal("valid branch name rejected")
	}
}

func TestAvailableDetectsMissingGit(t *testing.T) {
	t.Setenv("SHEYTAN_GIT_BINARY", "/nonexistent/path/to/git")

	if err := Available(); err == nil {
		t.Fatal("Available() must fail when git cannot be found")
	} else if ClassifyError(err) != ErrGitUnavailable {
		t.Fatalf("classified as %v", ClassifyError(err))
	}
}
