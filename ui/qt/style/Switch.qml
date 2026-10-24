// Switch.qml — themed switch (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.Switch {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    leftPadding: 0
    rightPadding: 0
    spacing: Theme.spaceS

    font.pixelSize: Theme.fontM

    indicator: Rectangle {
        x: control.leftPadding
        y: control.topPadding + (control.availableHeight - height) / 2
        width: 40
        height: 22
        radius: height / 2
        border.width: 1
        border.color: control.checked ? Theme.accentPress : Theme.borderStrong
        color: control.checked ? (control.down ? Theme.accentPress : Theme.accentDim)
             : (control.hovered ? Theme.surfaceAlt : Theme.elevated)

        Rectangle {
            x: control.checked ? parent.width - width - 3 : 3
            anchors.verticalCenter: parent.verticalCenter
            width: 16
            height: 16
            radius: width / 2
            color: control.checked ? Theme.accent : Theme.mutedStrong
            border.width: 0
        }
    }

    contentItem: T.Text {
        text: control.text
        font: control.font
        opacity: control.enabled ? 1.0 : 0.45
        color: control.enabled ? Theme.text : Theme.muted
        verticalAlignment: Text.AlignVCenter
        leftPadding: control.indicator.width + control.spacing
        elide: Text.ElideRight
    }
}
