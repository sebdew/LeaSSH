import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { proxy: { "/api": "http://localhost:8080", "/mcp": "http://localhost:8080" } },
  build: { outDir: "dist", sourcemap: false, assetsInlineLimit: 0 }, // pas de data: URI (CSP stricte)
  test: { environment: "jsdom", setupFiles: ["./src/test-setup.ts"], globals: true, exclude: ["e2e/**", "node_modules/**"] },
});
