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

    Connections {
        target: mpv
        function onFullscreenRequested(fs) {
            if (fs) {
                win.showFullScreen();
            } else if (win.visibility === Window.FullScreen) {
                win.showNormal();
            }
        }
    }

    WebEngineView {
        id: web
        anchors.fill: parent
        backgroundColor: "transparent"
        webChannel: coveChannel
        url: launchUrl

        // Open window.open() links (e.g. JustWatch provider pages) in the
        // system browser rather than a new Qt window.
        onNewWindowRequested: function(request) {
            Qt.openUrlExternally(request.requestedUrl)
            request.action = WebEngineNewWindowRequest.IgnoreRequest
        }

        // Forward JS console output to the Qt process stdout so it's visible
        // in the terminal alongside Go backend logs.
        onJavaScriptConsoleMessage: function(level, message, lineNumber, sourceID) {
            // Drop console.trace / console.groupEnd lines — pure noise.
            if (message === "console.trace" || message === "console.groupEnd") return
            // Strip %c CSS-format arguments that Qt concatenates into the message
            // string (e.g. styled console.group output from Vidstack). This turns
            // "%cERROR%c ... background: hsl(...); color: white; ..." into
            // "ERROR ... [vidstack] ..." so it's readable in the terminal.
            var out = message
            if (out.indexOf("%c") !== -1) {
                out = out.replace(/%c/g, "")
                // Pass 1: remove semicolon-terminated CSS blocks (e.g. "background: hsl(...); color: white;")
                out = out.replace(/\s*(?:background|color|padding(?:-\w+)?|font-size|border(?:-\w+)?|font-weight):[^;]+;/g, "")
                // Pass 2: remove bare "color: word" args that have no semicolon
                // (Qt appends each console.log arg with a space, so these appear as "color: gray Value")
                out = out.replace(/\s*color:\s+\w+(?=\s|$)/g, "")
                out = out.replace(/\s+/g, " ").trim()
            }
            if (!out) return
            var prefix = "[js] "
            if (level === 1) prefix = "[js:warn] "
            else if (level >= 2) prefix = "[js:err] "
            console.log(prefix + out)
        }
    }
}
