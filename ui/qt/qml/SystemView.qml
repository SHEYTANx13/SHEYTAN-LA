// SystemView.qml — v1.11.0-fixed R3 (run-#48, Task ID 43): the System
// Centre surface, parity with the Wails SystemPanel.
//
// IPC CLIENT of the ONE Go authority: the hardware snapshot (GET
// /api/sysinfo via Core.sysinfo), the engine state (GET /api/engine via
// Core.engine), the preflight report (GET /api/preflight), the Governor
// policy block (GET /api/governor) and the maintenance facts (GET
// /api/maintenance) are read-only evidence — recommendations and
// enforcement stay server-owned; this view holds no second policy
// authority. Every nested read is guarded in the text binding itself
// (the run-#43 envelope rule: a visible: guard never stops eager text
// evaluation).
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "systemView"
    color: Theme.background

    Component.onCompleted: refreshAll()
    Connections {
        target: Core
        function onConnectedChanged() { if (Core.connected) refreshAll() }
    }

    function refreshAll() {
        if (!Core.connected) return
        Core.refreshSysinfo()
        Core.refreshEngine()
        Core.refreshPreflight()
        Core.refreshGovernor()
        Core.refreshMaintenance()
    }

    function gb(bytes) {
        const v = Number(bytes)
        return isNaN(v) ? "?" : (v / (1024 * 1024 * 1024)).toFixed(1) + " GB"
    }

    ScrollView {
        anchors.fill: parent
        clip: true

        ColumnLayout {
            width: root.width - 48
            x: 24
            spacing: Theme.spaceM

            Label {
                text: qsTr("SYSTEM CENTRE")
                color: Theme.accent
                font.pixelSize: Theme.fontXS
                font.bold: true
                Layout.topMargin: Theme.spaceL
            }
            Label {
                text: qsTr("Environment Centre — device, runtime, health")
                color: Theme.text
                font.pixelSize: Theme.fontXL
                font.bold: true
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.spaceS
                Button { text: qsTr("Refresh all"); onClicked: refreshAll() }
                Label {
                    text: Core.error !== "" ? Core.error : ""
                    color: Theme.warn
                    font.pixelSize: Theme.fontXS
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }

            // --- Hardware (v1.11.0-fixed R3: the DeviceCard parity row) ----
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: hwCol.implicitHeight + 24
                radius: Theme.radiusM
                color: Theme.surfaceElevated
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: hwCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Hardware")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (Core.sysinfo.os !== undefined)
                              ? (Core.sysinfo.os + "/" + Core.sysinfo.arch
                                 + (Core.sysinfo.hostname !== undefined && Core.sysinfo.hostname !== ""
                                    ? "  ·  " + Core.sysinfo.hostname : ""))
                              : qsTr("hardware snapshot not loaded — connect and refresh")
                        color: Theme.info
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (Core.sysinfo.cpu !== undefined && Core.sysinfo.cpu.name !== undefined)
                              ? qsTr("CPU: ") + Core.sysinfo.cpu.name
                                + qsTr("  ·  ") + Core.sysinfo.cpu.physicalCores + "C/" + Core.sysinfo.cpu.logicalCores + "T"
                              : qsTr("CPU: not loaded")
                        color: Theme.text
                        font.pixelSize: Theme.fontS
                        elide: Text.ElideRight
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (Core.sysinfo.ram !== undefined)
                              ? qsTr("RAM: ") + gb(Core.sysinfo.ram.totalBytes)
                                + qsTr(" total · ") + gb(Core.sysinfo.ram.availableBytes) + qsTr(" available")
                              : qsTr("RAM: not loaded")
                        color: Theme.text
                        font.pixelSize: Theme.fontS
                        elide: Text.ElideRight
                    }
                    Label {
                        Layout.fillWidth: true
                        // The GPU list — the operator's "hardware unmatched"
                        // story: every detected device, named, with VRAM and
                        // driver. No GPU row renders "no GPU detected".
                        text: (Core.sysinfo.gpus !== undefined && Core.sysinfo.gpus.length > 0)
                              ? qsTr("GPU: ") + Core.sysinfo.gpus.map(function (g) {
                                    var s = (g.name !== undefined && g.name !== "") ? g.name : qsTr("unknown device")
                                    if (g.vramBytes !== undefined && g.vramBytes > 0) s += " (" + gb(g.vramBytes) + ")"
                                    if (g.driverVersion !== undefined && g.driverVersion !== "") s += " driver " + g.driverVersion
                                    return s
                                }).join("  ·  ")
                              : (Core.sysinfo.os !== undefined ? qsTr("GPU: none detected") : qsTr("GPU: not loaded"))
                        color: Theme.text
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.sysinfo.npu !== undefined && Core.sysinfo.npu !== null
                                 && Core.sysinfo.npu.name !== undefined
                        text: qsTr("NPU: ") + Core.sysinfo.npu.name
                        color: Theme.text
                        font.pixelSize: Theme.fontS
                        elide: Text.ElideRight
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (Core.sysinfo.disk !== undefined)
                              ? qsTr("Disk: ") + gb(Core.sysinfo.disk.freeBytes) + qsTr(" free of ") + gb(Core.sysinfo.disk.totalBytes)
                                + (Core.sysinfo.wsl2 === true ? qsTr("  ·  WSL2") : "")
                                + (Core.sysinfo.docker === true ? qsTr("  ·  Docker") : "")
                              : qsTr("Disk: not loaded")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        elide: Text.ElideRight
                    }
                }
            }

            // --- Engine (v1.11.0-fixed R3: the RuntimeCard parity row) -----
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: engCol.implicitHeight + 24
                radius: Theme.radiusM
                color: Theme.surfaceElevated
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: engCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Engine")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (Core.engine.state !== undefined)
                              ? qsTr("state: ") + Core.engine.state
                                + (Core.engine.backend !== undefined && Core.engine.backend !== ""
                                   ? qsTr("  ·  backend: ") + Core.engine.backend : "")
                                + (Core.engine.provider !== undefined && Core.engine.provider !== ""
                                   ? qsTr("  ·  provider: ") + Core.engine.provider : "")
                              : qsTr("engine snapshot not loaded")
                        color: Theme.info
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        text: (Core.engine.model !== undefined && Core.engine.model !== "")
                              ? qsTr("serving: ") + Core.engine.model
                              : (Core.engine.selection !== undefined
                                 && Core.engine.selection.selectionRequired === true
                                 ? qsTr("serving: nothing — model selection required")
                                 : qsTr("serving: no model loaded"))
                        color: (Core.engine.selection !== undefined
                                && Core.engine.selection.selectionRequired === true) ? Theme.warn : Theme.text
                        font.pixelSize: Theme.fontS
                        elide: Text.ElideRight
                    }
                    // The GPU-offload posture exactly as the config owns it:
                    // the AUTO pipeline's recommendation state and the user's
                    // explicit override are both visible here.
                    Label {
                        Layout.fillWidth: true
                        text: (Core.config.gpuAutoOffload !== undefined)
                              ? qsTr("GPU offload: ") + (Core.config.gpuAutoOffload === true
                                   ? qsTr("auto (provision when worthy)")
                                   : (Number(Core.config.llm !== undefined && Core.config.llm !== null
                                       ? (Core.config.llm.numGPU !== undefined ? Core.config.llm.numGPU : 0) : 0) > 0
                                       ? qsTr("on (numGPU ") + Core.config.llm.numGPU + ")"
                                       : qsTr("off")))
                              : qsTr("GPU offload: config not loaded")
                        color: Theme.text
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                }
            }

            // --- Preflight -----------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: preCol.implicitHeight + 24
                radius: Theme.radiusM
                color: Theme.surfaceElevated
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: preCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Preflight report")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.preflight.compatible !== undefined
                        text: qsTr("compatible: ") + Core.preflight.compatible
                              + qsTr("  ·  severity: ") + (Core.preflight.severity !== undefined ? Core.preflight.severity : "?")
                              + qsTr("  ·  device: ") + (Core.preflight.device !== undefined ? Core.preflight.device : "?")
                              + qsTr("  ·  backend: ") + (Core.preflight.backend !== undefined ? Core.preflight.backend : "?")
                        color: Core.preflight.compatible === false ? Theme.warn : Theme.success
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.preflight.refusalMessage !== undefined
                                 && Core.preflight.refusalMessage !== ""
                        text: String(Core.preflight.refusalMessage)
                        color: Theme.warn
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }
                    Repeater {
                        model: Core.preflight.reasons !== undefined ? Core.preflight.reasons : []
                        delegate: Label {
                            required property var modelData
                            Layout.fillWidth: true
                            text: "· " + modelData
                            color: Theme.muted
                            font.pixelSize: Theme.fontXS
                            wrapMode: Text.Wrap
                        }
                    }
                    Label {
                        visible: Core.preflight.compatible === undefined
                        text: qsTr("Not loaded yet — connect and refresh.")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                    }
                }
            }

            // --- Governor --------------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: govCol.implicitHeight + 24
                radius: Theme.radiusM
                color: Theme.surfaceElevated
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: govCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Governor")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.governor.governor !== undefined
                              ? qsTr("available: ") + Core.governor.governor.available
                                + qsTr("  ·  state: ") + (Core.governor.governor.state !== undefined
                                   ? String(JSON.stringify(Core.governor.governor.state)) : "?")
                              : qsTr("Governor block not loaded")
                        color: Theme.info
                        font.pixelSize: Theme.fontS
                        wrapMode: Text.Wrap
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.governor.governor !== undefined
                                 && Core.governor.governor.envelope !== undefined
                        text: (Core.governor.governor !== undefined
                               && Core.governor.governor.envelope !== undefined)
                              ? qsTr("envelope: ") + String(JSON.stringify(Core.governor.governor.envelope))
                              : qsTr("envelope: not loaded")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }
                }
            }

            // --- Maintenance -------------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: maintCol.implicitHeight + 24
                radius: Theme.radiusM
                color: Theme.surfaceElevated
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: maintCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Maintenance")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.maintenance !== undefined
                              && Object.keys(Core.maintenance).length > 0
                              ? String(JSON.stringify(Core.maintenance))
                              : qsTr("Maintenance facts not loaded")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }
                }
            }

            Item { Layout.preferredHeight: Theme.spaceL }
        }
    }
}
