import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SettingsPage } from "./pages/SettingsPage";
import { mockApi, renderApp } from "./test-utils";

const admin = { id: "u1", username: "sd", display_name: "SD", role: "admin", theme: "system", csrf_token: "t", auth_provider: "local" };
const general = { base_url: "", trusted_proxies: "", session_ttl_hours: 12, approval_ttl_minutes: 30, log_retention_days: 365, log_level: "info", has_metrics_token: false, detected_url: "http://192.168.6.167:8080" };

describe("réglages généraux", () => {
  it("enregistre l'URL publique, les proxies de confiance et un jeton /metrics", async () => {
    let saved: Record<string, unknown> | undefined;
    mockApi({
      "GET /auth/me": () => ({ body: admin }),
      "GET /users": () => ({ body: [] }),
      "GET /settings/general": () => ({ body: general }),
      "PUT /settings/general": (b) => { saved = b as Record<string, unknown>; return { body: general }; },
    });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "Général" }));
    await u.type(await screen.findByLabelText("URL publique"), "https://leassh.example.com");
    await u.type(screen.getByLabelText("Reverse proxies de confiance"), "10.0.0.0/8");
    await u.type(screen.getByLabelText("Jeton de /metrics"), "secret-token");
    await u.click(screen.getByRole("button", { name: "Enregistrer" }));
    await waitFor(() => expect(saved).toMatchObject({ base_url: "https://leassh.example.com", trusted_proxies: "10.0.0.0/8", metrics_token: "secret-token", session_ttl_hours: 12 }));
    expect(saved).not.toHaveProperty("detected_url");
  });

  it("un compte SSO n'a pas de formulaire de changement de mot de passe", async () => {
    mockApi({ "GET /auth/me": () => ({ body: { ...admin, role: "viewer", auth_provider: "oidc" } }) });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "Mon compte" }));
    expect(await screen.findByText(/connecté par SSO/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Mot de passe actuel")).not.toBeInTheDocument();
  });
});
