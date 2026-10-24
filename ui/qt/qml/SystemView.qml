// SystemView.qml — v1.10.15-fixed R1 (D28/I21): the System Centre surface,
// parity with the Wails SystemPanel.
//
// IPC CLIENT of the ONE Go authority: the preflight report
// (GET /api/preflight), the Governor policy block (GET /api/governor) and
// the maintenance facts (GET /api/maintenance) are read-only evidence —
// recommendations and enforcement stay server-owned; this view holds no
// second policy authority.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "systemView"
    color: "#121214"

    Component.onCompleted: refreshAll()
    Connections {
        target: Core
        function onConnectedChanged() { if (Core.connected) refreshAll() }
    }

    function refreshAll() {
        if (!Core.connected) return
        Core.refreshPreflight()
        Core.refreshGovernor()
        Core.refreshMaintenance()
    }

    ScrollView {
        anchors.fill: parent
        clip: true

        ColumnLayout {
            width: root.width - 48
            x: 24
            spacing: 12

            Label {
                text: qsTr("SYSTEM CENTRE")
                color: "#b8a6e8"
                font.pixelSize: 11
                font.bold: true
                Layout.topMargin: 16
            }
            Label {
                text: qsTr("Environment Centre — device, runtime, health")
                color: "#e8e8ea"
                font.pixelSize: 18
                font.bold: true
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: 8
                Button { text: qsTr("Refresh all"); onClicked: refreshAll() }
                Label {
                    text: Core.error !== "" ? Core.error : ""
                    color: "#e8c268"
                    font.pixelSize: 11
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }

            // --- Preflight -----------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: preCol.implicitHeight + 24
                radius: 8
                color: "#1a1a1d"
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: 120 }
                }

                ColumnLayout {
                    id: preCol
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    Label {
                        text: qsTr("Preflight report")
                        color: "#e8e8ea"
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.preflight.compatible !== undefined
                        text: qsTr("compatible: ") + Core.preflight.compatible
                              + qsTr("  ·  severity: ") + (Core.preflight.severity !== undefined ? Core.preflight.severity : "?")
                              + qsTr("  ·  device: ") + (Core.preflight.device !== undefined ? Core.preflight.device : "?")
                              + qsTr("  ·  backend: ") + (Core.preflight.backend !== undefined ? Core.preflight.backend : "?")
                        color: Core.preflight.compatible === false ? "#e8c268" : "#7dd3a0"
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.preflight.refusalMessage !== undefined
                                 && Core.preflight.refusalMessage !== ""
                        text: String(Core.preflight.refusalMessage)
                        color: "#e8c268"
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }
                    Repeater {
                        model: Core.preflight.reasons !== undefined ? Core.preflight.reasons : []
                        delegate: Label {
                            required property var modelData
                            Layout.fillWidth: true
                            text: "· " + modelData
                            color: "#8a8a92"
                            font.pixelSize: 11
                            wrapMode: Text.Wrap
                        }
                    }
                    Label {
                        visible: Core.preflight.compatible === undefined
                        text: qsTr("Not loaded yet — connect and refresh.")
                        color: "#8a8a92"
                        font.pixelSize: 11
                    }
                }
            }

            // --- Governor --------------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: govCol.implicitHeight + 24
                radius: 8
                color: "#1a1a1d"
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: 120 }
                }

                ColumnLayout {
                    id: govCol
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    Label {
                        text: qsTr("Governor")
                        color: "#e8e8ea"
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.governor.governor !== undefined
                              ? qsTr("available: ") + Core.governor.governor.available
                                + qsTr("  ·  state: ") + (Core.governor.governor.state !== undefined
                                   ? String(JSON.stringify(Core.governor.governor.state)) : "?")
                              : qsTr("Governor block not loaded")
                        color: "#8fb8dd"
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.governor.governor !== undefined
                                 && Core.governor.governor.envelope !== undefined
                        // v1.10.15-fixed (R2/#43, Task ID 40): the text
                        // binding itself now guards the nested read. A
                        // `visible:` guard NEVER stops the eager text
                        // evaluation — with a fresh CoreClient (m_governor
                        // = {}), Core.governor.governor is undefined and
                        // the old unguarded expression threw "Cannot read
                        // property 'envelope' of undefined" at load time
                        // (hosted TypeError, qmlLoadsCleanly, run #43).
                        text: (Core.governor.governor !== undefined
                               && Core.governor.governor.envelope !== undefined)
                              ? qsTr("envelope: ") + String(JSON.stringify(Core.governor.governor.envelope))
                              : qsTr("envelope: not loaded")
                        color: "#8a8a92"
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }
                }
            }

            // --- Maintenance -------------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: maintCol.implicitHeight + 24
                radius: 8
                color: "#1a1a1d"
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: 120 }
                }

                ColumnLayout {
                    id: maintCol
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    Label {
                        text: qsTr("Maintenance")
                        color: "#e8e8ea"
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.maintenance !== undefined
                              && Object.keys(Core.maintenance).length > 0
                              ? String(JSON.stringify(Core.maintenance))
                              : qsTr("Maintenance facts not loaded")
                        color: "#8a8a92"
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }
                }
            }

            Item { Layout.preferredHeight: 16 }
        }
    }
}
