import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Plus, ShieldAlert } from "lucide-react";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, ConfirmDialog, Empty, Modal, PageHeader, Select, Tabs, when } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import { applyTheme, getTheme, type ThemeChoice } from "../lib/theme";
import { applyLang, getLang, LANGS, type Lang } from "../lib/lang";
import { useMe } from "../lib/useAuth";
import { SyslogPanel } from "./SyslogPanel";
import { OidcPanel } from "./OidcPanel";
import type { ChainResult, UserAccount, WebhookConfig, WebhookDelivery } from "../lib/types";

type Tab = "users" | "webhook" | "syslog" | "sso" | "journal" | "appearance" | "account";

export function SettingsPage() {
  const { t } = useTranslation();
  const me = useMe(true);
  const admin = me.data?.role === "admin";
  const [tab, setTab] = useState<Tab | null>(null); // onglet par défaut selon le rôle, une fois connu
  const tabs: { id: Tab; label: string }[] = [
    ...(admin ? [{ id: "users" as const, label: t("settings.users") }, { id: "webhook" as const, label: t("settings.webhook") }, { id: "syslog" as const, label: t("settings.syslog") }, { id: "sso" as const, label: t("settings.sso") }, { id: "journal" as const, label: t("settings.journal") }] : []),
    { id: "appearance", label: t("settings.appearance") }, { id: "account", label: t("settings.account") },
  ];
  const current = tab && tabs.some((x) => x.id === tab) ? tab : tabs[0].id;
  return (
    <>
      <PageHeader title={t("settings.title")} subtitle={t("settings.subtitle")} />
      <Tabs value={current} onChange={setTab} tabs={tabs} />
      {current === "users" && <UsersPanel meId={me.data?.id ?? ""} />}
      {current === "webhook" && <WebhookPanel />}
      {current === "syslog" && <SyslogPanel />}
      {current === "sso" && <OidcPanel />}
      {current === "journal" && <JournalPanel />}
      {current === "appearance" && <AppearancePanel />}
      {current === "account" && <AccountPanel />}
    </>
  );
}

function UsersPanel({ meId }: { meId: string }) {
  const { t } = useTranslation();
  const users = useList<UserAccount>("/users");
  const [editing, setEditing] = useState<UserAccount | "new" | null>(null);
  const [deleting, setDeleting] = useState<UserAccount | null>(null);
  const del = useApiMutation<{ id: string }>("DELETE", (v) => `/users/${v.id}`, ["/users"]);
  return (
    <>
      <div className="mb-4 flex justify-end"><Button onClick={() => setEditing("new")}><Plus aria-hidden size={16} />{t("settings.addUser")}</Button></div>
      {users.data?.length === 0 ? <Empty text={t("settings.noUsers")} /> : (
        <div className="overflow-x-auto rounded-card border border-border bg-surface">
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-muted"><tr>
              {["user", "role", "lastLogin", "status", ""].map((c, i) => <th key={i} scope="col" className="px-4 py-2 font-medium">{c && t(`settings.col.${c}`)}</th>)}
            </tr></thead>
            <tbody>
              {users.data?.map((u) => (
                <tr key={u.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2"><div className="font-medium">{u.display_name}</div><code className="font-mono text-muted">{u.username}</code>{u.auth_provider === "oidc" && <> <Badge tone="info">{t("settings.sso")}</Badge></>}{u.id === meId && <> <Badge tone="primary">{t("settings.you")}</Badge></>}</td>
                  <td className="px-4 py-2">{t(`settings.roles.${u.role}`)}</td>
                  <td className="px-4 py-2 text-muted">{u.last_login_at ? when(u.last_login_at) : t("keys.never")}</td>
                  <td className="px-4 py-2"><Badge tone={u.disabled ? "destructive" : "read"}>{u.disabled ? t("common.disabled") : t("common.enabled")}</Badge></td>
                  <td className="px-4 py-2 text-right">
                    <Button variant="ghost" className="h-8 px-2" onClick={() => setEditing(u)}>{t("common.edit")}</Button>
                    {u.id !== meId && <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setDeleting(u)}>{t("common.delete")}</Button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Modal open={!!editing} onClose={() => setEditing(null)} title={editing && editing !== "new" ? t("settings.editUser") : t("settings.addUser")}>
        <UserForm key={editing && editing !== "new" ? editing.id : "new"} user={editing && editing !== "new" ? editing : null} meId={meId} onClose={() => setEditing(null)} />
      </Modal>
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} title={t("settings.deleteUser")} body={t("settings.deleteUserBody", { name: deleting?.username })} confirmWord={deleting?.username} busy={del.isPending}
        onConfirm={() => deleting && del.mutate({ id: deleting.id }, { onSuccess: () => { setDeleting(null); toast(t("common.deleted")); }, onError: () => setDeleting(null) })} />
      {del.error && <div className="mt-4"><ErrorBox message={t("settings.deleteFailed")} detail={errorMessage(del.error)} title={t("errors.details")} /></div>}
    </>
  );
}

function UserForm({ user, meId, onClose }: { user: UserAccount | null; meId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState(user?.username ?? "");
  const [displayName, setDisplayName] = useState(user?.display_name ?? "");
  const [role, setRole] = useState<UserAccount["role"]>(user?.role ?? "viewer");
  const [password, setPassword] = useState("");
  const [disabled, setDisabled] = useState(user?.disabled ?? false);
  const sso = user?.auth_provider === "oidc"; // rôle fourni par le mapping SSO, pas de mot de passe local
  const save = useApiMutation<unknown>(user ? "PUT" : "POST", user ? `/users/${user.id}` : "/users", ["/users"]);
  return (
    <div className="flex flex-col gap-3">
      <Field id="u-name" label={t("setup.username")} value={username} disabled={!!user} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
      <Field id="u-display" label={t("settings.displayName")} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
      <Select id="u-role" label={t("settings.col.role")} value={role} disabled={sso} onChange={(e) => setRole(e.target.value as UserAccount["role"])}>
        {(["admin", "approver", "viewer"] as const).map((r) => <option key={r} value={r}>{t(`settings.roles.${r}`)}</option>)}
      </Select>
      {sso && <p className="text-muted">{t("settings.ssoManaged")}</p>}
      {!sso && <Field id="u-pass" type="password" autoComplete="new-password" label={user ? t("settings.resetPassword") : t("setup.password")} value={password} onChange={(e) => setPassword(e.target.value)} />}
      {user && <Check2 id="u-disabled" label={t("settings.disableAccount")} checked={disabled} disabled={user.id === meId} onChange={setDisabled} />}
      <p className="text-muted">{t("settings.rolesHelp")}</p>
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={save.isPending || !username || (!user && !password)}
          onClick={() => save.mutate({ username, display_name: displayName, role, password, disabled }, { onSuccess: () => { onClose(); toast(t("common.saved")); } })}>{t("common.save")}</Button>
      </div>
    </div>
  );
}

function ChainLine({ name, r }: { name: string; r: ChainResult }) {
  const { t } = useTranslation();
  return (
    <li className="flex items-start gap-2">
      {r.ok ? <CheckCircle2 aria-hidden size={18} className="mt-0.5 text-read" /> : <ShieldAlert aria-hidden size={18} className="mt-0.5 text-destructive" />}
      <div>
        <strong>{name}</strong> — {r.ok ? t("settings.chainOk", { count: r.checked }) : <span role="alert" className="text-destructive">{t("settings.chainBad", { seq: r.bad_seq, reason: r.reason })}</span>}
      </div>
    </li>
  );
}

function JournalPanel() {
  const { t } = useTranslation();
  const meta = useQuery({ queryKey: ["meta"], queryFn: () => api<{ log_retention_days: number }>("/meta") });
  const verify = useMutation({ mutationFn: () => api<{ ok: boolean; executions: ChainResult; audit: ChainResult }>("/integrity/verify", { method: "POST" }) });
  const days = meta.data?.log_retention_days;
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card className="flex flex-col gap-2">
        <h2 className="text-[15px] font-semibold">{t("settings.retention")}</h2>
        <p>{days === undefined ? "…" : days === 0 ? t("settings.retentionUnlimited") : t("settings.retentionDays", { count: days })}</p>
        <p className="text-muted">{t("settings.retentionHelp")}</p>
      </Card>
      <Card className="flex flex-col gap-3">
        <h2 className="text-[15px] font-semibold">{t("settings.integrity")}</h2>
        <p className="text-muted">{t("settings.integrityHelp")}</p>
        <div><Button onClick={() => verify.mutate()} disabled={verify.isPending}>{t("settings.verify")}</Button></div>
        {verify.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(verify.error)} title={t("errors.details")} />}
        {verify.data && (
          <ul className="flex flex-col gap-2" aria-live="polite">
            <ChainLine name={t("settings.chainExec")} r={verify.data.executions} />
            <ChainLine name={t("settings.chainAudit")} r={verify.data.audit} />
          </ul>
        )}
      </Card>
    </div>
  );
}

function AppearancePanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [theme, setTheme] = useState<ThemeChoice>(getTheme());
  const [lang, setLang] = useState<Lang>(getLang());
  const save = useApiMutation<{ theme: ThemeChoice }>("PUT", "/auth/theme", ["me"]);
  return (
    <Card className="flex max-w-md flex-col gap-3">
      <Select id="set-theme" label={t("topbar.theme")} value={theme} onChange={(e) => {
        const v = e.target.value as ThemeChoice;
        setTheme(v); applyTheme(v); save.mutate({ theme: v }, { onSuccess: () => { void qc.invalidateQueries({ queryKey: ["me"] }); toast(t("common.saved")); } });
      }}>
        <option value="system">{t("topbar.themeSystem")}</option><option value="light">{t("topbar.themeLight")}</option><option value="dark">{t("topbar.themeDark")}</option>
      </Select>
      <p className="text-muted">{t("settings.themeHelp")}</p>
      <Select id="set-lang" label={t("settings.language")} value={lang} onChange={(e) => { const v = e.target.value as Lang; setLang(v); applyLang(v); }}>
        {LANGS.map((l) => <option key={l.id} value={l.id}>{l.label}</option>)}
      </Select>
      <p className="text-muted">{t("settings.languageHelp")}</p>
    </Card>
  );
}

function AccountPanel() {
  const { t } = useTranslation();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const save = useApiMutation<{ current: string; new: string }>("POST", "/auth/password", []);
  const mismatch = confirm !== "" && confirm !== next;
  return (
    <Card className="flex max-w-md flex-col gap-3">
      <h2 className="text-[15px] font-semibold">{t("settings.changePassword")}</h2>
      <Field id="pw-current" type="password" autoComplete="current-password" label={t("settings.currentPassword")} value={current} onChange={(e) => setCurrent(e.target.value)} />
      <Field id="pw-new" type="password" autoComplete="new-password" label={t("settings.newPassword")} value={next} onChange={(e) => setNext(e.target.value)} />
      <Field id="pw-confirm" type="password" autoComplete="new-password" label={t("setup.confirm")} value={confirm} onChange={(e) => setConfirm(e.target.value)} />
      {mismatch && <p role="alert" className="text-destructive">{t("setup.mismatch")}</p>}
      <p className="text-muted">{t("settings.otherSessions")}</p>
      {save.error && <ErrorBox message={t("settings.passwordFailed")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div><Button disabled={save.isPending || mismatch || !current || next.length < 10}
        onClick={() => save.mutate({ current, new: next }, { onSuccess: () => { setCurrent(""); setNext(""); setConfirm(""); toast(t("settings.passwordChanged")); } })}>{t("common.save")}</Button></div>
    </Card>
  );
}

function WebhookPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const cfg = useQuery({ queryKey: ["/settings/webhook"], queryFn: () => api<{ config: WebhookConfig; events: string[] }>("/settings/webhook") });
  const deliveries = useQuery({ queryKey: ["/settings/webhook/deliveries"], queryFn: () => api<{ items: WebhookDelivery[] }>("/settings/webhook/deliveries"), refetchInterval: 10000 });
  if (!cfg.data) return <p className="text-muted">{t("errors.loading")}</p>;
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <WebhookForm key={cfg.data.config.url + String(cfg.data.config.enabled)} config={cfg.data.config} events={cfg.data.events}
        onSaved={() => { void qc.invalidateQueries({ queryKey: ["/settings/webhook"] }); }} onTested={() => void qc.invalidateQueries({ queryKey: ["/settings/webhook/deliveries"] })} />
      <Card className="flex flex-col gap-3">
        <h2 className="text-[15px] font-semibold">{t("webhook.deliveries")}</h2>
        {(deliveries.data?.items.length ?? 0) === 0 ? <p className="text-muted">{t("webhook.noDeliveries")}</p> : (
          <ul className="flex max-h-96 flex-col gap-2 overflow-y-auto">
            {deliveries.data?.items.map((d) => (
              <li key={d.id} className="rounded-field border border-border p-2">
                <div className="flex flex-wrap items-center gap-2"><Badge tone={d.ok ? "read" : "destructive"}>{d.ok ? t("webhook.delivered") : t("webhook.failed")}</Badge>
                  <code className="font-mono">{d.event}</code><span className="text-muted">{when(d.ts)}</span></div>
                <p className="text-muted">{t("webhook.attempts", { count: d.attempts })}{d.http_status ? ` · HTTP ${d.http_status}` : ""}{d.error ? ` · ${d.error}` : ""}</p>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function WebhookForm({ config, events, onSaved, onTested }: { config: WebhookConfig; events: string[]; onSaved: () => void; onTested: () => void }) {
  const { t } = useTranslation();
  const [url, setUrl] = useState(config.url);
  const [secret, setSecret] = useState("");
  const [enabled, setEnabled] = useState(config.enabled);
  const [test, setTest] = useState<{ ok: boolean; http_status: number; error?: string } | null>(null);
  const save = useApiMutation<unknown>("PUT", "/settings/webhook", []);
  const testIt = useMutation({ mutationFn: () => api<{ ok: boolean; http_status: number; error?: string }>("/settings/webhook/test", { method: "POST" }), onSuccess: (r) => { setTest(r); onTested(); } });
  return (
    <Card className="flex flex-col gap-3">
      <h2 className="text-[15px] font-semibold">{t("settings.webhook")}</h2>
      <p className="text-muted">{t("webhook.help")}</p>
      <Field id="wh-url" label={t("webhook.url")} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://ntfy.example.com/leash" />
      <Field id="wh-secret" type="password" autoComplete="new-password" label={t("webhook.secret")} value={secret} onChange={(e) => setSecret(e.target.value)}
        placeholder={config.has_secret ? t("webhook.secretKept") : ""} />
      <Check2 id="wh-enabled" label={t("common.enabled")} checked={enabled} onChange={setEnabled} />
      <details className="text-muted"><summary className="cursor-pointer">{t("webhook.events")}</summary><ul className="mt-1 list-disc pl-5">{events.map((e) => <li key={e}><code className="font-mono">{e}</code></li>)}</ul></details>
      <p className="text-muted">{t("webhook.signatureHelp")}</p>
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      {testIt.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(testIt.error)} title={t("errors.details")} />}
      {test && (test.ok ? <p className="text-read" aria-live="polite">✔ {t("webhook.testOk", { status: test.http_status })}</p> : <p role="alert" className="text-destructive">{t("webhook.testFailed", { error: test.error })}</p>)}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="secondary" disabled={testIt.isPending || !config.url || !config.has_secret} onClick={() => testIt.mutate()}>{t("webhook.test")}</Button>
        <Button disabled={save.isPending} onClick={() => save.mutate({ url, secret, enabled }, { onSuccess: () => { setSecret(""); toast(t("common.saved")); onSaved(); } })}>{t("common.save")}</Button>
      </div>
    </Card>
  );
}
