// Package engdiscovery implements System Engine Discovery (v1.3.6,
// spec §10–§13): before downloading llama.cpp, intelligently search the
// machine for an already-installed compatible llama.cpp / SHEYTAN engine
// package and reuse it after validation.
//
// Discovery is TIERED (spec §11):
//
//	Tier 0 — managed local state: the managed bin dir, the persisted
//	         discovery cache, previously known-good candidates.
//	Tier 1 — cheap system locations: PATH, exe dir, sibling bin dirs,
//	         common user/package locations.
//	Tier 2 — full system scan: bounded parallel walk, noise-skipping,
//	         time-bounded. NEVER runs on the startup path.
//
// Discovery is NON-DESTRUCTIVE (spec §12): no candidate is executed
// because of its filename. Static evidence first (architecture, PE
// import closure), a bounded probe only after static checks pass.
package engdiscovery

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// Candidate is one discovered engine package entry point.
type Candidate struct {
	Path       string    `json:"path"`
	Kind       string    `json:"kind"` // "llama-server" | "shtn-engine-host"
	Size       int64     `json:"size"`
	ModTime    int64     `json:"modTimeUnix"`
	SHA256     string    `json:"sha256,omitempty"`
	Arch       string    `json:"arch,omitempty"`
	Format     string    `json:"format,omitempty"`
	Tag        string    `json:"tag,omitempty"`
	Tier       int       `json:"tier"`
	Source     string    `json:"source,omitempty"` // volume / location class
	Validated  bool      `json:"validated,omitempty"`
	Validation string    `json:"validation,omitempty"` // evidence string
	FoundAt    time.Time `json:"foundAt"`
}

// cacheFile is the persisted discovery cache (second startup is
// substantially cheaper — no re-scan, revalidation only).
const cacheFile = "discovery-cache.json"

func cachePath(cfg *config.Config) string {
	return filepath.Join(cfg.DataDir, cacheFile)
}

// LoadCache reads the persisted discovery cache.
func LoadCache(cfg *config.Config) []Candidate {
	data, err := os.ReadFile(cachePath(cfg))
	if err != nil {
		return nil
	}

	var cands []Candidate
	if err := json.Unmarshal(data, &cands); err != nil {
		return nil
	}

	return cands
}

// SaveCache persists the discovery cache atomically.
func SaveCache(cfg *config.Config, cands []Candidate) error {
	data, err := json.MarshalIndent(cands, "", "  ")
	if err != nil {
		return err
	}

	tmp := cachePath(cfg) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, cachePath(cfg))
}

// QuickFind runs the synchronous Tier 0 + Tier 1 pass. It is fast
// (bounded directory set, no deep walk) and safe on the boot path.
// Returns nil when no usable candidate is found — callers then fall
// back to downloading (never Tier 2 synchronously).
func QuickFind(cfg *config.Config, engineName string) (*Candidate, error) {
	name := engineName
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}

	var cands []Candidate

	// Tier 0a: managed bin dir (already the active location).
	managed := filepath.Join(updaterEngineBinDir(cfg), name)
	if cand, ok := inspectCandidate(managed, 0, "managed-bin"); ok {
		cands = append(cands, *cand)
	}

	// Tier 0b: persisted cache — validated candidates from a previous
	// run, revalidated by identity (size+mtime; hash only on mismatch).
	for _, cached := range LoadCache(cfg) {
		if filepath.Clean(cached.Path) == filepath.Clean(managed) {
			continue
		}

		if cand, ok := inspectCandidate(cached.Path, 0, "cache"); ok {
			if cand.Size == cached.Size && cand.ModTime == cached.ModTime {
				cand.SHA256 = cached.SHA256
				cand.Tag = cached.Tag
				cand.Validated = cached.Validated
				cand.Validation = cached.Validation
			}

			cands = append(cands, *cand)
		}
	}

	// Tier 1: cheap system locations.
	for _, dir := range tier1Directories(cfg) {
		p := filepath.Join(dir, name)
		if cand, ok := inspectCandidate(p, 1, "tier1:"+dir); ok {
			cands = append(cands, *cand)
		}
	}

	best := pickBest(cands, managed)
	if best == nil {
		return nil, nil
	}

	// The managed binary itself is always acceptable (preflight
	// validates it downstream); an external candidate must at least
	// match the host architecture before it is interesting.
	if filepath.Clean(best.Path) != filepath.Clean(managed) {
		if !engcheck.ArchMatchesHost(best.Arch) {
			return nil, nil
		}
	}

	mergeIntoCache(cfg, *best)

	return best, nil
}

// updaterEngineBinDir avoids an import cycle with internal/updater by
// resolving the managed bin dir locally (the derivation is one line and
// pinned by tests in config + updater).
func updaterEngineBinDir(cfg *config.Config) string {
	if cfg.LlamaBinPath != "" {
		return filepath.Dir(cfg.LlamaBinPath)
	}

	return filepath.Join(cfg.DataDir, "bin")
}

// tier1Directories lists the cheap search locations (spec §11 Tier 1):
// PATH, the executable directory, sibling bin directories and common
// user application / package locations.
func tier1Directories(cfg *config.Config) []string {
	var dirs []string

	seen := map[string]bool{}
	add := func(d string) {
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	// PATH entries.
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		add(p)
	}

	// Executable directory + sibling bin.
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		add(exeDir)
		add(filepath.Join(exeDir, "bin"))
		add(filepath.Join(filepath.Dir(exeDir), "bin"))
	}

	// The config's own data dir bin (managed) — harmless duplicate.
	add(filepath.Join(cfg.DataDir, "bin"))

	// Common user application locations.
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".local", "bin"))
		add(filepath.Join(home, "bin"))
		add(filepath.Join(home, "scoop", "shims"))
		add(filepath.Join(home, "AppData", "Local", "Programs"))
	}

	if local, ok := localAppData(); ok {
		add(filepath.Join(local, "Programs"))
		add(filepath.Join(local, "llama.cpp"))
		add(filepath.Join(local, "SHEYTAN-LA", "bin"))
	}

	// Previously observed candidate locations (cache dirs of past runs).
	for _, c := range LoadCache(cfg) {
		add(filepath.Dir(c.Path))
	}

	return dirs
}

func localAppData() (string, bool) {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return v, true
		}
	}

	if v, err := os.UserCacheDir(); err == nil {
		return v, true
	}

	return "", false
}

// inspectCandidate collects static metadata for one path. ok is false
// when the path is not a plausible engine binary (missing, directory,
// zero bytes, or unrecognizable format).
func inspectCandidate(path string, tier int, source string) (*Candidate, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() == 0 {
		return nil, false
	}

	arch, format := engcheck.SniffArch(path)
	if format == "" {
		return nil, false
	}

	return &Candidate{
		Path:    path,
		Size:    st.Size(),
		ModTime: st.ModTime().Unix(),
		Arch:    arch,
		Format:  format,
		Tier:    tier,
		Source:  source,
		FoundAt: time.Now().UTC(),
	}, true
}

// pickBest orders candidates: the managed binary first, then lowest
// tier, then largest binary (a real server build beats a stub).
func pickBest(cands []Candidate, managed string) *Candidate {
	if len(cands) == 0 {
		return nil
	}

	best := 0
	for i := range cands {
		a, b := cands[i], cands[best]

		aManaged := filepath.Clean(a.Path) == filepath.Clean(managed)
		bManaged := filepath.Clean(b.Path) == filepath.Clean(managed)

		switch {
		case aManaged && !bManaged:
			best = i
		case aManaged == bManaged && a.Tier < b.Tier:
			best = i
		case aManaged == bManaged && a.Tier == b.Tier && a.Size > b.Size:
			best = i
		}
	}

	c := cands[best]

	return &c
}

func mergeIntoCache(cfg *config.Config, cand Candidate) {
	cands := LoadCache(cfg)

	for i := range cands {
		if filepath.Clean(cands[i].Path) == filepath.Clean(cand.Path) {
			cands[i] = cand
			_ = SaveCache(cfg, cands)
			return
		}
	}

	cands = append(cands, cand)
	_ = SaveCache(cfg, cands)
}

// ---------------------------------------------------------------------------
// Tier 2 — bounded priority-frontier scan (background only, spec §11)
// ---------------------------------------------------------------------------

// ScanOptions bounds a Tier 2 scan.
type ScanOptions struct {
	// Workers bounds the parallel directory walk (bounded worker pool).
	Workers int

	// Timeout bounds the whole scan.
	Timeout time.Duration

	// MaxDepth bounds recursion depth per volume.
	MaxDepth int

	// MaxCandidates stops the scan after this many plausible candidates.
	MaxCandidates int
}

// DefaultScanOptions is the conservative default: 4 workers, 90 s, deep
// enough for user trees, candidate-capped.
func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		Workers:       4,
		Timeout:       90 * time.Second,
		MaxDepth:      12,
		MaxCandidates: 16,
	}
}

// noiseDirs are skipped wholesale during Tier 2 scans: OS internals,
// package-manager noise and VCS dirs that can never contain a user's
// engine install (bounded scan, no wasted IO).
//
// v1.8.7 — the list now also covers language-toolchain caches and
// per-run temp churn (Actions run 37273354268 reported a 126 s
// package runtime driven by deep, high-latency walks of exactly these
// trees — .rustup/.cargo/.go/.nuget/.dotnet on the runners, the
// hostedtoolcache roots, and %TEMP%). None of these locations is a
// plausible user engine install (deliberately NOT skipped: .local,
// bin, scoop shims, .ollama — all real engine hosts); skipping them
// removes avoidable IO on every platform without weakening discovery.
var noiseDirs = map[string]bool{
	// OS internals / system reserved.
	"$recycle.bin": true, "windows": true, "winsxs": true, "system volume information": true,
	"$windows.~bt": true, "$windows.~ws": true, "perflogs": true,
	"proc": true, "sys": true, "dev": true, "run": true,
	"temporary items": true, "vmware": true,

	// VCS / dependency trees.
	"node_modules": true, ".git": true, ".hg": true, ".svn": true,
	"vendor": true, "cache": true, ".cache": true,

	// Per-run temp churn — never a stable engine install location.
	"temp": true, "tmp": true,

	// Language toolchain / package-manager caches (Tier 1 covers PATH,
	// so tools actually installed for use remain discoverable).
	".cargo": true, ".rustup": true, ".go": true, ".npm": true,
	".nuget": true, ".dotnet": true, ".gradle": true, ".m2": true,
	".android": true, ".bun": true, ".deno": true, ".pnpm-store": true,
	".pyenv": true, ".rbenv": true, "__pycache__": true,
	".vscode": true, ".vscode-server": true, ".kube": true,
	".docker": true, ".vagrant.d": true, ".terraform": true,
	"hostedtoolcache": true,
}

// FullScan enumerates available volumes and scans them with a bounded
// worker pool fed by a priority frontier (spec §11 Tier 2). Inaccessible
// directories are ignored without crashing; file contents are never read
// except for validation of promising candidates. The returned candidates
// are static metadata only — run Validate before importing anything.
//
// v1.7.3 — the traversal is a BOUNDED PRIORITY FRONTIER, not a recursive
// DFS. The previous per-root walkBound was lexical-order DFS: it descended
// one subtree to full depth before touching its next sibling, so a huge
// sibling directory (AppData on the Windows CI runners) could consume the
// entire Tier 2 time budget before a shallow, high-value location such as
// <home>\sheytan-discovery-fixture was ever read (Actions run
// 36311052375 / Windows job 108597318493). Root reordering (v1.4.0)
// cannot fix that class of defect — the starvation happens INSIDE the
// first root's own subtree. The frontier inverts the shape:
//
//	priority directory frontier → bounded worker pool → read directory
//	→ inspect matching engine file → enqueue child directories
//	   with (class, depth, name) priority
//
// Directories are visited (class, depth)-ordered — the user home first,
// per-user application roots and previously observed candidate parents
// second, broad volume recursion last, and every shallower level is
// exhausted before any deeper level is touched. A valid engine a few
// levels below home can no longer be blocked behind a giant unrelated
// subtree: each directory visit costs one ReadDir, and the fixture's
// ancestors are therefore reached within a handful of directory reads
// regardless of sibling size.
//
// Preserved contract (unchanged from v1.4.0): Tier 0/Tier 1 are separate
// synchronous passes (QuickFind); Tier 2 remains background-only; the
// hard total timeout, bounded workers, MaxDepth, MaxCandidates,
// cancellation, inaccessible-directory tolerance, noise-directory
// skipping, candidate deduplication, static inspection, architecture
// validation, bounded executable validation, cache integration and
// symlink/reparse-point safety all keep their previous semantics.
//
// v1.8.7 — DETERMINISTIC RETENTION BARRIER (the v1.8.6 Windows CI
// failure, Actions run 37273354268). The frontier ordered QUEUED
// directories correctly, but multiple workers executed jobs
// concurrently and emit() sealed the whole scan on first arrival of
// the MaxCandidates-th candidate. Candidate discovery order was
// therefore race-dependent: an already-running deeper worker could
// report before a shallower worker and seal the scan, violating the
// documented contract that shallower discovery outranks deeper work
// regardless of worker completion order. The repair keeps parallelism
// WITHIN one discovery priority level and adds a deterministic
// priority barrier before a candidate can cause scan termination:
//
//	level barrier — a worker is only ever handed a job of the
//	current (lowest outstanding) priority value; deeper levels are
//	not started while the current level has queued or active work;
//
//	level-drain retention — candidates are buffered per level and
//	retained in (level, path) order when the level fully drains, so
//	the retained set is a pure function of the scanned dataset;
//
//	sealed-at-level — MaxCandidates seals the scan at the drain of
//	the level that satisfied it: every higher-priority level has
//	been exhausted and every same-level peer has been visited, so a
//	deeper or faster worker can never preempt a better candidate.
//
// Proof obligation (pinned by the regression suite): parallel scan +
// same dataset + repeated executions → same winning candidate, for
// any worker count and any interleaving. No sleeps, no timing
// assumptions, Workers never reduced to 1.
func FullScan(ctx context.Context, cfg *config.Config, engineName string, opts ScanOptions) []Candidate {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 90 * time.Second
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 12
	}
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 16
	}

	name := engineName
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}

	roots := scanRoots()

	scanCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	f := newScanFrontier(opts.MaxCandidates)

	// Seed the frontier in mission priority order: (1) the user's own
	// tree, (2) per-user application roots, (5) previously observed
	// candidate parents from the discovery cache; the broad volume
	// recursion (6) carries the lowest class. Seeding is ordered —
	// roots first, so an overlapping cache parent can never re-mark
	// the higher-priority home seed.
	for _, root := range roots {
		f.seed(root, scanRootClass(root))
	}
	// Cache-parent seeds are DEFERRED, not enqueued here: materializing
	// them up front pre-marked their directories as visited and demoted
	// any location inside the user tree to the seed's class-1 priority —
	// so a repeated scan could rank a previously-cached deep directory
	// above a fresh shallow one. The barrier flushes the deferred seeds
	// only when it enters the class-1 band (all class-0 work drained),
	// preserving the v1.4.0 intent (cached locations rank ahead of
	// broad volume recursion) without the priority inversion.
	for _, cached := range LoadCache(cfg) {
		f.deferSeed(filepath.Dir(cached.Path))
	}

	// Candidate sink (v1.8.7): the frontier itself dedupes by path,
	// buffers per-level candidates and retains them in (level, path)
	// order at each level drain. The scan seals only at the drain of
	// the level that satisfied MaxCandidates — never on first
	// arrival — so candidate selection is a deterministic function of
	// the dataset, not of worker completion order. One lock domain
	// for all scan state (v1.5.0 race audit preserved: every shared
	// read/write stays under the frontier mutex).

	// Bounded worker pool: at most opts.Workers goroutines ever exist,
	// each pulling the next-highest-priority directory from the
	// frontier. Parallelism stays within one priority level; the
	// barrier in next() advances the level only after its full drain.
	var wg sync.WaitGroup

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				if scanCtx.Err() != nil {
					return
				}

				job, ok := f.next()
				if !ok {
					return
				}

				f.visit(scanCtx, job, name, opts, f.emit)
				f.release()
			}
		}()
	}

	// Cancellation watcher: wake every worker blocked on the frontier the
	// moment the budget expires (hard total timeout, spec §11). Without
	// this a worker waiting on an idle-but-active frontier would only
	// notice cancellation when another worker happened to broadcast. No
	// leak: the watcher exits as soon as the workers finish.
	finished := make(chan struct{})

	go func() {
		select {
		case <-scanCtx.Done():
			f.stop()
		case <-finished:
		}
	}()

	wg.Wait()
	close(finished)

	found := f.result()

	// Validate the most promising candidates now (bounded: each is a
	// static check + one bounded probe).
	for i := range found {
		if scanCtx.Err() != nil {
			break
		}

		if engcheck.ArchMatchesHost(found[i].Arch) {
			ValidateCandidate(&found[i])
		}
	}

	mergeCacheAll(cfg, found)

	logging.Default().Info("engine",
		"system engine discovery (tier 2): scanned %d root(s), %d candidate(s)",
		len(roots), len(found))

	return found
}

// scanRootClass ranks a scan root for the priority frontier (lower is
// visited earlier): (0) the user's own tree, (1) per-user application
// roots, (2) broad volume recursion — the discovery priority order of
// spec §11. Overlapping roots are collapsed later by the frontier's
// visited set (e.g. LOCALAPPDATA lives inside the home tree), so the
// same directory is never scanned twice.
func scanRootClass(root string) int {
	clean := filepath.Clean(root)

	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return 0
	}

	if local, ok := localAppData(); ok && clean == filepath.Clean(local) {
		return 1
	}

	return 2
}

// scanRoots enumerates the volumes/roots to scan. On Windows that is the
// available drive letters; on Unix a small curated root set (bounded —
// never a blind / walk).
//
// v1.4.0 (run 35996462352): the USER TREE is scanned FIRST on every
// platform. On Windows the previous drive-letter-only root set meant a
// user-installed engine sat behind a full C:\ walk — deep behind
// AppData/Program Files alphabetically — so the bounded Tier 2 budget
// expired before the highest-value locations were reached, and
// discovery effectively never found user installs on Windows. Putting
// the home directory (and LOCALAPPDATA) at the FRONT of the root list
// matches the Unix behavior, reaches the most likely install locations
// within milliseconds, and lets the slower full-volume walks continue
// in parallel behind them.
func scanRoots() []string {
	var roots []string

	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" {
			return
		}
		clean := filepath.Clean(dir)
		if seen[clean] {
			return
		}
		seen[clean] = true
		roots = append(roots, clean)
	}

	// Highest-value roots first: the user's own tree (and, on Windows,
	// the per-user AppData location where engines are commonly
	// installed).
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}

	if runtime.GOOS == "windows" {
		if local, ok := localAppData(); ok {
			add(local)
		}

		for c := 'C'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if _, err := os.Stat(root); err == nil {
				add(root)
			}
		}

		return roots
	}

	// Unix: user trees and common install prefixes only.
	add("/usr/local")
	add("/opt")
	add("/snap")

	return roots
}

// ---------------------------------------------------------------------------
// Priority directory frontier (the Tier 2 traversal engine)
// ---------------------------------------------------------------------------

// Frontier priority arithmetic. A job's priority is
//
//	class*prioClassBand + depth*prioDepthBand - nameBonus
//
// so root class dominates, then depth (strictly: the name bonus is
// smaller than one depth band, so a "high-value" directory can never
// overtake a shallower level), then enqueue order breaks exact ties.
// Depth is capped far below the class band so the arithmetic can never
// leak a deep directory into a higher class.
const (
	prioClassBand = 1_000_000_000
	prioDepthBand = 100_000
	prioNameBonus = 50_000
	prioDepthCap  = 9_999

	// maxQueuedDirs is a defensive memory bound on the frontier. The
	// scan budget (timeout/cancellation) is the real limiter — no real
	// scan visits remotely this many directories — but a pathological
	// directory with millions of subdirectories must not be able to
	// grow the heap without bound. Past the cap, breadth is dropped
	// (shallow levels enqueue first, so the highest-value work is
	// never the part dropped).
	maxQueuedDirs = 200_000
)

// priorityDirNames are directory names that historically host engine
// installs (spec §11 Tier 1 locations, plus the product's own roots).
// A child with one of these names is visited ahead of its same-depth
// siblings (mission priority: common bin/Programs/llama.cpp locations).
var priorityDirNames = map[string]bool{
	"bin": true, "programs": true, "llama.cpp": true, "scoop": true,
	"shims": true, ".local": true, "sheytan-la": true,
	"sheytan-local-agent": true,
}

// dirJob is one directory in the frontier.
type dirJob struct {
	path  string
	depth int   // depth below its seed root
	class int   // root class (0 = home, 1 = per-user roots, 2 = volumes)
	prio  int64 // priority; lower is visited earlier
	seq   int64 // enqueue order — deterministic tiebreak
}

// dirHeap orders dirJobs by (prio, seq).
type dirHeap []*dirJob

func (h dirHeap) Len() int { return len(h) }

func (h dirHeap) Less(i, j int) bool {
	if h[i].prio != h[j].prio {
		return h[i].prio < h[j].prio
	}

	return h[i].seq < h[j].seq
}

func (h dirHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *dirHeap) Push(x any) { *h = append(*h, x.(*dirJob)) }

func (h *dirHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]

	return it
}

// scanFrontier is the bounded priority frontier: a mutex-guarded
// container/heap plus a condition variable that implements the classic
// parallel-BFS termination protocol (a worker may only exit when the
// heap is empty AND no worker is processing a job that could enqueue
// more work). All state lives under one mutex; no production path ever
// sleeps — waiting is condition-variable based, so shutdown is
// immediate at budget expiry, at MaxCandidates, or at full drain.
//
// v1.8.7 deterministic retention barrier: workers are only ever handed
// jobs of currentLevel — the lowest outstanding priority value. A level
// advances only after its full drain (queue empty AND no active job),
// so a deeper job can never run while shallower work is queued or in
// flight. Candidates are buffered per level (pending) and retained in
// (level, path) order (retained) at the level drain; the scan seals at
// the drain of the level that satisfied maxCandidates. Retention is
// therefore a pure function of the scanned dataset.
type scanFrontier struct {
	mu      sync.Mutex
	cond    *sync.Cond
	jobs    dirHeap
	visited map[string]bool
	seq     int64
	active  int
	stopped bool

	// Deterministic retention state (all guarded by mu).
	maxCandidates int
	currentLevel  int64 // lowest outstanding priority value; math.MinInt64 = unseeded
	capReached    bool  // retained+pending satisfied maxCandidates at the current level
	retained      []Candidate
	pending       []Candidate
	seenPaths     map[string]bool // defensive path dedupe across retained+pending

	// Deferred cache-parent seeds (all class 1, guarded by mu). They are
	// enqueued only when the barrier reaches their priority band, so a
	// cached location never pre-marks a directory that the higher-priority
	// natural roots would have discovered — pre-marking it there demoted
	// discovery to the seed class and made repeated scans order-dependent
	// (the v1.8.7 determinism audit finding).
	deferredSeeds []string
}

// maxPendingCandidates bounds the per-level candidate buffer. Real
// machines host a handful of engine binaries; the bound exists only so
// a pathological dataset cannot grow the buffer without limit. Past
// the bound extra same-level candidates are dropped (the higher-value
// shallow levels are always processed first, so this can only trim
// retention inside a single over-large level — never the priority
// contract between levels).
const maxPendingCandidates = 4096

func newScanFrontier(maxCandidates int) *scanFrontier {
	f := &scanFrontier{
		visited:       map[string]bool{},
		seenPaths:     map[string]bool{},
		maxCandidates: maxCandidates,
		currentLevel:  math.MinInt64, // unseeded: the first next() call adopts the top priority
	}
	f.cond = sync.NewCond(&f.mu)

	return f
}

// seed enqueues a scan root at depth 0.
func (f *scanFrontier) seed(path string, class int) {
	f.enqueue(path, 0, class)
}

// deferSeed records a cache-parent seed for deferred enqueue. The seed
// is materialized by flushDeferredSeedsLocked exactly when the barrier
// reaches the class-1 priority band — after every class-0 (user tree)
// level has fully drained — so it can never pre-empt or demote a
// higher-priority natural discovery.
func (f *scanFrontier) deferSeed(path string) {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.deferredSeeds = append(f.deferredSeeds, clean)
}

// flushDeferredSeedsLocked materializes the deferred cache-parent seeds
// (class 1, depth 0). Called only at level-drain decision points with
// mu held and active == 0 — no worker can race the enqueue. The
// visited set discards seeds whose directories the natural roots
// already covered.
func (f *scanFrontier) flushDeferredSeedsLocked() {
	if len(f.deferredSeeds) == 0 {
		return
	}

	seeds := f.deferredSeeds
	f.deferredSeeds = nil

	for _, p := range seeds {
		f.enqueueLocked(p, 0, 1)
	}
}

// enqueue adds one directory unless the scan is stopped, the directory
// was already queued (the visited set also collapses overlapping roots,
// so a directory is never scanned twice), or the defensive queue cap is
// reached.
func (f *scanFrontier) enqueue(path string, depth int, class int) {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.enqueueLocked(clean, depth, class)
}

// enqueueLocked is the lock-held body of enqueue.
func (f *scanFrontier) enqueueLocked(clean string, depth int, class int) {
	if f.stopped || f.visited[clean] || len(f.jobs) >= maxQueuedDirs {
		return
	}

	f.visited[clean] = true

	if depth > prioDepthCap {
		depth = prioDepthCap
	}

	bonus := int64(0)
	if priorityDirNames[strings.ToLower(filepath.Base(clean))] {
		bonus = prioNameBonus
	}

	prio := int64(class)*prioClassBand + int64(depth)*prioDepthBand - bonus
	if prio < 0 {
		prio = 0
	}

	f.seq++
	heap.Push(&f.jobs, &dirJob{path: clean, depth: depth, class: class, prio: prio, seq: f.seq})
	f.cond.Broadcast()
}

// stop seals the frontier: no further enqueues are accepted and every
// worker drains out at its next check.
func (f *scanFrontier) stop() {
	f.mu.Lock()

	if !f.stopped {
		f.stopped = true
		f.cond.Broadcast()
	}

	f.mu.Unlock()
}

// next blocks for the highest-priority job. It returns ok=false when the
// scan is stopped or the frontier is fully drained (heap empty and no
// worker processing — the only safe exit, because a running worker may
// still enqueue children).
//
// v1.8.7 barrier: a job is handed out only when it belongs to the
// current level (top prio == currentLevel). When the current level's
// queue is exhausted and no worker is still active in it, the level's
// pending candidates are retained (deterministically) and the barrier
// either advances to the next level or — if the candidate budget was
// satisfied inside the level — seals the scan. A deeper job therefore
// cannot start while any higher-priority work is queued or active.
func (f *scanFrontier) next() (dirJob, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for {
		if f.stopped {
			return dirJob{}, false
		}

		if len(f.jobs) > 0 {
			top := f.jobs[0].prio

			if top == f.currentLevel {
				job := heap.Pop(&f.jobs).(*dirJob)
				f.active++

				return *job, true
			}

			// top > currentLevel: the current level's queued work is
			// exhausted. The barrier may move only when no worker is
			// still processing a job of this level — an active visit
			// can still emit a same-level candidate (which must take
			// part in this level's deterministic retention).
			if f.active == 0 {
				f.mergePendingLocked()

				if f.capReached {
					// The candidate budget is satisfied and every job
					// of its level has been visited: seal the scan.
					// Deeper levels are never started — this is the
					// contract the v1.8.6 race violated.
					f.stopped = true
					f.cond.Broadcast()

					return dirJob{}, false
				}

				// Entering the class-1 band: materialize the deferred
				// cache-parent seeds now (prioClassBand = the class-1
				// base). Every class-0 level has fully drained above,
				// so a seed can no longer pre-mark a directory the
				// user tree would have discovered at its natural
				// (higher) priority.
				if top >= prioClassBand && len(f.deferredSeeds) > 0 {
					f.flushDeferredSeedsLocked()
					continue
				}

				f.currentLevel = top
				continue
			}

			f.cond.Wait()
			continue
		}

		// Queue empty.
		if f.active == 0 {
			// Deferred cache seeds still pending: materialize them
			// (defensive — the natural root set is never empty in
			// production, but the flush must not be skippable).
			if !f.capReached && len(f.deferredSeeds) > 0 {
				f.flushDeferredSeedsLocked()
				continue
			}

			// Fully drained: retain the last level's pending
			// candidates (if any) and end the scan.
			f.mergePendingLocked()

			if f.capReached {
				f.stopped = true
				f.cond.Broadcast()
			}

			return dirJob{}, false
		}

		f.cond.Wait()
	}
}

// mergePendingLocked retains the drained level's candidates in
// deterministic (path) order, bounded by maxCandidates. Called only at
// level-drain decision points with mu held. The pending buffer is
// reset, so the next level buffers into a clean slate.
func (f *scanFrontier) mergePendingLocked() {
	if len(f.pending) > 0 {
		sort.Slice(f.pending, func(i, j int) bool {
			return f.pending[i].Path < f.pending[j].Path
		})

		for _, c := range f.pending {
			if f.maxCandidates > 0 && len(f.retained) >= f.maxCandidates {
				break
			}

			f.retained = append(f.retained, c)
		}

		f.pending = f.pending[:0]
	}
}

// emit records one discovered candidate into the current level's
// pending buffer. It reports false only when the scan is stopped (the
// caller, visit, then aborts its directory iteration). The scan is NOT
// sealed here on candidate arrival: sealing happens at the level drain
// in next(), after every same-level peer has been visited — that is
// the v1.8.7 determinism repair (the v1.8.6 code sealed here, which
// made retention race-dependent on worker completion order).
func (f *scanFrontier) emit(cand Candidate) bool {
	clean := filepath.Clean(cand.Path)

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.stopped {
		return false
	}

	// Defensive path dedupe (the visited set already prevents double
	// visits; this also guards against dataset aliasing).
	if f.seenPaths[clean] {
		return true
	}

	f.seenPaths[clean] = true

	if len(f.pending) < maxPendingCandidates {
		f.pending = append(f.pending, cand)
	}

	if !f.capReached && f.maxCandidates > 0 &&
		len(f.retained)+len(f.pending) >= f.maxCandidates {
		// Budget satisfied at THIS level. The scan seals at the level
		// drain (next()); broadcast so workers blocked on the barrier
		// re-evaluate immediately.
		f.capReached = true
		f.cond.Broadcast()
	}

	return true
}

// isCapReached reports whether the candidate budget is already
// satisfied at the current level. visit() uses it to skip enqueueing
// child directories that could never be visited (the scan seals at the
// current level's drain) — avoidable-work reduction, not a semantic
// change: those children were unreachable in any case.
func (f *scanFrontier) isCapReached() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.capReached
}

// result returns the deterministically retained candidates. The
// pending buffer is merged first so a scan interrupted by timeout or
// cancellation still reports the candidates discovered inside its
// final, in-flight level (best-effort on that path — the deterministic
// contract applies to scans that complete their levels; cancellation
// is inherently timing-dependent). Normal completion paths drain the
// buffer themselves, so this merge is a no-op for them.
func (f *scanFrontier) result() []Candidate {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.mergePendingLocked()

	return f.retained
}

// release marks one job fully processed and wakes peers when the last
// one finishes so they can observe the drained frontier.
func (f *scanFrontier) release() {
	f.mu.Lock()
	f.active--

	if f.active == 0 {
		f.cond.Broadcast()
	}

	f.mu.Unlock()
}

// isStopped reports whether the scan has been sealed.
func (f *scanFrontier) isStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.stopped
}

// visit processes one directory: read it, inspect any matching engine
// file, enqueue its child directories with their own priority. Errors
// (inaccessible directories, permission walls) are tolerated silently —
// spec §11 requires the scan to continue without crashing.
func (f *scanFrontier) visit(
	ctx context.Context,
	job dirJob,
	engineName string,
	opts ScanOptions,
	emit func(Candidate) bool,
) {
	entries, err := os.ReadDir(job.path)
	if err != nil {
		return // inaccessible — ignore without crashing (spec §11)
	}

	// Avoidable-work guard (v1.8.7): once the candidate budget is
	// satisfied, the scan seals at this level's drain, so child
	// directories can never be visited. Skip enqueueing them. The flag
	// is sampled once per visit; a mid-visit flip only means a few
	// harmless never-visited enqueues (bounded by maxQueuedDirs).
	skipEnqueue := f.isCapReached()

	for _, e := range entries {
		if ctx.Err() != nil || f.isStopped() {
			return
		}

		name := e.Name()

		if e.IsDir() {
			// Symlink/reparse-point safety: ReadDir never reports a
			// symlink (or a Windows junction/reparse point) as a plain
			// directory, so only real directories are followed — a
			// link named like the engine falls through to the file
			// branch below, where inspectCandidate stats the TARGET
			// and still rejects directories. No walk can loop.
			if noiseDirs[strings.ToLower(name)] {
				continue
			}

			// Root-level dot-directories are skipped (preserved from
			// the v1.3.6 walk: saves the giant runner tool trees that
			// live directly under the home root without hiding deeper
			// user locations).
			if job.depth == 0 && strings.HasPrefix(name, ".") {
				continue
			}

			if job.depth+1 > opts.MaxDepth {
				continue
			}

			if !skipEnqueue {
				f.enqueue(filepath.Join(job.path, name), job.depth+1, job.class)
			}

			continue
		}

		if !strings.EqualFold(name, engineName) {
			continue
		}

		if cand, ok := inspectCandidate(filepath.Join(job.path, name), 2, "scan"); ok {
			if !emit(*cand) {
				return
			}
		}
	}
}

func mergeCacheAll(cfg *config.Config, cands []Candidate) {
	if len(cands) == 0 {
		return
	}

	existing := LoadCache(cfg)

	byPath := map[string]Candidate{}
	for _, c := range existing {
		byPath[filepath.Clean(c.Path)] = c
	}

	for _, c := range cands {
		byPath[filepath.Clean(c.Path)] = c
	}

	out := make([]Candidate, 0, len(byPath))
	for _, c := range byPath {
		out = append(out, c)
	}

	// v1.8.7 determinism: the persisted cache order feeds the next
	// scan's deferred seed order (same-level seq tiebreak), so it must
	// be a pure function of the cache contents — not of map iteration
	// order. Sort by (tier, path).
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}

		return out[i].Path < out[j].Path
	})

	_ = SaveCache(cfg, out)
}

// ---------------------------------------------------------------------------
// Validation (spec §12) — static evidence first, bounded probe last
// ---------------------------------------------------------------------------

// ValidateCandidate runs the non-destructive validation ladder on one
// candidate and stamps the evidence into it:
//
//  1. static gate: exists / executable bit / recognizable format / arch;
//  2. dependency closure: PE imports resolved beside the binary;
//  3. bounded safe probe (--version) — ONLY after 1+2 pass.
//
// The probe output is recorded as evidence, never trusted as
// configuration.
func ValidateCandidate(cand *Candidate) bool {
	if cand == nil {
		return false
	}

	ident, err := engcheck.StaticValidate(cand.Path)
	if err != nil {
		cand.Validation = "rejected: " + err.Error()

		return false
	}

	cand.SHA256 = ident.SHA256
	cand.Arch = ident.Arch
	cand.Format = ident.Format

	deps, err := engcheck.CheckDependencies(cand.Path)
	if err == nil && len(deps.Missing) > 0 {
		cand.Validation = "rejected: missing runtime DLLs: " + strings.Join(deps.Missing, ", ")

		return false
	}

	// Bounded probe — the binary passed every static check; running its
	// --version is now evidence-gathering, not blind execution.
	ver, probeErr := probeVersion(cand.Path, 10*time.Second)
	if probeErr != nil {
		cand.Validation = "rejected: probe failed: " + probeErr.Error()

		return false
	}

	cand.Tag = probeTag(ver)
	cand.Validated = true
	cand.Validation = "validated: arch " + ident.Arch + ", probe: " + ver

	return true
}

// probeVersion runs --version with a hard timeout and returns the first
// output line. It reports (not swallows) failures.
func probeVersion(path string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := runBounded(ctx, path)
	if err != nil {
		return "", err
	}

	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			line = strings.TrimSpace(l)
			break
		}
	}

	if line == "" {
		return "", errors.New("empty --version output")
	}

	return line, nil
}

// probeTag extracts a build tag from a version line like
// "version: 4818 (abcd1234)" → "b4818".
func probeTag(versionLine string) string {
	v := strings.ToLower(versionLine)

	if i := strings.Index(v, "version:"); i >= 0 {
		fields := strings.Fields(v[i:])

		if len(fields) >= 2 {
			t := strings.TrimPrefix(fields[1], "b")

			if allDigits(t) {
				return "b" + t
			}
		}
	}

	return ""
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
