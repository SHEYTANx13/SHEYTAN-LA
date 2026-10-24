// ProgressBar.qml — themed progress bar (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.ProgressBar {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    leftPadding: 0
    rightPadding: 0
    topPadding: 0
    bottomPadding: 0

    background: Rectangle {
        x: control.leftPadding
        y: control.topPadding + (control.availableHeight - height) / 2
        width: control.availableWidth
        height: 6
        radius: height / 2
        color: Theme.elevatedAlt
        border.width: 0
    }

    contentItem: Item {
        clip: true

        Rectangle {
            x: control.leftPadding
            y: control.topPadding + (control.availableHeight - height) / 2
            width: control.availableWidth * control.position
            height: 6
            radius: height / 2
            color: Theme.accent
        }
    }
}
