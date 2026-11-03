import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GroupsPanel } from "./components/GroupsPanel";
import { ProvisioningPage } from "./pages/ProvisioningPage";
import { ServersPage } from "./pages/ServersPage";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const prov = { id: "p1", name: "Bastion", type: "warpgate", enabled: true, interval_min: 15, identity_id: "i1", default_user: "ia", config: {}, has_secret: true, rule_ids: [],
  last_sync_at: "", last_sync_status: "", last_sync_message: "", created_at: "" };
const server = (over = {}) => ({ id: "s1", slug: "pve01", name: "pve01 - SSH root", host: "10.0.0.9", port: 2222, identity_id: "i1", host_key_fingerprint: "SHA256:x", host_key_status: "pinned", description: "Proxmox", tags: [], enabled: true, ssh_user: "ia:pve01", managed_by: "p1", ...over });

describe("éléments synchronisés", () => {
  it("serveur: icône de synchro, champs verrouillés, pas de suppression", async () => {
    mockApi({ "GET /servers": () => ({ body: { items: [server(), server({ id: "s2", slug: "manuel", name: "Manuel", managed_by: "" })] } }),
      "GET /identities": () => ({ body: { items: [{ id: "i1", name: "Warpgate", username: "ia", auth_type: "key", public_key: "", created_at: "" }] } }),
      "GET /provisioners": () => ({ body: { items: [prov] } }) });
    renderApp(<ServersPage />);
    expect(await screen.findByLabelText("Synchronisé depuis Bastion")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Supprimer" })).toHaveLength(1); // seul le serveur manuel
    await userEvent.setup().click(screen.getAllByRole("button", { name: "Modifier" })[0]);
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByLabelText("Nom")).toBeDisabled();
    expect(within(dlg).getByLabelText(/Hôte/)).toBeDisabled();
    expect(within(dlg).getByLabelText(/Utilisateur SSH/)).not.toBeDisabled(); // choix de l'admin, conservé par la synchro
    expect(within(dlg).getByLabelText(/Identité SSH/)).not.toBeDisabled();
  });

  it("groupe: verrouillé, membres synchronisés non décochables, envoi sur /members", async () => {
    let sent: unknown;
    mockApi({
      "GET /server-groups": () => ({ body: { items: [{ id: "g1", slug: "hyperviseurs", name: "Hyperviseurs", description: "", member_ids: ["a"], managed_by: "p1", managed_member_ids: ["a"] }] } }),
      "GET /provisioners": () => ({ body: { items: [prov] } }),
      "PUT /server-groups/g1/members": (b) => { sent = b; return { body: {} }; },
    });
    renderApp(<GroupsPanel kind="server" members={[{ id: "a", label: "pve01" }, { id: "b", label: "manuel" }]} />);
    expect(await screen.findByLabelText("Groupe synchronisé depuis Bastion")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Supprimer" })).toBeNull();
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "Modifier" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByLabelText("Nom")).toBeDisabled();
    expect(within(dlg).getByLabelText("pve01")).toBeChecked();
    expect(within(dlg).getByLabelText("pve01")).toBeDisabled(); // appartenance posée par la synchro
    await u.click(within(dlg).getByLabelText("manuel")); // un hôte hors synchro peut rejoindre le groupe
    await u.click(within(dlg).getByRole("button", { name: "Enregistrer" }));
    expect(sent).toEqual({ ids: ["a", "b"] });
  });
});

describe("utilisateur SSH d'un serveur synchronisé", () => {
  const ident = { "GET /identities": () => ({ body: { items: [{ id: "i1", name: "Warpgate", username: "ia", auth_type: "key", public_key: "", created_at: "" }] } }), "GET /provisioners": () => ({ body: { items: [prov] } }) };

  it("« suivre la synchro » n'apparaît que si l'utilisateur a été personnalisé, et l'envoie", async () => {
    let sent: Record<string, unknown> = {};
    const custom = server({ ssh_user: "root:pve01", ssh_user_custom: true });
    mockApi({ ...ident, "GET /servers": () => ({ body: { items: [custom] } }), "PUT /servers/s1": (b) => { sent = b as Record<string, unknown>; return { body: custom }; } });
    renderApp(<ServersPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Modifier" }));
    const dlg = await screen.findByRole("dialog");
    await u.click(within(dlg).getByLabelText(/Suivre la synchro/));
    expect(within(dlg).getByLabelText(/Utilisateur SSH/)).toBeDisabled();
    await u.click(within(dlg).getByRole("button", { name: "Enregistrer" }));
    expect(sent.ssh_user_follow_sync).toBe(true);
  });

  it("pas de case tant que l'utilisateur suit la synchro", async () => {
    mockApi({ ...ident, "GET /servers": () => ({ body: { items: [server()] } }) });
    renderApp(<ServersPage />);
    await userEvent.setup().click(await screen.findByRole("button", { name: "Modifier" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).queryByLabelText(/Suivre la synchro/)).toBeNull();
  });
});

describe("page Provisioning", () => {
  const base = {
    "GET /provisioners": () => ({ body: { items: [prov] } }),
    "GET /provisioners/types": () => ({ body: { items: [{ id: "warpgate", label: "Warpgate", fields: [] }] } }),
    "GET /identities": () => ({ body: { items: [{ id: "i1", name: "Warpgate", username: "ia", auth_type: "key", public_key: "", created_at: "" }] } }),
    "GET /rules": () => ({ body: { items: [] } }), "GET /servers": () => ({ body: { items: [server()] } }), "GET /server-groups": () => ({ body: { items: [] } }),
  };

  it("liste le connecteur, ses compteurs, et un aperçu qui liste les changements", async () => {
    mockApi({ ...base, "POST /provisioners/p1/preview": () => ({ body: { machines: 2, changes: [{ kind: "server", action: "create", name: "pve04", detail: "" }, { kind: "group", action: "create", name: "Web Sites", detail: "ajouté à 1 règle(s) par défaut" }] } }) });
    renderApp(<ProvisioningPage />);
    expect(await screen.findByText("Bastion")).toBeInTheDocument();
    expect(screen.getByText(/1 serveur\(s\) et 0 groupe\(s\) synchronisés/)).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Synchroniser" }));
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByText("pve04")).toBeInTheDocument();
    expect(within(dlg).getByText(/ajouté à 1 règle/)).toBeInTheDocument();
    expect(within(dlg).getByRole("button", { name: "Appliquer" })).toBeEnabled();
  });

  it("une synchro bloquée exige une confirmation explicite, puis envoie force", async () => {
    let sent: unknown;
    mockApi({ ...base,
      "POST /provisioners/p1/preview": () => ({ body: { machines: 1, blocked: "4 machine(s) sur 5 seraient désactivées", changes: [{ kind: "server", action: "disable", name: "a", detail: "absent de la source" }] } }),
      "POST /provisioners/p1/sync": (b) => { sent = b; return { body: { machines: 1, changes: [] } }; } });
    renderApp(<ProvisioningPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Synchroniser" }));
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByText(/4 machine\(s\) sur 5/)).toBeInTheDocument();
    const apply = within(dlg).getByRole("button", { name: "Appliquer" });
    expect(apply).toBeDisabled();
    await u.click(within(dlg).getByLabelText(/Je confirme/));
    await u.click(apply);
    expect(sent).toEqual({ force: true });
  });
});
