import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { KeysPage } from "./pages/KeysPage";
import { RulesPage } from "./pages/RulesPage";
import { ServersPage } from "./pages/ServersPage";
import { GroupsPanel } from "./components/GroupsPanel";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const past = "2020-01-01T00:00:00.000Z", future = "2999-01-01T00:00:00.000Z";
const key = (id: string, name: string, over = {}) => ({ id, name, prefix: "lsh_" + id, description: "", expires_at: "", last_used_at: "", use_count: 0, can_propose: true, disabled: false, rule_ids: [], created_at: "", ...over });
const keys = [key("k1", "N1 actif"), key("k2", "N1 révoquée", { disabled: true }), key("k3", "N3 expirée", { expires_at: past }), key("k4", "N4 valide", { expires_at: future })];

describe("les éléments inactifs ne sont jamais proposés", () => {
  it("règle: seules les clés actives sont sélectionnables, et la phrase ne cite pas les inactives", async () => {
    mockApi({
      "GET /rules": () => ({ body: { items: [{ id: "r1", name: "Diag niveau 1", server_group_ids: ["sg"], command_group_ids: ["cg"], profile: "RO", write_requires_approval: true, enabled: true, key_ids: ["k1", "k2", "k3"] }] } }),
      "GET /keys": () => ({ body: { items: keys } }),
      "GET /server-groups": () => ({ body: { items: [{ id: "sg", name: "Prod", slug: "prod", member_ids: [] }] } }),
      "GET /command-groups": () => ({ body: { items: [{ id: "cg", name: "Système", slug: "sys", member_ids: [] }] } }),
    });
    renderApp(<RulesPage />);
    const sentence = await screen.findByTestId("rule-sentence");
    expect(screen.getByTestId("rule-profile")).toHaveTextContent("Lecture seule"); // profil visible sur la carte
    expect(sentence).toHaveTextContent("N1 actif");
    expect(sentence).not.toHaveTextContent("révoquée");
    expect(sentence).not.toHaveTextContent("expirée");
    await userEvent.setup().click(screen.getByRole("button", { name: "Modifier" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByLabelText("N1 actif")).toBeChecked();
    expect(within(dlg).getByLabelText("N4 valide")).toBeInTheDocument();
    expect(within(dlg).queryByLabelText("N1 révoquée")).toBeNull();
    expect(within(dlg).queryByLabelText("N3 expirée")).toBeNull();
  });

  it("clés MCP: révoquées et expirées masquées par défaut, ré-affichables", async () => {
    mockApi({ "GET /keys": () => ({ body: { items: keys } }), "GET /rules": () => ({ body: { items: [] } }) });
    renderApp(<KeysPage />);
    expect(await screen.findByText("N1 actif")).toBeInTheDocument();
    expect(screen.getByText("N4 valide")).toBeInTheDocument();
    expect(screen.queryByText("N1 révoquée")).toBeNull();
    expect(screen.queryByText("N3 expirée")).toBeNull();
    await userEvent.setup().click(screen.getByLabelText("Afficher les éléments désactivés (2)"));
    expect(screen.getByText("N1 révoquée")).toBeInTheDocument();
    expect(screen.getByText("N3 expirée")).toBeInTheDocument();
  });

  it("serveurs désactivés masqués par défaut", async () => {
    const s = (id: string, slug: string, enabled: boolean) => ({ id, slug, name: "Serveur " + id, host: "h", port: 22, identity_id: "i", host_key_fingerprint: "", host_key_status: "pinned", description: "", tags: [], enabled });
    // le nom diffère du slug pour ne pas dupliquer le texte dans le tableau
    mockApi({ "GET /servers": () => ({ body: { items: [s("1", "web-01", true), s("2", "old-01", false)] } }), "GET /identities": () => ({ body: { items: [] } }) });
    renderApp(<ServersPage />);
    expect(await screen.findByText("web-01")).toBeInTheDocument();
    expect(screen.queryByText("old-01")).toBeNull();
    await userEvent.setup().click(screen.getByLabelText("Afficher l'élément désactivé (1)"));
    expect(screen.getByText("old-01")).toBeInTheDocument();
  });

  it("groupe: un membre désactivé n'est proposé que s'il l'est déjà", async () => {
    mockApi({ "GET /server-groups": () => ({ body: { items: [{ id: "g1", slug: "prod", name: "Prod", description: "", member_ids: ["b"] }] } }) });
    const members = [{ id: "a", label: "web-01" }, { id: "b", label: "old-01", inactive: true }, { id: "c", label: "old-02", inactive: true }];
    renderApp(<GroupsPanel kind="server" members={members} />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Modifier" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByLabelText("Nom")).toHaveValue("Prod"); // régression: le formulaire doit être pré-rempli
    expect(within(dlg).getByLabelText("web-01")).toBeInTheDocument();
    expect(within(dlg).getByLabelText("old-01 (désactivé)")).toBeChecked(); // déjà membre: conservé et signalé
    expect(within(dlg).queryByLabelText(/old-02/)).toBeNull(); // désactivé et non membre: jamais proposé
  });
});

describe("utilisateur SSH par serveur", () => {
  it("le formulaire pré-remplit et envoie ssh_user (cas Warpgate « utilisateur:cible »)", async () => {
    let sent: Record<string, unknown> = {};
    const srv = { id: "1", slug: "resa", name: "Resa", host: "192.168.6.163", port: 2222, identity_id: "i1", host_key_fingerprint: "SHA256:x", host_key_status: "pinned", description: "", tags: [], enabled: true, ssh_user: "ia:resa" };
    mockApi({
      "GET /servers": () => ({ body: { items: [srv] } }), "GET /identities": () => ({ body: { items: [{ id: "i1", name: "Warpgate", username: "ia", auth_type: "key", public_key: "", created_at: "" }] } }),
      "PUT /servers/1": (b) => { sent = b as Record<string, unknown>; return { body: srv }; },
    });
    renderApp(<ServersPage />);
    const u = userEvent.setup();
    expect(await screen.findByText("ia:resa@")).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Modifier" }));
    const dlg = await screen.findByRole("dialog");
    const field = within(dlg).getByLabelText("Utilisateur SSH (facultatif)");
    expect(field).toHaveValue("ia:resa");
    await u.clear(field);
    await u.type(field, "ia:reduc");
    await u.click(within(dlg).getByRole("button", { name: "Enregistrer" }));
    await vi.waitFor(() => expect(sent).toMatchObject({ slug: "resa", ssh_user: "ia:reduc" }));
  });
});
