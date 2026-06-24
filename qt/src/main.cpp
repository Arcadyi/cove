// Cove — Qt/QtWebEngine shell, Phase 2a (mpv compositing proof).
//
// Builds on Phase 1 (spawn Go backend, serve renderer over http) but moves the
// UI into a QML scene so an mpv video surface can sit BEHIND a transparent
// WebEngineView and the web UI can draw on top — the in-window playback Electron
// couldn't do.
//
// Two modes:
//   ./cove_shell                 → loads the real app (mpv idle, looks like P1)
//   ./cove_shell --play <file>   → loads a translucent test overlay and plays
//                                  <file> in mpv, so you can confirm the video
//                                  shows through with HTML on top.

#include <QCommandLineParser>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QGuiApplication>
#include <QHostAddress>
#include <QMimeDatabase>
#include <QProcess>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QSurfaceFormat>
#include <QTcpServer>
#include <QTcpSocket>
#include <QTimer>
#include <QUrl>
#include <QtWebEngineQuick/QtWebEngineQuick>
#include <QtWebEngineCore/QWebEngineProfile>
#include <QtWebEngineCore/QWebEngineScript>
#include <QtWebEngineCore/QWebEngineScriptCollection>
#include <functional>
#include <memory>

#include "MpvObject.h"

// ── Tiny static file server (unchanged from Phase 1) ─────────────────────────
class StaticServer : public QTcpServer {
public:
  explicit StaticServer(const QString &root, QObject *parent = nullptr)
      : QTcpServer(parent), m_root(QDir(root).absolutePath()) {}

  QUrl start() {
    if (!listen(QHostAddress::LocalHost, 0)) {
      qWarning() << "[shell] static server failed to listen:" << errorString();
      return {};
    }
    return QUrl(QStringLiteral("http://127.0.0.1:%1/").arg(serverPort()));
  }

protected:
  void incomingConnection(qintptr handle) override {
    auto *sock = new QTcpSocket(this);
    sock->setSocketDescriptor(handle);
    auto buffer = std::make_shared<QByteArray>();
    connect(sock, &QTcpSocket::readyRead, this, [this, sock, buffer]() {
      buffer->append(sock->readAll());
      if (buffer->indexOf("\r\n\r\n") < 0)
        return;
      serve(sock, *buffer);
    });
    connect(sock, &QTcpSocket::disconnected, sock, &QObject::deleteLater);
  }

private:
  void serve(QTcpSocket *sock, const QByteArray &request) {
    const QByteArray firstLine = request.left(request.indexOf("\r\n"));
    const QList<QByteArray> tokens = firstLine.split(' ');
    QString path = tokens.size() >= 2 ? QString::fromUtf8(tokens[1]) : "/";
    path = QUrl(path).path();
    if (path.isEmpty() || path == "/")
      path = "/index.html";

    QString filePath =
        QFileInfo(QDir(m_root).filePath(path.mid(1))).absoluteFilePath();
    if (filePath != m_root && !filePath.startsWith(m_root + "/")) {
      respond(sock, 403, "text/plain", "Forbidden");
      return;
    }

    QFileInfo info(filePath);
    if (!info.exists() || info.isDir()) {
      if (QFileInfo(path).suffix().isEmpty())
        filePath = QDir(m_root).filePath("index.html");
      else {
        respond(sock, 404, "text/plain", "Not found");
        return;
      }
    }

    QFile file(filePath);
    if (!file.open(QIODevice::ReadOnly)) {
      respond(sock, 500, "text/plain", "Read error");
      return;
    }
    respond(sock, 200, mimeFor(filePath), file.readAll());
  }

  static QByteArray mimeFor(const QString &filePath) {
    const QString ext = QFileInfo(filePath).suffix().toLower();
    if (ext == "js" || ext == "mjs")
      return "text/javascript; charset=utf-8";
    if (ext == "css")
      return "text/css; charset=utf-8";
    if (ext == "html")
      return "text/html; charset=utf-8";
    if (ext == "json" || ext == "map")
      return "application/json; charset=utf-8";
    if (ext == "wasm")
      return "application/wasm";
    return QMimeDatabase().mimeTypeForFile(filePath).name().toUtf8();
  }

  void respond(QTcpSocket *sock, int code, const QByteArray &mime,
               const QByteArray &body) {
    QByteArray resp;
    resp += "HTTP/1.1 " + QByteArray::number(code) + " OK\r\n";
    resp += "Content-Type: " + mime + "\r\n";
    resp += "Content-Length: " + QByteArray::number(body.size()) + "\r\n";
    resp += "Cache-Control: no-cache\r\n";
    resp += "Connection: close\r\n\r\n";
    resp += body;
    sock->write(resp);
    sock->disconnectFromHost();
  }

  QString m_root;
};

// ── Backend (Go sidecar) ─────────────────────────────────────────────────────
static QProcess *startBackend(const QString &exePath, QObject *parent) {
  auto *proc = new QProcess(parent);
  proc->setProcessChannelMode(QProcess::MergedChannels);
  QObject::connect(proc, &QProcess::readyReadStandardOutput, proc, [proc]() {
    const QString out = QString::fromUtf8(proc->readAllStandardOutput());
    for (const QString &line : out.split('\n', Qt::SkipEmptyParts))
      qInfo().noquote() << "[go]" << line;
  });
  proc->start(exePath, {});
  return proc;
}

static void waitForBackend(quint16 port, std::function<void()> onReady) {
  auto *timer = new QTimer;
  timer->setInterval(150);
  QObject::connect(timer, &QTimer::timeout, timer, [timer, port, onReady]() {
    auto *probe = new QTcpSocket;
    QObject::connect(probe, &QTcpSocket::connected, probe,
                     [timer, probe, onReady]() {
                       timer->stop();
                       timer->deleteLater();
                       probe->abort();
                       probe->deleteLater();
                       onReady();
                     });
    QObject::connect(probe, &QTcpSocket::errorOccurred, probe,
                     [probe]() { probe->deleteLater(); });
    probe->connectToHost(QHostAddress::LocalHost, port);
  });
  timer->start();
}

// Qt ships qwebchannel.js as a compiled-in resource of the WebChannel module.
static QString readQWebChannelJs() {
  QFile f(QStringLiteral(":/qtwebchannel/qwebchannel.js"));
  if (!f.open(QIODevice::ReadOnly)) {
    qWarning() << "[shell] qwebchannel.js resource missing; bridge unavailable";
    return {};
  }
  return QString::fromUtf8(f.readAll());
}

// Inject qwebchannel.js into every page at document creation so window.QWebChannel
// exists before the app's JS runs. Installed on the default profile (which the
// QML WebEngineView uses) BEFORE the engine loads, so it covers the first
// navigation too. WebEngineScript isn't creatable from QML in Qt 6, so this is
// done here where QWebEngineScript is a proper value type.
static void installBridgeScript() {
  const QString src = readQWebChannelJs();
  if (src.isEmpty())
    return;
  QWebEngineScript script;
  script.setName(QStringLiteral("qwebchannel"));
  script.setSourceCode(src);
  script.setInjectionPoint(QWebEngineScript::DocumentCreation);
  script.setWorldId(QWebEngineScript::MainWorld);
  script.setRunsOnSubFrames(false);
  QWebEngineProfile::defaultProfile()->scripts()->insert(script);
}

// Translucent test overlay written to a temp file. Beyond the compositing proof
// (transparent HTML over video), it also exercises the QWebChannel bridge: it
// connects to the registered `mpv` object (QWebChannel is provided globally by
// the injected user script — see main()), shows live position/duration/track
// data pushed from C++ signals, and calls mpv.pause()/resume() on a timer so you
// can watch JS drive native playback.
static QString testOverlayUrl() {
  // The bridge bootstrap: connect, wire signals to the DOM, and exercise slots.
  const QString bootstrap = QStringLiteral(R"JS(
new QWebChannel(qt.webChannelTransport, function (channel) {
  var mpv = channel.objects.mpv;
  window.mpv = mpv; // handy for poking from devtools
  var byId = function (id) { return document.getElementById(id); };
  byId('bridge').textContent = 'bridge: connected';
  mpv.positionChanged.connect(function (p) {
    byId('pos').textContent = 'position: ' + p.toFixed(1) + 's';
  });
  mpv.durationChanged.connect(function (d) {
    byId('dur').textContent = 'duration: ' + d.toFixed(1) + 's';
  });
  mpv.tracksChanged.connect(function (tracks) {
    byId('trk').textContent = 'tracks: ' + JSON.stringify(tracks);
  });
  mpv.pausedChanged.connect(function (paused) {
    byId('act').textContent = paused ? 'paused (by JS)' : 'playing';
  });
  // Prove JS->C++ slot calls move native playback:
  setTimeout(function () { mpv.pause(); }, 4000);
  setTimeout(function () { mpv.resume(); }, 7000);
});
)JS");

  QString html;
  html += "<!doctype html><html><head><meta charset=\"utf-8\"><style>";
  html += "html,body{margin:0;height:100%;background:transparent;"
          "font-family:sans-serif;color:#fff}";
  html += ".tag{position:fixed;top:20px;left:20px;background:rgba(0,150,60,.85);"
          "padding:8px 14px;border-radius:8px;font-size:18px}";
  html += ".bar{position:fixed;left:0;right:0;bottom:0;padding:18px;"
          "background:rgba(0,0,0,.6);font-size:16px;line-height:1.6}";
  html += "</style></head><body>";
  html += "<div class=\"tag\">HTML overlay &mdash; on top</div>";
  html += "<div class=\"bar\">";
  html += "<div>If video is visible behind this bar, mpv is compositing under "
          "the transparent WebEngine.</div>";
  html += "<div id=\"bridge\">bridge: connecting&hellip;</div>";
  html += "<div id=\"pos\">position: &mdash;</div>";
  html += "<div id=\"dur\">duration: &mdash;</div>";
  html += "<div id=\"trk\">tracks: &mdash;</div>";
  html += "<div id=\"act\">playing</div>";
  html += "</div>";
  html += "<script>" + bootstrap + "</script>";
  html += "</body></html>";

  const QString path = QDir::temp().filePath("cove_overlay.html");
  QFile f(path);
  if (f.open(QIODevice::WriteOnly | QIODevice::Truncate)) {
    f.write(html.toUtf8());
    f.close();
  }
  return QUrl::fromLocalFile(path).toString();
}

int main(int argc, char *argv[]) {
  // Required before the app: share GL contexts, force Quick onto the OpenGL RHI
  // (mpv renders via OpenGL), and give the default surface an alpha channel so
  // the transparent web layer can composite.
  QCoreApplication::setAttribute(Qt::AA_ShareOpenGLContexts);
  QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);
  QSurfaceFormat fmt = QSurfaceFormat::defaultFormat();
  fmt.setAlphaBufferSize(8);
  QSurfaceFormat::setDefaultFormat(fmt);

  QtWebEngineQuick::initialize();
  QGuiApplication app(argc, argv);
  app.setApplicationName("cove");
  app.setOrganizationName("arcadyi");

  qmlRegisterType<MpvObject>("mpv", 1, 0, "MpvObject");

  QCommandLineParser parser;
  parser.setApplicationDescription("Cove Qt shell (Phase 2a)");
  parser.addHelpOption();
  QCommandLineOption backendOpt("backend", "Path to the Go sidecar binary.",
                                "path", "../../cove");
  QCommandLineOption webrootOpt("webroot", "Path to the renderer build dir.",
                                "path", "../../web/dist");
  QCommandLineOption apiPortOpt("api-port", "Backend API port.", "port", "6969");
  QCommandLineOption playOpt(
      "play", "Compositing test: play this media file behind a test overlay.",
      "file");
  parser.addOption(backendOpt);
  parser.addOption(webrootOpt);
  parser.addOption(apiPortOpt);
  parser.addOption(playOpt);
  parser.process(app);

  const QString backendPath =
      QFileInfo(parser.value(backendOpt)).absoluteFilePath();
  const QString webRoot = QFileInfo(parser.value(webrootOpt)).absoluteFilePath();
  const quint16 apiPort = parser.value(apiPortOpt).toUShort();
  const QString testFile =
      parser.isSet(playOpt)
          ? QFileInfo(parser.value(playOpt)).absoluteFilePath()
          : QString();

  QQmlApplicationEngine engine;

  // Must run before the WebEngineView navigates so the script covers the first
  // load (the QML view uses the default profile this installs onto).
  installBridgeScript();

  auto loadScene = [&](const QString &url, const QString &mpvFile) {
    engine.rootContext()->setContextProperty("launchUrl", url);
    engine.rootContext()->setContextProperty("mpvTestFile", mpvFile);
    engine.load(QUrl("qrc:/qml/main.qml"));
  };

  if (!testFile.isEmpty()) {
    // Compositing proof — no backend needed for a local file.
    qInfo().noquote() << "[shell] compositing test, playing:" << testFile;
    loadScene(testOverlayUrl(), testFile);
  } else {
    qInfo().noquote() << "[shell] backend:" << backendPath
                      << (QFileInfo::exists(backendPath) ? "(ok)" : "(MISSING)");
    qInfo().noquote() << "[shell] webroot:" << webRoot;

    auto *server = new StaticServer(webRoot, &app);
    const QUrl baseUrl = server->start();
    if (baseUrl.isEmpty())
      return 1;
    qInfo().noquote() << "[shell] serving renderer at" << baseUrl.toString();

    QProcess *backend = startBackend(backendPath, &app);
    QObject::connect(&app, &QCoreApplication::aboutToQuit, [backend]() {
      if (backend->state() == QProcess::NotRunning)
        return;
      backend->terminate();
      if (!backend->waitForFinished(2000))
        backend->kill();
    });

    waitForBackend(apiPort, [loadScene, baseUrl]() {
      qInfo().noquote() << "[shell] backend up — loading UI";
      loadScene(baseUrl.toString(), QString());
    });
  }

  return app.exec();
}
