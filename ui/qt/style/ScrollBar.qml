// ScrollBar.qml — themed scroll bar (run-#48 R1b): slim rounded thumb on a
// transparent groove.
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.ScrollBar {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth, 10)
    implicitHeight: Math.max(implicitBackgroundHeight, 10)

    contentItem: Rectangle {
        implicitWidth: control.horizontal ? parent.width : 6
        implicitHeight: control.horizontal ? 6 : parent.height
        radius: Math.min(width, height) / 2
        color: control.pressed ? Theme.borderStrong
             : (control.hovered ? Theme.mutedStrong : Theme.border)
        visible: !(control.policy === T.ScrollBar.AlwaysOff)
                 && (control.active || control.hovered || control.pressed
                     || control.policy === T.ScrollBar.AlwaysOn)
    }

    background: Rectangle {
        radius: Theme.radiusS
        color: "transparent"
    }
}
