// TextField.qml — themed input (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.TextField {
    id: control

    implicitWidth: implicitBackgroundWidth + leftInset + rightInset
                   || 120
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    leftPadding: Theme.spaceM
    rightPadding: Theme.spaceM
    topPadding: Theme.spaceS
    bottomPadding: Theme.spaceS

    color: enabled ? Theme.text : Theme.muted
    selectionColor: Theme.accentDim
    selectedTextColor: Theme.text
    placeholderTextColor: Theme.mutedStrong
    font.pixelSize: Theme.fontM

    background: Rectangle {
        implicitWidth: 140
        implicitHeight: 30
        radius: Theme.radiusM
        border.width: 1
        border.color: control.activeFocus ? Theme.accent : Theme.border
        color: control.enabled ? Theme.surface : Theme.elevated
    }
}
