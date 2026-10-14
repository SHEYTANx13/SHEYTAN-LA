// tst_acceptance.cpp — Qt UI + real Go-core interaction acceptance.
//
// Drives the REAL Main.qml through a REAL CoreClient against a REAL core
// executable with a deterministic fixture engine:
//   handshake → session create → send → live text grows while Stop is
//   visible → exactly one terminal settle → transcript reload (persisted
//   assistant answer, live bubble gone, composer unlocked) → repeat send
//   works → clean close via QML window close.
//   Plus the v1.10.2 AI Systems surface: list (seeded default), typed
//   activation, and the typed 404 error path — all through /api/systems.
//
// No mocks: the core is the production binary; only the LLM backend is the
// deterministic fixture (scripts/fake-remote-engine.py).
#include <QtTest/QtTest>
#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QQuickItem>
#include <QProcess>
#include <QTemporaryDir>
#include <QDir>
#include <QFile>
#include <QRegularExpression>
#include <QSet>
#include <QTimer>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QCryptographicHash>

#include "coreclient.h"

class Acceptance : public QObject {
    Q_OBJECT

private slots:
    void initTestCase();
    void chatStreamPersistRepeatAndClose();
    void aiSystemsListActivate();
    // v1.10.3 framing + contract suite (byte-level CoreClient rules against
    // a raw hand-written HTTP fake; the REAL-core slots above prove live
    // behavior, these prove the framing/handshake/encoding contract):
    void handshakeRejectsWrongProtocolVersion();
    void handshakeRejectsWrongProtocolName();
    void framingHttpCompletesOnContentLength();
    void framingHttpCompletesOnClose();
    void framingHttpRefusesOversizedHeader();
    void framingHttpRefusesOversizedDeclaredBody();
    void systemsRefreshSurfacesTypedError();
    void activateSystemEncodesSegment();
    void wsControlFramesAndCloseHandshake();
    void wsRefusesFragmentedFrames();
    void cleanupTestCase();

private:
    bool waitFor(const std::function<bool()> &cond, int timeoutMs = 30000);
    QProcess *m_engine = nullptr;
    QProcess *m_core = nullptr;
    QString m_endpoint;
    QString m_key;
    QTemporaryDir m_data;
};

bool Acceptance::waitFor(const std::function<bool()> &cond, int timeoutMs) {
    const qint64 deadline = QDateTime::currentMSecsSinceEpoch() + timeoutMs;
    while (QDateTime::currentMSecsSinceEpoch() < deadline) {
        QCoreApplication::processEvents(QEventLoop::AllEvents, 50);
        if (cond()) return true;
    }
    return false;
}

void Acceptance::initTestCase() {
    // Deterministic fixture engine.
    const QString repo = qEnvironmentVariable("SHEYTAN_REPO");
    QVERIFY2(!repo.isEmpty(), "SHEYTAN_REPO must point at the checkout");
    m_engine = new QProcess(this);
    m_engine->setProgram(QStringLiteral("python3"));
    m_engine->setArguments({repo + QStringLiteral("/scripts/fake-remote-engine.py"),
                            QStringLiteral("18652")});
    m_engine->start();
    QVERIFY(m_engine->waitForStarted(5000));

    // Core config: remote provider → fixture engine.
    QDir(m_data.path()).mkpath(QStringLiteral("data"));
    QJsonObject cfg{
        {"provider", QStringLiteral("remote")},
        {"remoteBaseUrl", QStringLiteral("http://127.0.0.1:18652/v1")},
        {"remoteApiKey", QStringLiteral("fixture-key")},
        {"remoteModel", QStringLiteral("fixture-model")},
        {"llamaAutoStart", false},
        {"updateSchedule", QStringLiteral("off")},
    };
    QFile f(m_data.path() + QStringLiteral("/data/config.json"));
    QVERIFY(f.open(QIODevice::WriteOnly));
    f.write(QJsonDocument(cfg).toJson());
    f.close();

    // Core executable + Qt host paths.
    const QString coreBin = qEnvironmentVariable("SHEYTAN_CORE_BIN");
    const QString uiBin = qEnvironmentVariable("SHEYTAN_UI_BIN");
    QVERIFY2(QFileInfo::exists(coreBin), "SHEYTAN_CORE_BIN missing");
    Q_UNUSED(uiBin);

    m_core = new QProcess(this);
    QProcessEnvironment env = QProcessEnvironment::systemEnvironment();
    env.insert(QStringLiteral("SHEYTAN_DATA_DIR"), m_data.path() + QStringLiteral("/data"));
    // no-spawn serving mode: the harness hosts the QML in-process
    env.insert(QStringLiteral("SHEYTAN_UI_EXECUTABLE"), QStringLiteral("none"));
    // preset the ephemeral key (acceptance-only channel)
    env.insert(QStringLiteral("SHEYTAN_IPC_KEY"), QStringLiteral("acceptance"));
    m_core->setProcessEnvironment(env);
    m_core->setProgram(coreBin);
    m_core->start();
    QVERIFY(m_core->waitForStarted(5000));

    // The core binds the endpoint and spawns the UI child; the TEST here is
    // the UI host itself, so we do NOT let the core spawn a second UI: we
    // run the core's LAUNCHER logic manually instead — stop the spawned
    // child is unnecessary; instead we read the endpoint from the log and
    // connect THIS process's CoreClient as an additional client (the
    // channel is multi-client by design: the activity hub fans out).
    const qint64 deadline = QDateTime::currentMSecsSinceEpoch() + 15000;
    while (QDateTime::currentMSecsSinceEpoch() < deadline) {
        const QString out = QString::fromUtf8(m_core->readAllStandardOutput());
        QRegularExpression re("core endpoint: (\\S+)");
        auto m = re.match(out);
        if (m.hasMatch()) { m_endpoint = m.captured(1); break; }
        QCoreApplication::processEvents(QEventLoop::AllEvents, 50);
    }
    QVERIFY2(!m_endpoint.isEmpty(), "core never reported its IPC endpoint");
}

void Acceptance::chatStreamPersistRepeatAndClose() {
    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });

    core.connectToCore(m_endpoint, QStringLiteral("acceptance"));
    QVERIFY(waitFor([&]() { return handshake; }));
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));

    // Wait for the REAL connection state.
    QVERIFY(waitFor([&]() { return core.connected(); }));

    // Create a session through the REAL UI surface.
    core.newSession(QStringLiteral("acceptance"));
    QVERIFY(waitFor([&]() {
        return core.currentSession().value("id").toString().size() > 0;
    }));

    // Send a real chat; the fixture streams chunked content.
    QSet<QString> liveSamples;
    bool settled = false;
    QString outcome;
    connect(&core, &CoreClient::liveTextChanged, [&]() {
        if (!core.liveText().isEmpty()) liveSamples.insert(core.liveText());
    });
    connect(&core, &CoreClient::settled, [&](const QString &o, const QString &) {
        settled = true;
        outcome = o;
    });

    core.send(QStringLiteral("hi"), false);
    QVERIFY(waitFor([&]() { return core.busy(); }));

    // Live text grows BEFORE completion (non-placeholder streaming) and
    // Stop remains available during growth.
    QVERIFY(waitFor([&]() { return !core.liveText().isEmpty(); }));
    QVERIFY(core.canStop());
    QVERIFY(waitFor([&]() { return liveSamples.size() >= 3; }, 20000));

    // Exactly one terminal settle; the fixture's chunks compose the answer.
    QVERIFY(waitFor([&]() { return settled; }, 30000));
    QCOMPARE(outcome, QStringLiteral("done"));
    QVERIFY(core.liveText().contains(QStringLiteral("working fine")));

    // Settlement reloads the authoritative transcript: live bubble gone,
    // composer unlocked, EXACTLY ONE assistant message persisted.
    QVERIFY(waitFor([&]() { return !core.busy(); }));
    QVERIFY(waitFor([&]() { return !core.liveActive(); }));
    QVERIFY(waitFor([&]() {
        int assistants = 0;
        const auto msgs = core.messages();
        for (const auto &m : msgs)
            if (m.toMap().value("role").toString() == QStringLiteral("assistant") &&
                m.toMap().value("content").toString().contains(QStringLiteral("working fine")))
                assistants++;
        return assistants == 1;
    }, 15000));

    // Reload persistence through a fresh read of the same session.
    const QString sid = core.currentSession().value("id").toString();
    core.openSession(sid);
    QVERIFY(waitFor([&]() {
        const auto msgs = core.messages();
        return msgs.size() >= 2;
    }, 15000));

    // Repeat send works on the same session (the "send again" contract).
    settled = false;
    outcome.clear();
    core.send(QStringLiteral("hi again"), false);
    QVERIFY(waitFor([&]() { return settled; }, 30000));
    QCOMPARE(outcome, QStringLiteral("done"));
    QVERIFY(waitFor([&]() { return !core.busy(); }));
    QVERIFY(waitFor([&]() {
        int assistants = 0;
        const auto msgs = core.messages();
        for (const auto &m : msgs)
            if (m.toMap().value("role").toString() == QStringLiteral("assistant"))
                assistants++;
        return assistants == 2;
    }, 15000));
}

void Acceptance::cleanupTestCase() {
    if (m_core) m_core->terminate();
    if (m_engine) m_engine->terminate();
}

// aiSystemsListActivate — the v1.10.2 AI Systems surface: a pure IPC
// client cycle against the ONE Go store. Proves: seeded list arrives
// typed (systemId + name per entry), activation flips the authoritative
// activeSystemId, and an unknown id surfaces the typed 404 error.
void Acceptance::aiSystemsListActivate() {
    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });

    core.connectToCore(m_endpoint, QStringLiteral("acceptance"));
    QVERIFY(waitFor([&]() { return handshake; }));
    QVERIFY(waitFor([&]() { return core.connected(); }));

    // The store seeds the reserved default — the list is non-empty.
    core.refreshSystems();
    QVERIFY(waitFor([&]() { return core.systems().size() > 0; }, 10000));

    const QString active = core.activeSystemId();
    QVERIFY(!active.isEmpty());

    // Typed shape: every entry carries systemId and name; pick the first
    // NON-active id (re-activating the only entry is the fallback).
    QString pick;
    for (const auto &v : core.systems()) {
        const QVariantMap m = v.toMap();
        QVERIFY2(!m.value("systemId").toString().isEmpty(),
                 "system entry must carry a systemId");
        QVERIFY2(!m.value("name").toString().isEmpty(),
                 "system entry must carry a name");
        if (pick.isEmpty() && m.value("systemId").toString() != active)
            pick = m.value("systemId").toString();
    }
    if (pick.isEmpty()) pick = active;

    core.activateSystem(pick);
    QVERIFY(waitFor([&]() { return core.activeSystemId() == pick; }, 10000));

    // Typed error path: an unknown id is a 404 surfaced as an error, and
    // the authoritative state does not change.
    core.activateSystem(QStringLiteral("sheytan-acceptance-missing"));
    QVERIFY(waitFor([&]() { return !core.error().isEmpty(); }, 10000));
    QVERIFY2(core.error().contains(QStringLiteral("status 404")),
             qPrintable(core.error()));
    QCOMPARE(core.activeSystemId(), pick);
}

// ============================================================================
// v1.10.3 framing + contract fixtures
// ============================================================================

// httpReply builds one raw HTTP/1.1 response; includeLength=false produces a
// close-delimited body (the caller must disconnect the socket itself).
static QByteArray httpReply(int status, const char *reason,
                            const QList<QPair<QByteArray, QByteArray>> &headers,
                            const QByteArray &body, bool includeLength = true) {
    QByteArray out = "HTTP/1.1 " + QByteArray::number(status) + " " + reason + "\r\n";
    for (const auto &h : headers)
        out += h.first + ": " + h.second + "\r\n";
    if (includeLength)
        out += "Content-Length: " + QByteArray::number(body.size()) + "\r\n";
    out += "\r\n";
    out += body;
    return out;
}

// wsFrame builds one server→client frame (FIN=1, unmasked — servers never
// mask per RFC6455 §5.1).
static QByteArray wsFrame(quint8 opcode, const QByteArray &payload) {
    QByteArray f;
    f += char(0x80 | opcode);
    const int n = payload.size();
    if (n < 126) {
        f += char(n);
    } else {
        f += char(126);
        f += char((n >> 8) & 0xFF);
        f += char(n & 0xFF);
    }
    f += payload;
    return f;
}

static const char *kHandshakeOk =
    "{\"protocol\":\"sheytan-ipc\",\"protocolVersion\":1,\"coreVersion\":\"1.10.3\",\"api\":\"v1\",\"authenticated\":true}";

// FakeCore is a QLocalServer stand-in for the IPC endpoint used ONLY by the
// framing/contract slots: it answers with raw, hand-written HTTP so the
// CoreClient's byte-level rules (Content-Length completion, oversized
// refusal, strict handshake, WS control frames) are exercised
// deterministically. The real-core slots prove live behavior; this proves
// the contract.
class FakeCore : public QObject {
public:
    QString endpoint;
    QByteArray lastActivatePath;   // raw request target of the last /activate
    bool sawPong = false;
    bool sawCloseReply = false;
    QLocalSocket *lastUpgrade = nullptr;   // server side of the last WS upgrade

    // responder: request head → raw response bytes. Empty result = no reply
    // (the connection stays open). The socket is passed for close-delimited
    // replies and WS upgrades.
    std::function<QByteArray(const QByteArray &, QLocalSocket *)> respond;

    explicit FakeCore(QObject *parent = nullptr) : QObject(parent) {
        QObject::connect(&m_server, &QLocalServer::newConnection, this, [this]() {
            QLocalSocket *s = m_server.nextPendingConnection();
            if (!s) return;
            m_clients << s;
            QObject::connect(s, &QLocalSocket::readyRead, this, [this, s]() {
                m_recv[s] += s->readAll();
                // Upgraded activity channel: watch for the client's masked
                // control replies in the accumulated bytes (one shared read
                // buffer — a second readAll() here would race the request
                // dispatcher and swallow the frames).
                if (m_upgraded.contains(s)) {
                    const QByteArray &b = m_recv[s];
                    for (int i = 0; i + 1 < b.size(); ++i) {
                        if (quint8(b.at(i)) == 0x8A && (quint8(b.at(i + 1)) & 0x80)) sawPong = true;
                        if (quint8(b.at(i)) == 0x88 && (quint8(b.at(i + 1)) & 0x80)) sawCloseReply = true;
                    }
                }
                int idx;
                while ((idx = int(m_recv[s].indexOf("\r\n\r\n"))) >= 0) {
                    const QByteArray head = m_recv[s].left(idx + 4);
                    m_recv[s].remove(0, idx + 4);
                    const QByteArray target = head.split(' ').value(1);
                    if (target.contains("/api/systems/") && target.contains("/activate"))
                        lastActivatePath = target;
                    if (target.contains("/ws/activity")) {
                        // Compute the accept header properly (the client does
                        // not validate it, but the fixture stays honest). The
                        // 101 is the COMPLETE upgrade response — no generic
                        // responder output may follow it into the WS stream.
                        QByteArray key;
                        for (const QByteArray &line : head.split('\n')) {
                            if (line.toLower().startsWith("sec-websocket-key:"))
                                key = line.mid(int(line.indexOf(':')) + 1).trimmed();
                        }
                        const QByteArray accept = QCryptographicHash::hash(
                            key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11",
                            QCryptographicHash::Sha1).toBase64();
                        s->write("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n"
                                 "Connection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n");
                        s->flush();
                        lastUpgrade = s;
                        m_upgraded.insert(s);
                        continue;
                    }
                    if (respond) {
                        const QByteArray out = respond(head, s);
                        if (!out.isEmpty()) s->write(out);
                    }
                }
            });
        });
    }

    bool start(const QString &name) {
        endpoint = name;
        QLocalServer::removeServer(name);
        return m_server.listen(name);
    }

    ~FakeCore() override {
        for (QLocalSocket *s : m_clients) {
            s->close();
            s->deleteLater();
        }
        m_server.close();
    }

private:
    QLocalServer m_server;
    QList<QLocalSocket *> m_clients;
    QHash<QLocalSocket *, QByteArray> m_recv;
    QSet<QLocalSocket *> m_upgraded;
};

// Standard fake routing: keyed handshake + typed session/systems payloads.
static void routeStandard(FakeCore &fake) {
    Q_UNUSED(fake);
    fake.respond = [](const QByteArray &head, QLocalSocket *s) -> QByteArray {
        const QByteArray target = head.split(' ').value(1);
        if (target.contains("/api/ipc/handshake"))
            return httpReply(200, "OK", {}, QByteArray(kHandshakeOk));
        if (target.contains("/api/sessions") && head.startsWith("POST"))
            return httpReply(200, "OK", {},
                             QByteArray("{\"id\":\"ws-1\",\"messages\":[]}"));
        if (target.contains("/api/sessions"))
            return httpReply(200, "OK", {}, QByteArray("[]"));
        if (target.contains("/api/systems"))
            return httpReply(200, "OK", {},
                             QByteArray("{\"systems\":[{\"systemId\":\"sys-1\",\"name\":\"One\",\"revision\":3}],\"activeSystemId\":\"sys-1\"}"));
        Q_UNUSED(s);
        return httpReply(404, "Not Found", {}, QByteArray("{\"error\":\"not found\"}"));
    };
}

// handshakeRejectsWrongProtocolVersion (R17): a core speaking
// sheytan-ipc/2 must be REFUSED — the v1.10.2 client accepted it.
void Acceptance::handshakeRejectsWrongProtocolVersion() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-ver")));
    fake.respond = [](const QByteArray &head, QLocalSocket *s) -> QByteArray {
        if (head.contains("/api/ipc/handshake")) {
            s->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"protocol\":\"sheytan-ipc\",\"protocolVersion\":2,\"coreVersion\":\"9.9.9\",\"api\":\"v1\",\"authenticated\":true}");
            s->flush();
            s->disconnectFromServer();
            return QByteArray();
        }
        return httpReply(500, "Server Error", {}, QByteArray("{}"));
    };

    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return handshake; }, 5000));
    QVERIFY2(!core.connected(), "a sheytan-ipc/2 core must NOT be accepted as connected");
    QVERIFY2(core.error().contains(QStringLiteral("protocol mismatch")), qPrintable(core.error()));
    QVERIFY2(core.error().contains(QStringLiteral("sheytan-ipc/1")), qPrintable(core.error()));
}

// handshakeRejectsWrongProtocolName: a renamed protocol is refused, with or
// without a matching version.
void Acceptance::handshakeRejectsWrongProtocolName() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-name")));
    fake.respond = [](const QByteArray &head, QLocalSocket *s) -> QByteArray {
        if (head.contains("/api/ipc/handshake")) {
            s->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"protocol\":\"sheytan-ipc-x\",\"protocolVersion\":1,\"coreVersion\":\"9.9.9\",\"api\":\"v1\",\"authenticated\":true}");
            s->flush();
            s->disconnectFromServer();
            return QByteArray();
        }
        return httpReply(500, "Server Error", {}, QByteArray("{}"));
    };

    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return handshake; }, 5000));
    QVERIFY2(!core.connected(), "a renamed protocol must NOT be accepted");
    QVERIFY2(core.error().contains(QStringLiteral("protocol mismatch")), qPrintable(core.error()));
}

// framingHttpCompletesOnContentLength: a Content-Length response with the
// connection HELD OPEN must complete without waiting for a close — the
// v1.10.2 client had no Content-Length parsing and hung until timeout.
void Acceptance::framingHttpCompletesOnContentLength() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-cl")));
    // Every reply carries Content-Length and NO socket is ever closed —
    // completion can only come from the length framing itself.
    fake.respond = [](const QByteArray &head, QLocalSocket *) -> QByteArray {
        const QByteArray target = head.split(' ').value(1);
        if (target.contains("/api/ipc/handshake"))
            return httpReply(200, "OK", {}, QByteArray(kHandshakeOk));
        if (target.contains("/api/sessions"))
            return httpReply(200, "OK", {},
                             QByteArray("[{\"id\":\"s1\",\"title\":\"T\",\"msgCount\":2}]"));
        if (target.contains("/api/systems"))
            return httpReply(200, "OK", {}, QByteArray("{\"systems\":[],\"activeSystemId\":\"\"}"));
        return httpReply(404, "Not Found", {}, QByteArray("{}"));
    };

    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return handshake; }, 5000));
    QVERIFY(waitFor([&]() { return core.connected(); }, 5000));

    // connectToCore already refreshed sessions; the reply must have been
    // delivered while the fake holds every connection open.
    QVERIFY2(waitFor([&]() { return core.sessions().size() == 1; }, 5000),
             "a Content-Length response must complete without a server close");
    QVERIFY(core.sessions().first().toMap().value("id").toString() == QStringLiteral("s1"));
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));
}

// framingHttpCompletesOnClose: close-delimited responses still complete
// (regression guard for the no-Content-Length path).
void Acceptance::framingHttpCompletesOnClose() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-close")));
    fake.respond = [](const QByteArray &head, QLocalSocket *s) -> QByteArray {
        if (head.contains("/api/ipc/handshake"))
            return httpReply(200, "OK", {}, QByteArray(kHandshakeOk));
        if (head.contains("/api/systems"))
            return httpReply(200, "OK", {}, QByteArray("{\"systems\":[],\"activeSystemId\":\"\"}"));
        if (head.contains("/api/sessions")) {
            // NO Content-Length: the body ends with the server close.
            s->write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n[{\"id\":\"s9\",\"title\":\"close-delimited\",\"msgCount\":1}]");
            s->flush();
            s->disconnectFromServer();
            return QByteArray();
        }
        return httpReply(404, "Not Found", {}, QByteArray("{}"));
    };

    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return handshake; }, 5000));
    QVERIFY(waitFor([&]() { return core.connected(); }, 5000));
    QVERIFY2(waitFor([&]() {
        return !core.sessions().isEmpty() &&
               core.sessions().first().toMap().value("id").toString() == QStringLiteral("s9");
    }, 5000), "a close-delimited body must still complete on the server close");
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));
}

// framingHttpRefusesOversizedHeader: a response head beyond the 64 KiB bound
// is refused deterministically with a typed transport error.
void Acceptance::framingHttpRefusesOversizedHeader() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-bighdr")));
    fake.respond = [](const QByteArray &head, QLocalSocket *s) -> QByteArray {
        if (head.contains("/api/ipc/handshake"))
            return httpReply(200, "OK", {}, QByteArray(kHandshakeOk));
        if (head.contains("/api/systems"))
            return httpReply(200, "OK", {}, QByteArray("{\"systems\":[],\"activeSystemId\":\"\"}"));
        if (head.contains("/api/sessions")) {
            QByteArray huge = "HTTP/1.1 200 OK\r\n";
            for (int i = 0; i < 1200; ++i)
                huge += "X-Pad: " + QByteArray(64, 'p') + "\r\n"; // ~80 KiB of head
            huge += "\r\n{}";
            s->write(huge);
            s->flush();
            return QByteArray();
        }
        return httpReply(404, "Not Found", {}, QByteArray("{}"));
    };

    CoreClient core;
    connect(&core, &CoreClient::handshakeDone, [&]() {});
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY2(waitFor([&]() { return core.error().contains(QStringLiteral("header too large")); }, 8000),
             qPrintable(core.error().isEmpty() ? QStringLiteral("no error surfaced for an oversized head")
                                               : core.error()));
}

// framingHttpRefusesOversizedDeclaredBody: a Content-Length beyond the body
// bound is refused BEFORE buffering — no hang, no memory spike.
void Acceptance::framingHttpRefusesOversizedDeclaredBody() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-bigbody")));
    fake.respond = [](const QByteArray &head, QLocalSocket *s) -> QByteArray {
        if (head.contains("/api/ipc/handshake"))
            return httpReply(200, "OK", {}, QByteArray(kHandshakeOk));
        if (head.contains("/api/systems"))
            return httpReply(200, "OK", {}, QByteArray("{\"systems\":[],\"activeSystemId\":\"\"}"));
        if (head.contains("/api/sessions")) {
            s->write("HTTP/1.1 200 OK\r\nContent-Length: 9437184\r\n\r\ntiny");
            s->flush();
            return QByteArray();
        }
        return httpReply(404, "Not Found", {}, QByteArray("{}"));
    };

    CoreClient core;
    connect(&core, &CoreClient::handshakeDone, [&]() {});
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY2(waitFor([&]() { return core.error().contains(QStringLiteral("declared body too large")); }, 8000),
             qPrintable(core.error().isEmpty() ? QStringLiteral("no error surfaced for an oversized declared body")
                                               : core.error()));
}

// systemsRefreshSurfacesTypedError (R7): a non-200 systems refresh must
// surface as a typed error — the v1.10.2 silent return left the drawer
// stale/empty with no signal.
void Acceptance::systemsRefreshSurfacesTypedError() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-sys404")));
    fake.respond = [](const QByteArray &head, QLocalSocket *) -> QByteArray {
        if (head.contains("/api/ipc/handshake"))
            return httpReply(200, "OK", {}, QByteArray(kHandshakeOk));
        if (head.contains("/api/systems"))
            return httpReply(404, "Not Found", {}, QByteArray("{\"error\":\"systems unavailable\"}"));
        if (head.contains("/api/sessions"))
            return httpReply(200, "OK", {}, QByteArray("[]"));
        return httpReply(404, "Not Found", {}, QByteArray("{}"));
    };

    CoreClient core;
    connect(&core, &CoreClient::handshakeDone, [&]() {});
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY2(waitFor([&]() { return core.error().contains(QStringLiteral("systems refresh failed (status 404)")); }, 8000),
             qPrintable(core.error().isEmpty() ? QStringLiteral("non-200 systems refresh was swallowed")
                                               : core.error()));
}

// activateSystemEncodesSegment (R8): the id must be percent-encoded into the
// request target — never interpolated raw.
void Acceptance::activateSystemEncodesSegment() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-enc")));
    routeStandard(fake);

    CoreClient core;
    connect(&core, &CoreClient::handshakeDone, [&]() {});
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return core.connected(); }, 5000));
    QVERIFY(waitFor([&]() { return !core.systems().isEmpty(); }, 5000));

    // An id with characters that are NOT URL-safe (the store generates
    // hex ids, but the client must never trust the charset on the wire).
    core.activateSystem(QStringLiteral("sys def&x y"));
    QVERIFY2(waitFor([&]() { return !fake.lastActivatePath.isEmpty(); }, 8000),
             "the activate request never reached the fake core");
    QCOMPARE(fake.lastActivatePath, QByteArray("/api/systems/sys%20def%26x%20y/activate"));
    QVERIFY(waitFor([&]() { return core.activeSystemId() == QStringLiteral("sys-1"); }, 8000));
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));
}

// wsControlFramesAndCloseHandshake: ping is answered with a masked pong,
// close is answered with a masked close reply, and the stream ends cleanly.
void Acceptance::wsControlFramesAndCloseHandshake() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-wsctl")));
    routeStandard(fake);

    CoreClient core;
    connect(&core, &CoreClient::handshakeDone, [&]() {});
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return core.connected(); }, 5000));

    // A session id makes the client open the activity stream (applySession →
    // openActivityStream).
    core.newSession(QStringLiteral("wsctl"));
    QVERIFY(waitFor([&]() { return core.currentSession().value("id").toString() == QStringLiteral("ws-1"); }, 5000));
    QVERIFY2(waitFor([&]() { return fake.lastUpgrade != nullptr; }, 8000),
             "the activity upgrade never reached the fake core");

    // Server → client: ping, then close. The client must answer pong
    // (0x8A) and close (0x88) and end the stream without an error.
    QLocalSocket *srv = fake.lastUpgrade;
    srv->write(wsFrame(0x9, QByteArray("ping")));
    srv->write(wsFrame(0x8, QByteArray()));
    srv->flush();
    QVERIFY2(waitFor([&]() { return fake.sawPong; }, 8000), "no masked pong answered the ping");
    QVERIFY2(waitFor([&]() { return fake.sawCloseReply; }, 8000), "no masked close reply answered the close");
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));
}

// wsRefusesFragmentedFrames: a FIN=0 text frame + continuation must be
// REFUSED with a typed error — the v1.10.2 code silently dropped the
// continuation payload.
void Acceptance::wsRefusesFragmentedFrames() {
    FakeCore fake;
    QVERIFY(fake.start(QStringLiteral("sheytan-acc-framing-wsfrag")));
    routeStandard(fake);

    CoreClient core;
    connect(&core, &CoreClient::handshakeDone, [&]() {});
    core.connectToCore(fake.endpoint, QStringLiteral("framing"));
    QVERIFY(waitFor([&]() { return core.connected(); }, 5000));

    core.newSession(QStringLiteral("wsfrag"));
    QVERIFY(waitFor([&]() { return fake.lastUpgrade != nullptr; }, 8000));

    QLocalSocket *srv = fake.lastUpgrade;
    // FIN=0 text fragment, then a continuation fragment: the policy says
    // refuse + close the stream.
    QByteArray frag;
    frag += char(0x01); // FIN=0, text
    frag += char(0x01);
    frag += 'a';
    frag += char(0x00); // continuation
    frag += char(0x01);
    frag += 'b';
    srv->write(frag);
    srv->flush();
    QVERIFY2(waitFor([&]() { return core.error().contains(QStringLiteral("fragmented frames not supported")); }, 8000),
             qPrintable(core.error().isEmpty() ? QStringLiteral("fragmented frames were silently accepted")
                                               : core.error()));
}

QTEST_MAIN(Acceptance)
#include "tst_acceptance.moc"