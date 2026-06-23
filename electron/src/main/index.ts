import { app, BrowserWindow, ipcMain, shell } from "electron";
import { join, extname, normalize, sep } from "path";
import { createReadStream, statSync, createWriteStream, existsSync } from "fs";
import type { WriteStream } from "fs";
import { spawn, ChildProcess } from "child_process";
import { is } from "@electron-toolkit/utils";
import { autoUpdater } from "electron-updater";
import http from "http";

// ffmpeg/ffprobe binaries via @ffmpeg-installer / @ffprobe-installer — recent,
// per-platform static builds. (ffprobe-static's bundled Linux binary segfaulted
// even with a cleaned environment.) Both export { path }; we require + cast to
// avoid depending on bundled type declarations. This file is CommonJS (it uses
// __dirname), so require is available at runtime.
const ffmpegInstaller = require("@ffmpeg-installer/ffmpeg") as { path: string };
const ffprobeInstaller = require("@ffprobe-installer/ffprobe") as {
  path: string;
};

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

// Resolve the bundled ffmpeg/ffprobe binaries and hand them to the Go sidecar
// via env. Both installer packages expose { path }. In the packaged app these
// resolve to a path inside the asar, so remap to app.asar.unpacked (where
// asarUnpack actually extracts them); in dev the path already points at
// node_modules and the replace is a harmless no-op. If a binary is somehow
// unavailable we just omit the var and Go falls back to PATH.
// When packaged as an AppImage, AppRun injects LD_LIBRARY_PATH / LD_PRELOAD
// pointing at the AppImage's bundled libraries and stashes the originals in
// *_ORIG. Anything we spawn (the Go sidecar, and the ffmpeg/ffprobe it execs)
// inherits those and loads mismatched host libs — which makes the bundled
// ffmpeg/ffprobe segfault. Restore the original values (or drop the vars) so
// child processes run against a clean, host environment. No-op off AppImage.
function stripAppImageEnv(env: NodeJS.ProcessEnv): void {
  if (!process.env.APPIMAGE) return;
  for (const key of ["LD_LIBRARY_PATH", "LD_PRELOAD"]) {
    const orig = env[`${key}_ORIG`];
    if (orig !== undefined) env[key] = orig;
    else delete env[key];
  }
}

function goSpawnEnv(): NodeJS.ProcessEnv {
  const env = { ...process.env };
  const ffmpeg = ffmpegInstaller?.path as string | undefined;
  const ffprobe = ffprobeInstaller?.path as string | undefined;
  if (ffmpeg) env.FFMPEG_PATH = ffmpeg.replace("app.asar", "app.asar.unpacked");
  if (ffprobe)
    env.FFPROBE_PATH = ffprobe.replace("app.asar", "app.asar.unpacked");
  stripAppImageEnv(env);
  return env;
}

// Backend logging. The packaged app previously discarded the Go sidecar's
// stdout/stderr, so ffmpeg failures were invisible. We now tee everything to a
// log file under userData (~/.config/cove on Linux, %AppData%\cove on Windows)
// and to the console, so a terminal-launched build shows it live and a
// GUI-launched one leaves a readable trail.
let backendLog: WriteStream | null = null;

function openBackendLog(): string {
  const p = join(app.getPath("userData"), "cove-backend.log");
  try {
    backendLog = createWriteStream(p, { flags: "a" });
    backendLog.write(`\n--- session ${new Date().toISOString()} ---\n`);
  } catch (e) {
    console.error("[main] could not open backend log:", e);
  }
  return p;
}

function logMain(line: string): void {
  console.log(line);
  backendLog?.write(line + "\n");
}

function attachGoLogging(proc: ChildProcess): void {
  proc.stderr?.on("data", (d) => {
    const s = d.toString();
    console.error("[go]", s);
    backendLog?.write(s);
  });
  proc.stdout?.on("data", (d) => {
    const s = d.toString();
    console.log("[go]", s);
    backendLog?.write(s);
  });
}

app.whenReady().then(async () => {
  const logPath = openBackendLog();
  logMain(`[main] backend log: ${logPath}`);

  const env = goSpawnEnv();
  logMain(
    `[main] APPIMAGE=${process.env.APPIMAGE ?? "(no)"} ` +
    `child LD_LIBRARY_PATH=${env.LD_LIBRARY_PATH ?? "(cleared)"}`,
  );
  // Verify the bundled binaries actually resolved to a real file — a wrong or
  // missing path here is the prime suspect for "stream never starts".
  logMain(
    `[main] FFMPEG_PATH=${env.FFMPEG_PATH ?? "(unset → PATH)"} exists=${
      env.FFMPEG_PATH ? existsSync(env.FFMPEG_PATH) : false
    }`,
  );
  logMain(
    `[main] FFPROBE_PATH=${env.FFPROBE_PATH ?? "(unset → PATH)"} exists=${
      env.FFPROBE_PATH ? existsSync(env.FFPROBE_PATH) : false
    }`,
  );

  if (is.dev) {
    goProcess = spawn(join(__dirname, "../../..", goBinary), { env });
  } else {
    const binaryPath = join(process.resourcesPath, goBinary);
    goProcess = spawn(binaryPath, { env });
  }
  attachGoLogging(goProcess);

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
