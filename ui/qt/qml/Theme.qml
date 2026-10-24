// Theme.qml — the ONE theme token source (run-#48 R1a).
//
// A sheytan.ui module singleton: every QML file in the module sees it
// through the implicit module import — no per-file import churn. The
// QuickControls2 custom style (ui/qt/style/) imports sheytan.ui to bind
// the same tokens, so stock controls and authored chrome share ONE
// palette, ONE spacing scale, ONE typography scale, ONE corner-radius
// scale (no control ships a 90-degree corner) and ONE motion duration
// set. Every authored motion still reads Core.animationsEnabled (I22) —
// these durations only size the gated animations.
pragma Singleton
import QtQuick

QtObject {
    // --- palette ---
    readonly property color background: "#121214"
    readonly property color backgroundAlt: "#141416"
    readonly property color surface: "#17171a"
    readonly property color surfaceElevated: "#1a1a1d"
    readonly property color surfaceAlt: "#1e1e22"
    readonly property color elevated: "#1f1f23"
    readonly property color elevatedAlt: "#222226"
    readonly property color control: "#2c2c31"
    readonly property color controlHover: "#33333a"
    readonly property color border: "#2a2a2e"
    readonly property color borderStrong: "#4a4a52"

    readonly property color text: "#e8e8ea"
    readonly property color textSoft: "#b8b8c0"
    readonly property color muted: "#8a8a92"
    readonly property color mutedStrong: "#5a5a62"

    readonly property color accent: "#b8a6e8"
    readonly property color accentHover: "#c9bbf0"
    readonly property color accentPress: "#a08ad8"
    readonly property color accentDim: "#2a2440"
    readonly property color accentText: "#1a1524"

    readonly property color success: "#7dd3a0"
    readonly property color successDim: "#243a2c"
    readonly property color error: "#e8a0a0"
    readonly property color errorDim: "#5a1f22"
    readonly property color errorBg: "#ffd9da"
    readonly property color warn: "#e8c268"
    readonly property color warnDim: "#3a3324"
    readonly property color info: "#8fb8dd"
    readonly property color infoDim: "#26415a"
    readonly property color infoDimAlt: "#1f2a3a"

    readonly property color overlay: "#cc000000"

    // --- spacing scale ---
    readonly property int spaceXS: 4
    readonly property int spaceS: 8
    readonly property int spaceM: 12
    readonly property int spaceL: 16
    readonly property int spaceXL: 24

    // --- typography scale ---
    readonly property int fontXS: 11
    readonly property int fontS: 12
    readonly property int fontM: 13
    readonly property int fontL: 15
    readonly property int fontXL: 18

    // --- corner-radius scale (no 90-degree corner anywhere) ---
    readonly property int radiusS: 4
    readonly property int radiusM: 6
    readonly property int radiusL: 10

    // --- motion durations (every authored use reads Core.animationsEnabled) ---
    readonly property int motionFast: 120
    readonly property int motionNormal: 200
}
