// handoff.go — v1.2.8 mandatory agent.md handoff (strengthened in
// v1.2.8.1).
//
// The repository already carries agent.md as the canonical engineering
// handoff document (lowercase — Windows filesystems may treat agent.md and
// Agent.md as the SAME path, so the exact filename `agent.md` is a
// constant here and must never be "normalized" into another case).
//
// v1.2.8.1 product contract: EVERY completed Agent-mode run updates the
// workspace's agent.md — including runs with NO engineering evidence (no
// files changed, no commands, no tests). Such runs write an honest
// "No engineering changes were made." handoff instead of being skipped.
// Chat mode never writes agent.md. The dynamic section:
//
//	<!-- sheytan:handoff:begin -->
//	# Latest Agent Handoff
//	...
//	<!-- sheytan:handoff:end -->
//
// Everything OUTSIDE the marker block is preserved byte-for-byte: stable
// engineering instructions the file already carries are never rewritten by
// the runtime. The new section replaces any previous one (the latest
// handoff is the authoritative one). The write is durable (tmp + fsync +
// rename + directory sync), verified by reading the file back, and only
// reported as completed after that verification succeeds — failures are
// returned to the caller, never swallowed. Content is factual, bounded, no
// private chain-of-thought, no speculative success claims, no fabricated
// metrics.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// HandoffFileName is the ONLY filename the handoff ever writes. Lowercase
// by contract (see package comment).
const HandoffFileName = "agent.md"

// handoffLocks (v1.2.9) serializes agent.md writes per ABSOLUTE path.
// <workspace>/agent.md is a SHARED resource: two concurrent Agent runs
// (or an Agent run racing the CLI) both performing the
// read-splice-rename sequence against the same file could interleave —
// the second writer's base was read before the first writer's rename,
// so the first handoff section is silently lost even though each
// individual write is atomic. The per-path mutex makes the whole
// read-modify-write critical section atomic with respect to other
// handoff writers IN THIS PROCESS; the file-level atomic rename (below)
// additionally protects against readers observing torn content and
// against cross-process interleaving at the rename boundary.
var (
	handoffLocksMu sync.Mutex
	handoffLocks   = map[string]*sync.Mutex{}
)

func handoffLock(path string) *sync.Mutex {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs
	}
	handoffLocksMu.Lock()
	defer handoffLocksMu.Unlock()
	mu, ok := handoffLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		handoffLocks[key] = mu
	}
	return mu
}

// Marker pair bounding the dynamic section inside agent.md.
const (
	HandoffBeginMarker = "<!-- sheytan:handoff:begin -->"
	HandoffEndMarker   = "<!-- sheytan:handoff:end -->"
)

// Handoff is the factual payload rendered into the dynamic section.
type Handoff struct {
	Task         string   // what was run
	Objective    string   // the goal
	CurrentState string   // where the work stands
	ChangesMade  []string // concrete changes
	FilesChanged []string // exact paths
	TestsAndVer  []string // tests run + verification verdict
	Evidence     []string // important evidence (outputs, ids, paths)
	Failures     []string // unresolved failures / blockers
	Remaining    []string // remaining work
	NextAction   string   // recommended next action
	DoNotRedo    []string // settled work the next agent must not repeat
}

// RenderHandoff renders the bounded markdown section between the markers.
// Machine-readable enough for another model to parse: stable `## Heading`
// blocks, `- key: value` bullets, exact values preserved verbatim.
func RenderHandoff(h Handoff) string {
	var b strings.Builder

	b.WriteString(HandoffBeginMarker + "\n")
	b.WriteString("# Latest Agent Handoff\n\n")

	writeSection := func(heading, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		fmt.Fprintf(&b, "## %s\n%s\n\n", heading, strings.TrimSpace(body))
	}
	writeList := func(heading string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n", heading)
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				continue
			}
			b.WriteString("- " + strings.TrimSpace(item) + "\n")
		}
		b.WriteString("\n")
	}

	writeSection("Task", h.Task)
	writeSection("Objective", h.Objective)
	writeSection("Current state", h.CurrentState)
	writeList("Changes made", h.ChangesMade)
	writeList("Files changed", h.FilesChanged)
	writeList("Tests and verification", h.TestsAndVer)
	writeList("Important evidence", h.Evidence)
	writeList("Failures / blockers", h.Failures)
	writeList("Remaining work", h.Remaining)
	writeSection("Recommended next action", h.NextAction)
	writeList("Do not redo", h.DoNotRedo)

	b.WriteString(HandoffEndMarker + "\n")

	return b.String()
}

// WriteHandoffFile updates path (an agent.md) with the rendered handoff:
//   - file absent      → created with a minimal stable header + the section;
//   - file without the section → section appended after the exact existing
//     bytes (only separator newlines are ADDED — existing bytes are never
//     rewritten);
//   - file with the section → ONLY the marker-bounded region is replaced;
//     everything outside the markers is preserved byte-for-byte.
//
// v1.2.8.1 durability contract: the file is read fresh (an UNREADABLE
// existing file aborts — the previous behavior treated any read error as
// "absent" and could clobber existing content), written to a unique temp
// file in the same directory, fsynced, renamed, and read back to verify
// the section actually landed. Only then does the write report success.
//
// v1.2.9 concurrency contract: the whole read-splice-rename sequence runs
// under the per-path handoff lock (see handoffLocks) — concurrent runs
// targeting the same workspace agent.md are SERIALIZED, so one run's
// handoff can never silently overwrite another's. The latest COMPLETED
// write wins; agent.md remains the deterministic latest-handoff
// projection of the serialized sequence.
func WriteHandoffFile(path string, h Handoff) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("handoff: empty path")
	}

	// v1.2.9: serialize the whole read-modify-write against other
	// handoff writers for this path (see handoffLocks).
	mu := handoffLock(path)
	mu.Lock()
	defer mu.Unlock()

	section := RenderHandoff(h)

	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		// v1.2.8.1: an existing-but-unreadable agent.md (locked,
		// permission denied) must NEVER be silently replaced —
		// abort so the caller surfaces the real failure.
		return fmt.Errorf("handoff: read %s: %w", path, err)
	}

	updated := spliceHandoff(existing, section)

	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".agent.md-*.tmp")
	if err != nil {
		return fmt.Errorf("handoff: temp file: %w", err)
	}

	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.WriteString(updated); err != nil {
		tmp.Close()
		return fmt.Errorf("handoff: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("handoff: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("handoff: close temp: %w", err)
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("handoff: chmod temp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("handoff: rename: %w", err)
	}

	// Directory sync best-effort: makes the rename itself durable where
	// the platform supports directory fsync (Linux); a refusal here
	// (Windows) does not invalidate the verified file content.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	// v1.2.8.1 read-back verification: the handoff is only "completed"
	// when the file on disk actually carries the section we wrote.
	verified, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("handoff: read-back verify: %w", err)
	}
	if !verifyHandoffContent(string(verified), section) {
		return fmt.Errorf("handoff: read-back verification failed — section not intact in %s", path)
	}

	return nil
}

// verifyHandoffContent checks that content carries exactly one complete
// marker pair and the exact section between the markers (the section's
// trailing newline after the end marker is not part of the comparison —
// it belongs to whatever follows the markers in the file).
func verifyHandoffContent(content, section string) bool {
	begin := strings.Index(content, HandoffBeginMarker)
	end := strings.Index(content, HandoffEndMarker)
	if begin < 0 || end <= begin {
		return false
	}
	if strings.Index(content[begin+1:], HandoffBeginMarker) >= 0 {
		return false // more than one begin marker
	}
	got := content[begin : end+len(HandoffEndMarker)]
	return got == strings.TrimSuffix(section, "\n")
}

// spliceHandoff replaces the marker-bounded section in existing (or
// appends it) while preserving everything outside the markers BYTE-FOR-
// BYTE (v1.2.8.1: no whitespace re-normalization at the boundaries — the
// previous TrimRight/TrimLeft silently rewrote the file's own spacing).
func spliceHandoff(existing, section string) string {
	begin := strings.Index(existing, HandoffBeginMarker)
	end := strings.Index(existing, HandoffEndMarker)

	if begin >= 0 && end > begin {
		before := existing[:begin]                    // exact bytes before the section
		after := existing[end+len(HandoffEndMarker):] // exact bytes after the section
		return before + section + after
	}

	// No COMPLETE section present. A dangling begin marker without an
	// end marker (a torn write) is STRIPPED — the fresh section
	// supersedes it, so exactly one marker pair ever remains. The
	// bytes before the torn marker are preserved exactly.
	if begin >= 0 {
		existing = existing[:begin]
	}

	if existing == "" {
		return handoffFileHeader() + "\n" + section
	}

	// Append: original bytes untouched; only ADD separator newlines when
	// the existing content does not already end on its own line.
	sep := ""
	if !strings.HasSuffix(existing, "\n\n") {
		if strings.HasSuffix(existing, "\n") {
			sep = "\n"
		} else {
			sep = "\n\n"
		}
	}
	return existing + sep + section
}

func handoffFileHeader() string {
	return "# Agent Handoff\n\n> Dynamic next-agent handoff maintained by SHEYTAN agent runs.\n" +
		"> The bounded `Latest Agent Handoff` section below is replaced after\n" +
		"> every completed engineering task; content outside it is preserved.\n"
}

// HandoffFromTaskState builds the factual payload from the run's task
// state + verification verdict + the persistence facts the API layer
// observed. Values are passed through verbatim — no re-derivation, no
// fabrication.
//
// v1.2.8.1: evidence-free runs get HONEST defaults instead of empty
// sections — "No engineering changes were made." — so every completed
// Agent run leaves a usable, truthful handoff for the next agent.
func HandoffFromTaskState(t TaskState, verificationSummary, outcome string) Handoff {
	h := Handoff{
		Task:         t.Goal,
		Objective:    t.Goal,
		CurrentState: t.CurrentStep,
		FilesChanged: t.FilesChanged,
		Failures:     t.Failures,
		Remaining:    t.OpenQuestions,
	}
	if t.NextStep != "" {
		h.NextAction = t.NextStep
	}
	if len(t.CommandsRun) > 0 {
		h.TestsAndVer = append(h.TestsAndVer, "commands run:")
		for _, c := range t.CommandsRun {
			h.TestsAndVer = append(h.TestsAndVer, "  - "+c)
		}
	}
	if len(t.TestsRun) > 0 {
		h.TestsAndVer = append(h.TestsAndVer, "tests run:")
		for _, c := range t.TestsRun {
			h.TestsAndVer = append(h.TestsAndVer, "  - "+c)
		}
	}
	if verificationSummary != "" {
		h.TestsAndVer = append(h.TestsAndVer, "verification: "+verificationSummary)
	}
	if outcome != "" {
		h.ChangesMade = append(h.ChangesMade, "run outcome: "+outcome)
	}
	if len(t.Repairs) > 0 {
		h.ChangesMade = append(h.ChangesMade, t.Repairs...)
	}

	noEvidence := len(t.FilesChanged) == 0 && len(t.CommandsRun) == 0 &&
		len(t.TestsRun) == 0 && len(t.Repairs) == 0 && len(t.Artifacts) == 0
	if noEvidence {
		h.ChangesMade = append(h.ChangesMade, "No engineering changes were made.")
		if h.CurrentState == "" {
			h.CurrentState = "The run completed without file, command or test activity."
		}
		if h.NextAction == "" {
			h.NextAction = "Review the session transcript for context before starting new work."
		}
		if len(h.Remaining) == 0 {
			h.Remaining = []string{"None recorded."}
		}
	}

	// "Do not redo": the settled artifacts (files already written, tests
	// already passing) — the next agent must not repeat completed work.
	h.DoNotRedo = append(h.DoNotRedo, t.FilesChanged...)
	h.DoNotRedo = append(h.DoNotRedo, t.Artifacts...)
	return h
}
