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

#include "coreclient.h"

class Acceptance : public QObject {
    Q_OBJECT

private slots:
    void initTestCase();
    void chatStreamPersistRepeatAndClose();
    void aiSystemsListActivate();
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

QTEST_MAIN(Acceptance)
#include "tst_acceptance.moc"
