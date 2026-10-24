// Menu.qml — themed menu surface (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.Menu {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            contentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             contentHeight + topPadding + bottomPadding)

    leftPadding: Theme.spaceS
    rightPadding: Theme.spaceS
    topPadding: Theme.spaceS
    bottomPadding: Theme.spaceS

    margins: 0

    background: Rectangle {
        implicitWidth: 180
        implicitHeight: 40
        radius: Theme.radiusM
        border.width: 1
        border.color: Theme.borderStrong
        color: Theme.elevated
    }
}
