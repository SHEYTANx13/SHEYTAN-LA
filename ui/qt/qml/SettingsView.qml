// SettingsView.qml — v1.11.0-fixed R3 (run-#48, Task ID 43): the Settings
// surface, full parity with the Wails IA (src/settings-sections.ts — the
// React reference stays the authority on WHAT lives where; this client
// renders the SAME eight sections from the SAME /api/config source).
//
// v1.11.0-fixed R3 schema repair — the "fewer options" defect class:
//  · sampling + context fields bind the NESTED llm.* schema via `path`
//    (the typed config merge drops unknown flat keys — the flat binding
//    rendered the field but never persisted it);
//  · the four type-mismatched controls match the Go schema now (the old
//    shapes 400'd every patch: thinkingMode/parallelTools/labKeepWorkspaces
//    are Go bools, visionMmprojOffload is a Go auto|on|off enum);
//  · the honest option sets (updateSchedule carries monthly,
//    researchBackend carries auto, kvCacheQuant carries the served set);
//  · enabledTools renders the per-tool checkbox list fed by Core.toolCatalog
//    (GET /api/tools) — the dead comma-textfield is gone;
//  · the browserSlowMoMsAlias ghost key (no Go field exists — every patch
//    was silently dropped) is removed.
//
// IPC CLIENT of the ONE Go authority: every value renders from
// Core.config (GET /api/config, the redacted server view); every edit
// goes through Core.patchConfig (POST /api/config) and re-reads the
// source — no local defaults, no second authority, no fabricated
// telemetry. Diagnostics renders read-only facts from /api/preflight,
// /api/sysinfo and /api/logs; fields the server redacts are rendered
// exactly as served.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "settingsView"
    color: Theme.background

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
            { key: "numCtx", path: "llm.numCtx", label: qsTr("Context size (numCtx)"), type: "int" },
            { key: "historyWindowPct", label: qsTr("History window %"), type: "int" },
            { key: "continuumEnabled", label: qsTr("Continuum memory"), type: "bool" }
        ] },
        { id: "agent", label: qsTr("Agent & Tools"), hint: qsTr("Tools, permissions, sandbox"), fields: [
            { key: "maxIterations", label: qsTr("Max iterations"), type: "int" },
            // v1.11.0-fixed R3: Go schema is bool — the old int spinner 400'd.
            { key: "parallelTools", label: qsTr("Parallel tool execution"), type: "bool" },
            // v1.11.0-fixed R3: Go schema is bool — the old enum 400'd.
            { key: "thinkingMode", label: qsTr("Thinking mode"), type: "bool" },
            { key: "recallEnabled", label: qsTr("Recall enabled"), type: "bool" },
            { key: "recallTopK", label: qsTr("Recall top-K"), type: "int" },
            // v1.11.0-fixed R3: the real per-tool checkbox list fed by
            // Core.toolCatalog (GET /api/tools), not a comma-textfield.
            { key: "enabledTools", label: qsTr("Enabled tools"), type: "tools" },
            { key: "labEnabled", label: qsTr("Coding Lab enabled"), type: "bool" },
            // v1.11.0-fixed R3: Go schema is bool — the old int spinner 400'd.
            { key: "labKeepWorkspaces", label: qsTr("Lab keeps workspaces"), type: "bool" },
            { key: "labAllowNetwork", label: qsTr("Lab allows network"), type: "bool" },
            { key: "sandboxEnabled", label: qsTr("Sandbox enabled"), type: "bool" }
        ] },
        { id: "network", label: qsTr("Network"), hint: qsTr("Research, remote provider"), fields: [
            { key: "researchEnabled", label: qsTr("Research enabled"), type: "bool" },
            { key: "researchBackend", label: qsTr("Research backend"), type: "enum", options: ["auto", "duckduckgo", "searxng"] },
            { key: "researchSearxngUrl", label: qsTr("SearXNG URL"), type: "string" },
            { key: "researchMaxResults", label: qsTr("Max results"), type: "int" },
            { key: "researchGitHub", label: qsTr("GitHub research"), type: "bool" },
            { key: "researchReddit", label: qsTr("Reddit research"), type: "bool" },
            { key: "researchWeb", label: qsTr("Web research"), type: "bool" }
        ] },
        { id: "updates", label: qsTr("Updates"), hint: qsTr("Check, policy, version"), fields: [
            { key: "updateSchedule", label: qsTr("Update schedule"), type: "enum", options: ["off", "daily", "weekly", "monthly"] }
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
            // v1.11.0-fixed R3: Go schema is auto|on|off — the old bool switch 400'd.
            { key: "visionMmprojOffload", label: qsTr("Vision mmproj offload"), type: "enum", options: ["auto", "on", "off"] },
            { key: "flashAttention", label: qsTr("Flash attention"), type: "bool" },
            { key: "gpuAutoOffload", label: qsTr("GPU auto offload"), type: "bool" },
            { key: "mlock", label: qsTr("mlock"), type: "bool" },
            { key: "cacheReuse", label: qsTr("Cache reuse"), type: "bool" },
            { key: "ubatchSize", label: qsTr("ubatch size"), type: "int" },
            { key: "threadsBatch", label: qsTr("Batch threads"), type: "int" },
            // v1.11.0-fixed R3: the served set — an empty value clears the quant.
            { key: "kvCacheQuant", label: qsTr("KV cache quant"), type: "enum", options: ["", "q8_0"] },
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
            // v1.11.0-fixed R3: the sampling block binds the nested llm
            // schema — the flat keys were silently dropped by the typed
            // config merge (rendered, never persisted).
            { key: "temperature", path: "llm.temperature", label: qsTr("Temperature"), type: "float" },
            { key: "topP", path: "llm.topP", label: qsTr("top-P"), type: "float" },
            { key: "topK", path: "llm.topK", label: qsTr("top-K"), type: "int" },
            { key: "minP", path: "llm.minP", label: qsTr("min-P"), type: "float" },
            { key: "maxTokens", path: "llm.maxTokens", label: qsTr("Max tokens"), type: "int" },
            { key: "repeatPenalty", path: "llm.repeatPenalty", label: qsTr("Repeat penalty"), type: "float" },
            { key: "seed", path: "llm.seed", label: qsTr("Seed"), type: "int" }
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
        if (Core.connected) { Core.refreshConfig(); Core.refreshToolCatalog() }
    }
    // A config edit re-renders from the server's redacted source — the
    // patchConfig callback re-reads /api/config; the binding below never
    // writes back locally.
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) { Core.refreshConfig(); Core.refreshToolCatalog() }
        }
    }

    function sectionById(id) {
        for (var s = 0; s < sections.length; s++)
            if (sections[s].id === id) return sections[s]
        return sections[0]
    }

    // v1.11.0-fixed R3: path-aware read. A field with a `path` ("llm.numCtx")
    // reads the nested schema object; the flat key is dropped by the typed
    // config merge, so the nested home is the ONLY persisted one.
    function fieldValue(field) {
        if (field.path !== undefined) {
            var segs = field.path.split(".")
            var v = Core.config
            for (var i = 0; i < segs.length; i++) {
                if (v === undefined || v === null) return undefined
                v = v[segs[i]]
            }
            return v
        }
        return Core.config[field.key]
    }

    // v1.11.0-fixed R3: path-aware write. "llm.numCtx" becomes the nested
    // patch { llm: { numCtx: v } } — the shape the typed merge accepts.
    function setField(field, value) {
        var patch = {}
        if (field.path !== undefined) {
            var segs = field.path.split(".")
            var node = patch
            for (var i = 0; i < segs.length - 1; i++) {
                node[segs[i]] = {}
                node = node[segs[i]]
            }
            node[segs[segs.length - 1]] = value
        } else {
            patch[field.key] = value
        }
        Core.patchConfig(patch)
    }

    // v1.11.0-fixed R3: the enabledTools CSV — the config value is a
    // comma-separated tool-name list; the checkbox list below reads and
    // rewrites it as one patch.
    function enabledToolSet() {
        var raw = Core.config.enabledTools
        var out = {}
        if (raw !== undefined && typeof raw === "string" && raw !== "") {
            var parts = raw.split(",")
            for (var i = 0; i < parts.length; i++) {
                var t = parts[i].trim()
                if (t !== "") out[t] = true
            }
        }
        return out
    }
    function toggleTool(name, on) {
        var set = enabledToolSet()
        if (on) set[name] = true
        else delete set[name]
        var names = []
        for (var k in set) names.push(k)
        Core.patchConfig({ enabledTools: names.join(",") })
    }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // --- Section rail ------------------------------------------------
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 210
            color: Theme.surfaceElevated

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 4

                Label {
                    text: qsTr("Settings")
                    color: Theme.text
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
                                color: parent.parent.active ? Theme.accent : Theme.text
                                font.bold: parent.parent.active
                                elide: Text.ElideRight
                            }
                            Label {
                                text: modelData.hint
                                color: Theme.muted
                                font.pixelSize: 10
                                elide: Text.ElideRight
                            }
                        }
                        background: Rectangle {
                            color: parent.active ? Theme.control : (parent.hovered ? Theme.elevatedAlt : "transparent")
                            radius: Theme.radiusM
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
                    color: Theme.muted
                    font.pixelSize: 10
                }
            }
        }

        Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: Theme.border }

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
                    color: Theme.text
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
                    radius: Theme.radiusM
                    color: Theme.surfaceElevated

                    ColumnLayout {
                        id: diagCol
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 4

                        Label {
                            text: qsTr("Runtime evidence (read-only, from the core)")
                            color: Theme.text
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.preflight.compatible !== undefined
                                  ? qsTr("preflight: compatible=") + Core.preflight.compatible
                                    + qsTr(" device=") + (Core.preflight.device !== undefined ? Core.preflight.device : "?")
                                    + qsTr(" backend=") + (Core.preflight.backend !== undefined ? Core.preflight.backend : "?")
                                  : qsTr("preflight: not loaded yet")
                            color: Theme.info
                            font.pixelSize: 12
                            wrapMode: Text.Wrap
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.logs.length > 0
                                  ? qsTr("recent log entries: ") + Core.logs.length
                                  : qsTr("recent log entries: 0 (open the Log Viewer)")
                            color: Theme.muted
                            font.pixelSize: 12
                        }
                        Label {
                            Layout.fillWidth: true
                            // v1.11.0-fixed R3: the hardware evidence line —
                            // the same /api/sysinfo snapshot the Wails
                            // diagnostics hardware card renders.
                            text: (Core.sysinfo.cpu !== undefined && Core.sysinfo.cpu.name !== undefined)
                                  ? qsTr("hardware: ") + Core.sysinfo.cpu.name
                                    + (Core.sysinfo.ram !== undefined
                                       ? qsTr(" · RAM ") + (Core.sysinfo.ram.totalBytes / (1024 * 1024 * 1024)).toFixed(1) + " GB"
                                       : "")
                                    + (Core.sysinfo.gpus !== undefined && Core.sysinfo.gpus.length > 0
                                       ? qsTr(" · GPU ") + Core.sysinfo.gpus.map(function (g) { return g.name }).join(", ")
                                       : qsTr(" · no GPU detected"))
                                  : qsTr("hardware: not loaded")
                            color: Theme.muted
                            font.pixelSize: 12
                            wrapMode: Text.Wrap
                        }
                        Button {
                            text: qsTr("Re-read evidence")
                            onClicked: { Core.refreshPreflight(); Core.refreshSysinfo(); Core.refreshLogs() }
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
                        implicitHeight: modelData.type === "tools"
                                        ? toolsCol.implicitHeight + 20
                                        : 52
                        radius: Theme.radiusM
                        color: fieldHover.hovered ? Theme.surfaceAlt : Theme.surfaceElevated
                        // v1.10.15-fixed R3/R4 (D32/I22): rows answer the
                        // pointer with a gated highlight step.
                        HoverHandler { id: fieldHover }
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }

                        // v1.11.0-fixed R3: the tools row — one checkbox per
                        // AVAILABLE tool (Core.toolCatalog / GET /api/tools),
                        // the CSV rewritten as ONE patch on every toggle.
                        ColumnLayout {
                            id: toolsCol
                            visible: fieldRow.modelData.type === "tools"
                            anchors.verticalCenter: parent.verticalCenter
                            width: parent.width
                            leftPadding: 10
                            rightPadding: 10
                            topPadding: 10
                            bottomPadding: 10
                            spacing: 4

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: 10
                                Label {
                                    text: fieldRow.modelData.label
                                    color: Theme.text
                                    Layout.fillWidth: true
                                    elide: Text.ElideRight
                                }
                                Label {
                                    text: Core.toolCatalog.length > 0
                                          ? qsTr("available: ") + Core.toolCatalog.length
                                          : qsTr("catalog not loaded")
                                    color: Theme.muted
                                    font.pixelSize: 11
                                }
                            }

                            Flow {
                                Layout.fillWidth: true
                                spacing: 6
                                Repeater {
                                    model: Core.toolCatalog
                                    delegate: CheckBox {
                                        required property var modelData
                                        text: modelData.name
                                        checked: root.enabledToolSet()[modelData.name] === true
                                        onToggled: root.toggleTool(modelData.name, checked)
                                    }
                                }
                            }
                        }

                        RowLayout {
                            visible: fieldRow.modelData.type !== "tools"
                            anchors.fill: parent
                            anchors.margins: 10
                            spacing: 10

                            Label {
                                text: fieldRow.modelData.label
                                color: Theme.text
                                Layout.fillWidth: true
                                elide: Text.ElideRight
                            }

                            Switch {
                                visible: fieldRow.modelData.type === "bool"
                                checked: root.fieldValue(fieldRow.modelData) === true
                                onClicked: root.setField(fieldRow.modelData, checked)
                            }

                            TextField {
                                visible: fieldRow.modelData.type === "string"
                                         || fieldRow.modelData.type === "int"
                                         || fieldRow.modelData.type === "float"
                                Layout.preferredWidth: 260
                                text: {
                                    var v = root.fieldValue(fieldRow.modelData)
                                    return v === undefined ? "" : String(v)
                                }
                                inputMethodHints: fieldRow.modelData.type === "int" || fieldRow.modelData.type === "float"
                                                  ? Qt.ImhFormattedNumbersOnly : Qt.ImhNone
                                color: Theme.text
                                selectByMouse: true
                                onEditingFinished: {
                                    var v = text
                                    if (fieldRow.modelData.type === "int") v = parseInt(text, 10)
                                    else if (fieldRow.modelData.type === "float") v = parseFloat(text)
                                    if (!isNaN(v) || fieldRow.modelData.type === "string")
                                        root.setField(fieldRow.modelData, v)
                                }
                                background: Rectangle {
                                    color: Theme.elevatedAlt
                                    radius: Theme.radiusM
                                    border.color: Theme.controlHover
                                }
                            }

                            ComboBox {
                                visible: fieldRow.modelData.type === "enum"
                                Layout.preferredWidth: 220
                                model: fieldRow.modelData.options
                                editable: true
                                editText: {
                                    var v = root.fieldValue(fieldRow.modelData)
                                    return v === undefined ? "" : String(v)
                                }
                                onActivated: root.setField(fieldRow.modelData, currentText)
                                onAccepted: root.setField(fieldRow.modelData, editText)
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
                    radius: Theme.radiusM
                    color: animHover.hovered ? Theme.surfaceAlt : Theme.surfaceElevated
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
                                color: Theme.text
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                text: qsTr("Instant UI when off — built for low-end devices")
                                color: Theme.muted
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
