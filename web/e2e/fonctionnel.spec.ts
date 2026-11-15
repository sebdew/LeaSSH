import { expect, test } from "@playwright/test";
import { api, dockerAvailable, login, mcpClient, startSshd } from "./helpers";

// Parcours fonctionnels complets, de la configuration à l'exécution réelle via MCP sur un vrai sshd (Docker).
// Dépend du projet « parcours » (compte administrateur) et précède le balayage (qui voit alors des écrans remplis).
test.skip(!dockerAvailable() && !process.env.CI, "Docker indisponible : ces parcours ont besoin d'un sshd de test");

let sshd: { port: number; stop: () => void } | undefined;
let token = "";
test.afterAll(() => sshd?.stop());

const SERVER = "srv-e2e";

test.describe.serial("configuration → exécution → validation", () => {
  test.beforeEach(async ({ page }) => { await login(page); });

  test("identité SSH générée depuis l'interface", async ({ page }) => {
    await page.goto("/identites");
    await page.getByRole("button", { name: "Nouvelle identité" }).first().click();
    const dlg = page.getByRole("dialog", { name: "Nouvelle identité" });
    await dlg.getByLabel("Nom", { exact: true }).fill("Identité e2e");
    await dlg.getByLabel("Utilisateur SSH distant").fill("leassh");
    await dlg.getByRole("button", { name: "Créer", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "Identité créée" })).toContainText("ssh-ed25519");
    await page.getByRole("dialog", { name: "Identité créée" }).getByRole("button", { name: "Fermer" }).first().click();
    await expect(page.getByRole("heading", { name: "Identité e2e" })).toBeVisible();
  });

  test("serveur : création, test de connexion, épinglage de la clé d'hôte", async ({ page }) => {
    test.setTimeout(420_000);
    const ids = (await api(page, "GET", "/identities")).items as { name: string; public_key: string }[];
    sshd = await startSshd(ids.find((i) => i.name === "Identité e2e")!.public_key);

    await page.goto("/serveurs");
    await page.getByRole("button", { name: "Ajouter un serveur" }).first().click();
    const dlg = page.getByRole("dialog", { name: "Ajouter un serveur" });
    await dlg.getByLabel("Nom", { exact: true }).fill("Serveur e2e");
    await dlg.getByLabel("Identifiant (slug)").fill(SERVER);
    await dlg.getByLabel("Hôte").fill("127.0.0.1");
    await dlg.getByLabel("Port").fill(String(sshd.port));
    await dlg.getByRole("button", { name: "Enregistrer" }).click();
    await expect(page.getByRole("row").filter({ hasText: SERVER })).toBeVisible();

    await page.getByRole("row").filter({ hasText: SERVER }).getByRole("button", { name: "Tester la connexion" }).click();
    const probe = page.getByRole("dialog", { name: /Connexion à/ });
    await probe.getByRole("button", { name: "Tester la connexion" }).click();
    await probe.getByRole("button", { name: "Faire confiance à cette clé" }).click();
    await expect(page.getByRole("row").filter({ hasText: SERVER })).toContainText("épinglée");

    // Clé d'hôte épinglée : l'authentification peut maintenant être vérifiée (le conteneur finit parfois de créer l'utilisateur).
    const srv = ((await api(page, "GET", "/servers")).items as { id: string; slug: string }[]).find((s) => s.slug === SERVER)!;
    await expect.poll(async () => (await api(page, "POST", `/servers/${srv.id}/test-connection`)).auth_ok, { timeout: 120_000, intervals: [2000] }).toBe(true);
  });

  test("commande créée dans l'éditeur, aperçu et test sur le serveur", async ({ page }) => {
    await page.goto("/commandes/nouvelle");
    await page.getByLabel("Identifiant (slug)").fill("demo.echo");
    await page.getByLabel("Nom", { exact: true }).fill("Écho de démonstration");
    await page.getByLabel("Description", { exact: true }).first().fill("Affiche le mot reçu. Sans effet de bord.");
    await page.getByLabel("Modèle de commande").fill("echo {{word}}");
    await expect(page.getByText("Le modèle est valide.")).toBeVisible();
    await page.getByLabel("word", { exact: true }).fill("bonjour");
    await expect(page.locator("code", { hasText: "echo 'bonjour'" }).first()).toBeVisible();
    await page.getByRole("button", { name: "Enregistrer" }).click();
    await expect(page).toHaveURL(/\/commandes\/[^/]+$/);

    await page.getByLabel("word", { exact: true }).fill("bonjour");
    await page.getByLabel("Serveur (clé d'hôte épinglée)").selectOption({ label: SERVER });
    await page.getByRole("button", { name: "Lancer le test" }).click();
    await expect(page.getByText("réussie").first()).toBeVisible({ timeout: 30_000 });
  });

  test("groupes, clé MCP et règle d'accès", async ({ page }) => {
    // Commande d'écriture + groupes + clé : préparés par l'API (couverts par ailleurs côté interface).
    await api(page, "POST", "/commands", { slug: "demo.write_echo", name: "Écho (écriture)", description: "Commande d'écriture de démonstration, validation requise.",
      risk: "write", template: "echo {{word}}", params: { word: { type: "identifier" } }, timeout_s: 20, max_output_kb: 64, status: "enabled" });
    const cmds = (await api(page, "GET", "/commands")).items as { id: string; slug: string }[];
    const srv = ((await api(page, "GET", "/servers")).items as { id: string; slug: string }[]).find((s) => s.slug === SERVER)!;
    await api(page, "POST", "/server-groups", { slug: "g-srv-e2e", name: "Serveurs e2e", member_ids: [srv.id] });
    await api(page, "POST", "/command-groups", { slug: "g-cmd-e2e", name: "Commandes e2e", member_ids: cmds.filter((c) => c.slug.startsWith("demo.")).map((c) => c.id) });
    const k = await api(page, "POST", "/keys", { name: "Agent fonctionnel", can_propose: true, rule_ids: [] });
    token = k.token;

    await page.goto("/regles");
    await page.getByRole("button", { name: "Nouvelle règle" }).first().click();
    const dlg = page.getByRole("dialog", { name: "Nouvelle règle" });
    await dlg.getByLabel("Nom", { exact: true }).fill("Règle e2e");
    await dlg.getByLabel("Agent fonctionnel").check();
    await dlg.getByLabel("Serveurs e2e").check();
    await dlg.getByLabel("Commandes e2e").check();
    await dlg.getByLabel("Profil").selectOption("RW");
    await dlg.getByRole("button", { name: "Enregistrer" }).click();
    await expect(page.getByTestId("rule-sentence")).toContainText("Agent fonctionnel");
  });

  test("le simulateur distingue exécution directe et validation", async ({ page }) => {
    await page.goto("/simulateur");
    await page.getByLabel("Clé MCP").selectOption({ label: "Agent fonctionnel" });
    await expect(page.getByLabel(/^direct:/)).toHaveCount(1);
    await expect(page.getByLabel(/^validation:/)).toHaveCount(1);
  });

  test("MCP : lecture exécutée, injection refusée, écriture soumise à validation", async ({ browser }) => {
    test.setTimeout(180_000);
    const mcp = await mcpClient(token);
    expect(await mcp.tools()).toEqual(expect.arrayContaining(["list_servers", "list_commands", "run_command", "get_execution", "propose_command"]));
    expect(JSON.stringify((await mcp.call("list_servers")).data)).toContain(SERVER);

    const ok = await mcp.call("run_command", { server: SERVER, command: "demo.echo", params: { word: "bonjour" }, reason: "e2e lecture" });
    expect(ok.data).toMatchObject({ status: "succeeded", exit_code: 0 });
    expect(ok.data.stdout).toContain("bonjour");

    for (const mal of ["; id", "$(id)", "a b", "-rf"]) {
      const bad = await mcp.call("run_command", { server: SERVER, command: "demo.echo", params: { word: mal }, reason: "e2e injection" });
      expect(bad.isError, `« ${mal} » aurait dû être refusé`).toBe(true);
    }

    // Écriture : en attente, décidée depuis l'écran Validations en largeur mobile.
    const w1 = (await mcp.call("run_command", { server: SERVER, command: "demo.write_echo", params: { word: "approuve" }, reason: "e2e écriture à approuver" })).data;
    expect(w1.status).toBe("pending_approval");
    const w2 = (await mcp.call("run_command", { server: SERVER, command: "demo.write_echo", params: { word: "refuse" }, reason: "e2e écriture à refuser" })).data;

    const ctx = await browser.newContext({ baseURL: "http://127.0.0.1:18080", locale: "fr-FR", viewport: { width: 390, height: 844 } });
    const m = await ctx.newPage();
    await login(m);
    await m.goto("/validations");
    const c1 = m.locator("div").filter({ hasText: "e2e écriture à approuver" }).filter({ has: m.getByRole("button", { name: "Approuver" }) }).last();
    await c1.getByRole("button", { name: "Approuver" }).click();
    await expect(m.getByText("Demande approuvée")).toBeVisible();

    const c2 = m.locator("div").filter({ hasText: "e2e écriture à refuser" }).filter({ has: m.getByRole("button", { name: "Refuser" }) }).last();
    await c2.getByRole("button", { name: "Refuser" }).click(); // sans commentaire : bloqué
    await expect(m.getByText("Un commentaire est obligatoire pour refuser.")).toBeVisible();
    await c2.getByLabel("Commentaire").fill("hors fenêtre de maintenance");
    await c2.getByRole("button", { name: "Refuser" }).click();
    await expect(m.getByText("Demande refusée")).toBeVisible();
    await ctx.close();

    const done = (await mcp.call("get_execution", { execution_id: w1.execution_id, wait_seconds: 20 })).data;
    expect(done).toMatchObject({ status: "succeeded" });
    expect(done.stdout).toContain("approuve");
    const rej = (await mcp.call("get_execution", { execution_id: w2.execution_id, wait_seconds: 20 })).data;
    expect(rej.status).toBe("rejected");
  });

  test("le journal montre les exécutions, y compris les refus", async ({ page }) => {
    await page.goto("/journal");
    await expect(page.getByRole("row").filter({ hasText: "demo.echo" }).first()).toBeVisible();
    await expect(page.getByRole("row").filter({ hasText: "demo.write_echo" }).first()).toBeVisible();
    await page.goto("/journal?tab=admin");
    await expect(page.getByRole("row").filter({ hasText: SERVER }).first()).toBeVisible();
  });

  test("proposition d'un agent : examen puis approbation", async ({ page }) => {
    const mcp = await mcpClient(token);
    const p = await mcp.call("propose_command", { slug: "demo.uptime", name: "Durée de fonctionnement", description: "Affiche la durée depuis le démarrage. Lecture seule.",
      template: "uptime", risk: "read", justification: "e2e : diagnostic de charge" });
    expect(p.isError, p.text).toBe(false);

    await page.goto("/propositions");
    await page.getByRole("row").filter({ hasText: "demo.uptime" }).getByRole("link").first().click();
    await expect(page.getByText("e2e : diagnostic de charge")).toBeVisible();
    await expect(page.getByText("Le modèle est valide.")).toBeVisible();
    await page.getByRole("button", { name: "Approuver", exact: true }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Approuver", exact: true }).click();
    await expect(page.getByText("Proposition approuvée")).toBeVisible();

    await page.goto("/commandes");
    await expect(page.getByText("demo.uptime").first()).toBeVisible();
  });
});
