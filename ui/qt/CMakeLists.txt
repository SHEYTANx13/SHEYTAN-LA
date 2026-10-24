cmake_minimum_required(VERSION 3.21)

project(SHEYTAN-UI VERSION 1.11.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)

# v1.10.15-fixed R2 (D36, run #47 cycle, Task ID 42): Network joins the
# declared components — coreclient.h includes <QLocalSocket> (and the test
# TU adds <QLocalServer>), both Qt6::Network headers, and BOTH targets
# compile them. The former set resolved Network only through Qml's
# transitive public dependency — an include OUTSIDE the target's visible
# contract, the exact class run #44 died of at C1083 for QuickControls2.
find_package(Qt6 6.5 REQUIRED COMPONENTS Core Gui Network Quick QuickControls2 Qml Test)

qt_standard_project_setup()

qt_add_executable(sheytan-ui
    src/main.cpp
    src/coreclient.h src/coreclient.cpp
)

qt_add_qml_module(sheytan-ui
    URI sheytan.ui
    VERSION 1.0
    QML_FILES
        qml/Main.qml
        qml/EmptyState.qml
        qml/SettingsView.qml
        qml/AgentView.qml
        qml/WorkspaceView.qml
        qml/LabView.qml
        qml/AutomationView.qml
        qml/SystemView.qml
        qml/HistoryDrawer.qml
        qml/ModelPickerDrawer.qml
        qml/MyToolsDrawer.qml
        qml/LogViewerDrawer.qml
        qml/PerfStrip.qml
        # v1.11.0 (R1a, run-#48, Task ID 43): the ONE theme token source —
        # a sheytan.ui module singleton every view (and every styled stock
        # control, via import sheytan.ui) binds instead of raw literals.
        qml/Theme.qml
    NO_RESOURCE_TARGET_PATH
)

# v1.11.0 (R1a, run-#48, Task ID 43): the theme token singleton must be
# registered AS a singleton, not a plain type — qt_add_qml_module only
# learns the marker through this source-file property.
set_source_files_properties(qml/Theme.qml PROPERTIES
    QT_QML_SINGLETON_TYPE TRUE
)

# v1.11.0 (R1b, run-#48, Task ID 43): the QuickControls2 custom style —
# embedded raw (qt_add_resources, NOT a QML module) so QQuickStyle can
# resolve it as a style directory at qrc:/sheytanstyle. main.cpp and the
# acceptance harness both call QQuickStyle::setStyle(":/sheytanstyle")
# BEFORE any engine loads. Every styled component imports sheytan.ui for
# the Theme tokens — ONE theme system, no duplicated palette. Embedded
# into BOTH targets: the acceptance harness loads the same views and must
# resolve the same style (I24 — the harness/shipped divergence class).
qt_add_resources(sheytan-ui sheytan_style
    PREFIX "/sheytanstyle"
    BASE "${CMAKE_CURRENT_SOURCE_DIR}/style"
    FILES Button.qml ToolButton.qml TextField.qml TextArea.qml
          ComboBox.qml CheckBox.qml Switch.qml Slider.qml ProgressBar.qml
          ScrollBar.qml Menu.qml MenuItem.qml ItemDelegate.qml
)
qt_add_resources(sheytan-ui-acceptance sheytan_style
    PREFIX "/sheytanstyle"
    BASE "${CMAKE_CURRENT_SOURCE_DIR}/style"
    FILES Button.qml ToolButton.qml TextField.qml TextArea.qml
          ComboBox.qml CheckBox.qml Switch.qml Slider.qml ProgressBar.qml
          ScrollBar.qml Menu.qml MenuItem.qml ItemDelegate.qml
)

target_link_libraries(sheytan-ui PRIVATE
    Qt6::Core Qt6::Gui Qt6::Network Qt6::Quick Qt6::QuickControls2 Qt6::Qml
)

# Windows: GUI subsystem (no console window), version metadata.
if(WIN32)
    set_target_properties(sheytan-ui PROPERTIES WIN32_EXECUTABLE TRUE)
endif()

# Official Qt deployment mechanism (doc.qt.io/qt-6/cmake-deployment.html):
# the QML-aware deploy script wires windeployqt/macdeployqt/qt-deploy
# including QML imports/plugins at install time.
if(QT_DEPLOY_AND_INSTALL)
    qt_generate_deploy_qml_app_script(
        TARGET sheytan-ui
        FILENAME_VARIABLE deploy_script
    )
    install(TARGETS sheytan-ui
        RUNTIME DESTINATION .
        BUNDLE DESTINATION .
    )
    install(SCRIPT ${deploy_script})
endif()

# QtTest acceptance suite: real QML UI + real Go-core interaction.
add_executable(sheytan-ui-acceptance tests/tst_acceptance.cpp src/coreclient.h src/coreclient.cpp)
target_include_directories(sheytan-ui-acceptance PRIVATE src)
# v1.10.15-fixed (R1/#44, Task ID 41): QuickControls2 joins the link line —
# tst_acceptance.cpp includes <QQuickStyle> and calls QQuickStyle::setStyle
# (both QuickControls2); the former Core/Gui/Qml/Quick/Test set died at
# MSVC error C1083 "Cannot open include file: 'QQuickStyle'" (run #44,
# win-qt-build.log) with the Linux twin — the ENTIRE parity suite was
# hosted-UNVERIFIED because acceptance never built. find_package already
# declared the component; this line makes the target link it.
# v1.10.15-fixed R2 (D36, run #47 cycle): Qt6::Network joins the link line
# for the same include-contract reason (QLocalSocket/QLocalServer are
# Network headers compiled into this target); the QuickControls2 line
# below it is the run-#44 C1083 fix and stays.
target_link_libraries(sheytan-ui-acceptance PRIVATE Qt6::Core Qt6::Gui Qt6::Network Qt6::Qml Qt6::Quick Qt6::QuickControls2 Qt6::Test)
