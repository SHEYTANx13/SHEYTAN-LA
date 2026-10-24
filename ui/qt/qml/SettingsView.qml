// SettingsView.qml — v1.10.15-fixed R1 (D28/I21): the Settings surface,
// full parity with the Wails IA (src/settings-sections.ts — the React
// reference stays the authority on WHAT lives where; this client renders
// the SAME eight sections from the SAME /api/config source).
//
// IPC CLIENT of the ONE Go authority: every value renders from
// Core.config (GET /api/config, the redacted server view); every edit
// goes through Core.patchConfig (POST /api/config) and re-reads the
// source — no local defaults, no second authority, no fabricated
// telemetry. Diagnostics renders read-only facts from /api/preflight and
// /api/logs; fields the server redacts are rendered exactly as served.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "settingsView"
    color: "#121214"

    // The declarative section/field registry — mirrors FIELD_PLACEMENT in
    // src/settings-sections.ts one-for-one (72 editable fields across the
    // eight sections; Diagnostics is read-only evidence).
    property var sections: [
        { id: "general", label: qsTr("General"), hint: qsTr("Startup, workspace, interaction"), fields: [
            { key: "llamaAutoStart", label: qsTr("Auto-start engine"), type: "bool" },
            { key: "smoothStream", label: qsTr("Smooth streaming"), type: "bool" },
            { key: "showPerfHud", label: qsTr("Show performance HUD"), type: "bool" },
            { key: "targetFps", label: qsTr("Target FPS"), type: "int" }
        ] },
        { id: "models", label: qsTr("Models"), hint: qsTr("Local models, provider, vision"), fields: [
            { key: "provider", label: qsTr("Provider"), type: "enum", options: ["local", "remote"] },
            { key: "model", label: qsTr("Model"), type: "string" },
            { key: "remoteBaseUrl", label: qsTr("Remote base URL"), type: "string" },
            { key: "remoteModel", label: qsTr("Remote model"), type: "string" },
            { key: "visionEnabled", label: qsTr("Vision enabled"), type: "bool" }
        ] },
        { id: "performance", label: qsTr("Performance"), hint: qsTr("Quiet, Balanced, Maximum"), fields: [
            { key: "performanceMode", label: qsTr("Performance mode"), type: "enum", options: ["auto", "quiet", "balanced", "maximum"] },
            { key: "numCtx", label: qsTr("Context size (numCtx)"), type: "int" },
            { key: "historyWindowPct", label: qsTr("History window %"), type: "int" },
            { key: "continuumEnabled", label: qsTr("Continuum memory"), type: "bool" }
        ] },
        { id: "agent", label: qsTr("Agent & Tools"), hint: qsTr("Tools, permissions, sandbox"), fields: [
            { key: "maxIterations", label: qsTr("Max iterations"), type: "int" },
            { key: "parallelTools", label: qsTr("Parallel tools"), type: "int" },
            { key: "thinkingMode", label: qsTr("Thinking mode"), type: "enum", options: ["off", "budgeted", "always"] },
            { key: "recallEnabled", label: qsTr("Recall enabled"), type: "bool" },
            { key: "recallTopK", label: qsTr("Recall top-K"), type: "int" },
            { key: "enabledTools", label: qsTr("Enabled tools (comma-separated)"), type: "string" },
            { key: "labEnabled", label: qsTr("Coding Lab enabled"), type: "bool" },
            { key: "labKeepWorkspaces", label: qsTr("Lab keeps workspaces"), type: "int" },
            { key: "labAllowNetwork", label: qsTr("Lab allows network"), type: "bool" },
            { key: "sandboxEnabled", label: qsTr("Sandbox enabled"), type: "bool" }
        ] },
        { id: "network", label: qsTr("Network"), hint: qsTr("Research, remote provider"), fields: [
            { key: "researchEnabled", label: qsTr("Research enabled"), type: "bool" },
            { key: "researchBackend", label: qsTr("Research backend"), type: "enum", options: ["searxng", "duckduckgo", "github", "reddit"] },
            { key: "researchSearxngUrl", label: qsTr("SearXNG URL"), type: "string" },
            { key: "researchMaxResults", label: qsTr("Max results"), type: "int" },
            { key: "researchGitHub", label: qsTr("GitHub research"), type: "bool" },
            { key: "researchReddit", label: qsTr("Reddit research"), type: "bool" },
            { key: "researchWeb", label: qsTr("Web research"), type: "bool" }
        ] },
        { id: "updates", label: qsTr("Updates"), hint: qsTr("Check, policy, version"), fields: [
            { key: "updateSchedule", label: qsTr("Update schedule"), type: "enum", options: ["off", "daily", "weekly"] }
        ] },
        { id: "diagnostics", label: qsTr("Diagnostics"), hint: qsTr("Health, hardware, logs"), fields: [] },
        { id: "advanced", label: qsTr("Advanced"), hint: qsTr("Expert controls"), fields: [
            { key: "llamaBinPath", label: qsTr("llama binary path"), type: "string" },
            { key: "llamaHost", label: qsTr("llama host"), type: "string" },
            { key: "llamaPort", label: qsTr("llama port"), type: "int" },
            { key: "llamaExtraArgs", label: qsTr("llama extra args"), type: "string" },
            { key: "llmBaseUrl", label: qsTr("LLM base URL"), type: "string" },
            { key: "modelsDir", label: qsTr("Models directory"), type: "string" },
            { key: "visionMmproj", label: qsTr("Vision mmproj path"), type: "string" },
            { key: "visionMmprojOffload", label: qsTr("Vision mmproj offload"), type: "bool" },
            { key: "flashAttention", label: qsTr("Flash attention"), type: "bool" },
            { key: "gpuAutoOffload", label: qsTr("GPU auto offload"), type: "bool" },
            { key: "mlock", label: qsTr("mlock"), type: "bool" },
            { key: "cacheReuse", label: qsTr("Cache reuse"), type: "bool" },
            { key: "ubatchSize", label: qsTr("ubatch size"), type: "int" },
            { key: "threadsBatch", label: qsTr("Batch threads"), type: "int" },
            { key: "kvCacheQuant", label: qsTr("KV cache quant"), type: "enum", options: ["f16", "q8_0", "q4_0"] },
            { key: "draftModel", label: qsTr("Draft model"), type: "string" },
            { key: "sandboxMemory", label: qsTr("Sandbox memory (MB)"), type: "int" },
            { key: "sandboxCPU", label: qsTr("Sandbox CPU"), type: "int" },
            { key: "maxWorkspaceMb", label: qsTr("Max workspace (MB)"), type: "int" },
            { key: "maxSessionsKept", label: qsTr("Max sessions kept"), type: "int" },
            { key: "maxLogMb", label: qsTr("Max log (MB)"), type: "int" },
            { key: "labWorkspaceRoot", label: qsTr("Lab workspace root"), type: "string" },
            { key: "labCommandTimeoutSec", label: qsTr("Lab command timeout (s)"), type: "int" },
            { key: "labMaxIterations", label: qsTr("Lab max iterations"), type: "int" },
            { key: "runTimeoutMinutes", label: qsTr("Run timeout (min)"), type: "int" },
            { key: "attachmentsBudgetKb", label: qsTr("Attachments budget (KB)"), type: "int" },
            { key: "multiAgentDepth", label: qsTr("Multi-agent depth"), type: "int" },
            { key: "researchTimeoutSec", label: qsTr("Research timeout (s)"), type: "int" },
            { key: "researchCacheTtlMin", label: qsTr("Research cache TTL (min)"), type: "int" },
            { key: "researchUserAgent", label: qsTr("Research user agent"), type: "string" },
            { key: "browserExecutablePath", label: qsTr("Browser executable path"), type: "string" },
            { key: "browserSlowMoMs", label: qsTr("Browser slow-mo (ms)"), type: "int" },
            { key: "browserHeadless", label: qsTr("Browser headless"), type: "bool" },
            { key: "browserSlowMoMsAlias", label: qsTr("Browser slow-mo (alias)"), type: "int" },
            { key: "temperature", label: qsTr("Temperature"), type: "string" },
            { key: "topP", label: qsTr("top-P"), type: "string" },
            { key: "topK", label: qsTr("top-K"), type: "int" },
            { key: "minP", label: qsTr("min-P"), type: "string" },
            { key: "maxTokens", label: qsTr("Max tokens"), type: "int" },
            { key: "repeatPenalty", label: qsTr("Repeat penalty"), type: "string" },
            { key: "seed", label: qsTr("Seed"), type: "int" }
        ] }
    ]

    // Acceptance-surface facts (tst_acceptance::settingsEightSectionsAllFields).
    property int sectionCount: sections.length
    property var allFieldKeys: {
        var keys = []
        for (var s = 0; s < sections.length; s++) {
            var f = sections[s].fields
            for (var i = 0; i < f.length; i++) keys.push(f[i].key)
        }
        return keys
    }

    property string currentSection: "general"

    Component.onCompleted: {
        if (Core.connected) Core.refreshConfig()
    }
    // A config edit re-renders from the server's redacted source — the
    // patchConfig callback re-reads /api/config; the binding below never
    // writes back locally.
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) Core.refreshConfig()
        }
    }

    function sectionById(id) {
        for (var s = 0; s < sections.length; s++)
            if (sections[s].id === id) return sections[s]
        return sections[0]
    }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // --- Section rail ------------------------------------------------
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 210
            color: "#1a1a1d"

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 4

                Label {
                    text: qsTr("Settings")
                    color: "#e8e8ea"
                    font.pixelSize: 16
                    font.bold: true
                }

                Repeater {
                    model: root.sections
                    delegate: ItemDelegate {
                        required property var modelData
                        property bool active: root.currentSection === modelData.id
                        width: 182
                        height: 52
                        onClicked: root.currentSection = modelData.id
                        contentItem: ColumnLayout {
                            spacing: 1
                            Label {
                                text: modelData.label
                                color: parent.parent.active ? "#b8a6e8" : "#e8e8ea"
                                font.bold: parent.parent.active
                                elide: Text.ElideRight
                            }
                            Label {
                                text: modelData.hint
                                color: "#8a8a92"
                                font.pixelSize: 10
                                elide: Text.ElideRight
                            }
                        }
                        background: Rectangle {
                            color: parent.active ? "#2c2c31" : (parent.hovered ? "#222226" : "transparent")
                            radius: 6
                            // v1.10.15-fixed R3/R4 (D32/I22): the rail's
                            // selection/hover steps read the ONE motion gate.
                            Behavior on color {
                                enabled: Core.animationsEnabled
                                ColorAnimation { duration: 120 }
                            }
                        }
                    }
                }

                Item { Layout.fillHeight: true }

                Label {
                    Layout.fillWidth: true
                    text: Core.connected
                          ? qsTr("core v") + Core.coreVersion
                          : qsTr("not connected")
                    color: "#8a8a92"
                    font.pixelSize: 10
                }
            }
        }

        Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: "#2a2a2e" }

        // --- Section body ------------------------------------------------
        ScrollView {
            id: sectionScroll
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true

            ColumnLayout {
                width: sectionScroll.availableWidth - 32
                x: 16
                spacing: 6

                Label {
                    text: root.sectionById(root.currentSection).label
                    color: "#e8e8ea"
                    font.pixelSize: 18
                    font.bold: true
                    Layout.topMargin: 16
                }

                // Diagnostics: read-only evidence from /api/preflight +
                // /api/logs — never editable, never fabricated.
                Rectangle {
                    visible: root.currentSection === "diagnostics"
                    Layout.fillWidth: true
                    implicitHeight: diagCol.implicitHeight + 24
                    radius: 8
                    color: "#1a1a1d"

                    ColumnLayout {
                        id: diagCol
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 4

                        Label {
                            text: qsTr("Runtime evidence (read-only, from the core)")
                            color: "#e8e8ea"
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.preflight.compatible !== undefined
                                  ? qsTr("preflight: compatible=") + Core.preflight.compatible
                                    + qsTr(" device=") + (Core.preflight.device !== undefined ? Core.preflight.device : "?")
                                    + qsTr(" backend=") + (Core.preflight.backend !== undefined ? Core.preflight.backend : "?")
                                  : qsTr("preflight: not loaded yet")
                            color: "#8fb8dd"
                            font.pixelSize: 12
                            wrapMode: Text.Wrap
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.logs.length > 0
                                  ? qsTr("recent log entries: ") + Core.logs.length
                                  : qsTr("recent log entries: 0 (open the Log Viewer)")
                            color: "#8a8a92"
                            font.pixelSize: 12
                        }
                        Button {
                            text: qsTr("Re-read evidence")
                            onClicked: { Core.refreshPreflight(); Core.refreshLogs() }
                        }
                    }
                }

                // Editable fields of the active section (Diagnostics ships
                // none — its evidence panel above is the section body).
                Repeater {
                    model: root.sectionById(root.currentSection).fields

                    delegate: Rectangle {
                        id: fieldRow
                        objectName: "field-" + modelData.key
                        required property var modelData
                        Layout.fillWidth: true
                        implicitHeight: 52
                        radius: 6
                        color: fieldHover.hovered ? "#1e1e22" : "#1a1a1d"
                        // v1.10.15-fixed R3/R4 (D32/I22): rows answer the
                        // pointer with a gated highlight step.
                        HoverHandler { id: fieldHover }
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: 120 }
                        }

                        RowLayout {
                            anchors.fill: parent
                            anchors.margins: 10
                            spacing: 10

                            Label {
                                text: fieldRow.modelData.label
                                color: "#e8e8ea"
                                Layout.fillWidth: true
                                elide: Text.ElideRight
                            }

                            Switch {
                                visible: fieldRow.modelData.type === "bool"
                                checked: Core.config[fieldRow.modelData.key] === true
                                onClicked: root.setField(fieldRow.modelData.key, checked)
                            }

                            TextField {
                                visible: fieldRow.modelData.type === "string" || fieldRow.modelData.type === "int"
                                Layout.preferredWidth: 260
                                text: {
                                    var v = Core.config[fieldRow.modelData.key]
                                    return v === undefined ? "" : String(v)
                                }
                                inputMethodHints: fieldRow.modelData.type === "int"
                                                  ? Qt.ImhFormattedNumbersOnly : Qt.ImhNone
                                color: "#e8e8ea"
                                selectByMouse: true
                                onEditingFinished: {
                                    var v = fieldRow.modelData.type === "int"
                                            ? parseInt(text, 10) : text
                                    if (!isNaN(v) || fieldRow.modelData.type !== "int")
                                        root.setField(fieldRow.modelData.key, v)
                                }
                                background: Rectangle {
                                    color: "#222226"
                                    radius: 6
                                    border.color: "#33333a"
                                }
                            }

                            ComboBox {
                                visible: fieldRow.modelData.type === "enum"
                                Layout.preferredWidth: 220
                                model: fieldRow.modelData.options
                                editable: true
                                editText: {
                                    var v = Core.config[fieldRow.modelData.key]
                                    return v === undefined ? "" : String(v)
                                }
                                onActivated: root.setField(fieldRow.modelData.key, currentText)
                                onAccepted: root.setField(fieldRow.modelData.key, editText)
                            }
                        }
                    }
                }

                // v1.10.15-fixed R4 (D32/I22, Task ID 41): the ONE motion
                // control. Not a /api/config field — the core's config is a
                // typed struct that drops unknown keys, so the override is
                // persisted locally by CoreClient (QSettings) while the
                // device-tier default comes from the core (/api/sysinfo).
                // Off = instant state, identical function.
                Rectangle {
                    objectName: "field-animationsEnabled"
                    visible: root.currentSection === "general"
                    Layout.fillWidth: true
                    implicitHeight: 52
                    radius: 6
                    color: animHover.hovered ? "#1e1e22" : "#1a1a1d"
                    HoverHandler { id: animHover }
                    Behavior on color {
                        enabled: Core.animationsEnabled
                        ColorAnimation { duration: 120 }
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: 10

                        ColumnLayout {
                            spacing: 1
                            Layout.fillWidth: true
                            Label {
                                text: qsTr("Animations")
                                color: "#e8e8ea"
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                text: qsTr("Instant UI when off — built for low-end devices")
                                color: "#8a8a92"
                                font.pixelSize: 10
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }

                        Switch {
                            checked: Core.animationsEnabled
                            onToggled: Core.animationsEnabled = checked
                        }
                    }
                }

                Item { Layout.preferredHeight: 16 }
            }
        }
    }

    function setField(key, value) {
        var patch = {}
        patch[key] = value
        Core.patchConfig(patch)
    }
}
