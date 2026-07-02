# Architecture

This document explains how Cove is put together: the three cooperating
components, how data flows through them, and the build-tag mechanism that
separates the open-source core from proprietary functionality. It assumes
you've read the [README](README.md) for the user-facing feature list and
build instructions.

For an API endpoint reference, see [docs/API.md](docs/API.md). For dev setup
and contribution conventions, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Three components at a glance

Cove is three processes cooperating over local sockets, not one monolith:

1. **Go backend** (`main.go` + `internal/`) — an HTTP server on `:6969`
   handling TMDB metadata, streaming, addon integration (Stremio-style
   providers/subtitles), sandboxed community plugin execution, the local
   library/settings/profiles stores, and personalized recommendations.
2. **Svelte frontend** (`web/`) — a Svelte 5 + TypeScript + Vite SPA that
   talks exclusively to the Go backend over HTTP (`web/src/lib/api.ts`). It
   has no direct filesystem or process access of its own.
3. **Qt shell** (`qt/`) — a native Qt Quick application that hosts the web UI
   in a `QtWebEngine` view and renders video via libmpv, composited *behind*
   the (transparent) web layer. It also owns process lifecycle: it spawns the
   Go binary as a child process and serves the built frontend.

At startup, `qt/src/main.cpp` does three things in parallel: starts a small
built-in static file server for `web/dist` (see below), spawns the Go binary
as a `QProcess` (`startBackend`, `main.cpp:142-152`, stdout/stderr merged and
forwarded to Qt's log with a `[go]` prefix), and polls `localhost:6969` with a
raw TCP connect every 150ms (`waitForBackend`, `main.cpp:154-172` — a
connectivity check, not an HTTP health check). Once the backend answers, the
shell loads the QML scene and points the `WebEngineView` at either the static
server's URL or, in `--dev` mode, Vite's dev server at `localhost:5173`.

## Backend package map (`internal/*`)

- **`tmdb`** — the TMDB API client and the largest single set of HTTP routes
  (search, details, images, videos, providers, similar-titles, genre lists,
  a batched quality-probe endpoint). No build-tag variance; always compiled
  the same way. See `docs/API.md` for the full route list.
- **`library`** — the local watch history / ratings / "not interested" store,
  persisted as `library-<profileID>.json` under the OS config dir
  (`internal/utils.ConfigPath`). Exposes `TasteSignals()` and `Generation()`
  — the interface the recommendation engine consumes without either package
  importing the other.
- **`settings`** — a single flat `Settings` struct, same persistence pattern
  as `library` (`settings-<profileID>.json`), whole-object GET/PUT over
  `/api/settings`. Select-style preferences (stream selection mode, discovery
  algorithm) are plain strings with no server-side enum validation — the
  frontend owns the allowed-value metadata.
- **`addons`** — the Stremio-compatible provider/subtitle addon manager. Two
  "official" addons (JustWatch availability, IntroDB timestamps) are hardcoded
  Go integrations; anything else is a user-pasted Stremio manifest URL,
  fetched and classified by resource type at add-time. Fan-out across
  multiple enabled addons of the same kind is a sequential loop with
  per-addon failures swallowed (non-fatal, matching the "addon failures don't
  break the app" principle).
- **`player`** — owns the `anacrolix/torrent` client and streams the largest
  file in a torrent as seekable HTTP (`http.ServeContent`, so mpv's Range
  requests just work). See "Playback data flow" below — there is no
  transcoding here.
- **`nuvio`** — runs community-maintained JS scraper plugins in a sandboxed
  `goja` runtime (pure Go, no CGO, no Node) to produce additional direct-HTTP
  stream candidates alongside `addons`. Off by default: a plugin repo must be
  added and each scraper individually enabled by the user. One fresh
  runtime per invocation, bounded by a per-scraper timeout and an overall
  deadline for the whole batch; per-scraper errors/timeouts are logged and
  skipped, matching `addons`'s swallow-per-failure philosophy. Called only
  from `player.go`'s `/api/streams` handler — deliberately excluded from
  `tmdb.go`'s batched `/api/quality/batch` endpoint, which fans out over
  every title in a discovery grid and can't afford a JS runtime + network
  call per tile.
- **`profiles`** — Netflix-style local user profiles (not to be confused with
  content-rating). Switching the active profile reloads `library`,
  `settings`, and `addons` in place via a callback registered in `main.go`.
- **`updater`** — self-update via GitHub releases; skips the check entirely
  on managed distributions (`APPIMAGE`/`FLATPAK_ID` env vars set) or dev
  builds (non-semver version string). Applying an update exits the process
  with code `42`, which the Qt shell interprets as "restart me"
  (`main.cpp:352-376`).
- **`clientsession`** — a tiny opaque JSON blob store
  (`os.UserConfigDir()/cove/session.json`) used for client-side auth token
  persistence, because QtWebEngine's `localStorage` isn't reliably durable
  across restarts.
- **`utils`** — shared helpers: `ConfigPath` (per-OS config directory
  resolution), `AtomicWriteFile`, `CorsMiddleware` (wraps nearly every
  handler in the app; auto-answers `OPTIONS` with 204 before the wrapped
  handler runs).
- **`discover`** and **`supabase`** — see "The OSS/proprietary split" below;
  these are the two packages with a compile-time swap between an open-source
  stub and proprietary functionality.

## Playback data flow
1. The frontend requests candidate streams for a title via `GET /api/streams`
   (`internal/player/player.go:316`), which fans out to
   `addons.Manager.GetAllStreams` — each enabled provider addon contributes
   infohashes and/or direct URLs — and, if any Nuvio scrapers are enabled,
   `nuvio.Manager.GetStreams`, which runs each one in its own sandboxed goja
   runtime and appends whatever direct-HTTP streams they produce.
2. `Player.svelte` picks a source (`streamSelection.ts`'s `pickBestStream`, or
   a manual choice) and calls `Player.play(api.playUrl(src))`
   (`web/src/lib/player/player.svelte.ts`). `playUrl` builds either
   `/api/play?hash=<infohash>` or passes a direct URL through unchanged.
3. The `MpvPlayer` wrapper sends that URL over a `QWebChannel` bridge to the
   native `MpvObject`, which issues an mpv `loadfile` command
   (`qt/src/MpvObject.cpp:224-233` — deferred until the render context
   exists, since loading before that silently drops video for that file).
4. **mpv itself opens the URL as an HTTP client**, hitting the Go backend's
   `GET /api/play` route directly (Qt/QML plays no part in the actual byte
   transfer):
   - `?url=<direct>` → `307 Temporary Redirect` straight to the origin
     server; the Go process isn't in the data path at all
     (`player.go:368-386`).
   - `?hash=<infohash>` → `Player.StreamTorrent` resolves the largest file in
     the torrent (`getLargestTorrentFile`, `player.go:100-141` — a heuristic
     assuming one file of interest per torrent, with a 45s metadata-fetch
     timeout), opens a responsive/16MiB-readahead reader, and calls
     `http.ServeContent`. Range-request seeking works because
     `http.ServeContent` handles `Range:` headers and the anacrolix reader's
     `io.ReadSeeker` reprioritizes piece downloads around the seeked offset.
     Every mpv seek opens a *new* HTTP request (and thus a new reader) —
     closing the old reader on handler return matters because anacrolix
     readers hold download priority until closed.
5. mpv decodes and renders every codec/container it natively supports — no
   transcoding step exists to bridge format gaps.

**The torrent reaper.** `CleanupTorrents()` (`player.go:150-178`) runs on a
30-minute ticker (`main.go:133-139`). A torrent is dropped and its on-disk
pieces deleted (from `os.TempDir()/cove-torrents`) only if its reader count is
`<= 0` **and** it hasn't been used in the last 30 minutes. The reader count is
incremented for the duration of `StreamTorrent` and `lastUsed` is also
refreshed by `GetProgress` (so an open progress-bar poll counts as activity)
— together these protect a torrent that's actively being watched, or whose
progress UI is still open, from being collected mid-watch. Without this
reaper, downloaded pieces and open file handles would accumulate for the life
of the process.

An earlier browser-video + HLS.js architecture (predating the current mpv/Qt
shell) left behind an unused `Settings.PreferHLS` field, `api.ts`'s
`hlsStart`/`hlsStop`/`hlsMasterUrl`/`probe`/`subtitleExtractUrl`, a
`subtitleCues.svelte.ts` WebVTT cue-tracker with no callers, and an unused
`player.NewServer()` helper (`main.go` built its own inline `*http.Server`).
All of this has since been removed — mentioned here only so a future
spelunk through git history for "HLS" doesn't look like it's chasing a
still-live feature.

## The Qt shell in detail

`MpvObject` (`qt/src/MpvObject.h/cpp`) subclasses `QQuickFramebufferObject`.
Its nested `MpvRenderer` runs on the Qt Quick render thread and does the
actual libmpv work: it creates an mpv render context configured for OpenGL
(`MPV_RENDER_PARAM_API_TYPE = "opengl"`, with `get_proc_address` resolved
through the *current* `QOpenGLContext`), then on every frame wraps the Quick-
provided framebuffer object into an `mpv_opengl_fbo` and calls
`mpv_render_context_render` — mpv draws directly into the same FBO Qt Quick
composites from. mpv's own update callback fires on mpv's render thread, so
it's marshalled to the GUI thread via a queued `QMetaObject::invokeMethod`
before touching any Qt signal.

**"Video behind a transparent web layer"** is ordinary QML z-ordering, not a
special video flag: `main.qml` declares the `MpvObject` first (bottom of the
scene graph) and a `WebEngineView` with `backgroundColor: "transparent"`
after it (on top), filling the same window. Three things in `main()`
(`main.cpp:266-274`) make this actually render correctly:
`Qt::AA_ShareOpenGLContexts` (mpv and Quick's renderer must share GL
contexts), forcing Quick onto the OpenGL RHI backend to match mpv's OpenGL
render API, and giving the window's default surface format an 8-bit alpha
channel so the WebEngineView's transparent background has something to show
through to.

**The `QWebChannel` bridge** is used *only* for player control/state — every
other piece of app data (metadata, library, settings, addon config) goes over
plain HTTP from the web layer to the Go backend, same as if the web UI were
running in an ordinary browser. `main.qml` registers the `MpvObject` instance
under the id `"mpv"` on a `WebChannel` attached to the `WebEngineView`, which
causes QtWebEngine to inject `qt.webChannelTransport` into `window`. Since the
JS-side `QWebChannel` shim class also needs to exist before app code runs,
`main.cpp` reads Qt's own compiled-in `qwebchannel.js` resource and injects it
via a `QWebEngineScript` at `DocumentCreation` time (`installBridgeScript`,
`main.cpp:187-198`) on every page load. On the frontend,
`web/src/lib/player/player.svelte.ts`'s `MpvPlayer` class is the sole
consumer: if `window.qt.webChannelTransport`/`window.QWebChannel` aren't
present (e.g. plain `npm run dev` in a browser via `make web-dev`), it stays
in an `available: false` no-op state rather than throwing.

**The static file server** (`qt/src/main.cpp:49-139`, a `StaticServer :
public QTcpServer` subclass) is a small hand-rolled HTTP/1.1 server — not a
separate binary — that exists purely to serve `web/dist` to the
`WebEngineView` without hitting `file://` URL CORS/fetch restrictions in
Chromium. It parses only the request line (no Range support, no keep-alive,
one request per connection), guards against path traversal, and falls back to
`index.html` for extensionless paths (SPA routing). It plays no role in video
delivery — that's exclusively the Go backend's `/api/play`, which mpv hits
directly over its own HTTP client.

## Frontend structure (`web/src/`)

`App.svelte` is the true top-level component (not `web/src/renderer/...`
despite older references — see current tree). Routing is **not** a router:
`currentPage` is a single reactive `$state` holding a discriminated union
(`web/src/lib/types/types.ts`), and every page component is always mounted,
toggled purely via `class:hidden` rather than conditional rendering — this
preserves scroll position and component state across navigation. Pages:
`HomePage` (personalized feed), `QueryPage` (search), `MyListPage` (library),
`SettingsPage`, `InsightsPage` (taste/library stats), `ExplorePage`
(genre browse, no personalization), and `OnboardingPage` (first-run wizard,
rendered as a full-screen overlay rather than a `currentPage` value).

Media detail is a floating overlay, not a page — Netflix-style. `App.svelte`
provides `openMediaDetail`/`watchMedia` via Svelte context; any card
component calls `getContext("openMediaDetail")` to pop open
`MediaExpandedModal` without prop drilling, keyed by media id so switching
titles remounts cleanly.

Stores (`web/src/lib/stores/`): `settings.ts` (writable wrapping the
backend's `Settings`, optimistic save), `auth.svelte.ts` (session/profile
state, Supabase `onAuthStateChange` wiring), `library.ts` (a trivial mutation
counter other components react to, not a cache).

Player-adjacent modules (`web/src/lib/player/`): `player.svelte.ts`
(`MpvPlayer`, the `QWebChannel` bridge described above),
`progressSaver.svelte.ts` (throttled watch-position saves),
`torrentProgress.svelte.ts` (SSE-driven download stats). Not in this
directory but related: `streamSelection.ts` (stream-ranking heuristics).

`web/src/lib/api.ts` is the single point of contact with the backend and
carries three load-bearing mechanisms worth knowing about before touching it:
a concurrency limiter (max 8 in-flight fetches — a full homepage can fire
hundreds of metadata requests at once, and Chromium throws
`ERR_INSUFFICIENT_RESOURCES` past its own cap), request coalescing (an
in-flight `Map` keyed by request signature, so duplicate concurrent GETs
share one promise instead of re-firing), and a single `BASE` URL constant
(overridable via `VITE_API_BASE`) that every other URL-builder in the file
routes through. The torrent progress SSE and the speed test deliberately
bypass the concurrency limiter — they'd hold a slot open indefinitely.

UI components come from **shadcn-svelte** (built on **bits-ui** primitives),
styled with **TailwindCSS 4**. `vidstack` powers `PlayerSimple.svelte`, a
separate lightweight browser-native player used for trailer/preview clips in
cards, carousels, and modals — it's unrelated to the main mpv-backed content
player (`Player.svelte`) described above.

## The OSS/proprietary split

Two packages ship two implementations each, selected at compile time via Go
build tags:

| Package | OSS default (`noop.go`, no tag) | Proprietary (`-tags discover`/`supabase`) |
|---|---|---|
| `internal/discover` | `//go:build !discover`. Personalization rows return `[]`/`{}` unless a custom algorithm URL is configured in Settings (see below); `/api/genres` still works (plain TMDB proxy). | `//go:build discover`, source lives in the `_private/cove-discover` git submodule. Real taste-profile-driven recommendations: genre/keyword/cast-crew scoring with recency decay, re-ranking, and the pluggable custom-algorithm system. |
| `internal/supabase` | `//go:build !supabase`. Every `/api/auth/*` route returns `503`. | `//go:build supabase`, source in `_private/cove-auth`. Real account creation, login, cross-device library sync. |

The proprietary sources are pulled in via `make inject-private`
(`Makefile:100-104`): `git submodule update --init`, then a plain `cp` of
`_private/cove-auth/*.go` into `internal/supabase/` and
`_private/cove-discover/*.go` into `internal/discover/`. The `Makefile`'s
`go` target auto-detects which private files are present
(`Makefile:18-28`, checking for `internal/supabase/client.go` and
`internal/discover/discover.go`) and adds the matching `-tags` automatically
— so `make inject-private && make go` alone is enough; you don't need to
remember the tag names. The copied-in files are gitignored in the main repo
(`internal/discover/discover.go`, `discover_test.go`, `algorithm.go`,
`algorithm_test.go`, and their `supabase` equivalents) — the git submodules
under `_private/` are the actual source of truth, tracked in their own
separate repos.

**Licensing note**: the main repo is AGPL-3.0. The proprietary submodule
files carry their own "All Rights Reserved" copyright header and are
explicitly excluded from the AGPL grant (see the header comment at the top of
each file). Anyone building from a plain `git clone` (without submodule
access) gets a fully-functional AGPL-licensed app with no personalization and
no cloud sync — that's the intended OSS experience, not a degraded trial.

**Worked example — the pluggable discovery algorithm.** This split doesn't
have to mean "OSS users get nothing": `internal/discover`'s "custom
algorithm" feature (an HTTP endpoint the user points Settings at, which
receives a taste profile + a pre-filtered candidate pool and returns
relevance scores) is implemented independently in *both* builds. The
proprietary build sends a real taste profile built from library signals; the
OSS `noop.go` build has no taste-profile machinery at all, so it sends the
same JSON shape with empty profile arrays and a plain TMDB-popularity
candidate pool instead. A single third-party algorithm implementation works
unmodified against either edition — the contract, not the personalization
data, is what's shared.
