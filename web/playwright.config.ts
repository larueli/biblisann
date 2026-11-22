import { defineConfig } from "@playwright/test";

// Runs against a built binary: BIBLISANN_E2E_URL (default http://localhost:8080).
export default defineConfig({
  testDir: "e2e",
  use: { baseURL: process.env.BIBLISANN_E2E_URL ?? "http://localhost:8080" },
  reporter: [["list"]],
});
