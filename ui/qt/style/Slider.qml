// Slider.qml — themed slider (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.Slider {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    leftPadding: 6
    rightPadding: 6
    topPadding: 6
    bottomPadding: 6

    background: Rectangle {
        x: control.leftPadding
        y: control.topPadding + control.availableHeight / 2 - height / 2
        width: control.availableWidth
        height: 4
        radius: height / 2
        color: Theme.elevatedAlt

        Rectangle {
            width: control.visualPosition * parent.width
            height: parent.height
            radius: parent.radius
            color: control.enabled ? Theme.accent : Theme.mutedStrong
        }
    }

    handle: Rectangle {
        x: control.leftPadding + control.visualPosition * (control.availableWidth - width)
        y: control.topPadding + control.availableHeight / 2 - height / 2
        width: 16
        height: 16
        radius: width / 2
        border.width: 1
        border.color: control.pressed ? Theme.accentPress : Theme.borderStrong
        color: control.enabled ? (control.pressed ? Theme.accentHover : Theme.accent) : Theme.muted
    }
}
