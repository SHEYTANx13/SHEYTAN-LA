# SHEYTAN Qt parity manifest — v1.10.15-fixed (Task ID 39, D28/I21)

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
| perf-strip | src/PerfStrip.tsx | ui/qt/qml/PerfStrip.qml | activity WS metrics | extrasSurfacesLoad | LANDED-LOCAL |
| ai-systems | src/AISystemsCard.tsx | ui/qt/qml/Main.qml systemsDrawer (pre-existing) | /api/systems | aiSystemsListActivate | LANDED-LOCAL (pre-existing at HEAD) |
| goals | src/GoalsCard.tsx | ui/qt/qml/Main.qml goalsDrawer (pre-existing) | /api/goals | goalsListFieldContract, goalsStartPauseResumeCancelRoundTrip, goalsApproveRejectParkedGoal | LANDED-LOCAL (pre-existing at HEAD) |

## Honest boundary

- LANDED-LOCAL rows are static-verified (C++/QML balance scan, symbol
  inventory) and suite-verified (manifest pins in
  scripts/installer-contract.test.mjs). Hosted Qt compilation and the
  hosted QtTest acceptance run are the ONLY compile-proof — NOT RUN at
  doc time; the next hosted run on the exact changed commit is
  authoritative (I20/I21).
- Pre-existing dirty state: none. Baseline f5a31cf (run-#44 cycle,
  Task ID 40), version 1.10.15 held. Run-#44 fixes landed under the
  same rows: R1 nav chrome (seven static nav buttons — the Repeater
  required-index delegate class never created them), R2 per-engine
  Core singleton + SystemView envelope guard, R3 WorkspaceView recents
  delegates restored, R5 submit() rename + Basic style in the harness.
  The React reference tree under src/ is untouched.
