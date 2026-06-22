import { contextBridge, ipcRenderer } from "electron";
import { electronAPI } from "@electron-toolkit/preload";

// Custom APIs for renderer
const api = {};

// Use `contextBridge` APIs to expose Electron APIs to
// renderer only if context isolation is enabled, otherwise
// just add to the DOM global.
if (process.contextIsolated) {
  try {
    contextBridge.exposeInMainWorld("electron", electronAPI);
    contextBridge.exposeInMainWorld("api", api);
    contextBridge.exposeInMainWorld("updates", {
      check: () => ipcRenderer.invoke("update:check"),
      install: () => ipcRenderer.send("update:install"),
      on: (channel: string, cb: (payload: unknown) => void) => {
        const fn = (_: unknown, p: unknown) => cb(p);
        ipcRenderer.on(channel, fn);
        return () => ipcRenderer.removeListener(channel, fn);
      },
    });
  } catch (error) {
    console.error(error);
  }
} else {
  // @ts-ignore (define in dts)
  window.electron = electronAPI;
  // @ts-ignore (define in dts)
  window.api = api;
}
