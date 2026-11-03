import { currentLang } from "./lang";

export interface User {
  id: string;
  username: string;
  display_name: string;
  role: "admin" | "approver" | "viewer";
  theme: string;
  auth_provider?: "local" | "oidc";
  csrf_token?: string;
}

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
  }
}

let csrf = "";
export const setCsrf = (t: string) => { csrf = t; };
export const getCsrf = () => csrf;

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? "GET";
  const headers: Record<string, string> = { "Accept-Language": currentLang() }; // le serveur traduit ses messages d'erreur
  if (init.body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrf) headers["X-CSRF-Token"] = csrf;
  const res = await fetch(`/api/v1${path}`, {
    method, headers, credentials: "same-origin",
    body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const e = (data as { error?: { code?: string; message?: string } }).error;
    throw new ApiError(res.status, e?.code ?? "unknown", e?.message ?? res.statusText);
  }
  return data as T;
}
