package termshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("alpha ember\nbravo flame\nalpha fire\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

func TestBasicFSCommands(t *testing.T) {
	e := newEngine(t)
	if got := e.Exec("pwd"); got != "~" {
		t.Errorf("pwd = %q, want ~", got)
	}
	if got := e.Exec("ls"); !strings.Contains(got, "notes.md") || !strings.Contains(got, "workspace/") {
		t.Errorf("ls = %q", got)
	}
	if got := e.Exec("cat notes.md"); !strings.Contains(got, "alpha ember") {
		t.Errorf("cat = %q", got)
	}
	if got := e.Exec("cd workspace"); got != "" {
		t.Errorf("cd output = %q", got)
	}
	if got := e.Exec("pwd"); got != "~/workspace" {
		t.Errorf("pwd after cd = %q", got)
	}
	if got := e.Exec("cd ~"); got != "" || e.CWD() != "~" {
		t.Errorf("cd ~ failed: %q cwd=%s", got, e.CWD())
	}
}

func TestPathJail(t *testing.T) {
	e := newEngine(t)
	// Absolute-style path maps INTO the jail: /notes.md == notes.md.
	if got := e.Exec("cat /notes.md"); !strings.Contains(got, "alpha ember") {
		t.Errorf("/notes.md should map inside jail, got %q", got)
	}
	// Escape attempts are refused.
	for _, cmd := range []string{
		"cat ../../etc/passwd",
		"cat ../../../etc/passwd",
		"ls ../..",
		"rm -r ../..",
	} {
		if got := e.Exec(cmd); !strings.Contains(got, "denied") && !strings.Contains(got, "outside") && !strings.Contains(got, "no such file") {
			// ../.. resolves within-host to outside root → must be denied or
			// safely mapped; it can NEVER list the real parent.
			t.Errorf("%s leaked outside jail: %q", cmd, got)
		}
	}
	// rm of the root itself is refused.
	if got := e.Exec("rm -r /"); !strings.Contains(got, "refusing") {
		t.Errorf("rm / = %q, want refusing", got)
	}
}

func TestWriteCommands(t *testing.T) {
	e := newEngine(t)
	e.Exec("mkdir -p projects/sheytan")
	if _, err := os.Stat(filepath.Join(e.Root(), "projects", "sheytan")); err != nil {
		t.Fatalf("mkdir -p failed: %v", err)
	}
	e.Exec("touch projects/a.txt")
	if _, err := os.Stat(filepath.Join(e.Root(), "projects", "a.txt")); err != nil {
		t.Fatalf("touch failed: %v", err)
	}
	e.Exec("echo 'hello world' > /dev/null") // echo has no redirection; harmless
	if got := e.Exec("echo hello world"); got != "hello world" {
		t.Errorf("echo = %q", got)
	}
	e.Exec("cp notes.md projects/notes-copy.md")
	if data, err := os.ReadFile(filepath.Join(e.Root(), "projects", "notes-copy.md")); err != nil || len(data) == 0 {
		t.Errorf("cp failed: %v", err)
	}
	e.Exec("mv projects/a.txt projects/b.txt")
	if _, err := os.Stat(filepath.Join(e.Root(), "projects", "b.txt")); err != nil {
		t.Errorf("mv failed: %v", err)
	}
	if got := e.Exec("rm projects/b.txt"); got != "" {
		t.Errorf("rm output = %q", got)
	}
	if _, err := os.Stat(filepath.Join(e.Root(), "projects", "b.txt")); !os.IsNotExist(err) {
		t.Error("rm did not remove the file")
	}
}

func TestPipes(t *testing.T) {
	e := newEngine(t)
	got := e.Exec("cat notes.md | grep alpha")
	if got != "alpha ember\nalpha fire" {
		t.Errorf("pipe grep = %q", got)
	}
	got = e.Exec("cat notes.md | grep -i ALPHA | wc -l")
	if !strings.Contains(got, "2") {
		t.Errorf("pipe wc -l = %q", got)
	}
	got = e.Exec("cat notes.md | head -n 1")
	if got != "alpha ember" {
		t.Errorf("head = %q", got)
	}
	got = e.Exec("cat notes.md | tail -n 1")
	if got != "alpha fire" {
		t.Errorf("tail = %q", got)
	}
	got = e.Exec("cat notes.md | sort | uniq")
	if !strings.Contains(got, "alpha ember") || !strings.Contains(got, "bravo flame") {
		t.Errorf("sort|uniq = %q", got)
	}
}

func TestQuoting(t *testing.T) {
	e := newEngine(t)
	if got := e.Exec(`echo "hello   world"`); got != "hello   world" {
		t.Errorf("double quotes = %q", got)
	}
	if got := e.Exec(`echo 'a | b'`); got != "a | b" {
		t.Errorf("single quotes must hide pipe: %q", got)
	}
}

func TestUnknownCommandAndHistory(t *testing.T) {
	e := newEngine(t)
	if got := e.Exec("frobnicate"); !strings.Contains(got, "command not found") {
		t.Errorf("unknown = %q", got)
	}
	e.Exec("ls")
	e.Exec("pwd")
	h := e.History()
	if len(h) != 3 || h[0] != "frobnicate" {
		t.Errorf("history = %v", h)
	}
	if got := e.Exec("history"); !strings.Contains(got, "frobnicate") {
		t.Errorf("history cmd = %q", got)
	}
}

func TestIntrospectionCommands(t *testing.T) {
	e := newEngine(t)
	if got := e.Exec("whoami"); got != "user" {
		t.Errorf("whoami = %q", got)
	}
	if got := e.Exec("uname -a"); !strings.Contains(got, "simulated") {
		t.Errorf("uname = %q", got)
	}
	if got := e.Exec("neofetch"); !strings.Contains(got, "SHEYTAN") {
		t.Errorf("neofetch = %q", got)
	}
	if got := e.Exec("clear"); got != ClearMarker {
		t.Errorf("clear = %q", got)
	}
	if got := e.Exec("help"); !strings.Contains(got, "Built-ins") {
		t.Errorf("help = %q", got)
	}
	// ps shows the injected live process.
	e.ProcInfo = func() []ProcEntry {
		return []ProcEntry{{PID: 42, Name: "llama-server", CPU: "18%", RAM: "3.2 GB", Kind: "engine"}}
	}
	if got := e.Exec("ps"); !strings.Contains(got, "llama-server") {
		t.Errorf("ps with ProcInfo = %q", got)
	}
}

func TestEnvAndExport(t *testing.T) {
	e := newEngine(t)
	e.Exec("export PROJECT=sheytan")
	if got := e.Exec("echo $PROJECT"); got != "sheytan" {
		t.Errorf("env expansion = %q", got)
	}
	if got := e.Exec("env"); !strings.Contains(got, "PROJECT=sheytan") {
		t.Errorf("env = %q", got)
	}
}

func TestCatOutputCap(t *testing.T) {
	e := newEngine(t)
	big := filepath.Join(e.Root(), "big.txt")
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		sb.WriteString("line of substantial length for the chunking test bench\n")
	}
	if err := os.WriteFile(big, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := e.Exec("cat big.txt")
	if len(got) > maxOutput+200 {
		t.Errorf("cat big file = %d bytes, exceeds cap %d", len(got), maxOutput)
	}
	if !strings.Contains(got, "truncated by the SHEYTAN data-chunking layer") {
		t.Error("truncation marker missing")
	}
}

func TestTreeAndFind(t *testing.T) {
	e := newEngine(t)
	e.Exec("mkdir -p workspace/deep/deeper")
	e.Exec("touch workspace/deep/file.txt")
	if got := e.Exec("tree"); !strings.Contains(got, "deep") || !strings.Contains(got, "file.txt") {
		t.Errorf("tree = %q", got)
	}
	if got := e.Exec("find . -name file"); !strings.Contains(got, "file.txt") {
		t.Errorf("find = %q", got)
	}
	if got := e.Exec("find . -name nothing"); got != "" {
		t.Errorf("find no match = %q", got)
	}
}
