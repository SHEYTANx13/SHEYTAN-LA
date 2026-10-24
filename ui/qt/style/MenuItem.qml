// MenuItem.qml — themed menu item (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.MenuItem {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    leftPadding: Theme.spaceM
    rightPadding: Theme.spaceM
    topPadding: Theme.spaceS
    bottomPadding: Theme.spaceS
    spacing: Theme.spaceM

    font.pixelSize: Theme.fontM

    contentItem: T.IconLabel {
        spacing: control.spacing
        display: control.display
        alignment: Qt.AlignLeft
        icon: control.icon
        text: control.text
        font: control.font
        color: control.enabled ? Theme.text : Theme.muted
    }

    background: Rectangle {
        implicitWidth: 180
        implicitHeight: 28
        radius: Theme.radiusS
        color: control.highlighted ? (control.pressed ? Theme.accentDim : Theme.surfaceAlt)
             : "transparent"
    }
}
