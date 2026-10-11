package updater

// v1.3.6 (spec §2/§14/§20): THE single engine provisioning authority.
//
// Every path that puts a llama.cpp engine into the managed bin directory
// — first-run download, scheduled update, manual update, repair — goes
// through InstallStaged. There is no second download implementation and
// no in-place overwrite of a live engine directory any more.
//
// Transaction (each step all-or-nothing for the ACTIVE installation):
//
//      DISCOVER url → DOWNLOAD to staging → EXTRACT to .update-stage →
//      VALIDATE CANDIDATE (static: exists/arch/dependency closure) →
//      SWAP (old dir renamed aside, staging renamed in, non-engine files
//            merged back) → VERIFY INSTALLED BINARY (identity == staged) →
//      COMMIT (record tag + sha) → CLEANUP (staging archives, old dir)
//
// On any failure BEFORE the commit the previous engine directory is
// restored byte-for-byte; last-known-good is never destroyed.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/englease"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// installMu serializes every provisioning operation process-wide: two
// updaters (scheduled + manual) can never mutate the bin directory
// concurrently, and neither can a boot-path provisioning step racing a
// scheduled pass (spec §1 Update+Update, scheduled+manual).
var installMu = make(chan struct{}, 1)

// WithInstallLock runs fn while holding the process-wide install lock.
// The engine-owned transaction (LlamaServer.UpdateEngineNow) takes the
// lifecycle lock FIRST and the install lock SECOND — the same order as
// every other provisioning caller, so no lock-order inversion exists.
func WithInstallLock(fn func()) {
	installMu <- struct{}{}
	defer func() { <-installMu }()

	fn()
}

// InstallResult reports what one install committed.
type InstallResult struct {
	Outcome string `json:"outcome"`
	Tag     string `json:"tag"`
	BinPath string `json:"binPath"`
	SHA256  string `json:"sha256"`
}

// leaseDir resolves the cross-process lease directory for a config.
func leaseDir(cfg *config.Config) string {
	return filepath.Join(cfg.DataDir, "run")
}

// refuseIfForeignEngineOwner reports — without touching anything — when
// a live FOREIGN process owns this engine installation (spec §16: a CLI
// updater must never mutate the bin directory under a live app, and two
// app instances must never both manage one installation).
func refuseIfForeignEngineOwner(cfg *config.Config) error {
	if holder, ok := englease.ForeignHolder(leaseDir(cfg), englease.OwnerLease); ok {
		return fmt.Errorf(
			"engine installation is owned by a live SHEYTAN process (pid %d, started %s) — stop that instance's engine or close it before provisioning; it will not be killed automatically",
			holder.PID, holder.AcquiredAt,
		)
	}

	return nil
}

// ResolveDownloadURL resolves (url, tag) for a fresh engine download:
// the pinned tag when its asset exists on this platform, else the newest
// release that carries this platform's asset (bounded scan), else an
// honest error. Moved here from llm.llamaDownloadURL (v1.3.6) so the
// provisioning authority owns source resolution too.
func ResolveDownloadURL(ctx context.Context) (string, string, error) {
	if url := AssetURL(DefaultEngineTag); url != "" {
		probeCtx, probeCancel := context.WithTimeout(ctx, 30*time.Second)

		if AssetExists(probeCtx, DefaultEngineTag) {
			probeCancel()
			return url, DefaultEngineTag, nil
		}

		probeCancel()
	}

	scanCtx, scanCancel := context.WithTimeout(ctx, 60*time.Second)
	tag, tagErr := LatestTag(scanCtx)
	scanCancel()

	if tagErr == nil && tag != "" {
		if url := AssetURL(tag); url != "" {
			return url, tag, nil
		}

		return "", "", fmt.Errorf(
			"no prebuilt llama.cpp server asset exists for %s/%s — this platform/architecture is not served by upstream releases; build llama-server from source and set llamaBinPath, or select the native engine (engineBackend \"native\")",
			runtime.GOOS, runtime.GOARCH,
		)
	}

	if tagErr != nil {
		return "", "", fmt.Errorf(
			"could not resolve a downloadable engine (network check failed: %v) — connect to the internet once so the engine can be downloaded, or place a prebuilt llama-server(.exe) into the managed bin directory",
			tagErr,
		)
	}

	return "", "", fmt.Errorf("no downloadable engine release found")
}

// EngineBinDir resolves the managed bin directory for engine files
// (single authority for the derivation — spec §40).
func EngineBinDir(cfg *config.Config) string {
	if cfg.LlamaBinPath != "" {
		return filepath.Dir(cfg.LlamaBinPath)
	}

	return filepath.Join(cfg.DataDir, "bin")
}

// EngineBinaryPath resolves the full path of the engine binary the next
// start would launch (v1.6.0: the startup maintenance gate validates
// exactly this path). Mirrors llm.expectedEngineBinPath without
// duplicating it as a second provisioning authority.
func EngineBinaryPath(cfg *config.Config) string {
	if cfg.LlamaBinPath != "" {
		return cfg.LlamaBinPath
	}

	return filepath.Join(cfg.DataDir, "bin", engineBinaryName())
}

// installManifest records the committed package identity beside the
// binary (spec §2: verify executable identity; spec §34: real recorded
// tag + hash, not bookkeeping guesses). v1.6.1: Variant records the
// backend family the package carries ("cpu" | "vulkan") — a v1.6.0-era
// manifest without the field reads back as "cpu".
type installManifest struct {
	Tag         string       `json:"tag"`
	SHA256      string       `json:"sha256"`
	Size        int64        `json:"size"`
	InstalledAt string       `json:"installedAt"`
	Source      string       `json:"source"`
	Variant     AssetVariant `json:"variant,omitempty"`
}

const installManifestName = "engine-install.json"

// v1.8.4 (P0-B ENGINE IDENTITY): the staged-identity marker. Written the
// moment a validated candidate is swapped into the managed bin directory,
// removed at Commit (the install manifest becomes the authority) and at
// Rollback (the previous package is restored). While present it is the
// ONLY truthful identity of the binary at EngineBinaryPath: the recorded
// engine tag and the install manifest still describe the PREVIOUS build
// for the whole deferred-commit verification window.
const stagedIdentityName = "engine-stage-pending.json"

// stagedManifest is the marker's shape — the identity of the executable
// that is actually on disk during the verification window.
type stagedManifest struct {
	Tag      string `json:"tag"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	StagedAt string `json:"stagedAt"`
}

func writeStagedIdentityMarker(binDir string, m stagedManifest) error {
	m.StagedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(binDir, stagedIdentityName)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// StagedEngineIdentity reports the identity of the staged candidate while
// a deferred install is inside its verification window (ok=true). When no
// staged identity is pending — every settled state — ok=false and the
// committed install manifest / recorded tag remain the authorities. This
// accessor creates NO second identity store: it reads the window-scoped
// marker the installer itself writes and clears.
func StagedEngineIdentity(cfg *config.Config) (tag, sha256 string, ok bool) {
	data, err := os.ReadFile(filepath.Join(EngineBinDir(cfg), stagedIdentityName))
	if err != nil {
		return "", "", false
	}

	var m stagedManifest
	if err := json.Unmarshal(data, &m); err != nil || m.Tag == "" {
		return "", "", false
	}

	return m.Tag, m.SHA256, true
}

// InstallStaged downloads, validates and atomically installs the engine
// release `tag` into the managed bin directory, committing immediately.
// Callers that must verify the engine STARTS before the transaction is
// committed use InstallStagedDeferred + Commit/Rollback instead (spec
// §7: START → HEALTH → COMMIT). The caller must already own the engine
// lifecycle; file safety is guaranteed by the process-wide install lock
// and the cross-process install lease.
func InstallStaged(
	ctx context.Context,
	cfg *config.Config,
	tag string,
	onProgress func(downloader.Progress),
) (InstallResult, error) {
	staged, err := InstallStagedDeferred(ctx, cfg, tag, onProgress)
	if err != nil {
		return InstallResult{}, err
	}

	staged.Commit()

	return staged.Result(), nil
}

// InstallStagedFromArchive runs the validate→swap→verify→commit
// transaction from an ALREADY DOWNLOADED archive. It is the testable
// seam of InstallStaged (deterministic CI without a network) and the
// recovery path for resuming from a staged archive.
func InstallStagedFromArchive(
	cfg *config.Config,
	tag string,
	archivePath string,
) (InstallResult, error) {
	staged, err := InstallStagedFromArchiveDeferred(cfg, tag, archivePath)
	if err != nil {
		return InstallResult{}, err
	}

	staged.Commit()

	return staged.Result(), nil
}

// InstallStagedDeferred downloads, validates and atomically SWAPS IN
// the engine release `tag`, but does NOT commit: the caller decides —
// Commit() after the engine is verified to start, Rollback() to restore
// the previous package byte-for-byte (spec §13/§14: a startup
// verification failure must restore last-known-good and return an
// error, never leave an unverified engine recorded as success).
func InstallStagedDeferred(
	ctx context.Context,
	cfg *config.Config,
	tag string,
	onProgress func(downloader.Progress),
) (*StagedInstall, error) {
	return InstallStagedDeferredWithVariant(ctx, cfg, tag, VariantCPU, onProgress)
}

// InstallStagedDeferredWithVariant is InstallStagedDeferred for a
// specific BACKEND VARIANT (v1.6.1: the Windows Vulkan engine package).
// Same transactional choreography, same lease/install-lock discipline —
// only the upstream asset differs, and the manifest records which
// variant was installed.
func InstallStagedDeferredWithVariant(
	ctx context.Context,
	cfg *config.Config,
	tag string,
	variant AssetVariant,
	onProgress func(downloader.Progress),
) (*StagedInstall, error) {
	installMu <- struct{}{}
	defer func() { <-installMu }()

	if err := refuseIfForeignEngineOwner(cfg); err != nil {
		return nil, err
	}

	release, err := englease.Acquire(leaseDir(cfg), englease.InstallLease, "engine-install")
	if err != nil {
		return nil, fmt.Errorf("engine install lease: %w", err)
	}

	defer release.Release()

	url := AssetURLForVariant(tag, variant)
	if url == "" {
		return nil, fmt.Errorf(
			"no prebuilt llama.cpp %s engine asset for %s/%s — the %s backend variant is not served by upstream releases for this platform",
			variant, runtime.GOOS, runtime.GOARCH, variant)
	}

	binDir := EngineBinDir(cfg)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, err
	}

	// 1) DISCOVER + DOWNLOAD to staging (never the live directory).
	logging.Default().Info("updater", "downloading engine %s (%s backend) from %s", tag, variant, url)

	archive, err := downloadEngineArchiveStaged(ctx, url, tag, binDir, onProgress)
	if err != nil {
		return nil, fmt.Errorf("download engine %s: %w", tag, err)
	}

	defer os.Remove(archive)

	return stageFromArchive(cfg, tag, archive, url, variant)
}

// InstallStagedFromArchiveDeferred is the archive-seam variant of
// InstallStagedDeferred (deterministic tests without a network).
func InstallStagedFromArchiveDeferred(
	cfg *config.Config,
	tag string,
	archivePath string,
) (*StagedInstall, error) {
	return InstallStagedFromArchiveDeferredWithVariant(cfg, tag, VariantCPU, archivePath)
}

// InstallStagedFromArchiveDeferredWithVariant is the variant-aware
// archive seam (deterministic tests without a network).
func InstallStagedFromArchiveDeferredWithVariant(
	cfg *config.Config,
	tag string,
	variant AssetVariant,
	archivePath string,
) (*StagedInstall, error) {
	installMu <- struct{}{}
	defer func() { <-installMu }()

	if err := refuseIfForeignEngineOwner(cfg); err != nil {
		return nil, err
	}

	release, err := englease.Acquire(leaseDir(cfg), englease.InstallLease, "engine-install")
	if err != nil {
		return nil, fmt.Errorf("engine install lease: %w", err)
	}

	defer release.Release()

	return stageFromArchive(cfg, tag, archivePath, "archive://"+filepath.Base(archivePath), variant)
}

// DownloadEngineArchiveStaged is the NON-DESTRUCTIVE first half of the
// staged install (v1.9.6): resolve the variant asset URL for the tag and
// download the verified archive into the engine download staging area.
// It performs NO swap, holds NO install lease, and never touches the
// live bin tree — the caller owns the destructive continuation through
// InstallStagedFromArchiveDeferredWithVariant after ITS run-coordination
// boundary. Splitting the phases lets the variant transaction keep the
// whole network window (which can run for minutes) free of the
// run-admission claim: runs stay servable while the archive transfers,
// and the claim covers only the actually-destructive window
// (stop → swap → start → verify → commit/rollback). The download is
// bounded, verified and resumable exactly as the bundled installer's
// own first half. The caller owns the returned archive's cleanup.
func DownloadEngineArchiveStaged(
	ctx context.Context,
	cfg *config.Config,
	tag string,
	variant AssetVariant,
	onProgress func(downloader.Progress),
) (string, error) {
	url := AssetURLForVariant(tag, variant)
	if url == "" {
		return "", fmt.Errorf(
			"no prebuilt llama.cpp %s engine asset for %s/%s — the %s backend variant is not served by upstream releases for this platform",
			variant, runtime.GOOS, runtime.GOARCH, variant)
	}

	binDir := EngineBinDir(cfg)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}

	return downloadEngineArchiveStaged(ctx, url, tag, binDir, onProgress)
}

// StagedInstall is a validated engine package that has been atomically
// swapped into the managed bin directory but NOT yet committed. Until
// Commit() runs, the previous package remains recoverable in
// <binDir>.update-old (spec §7: COMMIT and CLEANUP happen only after
// the installed binary is started and verified).
type StagedInstall struct {
	cfg      *config.Config
	tag      string
	url      string
	binDir   string
	oldDir   string
	variant  AssetVariant
	result   InstallResult
	manifest installManifest
}

// Tag reports the build tag this staged install carries (v1.8.2 seam:
// the commit-path log names the build that became the active engine).
// Empty for a nil staged install.
func (st *StagedInstall) Tag() string {
	if st == nil {
		return ""
	}

	return st.tag
}

// Result returns the install identity of the staged package.
func (st *StagedInstall) Result() InstallResult {
	if st == nil {
		return InstallResult{}
	}

	return st.result
}

// Commit records the install manifest + engine tag and removes the
// previous package. Idempotent; safe to call once the engine has been
// verified running (or when no previous package needs preserving).
func (st *StagedInstall) Commit() {
	if st == nil {
		return
	}

	// 6) COMMIT — record the REAL identity in both manifests.
	recordInstallManifest(st.binDir, st.manifest)

	RecordEngineTag(st.cfg, st.tag)

	// 7) CLEANUP — obsolete staging + previous package (spec §20: keep
	// only the active validated package; last-known-good protection is
	// the ROLLBACK path, not a permanent second copy).
	_ = os.RemoveAll(st.oldDir)
	_ = os.RemoveAll(filepath.Join(st.binDir, ".update-stage"))
	_ = os.Remove(filepath.Join(st.binDir, ".engine-download"))

	// v1.8.4: the committed manifest is now the identity authority —
	// the window-scoped staged marker is obsolete.
	_ = os.Remove(filepath.Join(st.binDir, stagedIdentityName))

	logging.Default().Info("updater",
		"engine %s committed (sha256 %.12s…)", st.tag, st.result.SHA256)

	st.oldDir = ""
}

// engineDirIsManaged reports whether dir is a location SHEYTAN owns
// (inside the data root). The transactional engine install swaps the
// WHOLE bin directory aside; doing that to a directory SHEYTAN does not
// own would permanently delete every non-companion file in it — e.g. a
// user-set llamaBinPath pointing at a shared tools directory or a
// hand-built llama.cpp tree (v1.3.7 guard).
func engineDirIsManaged(cfg *config.Config, dir string) bool {
	dataDir := filepath.Clean(cfg.DataDir)
	dir = filepath.Clean(dir)

	if dir == dataDir {
		return true
	}

	return strings.HasPrefix(dir, dataDir+string(os.PathSeparator))
}

// refuseUnmanagedEngineDir is the swap gate: the transactional
// choreography (rename the whole bin dir aside, replace it, delete the
// old one on commit) is only safe in a SHEYTAN-owned directory. A custom
// llamaBinPath outside the data root keeps working as an engine — it is
// simply never auto-swapped (update it manually, or clear llamaBinPath
// to return to the managed location).
func refuseUnmanagedEngineDir(cfg *config.Config, binDir string) error {
	if engineDirIsManaged(cfg, binDir) {
		return nil
	}

	return fmt.Errorf(
		"refusing to swap engine directory %s: it is outside the SHEYTAN data root %s — "+
			"a custom llamaBinPath location is user-managed and an automatic package swap could destroy files SHEYTAN does not own. "+
			"Clear llamaBinPath to use the managed location, or update the custom engine manually",
		binDir, cfg.DataDir,
	)
}

// stageFromArchive is the shared transaction body up to (and including)
// the byte-identity verification of the swapped-in package; COMMIT and
// CLEANUP are the caller's decision via StagedInstall.Commit/Rollback
// (spec §7). Caller holds installMu and the install lease.
func stageFromArchive(cfg *config.Config, tag, archive, url string, variant AssetVariant) (*StagedInstall, error) {
	binDir := EngineBinDir(cfg)

	// v1.3.7: the directory swap only ever touches SHEYTAN-owned
	// locations (see refuseUnmanagedEngineDir).
	if err := refuseUnmanagedEngineDir(cfg, binDir); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, err
	}

	// 2) EXTRACT to the staging directory.
	stageDir := filepath.Join(binDir, ".update-stage")
	_ = os.RemoveAll(stageDir)

	if err := extractArchiveBySuffix(archive, url, stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("extract engine %s: %w", tag, err)
	}

	// 3) VALIDATE CANDIDATE — static gate BEFORE anything is swapped.
	found := findBinary(stageDir)
	if found == "" {
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("release archive for %s contained no server binary", tag)
	}

	ident, verr := engcheck.StaticValidate(found)
	if verr != nil {
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("candidate validation failed for %s: %w", tag, verr)
	}

	deps, derr := engcheck.CheckDependencies(found)
	if derr == nil && len(deps.Missing) > 0 {
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf(
			"candidate for %s is missing runtime DLLs beside the binary: %s — refusing to install an incomplete package",
			tag, strings.Join(deps.Missing, ", "),
		)
	}

	// 4) SWAP — atomic directory rename choreography:
	//     binDir → binDir.old        (previous engine, preserved)
	//     old/.update-stage → binDir (validated candidate becomes active)
	//
	// .update-stage lives INSIDE binDir, so it travels with the rename
	// and is renamed back out in one move. Same volume → atomic.
	oldDir := binDir + ".update-old"
	_ = os.RemoveAll(oldDir)

	if err := os.Rename(binDir, oldDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("stage engine swap: %w", err)
	}

	restore := func() {
		// Rollback: put the previous engine directory back.
		_ = os.RemoveAll(binDir)
		_ = os.Rename(oldDir, binDir)
	}

	if err := os.Rename(filepath.Join(oldDir, ".update-stage"), binDir); err != nil {
		restore()
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("activate staged engine: %w", err)
	}

	// Merge-back: ONLY allowlisted non-engine companions (native host,
	// license/metadata files) survive the package swap. Stale DLLs and
	// foreign binaries can never re-enter the new package (spec §12).
	logDroppedCompanions(mergeCompanionFiles(oldDir, binDir))

	// 5) VERIFY INSTALLED BINARY — byte identity must match the staged,
	// validated candidate.
	installedPath := filepath.Join(binDir, filepath.Base(found))
	_ = os.Chmod(installedPath, 0o755)

	installedIdent, ierr := engcheck.IdentityOf(installedPath)
	if ierr != nil || installedIdent.SHA256 != ident.SHA256 {
		restore()
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("installed binary identity mismatch for %s (want %s…)", tag, ident.SHA256[:12])
	}

	staged := &StagedInstall{
		cfg:     cfg,
		tag:     tag,
		url:     url,
		binDir:  binDir,
		oldDir:  oldDir,
		variant: variant,
		result: InstallResult{
			BinPath: installedPath,
			SHA256:  installedIdent.SHA256,
			Tag:     tag,
			Outcome: fmt.Sprintf("engine installed: llama.cpp %s (%s backend)", tag, variant),
		},
		manifest: installManifest{
			Tag:         tag,
			SHA256:      installedIdent.SHA256,
			Size:        installedIdent.Size,
			InstalledAt: time.Now().UTC().Format(time.RFC3339),
			Source:      url,
			Variant:     variant,
		},
	}

	// v1.8.4 (P0-B ENGINE IDENTITY): record the STAGED identity beside
	// the binary for the whole deferred-commit verification window.
	// From this moment the binary at EngineBinaryPath IS the staged
	// candidate (byte-verified above), but the RECORDED tag
	// (installed.json) and the install manifest still describe the
	// previous build until Commit runs. Without this marker every boot
	// path inside the window reported (and caps-cached against) the OLD
	// build — the observed "b11310 staged + SHA verified → boot probe
	// reports b11273 → b11310 committed" identity contradiction. The
	// marker is the window-scoped authority for what is ACTUALLY on
	// disk; Commit/Rollback remove it.
	if werr := writeStagedIdentityMarker(binDir, stagedManifest{
		Tag:    tag,
		SHA256: installedIdent.SHA256,
		Size:   installedIdent.Size,
	}); werr != nil {
		restore()
		_ = os.RemoveAll(stageDir)
		return nil, fmt.Errorf("record staged identity for %s: %w", tag, werr)
	}

	logging.Default().Info("updater",
		"engine %s staged and identity-verified (sha256 %.12s…) — awaiting startup verification before commit",
		tag, installedIdent.SHA256)

	return staged, nil
}

// Rollback restores the previous package byte-for-byte and discards the
// staged one. Used when startup verification failed after the swap
// (spec §14: the old package remains the active, usable engine).
func (st *StagedInstall) Rollback() error {
	if st == nil || st.oldDir == "" {
		return nil
	}

	if _, err := os.Stat(st.oldDir); err != nil {
		return fmt.Errorf("rollback: previous engine package is gone: %w", err)
	}

	_ = os.RemoveAll(st.binDir)

	if err := os.Rename(st.oldDir, st.binDir); err != nil {
		return fmt.Errorf("rollback: restore previous engine package: %w", err)
	}

	// v1.8.4: the staged candidate is gone with the bin directory —
	// its window-scoped identity marker must be gone with it (the
	// restored package's manifest is the authority again). Defensive
	// remove: the marker lived inside binDir, which was just replaced,
	// so this is normally a no-op that also covers any caller-invoked
	// rollback on an already-restored tree.
	_ = os.Remove(filepath.Join(st.binDir, stagedIdentityName))

	// v1.8.1 IDENTITY REPAIR: the previous package's manifest is back —
	// make the RECORDED tag agree with it again. During startup
	// verification (between the swap and this rollback) ensureBinary may
	// have stamped the BUNDLED DEFAULT tag as the last-resort identity of
	// a tagless, manifest-less binary; with the old package restored,
	// that transient stamp would claim the WRONG engine to every plain
	// InstalledEngineTag reader (the UI's engine tag, the next "update
	// required" comparison). EffectiveInstalledEngineTag is state-FIRST,
	// so a wrong recorded tag outranks the honest restored manifest —
	// re-record from the manifest that now describes the serving binary.
	// Conservative: when the restored manifest carries no tag either,
	// the state is left untouched (nothing better is known).
	if tag := ManifestEngineTag(st.cfg); tag != "" {
		RecordEngineTag(st.cfg, tag)
	}

	logging.Default().Warn("updater",
		"engine %s rolled back — previous package restored as active", st.tag)

	st.oldDir = ""

	return nil
}

// ImportCandidate installs an EXTERNALLY discovered engine package
// (System Engine Discovery, spec §10–§13) into the managed directory:
// the candidate's VALIDATED CLOSURE (entry binary + its runtime DLL
// set + package metadata) is copied into staging, validated, and
// swapped in with the same transactional choreography as a download.
// Unrelated neighboring binaries, stale DLLs from OTHER packages and
// arbitrary files are never imported (spec §11/§12).
//
// Commit is immediate: import only runs when the managed binary is
// missing or unusable (there is no verified last-known-good package to
// protect), and byte-identical files already present in the managed dir
// are not copied again (dedupe by SHA-256, spec §13/§20).
func ImportCandidate(cfg *config.Config, candidateDir, binaryRelPath string) (InstallResult, error) {
	result := InstallResult{}

	installMu <- struct{}{}
	defer func() { <-installMu }()

	if err := refuseIfForeignEngineOwner(cfg); err != nil {
		return result, err
	}

	release, err := englease.Acquire(leaseDir(cfg), englease.InstallLease, "engine-install")
	if err != nil {
		return result, fmt.Errorf("engine install lease: %w", err)
	}

	defer release.Release()

	binDir := EngineBinDir(cfg)

	// v1.3.7: the import performs the same whole-directory swap — the
	// same SHEYTAN-owned-location rule applies.
	if err := refuseUnmanagedEngineDir(cfg, binDir); err != nil {
		return result, err
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return result, err
	}

	// Stage the candidate's validated closure into .update-stage.
	stageDir := filepath.Join(binDir, ".update-stage")
	_ = os.RemoveAll(stageDir)

	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return result, err
	}

	copied, skipped, err := copyEngineClosure(candidateDir, stageDir, binaryRelPath)
	if err != nil {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf("stage discovered engine: %w", err)
	}

	found := filepath.Join(stageDir, binaryRelPath)

	ident, verr := engcheck.StaticValidate(found)
	if verr != nil {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf("discovered candidate failed validation: %w", verr)
	}

	deps, derr := engcheck.CheckDependencies(found)
	if derr == nil && len(deps.Missing) > 0 {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf(
			"discovered candidate is missing runtime DLLs: %s",
			strings.Join(deps.Missing, ", "),
		)
	}

	oldDir := binDir + ".update-old"
	_ = os.RemoveAll(oldDir)

	swapDone := false

	if err := os.Rename(binDir, oldDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return result, fmt.Errorf("stage engine swap: %w", err)
	}

	restore := func() {
		if swapDone {
			return
		}
		_ = os.RemoveAll(binDir)
		_ = os.Rename(oldDir, binDir)
	}

	if err := os.Rename(filepath.Join(oldDir, ".update-stage"), binDir); err != nil {
		restore()
		return result, fmt.Errorf("activate imported engine: %w", err)
	}

	logDroppedCompanions(mergeCompanionFiles(oldDir, binDir))

	installedPath := filepath.Join(binDir, filepath.Base(found))
	_ = os.Chmod(installedPath, 0o755)

	installedIdent, ierr := engcheck.IdentityOf(installedPath)
	if ierr != nil || installedIdent.SHA256 != ident.SHA256 {
		restore()
		return result, fmt.Errorf("imported binary identity mismatch")
	}

	result.BinPath = installedPath
	result.SHA256 = installedIdent.SHA256
	result.Outcome = fmt.Sprintf(
		"imported discovered engine package (%d closure files copied, %d byte-identical skipped)",
		copied, skipped,
	)

	// Provenance (spec §13): record WHERE the package came from. The
	// recorded release tag is intentionally NOT overwritten — a
	// discovered candidate's upstream tag is unknown until probed, and
	// bookkeeping must not silently relabel the engine (spec §34).
	recordInstallManifest(binDir, installManifest{
		Tag:         "discovered",
		SHA256:      installedIdent.SHA256,
		Size:        installedIdent.Size,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Source:      candidateDir,
	})

	_ = os.RemoveAll(oldDir)
	_ = os.RemoveAll(stageDir)

	swapDone = true

	return result, nil
}

// copyEngineClosure copies ONLY the candidate's validated engine
// closure from srcDir into dstDir (spec §11: import the closure, never
// the directory):
//
//   - the entry binary itself;
//   - every runtime DLL living DIRECTLY beside the entry binary — the
//     package's runtime closure, including dynamically loaded backends
//     (ggml-cuda.dll & friends are LoadLibrary'd, not PE-imported, so a
//     pure import-table closure would BREAK the engine);
//   - package metadata beside the binary (LICENSE/README/NOTICE/version
//     manifests).
//
// Everything else — other executables, subdirectory trees, models,
// scripts, build artifacts, neighboring engines' libraries — is
// skipped as foreign and reported. Byte-identical files already in
// dstDir are not copied again (dedupe). Returns (copied, skipped, err).
func copyEngineClosure(srcDir, dstDir, binaryRelPath string) (int, int, error) {
	exeAbs := filepath.Join(srcDir, binaryRelPath)
	binDirInSrc := filepath.Dir(exeAbs)

	copied, skipped := 0, 0

	err := filepath.Walk(srcDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries quietly
		}

		rel, rerr := filepath.Rel(srcDir, p)
		if rerr != nil || rel == "." {
			return nil
		}

		if info.IsDir() {
			// Descend ONLY along the path leading to the entry binary
			// (the package may be nested, e.g. build/bin/llama-server).
			// Every other subtree is foreign (spec §11).
			if relTo, relErr := filepath.Rel(p, exeAbs); relErr == nil && !strings.HasPrefix(relTo, "..") {
				return nil
			}

			return filepath.SkipDir
		}

		inBinDir := filepath.Dir(p) == binDirInSrc
		base := filepath.Base(p)
		ext := strings.ToLower(filepath.Ext(base))

		allowed := false

		switch {
		case rel == binaryRelPath:
			allowed = true // the validated entry binary

		case inBinDir && ext == ".dll":
			allowed = true // the package's own runtime closure (one unit)

		case inBinDir && isPackageMetadataName(base):
			allowed = true // provenance/metadata
		}

		if !allowed {
			skipped++

			return nil
		}

		target := filepath.Join(dstDir, rel)

		// Dedupe: when the destination already exists with identical
		// bytes, never copy it again (spec §20).
		if dstInfo, statErr := os.Stat(target); statErr == nil && dstInfo.Size() == info.Size() {
			srcHash, hErr := engcheck.HashFile(p)
			if hErr == nil {
				dstHash, hErr2 := engcheck.HashFile(target)
				if hErr2 == nil && srcHash == dstHash {
					skipped++

					return nil
				}
			}
		}

		if err := copyFileMode(p, target, info.Mode()); err != nil {
			return err
		}

		copied++

		return nil
	})

	return copied, skipped, err
}

// isPackageMetadataName reports whether a file name is package
// provenance/metadata (never a runtime component, never a foreign
// binary). Deliberately NARROW: a prefix allowlist plus the .md
// documentation extension — generic .txt/.json data files are NOT
// metadata (server-logs.txt would otherwise survive swaps).
func isPackageMetadataName(base string) bool {
	upper := strings.ToUpper(base)

	prefixes := []string{"LICENSE", "README", "NOTICE", "AUTHORS", "CONTRIBUTING", "CHANGELOG", "VERSION"}

	for _, p := range prefixes {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}

	return strings.ToLower(filepath.Ext(base)) == ".md"
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)

	return err
}

// mergeCompanionFiles carries ONLY allowlisted non-engine companions
// from oldDir into newDir after a package swap (spec §11/§12):
//
//   - the SHEYTAN native engine host (shtn-engine-host*) — managed by
//     SHEYTAN, never shipped inside a llama.cpp release package;
//   - license/readme/metadata text files.
//
// A stale DLL, a foreign binary or ANY other file that the new package
// does not own is deliberately DROPPED (and logged): the new package's
// DLL closure must come from the new package alone, or the runtime ends
// in the mixed-DLL 0xC0000139 state. Returns the dropped entries for
// diagnostics (spec §21).
func mergeCompanionFiles(oldDir, newDir string) []string {
	if _, err := os.Stat(oldDir); err != nil {
		return nil
	}

	var dropped []string

	_ = filepath.Walk(oldDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || p == oldDir {
			return nil
		}

		rel, rerr := filepath.Rel(oldDir, p)
		if rerr != nil {
			return nil
		}

		// Skip provisioning bookkeeping — it belongs to the swap.
		base := filepath.Base(rel)
		if strings.HasPrefix(base, ".update-") || base == ".engine-download" || base == installManifestName {
			if info.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if info.IsDir() {
			return nil // created on demand by file moves
		}

		if !isCompanionFile(base) {
			dropped = append(dropped, rel)

			// v1.6.1 LOG DISCIPLINE: a real llama.cpp package carries dozens
			// of backend DLLs — one WARN line per stale file turned every
			// package swap into a wall of noise. The per-file evidence
			// stays available at DEBUG (Advanced diagnostics) and through
			// the returned diagnostics; the normal log gets ONE aggregated
			// line per swap.
			logging.Default().Debug("updater",
				"package swap: stale engine file NOT carried into the new package: %s", rel)

			return nil
		}

		target := filepath.Join(newDir, rel)

		if _, err := os.Stat(target); err == nil {
			return nil // the new package owns this name — keep the new one
		}

		_ = os.MkdirAll(filepath.Dir(target), 0o755)

		// v1.3.7: companions are COPIED, never moved. The v1.3.6 code
		// os.Rename'd them out of oldDir BEFORE every rollback point —
		// a failed update then RemoveAll'd the directory holding the
		// (already-moved) only copy: shtn-engine-host* and metadata were
		// permanently lost, breaking the "restored byte-for-byte"
		// contract and silently deleting the native-engine host. A copy
		// leaves the original inside oldDir for Rollback to restore; the
		// duplicate dies with Commit's cleanup of oldDir.
		if err := copyFileMode(p, target, info.Mode()); err == nil {
			logging.Default().Info("updater",
				"package swap: carried companion file into the new package: %s", rel)
		}

		return nil
	})

	return dropped
}

// logDroppedCompanions emits ONE aggregated line per package swap for the
// stale files the new package does not carry forward (v1.6.1 log
// discipline: dozens of per-file WARN lines made the update log unusable;
// the per-file evidence remains at DEBUG inside mergeCompanionFiles and
// in the returned diagnostics).
func logDroppedCompanions(dropped []string) {
	if len(dropped) == 0 {
		return
	}

	sample := dropped
	if len(sample) > 5 {
		sample = sample[:5]
	}

	logging.Default().Warn("updater",
		"package swap: dropped %d stale engine file(s) not carried into the new package (stale DLLs, foreign binaries — the new package owns its own closure; first: %s)",
		len(dropped), strings.Join(sample, ", "))
}

// isCompanionFile is the swap carry-over allowlist (spec §12). DLLs and
// unknown executables are NEVER companions: they must come from the new
// package as one coherent unit.
func isCompanionFile(base string) bool {
	lower := strings.ToLower(base)

	// The native engine host family — built and managed by SHEYTAN.
	if strings.HasPrefix(lower, "shtn-engine-host") {
		return true
	}

	if strings.HasSuffix(lower, ".dll") || strings.HasSuffix(lower, ".so") || strings.HasSuffix(lower, ".dylib") {
		return false // runtime libraries are the package's own closure
	}

	if strings.HasSuffix(lower, ".exe") {
		return false // unknown executables never survive a swap
	}

	return isPackageMetadataName(base)
}

func recordInstallManifest(binDir string, m installManifest) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(filepath.Join(binDir, installManifestName), data, 0o644)
}

// ReadInstallManifest returns the committed install manifest, when present.
func ReadInstallManifest(cfg *config.Config) (installManifest, bool) {
	var m installManifest

	data, err := os.ReadFile(filepath.Join(EngineBinDir(cfg), installManifestName))
	if err != nil {
		return m, false
	}

	if err := json.Unmarshal(data, &m); err != nil {
		return m, false
	}

	return m, true
}

// downloadEngineArchiveStaged fetches the release archive through the
// Download Manager into <binDir>/.engine-download (verified, resumable,
// bounded) and returns the archive path.
func downloadEngineArchiveStaged(
	ctx context.Context,
	url, tag, binDir string,
	onProgress func(downloader.Progress),
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	stage := filepath.Join(binDir, ".engine-download")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", err
	}

	opts := downloader.Options{
		Dest: filepath.Join(stage, AssetName(tag)),
		Sources: []downloader.Source{{
			URL:   url,
			Label: "llama.cpp release " + tag + " (github.com/ggml-org/llama.cpp)",
			Trust: downloader.TrustPrimary,
		}},
		MaxBytes:   engineUpdateCapBytes,
		FileMode:   0o644,
		Resume:     true,
		AllowHTTP:  downloader.IsLoopbackURL(url),
		CacheKey:   "llama-engine-update-" + tag,
		CacheDir:   stage,
		OnProgress: onProgress,
	}

	job, err := downloader.New(opts)
	if err != nil {
		return "", err
	}

	res, err := job.Run(ctx)
	if err != nil {
		return "", err
	}

	return res.Path, nil
}

// extractArchiveBySuffix unpacks a VERIFIED archive into dir. Zip members
// go through the shared zip-slip guard; tar.gz extraction is delegated
// to the system tar exactly as the previous llm-side implementation did.
// (Moved from llm.extractEngineArchive, v1.3.6.)
func extractArchiveBySuffix(archivePath, url, dir string) error {
	switch engineArchiveSuffix(url) {
	case ".zip":
		return extractZip(archivePath, dir)

	case ".tar.gz", ".tgz":
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()

		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()

		tr := tar.NewReader(gz)

		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			out, err := SafeArchivePath(dir, hdr.Name)
			if err != nil {
				return err
			}

			if hdr.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(out, 0o755); err != nil {
					return err
				}
				continue
			}

			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}

			outFile, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}

			_, copyErr := io.Copy(outFile, tr)
			closeErr := outFile.Close()

			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}

	default:
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		return copyFileMode(archivePath, filepath.Join(dir, filepath.Base(url)), 0o755)
	}
}

// engineArchiveSuffix maps a release URL to its archive suffix.
func engineArchiveSuffix(url string) string {
	switch {
	case strings.HasSuffix(url, ".zip"):
		return ".zip"
	case strings.HasSuffix(url, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(url, ".tgz"):
		return ".tgz"
	default:
		return filepath.Ext(url)
	}
}
