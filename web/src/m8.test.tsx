import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Dashboard } from "./pages/Pages";
import { CommandPalette } from "./components/CommandPalette";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const steps = ["identity", "server", "pack", "rule", "key", "agent"] as const;
const dash = (done: number, over: Record<string, unknown> = {}) => ({
  checklist: steps.map((id, i) => ({ id, done: i < done, href: "/" + id })),
  stats: { executions_24h: { succeeded: 12, failed: 2, denied: 3, pending: 1, other: 0 }, active_agents_24h: 2, pending_approvals: 0, pending_proposals: 0, servers_in_anomaly: [] },
  recent: [], ...over,
});

describe("tableau de bord", () => {
  it("affiche la checklist avec progression et ordre des étapes", async () => {
    mockApi({ "GET /dashboard": () => ({ body: dash(2) }) });
    renderApp(<Dashboard />);
    expect(await screen.findByText("Premiers pas")).toBeInTheDocument();
    expect(screen.getByText("2/6")).toBeInTheDocument();
    const links = within(screen.getByRole("list")).getAllByRole("link");
    expect(links).toHaveLength(6);
    expect(links[0]).toHaveAccessibleName("Créer une identité SSH — terminée");
    expect(links[5]).toHaveAccessibleName("Connecter un agent — à faire");
  });

  it("masque la checklist une fois terminée", async () => {
    mockApi({ "GET /dashboard": () => ({ body: dash(6) }) });
    renderApp(<Dashboard />);
    await screen.findByText("Exécutions sur 24 h");
    expect(screen.queryByText("Premiers pas")).toBeNull();
    expect(screen.getByText("Aucune exécution pour l'instant. Elles apparaîtront ici dès qu'un agent utilisera Leassh.")).toBeInTheDocument();
  });

  it("bandeau de validations, chiffres, anomalies et activité récente", async () => {
    mockApi({
      "GET /dashboard": () => ({ body: dash(6, {
        stats: { executions_24h: { succeeded: 12, failed: 2, denied: 3, pending: 1, other: 0 }, active_agents_24h: 2, pending_approvals: 3, pending_proposals: 1,
          servers_in_anomaly: [{ slug: "db-02", name: "DB", reason: "host_key_mismatch" }] },
        recent: [{ id: "E1", key_name: "Agent N1", decided_by_name: "", actor_user_id: "", command_slug: "systemd.status", server_slug: "web-01", risk: "read", status: "succeeded", requested_at: "2026-10-18T13:42:07.000Z" }] }) }),
    });
    renderApp(<Dashboard />);
    expect(await screen.findByRole("status")).toHaveTextContent("3 validations en attente");
    expect(within(screen.getByRole("status")).getByRole("link", { name: "Examiner" })).toHaveAttribute("href", "/validations");
    expect(screen.getByText("18")).toBeInTheDocument(); // 12 + 2 + 3 + 1
    expect(screen.getByText(/12 réussies/)).toBeInTheDocument();
    expect(screen.getByText(/clé d'hôte divergente/)).toBeInTheDocument();
    const row = screen.getByRole("link", { name: /systemd\.status/ });
    expect(row).toHaveAttribute("href", expect.stringContaining("exec=E1"));
    expect(within(row).getByText("réussie")).toBeInTheDocument();
  });
});

describe("palette de commandes", () => {
  const routes = {
    "GET /servers": () => ({ body: { items: [{ id: "s1", slug: "web-01", name: "Web 01" }, { id: "s2", slug: "db-02", name: "BDD" }] } }),
    "GET /commands": () => ({ body: { items: [{ id: "c1", slug: "mysql.processlist", name: "Requêtes en cours" }] } }),
    "GET /keys": () => ({ body: { items: [{ id: "k1", name: "Agent N1", prefix: "lsh_ab12" }] } }),
  };

  it("propose les pages par défaut puis filtre serveurs, commandes et clés", async () => {
    mockApi(routes);
    renderApp(<CommandPalette open onClose={() => undefined} />);
    const input = await screen.findByRole("combobox");
    expect(await screen.findByRole("option", { name: /Journal/ })).toBeInTheDocument();
    const u = userEvent.setup();
    await u.type(input, "web");
    expect(await screen.findByRole("option", { name: /web-01/ })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /db-02/ })).toBeNull();
    await u.clear(input);
    await u.type(input, "processlist");
    expect(await screen.findByRole("option", { name: /mysql\.processlist/ })).toBeInTheDocument();
    await u.clear(input);
    await u.type(input, "lsh_ab");
    expect(await screen.findByRole("option", { name: /Agent N1/ })).toBeInTheDocument();
    await u.clear(input);
    await u.type(input, "zzz");
    expect(await screen.findByText("Aucun résultat pour « zzz ».")).toBeInTheDocument();
  });

  it("navigue au clavier et ferme à la validation", async () => {
    mockApi(routes);
    const onClose = vi.fn();
    renderApp(<CommandPalette open onClose={onClose} />);
    const input = await screen.findByRole("combobox");
    const u = userEvent.setup();
    await u.type(input, "e");
    await u.keyboard("{ArrowDown}");
    const opts = await screen.findAllByRole("option");
    expect(opts[1]).toHaveAttribute("aria-selected", "true");
    await u.keyboard("{Enter}");
    expect(onClose).toHaveBeenCalled();
  });
});
