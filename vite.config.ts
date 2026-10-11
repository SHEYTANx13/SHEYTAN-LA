import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";

// v1.3.6 (spec §29/§30): BUILD-TIME CANONICAL FRONTEND VERSION.
// package.json is the single release version source (the same source
// scripts/release-version.mjs syncs into internal/config, build/config.yml
// and SIGNATURE). The value is injected as __APP_VERSION__ at build time;
// the backend's runtime appVersion (/api/state → config.AppVersion) stays
// authoritative once loaded, and first paint shows the SAME canonical
// version instead of a stale hard-coded literal.
const pkg = JSON.parse(readFileSync(new URL("./package.json", import.meta.url), "utf-8")) as {
  version: string;
};

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8765",
        changeOrigin: false,
      },
      "/ws": {
        target: "ws://127.0.0.1:8765",
        ws: true,
        changeOrigin: false,
      },
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 4173,
    strictPort: true,
  },
  build: {
    target: "es2022",
    sourcemap: mode !== "production",
    // Manifest generation is disabled — no runtime consumer exists (the
    // embedded index.html references the stable assets directly; the
    // static-asset contract is verified by scripts/verify-static-assets.mjs).
    manifest: false,
    rollupOptions: {
      output: {
        // STABLE FILENAME CONTRACT: production assets are named after
        // their source module with NO content hash (index.js,
        // AgentBody.js, index.css, ...). The same physical file keeps the
        // same name across builds, so the embedded tree can be audited,
        // diffed and navigated without rebuilding. Code splitting stays
        // enabled — every lazy chunk is also stable.
        entryFileNames: "assets/[name].js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
}));
