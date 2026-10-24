// WorkspaceView.qml — v1.10.15-fixed R1 (D28/I21): the Work Environment
// surface, parity with the Wails WorkspacePanel + RepositoryIndexCard.
//
// IPC CLIENT of the ONE Go authority: the summary is /api/workspace (the
// server assembles it from config, session store, engine snapshot and
// project intelligence — no new state here); the quick actions map 1:1 to
// POST /api/workspace/switch, /api/workspace/reveal and
// /api/repo/index/refresh. The store stays server-owned.
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

Rectangle {
    id: root
    objectName: "workspaceView"
    color: Theme.background

    Component.onCompleted: {
        if (Core.connected) { Core.refreshWorkspace(); Core.refreshRepoIndex() }
    }
    Connections {
        target: Core
        function onConnectedChanged() {
            if (Core.connected) { Core.refreshWorkspace(); Core.refreshRepoIndex() }
        }
    }

    ScrollView {
        anchors.fill: parent
        clip: true

        ColumnLayout {
            width: root.width - 48
            x: 24
            spacing: Theme.spaceM

            Label {
                text: qsTr("WORK ENVIRONMENT")
                color: Theme.accent
                font.pixelSize: Theme.fontXS
                font.bold: true
                Layout.topMargin: Theme.spaceL
            }
            Label {
                text: qsTr("Project, sessions, runtime state, quick actions")
                color: Theme.text
                font.pixelSize: Theme.fontXL
                font.bold: true
            }

            // --- Current project -------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: projCol.implicitHeight + 24
                radius: Theme.radiusM
                color: projHover.hovered ? Theme.surfaceAlt : Theme.surfaceElevated
                // v1.10.15-fixed R3/R4 (D32/I22): cards answer the pointer
                // with a gated highlight step.
                HoverHandler { id: projHover }
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: projCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Current project")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.workspace.root !== undefined
                              ? Core.workspace.root
                              : qsTr("not loaded yet")
                        color: Theme.info
                        elide: Text.ElideMiddle
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.workspace.projectCard !== undefined
                                 && Core.workspace.projectCard !== null
                        text: Core.workspace.projectCard !== undefined && Core.workspace.projectCard.summary !== undefined
                              ? String(Core.workspace.projectCard.summary)
                              : ""
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }
                    RowLayout {
                        spacing: Theme.spaceS
                        TextField {
                            id: switchPath
                            Layout.preferredWidth: 360
                            placeholderText: qsTr("Switch to directory…")
                            color: Theme.text
                            selectByMouse: true
                            background: Rectangle { color: Theme.elevatedAlt; radius: Theme.radiusM; border.color: Theme.controlHover }
                        }
                        Button {
                            text: qsTr("Switch")
                            enabled: switchPath.text.trim() !== ""
                            onClicked: Core.switchWorkspace(switchPath.text.trim())
                        }
                        Button {
                            text: qsTr("Reveal")
                            enabled: Core.workspace.root !== undefined && Core.workspace.root !== ""
                            onClicked: Core.revealWorkspace(Core.workspace.root !== undefined ? Core.workspace.root : "")
                        }
                        Button {
                            text: qsTr("Refresh")
                            onClicked: Core.refreshWorkspace()
                        }
                    }
                }
            }

            // --- Recent files -----------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: Math.min(220, recentFilesCol.implicitHeight + 24)
                radius: Theme.radiusM
                color: recentHover.hovered ? Theme.surfaceAlt : Theme.surfaceElevated
                HoverHandler { id: recentHover }
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }
                clip: true

                ColumnLayout {
                    id: recentFilesCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Recent files")
                        color: Theme.text
                        font.bold: true
                    }
                    Repeater {
                        model: Core.workspace.recentFiles !== undefined ? Core.workspace.recentFiles : []
                        delegate: Label {
                            required property var modelData
                            // v1.10.15-fixed (R3/#43): `required property int
                            // index` dropped — Qt 6.8.2 QQuickRepeater never
                            // initializes required index on JS-array models,
                            // so every delegate aborted creation and the
                            // recents list silently rendered EMPTY (the same
                            // class as the nav-chat red gate; index is unused
                            // in this delegate).
                            Layout.fillWidth: true
                            text: (modelData.path !== undefined ? modelData.path : String(modelData))
                            color: Theme.muted
                            font.pixelSize: Theme.fontXS
                            elide: Text.ElideMiddle
                        }
                    }
                    Label {
                        visible: (Core.workspace.recentFiles !== undefined ? Core.workspace.recentFiles.length : 0) === 0
                        text: qsTr("No recent files recorded for this project.")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                    }
                }
            }

            // --- Active session + runtime ------------------------------------
            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.spaceM

                Rectangle {
                    Layout.fillWidth: true
                    implicitHeight: sessCol.implicitHeight + 24
                    radius: Theme.radiusM
                    color: Theme.surfaceElevated

                    ColumnLayout {
                        id: sessCol
                        anchors.fill: parent
                        anchors.margins: Theme.spaceM
                        spacing: Theme.spaceXS
                        Label {
                            text: qsTr("Active session")
                            color: Theme.text
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.workspace.session !== undefined && Core.workspace.session.id !== undefined
                                  ? (Core.workspace.session.title !== undefined && Core.workspace.session.title !== ""
                                     ? Core.workspace.session.title : Core.workspace.session.id)
                                  : qsTr("no session yet")
                            color: Theme.info
                            font.pixelSize: Theme.fontS
                            elide: Text.ElideRight
                        }
                        Button {
                            text: qsTr("Open in Chat")
                            onClicked: root.openChatRequested()
                        }
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    implicitHeight: runCol.implicitHeight + 24
                    radius: Theme.radiusM
                    color: Theme.surfaceElevated

                    ColumnLayout {
                        id: runCol
                        anchors.fill: parent
                        anchors.margins: Theme.spaceM
                        spacing: Theme.spaceXS
                        Label {
                            text: qsTr("Runtime state")
                            color: Theme.text
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.workspace.runtime !== undefined
                                  ? String(JSON.stringify(Core.workspace.runtime))
                                  : qsTr("engine snapshot not loaded")
                            color: Theme.muted
                            font.pixelSize: Theme.fontXS
                            wrapMode: Text.Wrap
                        }
                    }
                }
            }

            // --- Repository index (RepositoryIndexCard parity) ---------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: repoCol.implicitHeight + 24
                radius: Theme.radiusM
                color: repoHover.hovered ? Theme.surfaceAlt : Theme.surfaceElevated
                HoverHandler { id: repoHover }
                Behavior on color {
                    enabled: Core.animationsEnabled
                    ColorAnimation { duration: Theme.motionFast }
                }

                ColumnLayout {
                    id: repoCol
                    anchors.fill: parent
                    anchors.margins: Theme.spaceM
                    spacing: Theme.spaceXS

                    Label {
                        text: qsTr("Repository index")
                        color: Theme.text
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.repoIndex.files !== undefined || Core.repoIndex.status !== undefined
                              ? String(JSON.stringify(Core.repoIndex))
                              : qsTr("index status not loaded")
                        color: Theme.muted
                        font.pixelSize: Theme.fontXS
                        wrapMode: Text.Wrap
                    }
                    RowLayout {
                        spacing: Theme.spaceS
                        Button {
                            text: qsTr("Re-index")
                            enabled: Core.connected
                            onClicked: Core.refreshRepoIndexNow()
                        }
                        Button {
                            text: qsTr("Refresh status")
                            onClicked: Core.refreshRepoIndex()
                        }
                    }
                }
            }

            Item { Layout.preferredHeight: 16 }
        }
    }

    signal openChatRequested()
}
