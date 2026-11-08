import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./i18n";
import App from "./App";
import { passwordStrength } from "./lib/theme";

function mockFetch(routes: Record<string, () => { status?: number; body?: unknown }>) {
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url.replace("/api/v1", "")}`;
    const r = routes[key]?.() ?? { status: 404, body: {} };
    return { ok: (r.status ?? 200) < 400, status: r.status ?? 200, json: async () => r.body ?? {} } as Response;
  }));
}
const wrap = () => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><App /></QueryClientProvider>);
afterEach(() => vi.unstubAllGlobals());

describe("passwordStrength", () => {
  it("classe les mots de passe", () => {
    expect(passwordStrength("court")).toBe(0);
    expect(passwordStrength("aaaaaaaaaa")).toBe(1);
    expect(passwordStrength("Abcdefgh12345!")).toBe(2);
    expect(passwordStrength("Abcdefgh12345!xyz")).toBe(3);
  });
});

describe("App", () => {
  it("affiche l'écran d'initialisation au premier lancement", async () => {
    mockFetch({ "GET /setup/status": () => ({ body: { initialized: false } }) });
    wrap();
    expect(await screen.findByRole("heading", { name: /Bienvenue/ })).toBeInTheDocument();
    expect(screen.getByText(/LEASSH_MASTER_KEY/)).toBeInTheDocument();
  });

  it("envoie le jeton d'initialisation avec la création du compte", async () => {
    let sent: Record<string, string> = {};
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? "GET"} ${url.replace("/api/v1", "")}`;
      if (key === "POST /setup") sent = JSON.parse(String(init?.body));
      const body = key === "GET /setup/status" ? { initialized: false } : key === "POST /setup" ? { id: "u1", username: "admin", role: "admin", csrf_token: "c" } : {};
      return { ok: true, status: 200, json: async () => body } as Response;
    }));
    wrap();
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("Jeton d'initialisation"), " tok-123 ");
    await u.type(screen.getByLabelText("Nom d'utilisateur"), "admin");
    await u.type(screen.getByLabelText("Mot de passe"), "Un-mot-de-passe-solide1");
    await u.type(screen.getByLabelText("Confirmation"), "Un-mot-de-passe-solide1");
    await u.click(screen.getByRole("button", { name: "Créer le compte" }));
    await waitFor(() => expect(sent.setup_token).toBe("tok-123"));
  });

  it("bloque la création si les mots de passe diffèrent", async () => {
    mockFetch({ "GET /setup/status": () => ({ body: { initialized: false } }) });
    wrap();
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("Nom d'utilisateur"), "admin");
    await u.type(screen.getByLabelText("Mot de passe"), "Un-mot-de-passe-solide1");
    await u.type(screen.getByLabelText("Confirmation"), "autre");
    expect(screen.getByRole("alert")).toHaveTextContent(/ne correspondent pas/);
    expect(screen.getByRole("button", { name: "Créer le compte" })).toBeDisabled();
  });

  it("affiche la connexion puis le layout une fois authentifié", async () => {
    mockFetch({
      "GET /setup/status": () => ({ body: { initialized: true } }),
      "GET /auth/me": () => ({ status: 401, body: { error: { code: "unauthenticated", message: "x" } } }),
      "POST /auth/login": () => ({ body: { id: "1", username: "admin", display_name: "admin", role: "admin", theme: "system", csrf_token: "t" } }),
    });
    wrap();
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("Nom d'utilisateur"), "admin");
    await u.type(screen.getByLabelText("Mot de passe"), "x");
    await u.click(screen.getByRole("button", { name: "Se connecter" }));
    await waitFor(() => expect(screen.getByRole("navigation", { name: "Navigation principale" })).toBeInTheDocument());
    expect(screen.getByRole("link", { name: /Validations/ })).toBeInTheDocument();
  });
});
