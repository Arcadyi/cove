// Cove — Qt/QtWebEngine shell.
//
// An mpv surface sits below a transparent WebEngineView; the web UI composites
// on top for in-window playback.
//
// Two modes:
//   ./cove_shell                 → loads the real app (mpv idle)
//   ./cove_shell --play <file>   → plays <file> behind a translucent test overlay

#include <QCommandLineParser>
#include <QCoreApplication>
#include <QDir>
#include <QElapsedTimer>
#include <QFile>
#include <QFileInfo>
#include <QGuiApplication>
#include <QHostAddress>
#include <QIcon>
#include <QLockFile>
#include <QMimeDatabase>
#include <QProcess>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QStandardPaths>
#include <QSurfaceFormat>
#include <QTcpServer>
#include <QTcpSocket>
#include <QTextStream>
#include <QTimer>
#include <QUrl>
#include <QtWebEngineQuick/QtWebEngineQuick>
#include <QtWebEngineCore/QWebEngineProfile>
#include <QtWebEngineCore/QWebEngineScript>
#include <QtWebEngineCore/QWebEngineScriptCollection>
#include <functional>
#include <memory>

#ifdef Q_OS_WIN
#include <windows.h>
#endif

#ifdef Q_OS_LINUX
#include <csignal>
#include <sys/prctl.h>
#endif

#include "MpvObject.h"

// Filter noisy-but-benign Qt WebChannel warnings about QQuickItem-inherited
// properties (data, resources, states, transform, etc.) that don't have notify
// signals. WebChannel's introspection emits one per property at startup; they
// have no effect on functionality.
static QtMessageHandler s_defaultMsgHandler = nullptr;
static QFile *s_logFile = nullptr;
static QString s_logFilePath;

static void msgFilter(QtMsgType type, const QMessageLogContext &ctx,
                      const QString &msg) {
  if (type == QtWarningMsg &&
      msg.contains(QLatin1String("has no notify signal")))
    return;
  if (s_logFile && s_logFile->isOpen()) {
    QTextStream ts(s_logFile);
    ts << msg << '\n';
    ts.flush();
  }
  s_defaultMsgHandler(type, ctx, msg);
}

// Opens <config dir>/cove/shell.log, truncated each run. cove_shell is built
// WIN32-subsystem on Windows (see CMakeLists.txt) so it has no console —
// without this file, every qInfo()/qWarning() below is discarded and a
// startup failure is completely undiagnosable.
static void openLogFile() {
  const QString dir = QDir(QStandardPaths::writableLocation(
                                QStandardPaths::GenericConfigLocation))
                          .filePath("cove");
  QDir().mkpath(dir);
  s_logFilePath = QDir(dir).filePath("shell.log");
  s_logFile = new QFile(s_logFilePath);
  if (!s_logFile->open(QIODevice::WriteOnly | QIODevice::Truncate |
                        QIODevice::Text)) {
    delete s_logFile;
    s_logFile = nullptr;
  }
}

// Surfaces a fatal startup failure to the user, then exits. A QML-rendered
// error screen would depend on the same Qt Quick/OpenGL stack that's the top
// suspect for a startup failure in the first place; MessageBoxW works even
// if that stack itself is what's broken. Non-Windows builds still have a
// console, so the qWarning() below is enough there.
static void reportStartupFailure(const QString &reason) {
  qWarning().noquote() << "[shell] startup failed:" << reason;
#ifdef Q_OS_WIN
  const QString text =
      reason + QStringLiteral("\n\nLog file: ") + s_logFilePath;
  MessageBoxW(nullptr, reinterpret_cast<const wchar_t *>(text.utf16()),
              L"Cove — Startup Error", MB_OK | MB_ICONERROR);
#endif
  if (qApp)
    qApp->exit(1);
}

// ── Static file server ───────────────────────────────────────────────────────
class StaticServer : public QTcpServer {
public:
  explicit StaticServer(const QString &root, QObject *parent = nullptr)
      : QTcpServer(parent), m_root(QDir(root).absolutePath()) {}

  QUrl start() {
    if (!listen(QHostAddress::LocalHost, 5174)) {
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
// The aboutToQuit handler in main() covers clean shutdowns, but a crashed or
// SIGKILLed shell never reaches it, leaving an orphan backend holding :6969
// (which then makes the next launch fail its port bind). Tie the child's
// lifetime to ours at the OS level: PDEATHSIG on Linux, a kill-on-close Job
// Object on Windows.

#ifdef Q_OS_WIN
static HANDLE backendJob() {
  static HANDLE job = [] {
    HANDLE h = CreateJobObjectW(nullptr, nullptr);
    if (h) {
      JOBOBJECT_EXTENDED_LIMIT_INFORMATION info{};
      info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
      SetInformationJobObject(h, JobObjectExtendedLimitInformation, &info,
                              sizeof(info));
    }
    return h;
  }();
  return job;
}
#endif

static QProcess *startBackend(const QString &exePath, QObject *parent) {
  auto *proc = new QProcess(parent);
  proc->setProcessChannelMode(QProcess::MergedChannels);
  QObject::connect(proc, &QProcess::readyReadStandardOutput, proc, [proc]() {
    const QString out = QString::fromUtf8(proc->readAllStandardOutput());
    for (const QString &line : out.split('\n', Qt::SkipEmptyParts))
      qInfo().noquote() << "[go]" << line;
  });
#ifdef Q_OS_LINUX
  proc->setChildProcessModifier(
      [] { prctl(PR_SET_PDEATHSIG, SIGTERM); });
#endif
  proc->start(exePath, {});
#ifdef Q_OS_WIN
  if (HANDLE job = backendJob(); job && proc->processId() != 0) {
    HANDLE child = OpenProcess(PROCESS_SET_QUOTA | PROCESS_TERMINATE, FALSE,
                               static_cast<DWORD>(proc->processId()));
    if (child) {
      AssignProcessToJobObject(job, child);
      CloseHandle(child);
    }
  }
#endif
  return proc;
}

// Polls 127.0.0.1:port until it accepts a connection, then calls onReady.
// Calls onTimeout instead if the backend never comes up within timeoutMs —
// without this, a backend that's alive but never binds the port (or never
// started at all) left the shell polling forever with no window and no
// error, indistinguishable from the app doing nothing.
static void waitForBackend(quint16 port, int timeoutMs,
                            std::function<void()> onReady,
                            std::function<void()> onTimeout) {
  auto *timer = new QTimer;
  timer->setInterval(150);
  auto elapsed = std::make_shared<QElapsedTimer>();
  elapsed->start();
  QObject::connect(
      timer, &QTimer::timeout, timer,
      [timer, port, timeoutMs, onReady, onTimeout, elapsed]() {
        if (elapsed->hasExpired(timeoutMs)) {
          timer->stop();
          timer->deleteLater();
          onTimeout();
          return;
        }
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
// exists before the app's JS runs. WebEngineScript isn't creatable from QML in
// Qt 6, so this is done in C++ where QWebEngineScript is a proper value type.
static void installBridgeScript(QWebEngineProfile *profile) {
  const QString src = readQWebChannelJs();
  if (src.isEmpty())
    return;
  QWebEngineScript script;
  script.setName(QStringLiteral("qwebchannel"));
  script.setSourceCode(src);
  script.setInjectionPoint(QWebEngineScript::DocumentCreation);
  script.setWorldId(QWebEngineScript::MainWorld);
  script.setRunsOnSubFrames(false);
  profile->scripts()->insert(script);
}

// Translucent test overlay: exercises the QWebChannel bridge by connecting to
// the registered `mpv` object (injected globally — see main()), showing live
// position/duration/track data from C++ signals, and calling mpv.pause()/resume()
// on a timer to verify JS→C++ slot calls.
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
  // Suppress noisy-but-benign Qt WebChannel property warnings before anything
  // else logs. The returned pointer is the Qt default handler.
  s_defaultMsgHandler = qInstallMessageHandler(msgFilter);

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
  app.setOrganizationName("coveninja");
  app.setWindowIcon(QIcon(QStringLiteral(":/cove.png")));

  openLogFile();

  // Single-instance guard. QLockFile detects stale locks from crashed
  // processes (it records the holder's PID), so a crash never wedges future
  // launches the way the old "did port 5174 bind?" heuristic could.
  QLockFile instanceLock(QDir::temp().filePath("cove_shell.lock"));
  if (!instanceLock.tryLock(0)) {
    reportStartupFailure(
        QStringLiteral("Cove is already running (another instance holds the "
                       "instance lock)."));
    return 0;
  }

  qmlRegisterType<MpvObject>("mpv", 1, 0, "MpvObject");

  QCommandLineParser parser;
  parser.setApplicationDescription("Cove Qt shell");
  parser.addHelpOption();
  QCommandLineOption backendOpt("backend", "Path to the Go sidecar binary.",
#ifdef Q_OS_WIN
                                "path", "../../cove.exe");
#else
                                "path", "../../cove");
#endif
  QCommandLineOption webrootOpt("webroot", "Path to the renderer build dir.",
                                "path", "../../web/dist");
  QCommandLineOption playOpt(
      "play", "Compositing test: play this media file behind a test overlay.",
      "file");
  QCommandLineOption devOpt("dev", "Connect to the Vite development server for hot reload.");
  parser.addOption(backendOpt);
  parser.addOption(webrootOpt);
  parser.addOption(playOpt);
  parser.addOption(devOpt);
  parser.process(app);

  const QString backendPath =
      QFileInfo(parser.value(backendOpt)).absoluteFilePath();
  const QString webRoot = QFileInfo(parser.value(webrootOpt)).absoluteFilePath();
  // The API port is pinned across the stack (backend bind, web bundle's BASE
  // URL, index.html CSP) — a configurable flag here would only pretend it can
  // be changed.
  constexpr quint16 apiPort = 6969;
    const QString testFile =
      parser.isSet(playOpt)
          ? QFileInfo(parser.value(playOpt)).absoluteFilePath()
          : QString();
  const bool isDev = parser.isSet(devOpt);

  QQmlApplicationEngine engine;

  // Must run before the WebEngineView navigates so the script covers the first
  // load (the QML view uses the default profile this installs onto).
  // WebEngineScript isn't creatable from QML in Qt 6, so this is done here.
  installBridgeScript(QWebEngineProfile::defaultProfile());

  auto loadScene = [&](const QString &url, const QString &mpvFile) {
    engine.rootContext()->setContextProperty("launchUrl", url);
    engine.rootContext()->setContextProperty("mpvTestFile", mpvFile);
    engine.load(QUrl("qrc:/qml/main.qml"));
  };

  if (!testFile.isEmpty()) {
    // Test mode — no backend needed for a local file.
    qInfo().noquote() << "[shell] compositing test, playing:" << testFile;
    loadScene(testOverlayUrl(), testFile);
  } else {
    qInfo().noquote() << "[shell] backend:" << backendPath
                      << (QFileInfo::exists(backendPath) ? "(ok)" : "(MISSING)");
    qInfo().noquote() << "[shell] webroot:" << webRoot;

    auto *server = new StaticServer(webRoot, &app);
    const QUrl baseUrl = server->start();
    if (baseUrl.isEmpty()) {
      reportStartupFailure(QStringLiteral(
          "Local static server failed to bind 127.0.0.1:5174 (port already "
          "in use by another instance?)."));
      return 1;
    }
    qInfo().noquote() << "[shell] serving renderer at" << baseUrl.toString();

    QProcess *backend = startBackend(backendPath, &app);
    QObject::connect(&app, &QCoreApplication::aboutToQuit, [backend]() {
      if (backend->state() == QProcess::NotRunning)
        return;
      backend->terminate();
      if (!backend->waitForFinished(2000))
        backend->kill();
    });

    // Guards against reportStartupFailure firing twice (e.g. errorOccurred
    // and finished both fire for a crashed process) and against a late
    // failure signal arriving after the backend was already confirmed ready.
    auto settled = std::make_shared<bool>(false);

    QObject::connect(
        backend, &QProcess::errorOccurred, &app,
        [backend, backendPath, settled](QProcess::ProcessError) {
          if (*settled)
            return;
          *settled = true;
          reportStartupFailure(
              QStringLiteral("Backend process failed to start: %1 (path: %2)")
                  .arg(backend->errorString(), backendPath));
        });

    // Exit code 42 signals that the backend applied an update and wants the
    // shell to restart so the new binaries are loaded. Re-exec this process
    // with the same arguments, then quit the current instance.
    // On Windows the backend cannot rename its own .exe while running, so it
    // writes cove.exe.new and exits; we perform the rename here, when the
    // process is guaranteed to be gone.
    QObject::connect(
        backend,
        QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished),
        [&app, backendPath, settled](int exitCode, QProcess::ExitStatus) {
          if (exitCode == 42) {
#ifdef Q_OS_WIN
            const QString newExe = backendPath + ".new";
            if (QFile::exists(newExe)) {
              QFile::remove(backendPath + ".old");
              QFile::rename(backendPath, backendPath + ".old");
              QFile::rename(newExe, backendPath);
            }
#endif
            const QStringList args = QCoreApplication::arguments().mid(1);
            QProcess::startDetached(QCoreApplication::applicationFilePath(),
                                    args);
            app.quit();
            return;
          }
          if (!*settled) {
            *settled = true;
            reportStartupFailure(
                QStringLiteral(
                    "Backend exited before it was ready (exit code %1).")
                    .arg(exitCode));
          }
        });

    waitForBackend(
        apiPort, 20000,
        [loadScene, baseUrl, isDev, settled]() {
          if (*settled)
            return;
          *settled = true;
          qInfo().noquote() << "[shell] backend up — loading UI";
          if (isDev) {
            loadScene(QStringLiteral("http://localhost:5173"), QString());
          } else {
            loadScene(baseUrl.toString(), QString());
          }
        },
        [apiPort, settled]() {
          if (*settled)
            return;
          *settled = true;
          reportStartupFailure(
              QStringLiteral(
                  "Backend did not respond on 127.0.0.1:%1 within 20s.")
                  .arg(apiPort));
        });
  }

  return app.exec();
}
