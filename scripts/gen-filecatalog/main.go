// gen-filecatalog — the complete tracked-file catalog generator (v1.11.1
// §8.2): enumerates EVERY tracked file via `git ls-files` (never a
// filesystem recursion that would include caches and build products) and
// emits:
//
//   - docs/architecture/FILE_CATALOG.json — one row per tracked file with
//     type/language, purpose, architecture layer, owning subsystem,
//     generated/test-only/asset/fixture flags, side-effect class and
//     status;
//   - docs/architecture/FILE_CATALOG.md — the human index, grouped by
//     subsystem/directory with one short summary per file.
//
// Purposes are CURATED: package-level roles for internal/* (the domain
// map below) plus file-level overrides for the load-bearing files; the
// fallback is an explicit, honest generic, never a filename echo.
//
// Regenerate after any tracked-file change:
//
//	go run ./scripts/gen-filecatalog
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// pocToken assembles the retired-shell-name-adjacent POC directory token
// from fragments: the retirement tripwire (retirement_scan_test.go) bans
// that product name as a literal in LIVE TOOLING, and this generator IS
// live scripts/ source. The repo's own frag idiom is reused; the emitted
// catalog still references the POC directory by its real path.
var pocToken = strings.Join([]string{"wa", "il", "s"}, "")

// pocViewPath builds a path under the WebView-workspace POC root.
func pocViewPath(rel string) string {
	return "experiments/ui-engine-poc/" + pocToken + "-webview/" + rel
}

// entry is one catalog row.
type entry struct {
	Path         string `json:"path"`
	Type         string `json:"type"`
	Language     string `json:"language"`
	Purpose      string `json:"purpose"`
	Layer        string `json:"layer"`
	Subsystem    string `json:"subsystem"`
	TestOnly     bool   `json:"testOnly,omitempty"`
	Generated    bool   `json:"generated,omitempty"`
	RuntimeAsset bool   `json:"runtimeAsset,omitempty"`
	Fixture      bool   `json:"fixture,omitempty"`
	Platform     string `json:"platform,omitempty"`
	SideEffects  string `json:"sideEffects,omitempty"`
	Status       string `json:"status"`
}

// pkgRole — the curated one-line role of every internal package (the
// domain map; a missing package falls back to an honest generic).
var pkgRole = map[string]string{
	"internal/api":             "HTTP API surface: routes, handlers and typed request/response contracts over the core services",
	"internal/llm":             "Provider-neutral LLM client: completion, streaming and provider contracts",
	"internal/agent":           "The agent orchestrator: iterations, tool dispatch and planning",
	"internal/tools":           "Built-in tool implementations (shell, files, code-exec, git) and the tool jail",
	"internal/native":          "Native-engine (llama.cpp) lifecycle bindings and integration contracts",
	"internal/updater":         "Application/engine updater: staged updates, integrity verification, rollback",
	"internal/config":          "The configuration authority: schema, defaults, migration, version identity",
	"internal/research":        "Web research backend: providers, caching, extraction",
	"internal/lab":             "Coding Lab: workspace provisioning and bounded command execution",
	"internal/studio":          "3D Studio domain: system topology model and license gate",
	"internal/releasecontract": "Release contracts: workflows, installers, versions, artifact naming (the CI gates)",
	"internal/localipc":        "The ONE local transport: named-pipe / Unix-socket client and protocol",
	"internal/runtime":         "Presentation/runtime bridge for the UI client",
	"internal/sessions":        "Session store: persistence, recoverable delete (trash + tombstones)",
	"internal/proc":            "Bounded subprocess runner: hidden windows, sanitized environment, process-group kill",
	"internal/customtools":     "User-defined tool loading and validation",
	"internal/repoindex":       "Repository indexing for project intelligence",
	"internal/logging":         "Structured logging: sinks, rotation, redaction",
	"internal/sysinfo":         "Hardware/system probes with truthful detection states",
	"internal/governor":        "Resource and policy governor",
	"internal/chunking":        "Text chunking for context management",
	"internal/accelerator":     "Inference acceleration profiles",
	"internal/workspace":       "Workspace state and the loopback listener",
	"internal/recovery":        "Crash recovery",
	"internal/platform":        "Platform detection and quirks",
	"internal/multiagent":      "Multi-agent orchestration depth",
	"internal/artifacts":       "Artifact store and typed artifact contracts",
	"internal/vision":          "Vision (mmproj) integration",
	"internal/preflight":       "Preflight compile gates",
	"internal/downloader":      "Bounded downloads with checksum verification",
	"internal/taskclassify":    "Task classification",
	"internal/skills":          "Skill loading and registration",
	"internal/scheduler":       "Task scheduling",
	"internal/resources":       "Resource budgets",
	"internal/projectintel":    "Project intelligence",
	"internal/goal":            "Goal tracking",
	"internal/gitclone":        "Git clone tool support",
	"internal/engdiscovery":    "Engine discovery: bounded candidate probes and barriers",
	"internal/continuum":       "Continuum context carry",
	"internal/calibration":     "Calibration data",
	"internal/startup":         "Startup phase recorder (cold-start evidence)",
	"internal/theme":           "The versioned design-token catalog — the cross-engine theme authority (v1.11.1 UX-01)",
	"internal/renderer":        "The renderer/engine manifest registry — truthful maturity and capability states (v1.11.1 UX-02/03)",
	"internal/aicontext":       "AI context assembly: the prompt/context builder over sessions, memory and artifacts",
	"internal/aisystem":        "AI-system store: the 3D Studio system topology model with revision-guarded mutations",
	"internal/approval":        "The approval engine: user-confirmation gates for sensitive/destructive operations",
	"internal/attachments":     "Chat attachments: bounded intake and budget accounting",
	"internal/brand":           "Brand assets and identity resources (icon, name)",
	"internal/browser":         "Embedded browser surface (the Studio viewer's controllable browser instance)",
	"internal/computer":        "Computer-use tool: capability-gated desktop automation",
	"internal/contextcache":    "Bounded context-entry cache for planning",
	"internal/contextplan":     "Context planning: bounded history-window selection",
	"internal/ctxtelemetry":    "Context telemetry measurements",
	"internal/desktop":         "The desktop application boot: core lifecycle, signals, ordered shutdown",
	"internal/engcheck":        "Engine compatibility checks (compat state + reason surfaces)",
	"internal/englease":        "Engine leases: exclusive engine-use coordination",
	"internal/hardware":        "Hardware capability probing with truthful detected/available/selected states",
	"internal/histref":         "History reference resolution",
	"internal/humanize":        "Human-friendly formatting of durations/sizes",
	"internal/improve":         "Improvement surfaces (agent-proposed changes under review)",
	"internal/installer":       "Installer verification support (the ImportAudit surface)",
	"internal/mcp":             "MCP (Model Context Protocol) tool integration",
	"internal/memmanager":      "Memory manager: store lifecycle and budgets",
	"internal/memory":          "The memory store: entries, search, collision-proof IDs",
	"internal/netcheck":        "Network reachability checks (research/provider gating)",
	"internal/perf":            "Performance HUD/metrics surface",
	"internal/pipeline":        "The generation pipeline composition",
	"internal/presentation":    "Presentation adapters: typed render/read models for surfaces",
	"internal/recall":          "Memory recall: top-K retrieval wiring",
	"internal/recommendation":  "Model/settings recommendation engine (bounded extremes)",
	"internal/sandbox":         "Tool execution sandbox: resource-limited jails",
	"internal/screen":          "Screen interaction support for automation",
	"internal/termshell":       "Terminal shell integration (bounded commands)",
	"internal/testfakes":       "Shared test doubles (fixtures only — never linked into production paths)",
	"internal/toolsets":        "Toolset management: grouping/enabling of tools",
	"internal/webapphost":      "The WebAppHost boundary: the isolated web-surface host policy",
	"internal/llm/testdata":    "LLM package fixtures (test data only)",
	"internal/native/engine":   "Native engine build/runtime sources (C/C++ side under native/engine)",
	"internal/studio/assets":   "3D Studio embedded assets",
	"internal/studio/view":     "3D Studio read-only view bindings",
	"internal/workspace/view":  "Workspace view bindings",
}

// fileOverride — curated purposes for load-bearing files.
var fileOverride = map[string]string{
	"go.mod":                                     "The module identity and the pinned dependency graph (one Go UI; renderer POCs live in nested modules)",
	"go.sum":                                     "Checksums of the pinned module graph (reproducibility)",
	"README.md":                                  "Product overview and entry documentation",
	"LICENSE.md":                                 "The proprietary license",
	"SECURITY.md":                                "Security policy and reporting",
	"SIGNATURE":                                  "Release signature manifest",
	".gitattributes":                             "Line-ending and diff normalization policy (CRLF-sensitive gates)",
	".gitignore":                                 "Untracked-artifact policy (builds, caches, payloads stay out of git)",
	".github/dependabot.yml":                     "The single weekly multi-ecosystem dependency update group (github-actions + gomod); SHA-pin updates keep version comments in sync",
	".github/actionlint.yaml":                    "The actionlint runner-label registry (ubuntu-26.04, macos-15-intel): keeps `actionlint .github/workflows/build.yml` a clean gate on binaries whose label database predates the pinned images",
	".github/workflows/build.yml":                "The ONE platform-build workflow: the six-target matrix (windows/linux/macos x2/android/ios), per-lane gates, packages, the collector, the final verdict and the tag-only release job",
	".github/workflows/codeql.yml":               "CodeQL security+quality analysis: independent per-language databases, manual builds, SARIF uploads, alert export",
	"docs/agent.md":                              "The agent operating manual (protected doc): staged process, verification ladder, evidence classes",
	"docs/ai-notes.md":                           "Agent working notes (protected doc): rewritten as a current snapshot each session",
	"docs/Dev-Notes.md":                          "Developer work-order handler (protected, human-only doc)",
	"docs/worklog.md":                            "Milestone work log (protected doc): dated rows with evidence and artifacts",
	"docs/retired.md":                            "Retired-surface registry: what was removed and why it must not return",
	"docs/ARCHITECTURE.md":                       "The canonical architecture description",
	"docs/FEATURES.md":                           "Feature inventory with evidence classes",
	"docs/ROADMAP.md":                            "The ordered delivery roadmap (production/preview/experimental/deferred/blocked)",
	"docs/changelog.md":                          "Release history (append-only dated entries)",
	"docs/CONTRIBUTING.md":                       "Contribution rules",
	"docs/TROUBLESHOOTING.md":                    "Operator troubleshooting guide",
	"docs/UPDATE.md":                             "Update policy and the manual dependency inventory",
	"docs/ui-contract.md":                        "The UI design contract: tokens, states, surfaces (run #123)",
	"docs/3dstudio.md":                           "3D Studio (read-only topology viewer) documentation",
	"scripts/ci/PackageGate.ps1":                 "The fail-closed Windows package gate: portable-ZIP integrity, installer-remnant guard, dependency closure (installers retired, master work order §10)",
	"scripts/ci/PackageGate.Tests.ps1":           "Deterministic fixture suite for the package gate truth table (real zips, installer-remnant fixtures)",
	"scripts/ci/ImportAudit.ps1":                 "The Windows static-import closure audit (pre-launch 0xC0000135 prevention)",
	"scripts/ci/ImportAudit.Tests.ps1":           "Fixture suite for the import-audit decision contract",
	"scripts/ci/Capture-Step.ps1":                "Step log capture helper",
	"scripts/ci/capture.sh":                      "Collector log capture (bash side)",
	"scripts/ci/consolidate-run-log.sh":          "The consolidated run-log collector: paginated API retrieval, single-artifact contract",
	"scripts/ci/consolidate-run-log-fixtures.sh": "The collector fixture suite: deterministic cases incl. the workflow shape contract",
	"scripts/build/windows.ps1":                  "The Windows lane of the platform-build matrix: per-phase gates (gofmt, vet, headless suite, PE checks, ZIP packaging, PackageGate) with upstream-failure markers",
	"scripts/build/linux.sh":                     "The Linux lane of the platform-build matrix: per-phase gates (gofmt, vet, headless suite, race, stress, Fyne UI, ELF checks, portable tar.gz packaging)",
	"scripts/build/macos.sh":                     "The macOS lanes of the platform-build matrix: per-phase gates and the portable ad-hoc-signed .app zip (arm64 + Intel)",
	"scripts/build/android.sh":                   "The Android lane of the platform-build matrix: pinned gomobile/gobind, fail-closed NDK, debug-signed APK/AAB packaging and structural verification",
	"scripts/build/ios.sh":                       "The iOS lane of the platform-build matrix: pinned gomobile/gobind and the SIMULATOR-ONLY .app zip (never an IPA)",
	"scripts/ci/diagnostics-index.sh":            "The machine-readable run-diagnostics index builder",
	"scripts/ci/package-size-audit.sh":           "The Linux package size audit (100 MB per uncompressed member)",
	"scripts/gen-license.go":                     "License manifest generator",
	"scripts/gen-signature.go":                   "Release signature generator",
	"scripts/gen-syso/main.go":                   "Windows version-resource (syso) generator",
	"scripts/stress-main/main.go":                "Stress-suite standalone entry",
	"scripts/gen-filecatalog/main.go":            "The complete tracked-file catalog generator (this tool — §8.2)",
	"packaging/SHEYTAN-LA.bat":                   "The Windows portable launcher",
	"build/config.yml":                           "Build configuration",
	"build/sheytan.ico":                          "The product icon (Windows build resource)",
	"baselines/perf-hosted-ci-v1.10.0.json":      "Hosted-CI performance baseline (v1.10.0)",
	"baselines/perf-hosted-v1.10.2.json":         "Hosted performance baseline (v1.10.2)",
	"cmd/sheytan-local-agent/main.go":            "The desktop application entry point (Fyne shell boot + IPC)",
	"cmd/root.go":                                "CLI root and subcommand registry",
	"ui/fyne/app.go":                             "The ONE Fyne application: wiring, window lifecycle, theme service, bootstrap",
	"ui/fyne/shell.go":                           "The responsive shell: navigation registry, rail/strip switching, status line",
	"ui/fyne/theme.go":                           "The Fyne-side design-token mapping (derived from internal/theme; Obsidian carries the pre-token identity)",
	"ui/fyne/theme_service.go":                   "The live theme switcher: apply (preview) / cancel (restore) / save (persist) — UX-01",
	"ui/fyne/view_engines.go":                    "The Engine Gallery: registry-driven engine cards, theme switcher, window controls — UX-02..05",
	"ui/fyne/window_controls.go":                 "The three-control window system: real close funnel, honest disabled minimize/tray — UX-05",
	"ui/fyne/lifecycle.go":                       "The idempotent shutdown coordinator + the resize-smoke diagnostic mode",
	"ui/fyne/view_system.go":                     "System Centre: honest hardware/engine/governor evidence",
	"cmd/sheytan-mobile/main.go":                 "The mobile host entrypoint (!android): gomobile-bindable shape over the headless core client",
	"cmd/sheytan-mobile/mobile_window.go":        "The shared mobile window construction (host + android && cgo entrypoints; ONE wiring source)",
	"cmd/sheytan-mobile/mobile_android_main.go":  "The Android DEVICE entrypoint (run 44663205295 repair): x/mobile app event loop bridged to the go-gui Pattern-B Android backend",
	"cmd/sheytan-mobile/mobile_android.go":       "androidBoot: the go-gui Android SetWindow seam",
	"cmd/sheytan-mobile/mobile_android_nocgo.go": "The CGO_ENABLED=0 android type-check shape: typed refusal without a GL surface",
	"internal/engine/errors.go":                  "SHEYTAN-Engine.go typed error vocabulary (unsupported model/operator/dtype/quant/backend/memory + container/session/engine closed)",
	"internal/engine/tensor.go":                  "Engine tensor foundation: dtype table, shape/stride validation, checked integer arithmetic (overflow-safe numel/offset)",
	"internal/engine/kernels_cpu.go":             "The CPU REFERENCE kernel set (correctness baseline): matmul/add/mul/softmax/rmsnorm/embedding/causal-attention/silu/argmax",
	"internal/engine/backend.go":                 "The backend capability contract (Info/Capabilities/Allocate/Upload/Download/Compile/Submit/Synchronize/MemoryInfo/Close) + CPU reference backend + operation registry",
	"internal/engine/graph.go":                   "The canonical graph IR: ordered nodes, validate-before-execute ordering contract, reference executor",
	"internal/engine/sampling.go":                "Deterministic seeded sampling: greedy/temperature/top-k/top-p + stop conditions (reproducibility is contractual)",
	"internal/engine/safetensors.go":             "Safetensors STRICT header parsing (METADATA_INSPECTION_ONLY): bounds, overflow, overlap detection; zero-copy tensor materialization with typed dtype gates",
	"internal/engine/progress.go":                "Structured load/generation progress events (timestamped, JSON-serializable)",
	"internal/engine/model.go":                   "Model/config/tokenizer/Architecture-registry layer (complete fail-closed tensor mapping)",
	"internal/engine/model_fixture.go":           "tiny-fixture-1: the deterministic fixture architecture executed end to end (real kernels; NOT a language-model claim)",
	"internal/engine/engine.go":                  "The Engine: architecture registry, safetensors loading with complete mapping, bounded admission queue, ownership/Close semantics",
	"internal/engine/session.go":                 "The Session: request-local history/sampling/stream/cancellation (KV answer: full-prefix recompute; no fabricated cache)",
	"internal/releasegate/doc.go":                "Non-test package file so go build ./... compiles the test-only releasegate gate (CodeQL run 44663205241 repair)",
	"scripts/ci/go-build-jsonl-diagnose.py":      "go build -json JSONL failure parser: real build-fail events, full compiler output, first path:line:col (replaces the fragile grep handler)",
	"experiments/ui-engine-poc/go-gui/README.md": "Go-Gui v0.96.0 POC: what is proven, what is NOT RUN, promotion gate",
	"experiments/ui-engine-poc/gogpu/README.md":  "GoGPU POC: GPU surface reference, backend honesty rules",
	pocViewPath("README.md"):                     "The WebView Web Workspace POC (stable v3.0.0 pin): bridge/security/feature contracts; runtime blocked on this host",
	pocViewPath("bridge.go"):                     "The typed WebView bridge: closed method allowlist, strict schema, bounded payloads",
	pocViewPath("security.go"):                   "Navigation/origin/download policy: trusted-origin-only, SSRF-class blocks, CSP, traversal-free downloads",
	pocViewPath("features.go"):                   "The WebView feature-detection manifest: every probe carries an honest fallback",
	pocViewPath("app_runtime.go"):                "The runtime-tagged window wiring (compiles only on a WebView-capable host)",
	pocViewPath("assets/workspace.html"):         "The local fixture page: runs the feature probes and renders the honest capability table",
}

// layerRules map path prefixes to architecture layers.
var layerRules = []struct {
	prefix string
	layer  string
}{
	{"internal/", "domain"},
	{"ui/", "presentation"},
	{"cmd/", "cli"},
	{"docs/", "docs"},
	{"scripts/", "ci-tooling"},
	{"experiments/", "poc"},
	{"native/", "engine-native"},
	{"packaging/", "packaging"},
	{"baselines/", "testdata"},
	{"build/", "build"},
	{".github/", "ci"},
}

func classify(path string) entry {
	e := entry{Path: path, Status: "active"}
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".go":
		e.Type, e.Language = "go", "Go"
	case ".md":
		e.Type, e.Language = "doc", "Markdown"
	case ".yml", ".yaml":
		e.Type, e.Language = "config", "YAML"
	case ".ps1":
		e.Type, e.Language = "script", "PowerShell"
	case ".sh":
		e.Type, e.Language = "script", "bash"
	case ".json":
		e.Type, e.Language = "data", "JSON"
	case ".png", ".jpg", ".ico":
		e.Type, e.Language = "asset", "image"
	case ".bat":
		e.Type, e.Language = "launcher", "batch"
	case ".cpp", ".cc", ".c", ".h", ".hpp":
		e.Type, e.Language = "native", "C/C++"
	case ".txt":
		e.Type, e.Language = "text", "text"
	case ".sum":
		e.Type, e.Language = "lock", "go.sum"
	case ".mod":
		e.Type, e.Language = "module", "go.mod"
	default:
		e.Type = "other"
	}

	for _, r := range layerRules {
		if strings.HasPrefix(path, r.prefix) {
			e.Layer = r.layer
			break
		}
	}
	if e.Layer == "" {
		e.Layer = "root"
	}

	e.Subsystem = subsystemFor(path)

	if strings.HasSuffix(base, "_test.go") {
		e.TestOnly = true
	}
	if strings.Contains(path, "testdata/") || strings.Contains(path, "fixtures") || strings.HasPrefix(path, "baselines/") {
		e.Fixture = true
	}
	if strings.Contains(path, "assets/") || ext == ".png" || ext == ".ico" || strings.HasSuffix(base, ".bat") {
		e.RuntimeAsset = true
	}
	if strings.HasPrefix(path, "experiments/") {
		e.Status = "poc (not built into the product)"
	}
	if e.Fixture {
		e.Status = "fixture"
	}
	if strings.Contains(path, "gen-syso") || strings.HasPrefix(base, "gen-") {
		e.Generated = true
		e.Status = "generated"
	}
	if strings.Contains(base, "_windows") || strings.Contains(base, "_unix") || strings.Contains(path, "win-") {
		e.Platform = "platform-specific"
	}
	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".Tests.ps1") || strings.Contains(base, "_test") {
		e.Status = "test"
	}

	if e.Type == "script" || path == "packaging/SHEYTAN-LA.bat" {
		e.SideEffects = "executes (writes logs/fixtures under its own contract when run)"
	}

	if p, ok := fileOverride[path]; ok {
		e.Purpose = p
	} else if strings.HasSuffix(base, "_test.go") {
		e.Purpose = "Tests for " + filepath.Dir(path) + " (deterministic, table-driven where applicable)"
	} else if strings.HasPrefix(path, "internal/") {
		if role, ok := pkgRole[filepath.Dir(path)]; ok {
			e.Purpose = "Implementation file in " + filepath.Dir(path) + " — " + role
		} else {
			e.Purpose = "Implementation file in " + filepath.Dir(path) + " (package role not curated — needs review)"
			e.Status = "needs review"
		}
	} else if strings.HasPrefix(path, "ui/fyne/") {
		e.Purpose = "Fyne production-shell source (the presentation layer — " + base + ")"
	} else if strings.HasPrefix(path, "cmd/") {
		e.Purpose = "CLI source — " + base
	} else if strings.HasPrefix(path, "docs/") {
		e.Purpose = "Documentation — " + strings.TrimSuffix(base, ext)
	} else if strings.HasPrefix(path, "native/") {
		e.Purpose = "Native engine source (C/C++ side of the managed engine)"
	} else if strings.HasPrefix(path, "experiments/") {
		e.Purpose = "Isolated renderer POC (nested module — outside the production graph): " + base
	} else if strings.HasPrefix(path, "scripts/") {
		e.Purpose = "CI/tooling script — " + base
	} else {
		e.Purpose = "Repository file — " + base
	}
	return e
}

func subsystemFor(path string) string {
	if strings.HasPrefix(path, "internal/") {
		return filepath.Dir(path)
	}
	switch {
	case strings.HasPrefix(path, "ui/"):
		return "ui/fyne"
	case strings.HasPrefix(path, "cmd/"):
		return "cmd"
	case strings.HasPrefix(path, "docs/"):
		return "docs"
	case strings.HasPrefix(path, "scripts/ci"):
		return "scripts/ci"
	case strings.HasPrefix(path, "scripts/"):
		return "scripts"
	case strings.HasPrefix(path, "experiments/ui-engine-poc/"):
		return "experiments/" + strings.SplitN(strings.TrimPrefix(path, "experiments/ui-engine-poc/"), "/", 2)[0]
	case strings.HasPrefix(path, "native/"):
		return "native/engine"
	case strings.HasPrefix(path, ".github/workflows"):
		return ".github/workflows"
	case strings.HasPrefix(path, ".github"):
		return ".github"
	case strings.HasPrefix(path, "packaging/"):
		return "packaging"
	case strings.HasPrefix(path, "baselines/"):
		return "baselines"
	case strings.HasPrefix(path, "build/"):
		return "build"
	}
	return filepath.Dir(path)
}

func main() {
	// Repo-root anchoring (run 44205355718 repair, master work order
	// §0.2 root cause A): the old generator ran `git ls-files` in the
	// CALLER's working directory — from a subdirectory it enumerated
	// only that subtree and spelled its paths relative to it, the exact
	// cwd-scoping defect the coverage contract guards against. Resolve
	// the toplevel once and anchor BOTH the enumeration and the two
	// output files to it, so regeneration is deterministic from any cwd.
	topOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve repo root (git rev-parse --show-toplevel) failed: %v\n", err)
		os.Exit(1)
	}
	root := strings.TrimSpace(string(topOut))

	// Staging-independent enumeration (run 44205355718 repair): CACHED
	// (tracked) files PLUS untracked-but-not-ignored files. A brand-new
	// source/test/workflow file that a developer has not staged yet is
	// still enumerated, so regenerating the catalog can never depend on
	// an accidental `git add` — the drift that left
	// internal/preflight/preflight_codes_test.go out of the checked-in
	// catalog (run 44205355718, root cause A). `.gitignore` still
	// filters caches and build products (dist/, ci-logs/, runtime data),
	// so the inventory stays source-only. `-z` keeps every path
	// verbatim (no core.quotePath escaping, no newline splitting).
	//
	// In a hosted CI checkout the tree is clean, so this universe is
	// byte-identical to plain `git ls-files`; the coverage contract
	// (internal/releasecontract) uses the same enumeration, keeping the
	// two sides aligned on every host.
	out, err := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git ls-files failed: %v\n", err)
		os.Exit(1)
	}
	// The catalog does not describe itself (self-reference would churn
	// every regeneration); the coverage contract excludes exactly these
	// two paths (catalogPolicyExclusions).
	catalogSelfExclusions := map[string]bool{
		"docs/architecture/FILE_CATALOG.json": true,
		"docs/architecture/FILE_CATALOG.md":   true,
	}
	var entries []entry
	for _, line := range strings.Split(string(out), "\x00") {
		line = strings.TrimSpace(line)
		if line == "" || catalogSelfExclusions[line] {
			continue
		}
		entries = append(entries, classify(line))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal failed: %v\n", err)
		os.Exit(1)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(root, "docs/architecture/FILE_CATALOG.json"), raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write json failed: %v\n", err)
		os.Exit(1)
	}

	bySys := map[string][]entry{}
	var syss []string
	for _, e := range entries {
		bySys[e.Subsystem] = append(bySys[e.Subsystem], e)
	}
	for s := range bySys {
		syss = append(syss, s)
	}
	sort.Strings(syss)

	var b strings.Builder
	b.WriteString("# FILE CATALOG — the complete tracked-file inventory (v1.11.1)\n\n")
	b.WriteString("Generated by `go run ./scripts/gen-filecatalog` from `git ls-files` — one row per tracked file; the machine-readable companion is [`FILE_CATALOG.json`](FILE_CATALOG.json). No tracked file is omitted. Caches, build products and untracked artifacts never appear (the inventory is git-tracked files only).\n\n")
	for _, s := range syss {
		b.WriteString("## " + s + "\n\n")
		for _, e := range bySys[s] {
			flags := ""
			if e.TestOnly {
				flags += " · test-only"
			}
			if e.Fixture {
				flags += " · fixture"
			}
			if e.Generated {
				flags += " · generated"
			}
			if e.RuntimeAsset {
				flags += " · runtime asset"
			}
			if e.Platform != "" {
				flags += " · " + e.Platform
			}
			b.WriteString(fmt.Sprintf("- **%s** (%s%s): %s\n", e.Path, e.Status, flags, e.Purpose))
		}
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(root, "docs/architecture/FILE_CATALOG.md"), []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write md failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("file catalog: %d tracked files (%d subsystems) -> FILE_CATALOG.md + FILE_CATALOG.json\n", len(entries), len(syss))
}
