import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CommandEditor } from "./pages/CommandEditor";
import { ApprovalsPage } from "./pages/ApprovalsPage";
import { RevealDialog, configSnippets } from "./pages/KeysPage";
import { CellView } from "./pages/SimulatorPage";
import { renderApp, mockApi } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

describe("éditeur de commande", () => {
  it("détecte les placeholders, affiche le lint et l'aperçu avec échappement", async () => {
    mockApi({
      "GET /commands": () => ({ body: { items: [] } }),
      "POST /commands/lint": (b) => {
        const tpl = (b as { template: string }).template;
        return { body: { placeholders: tpl.includes("{{service}}") ? ["service"] : [], lint: { errors: [], warnings: tpl.includes("|") ? [{ code: "pipe", message: "métacaractère shell hors placeholder: |" }] : [] } } };
      },
      "POST /commands/preview": (b) => {
        const v = (b as { values: { service?: string } }).values.service;
        return { body: v ? { ok: true, rendered: `systemctl status '${v}'` } : { ok: false, error: "paramètre `service`: obligatoire" } };
      },
    });
    renderApp(<CommandEditor />);
    const u = userEvent.setup();
    await u.click(await screen.findByLabelText("Modèle de commande"));
    await u.paste("systemctl status {{service}} | head"); // user-event.type interprète « { »
    expect(await screen.findByTestId("param-service")).toBeInTheDocument();
    expect(await screen.findByText(/métacaractère shell/)).toBeInTheDocument();
    expect(screen.getByTestId("highlighted")).toHaveTextContent("{{service}}");
    await u.type(screen.getByLabelText("service", { selector: "#pv-service" }), "nginx");
    expect(await screen.findByText("systemctl status 'nginx'")).toBeInTheDocument();
  });

  it("n'autorise pas l'enregistrement sans identifiant ni nom", async () => {
    mockApi({ "GET /commands": () => ({ body: { items: [] } }) });
    renderApp(<CommandEditor />);
    expect(await screen.findByRole("button", { name: /Enregistrer/ })).toBeDisabled();
  });
});

const pending = (over = {}) => ({ id: "E1", key_id: "k", key_name: "Agent N2", server_slug: "db-02", command_slug: "mysql.kill", risk: "destructive", params: '{"id":42}',
  rendered: "mysql -e 'KILL 42'", reason: "Requête bloquante #4521", rule_name: "N2 — BDD", status: "pending_approval", requested_at: new Date().toISOString(), ...over });

describe("validations", () => {
  const routes = (calls: { current?: unknown }) => ({
    "GET /executions?status=pending_approval": () => ({ body: { items: [pending()] } }),
    "GET /meta": () => ({ body: { approval_ttl_seconds: 1800 } }),
    "GET /setup/status": () => ({ body: { initialized: true } }),
    "GET /auth/me": () => ({ body: { id: "u", username: "sd", display_name: "SD", role: "approver", csrf_token: "t" } }),
    "POST /executions/E1/decision": (b: unknown) => { calls.current = b; return { body: {} }; },
    "GET /executions/E1": () => ({ body: pending({ status: "succeeded", decided_by_name: "sd", exit_code: 0 }) }),
  });

  it("montre la raison, la commande rendue et exige le slug pour une commande destructive", async () => {
    const seen: { current?: unknown } = {};
    mockApi(routes(seen));
    renderApp(<ApprovalsPage />);
    const u = userEvent.setup();
    expect(await screen.findByText("Requête bloquante #4521")).toBeInTheDocument();
    expect(screen.getByText("mysql -e 'KILL 42'")).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: /Approuver/ }));
    const dlg = await screen.findByRole("dialog");
    const confirm = within(dlg).getByRole("button", { name: "Confirmer" });
    expect(confirm).toBeDisabled();
    await u.type(within(dlg).getByLabelText(/Saisissez/), "db-01");
    expect(confirm).toBeDisabled();
    await u.clear(within(dlg).getByLabelText(/Saisissez/));
    await u.type(within(dlg).getByLabelText(/Saisissez/), "db-02");
    await u.click(confirm);
    await waitFor(() => expect(seen.current).toMatchObject({ decision: "approve", confirm_slug: "db-02" }));
    expect(await screen.findByText("réussie")).toBeInTheDocument();
  });

  it("exige un commentaire pour refuser", async () => {
    const seen: { current?: unknown } = {};
    mockApi(routes(seen));
    renderApp(<ApprovalsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: /Refuser/ }));
    expect(await screen.findByText("Un commentaire est obligatoire pour refuser.")).toBeInTheDocument();
    expect(seen.current).toBeUndefined();
    await u.type(screen.getByLabelText("Commentaire"), "Pas maintenant");
    await u.click(screen.getByRole("button", { name: /Refuser/ }));
    await waitFor(() => expect(seen.current).toMatchObject({ decision: "reject", comment: "Pas maintenant" }));
  });

  it("n'affiche pas les boutons de décision à un lecteur", async () => {
    mockApi({ ...routes({}), "GET /auth/me": () => ({ body: { id: "u", username: "v", display_name: "V", role: "viewer", csrf_token: "t" } }) });
    renderApp(<ApprovalsPage />);
    await screen.findByText("Requête bloquante #4521");
    await waitFor(() => expect(screen.getByText(/ne permet pas de décider/)).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /Approuver/ })).toBeNull();
  });
});

describe("clés MCP", () => {
  it("insère le jeton dans les exemples de configuration", async () => {
    const snip = configSnippets("lsh_abc", "https://leassh.example.com");
    expect(snip.claude).toContain("claude mcp add --transport http leassh https://leassh.example.com/mcp");
    expect(snip.claude).toContain("Bearer lsh_abc");
    expect(JSON.parse(snip.json).mcpServers.leassh.headers.Authorization).toBe("Bearer lsh_abc");
    expect(snip.litellm).toContain('auth_value: "lsh_abc"');
    renderApp(<RevealDialog token="lsh_abc" onClose={() => undefined} />);
    expect(await screen.findByText(/ne sera plus jamais affiché/)).toBeInTheDocument();
  });
});

describe("simulateur", () => {
  it("exprime l'autorisation par un libellé, pas seulement par la couleur", async () => {
    renderApp(<><CellView cell={{ server: "a", command: "b", allowed: true, requires_approval: true, rule: "R1" }} />
      <CellView cell={{ server: "a", command: "c", allowed: false, requires_approval: false, reason: "profile_forbids_risk" }} /></>);
    expect(await screen.findByLabelText(/validation: autorisé par la règle R1/)).toBeInTheDocument();
    expect(screen.getByLabelText(/refusé: refusé : le profil RO interdit ce risque/)).toBeInTheDocument();
  });
});

// Contrastes WCAG AA des tokens (texte normal ≥ 4.5) dans les deux thèmes.
describe("contrastes des thèmes", () => {
  const css = readFileSync(resolve(process.cwd(), "src/styles.css"), "utf8");
  const block = (re: RegExp) => Object.fromEntries([...(css.match(re)?.[1] ?? "").matchAll(/--([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})/g)].map((m) => [m[1], m[2]]));
  const light = block(/:root\s*\{([^}]*)\}/);
  const dark = block(/:root\[data-theme="dark"\]\s*\{([^}]*)\}/);
  const lum = (hex: string) => {
    const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
  };
  const ratio = (a: string, b: string) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05); };
  const pairs: [string, string][] = [["text", "bg"], ["text", "surface"], ["text-muted", "bg"], ["text-muted", "surface"], ["text-muted", "surface-2"],
    ["primary-fg", "primary"], ["border-strong", "surface"], ["primary", "surface"], ["read", "surface"], ["write", "surface"], ["destructive", "surface"], ["info", "surface"]];
  // fond « soft » des badges: 12 % de la couleur sur la surface
  const soft = (c: string, bg: string) => "#" + [1, 3, 5].map((i) => Math.round(parseInt(bg.slice(i, i + 2), 16) * 0.88 + parseInt(c.slice(i, i + 2), 16) * 0.12).toString(16).padStart(2, "0")).join("");
  for (const [name, theme] of [["clair", light], ["sombre", dark]] as const) {
    it(`respecte AA en thème ${name}`, () => {
      for (const [fg, bg] of pairs) expect(ratio(theme[fg], theme[bg]), `${fg} sur ${bg}`).toBeGreaterThanOrEqual(fg === "border-strong" ? 3 : 4.5);
    });
    it(`respecte AA pour les badges en thème ${name}`, () => {
      for (const c of ["read", "write", "destructive", "info", "primary"]) expect(ratio(theme[c], soft(theme[c], theme.surface)), `badge ${c}`).toBeGreaterThanOrEqual(4.5);
    });
  }
});

describe("régression: réponse du linter avec des valeurs null", () => {
  it("l'éditeur ne plante pas quand placeholders / errors / warnings valent null", async () => {
    mockApi({
      "GET /commands": () => ({ body: { items: [] } }),
      "POST /commands/lint": () => ({ body: { placeholders: null, lint: { errors: null, warnings: null } } }),
    });
    renderApp(<CommandEditor />);
    const u = userEvent.setup();
    await u.click(await screen.findByLabelText("Modèle de commande"));
    await u.paste("uptime");
    expect(await screen.findByText(/Le modèle est valide/)).toBeInTheDocument();
    expect(screen.getByText(/Aucun paramètre/)).toBeInTheDocument();
  });
});

describe("bouton Copier", () => {
  const setSecure = (v: boolean) => Object.defineProperty(window, "isSecureContext", { value: v, configurable: true });
  afterEach(() => { setSecure(true); Reflect.deleteProperty(document, "execCommand"); });

  it("utilise l'API Clipboard en contexte sécurisé", async () => {
    const u = userEvent.setup(); // installe son propre navigator.clipboard
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    setSecure(true);
    renderApp(<RevealDialog token="lsh_secret" onClose={() => undefined} />);
    await u.click((await screen.findAllByRole("button", { name: "Copier" }))[0]);
    expect(writeText).toHaveBeenCalledWith("lsh_secret");
    expect(await screen.findAllByText("Copié")).not.toHaveLength(0);
  });

  it("retombe sur execCommand en HTTP simple (pas de navigator.clipboard) et copie le bon texte", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    setSecure(false);
    let copied = "";
    document.execCommand = vi.fn(() => { copied = (document.activeElement as HTMLTextAreaElement).value; return true; });
    renderApp(<RevealDialog token="lsh_secret" onClose={() => undefined} />);
    await userEvent.setup().click((await screen.findAllByRole("button", { name: "Copier" }))[0]);
    expect(document.execCommand).toHaveBeenCalledWith("copy");
    expect(copied).toBe("lsh_secret");
    expect(await screen.findAllByText("Copié")).not.toHaveLength(0);
    expect(document.querySelectorAll("textarea[aria-hidden]")).toHaveLength(0); // nettoyage
  });

  it("n'affiche pas « Copié » quand la copie échoue", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    setSecure(false);
    document.execCommand = vi.fn(() => false);
    renderApp(<RevealDialog token="lsh_secret" onClose={() => undefined} />);
    await userEvent.setup().click((await screen.findAllByRole("button", { name: "Copier" }))[0]);
    expect(await screen.findAllByText("Échec : copiez à la main")).not.toHaveLength(0);
    expect(screen.queryByText("Copié")).toBeNull();
  });
});
