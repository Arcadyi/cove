<div align="center">
  <img src="web/src/assets/CoveIcon.svg" alt="Cove" width="120" />

  # Cove

  A media streaming desktop app for Linux and Windows. Discover, track, and stream movies and TV shows — powered by TMDB metadata, Stremio-compatible addons, and a built-in mpv player.

  [![CI](https://github.com/coveninja/cove/actions/workflows/release.yml/badge.svg)](https://github.com/coveninja/cove/actions/workflows/release.yml)
  [![Latest Release](https://img.shields.io/github/v/release/coveninja/cove?label=release)](https://github.com/coveninja/cove/releases/latest)
  [![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
  [![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
  [![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00?logo=svelte&logoColor=white)](https://svelte.dev)
  [![Qt](https://img.shields.io/badge/Qt-6-41CD52?logo=qt&logoColor=white)](https://www.qt.io)
</div>

## Features

- **Stream anything** — connects to Stremio-compatible addon sources and streams directly in the app
- **Extend with plugins** — add community JS scraper plugins for additional stream sources, sandboxed and opt-in per scraper
- **Built-in player** — hardware-accelerated mpv playback with subtitle support, live buffering/download progress, and progress saving
- **Smart stream picker** — auto-selects the best available stream using a configurable strategy (quality, size, reliability, or a connection-speed match via a built-in speed test), or sort/filter candidates yourself
- **Skip intro & recap** — auto-skip buttons for intro, recap, and credits segments during playback, independently toggleable
- **Where to watch** — see which legal streaming/rental services carry a title alongside the stream picker
- **Discover** — personalized recommendations based on your watch history, ratings, and taste profile
- **Library** — track what you've watched, mark favorites, and pick up where you left off with continue watching
- **Explore** — browse trending, upcoming releases, genres, and curated categories
- **Insights** — view your watch stats and genre/actor taste breakdown
- **Search** — find any movie or TV show by title
- **Spoiler-free browsing** — optionally blurs thumbnails and titles for unwatched episodes
- **Multiple profiles** — local, Netflix-style profile switching, works fully offline with no sign-in required
- **Accounts & sync** — optional sign-in syncs your library and preferences across devices

## Install

### Arch / CachyOS (PKGBUILD)

One-liner install:

```sh
cd "$(mktemp -d)" && curl -LO https://github.com/coveninja/cove/releases/latest/download/PKGBUILD && makepkg -si
```

Or download `PKGBUILD` from the [latest release](https://github.com/coveninja/cove/releases/latest) manually and run `makepkg -si` in the same directory. To update, repeat with the new release's `PKGBUILD`.

### Flatpak — any Linux distro

Download `cove-linux-amd64.flatpak` from the [latest release](https://github.com/coveninja/cove/releases/latest), then:

```sh
flatpak install --user cove-linux-amd64.flatpak
flatpak run io.github.coveninja.Cove
```

### Windows

Download `cove-windows-amd64-setup.exe` from the [latest release](https://github.com/coveninja/cove/releases/latest) and run the installer. Or grab `cove-windows-amd64.zip` for a portable install.

## Build from source

**Prerequisites:** Go 1.21+, Node.js 20+, Qt 6 with QtWebEngine, libmpv, cmake

```sh
git clone https://github.com/coveninja/cove
cd cove
echo "TMDB_API_KEY=your_key_here" > .env
make run  # builds everything and launches the app
```

### Development

```sh
make hot        # hot-reload: Vite HMR in-window, rebuilds Go + Qt on changes
make hot-debug  # same + QtWebEngine remote devtools on :9222
make web-dev    # browser-only Vite dev server (player shows "unavailable")
```

### Individual builds

```sh
make go      # build the Go backend binary
make web     # build the Svelte frontend
make qt      # build the Qt shell
make dev     # full build + regenerate TypeScript types from Go structs
```

### Frontend checks

```sh
cd web
npm run check   # svelte-check
npm run lint    # eslint
npm run format  # prettier
```

## Configuration

A fresh profile ships with no provider addons and no plugin repos enabled — only two hardcoded "official" integrations (streaming-availability lookup, intro/recap timestamps) work out of the box. To get streams, add one or more Stremio-compatible addon URLs and/or community plugin repos in the app's Settings page.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — how the Go backend, Svelte frontend, and Qt shell fit together, the playback data flow, and the open-source/proprietary build-tag split
- [docs/API.md](docs/API.md) — HTTP endpoint reference
- [CONTRIBUTING.md](CONTRIBUTING.md) — dev setup and code style for contributors
