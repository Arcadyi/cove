#include "MpvObject.h"

#include <clocale>

#include <QtGui/QOpenGLContext>
#include <QtOpenGL/QOpenGLFramebufferObject>
#include <QtQuick/QQuickOpenGLUtils>
#include <QtQuick/QQuickWindow>
#include <QByteArray>
#include <QDebug>
#include <QStringList>
#include <QVector>

namespace {

// libmpv asks us to resolve GL function pointers; route to the current Qt GL
// context (valid because mpv renders on Quick's render thread with it current).
void *getProcAddressMpv(void *ctx, const char *name) {
  Q_UNUSED(ctx)
  QOpenGLContext *glctx = QOpenGLContext::currentContext();
  if (!glctx)
    return nullptr;
  return reinterpret_cast<void *>(glctx->getProcAddress(QByteArray(name)));
}

// Recursively convert an mpv_node (used by node-typed properties like
// track-list) into a QVariant tree.
QVariant nodeToVariant(const mpv_node *node) {
  switch (node->format) {
  case MPV_FORMAT_STRING:
    return QString::fromUtf8(node->u.string);
  case MPV_FORMAT_FLAG:
    return bool(node->u.flag);
  case MPV_FORMAT_INT64:
    return qlonglong(node->u.int64);
  case MPV_FORMAT_DOUBLE:
    return node->u.double_;
  case MPV_FORMAT_NODE_ARRAY: {
    QVariantList list;
    for (int i = 0; i < node->u.list->num; ++i)
      list.append(nodeToVariant(&node->u.list->values[i]));
    return list;
  }
  case MPV_FORMAT_NODE_MAP: {
    QVariantMap map;
    for (int i = 0; i < node->u.list->num; ++i)
      map.insert(QString::fromUtf8(node->u.list->keys[i]),
                 nodeToVariant(&node->u.list->values[i]));
    return map;
  }
  default:
    return {};
  }
}

} // namespace

// ── Renderer (runs on the Quick render thread) ───────────────────────────────
class MpvRenderer : public QQuickFramebufferObject::Renderer {
public:
  explicit MpvRenderer(MpvObject *obj) : m_obj(obj) {}
  ~MpvRenderer() override {
    // Must free the render context while the GL context is current — which it
    // is during renderer teardown on the render thread.
    if (m_obj->m_mpvGl) {
      mpv_render_context_free(m_obj->m_mpvGl);
      m_obj->m_mpvGl = nullptr;
    }
  }

  QOpenGLFramebufferObject *createFramebufferObject(const QSize &size) override {
    // Lazily create mpv's render context the first time we have a GL context.
    if (!m_obj->m_mpvGl) {
      mpv_opengl_init_params glInit{getProcAddressMpv, nullptr};
      mpv_render_param params[]{
          {MPV_RENDER_PARAM_API_TYPE,
           const_cast<char *>(MPV_RENDER_API_TYPE_OPENGL)},
          {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &glInit},
          {MPV_RENDER_PARAM_INVALID, nullptr}};
      if (mpv_render_context_create(&m_obj->m_mpvGl, m_obj->m_mpv, params) < 0) {
        qWarning() << "[mpv] failed to create render context";
        m_obj->m_mpvGl = nullptr;
      } else {
        mpv_render_context_set_update_callback(m_obj->m_mpvGl,
                                               MpvObject::on_update, m_obj);
        // The context now exists — let the object flush any deferred load. mpv
        // disables video if loadfile runs before the VO has a render context.
        QMetaObject::invokeMethod(m_obj, "handleRenderReady",
                                  Qt::QueuedConnection);
      }
    }
    return QQuickFramebufferObject::Renderer::createFramebufferObject(size);
  }

  void render() override {
    if (!m_obj->m_mpvGl)
      return;

    QQuickOpenGLUtils::resetOpenGLState();

    QOpenGLFramebufferObject *fbo = framebufferObject();
    mpv_opengl_fbo mpfbo{static_cast<int>(fbo->handle()), fbo->width(),
                         fbo->height(), 0};
    int flipY = 0; // if the picture is upside down, set this to 1
    mpv_render_param params[]{
        {MPV_RENDER_PARAM_OPENGL_FBO, &mpfbo},
        {MPV_RENDER_PARAM_FLIP_Y, &flipY},
        {MPV_RENDER_PARAM_INVALID, nullptr}};
    mpv_render_context_render(m_obj->m_mpvGl, params);

    QQuickOpenGLUtils::resetOpenGLState();
  }

private:
  MpvObject *m_obj;
};

// ── MpvObject (GUI thread) ───────────────────────────────────────────────────
MpvObject::MpvObject(QQuickItem *parent) : QQuickFramebufferObject(parent) {
  // libmpv requires the C numeric locale; Qt sets the locale from the
  // environment, so reset just LC_NUMERIC right before creating mpv. Safe with
  // Qt, which formats via QLocale rather than the C locale.
  std::setlocale(LC_NUMERIC, "C");

  m_mpv = mpv_create();
  if (!m_mpv) {
    qFatal("[mpv] mpv_create() failed");
    return;
  }

  // Render via the embedded (libmpv) video output; hardware decode where safe.
  mpv_set_option_string(m_mpv, "vo", "libmpv");
  mpv_set_option_string(m_mpv, "hwdec", "auto-safe"); // HW decode (VAAPI on your AMD); falls back to SW
  // Surface only real errors; flip to terminal=yes + msg-level=all=v to debug.
  mpv_set_option_string(m_mpv, "terminal", "yes");
  mpv_set_option_string(m_mpv, "msg-level", "all=error");

  if (mpv_initialize(m_mpv) < 0) {
    qFatal("[mpv] mpv_initialize() failed");
    return;
  }

  // on_update fires on mpv's render thread → marshal to the GUI thread.
  connect(this, &MpvObject::mpvUpdated, this,
          [this]() { update(); }, Qt::QueuedConnection);

  // Observe the properties we surface as signals, and wake us on mpv events.
  mpv_observe_property(m_mpv, 0, "time-pos", MPV_FORMAT_DOUBLE);
  mpv_observe_property(m_mpv, 0, "duration", MPV_FORMAT_DOUBLE);
  mpv_observe_property(m_mpv, 0, "pause", MPV_FORMAT_FLAG);
  mpv_observe_property(m_mpv, 0, "volume", MPV_FORMAT_DOUBLE);
  mpv_observe_property(m_mpv, 0, "eof-reached", MPV_FORMAT_FLAG);
  // NONE = notify on change without delivering the (complex) node; we re-query.
  mpv_observe_property(m_mpv, 0, "track-list", MPV_FORMAT_NONE);
  mpv_set_wakeup_callback(m_mpv, on_events, this);
}

MpvObject::~MpvObject() {
  // The render context is freed by the renderer (render thread). Here we just
  // tear down the handle.
  if (m_mpv) {
    mpv_terminate_destroy(m_mpv);
    m_mpv = nullptr;
  }
}

QQuickFramebufferObject::Renderer *MpvObject::createRenderer() const {
  return new MpvRenderer(const_cast<MpvObject *>(this));
}

void MpvObject::on_update(void *ctx) {
  emit static_cast<MpvObject *>(ctx)->mpvUpdated();
}

void MpvObject::command(const QVariant &args) {
  if (!m_mpv)
    return;
  const QStringList list = args.toStringList();
  QVector<QByteArray> bytes;
  bytes.reserve(list.size());
  for (const QString &s : list)
    bytes.append(s.toUtf8());

  QVector<const char *> argv;
  argv.reserve(bytes.size() + 1);
  for (const QByteArray &b : bytes)
    argv.append(b.constData());
  argv.append(nullptr);

  mpv_command(m_mpv, argv.data());
}

void MpvObject::setOption(const QString &name, const QString &value) {
  if (m_mpv)
    mpv_set_option_string(m_mpv, name.toUtf8().constData(),
                          value.toUtf8().constData());
}

void MpvObject::setMpvProperty(const QString &name, const QString &value) {
  if (m_mpv)
    mpv_set_property_string(m_mpv, name.toUtf8().constData(),
                            value.toUtf8().constData());
}

void MpvObject::handleRenderReady() {
  m_renderReady = true;
  if (!m_pendingUrl.isEmpty()) {
    command(QVariant(QStringList{"loadfile", m_pendingUrl}));
    m_pendingUrl.clear();
  }
}

void MpvObject::play(const QString &url) {
  // Composite the video surface again (it's hidden while stopped).
  setVisible(true);
  // Defer until the render context exists, otherwise mpv inits the video output
  // with no context and permanently drops the video track for this file.
  if (m_renderReady)
    command(QVariant(QStringList{"loadfile", url}));
  else
    m_pendingUrl = url;
}
void MpvObject::pause() { setMpvProperty("pause", "yes"); }
void MpvObject::resume() { setMpvProperty("pause", "no"); }
void MpvObject::stop() {
  m_pendingUrl.clear();
  command({QStringList{"stop"}});
  // mpv leaves the last frame in the FBO (it won't repaint with nothing loaded),
  // so hide the item — otherwise the stale frame shows through the UI.
  setVisible(false);
}
void MpvObject::seek(double seconds) {
  command({QStringList{"seek", QString::number(seconds), "absolute"}});
}

void MpvObject::setAudioTrack(int id) {
  setMpvProperty("aid", QString::number(id));
}

void MpvObject::setSubtitleTrack(int id) {
  setMpvProperty("sid", id < 0 ? QStringLiteral("no") : QString::number(id));
}

void MpvObject::addSubtitle(const QString &url, const QString &title,
                            const QString &lang) {
  // sub-add <url> select [<title> [<lang>]] — "select" makes it the active sub.
  QStringList args{"sub-add", url, "select", title};
  if (!lang.isEmpty())
    args << lang;
  command(QVariant(args));
}

void MpvObject::setVolume(double volume) {
  setMpvProperty("volume", QString::number(volume));
}

// ── Events / property observation ────────────────────────────────────────────
void MpvObject::on_events(void *ctx) {
  // Called on an mpv-internal thread; hop to the GUI thread to touch the queue.
  QMetaObject::invokeMethod(static_cast<MpvObject *>(ctx), "onMpvEvents",
                            Qt::QueuedConnection);
}

void MpvObject::onMpvEvents() {
  while (m_mpv) {
    mpv_event *event = mpv_wait_event(m_mpv, 0);
    if (event->event_id == MPV_EVENT_NONE)
      break;
    switch (event->event_id) {
    case MPV_EVENT_PROPERTY_CHANGE:
      handlePropertyChange(static_cast<mpv_event_property *>(event->data));
      break;
    case MPV_EVENT_FILE_LOADED:
      emit fileLoaded();
      emit tracksChanged(readTrackList());
      break;
    case MPV_EVENT_END_FILE:
      emit endReached();
      break;
    default:
      break;
    }
  }
}

void MpvObject::handlePropertyChange(mpv_event_property *prop) {
  const QString name = QString::fromUtf8(prop->name);
  if (name == "time-pos" && prop->format == MPV_FORMAT_DOUBLE)
    emit positionChanged(*static_cast<double *>(prop->data));
  else if (name == "duration" && prop->format == MPV_FORMAT_DOUBLE)
    emit durationChanged(*static_cast<double *>(prop->data));
  else if (name == "volume" && prop->format == MPV_FORMAT_DOUBLE)
    emit volumeChanged(*static_cast<double *>(prop->data));
  else if (name == "pause" && prop->format == MPV_FORMAT_FLAG)
    emit pausedChanged(*static_cast<int *>(prop->data) != 0);
  else if (name == "eof-reached" && prop->format == MPV_FORMAT_FLAG) {
    if (*static_cast<int *>(prop->data))
      emit endReached();
  } else if (name == "track-list")
    emit tracksChanged(readTrackList());
}

QVariantList MpvObject::readTrackList() {
  mpv_node node;
  if (!m_mpv ||
      mpv_get_property(m_mpv, "track-list", MPV_FORMAT_NODE, &node) < 0)
    return {};

  const QVariant tree = nodeToVariant(&node);
  mpv_free_node_contents(&node);

  QVariantList out;
  const QVariantList tracks = tree.toList();
  for (const QVariant &t : tracks) {
    const QVariantMap m = t.toMap();
    QVariantMap track;
    track.insert("id", m.value("id"));
    track.insert("type", m.value("type")); // "video" | "audio" | "sub"
    track.insert("title", m.value("title"));
    track.insert("lang", m.value("lang"));
    track.insert("selected", m.value("selected"));
    out.append(track);
  }
  return out;
}
