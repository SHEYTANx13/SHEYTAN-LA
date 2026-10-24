# SHEYTAN Qt parity manifest — v1.11.0-fixed (Task ID 43, D28/I21)

ONE tracked inventory of the Wails (React) surface vs the Qt Quick surface.
The React tree under `src/` is the REFERENCE, never edited. Every row names
the Qt target file, the backing core API (all pre-existing handlers — the
Qt client adds NO backend endpoint), and the acceptance assertion. Status
values: `LANDED-LOCAL` (code + suite-verified locally; hosted Qt compile
and acceptance NOT RUN), `NOT RUN` (unported — honest gap, never faked).

## Navigation chrome (priority 1)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| nav-chrome (7 tabs: chat, agent, workspace, lab, automation, system, settings) | src/workspace.ts WORKSPACE_LAYERS | ui/qt/qml/Main.qml (left rail) | none (pure navigation) | navChromeSevenViewsLoad | LANDED-LOCAL |
| chat view (session list, transcript, live bubble, composer, stop) | src/App.tsx chat route | ui/qt/qml/Main.qml (pre-existing) | /api/sessions, /api/run, /api/run/stop, activity WS | qmlLoadsCleanly, chatStreamPersistRepeatAndClose | LANDED-LOCAL (pre-existing at HEAD) |

## Settings (priority 2 — all eight sections, every FIELD_PLACEMENT control)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| settings-general | src/settings-sections.ts (llamaAutoStart, smoothStream, showPerfHud, targetFps) | ui/qt/qml/SettingsView.qml | GET/POST /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-models | src/settings-sections.ts (provider, model, remoteBaseUrl, remoteModel, visionEnabled) | ui/qt/qml/SettingsView.qml | /api/config, /api/models | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-performance | src/settings-sections.ts (numCtx, historyWindowPct, continuumEnabled, performanceMode) | ui/qt/qml/SettingsView.qml | /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-agent | src/settings-sections.ts (maxIterations, parallelTools, thinkingMode, recallEnabled, recallTopK, enabledTools, labEnabled, labKeepWorkspaces, labAllowNetwork, sandboxEnabled) | ui/qt/qml/SettingsView.qml | /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-network | src/settings-sections.ts (researchEnabled, researchBackend, researchSearxngUrl, researchMaxResults, researchGitHub, researchReddit, researchWeb) | ui/qt/qml/SettingsView.qml | /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-updates | src/settings-sections.ts (updateSchedule) | ui/qt/qml/SettingsView.qml | /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-diagnostics | src/settings-sections.ts (read-only health) | ui/qt/qml/SettingsView.qml | /api/logs, /api/preflight | settingsEightSectionsAllFields | LANDED-LOCAL |
| settings-advanced | src/settings-sections.ts (every advanced field in FIELD_PLACEMENT) | ui/qt/qml/SettingsView.qml | /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| config read glue | src/api.ts request("/api") via refreshConfig | ui/qt/src/coreclient.cpp (stub implemented) | GET /api/config → config property | settingsEightSectionsAllFields | LANDED-LOCAL |

## Agent (priority 3)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| agent-view (sidebar, header, body, task panel) | src/AgentSidebar/AgentHeader/AgentBody/AgentTaskPanel.tsx | ui/qt/qml/AgentView.qml | /api/goals, /api/run, activity WS | agentViewLoads | LANDED-LOCAL |

## Workspace (priority 4)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| workspace-view (project, recents, session, runtime, quick actions) | src/WorkspacePanel.tsx | ui/qt/qml/WorkspaceView.qml | GET /api/workspace, POST /api/workspace/switch | workspaceViewLoads | LANDED-LOCAL |
| repository-index | src/RepositoryIndexCard.tsx | ui/qt/qml/WorkspaceView.qml (index panel) | GET /api/repo/index, POST /api/repo/index/refresh | workspaceViewLoads | LANDED-LOCAL |

## Lab (priority 5)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| lab-view (snapshots, run/control) | src/LabPanel.tsx | ui/qt/qml/LabView.qml | GET /api/lab, POST /api/lab | labViewLoads | LANDED-LOCAL |

## Automation (priority 6)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| automation-view (tasks, pause/resume/cancel, runs, artifacts) | src/AutomationPanel.tsx, src/AutomationArtifactViewer.tsx | ui/qt/qml/AutomationView.qml | GET/POST /api/automation/tasks(+/{id}/{action}) | automationViewLoads | LANDED-LOCAL |

## System (priority 7)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| system-view (preflight, governor, maintenance) | src/SystemPanel.tsx | ui/qt/qml/SystemView.qml | /api/preflight, /api/governor, /api/maintenance | systemViewLoads | LANDED-LOCAL |

## Extras (priority 8)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| history | src/HistoryPicker.tsx | ui/qt/qml/HistoryDrawer.qml | GET /api/history/search?q&mode&limit | extrasSurfacesLoad | LANDED-LOCAL |
| model-picker | src/ModelPicker.tsx | ui/qt/qml/ModelPickerDrawer.qml | GET /api/models, POST /api/models/select | extrasSurfacesLoad | LANDED-LOCAL |
| my-tools | src/MyToolsCard.tsx | ui/qt/qml/MyToolsDrawer.qml | GET /api/custom-tools | extrasSurfacesLoad | LANDED-LOCAL |
| log-viewer | src/LogViewer.tsx | ui/qt/qml/LogViewerDrawer.qml | GET /api/logs?limit | extrasSurfacesLoad | LANDED-LOCAL |
| animations (animationsEnabled — device-tier default, persisted user override) | src/settings-sections.ts (General row pattern) | ui/qt/qml/SettingsView.qml | GET /api/sysinfo (tier default) + local QSettings override (the core's /api/config is a typed struct that drops unknown keys — core-side persistence needs a Go field, out of scope this cycle) | animationsToggleGatesEveryMotion | LANDED-LOCAL |
| perf-strip | src/PerfStrip.tsx | ui/qt/qml/PerfStrip.qml | activity WS metrics | extrasSurfacesLoad | LANDED-LOCAL |
| ai-systems | src/AISystemsCard.tsx | ui/qt/qml/Main.qml systemsDrawer (pre-existing) | /api/systems | aiSystemsListActivate | LANDED-LOCAL (pre-existing at HEAD) |
| goals | src/GoalsCard.tsx | ui/qt/qml/Main.qml goalsDrawer (pre-existing) | /api/goals | goalsListFieldContract, goalsStartPauseResumeCancelRoundTrip, goalsApproveRejectParkedGoal | LANDED-LOCAL (pre-existing at HEAD) |

## Agent-view parity + shell interactions (priority 9 — run #47 cycle, Task ID 42, D37)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| activity-stream | src/ActivityStream.tsx (AgentBody "Show activity" toggle) | ui/qt/qml/AgentView.qml (activity stream panel) | activity WS (the bounded display ring — last 50 frames, the Wails MAX_VISIBLE_EVENTS contract — in ui/qt/src/coreclient.cpp; authoritative routing unchanged) | agentViewLoads | LANDED-LOCAL |
| download-progress | src/DownloadProgress.tsx (DownloadProgressPanel, v1.2.3 Download Manager) | ui/qt/qml/AgentView.qml (download panel) | GET /api/engine + POST /api/llama {"action":"cancel-download"} (both pre-existing handlers) | agentViewLoads | LANDED-LOCAL |
| maintenance-banner | src/MaintenanceBanner.tsx (v1.6.0 P0/P4 honest lifecycle) | ui/qt/qml/AgentView.qml (maintenance banner, envelope-guarded) | GET /api/maintenance (pre-existing) | agentViewLoads | LANDED-LOCAL |
| keyboard-shortcuts | src/shortcuts.ts (v1.2.4 global shortcuts + "?" help overlay) | ui/qt/qml/Main.qml (Shortcut elements Ctrl+1..7 + Ctrl+/ help toggle + Escape-gated overlay) | none (pure navigation) | navChromeSevenViewsLoad | LANDED-LOCAL |

## Run-#48 cycle (priority 10 — Task ID 43, the operator's four defects)

| surface | wails source | qt target | core api | acceptance assertion | status |
|---|---|---|---|---|---|
| model-select-live-path | src/ModelPicker.tsx ("Use model" → /api/models/select) | ui/qt/src/coreclient.cpp selectModel + ui/qt/qml/ModelPickerDrawer.qml | POST /api/models/select (the shipped selection state machine: persist + verify + engine boot) | modelSelectRoundTripPersistsAcrossRestart | LANDED-LOCAL |
| hardware-panel | src/SystemPanel.tsx DeviceCard + RuntimeCard | ui/qt/qml/SystemView.qml (Hardware + Engine panels) + ui/qt/src/coreclient.cpp (sysinfo property) | GET /api/sysinfo, GET /api/engine (both pre-existing) | hardwarePanelSurfacesSysinfo | LANDED-LOCAL |
| settings-nested-schema | src/settings-sections.ts + SettingsPanel.tsx ({llm: …} patches, bool/enum controls) | ui/qt/qml/SettingsView.qml (path-aware fieldValue/setField, llm.* bindings, type-correct controls) | GET/POST /api/config | settingsEightSectionsAllFields | LANDED-LOCAL |
| tool-catalog-enabledTools | src/settings-sections.ts enabledTools (per-tool checkboxes fed by /api/tools) | ui/qt/qml/SettingsView.qml (tools row) + ui/qt/src/coreclient.cpp (toolCatalog) | GET /api/tools (pre-existing) | settingsEightSectionsAllFields | LANDED-LOCAL |
| theme-system (ONE Theme singleton + QuickControls2 custom style; every stock control themed; zero 90-degree corners; zero hardcoded colors in qml/) | src/styles.css design tokens | ui/qt/qml/Theme.qml (sheytan.ui singleton) + ui/qt/style/* (qmldir + 13 styled controls) + ui/qt/src/main.cpp (setStyle ":/sheytanstyle") | none (pure presentation) | qmlLoadsCleanly, animationsToggleGatesEveryMotion, navChromeSevenViewsLoad | LANDED-LOCAL |
| engine-download-progress (pre-existing row, surfaced in the Engine panel) | src/DownloadProgress.tsx | ui/qt/qml/SystemView.qml (Engine panel reuses Core.engine) | GET /api/engine | hardwarePanelSurfacesSysinfo | LANDED-LOCAL |

### v1.11.0-fixed run-#48 root-cause notes (Task ID 43)

- R2: the shipped desktop's model selection never called POST
  /api/models/select — patchConfig({model}) rendered the choice while the
  selection state machine (persist + verify + engine boot) was bypassed;
  the harness only listed models, so the break was invisible (I24).
  selectModel lands the real path; the round-trip asserts it through the
  live transport and a REAL core restart.
- R3 (the stall): updateEngineVariantTx held switchMu across the
  minutes-long candidate download — Start()/Restart() (model-select boot,
  first-send boot) serialize on the same mutex, so the shipped app sat
  with dead selection/chat while gpuAssetResolved never completed. The
  download window now holds NOTHING (the v1.9.6 contract finished); the
  destructive boundary re-validates.
- R4: the Linux package shipped a bare sheytan-ui and leaned on the
  RUNNER's Qt (LD_LIBRARY_PATH outside the zip). The official CMake deploy
  path (QT_DEPLOY_AND_INSTALL) + rpath $ORIGIN/lib now close the runtime
  INSIDE the package; an ldd closure gate and a packaged-binary smoke
  prove it.
- The tool catalog is bounded to the per-tool checkbox list the Wails
  settings render; the wider card-level surfaces (tools builder editor,
  model import, performance recommendation machinery, automation/lab
  forms, clone card) remain NOT RUN — honest gaps, never faked (I21).

## Honest boundary

- LANDED-LOCAL rows are static-verified (C++/QML balance scan, symbol
  inventory) and suite-verified (manifest pins in
  scripts/installer-contract.test.mjs). Hosted Qt compilation and the
  hosted QtTest acceptance run are the ONLY compile-proof — NOT RUN at
  doc time; the next hosted run on the exact changed commit is
  authoritative (I20/I21).
- v1.10.15-fixed run-#45 cycle (Task ID 41): R1 links QuickControls2 into
  sheytan-ui-acceptance (the run-#44 C1083 — the hosted build of this
  suite is the verification of EVERY row above); R3 adds the gated-motion
  polish pass; R4 adds the animationsEnabled row. Authored motion
  (every Behavior/Transition in ui/qt/) is gated by the ONE setting —
  Qt Basic style's internal motion (ScrollBar hover-fade, default Drawer
  enter/exit before the explicit override) is the platform boundary no
  application property can gate; the drawers override their enter/exit
  with gated Transitions.
- Pre-existing dirty state: none. Baseline 77ff79f (run-#44 RED, Task ID
  41 cycle), version 1.10.15 held. The React reference tree under src/
  is untouched.
