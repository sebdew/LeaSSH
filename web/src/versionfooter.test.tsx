import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VersionFooter } from "./components/VersionFooter";
import { renderApp, mockApi } from "./test-utils";

afterEach(() => vi.unstubAllGlobals());

describe("pied de barre latérale (#43)", () => {
  it("affiche la version, l'auteur et un lien vers le dépôt GitHub", async () => {
    mockApi({ "GET /meta": () => ({ body: { approval_ttl_seconds: 1800, base_url: "", log_retention_days: 365, version: "v1.2.3", author: "Sébastien Dewarlez", repository_url: "https://github.com/sebdew/LeaSSH" } }) });
    renderApp(<VersionFooter />);
    expect(await screen.findByText(/v1\.2\.3/)).toBeInTheDocument();
    expect(screen.getByText(/Sébastien Dewarlez/)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /GitHub/ });
    expect(link).toHaveAttribute("href", "https://github.com/sebdew/LeaSSH");
    expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("n'affiche rien tant que la version est inconnue", () => {
    mockApi({ "GET /meta": () => ({ body: {} }) });
    renderApp(<VersionFooter />);
    expect(screen.queryByRole("link", { name: /GitHub/ })).toBeNull();
  });

  it("signale une nouvelle version avec un lien vers sa release (#86)", async () => {
    mockApi({ "GET /meta": () => ({ body: { version: "v0.1.0", author: "A", repository_url: "https://github.com/sebdew/LeaSSH", update_available: true, latest_version: "v0.2.0", release_url: "https://github.com/sebdew/LeaSSH/releases/tag/v0.2.0" } }) });
    renderApp(<VersionFooter />);
    const link = await screen.findByRole("link", { name: /v0\.2\.0/ });
    expect(link).toHaveAttribute("href", "https://github.com/sebdew/LeaSSH/releases/tag/v0.2.0");
    expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
  });

  it("ne signale rien quand la version est à jour", async () => {
    mockApi({ "GET /meta": () => ({ body: { version: "v0.2.0", author: "A", repository_url: "https://github.com/sebdew/LeaSSH", update_available: false, latest_version: "v0.2.0", release_url: "https://github.com/sebdew/LeaSSH/releases/tag/v0.2.0" } }) });
    renderApp(<VersionFooter />);
    await screen.findByText(/v0\.2\.0/);
    expect(screen.queryByRole("link", { name: /disponible|available/i })).toBeNull();
  });
});
