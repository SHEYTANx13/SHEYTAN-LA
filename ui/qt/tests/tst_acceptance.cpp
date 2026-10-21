// tst_acceptance.cpp — acceptance suite, honestly labeled (v1.10.13 T-INTEGRITY).
//
// LAYER A — CoreClient/IPC integration (the proven surface of every slot
// below except one): the REAL core executable over the REAL local IPC
// channel with the deterministic fixture engine:
//   handshake → session create → send → live text grows while Stop is
//   visible → exactly one terminal settle → transcript reload (persisted
//   assistant answer, live bubble gone, composer unlocked) → repeat send
//   works → clean close. Plus the AI Systems surface, the Goals surface
//   (/api/goals list/control/decisions) and the byte-level framing and
//   contract suite. Every Layer-A slot drives CoreClient DIRECTLY — no
//   QML is loaded by these slots. The former header claim "drives the
//   REAL Main.qml" was FALSE (QQmlApplicationEngine was included but
//   never constructed) and is re-labeled by this comment; nothing else
//   about the suite changed.
// LAYER B — genuine QML, bounded to what the budget allows (v1.10.13):
//   qmlLoadsCleanly is the ONE QML slot: a real QQmlApplicationEngine
//   loads the production Main.qml (the exact file the sheytan-ui host
//   ships) offscreen and asserts object creation. The full interactive
//   QML acceptance — composer → live bubble → settle → persistence
//   through real QML controls — remains FUTURE WORK and is never
//   claimed by this suite.
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
#include <QFileInfo>
#include <QUrl>
#include <QRegularExpression>
#include <QSet>
#include <QTimer>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QCryptographicHash>

// §10 diagnostics-channel contract (v1.10.13 cycle, Task ID 28): every
// dump line must reach BOTH channels — the QtTest -o report (QWARN, the
// record of record) AND native stderr, independent of the QtTest logger.
#include <cstdio>

#include "coreclient.h"

static void dumpChatPersistDiagnostics(const CoreClient &, const QString &,
                                       const QString &);

class Acceptance : public QObject {
    Q_OBJECT

private slots:
    void initTestCase();
    void chatStreamPersistRepeatAndClose();
    void aiSystemsListActivate();
    // v1.10.13 Goals surface (roadmap Phase 2): pure IPC client cycle of the
    // ONE Go goal engine (/api/goals) — the list field contract, the full
    // start→pause→resume→cancel control round trip, and the durable
    // approve/reject decision path on goals parked at waiting_for_approval.
    void goalsListFieldContract();
    void goalsStartPauseResumeCancelRoundTrip();
    void goalsApproveRejectParkedGoal();
    // v1.10.13 T-INTEGRITY (layer B): the ONE genuine-QML slot — the
    // production Main.qml loads in a real engine, offscreen, and creates
    // its root object. The interactive QML journey is future work.
    void qmlLoadsCleanly();
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
    void wsReassemblesFragmentedMessages();
    void cleanupTestCase();

private:
    bool waitFor(const std::function<bool()> &cond, int timeoutMs = 30000);
    // v1.10.13 Goals observability: the client owns NO goal state beyond
    // its last read (the coreclient.cpp contract), and the drive mutates
    // the durable store ASYNCHRONOUSLY — an async transition is observable
    // only by RE-READING the authoritative list. Same deadline discipline
    // as waitFor; conditions and bounds unchanged, nothing weakened.
    bool waitForGoalState(CoreClient *core, const std::function<bool()> &cond,
                          int timeoutMs);
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

// waitForGoalState — observe the DURABLE goal store through the client's
// one contract: "the client keeps no goal state beyond the last list read"
// (coreclient.cpp). The former goals slots polled core.goals() while the
// drive mutated the store ASYNCHRONOUSLY and nothing ever re-read the list,
// so the waits could provably never observe the transitions (the run #17
// / run #23 Windows exit-2 class). Each iteration pumps the event loop,
// re-reads the authoritative list via refreshGoals(), and re-checks the
// SAME condition under the SAME bound — no assertion is weakened, skipped
// or sleep-replaced; the waits gain the observability the store contract
// requires (the POST /start control returns before the drive has run its
// first model step, so the one-shot post-action refresh cannot carry the
// transition it precedes).
bool Acceptance::waitForGoalState(CoreClient *core, const std::function<bool()> &cond,
                                  int timeoutMs) {
    const qint64 deadline = QDateTime::currentMSecsSinceEpoch() + timeoutMs;
    while (QDateTime::currentMSecsSinceEpoch() < deadline) {
        QCoreApplication::processEvents(QEventLoop::AllEvents, 50);
        if (cond()) return true;
        core->refreshGoals();
        // Pump ~300ms so the GET /api/goals round trip and its callback
        // land on this thread's event loop before the next check.
        const qint64 pumpUntil = QDateTime::currentMSecsSinceEpoch() + 300;
        while (QDateTime::currentMSecsSinceEpoch() < pumpUntil)
            QCoreApplication::processEvents(QEventLoop::AllEvents, 25);
    }
    return false;
}

void Acceptance::initTestCase() {
    // Deterministic fixture engine.
    const QString repo = qEnvironmentVariable("SHEYTAN_REPO");
    QVERIFY2(!repo.isEmpty(), "SHEYTAN_REPO must point at the checkout");
    // v1.10.13 P-PY: resolve the fixture interpreter platform-robustly.
    // The former hardcoded python3 is NOT contractually present on Windows
    // runners (they expose python; python3.exe is best-effort) — had it
    // failed to resolve, the live-answer acceptance gate would have died
    // at waitForStarted with no named interpreter in the output.
    // Candidates follow the repo's own e2e fixture precedent
    // (e2e/fixtures/sheytan-server.ts uses python on win32, python3
    // elsewhere); the chosen interpreter is ASSERTED in the harness
    // output below. A candidate that cannot start is skipped; any LATER
    // engine failure (script error, crash mid-run) is caught by the real
    // gates and is never retried here.
    m_engine = new QProcess(this);
#ifdef Q_OS_WINDOWS
    const QStringList candidates{
        QStringLiteral("python"), QStringLiteral("python3")};
#else
    const QStringList candidates{
        QStringLiteral("python3"), QStringLiteral("python")};
#endif
    m_engine->setArguments({repo + QStringLiteral("/scripts/fake-remote-engine.py"),
                            QStringLiteral("18652")});
    QString chosen;
    for (const QString &candidate : candidates) {
        m_engine->setProgram(candidate);
        m_engine->start();
        if (m_engine->waitForStarted(5000)) { chosen = candidate; break; }
        m_engine->close();
    }
    QVERIFY2(!chosen.isEmpty(),
             "no fixture interpreter could start (tried python3 and python) "
             "— the acceptance gate requires a working Python on PATH");
    qInfo("acceptance fixture interpreter: %s (candidates tried: %s)",
          qPrintable(chosen), qPrintable(candidates.join(QStringLiteral(", "))));

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
    // v1.10.13 run #26: the wait keeps its exact condition and 15s bound;
    // a timeout now QWARNs the observable state before the assertion
    // fires (the Windows-only first-wait failure becomes readable).
    const bool firstPersisted = waitFor([&]() {
        int assistants = 0;
        const auto msgs = core.messages();
        for (const auto &m : msgs)
            if (m.toMap().value("role").toString() == QStringLiteral("assistant") &&
                m.toMap().value("content").toString().contains(QStringLiteral("working fine")))
                assistants++;
        return assistants == 1;
    }, 15000);
    if (!firstPersisted) {
        dumpChatPersistDiagnostics(core, m_endpoint, QStringLiteral("first-persisted-assistant==1"));
    }
    QVERIFY(firstPersisted);

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
    // v1.10.13 run #26: same instrumentation contract on the repeat-send
    // wait — condition and bound unchanged, a timeout names the state.
    const bool repeatPersisted = waitFor([&]() {
        int assistants = 0;
        const auto msgs = core.messages();
        for (const auto &m : msgs)
            if (m.toMap().value("role").toString() == QStringLiteral("assistant"))
                assistants++;
        return assistants == 2;
    }, 15000);
    if (!repeatPersisted) {
        dumpChatPersistDiagnostics(core, m_endpoint, QStringLiteral("repeat-assistant-count==2"));
    }
    QVERIFY(repeatPersisted);
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
// v1.10.13 Goals surface (real core, real engine, /api/goals over IPC)
// ============================================================================

// ipcRequest performs ONE raw HTTP/1.1 request over the real local IPC
// channel (the same wire CoreClient speaks) and returns {status, body}. The
// goals slots use it to seed fixtures (an ask-all AI System, unstarted
// goals) WITHOUT widening the CoreClient surface beyond the roadmap's seven
// goal invokables: the client contract stays exactly list + control +
// decisions, and the fixtures still travel the real channel.
static QPair<int, QByteArray> ipcRequest(const QString &endpoint, const QString &key,
                                         const char *method, const QString &path,
                                         const QByteArray &body) {
    QLocalSocket sock;
    sock.connectToServer(endpoint);
    if (!sock.waitForConnected(5000)) return {0, QByteArray()};

    QByteArray req;
    req += QByteArray(method) + " " + path.toUtf8() + " HTTP/1.1\r\n";
    req += "Host: sheytan-core\r\n";
    if (!key.isEmpty()) req += "X-SHEYTAN-IPC-Key: " + key.toUtf8() + "\r\n";
    if (!body.isEmpty()) {
        req += "Content-Type: application/json\r\n";
        req += "Content-Length: " + QByteArray::number(body.size()) + "\r\n";
    }
    req += "Connection: close\r\n\r\n";
    req += body;
    sock.write(req);

    QByteArray all;
    const qint64 deadline = QDateTime::currentMSecsSinceEpoch() + 10000;
    while (QDateTime::currentMSecsSinceEpoch() < deadline) {
        all += sock.readAll();
        if (sock.state() != QLocalSocket::ConnectedState) break;
        sock.waitForReadyRead(100);
    }
    const int headEnd = int(all.indexOf("\r\n\r\n"));
    if (headEnd < 0) return {0, QByteArray()};
    const QByteArray statusLine = all.left(int(all.indexOf("\r\n")));
    const QList<QByteArray> parts = statusLine.split(' ');
    return {parts.value(1).toInt(), all.mid(headEnd + 4)};
}

// goalById projects the authoritative CoreClient goals list onto one goal.
static QVariantMap goalById(const QVariantList &goals, const QString &id) {
    for (const auto &v : goals) {
        const QVariantMap g = v.toMap();
        if (g.value("goalId").toString() == id) return g;
    }
    return {};
}

// emitDumpLine — §10 DUAL-CHANNEL emission (run-#28 repair): one local
// QString, emitted byte-identically through BOTH channels —
//   (a) QWARN → the QtTest -o report (the record of record), and
//   (b) std::fputs + std::fflush to native stderr, independent of the
//       QtTest logger. C1 (Qt Test docs, "-o filename,format"): with an
//       -o file given, ALL QtTest output routes to the report file and
//       the console captures stay empty BY DOCUMENTATION — so the native
//       stderr channel is what the workflow's stderr captures and failure
//       summaries can always see, and the report keeps the QWARN form.
// No assertion, condition or bound is touched here; this widens the
// transport of the diagnostics only.
static void emitDumpLine(const QString &line)
{
    QWARN(qPrintable(line));
    std::fputs(qPrintable(line), stderr);
    std::fputs("\n", stderr);
    std::fflush(stderr);
}

// dumpGoalDiagnostics — the failure snapshot the run #24 test lacked (I5:
// the failure NAMES its state). On a bounded wait timeout, read each
// goal's durable document through the real IPC channel and print status,
// phase, the live driving flag, last action and the LAST evidence record
// BEFORE the assertion fails — the hosted run then shows exactly which
// phase stalled and why (the swallowed-error class died here with zero
// diagnostics). §10: every line rides BOTH channels (emitDumpLine) —
// the QtTest -o report AND native stderr.
static void dumpGoalDiagnostics(const QString &endpoint, const QStringList &ids)
{
    for (const QString &id : ids) {
        const auto item = ipcRequest(endpoint, QStringLiteral("acceptance"), "GET",
                                     QStringLiteral("/api/goals/") + id, QByteArray());
        if (item.first != 200) {
            emitDumpLine(QStringLiteral("goal %1 → GET failed (status %2)")
                             .arg(id).arg(item.first));
            continue;
        }
        // QJsonValue has no toMap() (run #25 C2039 on both toolchains):
        // parse → object check → value("goal") → object check → variant map.
        // Every malformed path emits a named QWARN — never a silent empty map.
        const QJsonDocument diag = QJsonDocument::fromJson(item.second);
        if (!diag.isObject()) {
            emitDumpLine(QStringLiteral("goal %1 → diagnostics body is not a JSON object (bytes=%2)")
                             .arg(id).arg(item.second.size()));
            continue;
        }
        const QJsonValue goalVal = diag.object().value(QStringLiteral("goal"));
        if (!goalVal.isObject()) {
            emitDumpLine(QStringLiteral("goal %1 → diagnostics JSON carries no object 'goal' field")
                             .arg(id));
            continue;
        }
        const QVariantMap g = goalVal.toObject().toVariantMap();
        const auto ev = g.value("evidence").toList();
        QString lastEv;
        if (!ev.isEmpty()) {
            const QVariantMap em = ev.last().toMap();
            lastEv = QStringLiteral("%1 [%2] %3")
                         .arg(em.value("action").toString(),
                              em.value("status").toString(),
                              em.value("detail").toString().left(160));
        }
        emitDumpLine(QStringLiteral(
                         "goal %1 → status=%2 phase=%3 driving=%4 lastAction=%5 lastEvidence=%6")
                         .arg(id,
                              g.value("status").toString(),
                              g.value("phase").toString(),
                              g.value("driving").toBool()
                                  ? QStringLiteral("true")
                                  : QStringLiteral("false"),
                              g.value("lastAction").toString(),
                              lastEv));
    }

    // v1.10.13 run #33 R1 discriminator (I6 at the LIST boundary): both
    // HTTP views read the SAME durable documents, so a parked goal the
    // single-goal GET shows while the list never carries it names the LIST
    // PROJECTION as the failing layer; a list that carries the identical
    // status exonerates the serializer and moves the delta to the client's
    // list parsing. ONE raw list read per dump: per requested id, the wire
    // fields the LIST item actually carries — the exact bytes the client's
    // list model parses (no client code between this body and the slot's
    // condition). Both channels; no assertion or bound is touched.
    const auto list = ipcRequest(endpoint, QStringLiteral("acceptance"), "GET",
                                 QStringLiteral("/api/goals"), QByteArray());
    if (list.first != 200) {
        emitDumpLine(QStringLiteral("goal LIST → GET /api/goals failed (status %1)")
                         .arg(list.first));
        return;
    }
    const QJsonDocument ld = QJsonDocument::fromJson(list.second);
    if (!ld.isObject()) {
        emitDumpLine(QStringLiteral("goal LIST → list body is not a JSON object (bytes=%1)")
                         .arg(list.second.size()));
        return;
    }
    const auto items = ld.object().value(QStringLiteral("goals")).toArray();
    for (const QString &id : ids) {
        bool found = false;
        for (const auto &iv : items) {
            const QJsonObject im = iv.toObject();
            if (im.value(QStringLiteral("goalId")).toString() != id) continue;
            found = true;
            const QJsonValue stv = im.value(QStringLiteral("status"));
            const QString st = stv.toString();
            QString rawNote;
            if (st != QStringLiteral("waiting_for_approval")) {
                rawNote = QStringLiteral(" rawStatusJson=") + QString::fromUtf8(
                    QJsonDocument(QJsonObject{{QStringLiteral("status"), stv}})
                        .toJson(QJsonDocument::Compact));
            }
            emitDumpLine(QStringLiteral(
                             "goal %1-list → status=%2 phase=%3 pendingApproval=%4 toolName=%5 drivingField=%6%7")
                             .arg(id,
                                  st,
                                  im.value(QStringLiteral("phase")).toString(),
                                  im.value(QStringLiteral("pendingApproval")).isObject()
                                      ? QStringLiteral("present")
                                      : QStringLiteral("absent"),
                                  im.value(QStringLiteral("pendingApproval"))
                                      .toObject()
                                      .value(QStringLiteral("toolName"))
                                      .toString(),
                                  im.contains(QStringLiteral("driving"))
                                      ? QStringLiteral("present")
                                      : QStringLiteral("absent"),
                                  rawNote));
            break;
        }
        if (!found) {
            emitDumpLine(QStringLiteral("goal %1-list → MISSING from list (items=%2)")
                             .arg(id).arg(items.size()));
        }
    }
}

// dumpChatPersistDiagnostics — the settle→persist snapshot for
// chatStreamPersistRepeatAndClose's two assistant-count waits (the
// Windows-only first-wait failure class). On a timed-out wait it QWARNs
// the observable facts that discriminate a slow-runner persistence race
// from a CoreClient settle→transcript-reload defect: the transcript
// size, the role list, whether any assistant content carries the
// fixture marker, and the client's own error/busy/liveActive/session
// state at the moment of the timeout. QWARN rides the QtTest -o report
// and (from this cycle) the workflow failure summaries, so the facts
// are anonymously readable on a red run — never a tail dump, the
// assertion and its bound stay exactly as they were.
static void dumpChatPersistDiagnostics(const CoreClient &core, const QString &endpoint,
                                       const QString &which)
{
    QStringList roles;
    bool anyAssistantWorkingFine = false;
    const auto msgs = core.messages();
    for (const auto &m : msgs) {
        const QVariantMap mm = m.toMap();
        const QString role = mm.value("role").toString();
        roles << role;
        if (role == QStringLiteral("assistant")
            && mm.value("content").toString().contains(QStringLiteral("working fine")))
            anyAssistantWorkingFine = true;
    }
    emitDumpLine(QStringLiteral(
                     "chat-persist[%1] → messages=%2 roles=[%3] anyAssistantWorkingFine=%4 error=%5 busy=%6 liveActive=%7 session=%8")
                     .arg(which,
                          QString::number(msgs.size()),
                          roles.join(QLatin1Char(',')),
                          anyAssistantWorkingFine ? QStringLiteral("true") : QStringLiteral("false"),
                          core.error(),
                          core.busy() ? QStringLiteral("true") : QStringLiteral("false"),
                          core.liveActive() ? QStringLiteral("true") : QStringLiteral("false"),
                          core.currentSession().value("id").toString()));

    // v1.10.13 run #33 R2 discriminator: the numbers above read ONLY the
    // client model — they cannot tell a core-persist failure from a
    // client-reload failure. ONE raw transcript read through the SAME wire
    // the reload path uses (GET /api/sessions/{id}): if the raw transcript
    // already carries the assistant reply while the client model does not,
    // the persist committed and the RELOAD path is the defect; if the raw
    // transcript lacks it too, the persist itself never committed. Both
    // channels; no assertion, condition or bound is touched.
    const QString sid = core.currentSession().value(QStringLiteral("id")).toString();
    if (sid.isEmpty()) {
        emitDumpLine(QStringLiteral("chat-persist[raw:%1] → no current session id; raw transcript not read")
                         .arg(which));
        return;
    }
    const auto raw = ipcRequest(endpoint, QStringLiteral("acceptance"), "GET",
                                QStringLiteral("/api/sessions/") + sid, QByteArray());
    if (raw.first != 200) {
        emitDumpLine(QStringLiteral("chat-persist[raw:%1] → GET /api/sessions/%2 failed (status %3)")
                         .arg(which, sid).arg(raw.first));
        return;
    }
    const QJsonDocument rd = QJsonDocument::fromJson(raw.second);
    if (!rd.isObject()) {
        emitDumpLine(QStringLiteral("chat-persist[raw:%1] → transcript body is not a JSON object (bytes=%2)")
                         .arg(which).arg(raw.second.size()));
        return;
    }
    const auto rmsgs = rd.object().value(QStringLiteral("messages")).toArray();
    QStringList rroles;
    bool rawAssistantWorkingFine = false;
    for (const auto &mv : rmsgs) {
        const QJsonObject mm = mv.toObject();
        const QString role = mm.value(QStringLiteral("role")).toString();
        rroles << role;
        if (role == QStringLiteral("assistant")
            && mm.value(QStringLiteral("content")).toString().contains(QStringLiteral("working fine")))
            rawAssistantWorkingFine = true;
    }
    emitDumpLine(QStringLiteral(
                     "chat-persist[raw:%1] → rawMessages=%2 rawRoles=[%3] rawAssistantWorkingFine=%4")
                     .arg(which,
                          QString::number(rmsgs.size()),
                          rroles.join(QLatin1Char(',')),
                          rawAssistantWorkingFine ? QStringLiteral("true") : QStringLiteral("false")));
}

// goalsListFieldContract — the list contract the client mirrors: a created
// goal is served by GET /api/goals as {"goals":[…]} carrying exactly the
// wire fields the QML cards render (goalId, status, phase, originalGoal,
// currentStep, turnBudget), and the single-goal GET carries the
// goal/driving/done/total envelope. Fixture seeding rides the raw IPC
// channel; the observed list is the CoreClient's own refreshGoals read.
void Acceptance::goalsListFieldContract() {
    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });

    core.connectToCore(m_endpoint, QStringLiteral("acceptance"));
    QVERIFY(waitFor([&]() { return handshake; }));
    QVERIFY(waitFor([&]() { return core.connected(); }));

    const auto created = ipcRequest(m_endpoint, QStringLiteral("acceptance"), "POST",
                                    QStringLiteral("/api/goals"),
                                    QJsonDocument(QJsonObject{
                                        {"goal", QStringLiteral("acceptance field contract fixture")},
                                    }).toJson());
    QCOMPARE(created.first, 200);
    const QString goalId = QJsonDocument::fromJson(created.second)
                               .object().value("goalId").toString();
    QVERIFY2(!goalId.isEmpty(), "created goal must carry goalId");

    core.refreshGoals();
    QVERIFY(waitFor([&]() { return core.goals().size() > 0; }, 10000));

    bool found = false;
    for (const auto &v : core.goals()) {
        const QVariantMap g = v.toMap();
        if (g.value("goalId").toString() != goalId) continue;
        found = true;
        QCOMPARE(g.value("status").toString(), QStringLiteral("active"));
        QCOMPARE(g.value("phase").toString(), QStringLiteral("understanding"));
        QCOMPARE(g.value("originalGoal").toString(),
                 QStringLiteral("acceptance field contract fixture"));
        QCOMPARE(g.value("currentStep").toInt(), -1);
        QVERIFY(g.value("turnBudget").toInt() > 0);
        break;
    }
    QVERIFY2(found, "the seeded goal must appear in the CoreClient goals list");

    const auto item = ipcRequest(m_endpoint, QStringLiteral("acceptance"), "GET",
                                 QStringLiteral("/api/goals/") + goalId, QByteArray());
    QCOMPARE(item.first, 200);
    const auto envelope = QJsonDocument::fromJson(item.second).object();
    for (const char *field : {"goal", "driving", "done", "total"})
        QVERIFY2(envelope.contains(QLatin1String(field)),
                 (QByteArray("single-goal GET missing field: ") + field).constData());
}

// goalsStartPauseResumeCancelRoundTrip — the full control round trip of the
// Goals surface through the real engine: start flips the bounded drive on
// (the phase leaves understanding), pause parks the goal deterministically
// (the store flip is the control surface; the drive stops at the next
// checkpoint boundary), resume re-drives from the checkpoint, cancel
// terminates — never resurrects. Every observed state is the CoreClient's
// own re-read of the authoritative list; no client-side goal state exists.
void Acceptance::goalsStartPauseResumeCancelRoundTrip() {
    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });

    core.connectToCore(m_endpoint, QStringLiteral("acceptance"));
    QVERIFY(waitFor([&]() { return handshake; }));
    QVERIFY(waitFor([&]() { return core.connected(); }));

    const auto created = ipcRequest(m_endpoint, QStringLiteral("acceptance"), "POST",
                                    QStringLiteral("/api/goals"),
                                    QJsonDocument(QJsonObject{
                                        {"goal", QStringLiteral("acceptance round trip fixture")},
                                    }).toJson());
    QCOMPARE(created.first, 200);
    const QString goalId = QJsonDocument::fromJson(created.second)
                               .object().value("goalId").toString();
    QVERIFY(!goalId.isEmpty());

    core.refreshGoals();
    QVERIFY(waitFor([&]() {
        return goalById(core.goals(), goalId).value("status").toString()
               == QStringLiteral("active");
    }, 10000));

    // Start: the bounded drive begins — the phase leaves understanding (the
    // store is the only authority for the observed transition).
    // v1.10.13: waitForGoalState — the drive runs ASYNC and the client owns
    // no goal state beyond its last read, so the transition is observed by
    // re-reading the store, never by a stale list (45s bound unchanged).
    core.startGoal(goalId);
    const bool leftUnderstanding = waitForGoalState(&core, [&]() {
        const QVariantMap g = goalById(core.goals(), goalId);
        return g.value("phase").toString() != QStringLiteral("understanding");
    }, 45000);
    if (!leftUnderstanding) {
        // v1.10.13: name the stall before failing (I5).
        dumpGoalDiagnostics(m_endpoint, {goalId});
    }
    QVERIFY(leftUnderstanding);

    // Pause: parks at the next checkpoint boundary (durable flip is
    // synchronous with the control POST; the re-read confirms it).
    core.pauseGoal(goalId);
    QVERIFY(waitForGoalState(&core, [&]() {
        return goalById(core.goals(), goalId).value("status").toString()
               == QStringLiteral("paused");
    }, 15000));

    // Resume: back to active, the drive re-runs from the checkpoint.
    core.resumeGoal(goalId);
    const bool resumed = waitForGoalState(&core, [&]() {
        return goalById(core.goals(), goalId).value("status").toString()
               == QStringLiteral("active");
    }, 15000);
    if (!resumed) {
        // v1.10.13: the former cycle's ONLY failure signal here was a bare
        // "goal resume failed (status 500)" — the snapshot names the
        // durable state (and the drive flag) before the assertion fires.
        dumpGoalDiagnostics(m_endpoint, {goalId});
    }
    QVERIFY(resumed);

    // Cancel: terminal; the re-driven loop stops at its next boundary.
    // v1.10.13 §10 (run-#28 repair): the cancel leg — the ONLY failing leg
    // of the round trip on runs #26/#28 — gains its missing dump, mirroring
    // the start and resume legs above. The assertion, its condition and
    // the 15000 ms bound are unchanged.
    core.cancelGoal(goalId);
    const bool cancelled = waitForGoalState(&core, [&]() {
        return goalById(core.goals(), goalId).value("status").toString()
               == QStringLiteral("cancelled");
    }, 15000);
    if (!cancelled) {
        dumpGoalDiagnostics(m_endpoint, {goalId});
    }
    QVERIFY(cancelled);
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));
}

// goalsApproveRejectParkedGoal — the durable approval path: a goal bound to
// an ask-all AI System parks at waiting_for_approval BEFORE its first acting
// step (the pending call is durably recorded with a stable approvalId);
// approve records the decision as goal evidence and arms the loop; reject
// records the decision and leaves the goal blocked with the pending call
// cleared. The ask-all binding is created through the raw IPC channel
// (POST /api/systems) so the CoreClient surface stays the seven invokables.
void Acceptance::goalsApproveRejectParkedGoal() {
    CoreClient core;
    bool handshake = false;
    connect(&core, &CoreClient::handshakeDone, [&]() { handshake = true; });

    core.connectToCore(m_endpoint, QStringLiteral("acceptance"));
    QVERIFY(waitFor([&]() { return handshake; }));
    QVERIFY(waitFor([&]() { return core.connected(); }));

    const auto sys = ipcRequest(m_endpoint, QStringLiteral("acceptance"), "POST",
                                QStringLiteral("/api/systems"),
                                QJsonDocument(QJsonObject{
                                    {"name", QStringLiteral("acceptance ask-all")},
                                    {"instructions",
                                     QStringLiteral("fixture policy: park before every acting step")},
                                    {"approvalPolicy", QStringLiteral("ask-all")},
                                }).toJson());
    QCOMPARE(sys.first, 200);
    const QString systemId = QJsonDocument::fromJson(sys.second)
                                 .object().value("systemId").toString();
    QVERIFY(!systemId.isEmpty());

    auto parkGoal = [&](const QString &text) -> QString {
        const auto created = ipcRequest(m_endpoint, QStringLiteral("acceptance"), "POST",
                                        QStringLiteral("/api/goals"),
                                        QJsonDocument(QJsonObject{
                                            {"goal", text},
                                            {"systemId", systemId},
                                            {"start", true},
                                        }).toJson());
        if (created.first != 200) return QString();
        return QJsonDocument::fromJson(created.second).object().value("goalId").toString();
    };
    const QString approveId = parkGoal(QStringLiteral("acceptance approval fixture"));
    const QString rejectId = parkGoal(QStringLiteral("acceptance rejection fixture"));
    QVERIFY(!approveId.isEmpty());
    QVERIFY(!rejectId.isEmpty());

    core.refreshGoals();
    // The park is deterministic: the ask-all check fires BEFORE every acting
    // step, so both goals reach waiting_for_approval while driving.
    // v1.10.13: waitForGoalState — the park happens in the ASYNC drive; the
    // client must re-read the store to observe it (60s bound unchanged).
    const bool parked = waitForGoalState(&core, [&]() {
        const QVariantMap a = goalById(core.goals(), approveId);
        const QVariantMap r = goalById(core.goals(), rejectId);
        return !a.isEmpty() && !r.isEmpty()
               && a.value("status").toString() == QStringLiteral("waiting_for_approval")
               && r.value("status").toString() == QStringLiteral("waiting_for_approval");
    }, 60000);
    if (!parked) {
        // v1.10.13: BOTH goals' durable status, phase, last evidence and
        // driving flag, named BEFORE the failure — the diagnostic snapshot
        // the run #24 test lacked (its timeout had zero diagnostics).
        dumpGoalDiagnostics(m_endpoint, {approveId, rejectId});
    }
    QVERIFY(parked);

    auto pendingApprovalOf = [&](const QString &id) -> QVariantMap {
        return goalById(core.goals(), id).value("pendingApproval").toMap();
    };
    const QVariantMap approvePa = pendingApprovalOf(approveId);
    QVERIFY2(!approvePa.isEmpty(), "the parked goal must carry a durable pendingApproval");
    QVERIFY2(!approvePa.value("approvalId").toString().isEmpty(),
             "the pending approval must carry a stable approvalId");
    QVERIFY2(approvePa.value("toolName").toString() == QStringLiteral("goal.step"),
             qPrintable(approvePa.value("toolName").toString()));

    // Approve: the decision is recorded durably as goal evidence and the
    // loop re-drives (under ask-all it may park again at the NEXT boundary
    // — the core's policy owns that; the client only dispatches).
    core.approveGoal(approveId, approvePa.value("approvalId").toString());
    QVERIFY(waitForGoalState(&core, [&]() {
        const QVariantMap g = goalById(core.goals(), approveId);
        if (g.isEmpty()) return false;
        const auto ev = g.value("evidence").toList();
        for (const auto &e : ev) {
            const QVariantMap em = e.toMap();
            if (em.value("action").toString() == QStringLiteral("approval decision")
                && em.value("status").toString() == QStringLiteral("approved"))
                return true;
        }
        return false;
    }, 20000));

    // Reject: the decision is recorded, the pending call is cleared and the
    // goal stays blocked — it is never re-driven.
    const QVariantMap rejectPa = pendingApprovalOf(rejectId);
    QVERIFY2(!rejectPa.isEmpty(), "the second goal must still park for its reject leg");
    core.rejectGoal(rejectId, rejectPa.value("approvalId").toString());
    QVERIFY(waitForGoalState(&core, [&]() {
        const QVariantMap g = goalById(core.goals(), rejectId);
        return !g.isEmpty()
               && g.value("status").toString() == QStringLiteral("blocked")
               && g.value("pendingApproval").toMap().isEmpty();
    }, 20000));
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));
}

// qmlLoadsCleanly — the ONE bounded genuine-QML slot (v1.10.13 T-INTEGRITY,
// layer B). Every slot above drives CoreClient directly; this slot proves
// the production QML surface at its most basic contract: the REAL Main.qml
// (the exact file the sheytan-ui host ships, resolved from the checkout via
// SHEYTAN_REPO or from the build tree) loads in a REAL QQmlApplicationEngine
// under the offscreen/software environment the workflow pins, creates a
// root object, and emits no objectCreationFailed. It does NOT drive the
// composer → live bubble → settle → persistence journey through real QML
// controls — that interactive acceptance is future work and is never
// claimed here. The engine is scoped to this slot; no state leaks into the
// IPC slots.
void Acceptance::qmlLoadsCleanly() {
    const QString repo = qEnvironmentVariable("SHEYTAN_REPO");
    QStringList candidates;
    if (!repo.isEmpty())
        candidates << (repo + QStringLiteral("/ui/qt/qml/Main.qml"));
    candidates << (QCoreApplication::applicationDirPath()
                   + QStringLiteral("/../qml/Main.qml"));
    QString mainQml;
    for (const QString &c : candidates)
        if (QFileInfo::exists(c)) { mainQml = c; break; }
    if (mainQml.isEmpty())
        QSKIP("production Main.qml not reachable (no SHEYTAN_REPO, no build-tree layout) — NOT RUN");

    QQmlApplicationEngine engine;
    // The host registers the CoreClient singleton under the sheytan.ui URI
    // BEFORE the engine loads Main.qml (main.cpp: qmlRegisterSingletonInstance
    // + Main.qml "import sheytan.ui" + Core.* bindings). Mirror that wiring
    // exactly, or creation would fail for an environment artifact instead of
    // a product defect. The instance is unconnected — identical to the
    // host's own pre-connect state at creation time (Main.qml's
    // Component.onCompleted connect is endpoint-guarded and stays off).
    qmlRegisterSingletonInstance("sheytan.ui", 1, 0, "Core", new CoreClient());
    bool creationFailed = false;
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed,
                     [&creationFailed]() { creationFailed = true; });
    engine.load(QUrl::fromLocalFile(mainQml));

    QVERIFY2(!creationFailed,
             qPrintable(QStringLiteral("the production Main.qml failed to create objects: %1")
                            .arg(mainQml)));
    QVERIFY2(!engine.rootObjects().isEmpty(),
             "the production Main.qml must create a non-empty rootObjects() list");
    QVERIFY2(qobject_cast<QQuickWindow *>(engine.rootObjects().constFirst()) != nullptr,
             "the production Main.qml root must be a QQuickWindow");
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

// wsReassemblesFragmentedMessages: the v1.10.5 fragmentation contract
// (REVISED from v1.10.3's refuse-and-close). The REAL core fragments every
// activity event larger than its 1024-byte gorilla write buffer — the raw
// probe captured fragment(1024, FIN=0) + continuation(1, FIN=1) on a real
// generation — so refusing fragmentation killed the live bubble mid-run on
// every long-context chat (the very defect chatStreamPersistRepeatAndClose
// exposed). The client now:
//   1. REASSEMBLES fragmented text messages and delivers them (a live
//      event split across fragments must reach the UI intact);
//   2. still refuses a continuation WITHOUT a first fragment with a typed
//      error + close — protocol violations stay loud, nothing silently
//      dropped (the v1.10.2 regression class).
void Acceptance::wsReassemblesFragmentedMessages() {
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
    // (1) A REAL activity event split mid-JSON across a FIN=0 text fragment
    // and a FIN=1 continuation: the reassembled event must drive liveText.
    const QByteArray part1 = "{\"type\":\"response\",\"caption\":\"re-as";
    const QByteArray part2 = "sembled\"}";
    QVERIFY(part1.size() < 126 && part2.size() < 126);
    QByteArray frag;
    frag += char(0x01);             // FIN=0, text — first fragment
    frag += char(part1.size());     // unmasked length
    frag += part1;
    frag += char(0x80);             // continuation — FINAL fragment (FIN=1, opcode 0x0)
    frag += char(part2.size());
    frag += part2;
    srv->write(frag);
    srv->flush();
    QVERIFY2(waitFor([&]() { return core.liveText() == QStringLiteral("re-assembled"); }, 8000),
             qPrintable(QStringLiteral("liveText=%1 error=%2")
                            .arg(core.liveText(), core.error())));
    QVERIFY2(core.error().isEmpty(), qPrintable(core.error()));

    // (2) A continuation with NO open first fragment remains a protocol
    // violation: typed error, loud close — never a silent drop.
    QByteArray orphan;
    orphan += char(0x80); // continuation (FIN=1) with no open first fragment
    orphan += char(0x01); // unmasked, 1 byte
    orphan += 'z';
    srv->write(orphan);
    srv->flush();
    QVERIFY2(waitFor([&]() { return core.error().contains(QStringLiteral("continuation without a first fragment")); }, 8000),
             qPrintable(core.error().isEmpty() ? QStringLiteral("an orphan continuation was silently accepted")
                                               : QStringLiteral("wrong refusal: %1").arg(core.error())));
}

QTEST_MAIN(Acceptance)
#include "tst_acceptance.moc"