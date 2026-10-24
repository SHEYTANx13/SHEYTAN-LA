// LabView.qml — v1.10.15-fixed R1 (D28/I21): the Coding Lab surface,
// parity with the Wails LabPanel.
//
// IPC CLIENT of the ONE Go authority: snapshots come from GET /api/lab
// ({"tasks": […]}); a request is POSTed VERBATIM through /api/lab via
// Core.labRun — the lab's own request protocol, its workspace policy and
// its verification stay server-owned; nothing is composed or validated
// client-side beyond "valid JSON object".
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "labView"
    color: Theme.background

    Component.onCompleted: if (Core.connected) Core.refreshLab()
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) Core.refreshLab()
        }
    }

    RowLayout {
        anchors.fill: parent
        anchors.margins: Theme.spaceL
        spacing: Theme.spaceL

        // --- Snapshots -----------------------------------------------------
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
                        text: qsTr("CODING LAB")
                        color: Theme.accent
                        font.pixelSize: Theme.fontXS
                        font.bold: true
                    }
                    Label {
                        text: qsTr("Autonomous engineering — execute, verify, repair")
                        color: Theme.text
                        font.pixelSize: Theme.fontL
                        font.bold: true
                        Layout.fillWidth: true
                    }
                    Button {
                        text: qsTr("Refresh")
                        onClicked: Core.refreshLab()
                    }
                }

                ListView {
                    id: labList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: Theme.spaceS
                    model: Core.labTasks
                    ScrollBar.vertical: ScrollBar {}

                    delegate: Rectangle {
                        required property var modelData
                        width: labList.width
                        height: labCol.implicitHeight + 20
                        radius: Theme.radiusM
                        color: Theme.elevatedAlt
                        Behavior on color {
                            enabled: Core.animationsEnabled
                            ColorAnimation { duration: Theme.motionFast }
                        }

                        ColumnLayout {
                            id: labCol
                            anchors.fill: parent
                            anchors.margins: 10
                            spacing: Theme.spaceXS

                            RowLayout {
                                Layout.fillWidth: true
                                Label {
                                    text: modelData.taskId !== undefined ? modelData.taskId
                                          : (modelData.id !== undefined ? modelData.id : qsTr("task"))
                                    color: Theme.text
                                    font.bold: true
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: modelData.status !== undefined ? String(modelData.status) : ""
                                    color: modelData.status === "completed" ? Theme.success : Theme.warn
                                    font.pixelSize: Theme.fontXS
                                }
                            }
                            Label {
                                Layout.fillWidth: true
                                text: String(JSON.stringify(modelData))
                                color: Theme.muted
                                font.pixelSize: 10
                                wrapMode: Text.Wrap
                                maximumLineCount: 4
                            }
                        }
                    }

                    EmptyState {
                        anchors.centerIn: parent
                        visible: Core.labTasks.length === 0
                        icon: "◆"
                        title: Core.connected ? qsTr("No lab tasks") : qsTr("Not connected")
                        detail: Core.connected
                                ? qsTr("The lab has no active task snapshots.")
                                : qsTr("Connect to the core to list lab tasks.")
                    }
                }
            }
        }

        // --- Request console (1:1 POST /api/lab passthrough) -----------------
        Rectangle {
            Layout.preferredWidth: 380
            Layout.fillHeight: true
            radius: Theme.radiusM
            color: Theme.surface

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.spaceM
                spacing: Theme.spaceS

                Label {
                    text: qsTr("Lab request")
                    color: Theme.text
                    font.bold: true
                }
                Label {
                    Layout.fillWidth: true
                    text: qsTr("The JSON object is forwarded verbatim to POST /api/lab — the lab's own protocol, unchanged.")
                    color: Theme.muted
                    font.pixelSize: Theme.fontXS
                    wrapMode: Text.Wrap
                }
                ScrollView {
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    TextArea {
                        id: labRequest
                        text: "{\n  \"action\": \"\"\n}"
                        wrapMode: TextArea.Wrap
                        color: Theme.text
                        selectByMouse: true
                        background: Rectangle { color: Theme.elevatedAlt; radius: Theme.radiusM; border.color: Theme.controlHover }
                    }
                }
                Button {
                    Layout.fillWidth: true
                    text: qsTr("Run request")
                    enabled: Core.connected && labRequest.text.trim() !== ""
                    onClicked: Core.labRun(labRequest.text)
                }
            }
        }
    }
}
