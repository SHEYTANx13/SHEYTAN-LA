package aicontext

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// FileName is the instruction file's name inside the app folder.
const FileName = "AI-CONTEXT.md"

// ContextVersion is the instruction-file version. Bump it whenever the
// embedded AI-CONTEXT.md changes in a release; existing installs with an
// older marker get the fresh file on next boot.
const ContextVersion = 10

//go:embed AI-CONTEXT.md
var embedded string

// markerLine is the machine-readable version marker inside the file.
const markerLine = "<!-- sheytan-context-version:"

// Path returns the instruction file's location for a data dir (app folder).
func Path(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// Embedded returns the canonical embedded instruction text.
func Embedded() string {
	return embedded
}

// EnsureFile materializes AI-CONTEXT.md in the app folder:
//   - missing                          → write the embedded canonical file
//   - marker version < ContextVersion → overwrite with the new canonical
//   - marker version >= current       → keep (the user may have edited it)
//   - no marker (user-authored file)  → keep, untouched
//
// It returns the path of the effective file.
func EnsureFile(dataDir string) (string, error) {
	p := Path(dataDir)

	data, err := os.ReadFile(p)

	switch {
	case err == nil:
		v, hasMarker := fileVersion(string(data))

		if hasMarker && v >= ContextVersion {
			return p, nil
		}

		if !hasMarker {
			return p, nil
		}

	case os.IsNotExist(err):
		// Missing → write below.

	default:
		return p, err
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return p, err
	}

	if err := os.WriteFile(
		p,
		[]byte(embedded),
		0o644,
	); err != nil {
		return p, err
	}

	return p, nil
}

// fileVersion extracts the marker version from an AI-CONTEXT.md body.
func fileVersion(body string) (int, bool) {
	idx := strings.Index(
		body,
		markerLine,
	)

	if idx < 0 {
		return 0, false
	}

	rest := body[idx+len(markerLine):]

	end := strings.IndexAny(
		rest,
		">-\n",
	)

	if end < 0 {
		end = len(rest)
	}

	v := 0

	for _, ch := range strings.TrimSpace(rest[:end]) {
		if ch < '0' || ch > '9' {
			break
		}

		v = v*10 + int(ch-'0')
	}

	if v <= 0 {
		return 0, false
	}

	return v, true
}

// Load returns the effective instruction text: the file in the app folder
// when present (it may be user-customized), otherwise the embedded copy.
//
// v1.2.4: the file read is cached and validated by (size, mtime) — the
// briefing is composed on EVERY turn, so re-reading the file each time was
// measurable per-turn I/O + allocation for content that changes at most
// when the user edits it. A stat per turn keeps external edits visible
// immediately (same pattern as the memory store's external-writer check).
func Load(dataDir string) string {
	p := Path(dataDir)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
		fileCacheMu.Lock()
		hit := fileCache != nil && fileCache.dir == dataDir &&
			fileCache.size == fi.Size() && fileCache.mod.Equal(fi.ModTime())
		if hit {
			text := fileCache.text
			fileCacheMu.Unlock()
			return text
		}
		fileCacheMu.Unlock()

		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			text := string(data)
			fileCacheMu.Lock()
			fileCache = &fileCacheEntry{dir: dataDir, size: fi.Size(), mod: fi.ModTime(), text: text}
			fileCacheMu.Unlock()
			return text
		}
	}

	return embedded
}

// fileCacheEntry caches the resolved AI-CONTEXT.md text per data dir,
// validated by (size, mtime).
type fileCacheEntry struct {
	dir  string
	size int64
	mod  time.Time
	text string
}

var (
	fileCacheMu sync.Mutex
	fileCache   *fileCacheEntry
)

// ResetFileCache drops the cached instruction text (tests).
func ResetFileCache() {
	fileCacheMu.Lock()
	fileCache = nil
	fileCacheMu.Unlock()
}

// --- live environment briefing ---

var (
	probeOnce sync.Once
	probeInfo *sysinfo.SysInfo
)

// cachedProbe runs the hardware probe once per process — probing GPU
// drivers and disk stats is far too expensive to repeat per turn.
func cachedProbe() *sysinfo.SysInfo {
	probeOnce.Do(func() {
		probeInfo = sysinfo.Probe()
	})

	return probeInfo
}

// ResetProbeCache drops the cached hardware snapshot (tests).
func ResetProbeCache() {
	probeOnce = sync.Once{}
	probeInfo = nil
}

// defaultToolNames exists only as a compatibility fallback for callers that
// do not yet have the orchestrator registry available.
//
// The live agent path should call SystemMessageWithTools/SystemMessageWithTools
// with the actual registered tool names.
var defaultToolNames = []string{
	"files",
	"shell",
	"codeExec",
	"dataAnalysis",
	"webSearch",
	"browser",
	"git",
	"memory",
	"screenshot",
	"linux",
	"json",
	"archive",
	"fetch",
	"diff",
}

// Briefing builds the LIVE ENVIRONMENT block for the active config using the
// compatibility tool list.
func Briefing(cfg *config.Config) string {
	return BriefingWithTools(
		cfg,
		nil,
	)
}

// BriefingWithTools builds the LIVE ENVIRONMENT block using the actual
// registered tool names supplied by the runtime.
//
// Passing nil/empty tools intentionally falls back to the compatibility
// built-in list so older callers retain stable behavior.
func BriefingWithTools(
	cfg *config.Config,
	registeredTools []string,
) string {
	var b strings.Builder

	b.WriteString(
		"\n\n---\n\n## LIVE ENVIRONMENT\n\n",
	)

	fmt.Fprintf(
		&b,
		"- App: SHEYTAN-Local-Agent v%s (native desktop, local-first)\n",
		config.AppVersion,
	)

	fmt.Fprintf(
		&b,
		"- Date: %s\n",
		time.Now().Format("2006-01-02 (MST)"),
	)

	fmt.Fprintf(
		&b,
		"- OS: %s/%s",
		cap(runtime.GOOS),
		runtime.GOARCH,
	)

	if info := cachedProbe(); info != nil {
		if info.CPU.Name != "" {
			fmt.Fprintf(
				&b,
				" · CPU: %s (%d cores)",
				info.CPU.Name,
				info.CPU.LogicalCores,
			)
		}

		if info.RAM.TotalBytes > 0 {
			fmt.Fprintf(
				&b,
				" · RAM: %s",
				sysinfo.FormatBytes(info.RAM.TotalBytes),
			)
		}

		if len(info.GPU) > 0 &&
			info.GPU[0].Name != "" {
			fmt.Fprintf(
				&b,
				" · GPU: %s",
				info.GPU[0].Name,
			)
		}
	}

	b.WriteString("\n")

	if cfg != nil {
		fmt.Fprintf(
			&b,
			"- Working directory (app folder): %s\n",
			cfg.DataDir,
		)

		if cfg.IsRemote() {
			fmt.Fprintf(
				&b,
				"- Provider: remote OpenAI-compatible endpoint · model: %s\n",
				cfg.EffectiveModel(),
			)
		} else {
			fmt.Fprintf(
				&b,
				"- Provider: local llama.cpp (bundled) · model: %s\n",
				cfg.EffectiveModel(),
			)
		}
	}

	// Effective tool set: registered tools (or the compatibility fallback),
	// further restricted by the user's EnabledTools selection.
	tools := normalizeToolNames(registeredTools)

	if cfg != nil {
		tools = cfg.EnabledToolList(tools)
	}

	hasTool := func(name string) bool {
		for _, tool := range tools {
			if tool == name {
				return true
			}
		}

		return false
	}

	networkTools := func() string {
		names := make([]string, 0, 2)

		if hasTool("webSearch") {
			names = append(names, "webSearch")
		}

		if hasTool("browser") {
			names = append(names, "browser")
		}

		return strings.Join(names, " and ")
	}

	switch {
	case netcheck.IsOffline():
		if names := networkTools(); names != "" {
			b.WriteString(
				"- Connectivity: OFFLINE — " + names +
					" are DISABLED; all local tools work\n",
			)
		} else {
			b.WriteString(
				"- Connectivity: OFFLINE — no network tools are registered; all local tools work\n",
			)
		}
	case networkTools() != "":
		b.WriteString(
			"- Connectivity: online (" + networkTools() + " available)\n",
		)
	default:
		b.WriteString(
			"- Connectivity: online (no network tools registered)\n",
		)
	}

	b.WriteString(
		"- Tools available: " +
			strings.Join(tools, ", ") +
			"\n",
	)

	if cfg != nil &&
		len(cfg.EnabledTools) > 0 {
		b.WriteString(
			"- Tool selection: the user restricted the toolset — never call a tool outside this list\n",
		)
	}

	if cfg != nil &&
		!cfg.IsRemote() &&
		cfg.VisionEnabled {
		if proj := vision.FindProjector(
			cfg.ModelsDir,
			cfg.Model,
			cfg.VisionMMProj,
		); proj != "" {
			fmt.Fprintf(
				&b,
				"- Vision: ENABLED (projector %s) — attached images and screenshot captures arrive as real image parts you can see; analyze what is actually visible\n",
				filepath.Base(proj),
			)
		}
	}

	if cfg != nil &&
		cfg.ContinuumEnabled {
		fmt.Fprintf(
			&b,
			"- Continuum: ENABLED — long conversations roll into chapters; a [CONTINUUM FRAMEWORK] system block is your distilled memory of earlier chapters (mission, facts, decisions, open threads). Treat it as your own memory: continue seamlessly, never re-ask anything it answers, and pick up its open threads.\n",
		)
	}

	b.WriteString(
		"- Full instructions: see the AI Operating Instructions above and AI-CONTEXT.md in the app folder.\n",
	)

	return b.String()
}

// normalizeToolNames removes empty names and duplicates, then sorts the
// registry into a deterministic order so prompt construction remains stable
// across runs and makes cache behavior predictable.
func normalizeToolNames(names []string) []string {
	if len(names) == 0 {
		names = defaultToolNames
	}

	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// SystemMessage builds the complete first system message using the
// compatibility built-in tool list.
func SystemMessage(cfg *config.Config) string {
	return SystemMessageWithTools(
		cfg,
		nil,
	)
}

// SystemMessageWithTools builds the complete first system message using the
// actual registered tool names supplied by the runtime.
func SystemMessageWithTools(
	cfg *config.Config,
	registeredTools []string,
) string {
	dataDir := ""

	if cfg != nil {
		dataDir = cfg.DataDir
	}

	return Load(dataDir) +
		BriefingWithTools(
			cfg,
			registeredTools,
		)
}

// CompactMarker identifies a compact briefing (context-pressure mode).
const CompactMarker = "SHEYTAN compact briefing (context-pressure mode)"

// CompactSystemMessage builds a REDUCED first system message for
// small-context pressure: the identity header, the non-negotiable operating
// rules and the live environment block, dropping the long-form guidance
// sections of the full briefing. Used only by the Phase 7 preflight
// degradation ladder when the full briefing would crowd out the
// conversation itself; the full briefing is restored as soon as the
// context allows it.
func CompactSystemMessage(
	cfg *config.Config,
	registeredTools []string,
) string {
	var b strings.Builder

	b.WriteString("# " + HeaderSentinel + " — compact mode\n\n")
	b.WriteString("You are SHEYTAN, a local-first agent running fully on this machine.\n\n")
	b.WriteString(CompactMarker + ". The full briefing was compacted because the context window is tight; the rules below are binding.\n\n")
	b.WriteString("## OPERATING RULES\n")
	b.WriteString("- Tools execute; you coordinate. Never claim an action happened without the matching tool result in this conversation.\n")
	b.WriteString("- Verify outcomes objectively (build/test/file state). Your own statement of success is not evidence.\n")
	b.WriteString("- Stay inside the working directory and the tool policies; never invent tool names outside the registered list.\n")
	b.WriteString("- Keep answers tight; prefer completing the user's current instruction over narrating.\n")
	b.WriteString("- Persist durable facts with the memory tool instead of relying on conversation memory.\n\n")

	b.WriteString(BriefingWithTools(cfg, registeredTools))

	return b.String()
}

// HeaderSentinel is a stable substring identifying an AI-context system
// message, so callers can avoid double-prepending it.
const HeaderSentinel = "SHEYTAN™ Local-Agent — AI Operating Instructions"

func cap(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}
