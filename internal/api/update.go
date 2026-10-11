// v1.2.0 — the application update surface: check against the authoritative
// GitHub release manifest, and stage a SHA-256-verified artifact. This file
// NEVER executes installers and never deletes user data; the staged
// artifact is applied by the user / OS installer, which is the honest
// recovery-friendly flow for this release.
//
// v1.2.3 — staging became ASYNCHRONOUS on the reusable Download Manager:
//   - POST /api/update/download builds the staging job, runs it on a
//     background goroutine and returns the current status immediately
//     (state "downloading"); progress (phase, bytes, speed, ETA, source,
//     verification) is observed via GET /api/update/status — no more
//     5-minute blocking request.
//   - POST /api/update/cancel stops network + file activity immediately;
//     the .part file is kept so a retry resumes instead of restarting.
//   - Only one staging job runs at a time; a second request reports 409.
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// handleUpdateStatus serves GET /api/update/status — the last-known update
// state (up-to-date | update-available | ready | …) plus live staging
// progress while a download runs. A stale/absent state reports honestly as
// unknown; use /api/update/check for a live check.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	st := AppUpdateStatusInternal{
		State:   updater.AppUnknown,
		Version: config.AppVersion,
		Message: "not checked yet — run Check now",
	}

	if cached, ok := s.lastAppUpdate.Load().(updater.AppUpdateStatus); ok {
		st = cached
	}

	// Live staging progress (v1.2.3): the atomic job pointer is the
	// authority — non-nil means a download is running RIGHT NOW, even if
	// the cached status has not caught up yet.
	if job := s.appUpdateJob.Load(); job != nil {
		p := job.Progress()
		st.Download = &p
		st.State = updater.AppDownloading
	}

	writeJSON(w, st)
}

// handleUpdateCheck serves POST /api/update/check — live manifest check.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	st, err := updater.CheckAppUpdate(r.Context(), config.AppVersion, updater.AppManifestURL)
	s.lastAppUpdate.Store(st)

	if err != nil {
		// A failed check is a REPORTED state, not a 5xx: the UI shows the
		// honest check-failed message.
		writeJSON(w, st)
		return
	}
	writeJSON(w, st)
}

// handleUpdateDownload serves POST /api/update/download — START staging the
// platform artifact with SHA-256 verification and return immediately. The
// UI polls /api/update/status for live progress (v1.2.3).
func (s *Server) handleUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cached, ok := s.lastAppUpdate.Load().(updater.AppUpdateStatus)
	if !ok || cached.Latest == "" || cached.State != updater.AppUpdateAvailable {
		writeErr(w, http.StatusConflict, errors.New("run a successful update check first"))
		return
	}

	// One staging job at a time.
	if !s.appUpdateStaging.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, errors.New("an update download is already running"))
		return
	}

	// Re-fetch the manifest NOW (in the handler, bounded) so the digest we
	// verify against is the one the release CURRENTLY publishes — never a
	// stale copy. The staging goroutine only consumes the pinned artifact.
	manifest := &updater.AppManifest{
		Version:     cached.Latest,
		Channel:     cached.Channel,
		Notes:       cached.Notes,
		PublishedAt: cached.PublishedAt,
	}
	fresh, err := updater.FetchAppManifest(r.Context(), updater.AppManifestURL)
	if err == nil && fresh.Version == cached.Latest {
		manifest = fresh
	} else if err != nil {
		s.appUpdateStaging.Store(false)
		cached.State = updater.AppFailed
		cached.Message = "manifest re-fetch failed: " + err.Error()
		cached.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		s.lastAppUpdate.Store(cached)
		writeJSON(w, cached)
		return
	}

	cfg := s.src.Load()
	job, err := updater.AppUpdateStagingJob(cfg.DataDir, manifest, updater.PlatformID(), nil)
	if err != nil {
		s.appUpdateStaging.Store(false)
		cached.State = updater.AppFailed
		cached.Message = err.Error()
		cached.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		s.lastAppUpdate.Store(cached)
		writeJSON(w, cached)
		return
	}

	// Own lifecycle context so /api/update/cancel can stop the transfer;
	// the staging deadline (15 min) bounds the whole job as before.
	ctx, cancel := context.WithCancel(context.Background())
	s.appUpdateCancel.Store(&cancel)
	s.appUpdateJob.Store(job)

	go func() {
		defer func() {
			s.appUpdateStaging.Store(false)
			s.appUpdateJob.Store(nil)
			cancel()
		}()

		dctx, dcancel := context.WithTimeout(ctx, 15*time.Minute)
		defer dcancel()

		res, stageErr := job.Run(dctx)

		cached, _ := s.lastAppUpdate.Load().(updater.AppUpdateStatus)
		cached.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		if stageErr != nil {
			cached.State = updater.AppFailed
			cached.Message = stageErr.Error()
			if errors.Is(stageErr, downloader.ErrCancelled) {
				cached.Message = "download cancelled — retry resumes from the downloaded part"
			}
		} else {
			cached.State = updater.AppReady
			cached.StagedPath = res.Path
			cached.StagedSHA256 = res.SHA256
			cached.Message = "verified and staged — run the installer to apply"
		}
		s.lastAppUpdate.Store(cached)
	}()

	st := cached
	st.State = updater.AppDownloading
	st.Message = "staging v" + manifest.Version + " — download running"
	st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	p := job.Progress()
	st.Download = &p
	s.lastAppUpdate.Store(st)

	writeJSON(w, st)
}

// handleUpdateCancel serves POST /api/update/cancel — stops the running
// staging job immediately (network + file activity). The .part file stays
// on disk so a later download resumes.
func (s *Server) handleUpdateCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	if fn := s.appUpdateCancel.Load(); fn != nil {
		(*fn)()
	}
	if job := s.appUpdateJob.Load(); job != nil {
		job.Cancel()
	}

	st, _ := s.lastAppUpdate.Load().(updater.AppUpdateStatus)
	st.Message = "cancelling download"
	writeJSON(w, st)
}

// AppUpdateStatusInternal is the wire shape (mirrors
// updater.AppUpdateStatus with stable JSON names).
type AppUpdateStatusInternal = updater.AppUpdateStatus
