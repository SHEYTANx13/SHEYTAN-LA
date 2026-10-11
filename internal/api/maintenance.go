// StartupMaintenanceGate (v1.6.0, spec §2/§4 — P0).
//
// THE STARTUP ORDERING DEFECT THIS REPAIRS (observed on real Windows
// runtime at v1.5.1):
//
//      startup → engine starts → compatibility mode → engine ready
//              → updater stops engine → updater downloads new engine
//
// EnsureSetup prewarmed the engine AND THEN started the scheduled
// updater, whose immediate pass could decide an update was due and stop
// the freshly started engine mid-boot. The required lifecycle is the
// exact reverse: the startup maintenance decision — and any transactional
// maintenance it requires — completes BEFORE any engine process is
// started or prewarmed:
//
//      PROCESS START
//        → INITIALIZE CONFIG / RUNTIME / UI SERVICES
//        → StartupMaintenanceGate (CHECKING → … → READY_FOR_PREWARM)
//        → MODEL PREWARM (model-first: only with an explicit selection)
//        → HEALTH CHECK → ENGINE READY
//
// INVARIANT (tested in maintenance_test.go): no engine process may be
// started or prewarmed before the maintenance decision completed. The
// gate enforces it with explicit synchronization — a done channel every
// prewarm and the scheduled updater's FIRST pass wait on. No sleeps, no
// timing assumptions.
//
// The gate is NOT a second updater: it drives the SAME authorities
// (updater.CheckDue / LatestTag / UpdateEngineWithProgress → the
// engine-owned UpdateEngineNow transaction, installer, engcheck). The
// scheduled loop keeps ownership of every later pass.
package api

import (
        "context"
        "fmt"
        "net/http"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// MaintenancePhase is the explicit, UI/telemetry-ready state of the
// startup maintenance gate (spec §4).
type MaintenancePhase string

const (
        // PhaseChecking: the installed engine is being inspected and the
        // maintenance requirement determined.
        PhaseChecking MaintenancePhase = "CHECKING"
        // PhaseMaintenanceRequired: an update/repair is due and will be
        // performed transactionally before any engine start.
        PhaseMaintenanceRequired MaintenancePhase = "MAINTENANCE_REQUIRED"
        // PhaseDownloading: the new engine archive is transferring.
        PhaseDownloading MaintenancePhase = "DOWNLOADING"
        // PhaseVerifying: the downloaded archive is being verified.
        PhaseVerifying MaintenancePhase = "VERIFYING"
        // PhaseInstalling: the verified engine is being installed.
        PhaseInstalling MaintenancePhase = "INSTALLING"
        // PhaseReadyForPrewarm: maintenance completed (or was not needed);
        // model prewarm is released.
        PhaseReadyForPrewarm MaintenancePhase = "READY_FOR_PREWARM"
        // PhaseDeferred: no maintenance this boot (schedule off / not due /
        // offline) — prewarm is released immediately.
        PhaseDeferred MaintenancePhase = "DEFERRED"
        // PhaseBlocked: maintenance failed AND the installed engine is not
        // usable — prewarm is refused with an explicit diagnostic.
        PhaseBlocked MaintenancePhase = "BLOCKED"
        // PhaseFailed: maintenance failed but the installed (last-known-good)
        // engine remains usable — startup continues with it and the failure
        // is surfaced.
        PhaseFailed MaintenancePhase = "FAILED"
)

// prewarmPermitted reports which terminal phases release the prewarm.
// BLOCKED is the only refusing phase: an engine that failed static
// validation must never be launched blindly.
func prewarmPermitted(p MaintenancePhase) bool {
        switch p {
        case PhaseReadyForPrewarm, PhaseDeferred, PhaseFailed:
                return true
        default:
                return false
        }
}

// MaintenanceStatus is the deterministic GET /api/maintenance shape.
type MaintenanceStatus struct {
        Phase      MaintenancePhase `json:"phase"`
        Reason     string           `json:"reason,omitempty"`
        Detail     string           `json:"detail,omitempty"`
        StartedAt  time.Time        `json:"startedAt"`
        FinishedAt *time.Time       `json:"finishedAt,omitempty"`
        // PrewarmPermitted states whether model prewarm was released.
        PrewarmPermitted bool `json:"prewarmPermitted"`
        // Updated reports whether a maintenance update was applied this boot.
        Updated bool `json:"updated"`
        // TargetTag is the engine tag maintenance targeted (set when an
        // update was required).
        TargetTag string `json:"targetTag,omitempty"`
}

// maintenanceGate is the one authoritative startup maintenance
// coordinator. Zero safe-value is NOT usable — build it with
// newMaintenanceGate.
type maintenanceGate struct {
        phase     atomic.Value // MaintenancePhase
        reason    atomic.Value // string
        detail    atomic.Value // string
        updated   atomic.Bool
        targetTag atomic.Value // string

        startedAt  time.Time
        finishedAt atomic.Value // time.Time

        done     chan struct{}
        closeOne sync.Once
}

func newMaintenanceGate() *maintenanceGate {
        g := &maintenanceGate{
                startedAt: time.Now(),
                done:      make(chan struct{}),
        }
        g.phase.Store(PhaseChecking)
        g.reason.Store("")
        g.detail.Store("")
        g.targetTag.Store("")
        return g
}

func (g *maintenanceGate) setPhase(p MaintenancePhase, reason, detail string) {
        g.phase.Store(p)
        g.reason.Store(reason)
        g.detail.Store(detail)
}

func (g *maintenanceGate) finish(p MaintenancePhase, reason, detail string) {
        g.setPhase(p, reason, detail)
        g.finishedAt.Store(time.Now())
        g.closeOne.Do(func() { close(g.done) })
}

// Done is closed exactly once, when the maintenance decision (including
// any transactional update) has completed. Prewarm and the scheduled
// updater's first pass synchronize on this — never on timing.
func (g *maintenanceGate) Done() <-chan struct{} { return g.done }

// Snapshot returns the deterministic status for /api/maintenance.
func (g *maintenanceGate) Snapshot() MaintenanceStatus {
        phase := g.phase.Load().(MaintenancePhase)
        reason, _ := g.reason.Load().(string)
        detail, _ := g.detail.Load().(string)
        tag, _ := g.targetTag.Load().(string)

        st := MaintenanceStatus{
                Phase:            phase,
                Reason:           reason,
                Detail:           detail,
                StartedAt:        g.startedAt,
                PrewarmPermitted: prewarmPermitted(phase),
                Updated:          g.updated.Load(),
                TargetTag:        tag,
        }

        if f, ok := g.finishedAt.Load().(time.Time); ok {
                st.FinishedAt = &f
        }

        return st
}

// WaitForGate blocks until the gate completed, the context expired, or
// no gate exists (schedule-independent degenerate case: nothing to
// wait for). Returns whether the gate reached a terminal phase.
func (s *Server) WaitForGate(ctx context.Context) bool {
        g := s.gateValue()
        if g == nil {
                return true // no gate armed — nothing gates the caller
        }

        select {
        case <-g.Done():
                return true
        case <-ctx.Done():
                return false
        }
}

// gateValue reads the armed gate without racing EnsureSetup.
func (s *Server) gateValue() *maintenanceGate {
        s.gateMu.Lock()
        defer s.gateMu.Unlock()
        return s.gate
}

// maintenanceSeams are the test seams for the gate (nil in production).
// They exist so the ordering regression tests are deterministic — no
// network, no engine binary, no sleeps.
type maintenanceSeams struct {
        // latestTag replaces updater.LatestTag (release resolution).
        latestTag func(ctx context.Context) (string, error)
        // performUpdate replaces updater.UpdateEngineWithProgress.
        performUpdate func(ctx context.Context, eng updater.Engine, tag string, onProgress func(downloader.Progress)) (string, error)
        // validateInstalled replaces the installed-engine static check.
        validateInstalled func() error
}

// startStartupMaintenance arms the gate exactly once and launches the
// maintenance decision goroutine. Repeated EnsureSetup calls reuse the
// SAME gate — no duplicate maintenance (spec §5 test 6).
func (s *Server) startStartupMaintenance() *maintenanceGate {
        s.gateMu.Lock()
        defer s.gateMu.Unlock()

        if s.gate != nil {
                return s.gate
        }

        g := newMaintenanceGate()
        s.gate = g

        go s.runStartupMaintenance(g)

        return g
}

// runStartupMaintenance is the decision body. Phases:
//
//      CHECKING → (schedule off | not due | offline) → DEFERRED
//      CHECKING → up to date                             → READY_FOR_PREWARM
//      CHECKING → MAINTENANCE_REQUIRED → transfer phases → READY_FOR_PREWARM
//                                       → failure + engine usable → FAILED
//                                       → failure + engine unusable → BLOCKED
func (s *Server) runStartupMaintenance(g *maintenanceGate) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
        defer cancel()

        cur := s.src.Load()
        mutable := *cur

        schedule := updater.NormalizeSchedule(mutable.UpdateSchedule)

        if schedule == updater.ScheduleOff {
                logging.Default().Info("maintenance", "startup gate: scheduled engine updates are off — no maintenance this boot")
                g.finish(PhaseDeferred, "updates-off", "scheduled engine updates disabled")
                return
        }

        last, _ := time.Parse(time.RFC3339, mutable.LastUpdateCheck)
        if !updater.CheckDue(schedule, last) {
                g.finish(PhaseDeferred, "not-due", "last check "+humanDueWhen(last))
                return
        }

        if netcheck.IsOffline() {
                g.finish(PhaseDeferred, "offline", "update check skipped (offline)")
                return
        }

        // The check is happening: stamp it on a PRIVATE copy and publish
        // ONLY the updater's own field (the v1.5.0 selection-race rule).
        updater.MarkChecked(&mutable)
        s.src.Update(func(c *config.Config) {
                c.LastUpdateCheck = mutable.LastUpdateCheck
        })
        if s.maintenanceSave != nil {
                s.maintenanceSave()
        }

        latest, err := s.maintenanceLatestTag(ctx)
        if err != nil {
                // Case C (network variant): check failed, the installed engine
                // is untouched and still the authority — surface and continue.
                logging.Default().Warn("maintenance", "startup gate: release check failed: %v", err)
                g.finish(PhaseFailed, "check-failed", err.Error())
                return
        }

        // v1.7.4 (P0 #3, B): the EFFECTIVE installed tag — the recorded tag, or
        // the committed engine-install.json manifest beside the binary when the
        // state file carries none. The plain state-file read used to fall back
        // to the bundled default tag here and re-download the engine every boot.
        current := updater.EffectiveInstalledEngineTag(&mutable)
        if current == "" {
                current = updater.DefaultEngineTag
        }

        if latest == current {
                logging.Default().Info("maintenance", "startup gate: engine is current (%s)", latest)
                g.finish(PhaseReadyForPrewarm, "current", "engine is current ("+latest+")")
                return
        }

        g.targetTag.Store(latest)
        g.setPhase(PhaseMaintenanceRequired, "update-available", current+" → "+latest)

        logging.Default().Info("maintenance",
                "startup gate: engine update required (%s → %s) — performing transactional maintenance BEFORE any engine start",
                current, latest)

        // MODEL-FIRST (spec §2 Case D): with no selected model the engine
        // must not be STARTED — but the engine FILES may still be updated.
        // A nil Engine performs the staged install without any start.
        var eng updater.Engine
        if strings.TrimSpace(mutable.Model) != "" {
                eng = s.llama
        } else {
                logging.Default().Info("maintenance",
                        "startup gate: no model selected — engine update installs files only; the model selection owns the first start")
        }

        msg, err := s.maintenancePerformUpdate(ctx, eng, latest, func(p downloader.Progress) {
                switch p.Phase {
                case downloader.PhaseDownloading:
                        g.setPhase(PhaseDownloading, "downloading", latest)
                case downloader.PhaseVerifying:
                        g.setPhase(PhaseVerifying, "verifying", latest)
                case downloader.PhaseInstalling:
                        g.setPhase(PhaseInstalling, "installing", latest)
                }
        })

        if err != nil {
                // The transaction preserved the last-known-good engine on disk
                // (UpdateEngineNow rollback / staged install never destroys the
                // working binary). Verify that claim honestly before releasing
                // prewarm: a binary that fails static validation must never be
                // launched blindly (spec §2 Case C, blocked variant).
                if verr := s.maintenanceValidateInstalled(); verr != nil {
                        logging.Default().Error("maintenance",
                                "startup gate: update to %s failed (%v) AND the installed engine fails validation (%v) — prewarm BLOCKED",
                                latest, err, verr)
                        g.finish(PhaseBlocked, "update-failed-engine-unusable",
                                fmt.Sprintf("update: %v; installed engine: %v", err, verr))
                        return
                }

                logging.Default().Warn("maintenance",
                        "startup gate: update to %s failed (%v) — keeping the last-known-good engine and continuing startup with it",
                        latest, err)
                g.finish(PhaseFailed, "update-failed-engine-valid", err.Error())
                return
        }

        g.updated.Store(true)
        logging.Default().Info("maintenance", "startup gate: %s — engine verified, prewarm released", msg)
        g.finish(PhaseReadyForPrewarm, "updated", msg)
}

// maintenanceLatestTag resolves the newest release (test seam aware).
func (s *Server) maintenanceLatestTag(ctx context.Context) (string, error) {
        if s.seams != nil && s.seams.latestTag != nil {
                return s.seams.latestTag(ctx)
        }
        return updater.LatestTag(ctx)
}

// maintenancePerformUpdate runs the engine update through the ONE
// updater authority (test seam aware).
func (s *Server) maintenancePerformUpdate(
        ctx context.Context,
        eng updater.Engine,
        tag string,
        onProgress func(downloader.Progress),
) (string, error) {
        if s.seams != nil && s.seams.performUpdate != nil {
                return s.seams.performUpdate(ctx, eng, tag, onProgress)
        }
        return updater.UpdateEngineWithProgress(ctx, s.src.Load(), eng, tag, onProgress)
}

// maintenanceValidateInstalled statically validates the engine binary
// the next start would launch (test seam aware).
func (s *Server) maintenanceValidateInstalled() error {
        if s.seams != nil && s.seams.validateInstalled != nil {
                return s.seams.validateInstalled()
        }

        cfg := s.src.Load()
        bin := cfg.LlamaBinPath
        if strings.TrimSpace(bin) == "" {
                bin = updater.EngineBinaryPath(cfg)
        }

        if _, err := engcheck.StaticValidate(bin); err != nil {
                return err
        }
        return nil
}

// prewarmAfterGate releases the model prewarm strictly after the gate.
// MODEL-FIRST: with no selected model nothing is ever started here —
// the Model Selector owns the next step (spec §2 Case D).
func (s *Server) prewarmAfterGate() {
        g := s.gateValue()
        if g == nil {
                // No gate armed (EnsureSetup not run / schedule-independent
                // caller) — nothing to order.
                return
        }

        go func() {
                <-g.Done()

                st := g.Snapshot()

                if !st.PrewarmPermitted {
                        logging.Default().Error("engine",
                                "startup prewarm REFUSED by the maintenance gate (phase %s, reason %s): %s",
                                st.Phase, st.Reason, st.Detail)
                        return
                }

                cfg := s.src.Load()

                if !cfg.LlamaAutoStart || s.stack == nil {
                        return
                }

                if strings.TrimSpace(cfg.Model) == "" {
                        logging.Default().Info("engine",
                                "no model selected — engine prewarm deferred; the Model Selector owns the next step")
                        return
                }

                s.prewarmOnce.Do(func() {
                        if s.prewarmHook != nil {
                                s.prewarmHook()
                                return
                        }
                        s.stack.PrewarmLLM()
                })
        }()
}

// handleMaintenance serves GET /api/maintenance — the explicit startup
// maintenance state for telemetry/UI (spec §4: "expose explicit state").
func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
        g := s.gateValue()
        if g == nil {
                writeJSON(w, MaintenanceStatus{
                        Phase:             PhaseDeferred,
                        Reason:            "not-armed",
                        PrewarmPermitted:  true,
                        StartedAt:         time.Now(),
                })
                return
        }
        writeJSON(w, g.Snapshot())
}

// humanDueWhen renders the last-check stamp for the DEFERRED detail.
func humanDueWhen(t time.Time) string {
        if t.IsZero() {
                return "never"
        }
        return t.Format(time.RFC3339)
}
