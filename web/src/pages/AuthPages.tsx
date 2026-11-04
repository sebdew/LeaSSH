import { useEffect, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Logo } from "../components/Logo";
import { api, ApiError, type User } from "../lib/api";
import { passwordStrength } from "../lib/theme";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import type { AuthProviders } from "../lib/types";

function Shell({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  const { t } = useTranslation();
  return (
    <main className="grid min-h-screen place-items-center p-4">
      <Card className="w-full max-w-sm">
        <div className="mb-4 text-fg"><Logo className="h-10 w-auto" title={t("app.name")} /></div>
        <h1 className="text-[24px] font-semibold">{title}</h1>
        {subtitle ? <p className="mb-4 mt-1 text-muted">{subtitle}</p> : <div className="mb-4" />}
        {children}
      </Card>
    </main>
  );
}

function useSubmit(path: string, onDone: (u: User) => void) {
  const { t } = useTranslation();
  const [error, setError] = useState<{ message: string; detail?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (body: unknown) => {
    setBusy(true); setError(null);
    try {
      onDone(await api<User>(path, { method: "POST", body }));
    } catch (e) {
      const code = e instanceof ApiError ? e.code : "generic";
      setError({ message: t(`errors.${code}`, { defaultValue: t("errors.generic") }), detail: e instanceof Error ? e.message : undefined });
    } finally { setBusy(false); }
  };
  return { error, busy, submit };
}

// Motif d'échec SSO renvoyé par le serveur (?sso_error=…), lu une fois puis retiré de l'URL.
function useSsoError(): string | null {
  const [code] = useState(() => new URLSearchParams(window.location.search).get("sso_error"));
  useEffect(() => {
    if (code) window.history.replaceState(null, "", window.location.pathname);
  }, [code]);
  return code;
}

export function LoginPage({ onDone }: { onDone: (u: User) => void }) {
  const { t } = useTranslation();
  const providers = useQuery({ queryKey: ["auth-providers"], retry: false, queryFn: () => api<AuthProviders>("/auth/providers") });
  const ssoError = useSsoError();
  const sso = providers.data?.oidc ?? null;
  // Redirection automatique vers le fournisseur, sauf en cas d'erreur SSO ou avec « ?login » (accès local de secours).
  const bypass = new URLSearchParams(window.location.search).has("login");
  const autoRedirect = !!sso && !!providers.data?.auto_redirect && !ssoError && !bypass;
  useEffect(() => {
    if (autoRedirect && sso) window.location.replace(sso.start_url);
  }, [autoRedirect, sso]);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { error, busy, submit } = useSubmit("/auth/login", onDone);
  const onSubmit = (e: FormEvent) => { e.preventDefault(); void submit({ username, password }); };
  if (autoRedirect) return <Shell title={t("login.title")}><p role="status" className="text-muted">{t("login.sso.redirecting", { name: sso?.name })}</p></Shell>;
  return (
    <Shell title={t("login.title")}>
      {ssoError && <div className="mb-3"><ErrorBox message={t(`login.sso.errors.${ssoError}`, { defaultValue: t("login.sso.errors.generic") })} detail={ssoError} title={t("errors.details")} /></div>}
      {sso && (
        <>
          <a href={sso.start_url} className="inline-flex h-9 w-full items-center justify-center gap-2 rounded-field bg-primary px-4 font-medium text-primary-fg hover:opacity-90">
            <KeyRound aria-hidden size={16} />{t("login.sso.button", { name: sso.name })}
          </a>
          <div className="my-4 flex items-center gap-3 text-muted" aria-hidden><span className="h-px flex-1 bg-border" />{t("login.sso.or")}<span className="h-px flex-1 bg-border" /></div>
          {providers.data?.local_password_admins_only && <p className="mb-3 text-muted">{t("login.sso.localAdminsOnly")}</p>}
        </>
      )}
      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        <Field id="username" label={t("setup.username")} autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <Field id="password" label={t("setup.password")} type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        {error && <ErrorBox {...error} title={t("errors.details")} />}
        <Button type="submit" variant={sso ? "secondary" : "primary"} disabled={busy}>{t("login.submit")}</Button>
      </form>
    </Shell>
  );
}

export function SetupPage({ onDone }: { onDone: (u: User) => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const { error, busy, submit } = useSubmit("/setup", onDone);
  const strength = passwordStrength(password);
  const mismatch = confirm !== "" && confirm !== password;
  const onSubmit = (e: FormEvent) => { e.preventDefault(); if (!mismatch) void submit({ username, password }); };
  return (
    <Shell title={t("setup.title")} subtitle={t("setup.subtitle")}>
      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        <Field id="username" label={t("setup.username")} autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <Field id="password" label={t("setup.password")} type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        <p aria-live="polite" className="text-muted">{t("setup.strength.label")} : <strong className="text-fg">{t(`setup.strength.${strength}`)}</strong></p>
        <Field id="confirm" label={t("setup.confirm")} type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
        {mismatch && <p role="alert" className="text-destructive">{t("setup.mismatch")}</p>}
        {error && <ErrorBox {...error} title={t("errors.details")} />}
        <p className="text-muted">{t("setup.backup")}</p>
        <Button type="submit" disabled={busy || strength === 0 || mismatch}>{t("setup.submit")}</Button>
      </form>
    </Shell>
  );
}
