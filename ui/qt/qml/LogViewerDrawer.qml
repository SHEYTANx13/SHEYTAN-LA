// LogViewerDrawer.qml — v1.10.15-fixed R1 (D28/I21): the Log Viewer
// surface, parity with the Wails LogViewer. Entries are GET /api/logs
// (the server redacts BEFORE anything leaves the process); the filter is
// client-side text matching over the already-bounded window.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Drawer {
    id: root
    objectName: "logViewerDrawer"
    edge: Qt.RightEdge
    width: 560
    height: parent ? parent.height : 0
    onOpened: if (Core.connected) Core.refreshLogs()

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
                text: qsTr("Logs")
                color: Theme.text
                font.pixelSize: 16
                font.bold: true
                Layout.fillWidth: true
            }
            TextField {
                id: filterField
                Layout.preferredWidth: 180
                placeholderText: qsTr("filter…")
                color: Theme.text
                selectByMouse: true
                background: Rectangle { color: Theme.elevatedAlt; radius: Theme.radiusM; border.color: Theme.controlHover }
            }
            Button {
                text: qsTr("Refresh")
                onClicked: Core.refreshLogs()
            }
        }

        ListView {
            id: logList
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: 2
            model: Core.logs
            ScrollBar.vertical: ScrollBar {}

            delegate: Rectangle {
                id: logRow
                required property var modelData
                required property int index
                width: logList.width
                height: logText.implicitHeight + 12
                color: logHover.hovered ? Theme.surfaceElevated : "transparent"
                // v1.10.15-fixed R3/R4 (D32/I22): a gated hover step for
                // scanning long log runs.
                HoverHandler { id: logHover }
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: 100 }
                }
                visible: {
                    var f = filterField.text
                    if (f === "") return true
                    return String(JSON.stringify(modelData)).indexOf(f) !== -1
                }

                TextEdit {
                    id: logText
                    anchors.fill: parent
                    anchors.margins: 6
                    readOnly: true
                    selectByMouse: true
                    wrapMode: TextEdit.Wrap
                    textFormat: TextEdit.PlainText
                    color: logRow.modelData !== undefined && logRow.modelData.level !== undefined
                           && (logRow.modelData.level === "ERROR" || logRow.modelData.level === "WARN")
                           ? Theme.warn : Theme.muted
                    font.pixelSize: 10
                    font.family: "monospace"
                    text: String(JSON.stringify(logRow.modelData))
                }
            }

            EmptyState {
                anchors.centerIn: parent
                visible: Core.logs.length === 0
                icon: "\u25C7"
                title: qsTr("No log entries")
                detail: qsTr("The core's bounded, redacted log ring appears here.")
            }
        }
    }
}
