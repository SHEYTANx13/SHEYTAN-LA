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

#include <functional>

namespace {

constexpr int kMaxHeaderBytes = 64 * 1024;     // bounded response header
constexpr qint64 kMaxBodyBytes = 8 << 20;      // bounded response body (8 MiB)
constexpr int kMaxWsFrame = 512 * 1024;        // bounded activity frame

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
    if (m_wsSock) {
        m_wsSock->close();
        m_wsSock->deleteLater();
    }
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
    }

    // Body accumulates until the server closes the channel (Connection:
    // close request semantics) — bounded by kMaxBodyBytes above.
    sock->setProperty("buf", buf);
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
                if (status == 200 && ok && hs.value("protocol").toString() == "sheytan-ipc") {
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
        if (status != 200) return;
        bool ok = false;
        const auto list = toList(body, "sessions", &ok);
        if (!ok) return;
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
    request("GET", QString("/api/sessions/") + id, QByteArray(),
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
    request("DELETE", QString("/api/sessions/") + id, QByteArray(),
            [this, id](int status, const QByteArray &) {
                if (status != 200) return;
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
        if (status != 200) return;
        bool ok = false;
        const QVariantMap m = toMap(body, &ok);
        if (!ok) return;
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
    request("POST", QStringLiteral("/api/systems/%1/activate").arg(id), QByteArray(),
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
    if (m_wsSock) {
        m_wsSock->close();
        m_wsSock->deleteLater();
        m_wsSock = nullptr;
        m_wsUp = false;
        m_wsBuf.clear();
    }

    auto *sock = new QLocalSocket(this);
    m_wsSock = sock;

    connect(sock, &QLocalSocket::connected, this, [this, sock]() {
        // RFC6455 upgrade over the channel.
        const QByteArray key = randomKey16();
        sock->setProperty("wsKey", key);
        QByteArray req;
        req += "GET /ws/activity?sessionId=" + m_sessionId.toUtf8() + " HTTP/1.1\r\n";
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
                sock->close();
                return;
            }
            m_wsBuf = m_wsBuf.mid(headEnd + 4);
            m_wsUp = true;
            // Activity frames now flow.
        }

        bool ok = true;
        while (ok) {
            const QByteArray frame = readWsFrame(sock, ok);
            if (!ok || frame.isEmpty()) break;
            handleActivityFrame(frame);
        }
    });

    sock->connectToServer(m_endpoint);
}

QByteArray CoreClient::readWsFrame(QLocalSocket *sock, bool &ok) {
    ok = true;
    if (m_wsBuf.size() < 2) { ok = false; return {}; }

    const quint8 b0 = quint8(m_wsBuf.at(0));
    const quint8 b1 = quint8(m_wsBuf.at(1));
    const bool masked = (b1 & 0x80) != 0;
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

    const quint8 opcode = b0 & 0x0F;
    if (opcode == 0x8) { // close
        ok = false;
        return {};
    }
    if (opcode == 0x9) { // ping → masked pong
        sendWsFrameControl(sock, 0x8A, payload);
        return {};
    }
    if (opcode != 0x1 && opcode != 0x2 && opcode != 0xA) return {}; // pong/other

    return payload;
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
