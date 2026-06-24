import QtQuick
import QtQuick.Window
import QtWebEngine
import QtWebChannel
import mpv

// Single Quick scene: mpv FBO at the back, transparent WebEngineView on top.
// They share one scene graph and the web layer background is transparent, so
// the web UI composites over the video (what Electron cannot do).
Window {
    id: win
    visible: true
    width: 1366
    height: 850
    title: "Cove"
    color: "black"

    MpvObject {
        id: mpv
        anchors.fill: parent

        // Identifier the JS side uses: channel.objects.mpv
        WebChannel.id: "mpv"

        Component.onCompleted: {
            if (typeof mpvTestFile !== "undefined" && mpvTestFile.length > 0)
                mpv.play(mpvTestFile)
        }
    }

    // Bridge: exposes mpv's slots/signals to the web layer. QtWebEngine injects
    // qt.webChannelTransport into the page once this channel is set on the view.
    WebChannel {
        id: coveChannel
        registeredObjects: [mpv]
    }

    // Step 1 verification: log mpv state changes (position omitted — too noisy).
    Connections {
        target: mpv
        function onFileLoaded() { console.log("[mpv] file loaded") }
        function onDurationChanged(d) { console.log("[mpv] duration:", d.toFixed(2), "s") }
        function onPausedChanged(p) { console.log("[mpv] paused:", p) }
        function onVolumeChanged(v) { console.log("[mpv] volume:", v) }
        function onEndReached() { console.log("[mpv] end reached") }
        function onTracksChanged(tracks) { console.log("[mpv] tracks:", JSON.stringify(tracks)) }
    }

    WebEngineView {
        id: web
        anchors.fill: parent
        backgroundColor: "transparent"
        webChannel: coveChannel
        url: launchUrl
        // window.QWebChannel is injected from C++ (installBridgeScript) onto the
        // default profile this view uses — see src/main.cpp.
    }
}
