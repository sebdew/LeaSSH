import { readFileSync } from "node:fs";
import { AxeBuilder } from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

// Balayage automatique de tous les écrans : thème clair/sombre × bureau/mobile.
// Les routes sont lues dans src/App.tsx : toute page ajoutée est testée sans toucher à ce fichier.
// Projet « balayage » : il passe après « parcours » (compte administrateur) et « fonctionnel » (données créées).
const admin = { user: "admin", password: "Correct-Horse-Battery-9" };

// Routes à paramètre : une URL d'exemple est obligatoire (le test échoue tant qu'elle manque).
// Les pages affichent un état « introuvable » propre quand l'identifiant n'existe pas.
const exemples: Record<string, string> = {
  "/commandes/$id": "/commandes/inexistante",
  "/propositions/$id": "/propositions/inexistante",
  "/propositions/$id/modifier": "/propositions/inexistante/modifier",
};

function routesDeclarees(): string[] {
  const src = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
  const paths = new Set<string>(["/"]);
  for (const m of src.matchAll(/(?:path:\s*|page\()"(\/[^"]*)"/g)) paths.add(m[1]);
  return [...paths];
}

const routes = routesDeclarees();
const viewports = [
  { nom: "bureau", size: { width: 1280, height: 800 } },
  { nom: "mobile", size: { width: 390, height: 844 } },
] as const;
const themes = ["light", "dark"] as const;

async function login(page: Page) {
  await page.goto("/");
  await page.getByLabel("Nom d'utilisateur").fill(admin.user);
  await page.getByLabel("Mot de passe").fill(admin.password);
  await page.getByRole("button", { name: "Se connecter" }).click();
  await expect(page.getByRole("link", { name: "Clés MCP" })).toBeVisible();
}

test("le balayage a bien trouvé les routes de l'application", () => {
  expect(routes.length).toBeGreaterThanOrEqual(15);
  for (const r of routes.filter((p) => p.includes("$"))) {
    expect(exemples[r], `route à paramètre « ${r} » : ajouter une URL d'exemple dans tous-les-ecrans.spec.ts`).toBeTruthy();
  }
});

for (const viewport of viewports) {
  for (const theme of themes) {
    test(`écrans — ${viewport.nom}, thème ${theme}`, async ({ page }, testInfo) => {
      test.setTimeout(180_000);
      await page.setViewportSize(viewport.size);
      await page.addInitScript((t) => localStorage.setItem("leassh-theme", t), theme);
      const problemes: string[] = [];
      page.on("console", (m) => { if (m.type() === "error") problemes.push(`console: ${m.text()}`); });
      page.on("pageerror", (e) => problemes.push(`exception: ${e.message}`));
      page.on("response", (r) => { if (r.status() >= 500) problemes.push(`HTTP ${r.status()}: ${r.url()}`); });
      await login(page);

      for (const route of routes) {
        const url = exemples[route] ?? route;
        problemes.length = 0;
        await page.goto(url);
        await page.waitForLoadState("networkidle");
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);

        // Page non blanche et sans débordement horizontal.
        const main = page.locator("main, #root").first();
        expect.soft((await main.innerText()).trim().length, `${url} : page vide`).toBeGreaterThan(0);
        const debord = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        expect.soft(debord, `${url} : débordement horizontal de ${debord}px`).toBeLessThanOrEqual(1);

        // Accessibilité et contrastes (WCAG 2 A/AA).
        const axe = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze();
        const violations = axe.violations.map((v) => `${v.id} (${v.nodes.length}) ${v.nodes[0]?.target.join(" ")}`);
        expect.soft(violations, `${url} : accessibilité`).toEqual([]);

        // Aucune erreur console / réseau 5xx.
        // Un identifiant inexistant provoque légitimement un 404 sur l'API : toléré pour les URLs d'exemple.
        const retenus = route in exemples ? problemes.filter((p) => !p.includes("status of 404")) : problemes;
        expect.soft(retenus, `${url} : erreurs`).toEqual([]);

        // Capture jointe au rapport pour relecture éventuelle.
        const nom = `${viewport.nom}-${theme}${url.replace(/\//g, "_") || "_"}.png`;
        await testInfo.attach(nom, { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
      }
    });
  }
}
