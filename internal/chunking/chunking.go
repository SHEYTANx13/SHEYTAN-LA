// Package chunking is SHEYTAN's data-chunking engine (v1.0.2). Every place
// where data crosses a size boundary — files attached to chat, conversation
// history heading into the context window, recall blocks — flows through
// here so the app stays smooth and the LLM prompt stays inside budget.
//
// The three core budgets:
//
//   - Attachments: .txt/.md (and every other text/code format) are inlined
//     into the user message with a head+tail window so a 5 MB log still
//     delivers its beginning and end within a byte budget.
//   - History: the orchestrator keeps as many recent messages as fit in a
//     configurable share of num_ctx, compacting older turns into a single
//     elision marker (their facts remain retrievable via the recall engine).
//   - Recall: past-exchange digests are injected as a bounded block.
//
// Everything is pure string math — no allocations beyond the output, no
// goroutines, safe to call from any thread.
package chunking

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/humanize"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// DefaultAttachmentBudgetBytes is the per-message attachment budget when the
// config has no explicit value (256 KB of file content inline — roughly
// llama.cpp's sweet spot for one exchange while leaving room for history).
const DefaultAttachmentBudgetBytes = 256 * 1024

// textExts are extensions that are ALWAYS treated as text (the 100%-supported
// set starts with .txt/.md; the rest are the common text/code formats every
// OpenAI-compatible endpoint ingests happily as plain content).
var textExts = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".mdx": true,
	".csv": true, ".tsv": true, ".json": true, ".jsonl": true, ".ndjson": true,
	".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".cfg": true,
	".conf": true, ".properties": true, ".env": true,
	".xml": true, ".html": true, ".htm": true, ".css": true, ".scss": true,
	".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".jsx": true, ".vue": true, ".svelte": true,
	".go": true, ".py": true, ".pyi": true, ".rs": true, ".java": true,
	".kt": true, ".kts": true, ".scala": true, ".swift": true, ".dart": true,
	".c": true, ".h": true, ".cpp": true, ".cc": true, ".hpp": true,
	".cs": true, ".rb": true, ".php": true, ".pl": true, ".lua": true,
	".r": true, ".m": true, ".sql": true, ".sh": true, ".bash": true,
	".zsh": true, ".fish": true, ".ps1": true, ".psm1": true,
	".bat": true, ".cmd": true, ".mk": true, ".makefile": true,
	".log": true, ".diff": true, ".patch": true, ".srt": true, ".vtt": true,
	".gitignore": true, ".gitattributes": true, ".dockerfile": true,
	".editorconfig": true, ".lock": true, ".sum": true, ".mod": true,
}

// EstimateMessagesTokens sums the token estimates of every message (content
// + tool-call arguments).
func EstimateMessagesTokens(msgs []llm.Message) int {
	total := 0
	for i := range msgs {
		total += EstimateTokens(msgs[i].Content)
		for _, tc := range msgs[i].ToolCalls {
			total += EstimateTokens(tc.Function.Arguments) + 4
		}
	}
	return total
}

// IsTextFile reports whether the file at `path` looks like human-readable
// text: a known text extension OR content that sniffs as UTF-8-ish with no
// NUL runs. The sniff reads at most 8 KB.
func IsTextFile(path string) bool {
	// Known text extension — still sniff: a .txt that is really a renamed
	// .exe must not be inlined. Extension-less files (Makefile, .gitignore)
	// and unknown extensions get the pure content sniff.
	return sniffsText(path)
}

// sniffsText reads the first 8 KB and decides text vs binary: no NUL bytes
// and either valid UTF-8 or a low replacement-rune ratio.
func sniffsText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	if n == 0 {
		return true // empty file — trivially text
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return false // NUL bytes → binary
	}
	if utf8.Valid(buf) {
		return true
	}
	// Not valid UTF-8: tolerate legacy 8-bit encodings (Latin-1 logs) when
	// fewer than 10% of runes would be replacement chars.
	bad := 0
	total := 0
	for _, r := range string(buf) {
		total++
		if r == utf8.RuneError {
			bad++
		}
	}
	return total > 0 && bad*10 < total
}

// IsKnownTextExt reports whether the extension is in the always-text set
// (used by the GUI to badge attachments as "inlined").
func IsKnownTextExt(path string) bool {
	return textExts[strings.ToLower(filepath.Ext(path))]
}

// SplitParagraphs splits text into chunks of at most maxBytes bytes each,
// breaking on blank lines first, then on single newlines, then hard-splitting
// any run longer than maxBytes. Returns nil for empty input. Chunks keep
// their trailing newline so re-joining is lossless.
//
// v1.1.5 Phase 3: the split runs as one interval pass shared with
// ChunkText (no rescanning, chunk strings share the source backing array,
// hard splits are UTF-8-rune-aligned). With no multi-byte runes the cut
// points are byte-identical to the pre-Phase-3 primitive.
func SplitParagraphs(text string, maxBytes int) []string {
	if text == "" {
		return nil
	}

	intervals := chunkIntervals(text, ChunkerConfig{MaxBytes: maxBytes})
	if len(intervals) == 0 {
		return nil
	}

	out := make([]string, 0, len(intervals))
	for _, iv := range intervals {
		out = append(out, text[iv[0]:iv[1]])
	}

	return out
}

// boundary finds the best split point within the first n bytes of s:
// the last blank line ("\n\n"), else the last newline, else -1.
func boundary(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	if i := strings.LastIndex(s[:n], "\n\n"); i > 0 {
		return i + 2
	}
	if i := strings.LastIndexByte(s[:n], '\n'); i > 0 {
		return i + 1
	}
	return -1
}

// WindowHeadTail elides the MIDDLE of text when it exceeds budget bytes,
// keeping the head (75% of the budget) and the tail (25%) with an explicit
// elision marker line. Text within budget returns unchanged. Line-oriented:
// cuts never split a line in half unless a single line exceeds the budget.
func WindowHeadTail(text string, budgetBytes int) string {
	if budgetBytes < 256 {
		budgetBytes = 256
	}
	if len(text) <= budgetBytes {
		return text
	}
	headBudget := budgetBytes * 3 / 4
	tailBudget := budgetBytes / 4

	head := text
	// head: cut at a line boundary within headBudget
	if len(head) > headBudget {
		cut := lastLineBoundary(head, headBudget)
		head = head[:cut]
	}
	tail := text
	// tail: take the last tailBudget bytes, aligned up to a line start
	start := len(tail) - tailBudget
	if start < 0 {
		start = 0
	}
	if start > 0 {
		if idx := strings.IndexByte(tail[start:], '\n'); idx >= 0 {
			start += idx + 1
		}
		tail = tail[start:]
	}
	elided := len(text) - len(head) - len(tail)
	elidedLines := strings.Count(text, "\n") - strings.Count(head, "\n") - strings.Count(tail, "\n")
	return head +
		fmt.Sprintf("\n… [elided %s (%d lines) — middle of the file omitted to fit the context budget] …\n\n", humanize.Bytes(int64(elided)), elidedLines) +
		tail
}

// lastLineBoundary returns the largest cut point <= limit that does not
// split a line (limit itself when no newline exists before it).
func lastLineBoundary(s string, limit int) int {
	if limit >= len(s) {
		return len(s)
	}
	if i := strings.LastIndexByte(s[:limit], '\n'); i > 0 {
		return i + 1
	}
	return limit
}

// FormatFileAttachment renders one attachment as a prompt-ready block.
//
// Text files become a fenced block with the file name as a header, windowed
// to the budget. Binary/unreadable files become a metadata note (name, size,
// type) telling the model the exact path so it can pull what it needs via
// the files tool — the model decides how much of a binary it really needs.
func FormatFileAttachment(path string, budgetBytes int) string {
	if budgetBytes <= 0 {
		budgetBytes = DefaultAttachmentBudgetBytes
	}
	name := filepath.Base(path)
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("[attachment: %s — could not be read: %v]\n", path, err)
	}
	if fi.Size() > 64<<20 {
		return fmt.Sprintf("[attachment: %s — %s, too large to attach; read it with the files tool at %s]\n",
			name, humanize.Bytes(fi.Size()), path)
	}
	if !IsTextFile(path) {
		ext := strings.ToLower(filepath.Ext(name))
		return fmt.Sprintf("[attachment: %s — %s binary%s — not inlined as text; if its content matters, read it with the files tool at %s]\n",
			name, humanize.Bytes(fi.Size()), extSuffix(ext), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("[attachment: %s — read failed: %v]\n", name, err)
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		ext = "text"
	}
	// v1.2.4: window the raw bytes directly instead of converting the whole
	// file to string first. The old path held ~3× the file size live at once
	// (read buffer + full string copy + windowed concat); this path copies
	// only the window that actually enters the prompt.
	windowed := WindowHeadTailBytes(data, budgetBytes)
	return fmt.Sprintf("----- attached file: %s (%s) -----\n```%s\n%s\n```\n", name, humanize.Bytes(int64(len(data))), ext, strings.TrimRight(windowed, "\n"))
}

// WindowHeadTailBytes is the []byte counterpart of WindowHeadTail: identical
// semantics (75/25 head/tail split at line boundaries, explicit elision
// marker) but operating on the raw byte slice so callers that already hold
// the file as bytes avoid one full-size string copy. The returned string is
// always a fresh allocation bounded by ~budget + marker, never a view of
// data.
func WindowHeadTailBytes(data []byte, budgetBytes int) string {
	if budgetBytes < 256 {
		budgetBytes = 256
	}
	if len(data) <= budgetBytes {
		return string(data)
	}
	headBudget := budgetBytes * 3 / 4
	tailBudget := budgetBytes / 4

	headEnd := len(data)
	if headEnd > headBudget {
		headEnd = lastLineBoundaryBytes(data, headBudget)
	}
	tailStart := len(data) - tailBudget
	if tailStart < 0 {
		tailStart = 0
	}
	if tailStart > 0 {
		if idx := bytes.IndexByte(data[tailStart:], '\n'); idx >= 0 {
			tailStart += idx + 1
		}
	}

	head := data[:headEnd]
	tail := data[tailStart:]
	elided := len(data) - len(head) - len(tail)
	elidedLines := bytes.Count(data, []byte("\n")) - bytes.Count(head, []byte("\n")) - bytes.Count(tail, []byte("\n"))

	var b strings.Builder
	b.Grow(len(head) + len(tail) + 128)
	b.Write(head)
	fmt.Fprintf(&b, "\n… [elided %s (%d lines) — middle of the file omitted to fit the context budget] …\n\n", humanize.Bytes(int64(elided)), elidedLines)
	b.Write(tail)
	return b.String()
}

// lastLineBoundaryBytes is lastLineBoundary over a byte slice.
func lastLineBoundaryBytes(data []byte, limit int) int {
	if limit >= len(data) {
		return len(data)
	}
	if i := bytes.LastIndexByte(data[:limit], '\n'); i > 0 {
		return i + 1
	}
	return limit
}

func extSuffix(ext string) string {
	if ext == "" {
		return ""
	}
	return " (" + ext + ")"
}

// ComposeUserMessage builds the final user-message content from the typed
// text and a list of attachment paths. Attachments share one byte budget:
// each gets budget/len(attachments) so no single file starves the rest.
// Text attachments are inlined; binaries become metadata notes.
func ComposeUserMessage(text string, attachments []string, budgetBytes int) string {
	if len(attachments) == 0 {
		return text
	}
	if budgetBytes <= 0 {
		budgetBytes = DefaultAttachmentBudgetBytes
	}
	var b strings.Builder
	if text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("### Attached files\n\n")
	per := budgetBytes / len(attachments)
	for _, p := range attachments {
		b.WriteString(FormatFileAttachment(p, per))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// SplitAttachments separates image attachments (v1.0.6 vision) from the
// text/binary ones. Images ride the message's Images field as real
// image_url parts on the wire; everything else goes through the text
// pipeline (ComposeUserMessage). The image list is capped at
// vision.MaxImagesPerMessage — the surplus falls back to the text pipeline
// (as a metadata note).
func SplitAttachments(attachments []string) (images, others []string) {
	for _, p := range attachments {
		if vision.IsImageFile(p) && len(images) < vision.MaxImagesPerMessage {
			images = append(images, p)
		} else {
			others = append(others, p)
		}
	}
	return images, others
}

// ComposeWithImages is the ONE v1.0.6 composition used by every surface
// (GUI composer, CLI ask, API server): it splits the attachments, composes
// the text pipeline for non-images, and appends an image note so text-only
// models still know pictures were shared. Returns the composed content and
// the image paths to ride the message.
func ComposeWithImages(text string, attachments []string, budgetBytes int) (composed string, images []string) {
	images, others := SplitAttachments(attachments)
	composed = ComposeUserMessage(text, others, budgetBytes)
	if len(images) > 0 {
		var names []string
		for _, p := range images {
			names = append(names, filepath.Base(p))
		}
		note := "[image attached: " + strings.Join(names, ", ") + "]"
		if strings.TrimSpace(composed) == "" {
			composed = note
		} else {
			composed += "\n" + note
		}
	}
	return composed, images
}

// WindowMessages compacts conversation history into a token budget (the
// "history window"). It always keeps:
//
//   - every message from the last user message onward (the current turn)
//   - as many earlier messages as fit, walking backwards (recent pairs win)
//
// When messages are dropped a marker is prepended noting the elision so the
// model knows older turns exist but are compacted (their key facts remain
// retrievable through memory recall). Returns the windowed slice and the
// number of elided messages.
//
// v1.0.9 (TURBINE): this was O(n²) — every kept message was re-prepended
// with `append([]llm.Message{history[i]}, kept...)`, copying the whole kept
// slice each time. A 400-message session paid ~80k struct copies per turn
// (every iteration of every agent turn). It is now one backward token pass
// plus exactly ONE slice copy at the end.
func WindowMessages(history []llm.Message, budgetTokens int) ([]llm.Message, int) {
	// v1.2.8.1: no artificial 256-token floor. When the caller's real
	// remainder is smaller than the floor, raising it here overfilled
	// the window and turned recoverable pressure into a hard refusal.
	// A budget below the current turn's cost keeps the current turn
	// verbatim and elides everything before it (the branch below).
	if budgetTokens < 0 {
		budgetTokens = 0
	}
	if len(history) == 0 {
		return history, 0
	}

	// Locate the last user message — everything after it (tool results,
	// assistant replies of the current turn) is untouchable.
	lastUser := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			lastUser = i
			break
		}
	}
	tail := history
	if lastUser > 0 {
		tail = history[lastUser:]
	}

	tailTokens := EstimateMessagesTokens(tail)
	if tailTokens >= budgetTokens {
		// The current turn alone blows the budget: keep it verbatim anyway
		// (dropping the user's fresh question is never acceptable) and
		// elide everything before it.
		elided := len(history) - len(tail)
		return prependMarker(tail, elided), elided
	}

	// Walk backwards from the last user message, accumulating message costs
	// until the budget is exhausted. keptFrom is the final cut index — the
	// whole result is materialized in one copy afterwards.
	remaining := budgetTokens - tailTokens
	keptFrom := lastUser // everything before this index is elided
	if lastUser < 0 {
		// No user message at all: the whole history is the "tail", nothing
		// may be prepended before it except the marker when it overflows
		// (handled above) — keep it intact.
		keptFrom = 0
	}
	for i := keptFrom - 1; i >= 0; i-- {
		cost := EstimateTokens(history[i].Content)
		for _, tc := range history[i].ToolCalls {
			cost += EstimateTokens(tc.Function.Arguments) + 4
		}
		if cost > remaining {
			break
		}
		remaining -= cost
		keptFrom = i
	}
	elided := keptFrom

	// Marker + kept window, materialized with exactly one allocation.
	total := len(history) - keptFrom
	out := make([]llm.Message, 0, total+1)
	if elided > 0 {
		out = append(out, llm.Message{
			Role:    "system",
			Content: fmt.Sprintf(elisionMarker, elided),
		})
	}
	out = append(out, history[keptFrom:]...)
	return out, elided
}

// elisionMarker is the system note inserted when history is compacted.
const elisionMarker = "[context window] %d older messages were compacted out of this conversation to keep performance at peak. Their essential facts remain available through memory recall (memory tool, action=recall) — retrieve them if the user references something from earlier."

func prependMarker(msgs []llm.Message, elided int) []llm.Message {
	marker := llm.Message{
		Role:    "system",
		Content: fmt.Sprintf(elisionMarker, elided),
	}
	return append([]llm.Message{marker}, msgs...)
}
