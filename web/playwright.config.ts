import { defineConfig, devices } from "@playwright/test";

// Parcours de bout en bout : `make e2e` (construit le binaire, lance Chromium).
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1, // les parcours partagent une même instance, créée neuve à chaque lancement
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: "http://127.0.0.1:18080", locale: "fr-FR", trace: "retain-on-failure" },
  // Ordre explicite : compte administrateur → parcours fonctionnels (données) → balayage de tous les écrans remplis.
  projects: [
    { name: "parcours", testMatch: "parcours.spec.ts", use: { ...devices["Desktop Chrome"] } },
    { name: "fonctionnel", testMatch: "fonctionnel.spec.ts", dependencies: ["parcours"], use: { ...devices["Desktop Chrome"] } },
    { name: "balayage", testMatch: "tous-les-ecrans.spec.ts", dependencies: ["fonctionnel"], use: { ...devices["Desktop Chrome"] } },
  ],
  webServer: { command: "node e2e/serve.mjs", url: "http://127.0.0.1:18080/health", reuseExistingServer: false, timeout: 60_000 },
});
