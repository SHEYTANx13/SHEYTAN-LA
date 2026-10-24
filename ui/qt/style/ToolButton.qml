// ToolButton.qml — themed flat tool button (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.ToolButton {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    padding: Theme.spaceS
    spacing: Theme.spaceXS

    icon.width: 16
    icon.height: 16
    icon.color: enabled ? Theme.text : Theme.muted

    opacity: enabled ? 1.0 : 0.45

    contentItem: T.IconLabel {
        spacing: control.spacing
        display: control.display
        alignment: Qt.AlignCenter
        icon: control.icon
        text: control.text
        font: control.font
        color: control.enabled ? Theme.text : Theme.muted
    }

    background: Rectangle {
        implicitWidth: 30
        implicitHeight: 30
        radius: Theme.radiusM
        color: control.pressed ? Theme.elevatedAlt
             : (control.hovered ? Theme.surfaceAlt : "transparent")
        border.width: control.checked ? 1 : 0
        border.color: Theme.borderStrong
    }
}
