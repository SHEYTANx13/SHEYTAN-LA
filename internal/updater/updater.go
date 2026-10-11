// Package updater keeps the bundled libraries current on a schedule.
//
// v1.0.0 introduced scheduled engine updates: the llama.cpp inference
// engine is checked against the upstream GitHub release on a configurable
// cadence — daily, weekly, or monthly — and silently upgraded in the
// background when a newer build exists. Everything degrades gracefully
// offline (checks are skipped and retried on the next due tick) and the
// running engine is restarted around the swap so the user never notices
// more than a brief "engine reloading" caption.
//
// v1.0.3 fixes — the updater was systematically broken before:
//   - "latest release" on GitHub can point at a tag with NO prebuilt
//     binaries (e.g. the v0.3.0 milestone tag), so the updater tried to
//     download an asset that does not exist and every update failed.
//     LatestTag now walks the recent release LIST and returns the newest
//     tag that actually ships a prebuilt asset for this platform.
//   - the manual "Check for engine update now" button was gated by the
//     same due-check as the scheduled pass, so clicking it usually did
//     nothing. CheckAndApplyForced always checks.
//   - swapping the engine files while the engine was RUNNING failed on
//     Windows (the exe is locked). The engine is now stopped before the
//     swap and restarted after.
package updater

import (
        "archive/zip"
        "context"
        "encoding/json"
        "encoding/xml"
        "errors"
        "fmt"
        "io"
        "net/http"
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// Schedules supported by the updater.
const (
        ScheduleDaily   = "daily"
        ScheduleWeekly  = "weekly"
        ScheduleMonthly = "monthly"
        ScheduleOff     = "off"
)

// DefaultSchedule is applied when the config value is empty or unknown.
const DefaultSchedule = ScheduleDaily

// DefaultEngineTag is the llama.cpp release the app BUNDLES in its portable
// folder (v1.0.3 ships the engine inside bin/ — no first-run download
// needed). Must match the tag format used by upstream releases.
const DefaultEngineTag = "b10642"

// apiTimeout bounds every network call the updater makes.
const apiTimeout = 20 * time.Second

// Engine is the subset of the llama.cpp manager the updater needs.
type Engine interface {
        IsRunning() bool
        Stop() error
        Restart() error
}

// NormalizeSchedule validates a schedule string, returning the default for
// anything unknown (and "off" passed through untouched).
func NormalizeSchedule(s string) string {
        switch strings.ToLower(strings.TrimSpace(s)) {
        case ScheduleDaily:
                return ScheduleDaily
        case ScheduleWeekly:
                return ScheduleWeekly
        case ScheduleMonthly:
                return ScheduleMonthly
        case ScheduleOff:
                return ScheduleOff
        default:
                return DefaultSchedule
        }
}

// CheckDue reports whether a scheduled update check is due given the last
// check time. "off" never reports due.
func CheckDue(schedule string, last time.Time) bool {
        switch NormalizeSchedule(schedule) {
        case ScheduleOff:
                return false
        case ScheduleDaily:
                return time.Since(last) >= 24*time.Hour
        case ScheduleWeekly:
                return time.Since(last) >= 7*24*time.Hour
        case ScheduleMonthly:
                return time.Since(last) >= 30*24*time.Hour
        }
        return false
}

// stateFile is the shape of installed.json (subset the updater touches).
type stateFile struct {
        AppVersion string               `json:"appVersion"`
        LastRunAt  time.Time            `json:"lastRunAt"`
        Components map[string]component `json:"components"`
}

type component struct {
        Version    string            `json:"version,omitempty"`
        Status     string            `json:"status"`
        ObservedAt time.Time         `json:"observedAt"`
        Meta       map[string]string `json:"meta,omitempty"`
}

// InstalledEngineTag returns the llama.cpp build tag recorded at install or
// last update. Missing/legacy installs report "".
func InstalledEngineTag(cfg *config.Config) string {
        data, err := os.ReadFile(cfg.StatePath())
        if err != nil {
                return ""
        }
        var st stateFile
        if err := json.Unmarshal(data, &st); err != nil {
                return ""
        }
        c, ok := st.Components["llamaServer"]
        if !ok {
                return ""
        }
        if c.Meta != nil {
                if t := c.Meta["engineTag"]; t != "" {
                        return t
                }
        }
        return c.Version
}

// EffectiveInstalledEngineTag returns the llama.cpp build tag the "update
// required" decision points must compare against.
//
// v1.7.4 (P0 #3): the recorded tag (installed.json
// components.llamaServer.meta.engineTag, via InstalledEngineTag) is
// preferred; when it is absent — a legacy install, or a state file whose
// identity was lost — the committed install manifest beside the binary
// (engine-install.json, rewritten at every install commit by
// recordInstallManifest) is the honest fallback, because it describes the
// package that is ACTUALLY on disk. Returns "" when neither source knows
// the tag; callers keep their own DefaultEngineTag fallback.
func EffectiveInstalledEngineTag(cfg *config.Config) string {
        if tag := InstalledEngineTag(cfg); tag != "" {
                return tag
        }
        return ManifestEngineTag(cfg)
}

// ManifestEngineTag returns the tag recorded in the engine install manifest
// beside the managed binary (engine-install.json), or "" when the manifest
// is missing, unreadable, or carries no tag.
func ManifestEngineTag(cfg *config.Config) string {
        m, ok := ReadInstallManifest(cfg)
        if !ok || m.Tag == "" {
                return ""
        }
        return m.Tag
}

// RecordEngineTag persists the engine tag (and install time) into
// installed.json, creating the file when necessary. Failures are logged and
// swallowed — bookkeeping must never break the app.
func RecordEngineTag(cfg *config.Config, tag string) {
        st := stateFile{Components: map[string]component{}}
        if data, err := os.ReadFile(cfg.StatePath()); err == nil {
                _ = json.Unmarshal(data, &st)
        } else if !os.IsNotExist(err) {
                // v1.7.4 (P0 #3, D): a state file that cannot be READ is exactly how
                // the recorded identity silently vanished — surface it.
                logging.Default().Warn("updater", "engine tag record: read %s: %v", cfg.StatePath(), err)
        }
        if st.Components == nil {
                st.Components = map[string]component{}
        }
        st.AppVersion = config.AppVersion
        st.LastRunAt = time.Now().UTC()
        c := st.Components["llamaServer"]
        c.Status = "installed"
        c.ObservedAt = time.Now().UTC()
        if c.Meta == nil {
                c.Meta = map[string]string{}
        }
        c.Meta["engineTag"] = tag
        // v1.7.4 (P0 #3): also record WHERE the committed binary lives — the
        // installer's identity merge needs concrete path evidence to tell an
        // unchanged engine from a genuinely replaced one.
        c.Meta["path"] = EngineBinaryPath(cfg)
        c.Meta["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
        st.Components["llamaServer"] = c
        out, err := json.MarshalIndent(st, "", "  ")
        if err != nil {
                // v1.7.4 (P0 #3, D): never silent — bookkeeping failures must reach
                // the log or the tag loss is undiagnosable.
                logging.Default().Warn("updater", "engine tag record: encode %s: %v", cfg.StatePath(), err)
                return
        }
        if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
                logging.Default().Warn("updater", "engine tag record: create %s: %v", cfg.DataDir, err)
                return
        }
        if err := os.WriteFile(cfg.StatePath(), out, 0o644); err != nil {
                // v1.7.4 (P0 #3, D): the write failure used to be swallowed whole —
                // the tag then never landed in the state file and every boot
                // re-decided "update required" from the bundled default tag.
                logging.Default().Warn("updater", "engine tag record: write %s: %v", cfg.StatePath(), err)
        }
}

// MarkChecked records the last update-check time in the config (persisted by
// the caller).
func MarkChecked(cfg *config.Config) {
        cfg.LastUpdateCheck = time.Now().UTC().Format(time.RFC3339)
}

// ghRelease is the subset of the GitHub release payload we need.
type ghRelease struct {
        TagName string    `json:"tag_name"`
        Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
        Name string `json:"name"`
}

// LatestTag returns the newest llama.cpp release tag that actually ships a
// prebuilt server binary for this OS/arch.
//
// v1.0.3: the previous implementation queried /releases/latest, which can
// point at a milestone tag (v0.3.0) whose release contains ONLY source —
// the download then 404'd and updates never worked. We now page through the
// recent release list and pick the first tag with a matching asset. When
// the API is rate-limited (anonymous GitHub API is capped at 60 req/h per
// IP) we fall back to the releases Atom feed and verify candidate tags by
// HEAD-checking the asset URL.
// latestTagProbe is the swappable release probe behind LatestTag (tests
// inject a deterministic answer; production always resolves from the
// network — the same seam pattern as netcheck.SetProbe).
var latestTagProbe func(ctx context.Context) (string, error)

// SetLatestTagForTest overrides the release-tag probe (tests only).
func SetLatestTagForTest(fn func(ctx context.Context) (string, error)) {
        latestTagProbe = fn
}

// releaseListProbe is the swappable release-LIST source behind the
// v1.6.2 variant-aware resolver (tests inject a deterministic list;
// production always pages the real GitHub API).
var releaseListProbe func(ctx context.Context) ([]ghRelease, error)

// SetReleaseListForTest overrides the release-list source (tests only).
// The injected list must be NEWEST-FIRST, exactly like the API pages.
func SetReleaseListForTest(fn func(ctx context.Context) ([]ghRelease, error)) {
        releaseListProbe = fn
}

func LatestTag(ctx context.Context) (string, error) {
        if latestTagProbe != nil {
                return latestTagProbe(ctx)
        }

        if netcheck.IsOffline() {
                return "", fmt.Errorf("offline — skipping update check")
        }
        if tag, err := latestTagFromAPI(ctx); err == nil && tag != "" {
                return tag, nil
        } else if err != nil && !errors.Is(err, errNoAsset) {
                logging.Default().Warn("updater", "release list via API failed: %v (trying atom feed)", err)
        }
        return latestTagFromAtom(ctx)
}

// errNoAsset marks "API reachable but no release had a matching asset".
var errNoAsset = errors.New("no recent release ships a prebuilt asset for this platform")

// IsNoAssetError reports whether err means upstream GENUINELY ships no
// prebuilt engine asset for this platform (v1.2.2). Distinguishing this
// from a NETWORK failure matters: a blocked/unreachable api.github.com
// must never be reported as "no prebuilt asset exists" — that misled
// engine startup on firewalled machines (the Windows log symptom).
func IsNoAssetError(err error) bool {
        if err == nil {
                return false
        }
        if errors.Is(err, errNoAsset) {
                return true
        }
        // The Atom fallback's no-asset wording (it can only be produced after
        // the feed itself was fetched successfully — a network failure
        // surfaces as the HTTP error instead).
        return strings.Contains(err.Error(), "no recent release ships a prebuilt asset")
}

// latestTagFromAPI pages the GitHub release list (newest first) and returns
// the first tag whose assets include our platform build.
func latestTagFromAPI(ctx context.Context) (string, error) {
        return pageReleasesForAsset(ctx, AssetName)
}

// pageReleasesForAsset pages the GitHub release list (newest first) and
// returns the first tag whose assets include the asset named by
// want(tag) ("" = this platform never has an asset at that tag). It is
// the single release-paging authority shared by the CPU-oriented
// LatestTag and the v1.6.2 variant-aware resolver — no second lister,
// no divergent rules.
func pageReleasesForAsset(ctx context.Context, want func(tag string) string) (string, error) {
        urls := []string{
                "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=30",
                "https://api.github.com/repos/ggerganov/llama.cpp/releases?per_page=30",
        }
        var lastErr error
        for _, u := range urls {
                ctx, cancel := context.WithTimeout(ctx, apiTimeout)
                req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
                if err != nil {
                        cancel()
                        lastErr = err
                        continue
                }
                req.Header.Set("Accept", "application/vnd.github+json")
                resp, err := http.DefaultClient.Do(req)
                if err != nil {
                        cancel()
                        lastErr = err
                        continue
                }
                var releases []ghRelease
                decodeErr := json.NewDecoder(resp.Body).Decode(&releases)
                _ = resp.Body.Close()
                cancel()
                if decodeErr != nil {
                        lastErr = decodeErr
                        continue
                }
                if resp.StatusCode != 200 {
                        lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
                        continue
                }
                if tag := firstReleaseWithAsset(releases, want); tag != "" {
                        return tag, nil
                }
                lastErr = errNoAsset
        }
        if lastErr == nil {
                lastErr = errNoAsset
        }
        return "", lastErr
}

// firstReleaseWithAsset walks a newest-first release list and returns
// the first tag carrying the asset named by want(tag).
func firstReleaseWithAsset(releases []ghRelease, want func(tag string) string) string {
        for _, r := range releases {
                if r.TagName == "" {
                        continue
                }
                name := want(r.TagName)
                if name == "" {
                        continue
                }
                for _, a := range r.Assets {
                        if a.Name == name {
                                return r.TagName
                        }
                }
        }
        return ""
}

var _ = fmt.Sprintf // keep fmt when stubs are trimmed

// AssetInfo is the exported asset shape (name only) used by tests.
type AssetInfo = ghAsset

// GHAssetStub is kept for source compatibility; use AssetInfo.
type GHAssetStub = ghAsset

// ReleaseInfo is the exported shape of one GitHub release (tag + asset
// names) used by FirstWithAsset. Test-facing alias (v1.0.3).
type ReleaseInfo = ghRelease

// GHReleaseStub is kept for source compatibility; use ReleaseInfo.
type GHReleaseStub = ghRelease

// FirstWithAsset is the exported test seam for firstWithAsset: it walks a
// newest-first release list and returns the first tag whose assets include
// the prebuilt asset for this platform.
func FirstWithAsset(releases []ghRelease) string {
        return firstWithAsset(releases)
}

// FirstWithVariantAsset is the v1.6.2 variant-aware export: it walks a
// newest-first release list and returns the first tag whose assets
// include the EXACT variant asset for this platform ("" when no release
// in the list serves it). It is the deterministic core of the
// variant-aware release resolution.
func FirstWithVariantAsset(releases []ghRelease, v AssetVariant) string {
        return firstWithVariantAsset(releases, v)
}

// firstWithAsset walks a newest-first release list and returns the first
// tag carrying the platform asset (v1.0.3).
func firstWithAsset(releases []ghRelease) string {
        return firstWithVariantAsset(releases, VariantCPU)
}

// firstWithVariantAsset walks a newest-first release list and returns
// the first tag carrying the platform asset for the requested BACKEND
// VARIANT — the exact asset-name match against the release payload
// ("a tag has it because the release says it has it", never because a
// CPU-oriented scan named the tag).
func firstWithVariantAsset(releases []ghRelease, v AssetVariant) string {
        return firstReleaseWithAsset(releases, func(tag string) string {
                return AssetNameForVariant(tag, v)
        })
}

// atomFeed is the subset of the GitHub releases Atom feed we need.
type atomFeed struct {
        Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
        Title string `xml:"title"`
}

// latestTagFromAtom parses the public releases Atom feed (no rate limit)
// and HEAD-checks candidate tags until one has a downloadable asset.
func latestTagFromAtom(ctx context.Context) (string, error) {
        ctx, cancel := context.WithTimeout(ctx, apiTimeout)
        defer cancel()
        req, err := http.NewRequestWithContext(ctx, http.MethodGet,
                "https://github.com/ggml-org/llama.cpp/releases.atom", nil)
        if err != nil {
                return "", err
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
                return "", fmt.Errorf("atom feed: HTTP %d", resp.StatusCode)
        }
        var feed atomFeed
        if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
                return "", err
        }
        checked := 0
        for _, e := range feed.Entries {
                tag := strings.TrimSpace(e.Title)
                if tag == "" || !strings.HasPrefix(tag, "b") {
                        continue // skip milestone tags (vX.Y.Z) — they carry no binaries
                }
                if AssetURL(tag) == "" {
                        continue
                }
                checked++
                if checked > 10 {
                        break // bounded effort
                }
                if assetExists(ctx, tag) {
                        return tag, nil
                }
        }
        return "", fmt.Errorf("no recent release ships a prebuilt asset for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// AssetName returns the prebuilt llama.cpp asset name for this OS/arch at
// the given tag, or "" when no prebuilt asset exists for the platform.
// v1.6.1: this is the CPU variant alias — variant-aware callers use
// AssetNameForVariant (the Windows Vulkan package has its own asset).
func AssetName(tag string) string {
        return AssetNameForVariant(tag, VariantCPU)
}

// AssetURL builds the download URL for the prebuilt asset at a tag.
func AssetURL(tag string) string {
        a := AssetName(tag)
        if a == "" {
                return ""
        }
        return "https://github.com/ggml-org/llama.cpp/releases/download/" + tag + "/" + a
}

// assetExists HEAD-checks the prebuilt asset URL for a tag (v1.0.3).
func assetExists(ctx context.Context, tag string) bool {
        return AssetExists(ctx, tag)
}

// AssetExists is the exported test/production seam for assetExists: it
// HEAD-checks whether the prebuilt engine asset for this platform exists
// at the given tag. v1.1.5 repair: the engine download path needs this to
// probe the pinned tag before falling back to a release scan, because
// llama.cpp removed its Linux prebuilt assets upstream (the pinned tag
// now 404s on Linux while the Windows asset still exists).
func AssetExists(ctx context.Context, tag string) bool {
        url := AssetURL(tag)
        if url == "" {
                return false
        }
        checkCtx, cancel := context.WithTimeout(ctx, apiTimeout)
        defer cancel()
        req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
        if err != nil {
                return false
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return false
        }
        _ = resp.Body.Close()
        // GitHub answers 302 → 200 for existing release assets.
        return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// engineBinaryName is the local name of the server binary.
func engineBinaryName() string {
        if runtime.GOOS == "windows" {
                return "llama-server.exe"
        }
        return "llama-server"
}

// UpdateEngine downloads the engine at `tag` and swaps it in. When `eng` is
// non-nil and running, the engine is STOPPED for the swap (Windows locks a
// running exe) and restarted afterwards. Returns the human-readable outcome.
func UpdateEngine(ctx context.Context, cfg *config.Config, eng Engine, tag string) (string, error) {
        return UpdateEngineWithProgress(ctx, cfg, eng, tag, nil)
}

// TransactionalEngine is implemented by the engine lifecycle owner
// (v1.3.6, spec §1/§2): it wraps the whole update in an EXCLUSIVE
// lifecycle operation so Start/Restart/prewarm can never interleave with
// the file swap.
type TransactionalEngine interface {
        Engine
        UpdateEngineNow(ctx context.Context, tag string, onProgress func(downloader.Progress)) (string, error)
}

// UpdateEngineWithProgress is UpdateEngine with a live progress callback
// (v1.2.3 Download Manager) for callers with UI to feed.
//
// v1.3.6: when the engine implements TransactionalEngine — the live
// LlamaServer always does — the ENTIRE update is delegated to the
// engine-owned transaction (lifecycle lock held start-to-finish,
// transactional staged install, verified restart). The legacy
// stop/download/copy/restart choreography below remains only for plain
// Engine implementations and is serialized by the process-wide install
// lock inside InstallStaged.
func UpdateEngineWithProgress(ctx context.Context, cfg *config.Config, eng Engine, tag string, onProgress func(downloader.Progress)) (string, error) {
        if te, ok := eng.(TransactionalEngine); ok && te != nil {
                return te.UpdateEngineNow(ctx, tag, onProgress)
        }

        url := AssetURL(tag)
        if url == "" {
                return "", fmt.Errorf("no prebuilt llama.cpp asset for %s/%s", runtime.GOOS, runtime.GOARCH)
        }
        binDir := EngineBinDir(cfg)
        if err := os.MkdirAll(binDir, 0o755); err != nil {
                return "", err
        }

        wasRunning := eng != nil && eng.IsRunning()

        // v1.0.3: stop the engine BEFORE touching files — on Windows the
        // running llama-server.exe is locked and every copy over it fails
        // (the old updater only restarted after the swap and silently died).
        if wasRunning {
                logging.Default().Info("updater", "stopping engine for update")
                if err := eng.Stop(); err != nil {
                        return "", fmt.Errorf("stop engine before update: %w", err)
                }
                // Give the OS a beat to release file handles and the port.
                time.Sleep(700 * time.Millisecond)
        }

        logging.Default().Info("updater", "downloading engine %s from %s", tag, url)
        if _, err := InstallStaged(ctx, cfg, tag, onProgress); err != nil {
                // Best effort: bring the old engine back up.
                if wasRunning {
                        _ = eng.Restart()
                }
                return "", fmt.Errorf("download engine %s: %w", tag, err)
        }

        if wasRunning {
                logging.Default().Info("updater", "restarting engine with %s", tag)
                if err := eng.Restart(); err != nil {
                        // v1.3.6 (spec §13): an updated engine that does not restart is
                        // an ERROR — never a nil-error success message.
                        return "", fmt.Errorf("engine updated to %s but restart verification failed: %w", tag, err)
                }
        }
        return fmt.Sprintf("engine updated to llama.cpp %s", tag), nil
}

// CheckAndApply performs one scheduled check: when due, online, and a newer
// tag exists, the engine is updated. The caller persists cfg
// (LastUpdateCheck) after the call.
func CheckAndApply(ctx context.Context, cfg *config.Config, eng Engine) (msg string, updated bool, err error) {
        return checkAndApply(ctx, cfg, eng, false)
}

// CheckAndApplyForced performs one check NOW, ignoring the schedule and the
// last-check timestamp (the "Check for engine update now" button,
// v1.0.3 — the button used to hit the due-gate and do nothing).
func CheckAndApplyForced(ctx context.Context, cfg *config.Config, eng Engine) (msg string, updated bool, err error) {
        return checkAndApply(ctx, cfg, eng, true)
}

func checkAndApply(ctx context.Context, cfg *config.Config, eng Engine, force bool) (msg string, updated bool, err error) {
        schedule := NormalizeSchedule(cfg.UpdateSchedule)
        if schedule == ScheduleOff && !force {
                return "updates off", false, nil
        }
        last, _ := time.Parse(time.RFC3339, cfg.LastUpdateCheck)
        if !force && !CheckDue(schedule, last) {
                return "up to date (last check " + humanWhen(last) + ")", false, nil
        }
        MarkChecked(cfg)
        if netcheck.IsOffline() {
                return "offline — update check skipped", false, nil
        }
        latest, err := LatestTag(ctx)
        if err != nil {
                return "check failed: " + err.Error(), false, err
        }
        // v1.7.4 (P0 #3, B): compare against the EFFECTIVE installed tag — the
        // recorded tag, or the committed manifest beside the binary when the
        // state file lost it. The old plain InstalledEngineTag read made every
        // identity-less boot re-download the engine.
        current := EffectiveInstalledEngineTag(cfg)
        if current == "" {
                current = DefaultEngineTag
        }
        if latest == current {
                return fmt.Sprintf("engine is current (%s)", latest), false, nil
        }
        msg, err = UpdateEngine(ctx, cfg, eng, latest)
        if err != nil {
                // v1.3.0: an error must always reach the log WITH its message —
                // UpdateEngine's download failure path returns ("", err) and
                // the scheduled loop then logged a bare "WARN [updater]".
                if strings.TrimSpace(msg) == "" {
                        msg = "engine update failed: " + err.Error()
                }
                return msg, false, err
        }
        logging.Default().Info("updater", "%s", msg)
        return msg, true, nil
}

// RunScheduled is the long-lived background loop: an immediate pass at boot
// (if due) then a re-evaluation every 6 hours so daily/weekly/monthly
// cadences fire even in long-running sessions. `notify` (optional) receives
// human-readable status lines; `save` persists the config.
//
// v1.1.4: takes the live config Source. CheckAndApply mutates cfg
// (LastUpdateCheck) in place, so each pass runs on a PRIVATE copy that is
// published back through the source — a published immutable value is never
// mutated. Wired by api.Server.EnsureSetup (previously zero callers).
//
// v1.2.0 (deterministic shutdown): returns a done channel closed when the
// loop has observed ctx cancellation AND any in-flight pass (including a
// download/extract) has finished. The owner MUST cancel the context and
// may wait on the channel — that is the whole ownership contract that
// previously leaked an active updater goroutine past Server.Close and
// raced test temp-directory cleanup with .update-stage writes.
func RunScheduled(ctx context.Context, src *config.Source, eng Engine, notify func(string), save func()) <-chan struct{} {
        return RunScheduledAfter(ctx, src, eng, notify, save, nil)
}

// RunScheduledAfter is RunScheduled with a startup gate (v1.6.0, P0):
// the loop's FIRST pass waits until `gateDone` closes (or the context
// dies) before it may touch the engine. This is the explicit
// synchronization that makes the v1.5.1 startup race impossible — the
// scheduled updater can never stop an engine that the startup prewarm is
// concurrently trying to boot, because it cannot even look until the
// startup maintenance gate has completed the maintenance decision and
// released both the prewarm and this loop together. A nil channel
// preserves the exact v1.2.0 semantics for existing callers/tests.
func RunScheduledAfter(ctx context.Context, src *config.Source, eng Engine, notify func(string), save func(), gateDone <-chan struct{}) <-chan struct{} {
        if notify == nil {
                notify = func(string) {}
        }
        if save == nil {
                save = func() {}
        }

        done := make(chan struct{})

        go func() {
                defer close(done)

                pass := func() {
                        cur := src.Load()
                        mutable := *cur

                        msg, updated, err := CheckAndApply(ctx, &mutable, eng)

                        // v1.5.0 (the model-first selection race): apply ONLY the
                        // updater's own mutations to the CURRENT config. The previous
                        // wholesale src.Store(&mutable) re-published a snapshot taken
                        // BEFORE the network check — which can stall for seconds on an
                        // offline machine — so any concurrent config write (a model
                        // selection seconds after boot, exactly the model-first
                        // window; equally any settings save) was silently reverted in
                        // memory while its on-disk file kept the new value: the live
                        // source and config.json disagreed until the next write. The
                        // updater owns exactly ONE config field (LastUpdateCheck —
                        // engine tags live in the separate state file), and that is
                        // all it may publish.
                        src.Update(func(c *config.Config) {
                                c.LastUpdateCheck = mutable.LastUpdateCheck
                        })

                        if err != nil {
                                // v1.3.0: never emit a blank WARN. checkAndApply occasionally
                                // returns ("", err) (UpdateEngine's error path), which used to
                                // produce the empty "WARN [updater]" lines in v1.2.9 logs.
                                if strings.TrimSpace(msg) == "" {
                                        msg = err.Error()
                                }
                                logging.Default().Warn("updater", "%s", msg)
                        } else if updated {
                                logging.Default().Info("updater", "%s", msg)
                        }
                        notify(msg)
                        save()
                }

                // v1.6.0 P0: hold the first pass behind the startup maintenance
                // gate. Cancellation still wins immediately.
                if gateDone != nil {
                        select {
                        case <-gateDone:
                        case <-ctx.Done():
                                return
                        }
                }

                if ctx.Err() == nil {
                        pass()
                }

                t := time.NewTicker(6 * time.Hour)
                defer t.Stop()

                for {
                        select {
                        case <-ctx.Done():
                                return
                        case <-t.C:
                                pass()
                        }
                }
        }()

        return done
}

// downloadEngine fetches the release zip for `tag` and extracts only the
// server binary (plus adjacent runtime DLLs) into binDir, staging first so
// a bad download can never destroy a working engine.
//
// v1.1.4: the transfer is capped (engineUpdateCapBytes) — the previous
// io.Copy accepted an arbitrarily large body.
// v1.2.3: the transfer runs through the reusable Download Manager:
// HTTPS-only (loopback test doubles excepted), streamed to a .part file,
// resume-capable via HTTP Range, retried with bounded backoff, and the
// archive is verified before extraction. onProgress (when non-nil)
// receives measured progress.
const engineUpdateCapBytes = 2 << 30 // 2 GiB; Vulkan bundles are ~300 MB

// findBinary locates the server executable inside an extracted tree.
func findBinary(root string) string {
        var hit string
        _ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
                if err != nil || hit != "" || info == nil {
                        return nil
                }
                if info.IsDir() {
                        return nil
                }
                n := strings.ToLower(info.Name())
                if n == "llama-server.exe" || n == "llama-server" {
                        hit = p
                }
                return nil
        })
        return hit
}

// extractZip extracts a zip archive into dir.
//
// v1.1.4: every member path is sanitized (no absolute paths, volume
// names, or .. traversal) and per-entry sizes are bounded. The previous
// version joined filepath.Join(dir, f.Name) with NO validation — a
// hostile archive could write outside the staging directory.
const engineUpdateEntryCapBytes = 2 << 30 // single-member cap

func extractZip(zipPath, dir string) error {
        zr, err := zip.OpenReader(zipPath)
        if err != nil {
                return err
        }
        defer zr.Close()
        if err := os.MkdirAll(dir, 0o755); err != nil {
                return err
        }
        for _, f := range zr.File {
                out, err := safeZipPath(dir, f.Name)
                if err != nil {
                        return err
                }
                if f.FileInfo().IsDir() {
                        if err := os.MkdirAll(out, 0o755); err != nil {
                                return err
                        }
                        continue
                }
                if f.UncompressedSize64 > engineUpdateEntryCapBytes {
                        return fmt.Errorf("zip member %q exceeds %d bytes", f.Name, engineUpdateEntryCapBytes)
                }
                if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
                        return err
                }
                rc, err := f.Open()
                if err != nil {
                        return err
                }
                // v1.3.6: honor the archive's stored Unix mode — release zips
                // mark the server binary executable, and the validated candidate
                // must be executable BEFORE it is swapped in (the pre-swap
                // static gate rejects non-executable files on Unix).
                fo, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
                if err != nil {
                        _ = rc.Close()
                        return err
                }
                _, copyErr := io.Copy(fo, io.LimitReader(rc, engineUpdateEntryCapBytes+1))
                closeErr := fo.Close()
                rcErr := rc.Close()
                if copyErr != nil {
                        return copyErr
                }
                if closeErr != nil {
                        return closeErr
                }
                if rcErr != nil {
                        return rcErr
                }
        }
        return nil
}

// safeZipPath validates one archive member and returns the absolute
// extraction target.
//
// v1.2.0: THE single authoritative safe archive-path validator —
// internal/llm delegates here so the engine updater and the app updater
// can never disagree about what an extractable member looks like.
//
// The validator is separator-canonical: the member name is first viewed
// with every backslash as a separator, so the checks hold regardless of
// which OS the archive was created on. A member is accepted only when it
// is a RELATIVE, CLEAN path that stays inside the extraction root.
// Rejected:
//   - POSIX-absolute members          "/absolute/path"
//   - Windows-rooted members          "\absolute\path" (→ "/absolute/path")
//   - drive-letter members            "C:\x" / "C:/x"
//   - UNC members                     "\\server\share\path"
//   - any ".." traversal              "../x", "..\x", "foo/../../x",
//     mixed separators "..\/x"
//   - empty, "." or NUL-bearing names
//
// The Windows-specific trap this fixes: "\absolute\path" is NOT absolute
// under filepath.IsAbs on Windows (it is "rooted" without a volume), so
// the previous IsAbs/VolumeName checks alone let it through.
func safeZipPath(dir, name string) (string, error) {
        name = strings.TrimSpace(name)

        if name == "" {
                return "", fmt.Errorf("zip contains an empty path")
        }

        if strings.ContainsRune(name, 0) {
                return "", fmt.Errorf("zip path %q contains a NUL byte", name)
        }

        // Separator-canonical view: backslash becomes a separator on every
        // host, so rooted/UNC/traversal shapes are visible everywhere.
        unified := strings.ReplaceAll(name, "\\", "/")

        // POSIX-absolute, Windows-rooted ("\x") and UNC ("\\srv/share")
        // members all begin with a separator once unified.
        if strings.HasPrefix(unified, "/") {
                return "", fmt.Errorf("zip path %q is absolute", name)
        }

        // Drive-letter volumes: "C:/x" and "C:\x".
        if len(unified) >= 2 && unified[1] == ':' {
                return "", fmt.Errorf("zip path %q contains a volume", name)
        }

        if unified == "." || unified == "" {
                return "", fmt.Errorf("zip contains invalid path %q", name)
        }

        clean := filepath.Clean(filepath.FromSlash(unified))

        if clean == "." || clean == string(filepath.Separator) || clean == "" {
                return "", fmt.Errorf("zip contains invalid path %q", name)
        }

        // Belt and braces for host-native spellings that survive the
        // unified view (device namespaces, reserved DOS forms).
        if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
                return "", fmt.Errorf("zip path %q is absolute or contains a volume", name)
        }

        base, err := filepath.Abs(dir)
        if err != nil {
                return "", err
        }

        base = filepath.Clean(base)

        target := filepath.Join(base, clean)
        rel, err := filepath.Rel(base, target)
        if err != nil {
                return "", err
        }

        if rel == "" || rel == ".." ||
                strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
                return "", fmt.Errorf("zip path %q escapes extraction directory", name)
        }

        return target, nil
}

// SafeArchivePath is the exported view of the authoritative validator for
// sibling packages (internal/llm engine downloads share the contract).
func SafeArchivePath(dir, name string) (string, error) {
        return safeZipPath(dir, name)
}

func humanWhen(t time.Time) string {
        if t.IsZero() {
                return "never"
        }
        d := time.Since(t)
        switch {
        case d < time.Hour:
                return fmt.Sprintf("%d min ago", int(d.Minutes()))
        case d < 24*time.Hour:
                return fmt.Sprintf("%d h ago", int(d.Hours()))
        default:
                return fmt.Sprintf("%d d ago", int(d.Hours()/24))
        }
}
