package lab

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Safe file editing — v1.1.5 Phase 6 (coding effectiveness).
//
// Small local models corrupt files through shell-based edits: broken quoting,
// half-written heredocs, glob surprises. The safe-edit primitives replace that
// with an anchored, verified, atomic operation:
//
//      read_file  — bounded, line-numbered inspection (no cat of a 40MB log)
//      edit_file — read → verify context → modify → verify result
//
// edit_file refuses to run unless the anchor (oldText) appears EXACTLY ONCE
// in the file:
//
//      0 matches  → STALE: the file changed since the model last read it (or the
//                   model invented the context) — the model must re-read.
//      2+ matches → AMBIGUOUS: the model must include more surrounding lines to
//                   make the anchor unique.
//
// After the write the file is re-read and the result verified (newText
// present exactly once, oldText absent) before success is reported. The
// write itself is atomic: a temp file in the same directory, fsync, rename.
// Any mutation invalidates the task's verification state — the same rule the
// command runner applies — so nothing can claim verified status across an
// edit it does not account for.

const (
	// maxReadFileBytes bounds one read_file response payload.
	maxReadFileBytes = 256 * 1024
	// maxReadFileLines bounds one read_file response payload.
	maxReadFileLines = 2000
	// maxEditAnchorBytes bounds the anchor/new text size.
	maxEditAnchorBytes = 256 * 1024
)

var (
	// ErrEditStale: the anchor does not occur in the file — the file changed
	// or the anchor was invented.
	ErrEditStale = errors.New(
		"lab: edit refused: anchor text not found — the file changed since it was last read; re-read it and retry with the current content",
	)
	// ErrEditAmbiguous: the anchor occurs more than once.
	ErrEditAmbiguous = errors.New(
		"lab: edit refused: anchor text occurs more than once — include more surrounding lines so it is unique",
	)
	// ErrEditNoop: old and new text are identical.
	ErrEditNoop = errors.New(
		"lab: edit refused: old and new text are identical",
	)
	// ErrEditEmptyAnchor: empty anchor would be ambiguous everywhere.
	ErrEditEmptyAnchor = errors.New(
		"lab: edit refused: empty anchor — provide the exact existing text to replace",
	)
	// ErrEditVerifyFailed: the post-write verification did not confirm the edit.
	ErrEditVerifyFailed = errors.New(
		"lab: edit verification failed after write",
	)
)

// FileReadResult is the structured read_file payload.
type FileReadResult struct {
	Path       string `json:"path"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	TotalLines int    `json:"totalLines"`
	Truncated  bool   `json:"truncated,omitempty"`
	Content    string `json:"content"`
}

// EditResult is the structured edit_file payload.
type EditResult struct {
	Path        string `json:"path"`
	Line        int    `json:"line"` // 1-based line where the edit starts
	ReplacedLen int    `json:"replacedBytes"`
	InsertedLen int    `json:"insertedBytes"`
	SizeBefore  int    `json:"sizeBytesBefore"`
	SizeAfter   int    `json:"sizeBytesAfter"`
}

// ReadFile returns a bounded, line-numbered slice of a workspace file.
// offset is 1-based (0/1 both mean "from the top"); limit ≤ 0 means the
// default bound. Binary files are detected (NUL byte sniff) and refused
// with a clear message instead of flooding the context with garbage.
func (t *Tool) ReadFile(task *Task, relPath string, offset, limit int) (FileReadResult, error) {
	if task == nil || task.Workspace == nil {
		return FileReadResult{}, ErrInvalidWorkspace
	}

	target, err := task.Workspace.PathFor(strings.TrimSpace(relPath))
	if err != nil {
		return FileReadResult{}, err
	}

	info, err := os.Stat(target)
	if err != nil {
		return FileReadResult{}, fmt.Errorf("lab: read_file: %w", err)
	}

	if info.IsDir() {
		return FileReadResult{}, fmt.Errorf(
			"lab: read_file: %s is a directory (list it with run ls)",
			relPath,
		)
	}

	if info.Size() > int64(maxReadFileBytes*4) {
		return FileReadResult{}, fmt.Errorf(
			"lab: read_file: %s is %d bytes — too large to read whole; read it in slices with offset/limit",
			relPath, info.Size(),
		)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return FileReadResult{}, fmt.Errorf("lab: read_file: %w", err)
	}

	// Binary sniff: a NUL in the first 8KB means this is not text the model
	// can meaningfully edit.
	if i := strings.IndexByte(string(data[:min(len(data), 8192)]), 0); i >= 0 {
		return FileReadResult{}, fmt.Errorf(
			"lab: read_file: %s looks binary (NUL byte at offset %d) — edit it with commands, not edit_file",
			relPath, i,
		)
	}

	lines := strings.Split(string(data), "\n")
	total := len(lines)

	if offset <= 0 {
		offset = 1
	}

	if offset > total {
		offset = total
	}

	if limit <= 0 || limit > maxReadFileLines {
		limit = maxReadFileLines
	}

	end := offset - 1 + limit
	truncated := end < total

	if end > total {
		end = total
	}

	var b strings.Builder

	for i := offset - 1; i < end; i++ {
		fmt.Fprintf(&b, "%6d| %s\n", i+1, lines[i])

		if b.Len() > maxReadFileBytes {
			// Hard byte bound: stop here, report what was rendered.
			end = i + 1
			truncated = true

			break
		}
	}

	return FileReadResult{
		Path:       relPath,
		StartLine:  offset,
		EndLine:    end,
		TotalLines: total,
		Truncated:  truncated,
		Content:    b.String(),
	}, nil
}

// EditFile applies one anchored, verified, atomic replacement inside the
// task workspace. See the file header for the contract.
func (t *Tool) EditFile(task *Task, relPath, oldText, newText string) (EditResult, error) {
	if task == nil || task.Workspace == nil {
		return EditResult{}, ErrInvalidWorkspace
	}

	if task.Status != TaskRunning {
		return EditResult{}, fmt.Errorf(
			"%w: current status=%s",
			ErrTaskNotRunnable,
			task.Status,
		)
	}

	oldText = strings.ReplaceAll(oldText, "\r\n", "\n")
	newText = strings.ReplaceAll(newText, "\r\n", "\n")

	if strings.TrimSpace(oldText) == "" {
		return EditResult{}, ErrEditEmptyAnchor
	}

	if oldText == newText {
		return EditResult{}, ErrEditNoop
	}

	if len(oldText) > maxEditAnchorBytes || len(newText) > maxEditAnchorBytes {
		return EditResult{}, fmt.Errorf(
			"lab: edit_file: old/new text exceeds %d bytes — split the edit into smaller pieces",
			maxEditAnchorBytes,
		)
	}

	target, err := task.Workspace.PathFor(strings.TrimSpace(relPath))
	if err != nil {
		return EditResult{}, err
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return EditResult{}, fmt.Errorf("lab: edit_file: %w", err)
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	originalContent := content

	// Locate the anchor: exactly once, or refuse.
	first := strings.Index(content, oldText)

	switch {
	case first < 0:
		return EditResult{}, ErrEditStale

	case strings.Contains(content[first+len(oldText):], oldText):
		return EditResult{}, ErrEditAmbiguous
	}

	// Any workspace mutation invalidates prior verification — same rule as
	// the command runner. The edit must be re-verified afterwards.
	t.tasks.invalidateVerification(task)

	edited := content[:first] + newText + content[first+len(oldText):]

	// Atomic write: temp file in the same directory, sync, rename.
	if err := writeFileAtomic(target, []byte(edited)); err != nil {
		return EditResult{}, fmt.Errorf("lab: edit_file: %w", err)
	}

	// Post-write verification: re-read from disk and confirm the exact
	// expected content landed. Byte equality with the computed edit is the
	// complete check — it implies the new text is present and the write did
	// not tear. Anything else is a hard error, never a success.
	check, err := os.ReadFile(target)
	if err != nil {
		return EditResult{}, fmt.Errorf("lab: edit_file verify read: %w", err)
	}

	if string(check) != edited {
		return EditResult{}, ErrEditVerifyFailed
	}

	line := 1 + strings.Count(content[:first], "\n")

	return EditResult{
		Path:        relPath,
		Line:        line,
		ReplacedLen: len(oldText),
		InsertedLen: len(newText),
		SizeBefore:  len(originalContent),
		SizeAfter:   len(string(check)),
	}, nil
}

// writeFileAtomic writes data to path via a same-directory temp file,
// fsync, and rename — a reader either sees the old or the new content,
// never a partial write.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".sheytan-edit-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}

	tmpName := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()

		return fmt.Errorf("write temp: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		cleanup()

		return fmt.Errorf("sync temp: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("rename into place: %w", err)
	}

	return nil
}
