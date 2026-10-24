// EmptyState.qml — shared empty/loading placeholder (no mock data, no
// demo panels: honest empty and connecting states only).
import QtQuick
import QtQuick.Controls

Column {
    property string icon: "◇"
    property string title: ""
    property string detail: ""

    spacing: Theme.spaceS

    Label {
        text: icon
        color: Theme.borderStrong
        font.pixelSize: 42
        anchors.horizontalCenter: parent.horizontalCenter
    }
    Label {
        text: title
        color: Theme.muted
        font.pixelSize: Theme.fontL
        font.bold: true
        anchors.horizontalCenter: parent.horizontalCenter
    }
    Label {
        text: detail
        color: Theme.mutedStrong
        font.pixelSize: Theme.fontS
        wrapMode: Text.Wrap
        width: 320
        horizontalAlignment: Text.AlignHCenter
        anchors.horizontalCenter: parent.horizontalCenter
    }
}
