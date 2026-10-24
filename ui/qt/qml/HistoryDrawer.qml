// HistoryDrawer.qml — v1.10.15-fixed R1 (D28/I21): the History surface,
// parity with the Wails HistoryPicker. A pure /api/history/search client
// — hits open the authoritative session; nothing is stored client-side.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Drawer {
    id: root
    objectName: "historyDrawer"
    edge: Qt.RightEdge
    width: 420
    height: parent ? parent.height : 0
    onOpened: if (Core.connected) Core.searchHistory(searchField.text, modeBox.currentText)

    // v1.10.15-fixed R3/R4 (D32/I22, Task ID 41): the drawer's slide is
    // EXPLICIT and gated — off means the panel appears instantly in its
    // final position (identical function, zero motion).
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
                text: qsTr("History")
                color: Theme.text
                font.pixelSize: 16
                font.bold: true
                Layout.fillWidth: true
            }
            Button {
                text: qsTr("Search")
                onClicked: Core.searchHistory(searchField.text, modeBox.currentText)
            }
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.spaceS
            TextField {
                id: searchField
                Layout.fillWidth: true
                placeholderText: qsTr("Search past sessions…")
                color: Theme.text
                selectByMouse: true
                onAccepted: Core.searchHistory(text, modeBox.currentText)
                background: Rectangle { color: Theme.elevatedAlt; radius: Theme.radiusM; border.color: Theme.controlHover }
            }
            ComboBox {
                id: modeBox
                model: ["", "chat", "agent"]
            }
        }

        ListView {
            id: hitList
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: 6
            model: Core.historyHits
            ScrollBar.vertical: ScrollBar {}

            delegate: ItemDelegate {
                required property var modelData
                width: hitList.width
                height: 64
                onClicked: {
                    if (modelData.sessionId !== undefined) Core.openSession(modelData.sessionId)
                    root.close()
                }
                contentItem: ColumnLayout {
                    spacing: 2
                    Label {
                        Layout.fillWidth: true
                        text: modelData.title !== undefined ? modelData.title
                              : (modelData.sessionId !== undefined ? modelData.sessionId : qsTr("hit"))
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
                background: Rectangle {
                    color: parent.hovered ? Theme.elevatedAlt : Theme.surfaceElevated
                    radius: Theme.radiusM
                    Behavior on color {
                        enabled: Core.animationsEnabled
                        ColorAnimation { duration: Theme.motionFast }
                    }
                }
            }

            EmptyState {
                anchors.centerIn: parent
                visible: Core.historyHits.length === 0
                icon: "\u25C7"
                title: qsTr("No hits")
                detail: qsTr("Search the durable history across sessions.")
            }
        }
    }
}
