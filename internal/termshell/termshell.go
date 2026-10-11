// Package termshell is SHEYTAN's built-in Linux-like shell (v1.0.6): a
// self-contained, dependency-free command environment that behaves like a
// busybox coreutils shell while being JAILED to the app folder — every path
// resolves inside the portable root and can never escape it.
//
// It powers two surfaces:
//   - the Terminal view (a real interactive console inside the app)
//   - the `linux` agent tool (a safe scratch environment the model can use
//     whenever a real shell is unnecessary or unwanted)
//
// Supported: pipes (grep/wc/head/tail/sort/uniq/rev as filters), quoting,
// ~ expansion, a busybox-style core (ls/cd/cat/mkdir/touch/rm/cp/mv/
// find/du/df/tree), introspection (ps/uname/whoami/uptime/env/history/date)
// and a SHEYTAN-flavored neofetch.
package termshell

import (
	"fmt"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/humanize"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcEntry is one row of the `ps` feed. The caller injects live data (the
// app process, the llama.cpp engine, agents) — the shell never fabricates
// processes.
type ProcEntry struct {
	PID  int
	Name string
	CPU  string // e.g. "12%"
	RAM  string // e.g. "1.2 GB"
	Kind string // "app" | "engine" | "agent"
}

// Engine is one shell session (its own cwd, history and environment).
//
// v1.1.4: Exec is serialized by a mutex. The engine instance is shared
// between the agent's `linux` tool and any future Terminal view; the agent
// executes tools from concurrent session runs, and cwd/history/env were
// unsynchronized shared state.
type Engine struct {
	mu      sync.Mutex
	root    string
	cwd     string
	history []string
	env     map[string]string
	started time.Time

	// ProcInfo optionally feeds `ps` with live process rows.
	ProcInfo func() []ProcEntry
	// DiskInfo optionally feeds `df` with the real disk numbers.
	DiskInfo func() (total, free uint64)
	// Hostname decorates the prompt ("user@host"); defaults to "sheytan".
	Hostname string
	// User is the shell identity; defaults to "user".
	User string

	maxHistory int
}

// New creates an engine jailed to root. cwd starts at root ("~").
func New(root string) *Engine {
	return &Engine{
		root:       filepath.Clean(root),
		cwd:        filepath.Clean(root),
		env:        map[string]string{"SHELL": "/bin/sheytan-sh", "HOME": "~", "PATH": "/usr/local/bin:/usr/bin:/bin"},
		started:    time.Now(),
		Hostname:   "sheytan",
		User:       "user",
		maxHistory: 500,
	}
}

// Root returns the jail directory.
func (e *Engine) Root() string { return e.root }

// CWD returns the display form of the working directory ("~", "~/workspace").
func (e *Engine) CWD() string { return e.display(e.cwd) }

// History returns the command history (oldest first, capped).
func (e *Engine) History() []string { return append([]string{}, e.history...) }

// ClearHistory wipes the command history.
func (e *Engine) ClearHistory() { e.history = nil }

// Prompt renders the shell prompt, e.g. "user@sheytan:~$ ".
func (e *Engine) Prompt() string {
	return fmt.Sprintf("%s@%s:%s$ ", e.User, e.Hostname, e.CWD())
}

// ClearMarker is what Exec returns for the `clear` command — the Terminal
// view erases the screen when it sees it.
const ClearMarker = "\x00SHEYTAN:CLEAR\x00"

// Commands lists every built-in command (for help + autocomplete).
func Commands() []string {
	return []string{
		"basename", "cat", "cd", "clear", "cp", "date", "df", "dirname", "du",
		"echo", "env", "export", "find", "grep", "head", "help", "history",
		"ls", "mkdir", "mv", "neofetch", "ps", "pwd", "rev", "rm", "sort",
		"stat", "tail", "touch", "tree", "uname", "uniq", "uptime", "wc",
		"whoami",
	}
}

// Exec runs one command line and returns its output (stdout+stderr merged,
// like a terminal). Empty lines return "".
func (e *Engine) Exec(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.pushHistory(line)

	stages := splitPipeline(line)
	var stdin string
	var out string
	for _, st := range stages {
		tokens := e.tokenize(st)
		if len(tokens) == 0 {
			continue
		}
		out = e.runCommandStage(tokens, stdin)
		stdin = out
	}
	return out
}

func (e *Engine) pushHistory(line string) {
	e.history = append(e.history, line)
	if len(e.history) > e.maxHistory {
		e.history = e.history[len(e.history)-e.maxHistory:]
	}
}

// --- tokenizer ---------------------------------------------------------------

// tokenize splits one pipeline stage into words honoring single/double
// quotes. Bare words may contain env refs ($VAR) expanded inline.
func (e *Engine) tokenize(s string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	// env expansion ($VAR / ${VAR}) outside quotes is approximated for all
	// tokens — good enough for a simulator.
	for i, t := range tokens {
		tokens[i] = e.expandEnv(t)
	}
	return tokens
}

func (e *Engine) expandEnv(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	out := s
	for k, v := range e.env {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
		out = strings.ReplaceAll(out, "$"+k, v)
	}
	return out
}

// splitPipeline splits on '|' that are not inside quotes. Quote characters
// are PRESERVED here — the tokenizer is the component that consumes them.
func splitPipeline(s string) []string {
	var parts []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
		case c == '|' && !inSingle && !inDouble:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

// --- path jail ----------------------------------------------------------------

// resolve maps a user-typed path to an absolute OS path inside the jail.
// Returns an error string when the target escapes the root.
func (e *Engine) resolve(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "~" {
		return e.root, nil
	}
	var abs string
	switch {
	case strings.HasPrefix(p, "~/"):
		abs = filepath.Join(e.root, strings.TrimPrefix(p, "~/"))
	case filepath.IsAbs(p):
		abs = filepath.Clean(strings.TrimPrefix(filepath.Clean(p), "/"))
		abs = filepath.Join(e.root, abs)
	default:
		abs = filepath.Join(e.cwd, p)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(e.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("permission denied: %s is outside the SHEYTAN sandbox", p)
	}
	return abs, nil
}

// display converts an absolute path to the ~-relative display form.
func (e *Engine) display(abs string) string {
	rel, err := filepath.Rel(e.root, abs)
	if err != nil {
		return abs
	}
	if rel == "." {
		return "~"
	}
	return "~/" + filepath.ToSlash(rel)
}

// --- command implementations ----------------------------------------------------

type cmdFunc func(e *Engine, args []string, stdin string) string

var registry = map[string]cmdFunc{
	"help":     cmdHelp,
	"ls":       cmdLS,
	"cd":       cmdCD,
	"pwd":      cmdPWD,
	"cat":      cmdCat,
	"echo":     cmdEcho,
	"mkdir":    cmdMkdir,
	"touch":    cmdTouch,
	"rm":       cmdRm,
	"cp":       cmdCp,
	"mv":       cmdMv,
	"head":     cmdHead,
	"tail":     cmdTail,
	"wc":       cmdWc,
	"grep":     cmdGrep,
	"sort":     cmdSort,
	"uniq":     cmdUniq,
	"rev":      cmdRev,
	"find":     cmdFind,
	"du":       cmdDu,
	"df":       cmdDf,
	"tree":     cmdTree,
	"ps":       cmdPs,
	"uname":    cmdUname,
	"whoami":   cmdWhoami,
	"uptime":   cmdUptime,
	"env":      cmdEnv,
	"export":   cmdExport,
	"history":  cmdHistory,
	"date":     cmdDate,
	"clear":    cmdClear,
	"neofetch": cmdNeofetch,
	"basename": cmdBasename,
	"dirname":  cmdDirname,
	"stat":     cmdStat,
}

// runCommand executes one stage; the stdin pipe feeds filter commands.
func (e *Engine) runCommandStage(tokens []string, stdin string) string {
	name := tokens[0]
	args := tokens[1:]
	if fn, ok := registry[name]; ok {
		return fn(e, args, stdin)
	}
	return fmt.Sprintf("sh: %s: command not found (try 'help')", name)
}

func cmdHelp(e *Engine, args []string, stdin string) string {
	cmds := Commands()
	return "SHEYTAN Linux simulator — every command runs INSIDE the app folder.\n" +
		"Built-ins (" + strconv.Itoa(len(cmds)) + "): " + strings.Join(cmds, ", ") + "\n" +
		"Filters (pipeable): cat FILE | grep -i WORD | wc -l, head -n N, tail -n N, sort, uniq, rev\n" +
		"Path jail: / and ~ map to the app folder — nothing outside it is reachable."
}

func cmdLS(e *Engine, args []string, stdin string) string {
	long, all := false, false
	var targets []string
	for _, a := range args {
		switch {
		case a == "-l":
			long = true
		case a == "-a" || a == "-la" || a == "-al":
			all = true
			if a != "-a" {
				long = true
			}
		case strings.HasPrefix(a, "-"):
			// tolerate other flags
		default:
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	var b strings.Builder
	for _, t := range targets {
		abs, err := e.resolve(t)
		if err != nil {
			fmt.Fprintf(&b, "ls: %s\n", err)
			continue
		}
		fi, err := os.Stat(abs)
		if err != nil {
			fmt.Fprintf(&b, "ls: cannot access '%s': no such file or directory\n", t)
			continue
		}
		if !fi.IsDir() {
			if long {
				fmt.Fprintf(&b, "%s  %10d  %s  %s\n", "-rw-r--r--", fi.Size(), fi.ModTime().Format("Jan 02 15:04"), filepath.Base(t))
			} else {
				fmt.Fprintln(&b, filepath.Base(t))
			}
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			fmt.Fprintf(&b, "ls: %v\n", err)
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, en := range entries {
			if !all && strings.HasPrefix(en.Name(), ".") {
				continue
			}
			if long {
				info, err := en.Info()
				size := int64(0)
				mod := time.Time{}
				if err == nil {
					size, mod = info.Size(), info.ModTime()
				}
				perm := "-rw-r--r--"
				if en.IsDir() {
					perm = "drwxr-xr-x"
				}
				fmt.Fprintf(&b, "%s  %10d  %s  %s\n", perm, size, mod.Format("Jan 02 15:04"), en.Name())
			} else {
				suffix := ""
				if en.IsDir() {
					suffix = "/"
				}
				fmt.Fprintln(&b, en.Name()+suffix)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdCD(e *Engine, args []string, stdin string) string {
	target := "~"
	if len(args) > 0 {
		target = args[0]
	}
	abs, err := e.resolve(target)
	if err != nil {
		return "cd: " + err.Error()
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("cd: %s: no such file or directory", target)
	}
	if !fi.IsDir() {
		return fmt.Sprintf("cd: %s: not a directory", target)
	}
	e.cwd = abs
	return ""
}

func cmdPWD(e *Engine, args []string, stdin string) string {
	return e.display(e.cwd)
}

func cmdCat(e *Engine, args []string, stdin string) string {
	if len(args) == 0 {
		return stdin // cat as a pure pipe pass-through
	}
	var b strings.Builder
	for _, a := range args {
		abs, err := e.resolve(a)
		if err != nil {
			fmt.Fprintf(&b, "cat: %s\n", err)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			fmt.Fprintf(&b, "cat: %s: no such file or directory\n", a)
			continue
		}
		b.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	// Cap pathological output (cat'ing a model file) — the chunking mindset.
	return capOutput(b.String())
}

func cmdEcho(e *Engine, args []string, stdin string) string {
	return strings.Join(args, " ")
}

func cmdMkdir(e *Engine, args []string, stdin string) string {
	recursive := false
	var dirs []string
	for _, a := range args {
		if a == "-p" {
			recursive = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		dirs = append(dirs, a)
	}
	if len(dirs) == 0 {
		return "mkdir: missing operand"
	}
	var b strings.Builder
	for _, d := range dirs {
		abs, err := e.resolve(d)
		if err != nil {
			fmt.Fprintf(&b, "mkdir: %s\n", err)
			continue
		}
		var err2 error
		if recursive {
			err2 = os.MkdirAll(abs, 0o755)
		} else {
			err2 = os.Mkdir(abs, 0o755)
		}
		if err2 != nil {
			fmt.Fprintf(&b, "mkdir: cannot create '%s': %v\n", d, err2)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdTouch(e *Engine, args []string, stdin string) string {
	if len(args) == 0 {
		return "touch: missing file operand"
	}
	var b strings.Builder
	for _, a := range args {
		abs, err := e.resolve(a)
		if err != nil {
			fmt.Fprintf(&b, "touch: %s\n", err)
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			now := time.Now()
			_ = os.Chtimes(abs, now, now)
			continue
		}
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(&b, "touch: cannot touch '%s': %v\n", a, err)
			continue
		}
		f.Close()
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdRm(e *Engine, args []string, stdin string) string {
	recursive := false
	var targets []string
	for _, a := range args {
		switch {
		case a == "-r" || a == "-rf" || a == "-fr":
			recursive = true
		case strings.HasPrefix(a, "-"):
		default:
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		return "rm: missing operand"
	}
	var b strings.Builder
	for _, t := range targets {
		abs, err := e.resolve(t)
		if err != nil {
			fmt.Fprintf(&b, "rm: %s\n", err)
			continue
		}
		if abs == e.root {
			return "rm: refusing to remove the sandbox root"
		}
		fi, err := os.Stat(abs)
		if err != nil {
			fmt.Fprintf(&b, "rm: cannot remove '%s': no such file or directory\n", t)
			continue
		}
		if fi.IsDir() && !recursive {
			fmt.Fprintf(&b, "rm: cannot remove '%s': is a directory (use -r)\n", t)
			continue
		}
		var rmErr error
		if fi.IsDir() {
			rmErr = os.RemoveAll(abs)
		} else {
			rmErr = os.Remove(abs)
		}
		if rmErr != nil {
			fmt.Fprintf(&b, "rm: cannot remove '%s': %v\n", t, rmErr)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdCp(e *Engine, args []string, stdin string) string {
	if len(args) < 2 {
		return "cp: missing destination operand"
	}
	src, err1 := e.resolve(args[0])
	dst, err2 := e.resolve(args[len(args)-1])
	if err1 != nil {
		return "cp: " + err1.Error()
	}
	if err2 != nil {
		return "cp: " + err2.Error()
	}
	if fi, err := os.Stat(dst); err == nil && fi.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Sprintf("cp: cannot stat '%s': no such file", args[0])
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Sprintf("cp: %v", err)
	}
	return ""
}

func cmdMv(e *Engine, args []string, stdin string) string {
	if len(args) < 2 {
		return "mv: missing destination operand"
	}
	src, err1 := e.resolve(args[0])
	dst, err2 := e.resolve(args[len(args)-1])
	if err1 != nil {
		return "mv: " + err1.Error()
	}
	if err2 != nil {
		return "mv: " + err2.Error()
	}
	if fi, err := os.Stat(dst); err == nil && fi.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := os.Rename(src, dst); err != nil {
		// cross-device fallback: copy + delete
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			return fmt.Sprintf("mv: %v", err)
		}
		if werr := os.WriteFile(dst, data, 0o644); werr != nil {
			return fmt.Sprintf("mv: %v", err)
		}
		_ = os.Remove(src)
	}
	return ""
}

func cmdHead(e *Engine, args []string, stdin string) string {
	n := 10
	text := stdin
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" && i+1 < len(args) {
			if v, err := strconv.Atoi(args[i+1]); err == nil {
				n = v
			}
			i++
		} else if !strings.HasPrefix(args[i], "-") {
			if out, err := e.readWhole(args[i]); err == nil {
				text = out
			} else {
				return "head: " + err.Error()
			}
		}
	}
	lines := readLines(text)
	if n > len(lines) {
		n = len(lines)
	}
	if n < 0 {
		n = 0
	}
	return strings.Join(lines[:n], "\n")
}

func cmdTail(e *Engine, args []string, stdin string) string {
	n := 10
	text := stdin
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" && i+1 < len(args) {
			if v, err := strconv.Atoi(args[i+1]); err == nil {
				n = v
			}
			i++
		} else if !strings.HasPrefix(args[i], "-") {
			if out, err := e.readWhole(args[i]); err == nil {
				text = out
			} else {
				return "tail: " + err.Error()
			}
		}
	}
	lines := readLines(text)
	if n > len(lines) {
		n = len(lines)
	}
	if n < 0 {
		n = 0
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// readWhole is the shared file slurp for cat/head/tail/wc-style commands:
// jail-checked, capped output.
func (e *Engine) readWhole(name string) (string, error) {
	abs, err := e.resolve(name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("%s: no such file or directory", name)
	}
	return capOutput(string(data)), nil
}

func cmdWc(e *Engine, args []string, stdin string) string {
	text := stdin
	var srcName string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		srcName = args[0]
		abs, err := e.resolve(srcName)
		if err != nil {
			return "wc: " + err.Error()
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("wc: %s: no such file", srcName)
		}
		text = string(data)
	}
	lines := len(readLines(text))
	words := len(strings.Fields(text))
	chars := len(text)
	name := ""
	if srcName != "" {
		name = " " + srcName
	}
	return fmt.Sprintf("%8d %8d %8d%s", lines, words, chars, name)
}

func cmdGrep(e *Engine, args []string, stdin string) string {
	ignoreCase := false
	var rest []string
	for _, a := range args {
		if a == "-i" || a == "--ignore-case" {
			ignoreCase = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 {
		return "usage: grep [-i] PATTERN [FILE...]"
	}
	pattern := rest[0]
	files := rest[1:]
	text := stdin
	if len(files) > 0 {
		var b strings.Builder
		for _, f := range files {
			abs, err := e.resolve(f)
			if err != nil {
				fmt.Fprintf(&b, "grep: %s\n", err)
				continue
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				fmt.Fprintf(&b, "grep: %s: no such file\n", f)
				continue
			}
			fmt.Fprintf(&b, "%s", data)
		}
		text = b.String()
	}
	hay := text
	needle := pattern
	if ignoreCase {
		hay, needle = strings.ToLower(hay), strings.ToLower(pattern)
	}
	var b strings.Builder
	for _, line := range readLines(text) {
		cmp := line
		if ignoreCase {
			cmp = strings.ToLower(line)
		}
		if strings.Contains(cmp, needle) {
			fmt.Fprintln(&b, line)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdSort(e *Engine, args []string, stdin string) string {
	lines := readLines(stdin)
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func cmdUniq(e *Engine, args []string, stdin string) string {
	lines := readLines(stdin)
	var out []string
	for i, l := range lines {
		if i == 0 || l != lines[i-1] {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func cmdRev(e *Engine, args []string, stdin string) string {
	lines := readLines(stdin)
	for i, l := range lines {
		r := []rune(l)
		for a, b := 0, len(r)-1; a < b; a, b = a+1, b-1 {
			r[a], r[b] = r[b], r[a]
		}
		lines[i] = string(r)
	}
	return strings.Join(lines, "\n")
}

func cmdFind(e *Engine, args []string, stdin string) string {
	start := "."
	var nameNeedle string
	for i := 0; i < len(args); i++ {
		if args[i] == "-name" && i+1 < len(args) {
			nameNeedle = strings.TrimSuffix(strings.TrimPrefix(args[i+1], "*"), "*")
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			start = args[i]
		}
	}
	abs, err := e.resolve(start)
	if err != nil {
		return "find: " + err.Error()
	}
	var b strings.Builder
	filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if nameNeedle != "" && !strings.Contains(strings.ToLower(d.Name()), strings.ToLower(nameNeedle)) {
			return nil
		}
		fmt.Fprintln(&b, e.display(p))
		return nil
	})
	return strings.TrimRight(b.String(), "\n")
}

func dirSizeOf(path string) int64 {
	var total int64
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func cmdDu(e *Engine, args []string, stdin string) string {
	human := true
	var targets []string
	for _, a := range args {
		if a == "-k" || a == "-b" {
			human = false
		} else if !strings.HasPrefix(a, "-") {
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	var b strings.Builder
	for _, t := range targets {
		abs, err := e.resolve(t)
		if err != nil {
			fmt.Fprintf(&b, "du: %s\n", err)
			continue
		}
		total := dirSizeOf(abs)
		if human {
			fmt.Fprintf(&b, "%s\t%s\n", humanize.Bytes(total), e.display(abs))
		} else {
			fmt.Fprintf(&b, "%d\t%s\n", total, e.display(abs))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdDf(e *Engine, args []string, stdin string) string {
	if e.DiskInfo == nil {
		return "Filesystem      Size  Used Avail Use% Mounted on\nsheytan-fs       ?      ?    ?    ?%   / (live disk info unavailable)"
	}
	total, free := e.DiskInfo()
	used := total - free
	pct := 0
	if total > 0 {
		pct = int(used * 100 / total)
	}
	return fmt.Sprintf("Filesystem      Size  Used Avail Use%% Mounted on\nsheytan-fs  %8s %6s %5s  %2d%%  /",
		humanize.Bytes(int64(total)), humanize.Bytes(int64(used)), humanize.Bytes(int64(free)), pct)
}

func cmdTree(e *Engine, args []string, stdin string) string {
	abs, err := e.resolve(".")
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if a, err2 := e.resolve(args[0]); err2 == nil {
			abs = a
		}
	}
	if err != nil {
		return "tree: " + err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", e.display(abs))
	var walk func(dir, prefix string, depth int)
	walk = func(dir, prefix string, depth int) {
		if depth > 4 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for i, en := range entries {
			connector := "├── "
			if i == len(entries)-1 {
				connector = "└── "
			}
			fmt.Fprintf(&b, "%s%s%s\n", prefix, connector, en.Name())
			if en.IsDir() {
				extend := "│   "
				if i == len(entries)-1 {
					extend = "    "
				}
				walk(filepath.Join(dir, en.Name()), prefix+extend, depth+1)
			}
		}
	}
	walk(abs, "", 0)
	return strings.TrimRight(b.String(), "\n")
}

func cmdPs(e *Engine, args []string, stdin string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  PID  KIND    %%CPU   RSS      COMMAND\n")
	procs := []ProcEntry{
		{PID: 1, Name: "sheytan-init", CPU: "0.0", RAM: "2.1 MB", Kind: "app"},
		{PID: 2, Name: "sheytan-agent", CPU: "0.0", RAM: "18 MB", Kind: "agent"},
	}
	if e.ProcInfo != nil {
		procs = append(procs, e.ProcInfo()...)
	}
	for _, p := range procs {
		fmt.Fprintf(&b, "%5d  %-6s  %5s  %-8s %s\n", p.PID, p.Kind, p.CPU, p.RAM, p.Name)
	}
	return b.String()
}

func cmdUname(e *Engine, args []string, stdin string) string {
	if len(args) > 0 && (args[0] == "-a" || args[0] == "--all") {
		return "SHEYTAN sheytan-linux 1.0.6 #1-SMP SHEYTAN Local Agent Runtime x86_64 GNU/Linux (simulated)"
	}
	return "sheytan-linux"
}

func cmdWhoami(e *Engine, args []string, stdin string) string { return e.User }

func cmdUptime(e *Engine, args []string, stdin string) string {
	d := time.Since(e.started).Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("up %02d:%02d:%02d, 1 user, load average: ember-warm", h, m, s)
}

func cmdEnv(e *Engine, args []string, stdin string) string {
	keys := make([]string, 0, len(e.env))
	for k := range e.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, e.env[k])
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdExport(e *Engine, args []string, stdin string) string {
	for _, a := range args {
		if k, v, ok := strings.Cut(a, "="); ok && k != "" {
			e.env[k] = v
		}
	}
	return ""
}

func cmdHistory(e *Engine, args []string, stdin string) string {
	var b strings.Builder
	for i, h := range e.history {
		fmt.Fprintf(&b, "%5d  %s\n", i+1, h)
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdDate(e *Engine, args []string, stdin string) string {
	return time.Now().Format("Mon Jan 02 15:04:05 UTC 2006")
}

func cmdClear(e *Engine, args []string, stdin string) string { return ClearMarker }

func cmdNeofetch(e *Engine, args []string, stdin string) string {
	art := []string{
		`        (    `,
		`       ) )   `,
		`      ( (    `,
		`    _____    `,
		`   /  _  \   `,
		`  |  (_)  |  `,
		`   \  ^  /   `,
		`    |||||    `,
		`    |||||    `,
	}
	info := []string{
		e.User + "@" + e.Hostname,
		"-----------------",
		"OS: SHEYTAN Linux (simulated)",
		"Host: SHEYTAN-Local-Agent v1.0.6",
		"Kernel: sheytan-linux 1.0.6",
		"Shell: sheytan-sh",
		"Uptime: " + time.Since(e.started).Round(time.Second).String(),
		"Workspace: " + humanize.Bytes(dirSizeOf(e.root)),
	}
	var b strings.Builder
	for i := 0; i < len(art) || i < len(info); i++ {
		l := ""
		if i < len(art) {
			l = art[i]
		}
		r := ""
		if i < len(info) {
			r = info[i]
		}
		fmt.Fprintf(&b, "%-16s %s\n", l, r)
	}
	return strings.TrimRight(b.String(), "\n")
}

func cmdBasename(e *Engine, args []string, stdin string) string {
	if len(args) == 0 {
		return "basename: missing operand"
	}
	return filepath.Base(strings.ReplaceAll(args[0], "\\", "/"))
}

func cmdDirname(e *Engine, args []string, stdin string) string {
	if len(args) == 0 {
		return "dirname: missing operand"
	}
	return filepath.ToSlash(filepath.Dir(strings.ReplaceAll(args[0], "\\", "/")))
}

func cmdStat(e *Engine, args []string, stdin string) string {
	if len(args) == 0 {
		return "stat: missing operand"
	}
	abs, err := e.resolve(args[0])
	if err != nil {
		return "stat: " + err.Error()
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("stat: cannot stat '%s'", args[0])
	}
	kind := "regular file"
	if fi.IsDir() {
		kind = "directory"
	}
	return fmt.Sprintf("  File: %s\n  Size: %-12d %s\nModify: %s",
		e.display(abs), fi.Size(), kind, fi.ModTime().Format("2006-01-02 15:04:05"))
}

// --- helpers ---------------------------------------------------------------------

func readLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}

const maxOutput = 64 * 1024

// capOutput protects the UI and the model from pathological dumps (cat'ing a
// .gguf): keep the head and the tail with an explicit elision marker.
func capOutput(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	head := s[:maxOutput/2]
	tail := s[len(s)-maxOutput/2:]
	return head + "\n… [output truncated by the SHEYTAN data-chunking layer] …\n" + tail
}
