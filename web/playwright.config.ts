import { defineConfig, devices } from "@playwright/test";

// Parcours de bout en bout : `make e2e` (construit le binaire, lance Chromium).
export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.spec.ts",
  fullyParallel: false,
  workers: 1, // les parcours partagent une même instance, créée neuve à chaque lancement
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: "http://127.0.0.1:18080", locale: "fr-FR", trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: { command: "node e2e/serve.mjs", url: "http://127.0.0.1:18080/health", reuseExistingServer: false, timeout: 60_000 },
});
