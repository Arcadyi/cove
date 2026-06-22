import { app, BrowserWindow, ipcMain, shell } from "electron";
import { join, extname, normalize, sep } from "path";
import { createReadStream, statSync } from "fs";
import { spawn, ChildProcess } from "child_process";
import { is } from "@electron-toolkit/utils";
import { autoUpdater } from "electron-updater";
import http from "http";

let goProcess: ChildProcess | null = null;

// In production the renderer is served over http://localhost (see
// startRendererServer) rather than file://, because YouTube embeds (trailers)
// refuse to load from a file:// origin — they require a real HTTP origin.
let rendererURL: string | null = null;

// The Go sidecar is `cove` everywhere except Windows, where the build produces
// `cove.exe`. spawn() needs the exact filename, so resolve it per platform.
const goBinary = process.platform === "win32" ? "cove.exe" : "cove";

function waitForGo(retries = 50): Promise<void> {
  return new Promise((resolve, reject) => {
    const attempt = (): void => {
      http
        .get("http://localhost:6969/api/ping", () => resolve())
        .on("error", () => {
          if (retries-- > 0) {
            setTimeout(attempt, 300);
          } else {
            reject(new Error("Go server did not start"));
          }
        });
    };
    attempt();
  });
}

// Serve the built renderer (out/renderer, inside the asar) over an ephemeral
// http://127.0.0.1 port. Electron's fs reads asar paths transparently, so
// createReadStream works on the packed files. We do this — instead of
// loadFile(file://) — so the renderer has a real HTTP origin, which YouTube
// trailer embeds require (a file:// or custom-scheme origin is rejected).
const MIME: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".wasm": "application/wasm",
};

function startRendererServer(): Promise<string> {
  const root = join(__dirname, "../renderer");

  const server = http.createServer((req, res) => {
    try {
      const urlPath = decodeURIComponent((req.url || "/").split("?")[0]);
      let filePath = normalize(join(root, urlPath));

      // Path-traversal guard: never serve outside the renderer root.
      if (filePath !== root && !filePath.startsWith(root + sep)) {
        res.writeHead(403).end("Forbidden");
        return;
      }

      let info: ReturnType<typeof statSync> | null = null;
      try {
        info = statSync(filePath);
      } catch {
        info = null;
      }

      if (!info || info.isDirectory()) {
        // A real asset that's missing → 404; a route/dir (no extension) → SPA
        // entry. This app loads at "/", so this mostly just serves index.html.
        if (extname(urlPath)) {
          res.writeHead(404).end("Not found");
          return;
        }
        filePath = join(root, "index.html");
      }

      const type = MIME[extname(filePath).toLowerCase()] || "application/octet-stream";
      res.writeHead(200, { "Content-Type": type });
      createReadStream(filePath).pipe(res);
    } catch {
      res.writeHead(500).end("Internal error");
    }
  });

  return new Promise((resolve, reject) => {
    server.on("error", reject);
    // Port 0 → OS picks a free port; any http://localhost origin satisfies YouTube.
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      resolve(`http://localhost:${port}`);
    });
  });
}

function createWindow(): BrowserWindow {
  const mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 1200,
    minHeight: 800,
    frame: false,
    webPreferences: {
      preload: join(__dirname, "../preload/index.js"),
      sandbox: false,
      autoplayPolicy: "no-user-gesture-required",
    },
  });

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url);
    return { action: "deny" };
  });

  mainWindow.webContents.session.webRequest.onHeadersReceived(
    (details, callback) => {
      callback({
        responseHeaders: {
          ...details.responseHeaders,
          "Cross-Origin-Resource-Policy": ["cross-origin"],
          "Cross-Origin-Embedder-Policy": ["unsafe-none"],
        },
      });
    },
  );

  ipcMain.on("window-minimize", () => {
    mainWindow.minimize();
  });

  ipcMain.on("window-maximize", () => {
    if (mainWindow.isMaximized()) {
      mainWindow.unmaximize();
    } else {
      mainWindow.maximize();
    }
  });

  ipcMain.on("window-close", () => {
    mainWindow.close();
  });

  mainWindow.webContents.on("before-input-event", (event, input) => {
    if (input.type === "keyDown" && input.key === "F12") {
      mainWindow.webContents.toggleDevTools();
      event.preventDefault();
    }
  });

  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    mainWindow.loadURL(process.env["ELECTRON_RENDERER_URL"]).then(() => {
      console.log("[COVE:ELECTRON]: Renderer URL Loaded");
    });
  } else if (rendererURL) {
    // Production: served over http://localhost so YouTube embeds work.
    mainWindow.loadURL(rendererURL).then(() => {
      console.log("[COVE:ELECTRON]: Renderer served at", rendererURL);
    });
  } else {
    // Last-resort fallback (shouldn't happen if the server started).
    mainWindow.loadFile(join(__dirname, "../renderer/index.html")).then(() => {
      console.log("[COVE:ELECTRON]: Index Loaded (file://)");
    });
  }

  return mainWindow;
}

// ── Auto-update (electron-updater → GitHub releases) ──────────────────────────
// The feed itself is configured at build time via electron-builder's `publish`
// block (provider: github), which bakes an app-update.yml into the package — so
// there's nothing to set here beyond kicking off the check and surfacing
// progress to the renderer for the custom titlebar UI.
function setupAutoUpdates(win: BrowserWindow): void {
  // Register IPC handlers unconditionally so the renderer's invoke/send never
  // hit "no handler registered" — in dev they're simply no-ops.
  ipcMain.handle("update:check", () => {
    if (is.dev) return null;
    return autoUpdater.checkForUpdates();
  });
  ipcMain.on("update:install", () => {
    if (is.dev) return;
    // Kill the Go sidecar first so its binary isn't locked while the installer
    // replaces the app's files — on Windows a running cove.exe blocks the swap.
    goProcess?.kill();
    autoUpdater.quitAndInstall();
  });

  // In dev there's no packaged app, so electron-updater throws
  // ("application is not packed and dev update config is not forced"). The
  // handlers above stay registered, but skip the real update machinery.
  if (is.dev) {
    return;
  }

  autoUpdater.autoDownload = true; // download as soon as an update is found
  autoUpdater.autoInstallOnAppQuit = true; // also apply silently on a normal quit

  const send = (channel: string, payload?: unknown): void => {
    if (!win.isDestroyed()) {
      win.webContents.send(channel, payload);
    }
  };

  autoUpdater.on("checking-for-update", () => send("update:checking"));
  autoUpdater.on("update-available", (info) =>
    send("update:available", { version: info.version }),
  );
  autoUpdater.on("update-not-available", () => send("update:none"));
  autoUpdater.on("download-progress", (p) =>
    send("update:progress", {
      percent: p.percent,
      transferred: p.transferred,
      total: p.total,
      bytesPerSecond: p.bytesPerSecond,
    }),
  );
  autoUpdater.on("update-downloaded", (info) =>
    send("update:downloaded", { version: info.version }),
  );
  autoUpdater.on("error", (err) =>
    send("update:error", {
      message: err == null ? "unknown error" : (err.message ?? String(err)),
    }),
  );

  // Initial check shortly after launch (don't block first paint), then poll
  // every 6 hours so long-running sessions still pick up releases.
  const check = (): void => {
    autoUpdater
      .checkForUpdates()
      .catch((e) => console.error("[update] check failed:", e));
  };
  setTimeout(check, 3000);
  setInterval(check, 6 * 60 * 60 * 1000);
}

app.whenReady().then(async () => {
  if (is.dev) {
    goProcess = spawn(join(__dirname, "../../..", goBinary));
    goProcess.stderr?.on("data", (d) => console.error("[go]", d.toString()));
    goProcess.stdout?.on("data", (d) => console.log("[go]", d.toString()));
  } else {
    const binaryPath = join(process.resourcesPath, goBinary);
    goProcess = spawn(binaryPath);
  }

  try {
    await waitForGo();
    if (!is.dev) {
      rendererURL = await startRendererServer();
    }
    const mainWindow = createWindow();
    setupAutoUpdates(mainWindow);
  } catch (error) {
    console.error("Initialization failed:", error);
    app.quit();
  }
});

app.on("quit", () => goProcess?.kill());
