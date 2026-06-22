import { defineConfig } from "electron-vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { vite as vidstack } from "vidstack/plugins";
import path from "path";

export default defineConfig({
  // electron-vite v5 externalizes node/npm deps for main & preload by default
  // (the build.externalizeDeps option), so electron-updater stays external
  // without any plugin. Leave it on — don't set externalizeDeps: false.
  main: {},
  preload: {},
  renderer: {
    plugins: [vidstack(), tailwindcss(), svelte()],
    resolve: {
      alias: {
        $lib: path.resolve(__dirname, "./src/lib"),
      },
    },
  },
});
