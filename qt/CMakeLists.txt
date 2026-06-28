cmake_minimum_required(VERSION 3.16)
project(cove_shell LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

# Quick/Qml for the scene, WebEngineQuick for the QML WebEngineView, OpenGL for
# the GL context plumbing mpv needs, WebChannel for the qwebchannel.js resource.
# Note: WebChannel (not WebChannelQuick) is sufficient — the bridge JS is
# injected via QWebEngineScript; no QML-side WebChannel types are used.
find_package(Qt6 REQUIRED COMPONENTS
  Core Gui Qml Quick WebEngineQuick WebEngineCore WebChannel OpenGL
)

# libmpv via pkg-config (Arch ships headers + mpv.pc in the `mpv` package).
find_package(PkgConfig REQUIRED)
pkg_check_modules(MPV REQUIRED IMPORTED_TARGET mpv)

add_executable(cove_shell
  src/main.cpp
  src/MpvObject.cpp
  src/MpvObject.h
  resources.qrc
)

target_link_libraries(cove_shell PRIVATE
  Qt6::Core
  Qt6::Gui
  Qt6::Qml
  Qt6::Quick
  Qt6::WebEngineQuick
  Qt6::WebEngineCore
  Qt6::WebChannel
  Qt6::OpenGL
  PkgConfig::MPV
)
