// EmptyState.qml — shared empty/loading placeholder (no mock data, no
// demo panels: honest empty and connecting states only).
import QtQuick
import QtQuick.Controls

Column {
    property string icon: "◇"
    property string title: ""
    property string detail: ""

    spacing: 8

    Label {
        text: icon
        color: "#4a4a52"
        font.pixelSize: 42
        anchors.horizontalCenter: parent.horizontalCenter
    }
    Label {
        text: title
        color: "#8a8a92"
        font.pixelSize: 15
        font.bold: true
        anchors.horizontalCenter: parent.horizontalCenter
    }
    Label {
        text: detail
        color: "#5a5a62"
        font.pixelSize: 12
        wrapMode: Text.Wrap
        width: 320
        horizontalAlignment: Text.AlignHCenter
        anchors.horizontalCenter: parent.horizontalCenter
    }
}
