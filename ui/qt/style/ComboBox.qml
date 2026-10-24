// ComboBox.qml — themed dropdown (run-#48 R1b). The popup reuses the
// themed ItemDelegate through the style; no 90-degree corner anywhere.
import QtQuick
import QtQuick.Templates as T
import sheytan.ui

T.ComboBox {
    id: control

    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)

    leftPadding: Theme.spaceM
    rightPadding: Theme.spaceM
    topPadding: Theme.spaceS
    bottomPadding: Theme.spaceS
    spacing: Theme.spaceS

    font.pixelSize: Theme.fontM

    contentItem: T.TextField {
        text: control.editable ? control.editText : control.displayText
        enabled: control.editable
        autoScroll: control.editable
        readOnly: !control.editable
        inputMethodHints: control.inputMethodHints
        validator: control.validator
        selectByMouse: control.selectTextByMouse
        color: control.enabled ? Theme.text : Theme.muted
        selectionColor: Theme.accentDim
        selectedTextColor: Theme.text
        placeholderTextColor: Theme.mutedStrong
        font.pixelSize: Theme.fontM
        background: Item {}
    }

    indicator: Text {
        x: control.mirrored ? control.leftPadding : control.width - width - control.rightPadding
        y: control.topPadding + (control.availableHeight - height) / 2
        width: 16
        height: 16
        text: "\u25BE"
        font.pixelSize: Theme.fontM
        color: control.enabled ? Theme.muted : Theme.mutedStrong
        verticalAlignment: Text.AlignVCenter
        horizontalAlignment: Text.AlignHCenter
    }

    background: Rectangle {
        implicitWidth: 140
        implicitHeight: 30
        radius: Theme.radiusM
        border.width: 1
        border.color: control.activeFocus ? Theme.accent : Theme.border
        color: control.enabled ? (control.hovered ? Theme.surfaceAlt : Theme.surface) : Theme.elevated
    }

    delegate: T.ItemDelegate {
        width: ListView.view.width
        required property var model
        required property int index
        text: model[control.textRole]
        font.weight: control.currentIndex === index ? Font.DemiBold : Font.Normal
        highlighted: control.highlightedIndex === index
        hoverEnabled: control.hoverEnabled
    }

    popup: T.Popup {
        y: control.height + Theme.spaceXS
        width: Math.max(control.width, 160)
        padding: 1

        contentItem: ListView {
            clip: true
            implicitHeight: contentHeight
            model: control.popup.visible ? control.delegateModel : null
            currentIndex: control.highlightedIndex
            ScrollBar.vertical: control.ScrollBar.vertical
        }

        background: Rectangle {
            radius: Theme.radiusM
            border.width: 1
            border.color: Theme.borderStrong
            color: Theme.elevated
        }
    }
}
