// PerfStrip.qml — v1.10.15-fixed R1 (D28/I21): the performance strip,
// parity with the Wails PerfStrip. EVERY number shown is DERIVED from the
// actual client state (stream activity, transcript size) — no decorative
// percentage, no fabricated telemetry.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "perfStrip"
    color: Theme.surface
    implicitWidth: 300
    implicitHeight: 28

    RowLayout {
        anchors.fill: parent
        anchors.margins: 6
        spacing: Theme.spaceM

        Rectangle {
            width: 8; height: 8; radius: Theme.radiusS
            color: Core.busy ? Theme.success : (Core.connected ? Theme.borderStrong : Theme.errorDim)
        }
        Label {
            text: Core.liveActive ? qsTr("streaming") : (Core.busy ? qsTr("working") : qsTr("idle"))
            color: Theme.muted
            font.pixelSize: 10
        }
        Label {
            text: qsTr("live chars: ") + Core.liveText.length
            color: Theme.muted
            font.pixelSize: 10
        }
        Label {
            text: qsTr("messages: ") + Core.messages.length
            color: Theme.muted
            font.pixelSize: 10
        }
        Item { Layout.fillWidth: true }
    }
}
