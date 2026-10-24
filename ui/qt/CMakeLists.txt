cmake_minimum_required(VERSION 3.21)

project(SHEYTAN-UI VERSION 1.10.13 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)

find_package(Qt6 6.5 REQUIRED COMPONENTS Core Gui Quick QuickControls2 Qml Test)

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
    NO_RESOURCE_TARGET_PATH
)

target_link_libraries(sheytan-ui PRIVATE
    Qt6::Core Qt6::Gui Qt6::Quick Qt6::QuickControls2 Qt6::Qml
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
target_link_libraries(sheytan-ui-acceptance PRIVATE Qt6::Core Qt6::Gui Qt6::Qml Qt6::Quick Qt6::QuickControls2 Qt6::Test)
