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
    color: "#121214"

    // v1.10.15-fixed R1 (D28/I21): the navigation contract — the SAME
    // seven views the Wails surface offers (src/workspace.ts), same ids.
    property var navViews: ["chat", "agent", "workspace", "lab", "automation", "system", "settings"]
    property string currentView: "chat"
    property int currentViewIndex: {
        var i = navViews.indexOf(currentView)
        return i >= 0 ? i : 0
    }

    // Acceptance-surface facts (tst_acceptance::navChromeSevenViewsLoad).

    // AI Systems panel (v1.10.2): right-edge drawer, pure /api/systems client.
    Drawer {
        id: systemsDrawer
        edge: Qt.RightEdge
        width: 380
        height: root.height
        onOpened: Core.refreshSystems()

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 14
            spacing: 10

            RowLayout {
                Layout.fillWidth: true
                Label {
                    text: qsTr("AI Systems")
                    color: "#e8e8ea"
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
                color: "#8fb8dd"
                font.pixelSize: 12
                elide: Text.ElideRight
            }

            ListView {
                id: systemsList
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 8
                model: Core.systems
                ScrollBar.vertical: ScrollBar {}

                delegate: Rectangle {
                    required property var modelData
                    width: systemsList.width
                    height: sysCol.implicitHeight + 20
                    radius: 8
                    color: Core.activeSystemId === modelData.systemId ? "#243a2c" : "#222226"

                    ColumnLayout {
                        id: sysCol
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: 4

                        RowLayout {
                            Layout.fillWidth: true
                            Label {
                                text: modelData.name !== undefined ? modelData.name : modelData.systemId
                                color: "#e8e8ea"
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
                                color: "#7dd3a0"
                                font.pixelSize: 10
                                font.bold: true
                            }
                        }
                        Label {
                            Layout.fillWidth: true
                            text: modelData.systemId + "  ·  r" + (modelData.revision !== undefined ? modelData.revision : 0)
                            color: "#8a8a92"
                            font.pixelSize: 11
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

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 14
            spacing: 10

            RowLayout {
                Layout.fillWidth: true
                Label {
                    text: qsTr("Goals")
                    color: "#e8e8ea"
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
                spacing: 8
                model: Core.goals
                ScrollBar.vertical: ScrollBar {}

                delegate: Rectangle {
                    required property var modelData
                    width: goalsList.width
                    height: goalCol.implicitHeight + 20
                    radius: 8
                    color: "#222226"

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
                        spacing: 4

                        RowLayout {
                            Layout.fillWidth: true
                            Label {
                                text: modelData.originalGoal
                                color: "#e8e8ea"
                                font.bold: true
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }
                        Label {
                            Layout.fillWidth: true
                            text: modelData.goalId + "  ·  " + modelData.status + "  ·  " + modelData.phase
                            color: modelData.status === "waiting_for_approval" ? "#e8c268" : "#8a8a92"
                            font.pixelSize: 11
                            elide: Text.ElideRight
                        }
                        Label {
                            Layout.fillWidth: true
                            text: planSteps().length > 0
                                  ? qsTr("plan: ") + completedSteps() + qsTr(" of ") + planSteps().length + qsTr(" steps completed")
                                  : qsTr("no plan yet")
                            color: "#8fb8dd"
                            font.pixelSize: 11
                            elide: Text.ElideRight
                        }
                        Label {
                            Layout.fillWidth: true
                            visible: currentObjective() !== ""
                            text: qsTr("current step: ") + currentObjective()
                            color: "#8a8a92"
                            font.pixelSize: 11
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
                            color: "#e8c268"
                            font.pixelSize: 11
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
            color: "#141416"

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 6
                spacing: 4

                Repeater {
                    model: [
                        { id: "chat", label: qsTr("Chat"), icon: "\u25C7" },
                        { id: "agent", label: qsTr("Agent"), icon: "\u25C8" },
                        { id: "workspace", label: qsTr("Workspace"), icon: "\u2380" },
                        { id: "lab", label: qsTr("Lab"), icon: "\u25C6" },
                        { id: "automation", label: qsTr("Autom."), icon: "\u23F1" },
                        { id: "system", label: qsTr("System"), icon: "\u25A4" },
                        { id: "settings", label: qsTr("Settings"), icon: "\u2699" }
                    ]

                    delegate: Button {
                        id: navButton
                        required property var modelData
                        required property int index
                        objectName: "nav-" + modelData.id
                        Layout.fillWidth: true
                        Layout.preferredHeight: 60
                        checkable: true
                        checked: root.currentView === modelData.id
                        onClicked: root.currentView = modelData.id
                        contentItem: ColumnLayout {
                            spacing: 1
                            Label {
                                text: navButton.modelData.icon
                                color: navButton.checked ? "#b8a6e8" : "#8a8a92"
                                font.pixelSize: 14
                                Layout.alignment: Qt.AlignHCenter
                            }
                            Label {
                                text: navButton.modelData.label
                                color: navButton.checked ? "#e8e8ea" : "#8a8a92"
                                font.pixelSize: 9
                                Layout.alignment: Qt.AlignHCenter
                            }
                        }
                        background: Rectangle {
                            color: navButton.checked ? "#2c2c31" : (navButton.hovered ? "#1f1f23" : "transparent")
                            radius: 6
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
            color: "#1a1a1d"

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 10

                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: qsTr("Sessions")
                        color: "#e8e8ea"
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
                                color: "#e8e8ea"
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                // v1.10.3 field contract: the wire field is
                                // msgCount (internal/sessions Session json
                                // tag) — the v1.10.2 messageCount read was
                                // always undefined, rendering "0 messages".
                                text: (modelData.msgCount !== undefined ? modelData.msgCount : 0) + qsTr(" messages")
                                color: "#8a8a92"
                                font.pixelSize: 11
                            }
                        }

                        background: Rectangle {
                            color: Core.currentSession.id === modelData.id
                                   ? "#2c2c31" : (parent.hovered ? "#222226" : "transparent")
                            radius: 6
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

        Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; visible: root.currentView === "chat" || root.currentView === "agent"; color: "#2a2a2e" }

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
                visible: Core.error !== ""
                color: "#5a1f22"
                Layout.preferredHeight: 34
                Label {
                    anchors.centerIn: parent
                    text: Core.error
                    color: "#ffd9da"
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
                    radius: 8
                    color: modelData.role === "user" ? "#26415a" : "#222226"

                    ColumnLayout {
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 4

                        Label {
                            text: modelData.role === "user" ? qsTr("You") : qsTr("SHEYTAN")
                            color: modelData.role === "user" ? "#8fb8dd" : "#b8a6e8"
                            font.pixelSize: 11
                            font.bold: true
                        }
                        TextEdit {
                            id: bubbleText
                            text: modelData.content
                            readOnly: true
                            wrapMode: TextEdit.Wrap
                            color: "#e8e8ea"
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
                Layout.margins: 12
                visible: Core.liveActive
                implicitHeight: liveColumn.implicitHeight + 24
                radius: 8
                color: "#2a2440"

                ColumnLayout {
                    id: liveColumn
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    RowLayout {
                        Layout.fillWidth: true
                        Label {
                            text: qsTr("SHEYTAN (live)")
                            color: "#b8a6e8"
                            font.pixelSize: 11
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
                            color: "#e8e8ea"
                            background: null
                        }
                    }
                }
            }

            // Composer
            Rectangle {
                Layout.fillWidth: true
                Layout.preferredHeight: composer.implicitHeight + 24
                color: "#1a1a1d"

                RowLayout {
                    id: composer
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 8

                    ScrollView {
                        Layout.fillWidth: true
                        Layout.maximumHeight: 120
                        TextArea {
                            id: input
                            placeholderText: Core.currentSession.id ? qsTr("Message…") : qsTr("Create or open a session first")
                            wrapMode: TextArea.Wrap
                            color: "#e8e8ea"
                            enabled: !Core.busy && Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                            background: Rectangle {
                                color: "#222226"
                                radius: 6
                                border.color: "#33333a"
                            }
                            Keys.onReturnPressed: (event) => {
                                if (!(event.modifiers & Qt.ShiftModifier)) { sendButton.action(); event.accepted = true }
                            }
                        }
                    }

                    Button {
                        id: sendButton
                        text: qsTr("Send")
                        enabled: !Core.busy && input.text.trim() !== "" && Core.currentSession.id !== ""
                        function action() {
                            Core.send(input.text, false)
                            input.clear()
                        }
                        onClicked: action()
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
