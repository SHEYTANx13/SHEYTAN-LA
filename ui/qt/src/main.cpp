// main.cpp — the Qt Quick/QML desktop host entry point.
//
// The UI is an IPC CLIENT of the Go core (sheytan-ipc/1): endpoint and
// ephemeral key arrive through the environment set by the Go launcher
// (internal/desktop). A developer override (SHEYTAN_IPC_ENDPOINT /
// SHEYTAN_IPC_KEY on the command line) exists for local runs only.
#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlEngine>
#include <QJSEngine>
#include <QQuickStyle>
#include <QCommandLineParser>
#include <QUrl>

#include "coreclient.h"

// v1.10.15-fixed (R2/#43, Task ID 40): the per-engine Core provider — the
// same wiring the acceptance suite mirrors. The former
// qmlRegisterSingletonInstance binds the instance to ONE engine; any
// second engine (the acceptance suite's pattern) hit the hosted warning
// "Singleton registered by registerSingletonInstance must only be accessed
// from one engine" and read Core.config as undefined. The provider hands
// every engine its own instance; the engine owns it.
static QObject *coreProvider(QQmlEngine *qml, QJSEngine *js) {
    Q_UNUSED(qml);
    Q_UNUSED(js);
    return new CoreClient();
}

int main(int argc, char *argv[]) {
    QGuiApplication app(argc, argv);
    QGuiApplication::setApplicationName(QStringLiteral("SHEYTAN Local Agent"));
    // v1.10.15-fixed (R3/I6, run-#41 cycle): this literal had drifted at
    // "1.10.1" for ten releases while package.json moved on — the ONE
    // release identity rule (I6/I12) holds for the Qt host too. The value
    // is pinned to package.json's version by the installer-contract suite
    // (R4/I12), so the next bump cannot re-drift it silently.
    QGuiApplication::setApplicationVersion(QStringLiteral("1.11.0"));
    QGuiApplication::setOrganizationName(QStringLiteral("Parsaetak"));

    // v1.11.0 (R1b, run-#48, Task ID 43): the custom QuickControls2 style
    // replaces the Basic default — the operator's "old-fashioned UI"
    // defect (90-degree-corner buttons, unthemed controls). The style is
    // embedded at qrc:/sheytanstyle (ui/qt/style/, CMake qt_add_resources)
    // and binds the ONE Theme singleton; every stock control renders themed
    // by default. Set BEFORE any engine loads.
    QQuickStyle::setStyle(QStringLiteral(":/sheytanstyle"));

    QCommandLineParser parser;
    parser.setApplicationDescription(QStringLiteral("SHEYTAN Local Agent — Qt desktop host"));
    parser.addHelpOption();
    parser.addOptions({
        {{"e", "endpoint"}, QStringLiteral("Local IPC endpoint (socket path or pipe name)"),
         QStringLiteral("endpoint")},
        {{"k", "key"}, QStringLiteral("Ephemeral handshake key"), QStringLiteral("key")},
    });
    parser.process(app);

    const QString endpoint = parser.value("endpoint").isEmpty()
                                 ? qEnvironmentVariable("SHEYTAN_IPC_ENDPOINT")
                                 : parser.value("endpoint");
    const QString key = parser.value("key").isEmpty()
                            ? qEnvironmentVariable("SHEYTAN_IPC_KEY")
                            : parser.value("key");

    qmlRegisterSingletonType<CoreClient>("sheytan.ui", 1, 0, "Core", coreProvider);

    QQmlApplicationEngine engine;
    const QUrl url(QStringLiteral("qrc:/qml/Main.qml"));
    QObject::connect(
        &engine, &QQmlApplicationEngine::objectCreationFailed, &app,
        []() { QCoreApplication::exit(1); }, Qt::QueuedConnection);
    engine.load(url);

    if (engine.rootObjects().isEmpty()) return 1;

    // The QML main view calls Core.connectToCore(endpoint, key) on
    // Component.completed with the resolved values.
    if (!endpoint.isEmpty()) {
        engine.rootObjects().first()->setProperty("ipcEndpoint", endpoint);
        engine.rootObjects().first()->setProperty("ipcKey", key);
    }

    return app.exec();
}
