import { writable } from "svelte/store";

// Increment this to signal that library data has changed.
// Any component that mutates the library should call:
//   libraryChanged.update((n) => n + 1);
export const libraryChanged = writable(0);
