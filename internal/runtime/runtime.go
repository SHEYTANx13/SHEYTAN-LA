// Package runtime wires the full SHEYTAN agent stack: LLM client,
// orchestrator with every built-in tool, attachments, context cache,
// memory, multi-agent layer, research, and the llama.cpp subprocess
// manager. Both the desktop GUI and the headless `ask` CLI build on this
// so they stay feature-identical.
package runtime

import (
        "context"
        "fmt"
        "net/http"
        "path/filepath"
        "sync"
        "sync/atomic"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/aisystem"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/goal"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/artifacts"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/attachments"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/contextcache"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/ctxtelemetry"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/governor"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/lab"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/memmanager"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/memory"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/multiagent"
        nativeengine "github.com/Parsaetak/SHEYTAN-local-agent/internal/native/engine"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/projectintel"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/recall"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/repoindex"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/research"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sandbox"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/scheduler"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// Stack is the fully-wired agent runtime.
type Stack struct {
        // Src is the live, concurrency-safe configuration source shared by
        // every component that reads config at runtime (client, orchestrator,
        // engine, API handlers). Values obtained from Load() are immutable.
        Src *config.Source

        // Cfg is the configuration the stack was CONSTRUCTED with (v1.1.4:
        // historical field kept for construction-time consumers; live reads
        // must go through Src).
        Cfg    *config.Config
        Client *llm.Client
        Orch   *agent.Orchestrator
        Multi  *multiagent.MultiAgent

        // clientStream is the llama.cpp generation path (the client's
        // StreamChatDetailed), swappable for tests. Set in NewStack.
        clientStream func(ctx context.Context, req *llm.ChatRequest,
                onEvent func(llm.StreamEvent) error) (llm.PerfStats, error)
        Mem     *memory.Store
        Llama   *llm.LlamaServer
        Browser *tools.BrowserTool
        Sandbox *sandbox.CodeExecSandbox
        Recall  *recall.Engine

        // Sessions (v1.2.4) is the process-wide session store. Previously
        // each consumer built its own store over the same directory; one
        // shared instance means one hot cache for the memory manager to
        // bound and one place for the API layer to read from.
        Sessions *sessions.Store

        // Native (v1.1.5 Phase 1) is the supervised SHEYTAN native engine.
        // nil unless cfg.EngineBackend == "native" at construction: the
        // native path is an explicit opt-in. Since Phase 5 the native
        // engine performs REAL generation for validated llama-architecture
        // models (plain-text requests); llama.cpp remains the fallback for
        // everything the native path does not support.
        Native *nativeengine.Engine

        // llamaBackend / nativeBackend adapt the engines to the
        // llm.Backend contract (selection seam).
        llamaBackend  llm.Backend
        nativeBackend llm.Backend

        // Lab is the autonomous Coding Lab tool.
        Lab *lab.Tool

        // Research is the unified external research service.
        Research *research.Service

        // ResearchTool is the agent-facing research tool backed by Research.
        ResearchTool *research.Tool

        // Attachments is the staged-file store backing real file uploads.
        Attachments *attachments.Manager

        // Cache is the process-wide content-aware context cache.
        Cache *contextcache.Cache

        // Phase 7 additions ---------------------------------------------------

        // Skills is the persisted, load-on-demand skills store (validated
        // learning only — see internal/skills).
        Skills *skills.Store

        // Intel (v1.2.4) is the per-project measured-facts store. Exposed on
        // the stack so the Workspace surface can report project health and
        // so a workspace switch can re-observe the new root.
        Intel *projectintel.Store

        // RepoIndex (v1.3.4, ROADMAP v1.4 slice 1) is the persistent
        // repository index (symbols / dependency graph / test links /
        // hybrid search). Exposed so the Workspace surface can report
        // index state and so a workspace switch can refresh the index.
        RepoIndex *repoindex.Store

        // Systems (v1.9.0) is the ONE AI System authority: user-owned,
        // revisioned run configurations with a persisted active pointer.
        // The API layer freezes the active snapshot into every run;
        // in-flight runs never observe mid-run edits (value semantics).
        Systems *aisystem.Store

        // Goals (v1.9.0) is the ONE durable long-horizon Goal store. The
        // engine (internal/goal) drives runs through the orchestrator via
        // an injected executor; the store owns checkpoints, approvals and
        // boot recovery (never falsely running after a restart).
        Goals *goal.Store

        // Telemetry records per-turn context-effectiveness measurements.
        Telemetry *ctxtelemetry.Store

        // Sched is the local autonomous event/task foundation (manual /
        // startup / timer triggers in this release).
        Sched *scheduler.Scheduler

        // v1.7.0: the Automation / Tasks authorities — task-scoped custom
        // tools, the durable task/run artifact registry and the Markdown
        // SKILL.md layer (progressive disclosure). One instance each, shared
        // with the API surface.
        TaskTools      *customtools.TaskStore
        Artifacts      *artifacts.TaskRegistry
        MarkdownSkills *skills.MarkdownStore

        // Linux (v1.0.6) is the built-in Linux-like shell used by BOTH the agent
        // (the `linux` tool) and the Terminal view — one shared instance so the
        // user sees (and can replay) exactly what the agent did.
        Linux *tools.LinuxSim

        // MemMgr (v1.2.4) coordinates the runtime memory policy: registered
        // cache trims, run-boundary cleanup, pressure-triggered eviction and
        // measured reclamation telemetry (see internal/memmanager).
        MemMgr *memmanager.Manager

        // recoveryStore (v1.7.1) is the ONE durable store for context-
        // exhaustion handoff records under <DataDir>/recovery.
        recoveryStore *recovery.Store

        // liveMonitor (v1.7.1) is the bounded live resource monitor; the
        // activeRun* fields own cooperative protection cancellation.
        liveMonitor        *preflight.LiveMonitor
        activeRuns         atomic.Int64
        activeRunMu        sync.Mutex
        activeRunSeq       int64
        activeRunCancelFns map[int64]context.CancelFunc

        // governor (v1.8.0) is the ONE runtime policy authority: it folds
        // the monitor's accepted samples into the resource state and owns
        // the envelope/admission/self-model policy. It EXECUTES nothing —
        // every action stays with its existing subsystem authority.
        governor   *governor.Governor
        governorMu sync.Mutex

        // memStop ends the manager's idle maintenance loop.
        memStop chan struct{}

        // browserMu guards the lazy BrowserTool cache.
        browserMu sync.Mutex

        // -------------------------------------------------------------------
        // v1.2.5 deterministic lifecycle ownership. Every background worker
        // the Stack creates is OWNED: bound to lifeCtx and registered in
        // lifeWG (or awaited through its own done channel). Close() cancels
        // the context, waits for the owned workers, and only then tears down
        // engines and flushes state — so Close() returning means NO
        // Stack-owned goroutine can still write to Stack-owned filesystem
        // state (the TempDir-cleanup race class is closed by construction).
        // -------------------------------------------------------------------
        lifeCtx    context.Context
        lifeCancel context.CancelFunc
        lifeWG     sync.WaitGroup
        closeOnce  sync.Once

        // lifeMu guards the loop bookkeeping below (Start* vs Close races).
        lifeMu sync.Mutex

        // nativePrewarmDone closes when the in-flight native prewarm
        // (start + model load) has fully settled — success or failure.
        // v1.6.0 repair: the run gate (EnsureLLMContext) waits on it so a
        // cold-start run converges with the ALREADY-BOOTING native engine
        // instead of failing through the llama.cpp fallback while the armed
        // backend is seconds from ready. Guarded by lifeMu.
        nativePrewarmDone chan struct{}

        // memDone / schedDone close when the memory-manager loop and the
        // scheduler loop have fully exited after a Close.
        memDone   chan struct{}
        schedDone chan struct{}

        // nativeFallbackMu guards the last native-selection fallback
        // reason (v1.3.2 observable-fallback contract): when the user
        // selected the native engine but the selection policy had to route
        // generation to llama.cpp, the reason is recorded here once per
        // generation request and surfaced through /api/engine — the
        // fallback is documented behavior, but never a SILENT one.
        nativeFallbackMu    sync.Mutex
        nativeFallbackLast  string
        nativeFallbackCount int
}

// NewStack wires every tool into the orchestrator. The sandbox is optional —
// if the Job-Object sandbox can't be created, the plain codeExec tool stays
// registered. SandboxEnabled (v1.1.4, default true) gates the override: the
// setting was previously stored but never read — a meaningless toggle.
func NewStack(cfg *config.Config) *Stack {
        src := config.NewSource(cfg)

        // v1.2.5: the lifecycle context every owned background worker binds
        // to. Close() cancels it and waits for the owned workers.
        lifeCtx, lifeCancel := context.WithCancel(context.Background())

        client := llm.NewClient(src)
        orch := agent.New(src, client)

        // v1.1.3: content-aware context cache shared by attachments, chunking
        // pipelines and retrieval.
        cache := contextcache.New()

        // v1.1.3: real attachment staging under the app's private data dir.
        attMgr, attErr := attachments.NewManager(
                filepath.Join(cfg.DataDir, "attachments"),
                attachments.Options{Cache: cache},
        )

        if attErr != nil {
                logging.Default().Warn(
                        "runtime",
                        "attachment store unavailable: %v",
                        attErr,
                )
        }

        // v1.0.1: materialize AI-CONTEXT.md in the app folder.
        if path, err := aicontext.EnsureFile(
                cfg.DataDir,
        ); err != nil {
                logging.Default().Warn(
                        "runtime",
                        "AI context file: %v",
                        err,
                )
        } else {
                logging.Default().Info(
                        "runtime",
                        "AI context file: %s",
                        path,
                )
        }

        // Canonical base dir for every tool.
        tools.SetBaseDir(cfg.DataDir)

        // Core tools.
        orch.Register(tools.Shell{})
        orch.Register(tools.Files{})
        orch.Register(tools.CodeExec{})
        orch.Register(tools.Git{})
        orch.Register(tools.NewBrowserTool(cfg))
        orch.Register(tools.NewDataTool(cfg))

        // v1.0.10 (PRISM): structured data, archives, URLs, verification.
        orch.Register(tools.JSONTool{})
        orch.Register(tools.ArchiveTool{})
        orch.Register(tools.NewFetchTool())
        orch.Register(tools.DiffTool{})

        // v1.0.6: vision + terminal.
        llamaSrv := llm.NewLlamaServer(src)

        // v1.1.5 Phase 1: SHEYTAN Native Engine architecture. The native
        // engine exists ONLY behind the explicit "native" opt-in; the
        // default ("llama") preserves v1.1.4 behavior byte-for-byte.
        // Even when selected, generation still runs on llama.cpp until
        // the native engine implements it (see Stack.Engine).
        var nativeEng *nativeengine.Engine
        var nativeBack *nativeengine.Backend

        if cfg.NativeBackendEnabled() {
                hostPath := nativeengine.DefaultHostPath(
                        cfg.DataDir,
                        cfg.NativeEnginePath,
                )

                nativeEng = nativeengine.New(hostPath)
                nativeBack = nativeengine.NewBackend(nativeEng)

                if !nativeEng.Available() {
                        logging.Default().Warn(
                                "runtime",
                                "native engine selected but host binary not found at %s — build native/engine (CMake) or set nativeEnginePath; llama.cpp remains the engine",
                                hostPath,
                        )
                }
        }

        llamaBack := llm.NewLlamaBackend(llamaSrv, client)

        // v1.1.5 Phase 5: the backend-aware generation router. The
        // orchestrator's loop calls this seam instead of the client
        // directly, so generation actually flows through
        // llm.SelectGenerationBackend: the native engine when the user
        // selected it AND it can generate (llama graph validated at load
        // time); llama.cpp otherwise. Request shapes the native path
        // cannot serve (tools, images) and pre-first-token native
        // failures fall back to llama.cpp with an explicit, logged,
        // inspectable reason — the Phase 5 activation contract.
        stack := &Stack{
                Src:           src,
                Cfg:           cfg,
                Client:        client,
                Orch:          orch,
                Llama:         llamaSrv,
                Native:        nativeEng,
                llamaBackend:  llamaBack,
                nativeBackend: nativeBack,
                lifeCtx:       lifeCtx,
                lifeCancel:    lifeCancel,
        }
        stack.clientStream = client.StreamChatDetailed
        orch.SetGenerationStream(stack.streamGeneration)

        // v1.2.4: one shared session store + the runtime memory manager.
        // The manager owns coordinated cleanup: bounded caches stay bounded
        // by themselves; the manager sheds their cold tail on run boundaries
        // and under memory pressure, with before/after + duration telemetry.
        stack.Sessions = sessions.New(cfg.SessionsDir)
        memMgr := memmanager.New()
        stack.MemMgr = memMgr

        // v1.7.1: the context-exhaustion recovery authority — durable
        // handoff records + the coordinator seam the orchestrator calls.
        stack.recoveryStore = recovery.NewStore(recoveryDir(cfg))
        orch.SetRecoveryCoordinator(newRecoveryCoordinator(stack))
        stack.activeRunCancelFns = map[int64]context.CancelFunc{}
        stack.StartLiveMonitor()
        // v1.8.0: the Runtime Governor — the ONE runtime policy authority
        // over the monitor's accepted samples (envelope, admission,
        // self-model). Policy only; execution stays with the subsystems.
        stack.StartGovernor()
        memMgr.RegisterTrim("sessions-hot", func() int64 {
                return stack.Sessions.TrimHot(1) // keep the most recent session
        })
        memMgr.RegisterTrim("image-cache", func() int64 {
                return client.TrimImageCache()
        })

        // ---------------------------------------------------------------------
        // Phase 7 wiring: model capabilities → planner, dynamic toolsets →
        // orchestrator, skills, context telemetry, programmatic pipelines.
        // ---------------------------------------------------------------------

        // Model-aware context: the effective context is min(configured,
        // GGUF model limit, engine-reported limit). Both engines report
        // here: the native engine reports its loaded model's limit, and
        // llama.cpp reports its STARTUP-VERIFIED window (from /props) while
        // the subprocess is alive (1.1.6). A dead engine reports 0 so a
        // stale window from a previous boot can never clamp the next plan.
        orch.SetContextLimitProvider(func(c *config.Config) int {
                limit := 0

                if stack.Llama != nil {
                        if v := stack.Llama.EngineContextLimit(); v > 0 {
                                limit = v
                        }
                }

                if stack.Native != nil {
                        if res, err := stack.Native.ModelInfo(context.Background()); err == nil &&
                                res.Loaded && res.Model != nil && res.Model.ContextLength > 0 {
                                native := int(res.Model.ContextLength)
                                if limit == 0 || native < limit {
                                        limit = native
                                }
                        }
                }

                return limit
        })

        // 1.1.6 §5 per-agent context: the multi-agent runner resolves each
        // specialist's context policy against the same model/engine limits
        // the orchestrator plans with — one source of truth, no agent gets a
        // window the machine cannot serve.
        if stack.Multi != nil {
                stack.Multi.ModelLimitsFn = func() (modelMax, engineMax int) {
                        c := src.Load()
                        engineMax = 0
                        if stack.Llama != nil {
                                if v := stack.Llama.EngineContextLimit(); v > 0 {
                                        engineMax = v
                                }
                        }
                        if stack.Native != nil {
                                if res, err := stack.Native.ModelInfo(context.Background()); err == nil &&
                                        res.Loaded && res.Model != nil && res.Model.ContextLength > 0 {
                                        native := int(res.Model.ContextLength)
                                        if engineMax == 0 || native < engineMax {
                                                engineMax = native
                                        }
                                }
                        }
                        if !c.IsRemote() {
                                if modelPath, err := llm.ResolveModelPath(c.ModelsDir, c.Model); err == nil {
                                        if caps := llm.ResolveModelCapabilities(c, modelPath); caps != nil {
                                                modelMax = caps.ContextLength
                                        }
                                }
                        }
                        return modelMax, engineMax
                }
        }

        // Skills: load-on-demand injection of VALIDATED procedures only.
        skillStore := skills.NewStore(cfg.DataDir)
        if err := skillStore.Load(); err != nil {
                logging.Default().Warn("runtime", "skills store: %v", err)
        }
        stack.Skills = skillStore
        orch.SetSkillSource(skillStore)

        // Context-effectiveness telemetry (bounded JSONL under DataDir).
        telemetryStore := ctxtelemetry.NewStore(cfg.DataDir)
        stack.Telemetry = telemetryStore
        orch.SetTelemetry(telemetryStore)
        // v1.2.4: run-boundary cleanup persists coalesced telemetry records.
        memMgr.RegisterTrim("ctxtelemetry", func() int64 {
                telemetryStore.Flush()
                return 0
        })

        // Programmatic tool pipelines: the model declares a bounded stage
        // plan once; the runtime executes it deterministically.
        orch.Register(agent.NewPipelineTool(orch, src))

        // Scheduler foundation: manual / startup / timer triggers; the run
        // seam goes through the orchestrator so every scheduled run keeps the
        // full reliability and verification machinery.
        sched := scheduler.New(
                filepath.Join(cfg.DataDir, "scheduler"),
                taskRunner(stack, orch, scheduleRunner(orch)),
                func(task scheduler.Task, report scheduler.Report) {
                        summary := fmt.Sprintf("Scheduled task %q (%s): ok=%v in %s",
                                task.Name, report.Trigger, report.OK,
                                time.Duration(report.DurationMs)*time.Millisecond)
                        _ = stack.Mem.Append([]string{"scheduler", task.ID}, summary, "scheduler")
                },
        )
        if err := sched.Load(); err != nil {
                logging.Default().Warn("runtime", "scheduler tasks: %v", err)
        }
        stack.Sched = sched

        // v1.7.0: wire the automation layer (task-scoped tools/artifacts/
        // skills + the genuine startup/file_change emitters) AFTER the
        // scheduler exists.
        wireAutomation(stack)

        orch.Register(tools.Screenshot{})

        linuxSim := tools.NewLinuxSim(
                cfg.DataDir,
        )

        orch.Register(linuxSim)

        // Autonomous Coding Lab.
        var labTool *lab.Tool

        // v1.1.5 Phase 6: persistent project intelligence. One store per
        // install, keyed per project root. The workspace gets observed at
        // startup (bounded walk) and the card is injected per run; Lab
        // verification outcomes record VERIFIED build/test commands against
        // the task's source project.
        intel := projectintel.NewStore(
                cfg.DataDir + "/projectintel",
        )
        stack.Intel = intel

        // v1.2.5: the initial workspace observation (a bounded filesystem walk)
        // moved OFF the startup critical path — the first chat must not wait
        // for project indexing. The card renders empty until the background
        // pass lands (typically well under a second); escalation and later
        // runs pick it up normally.
        //
        // v1.2.5 lifecycle ownership: this worker is OWNED — registered in
        // lifeWG so Close() waits for it and no observe write can race the
        // data-dir teardown.
        stack.lifeWG.Add(1)

        go func() {
                defer stack.lifeWG.Done()

                if _, err := intel.Observe(cfg.WorkspaceDir()); err != nil {
                        logging.Default().Warn(
                                "runtime",
                                "project intelligence observe (background): %v",
                                err,
                        )
                        return
                }
                logging.Default().Info(
                        "runtime",
                        "project intelligence ready (background observe): %s",
                        cfg.WorkspaceDir(),
                )
        }()

        if cfg.LabEnabled {
                var err error

                labTool, err = lab.NewTool(cfg)

                if err != nil {
                        logging.Default().Warn(
                                "runtime",
                                "Coding Lab unavailable: %v",
                                err,
                        )
                } else {
                        labTool.SetIntel(intel)
                        orch.Register(labTool)

                        logging.Default().Info(
                                "runtime",
                                "Coding Lab registered: workspace=%s network=%t",
                                cfg.LabWorkspaceRoot,
                                cfg.LabAllowNetwork,
                        )
                }
        }

        // Project card injection: measured facts before every run. The root
        // is read LIVE (v1.2.4): switching the workspace in the UI moves the
        // card to the new project on the very next run, no restart.
        orch.SetProjectCard(func() string {
                return intel.Card(src.Load().EffectiveWorkspaceRoot())
        })

        // repoEvidenceTokenBudget bounds the repository-evidence context
        // block (same optional-block class as the recall block).
        const repoEvidenceTokenBudget = 400

        // v1.3.4 (ROADMAP v1.4 slice 1): persistent repository intelligence.
        // One store per install, keyed per workspace root, persisted under
        // <DataDir>/repoindex/. The initial update runs as an OWNED
        // background worker (bounded incremental pass — the first chat never
        // waits for it); the repo_search tool and the repo-evidence context
        // block read the root LIVE so a workspace switch applies instantly.
        repoIdx := repoindex.NewStore(
                cfg.DataDir + "/repoindex",
        )
        stack.RepoIndex = repoIdx

        // v1.9.0: the ONE AI System store — user-owned, revisioned run
        // configurations (instructions/model/reasoning/tool surface/
        // approval policy). Persisted under <DataDir>/ai-systems/; a
        // fresh install gets the default system whose behavior is the
        // pre-v1.9 runtime behavior exactly. Runs freeze the active
        // snapshot at start (api.handleRun -> agent.WithAISystem).
        // The store open cannot fail in practice (the directory is created
        // under the already-created data root); a failure degrades to the
        // default system lazily through the API layer instead of killing
        // the whole stack — the runtime never refuses to boot over the
        // AI System store.
        if systems, sysErr := aisystem.Open(cfg.DataDir); sysErr == nil {
                stack.Systems = systems
        }
        // v1.9.0: the durable Goal store — boot recovery marks any goal
        // found live with no run behind it as paused (never falsely
        // running); terminal goals stay terminal.
        if goals, goalErr := goal.Open(cfg.DataDir); goalErr == nil {
                stack.Goals = goals
                if _, recErr := goals.RecoverOnBoot(); recErr != nil {
                        logging.Default().Warn("goals", "boot recovery: %v", recErr)
                }
        }

        memMgr.RegisterTrim("repoindex-cache", func() int64 {
                return repoIdx.TrimCache()
        })

        stack.lifeWG.Add(1)

        go func() {
                defer stack.lifeWG.Done()

                report, err := repoIdx.Update(stack.lifeCtx, src.Load().EffectiveWorkspaceRoot())
                if err != nil {
                        logging.Default().Warn(
                                "runtime",
                                "repository index (background): %v",
                                err,
                        )
                        return
                }
                logging.Default().Info(
                        "runtime",
                        "repository index ready (background): %d files, %d re-parsed, partial=%t",
                        report.Indexed, report.Reindexed, report.Partial,
                )
        }()

        orch.Register(repoindex.NewTool(repoIdx, func() string {
                return src.Load().EffectiveWorkspaceRoot()
        }))

        // v1.9.0: deterministic repository NAVIGATION — open / navigate /
        // read / grep over identified sources through the SAME index and
        // the SAME path-safety authority as repo_search (spec §7: the
        // search → inspect → refine → open → navigate → read/grep →
        // verify workflow). Bounded ranges, provenance, truncation state.
        orch.Register(repoindex.NewNavTool(repoIdx, func() string {
                return src.Load().EffectiveWorkspaceRoot()
        }))

        // Repository evidence injection: targeted, evidence-backed file
        // suggestions for the current task, ranked by the repoindex hybrid
        // scorer. The root is read LIVE (same contract as the project card).
        orch.SetRepoEvidence(func(task string) string {
                return repoIdx.EvidenceBlock(
                        src.Load().EffectiveWorkspaceRoot(),
                        task,
                        repoEvidenceTokenBudget,
                )
        })

        // Unified external research.
        var researchService *research.Service
        var researchTool *research.Tool

        if cfg.ResearchEnabled {
                researchConfig := research.ServiceConfig{
                        Backend:    cfg.ResearchBackend,
                        MaxResults: cfg.ResearchMaxResults,
                        Timeout: researchTimeout(
                                cfg.ResearchTimeoutSec,
                        ),
                }

                researchService = research.NewService(
                        researchConfig,
                )

                researchHTTPClient := &http.Client{
                        Timeout: researchTimeout(
                                cfg.ResearchTimeoutSec,
                        ),
                }

                researchCacheTTL := researchCacheTTL(
                        cfg.ResearchCacheTTLMin,
                )

                if cfg.ResearchGitHub {
                        var githubProvider research.Provider

                        githubProvider = research.NewGitHubProvider(
                                researchHTTPClient,
                                "",
                                "",
                        )

                        githubProvider = research.NewCachedProvider(
                                githubProvider,
                                researchCacheTTL,
                        )

                        if err := researchService.Register(
                                githubProvider,
                        ); err != nil {
                                logging.Default().Warn(
                                        "research",
                                        "GitHub provider unavailable: %v",
                                        err,
                                )
                        } else {
                                logging.Default().Info(
                                        "research",
                                        "GitHub provider registered",
                                )
                        }
                }

                if cfg.ResearchReddit {
                        var redditProvider research.Provider

                        redditProvider = research.NewRedditProvider(
                                researchHTTPClient,
                                "",
                                "",
                                cfg.ResearchUserAgent,
                        )

                        redditProvider = research.NewCachedProvider(
                                redditProvider,
                                researchCacheTTL,
                        )

                        if err := researchService.Register(
                                redditProvider,
                        ); err != nil {
                                logging.Default().Warn(
                                        "research",
                                        "Reddit provider unavailable: %v",
                                        err,
                                )
                        } else {
                                logging.Default().Info(
                                        "research",
                                        "Reddit provider registered",
                                )
                        }
                }

                if cfg.ResearchWeb {
                        var duckDuckGoProvider research.Provider

                        duckDuckGoProvider =
                                research.NewDuckDuckGoProvider(
                                        researchHTTPClient,
                                        "",
                                )

                        duckDuckGoProvider = research.NewCachedProvider(
                                duckDuckGoProvider,
                                researchCacheTTL,
                        )

                        if err := researchService.Register(
                                duckDuckGoProvider,
                        ); err != nil {
                                logging.Default().Warn(
                                        "research",
                                        "DuckDuckGo provider unavailable: %v",
                                        err,
                                )
                        } else {
                                logging.Default().Info(
                                        "research",
                                        "DuckDuckGo provider registered",
                                )
                        }
                }

                if cfg.ResearchSearXNGURL != "" {
                        var searxngProvider research.Provider

                        searxngProvider =
                                research.NewSearXNGProvider(
                                        researchHTTPClient,
                                        cfg.ResearchSearXNGURL,
                                )

                        searxngProvider = research.NewCachedProvider(
                                searxngProvider,
                                researchCacheTTL,
                        )

                        if err := researchService.Register(
                                searxngProvider,
                        ); err != nil {
                                logging.Default().Warn(
                                        "research",
                                        "SearXNG provider unavailable: %v",
                                        err,
                                )
                        } else {
                                logging.Default().Info(
                                        "research",
                                        "SearXNG provider registered: %s",
                                        cfg.ResearchSearXNGURL,
                                )
                        }
                }

                tool, err := research.NewTool(
                        researchService,
                )

                if err != nil {
                        logging.Default().Warn(
                                "research",
                                "research tool unavailable: %v",
                                err,
                        )
                } else {
                        researchTool = tool

                        orch.Register(researchTool)

                        logging.Default().Info(
                                "research",
                                "unified research tool registered: backend=%s results=%d timeout=%s cache=%s providers=%v",
                                researchService.Backend(),
                                cfg.ResearchMaxResults,
                                researchTimeout(
                                        cfg.ResearchTimeoutSec,
                                ),
                                researchCacheTTL,
                                researchService.ProviderNames(),
                        )
                }
        }

        // Vision gate: the screenshot tool refuses politely when the engine
        // cannot see images.
        tools.VisionCheck = func() error {
                if cfg.IsRemote() {
                        return fmt.Errorf(
                                "the remote provider does not accept tool-result images — switch to the local engine with an mmproj projector, or attach the image to your message instead",
                        )
                }

                if !llamaSrv.VisionActive() {
                        if !cfg.VisionEnabled {
                                return fmt.Errorf(
                                        "vision is disabled in Settings — enable it and add an mmproj-*.gguf projector to the models folder",
                                )
                        }

                        return fmt.Errorf(
                                "no multimodal projector paired with the current model — drop a matching mmproj-*.gguf (e.g. mmproj-gemma-4-E2B-it-BF16.gguf) into the models folder and restart the engine",
                        )
                }

                return nil
        }

        // Memory + persistent recall.
        mem := memory.New(
                cfg.DataDir + "/memory.jsonl",
        )

        engine := recall.New(
                cfg.DataDir,
        )

        orch.Register(memory.Tool{
                Store: mem,
                RecallSearch: func(
                        query string,
                        k int,
                ) []string {
                        var lines []string

                        for _, c := range engine.Search(
                                query,
                                k,
                        ) {
                                lines = append(
                                        lines,
                                        formatCapsuleLine(c),
                                )
                        }

                        return lines
                },
        })

        if cfg.RecallEnabled {
                orch.SetRecaller(engine)

                // v1.2.5 lifecycle ownership: the backfill is an OWNED worker
                // — Close() waits for it, so its index writes can never race
                // the data-dir teardown.
                stack.lifeWG.Add(1)

                go func() {
                        defer stack.lifeWG.Done()

                        if err := engine.Backfill(
                                stack.Sessions,
                        ); err != nil {
                                logging.Default().Warn(
                                        "recall",
                                        "backfill: %v",
                                        err,
                                )
                        } else if n := engine.Count(); n > 0 {
                                logging.Default().Info(
                                        "recall",
                                        "index ready: %d past exchanges",
                                        n,
                                )
                        }
                }()
        }

        // Job-Object sandbox (overrides plain codeExec when available).
        // v1.1.4: the config's sandbox controls actually apply now —
        // SandboxEnabled gates registration, SandboxMemory/SandboxCPU feed the
        // governor (previously hardcoded 512 MB / 25% and the settings card did
        // nothing).
        var sb *sandbox.CodeExecSandbox

        if cfg.SandboxEnabled {
                var sbErr error

                sb, sbErr = sandbox.NewCodeExecSandbox(
                        cfg.EffectiveSandboxMemoryMB(),
                        cfg.EffectiveSandboxCPUPercent(),
                        cfg.SandboxDir(),
                )

                if sbErr == nil {
                        orch.Register(sb)
                } else {
                        logging.Default().Warn(
                                "runtime",
                                "Job-Object sandbox unavailable, using plain codeExec: %v",
                                sbErr,
                        )
                }
        } else {
                logging.Default().Info(
                        "runtime",
                        "Job-Object sandbox disabled by configuration — plain codeExec in use",
                )
        }

        multi := multiagent.NewMultiAgent(
                client,
                orch,
                mem,
                func() string { return src.Load().EffectiveModel() },
                cfg.EffectiveMultiAgentDepth(),
        )

        // v1.1.3: inference traffic reports engine busy state to the
        // authoritative state machine (no-op unless the local engine is
        // alive, so remote providers are unaffected).
        client.SetBusyHook(llamaSrv.MarkBusy)

        // (The Stack struct itself was constructed early so the
        // generation router could capture it; late-bound subsystems
        // attach here.)
        stack.Multi = multi
        stack.Mem = mem
        stack.Browser = nil
        stack.Sandbox = sb
        stack.Recall = engine
        stack.Lab = labTool
        stack.Research = researchService
        stack.ResearchTool = researchTool
        stack.Attachments = attMgr
        stack.Cache = cache
        stack.Linux = linuxSim

        return stack
}

// StartMemoryManager launches the idle maintenance loop of the runtime
// memory policy (v1.2.4): housekeeping runs ONLY while no agent run is
// active. The loop is OWNED (v1.2.5): its completion is tracked through
// memDone so Close() waits for the final pass to finish before returning.
func (s *Stack) StartMemoryManager(ctx context.Context) {
        if s == nil || s.MemMgr == nil {
                return
        }

        s.lifeMu.Lock()

        if s.memStop != nil {
                s.lifeMu.Unlock()

                return // already started — never leak a second loop
        }

        s.memStop = make(chan struct{})
        s.memDone = make(chan struct{})
        memStop, memDone := s.memStop, s.memDone
        s.lifeMu.Unlock()

        go func() {
                defer close(memDone)
                s.MemMgr.IdleLoop(memStop, 2*time.Minute)
        }()

        _ = ctx // reserved for future ctx-bound policies
}

// streamGeneration is the single backend-aware generation seam wired
// into the orchestrator (Phase 5). Policy (mirrors
// llm.SelectGenerationBackend + the honest activation rules):
//
//   - the native engine serves generation when selected (engineBackend
//     "native") AND its loaded model validated natively executable AND
//     the request is plain text (no tool schemas, no images);
//   - anything else runs on the llama.cpp client path;
//   - a native selection that cannot serve (engine down, no model, model
//     not natively executable) falls back to llama.cpp with the reason
//     LOGGED and recorded for /api/engine (v1.3.2: no silent fallback);
//   - a native failure BEFORE the first streamed token falls back to
//     llama.cpp with the reason logged (the llama retry discipline
//     applied to backend selection); a failure AFTER the first token
//     surfaces to the loop like any engine error.
func (s *Stack) streamGeneration(ctx context.Context, req *llm.ChatRequest,
        onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {

        // v1.7.1 live resource protection: every active generation registers
        // its cancellation ownership so critical pressure can reach the run
        // through cooperative cancellation (existing lifecycle, no killing).
        protCtx, protCancel := context.WithCancel(ctx)
        defer protCancel()
        protID := s.registerActiveRun(protCancel)
        defer s.unregisterActiveRun(protID)
        ctx = protCtx

        decision := llm.SelectGenerationBackendDetailed(
                s.Src.Load(),
                s.nativeBackend,
                s.llamaBackend,
        )

        if decision.FallbackReason != "" {
                // The user explicitly selected the native engine but it cannot
                // serve: record + log the reason so the routing decision is
                // observable (see NativeFallbackStatus).
                s.recordNativeFallback(decision.FallbackReason)

                logging.Default().Warn(
                        "native-engine",
                        "native engine selected but cannot serve generation (%s) — llama.cpp serving",
                        decision.FallbackReason,
                )
        }

        if decision.SelectedName != "native" {
                return s.clientStream(ctx, req, onEvent)
        }

        // Native selected: request-shape check (tools / images stay on
        // llama.cpp — documented Phase 5 limits, not silent behavior).
        hasTools := len(req.Tools) > 0
        hasImages := false
        for i := range req.Messages {
                if len(req.Messages[i].Images) > 0 {
                        hasImages = true
                        break
                }
        }
        if hasTools || hasImages {
                logging.Default().Info(
                        "native-engine",
                        "native generation skipped (request shape: tools=%v images=%v) — llama.cpp serving",
                        hasTools,
                        hasImages,
                )
                return s.clientStream(ctx, req, onEvent)
        }

        emitted := false
        wrapped := func(ev llm.StreamEvent) error {
                if ev.Content != "" || ev.Reasoning != "" {
                        emitted = true
                }
                return onEvent(ev)
        }

        perf, err := decision.Backend.StreamGenerate(ctx, req, wrapped)
        if err == nil {
                return perf, nil
        }

        if !emitted {
                // Pre-first-token failure: explicit, logged, inspectable
                // fallback (the llama.cpp path stays fully functional).
                logging.Default().Warn(
                        "native-engine",
                        "native generation failed before the first token (%v) — llama.cpp serving this request",
                        err,
                )
                return s.clientStream(ctx, req, onEvent)
        }

        // Mid-stream failure after content: surface like any engine
        // error (the loop's error handling owns it).
        return perf, err
}

// recordNativeFallback stores the latest native-selection fallback reason
// (v1.3.2 observable-fallback contract). Counted per request so the UI can
// show how often the explicit native selection was routed to llama.cpp.
func (s *Stack) recordNativeFallback(reason string) {
        s.nativeFallbackMu.Lock()
        defer s.nativeFallbackMu.Unlock()
        s.nativeFallbackLast = reason
        s.nativeFallbackCount++
}

// NativeFallbackStatus reports the observable native-selection fallback
// state: the most recent reason (empty when the native engine is serving or
// was never selected) and how many generation requests were routed to the
// llama.cpp fallback despite the native selection. Purely local reads —
// safe for the /api/engine poll path.
type NativeFallbackStatus struct {
        Reason string `json:"reason,omitempty"`
        Count  int    `json:"count"`
}

// NativeFallback reports the recorded fallback status (zero value when the
// native path is serving or not selected).
func (s *Stack) NativeFallback() NativeFallbackStatus {
        s.nativeFallbackMu.Lock()
        defer s.nativeFallbackMu.Unlock()
        return NativeFallbackStatus{
                Reason: s.nativeFallbackLast,
                Count:  s.nativeFallbackCount,
        }
}

// researchTimeout converts the configuration's seconds value
// into a safe service/client timeout.
func researchTimeout(seconds int) time.Duration {
        if seconds <= 0 {
                seconds = 20
        }

        return time.Duration(seconds) *
                time.Second
}

// researchCacheTTL converts the configured cache lifetime in minutes.
// Zero or negative values disable caching.
func researchCacheTTL(minutes int) time.Duration {
        if minutes <= 0 {
                return 0
        }

        return time.Duration(minutes) *
                time.Minute
}

// formatCapsuleLine renders one recall capsule for the memory
// tool's history action.
func formatCapsuleLine(
        c recall.Capsule,
) string {
        line := c.TS.Format(
                "2006-01-02",
        ) +
                " [" +
                c.SessionID +
                "]"

        if c.Title != "" {
                line += " " + c.Title
        }

        if c.Query != "" {
                line += "\n  asked: " + c.Query
        }

        if c.Answer != "" {
                line += "\n  outcome: " + c.Answer
        }

        return line
}

// BrowserTool returns the shared browser tool registered in the stack.
// (v1.1.4: the lazy cache is mutex-guarded — two concurrent callers could
// previously race the field write.)
func (s *Stack) BrowserTool() *tools.BrowserTool {
        s.browserMu.Lock()
        defer s.browserMu.Unlock()

        if s.Browser != nil {
                return s.Browser
        }

        for _, t := range s.Orch.Tools() {
                if bt, ok := t.(*tools.BrowserTool); ok {
                        s.Browser = bt
                        return bt
                }
        }

        return nil
}

// Engine (v1.1.5) returns the backend that must serve a generation
// request, applying the single selection policy (llm.SelectGenerationBackend):
// the native engine when the user selected it AND it can actually generate,
// otherwise the llama.cpp fallback. Since Phase 5 a validated llama-architecture
// model genuinely routes generation natively; anything unsupported (or not
// loaded) still resolves to the llama backend.
func (s *Stack) Engine() llm.Backend {
        return llm.SelectGenerationBackend(
                s.Src.Load(),
                s.nativeBackend,
                s.llamaBackend,
        )
}

// LlamaBackend exposes the llama.cpp backend adapter (contract access for
// diagnostics and tests).
func (s *Stack) LlamaBackend() llm.Backend { return s.llamaBackend }

// NativeBackend exposes the native engine backend adapter (nil unless the
// native path is enabled).
func (s *Stack) NativeBackend() llm.Backend {
        if s.nativeBackend == nil {
                return nil
        }
        return s.nativeBackend
}

// EnsureNativeReady brings the native engine to a USABLE state synchronously:
// start the host (idempotent — an already-started engine returns nil) and
// load the selected model natively with the load-time capability verdict.
//
// v1.1.5 repair: this was previously reachable ONLY through the launch
// prewarm (prewarmNative). The engine toggle started the host but never
// loaded a model, so a native-selected user who disabled auto-start and
// pressed "engine start" got an alive-but-incapable engine whose runs
// still gated on llama.cpp — the exact "control without a real path"
// defect class this repository forbids. The toggle now calls this same
// bounded seam, so there is ONE start+load implementation for both entry
// points.
//
// The returned error covers the ENGINE start only. Model-load outcomes
// (unresolvable path, load failure, incapable model) are logged and
// reflected in GenerationCapable() — llama.cpp keeps serving those, so
// they are fallback conditions, not engine-start failures.
func (s *Stack) EnsureNativeReady(ctx context.Context) error {
        if s.Native == nil {
                return fmt.Errorf("native engine path not enabled (engineBackend != \"native\")")
        }

        if !s.Native.IsAlive() {
                if err := s.Native.Start(ctx); err != nil {
                        logging.Default().Warn(
                                "native-engine",
                                "native engine did not start (llama.cpp remains the engine): %v",
                                err,
                        )
                        return err
                }
        }

        logging.Default().Info(
                "native-engine",
                "native engine host ready",
        )

        // Phase 5: load the selected model NATIVELY so generation can
        // actually flow through the native path (validate → map →
        // metadata → llama-graph verdict). A model that does not
        // validate stays loaded-but-incapable: generation keeps flowing
        // to llama.cpp with the reason recorded here (inspectable).
        cfg := s.Src.Load()
        if cfg.IsRemote() || cfg.Model == "" {
                return nil
        }

        resolved, rerr := llm.ResolveModelPath(cfg.ModelsDir, cfg.Model)
        if rerr != nil {
                logging.Default().Info(
                        "native-engine",
                        "native model load skipped (model %q not resolvable): %v",
                        cfg.Model,
                        rerr,
                )
                return nil
        }

        spec := llm.ModelSpec{Path: resolved}
        if err := s.NativeBackend().LoadModel(ctx, spec); err != nil {
                logging.Default().Warn(
                        "native-engine",
                        "native model load failed (%s) — llama.cpp serves generation: %v",
                        resolved,
                        err,
                )
                return nil
        }

        if gc, ok := s.NativeBackend().(llm.GenerationCapable); ok && gc.GenerationCapable() {
                logging.Default().Info(
                        "native-engine",
                        "native generation ACTIVE for %s (llama architecture validated; llama.cpp remains the fallback)",
                        filepath.Base(resolved),
                )
        } else {
                logging.Default().Info(
                        "native-engine",
                        "native model loaded but NOT generation-capable (%s) — llama.cpp serves generation; reason: %s",
                        filepath.Base(resolved),
                        s.Native.NativeGenerationReason(),
                )
        }

        return nil
}

// prewarmNative runs the native start+load sequence in the background
// (launch prewarm). Best-effort by design: native engine failures NEVER
// block or fail the llama.cpp path — they surface in the native engine
// state and logs instead.
func (s *Stack) prewarmNative() {
        if s.Native == nil {
                return
        }

        // v1.5.0 MODEL-FIRST: the native prewarm is an ENGINE+MODEL load —
        // without an explicit selection it must not start the host at all
        // (a model-less host process would still be an engine running before
        // selection, and the selection flow owns the next start).
        if s.Src.Load().Model == "" {
                return
        }

        // v1.6.0 repair: record the in-flight prewarm so EnsureLLMContext
        // can wait for it (bounded by the run gate's own deadline). Each
        // invocation records its own channel; a waiter that grabbed an older
        // channel still re-checks the engine state after it closes.
        done := make(chan struct{})
        s.lifeMu.Lock()
        s.nativePrewarmDone = done
        s.lifeMu.Unlock()

        // v1.2.5 lifecycle ownership: the prewarm is an OWNED worker bound
        // to the stack lifecycle context — Close() cancels it and waits.
        s.lifeWG.Add(1)

        go func() {
                defer s.lifeWG.Done()
                defer close(done)

                ctx, cancel := context.WithTimeout(
                        s.lifeCtx,
                        45*time.Second,
                )
                defer cancel()

                if err := s.EnsureNativeReady(ctx); err != nil {
                        return // already logged by EnsureNativeReady
                }
        }()
}

// EnsureLLM makes sure an LLM backend is reachable and ready:
//
//   - provider "local": boots the bundled llama.cpp server unless one is
//     already alive, and blocks until the model is actually serving
//   - provider "remote": nothing to boot — the endpoint is used as-is
//
// This is the ONE canonical engine gate: every inference path (desktop,
// serve, ask) funnels through it.
func (s *Stack) EnsureLLM() error {
        if s.Src.Load().IsRemote() {
                logging.Default().Info(
                        "runtime",
                        "remote provider active: %s (model %s)",
                        remoteBaseURL(s.Src.Load()),
                        s.Src.Load().EffectiveModel(),
                )

                return nil
        }

        // v1.5.0 MODEL-FIRST: an engine load requires an explicit selection.
        // Without one the gate fails honestly and immediately — the Model
        // Selector owns the next step; no arbitrary "first .gguf" is ever
        // loaded on the user's behalf.
        if s.Src.Load().Model == "" {
                return fmt.Errorf(
                        "%w — choose a model in the Model Selector first",
                        llm.ErrNoModelSelected,
                )
        }

        // Phase 5 repair: when the native engine is the backend actually
        // serving generation (selected AND generation-capable — the same
        // single selection policy that routes requests), the llama gate
        // is already satisfied: llama.cpp is a FALLBACK, not a
        // prerequisite, for native-serving users. This is what lets an
        // offline / llama-less native deployment actually run.
        if backend := s.Engine(); backend != nil && backend.Name() == "native" {
                return nil
        }

        // v1.1.5: bring the native engine up too when enabled (best-effort
        // — never blocks or fails the generation path).
        if s.Src.Load().NativeBackendEnabled() && s.Native != nil && !s.Native.IsAlive() {
                s.prewarmNative()
        }

        // v1.8.6 RESOURCE-AWARE RUN GATE: the ONE policy authority (the
        // Governor) decides whether the current envelope admits this
        // model load/generation boot BEFORE the engine starts. Unknown
        // Governor state falls through to the existing preflight gate.
        if err := s.GovernorAdmitsModelLoad(); err != nil {
                return err
        }

        if err := s.Llama.Start(); err != nil {
                return err
        }

        return nil
}

// PrewarmLLM boots the local engine in the background so a freshly
// launched application reaches a healthy model WITHOUT any user action
// (v1.1.3 acceptance: launch → engine starts automatically → ready).
// Failures are logged and reflected in the engine state — never fatal,
// because the user may only be browsing settings; a later explicit start
// or the first message retries through EnsureLLM.
//
// v1.5.0 MODEL-FIRST: the prewarm only happens when a model is
// EXPLICITLY selected. A fresh install (Model == "") reaches the Model
// Selector with the engine idle — discovery lists local GGUFs without
// loading any of them, and the engine/model load happens strictly after
// the user's selection. The old behavior (boot whichever .gguf sorts
// first) is gone.
func (s *Stack) PrewarmLLM() {
        if s.Src.Load().IsRemote() {
                logging.Default().Info(
                        "runtime",
                        "remote provider active: %s (model %s) — local engine not started",
                        remoteBaseURL(s.Src.Load()),
                        s.Src.Load().EffectiveModel(),
                )

                return
        }

        if s.Src.Load().Model == "" {
                logging.Default().Info(
                        "runtime",
                        "no model selected — engine prewarm deferred; the Model Selector owns the next step (local models are listed, none is loaded)",
                )

                return
        }

        // v1.1.5: supervised native engine (opt-in) — started alongside,
        // never fatal.
        s.prewarmNative()

        // v1.2.5 lifecycle ownership: the automatic engine boot is an OWNED
        // worker — Close() waits (bounded) for it, and the boot itself
        // honors a concurrent Stop() at its episode checkpoints.
        s.lifeWG.Add(1)

        go func() {
                defer s.lifeWG.Done()

                if err := s.Llama.Start(); err != nil {
                        logging.Default().Warn(
                                "engine",
                                "automatic startup failed (the agent will retry on first use): %v",
                                err,
                        )

                        return
                }

                logging.Default().Info(
                        "engine",
                        "local engine ready automatically (model %s)",
                        s.Src.Load().EffectiveModel(),
                )
        }()
}

// EnsureLLMContext is EnsureLLM with a deadline: the run path uses it so a
// cold start can never hang a request forever — the engine either becomes
// ready within the timeout or the request fails with a clear, visible
// error while the startup keeps progressing in the background.
func (s *Stack) EnsureLLMContext(ctx context.Context) error {
        if s.Src.Load().IsRemote() {
                return nil
        }

        if s.Llama.IsRunning() {
                return nil
        }

        // v1.5.0 MODEL-FIRST: fail fast and honestly when nothing is
        // selected — never boot an arbitrary model from the run gate.
        if s.Src.Load().Model == "" {
                return fmt.Errorf(
                        "%w — choose a model in the Model Selector first",
                        llm.ErrNoModelSelected,
                )
        }

        // Phase 5 repair: the native-serving early exit (see EnsureLLM).
        // Same single selection policy — native selected + actually
        // generation-capable — never a second, looser check.
        if backend := s.Engine(); backend != nil && backend.Name() == "native" {
                return nil
        }

        // v1.6.0 repair (the cold-start native race): when the native
        // backend is ENABLED but not serving YET, the boot prewarm may
        // still be converging (host start + model load). The run gate's
        // documented contract is to WAIT for a cold engine — bounded by the
        // caller's deadline — so wait for the in-flight prewarm to settle
        // and re-check the early exit BEFORE failing through the llama.cpp
        // fallback: "the agent will retry on first use" must actually hold
        // on first use. A settled-but-incapable native engine still falls
        // through to the llama.cpp path below (unchanged behavior).
        if s.Src.Load().NativeBackendEnabled() && s.Native != nil {
                s.lifeMu.Lock()
                prewarmDone := s.nativePrewarmDone
                s.lifeMu.Unlock()

                if prewarmDone != nil {
                        select {
                        case <-prewarmDone:
                                if backend := s.Engine(); backend != nil && backend.Name() == "native" {
                                        return nil
                                }
                        case <-ctx.Done():
                                return fmt.Errorf(
                                        "engine startup still in progress: %w",
                                        ctx.Err(),
                                )
                        }
                }
        }

        // v1.8.6 RESOURCE-AWARE RUN GATE (the run path's own copy of the
        // EnsureLLM gate — EnsureLLMContext does NOT delegate): the ONE
        // policy authority decides BEFORE the bounded boot starts.
        // Unknown Governor state falls through (preflight gate remains).
        if err := s.GovernorAdmitsModelLoad(); err != nil {
                return err
        }

        errCh := make(chan error, 1)

        // v1.2.5 lifecycle ownership: the bounded boot helper is an OWNED
        // worker; the request deadline (ctx) and the stack lifecycle both
        // bound it.
        s.lifeWG.Add(1)

        go func() {
                defer s.lifeWG.Done()
                errCh <- s.Llama.Start()
        }()

        select {
        case err := <-errCh:
                if err == nil {
                        return nil
                }

                // v1.6.0 repair (the cold-start native race, part 2): resolving
                // the llama.cpp fallback takes real time (binary lookup, release
                // checks) — during which the ARMED native engine (boot prewarm)
                // may well have converged. Re-check the native early exit once
                // before surfacing the failure: the run gate errors only when
                // BOTH backends have genuinely failed.
                if backend := s.Engine(); backend != nil && backend.Name() == "native" {
                        return nil
                }

                return err
        case <-ctx.Done():
                return fmt.Errorf(
                        "engine startup still in progress: %w",
                        ctx.Err(),
                )
        }
}

// remoteBaseURL renders the remote endpoint for logs (empty-safe).
func remoteBaseURL(cfg *config.Config) string {
        if cfg.RemoteBaseURL == "" {
                return "(unset)"
        }
        return cfg.RemoteBaseURL
}

// Close tears down every owned subprocess/handle.
//
// v1.2.5 deterministic lifecycle ownership — the shutdown ORDER is:
//
//      cancel the lifecycle context (ends ctx-bound workers)
//      → stop the memory-manager idle loop and WAIT for its exit
//      → stop the scheduler loop and WAIT for its exit
//      → wait (bounded) for every owned one-shot worker
//        (project-intel observe, recall backfill, engine prewarm)
//      → flush required state/telemetry
//      → stop engines/subprocesses (native first, then llama.cpp)
//      → return
//
// Close is idempotent (sync.Once) and RETURNING means no Stack-owned
// goroutine can still write to Stack-owned filesystem state — the class of
// TempDir RemoveAll races in tests is closed by construction, not by sleep.
func (s *Stack) Close() {
        if s == nil {
                return
        }

        s.closeOnce.Do(func() {
                // 1. Cancel the lifecycle context.
                if s.lifeCancel != nil {
                        s.lifeCancel()
                }

                // 2. Stop the memory-manager idle loop, then WAIT for the
                // loop goroutine to observe the stop and exit — a final
                // trim pass must not race the teardown.
                s.lifeMu.Lock()
                memStop, memDone := s.memStop, s.memDone
                schedDone := s.schedDone
                s.memStop = nil
                s.memDone = nil
                s.schedDone = nil
                s.lifeMu.Unlock()

                if memStop != nil {
                        close(memStop)
                }

                if memDone != nil {
                        select {
                        case <-memDone:
                        case <-time.After(3 * time.Second):
                        }
                }

                // 3. Stop the scheduler loop (it also watches lifeCtx) and
                // wait for any in-flight Tick to finish.
                if schedDone != nil {
                        select {
                        case <-schedDone:
                        case <-time.After(3 * time.Second):
                        }
                }

                // 4. Wait (bounded) for every owned one-shot worker. The
                // bound exists because a boot attempt can legitimately take
                // minutes; the engine teardown below is what actually stops
                // those (Llama.Stop aborts concurrent boots at their episode
                // checkpoints). Filesystem-writing workers (observe, backfill)
                // finish in well under a second.
                wgDone := make(chan struct{})

                go func() {
                        s.lifeWG.Wait()
                        close(wgDone)
                }()

                select {
                case <-wgDone:
                case <-time.After(15 * time.Second):
                }

                // 5. Flush required state: stop idle cleanup first, then
                // persist coalesced telemetry so a clean shutdown never
                // loses the coalescing window.
                if s.Telemetry != nil {
                        s.Telemetry.Flush()
                }

                // 6. Stop engines and subprocesses.
                if s.BrowserTool() != nil {
                        s.BrowserTool().Close()
                }

                if s.Sandbox != nil {
                        _ = s.Sandbox.Close()
                }

                // v1.1.5: stop the native engine FIRST (bounded) so its
                // teardown never waits behind the llama.cpp stop.
                if s.Native != nil {
                        stopCtx, cancel := context.WithTimeout(
                                context.Background(),
                                10*time.Second,
                        )
                        _ = s.Native.Stop(stopCtx)
                        cancel()
                }

                _ = s.Llama.Stop()
        })
}

// scheduleRunner adapts the orchestrator to the scheduler's Runner seam:
// one scheduled task = one bounded agent run with the full reliability,
// verification and telemetry machinery attached.
func scheduleRunner(orch *agent.Orchestrator) scheduler.Runner {
        return func(ctx context.Context, task scheduler.Task) (string, error) {
                if orch == nil {
                        return "", fmt.Errorf("scheduler: orchestrator unavailable")
                }
                return orch.Run(
                        ctx,
                        []llm.Message{{Role: "user", Content: task.Prompt}},
                        func(agent.Activity) {}, // scheduled runs publish no live stream
                )
        }
}

// StartScheduler launches the scheduler's timer loop (one Tick per
// minute). Cancellation of the returned context stops it. The loop is
// bounded by design: every fired task carries its own runtime budget.
//
// v1.2.5: the loop's exit is tracked through schedDone so Close() waits
// for any in-flight Tick before returning; the loop ends when the caller's
// ctx OR the stack lifecycle context is canceled.
func (s *Stack) StartScheduler(ctx context.Context) {
        if s == nil || s.Sched == nil {
                return
        }

        s.lifeMu.Lock()

        if s.schedDone != nil {
                s.lifeMu.Unlock()

                return // already started — never leak a second loop
        }

        s.schedDone = make(chan struct{})
        schedDone := s.schedDone
        s.lifeMu.Unlock()

        go func() {
                defer close(schedDone)

                ticker := time.NewTicker(time.Minute)
                defer ticker.Stop()

                for {
                        select {
                        case <-ctx.Done():
                                return
                        case <-s.lifeCtx.Done():
                                return
                        case <-ticker.C:
                                s.Sched.Tick(ctx)
                        }
                }
        }()
}
