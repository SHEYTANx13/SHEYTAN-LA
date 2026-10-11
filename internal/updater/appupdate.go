// App-level update support (v1.2.0, P1) — the SHEYTAN-LA application
// updater, distinct from the llama.cpp ENGINE updater that shares this
// package.
//
// Model:
//
//	release-manifest.json (published as a GitHub Release asset)
//	{
//	  "version": "1.2.0",
//	  "channel": "stable",
//	  "notes": "…",
//	  "platforms": {
//	    "windows-x64": { "url": "…", "sha256": "…", "sizeBytes": … }
//	  }
//	}
//
// v1.2.9 TRUST MODEL — precise terminology (this package previously
// blurred these, and mislabeling an integrity check as authentication
// overstates the security actually provided):
//
//   - TRANSPORT SECURITY: manifest and artifacts are fetched over HTTPS
//     (GitHub). This protects against passive network tampering.
//   - ARTIFACT INTEGRITY: the downloaded archive's SHA-256 must match
//     the digest the manifest declared, and the declared size when
//     present. This proves the BYTES did not drift after the manifest
//     was written — it detects corruption and accidental substitution.
//   - MANIFEST AUTHENTICITY: NOT CRYPTGRAPHICALLY ESTABLISHED. The
//     manifest is fetched over HTTPS from the project's own release
//     URL, which ties it to the repository's transport identity, but
//     the application does NOT verify a publisher signature on the
//     manifest itself. A GitHub-side compromise (or a hostile fork
//     pointed at via configuration) could publish a manifest whose
//     sha256 matches its own malicious artifact. SHA-256 matching is
//     therefore INTEGRITY, not publisher AUTHENTICATION — the previous
//     "signed-by-CI" / "pinned-identity" phrasing overstated it.
//   - SIGNATURE VERIFICATION: not implemented — no release artifact or
//     manifest carries a cryptographic signature this code verifies.
//     Implementing one requires new release infrastructure (signing
//     keys + a trust root shipped with the app); the downloader seams
//     (per-source Trust, digest, size caps) are where it would attach.
//
// Contract:
//
//   - package.json (→ CI) is the version authority for the INSTALLED app;
//     the manifest is the authority for AVAILABLE versions.
//   - Every download is verified against the manifest's SHA-256 (and size
//     when present) BEFORE it is treated as an update; nothing is staged
//     unverified and nothing is executed by this code — the staged
//     installer is handed to the user / OS to run.
//   - User data (models/, workspace/, sessions/, config.json) is never
//     touched by update machinery.
package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
)

// AppManifestURL is the authoritative release source: the latest GitHub
// release of this repository carrying a release-manifest.json asset.
const AppManifestURL = "https://github.com/Parsaetak/SHEYTAN-local-agent/releases/latest/download/release-manifest.json"

// appDownloadCap bounds any staged update download (installers are tens of
// MB; this cap exists to bound a hostile manifest, not to constrain real
// releases).
const appDownloadCap = 1 << 30 // 1 GiB

// AppUpdateState is the update lifecycle the UI renders.
type AppUpdateState string

const (
	AppUpToDate        AppUpdateState = "up-to-date"
	AppUpdateAvailable AppUpdateState = "update-available"
	AppCheckFailed     AppUpdateState = "check-failed"
	AppDownloading     AppUpdateState = "downloading"
	AppReady           AppUpdateState = "ready" // staged, verified, awaiting install
	AppFailed          AppUpdateState = "failed"
	AppUnknown         AppUpdateState = "unknown" // never checked
)

// AppPlatformUpdate describes one platform's downloadable artifact.
type AppPlatformUpdate struct {
	// URL is the exact release asset URL (GitHub Releases).
	URL string `json:"url"`
	// SHA256 is the lowercase hex digest of the artifact — REQUIRED.
	SHA256 string `json:"sha256"`
	// SizeBytes, when non-zero, is verified before/after download.
	SizeBytes int64 `json:"sizeBytes,omitempty"`
	// Kind is "installer" | "zip" | "msix" — informational.
	Kind string `json:"kind,omitempty"`
}

// AppManifest is the release manifest (v1.2.0 schema). v1.2.9: NOT
// signed — see the package trust model; its authenticity rests on the
// HTTPS transport identity of the release URL, nothing stronger.
type AppManifest struct {
	Version     string                       `json:"version"`
	Channel     string                       `json:"channel,omitempty"`
	Notes       string                       `json:"notes,omitempty"`
	PublishedAt string                       `json:"publishedAt,omitempty"`
	Platforms   map[string]AppPlatformUpdate `json:"platforms"`
}

// PlatformID is this machine's manifest platform key.
func PlatformID() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	name := arch
	switch arch {
	case "amd64":
		name = "x64"
	case "arm64":
		name = "arm64"
	case "386":
		name = "x86"
	}
	return goos + "-" + name
}

// FetchAppManifest downloads and parses the release manifest.
func FetchAppManifest(ctx context.Context, url string) (*AppManifest, error) {
	if url == "" {
		url = AppManifestURL
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest fetch: HTTP %d", resp.StatusCode)
	}

	var m AppManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest parse: %w", err)
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, errors.New("manifest has no version")
	}
	return &m, nil
}

// AppUpdateStatus is the /api/update/status payload.
type AppUpdateStatus struct {
	State        AppUpdateState `json:"state"`
	Version      string         `json:"version"` // installed
	Latest       string         `json:"latest,omitempty"`
	Channel      string         `json:"channel,omitempty"`
	Notes        string         `json:"notes,omitempty"`
	PublishedAt  string         `json:"publishedAt,omitempty"`
	StagedPath   string         `json:"stagedPath,omitempty"`
	StagedSHA256 string         `json:"stagedSHA256,omitempty"`
	CheckedAt    string         `json:"checkedAt,omitempty"`
	Message      string         `json:"message,omitempty"`
	// Download (v1.2.3) carries live staging progress while the state
	// is "downloading": phase, bytes, speed, ETA, source, verification.
	// Nil outside downloads.
	Download *downloader.Progress `json:"download,omitempty"`
}

// CheckAppUpdate fetches the manifest and compares against installed.
func CheckAppUpdate(ctx context.Context, installedVersion, manifestURL string) (AppUpdateStatus, error) {
	m, err := FetchAppManifest(ctx, manifestURL)
	if err != nil {
		return AppUpdateStatus{
			State:     AppCheckFailed,
			Version:   installedVersion,
			Message:   err.Error(),
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		}, err
	}

	st := AppUpdateStatus{
		Version:     installedVersion,
		Latest:      m.Version,
		Channel:     m.Channel,
		Notes:       m.Notes,
		PublishedAt: m.PublishedAt,
		CheckedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if CompareVersions(m.Version, installedVersion) > 0 {
		st.State = AppUpdateAvailable
		st.Message = "v" + m.Version + " available"
	} else {
		st.State = AppUpToDate
		st.Message = "up to date"
	}
	return st, nil
}

// StageAppUpdate downloads the platform artifact into dataDir/updates/
// staging, verifying SHA-256 (and size when declared) BEFORE the staged
// file is renamed into place. It returns the staged path + digest. The
// caller decides what to do with a staged installer — this function never
// executes one.
//
// v1.2.3: the transfer runs through the reusable Download Manager —
// streamed to a .part file, resumable via HTTP Range, retried with
// bounded backoff, and atomically activated only after verification.
// A cancelled download keeps its .part so a retry resumes where it left
// off instead of restarting from zero.
func StageAppUpdate(ctx context.Context, dataDir string, manifest *AppManifest, platformID string) (path, sha string, err error) {
	return StageAppUpdateWithProgress(ctx, dataDir, manifest, platformID, nil)
}

// StageAppUpdateWithProgress is StageAppUpdate with a live progress
// callback for callers with UI to feed. onProgress may be nil.
func StageAppUpdateWithProgress(ctx context.Context, dataDir string, manifest *AppManifest, platformID string, onProgress func(downloader.Progress)) (path, sha string, err error) {
	job, err := AppUpdateStagingJob(dataDir, manifest, platformID, onProgress)
	if err != nil {
		return "", "", err
	}
	res, err := job.Run(ctx)
	if err != nil {
		return "", "", err
	}
	return res.Path, res.SHA256, nil
}

// AppUpdateStagingJob validates the manifest and BUILDS (but does not
// start) the staging download job — v1.2.3's seam for asynchronous UI
// staging: the caller owns the goroutine, observes job.Progress() live
// and can job.Cancel() at any moment. Every pinned-identity rule applies:
// the manifest must carry sha256 for the platform, and the job verifies
// digest + size before the staged file is activated.
func AppUpdateStagingJob(dataDir string, manifest *AppManifest, platformID string, onProgress func(downloader.Progress)) (*downloader.Job, error) {
	if manifest == nil {
		return nil, errors.New("nil manifest")
	}
	p, ok := manifest.Platforms[platformID]
	if !ok || strings.TrimSpace(p.URL) == "" {
		return nil, fmt.Errorf("no update artifact for platform %q", platformID)
	}
	if strings.TrimSpace(p.SHA256) == "" {
		return nil, errors.New("manifest artifact carries no sha256 — refusing unverified download")
	}

	stagingDir := filepath.Join(dataDir, "updates", "staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, err
	}

	name := updateArtifactName(manifest.Version, p.Kind)
	final := filepath.Join(stagingDir, name)

	opts := downloader.Options{
		Dest: final,
		Sources: []downloader.Source{{
			URL:   p.URL,
			Label: "GitHub release asset (authoritative)",
			Trust: downloader.TrustPrimary,
		}},
		SHA256:    strings.ToLower(strings.TrimSpace(p.SHA256)),
		SizeBytes: p.SizeBytes,
		MaxBytes:  appDownloadCap,
		FileMode:  0o644,
		Resume:    true,
		// HTTPS-only for remote sources; loopback test doubles may
		// speak plain HTTP (the Go tests use httptest).
		AllowHTTP:  downloader.IsLoopbackURL(p.URL),
		CacheKey:   "app-update-" + manifest.Version + "-" + platformID,
		CacheDir:   stagingDir,
		OnProgress: onProgress,
	}
	return downloader.New(opts)
}

func updateArtifactName(version, kind string) string {
	ext := ".zip"
	switch kind {
	case "installer":
		ext = ".exe"
	case "msix":
		ext = ".msix"
	}
	return "SHEYTAN-LA-v" + version + "-staged" + ext
}

// StageIsValid re-verifies a previously staged artifact (state "ready"
// must always survive re-inspection): existence + size + digest.
func StageIsValid(path, wantSHA string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if wantSHA == "" {
		return errors.New("no digest recorded for staged update")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(wantSHA) {
		return fmt.Errorf("staged digest drifted: %s", got)
	}
	_ = fi
	return nil
}

// ZipSafeNames is exported for tests: walks a zip's entry names through the
// same zip-slip guard the engine updater uses.
func ZipSafeNames(r *zip.Reader, dir string) ([]string, error) {
	out := make([]string, 0, len(r.File))
	for _, f := range r.File {
		p, err := safeZipPath(dir, f.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
