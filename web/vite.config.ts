import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { VitePWA } from "vite-plugin-pwa";
import { defineConfig } from "vitest/config";

// The build output is embedded in the Go binary (internal/webui). emptyOutDir would delete the
// committed .gitkeep that keeps the Go package compilable before the first build: re-emit it.
const keepGitkeep = (): Plugin => ({
  name: "keep-gitkeep",
  generateBundle() {
    this.emitFile({ type: "asset", fileName: ".gitkeep", source: "" });
  },
});

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "prompt",
      includeAssets: ["icon.svg"],
      manifest: {
        name: "Biblisann",
        short_name: "Biblisann",
        description: "Self-hosted manager for a physical book library",
        start_url: "/",
        scope: "/",
        display: "standalone",
        background_color: "#ffffff",
        theme_color: "#1f3a5f",
        icons: [
          { src: "icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "icon-512.png", sizes: "512x512", type: "image/png" },
          { src: "icon-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        // Server routes must never be answered by the SPA shell.
        navigateFallbackDenylist: [/^\/v1\//, /^\/openapi\./, /^\/healthz$/, /^\/readyz$/],
        globPatterns: ["**/*.{js,css,html,svg,png,woff2}"],
      },
    }),
    keepGitkeep(),
  ],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    proxy: {
      "/v1": "http://localhost:8080",
      "/openapi.json": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
