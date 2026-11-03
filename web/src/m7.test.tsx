import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PacksPage } from "./pages/PacksPage";
import { ProposalDetail, ProposalEditorRoute, ProposalsPage } from "./pages/ProposalsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { mockApi, renderApp, type Handler } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const preview = (over = {}) => ({
  pack: { id: "demo", name: "Démo", version: "1.1.0", description: "" }, installed_version: "1.0.0", warnings: 0, errors: 0,
  counts: { new: 1, modified: 1, unchanged: 3, invalid: 0 },
  items: [
    { slug: "demo.new", name: "N", risk: "read", status: "new", lint: { errors: [], warnings: [] } },
    { slug: "demo.hello", name: "H", risk: "write", status: "modified", overrides_manual: true, lint: { errors: [], warnings: [] }, changes: [{ field: "template", before: "echo hello", after: "echo bonjour" }] },
  ], ...over,
});

const packRoutes = (extra: Record<string, Handler> = {}) => ({
  "GET /packs": () => ({ body: { installed: [], bundled: [{ id: "mysql", name: "MySQL / MariaDB", version: "1.0.0", description: "Diagnostic MySQL.", command_count: 7, installed_version: "" }] } }),
  "GET /command-groups": () => ({ body: { items: [{ id: "g1", slug: "std", name: "Standard", member_ids: [] }] } }),
  ...extra,
});

const many = ["system:Système:", "pfsense:Réseau:1.0.0", "unifi:Réseau:", "proxmox:Virtualisation:1.0.0", "mysql:Bases de données:"].map((x) => {
  const [id, category, installed] = x.split(":");
  return { id, name: id === "pfsense" ? "pfSense" : id, version: "1.0.0", description: `Pack ${id}`, category, command_count: 10, installed_version: installed ?? "", risks: { read: 8, write: 1, destructive: 1 }, command_ids: [`${id}.status`, `${id}.restart_service`] };
});

describe("liste des packs fournis", () => {
  it("regroupe par catégorie, filtre par catégorie, par état et par recherche (y compris sur les commandes)", async () => {
    mockApi(packRoutes({ "GET /packs": () => ({ body: { installed: [], bundled: many } }) }));
    renderApp(<PacksPage />);
    const u = userEvent.setup();
    expect(await screen.findByRole("heading", { name: /Réseau/ })).toBeInTheDocument(); // une section par catégorie
    expect(screen.getByTestId("pack-pfsense")).toHaveTextContent("8 lecture · 1 écriture · 1 destructive(s)");
    await u.click(screen.getByRole("button", { name: /Virtualisation/ }));
    expect(screen.getByTestId("pack-proxmox")).toBeInTheDocument();
    expect(screen.queryByTestId("pack-pfsense")).toBeNull();
    await u.click(screen.getByRole("button", { name: "Toutes" }));
    await u.selectOptions(screen.getByLabelText("Filtrer par état"), "ok");
    expect(screen.getByTestId("pack-pfsense")).toBeInTheDocument();
    expect(screen.queryByTestId("pack-unifi")).toBeNull();
    await u.selectOptions(screen.getByLabelText("Filtrer par état"), "");
    await u.type(screen.getByRole("searchbox"), "unifi.restart");
    expect(screen.getByTestId("pack-unifi")).toBeInTheDocument();
    expect(screen.queryByTestId("pack-system")).toBeNull();
    await u.clear(screen.getByRole("searchbox"));
    await u.type(screen.getByRole("searchbox"), "introuvable");
    await u.click(await screen.findByRole("button", { name: "Effacer les filtres" }));
    expect(screen.getByTestId("pack-system")).toBeInTheDocument();
  });
});

describe("packs", () => {
  it("liste les packs fournis avec leur état d'installation", async () => {
    mockApi(packRoutes());
    renderApp(<PacksPage />);
    expect(await screen.findByText("MySQL / MariaDB")).toBeInTheDocument();
    expect(within(screen.getByTestId("pack-mysql")).getByText("non installé")).toBeInTheDocument();
    expect(screen.getByText(/7 commandes/)).toBeInTheDocument();
    expect(screen.getByText("v1.0.0")).toBeInTheDocument();
  });

  it("aperçu d'un pack fourni : diff, pas de case à cocher d'avertissements, import avec groupe", async () => {
    let body: unknown;
    mockApi(packRoutes({
      "POST /packs/preview": () => ({ body: { preview: preview({ warnings: 2 }), trusted: true } }),
      "POST /packs/import": (b) => { body = b; return { body: { created: 1, updated: 1 } }; },
    }));
    renderApp(<PacksPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Installer" }));
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByText("echo hello")).toBeInTheDocument();
    expect(within(dlg).getByText("echo bonjour")).toBeInTheDocument();
    expect(within(dlg).getByText(/Remplace une commande créée à la main/)).toBeInTheDocument();
    expect(within(dlg).queryByLabelText(/J'ai examiné/)).toBeNull();
    await u.click(within(dlg).getByRole("button", { name: "Importer" }));
    await waitFor(() => expect(body).toMatchObject({ bundled: "mysql", create_group: true }));
  });

  it("un fichier importé exige d'accepter les avertissements", async () => {
    let body: unknown;
    mockApi(packRoutes({
      "POST /packs/preview": () => ({ body: { preview: preview({ warnings: 2 }), trusted: false } }),
      "POST /packs/import": (b) => { body = b; return { body: {} }; },
    }));
    renderApp(<PacksPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByLabelText("…ou collez son contenu"));
    await u.paste("pack:\n  id: demo");
    await u.click(screen.getByRole("button", { name: "Aperçu des changements" }));
    const dlg = await screen.findByRole("dialog");
    const confirm = await within(dlg).findByRole("button", { name: "Importer" });
    expect(within(dlg).getByText(/ne vient pas de Leash/)).toBeInTheDocument();
    expect(confirm).toBeDisabled();
    await u.click(within(dlg).getByLabelText(/J'ai examiné les 2 avertissement/));
    expect(confirm).toBeEnabled();
    await u.click(confirm);
    await waitFor(() => expect(body).toMatchObject({ yaml: "pack:\n  id: demo", allow_warnings: true }));
  });

  it("bloque l'import si une commande est invalide", async () => {
    mockApi(packRoutes({
      "POST /packs/preview": () => ({ body: { preview: preview({ errors: 1, counts: { new: 0, modified: 0, unchanged: 0, invalid: 1 },
        items: [{ slug: "bad.cmd", name: "B", risk: "read", status: "invalid", lint: { errors: [{ code: "placeholder_quoted", message: "{{x}} ne doit pas être entre guillemets" }], warnings: [] } }] }), trusted: false } }),
    }));
    renderApp(<PacksPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByLabelText("…ou collez son contenu"));
    await u.paste("x");
    await u.click(screen.getByRole("button", { name: "Aperçu des changements" }));
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByRole("alert")).toHaveTextContent("entre guillemets");
    expect(within(dlg).getByRole("button", { name: "Importer" })).toBeDisabled();
  });
});

const proposal = (over = {}) => ({
  id: "P1", key_id: "k1", key_name: "Agent N1", status: "pending", justification: "Diagnostic de réplication", created_at: "2026-10-18T10:00:00.000Z", reviewed_by: "", reviewed_at: "", review_comment: "", command_id: "",
  payload: { slug: "mysql.replica_status", name: "Réplication", description: "État de la réplication.", template: 'mysql --batch -e "SHOW REPLICA STATUS"', params: {}, risk: "read" },
  lint: { errors: [], warnings: [] }, ...over,
});

describe("propositions", () => {
  it("liste agent, slug, risque et statut", async () => {
    mockApi({ "GET /proposals": () => ({ body: { items: [proposal(), proposal({ id: "P2", status: "invalid" })] } }) });
    renderApp(<ProposalsPage />);
    expect(await screen.findAllByText("mysql.replica_status")).toHaveLength(2);
    expect(screen.getByText("en attente")).toBeInTheDocument();
    expect(screen.getByText("invalide")).toBeInTheDocument();
  });

  it("une proposition invalide montre le linter et ne peut pas être approuvée telle quelle", async () => {
    mockApi({
      "GET /proposals/P1": () => ({ body: proposal({ status: "invalid", lint: { errors: [{ code: "shell_metachar", message: "métacaractère shell hors placeholder: ;" }], warnings: [] } }) }),
      "GET /command-groups": () => ({ body: { items: [] } }),
    });
    renderApp(<ProposalDetail id="P1" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("métacaractère shell");
    expect(screen.getByRole("button", { name: "Approuver" })).toBeDisabled();
    expect(screen.getByRole("link", { name: "Modifier puis approuver" })).toBeInTheDocument();
  });

  const detailRoutes = (hooks: { approve?: (b: unknown) => void; reject?: (b: unknown) => void }) => ({
      "GET /proposals/P1": () => ({ body: proposal() }),
      "GET /command-groups": () => ({ body: { items: [{ id: "g1", slug: "diag", name: "Diag MySQL", member_ids: [] }] } }),
      "POST /proposals/P1/approve": (b: unknown) => { hooks.approve?.(b); return { body: {} }; },
      "POST /proposals/P1/reject": (b: unknown) => { hooks.reject?.(b); return { status: 204 }; },
  });

  it("refuse avec commentaire obligatoire", async () => {
    let rejected: unknown;
    mockApi(detailRoutes({ reject: (b) => { rejected = b; } }));
    renderApp(<ProposalDetail id="P1" />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Refuser" }));
    const dlg = await screen.findByRole("dialog");
    const rejectBtn = within(dlg).getByRole("button", { name: "Refuser" });
    expect(rejectBtn).toBeDisabled();
    await u.type(within(dlg).getByLabelText("Commentaire"), "Doublon");
    await u.click(rejectBtn);
    await waitFor(() => expect(rejected).toEqual({ comment: "Doublon" }));
  });

  it("approuve en choisissant les groupes", async () => {
    let approved: unknown;
    mockApi(detailRoutes({ approve: (b) => { approved = b; } }));
    renderApp(<ProposalDetail id="P1" />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Approuver" }));
    const dlg = await screen.findByRole("dialog");
    await u.click(within(dlg).getByLabelText("Diag MySQL"));
    await u.click(within(dlg).getByRole("button", { name: "Approuver" }));
    await waitFor(() => expect(approved).toMatchObject({ group_ids: ["g1"] }));
  });

  it("« Modifier puis approuver » : éditeur prérempli, linter strict, envoi de la version corrigée", async () => {
    let approved: { edited: { template: string; risk: string } } | undefined;
    let lintBodies: { template: string }[] = [];
    mockApi({
      "GET /proposals/P1": () => ({ body: proposal({ status: "invalid", payload: { ...proposal().payload, template: "uptime; id" } }) }),
      "GET /commands": () => ({ body: { items: [] } }),
      "GET /command-groups": () => ({ body: { items: [{ id: "g1", slug: "diag", name: "Diag MySQL", member_ids: [] }] } }),
      "POST /proposals/lint": (b) => {
        lintBodies.push(b as { template: string });
        const bad = (b as { template: string }).template.includes(";");
        return { body: { placeholders: [], lint: { errors: bad ? [{ code: "shell_metachar", message: "métacaractère shell hors placeholder: ;" }] : [], warnings: [] } } };
      },
      "POST /proposals/P1/approve": (b) => { approved = b as never; return { body: {} }; },
    });
    renderApp(<ProposalEditorRoute id="P1" />);
    const u = userEvent.setup();
    const tpl = await screen.findByLabelText("Modèle de commande");
    expect(tpl).toHaveValue("uptime; id");
    expect(await screen.findByText(/métacaractère shell/)).toBeInTheDocument();
    const approveBtn = screen.getByRole("button", { name: /Approuver/ });
    expect(approveBtn).toBeDisabled();
    await u.clear(tpl);
    await u.type(tpl, "uptime");
    await waitFor(() => expect(approveBtn).toBeEnabled());
    await u.click(screen.getByRole("radio", { name: /écriture/ }));
    await u.click(screen.getByLabelText("Diag MySQL"));
    await u.click(approveBtn);
    await waitFor(() => expect(approved?.edited).toMatchObject({ template: "uptime", risk: "write" }));
    expect(approved).toMatchObject({ group_ids: ["g1"] });
    expect(lintBodies.length).toBeGreaterThan(1);
  });
});

describe("webhook", () => {
  const admin = { id: "u1", username: "sd", display_name: "SD", role: "admin", theme: "system", csrf_token: "t" };
  it("n'affiche jamais le secret, l'enregistre à la demande et montre le résultat du test et les livraisons", async () => {
    let saved: unknown;
    mockApi({
      "GET /auth/me": () => ({ body: admin }), "GET /users": () => ({ body: { items: [] } }),
      "GET /settings/webhook": () => ({ body: { config: { url: "https://ntfy.example.com/leash", enabled: true, has_secret: true }, events: ["execution.finished", "proposal.created"] } }),
      "GET /settings/webhook/deliveries": () => ({ body: { items: [
        { id: "d1", ts: "2026-10-18T10:00:00.000Z", event: "execution.finished", ok: true, attempts: 1, http_status: 200, error: "", url: "" },
        { id: "d2", ts: "2026-10-18T10:05:00.000Z", event: "proposal.created", ok: false, attempts: 3, http_status: 500, error: "réponse HTTP 500", url: "" }] } }),
      "PUT /settings/webhook": (b) => { saved = b; return { body: {} }; },
      "POST /settings/webhook/test": () => ({ body: { ok: false, attempts: 1, http_status: 0, error: "connexion refusée" } }),
    });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "Webhook" }));
    expect(await screen.findByLabelText("URL")).toHaveValue("https://ntfy.example.com/leash");
    const secret = screen.getByLabelText("Secret de signature");
    expect(secret).toHaveValue("");
    expect(secret).toHaveAttribute("placeholder", expect.stringContaining("conserver"));
    expect(await screen.findByText(/3 tentatives · HTTP 500 · réponse HTTP 500/)).toBeInTheDocument();
    expect(screen.getByText("livré")).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Tester l'envoi" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Échec : connexion refusée");
    await u.type(secret, "nouveau");
    await u.click(screen.getByRole("button", { name: "Enregistrer" }));
    await waitFor(() => expect(saved).toEqual({ url: "https://ntfy.example.com/leash", secret: "nouveau", enabled: true }));
  });
});
