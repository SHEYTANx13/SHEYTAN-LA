// ItemDelegate.qml — themed list item (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.ItemDelegate {
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

    icon.width: 16
    icon.height: 16
    icon.color: control.enabled ? Theme.text : Theme.muted

    contentItem: T.IconLabel {
        spacing: control.spacing
        display: control.display
        alignment: Qt.AlignLeft
        icon: control.icon
        text: control.text
        font: control.font
        color: control.highlighted ? Theme.text : (control.enabled ? Theme.textSoft : Theme.muted)
        elide: Text.ElideRight
    }

    background: Rectangle {
        implicitWidth: 160
        implicitHeight: 30
        radius: Theme.radiusS
        color: control.highlighted ? (control.pressed ? Theme.accentDim : Theme.surfaceAlt)
             : (control.hovered ? Theme.elevatedAlt : "transparent")
        border.width: control.visualFocus ? 1 : 0
        border.color: Theme.accent
    }
}
