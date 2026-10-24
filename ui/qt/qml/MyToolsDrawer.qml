// MyToolsDrawer.qml — v1.10.15-fixed R1 (D28/I21): the My Tools surface,
// parity with the Wails MyToolsCard. A pure /api/custom-tools client —
// the tool store, its registration state and its policy stay server-owned.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Drawer {
    id: root
    objectName: "myToolsDrawer"
    edge: Qt.RightEdge
    width: 420
    height: parent ? parent.height : 0
    onOpened: if (Core.connected) Core.refreshTools()

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
                text: qsTr("My Tools")
                color: Theme.text
                font.pixelSize: 16
                font.bold: true
                Layout.fillWidth: true
            }
            Button {
                text: qsTr("Refresh")
                onClicked: Core.refreshTools()
            }
        }

        ListView {
            id: toolList
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: 6
            model: Core.tools
            ScrollBar.vertical: ScrollBar {}

            delegate: Rectangle {
                required property var modelData
                width: toolList.width
                height: toolCol.implicitHeight + 20
                radius: Theme.radiusM
                color: Theme.elevatedAlt
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: toolCol
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: 2
                    RowLayout {
                        Layout.fillWidth: true
                        Label {
                            Layout.fillWidth: true
                            text: modelData.name !== undefined ? modelData.name : qsTr("tool")
                            color: Theme.text
                            font.bold: true
                            elide: Text.ElideRight
                        }
                        Label {
                            text: modelData.registered === true ? qsTr("registered") : qsTr("unregistered")
                            color: modelData.registered === true ? Theme.success : Theme.muted
                            font.pixelSize: 10
                        }
                    }
                    Label {
                        Layout.fillWidth: true
                        text: modelData.description !== undefined ? String(modelData.description) : ""
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }
                }
            }

            EmptyState {
                anchors.centerIn: parent
                visible: Core.tools.length === 0
                icon: "\u25C7"
                title: Core.connected ? qsTr("No custom tools") : qsTr("Not connected")
                detail: qsTr("Custom tool definitions from the core store appear here.")
            }
        }
    }
}
