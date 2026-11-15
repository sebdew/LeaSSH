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
});
