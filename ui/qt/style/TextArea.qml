// TextArea.qml — themed multi-line input (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.TextArea {
    id: control

    implicitWidth: implicitContentWidth + leftPadding + rightPadding || 200
    implicitHeight: Math.max(implicitContentHeight + topPadding + bottomPadding, 60)

    leftPadding: Theme.spaceM
    rightPadding: Theme.spaceM
    topPadding: Theme.spaceS
    bottomPadding: Theme.spaceS

    color: enabled ? Theme.text : Theme.muted
    selectionColor: Theme.accentDim
    selectedTextColor: Theme.text
    placeholderTextColor: Theme.mutedStrong
    font.pixelSize: Theme.fontM
    wrapMode: TextEdit.Wrap

    background: Rectangle {
        implicitWidth: 200
        implicitHeight: 60
        radius: Theme.radiusM
        border.width: 1
        border.color: control.activeFocus ? Theme.accent : Theme.border
        color: control.enabled ? Theme.surface : Theme.elevated
    }
}
