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
    color: "#121214"

    property string selectedGoalId: ""
    // The History/Models/Goals overlays are owned by the shell (Main.qml);
    // the view emits intents — no cross-file id reach-in.
    signal openHistoryRequested()
    signal openModelsRequested()
    signal openGoalsRequested()

    Component.onCompleted: if (Core.connected) Core.refreshGoals()
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) Core.refreshGoals()
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // --- Agent header ------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 48
            color: "#1a1a1d"

            RowLayout {
                anchors.fill: parent
                anchors.margins: 10
                spacing: 10

                Label {
                    text: qsTr("AGENT")
                    color: "#b8a6e8"
                    font.pixelSize: 11
                    font.bold: true
                }
                Label {
                    text: qsTr("Interactive local intelligence")
                    color: "#e8e8ea"
                    font.pixelSize: 14
                    font.bold: true
                    Layout.fillWidth: true
                }
                Label {
                    text: Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                          ? qsTr("session: ") + (Core.currentSession.title !== undefined && Core.currentSession.title !== "" ? Core.currentSession.title : Core.currentSession.id)
                          : qsTr("no session")
                    color: "#8a8a92"
                    font.pixelSize: 11
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

        Rectangle { Layout.fillWidth: true; Layout.preferredHeight: 1; color: "#2a2a2e" }

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
                color: "#17171a"

                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: 8

                    Label {
                        text: qsTr("Goals")
                        color: "#e8e8ea"
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
                                    color: parent.parent.active ? "#b8a6e8" : "#e8e8ea"
                                    font.pixelSize: 12
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: modelData.status + " · " + modelData.phase
                                    color: "#8a8a92"
                                    font.pixelSize: 10
                                    elide: Text.ElideRight
                                }
                            }
                            background: Rectangle {
                                color: parent.active ? "#2c2c31" : (parent.hovered ? "#222226" : "transparent")
                                radius: 6
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

            Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: "#2a2a2e" }

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
                                id: agentText
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
                        title: qsTr("Agent")
                        detail: qsTr("Engineering machinery over the same local runtime.")
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.margins: 12
                    visible: Core.liveActive
                    implicitHeight: agentLive.implicitHeight + 24
                    radius: 8
                    color: "#2a2440"

                    ColumnLayout {
                        id: agentLive
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
                        ScrollView {
                            Layout.fillWidth: true
                            Layout.maximumHeight: 140
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

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: agentComposer.implicitHeight + 24
                    color: "#1a1a1d"

                    RowLayout {
                        id: agentComposer
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 8

                        ScrollView {
                            Layout.fillWidth: true
                            Layout.maximumHeight: 110
                            TextArea {
                                id: agentInput
                                placeholderText: Core.currentSession.id
                                                 ? qsTr("Task the agent…") : qsTr("Create or open a session first")
                                wrapMode: TextArea.Wrap
                                color: "#e8e8ea"
                                enabled: !Core.busy && Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                                background: Rectangle { color: "#222226"; radius: 6; border.color: "#33333a" }
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

            Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: "#2a2a2e" }

            // Task panel (AgentTaskPanel parity — the selected goal's
            // ACTUAL plan state; progress derives from PlanStep.status).
            Rectangle {
                Layout.fillHeight: true
                Layout.preferredWidth: 300
                color: "#17171a"

                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: 8

                    Label {
                        text: qsTr("Task panel")
                        color: "#e8e8ea"
                        font.bold: true
                    }

                    Label {
                        Layout.fillWidth: true
                        visible: root.selectedGoalId === ""
                        text: qsTr("Select a goal to inspect its plan.")
                        color: "#8a8a92"
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }

                    Repeater {
                        model: root.selectedGoalId === "" ? [] : goalsForTask()

                        delegate: Rectangle {
                            required property var modelData
                            Layout.fillWidth: true
                            implicitHeight: taskStepCol.implicitHeight + 16
                            radius: 6
                            color: modelData.status === "completed" ? "#243a2c" : "#222226"

                            ColumnLayout {
                                id: taskStepCol
                                anchors.fill: parent
                                anchors.margins: 8
                                spacing: 2
                                Label {
                                    text: qsTr("step") + " " + (index + 1)
                                    color: "#8a8a92"
                                    font.pixelSize: 10
                                }
                                Label {
                                    text: modelData.objective !== undefined ? modelData.objective : ""
                                    color: "#e8e8ea"
                                    font.pixelSize: 11
                                    wrapMode: Text.Wrap
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: modelData.status !== undefined ? modelData.status : ""
                                    color: modelData.status === "completed" ? "#7dd3a0" : "#e8c268"
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
