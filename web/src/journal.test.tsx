import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { JournalView } from "./pages/JournalPage";
import { SettingsPage } from "./pages/SettingsPage";
import { diffRows, parseSearch, toQuery, type JournalSearch } from "./lib/journal";
import { DiffView } from "./components/journal";
import { mockApi, renderApp } from "./test-utils";

const exec = (over = {}) => ({
  id: "E1", key_id: "k1", key_name: "Agent N1", actor_user_id: "", server_slug: "web-01", command_slug: "systemd.status", risk: "read", params: '{"service":"nginx"}',
  rendered: "systemctl status 'nginx'", reason: "Alarme Zabbix #4521", rule_name: "N1 diag", status: "succeeded", denial_reason: "", exit_code: 0, stdout: "ligne un\nligne deux\n", stderr: "",
  output_truncated: false, duration_ms: 412, requested_at: "2026-10-18T13:42:07.000Z", started_at: "2026-10-18T13:42:07.100Z", finished_at: "2026-10-18T13:42:07.512Z",
  decided_by: "", decided_by_name: "", decided_at: "", decision_comment: "", command_version: 3, command_hash: "abcdef0123456789", client_ip: "10.0.0.9", mcp_session_id: "", user_agent: "claude-code", server_id: "s1", ...over,
}) as Record<string, unknown>;

const baseRoutes = (list: unknown[], extra = {}) => ({
  "GET /executions": () => ({ body: { items: list, next_cursor: "" } }),
  "GET /executions/E1": () => ({ body: exec() }),
  "GET /keys": () => ({ body: { items: [{ id: "k1", name: "Agent N1" }] } }),
  "GET /servers": () => ({ body: { items: [{ id: "s1", slug: "web-01" }] } }),
  "GET /commands": () => ({ body: { items: [{ id: "c1", slug: "systemd.status" }] } }),
  "GET /users": () => ({ body: { items: [{ id: "u1", username: "sd" }] } }),
  ...extra,
});

function Harness({ initial = {} }: { initial?: JournalSearch }) {
  const [s, setS] = useState<JournalSearch>(initial);
  return <><JournalView search={s} onSearch={setS} /><output data-testid="url">{JSON.stringify(s)}</output></>;
}

class FakeES {
  static last: FakeES | null = null;
  listeners: Record<string, (e: MessageEvent) => void> = {};
  closed = false;
  constructor(public url: string) { FakeES.last = this; }
  addEventListener(n: string, cb: (e: MessageEvent) => void) { this.listeners[n] = cb; }
  close() { this.closed = true; }
  emit(data: unknown) { this.listeners.execution?.({ data: JSON.stringify(data) } as MessageEvent); }
}

beforeEach(() => { FakeES.last = null; vi.stubGlobal("EventSource", FakeES); });
afterEach(() => vi.unstubAllGlobals());

describe("état du journal dans l'URL", () => {
  it("normalise les search params et construit la requête API", () => {
    const s = parseSearch({ period: "24h", status: "denied,failed", junk: "x", tab: "weird", q: 5, exec: "E1" });
    expect(s).toEqual({ period: "24h", status: "denied,failed", q: "5", exec: "E1" });
    expect(toQuery(s, "exec")).toBe("period=24h&status=denied%2Cfailed&q=5");
    expect(toQuery({ ...s, action: "server." }, "admin")).toBe("period=24h&action=server.&q=5");
  });
});

describe("journal des exécutions", () => {
  it("applique les filtres de l'URL à la requête et les affiche en puces effaçables", async () => {
    const calls = mockApi(baseRoutes([exec()]));
    renderApp(<Harness initial={{ period: "24h", status: "denied,failed", key_id: "k1", q: "redémar" }} />);
    await screen.findAllByText("systemd.status");
    const url = calls.find((c) => c.key.startsWith("GET /executions?"))!.key;
    expect(url).toContain("period=24h");
    expect(url).toContain("status=denied%2Cfailed");
    expect(url).toContain("key_id=k1");
    expect(url).toContain("q=red%C3%A9mar");
    const chips = screen.getByLabelText("Filtres actifs");
    expect(within(chips).getByText("Agent N1")).toBeInTheDocument();
    const u = userEvent.setup();
    await u.click(within(chips).getByRole("button", { name: "Retirer le filtre agent" }));
    expect(JSON.parse(screen.getByTestId("url").textContent!).key_id).toBeUndefined();
    await u.click(screen.getByRole("button", { name: "Effacer" }));
    expect(JSON.parse(screen.getByTestId("url").textContent!)).toEqual({});
  });

  it("bascule un statut et le période depuis les puces", async () => {
    mockApi(baseRoutes([exec()]));
    renderApp(<Harness />);
    await screen.findAllByText("systemd.status");
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "refusée" }));
    await u.click(screen.getByRole("button", { name: "24h" }));
    expect(JSON.parse(screen.getByTestId("url").textContent!)).toMatchObject({ status: "denied", period: "24h" });
  });

  it("les liens d'export reprennent les filtres", async () => {
    mockApi(baseRoutes([exec()]));
    renderApp(<Harness initial={{ period: "7d", risk: "write" }} />);
    await screen.findAllByText("systemd.status");
    expect(screen.getByRole("link", { name: /CSV/ })).toHaveAttribute("href", "/api/v1/executions/export?period=7d&risk=write&format=csv");
    expect(screen.getByRole("link", { name: /NDJSON/ })).toHaveAttribute("href", expect.stringContaining("format=ndjson"));
  });

  it("atténue une ligne refusée et donne la raison au survol", async () => {
    mockApi(baseRoutes([exec({ id: "E2", status: "denied", denial_reason: "profile_forbids_risk", exit_code: null, duration_ms: null })]));
    renderApp(<Harness />);
    const row = (await screen.findAllByRole("row"))[1];
    expect(row).toHaveAttribute("title", "profile_forbids_risk");
    expect(row.className).toContain("opacity-70");
  });

  it("ouvre le tiroir: raison, commande rendue, chronologie, sortie, liens de filtre", async () => {
    mockApi(baseRoutes([exec()]));
    renderApp(<Harness />);
    const u = userEvent.setup();
    await u.click((await screen.findAllByRole("row"))[1]);
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByText("Alarme Zabbix #4521")).toBeInTheDocument();
    expect(within(dlg).getByText("systemctl status 'nginx'")).toBeInTheDocument();
    expect(within(dlg).getByText("Demandée par Agent N1")).toBeInTheDocument();
    expect(within(dlg).getByText(/Terminée \(exit 0, 0,4 s\)/)).toBeInTheDocument();
    expect(within(dlg).getByText("ligne deux")).toBeInTheDocument();
    await u.type(within(dlg).getByLabelText("Rechercher dans la sortie"), "un");
    expect(within(dlg).queryByText("ligne deux")).toBeNull();
    await u.click(within(dlg).getByRole("button", { name: "Voir toutes les exécutions de cet agent" }));
    expect(JSON.parse(screen.getByTestId("url").textContent!)).toMatchObject({ key_id: "k1" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("montre la validation humaine dans la chronologie", async () => {
    mockApi(baseRoutes([exec()], {
      "GET /executions/E1": () => ({ body: exec({ risk: "write", decided_at: "2026-10-18T13:42:30.000Z", decided_by_name: "Sébastien D.", decision_comment: "OK, redémarrage autorisé" }) }),
    }));
    renderApp(<Harness initial={{ exec: "E1" }} />);
    const dlg = await screen.findByRole("dialog");
    expect(await within(dlg).findByText("Validée par Sébastien D.")).toBeInTheDocument();
    expect(within(dlg).getByText(/OK, redémarrage autorisé/)).toBeInTheDocument();
  });

  it("direct: un événement SSE rafraîchit et surligne la nouvelle ligne; pause coupe le flux", async () => {
    let items = [exec()];
    mockApi(baseRoutes([], { "GET /executions": () => ({ body: { items, next_cursor: "" } }) }));
    renderApp(<Harness />);
    await screen.findAllByText("systemd.status");
    expect(FakeES.last?.url).toBe("/api/v1/executions/stream");
    items = [exec({ id: "E9", command_slug: "mysql.processlist" }), ...items];
    await act(async () => { FakeES.last!.emit({ id: "E9" }); await new Promise((r) => setTimeout(r, 400)); });
    const added = (await screen.findAllByRole("row"))[1];
    expect(added).toHaveTextContent("mysql.processlist");
    expect(added.className).toContain("bg-primary/10");
    const first = FakeES.last!;
    await userEvent.setup().click(screen.getByRole("button", { name: /Pause du direct/ }));
    await waitFor(() => expect(first.closed).toBe(true));
  });

  it("onglet Administration: libellé lisible et diff avant/après dans le tiroir", async () => {
    mockApi({
      ...baseRoutes([]),
      "GET /audit": () => ({ body: { items: [{ id: "A1", ts: "2026-10-18T13:00:00.000Z", actor_type: "user", actor_id: "u1", actor_name: "sd", action: "command.update", target_type: "command", target_id: "c1", target_name: "systemd.restart",
        diff: { before: { template: "systemctl restart {{service}}", risk: "write" }, after: { template: "systemctl reload {{service}}", risk: "write" } }, ip: "10.0.0.2", seq: 1 }], next_cursor: "" } }),
    });
    renderApp(<Harness initial={{ tab: "admin" }} />);
    expect((await screen.findAllByText(/a modifié la commande/)).length).toBeGreaterThan(0); // carte mobile + tableau
    await userEvent.setup().click(screen.getAllByText("systemd.restart")[0]);
    const dlg = await screen.findByRole("dialog");
    const removed = within(dlg).getByText("systemctl restart {{service}}").closest("[data-kind]")!;
    const added = within(dlg).getByText("systemctl reload {{service}}").closest("[data-kind]")!;
    expect(removed).toHaveAttribute("data-kind", "removed");
    expect(added).toHaveAttribute("data-kind", "added");
    expect(within(dlg).queryByText("write")).toBeNull(); // champ inchangé non affiché
  });
});

describe("diff", () => {
  it("compare avant/après, gère création et suppression", () => {
    expect(diffRows({ before: { a: 1, b: { c: 2 } }, after: { a: 1, b: { c: 3 }, d: 4 } })).toEqual([
      { path: "b.c", kind: "changed", before: 2, after: 3 }, { path: "d", kind: "added", after: 4 }]);
    expect(diffRows({ after: { name: "x" } })).toEqual([{ path: "name", kind: "added", after: "x" }]);
    expect(diffRows({ before: { name: "x" } })).toEqual([{ path: "name", kind: "removed", before: "x" }]);
    expect(diffRows({ expected: "a", received: "b" }).map((r) => r.kind)).toEqual(["added", "added"]);
    expect(diffRows(null)).toEqual([]);
  });
  it("marque +/− en plus de la couleur", async () => {
    renderApp(<DiffView diff={{ before: { x: "1" }, after: { x: "2" } }} />);
    expect(await screen.findByLabelText("supprimé")).toHaveTextContent("−");
    expect(screen.getByLabelText("ajouté")).toHaveTextContent("+");
  });
});

describe("réglages", () => {
  const admin = { id: "u1", username: "sd", display_name: "SD", role: "admin", theme: "system", csrf_token: "t" };
  it("vérifie l'intégrité et signale une altération", async () => {
    mockApi({
      "GET /auth/me": () => ({ body: admin }), "GET /settings/general": () => ({ body: { base_url: "", trusted_proxies: "", session_ttl_hours: 12, approval_ttl_minutes: 30, log_retention_days: 365, log_level: "info", has_metrics_token: false, detected_url: "http://x" } }), "GET /users": () => ({ body: { items: [] } }),
      "POST /integrity/verify": () => ({ body: { ok: false, executions: { ok: true, checked: 120 }, audit: { ok: false, checked: 7, bad_seq: 8, reason: "contenu altéré" } } }),
    });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "Général" }));
    await u.click(screen.getByRole("button", { name: "Vérifier l'intégrité" }));
    expect(await screen.findByText(/intègre \(120 lignes vérifiées\)/)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("altération détectée à la ligne 8 : contenu altéré");
  });

  it("crée un utilisateur et affiche le garde-fou « dernier admin »", async () => {
    let posted: unknown;
    mockApi({
      "GET /auth/me": () => ({ body: admin }),
      "GET /users": () => ({ body: { items: [{ id: "u1", username: "sd", display_name: "SD", role: "admin", disabled: false, last_login_at: "", created_at: "" }] } }),
      "POST /users": (b) => { posted = b; return { status: 201, body: {} }; },
      "PUT /users/u1": () => ({ status: 400, body: { error: { code: "last_admin", message: "il doit rester au moins un administrateur actif" } } }),
    });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: /Nouvel utilisateur/ }));
    const dlg = await screen.findByRole("dialog");
    await u.type(within(dlg).getByLabelText("Nom d'utilisateur"), "bob");
    await u.type(within(dlg).getByLabelText("Mot de passe"), "mot-de-passe-de-bob");
    await u.selectOptions(within(dlg).getByLabelText("Rôle"), "approver");
    await u.click(within(dlg).getByRole("button", { name: "Enregistrer" }));
    await waitFor(() => expect(posted).toMatchObject({ username: "bob", role: "approver", password: "mot-de-passe-de-bob" }));
  });

  it("n'expose pas les onglets d'administration aux autres rôles", async () => {
    mockApi({ "GET /auth/me": () => ({ body: { ...admin, role: "viewer" } }) });
    renderApp(<SettingsPage />);
    await screen.findByRole("tab", { name: "Mon compte" });
    await waitFor(() => expect(screen.queryByRole("tab", { name: "Utilisateurs" })).toBeNull());
  });
});
