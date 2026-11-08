import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, setCsrf, type User } from "./api";

export function useSetupStatus() {
  return useQuery({ queryKey: ["setup"], queryFn: () => api<{ initialized: boolean }>("/setup/status") });
}

export function useMe(enabled: boolean) {
  return useQuery({
    queryKey: ["me"], enabled, retry: false,
    queryFn: async () => {
      try {
        const u = await api<User>("/auth/me");
        setCsrf(u.csrf_token ?? "");
        return u;
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null;
        throw e;
      }
    },
  });
}

export function useSessionActions() {
  const qc = useQueryClient();
  return {
    onAuthenticated: (u: User) => {
      setCsrf(u.csrf_token ?? "");
      qc.setQueryData(["setup"], { initialized: true });
      qc.setQueryData(["me"], u);
    },
    logout: async () => {
      const res = await api<{ end_session_url?: string } | undefined>("/auth/logout", { method: "POST" });
      setCsrf("");
      // Compte SSO: la session locale est déjà détruite; on part fermer celle du fournisseur. `me` reste en place:
      // le passer à null afficherait la page de connexion, dont la redirection automatique vers le SSO
      // pourrait partir après cette navigation et reconnecter l'utilisateur.
      if (res?.end_session_url) {
        window.location.assign(res.end_session_url);
        return;
      }
      qc.setQueryData(["me"], null);
    },
  };
}
