import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, CopyButton, Select, Textarea } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useApiMutation } from "../lib/queries";
import type { OidcConfig, OidcRoleMap, OidcTestResult } from "../lib/types";

const KEY = "/settings/oidc";
const ROLES = ["admin", "approver", "viewer"] as const;
const lines = (v: string[]) => v.join("\n");
const parseLines = (v: string) => v.split("\n").map((x) => x.trim()).filter(Boolean);

export function OidcPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: [KEY], queryFn: () => api<OidcConfig>(KEY) });
  if (!q.data) return <p className="text-muted">{t("errors.loading")}</p>;
  return <OidcForm key={JSON.stringify(q.data)} config={q.data} onChanged={() => void qc.invalidateQueries({ queryKey: [KEY] })} />;
}

function OidcForm({ config, onChanged }: { config: OidcConfig; onChanged: () => void }) {
  const { t } = useTranslation();
  const [c, setC] = useState<OidcConfig>(config);
  const [secret, setSecret] = useState("");
  const [clearSecret, setClearSecret] = useState(false);
  const [test, setTest] = useState<OidcTestResult | null>(null);
  const set = <K extends keyof OidcConfig>(k: K, v: OidcConfig[K]) => setC((p) => ({ ...p, [k]: v }));
  const setRole = (r: keyof OidcRoleMap, v: string) => setC((p) => ({ ...p, role_map: { ...p.role_map, [r]: parseLines(v) } }));
  const save = useApiMutation<unknown>("PUT", KEY, []);
  const testIt = useMutation({ mutationFn: () => api<OidcTestResult>(`${KEY}/test`, { method: "POST", body: { issuer: c.issuer } }), onSuccess: setTest });
  const dirty = JSON.stringify(c) !== JSON.stringify(config) || secret !== "" || clearSecret;
  const logoutUrl = (() => { try { return new URL(c.redirect_url).origin + "/"; } catch { return ""; } })();
  const submit = () => {
    const { has_client_secret: _h, redirect_url: _r, ...body } = c;
    save.mutate({ ...body, client_secret: secret, clear_client_secret: clearSecret }, { onSuccess: () => { setSecret(""); setClearSecret(false); setTest(null); toast(t("common.saved")); onChanged(); } });
  };
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <Card className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-[15px] font-semibold">{t("oidc.title")}</h2>
          <Badge tone={config.enabled ? "read" : "muted"}>{config.enabled ? t("common.enabled") : t("common.disabled")}</Badge>
        </div>
        <p className="text-muted">{t("oidc.help")}</p>
        <Check2 id="oidc-enabled" label={t("oidc.enable")} checked={c.enabled} onChange={(v) => set("enabled", v)} />
        <Field id="oidc-name" label={t("oidc.name")} value={c.name} placeholder="Keycloak" onChange={(e) => set("name", e.target.value)} />
        <div className="flex flex-col gap-1.5">
          <Field id="oidc-issuer" label={t("oidc.issuer")} value={c.issuer} placeholder="https://sso.example.com/realms/leassh" autoComplete="off" onChange={(e) => set("issuer", e.target.value)} />
          <p className="text-muted">{t("oidc.issuerHelp")}</p>
          {c.issuer.startsWith("http://") && <p role="alert" className="text-write">{t("oidc.httpWarn")}</p>}
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field id="oidc-client" label={t("oidc.clientId")} value={c.client_id} autoComplete="off" onChange={(e) => set("client_id", e.target.value)} />
          <Field id="oidc-secret" type="password" label={t("oidc.clientSecret")} value={secret} autoComplete="new-password" spellCheck={false}
            placeholder={config.has_client_secret ? t("oidc.secretKept") : ""} onChange={(e) => { setSecret(e.target.value); setClearSecret(false); }} />
        </div>
        {config.has_client_secret && <Check2 id="oidc-clear" label={t("oidc.clearSecret")} checked={clearSecret} onChange={(v) => { setClearSecret(v); if (v) setSecret(""); }} />}
        <Field id="oidc-scopes" label={t("oidc.scopes")} value={c.scopes} onChange={(e) => set("scopes", e.target.value)} />

        <fieldset className="flex flex-col gap-3 rounded-field border border-border p-3">
          <legend className="px-1 font-medium">{t("oidc.rolesTitle")}</legend>
          <Field id="oidc-claim" label={t("oidc.roleClaim")} value={c.role_claim} placeholder="groups" onChange={(e) => set("role_claim", e.target.value)} />
          <p className="text-muted">{t("oidc.roleClaimHelp")}</p>
          <div className="grid gap-3 md:grid-cols-3">
            {ROLES.map((r) => (
              <Textarea key={r} id={`oidc-map-${r}`} mono label={t("oidc.mapTo", { role: t(`settings.roles.${r}`) })} value={lines(c.role_map[r] ?? [])} rows={3} spellCheck={false} onChange={(e) => setRole(r, e.target.value)} />
            ))}
          </div>
          <p className="text-muted">{t("oidc.mapHelp")}</p>
          <Select id="oidc-default" label={t("oidc.defaultRole")} value={c.default_role} onChange={(e) => set("default_role", e.target.value as OidcConfig["default_role"])}>
            <option value="viewer">{t("settings.roles.viewer")}</option>
            <option value="">{t("oidc.refuse")}</option>
          </Select>
        </fieldset>

        <fieldset className="flex flex-col gap-2 rounded-field border border-border p-3">
          <legend className="px-1 font-medium">{t("oidc.accounts")}</legend>
          <Check2 id="oidc-jit" label={t("oidc.jit")} checked={c.jit} onChange={(v) => set("jit", v)} />
          <p className="text-muted">{t(c.jit ? "oidc.jitOn" : "oidc.jitOff")}</p>
          <Check2 id="oidc-localonly" label={t("oidc.localOnly")} checked={c.local_password_admins_only} onChange={(v) => set("local_password_admins_only", v)} />
          <p className="text-muted">{t("oidc.localOnlyHelp")}</p>
          <Check2 id="oidc-localoff" label={t("oidc.localOff")} checked={c.local_login_disabled} onChange={(v) => set("local_login_disabled", v)} />
          <p className="text-muted">{t("oidc.localOffHelp")}</p>
          <Check2 id="oidc-endsession" label={t("oidc.endSession")} checked={c.end_session} onChange={(v) => set("end_session", v)} />
          <Check2 id="oidc-autoredirect" label={t("oidc.autoRedirect")} checked={c.auto_redirect} onChange={(v) => set("auto_redirect", v)} />
          <p className="text-muted">{t("oidc.autoRedirectHelp", { url: logoutUrl ? `${logoutUrl}?login` : "/?login" })}</p>
        </fieldset>

        {dirty && <p className="text-muted">{t("oidc.saveFirst")}</p>}
        {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
        {testIt.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(testIt.error)} title={t("errors.details")} />}
        {test && <TestResult r={test} />}
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" disabled={testIt.isPending || !c.issuer} onClick={() => testIt.mutate()}>{t("oidc.test")}</Button>
          <Button disabled={save.isPending || !dirty} onClick={submit}>{t("common.save")}</Button>
        </div>
      </Card>

      <Card className="flex h-fit flex-col gap-3">
        <h2 className="text-[15px] font-semibold">{t("oidc.provider")}</h2>
        <p className="text-muted">{t("oidc.providerHelp")}</p>
        <UrlRow label={t("oidc.redirectUrl")} value={c.redirect_url} />
        {logoutUrl && <UrlRow label={t("oidc.logoutUrl")} value={logoutUrl} />}
        <p className="text-muted">{t("oidc.flowHelp")}</p>
        <p className="text-muted">{t("oidc.breakGlass")}</p>
      </Card>
    </div>
  );
}

function UrlRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="font-medium">{label}</span>
      <div className="flex items-start gap-2 rounded-field bg-surface-2 p-2">
        <code className="min-w-0 flex-1 break-all font-mono text-[13px]">{value}</code>
        <CopyButton text={value} />
      </div>
    </div>
  );
}

function TestResult({ r }: { r: OidcTestResult }) {
  const { t } = useTranslation();
  if (!r.ok) return <p role="alert" className="break-words text-destructive">{t("oidc.testFailed", { error: r.error })}</p>;
  return (
    <div aria-live="polite" className="flex flex-col gap-2 rounded-field border border-border p-3">
      <p className="text-read">✔ {t("oidc.testOk", { count: r.jwks_keys ?? 0 })}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
        {([["authorization_endpoint", r.authorization_endpoint], ["token_endpoint", r.token_endpoint], ["jwks_uri", r.jwks_uri], ["userinfo_endpoint", r.userinfo_endpoint], ["end_session_endpoint", r.end_session_endpoint]] as const).map(([k, v]) => (
          <div key={k} className="contents"><dt className="text-muted">{k}</dt><dd className="break-all font-mono text-[13px]">{v || "—"}</dd></div>
        ))}
      </dl>
      {r.warnings?.map((w) => <p key={w} role="alert" className="text-write">{t(`oidc.warn.${w}`)}</p>)}
    </div>
  );
}
