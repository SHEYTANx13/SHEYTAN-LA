// engine_update.go — v1.11.1: the official llama.cpp engine update-check
// and USER-TRIGGERED update surface.
//
//	GET  /api/engine/update/check    read-only release check (no download,
//	                                  no install, no engine touch)
//	POST /api/engine/update          user-triggered engine update through
//	                                  the EXISTING engine-owned
//	                                  UpdateEngineNow transaction
//
// Contracts (operator request — automatic official-repository checks for
// llama.cpp, with USER-CONTROLLED download/install):
//
//   - CHECK ≠ INSTALL. The check resolves the newest official
//     ggml-org/llama.cpp release whose payload ACTUALLY carries the exact
//     asset for the installed variant (the ONE variant-aware resolver —
//     never a URL synthesized from a tag). A check never installs
//     anything; the scheduled updater keeps its own policy semantics.
//   - Honest status classes: current | update-available | offline |
//     no-asset | error | installing | installed | install-failed. A
//     network failure is NEVER reported as "no asset" — the no-asset
//     verdict only follows a successful official release lookup.
//   - The install is the SAME transactional authority the scheduled
//     updater and the maintenance gate drive (UpdateEngineNow: stop →
//     stage → verify → restart → health-check → commit | rollback). No
//     second updater, no second archive installer.
//   - Run-active / calibration / remote-provider gates refuse the
//     install BEFORE anything happens (the same gates as variant
//     provisioning).
//   - Already current = a NO-OP (no engine restart for nothing).
//   - Progress is the live downloader progress; cancellation goes
//     through the existing engine download cancel action.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/downloader"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/netcheck"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
)

// engineUpdateSource is the ONE canonical official project this surface
// consults (mirrors the updater's release-list URL).
const engineUpdateSource = "ggml-org/llama.cpp"

// engineUpdateCheck is the wire shape of the check surface.
type engineUpdateCheck struct {
	// InstalledTag is the EFFECTIVE installed engine tag (recorded state,
	// or the committed manifest beside the binary; "" when no engine is
	// installed yet).
	InstalledTag string `json:"installedTag"`
	// InstalledVariant is the committed install manifest's backend variant.
	InstalledVariant string `json:"installedVariant"`
	// LatestTag is the newest official release tag whose payload carries
	// the exact variant asset for this platform ("" when none resolved).
	LatestTag string `json:"latestTag,omitempty"`
	// AssetVariant/AssetName describe the exact prebuilt asset the
	// resolution matched (the strict resolver never synthesizes a URL).
	AssetVariant string `json:"assetVariant"`
	AssetName    string `json:"assetName,omitempty"`
	Platform     string `json:"platform"`
	Source       string `json:"source"`
	// Status is the honest class: current | update-available | offline |
	// no-asset | error | installing | installed | install-failed.
	Status string `json:"status"`
	// Message is the human-readable explanation (never a fabricated
	// "up to date" on a failed check).
	Message string `json:"message,omitempty"`
	// CheckedAt is the UTC stamp of the last live check/install outcome.
	CheckedAt string `json:"checkedAt,omitempty"`
	// Progress is the live download progress while Status == installing.
	Progress *downloader.Progress `json:"progress,omitempty"`
}

func engineUpdateNowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// handleEngineUpdateCheck serves GET /api/engine/update/check — a live,
// read-only check against the official release source. Never mutates
// engine state, never downloads.
func (s *Server) handleEngineUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	writeJSON(w, s.liveEngineUpdateCheck(r.Context()))
}

// liveEngineUpdateCheck resolves the current check state. A running
// user-triggered transaction takes precedence (its live progress is the
// truth); otherwise a fresh read-only resolution runs and its outcome is
// cached for the poll surfaces.
func (s *Server) liveEngineUpdateCheck(ctx context.Context) engineUpdateCheck {
	cfg := s.src.Load()
	variant := updater.InstalledEngineVariant(cfg)
	installed := updater.EffectiveInstalledEngineTag(cfg)
	if installed == "" {
		installed = updater.DefaultEngineTag
	}

	st := engineUpdateCheck{
		InstalledTag:     installed,
		InstalledVariant: string(variant),
		AssetVariant:     string(variant),
		Platform:         goPlatform(),
		Source:           engineUpdateSource,
		Status:           "error",
	}

	// A user-triggered transaction is the live truth — report it with
	// progress instead of racing a fresh resolution.
	if s.engineUpdateRunning.Load() {
		st.Status = "installing"
		if cached, ok := s.lastEngineUpdateCheck.Load().(engineUpdateCheck); ok {
			st.LatestTag = cached.LatestTag
			st.AssetName = cached.AssetName
		}
		if p := s.engineUpdateProgress.Load(); p != nil {
			st.Progress = p
			st.Message = fmt.Sprintf("installing engine update %s — download in progress", st.LatestTag)
		} else {
			st.Message = fmt.Sprintf("installing engine update %s — transaction in progress", st.LatestTag)
		}
		st.CheckedAt = engineUpdateNowUTC()
		return st
	}

	if netcheck.IsOffline() {
		st.Status = "offline"
		st.Message = "offline — the engine update check was skipped (no network)"
		st.CheckedAt = engineUpdateNowUTC()
		s.lastEngineUpdateCheck.Store(st)
		return st
	}

	latest, err := updater.LatestTagForVariant(ctx, variant)
	switch {
	case err != nil && updater.IsNoAssetError(err):
		st.Status = "no-asset"
		st.Message = fmt.Sprintf(
			"the official %s release list carries no prebuilt %s package this build can install for %s (upstream publishes .tar.gz archives there; this build's transactional installer consumes ZIP packages) — keep the current engine or point llamaBinPath at a self-built llama-server",
			engineUpdateSource, variant, goPlatform(),
		)
	case err != nil:
		// A NETWORK failure is an error — never a "no asset" verdict and
		// never a fabricated "up to date".
		st.Status = "error"
		st.Message = "check failed: " + err.Error()
	case latest == "":
		// Resolver miss without the typed wrapper (the same honest class).
		st.Status = "no-asset"
		st.Message = fmt.Sprintf(
			"no recent official %s release carries a prebuilt %s package for %s — keep the current engine or point llamaBinPath at a self-built llama-server",
			engineUpdateSource, variant, goPlatform(),
		)
	default:
		st.LatestTag = latest
		st.AssetName = updater.AssetNameForVariant(latest, variant)
		if latest == installed {
			st.Status = "current"
			st.Message = fmt.Sprintf("engine is current (%s, %s variant)", latest, variant)
		} else {
			st.Status = "update-available"
			st.Message = fmt.Sprintf("engine update available: %s → %s (exact asset %s)", installed, latest, st.AssetName)
		}
	}
	st.CheckedAt = engineUpdateNowUTC()
	s.lastEngineUpdateCheck.Store(st)
	return st
}

// handleEngineUpdate serves POST /api/engine/update — the USER-TRIGGERED
// engine update: body {"tag": "<optional tag>"} (empty tag = the newest
// release carrying the installed variant's exact asset). The transaction
// is the engine-owned UpdateEngineNow flow (stop → stage → verify →
// restart → commit | rollback), started once, observable through the
// check surface, cancellable through the existing engine download cancel.
func (s *Server) handleEngineUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decode engine-update body: %w", err))
		return
	}

	// The same coordination gates as variant provisioning: a live
	// generation is never served under a stopped engine, a calibration
	// never races the transaction, and remote providers have no local
	// engine to update.
	if s.anyRunActive() {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"a run is active — engine updates are locked while a generation is in flight",
		))
		return
	}
	if s.calibrating.Load() {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"an automatic calibration is in progress — retry when it settles",
		))
		return
	}
	if s.src.Load().IsRemote() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"remote provider active — engine updates are a local-engine concept",
		))
		return
	}

	// One user-triggered engine update at a time.
	if !s.engineUpdateRunning.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"an engine update is already running — watch the check surface for its progress",
		))
		return
	}

	cfg := s.src.Load()
	variant := updater.InstalledEngineVariant(cfg)

	// Resolve the target tag FIRST (bounded, in the handler): the exact
	// official asset must exist — never a URL synthesized from a tag.
	tag := strings.TrimSpace(body.Tag)
	if tag == "" {
		latest, err := updater.LatestTagForVariant(r.Context(), variant)
		if err != nil || latest == "" {
			s.engineUpdateRunning.Store(false)
			st := s.liveEngineUpdateCheck(r.Context())
			writeErr(w, http.StatusConflict, fmt.Errorf(
				"no engine update available to install (latest resolution: %s) — run a check first",
				st.Status,
			))
			return
		}
		tag = latest
	}

	// Already current is a NO-OP — the engine is never restarted for
	// nothing (the same short-circuit the scheduled updater applies).
	installed := updater.EffectiveInstalledEngineTag(cfg)
	if installed == "" {
		installed = updater.DefaultEngineTag
	}
	if tag == installed {
		s.engineUpdateRunning.Store(false)
		st := engineUpdateCheck{
			InstalledTag:     installed,
			InstalledVariant: string(variant),
			LatestTag:        tag,
			AssetVariant:     string(variant),
			AssetName:        updater.AssetNameForVariant(tag, variant),
			Platform:         goPlatform(),
			Source:           engineUpdateSource,
			Status:           "current",
			Message:          fmt.Sprintf("engine is already current (%s) — nothing to install", tag),
			CheckedAt:        engineUpdateNowUTC(),
		}
		s.lastEngineUpdateCheck.Store(st)
		writeJSON(w, st)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.engineUpdateCancel.Store(&cancel)
	s.engineUpdateProgress.Store(nil)

	go func() {
		defer func() {
			s.engineUpdateRunning.Store(false)
			s.engineUpdateCancel.Store(nil)
			cancel()
		}()

		onProgress := func(p downloader.Progress) {
			s.engineUpdateProgress.Store(&p)
		}

		logging.Default().Info("updater", "user-triggered engine update: installing %s through the engine-owned transaction", tag)
		outcome, err := s.llama.UpdateEngineNow(ctx, tag, onProgress)

		st := engineUpdateCheck{
			InstalledTag:     tag,
			InstalledVariant: string(updater.InstalledEngineVariant(s.src.Load())),
			LatestTag:        tag,
			AssetVariant:     string(variant),
			AssetName:        updater.AssetNameForVariant(tag, variant),
			Platform:         goPlatform(),
			Source:           engineUpdateSource,
			CheckedAt:        engineUpdateNowUTC(),
		}
		if err != nil {
			st.Status = "install-failed"
			st.Message = "engine update failed (the previous engine was restored): " + err.Error()
			logging.Default().Warn("updater", "user-triggered engine update failed: %v", err)
		} else {
			st.Status = "installed"
			st.Message = outcome
		}
		s.lastEngineUpdateCheck.Store(st)
	}()

	writeJSON(w, engineUpdateCheck{
		InstalledTag:     installed,
		InstalledVariant: string(variant),
		LatestTag:        tag,
		AssetVariant:     string(variant),
		AssetName:        updater.AssetNameForVariant(tag, variant),
		Platform:         goPlatform(),
		Source:           engineUpdateSource,
		Status:           "installing",
		Message:          fmt.Sprintf("engine update %s started — stop → stage → verify → restart → commit | rollback (progress via the check surface)", tag),
		CheckedAt:        engineUpdateNowUTC(),
	})
}
