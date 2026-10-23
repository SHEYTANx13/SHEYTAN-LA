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
    color: "#121214"

    Component.onCompleted: if (Core.connected) Core.refreshLab()
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) Core.refreshLab()
        }
    }

    RowLayout {
        anchors.fill: parent
        anchors.margins: 16
        spacing: 16

        // --- Snapshots -----------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            radius: 8
            color: "#1a1a1d"

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 8

                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: qsTr("CODING LAB")
                        color: "#b8a6e8"
                        font.pixelSize: 11
                        font.bold: true
                    }
                    Label {
                        text: qsTr("Autonomous engineering — execute, verify, repair")
                        color: "#e8e8ea"
                        font.pixelSize: 15
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
                    spacing: 8
                    model: Core.labTasks
                    ScrollBar.vertical: ScrollBar {}

                    delegate: Rectangle {
                        required property var modelData
                        width: labList.width
                        height: labCol.implicitHeight + 20
                        radius: 8
                        color: "#222226"

                        ColumnLayout {
                            id: labCol
                            anchors.fill: parent
                            anchors.margins: 10
                            spacing: 4

                            RowLayout {
                                Layout.fillWidth: true
                                Label {
                                    text: modelData.taskId !== undefined ? modelData.taskId
                                          : (modelData.id !== undefined ? modelData.id : qsTr("task"))
                                    color: "#e8e8ea"
                                    font.bold: true
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    text: modelData.status !== undefined ? String(modelData.status) : ""
                                    color: modelData.status === "completed" ? "#7dd3a0" : "#e8c268"
                                    font.pixelSize: 11
                                }
                            }
                            Label {
                                Layout.fillWidth: true
                                text: String(JSON.stringify(modelData))
                                color: "#8a8a92"
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
            radius: 8
            color: "#17171a"

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 8

                Label {
                    text: qsTr("Lab request")
                    color: "#e8e8ea"
                    font.bold: true
                }
                Label {
                    Layout.fillWidth: true
                    text: qsTr("The JSON object is forwarded verbatim to POST /api/lab — the lab's own protocol, unchanged.")
                    color: "#8a8a92"
                    font.pixelSize: 11
                    wrapMode: Text.Wrap
                }
                ScrollView {
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    TextArea {
                        id: labRequest
                        text: "{\n  \"action\": \"\"\n}"
                        wrapMode: TextArea.Wrap
                        color: "#e8e8ea"
                        selectByMouse: true
                        background: Rectangle { color: "#222226"; radius: 6; border.color: "#33333a" }
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
