import { readFileSync } from "node:fs";
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

// Interactive API documentation viewers, copied from their npm packages into the build so that the
// server hosts everything itself (no third-party CDN at runtime). See docs/dev/api-docs.md.
const apiDocViewers: [from: string, to: string][] = [
  ["swagger-ui-dist/swagger-ui-bundle.js", "swagger-ui-bundle.js"],
  ["swagger-ui-dist/swagger-ui.css", "swagger-ui.css"],
  ["redoc/bundles/redoc.standalone.js", "redoc.standalone.js"],
  ["@stoplight/elements/web-components.min.js", "elements.js"],
  ["@stoplight/elements/styles.min.css", "elements.css"],
  ["@scalar/api-reference/dist/browser/standalone.js", "scalar.js"],
];
const vendorApiDocs = (): Plugin => ({
  name: "vendor-api-docs",
  generateBundle() {
    for (const [from, to] of apiDocViewers) {
      this.emitFile({
        type: "asset",
        fileName: `docs/vendor/${to}`,
        source: readFileSync(`node_modules/${from}`),
      });
    }
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
        navigateFallbackDenylist: [
          /^\/v1\//,
          /^\/openapi/,
          /^\/docs/,
          /^\/mcp/,
          /^\/oauth\/(token|register)/,
          /^\/\.well-known\//,
          /^\/scim\//,
          /^\/opds\//,
          /^\/(healthz|livez|readyz|startupz|health|metrics)$/,
        ],
        globPatterns: ["**/*.{js,css,html,svg,png,woff2}"],
        globIgnores: ["docs/**"], // the API viewers are large and not part of the app
      },
    }),
    keepGitkeep(),
    vendorApiDocs(),
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
