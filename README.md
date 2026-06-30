<div align="center">
  <img src="web/src/assets/CoveIcon.svg" alt="Cove" width="120" />

  # Cove

  A media streaming desktop app for Linux. Discover, track, and stream movies and TV shows — powered by TMDB metadata, Stremio-compatible addons, and a built-in mpv player.

  [![CI](https://github.com/Arcadyi/cove/actions/workflows/release.yml/badge.svg)](https://github.com/Arcadyi/cove/actions/workflows/release.yml)
  [![Latest Release](https://img.shields.io/github/v/release/Arcadyi/cove?label=release)](https://github.com/Arcadyi/cove/releases/latest)
  [![Platform](https://img.shields.io/badge/platform-Linux-blue?logo=linux&logoColor=white)](https://github.com/Arcadyi/cove/releases/latest)
  [![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
  [![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00?logo=svelte&logoColor=white)](https://svelte.dev)
  [![Qt](https://img.shields.io/badge/Qt-6-41CD52?logo=qt&logoColor=white)](https://www.qt.io)
</div>

## Features

- **Stream anything** — connects to Stremio-compatible addon sources (torrentio, etc.) and streams directly in the app
- **Built-in player** — hardware-accelerated mpv playback with subtitle support and progress saving
- **Discover** — personalized recommendations based on your watch history, ratings, and taste profile
- **Library** — track what you've watched, mark favorites, and pick up where you left off with continue watching
- **Explore** — browse trending, upcoming releases, genres, and curated categories
- **Insights** — view your watch stats and genre/actor taste breakdown
- **Search** — find any movie or TV show by title
- **Accounts & sync** — optional sign-in syncs your library and preferences across devices.

## Install

### Flatpak — any Linux distro

Download `cove-linux-amd64.flatpak` from the [latest release](https://github.com/Arcadyi/cove/releases/latest), then:

```sh
flatpak install --user cove-linux-amd64.flatpak
flatpak run io.github.arcadyi.Cove
```

### Arch / CachyOS (PKGBUILD)
One-Liner: Download and install
```sh
cd "$(mktemp -d)" && curl -LO https://github.com/Arcadyi/cove/releases/latest/download/PKGBUILD && makepkg -si
```

Or download `PKGBUILD` from the [latest release](https://github.com/Arcadyi/cove/releases/latest) manually and run `makepkg -si` in the same directory. To update, repeat with the new release's `PKGBUILD`.

## Build from source

**Prerequisites:** Go 1.21+, Node.js 20+, Qt 6 with QtWebEngine, libmpv, cmake

```sh
git clone https://github.com/Arcadyi/cove
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

Addon URLs can be configured in the app's Settings page. The default setup includes some built-in addons but provides no streams apart from official sources.