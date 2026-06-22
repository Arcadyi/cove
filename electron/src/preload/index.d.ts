import { ElectronAPI } from "@electron-toolkit/preload";

interface UpdatesApi {
  /** Trigger an on-demand update check. */
  check: () => Promise<unknown>;
  /** Quit and apply the downloaded update (restarts the app). */
  install: () => void;
  /**
   * Subscribe to an update channel (e.g. "update:downloaded",
   * "update:progress"). Returns an unsubscribe function.
   */
  on: (channel: string, cb: (payload: unknown) => void) => () => void;
}

declare global {
  interface Window {
    electron: ElectronAPI;
    api: unknown;
    updates: UpdatesApi;
  }
}
export {};
