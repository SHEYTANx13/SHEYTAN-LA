// AutomationView.qml — v1.10.15-fixed R1 (D28/I21): the Automation &
// Tasks surface, parity with the Wails AutomationPanel.
//
// IPC CLIENT of the ONE Go authority: tasks are GET /api/automation/tasks
// (the scheduler's own view + running flag); every control action maps
// 1:1 to POST /api/automation/tasks/{id}/{pause|resume|cancel|run};
// artifacts are GET /api/automation/artifacts. The scheduler, its stores
// and its tool policy stay server-owned.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "automationView"
    color: Theme.background

    Component.onCompleted: {
        if (Core.connected) { Core.refreshAutomation(); Core.refreshAutomationArtifacts() }
    }
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) { Core.refreshAutomation(); Core.refreshAutomationArtifacts() }
        }
    }

    RowLayout {
        anchors.fill: parent
        anchors.margins: Theme.spaceL
        spacing: Theme.spaceL

        // --- Tasks ----------------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            radius: Theme.radiusM
            color: Theme.surfaceElevated
            Behavior on color {
                enabled: Core.animationsEnabled
                ColorAnimation { duration: Theme.motionFast }
            }

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.spaceM
                spacing: Theme.spaceS

                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: qsTr("AUTOMATION")
                        color: Theme.accent
                        font.pixelSize: Theme.fontXS
                        font.bold: true
                    }
                    Label {
                        text: qsTr("Schedules, runs, tools, artifacts")
                        color: Theme.text
                        font.pixelSize: Theme.fontL
                        font.bold: true
                        Layout.fillWidth: true
                    }
                    Button {
                        text: qsTr("Refresh")
                        onClicked: { Core.refreshAutomation(); Core.refreshAutomationArtifacts() }
                    }
                }

                ListView {
                    id: taskList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: Theme.spaceS
                    model: Core.automationTasks
                    ScrollBar.vertical: ScrollBar {}

                    delegate: Rectangle {
                        id: taskCard
                        required property var modelData
                        required property int index
                        width: taskList.width
                        height: taskCol.implicitHeight + 20
                        radius: Theme.radiusM
                        color: Theme.elevatedAlt
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }

                        function taskId() {
                            return taskCard.modelData.id !== undefined ? taskCard.modelData.id : ""
                        }
                        function running() {
                            return taskCard.modelData.running === true
                        }

                        ColumnLayout {
                            id: taskCol
                            anchors.fill: parent
                            anchors.margins: 10
                            spacing: Theme.spaceXS

                            RowLayout {
                                Layout.fillWidth: true
                                Label {
                                    text: taskCard.modelData.name !== undefined
                                          ? taskCard.modelData.name : taskCard.taskId()
                                    color: Theme.text
                                    font.bold: true
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: taskCard.running() ? qsTr("RUNNING") : qsTr("idle")
                                    color: taskCard.running() ? Theme.success : Theme.muted
                                    font.pixelSize: 10
                                    font.bold: true
                                }
                            }
                            Label {
                                Layout.fillWidth: true
                                text: taskCard.taskId() + "  ·  "
                                      + (taskCard.modelData.schedule !== undefined
                                         ? String(JSON.stringify(taskCard.modelData.schedule)) : qsTr("manual"))
                                color: Theme.muted
                                font.pixelSize: Theme.fontXS
                                elide: Text.ElideRight
                            }
                            RowLayout {
                                spacing: 6
                                Button {
                                    text: qsTr("Run")
                                    enabled: taskCard.taskId() !== "" && !taskCard.running()
                                    onClicked: Core.automationTaskAction(taskCard.taskId(), "run")
                                }
                                Button {
                                    text: qsTr("Pause")
                                    enabled: taskCard.taskId() !== ""
                                    onClicked: Core.automationTaskAction(taskCard.taskId(), "pause")
                                }
                                Button {
                                    text: qsTr("Resume")
                                    enabled: taskCard.taskId() !== ""
                                    onClicked: Core.automationTaskAction(taskCard.taskId(), "resume")
                                }
                                Button {
                                    text: qsTr("Cancel")
                                    enabled: taskCard.taskId() !== ""
                                    onClicked: Core.automationTaskAction(taskCard.taskId(), "cancel")
                                }
                            }
                        }
                    }

                    EmptyState {
                        anchors.centerIn: parent
                        visible: Core.automationTasks.length === 0
                        icon: "⏱"
                        title: Core.connected ? qsTr("No tasks") : qsTr("Not connected")
                        detail: Core.connected
                                ? qsTr("The scheduler lists no tasks.")
                                : qsTr("Connect to the core to list automation tasks.")
                    }
                }
            }
        }

        // --- Artifacts -------------------------------------------------------
        Rectangle {
            Layout.preferredWidth: 360
            Layout.fillHeight: true
            radius: Theme.radiusM
            color: Theme.surface

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.spaceM
                spacing: Theme.spaceS

                Label {
                    text: qsTr("Artifacts")
                    color: Theme.text
                    font.bold: true
                }

                ListView {
                    id: artifactList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: 6
                    model: Core.automationArtifacts
                    ScrollBar.vertical: ScrollBar {}

                    delegate: Rectangle {
                        required property var modelData
                        width: artifactList.width
                        height: artCol.implicitHeight + 16
                        radius: Theme.radiusM
                        color: Theme.elevatedAlt
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }

                        ColumnLayout {
                            id: artCol
                            anchors.fill: parent
                            anchors.margins: Theme.spaceS
                            spacing: 2
                            Label {
                                Layout.fillWidth: true
                                text: modelData.name !== undefined ? modelData.name
                                      : (modelData.id !== undefined ? modelData.id : qsTr("artifact"))
                                color: Theme.text
                                font.pixelSize: Theme.fontS
                                elide: Text.ElideRight
                            }
                            Label {
                                Layout.fillWidth: true
                                text: String(JSON.stringify(modelData))
                                color: Theme.muted
                                font.pixelSize: 10
                                elide: Text.ElideRight
                            }
                        }
                    }

                    EmptyState {
                        anchors.centerIn: parent
                        visible: Core.automationArtifacts.length === 0
                        icon: "◇"
                        title: qsTr("No artifacts")
                        detail: qsTr("Task-scoped artifacts appear here from the store.")
                    }
                }
            }
        }
    }
}
