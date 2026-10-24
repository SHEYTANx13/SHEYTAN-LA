// CheckBox.qml — themed checkbox (run-#48 R1b).
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.CheckBox {
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
        width: 18
        height: 18
        radius: Theme.radiusS
        border.width: 1
        border.color: control.down ? Theme.accentPress
                    : (control.hovered ? Theme.accent
                    : (control.visualFocus ? Theme.accent : Theme.borderStrong))
        color: control.checked ? (control.down ? Theme.accentPress : Theme.accent)
             : (control.hovered ? Theme.surfaceAlt : Theme.elevated)

        Text {
            anchors.centerIn: parent
            width: parent.width
            height: parent.height
            text: control.checkState === Qt.PartiallyChecked ? "\u2212" : "\u2713"
            font.pixelSize: Theme.fontM
            font.bold: true
            color: Theme.accentText
            visible: control.checkState !== Qt.Unchecked
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
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
