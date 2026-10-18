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
    bool canStop() const { return m_busy; }

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
