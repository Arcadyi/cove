/// <reference types="svelte" />
/// <reference types="vite/client" />

// The frontend runs inside the Qt shell (no Electron). TopBar still references
// window.electron for the old OS window controls, guarded with optional
// chaining; declare it as optional so that access type-checks and is undefined
// at runtime in the shell.
declare global {
  interface Window {
    electron?: {
      ipcRenderer?: {
        send: (channel: string, ...args: unknown[]) => void;
      };
    };
  }
}

export {};
