// Package tools contains the built-in agent tools: shell, files, code-exec,
// git, browser. All tools implement the agent.Tool interface. Web search
// lives in internal/research — the ONE search backend (v1.3.6 spec §26).
package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/proc"
)

// OnFileCreated is the optional v1.0.4 artifact hook: the GUI installs a
// callback at boot so every file a tool writes can be reported to the
// artifacts tracker and surfaced in the UI ("created files" chips + Files
// view). Always nil in CLI mode — callers must nil-check.
var OnFileCreated func(path string)

// --- Shell ---

type Shell struct{}

func (Shell) Name() string { return "shell" }
func (Shell) Description() string {
	return "Run a shell command and return stdout+stderr. On Windows commands run via cmd.exe; on macOS/Linux via bash. Default working directory is the app folder (use cwd to change). Relative paths match the files/dataAnalysis tools."
}
func (Shell) Parameters() any {
	return struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd,omitempty"`
		Timeout int    `json:"timeout,omitempty"`
	}{}
}
func (Shell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Timeout int    `json:"timeout"`
	}

	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}

	p.Command = strings.TrimSpace(p.Command)

	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := 60

	if p.Timeout > 0 {
		timeout = p.Timeout
	}

	cctx, cancel := context.WithTimeout(
		ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	var workingDir string

	if strings.TrimSpace(p.Cwd) != "" {
		resolved, err := ResolvePathChecked(p.Cwd)
		if err != nil {
			return "", fmt.Errorf("invalid cwd: %w", err)
		}

		workingDir = resolved
	} else {
		workingDir = BaseDir()

		if workingDir == "" {
			return "", ErrBaseDirUnset
		}
	}

	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = proc.CommandContext(
			cctx,
			"cmd",
			"/d",
			"/c",
			p.Command,
		)
	} else {
		cmd = proc.CommandContext(
			cctx,
			"bash",
			"-c",
			p.Command,
		)
	}

	cmd.Dir = workingDir

	// v1.2.4: bounded streaming capture replaces CombinedOutput — a runaway
	// command can no longer buffer gigabytes in RAM before any downstream
	// limit applies.
	out, _, err := boundedCombinedOutput(cmd, toolOutputCap)

	if err != nil &&
		cctx.Err() == context.DeadlineExceeded {
		return out + "\n[timeout]", nil
	}

	return out, err
}

// --- Files (v2 — v1.0.9 TURBINE) ---
//
// The files tool grew from read/write/list/delete into a complete studio:
// create, write (append), read (chunked by line windows), combine (merge
// many files into one through a 1 MB chunked copy — never a whole-file
// buffer), copy/move/mkdir, tree, content search (regex, chunked scan,
// hit-capped), literal/regex replace (atomic temp-file + rename, dry-run
// by default) and info. Everything is path-guarded and reports created
// files to the artifact tracker like every other writing tool.

type Files struct{}

func (Files) Name() string { return "files" }
func (Files) Description() string {
	return `Create, read, write, combine and manage files. Relative paths resolve against the app folder, so a file written here can be read by shell/git/dataAnalysis with the same relative path. Chain: write CSV → dataAnalysis to profile it.
Actions (flat JSON, one per call):
  read      {"path":"notes.txt","offsetLine":100,"maxLines":50}  — read a file; offsetLine/maxLines chunk big files (default: whole file, 2 MB cap per call)
  write     {"path":"notes.txt","content":"..."}                 — create or overwrite (parent folders are created)
  append    {"path":"notes.txt","content":"more"}                — add to the end of a file (creates it when missing)
  combine   {"sources":["a.csv","b.csv"],"path":"all.csv"}       — merge many files into one, in order, chunk-streamed (1 MB buffer); optional "separator" string between files
  list      {"path":"."}                                         — list a folder (name + size)
  tree      {"path":".","depth":2}                               — recursive listing (depth ≤ 4, 400 entries cap)
  delete    {"path":"notes.txt"}                                 — delete a file or empty folder
  copy      {"path":"a.txt","dest":"copy.txt"}                   — copy (chunk-streamed)
  move      {"path":"a.txt","dest":"b.txt"}                      — move/rename
  mkdir     {"path":"reports/2026"}                              — create a folder chain
  search    {"path":"logs","pattern":"ERROR \w+","maxHits":50}   — regex content search in a file or folder tree (line numbers + hits, capped)
  replace   {"path":"a.md","pattern":"old","replacement":"new","regex":false,"dryRun":true} — count (dryRun) or apply replacements (atomic write)
  info      {"path":"a.csv"}                                     — size, modified time, line count (chunked), text/binary guess`
}
func (Files) Parameters() any {
	return struct {
		Action      string   `json:"action"`
		Path        string   `json:"path"`
		Content     string   `json:"content,omitempty"`
		Dest        string   `json:"dest,omitempty"`
		Sources     []string `json:"sources,omitempty"`
		Separator   string   `json:"separator,omitempty"`
		Pattern     string   `json:"pattern,omitempty"`
		Replacement string   `json:"replacement,omitempty"`
		Regex       bool     `json:"regex,omitempty"`
		DryRun      *bool    `json:"dryRun,omitempty"`
		OffsetLine  int      `json:"offsetLine,omitempty"`
		MaxLines    int      `json:"maxLines,omitempty"`
		MaxHits     int      `json:"maxHits,omitempty"`
		Depth       int      `json:"depth,omitempty"`
	}{}
}

// chunkCopy streams src → dst through a fixed 1 MB buffer (constant memory
// regardless of file size — the v1.0.9 "manage data in chunks" contract).
//
// v1.11.1 path-injection remediation (work order §7 — class (a)): both
// endpoints re-resolve through the fail-closed chokepoint, so the copy can
// never open or create anything outside BaseDir() even if a future caller
// forgets to resolve first. A refused malicious operation has touched
// nothing — no source open, no destination MkdirAll, no create.
func chunkCopy(src, dst string) (int64, error) {
	src, err := toolPath(src)
	if err != nil {
		return 0, err
	}
	dst, err = toolPath(dst)
	if err != nil {
		return 0, err
	}
	in, err := toolOpen(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := toolMkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := toolCreate(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	buf := make([]byte, 1<<20)
	n, err := io.CopyBuffer(out, in, buf)
	if err != nil {
		return n, err
	}
	return n, out.Sync()
}

// countLinesChunked counts lines through a streaming reader (never loads
// the whole file).
//
// v1.11.1 path-injection remediation (work order §7): the path re-resolves
// through the fail-closed chokepoint before the open.
func countLinesChunked(path string) (int64, error) {
	path, err := toolPath(path)
	if err != nil {
		return 0, err
	}
	f, err := toolOpen(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var lines int64
	sawAny := false
	for {
		chunk, rerr := r.ReadSlice('\n')
		if len(chunk) > 0 {
			sawAny = true
			lines += int64(bytes.Count(chunk, []byte{'\n'}))
		}
		if rerr == bufio.ErrBufferFull {
			continue
		}
		if rerr != nil {
			break
		}
	}
	if sawAny && lines == 0 {
		lines = 1
	}
	return lines, nil
}

func (f Files) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Action      string   `json:"action"`
		Path        string   `json:"path"`
		Content     string   `json:"content"`
		Dest        string   `json:"dest"`
		Sources     []string `json:"sources"`
		Separator   string   `json:"separator"`
		Pattern     string   `json:"pattern"`
		Replacement string   `json:"replacement"`
		Regex       bool     `json:"regex"`
		DryRun      *bool    `json:"dryRun"`
		OffsetLine  int      `json:"offsetLine"`
		MaxLines    int      `json:"maxLines"`
		MaxHits     int      `json:"maxHits"`
		Depth       int      `json:"depth"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	switch strings.ToLower(p.Action) {
	case "read":
		// v1.11.1 path-injection remediation (work order §7): every
		// files-tool path argument resolves through the ONE fail-closed
		// chokepoint — a hostile path errors here and never reaches a
		// filesystem sink.
		path, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return filesRead(path, p.OffsetLine, p.MaxLines)
	case "write":
		dst, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		if err := toolMkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := toolWriteFile(dst, []byte(p.Content), 0o644); err != nil {
			return "", err
		}
		if OnFileCreated != nil {
			OnFileCreated(dst)
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), dst), nil
	case "append":
		dst, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		if err := toolMkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		fh, err := toolOpenFileFlags(dst, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		if _, werr := fh.WriteString(p.Content); werr != nil {
			// v1.11.1 (CodeQL go/unhandled-writable-file-close #251): the
			// write-failure path propagates the write AND the close error.
			return "", errors.Join(werr, fh.Close())
		}
		if err := fh.Close(); err != nil {
			return "", err
		}
		if OnFileCreated != nil {
			OnFileCreated(dst)
		}
		return fmt.Sprintf("appended %d bytes to %s", len(p.Content), dst), nil
	case "combine":
		dst, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return filesCombine(p.Sources, dst, p.Separator)
	case "copy":
		if p.Path == "" || p.Dest == "" {
			return "", fmt.Errorf("copy needs 'path' (source) and 'dest'")
		}
		src, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		dst, err := toolPath(p.Dest)
		if err != nil {
			return "", err
		}
		n, err := chunkCopy(src, dst)
		if err != nil {
			return "", err
		}
		if OnFileCreated != nil {
			OnFileCreated(dst)
		}
		return fmt.Sprintf("copied %s (%d bytes) → %s", p.Path, n, p.Dest), nil
	case "move", "rename":
		if p.Path == "" || p.Dest == "" {
			return "", fmt.Errorf("move needs 'path' (source) and 'dest'")
		}
		// v1.11.1 path-injection remediation (work order §7): BOTH
		// endpoints fail closed before any rename/copy/delete — a
		// refused malicious move leaves source and destination
		// untouched.
		src, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		dst, err := toolPath(p.Dest)
		if err != nil {
			return "", err
		}
		if err := toolMkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := toolRename(src, dst); err != nil {
			// Cross-device fallback: chunked copy + delete.
			if _, cerr := chunkCopy(src, dst); cerr != nil {
				return "", err
			}
			_ = toolRemove(src)
		}
		if OnFileCreated != nil {
			OnFileCreated(dst)
		}
		return fmt.Sprintf("moved %s → %s", p.Path, p.Dest), nil
	case "mkdir":
		dst, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return "created " + p.Path, toolMkdirAll(dst, 0o755)
	case "list":
		dir, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		entries, err := toolReadDir(dir)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, e := range entries {
			dir := ""
			if e.IsDir() {
				dir = "/"
			} else if fi, ferr := e.Info(); ferr == nil {
				dir = fmt.Sprintf("  (%s)", humanSize(fi.Size()))
			}
			fmt.Fprintf(&b, "%s%s\n", e.Name(), dir)
		}
		return b.String(), nil
	case "tree":
		root, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return filesTree(root, p.Depth)
	case "delete", "remove":
		dst, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted %s", dst), toolRemove(dst)
	case "search":
		root, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return filesSearch(root, p.Pattern, p.MaxHits)
	case "replace":
		path, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return filesReplace(path, p.Pattern, p.Replacement, p.Regex, p.DryRun)
	case "info":
		path, err := toolPath(p.Path)
		if err != nil {
			return "", err
		}
		return fileInfo(path)
	default:
		return "", fmt.Errorf("unknown action %q (read|write|append|combine|list|tree|copy|move|mkdir|delete|search|replace|info)", p.Action)
	}
}

const filesReadCap = 2 << 20 // 2 MB per read call

// filesRead reads a text file with an optional line window (chunked big
// files: offsetLine + maxLines bound the payload the model receives).
//
// v1.11.1 path-injection remediation (work order §7): the path re-resolves
// through the fail-closed chokepoint so the open is builder-derived.
func filesRead(path string, offsetLine, maxLines int) (string, error) {
	path, err := toolPath(path)
	if err != nil {
		return "", err
	}
	fh, err := toolOpen(path)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	r := bufio.NewReaderSize(fh, 256<<10)
	var b strings.Builder
	line := 0
	total := 0
	keptLines := 0
	capped := maxLines > 0 && maxLines <= 5000
	overflow := false
	for {
		chunk, rerr := r.ReadSlice('\n')
		if len(chunk) > 0 {
			line++
			keep := line > offsetLine && (!capped || keptLines < maxLines)
			if keep {
				if total+len(chunk) > filesReadCap {
					overflow = true
					break
				}
				total += len(chunk)
				keptLines++
				b.Write(chunk)
			}
		}
		if rerr == bufio.ErrBufferFull {
			continue
		}
		if rerr != nil {
			break // EOF (or a real read error — treat as end of file)
		}
	}
	out := b.String()
	if offsetLine > 0 && (keptLines > 0 || overflow) {
		out = fmt.Sprintf("[read %s from line %d]\n%s", filepath.Base(path), offsetLine+1, out)
	}
	if overflow {
		out += fmt.Sprintf("\n[… read cap reached after %d bytes — call again with offsetLine=%d]", filesReadCap, line)
	}
	return out, nil
}

// filesCombine merges sources into dst through the chunked copier —
// constant memory, ordered, with an optional separator between files.
//
// v1.11.1 path-injection remediation (work order §7 — class (a)): every
// source AND the destination resolve through the fail-closed chokepoint
// BEFORE the output file is created — a hostile source fails the whole
// combine and leaves dst untouched (the previous spelling created and
// truncated dst before discovering a bad source).
func filesCombine(sources []string, dst, separator string) (string, error) {
	if len(sources) == 0 {
		return "", fmt.Errorf("combine needs 'sources' (a list of file paths)")
	}
	if dst == "" {
		return "", fmt.Errorf("combine needs 'path' (the output file)")
	}
	dst, err := toolPath(dst)
	if err != nil {
		return "", err
	}
	srcs := make([]string, 0, len(sources))
	for _, s := range sources {
		resolved, err := toolPath(s)
		if err != nil {
			return "", fmt.Errorf("source %q: %w", s, err)
		}
		srcs = append(srcs, resolved)
	}
	if err := toolMkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	out, err := toolCreate(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	w := bufio.NewWriterSize(out, 256<<10)
	buf := make([]byte, 1<<20)
	var total int64
	var merged []string
	for i, src := range srcs {
		in, err := toolOpen(src)
		if err != nil {
			return "", fmt.Errorf("source %d (%s): %w", i+1, sources[i], err)
		}
		if i > 0 && separator != "" {
			if _, err := w.WriteString(separator); err != nil {
				in.Close()
				return "", err
			}
		}
		n, err := io.CopyBuffer(w, in, buf)
		in.Close()
		if err != nil {
			return "", fmt.Errorf("copying %s: %w", sources[i], err)
		}
		total += n
		merged = append(merged, filepath.Base(sources[i]))
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	if err := out.Sync(); err != nil {
		return "", err
	}
	if OnFileCreated != nil {
		OnFileCreated(dst)
	}
	return fmt.Sprintf("combined %d files (%s) → %s (%s)", len(sources), strings.Join(merged, ", "), dst, humanSize(total)), nil
}

// filesTree renders a bounded recursive listing (chunk-friendly caps keep
// huge folders from flooding the model's context).
//
// v1.11.1 path-injection remediation (work order §7 — class (a)): the root
// AND every descended directory re-resolve through the fail-closed
// chokepoint. A symlink planted inside the tree can no longer redirect a
// listing (or the recursion) outside the base — the previous spelling
// listed and descended through os.ReadDir, which follows directory
// symlinks anywhere.
func filesTree(root string, depth int) (string, error) {
	root, err := toolPath(root)
	if err != nil {
		return "", err
	}
	if depth <= 0 {
		depth = 2
	}
	if depth > 4 {
		depth = 4
	}
	const maxEntries = 400
	var b strings.Builder
	count := 0
	var walk func(dir string, level int) error
	walk = func(dir string, level int) error {
		checked, err := toolPath(dir)
		if err != nil {
			return err
		}
		entries, err := toolReadDir(checked)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if count >= maxEntries {
				fmt.Fprintf(&b, "%s… (entry cap reached)\n", strings.Repeat("  ", level))
				return errStopWalk
			}
			count++
			suffix := ""
			if e.IsDir() {
				suffix = "/"
			} else if fi, ferr := e.Info(); ferr == nil {
				suffix = fmt.Sprintf("  (%s)", humanSize(fi.Size()))
			}
			fmt.Fprintf(&b, "%s%s%s\n", strings.Repeat("  ", level), e.Name(), suffix)
			if e.IsDir() && level < depth-1 {
				if err := walk(filepath.Join(checked, e.Name()), level+1); err == errStopWalk {
					return err
				}
			}
		}
		return nil
	}
	b.WriteString(filepath.Base(root) + "/\n")
	if err := walk(root, 1); err != nil && err != errStopWalk {
		return "", err
	}
	return b.String(), nil
}

var errStopWalk = fmt.Errorf("stop walk")

// filesSearch regex-searches a file or a folder tree line by line (chunked
// scanner, hard hit cap so a broad pattern cannot flood the context).
//
// v1.11.1 path-injection remediation (work order §7 — class (a)): the root
// re-resolves through the fail-closed chokepoint, and every walked file is
// re-checked before its open — a file reachable only through a symlink
// escaping the base is skipped, exactly like the other unreadable-file
// cases, so its content can never be leaked into the result.
func filesSearch(root, pattern string, maxHits int) (string, error) {
	root, err := toolPath(root)
	if err != nil {
		return "", err
	}
	if pattern == "" {
		return "", fmt.Errorf("search needs 'pattern'")
	}
	if maxHits <= 0 {
		maxHits = 50
	}
	if maxHits > 200 {
		maxHits = 200
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("bad pattern: %w", err)
	}
	var hits []string
	var filesScanned, linesScanned int64
	truncated := false
	scanFile := func(path string) error {
		resolved, rerr := toolPath(path)
		if rerr != nil {
			return nil // escaping symlink (or hostile path): skip like an unreadable file
		}
		path = resolved
		fh, err := toolOpen(path)
		if err != nil {
			return nil // unreadable file: skip silently (permissions, lock)
		}
		defer fh.Close()
		if !sniffsTextPath(path) {
			return nil
		}
		filesScanned++
		r := bufio.NewReaderSize(fh, 256<<10)
		lineNo := 0
		for {
			line, err := r.ReadString('\n')
			lineNo++
			linesScanned++
			if re.MatchString(line) {
				if len(hits) < maxHits {
					hits = append(hits, fmt.Sprintf("%s:%d: %s", filepath.Base(path), lineNo, strings.TrimRight(clipStr(strings.TrimSpace(line), 160), "\n")))
				} else {
					truncated = true
				}
			}
			if err != nil {
				break
			}
			if truncated && len(hits) >= maxHits {
				return errStopWalk
			}
		}
		return nil
	}
	fi, err := toolStat(root)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		if err := scanFile(root); err != nil && err != errStopWalk {
			return "", err
		}
	} else {
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if err := scanFile(path); err == errStopWalk {
				return errStopWalk
			}
			return nil
		})
		if err != nil && err != errStopWalk {
			return "", err
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Search %q in %s — %d hit(s) over %d file(s), %s scanned\n\n",
		pattern, filepath.Base(root), len(hits), filesScanned, humanSize(linesScanned))
	for _, h := range hits {
		b.WriteString(h + "\n")
	}
	if truncated {
		b.WriteString(fmt.Sprintf("… hit cap (%d) reached — narrow the pattern or raise maxHits (≤ 200)\n", maxHits))
	}
	if len(hits) == 0 {
		b.WriteString("no matches\n")
	}
	return b.String(), nil
}

// filesReplace applies literal or regex replacements. Dry-run (default)
// only counts matches; the real write goes through a temp file + atomic
// rename so a crash mid-write can never destroy the original.
//
// v1.11.1 path-injection remediation (work order §7 — class (a)): the
// target re-resolves through the fail-closed chokepoint BEFORE the read —
// the read, the temp file and the rename then all operate on one
// builder-derived path. The temp file itself is a CreateTemp INSIDE the
// target's (already checked) directory.
func filesReplace(path, pattern, replacement string, useRegex bool, dryRun *bool) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("replace needs 'pattern'")
	}
	path, err := toolPath(path)
	if err != nil {
		return "", err
	}
	if dryRun == nil {
		t := true
		dryRun = &t
	}
	data, err := toolReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	var count int
	var out string
	if useRegex {
		re, cerr := regexp.Compile(pattern)
		if cerr != nil {
			return "", fmt.Errorf("bad regex: %w", cerr)
		}
		count = len(re.FindAllStringIndex(text, -1))
		if !*dryRun {
			out = re.ReplaceAllString(text, replacement)
		}
	} else {
		count = strings.Count(text, pattern)
		if !*dryRun {
			out = strings.ReplaceAll(text, pattern, replacement)
		}
	}
	if *dryRun {
		return fmt.Sprintf("replace (dry run): %d match(es) of %q in %s — call again with \"dryRun\":false to apply", count, pattern, filepath.Base(path)), nil
	}
	dir := filepath.Dir(path)
	tmp, err := toolCreateTempIn(dir, ".sheytan-replace-")
	if err != nil {
		return "", err
	}
	// The root-opened File reports its ROOT-RELATIVE name; the absolute
	// spelling joins it with the resolved target directory.
	tmpPath := filepath.Join(dir, filepath.Base(tmp.Name()))
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		_ = toolRemove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = toolRemove(tmpPath)
		return "", err
	}
	if err := toolRename(tmpPath, path); err != nil {
		_ = toolRemove(tmpPath)
		return "", err
	}
	if OnFileCreated != nil {
		OnFileCreated(path)
	}
	return fmt.Sprintf("replaced %d match(es) of %q in %s (%d → %d bytes)", count, pattern, filepath.Base(path), len(text), len(out)), nil
}

// fileInfo reports size, mtime, chunked line count and a text/binary guess.
//
// v1.11.1 path-injection remediation (work order §7): the path re-resolves
// through the fail-closed chokepoint before the stat/read pair.
func fileInfo(path string) (string, error) {
	path, err := toolPath(path)
	if err != nil {
		return "", err
	}
	fi, err := toolStat(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "path     %s\n", path)
	fmt.Fprintf(&b, "size     %s (%d bytes)\n", humanSize(fi.Size()), fi.Size())
	fmt.Fprintf(&b, "modified %s\n", fi.ModTime().Format("2006-01-02 15:04:05"))
	if fi.IsDir() {
		entries, _ := toolReadDir(path)
		fmt.Fprintf(&b, "type     directory (%d entries)\n", len(entries))
		return b.String(), nil
	}
	lines, lerr := countLinesChunked(path)
	if lerr == nil {
		fmt.Fprintf(&b, "lines    %d\n", lines)
	}
	kind := "binary"
	if sniffsTextPath(path) {
		kind = "text"
	}
	fmt.Fprintf(&b, "type     %s (%s)\n", kind, strings.TrimPrefix(filepath.Ext(path), "."))
	return b.String(), nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// sniffsTextPath is files tool's text sniffer (8 KB head, no NUL runs).
//
// v1.11.1 path-injection remediation (work order §7): the path re-resolves
// through the fail-closed chokepoint; a refused path sniffs as
// "not text" (the same answer an unreadable file gets).
func sniffsTextPath(path string) bool {
	resolved, err := toolPath(path)
	if err != nil {
		return false
	}
	path = resolved
	fh, err := toolOpen(path)
	if err != nil {
		return false
	}
	defer fh.Close()
	buf := make([]byte, 8192)
	n, _ := fh.Read(buf)
	if n == 0 {
		return true
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return false
	}
	return utf8.Valid(buf)
}

// --- Code Exec ---

type CodeExec struct{}

func (CodeExec) Name() string { return "codeExec" }
func (CodeExec) Description() string {
	return "Run Python code and return stdout/stderr. Code is written to a temp file and executed with python3."
}
func (CodeExec) Parameters() any {
	return struct {
		Lang    string `json:"lang"`
		Code    string `json:"code"`
		Timeout int    `json:"timeout,omitempty"`
	}{}
}
func (CodeExec) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Lang    string `json:"lang"`
		Code    string `json:"code"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if p.Code == "" {
		return "", fmt.Errorf("code is required")
	}
	timeout := 60
	if p.Timeout > 0 {
		timeout = p.Timeout
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	lang := strings.ToLower(p.Lang)
	if lang == "" {
		lang = "python"
	}
	switch lang {
	case "python", "py":
		py, err := pythonBin()
		if err != nil {
			return "", err
		}
		tmp, err := os.CreateTemp("", "sheytan-*.py")
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString(p.Code); err != nil {
			return "", err
		}
		_ = tmp.Close()
		cmd = proc.CommandContext(cctx, py, tmp.Name())
	case "node", "javascript", "js":
		tmp, err := os.CreateTemp("", "sheytan-*.js")
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString(p.Code); err != nil {
			return "", err
		}
		_ = tmp.Close()
		cmd = proc.CommandContext(cctx, "node", tmp.Name())
	default:
		return "", fmt.Errorf("unsupported language: %s", lang)
	}
	// v1.2.4: bounded streaming capture (see boundedCombinedOutput).
	out, _, err := boundedCombinedOutput(cmd, toolOutputCap)
	return out, err
}

func clipStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- Git ---

type Git struct{}

func (Git) Name() string { return "git" }
func (Git) Description() string {
	return "Run a git subcommand in a repository. Pass 'repo' (relative paths resolve against the app folder) and 'args' (space-separated)."
}
func (Git) Parameters() any {
	return struct {
		Repo string `json:"repo"`
		Args string `json:"args"`
	}{}
}
func (Git) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Repo string `json:"repo"`
		Args string `json:"args"`
	}

	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}

	p.Repo = strings.TrimSpace(p.Repo)
	p.Args = strings.TrimSpace(p.Args)

	if p.Args == "" {
		return "", fmt.Errorf("args is required")
	}

	argList := strings.Fields(p.Args)

	if err := validateGitArgs(argList); err != nil {
		return "", err
	}

	workingDir := BaseDir()

	if workingDir == "" {
		return "", ErrBaseDirUnset
	}

	if p.Repo != "" {
		resolved, err := ResolvePathChecked(p.Repo)
		if err != nil {
			return "", fmt.Errorf("invalid repo: %w", err)
		}

		workingDir = resolved
	}

	cctx, cancel := context.WithTimeout(
		ctx,
		30*time.Second,
	)
	defer cancel()

	cmd := proc.CommandContext(
		cctx,
		"git",
		argList...,
	)

	cmd.Dir = workingDir

	// v1.2.4: bounded streaming capture (see boundedCombinedOutput).
	out, _, err := boundedCombinedOutput(cmd, toolOutputCap)

	if err != nil &&
		cctx.Err() == context.DeadlineExceeded {
		return out + "\n[timeout]", nil
	}

	return out, err
}

func validateGitArgs(args []string) error {
	for _, arg := range args {
		token := strings.TrimSpace(arg)

		if token == "" {
			continue
		}

		lower := strings.ToLower(token)

		// Git's -C option changes the working directory and therefore can
		// bypass the repo jail.
		if lower == "-c" {
			return fmt.Errorf(
				"git: -C is blocked because it can escape the repository jail",
			)
		}

		if lower == "--git-dir" ||
			lower == "--work-tree" ||
			lower == "--separate-git-dir" {
			return fmt.Errorf(
				"git: path-changing option %q is blocked",
				token,
			)
		}

		for _, prefix := range []string{
			"-C=",
			"--git-dir=",
			"--work-tree=",
			"--separate-git-dir=",
			"gitdir:",
		} {
			if strings.HasPrefix(lower, prefix) {
				return fmt.Errorf(
					"git: path-changing option %q is blocked",
					token,
				)
			}
		}

		normalized := normalizePathSeparators(token)

		if filepath.IsAbs(normalized) {
			return fmt.Errorf(
				"git: absolute paths are blocked: %q",
				token,
			)
		}

		cleaned := filepath.Clean(normalized)

		if cleaned == ".." ||
			strings.HasPrefix(
				cleaned,
				".."+string(filepath.Separator),
			) {
			return fmt.Errorf(
				"git: parent-directory traversal is blocked: %q",
				token,
			)
		}

		// Git's file:// transport can access arbitrary local paths despite
		// the repository working directory being jailed.
		if strings.HasPrefix(
			lower,
			"file://",
		) {
			return fmt.Errorf(
				"git: file:// URLs are blocked",
			)
		}
	}

	return nil
}

// pythonBin resolves the Python interpreter across platforms
// (python3 on Unix, python/py on Windows).
func pythonBin() (string, error) {
	if runtime.GOOS == "windows" {
		for _, cand := range []string{"python", "py"} {
			if _, err := exec.LookPath(cand); err == nil {
				return cand, nil
			}
		}
		return "", fmt.Errorf("python not found (install from python.org or the Microsoft Store)")
	}
	if _, err := exec.LookPath("python3"); err == nil {
		return "python3", nil
	}
	if _, err := exec.LookPath("python"); err == nil {
		return "python", nil
	}
	return "", fmt.Errorf("python3 not found")
}
