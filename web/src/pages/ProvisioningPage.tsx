import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw } from "lucide-react";
import { Button, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, CodeBlock, ConfirmDialog, Empty, Modal, MultiPick, PageHeader, Select } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { fmtDate } from "../lib/format";
import { ruleActive } from "../lib/active";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import type { Identity, ProvType, Provisioner, Rule, SshServer, SyncReport, Group } from "../lib/types";

const INTERVALS = [0, 5, 15, 30, 60, 360, 1440];

export function ProvisioningPage() {
  const { t } = useTranslation();
  const provs = useList<Provisioner>("/provisioners");
  const types = useList<ProvType>("/provisioners/types");
  const identities = useList<Identity>("/identities");
  const rules = useList<Rule>("/rules");
  const servers = useList<SshServer>("/servers");
  const groups = useList<Group>("/server-groups");
  const [editing, setEditing] = useState<Provisioner | "new" | null>(null);
  const [testing, setTesting] = useState<Provisioner | null>(null);
  const [syncing, setSyncing] = useState<Provisioner | null>(null);
  const [deleting, setDeleting] = useState<Provisioner | null>(null);
  const del = useApiMutation<{ id: string }>("DELETE", (v) => `/provisioners/${v.id}`, ["/provisioners", "/servers", "/server-groups"]);
  const count = (p: Provisioner) => ({
    servers: servers.data?.filter((s) => s.managed_by === p.id).length ?? 0,
    groups: groups.data?.filter((g) => g.managed_by === p.id).length ?? 0,
  });

  return (
    <>
      <PageHeader title={t("provisioning.title")} subtitle={t("provisioning.subtitle")}
        action={<Button onClick={() => setEditing("new")}><Plus aria-hidden size={16} />{t("provisioning.add")}</Button>} />
      {provs.data?.length === 0 ? (
        <Empty text={t("provisioning.empty")} action={<Button onClick={() => setEditing("new")}>{t("provisioning.add")}</Button>} />
      ) : (
        <div className="grid gap-3">
          {provs.data?.map((p) => (
            <div key={p.id} className={`rounded-card border border-border bg-surface p-4 ${p.enabled ? "" : "opacity-60"}`}>
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div>
                  <h3 className="font-semibold"><RefreshCw aria-hidden size={14} className="mr-1.5 inline text-primary" />{p.name} <Badge>{types.data?.find((x) => x.id === p.type)?.label ?? p.type}</Badge> {!p.enabled && <Badge>{t("common.disabled")}</Badge>}</h3>
                  <p className="mt-1 text-muted">{t("provisioning.counts", count(p))} · {p.interval_min > 0 ? t("provisioning.every", { count: p.interval_min }) : t("provisioning.manual")}</p>
                </div>
                <div className="flex flex-wrap gap-1">
                  <Button variant="secondary" className="h-8 px-3" onClick={() => setTesting(p)}>{t("provisioning.test")}</Button>
                  <Button className="h-8 px-3" onClick={() => setSyncing(p)}>{t("provisioning.sync")}</Button>
                  <Button variant="ghost" className="h-8 px-2" onClick={() => setEditing(p)}>{t("common.edit")}</Button>
                  <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setDeleting(p)}>{t("common.delete")}</Button>
                </div>
              </div>
              <p className="mt-2 text-[13px]">
                {p.last_sync_status === "" && <span className="text-muted">{t("provisioning.neverSynced")}</span>}
                {p.last_sync_status === "ok" && <span className="text-read">✔ {t("provisioning.lastOk", { date: fmtDate(p.last_sync_at), message: p.last_sync_message })}</span>}
                {p.last_sync_status === "error" && <span role="alert" className="text-destructive">✖ {t("provisioning.lastError", { date: fmtDate(p.last_sync_at), message: p.last_sync_message })}</span>}
              </p>
            </div>
          ))}
        </div>
      )}
      <Modal open={!!editing} onClose={() => setEditing(null)} title={editing && editing !== "new" ? t("provisioning.edit") : t("provisioning.add")} wide>
        <ProvisionerForm key={editing && editing !== "new" ? editing.id : "new"} p={editing && editing !== "new" ? editing : null} types={types.data ?? []}
          identities={identities.data ?? []} rules={(rules.data ?? []).filter(ruleActive)} onClose={() => setEditing(null)} />
      </Modal>
      <TestDialog p={testing} onClose={() => setTesting(null)} />
      <SyncDialog p={syncing} onClose={() => setSyncing(null)} />
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} title={t("provisioning.deleteTitle")} body={t("provisioning.deleteBody", { name: deleting?.name })} confirmWord={deleting?.name} busy={del.isPending}
        onConfirm={() => deleting && del.mutate({ id: deleting.id }, { onSuccess: () => { setDeleting(null); toast(t("common.deleted")); } })} />
    </>
  );
}

function ProvisionerForm({ p, types, identities, rules, onClose }: { p: Provisioner | null; types: ProvType[]; identities: Identity[]; rules: Rule[]; onClose: () => void }) {
  const { t } = useTranslation();
  const [type, setType] = useState(p?.type ?? types[0]?.id ?? "");
  const spec = types.find((x) => x.id === type);
  const [name, setName] = useState(p?.name ?? "");
  const [identity, setIdentity] = useState(p?.identity_id ?? identities[0]?.id ?? "");
  const [user, setUser] = useState(p?.default_user ?? "");
  const [interval, setInterval] = useState(p?.interval_min ?? 15);
  const [enabled, setEnabled] = useState(p?.enabled ?? true);
  const [ruleIds, setRuleIds] = useState<string[]>((p?.rule_ids ?? []).filter((id) => rules.some((r) => r.id === id)));
  const [cfg, setCfg] = useState<Record<string, unknown>>(() => {
    const init: Record<string, unknown> = { ...(p?.config ?? {}) };
    for (const f of types.find((x) => x.id === (p?.type ?? types[0]?.id))?.fields ?? []) if (init[f.key] === undefined && f.default !== undefined) init[f.key] = f.default;
    return init;
  });
  const [secret, setSecret] = useState("");
  const save = useApiMutation<unknown>(p ? "PUT" : "POST", p ? `/provisioners/${p.id}` : "/provisioners", ["/provisioners"]);
  const identUser = identities.find((i) => i.id === identity)?.username ?? "";
  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field id="prov-name" label={t("common.name")} value={name} onChange={(e) => setName(e.target.value)} placeholder="Warpgate" />
        <Select id="prov-type" label={t("provisioning.type")} value={type} disabled={!!p} onChange={(e) => setType(e.target.value)}>
          {types.map((x) => <option key={x.id} value={x.id}>{x.label}</option>)}
        </Select>
      </div>
      {spec?.fields.map((f) => f.kind === "bool" ? (
        <div key={f.key} className="flex flex-col gap-1">
          <Check2 id={`prov-${f.key}`} label={f.label} checked={cfg[f.key] === true} onChange={(v) => setCfg((c) => ({ ...c, [f.key]: v }))} />
          {f.help && <p className="text-muted">{f.help}</p>}
        </div>
      ) : f.kind === "password" ? (
        <div key={f.key} className="flex flex-col gap-1">
          <Field id={`prov-${f.key}`} label={f.label} type="password" autoComplete="new-password" value={secret} onChange={(e) => setSecret(e.target.value)}
            placeholder={p?.has_secret ? t("provisioning.secretKept") : ""} />
          {f.help && <p className="text-muted">{f.help}</p>}
        </div>
      ) : (
        <div key={f.key} className="flex flex-col gap-1">
          <Field id={`prov-${f.key}`} label={f.label} type={f.kind === "number" ? "number" : "text"} value={String(cfg[f.key] ?? "")}
            onChange={(e) => setCfg((c) => ({ ...c, [f.key]: f.kind === "number" ? Number(e.target.value) : e.target.value }))} />
          {f.help && <p className="text-muted">{f.help}</p>}
        </div>
      ))}
      <div className="grid gap-3 sm:grid-cols-2">
        <Select id="prov-ident" label={t("provisioning.identity")} value={identity} onChange={(e) => setIdentity(e.target.value)}>
          {identities.length === 0 && <option value="">{t("servers.noIdentity")}</option>}
          {identities.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
        </Select>
        <div className="flex flex-col gap-1">
          <Field id="prov-user" label={t("provisioning.defaultUser")} value={user} onChange={(e) => setUser(e.target.value)} placeholder={identUser} autoComplete="off" />
          <p className="text-muted">{t("provisioning.defaultUserHelp")}</p>
        </div>
      </div>
      <Select id="prov-interval" label={t("provisioning.interval")} value={interval} onChange={(e) => setInterval(Number(e.target.value))}>
        {INTERVALS.map((m) => <option key={m} value={m}>{m === 0 ? t("provisioning.manual") : t("provisioning.every", { count: m })}</option>)}
      </Select>
      <MultiPick label={t("provisioning.defaultRules")} options={rules.map((r) => ({ id: r.id, label: r.name }))} value={ruleIds} onChange={setRuleIds} />
      <p className="text-muted">{t("provisioning.defaultRulesHelp")}</p>
      <Check2 id="prov-enabled" label={t("common.enabled")} checked={enabled} onChange={setEnabled} />
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={save.isPending || !name || !identity || (!p && !secret)}
          onClick={() => save.mutate({ name, type, enabled, interval_min: interval, identity_id: identity, default_user: user, config: cfg, secret, rule_ids: ruleIds }, { onSuccess: () => { onClose(); toast(t("common.saved")); } })}>{t("common.save")}</Button>
      </div>
    </div>
  );
}

interface TestResult { machines: number; groups: number; ssh?: { host: string; port: number; fingerprint?: string; key_type?: string; pinned_fingerprint?: string; error?: string } }

// Lecture de la source + clé d'hôte du bastion: l'admin confirme l'empreinte une fois, toutes les machines synchronisées en héritent.
function TestDialog({ p, onClose }: { p: Provisioner | null; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [res, setRes] = useState<TestResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => { setRes(null); setErr(""); }, [p?.id]);
  const run = async () => {
    if (!p) return;
    setBusy(true); setErr("");
    try { setRes(await api<TestResult>(`/provisioners/${p.id}/test`, { method: "POST" })); } catch (e) { setErr(errorMessage(e)); setRes(null); } finally { setBusy(false); }
  };
  const pin = async () => {
    if (!p || !res?.ssh?.fingerprint) return;
    setBusy(true); setErr("");
    try {
      await api(`/provisioners/${p.id}/host-key`, { method: "POST", body: { fingerprint: res.ssh.fingerprint } });
      await qc.invalidateQueries({ queryKey: ["/provisioners"] });
      toast(t("provisioning.pinned"));
      onClose();
    } catch (e) { setErr(errorMessage(e)); } finally { setBusy(false); }
  };
  const ssh = res?.ssh;
  const changed = !!ssh?.fingerprint && !!ssh.pinned_fingerprint && ssh.fingerprint !== ssh.pinned_fingerprint;
  const trusted = !!ssh?.fingerprint && ssh.fingerprint === ssh.pinned_fingerprint;
  return (
    <Modal open={!!p} onClose={onClose} title={t("provisioning.testTitle", { name: p?.name })}>
      <div className="flex flex-col gap-3">
        <Button onClick={() => void run()} disabled={busy}>{t("provisioning.test")}</Button>
        {err && <ErrorBox message={t("provisioning.sourceError")} detail={err} title={t("errors.details")} />}
        {res && <p className="text-read">✔ {t("provisioning.found", { machines: res.machines, groups: res.groups })}</p>}
        {ssh?.error && <ErrorBox message={t("provisioning.bastionUnreachable")} detail={ssh.error} title={t("errors.details")} />}
        {ssh?.fingerprint && (
          <>
            <p className="text-muted">{t("provisioning.presentedFingerprint", { host: ssh.host, port: ssh.port })}</p>
            <CodeBlock copy>{ssh.fingerprint}</CodeBlock>
            {trusted && <p className="text-read">✔ {t("provisioning.keyTrusted")}</p>}
            {changed && (<><p role="alert" className="text-destructive">{t("servers.fingerprintChanged")}</p><p className="text-muted">{t("servers.previousFingerprint")}</p><CodeBlock>{ssh.pinned_fingerprint ?? ""}</CodeBlock></>)}
            {!trusted && <Button onClick={() => void pin()} disabled={busy} className={changed ? "!bg-destructive !text-primary-fg" : ""}>{changed ? t("servers.acceptNewKey") : t("provisioning.trustKey")}</Button>}
            {!trusted && !changed && <p className="text-muted">{t("provisioning.trustHelp")}</p>}
          </>
        )}
      </div>
    </Modal>
  );
}

const ACTION_TONE: Record<string, "read" | "write" | "destructive" | "muted"> = { create: "read", add: "read", enable: "read", update: "write", disable: "destructive", delete: "destructive", remove: "destructive", release: "write" };

function SyncDialog({ p, onClose }: { p: Provisioner | null; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [rep, setRep] = useState<SyncReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [confirm, setConfirm] = useState(false);
  useEffect(() => {
    setRep(null); setErr(""); setConfirm(false);
    if (!p) return;
    let cancelled = false;
    setBusy(true);
    api<SyncReport>(`/provisioners/${p.id}/preview`, { method: "POST" })
      .then((r) => { if (!cancelled) setRep(r); })
      .catch((e) => { if (!cancelled) setErr(errorMessage(e)); })
      .finally(() => { if (!cancelled) setBusy(false); });
    return () => { cancelled = true; };
  }, [p?.id]);
  const apply = async () => {
    if (!p) return;
    setBusy(true); setErr("");
    try {
      const r = await api<SyncReport>(`/provisioners/${p.id}/sync`, { method: "POST", body: { force: confirm } });
      await Promise.all(["/provisioners", "/servers", "/server-groups", "/rules"].map((k) => qc.invalidateQueries({ queryKey: [k] })));
      toast(t("provisioning.synced", { count: r.changes.length }));
      onClose();
    } catch (e) { setErr(errorMessage(e)); } finally { setBusy(false); }
  };
  const blocked = !!rep?.blocked;
  return (
    <Modal open={!!p} onClose={onClose} title={t("provisioning.syncTitle", { name: p?.name })} wide>
      <div className="flex flex-col gap-3">
        {busy && !rep && <p className="text-muted">{t("common.loading")}</p>}
        {err && <ErrorBox message={t("provisioning.sourceError")} detail={err} title={t("errors.details")} />}
        {rep && (
          <>
            <p className="text-muted">{t("provisioning.preview", { machines: rep.machines, count: rep.changes.length })}</p>
            {rep.changes.length === 0 && <p className="text-read">✔ {t("provisioning.upToDate")}</p>}
            {rep.changes.length > 0 && (
              <ul className="max-h-72 divide-y divide-border overflow-y-auto rounded-field border border-border">
                {rep.changes.map((c, i) => (
                  <li key={i} className="flex flex-wrap items-center gap-2 px-3 py-1.5">
                    <Badge tone={ACTION_TONE[c.action] ?? "muted"}>{t(`provisioning.action.${c.action}`)}</Badge>
                    <span className="text-muted">{t(`provisioning.kind.${c.kind}`)}</span>
                    <span className="font-medium">{c.name}</span>
                    {c.detail && <span className="text-muted">— {c.detail}</span>}
                  </li>
                ))}
              </ul>
            )}
            {blocked && (
              <div role="alert" className="rounded-card border border-destructive bg-destructive/10 p-3">
                <p className="font-medium text-destructive">{t("provisioning.blockedTitle")}</p>
                <p className="mt-1">{rep.blocked}</p>
                <div className="mt-2"><Check2 id="sync-force" label={t("provisioning.forceLabel")} checked={confirm} onChange={setConfirm} /></div>
              </div>
            )}
          </>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
          <Button disabled={busy || !rep || rep.changes.length === 0 || (blocked && !confirm)} onClick={() => void apply()}>{t("provisioning.apply")}</Button>
        </div>
      </div>
    </Modal>
  );
}
