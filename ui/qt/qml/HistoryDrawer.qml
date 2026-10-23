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

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 14
        spacing: 10

        RowLayout {
            Layout.fillWidth: true
            Label {
                text: qsTr("History")
                color: "#e8e8ea"
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
            spacing: 8
            TextField {
                id: searchField
                Layout.fillWidth: true
                placeholderText: qsTr("Search past sessions…")
                color: "#e8e8ea"
                selectByMouse: true
                onAccepted: Core.searchHistory(text, modeBox.currentText)
                background: Rectangle { color: "#222226"; radius: 6; border.color: "#33333a" }
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
                        color: "#e8e8ea"
                        font.pixelSize: 12
                        elide: Text.ElideRight
                    }
                    Label {
                        Layout.fillWidth: true
                        text: String(JSON.stringify(modelData))
                        color: "#8a8a92"
                        font.pixelSize: 10
                        elide: Text.ElideRight
                    }
                }
                background: Rectangle {
                    color: parent.hovered ? "#222226" : "#1a1a1d"
                    radius: 6
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
