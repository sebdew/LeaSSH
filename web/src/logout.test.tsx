import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useSessionActions } from "./lib/useAuth";
import { mockApi } from "./test-utils";

const user = { id: "u1", username: "sd", role: "admin" };

function setup() {
  const qc = new QueryClient();
  qc.setQueryData(["me"], user);
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  const { result } = renderHook(() => useSessionActions(), { wrapper });
  return { qc, result };
}

describe("déconnexion SSO", () => {
  const orig = window.location;
  afterEach(() => Object.defineProperty(window, "location", { configurable: true, value: orig }));

  it("part vers la fermeture de session du fournisseur sans afficher la page de connexion (sinon l'auto-redirect SSO reconnecte)", async () => {
    const assign = vi.fn();
    Object.defineProperty(window, "location", { configurable: true, value: { ...orig, assign } });
    mockApi({ "POST /auth/logout": () => ({ body: { end_session_url: "https://idp.example/logout?client_id=leassh" } }) });
    const { qc, result } = setup();
    await act(() => result.current.logout());
    expect(assign).toHaveBeenCalledWith("https://idp.example/logout?client_id=leassh");
    expect(qc.getQueryData(["me"])).toEqual(user); // `me` à null ferait rendre LoginPage et son auto-redirect
  });

  it("sans fermeture de session fournisseur, l'utilisateur est simplement déconnecté", async () => {
    mockApi({ "POST /auth/logout": () => ({ status: 204 }) });
    const { qc, result } = setup();
    await act(() => result.current.logout());
    expect(qc.getQueryData(["me"])).toBeNull();
  });
});
