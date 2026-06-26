#!/usr/bin/env bash
# Cove hot-reload dev loop.
#
# Brings up the Vite dev server, then launches the Qt shell with --dev so the
# WebEngineView loads the UI from Vite (with HMR) instead of the built web/dist.
# Frontend edits hot-reload in-window, like the old Electron setup.
#
# The shell spawns the Go backend itself (--backend) and waits for it on :6969
# before navigating, so we only need Vite up first — the WebEngineView does not
# retry a failed initial load, hence the wait below.
#
# Prereqs (built by `make hot`): ./cove (Go) and qt/build/cove_shell (shell).
# Also requires the stripCspInDev() plugin in vite.config.ts, otherwise the
# production CSP blocks Vite's HMR websocket and nothing live-reloads.
#
# Place this at scripts/dev-hot.sh (the path math below assumes that).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT/web"
SHELL_BIN="$ROOT/qt/build/cove_shell"
GO_BIN="$ROOT/cove"
VITE_PORT="${VITE_PORT:-5173}"

for bin in "$SHELL_BIN" "$GO_BIN"; do
  [[ -x "$bin" ]] || {
    echo "[dev-hot] missing $bin — run 'make hot' (or 'make go qt') first" >&2
    exit 1
  }
done

# Start Vite in its own session so we can reliably kill it *and* its node child
# on exit (npm doesn't forward signals dependably).
setsid bash -c 'cd "$1" && exec npm run dev' _ "$WEB_DIR" &
VITE_PID=$!

cleanup() {
  # Kill the whole Vite process group; fall back to the single pid.
  kill -- -"$VITE_PID" 2>/dev/null || kill "$VITE_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for Vite to listen. Checked via `ss` so it doesn't matter which address
# family Vite bound — Vite 7 / Node 17+ resolve `localhost` to IPv6 ::1, so an
# IPv4-only probe would spin forever. Fall back to a localhost /dev/tcp connect
# (which tries the resolved family) if ss isn't available.
echo "[dev-hot] waiting for Vite on :$VITE_PORT ..."
port_open() {
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | grep -qE ":${VITE_PORT}\b"
  else
    (exec 3<>"/dev/tcp/localhost/${VITE_PORT}") 2>/dev/null && exec 3>&- || return 1
  fi
}
until port_open; do
  kill -0 "$VITE_PID" 2>/dev/null || {
    echo "[dev-hot] Vite exited during startup" >&2
    exit 1
  }
  sleep 0.2
done
echo "[dev-hot] Vite up — launching shell (--dev)"

# Foreground. web/dist need not exist: in --dev the shell still starts its
# StaticServer but navigates to Vite instead, so the static root is unused.
# When the shell exits, the EXIT trap stops Vite.
"$SHELL_BIN" --dev --backend "$GO_BIN" --webroot "$WEB_DIR/dist"