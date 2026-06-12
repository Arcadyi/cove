import { app, BrowserWindow, ipcMain, shell } from "electron";
import { join } from "path";
import { spawn, ChildProcess } from "child_process";
import { is } from "@electron-toolkit/utils";
import http from "http";

let goProcess: ChildProcess | null = null;

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

function createWindow(): void {
  const mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
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
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html")).then(() => {
      console.log("[COVE:ELECTRON]: Index Loaded");
    });
  }
}

app.whenReady().then(async () => {
  if (is.dev) {
    goProcess = spawn(join(__dirname, "../../..", "cove"));
    goProcess.stderr?.on("data", (d) => console.error("[go]", d.toString()));
    goProcess.stdout?.on("data", (d) => console.log("[go]", d.toString()));
  } else {
    const binaryPath = join(process.resourcesPath, "cove");
    goProcess = spawn(binaryPath);
  }

  try {
    await waitForGo();
    createWindow();
  } catch (error) {
    console.error("Initialization failed:", error);
    app.quit();
  }
});

app.on("quit", () => goProcess?.kill());
