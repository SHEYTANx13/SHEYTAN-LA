// AgentView.qml — v1.10.15-fixed R1 (D28/I21): the Agent surface, parity
// with the Wails Agent IA (AgentSidebar / AgentHeader / AgentBody /
// AgentTaskPanel — src/workspace.ts keeps Agent machinery on the agent
// view; Chat stays a calm surface without it).
//
// IPC CLIENT of the ONE Go authority: the SAME runtime as Chat (the
// transcript is the authoritative /api/sessions state, streaming via the
// activity WS); the goals/task panel is a pure /api/goals client — no
// second run registry, no local plan state beyond display.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "agentView"
    color: Theme.background

    property string selectedGoalId: ""
    // The History/Models/Goals overlays are owned by the shell (Main.qml);
    // the view emits intents — no cross-file id reach-in.
    signal openHistoryRequested()
    signal openModelsRequested()
    signal openGoalsRequested()

    Component.onCompleted: if (Core.connected) { Core.refreshGoals(); Core.refreshEngine() }
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) { Core.refreshGoals(); Core.refreshEngine() }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // --- Agent header ------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 48
            color: Theme.surfaceElevated

            RowLayout {
                anchors.fill: parent
                anchors.margins: 10
                spacing: 10

                Label {
                    text: qsTr("AGENT")
                    color: Theme.accent
                    font.pixelSize: Theme.fontXS
                    font.bold: true
                }
                Label {
                    text: qsTr("Interactive local intelligence")
                    color: Theme.text
                    font.pixelSize: 14
                    font.bold: true
                    Layout.fillWidth: true
                }
                Label {
                    text: Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                          ? qsTr("session: ") + (Core.currentSession.title !== undefined && Core.currentSession.title !== "" ? Core.currentSession.title : Core.currentSession.id)
                          : qsTr("no session")
                    color: Theme.muted
                    font.pixelSize: Theme.fontXS
                    elide: Text.ElideRight
                    Layout.maximumWidth: 320
                }
                Button {
                    text: qsTr("History")
                    enabled: Core.connected
                    onClicked: root.openHistoryRequested()
                }
                Button {
                    text: qsTr("Models")
                    enabled: Core.connected
                    onClicked: root.openModelsRequested()
                }
            }
        }

        Rectangle { Layout.fillWidth: true; Layout.preferredHeight: 1; color: Theme.border }

        // --- Body: goals sidebar | transcript | task panel -----------------
        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Goals sidebar (AgentSidebar parity — the drive machinery's
            // goal list; control actions stay in the Goals drawer).
            Rectangle {
                Layout.fillHeight: true
                Layout.preferredWidth: 240
                color: Theme.surface

                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: Theme.spaceS

                    Label {
                        text: qsTr("Goals")
                        color: Theme.text
                        font.bold: true
                    }

                    ListView {
                        id: agentGoalList
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        spacing: 6
                        model: Core.goals
                        ScrollBar.vertical: ScrollBar {}

                        delegate: ItemDelegate {
                            required property var modelData
                            property bool active: root.selectedGoalId === modelData.goalId
                            width: agentGoalList.width
                            height: 54
                            onClicked: root.selectedGoalId = modelData.goalId
                            contentItem: ColumnLayout {
                                spacing: 2
                                Label {
                                    text: modelData.originalGoal
                                    color: parent.parent.active ? Theme.accent : Theme.text
                                    font.pixelSize: Theme.fontS
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: modelData.status + " · " + modelData.phase
                                    color: Theme.muted
                                    font.pixelSize: 10
                                    elide: Text.ElideRight
                                }
                            }
                            background: Rectangle {
                                color: parent.active ? Theme.control : (parent.hovered ? Theme.elevatedAlt : "transparent")
                                radius: Theme.radiusM
                                Behavior on color {
                                    enabled: Core.animationsEnabled
                                    ColorAnimation { duration: Theme.motionFast }
                                }
                            }
                        }

                        EmptyState {
                            anchors.centerIn: parent
                            visible: Core.goals.length === 0
                            icon: "◇"
                            title: Core.connected ? qsTr("No goals") : qsTr("Not connected")
                            detail: qsTr("The task panel fills from the goal engine.")
                        }
                    }
                }
            }

            Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: Theme.border }

            // Transcript (AgentBody parity — same authoritative session).
            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: 0

                ListView {
                    id: agentTranscript
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
                        width: agentTranscript.width - 48
                        x: 24
                        height: agentText.implicitHeight + 28
                        radius: Theme.radiusM
                        color: modelData.role === "user" ? Theme.infoDim : Theme.elevatedAlt

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
                                id: agentText
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
                        title: qsTr("Agent")
                        detail: qsTr("Engineering machinery over the same local runtime.")
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.margins: Theme.spaceM
                    // v1.10.15-fixed R3/R4 (D32/I22): the live panel appears
                    // and settles under the ONE motion gate.
                    visible: Core.liveActive || opacity > 0
                    opacity: Core.liveActive ? 1 : 0
                    implicitHeight: agentLive.implicitHeight + 24
                    radius: Theme.radiusM
                    color: Theme.accentDim
                    Behavior on opacity {
                        enabled: Core.animationsEnabled
                        NumberAnimation { duration: 160 }
                    }

                    ColumnLayout {
                        id: agentLive
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
                        ScrollView {
                            Layout.fillWidth: true
                            Layout.maximumHeight: 140
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

                // v1.10.15-fixed R3 (D37/I21, Task ID 42): MaintenanceBanner
                // parity — the honest maintenance lifecycle from the
                // EXISTING /api/maintenance handler (Core.maintenance), the
                // src/MaintenanceBanner.tsx contract (v1.6.0 P0/P4): the
                // explicit phase renders, never a generic "Updating…".
                // Envelope-guarded like SystemView (the run-#43 TypeError
                // class): an empty map renders nothing; a terminal phase is
                // the core's own report, not a local guess.
                Rectangle {
                    objectName: "agent-maintenance-banner"
                    Layout.fillWidth: true
                    Layout.margins: Theme.spaceM
                    visible: Core.connected
                             && Core.maintenance.phase !== undefined
                             && Core.maintenance.phase !== ""
                    implicitHeight: maintLabel.implicitHeight + 20
                    radius: Theme.radiusM
                    color: Theme.warnDim
                    Label {
                        id: maintLabel
                        anchors.fill: parent
                        anchors.margins: 10
                        text: qsTr("engine maintenance: ")
                              + (Core.maintenance.phase !== undefined ? Core.maintenance.phase : "")
                              + (Core.maintenance.message !== undefined && Core.maintenance.message !== ""
                                 ? " — " + Core.maintenance.message : "")
                        color: Theme.warn
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                }

                // v1.10.15-fixed R3 (D37/I21): DownloadProgressPanel parity
                // — the engine Download Manager surface (src/DownloadProgress.tsx,
                // v1.2.3): determinate progress while bytesTotal > 0,
                // honest indeterminate otherwise (bytesTotal/etaSeconds go
                // negative when the server did not declare a size), phase
                // label, measured speed/ETA, and the cancel invokable 1:1
                // to POST /api/llama {"action":"cancel-download"}. The feed
                // is the EXISTING GET /api/engine snapshot (Core.engine);
                // the download lifecycle stays server-owned.
                Rectangle {
                    objectName: "agent-download-panel"
                    Layout.fillWidth: true
                    Layout.margins: Theme.spaceM
                    visible: Core.connected
                             && Core.engine.download !== undefined
                             && Core.engine.download.phase !== undefined
                    implicitHeight: downloadCol.implicitHeight + 20
                    radius: Theme.radiusM
                    color: Theme.infoDimAlt

                    ColumnLayout {
                        id: downloadCol
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: 6

                        RowLayout {
                            Layout.fillWidth: true
                            Label {
                                text: qsTr("Downloading engine asset — ")
                                      + (Core.engine.download !== undefined && Core.engine.download.phase !== undefined
                                         ? Core.engine.download.phase : "")
                                color: Theme.info
                                font.pixelSize: Theme.fontS
                                font.bold: true
                                Layout.fillWidth: true
                            }
                            Button {
                                text: qsTr("Stop download")
                                onClicked: Core.cancelEngineDownload()
                            }
                        }

                        ProgressBar {
                            Layout.fillWidth: true
                            indeterminate: Core.engine.download === undefined
                                           || Core.engine.download.bytesTotal === undefined
                                           || Core.engine.download.bytesTotal <= 0
                            from: 0
                            to: Core.engine.download !== undefined
                                && Core.engine.download.bytesTotal !== undefined
                                && Core.engine.download.bytesTotal > 0
                                ? Core.engine.download.bytesTotal : 1
                            value: Core.engine.download !== undefined
                                   && Core.engine.download.bytesDone !== undefined
                                   && !indeterminate ? Core.engine.download.bytesDone : 0
                        }

                        Label {
                            Layout.fillWidth: true
                            visible: Core.engine.download !== undefined
                                     && Core.engine.download.bytesPerSec !== undefined
                            text: visible
                                  ? (Core.engine.download.bytesPerSec / 1048576).toFixed(2) + qsTr(" MB/s")
                                  : ""
                            color: Theme.muted
                            font.pixelSize: 10
                        }
                    }
                }

                RowLayout {
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.spaceM
                    Layout.rightMargin: Theme.spaceM
                    // v1.10.15-fixed R3 (D37/I21): ActivityStream parity —
                    // the live activity feed toggle (src/AgentBody.tsx:
                    // "Show activity"/"Hide activity"). Events come from the
                    // bounded display ring Core.activityEvents (the last 50
                    // frames — the Wails MAX_VISIBLE_EVENTS bound); format
                    // mirrors formatActivity: caption → message → error →
                    // type.
                    Item { Layout.fillWidth: true }
                    Button {
                        id: agentActivityToggle
                        checkable: true
                        text: checked ? qsTr("Hide activity") : qsTr("Show activity")
                    }
                }

                Rectangle {
                    objectName: "agent-activity-stream"
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.spaceM
                    Layout.rightMargin: Theme.spaceM
                    Layout.preferredHeight: agentActivityToggle.checked ? 160 : 0
                    visible: agentActivityToggle.checked
                    clip: true
                    radius: Theme.radiusM
                    color: Theme.surface

                    ListView {
                        anchors.fill: parent
                        anchors.margins: Theme.spaceS
                        clip: true
                        spacing: Theme.spaceXS
                        model: Core.activityEvents
                        ScrollBar.vertical: ScrollBar {}

                        onCountChanged: Qt.callLater(positionAtEnd)
                        function positionAtEnd() { positionViewAtEnd() }

                        delegate: Label {
                            required property var modelData
                            width: ListView.view.width
                            text: {
                                if (modelData.caption !== undefined && modelData.caption !== "") return modelData.caption
                                if (modelData.message !== undefined) return modelData.message
                                if (modelData.error !== undefined) return modelData.error
                                return modelData.type
                            }
                            color: modelData.type === "error" ? Theme.error : Theme.textSoft
                            font.pixelSize: Theme.fontXS
                            elide: Text.ElideRight
                        }
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: agentComposer.implicitHeight + 24
                    color: Theme.surfaceElevated

                    RowLayout {
                        id: agentComposer
                        anchors.fill: parent
                        anchors.margins: Theme.spaceM
                        spacing: Theme.spaceS

                        ScrollView {
                            Layout.fillWidth: true
                            Layout.maximumHeight: 110
                            TextArea {
                                id: agentInput
                                placeholderText: Core.currentSession.id
                                                 ? qsTr("Task the agent…") : qsTr("Create or open a session first")
                                wrapMode: TextArea.Wrap
                                color: Theme.text
                                enabled: !Core.busy && Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                                background: Rectangle { color: Theme.elevatedAlt; radius: Theme.radiusM; border.color: Theme.controlHover }
                                Keys.onReturnPressed: (event) => {
                                    // v1.10.15-fixed (R5/#43): submit() — the
                                    // final-member action() override class is
                                    // retired (hosted QWARN evidence).
                                    if (!(event.modifiers & Qt.ShiftModifier)) { agentSend.submit(); event.accepted = true }
                                }
                            }
                        }

                        Button {
                            id: agentSend
                            text: qsTr("Send")
                            enabled: !Core.busy && agentInput.text.trim() !== "" && Core.currentSession.id !== ""
                            function submit() {
                                Core.send(agentInput.text, netSearchToggle.checked)
                                agentInput.clear()
                            }
                            onClicked: submit()
                        }

                        Button {
                            id: netSearchToggle
                            checkable: true
                            text: qsTr("Net search")
                        }
                    }
                }
            }

            Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: Theme.border }

            // Task panel (AgentTaskPanel parity — the selected goal's
            // ACTUAL plan state; progress derives from PlanStep.status).
            Rectangle {
                Layout.fillHeight: true
                Layout.preferredWidth: 300
                color: Theme.surface

                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: Theme.spaceS

                    Label {
                        text: qsTr("Task panel")
                        color: Theme.text
                        font.bold: true
                    }

                    Label {
                        Layout.fillWidth: true
                        visible: root.selectedGoalId === ""
                        text: qsTr("Select a goal to inspect its plan.")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }

                    Repeater {
                        model: root.selectedGoalId === "" ? [] : goalsForTask()

                        delegate: Rectangle {
                            required property var modelData
                            Layout.fillWidth: true
                            implicitHeight: taskStepCol.implicitHeight + 16
                            radius: Theme.radiusM
                            color: modelData.status === "completed" ? Theme.successDim : Theme.elevatedAlt

                            ColumnLayout {
                                id: taskStepCol
                                anchors.fill: parent
                                anchors.margins: Theme.spaceS
                                spacing: 2
                                Label {
                                    text: qsTr("step") + " " + (index + 1)
                                    color: Theme.muted
                                    font.pixelSize: 10
                                }
                                Label {
                                    text: modelData.objective !== undefined ? modelData.objective : ""
                                    color: Theme.text
                                    font.pixelSize: Theme.fontXS
                                    wrapMode: Text.Wrap
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: modelData.status !== undefined ? modelData.status : ""
                                    color: modelData.status === "completed" ? Theme.success : Theme.warn
                                    font.pixelSize: 10
                                }
                            }
                        }
                    }

                    Item { Layout.fillHeight: true }

                    Button {
                        Layout.fillWidth: true
                        text: qsTr("Open Goals")
                        enabled: Core.connected
                        onClicked: root.openGoalsRequested()
                    }
                }
            }
        }
    }

    function goalsForTask() {
        for (var i = 0; i < Core.goals.length; i++)
            if (Core.goals[i].goalId === root.selectedGoalId) {
                var g = Core.goals[i]
                return g.plan !== undefined ? g.plan : []
            }
        return []
    }
}
