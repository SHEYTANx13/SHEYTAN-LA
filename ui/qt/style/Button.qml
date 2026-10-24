// Button.qml — the themed default for every stock Button (run-#48 R1b).
// Templates base, Basic-style structure, Theme tokens: no 90-degree
// corner, hover/press/disabled themed. Authored motion stays gated at the
// view layer (I22) — this file ships NO animation by design.
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.Button {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    padding: Theme.spaceS
    leftPadding: Theme.spaceM
    rightPadding: Theme.spaceM
    spacing: Theme.spaceXS

    icon.width: 16
    icon.height: 16
    icon.color: highlighted ? Theme.accentText : (enabled ? Theme.text : Theme.muted)

    opacity: enabled ? 1.0 : 0.45

    contentItem: T.IconLabel {
        spacing: control.spacing
        display: control.display
        alignment: control.display === T.IconLabel.IconOnly || control.display === T.IconLabel.TextUnderIcon ? Qt.AlignCenter : Qt.AlignLeft
        icon: control.icon
        text: control.text
        font: control.font
        color: control.highlighted ? Theme.accentText : (control.enabled ? Theme.text : Theme.muted)
    }

    background: Rectangle {
        implicitWidth: 64
        implicitHeight: 30
        radius: Theme.radiusM
        border.width: 1
        border.color: control.highlighted ? Theme.accentPress : Theme.border
        color: control.highlighted
               ? (control.pressed ? Theme.accentPress : (control.hovered ? Theme.accentHover : Theme.accent))
               : (control.pressed ? Theme.elevatedAlt : (control.hovered ? Theme.surfaceAlt : Theme.elevatedAlt))
    }
}
