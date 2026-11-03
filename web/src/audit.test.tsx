import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuditPage, presets } from "./pages/AuditPage";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const report = (over = {}) => ({ id: "01R1", created_at: "2026-11-03T10:00:00.000Z", created_by: "u1", created_by_name: "alice", period_from: "2026-10-01", period_to: "2026-10-31", leash_version: "1.5.0",
  schema_version: 1, sha256: "a".repeat(64), findings_high: 1, findings_medium: 2, findings_low: 3, summary_model: "", summary_at: "", has_summary: false, ...over });
const aiCfg = (over = {}) => ({ enabled: false, url: "", model: "", has_key: false, ...over });
const routes = (reports = [report()], ai = aiCfg()) => ({
  "GET /audit-reports": () => ({ body: { items: reports } }),
  "GET /settings/audit-ai": () => ({ body: { config: ai } }),
});

describe("écran Audit", () => {
  it("rappelle que c'est une aide à l'audit et liste les rapports avec leurs liens", async () => {
    mockApi(routes());
    renderApp(<AuditPage />);
    expect(await screen.findByText(/Aide à l'audit, pas une certification/)).toBeInTheDocument();
    const row = (await screen.findByText("alice")).closest("tr")!;
    expect(within(row).getByLabelText("1 constats élevés, 2 moyens, 3 faibles")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: /Ouvrir/ })).toHaveAttribute("href", "/api/v1/audit-reports/01R1/html");
    expect(within(row).getByRole("link", { name: /JSON signé/ })).toHaveAttribute("href", "/api/v1/audit-reports/01R1/json");
    expect(within(row).queryByRole("button", { name: /Synthèse IA/ })).toBeNull(); // IA désactivée: pas de bouton
  });

  it("génère un rapport avec la période choisie", async () => {
    const calls = mockApi({ ...routes(), "POST /audit-reports": () => ({ status: 201, body: report() }) });
    renderApp(<AuditPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "Mois précédent" }));
    const lastMonth = presets().find((p) => p.id === "lastMonth")!;
    expect(screen.getByLabelText("Du")).toHaveValue(lastMonth.from);
    await u.click(screen.getByRole("button", { name: /Générer le rapport/ }));
    await vi.waitFor(() => expect(calls.some((c) => c.key === "POST /audit-reports")).toBe(true));
    expect(calls.find((c) => c.key === "POST /audit-reports")?.body).toEqual({ from: lastMonth.from, to: lastMonth.to });
  });

  it("n'autorise pas une période inversée", async () => {
    mockApi(routes());
    renderApp(<AuditPage />);
    const u = userEvent.setup();
    const from = await screen.findByLabelText("Du");
    await u.clear(from);
    await u.type(from, "2999-01-01");
    expect(screen.getByRole("button", { name: /Générer le rapport/ })).toBeDisabled();
  });

  it("propose la synthèse IA seulement quand elle est activée, et l'étiquette comme telle", async () => {
    const calls = mockApi({ ...routes([report(), report({ id: "01R2", created_by_name: "bob", has_summary: true, summary_model: "gpt-x" })], aiCfg({ enabled: true, url: "http://llm/v1", model: "gpt-x", has_key: true })),
      "POST /audit-reports/01R1/summary": () => ({ body: { summary: "x", model: "gpt-x" } }) });
    renderApp(<AuditPage />);
    const u = userEvent.setup();
    const rowA = (await screen.findByText("alice")).closest("tr")!;
    await u.click(within(rowA).getByRole("button", { name: "Synthèse IA" }));
    await vi.waitFor(() => expect(calls.some((c) => c.key === "POST /audit-reports/01R1/summary")).toBe(true));
    const rowB = screen.getByText("bob").closest("tr")!;
    expect(within(rowB).getByText("Synthèse IA (gpt-x)")).toBeInTheDocument();
    expect(within(rowB).getByRole("button", { name: "Régénérer la synthèse" })).toBeInTheDocument();
  });

  it("réglages IA: désactivés par défaut, avertissement visible, clé jamais renvoyée, enregistrement", async () => {
    let sent: Record<string, unknown> = {};
    mockApi({ ...routes([], aiCfg({ has_key: true })), "PUT /settings/audit-ai": (b) => { sent = b as Record<string, unknown>; return { body: { config: aiCfg() } }; } });
    renderApp(<AuditPage />);
    const u = userEvent.setup();
    expect(await screen.findByText(/Si l'URL est externe, ces chiffres quittent votre réseau/)).toBeInTheDocument();
    expect(screen.getByLabelText("Activer la synthèse IA")).not.toBeChecked();
    await vi.waitFor(() => expect(screen.getByLabelText("Clé d'API")).toHaveAttribute("placeholder", "Laisser vide pour conserver la clé actuelle"));
    await u.click(screen.getByLabelText("Activer la synthèse IA"));
    await u.type(screen.getByLabelText("URL de l'API"), "http://litellm:4000/v1");
    await u.type(screen.getByLabelText("Modèle"), "mon-modele");
    await u.click(screen.getByRole("button", { name: "Enregistrer" }));
    await vi.waitFor(() => expect(sent).toEqual({ enabled: true, url: "http://litellm:4000/v1", model: "mon-modele", api_key: "" }));
  });

  it("vérifie un fichier: intact et authentique, ou modifié", async () => {
    let verdict: Record<string, unknown> = { digest_ok: true, tag_ok: true, message: "Rapport authentique et intact: émis par cette instance, contenu inchangé." };
    mockApi({ ...routes(), "POST /audit-reports/verify": () => ({ body: verdict }) });
    renderApp(<AuditPage />);
    const u = userEvent.setup();
    const input = await screen.findByLabelText("Choisir un fichier JSON", { selector: "input" });
    await u.upload(input, new File(['{"format":"leash-audit-report/1"}'], "rapport.json", { type: "application/json" }));
    expect(await screen.findByText(/Rapport authentique et intact/)).toBeInTheDocument();
    verdict = { digest_ok: false, tag_ok: false, message: "Le contenu a été modifié: son empreinte ne correspond pas à celle du rapport d'origine." };
    await u.upload(input, new File(['{"format":"leash-audit-report/1","x":1}'], "modifie.json", { type: "application/json" }));
    expect(await screen.findByText(/Le contenu a été modifié/)).toBeInTheDocument();
  });
});
