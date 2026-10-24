// ModelPickerDrawer.qml — v1.10.15-fixed R1 (D28/I21): the ModelPicker
// surface, parity with the Wails ModelPicker. Models are GET /api/models;
// selection patches config.model through POST /api/config — the ONE
// selection path the settings surface already uses.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Drawer {
    id: root
    objectName: "modelPickerDrawer"
    edge: Qt.RightEdge
    width: 440
    height: parent ? parent.height : 0
    onOpened: if (Core.connected) Core.refreshModels()

    // v1.10.15-fixed R3/R4 (D32/I22, Task ID 41): gated slide — off means
    // instant open/close, identical function.
    enter: Transition {
        enabled: Core.animationsEnabled
        NumberAnimation { property: "position"; to: 1.0; duration: 220; easing.type: Easing.OutCubic }
    }
    exit: Transition {
        enabled: Core.animationsEnabled
        NumberAnimation { property: "position"; to: 0.0; duration: 180; easing.type: Easing.InCubic }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 14
        spacing: 10

        RowLayout {
            Layout.fillWidth: true
            Label {
                text: qsTr("Models")
                color: "#e8e8ea"
                font.pixelSize: 16
                font.bold: true
                Layout.fillWidth: true
            }
            Button {
                text: qsTr("Refresh")
                onClicked: Core.refreshModels()
            }
        }

        Label {
            Layout.fillWidth: true
            text: Core.config.model !== undefined && Core.config.model !== ""
                  ? qsTr("active: ") + Core.config.model
                  : qsTr("no model selected")
            color: "#8fb8dd"
            font.pixelSize: 12
            elide: Text.ElideRight
        }

        ListView {
            id: modelList
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: 6
            model: Core.models
            ScrollBar.vertical: ScrollBar {}

            delegate: Rectangle {
                id: modelCard
                required property var modelData
                width: modelList.width
                height: modCol.implicitHeight + 20
                radius: 8
                color: Core.config.model === modelData.id ? "#243a2c" : "#222226"
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: 150 }
                }

                ColumnLayout {
                    id: modCol
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: 2

                    RowLayout {
                        Layout.fillWidth: true
                        Label {
                            Layout.fillWidth: true
                            text: modelData.name !== undefined ? modelData.name : modelData.id
                            color: "#e8e8ea"
                            font.bold: true
                            elide: Text.ElideRight
                        }
                        Label {
                            visible: Core.config.model === modelCard.modelData.id
                            text: qsTr("ACTIVE")
                            color: "#7dd3a0"
                            font.pixelSize: 10
                            font.bold: true
                        }
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (modelData.id !== undefined ? modelData.id : "")
                              + (modelData.quantization !== undefined ? "  \u00b7  " + modelData.quantization : "")
                              + (modelData.sizeBytes !== undefined ? "  \u00b7  " + Math.round(modelData.sizeBytes / (1024 * 1024)) + " MB" : "")
                        color: "#8a8a92"
                        font.pixelSize: 11
                        elide: Text.ElideRight
                    }
                    Button {
                        visible: Core.config.model !== modelCard.modelData.id
                        text: qsTr("Select")
                        onClicked: Core.patchConfig({ model: modelCard.modelData.id })
                    }
                }
            }

            EmptyState {
                anchors.centerIn: parent
                visible: Core.models.length === 0
                icon: "\u25C7"
                title: Core.connected ? qsTr("No local models") : qsTr("Not connected")
                detail: qsTr("Models in the configured models directory appear here.")
            }
        }
    }
}
