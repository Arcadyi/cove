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

# libmpv — pkg-config on Linux/macOS; manual SDK path on Windows.
# On Windows, MPV_SDK must point to the extracted shinchiro mpv-dev directory
# (CI generates mpv.lib from the DLL via dumpbin+lib.exe before this runs).
if(WIN32)
    find_path(MPV_INCLUDE_DIR mpv/client.h
        HINTS "$ENV{MPV_SDK}/include" "$ENV{MPV_SDK}"
        REQUIRED)
    find_library(MPV_LIBRARY NAMES mpv libmpv
        HINTS "$ENV{MPV_SDK}" "$ENV{MPV_SDK}/lib"
        REQUIRED)
    set(MPV_LINK_TARGET "${MPV_LIBRARY}")
    set(MPV_INCLUDE_TARGET "${MPV_INCLUDE_DIR}")
else()
    find_package(PkgConfig REQUIRED)
    pkg_check_modules(MPV REQUIRED IMPORTED_TARGET mpv)
    set(MPV_LINK_TARGET PkgConfig::MPV)
endif()

# WIN32 suppresses the console window on Windows; no effect on other platforms.
add_executable(cove_shell WIN32
  src/main.cpp
  src/MpvObject.cpp
  src/MpvObject.h
  resources.qrc
)

if(WIN32)
    target_include_directories(cove_shell PRIVATE "${MPV_INCLUDE_TARGET}")
endif()

target_link_libraries(cove_shell PRIVATE
  Qt6::Core
  Qt6::Gui
  Qt6::Qml
  Qt6::Quick
  Qt6::WebEngineQuick
  Qt6::WebEngineCore
  Qt6::WebChannel
  Qt6::OpenGL
  ${MPV_LINK_TARGET}
)
