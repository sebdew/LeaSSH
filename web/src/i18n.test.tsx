import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import fr from "./locales/fr.json";
import en from "./locales/en.json";
import { applyLang, currentLocale, decimal, percent } from "./lib/lang";
import { fmtMs } from "./lib/format";
import { SettingsPage } from "./pages/SettingsPage";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => { applyLang("fr"); vi.unstubAllGlobals(); });

type Tree = { [k: string]: string | Tree };
const PLURAL = /_(zero|one|two|few|many|other)$/;
// Clés à plat; les suffixes de pluriel sont ramenés à la clé de base (le français et l'anglais n'ont pas les mêmes formes).
function flat(t: Tree, p = ""): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(t)) {
    if (typeof v === "string") out[p + k] = v;
    else Object.assign(out, flat(v, `${p}${k}.`));
  }
  return out;
}
const base = (m: Record<string, string>) => new Set(Object.keys(m).map((k) => k.replace(PLURAL, "")));
const vars = (s: string) => [...s.matchAll(/\{\{\s*(\w+)\s*\}\}/g)].map((m) => m[1]).sort().join(",");

describe("traductions", () => {
  const f = flat(fr as Tree), e = flat(en as Tree);

  it("le français et l'anglais ont exactement les mêmes clés", () => {
    const bf = base(f), be = base(e);
    expect([...bf].filter((k) => !be.has(k))).toEqual([]);
    expect([...be].filter((k) => !bf.has(k))).toEqual([]);
  });

  it("aucun texte vide et les mêmes variables d'interpolation dans les deux langues", () => {
    for (const [k, v] of Object.entries(e)) expect(v.trim(), `en.${k} vide`).not.toBe("");
    for (const k of Object.keys(f)) {
      const ek = e[k] !== undefined ? k : Object.keys(e).find((x) => x.replace(PLURAL, "") === k.replace(PLURAL, ""));
      if (ek) expect(vars(e[ek]), `variables de ${k}`).toBe(vars(f[k]));
    }
  });

  it("les formats suivent la langue", () => {
    expect([decimal("1.2"), percent(95), fmtMs(1200), currentLocale()]).toEqual(["1,2", "95 %", "1,2 s", "fr-FR"]);
    applyLang("en");
    expect([decimal("1.2"), percent(95), fmtMs(1200), currentLocale()]).toEqual(["1.2", "95%", "1.2 s", "en-GB"]);
  });
});

describe("choix de la langue", () => {
  it("bascule l'interface en anglais depuis Réglages → Apparence et mémorise le choix", async () => {
    mockApi({ "GET /auth/me": () => ({ body: { id: "u1", username: "sd", display_name: "SD", role: "admin", theme: "system", csrf_token: "t" } }), "GET /users": () => ({ body: { items: [] } }) });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "Apparence" }));
    await u.selectOptions(screen.getByLabelText("Langue"), "en");
    expect(await screen.findByRole("tab", { name: "Appearance" })).toBeInTheDocument();
    expect(screen.getByLabelText("Language")).toHaveValue("en");
    expect(localStorage.getItem("leassh-lang")).toBe("en");
    expect(document.documentElement.lang).toBe("en");
    await u.selectOptions(screen.getByLabelText("Language"), "fr");
    await waitFor(() => expect(screen.getByRole("tab", { name: "Apparence" })).toBeInTheDocument());
    expect(localStorage.getItem("leassh-lang")).toBe("fr");
  });
});

describe("langue des échanges avec le serveur", () => {
  it("annonce la langue à l'API (messages d'erreur traduits côté serveur)", async () => {
    const seen: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (_u: string, init?: RequestInit) => {
      seen.push((init?.headers as Record<string, string>)["Accept-Language"]);
      return { ok: true, status: 200, json: async () => ({}) } as Response;
    }));
    const { api } = await import("./lib/api");
    await api("/meta");
    applyLang("en");
    await api("/meta");
    expect(seen).toEqual(["fr", "en"]);
  });
});
