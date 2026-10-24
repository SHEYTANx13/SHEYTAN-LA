// coreclient.h — the ONE Qt-side bridge to the Go core.
//
// Transport: HTTP/1.1 + WebSocket over the secured local IPC channel
// (QLocalSocket = Unix domain socket on Linux/macOS, named pipe on
// Windows). The Go core is the ONLY authority: every request below maps
// 1:1 to an existing /api handler; no session/run/tool/policy behavior is
// reimplemented here. Bounded reads, request ids, deadlines and a versioned
// handshake are part of the contract (internal/localipc on the Go side).
#pragma once

#include <QObject>
#include <QVariantList>
#include <QVariantMap>
#include <QLocalSocket>
#include <QTimer>

class CoreClient : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool connected READ connected NOTIFY connectedChanged)
    Q_PROPERTY(bool busy READ busy NOTIFY busyChanged)
    Q_PROPERTY(bool liveActive READ liveActive NOTIFY liveActiveChanged)
    Q_PROPERTY(QString liveText READ liveText NOTIFY liveTextChanged)
    Q_PROPERTY(QString liveReasoning READ liveReasoning NOTIFY liveReasoningChanged)
    Q_PROPERTY(QString coreVersion READ coreVersion NOTIFY handshakeDone)
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)
    Q_PROPERTY(QVariantList sessions READ sessions NOTIFY sessionsChanged)
    Q_PROPERTY(QVariantMap currentSession READ currentSession NOTIFY currentSessionChanged)
    Q_PROPERTY(QVariantList messages READ messages NOTIFY messagesChanged)
    // AI Systems surface (v1.10.2): a pure IPC client of the ONE Go store
    // (/api/systems). List + activation only — validation, persistence and
    // the reserved default stay server-owned; no second authority here.
    Q_PROPERTY(QVariantList systems READ systems NOTIFY systemsChanged)
    Q_PROPERTY(QString activeSystemId READ activeSystemId NOTIFY activeSystemIdChanged)
    // Goals surface (v1.10.13, roadmap Phase 2): a pure IPC client of the
    // ONE durable Go goal engine (/api/goals). List + control actions only
    // — the store, the bounded drive loop and the approval semantics stay
    // server-owned; no second run registry or policy authority lives here
    // and no goal state is kept in QML beyond display.
    Q_PROPERTY(QVariantList goals READ goals NOTIFY goalsChanged)
    Q_PROPERTY(bool canStop READ canStop NOTIFY busyChanged)
    // v1.10.15-fixed R1 (D28/I21): the Wails-parity state surfaces. Each
    // property is a PURE IPC read of an EXISTING /api handler — no new
    // backend endpoint, no second authority; every refresh re-reads the
    // authoritative source and surfaces a typed error on failure (the
    // v1.10.3 rule: a failed refresh is never a silent stale panel).
    Q_PROPERTY(QVariantMap config READ config NOTIFY configChanged)
    Q_PROPERTY(QVariantMap workspace READ workspace NOTIFY workspaceChanged)
    Q_PROPERTY(QVariantMap repoIndex READ repoIndex NOTIFY repoIndexChanged)
    Q_PROPERTY(QVariantList labTasks READ labTasks NOTIFY labTasksChanged)
    Q_PROPERTY(QVariantList automationTasks READ automationTasks NOTIFY automationTasksChanged)
    Q_PROPERTY(QVariantList automationArtifacts READ automationArtifacts NOTIFY automationArtifactsChanged)
    Q_PROPERTY(QVariantList logs READ logs NOTIFY logsChanged)
    Q_PROPERTY(QVariantList historyHits READ historyHits NOTIFY historyHitsChanged)
    Q_PROPERTY(QVariantList models READ models NOTIFY modelsChanged)
    Q_PROPERTY(QVariantMap preflight READ preflight NOTIFY preflightChanged)
    Q_PROPERTY(QVariantMap governor READ governor NOTIFY governorChanged)
    Q_PROPERTY(QVariantMap maintenance READ maintenance NOTIFY maintenanceChanged)
    Q_PROPERTY(QVariantList tools READ tools NOTIFY toolsChanged)
    // v1.11.0-fixed R3 (run-#48, Task ID 43): the AVAILABLE-tool catalog
    // (GET /api/tools — the immutable registry snapshot the Wails
    // enabledTools checkbox list is fed by). Distinct from tools (the
    // custom-tool store on /api/custom-tools); the settings surface uses
    // THIS list to render per-tool checkboxes instead of a dead
    // comma-textfield.
    Q_PROPERTY(QVariantList toolCatalog READ toolCatalog NOTIFY toolCatalogChanged)
    // v1.10.15-fixed R3 (D37, run #47 cycle, Task ID 42): the Wails-parity
    // agent surfaces. activityEvents is a BOUNDED display ring (the last 50
    // frames — the Wails ActivityStream MAX_VISIBLE_EVENTS contract) of the
    // frames the client ALREADY receives on the activity WS; the
    // authoritative routing in handleActivityFrame is unchanged (display
    // only, never a second authority). engine is a pure IPC read of the
    // EXISTING GET /api/engine snapshot — the Download Manager feed the
    // Wails AgentBody renders as DownloadProgressPanel; every refresh
    // re-reads the authoritative source and surfaces a typed error on
    // failure (the v1.10.3 rule: a failed refresh is never a silent stale
    // panel).
    Q_PROPERTY(QVariantList activityEvents READ activityEvents NOTIFY activityEventsChanged)
    Q_PROPERTY(QVariantMap engine READ engine NOTIFY engineChanged)
    // v1.11.0-fixed R3 (run-#48, Task ID 43): the hardware/capability
    // surface. A pure IPC read of the EXISTING GET /api/sysinfo handler —
    // the same snapshot probeDeviceTier reads — exposed to QML so the
    // SystemView hardware panel (and Settings diagnostics) can show the
    // CPU/RAM/GPU/NPU facts the Wails DeviceCard shows. The core stays the
    // hardware authority; the client never derives a second truth.
    Q_PROPERTY(QVariantMap sysinfo READ sysinfo NOTIFY sysinfoChanged)
    // v1.10.15-fixed R4 (D32/I22, Task ID 41): the ONE motion gate. Every
    // animation/transition in the QML tree reads this single persisted
    // setting — off means instant state, identical function. The device
    // tier default is probed ONCE from the core's existing /api/sysinfo
    // (high-end: animations on; low-end: off); an explicit user toggle is
    // persisted locally via QSettings and always wins over the tier.
    // (The core's /api/config is a typed struct that drops unknown keys
    // server-side, so core-side persistence of this key needs a Go field
    // — out of scope this cycle; recorded in the parity manifest.)
    Q_PROPERTY(bool animationsEnabled READ animationsEnabled WRITE setAnimationsEnabled NOTIFY animationsEnabledChanged)

public:
    explicit CoreClient(QObject *parent = nullptr);
    ~CoreClient() override;

    // Versioned handshake: GET /api/ipc/handshake with the ephemeral key.
    Q_INVOKABLE void connectToCore(const QString &endpoint, const QString &key);
    Q_INVOKABLE void refreshSessions();
    Q_INVOKABLE void openSession(const QString &id);
    Q_INVOKABLE void newSession(const QString &name);
    Q_INVOKABLE void deleteSession(const QString &id);
    Q_INVOKABLE void send(const QString &text, bool netSearch);
    Q_INVOKABLE void stop();
    Q_INVOKABLE void refreshConfig();
    Q_INVOKABLE void patchConfig(const QVariantMap &patch);
    Q_INVOKABLE void refreshSystems();
    Q_INVOKABLE void activateSystem(const QString &id);
    // Goals surface (v1.10.13): each invokable maps 1:1 to the existing
    // /api/goals handler (list + start/pause/resume/cancel + approve/reject).
    Q_INVOKABLE void refreshGoals();
    Q_INVOKABLE void startGoal(const QString &id);
    Q_INVOKABLE void pauseGoal(const QString &id);
    Q_INVOKABLE void resumeGoal(const QString &id);
    Q_INVOKABLE void cancelGoal(const QString &id);
    Q_INVOKABLE void approveGoal(const QString &id, const QString &approvalId);
    Q_INVOKABLE void rejectGoal(const QString &id, const QString &approvalId);
    Q_INVOKABLE void refreshApprovals();
    Q_INVOKABLE void resolveApproval(const QString &id, bool approve);
    // v1.10.15-fixed R1 (D28): Wails-parity invokables. Each maps 1:1 to
    // an EXISTING /api handler (list + bounded control actions); the
    // store, the scheduler and the policy stay server-owned.
    Q_INVOKABLE void refreshWorkspace();
    Q_INVOKABLE void switchWorkspace(const QString &path);
    Q_INVOKABLE void revealWorkspace(const QString &path);
    Q_INVOKABLE void refreshRepoIndex();
    Q_INVOKABLE void refreshRepoIndexNow();
    Q_INVOKABLE void refreshLab();
    Q_INVOKABLE void labRun(const QString &rawJson);
    Q_INVOKABLE void refreshAutomation();
    Q_INVOKABLE void automationTaskAction(const QString &id, const QString &action);
    Q_INVOKABLE void refreshAutomationArtifacts();
    Q_INVOKABLE void refreshLogs();
    Q_INVOKABLE void searchHistory(const QString &q, const QString &mode);
    Q_INVOKABLE void refreshModels();
    // v1.11.0-fixed R2 (run-#48, Task ID 43): the SHIPPED selection path.
    // POST /api/models/select {"model": id} — the Wails ModelPicker's
    // authority (persist + verify + engine boot through the selection
    // state machine). The former patchConfig({model}) shortcut bypassed
    // the state machine entirely: config rendered the choice, the engine
    // never followed it — the operator's "model selection broken" defect
    // on the shipped app while the harness (which only listed models)
    // stayed green (I24).
    Q_INVOKABLE void selectModel(const QString &id);
    Q_INVOKABLE void refreshPreflight();
    Q_INVOKABLE void refreshGovernor();
    Q_INVOKABLE void refreshMaintenance();
    Q_INVOKABLE void refreshTools();
    // v1.11.0-fixed R3 (run-#48): the /api/tools catalog read.
    Q_INVOKABLE void refreshToolCatalog();
    // v1.10.15-fixed R3 (D37, Task ID 42): the agent-view parity invokables,
    // each 1:1 to an EXISTING /api handler — GET /api/engine (snapshot) and
    // POST /api/llama {action: "cancel-download"} (the Wails
    // DownloadProgressPanel cancel, 15s bound, matching api.ts). The engine
    // store and the download lifecycle stay server-owned.
    Q_INVOKABLE void refreshEngine();
    Q_INVOKABLE void cancelEngineDownload();
    // v1.11.0-fixed R3 (run-#48, Task ID 43): the hardware surface
    // invokable — one bounded GET /api/sysinfo read, exposed verbatim.
    Q_INVOKABLE void refreshSysinfo();

    bool connected() const { return m_connected; }
    bool busy() const { return m_busy; }
    bool liveActive() const { return m_liveActive; }
    QString liveText() const { return m_liveText; }
    QString liveReasoning() const { return m_liveReasoning; }
    QString coreVersion() const { return m_coreVersion; }
    QString error() const { return m_error; }
    QVariantList sessions() const { return m_sessions; }
    QVariantMap currentSession() const { return m_currentSession; }
    QVariantList messages() const { return m_messages; }
    QVariantList systems() const { return m_systems; }
    QString activeSystemId() const { return m_activeSystemId; }
    QVariantList goals() const { return m_goals; }
    QVariantMap config() const { return m_config; }
    QVariantMap workspace() const { return m_workspace; }
    QVariantMap repoIndex() const { return m_repoIndex; }
    QVariantList labTasks() const { return m_labTasks; }
    QVariantList automationTasks() const { return m_automationTasks; }
    QVariantList automationArtifacts() const { return m_automationArtifacts; }
    QVariantList logs() const { return m_logs; }
    QVariantList historyHits() const { return m_historyHits; }
    QVariantList models() const { return m_models; }
    QVariantMap preflight() const { return m_preflight; }
    QVariantMap governor() const { return m_governor; }
    QVariantMap maintenance() const { return m_maintenance; }
    QVariantList tools() const { return m_tools; }
    // v1.11.0-fixed R3 (run-#48): the available-tool catalog getter.
    QVariantList toolCatalog() const { return m_toolCatalog; }
    // v1.10.15-fixed R3 (D37): the agent-view parity getters.
    QVariantList activityEvents() const { return m_activityEvents; }
    QVariantMap engine() const { return m_engine; }
    // v1.11.0-fixed R3 (run-#48): the hardware snapshot getter.
    QVariantMap sysinfo() const { return m_sysinfo; }
    bool canStop() const { return m_busy; }
    bool animationsEnabled() const { return m_animationsEnabled; }
    void setAnimationsEnabled(bool on);

signals:
    void connectedChanged();
    void busyChanged();
    void liveActiveChanged();
    void liveTextChanged();
    void liveReasoningChanged();
    void handshakeDone();
    void errorChanged();
    void sessionsChanged();
    void currentSessionChanged();
    void messagesChanged();
    void systemsChanged();
    void activeSystemIdChanged();
    void goalsChanged();
    void settled(const QString &outcome, const QString &caption);
    // v1.10.15-fixed R1 (D28): parity-surface change signals.
    void configChanged();
    void workspaceChanged();
    void repoIndexChanged();
    void labTasksChanged();
    void automationTasksChanged();
    void automationArtifactsChanged();
    void logsChanged();
    void historyHitsChanged();
    void modelsChanged();
    void preflightChanged();
    void governorChanged();
    void maintenanceChanged();
    void toolsChanged();
    // v1.11.0-fixed R3 (run-#48): the tool-catalog signal.
    void toolCatalogChanged();
    void animationsEnabledChanged();
    // v1.10.15-fixed R3 (D37): the agent-view parity signals.
    void activityEventsChanged();
    void engineChanged();
    // v1.11.0-fixed R3 (run-#48): the hardware snapshot signal.
    void sysinfoChanged();

private:
    // --- IPC plumbing ---
    QLocalSocket *openSocket();
    // One HTTP/1.1 request over the channel; onReply invoked with status +
    // body. Requests are serialized (the UI's traffic is low-volume).
    int request(const QString &method, const QString &path,
                const QByteArray &body,
                std::function<void(int, const QByteArray &)> onReply,
                int timeoutMs = 20000);
    void pumpSocket(QLocalSocket *sock);
    void finishRequest(int id, QLocalSocket *sock, bool ok, const QByteArray &body);
    void openActivityStream();
    void closeWsStream();
    void handleActivityFrame(const QByteArray &text);
    QByteArray readWsFrame(QLocalSocket *sock, bool &ok);
    void setError(const QString &msg);
    void applySession(const QVariantMap &sess);
    // Goals plumbing: one 1:1 POST to a /api/goals/{id} control subroute
    // (action without a body) and one decision POST carrying the durable
    // pending approvalId. Every accepted action re-reads the authoritative
    // list; the store is the only state owner.
    void goalAction(const QString &action, const QString &id);
    void goalDecision(const QString &action, const QString &id, const QString &approvalId);
    // v1.10.15-fixed R4 (D32): the device-tier default probe — ONE
    // read of the existing /api/sysinfo handler after the first
    // successful handshake. The core stays the hardware authority.
    void probeDeviceTier();
    bool animationsOverrideFromStore(bool *hasOverride) const;

    QString m_endpoint;
    QString m_key;
    bool m_connected = false;
    bool m_busy = false;
    bool m_liveActive = false;
    bool m_stopping = false;
    QString m_liveText;
    QString m_liveReasoning;
    QString m_coreVersion;
    QString m_error;
    QString m_sessionId;
    QString m_activeRunId;
    QVariantList m_sessions;
    QVariantMap m_currentSession;
    QVariantList m_messages;
    QVariantList m_systems;
    QString m_activeSystemId;
    QVariantList m_goals;
    QVariantMap m_config;
    QVariantMap m_workspace;
    QVariantMap m_repoIndex;
    QVariantList m_labTasks;
    QVariantList m_automationTasks;
    QVariantList m_automationArtifacts;
    QVariantList m_logs;
    QVariantList m_historyHits;
    QVariantList m_models;
    QVariantMap m_preflight;
    QVariantMap m_governor;
    QVariantMap m_maintenance;
    QVariantList m_tools;
    // v1.11.0-fixed R3 (run-#48): the available-tool catalog state.
    QVariantList m_toolCatalog;
    bool m_animationsEnabled = true;
    bool m_tierProbed = false;
    // v1.10.15-fixed R3 (D37): the agent-view parity state — the bounded
    // activity display ring and the last engine snapshot read.
    QVariantList m_activityEvents;
    QVariantMap m_engine;
    // v1.11.0-fixed R3 (run-#48): the last /api/sysinfo snapshot.
    QVariantMap m_sysinfo;

    // activity WS state
    QLocalSocket *m_wsSock = nullptr;
    QByteArray m_wsBuf;
    bool m_wsUp = false;
    // v1.10.5: RFC6455 §5.4 reassembly. The core's activity hub writes
    // with a 1024-byte gorilla write buffer, so ANY activity event larger
    // than 1024 bytes arrives as a fragmented message (first fragment
    // FIN=0, continuation frames opcode 0x0). Refusing fragmentation —
    // the v1.10.2-era behavior — killed the activity stream mid-run on
    // every real generation whose context frame crossed the buffer, so
    // the live bubble never grew (acceptance chatStreamPersistRepeatAndClose,
    // proven by the raw-socket probe: fragment(1024, FIN=0)+cont(1, FIN=1)).
    // Control frames may interleave between fragments (RFC6455 §5.4) and
    // are still answered immediately.
    QByteArray m_wsFrag;
    bool m_wsFragOpen = false;
};
