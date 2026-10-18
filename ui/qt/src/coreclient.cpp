// coreclient.cpp — HTTP/1.1 + RFC6455 WebSocket over QLocalSocket.
//
// WHY hand-rolled framing: the Go core serves its EXISTING handlers over
// the local channel; a minimal, bounded client (fixed method set, small
// JSON bodies, cumulative-text stream events) is the thinnest possible
// bridge and keeps Qt an honest IPC CLIENT of the ONE Go authority.
#include "coreclient.h"

#include <QCryptographicHash>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QRandomGenerator>
#include <QRegularExpression>
#include <QUrl>

#include <functional>

namespace {

constexpr int kMaxHeaderBytes = 64 * 1024;     // bounded response header
constexpr qint64 kMaxBodyBytes = 8 << 20;      // bounded response body (8 MiB)
constexpr int kMaxWsFrame = 512 * 1024;        // bounded activity frame

// encodeSegment percent-encodes one URL path/query segment (v1.10.3, R8):
// ids are interpolated into request lines, and the store's id charset —
// though currently URL-safe ("default", "sys-" + lowercase hex) — must
// never be trusted on the wire. Encoded defensively, verified by test.
QString encodeSegment(const QString &raw) {
    return QString::fromLatin1(QUrl::toPercentEncoding(raw));
}

QByteArray randomKey16() {
    QByteArray raw(16, Qt::Uninitialized);
    for (int i = 0; i < 16; ++i)
        raw[i] = char(QRandomGenerator::system()->generate() & 0xFF);
    return raw.toBase64();
}

QByteArray maskKey() {
    QByteArray m(4, Qt::Uninitialized);
    for (int i = 0; i < 4; ++i)
        m[i] = char(QRandomGenerator::system()->generate() & 0xFF);
    return m;
}

QVariantMap toMap(const QByteArray &json, bool *ok = nullptr) {
    QJsonParseError err;
    const auto doc = QJsonDocument::fromJson(json, &err);
    QVariantMap m;
    bool parsed = err.error == QJsonParseError::NoError && doc.isObject();
    if (parsed) m = doc.object().toVariantMap();
    if (ok) *ok = parsed;
    return m;
}

QVariantList toList(const QByteArray &json, const char *field, bool *ok = nullptr) {
    QJsonParseError err;
    const auto doc = QJsonDocument::fromJson(json, &err);
    bool parsed = err.error == QJsonParseError::NoError && doc.isObject();
    QVariantList l;
    if (parsed) l = doc.object().toVariantMap().value(field).toList();
    if (ok) *ok = parsed;
    return l;
}

} // namespace

QLocalSocket *CoreClient::openSocket() {
    auto *sock = new QLocalSocket(this);
    return sock;
}

CoreClient::CoreClient(QObject *parent) : QObject(parent) {}

CoreClient::~CoreClient() {
    closeWsStream();
}

void CoreClient::setError(const QString &msg) {
    if (m_error == msg) return;
    m_error = msg;
    emit errorChanged();
}

// --- HTTP over the channel -------------------------------------------------

int CoreClient::request(const QString &method, const QString &path,
                        const QByteArray &body,
                        std::function<void(int, const QByteArray &)> onReply,
                        int timeoutMs) {
    auto *sock = openSocket();
    if (!sock) {
        onReply(0, QByteArray());
        return -1;
    }

    const int id = int(reinterpret_cast<quintptr>(sock) & 0x7fffffff);

    QByteArray req;
    req += method.toUtf8() + " " + path.toUtf8() + " HTTP/1.1\r\n";
    req += "Host: sheytan-core\r\n";
    if (!m_key.isEmpty()) req += "X-SHEYTAN-IPC-Key: " + m_key.toUtf8() + "\r\n";
    if (!body.isEmpty()) {
        req += "Content-Type: application/json\r\n";
        req += "Content-Length: " + QByteArray::number(body.size()) + "\r\n";
    }
    req += "Connection: close\r\n\r\n";
    req += body;

    sock->setProperty("reqId", id);
    sock->setProperty("resHead", false);
    sock->setProperty("buf", QByteArray());
    sock->setProperty("contentLen", qint64(-1));
    sock->setProperty("replyFn",
                      QVariant::fromValue<void *>(new std::function<void(int, const QByteArray &)>(onReply)));

    auto *timer = new QTimer(sock);
    timer->setSingleShot(true);
    timer->setProperty("reqId", id);
    connect(timer, &QTimer::timeout, this, [this, sock, id]() {
        qWarning("coreclient: request %d timed out", id);
        finishRequest(id, sock, false, QByteArray("timeout"));
    });
    timer->start(timeoutMs);
    sock->setProperty("timer", QVariant::fromValue<QTimer *>(timer));

    connect(sock, &QLocalSocket::connected, this, [sock, req]() { sock->write(req); });
    connect(sock, &QLocalSocket::readyRead, this, [this, sock]() { pumpSocket(sock); });
    connect(sock, &QLocalSocket::disconnected, this, [this, sock]() {
        // Connection: close semantics — once the head is parsed and the
        // server closed, the body is complete.
        if (sock->property("resHead").toBool()) {
            finishRequest(sock->property("reqId").toInt(), sock, true, sock->property("buf").toByteArray());
        }
    });
    connect(sock, &QLocalSocket::errorOccurred, this, [this, sock, id](QLocalSocket::LocalSocketError e) {
        // Connection: close semantics — the server closing after the
        // response surfaces as PeerClosedError; the disconnected handler
        // delivers the completed response. Only treat it as a failure when
        // no response head was ever parsed.
        if (e == QLocalSocket::PeerClosedError && sock->property("resHead").toBool()) return;
        qWarning("coreclient: socket error %d on request %d", int(e), id);
        finishRequest(id, sock, false, QByteArray());
    });

    sock->connectToServer(m_endpoint);
    return id;
}

void CoreClient::finishRequest(int id, QLocalSocket *sock, bool ok, const QByteArray &body) {
    if (sock->property("done").toBool()) return;
    sock->setProperty("done", true);
    if (auto *t = sock->findChild<QTimer *>()) t->stop();
    auto fnPtr = static_cast<std::function<void(int, const QByteArray &)> *>(
        sock->property("replyFn").value<void *>());
    if (fnPtr) {
        (*fnPtr)(ok ? sock->property("status").toInt() : 0, body);
        delete fnPtr;
    }
    sock->close();
    sock->deleteLater();
}

void CoreClient::pumpSocket(QLocalSocket *sock) {
    QByteArray buf = sock->property("buf").toByteArray();
    buf += sock->readAll();
    if (buf.size() > kMaxHeaderBytes + int(kMaxBodyBytes)) {
        finishRequest(sock->property("reqId").toInt(), sock, false, QByteArray("frame too large"));
        return;
    }

    if (!sock->property("resHead").toBool()) {
        const int headEnd = int(buf.indexOf("\r\n\r\n"));
        if (headEnd < 0) {
            if (buf.size() > kMaxHeaderBytes) {
                finishRequest(sock->property("reqId").toInt(), sock, false, QByteArray("header too large"));
            } else {
                sock->setProperty("buf", buf);
            }
            return;
        }
        // v1.10.3: a head that COMPLETES beyond the bound is still a
        // violation — the v1.10.2 code only checked the incomplete case, so
        // a completed oversized head was parsed and accepted.
        if (headEnd > kMaxHeaderBytes) {
            finishRequest(sock->property("reqId").toInt(), sock, false, QByteArray("header too large"));
            return;
        }

        const QByteArray head = buf.left(headEnd);
        const QList<QByteArray> lines = head.split('\n');
        if (lines.isEmpty() || !lines[0].startsWith("HTTP/")) {
            finishRequest(sock->property("reqId").toInt(), sock, false, QByteArray("bad response"));
            return;
        }
        const QList<QByteArray> parts = lines[0].split(' ');
        sock->setProperty("status", parts.size() > 1 ? parts[1].toInt() : 0);
        sock->setProperty("resHead", true);
        buf = buf.mid(headEnd + 4);

        // v1.10.3: parse the response body framing. The v1.10.2 client had
        // NO Content-Length handling — the body completed only when the
        // server closed the channel, so any response that kept the socket
        // open hung until the request timeout and looked truncated. Now:
        //
        //   - Content-Length present → the body completes as soon as that
        //     many bytes arrived; the close (whenever it comes) is noise.
        //   - A DECLARED length beyond the body bound is refused
        //     deterministically BEFORE buffering it.
        //   - No Content-Length → close-delimited completion (the client
        //     always sends Connection: close, matching HTTP/1.1 §6.3).
        //   - Transfer-Encoding: chunked is not claimed by this client
        //     (the Go core's small JSON answers always carry
        //     Content-Length; net/http buffers them). A chunked body would
        //     fail JSON parsing upstream — a typed error, not a hang.
        qint64 declared = -1;
        for (const QByteArray &line : lines) {
            const int colon = int(line.indexOf(':'));
            if (colon <= 0) continue;
            const QByteArray name = line.left(colon).trimmed().toLower();
            if (name == "content-length") {
                bool okNum = false;
                const qint64 v = QByteArray(line.mid(colon + 1).trimmed()).toLongLong(&okNum);
                if (okNum && v >= 0) declared = v;
                break;
            }
        }
        if (declared >= 0) {
            if (declared > kMaxBodyBytes) {
                finishRequest(sock->property("reqId").toInt(), sock, false,
                              QByteArray("declared body too large"));
                return;
            }
            sock->setProperty("contentLen", declared);
        } else {
            sock->setProperty("contentLen", qint64(-1));
        }
    }

    // Content-Length framing: satisfied → the request finishes NOW (the
    // socket close is not waited for).
    const qint64 want = sock->property("contentLen").toLongLong();
    if (want >= 0) {
        if (qint64(buf.size()) >= want) {
            finishRequest(sock->property("reqId").toInt(), sock, true, buf.left(int(want)));
        } else {
            sock->setProperty("buf", buf);
        }
        return;
    }

    // Close-delimited body: accumulates until the server closes the
    // channel — bounded by kMaxBodyBytes above (the disconnected handler
    // delivers the completed response).
    sock->setProperty("buf", buf);
}

// transportReason renders a non-HTTP failure (status 0) with its typed
// reason so surfaced errors name the actual framing problem instead of a
// meaningless "status 0".
static QString transportReason(int status, const QByteArray &body) {
    if (status == 0 && !body.isEmpty()) return QString::fromLatin1(body);
    return QStringLiteral("status %1").arg(status);
}

// --- API surface -----------------------------------------------------------

void CoreClient::connectToCore(const QString &endpoint, const QString &key) {
    m_endpoint = endpoint;
    m_key = key;
    setError(QString());

    request("GET", "/api/ipc/handshake", QByteArray(),
            [this](int status, const QByteArray &body) {
                bool ok = false;
                const QVariantMap hs = toMap(body, &ok);
                // v1.10.3 (R17): STRICT handshake identity — the protocol
                // NAME and the protocol VERSION must both match. The
                // v1.10.2 happy path checked the name only, so a core
                // speaking sheytan-ipc/2 was accepted as connected(). A
                // version bump means a wire-contract change; the client
                // refuses it visibly instead of guessing.
                const bool nameOk = status == 200 && ok && hs.value("protocol").toString() == QStringLiteral("sheytan-ipc");
                const bool versionOk = nameOk && hs.value("protocolVersion").toInt() == 1;
                if (nameOk && versionOk) {
                    m_coreVersion = hs.value("coreVersion").toString();
                    m_connected = true;
                    emit handshakeDone();
                    emit connectedChanged();
                    refreshSessions();
                    refreshSystems();
                    openActivityStream();
                } else if (status == 200 && ok) {
                    setError(QStringLiteral("protocol mismatch: core speaks %1/%2, UI requires sheytan-ipc/1")
                                 .arg(hs.value("protocol").toString())
                                 .arg(hs.value("protocolVersion").toInt()));
                    emit handshakeDone();
                } else {
                    setError(QStringLiteral("handshake failed (status %1)").arg(status));
                    emit handshakeDone();
                }
            });
}

void CoreClient::refreshSessions() {
    request("GET", "/api/sessions", QByteArray(), [this](int status, const QByteArray &body) {
        // v1.10.3: no silent swallowing — a failed list refresh is a typed,
        // visible error instead of a stale/empty sidebar. Transport-level
        // failures (status 0) carry their framing reason.
        if (status != 200) {
            setError(QStringLiteral("sessions refresh failed (%1)").arg(transportReason(status, body)));
            return;
        }
        // v1.10.3 field contract: the core serves GET /api/sessions as a
        // BARE JSON ARRAY of session stubs (id, title omitempty, msgCount —
        // internal/sessions stubOf). The v1.10.2 client parsed a wrapped
        // {"sessions":[…]} envelope that the core never sends; the silent
        // return hid the drift and the sidebar never loaded its list. The
        // wrapped shape is still accepted defensively, anything else is a
        // typed malformed-payload error.
        QVariantList list;
        const auto doc = QJsonDocument::fromJson(body);
        if (doc.isArray()) {
            list = doc.array().toVariantList();
        } else if (doc.isObject()) {
            list = doc.object().toVariantMap().value("sessions").toList();
        } else {
            setError(QStringLiteral("sessions refresh failed (malformed payload)"));
            return;
        }
        m_sessions = list;
        emit sessionsChanged();
    });
}

void CoreClient::applySession(const QVariantMap &sess) {
    m_currentSession = sess;
    m_messages = sess.value("messages").toList();
    // The activity stream is per-session (the hub replays the live run
    // snapshot at attach): (re)open it whenever the client session changes.
    openActivityStream();
    emit currentSessionChanged();
    emit messagesChanged();
}

void CoreClient::openSession(const QString &id) {
    request("GET", QStringLiteral("/api/sessions/") + encodeSegment(id), QByteArray(),
            [this, id](int status, const QByteArray &body) {
                if (status != 200) {
                    setError(QStringLiteral("open session failed (status %1)").arg(status));
                    return;
                }
                bool ok = false;
                const auto m = toMap(body, &ok);
                if (!ok) return;
                m_sessionId = id;
                applySession(m);
            });
}

void CoreClient::newSession(const QString &name) {
    const QJsonObject payload{{"name", name.isEmpty() ? QStringLiteral("New session") : name}};
    request("POST", "/api/sessions", QJsonDocument(payload).toJson(),
            [this](int status, const QByteArray &body) {
                if (status != 200) {
                    setError(QStringLiteral("new session failed (status %1)").arg(status));
                    return;
                }
                const auto m = toMap(body);
                m_sessionId = m.value("id").toString();
                applySession(m);
                refreshSessions();
            });
}

void CoreClient::deleteSession(const QString &id) {
    request("DELETE", QStringLiteral("/api/sessions/") + encodeSegment(id), QByteArray(),
            [this, id](int status, const QByteArray &) {
                if (status != 200) {
                    setError(QStringLiteral("session delete failed (status %1)").arg(status));
                    return;
                }
                if (m_sessionId == id) {
                    m_sessionId.clear();
                    m_currentSession = QVariantMap();
                    m_messages.clear();
                    emit currentSessionChanged();
                    emit messagesChanged();
                }
                refreshSessions();
            });
}

void CoreClient::send(const QString &text, bool netSearch) {
    if (m_busy || m_sessionId.isEmpty() || text.trimmed().isEmpty()) return;

    m_busy = true;
    m_liveActive = true;
    m_stopping = false;
    m_liveText.clear();
    m_liveReasoning.clear();
    emit busyChanged();
    emit liveActiveChanged();
    emit liveTextChanged();
    emit liveReasoningChanged();

    QJsonObject payload{{"sessionId", m_sessionId}, {"message", text}, {"netSearch", netSearch}};
    request("POST", "/api/run", QJsonDocument(payload).toJson(),
            [this](int status, const QByteArray &body) {
                if (status != 200) {
                    m_busy = false;
                    m_liveActive = false;
                    emit busyChanged();
                    emit liveActiveChanged();
                    setError(QStringLiteral("run rejected (status %1)").arg(status));
                    return;
                }
                const auto m = toMap(body);
                m_activeRunId = m.value("runId").toString();
                // The user bubble: reload the session so the accepted
                // message renders from the authoritative transcript.
                openSession(m_sessionId);
            },
            10000);
}

void CoreClient::stop() {
    if (m_sessionId.isEmpty()) return;
    m_stopping = true;
    QJsonObject payload{{"sessionId", m_sessionId}, {"runId", m_activeRunId}};
    request("POST", "/api/run/stop", QJsonDocument(payload).toJson(),
            [this](int, const QByteArray &) {}, 5000);
}

// Not migrated on purpose (v1.10.3 honesty note): the settings surface
// carries NO live config values — refreshConfig is an explicit stub and the
// QML renders no config property, so nothing here can claim data it does
// not have. Loading /api/config happens with the settings migration only.
void CoreClient::refreshConfig() {}

void CoreClient::patchConfig(const QVariantMap &patch) {
    request("POST", "/api/config", QJsonDocument(QJsonObject::fromVariantMap(patch)).toJson(),
            [this](int status, const QByteArray &) {
                if (status != 200) setError(QStringLiteral("config update failed (status %1)").arg(status));
            });
}

// --- AI Systems (pure IPC client of the ONE Go store) ----------------------

void CoreClient::refreshSystems() {
    request("GET", "/api/systems", QByteArray(), [this](int status, const QByteArray &body) {
        // v1.10.3 (R7): a failed refresh is a TYPED, visible error. The
        // v1.10.2 silent return left the drawer showing stale/empty data
        // with no indication anything failed.
        if (status != 200) {
            setError(QStringLiteral("systems refresh failed (%1)").arg(transportReason(status, body)));
            return;
        }
        bool ok = false;
        const QVariantMap m = toMap(body, &ok);
        if (!ok) {
            setError(QStringLiteral("systems refresh failed (malformed payload)"));
            return;
        }
        m_systems = m.value("systems").toList();
        const QString active = m.value("activeSystemId").toString();
        if (active != m_activeSystemId) {
            m_activeSystemId = active;
            emit activeSystemIdChanged();
        }
        emit systemsChanged();
    });
}

void CoreClient::activateSystem(const QString &id) {
    if (id.isEmpty()) return;
    // v1.10.3 (R8): the id is percent-encoded into the path segment —
    // never interpolated raw into the request line.
    request("POST", QStringLiteral("/api/systems/%1/activate").arg(encodeSegment(id)), QByteArray(),
            [this, id](int status, const QByteArray &) {
                if (status != 200) {
                    setError(QStringLiteral("system activation failed (status %1)").arg(status));
                    return;
                }
                // The store is the authority: re-read the list + active id.
                refreshSystems();
            });
}

void CoreClient::refreshApprovals() {}

void CoreClient::resolveApproval(const QString &id, bool approve) {}

// --- Goals (pure IPC client of the ONE Go goal engine) ----------------------
//
// v1.10.13, roadmap Phase 2. The wire shapes are mirrored FROM the core's
// handlers (internal/api/goals.go), never invented here:
//   GET  /api/goals                → {"goals":[…]} (list; entries carry
//                                    goalId, phase, status, originalGoal,
//                                    currentStep, turnBudget, plan omitempty)
//   POST /api/goals/{id}/start     → {"ok":true,"started":id}
//   POST /api/goals/{id}/pause     → goal document
//   POST /api/goals/{id}/resume    → goal document
//   POST /api/goals/{id}/cancel    → goal document
//   POST /api/goals/{id}/approve   → goal document (body {"approvalId":…})
//   POST /api/goals/{id}/reject    → goal document (body {"approvalId":…})
// The durable store, the bounded drive loop and the approval policy stay
// server-owned; the client keeps no goal state beyond the last list read.

void CoreClient::refreshGoals() {
    request("GET", "/api/goals", QByteArray(), [this](int status, const QByteArray &body) {
        // v1.10.3 rule: a failed refresh is a TYPED, visible error — never
        // a silent stale/empty panel.
        if (status != 200) {
            setError(QStringLiteral("goals refresh failed (%1)").arg(transportReason(status, body)));
            return;
        }
        bool ok = false;
        const QVariantMap m = toMap(body, &ok);
        if (!ok) {
            setError(QStringLiteral("goals refresh failed (malformed payload)"));
            return;
        }
        m_goals = m.value("goals").toList();
        emit goalsChanged();
    });
}

void CoreClient::goalAction(const QString &action, const QString &id) {
    if (id.isEmpty()) return;
    // v1.10.3 (R8) rule: ids are percent-encoded into path segments —
    // never interpolated raw into the request line.
    request("POST", QStringLiteral("/api/goals/%1/%2").arg(encodeSegment(id), action), QByteArray(),
            [this, action, id](int status, const QByteArray &) {
                if (status != 200) {
                    setError(QStringLiteral("goal %1 failed (status %2)").arg(action).arg(status));
                    return;
                }
                // The store is the authority: re-read the durable list.
                refreshGoals();
            });
}

void CoreClient::goalDecision(const QString &action, const QString &id, const QString &approvalId) {
    if (id.isEmpty() || approvalId.isEmpty()) return;
    const QJsonObject payload{{"approvalId", approvalId}};
    request("POST", QStringLiteral("/api/goals/%1/%2").arg(encodeSegment(id), action),
            QJsonDocument(payload).toJson(),
            [this, action, id](int status, const QByteArray &) {
                if (status != 200) {
                    setError(QStringLiteral("goal %1 failed (status %2)").arg(action).arg(status));
                    return;
                }
                refreshGoals();
            });
}

void CoreClient::startGoal(const QString &id)  { goalAction(QStringLiteral("start"), id); }
void CoreClient::pauseGoal(const QString &id)  { goalAction(QStringLiteral("pause"), id); }
void CoreClient::resumeGoal(const QString &id) { goalAction(QStringLiteral("resume"), id); }
void CoreClient::cancelGoal(const QString &id) { goalAction(QStringLiteral("cancel"), id); }
void CoreClient::approveGoal(const QString &id, const QString &approvalId) {
    goalDecision(QStringLiteral("approve"), id, approvalId);
}
void CoreClient::rejectGoal(const QString &id, const QString &approvalId) {
    goalDecision(QStringLiteral("reject"), id, approvalId);
}

// --- activity WebSocket ----------------------------------------------------

// sendWsFrameControl writes one small masked control/data frame (client
// frames are always masked per RFC6455 §5.3).
static void sendWsFrameControl(QLocalSocket *sock, quint8 opcode, const QByteArray &payload) {
    if (!sock) return;
    QByteArray frame;
    frame += char(opcode);
    const QByteArray mk = maskKey();
    const int n = payload.size();
    if (n < 126) {
        frame += char(0x80 | n);
    } else {
        frame += char(0x80 | 126);
        frame += char((n >> 8) & 0xFF);
        frame += char(n & 0xFF);
    }
    frame += mk;
    frame += payload;
    for (int i = 0; i < n; ++i)
        frame[frame.size() - n + i] = char(frame[frame.size() - n + i] ^ mk[i % 4]);
    sock->write(frame);
}

void CoreClient::openActivityStream() {
    if (m_sessionId.isEmpty()) return;
    closeWsStream();

    auto *sock = new QLocalSocket(this);
    m_wsSock = sock;

    connect(sock, &QLocalSocket::connected, this, [this, sock]() {
        // RFC6455 upgrade over the channel.
        const QByteArray key = randomKey16();
        sock->setProperty("wsKey", key);
        QByteArray req;
        req += "GET /ws/activity?sessionId=" + encodeSegment(m_sessionId).toUtf8() + " HTTP/1.1\r\n";
        req += "Host: sheytan-core\r\n";
        req += "Upgrade: websocket\r\n";
        req += "Connection: Upgrade\r\n";
        req += "Sec-WebSocket-Key: " + key + "\r\n";
        req += "Sec-WebSocket-Version: 13\r\n";
        if (!m_key.isEmpty()) req += "X-SHEYTAN-IPC-Key: " + m_key.toUtf8() + "\r\n";
        req += "\r\n";
        sock->write(req);
    });

    connect(sock, &QLocalSocket::readyRead, this, [this, sock]() {
        m_wsBuf += sock->readAll();

        if (!m_wsUp) {
            const int headEnd = int(m_wsBuf.indexOf("\r\n\r\n"));
            if (headEnd < 0) return;
            const QByteArray head = m_wsBuf.left(headEnd);
            if (!head.startsWith("HTTP/1.1 101")) {
                setError(QStringLiteral("activity stream refused"));
                closeWsStream();
                return;
            }
            m_wsBuf = m_wsBuf.mid(headEnd + 4);
            m_wsUp = true;
            // Activity frames now flow.
        }

        bool ok = true;
        while (ok) {
            const QByteArray frame = readWsFrame(sock, ok);
            if (!ok) break;                 // incomplete, terminal, or closed
            if (frame.isEmpty()) continue;  // control frame handled — KEEP PUMPING (the v1.10.2 loop broke on ping/pong, starving the frames behind them)
            handleActivityFrame(frame);
        }
    });

    sock->connectToServer(m_endpoint);
}

// closeWsStream tears down the activity channel on ANY terminal stream
// condition (close handshake, fatal frame error, refused upgrade). The
// v1.10.2 code left the socket open and silent after fatal frame errors —
// a zombie stream no gate could observe.
void CoreClient::closeWsStream() {
    if (m_wsSock) {
        // Flush BEFORE closing: the close-reply frame (and any pong) must
        // reach the peer — a discarded reply would break the RFC6455
        // close handshake the tests pin.
        m_wsSock->flush();
        m_wsSock->close();
        m_wsSock->deleteLater();
        m_wsSock = nullptr;
    }
    m_wsUp = false;
    m_wsBuf.clear();
    // v1.10.5: a closed stream also abandons any partially assembled
    // fragmented message — stale bytes must never leak into the next
    // stream's reassembly.
    m_wsFrag.clear();
    m_wsFragOpen = false;
}

// readWsFrame implements the client's FRAGMENTATION POLICY (documented,
// v1.10.3, REVISED v1.10.5):
//   - complete text frames (FIN=1, opcode 0x1) carry activity events;
//   - FRAGMENTED messages are REASSEMBLED per RFC6455 §5.4: the core's
//     activity hub writes through a 1024-byte gorilla buffer, so every
//     activity event larger than 1024 bytes arrives as
//     fragment(opcode 0x1, FIN=0) + continuations(opcode 0x0, last FIN=1).
//     The raw-socket probe proved this happens on every real generation
//     (the context frame is always > 1024 bytes), so the v1.10.3
//     refuse-and-close policy — written under the false assumption that
//     "the hub emits whole text messages" — killed the activity stream
//     mid-run and froze the live bubble. Reassembly stays BOUNDED: the
//     assembled size is refused above the same kMaxWsFrame bound as a
//     single frame, and protocol violations (continuation without a
//     first fragment, an interleaved message inside an open fragment)
//     still fail LOUDLY with a typed error — nothing is silently dropped;
//   - ping is answered with a masked pong (payload echoed);
//   - pong (0xA) and binary (0x2) frames are never mistaken for activity;
//   - close (0x8) is answered with a masked close reply (RFC6455 §5.5.1)
//     and ends the stream;
//   - any frame above kMaxWsFrame refuses and closes the stream.
QByteArray CoreClient::readWsFrame(QLocalSocket *sock, bool &ok) {
    ok = true;
    if (m_wsBuf.size() < 2) { ok = false; return {}; }

    const quint8 b0 = quint8(m_wsBuf.at(0));
    const quint8 b1 = quint8(m_wsBuf.at(1));
    const bool masked = (b1 & 0x80) != 0;
    const bool fin = (b0 & 0x80) != 0;
    const quint8 opcode = b0 & 0x0F;
    quint64 len = b1 & 0x7F;
    int off = 2;

    if (len == 126) {
        if (m_wsBuf.size() < off + 2) { ok = false; return {}; }
        len = quint64(quint8(m_wsBuf[off]) << 8 | quint8(m_wsBuf[off + 1]));
        off += 2;
    } else if (len == 127) {
        if (m_wsBuf.size() < off + 8) { ok = false; return {}; }
        len = 0;
        for (int i = 0; i < 8; ++i) len = (len << 8) | quint8(m_wsBuf[off + i]);
        off += 8;
    }

    if (len > quint64(kMaxWsFrame)) {
        setError(QStringLiteral("activity frame exceeds bound"));
        ok = false;
        closeWsStream();
        return {};
    }

    QByteArray mask;
    if (masked) {
        if (m_wsBuf.size() < off + 4) { ok = false; return {}; }
        mask = m_wsBuf.mid(off, 4);
        off += 4;
    }

    if (m_wsBuf.size() < off + int(len)) { ok = false; return {}; }

    QByteArray payload = m_wsBuf.mid(off, int(len));
    if (masked) {
        for (int i = 0; i < payload.size(); ++i)
            payload[i] = char(payload[i] ^ mask[i % 4]);
    }
    m_wsBuf.remove(0, off + int(len));

    if (opcode == 0x8) { // close → masked close reply, stream ends
        sendWsFrameControl(sock, 0x88, QByteArray());
        ok = false;
        closeWsStream();
        return {};
    }
    if (opcode == 0x9) { // ping → masked pong
        sendWsFrameControl(sock, 0x8A, payload);
        return {};
    }
    if (opcode == 0xA) return {};                    // pong: never activity

    // v1.10.5: RFC6455 §5.4 reassembly. The core's hub (gorilla
    // WriteBufferSize 1024) fragments every activity event larger than
    // 1024 bytes: first fragment opcode 0x1 with FIN=0, continuations
    // opcode 0x0, the last one carrying FIN=1. Refusing fragmentation
    // closed the stream mid-run on real generations, so the live bubble
    // never grew. Control frames were handled ABOVE and may interleave
    // between fragments. The reassembled message stays bounded by the
    // same kMaxWsFrame contract as single frames.
    if (opcode == 0x1 || opcode == 0x2) {
        if (!fin) {
            // First fragment of a fragmented message.
            if (m_wsFragOpen) { // interleaved new message: protocol violation
                setError(QStringLiteral("activity stream: unexpected new fragmented message"));
                ok = false;
                closeWsStream();
                return {};
            }
            m_wsFragOpen = true;
            m_wsFrag = payload;
            return {};
        }
        if (m_wsFragOpen) { // single unfragmented frame while assembling: violation
            setError(QStringLiteral("activity stream: unfragmented frame inside a fragmented message"));
            ok = false;
            closeWsStream();
            return {};
        }
        if (opcode == 0x1) return payload;           // complete text frame
        setError(QStringLiteral("activity stream: unsupported frame opcode %1").arg(opcode));
        ok = false;
        closeWsStream();
        return {};
    }
    if (opcode == 0x0) { // continuation
        if (!m_wsFragOpen) { // continuation without a first fragment: violation
            setError(QStringLiteral("activity stream: continuation without a first fragment"));
            ok = false;
            closeWsStream();
            return {};
        }
        if (m_wsFrag.size() + payload.size() > kMaxWsFrame) {
            setError(QStringLiteral("activity frame exceeds bound"));
            ok = false;
            closeWsStream();
            return {};
        }
        m_wsFrag += payload;
        if (!fin) return {};                         // more fragments to come
        QByteArray assembled = m_wsFrag;
        m_wsFrag.clear();
        m_wsFragOpen = false;
        return assembled;                            // the full text message
    }

    setError(QStringLiteral("activity stream: unsupported frame opcode %1").arg(opcode));
    ok = false;
    closeWsStream();
    return {};
}

void CoreClient::handleActivityFrame(const QByteArray &text) {
    bool ok = false;
    const QVariantMap ev = toMap(text, &ok);
    if (!ok) return;

    const QString type = ev.value("type").toString();

    // Response/reasoning events carry the FULL CUMULATIVE text in caption
    // (the same contract the browser frontend's stream-accumulator uses).
    if (type == "response") {
        m_liveText = ev.value("caption").toString();
        emit liveTextChanged();
        return;
    }
    if (type == "reasoning") {
        m_liveReasoning = ev.value("caption").toString();
        emit liveReasoningChanged();
        return;
    }
    if (type == "done" || type == "complete" || type == "aborted" || type == "error") {
        const QString outcome = (type == "done" || type == "complete") ? QStringLiteral("done")
                               : type == "error" ? QStringLiteral("error")
                                                 : QStringLiteral("aborted");
        m_liveActive = false;
        m_busy = false;
        emit liveActiveChanged();
        emit busyChanged();
        emit settled(outcome, ev.value("caption").toString());

        // The transcript is the authority: reload the session so the
        // persisted assistant answer replaces the live bubble.
        if (!m_sessionId.isEmpty()) openSession(m_sessionId);
        refreshSessions();
        return;
    }
    if (type == "idle" || type == "session" || type == "handoff" || type == "status") {
        // Idle sentinels re-anchor late attach; nothing to render live.
        return;
    }
}
