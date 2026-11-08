import { expect, test, type Page } from "@playwright/test";

// Les textes sont ceux de l'interface en français (locale fr-FR du navigateur de test).
const admin = { user: "admin", password: "Correct-Horse-Battery-9" };
const keyName = "Agent e2e";

async function login(page: Page) {
  await page.goto("/");
  await page.getByLabel("Nom d'utilisateur").fill(admin.user);
  await page.getByLabel("Mot de passe").fill(admin.password);
  await page.getByRole("button", { name: "Se connecter" }).click();
  await expect(page.getByRole("link", { name: "Clés MCP" })).toBeVisible();
}

test.describe.serial("parcours principaux", () => {
  test("initialisation puis connexion", async ({ page, browser }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Bienvenue dans Leassh" })).toBeVisible();
    await page.getByLabel("Jeton d'initialisation").fill("e2e-setup-token");
    await page.getByLabel("Nom d'utilisateur").fill(admin.user);
    await page.getByLabel("Mot de passe", { exact: true }).fill(admin.password);
    await page.getByLabel("Confirmation").fill(admin.password);
    await page.getByRole("button", { name: "Créer le compte" }).click();
    await expect(page.getByRole("link", { name: "Clés MCP" })).toBeVisible();

    // Une session neuve doit pouvoir se connecter avec le compte créé.
    const ctx = await browser.newContext({ baseURL: "http://127.0.0.1:18080", locale: "fr-FR" });
    await login(await ctx.newPage());
    await ctx.close();
  });

  test("mauvais mot de passe refusé", async ({ browser }) => {
    const ctx = await browser.newContext({ baseURL: "http://127.0.0.1:18080", locale: "fr-FR" });
    const page = await ctx.newPage();
    await page.goto("/");
    await page.getByLabel("Nom d'utilisateur").fill(admin.user);
    await page.getByLabel("Mot de passe").fill("mauvais-mot-de-passe");
    await page.getByRole("button", { name: "Se connecter" }).click();
    await expect(page.getByRole("link", { name: "Clés MCP" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Se connecter" })).toBeVisible();
    await ctx.close();
  });

  test("création d'une clé MCP : le jeton n'est affiché qu'une fois", async ({ page }) => {
    await login(page);
    await page.goto("/cles");
    await page.getByRole("button", { name: "Nouvelle clé" }).first().click();
    await page.getByLabel("Nom", { exact: true }).fill(keyName);
    await page.getByRole("button", { name: "Créer", exact: true }).click();

    await expect(page.getByRole("heading", { name: "Copiez votre clé maintenant" })).toBeVisible();
    await expect(page.getByRole("alert").filter({ hasText: "ne sera plus jamais affiché" })).toBeVisible();
    await page.getByRole("button", { name: "J'ai copié la clé" }).click();

    await expect(page.getByRole("cell", { name: keyName })).toBeVisible();
    await page.reload();
    await expect(page.getByText("ne sera plus jamais affiché")).toHaveCount(0);
  });

  test("le journal d'administration trace la création de la clé", async ({ page }) => {
    await login(page);
    await page.goto("/journal?tab=admin");
    await expect(page.getByRole("heading", { name: "Journal" })).toBeVisible();
    await expect(page.getByRole("row").filter({ hasText: keyName })).toBeVisible();
  });
});
