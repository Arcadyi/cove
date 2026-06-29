# Cove

A media streaming desktop app. Discover, track, and stream movies and TV shows using TMDB metadata and Stremio-compatible addons.

## Install

### Flatpak — any Linux distro

Download `cove-linux-amd64.flatpak` from the [latest release](https://github.com/Arcadyi/cove/releases/latest), then:

```sh
flatpak install --user cove-linux-amd64.flatpak
flatpak run io.github.arcadyi.Cove
```

### Arch / CachyOS (PKGBUILD)

Download `PKGBUILD` from the [latest release](https://github.com/Arcadyi/cove/releases/latest), then:

```sh
# in any empty directory
curl -LO https://github.com/Arcadyi/cove/releases/latest/download/PKGBUILD
makepkg -si
```

```shell
# to download and install automatically
cd "$(mktemp -d)" && curl -LO https://github.com/Arcadyi/cove/releases/latest/download/PKGBUILD && makepkg -si
```

This installs Cove as a native pacman package. Update it the same way — download the new `PKGBUILD` from the next release and run `makepkg -si` again.