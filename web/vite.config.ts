import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { vite as vidstack } from "vidstack/plugins";
import path from "path";

// Plain Vite build for the Cove frontend. The Qt shell (cove_shell) serves the
// built `dist/` over its StaticServer and loads it in QtWebEngine; the Go
// backend is spawned separately by the shell. Nothing Electron remains.
export default defineConfig({
  plugins: [vidstack(), tailwindcss(), svelte()],
  resolve: {
    alias: {
      $lib: path.resolve(__dirname, "./src/lib"),
    },
  },
  // Relative asset URLs so the bundle works regardless of the StaticServer's
  // mount, including the file:// / ephemeral-port cases.
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  // Dev server (browser): the bridge is absent here, so the player shows
  // "unavailable", but the rest of the UI works against the Go backend.
  server: {
    port: 5173,
  },
});
