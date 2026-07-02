#pragma once

#include <QtQuick/QQuickFramebufferObject>
#include <QString>
#include <QTimer>
#include <QVariant>

#include <atomic>

#include <mpv/client.h>
#include <mpv/render_gl.h>

// mpv rendered into a Quick scene-graph FBO. Placed behind a transparent
// WebEngineView in main.qml so the web UI composites on top of the video.
class MpvObject : public QQuickFramebufferObject {
  Q_OBJECT
  friend class MpvRenderer;

  // False when libmpv failed to create/initialize (broken GL/mpv stack). The
  // shell keeps running — every slot below no-ops — and the web UI reads this
  // through the bridge to show its "player unavailable" state.
  Q_PROPERTY(bool valid READ valid CONSTANT)

public:
  explicit MpvObject(QQuickItem *parent = nullptr);
  ~MpvObject() override;

  bool valid() const { return m_mpv != nullptr; }

  Renderer *createRenderer() const override;

public slots:
  // Generic mpv command, e.g. command(["loadfile", url]).
  void command(const QVariant &args);
  void setOption(const QString &name, const QString &value);
  void setMpvProperty(const QString &name, const QString &value);

  void play(const QString &url);
  void pause();
  void resume();
  void stop();
  void seek(double seconds);

  void setAudioTrack(int id);        // mpv aid
  void setSubtitleTrack(int id);     // mpv sid; id < 0 disables subtitles
  void addSubtitle(const QString &url, const QString &title = QString(),
                   const QString &lang = QString()); // external (e.g. OpenSubtitles)
  void setVolume(double volume);     // 0–100

  // Request the shell window to enter or leave fullscreen. The slot emits
  // fullscreenRequested(), which QML catches and forwards to win.showFullScreen()
  // / win.showNormal(). Roundtripping via a signal keeps C++ decoupled from QML.
  void setFullscreen(bool fullscreen);

  // Re-emit the current playback state. The web client calls this right after
  // it connects, because mpv emits the initial values of observed properties
  // (pause, duration, …) before the QWebChannel bridge has attached its signal
  // handlers — so without this, state like `paused` never reaches the UI until
  // the next time it happens to change.
  void requestState();

signals:
  // Playback state, from observed mpv properties / events.
  void positionChanged(double seconds);
  void durationChanged(double seconds);
  void pausedChanged(bool paused);
  void volumeChanged(double volume);
  void fileLoaded();
  void endReached();
  // Each entry: {id, type:"video"|"audio"|"sub", title, lang, selected}.
  void tracksChanged(const QVariantList &tracks);
  void fullscreenRequested(bool fullscreen);

private slots:
  // Invoked (queued) once the GL render context exists; flushes any load that
  // was requested before the context was ready.
  void handleRenderReady();
  // Drains mpv's event queue on the GUI thread (woken by on_events).
  void onMpvEvents();
  // Fallback position poll — fires every 250 ms for streams where mpv does not
  // emit time-pos property-change events (e.g. some HTTP or TS sources).
  void pollPosition();

private:
  static void on_update(void *ctx);
  static void on_events(void *ctx);
  void handlePropertyChange(mpv_event_property *prop);
  QVariantList readTrackList();

  mpv_handle *m_mpv = nullptr;
  // Shared between the GUI thread (dtor) and the Quick render thread
  // (create/render/renderer-dtor). Freed by whichever teardown path gets
  // there first via exchange(nullptr), so it can't be double-freed.
  std::atomic<mpv_render_context *> m_mpvGl{nullptr};
  bool m_renderReady = false;
  QString m_pendingUrl;
  QTimer *m_pollTimer = nullptr;
};
