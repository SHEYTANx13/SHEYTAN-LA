// Main.qml — the SHEYTAN Qt Quick desktop UI.
//
// IPC CLIENT of the ONE Go authority: sessions, runs, streaming and
// persistence all come from the existing core API over the local channel.
// Surfaces implemented here: the v1.10.15-fixed NAVIGATION CHROME (R1,
// D28/I21 — the seven-view Wails IA: chat, agent, workspace, lab,
// automation, system, settings, each a native Qt Quick view backed by the
// SAME core endpoints), the session list (create/open/delete), chat
// transcript with per-message roles, live streaming bubble (cumulative
// text), Stop while streaming, composer lock/unlock, settlement →
// persistence reload, error/empty/loading states, the AI Systems panel
// (v1.10.2: list + activation over /api/systems — the store's validation,
// persistence and reserved default stay server-owned; this view holds no
// second authority), the Goals panel (v1.10.13: list + control actions
// over /api/goals), and the parity overlays (History, Models, My Tools,
// Log Viewer — each a pure IPC client of its existing handler).
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

ApplicationWindow {
    id: root

    property string ipcEndpoint: ""
    property string ipcKey: ""

    width: 1280
    height: 840
    minimumWidth: 1024
    minimumHeight: 680
    visible: true
    title: qsTr("SHEYTAN Local Agent") + (Core.connected && Core.coreVersion !== "" ? "  ·  v" + Core.coreVersion : "")
    color: Theme.background

    // v1.10.15-fixed R1 (D28/I21): the navigation contract — the SAME
    // seven views the Wails surface offers (src/workspace.ts), same ids.
    property var navViews: ["chat", "agent", "workspace", "lab", "automation", "system", "settings"]
    property string currentView: "chat"
    property int currentViewIndex: {
        var i = navViews.indexOf(currentView)
        return i >= 0 ? i : 0
    }

    // v1.10.15-fixed R3 (D37/I21, Task ID 42): keyboard shortcuts parity —
    // every layer reachable from the keyboard (src/shortcuts.ts, v1.2.4),
    // plus the "?" help overlay whose Escape always closes. Sequences map
    // Ctrl+1..7 onto the QT nav order above (the Qt IA's own seven views;
    // the Wails digit map numbered its five-layer order — same
    // functionality, this surface's real order). The help toggle is
    // Ctrl+/ (a plain "?" WindowShortcut would hijack the character in
    // every composer — the shortcuts.ts "without touching the composer's
    // own key handling" rule).
    property bool shortcutHelpVisible: false

    Shortcut {
        objectName: "shortcut-view-chat"
        sequence: "Ctrl+1"
        onActivated: root.currentView = "chat"
    }
    Shortcut {
        objectName: "shortcut-view-agent"
        sequence: "Ctrl+2"
        onActivated: root.currentView = "agent"
    }
    Shortcut {
        objectName: "shortcut-view-workspace"
        sequence: "Ctrl+3"
        onActivated: root.currentView = "workspace"
    }
    Shortcut {
        objectName: "shortcut-view-lab"
        sequence: "Ctrl+4"
        onActivated: root.currentView = "lab"
    }
    Shortcut {
        objectName: "shortcut-view-automation"
        sequence: "Ctrl+5"
        onActivated: root.currentView = "automation"
    }
    Shortcut {
        objectName: "shortcut-view-system"
        sequence: "Ctrl+6"
        onActivated: root.currentView = "system"
    }
    Shortcut {
        objectName: "shortcut-view-settings"
        sequence: "Ctrl+7"
        onActivated: root.currentView = "settings"
    }
    Shortcut {
        objectName: "shortcut-help"
        sequence: "Ctrl+/"
        onActivated: root.shortcutHelpVisible = !root.shortcutHelpVisible
    }
    Shortcut {
        objectName: "shortcut-help-escape"
        sequence: "Escape"
        enabled: root.shortcutHelpVisible
        onActivated: root.shortcutHelpVisible = false
    }

    // The help overlay (shortcuts.ts): a simple card, no animation, Escape
    // closes. Non-modal dimmer so the operator can see the map without
    // losing the window.
    Rectangle {
        objectName: "shortcut-help-overlay"
        anchors.fill: parent
        visible: root.shortcutHelpVisible
        color: Theme.overlay
        z: 1000

        Rectangle {
            width: 460
            height: helpCol.implicitHeight + 48
            anchors.centerIn: parent
            radius: Theme.radiusL
            color: Theme.surfaceElevated
            border.color: Theme.controlHover

            ColumnLayout {
                id: helpCol
                anchors.fill: parent
                anchors.margins: Theme.spaceXL
                spacing: Theme.spaceS

                Label {
                    text: qsTr("Keyboard shortcuts")
                    color: Theme.accent
                    font.bold: true
                    font.pixelSize: 14
                }
                Label {
                    text: qsTr("Ctrl+1…7 — switch view (chat, agent, workspace, lab, automation, system, settings)\nCtrl+/ — toggle this help\nEsc — close this help")
                    color: Theme.text
                    font.pixelSize: Theme.fontS
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }
                Item { Layout.fillHeight: true }
                Button {
                    text: qsTr("Close")
                    onClicked: root.shortcutHelpVisible = false
                }
            }
        }
    }

    // Acceptance-surface facts (tst_acceptance::navChromeSevenViewsLoad).

    // AI Systems panel (v1.10.2): right-edge drawer, pure /api/systems client.
    Drawer {
        id: systemsDrawer
        edge: Qt.RightEdge
        width: 380
        height: root.height
        onOpened: Core.refreshSystems()

        // v1.10.15-fixed R3/R4 (D32/I22): the drawer's slide is EXPLICIT and
        // gated — off means the panel appears instantly in its final
        // position (same gate as the four parity overlay drawers).
        enter: Transition {
            enabled: Core.animationsEnabled
            NumberAnimation { property: "position"; to: 1.0; duration: Theme.motionNormal; easing.type: Easing.OutCubic }
        }
        exit: Transition {
            enabled: Core.animationsEnabled
            NumberAnimation { property: "position"; to: 0.0; duration: Theme.motionFast; easing.type: Easing.InCubic }
        }

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 14
            spacing: 10

            RowLayout {
                Layout.fillWidth: true
                Label {
                    text: qsTr("AI Systems")
                    color: Theme.text
                    font.pixelSize: 16
                    font.bold: true
                    Layout.fillWidth: true
                }
                Button {
                    text: qsTr("Refresh")
                    display: AbstractButton.TextOnly
                    onClicked: Core.refreshSystems()
                }
            }

            Label {
                Layout.fillWidth: true
                text: Core.activeSystemId !== ""
                      ? qsTr("Active: ") + Core.activeSystemId
                      : qsTr("No active system")
                color: Theme.info
                font.pixelSize: Theme.fontS
                elide: Text.ElideRight
            }

            ListView {
                id: systemsList
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: Theme.spaceS
                model: Core.systems
                ScrollBar.vertical: ScrollBar {}

                delegate: Rectangle {
                    required property var modelData
                    width: systemsList.width
                    height: sysCol.implicitHeight + 20
                    radius: Theme.radiusM
                    color: Core.activeSystemId === modelData.systemId ? Theme.successDim : Theme.elevatedAlt

                    ColumnLayout {
                        id: sysCol
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: Theme.spaceXS

                        RowLayout {
                            Layout.fillWidth: true
                            Label {
                                text: modelData.name !== undefined ? modelData.name : modelData.systemId
                                color: Theme.text
                                font.bold: true
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                // v1.10.3 (R9a): bind visibility to the text —
                                // the empty ACTIVE badge kept its layout space
                                // in the v1.10.2 row, shifting the title.
                                text: Core.activeSystemId === modelData.systemId ? qsTr("ACTIVE") : ""
                                visible: text !== ""
                                color: Theme.success
                                font.pixelSize: 10
                                font.bold: true
                            }
                        }
                        Label {
                            Layout.fillWidth: true
                            text: modelData.systemId + "  ·  r" + (modelData.revision !== undefined ? modelData.revision : 0)
                            color: Theme.muted
                            font.pixelSize: Theme.fontXS
                            elide: Text.ElideRight
                        }
                        Button {
                            visible: Core.activeSystemId !== modelData.systemId
                            text: qsTr("Activate")
                            onClicked: Core.activateSystem(modelData.systemId)
                        }
                    }
                }

                EmptyState {
                    anchors.centerIn: parent
                    visible: Core.systems.length === 0
                    icon: "◇"
                    title: Core.connected ? qsTr("No AI Systems") : qsTr("Not connected")
                    detail: Core.connected
                        ? qsTr("The core store lists no systems.")
                        : qsTr("Connect to the core to list AI Systems.")
                }
            }
        }
    }

    // Goals panel (v1.10.13): right-edge drawer, pure /api/goals client.
    Drawer {
        id: goalsDrawer
        edge: Qt.RightEdge
        width: 440
        height: root.height
        onOpened: Core.refreshGoals()

        // v1.10.15-fixed R3/R4 (D32/I22): the drawer's slide is EXPLICIT and
        // gated — off means the panel appears instantly in its final
        // position (same gate as the four parity overlay drawers).
        enter: Transition {
            enabled: Core.animationsEnabled
            NumberAnimation { property: "position"; to: 1.0; duration: Theme.motionNormal; easing.type: Easing.OutCubic }
        }
        exit: Transition {
            enabled: Core.animationsEnabled
            NumberAnimation { property: "position"; to: 0.0; duration: Theme.motionFast; easing.type: Easing.InCubic }
        }

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 14
            spacing: 10

            RowLayout {
                Layout.fillWidth: true
                Label {
                    text: qsTr("Goals")
                    color: Theme.text
                    font.pixelSize: 16
                    font.bold: true
                    Layout.fillWidth: true
                }
                Button {
                    text: qsTr("Refresh")
                    display: AbstractButton.TextOnly
                    onClicked: Core.refreshGoals()
                }
            }

            ListView {
                id: goalsList
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: Theme.spaceS
                model: Core.goals
                ScrollBar.vertical: ScrollBar {}

                delegate: Rectangle {
                    required property var modelData
                    width: goalsList.width
                    height: goalCol.implicitHeight + 20
                    radius: Theme.radiusM
                    color: Theme.elevatedAlt

                    // Derived from the ACTUAL plan state served by the core
                    // (PlanStep.status) — never a decorative percentage.
                    function planSteps()    { return modelData.plan !== undefined ? modelData.plan : [] }
                    function completedSteps() {
                        var n = 0
                        var plan = planSteps()
                        for (var i = 0; i < plan.length; i++)
                            if (plan[i].status === "completed") n++
                        return n
                    }
                    function currentObjective() {
                        var plan = planSteps()
                        if (modelData.currentStep !== undefined && modelData.currentStep >= 0
                                && modelData.currentStep < plan.length)
                            return plan[modelData.currentStep].objective
                        return ""
                    }
                    function terminal() {
                        return modelData.status === "completed"
                               || modelData.status === "cancelled"
                               || modelData.status === "failed"
                    }

                    ColumnLayout {
                        id: goalCol
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: Theme.spaceXS

                        RowLayout {
                            Layout.fillWidth: true
                            Label {
                                text: modelData.originalGoal
                                color: Theme.text
                                font.bold: true
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }
                        Label {
                            Layout.fillWidth: true
                            text: modelData.goalId + "  ·  " + modelData.status + "  ·  " + modelData.phase
                            color: modelData.status === "waiting_for_approval" ? Theme.warn : Theme.muted
                            font.pixelSize: Theme.fontXS
                            elide: Text.ElideRight
                        }
                        Label {
                            Layout.fillWidth: true
                            text: planSteps().length > 0
                                  ? qsTr("plan: ") + completedSteps() + qsTr(" of ") + planSteps().length + qsTr(" steps completed")
                                  : qsTr("no plan yet")
                            color: Theme.info
                            font.pixelSize: Theme.fontXS
                            elide: Text.ElideRight
                        }
                        Label {
                            Layout.fillWidth: true
                            visible: currentObjective() !== ""
                            text: qsTr("current step: ") + currentObjective()
                            color: Theme.muted
                            font.pixelSize: Theme.fontXS
                            elide: Text.ElideRight
                        }
                        Label {
                            Layout.fillWidth: true
                            visible: modelData.status === "waiting_for_approval"
                                     && modelData.pendingApproval !== undefined
                                     && modelData.pendingApproval !== null
                                     && modelData.pendingApproval.reason !== undefined
                                     && modelData.pendingApproval.reason !== ""
                            text: qsTr("awaiting approval: ") + modelData.pendingApproval.reason
                            color: Theme.warn
                            font.pixelSize: Theme.fontXS
                            wrapMode: Text.Wrap
                        }
                        RowLayout {
                            Layout.fillWidth: true
                            spacing: 6
                            Button {
                                visible: modelData.status === "waiting_for_approval"
                                         && modelData.pendingApproval !== undefined
                                         && modelData.pendingApproval !== null
                                text: qsTr("Approve")
                                onClicked: Core.approveGoal(modelData.goalId,
                                                            modelData.pendingApproval.approvalId)
                            }
                            Button {
                                visible: modelData.status === "waiting_for_approval"
                                         && modelData.pendingApproval !== undefined
                                         && modelData.pendingApproval !== null
                                text: qsTr("Reject")
                                onClicked: Core.rejectGoal(modelData.goalId,
                                                           modelData.pendingApproval.approvalId)
                            }
                            Button {
                                visible: modelData.status === "paused"
                                text: qsTr("Resume")
                                onClicked: Core.resumeGoal(modelData.goalId)
                            }
                            Button {
                                visible: !terminal()
                                text: qsTr("Pause")
                                onClicked: Core.pauseGoal(modelData.goalId)
                            }
                            Button {
                                visible: !terminal()
                                text: qsTr("Cancel")
                                onClicked: Core.cancelGoal(modelData.goalId)
                            }
                        }
                    }
                }

                EmptyState {
                    anchors.centerIn: parent
                    visible: Core.goals.length === 0
                    icon: "◇"
                    title: Core.connected ? qsTr("No goals") : qsTr("Not connected")
                    detail: Core.connected
                        ? qsTr("The core goal store is empty.")
                        : qsTr("Connect to the core to list goals.")
                }
            }
        }
    }

    function connect() {
        if (ipcEndpoint !== "") Core.connectToCore(ipcEndpoint, ipcKey)
    }

    Component.onCompleted: root.connect()

    // Accept the launcher's properties set after construction.
    onIpcEndpointChanged: root.connect()
    onIpcKeyChanged: root.connect()

    // Central refresh wiring: the ONE connection event fans out the
    // parity surfaces' first reads — every view also re-reads on demand
    // and on its own onCompleted/visibility.
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) {
                Core.refreshConfig()
                Core.refreshModels()
            }
        }
    }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // --- Nav rail (v1.10.15-fixed R1/D28): the seven-view IA — the
        // same ids, order and mode rules as src/workspace.ts. PerfStrip
        // and the parity overlays dock at the rail's bottom, mirroring
        // the Wails access points.
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 84
            color: Theme.backgroundAlt

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 6
                spacing: Theme.spaceXS

                // v1.10.15-fixed (R1/#43, Task ID 40): the nav buttons are
                // SEVEN EXPLICIT objects with STATIC objectNames. The
                // former Repeater delegate declared `required property int
                // index` on a JS-array model — Qt 6.8.2 QQuickRepeater
                // never initializes required index, so every delegate
                // aborted creation and no "nav-<id>" object ever existed
                // (run #43: "nav button missing for view: chat", both OSes,
                // deterministic). Static buttons kill the whole class and
                // keep the findChild contract (tst_acceptance.cpp:1060),
                // the seven ids, the order and the visuals byte-equal.
                Button {
                    id: navChat
                    objectName: "nav-chat"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "chat"
                    onClicked: root.currentView = "chat"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u25C7")
                            color: navChat.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("Chat")
                            color: navChat.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navChat.checked ? Theme.control : (navChat.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        // v1.10.15-fixed R3/R4 (D32/I22, Task ID 41): rail
                        // state changes read the ONE motion gate.
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }
                Button {
                    id: navAgent
                    objectName: "nav-agent"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "agent"
                    onClicked: root.currentView = "agent"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u25C8")
                            color: navAgent.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("Agent")
                            color: navAgent.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navAgent.checked ? Theme.control : (navAgent.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }
                Button {
                    id: navWorkspace
                    objectName: "nav-workspace"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "workspace"
                    onClicked: root.currentView = "workspace"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u2380")
                            color: navWorkspace.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("Workspace")
                            color: navWorkspace.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navWorkspace.checked ? Theme.control : (navWorkspace.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }
                Button {
                    id: navLab
                    objectName: "nav-lab"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "lab"
                    onClicked: root.currentView = "lab"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u25C6")
                            color: navLab.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("Lab")
                            color: navLab.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navLab.checked ? Theme.control : (navLab.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }
                Button {
                    id: navAutomation
                    objectName: "nav-automation"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "automation"
                    onClicked: root.currentView = "automation"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u23F1")
                            color: navAutomation.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("Autom.")
                            color: navAutomation.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navAutomation.checked ? Theme.control : (navAutomation.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }
                Button {
                    id: navSystem
                    objectName: "nav-system"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "system"
                    onClicked: root.currentView = "system"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u25A4")
                            color: navSystem.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("System")
                            color: navSystem.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navSystem.checked ? Theme.control : (navSystem.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }
                Button {
                    id: navSettings
                    objectName: "nav-settings"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 60
                    checkable: true
                    checked: root.currentView === "settings"
                    onClicked: root.currentView = "settings"
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: qsTr("\u2699")
                            color: navSettings.checked ? Theme.accent : Theme.muted
                            font.pixelSize: 14
                            Layout.alignment: Qt.AlignHCenter
                        }
                        Label {
                            text: qsTr("Settings")
                            color: navSettings.checked ? Theme.text : Theme.muted
                            font.pixelSize: 9
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                    background: Rectangle {
                        color: navSettings.checked ? Theme.control : (navSettings.hovered ? Theme.elevated : "transparent")
                        radius: Theme.radiusM
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }
                    }
                }

                Item { Layout.fillHeight: true }

                Button {
                    objectName: "nav-history"
                    Layout.fillWidth: true
                    text: qsTr("Hist.")
                    enabled: Core.connected
                    onClicked: historyDrawer.open()
                }
                Button {
                    objectName: "nav-models"
                    Layout.fillWidth: true
                    text: qsTr("Models")
                    enabled: Core.connected
                    onClicked: modelPickerDrawer.open()
                }
                Button {
                    objectName: "nav-mytools"
                    Layout.fillWidth: true
                    text: qsTr("Tools")
                    enabled: Core.connected
                    onClicked: myToolsDrawer.open()
                }
                Button {
                    objectName: "nav-logs"
                    Layout.fillWidth: true
                    text: qsTr("Logs")
                    enabled: Core.connected
                    onClicked: logViewerDrawer.open()
                }
                PerfStrip {
                    Layout.fillWidth: true
                }
            }
        }

        // --- Sidebar: sessions (chat + agent share the ONE session
        // runtime; the other views own the full width) -------------------
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 280
            visible: root.currentView === "chat" || root.currentView === "agent"
            color: Theme.surfaceElevated

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.spaceM
                spacing: 10

                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: qsTr("Sessions")
                        color: Theme.text
                        font.pixelSize: 16
                        font.bold: true
                        Layout.fillWidth: true
                    }
                    Button {
                        text: qsTr("+ New")
                        display: AbstractButton.TextOnly
                        onClicked: Core.newSession("")
                    }
                }

                ListView {
                    id: sessionList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    model: Core.sessions

                    ScrollBar.vertical: ScrollBar {}

                    delegate: ItemDelegate {
                        required property var modelData
                        width: sessionList.width
                        height: 56

                        contentItem: ColumnLayout {
                            spacing: 2
                            Label {
                                text: modelData.title !== undefined && modelData.title !== "" ? modelData.title : qsTr("Untitled")
                                color: Theme.text
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                // v1.10.3 field contract: the wire field is
                                // msgCount (internal/sessions Session json
                                // tag) — the v1.10.2 messageCount read was
                                // always undefined, rendering "0 messages".
                                text: (modelData.msgCount !== undefined ? modelData.msgCount : 0) + qsTr(" messages")
                                color: Theme.muted
                                font.pixelSize: Theme.fontXS
                            }
                        }

                        background: Rectangle {
                            color: Core.currentSession.id === modelData.id
                                   ? Theme.control : (parent.hovered ? Theme.elevatedAlt : "transparent")
                            radius: Theme.radiusM
                            Behavior on color {
                                enabled: Core.animationsEnabled
                                ColorAnimation { duration: Theme.motionFast }
                            }
                        }

                        onClicked: Core.openSession(modelData.id)
                    }

                    EmptyState {
                        anchors.centerIn: parent
                        visible: Core.sessions.length === 0
                        icon: "◇"
                        title: qsTr("No sessions yet")
                        detail: qsTr("Create a session to start chatting.")
                    }
                }

                Button {
                    Layout.fillWidth: true
                    text: qsTr("AI Systems")
                    enabled: Core.connected
                    onClicked: systemsDrawer.open()
                }

                Button {
                    Layout.fillWidth: true
                    text: qsTr("Goals")
                    enabled: Core.connected
                    onClicked: goalsDrawer.open()
                }

                Button {
                    Layout.fillWidth: true
                    text: qsTr("Delete current session")
                    enabled: Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                    onClicked: Core.deleteSession(Core.currentSession.id)
                }
            }
        }

        Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; visible: root.currentView === "chat" || root.currentView === "agent"; color: Theme.border }

        // --- View stack: seven views over the ONE core (D28) -------------
        StackLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            currentIndex: root.currentViewIndex

        // --- Main chat column -------------------------------------------
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Loading / error banner
            Rectangle {
                Layout.fillWidth: true
                // v1.10.15-fixed R3/R4 (D32/I22): the banner fades in and
                // out under the ONE motion gate — and stays mounted while
                // fading so the exit is visible; off = instant show/hide.
                visible: Core.error !== "" || opacity > 0
                opacity: Core.error !== "" ? 1 : 0
                color: Theme.errorDim
                Layout.preferredHeight: 34
                Behavior on opacity {
                    enabled: Core.animationsEnabled
                    NumberAnimation { duration: 160 }
                }
                Label {
                    anchors.centerIn: parent
                    text: Core.error
                    color: Theme.errorBg
                }
            }

            // Transcript
            ListView {
                id: transcript
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 10
                model: Core.messages
                ScrollBar.vertical: ScrollBar {}

                onCountChanged: Qt.callLater(positionAtEnd)
                function positionAtEnd() { positionViewAtEnd() }

                delegate: Rectangle {
                    required property var modelData
                    required property int index
                    Layout.fillWidth: true
                    width: transcript.width - 48
                    x: 24
                    height: bubbleText.implicitHeight + 28
                    radius: Theme.radiusM
                    color: modelData.role === "user" ? Theme.infoDimAlt : Theme.elevatedAlt

                    ColumnLayout {
                        anchors.fill: parent
                        anchors.margins: Theme.spaceM
                        spacing: Theme.spaceXS

                        Label {
                            text: modelData.role === "user" ? qsTr("You") : qsTr("SHEYTAN")
                            color: modelData.role === "user" ? Theme.info : Theme.accent
                            font.pixelSize: Theme.fontXS
                            font.bold: true
                        }
                        TextEdit {
                            id: bubbleText
                            text: modelData.content
                            readOnly: true
                            wrapMode: TextEdit.Wrap
                            color: Theme.text
                            Layout.fillWidth: true
                            textFormat: TextEdit.PlainText
                            selectByMouse: true
                        }
                    }
                }

                EmptyState {
                    anchors.centerIn: parent
                    visible: Core.messages.length === 0 && !Core.liveActive
                    icon: "◈"
                    title: Core.connected ? qsTr("Say hello") : qsTr("Connecting to the core…")
                    detail: Core.connected
                        ? qsTr("Send a message — answers stream live and persist to the session.")
                        : qsTr("The desktop UI talks to the Go core over the local IPC channel.")
                }
            }

            // Live streaming bubble (cumulative text; Stop stays visible)
            Rectangle {
                Layout.fillWidth: true
                Layout.margins: Theme.spaceM
                // v1.10.15-fixed R3/R4 (D32/I22): the bubble appears and
                // settles under the ONE motion gate (off = instant).
                visible: Core.liveActive || opacity > 0
                opacity: Core.liveActive ? 1 : 0
                implicitHeight: liveColumn.implicitHeight + 24
                radius: Theme.radiusM
                color: Theme.accentDim
                Behavior on opacity {
                    enabled: Core.animationsEnabled
                    NumberAnimation { duration: 160 }
                }

                ColumnLayout {
                    id: liveColumn
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    RowLayout {
                        Layout.fillWidth: true
                        Label {
                            text: qsTr("SHEYTAN (live)")
                            color: Theme.accent
                            font.pixelSize: Theme.fontXS
                            font.bold: true
                            Layout.fillWidth: true
                        }
                        Button {
                            text: qsTr("Stop")
                            enabled: Core.canStop
                            onClicked: Core.stop()
                        }
                    }
                    // v1.10.15-fixed (R3/D25): the live text is bounded and
                    // scrollable — the unbounded Label let a long generation
                    // push the composer (and the Stop row with it) off
                    // screen. The cap mirrors the composer's own ScrollView
                    // pattern below; the settled message still lands in the
                    // transcript in full.
                    ScrollView {
                        Layout.fillWidth: true
                        Layout.maximumHeight: 160
                        TextArea {
                            readOnly: true
                            text: Core.liveText === "" ? qsTr("…") : Core.liveText
                            wrapMode: TextArea.Wrap
                            color: Theme.text
                            background: null
                        }
                    }
                }
            }

            // Composer
            Rectangle {
                Layout.fillWidth: true
                Layout.preferredHeight: composer.implicitHeight + 24
                color: Theme.surfaceElevated

                RowLayout {
                    id: composer
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceS

                    ScrollView {
                        Layout.fillWidth: true
                        Layout.maximumHeight: 120
                        TextArea {
                            id: input
                            placeholderText: Core.currentSession.id ? qsTr("Message…") : qsTr("Create or open a session first")
                            wrapMode: TextArea.Wrap
                            color: Theme.text
                            enabled: !Core.busy && Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                            background: Rectangle {
                                color: Theme.elevatedAlt
                                radius: Theme.radiusM
                                border.color: Theme.controlHover
                            }
                            Keys.onReturnPressed: (event) => {
                                // v1.10.15-fixed (R5/#43): submit() — a Button-
                                // declared action() shadowed the FINAL
                                // AbstractButton.action member (hosted QWARN
                                // "Final member action is overridden ... The
                                // override won't be used").
                                if (!(event.modifiers & Qt.ShiftModifier)) { sendButton.submit(); event.accepted = true }
                            }
                        }
                    }

                    Button {
                        id: sendButton
                        text: qsTr("Send")
                        enabled: !Core.busy && input.text.trim() !== "" && Core.currentSession.id !== ""
                        // v1.10.15-fixed R3 (D32): the enabled state is
                        // mirrored by a gated opacity step.
                        opacity: enabled ? 1 : 0.55
                        Behavior on opacity {
                            enabled: Core.animationsEnabled
                            NumberAnimation { duration: Theme.motionFast }
                        }
                        function submit() {
                            Core.send(input.text, false)
                            input.clear()
                        }
                        onClicked: submit()
                    }
                }
            }
        }

        // v1.10.15-fixed R1 (D28/I21): the six parity views over the SAME
        // core — each a pure IPC client; the shell forwards their overlay
        // intents to the drawers it owns.
        AgentView {
            onOpenHistoryRequested: historyDrawer.open()
            onOpenModelsRequested: modelPickerDrawer.open()
            onOpenGoalsRequested: goalsDrawer.open()
        }
        WorkspaceView {
            onOpenChatRequested: root.currentView = "chat"
        }
        LabView {}
        AutomationView {}
        SystemView {}
        SettingsView {}
        } // StackLayout
    }

    // Parity overlays (D28/I21) — each a pure IPC client of its existing
    // handler; declared here so the whole surface loads with the shell.
    HistoryDrawer {
        id: historyDrawer
    }
    ModelPickerDrawer {
        id: modelPickerDrawer
    }
    MyToolsDrawer {
        id: myToolsDrawer
    }
    LogViewerDrawer {
        id: logViewerDrawer
    }
}
