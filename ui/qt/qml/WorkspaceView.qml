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
    color: "#121214"

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
            spacing: 12

            Label {
                text: qsTr("WORK ENVIRONMENT")
                color: "#b8a6e8"
                font.pixelSize: 11
                font.bold: true
                Layout.topMargin: 16
            }
            Label {
                text: qsTr("Project, sessions, runtime state, quick actions")
                color: "#e8e8ea"
                font.pixelSize: 18
                font.bold: true
            }

            // --- Current project -------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: projCol.implicitHeight + 24
                radius: 8
                color: "#1a1a1d"

                ColumnLayout {
                    id: projCol
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    Label {
                        text: qsTr("Current project")
                        color: "#e8e8ea"
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.workspace.root !== undefined
                              ? Core.workspace.root
                              : qsTr("not loaded yet")
                        color: "#8fb8dd"
                        elide: Text.ElideMiddle
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: Core.workspace.projectCard !== undefined
                                 && Core.workspace.projectCard !== null
                        text: Core.workspace.projectCard !== undefined && Core.workspace.projectCard.summary !== undefined
                              ? String(Core.workspace.projectCard.summary)
                              : ""
                        color: "#8a8a92"
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }
                    RowLayout {
                        spacing: 8
                        TextField {
                            id: switchPath
                            Layout.preferredWidth: 360
                            placeholderText: qsTr("Switch to directory…")
                            color: "#e8e8ea"
                            selectByMouse: true
                            // v1.10.15-fixed R3 (D32): focus feedback + gated
                            // motion (run-#43 pin; Basic style).
                            background: Rectangle {
                                color: "#222226"
                                radius: 6
                                border.color: parent.activeFocus ? "#b8a6e8" : "#33333a"
                                Behavior on border.color {
                                    enabled: Core.animationsEnabled
                                    ColorAnimation { duration: 140 }
                                }
                            }
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
                radius: 8
                color: "#1a1a1d"
                clip: true

                ColumnLayout {
                    id: recentFilesCol
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    Label {
                        text: qsTr("Recent files")
                        color: "#e8e8ea"
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
                            color: "#8a8a92"
                            font.pixelSize: 11
                            elide: Text.ElideMiddle
                        }
                    }
                    Label {
                        visible: (Core.workspace.recentFiles !== undefined ? Core.workspace.recentFiles.length : 0) === 0
                        text: qsTr("No recent files recorded for this project.")
                        color: "#8a8a92"
                        font.pixelSize: 11
                    }
                }
            }

            // --- Active session + runtime ------------------------------------
            RowLayout {
                Layout.fillWidth: true
                spacing: 12

                Rectangle {
                    Layout.fillWidth: true
                    implicitHeight: sessCol.implicitHeight + 24
                    radius: 8
                    color: "#1a1a1d"

                    ColumnLayout {
                        id: sessCol
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 4
                        Label {
                            text: qsTr("Active session")
                            color: "#e8e8ea"
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.workspace.session !== undefined && Core.workspace.session.id !== undefined
                                  ? (Core.workspace.session.title !== undefined && Core.workspace.session.title !== ""
                                     ? Core.workspace.session.title : Core.workspace.session.id)
                                  : qsTr("no session yet")
                            color: "#8fb8dd"
                            font.pixelSize: 12
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
                    radius: 8
                    color: "#1a1a1d"

                    ColumnLayout {
                        id: runCol
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 4
                        Label {
                            text: qsTr("Runtime state")
                            color: "#e8e8ea"
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: Core.workspace.runtime !== undefined
                                  ? String(JSON.stringify(Core.workspace.runtime))
                                  : qsTr("engine snapshot not loaded")
                            color: "#8a8a92"
                            font.pixelSize: 11
                            wrapMode: Text.Wrap
                        }
                    }
                }
            }

            // --- Repository index (RepositoryIndexCard parity) ---------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: repoCol.implicitHeight + 24
                radius: 8
                color: "#1a1a1d"

                ColumnLayout {
                    id: repoCol
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    Label {
                        text: qsTr("Repository index")
                        color: "#e8e8ea"
                        font.bold: true
                    }
                    Label {
                        Layout.fillWidth: true
                        text: Core.repoIndex.files !== undefined || Core.repoIndex.status !== undefined
                              ? String(JSON.stringify(Core.repoIndex))
                              : qsTr("index status not loaded")
                        color: "#8a8a92"
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }
                    RowLayout {
                        spacing: 8
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
