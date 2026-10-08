import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";
import { writeFileSync } from "node:fs";
import { join } from "node:path";

// The dashboard is built into the Go binary (internal/api/dist) and served by
// `roadpass serve`; in development Vite proxies the API to it.
const api = process.env.API_TARGET ?? "http://127.0.0.1:8090";

export default defineConfig({
  plugins: [
    react(),
    tailwind(),
    // The output dir is gitignored except this placeholder, which keeps
    // `//go:embed all:dist` compiling in a fresh clone; emptyOutDir removes it.
    { name: "keep-embed-dir", writeBundle: (out) => { if (out.dir) writeFileSync(join(out.dir, ".keep"), ""); } },
  ],
  build: {
    outDir: "../internal/api/dist",
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes("node_modules/react") || id.includes("node_modules/scheduler")) return "react";
          if (id.includes("node_modules/d3") || id.includes("node_modules/topojson") || id.includes("es-atlas")) return "viz";
          if (id.includes("node_modules/@radix-ui")) return "ui";
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5173,
    // API_TARGET=http://localhost:8080 proxies to the compose stack instead of the native server.
    proxy: {
      "/api": { target: api, changeOrigin: false },
      "/ngsi-ld": { target: api, changeOrigin: false },
      "/healthz": api,
    },
  },
});
