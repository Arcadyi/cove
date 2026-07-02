import { defineConfig, type Plugin } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { vite as vidstack } from "vidstack/plugins";
import path from "path";

// The production CSP (in index.html) locks connect-src down to the Go
// backend and a handful of known third parties — it doesn't allow the Vite
// dev server's HMR websocket (ws://localhost:5173), so in `vite dev` mode we
// strip the meta tag entirely rather than try to keep two policies in sync.
// Only active for `vite dev` (apply: "serve"); production builds are
// untouched and keep the CSP.
function stripCspInDev(): Plugin {
  return {
    name: "strip-csp-in-dev",
    apply: "serve",
    transformIndexHtml(html) {
      return html.replace(
        /\s*<meta\s+http-equiv="Content-Security-Policy"[\s\S]*?\/>\s*\n/,
        "\n",
      );
    },
  };
}

// Plain Vite build for the Cove frontend. The Qt shell (cove_shell) serves the
// built `dist/` over its StaticServer and loads it in QtWebEngine; the Go
// backend is spawned separately by the shell. Nothing Electron remains.
export default defineConfig({
  plugins: [vidstack(), tailwindcss(), svelte(), stripCspInDev()],
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
