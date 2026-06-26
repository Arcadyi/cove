#pragma once

#include <QtQuick/QQuickFramebufferObject>
#include <QString>
#include <QVariant>

#include <mpv/client.h>
#include <mpv/render_gl.h>

// mpv rendered into a Quick scene-graph FBO. Placed behind a transparent
// WebEngineView in main.qml so the web UI composites on top of the video.
//
// Phase 2a scope: enough to render and load a file (compositing proof). The
// QWebChannel bridge + property/event signals come in Phase 2b — the control
// slots below are already shaped for it.
class MpvObject : public QQuickFramebufferObject {
  Q_OBJECT
  friend class MpvRenderer;

public:
  explicit MpvObject(QQuickItem *parent = nullptr);
  ~MpvObject() override;

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

  // Re-emit the current playback state. The web client calls this right after
  // it connects, because mpv emits the initial values of observed properties
  // (pause, duration, …) before the QWebChannel bridge has attached its signal
  // handlers — so without this, state like `paused` never reaches the UI until
  // the next time it happens to change.
  void requestState();

signals:
  // Emitted from mpv's render thread; connected queued to update() on the GUI
  // thread (QQuickFramebufferObject::update() must run there).
  void mpvUpdated();

  // Playback state, from observed mpv properties / events.
  void positionChanged(double seconds);
  void durationChanged(double seconds);
  void pausedChanged(bool paused);
  void volumeChanged(double volume);
  void fileLoaded();
  void endReached();
  // Each entry: {id, type:"video"|"audio"|"sub", title, lang, selected}.
  void tracksChanged(const QVariantList &tracks);

private slots:
  // Invoked (queued) once the GL render context exists; flushes any load that
  // was requested before the context was ready.
  void handleRenderReady();
  // Drains mpv's event queue on the GUI thread (woken by on_events).
  void onMpvEvents();

private:
  static void on_update(void *ctx);
  static void on_events(void *ctx);
  void handlePropertyChange(mpv_event_property *prop);
  QVariantList readTrackList();

  mpv_handle *m_mpv = nullptr;
  mpv_render_context *m_mpvGl = nullptr;
  bool m_renderReady = false;
  QString m_pendingUrl;
};
