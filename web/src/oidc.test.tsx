import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LoginPage } from "./pages/AuthPages";
import { SettingsPage } from "./pages/SettingsPage";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => { vi.unstubAllGlobals(); window.history.replaceState(null, "", "/"); });

const admin = { id: "u1", username: "sd", display_name: "SD", role: "admin", theme: "system", csrf_token: "t" };
const config = {
  enabled: true, name: "Keycloak", issuer: "https://sso.example.com/realms/leash", client_id: "leash", has_client_secret: true, scopes: "openid profile email",
  role_claim: "groups", role_map: { admin: ["/leash-admins"], approver: [], viewer: ["/leash-readers"] }, default_role: "viewer", jit: true,
  local_password_admins_only: false, end_session: true, redirect_url: "http://192.168.6.167:8080/api/v1/auth/oidc/callback",
};

describe("connexion SSO", () => {
  it("propose le bouton du fournisseur et garde le formulaire local en secours", async () => {
    mockApi({ "GET /auth/providers": () => ({ body: { oidc: { name: "Keycloak", start_url: "/api/v1/auth/oidc/start" }, local_password_admins_only: true } }) });
    renderApp(<LoginPage onDone={() => {}} />);
    const link = await screen.findByRole("link", { name: "Se connecter avec Keycloak" });
    expect(link).toHaveAttribute("href", "/api/v1/auth/oidc/start");
    expect(screen.getByLabelText("Nom d'utilisateur")).toBeInTheDocument();
    expect(screen.getByText(/réservé aux administrateurs locaux/)).toBeInTheDocument();
  });

  it("sans SSO, aucun bouton fournisseur", async () => {
    mockApi({ "GET /auth/providers": () => ({ body: { oidc: null, local_password_admins_only: false } }) });
    renderApp(<LoginPage onDone={() => {}} />);
    await screen.findByLabelText("Nom d'utilisateur");
    expect(screen.queryByRole("link", { name: /Se connecter avec/ })).not.toBeInTheDocument();
  });

  it("affiche le motif d'un échec SSO puis nettoie l'URL", async () => {
    window.history.replaceState(null, "", "/?sso_error=role_not_mapped");
    mockApi({ "GET /auth/providers": () => ({ body: { oidc: { name: "Keycloak", start_url: "/x" }, local_password_admins_only: false } }) });
    renderApp(<LoginPage onDone={() => {}} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(/aucun rôle dans Leash/);
    await waitFor(() => expect(window.location.search).toBe(""));
  });

  it("repli générique pour un motif inconnu", async () => {
    window.history.replaceState(null, "", "/?sso_error=motif_inconnu");
    mockApi({ "GET /auth/providers": () => ({ status: 500, body: {} }) });
    renderApp(<LoginPage onDone={() => {}} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("La connexion SSO a échoué.");
  });
});

describe("réglages SSO", () => {
  const setup = (extra: Record<string, (b: unknown) => { status?: number; body?: unknown } | undefined> = {}) =>
    mockApi({ "GET /auth/me": () => ({ body: admin }), "GET /users": () => ({ body: { items: [] } }), "GET /settings/oidc": () => ({ body: config }), ...extra });

  it("n'expose jamais le secret, affiche les URL à copier et enregistre le mapping", async () => {
    let saved: Record<string, unknown> | undefined;
    setup({ "PUT /settings/oidc": (b) => { saved = b as Record<string, unknown>; return { body: config }; } });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "SSO" }));
    expect(await screen.findByLabelText("Issuer (URL du fournisseur)")).toHaveValue(config.issuer);
    const secret = screen.getByLabelText("Secret client");
    expect(secret).toHaveValue("");
    expect(secret).toHaveAttribute("type", "password");
    expect(secret).toHaveAttribute("placeholder", expect.stringContaining("conserver"));
    // URL de redirection et de déconnexion à déclarer chez le fournisseur
    expect(screen.getByText(config.redirect_url)).toBeInTheDocument();
    expect(screen.getByText("http://192.168.6.167:8080/")).toBeInTheDocument();
    expect(screen.queryByText(/Issuer en HTTP/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Enregistrer" })).toBeDisabled();

    // mapping: une valeur par ligne
    expect(screen.getByLabelText("Valeurs → Administrateur")).toHaveValue("/leash-admins");
    await u.type(screen.getByLabelText("Valeurs → Validateur"), "/leash-ops");
    await u.type(screen.getByLabelText("Secret client"), "nouveau-secret");
    await u.selectOptions(screen.getByLabelText("Sans correspondance"), "");
    await u.click(screen.getByLabelText("Réserver le mot de passe local aux administrateurs locaux"));
    await u.click(screen.getByRole("button", { name: "Enregistrer" }));
    await waitFor(() => expect(saved).toBeDefined());
    expect(saved).toMatchObject({ client_secret: "nouveau-secret", clear_client_secret: false, default_role: "", local_password_admins_only: true, role_claim: "groups",
      role_map: { admin: ["/leash-admins"], approver: ["/leash-ops"], viewer: ["/leash-readers"] } });
    expect(saved).not.toHaveProperty("has_client_secret");
    expect(saved).not.toHaveProperty("redirect_url");
  });

  it("avertit d'un issuer HTTP et affiche le résultat du test de découverte", async () => {
    const calls = setup({
      "POST /settings/oidc/test": () => ({ body: { ok: true, issuer: "http://kc.lan/realms/leash", authorization_endpoint: "http://kc.lan/auth", token_endpoint: "http://kc.lan/token", jwks_uri: "http://kc.lan/certs", jwks_keys: 2, warnings: ["issuer_http", "no_end_session"] } }),
    });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "SSO" }));
    const issuer = await screen.findByLabelText("Issuer (URL du fournisseur)");
    await u.clear(issuer);
    await u.type(issuer, "http://kc.lan/realms/leash");
    expect(screen.getByText(/Issuer en HTTP/)).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Tester la découverte" }));
    expect(await screen.findByText(/Découverte réussie/)).toBeInTheDocument();
    expect(screen.getByText("http://kc.lan/token")).toBeInTheDocument();
    expect(screen.getByText("L'issuer est en HTTP, sans chiffrement.")).toBeInTheDocument();
    expect(screen.getByText(/Pas de déconnexion fournisseur/)).toBeInTheDocument();
    expect(calls.find((c) => c.key === "POST /settings/oidc/test")?.body).toEqual({ issuer: "http://kc.lan/realms/leash" });
  });

  it("affiche l'échec de la découverte", async () => {
    setup({ "POST /settings/oidc/test": () => ({ body: { ok: false, issuer: "x", error: "découverte impossible: connection refused" } }) });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "SSO" }));
    await u.click(await screen.findByRole("button", { name: "Tester la découverte" }));
    expect(await screen.findByText("Échec : découverte impossible: connection refused")).toBeInTheDocument();
  });

  it("efface le secret enregistré sur demande", async () => {
    let saved: Record<string, unknown> | undefined;
    setup({ "PUT /settings/oidc": (b) => { saved = b as Record<string, unknown>; return { body: config }; } });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "SSO" }));
    await u.click(await screen.findByLabelText("Supprimer le secret enregistré"));
    await u.click(screen.getByRole("button", { name: "Enregistrer" }));
    await waitFor(() => expect(saved).toMatchObject({ clear_client_secret: true, client_secret: "" }));
  });

  it("n'est pas proposé aux non-administrateurs", async () => {
    mockApi({ "GET /auth/me": () => ({ body: { ...admin, role: "viewer" } }) });
    renderApp(<SettingsPage />);
    await screen.findByRole("tab", { name: "Apparence" });
    expect(screen.queryByRole("tab", { name: "SSO" })).not.toBeInTheDocument();
  });
});

describe("comptes SSO dans la liste des utilisateurs", () => {
  it("marque le compte SSO et verrouille son rôle et son mot de passe", async () => {
    const sso = { id: "u2", username: "alice", display_name: "Alice", role: "approver", disabled: false, theme: "system", created_at: "", last_login_at: "", auth_provider: "oidc" };
    const local = { ...sso, id: "u3", username: "bob", display_name: "Bob", auth_provider: "local" };
    mockApi({ "GET /auth/me": () => ({ body: admin }), "GET /users": () => ({ body: { items: [sso, local] } }) });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    const row = (await screen.findByText("alice")).closest("tr") as HTMLElement;
    expect(within(row).getByText("SSO")).toBeInTheDocument();
    expect(within((await screen.findByText("bob")).closest("tr") as HTMLElement).queryByText("SSO")).not.toBeInTheDocument();
    await u.click(within(row).getByRole("button", { name: "Modifier" }));
    expect(screen.getByLabelText("Rôle")).toBeDisabled();
    expect(screen.queryByLabelText(/Nouveau mot de passe/)).not.toBeInTheDocument();
    expect(screen.getByText(/Compte SSO/)).toBeInTheDocument();
  });
});
