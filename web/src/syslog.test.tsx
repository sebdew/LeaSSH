import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsPage } from "./pages/SettingsPage";
import { mockApi, renderApp } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

const admin = { id: "u1", username: "sd", display_name: "SD", role: "admin", theme: "system", csrf_token: "t" };
const config = {
  enabled: true, protocol: "udp", host: "wazuh.example.com", port: 514, message_format: "json", framing: "lf", facility: "local0", app_name: "leash", hostname: "",
  min_severity: "info", types: ["execution", "proposal", "audit"], tls_ca_pem: "", tls_server_name: "", tls_skip_verify: false, tls_client_cert_pem: "", has_tls_client_key: true,
};
const options = { facilities: ["local0", "local1"], severities: ["warning", "info"], types: ["execution", "proposal", "audit"] };
const status = { state: "retrying", queued: 12, sent: 340, dropped: 2, last_error: "connexion: connection refused", last_error_at: "2026-10-18T10:05:00.000Z", last_success_at: "2026-10-18T10:00:00.000Z" };

describe("syslog", () => {
  it("affiche l'état et la dernière erreur, n'expose pas la clé, enregistre puis teste l'envoi", async () => {
    let saved: Record<string, unknown> | undefined;
    mockApi({
      "GET /auth/me": () => ({ body: admin }), "GET /users": () => ({ body: { items: [] } }),
      "GET /settings/syslog": () => ({ body: { config, status, options } }),
      "PUT /settings/syslog": (b) => { saved = b as Record<string, unknown>; return { body: { config, status, options } }; },
      "POST /settings/syslog/test": () => ({ body: { ok: false, error: "connexion: i/o timeout" } }),
    });
    renderApp(<SettingsPage />);
    const u = userEvent.setup();
    await u.click(await screen.findByRole("tab", { name: "Syslog" }));
    expect(await screen.findByLabelText("Serveur")).toHaveValue("wazuh.example.com");
    expect(screen.getByText("nouvelle tentative")).toBeInTheDocument();
    expect(screen.getByText("connexion: connection refused")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
    // UDP: pas de cadrage ni de section TLS
    expect(screen.queryByLabelText("Cadrage TCP")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Clé privée du certificat client (PEM)")).not.toBeInTheDocument();
    expect(screen.getByText(/UDP n'accuse pas réception/)).toBeInTheDocument();

    // le test est possible sans modification
    await u.click(screen.getByRole("button", { name: "Envoyer un message de test" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Échec : connexion: i/o timeout");

    // passage en TLS: port 6514, section TLS, clé jamais préremplie
    await u.selectOptions(screen.getByLabelText("Protocole"), "tls");
    expect(screen.getByLabelText("Port")).toHaveValue(6514);
    const key = screen.getByLabelText("Clé privée du certificat client (PEM)");
    expect(key).toHaveValue("");
    expect(key).toHaveAttribute("placeholder", expect.stringContaining("conserver"));
    await u.click(screen.getByLabelText("Ne pas vérifier le certificat du serveur"));
    expect(screen.getAllByRole("alert").some((a) => /n'importe quel serveur/.test(a.textContent ?? ""))).toBe(true);
    await u.click(screen.getByLabelText("Ne pas vérifier le certificat du serveur"));
    // tester exige d'enregistrer d'abord
    expect(screen.getByRole("button", { name: "Envoyer un message de test" })).toBeDisabled();
    await u.click(screen.getByLabelText("Journal d'administration"));
    await u.click(screen.getByRole("button", { name: "Enregistrer" }));
    await waitFor(() => expect(saved).toMatchObject({ protocol: "tls", port: 6514, host: "wazuh.example.com", types: ["execution", "proposal"], tls_client_key_pem: "", clear_tls_client_key: false }));
  });
});
