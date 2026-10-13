// Main.qml — the SHEYTAN Qt Quick desktop UI.
//
// IPC CLIENT of the ONE Go authority: sessions, runs, streaming and
// persistence all come from the existing core API over the local channel.
// Surfaces implemented here: session list (create/open/delete), chat
// transcript with per-message roles, live streaming bubble (cumulative
// text), Stop while streaming, composer lock/unlock, settlement →
// persistence reload, error/empty/loading states, model/backend settings,
// and the AI Systems panel (v1.10.2: list + activation over /api/systems
// — the store's validation, persistence and reserved default stay
// server-owned; this view holds no second authority).
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import sheytan.ui

ApplicationWindow {
    id: root

    property string ipcEndpoint: ""
    property string ipcKey: ""

    width: 1280
    height: 840
    minimumWidth: 1024
    minimumHeight: 680
    visible: true
    title: qsTr("SHEYTAN Local Agent") + (Core.connected && Core.coreVersion !== "" ? "  ·  v" + Core.coreVersion : "")
    color: "#121214"

    // AI Systems panel (v1.10.2): right-edge drawer, pure /api/systems client.
    Drawer {
        id: systemsDrawer
        edge: Qt.RightEdge
        width: 380
        height: root.height
        onOpened: Core.refreshSystems()

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 14
            spacing: 10

            RowLayout {
                Layout.fillWidth: true
                Label {
                    text: qsTr("AI Systems")
                    color: "#e8e8ea"
                    font.pixelSize: 16
                    font.bold: true
                    Layout.fillWidth: true
                }
                Button {
                    text: qsTr("Refresh")
                    display: AbstractButton.TextOnly
                    onClicked: Core.refreshSystems()
                }
            }

            Label {
                Layout.fillWidth: true
                text: Core.activeSystemId !== ""
                      ? qsTr("Active: ") + Core.activeSystemId
                      : qsTr("No active system")
                color: "#8fb8dd"
                font.pixelSize: 12
                elide: Text.ElideRight
            }

            ListView {
                id: systemsList
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 8
                model: Core.systems
                ScrollBar.vertical: ScrollBar {}

                delegate: Rectangle {
                    required property var modelData
                    width: systemsList.width
                    height: sysCol.implicitHeight + 20
                    radius: 8
                    color: Core.activeSystemId === modelData.systemId ? "#243a2c" : "#222226"

                    ColumnLayout {
                        id: sysCol
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: 4

                        RowLayout {
                            Layout.fillWidth: true
                            Label {
                                text: modelData.name !== undefined ? modelData.name : modelData.systemId
                                color: "#e8e8ea"
                                font.bold: true
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                text: Core.activeSystemId === modelData.systemId ? qsTr("ACTIVE") : ""
                                color: "#7dd3a0"
                                font.pixelSize: 10
                                font.bold: true
                            }
                        }
                        Label {
                            Layout.fillWidth: true
                            text: modelData.systemId + "  ·  r" + (modelData.revision !== undefined ? modelData.revision : 0)
                            color: "#8a8a92"
                            font.pixelSize: 11
                            elide: Text.ElideRight
                        }
                        Button {
                            visible: Core.activeSystemId !== modelData.systemId
                            text: qsTr("Activate")
                            onClicked: Core.activateSystem(modelData.systemId)
                        }
                    }
                }

                EmptyState {
                    anchors.centerIn: parent
                    visible: Core.systems.length === 0
                    icon: "◇"
                    title: Core.connected ? qsTr("No AI Systems") : qsTr("Not connected")
                    detail: Core.connected
                        ? qsTr("The core store lists no systems.")
                        : qsTr("Connect to the core to list AI Systems.")
                }
            }
        }
    }

    function connect() {
        if (ipcEndpoint !== "") Core.connectToCore(ipcEndpoint, ipcKey)
    }

    Component.onCompleted: root.connect()

    // Accept the launcher's properties set after construction.
    onIpcEndpointChanged: root.connect()
    onIpcKeyChanged: root.connect()

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // --- Sidebar: sessions ------------------------------------------
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 280
            color: "#1a1a1d"

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 10

                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: qsTr("Sessions")
                        color: "#e8e8ea"
                        font.pixelSize: 16
                        font.bold: true
                        Layout.fillWidth: true
                    }
                    Button {
                        text: qsTr("+ New")
                        display: AbstractButton.TextOnly
                        onClicked: Core.newSession("")
                    }
                }

                ListView {
                    id: sessionList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    model: Core.sessions

                    ScrollBar.vertical: ScrollBar {}

                    delegate: ItemDelegate {
                        required property var modelData
                        width: sessionList.width
                        height: 56

                        contentItem: ColumnLayout {
                            spacing: 2
                            Label {
                                text: modelData.title !== undefined && modelData.title !== "" ? modelData.title : qsTr("Untitled")
                                color: "#e8e8ea"
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                text: (modelData.messageCount !== undefined ? modelData.messageCount : 0) + qsTr(" messages")
                                color: "#8a8a92"
                                font.pixelSize: 11
                            }
                        }

                        background: Rectangle {
                            color: Core.currentSession.id === modelData.id
                                   ? "#2c2c31" : (parent.hovered ? "#222226" : "transparent")
                            radius: 6
                        }

                        onClicked: Core.openSession(modelData.id)
                    }

                    EmptyState {
                        anchors.centerIn: parent
                        visible: Core.sessions.length === 0
                        icon: "◇"
                        title: qsTr("No sessions yet")
                        detail: qsTr("Create a session to start chatting.")
                    }
                }

                Button {
                    Layout.fillWidth: true
                    text: qsTr("AI Systems")
                    enabled: Core.connected
                    onClicked: systemsDrawer.open()
                }

                Button {
                    Layout.fillWidth: true
                    text: qsTr("Delete current session")
                    enabled: Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                    onClicked: Core.deleteSession(Core.currentSession.id)
                }
            }
        }

        Rectangle { Layout.fillHeight: true; Layout.preferredWidth: 1; color: "#2a2a2e" }

        // --- Main chat column -------------------------------------------
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Loading / error banner
            Rectangle {
                Layout.fillWidth: true
                visible: Core.error !== ""
                color: "#5a1f22"
                Layout.preferredHeight: 34
                Label {
                    anchors.centerIn: parent
                    text: Core.error
                    color: "#ffd9da"
                }
            }

            // Transcript
            ListView {
                id: transcript
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 10
                model: Core.messages
                ScrollBar.vertical: ScrollBar {}

                onCountChanged: Qt.callLater(positionAtEnd)
                function positionAtEnd() { positionViewAtEnd() }

                delegate: Rectangle {
                    required property var modelData
                    required property int index
                    Layout.fillWidth: true
                    width: transcript.width - 48
                    x: 24
                    height: bubbleText.implicitHeight + 28
                    radius: 8
                    color: modelData.role === "user" ? "#26415a" : "#222226"

                    ColumnLayout {
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 4

                        Label {
                            text: modelData.role === "user" ? qsTr("You") : qsTr("SHEYTAN")
                            color: modelData.role === "user" ? "#8fb8dd" : "#b8a6e8"
                            font.pixelSize: 11
                            font.bold: true
                        }
                        TextEdit {
                            id: bubbleText
                            text: modelData.content
                            readOnly: true
                            wrapMode: TextEdit.Wrap
                            color: "#e8e8ea"
                            Layout.fillWidth: true
                            textFormat: TextEdit.PlainText
                            selectByMouse: true
                        }
                    }
                }

                EmptyState {
                    anchors.centerIn: parent
                    visible: Core.messages.length === 0 && !Core.liveActive
                    icon: "◈"
                    title: Core.connected ? qsTr("Say hello") : qsTr("Connecting to the core…")
                    detail: Core.connected
                        ? qsTr("Send a message — answers stream live and persist to the session.")
                        : qsTr("The desktop UI talks to the Go core over the local IPC channel.")
                }
            }

            // Live streaming bubble (cumulative text; Stop stays visible)
            Rectangle {
                Layout.fillWidth: true
                Layout.margins: 12
                visible: Core.liveActive
                implicitHeight: liveColumn.implicitHeight + 24
                radius: 8
                color: "#2a2440"

                ColumnLayout {
                    id: liveColumn
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 4

                    RowLayout {
                        Layout.fillWidth: true
                        Label {
                            text: qsTr("SHEYTAN (live)")
                            color: "#b8a6e8"
                            font.pixelSize: 11
                            font.bold: true
                            Layout.fillWidth: true
                        }
                        Button {
                            text: qsTr("Stop")
                            enabled: Core.canStop
                            onClicked: Core.stop()
                        }
                    }
                    Label {
                        text: Core.liveText === "" ? qsTr("…") : Core.liveText
                        color: "#e8e8ea"
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                }
            }

            // Composer
            Rectangle {
                Layout.fillWidth: true
                Layout.preferredHeight: composer.implicitHeight + 24
                color: "#1a1a1d"

                RowLayout {
                    id: composer
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 8

                    ScrollView {
                        Layout.fillWidth: true
                        Layout.maximumHeight: 120
                        TextArea {
                            id: input
                            placeholderText: Core.currentSession.id ? qsTr("Message…") : qsTr("Create or open a session first")
                            wrapMode: TextArea.Wrap
                            color: "#e8e8ea"
                            enabled: !Core.busy && Core.currentSession.id !== undefined && Core.currentSession.id !== ""
                            background: Rectangle {
                                color: "#222226"
                                radius: 6
                                border.color: "#33333a"
                            }
                            Keys.onReturnPressed: (event) => {
                                if (!(event.modifiers & Qt.ShiftModifier)) { sendButton.action(); event.accepted = true }
                            }
                        }
                    }

                    Button {
                        id: sendButton
                        text: qsTr("Send")
                        enabled: !Core.busy && input.text.trim() !== "" && Core.currentSession.id !== ""
                        function action() {
                            Core.send(input.text, false)
                            input.clear()
                        }
                        onClicked: action()
                    }
                }
            }
        }
    }
}
