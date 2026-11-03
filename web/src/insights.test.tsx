import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InsightsSection, bucketByDay, heatmap } from "./components/insights";
import { fmtMs, fmtSeconds } from "./lib/format";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const insights = (over = {}) => ({
  days: 30, requests: 120, succeeded: 100, failed: 5, denied: 15, executed: 105, avg_ms: 850, p50_ms: 300, p95_ms: 4200, max_ms: 9000, approvals: 4, avg_approval_seconds: 72,
  hourly: [{ hour: "2026-11-02T10", ok: 3, ko: 1, denied: 0, other: 0 }],
  top_commands: [{ name: "system.uptime", count: 60, failed: 0, avg_ms: 120, servers: 6 }, { name: "pve.cluster_status", count: 30, failed: 2, avg_ms: 900, servers: 4 }],
  top_servers: [{ name: "pve01-ssh-root", count: 50, failed: 1, avg_ms: 400, servers: 12 }],
  top_keys: [{ name: "N1", count: 100, failed: 5, avg_ms: 500, servers: 20 }],
  slowest_commands: [{ name: "system.dir_sizes", count: 5, failed: 0, avg_ms: 8000 }],
  denial_reasons: [{ name: "no_matching_rule", count: 10, failed: 0, avg_ms: 0 }],
  risk: { read: 90, write: 14, destructive: 1 }, ...over,
});

describe("statistiques d'usage", () => {
  it("affiche durée moyenne, taux de succès, classements et change de période", async () => {
    const asked: string[] = [];
    mockApi({ "GET /dashboard/insights": () => ({ body: insights() }) });
    const orig = globalThis.fetch;
    vi.stubGlobal("fetch", (u: RequestInfo | URL, i?: RequestInit) => { asked.push(String(u)); return orig(u, i); });
    renderApp(<InsightsSection />);
    expect(await screen.findByText("850 ms")).toBeInTheDocument(); // durée moyenne
    expect(screen.getByText(/médiane 300 ms · 95 % sous 4,2 s/)).toBeInTheDocument();
    expect(screen.getByText("95,2 %")).toBeInTheDocument(); // 100 / (100 + 5)
    expect(screen.getByText("1 min 12 s")).toBeInTheDocument(); // délai de validation
    const rows = screen.getAllByTestId("top-row");
    expect(rows).toHaveLength(4);
    expect(within(rows[0]).getByText("system.uptime")).toBeInTheDocument();
    expect(within(rows[1]).getByText(/2 échec/)).toBeInTheDocument();
    expect(screen.getByText("Aucune règle ne l'autorise")).toBeInTheDocument(); // cause de refus traduite
    await userEvent.setup().click(screen.getByRole("button", { name: "7 jours" }));
    await vi.waitFor(() => expect(asked.some((u) => u.includes("/dashboard/insights?days=7"))).toBe(true));
  });

  it("état vide sans activité", async () => {
    mockApi({ "GET /dashboard/insights": () => ({ body: insights({ requests: 0, hourly: [] }) }) });
    renderApp(<InsightsSection />);
    expect(await screen.findByText(/Aucune demande sur les 30 derniers jours/)).toBeInTheDocument();
  });
});

describe("agrégation locale", () => {
  it("regroupe les heures par jour (jours sans activité inclus) et par jour de semaine × heure", () => {
    const now = new Date(2026, 10, 2, 15); // 2 novembre 2026 (local)
    const mid = (d: Date, h: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate(), h);
    const iso = (d: Date) => d.toISOString().slice(0, 13);
    const h1 = iso(mid(now, 12)), h2 = iso(mid(new Date(2026, 10, 1), 12));
    const hourly = [{ hour: h1, ok: 2, ko: 1, denied: 0, other: 0 }, { hour: h2, ok: 0, ko: 0, denied: 3, other: 0 }];
    const days = bucketByDay(hourly, 7, now);
    expect(days).toHaveLength(7);
    expect(days[6]).toMatchObject({ ok: 2, ko: 1 });
    expect(days[5]).toMatchObject({ denied: 3 });
    expect(days.slice(0, 5).every((d) => d.ok + d.ko + d.denied === 0)).toBe(true);
    const grid = heatmap(hourly);
    expect(grid[0][12]).toBe(3); // lundi 2 novembre 2026, 12 h
    expect(grid[6][12]).toBe(3); // dimanche 1er novembre
  });

  it("formate les durées", () => {
    expect([fmtMs(320), fmtMs(1200), fmtMs(72000), fmtSeconds(45), fmtSeconds(3600)]).toEqual(["320 ms", "1,2 s", "1 min 12 s", "45 s", "1 h"]);
  });
});
