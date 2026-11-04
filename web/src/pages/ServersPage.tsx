import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, HelpCircle, Plus, ShieldAlert } from "lucide-react";
import { Button, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, ListSearch, SortTh, CodeBlock, ConfirmDialog, Empty, Modal, PageHeader, Select, ShowInactiveToggle, SyncMark, Tabs } from "../components/kit";
import { GroupsPanel } from "../components/GroupsPanel";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import { serverActive } from "../lib/active";
import { useListView } from "../lib/listview";
import type { Identity, Provisioner, SshServer } from "../lib/types";

export function HostKeyBadge({ status }: { status: SshServer["host_key_status"] }) {
  const { t } = useTranslation();
  const m = {
    pinned: { tone: "read" as const, icon: <CheckCircle2 aria-hidden size={12} /> },
    unknown: { tone: "write" as const, icon: <HelpCircle aria-hidden size={12} /> },
    mismatch: { tone: "destructive" as const, icon: <ShieldAlert aria-hidden size={12} /> },
  }[status];
  return <Badge tone={m.tone}>{m.icon}{t(`servers.hostKey.${status}`)}</Badge>;
}

interface Probe { reachable: boolean; fingerprint?: string; key_type?: string; host_key_status?: string; pinned_fingerprint?: string; matches?: boolean; auth_ok?: boolean; error?: string }

export function ServersPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"servers" | "groups">("servers");
  const servers = useList<SshServer>("/servers");
  const identities = useList<Identity>("/identities");
  const provs = useList<Provisioner>("/provisioners");
  const sourceOf = (s: SshServer) => provs.data?.find((p) => p.id === s.managed_by)?.name ?? "";
  const [editing, setEditing] = useState<SshServer | "new" | null>(null);
  const [probing, setProbing] = useState<SshServer | null>(null);
  const [deleting, setDeleting] = useState<SshServer | null>(null);
  const [showInactive, setShowInactive] = useState(false);
  const del = useApiMutation<{ id: string }>("DELETE", (v) => `/servers/${v.id}`, ["/servers"]);
  const identName = (id: string) => identities.data?.find((i) => i.id === id)?.name ?? "—";
  const visible = (servers.data ?? []).filter((s) => showInactive || serverActive(s));
  const view = useListView(visible, {
    text: (s) => `${s.name} ${s.slug} ${s.host}:${s.port} ${s.ssh_user} ${identName(s.identity_id)} ${s.tags.join(" ")} ${s.host_key_status}`,
    sorts: { name: (s) => s.name, address: (s) => `${s.host}:${s.port}`, identity: (s) => identName(s.identity_id), hostKey: (s) => s.host_key_status },
  });
  const blocked = servers.data?.filter((s) => s.host_key_status === "mismatch") ?? [];

  return (
    <>
      <PageHeader title={t("servers.title")} subtitle={t("servers.subtitle")}
        action={<Button onClick={() => setEditing("new")}><Plus aria-hidden size={16} />{t("servers.add")}</Button>} />
      {blocked.map((s) => (
        <div key={s.id} role="alert" className="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-card border border-destructive bg-destructive/10 p-4">
          <p><strong className="text-destructive">{t("servers.mismatchTitle", { slug: s.slug })}</strong> {t("servers.mismatchBody")}</p>
          <Button variant="secondary" onClick={() => setProbing(s)}>{t("servers.review")}</Button>
        </div>
      ))}
      {tab === "servers" && <ShowInactiveToggle count={(servers.data ?? []).filter((s) => !serverActive(s)).length} checked={showInactive} onChange={setShowInactive} />}
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "servers", label: t("servers.tabServers") }, { id: "groups", label: t("servers.tabGroups") }]} />
      {tab === "groups" ? <GroupsPanel kind="server" members={(servers.data ?? []).map((s) => ({ id: s.id, label: s.slug, inactive: !serverActive(s) }))} /> : servers.data?.length === 0 ? (
        <Empty text={t("servers.empty")} action={<Button onClick={() => setEditing("new")}>{t("servers.add")}</Button>} />
      ) : (
        <>
        <ListSearch value={view.query} onChange={view.setQuery} label={t("servers.search")} count={view.rows.length} total={visible.length} />
        <div className="overflow-x-auto rounded-card border border-border bg-surface">
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-muted"><tr>
              {["name", "address", "identity", "hostKey", ""].map((c, i) => <SortTh key={i} label={c ? t(`servers.col.${c}`) : ""} sortKey={c || undefined} sort={view.sort} onSort={view.toggle} />)}
            </tr></thead>
            <tbody>
              {view.rows.length === 0 && <tr><td colSpan={5} className="px-4 py-6 text-center text-muted">{t("common.noMatch")}</td></tr>}
              {view.rows.map((s) => (
                <tr key={s.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2"><div className="font-medium">{s.name} {s.managed_by && <SyncMark source={sourceOf(s)} kind="server" />}</div><code className="font-mono text-muted">{s.slug}</code>{!s.enabled && <> <Badge>{t("common.disabled")}</Badge></>}</td>
                  <td className="px-4 py-2 font-mono">{s.host}:{s.port}{s.ssh_user && <div className="text-muted">{s.ssh_user}@</div>}</td>
                  <td className="px-4 py-2">{identName(s.identity_id)}</td>
                  <td className="px-4 py-2"><HostKeyBadge status={s.host_key_status} /></td>
                  <td className="px-4 py-2 text-right">
                    <Button variant="ghost" className="h-8 px-2" onClick={() => setProbing(s)}>{t("servers.test")}</Button>
                    <Button variant="ghost" className="h-8 px-2" onClick={() => setEditing(s)}>{t("common.edit")}</Button>
                    {!s.managed_by && <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setDeleting(s)}>{t("common.delete")}</Button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        </>
      )}
      <ServerForm editing={editing} identities={identities.data ?? []} onClose={() => setEditing(null)} />
      <ProbeDialog server={probing} onClose={() => setProbing(null)} />
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} title={t("servers.deleteTitle")} body={t("servers.deleteBody", { name: deleting?.name })}
        confirmWord={deleting?.slug} busy={del.isPending}
        onConfirm={() => deleting && del.mutate({ id: deleting.id }, { onSuccess: () => { setDeleting(null); toast(t("common.deleted")); } })} />
    </>
  );
}

function ServerForm({ editing, identities, onClose }: { editing: SshServer | "new" | null; identities: Identity[]; onClose: () => void }) {
  const { t } = useTranslation();
  const s = editing && editing !== "new" ? editing : null;
  return (
    <Modal open={!!editing} onClose={onClose} title={s ? t("servers.edit") : t("servers.add")}>
      <ServerFields key={s?.id ?? "new"} s={s} identities={identities} onClose={onClose} />
    </Modal>
  );
}

function ServerFields({ s, identities, onClose }: { s: SshServer | null; identities: Identity[]; onClose: () => void }) {
  const { t } = useTranslation();
  const [f, setF] = useState({ slug: s?.slug ?? "", name: s?.name ?? "", host: s?.host ?? "", port: s?.port ?? 22, identity_id: s?.identity_id ?? identities[0]?.id ?? "",
    description: s?.description ?? "", tags: (s?.tags ?? []).join(", "), enabled: s?.enabled ?? true, ssh_user: s?.ssh_user ?? "", ssh_user_follow_sync: false });
  const save = useApiMutation<unknown>(s ? "PUT" : "POST", s ? `/servers/${s.id}` : "/servers", ["/servers"]);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((x) => ({ ...x, [k]: v }));
  const locked = !!s?.managed_by; // serveur synchronisé: nom, adresse, utilisateur SSH et description viennent de la source
  return (
    <div className="flex flex-col gap-3">
      {locked && <p className="rounded-field bg-surface-2 p-3 text-muted">{t("provisioning.serverLocked")}</p>}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field id="srv-name" disabled={locked} label={t("common.name")} value={f.name} onChange={(e) => set("name", e.target.value)} />
        <Field id="srv-slug" disabled={locked} label={t("common.slug")} value={f.slug} onChange={(e) => set("slug", e.target.value)} placeholder="web-01" />
        <Field id="srv-host" disabled={locked} label={t("servers.host")} value={f.host} onChange={(e) => set("host", e.target.value)} />
        <Field id="srv-port" disabled={locked} label={t("servers.port")} type="number" value={f.port} onChange={(e) => set("port", Number(e.target.value))} />
      </div>
      <Select id="srv-ident" label={t("servers.identity")} value={f.identity_id} onChange={(e) => set("identity_id", e.target.value)}>
        {identities.length === 0 && <option value="">{t("servers.noIdentity")}</option>}
        {identities.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
      </Select>
      <div className="flex flex-col gap-1">
        <Field id="srv-ssh-user" disabled={f.ssh_user_follow_sync} label={t("servers.sshUser")} value={f.ssh_user} onChange={(e) => set("ssh_user", e.target.value)} placeholder={t("servers.sshUserPlaceholder")} autoComplete="off" />
        <p className="text-muted">{t("servers.sshUserHelp")}</p>
        {locked && s?.ssh_user_custom && <Check2 id="srv-follow-sync" label={t("provisioning.followSync")} checked={f.ssh_user_follow_sync} onChange={(v) => set("ssh_user_follow_sync", v)} />}
      </div>
      <Field id="srv-desc" disabled={locked} label={t("servers.descriptionForAgent")} value={f.description} onChange={(e) => set("description", e.target.value)} />
      <Field id="srv-tags" label={t("servers.tags")} value={f.tags} onChange={(e) => set("tags", e.target.value)} />
      <Check2 id="srv-enabled" label={t("common.enabled")} checked={f.enabled} onChange={(v) => set("enabled", v)} />
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={save.isPending || !f.identity_id} onClick={() => save.mutate({ ...f, tags: f.tags.split(",").map((x) => x.trim()).filter(Boolean) }, { onSuccess: () => { onClose(); toast(t("common.saved")); } })}>{t("common.save")}</Button>
      </div>
    </div>
  );
}

// Test de connexion → affichage de l'empreinte → « Faire confiance à cette clé » (docs/UX.md §5.7).
function ProbeDialog({ server, onClose }: { server: SshServer | null; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [probe, setProbe] = useState<Probe | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const run = async () => {
    if (!server) return;
    setBusy(true); setErr("");
    try { setProbe(await api<Probe>(`/servers/${server.id}/test-connection`, { method: "POST" })); await qc.invalidateQueries({ queryKey: ["/servers"] }); }
    catch (e) { setErr(errorMessage(e)); } finally { setBusy(false); }
  };
  const pin = async () => {
    if (!server || !probe?.fingerprint) return;
    setBusy(true); setErr("");
    try {
      await api(`/servers/${server.id}/host-key`, { method: "POST", body: { fingerprint: probe.fingerprint } });
      await qc.invalidateQueries({ queryKey: ["/servers"] });
      toast(t("servers.pinned"));
      onClose();
    } catch (e) { setErr(errorMessage(e)); } finally { setBusy(false); }
  };
  const changed = probe?.reachable && probe.pinned_fingerprint && probe.fingerprint !== probe.pinned_fingerprint;
  return (
    <Modal open={!!server} onClose={() => { setProbe(null); setErr(""); onClose(); }} title={t("servers.probeTitle", { slug: server?.slug })}>
      <div className="flex flex-col gap-3">
        <Button onClick={() => void run()} disabled={busy}>{t("servers.test")}</Button>
        {err && <ErrorBox message={t("errors.generic")} detail={err} title={t("errors.details")} />}
        {probe && !probe.reachable && <ErrorBox message={t("servers.unreachable")} detail={probe.error} title={t("errors.details")} />}
        {probe?.reachable && (
          <>
            <p className="text-muted">{t("servers.presentedFingerprint")}</p>
            <CodeBlock copy>{probe.fingerprint ?? ""}</CodeBlock>
            {changed && (<><p role="alert" className="text-destructive">{t("servers.fingerprintChanged")}</p><p className="text-muted">{t("servers.previousFingerprint")}</p><CodeBlock>{probe.pinned_fingerprint ?? ""}</CodeBlock></>)}
            {probe.auth_ok === true && <p className="text-read">✔ {t("servers.authOk")}</p>}
            {probe.auth_ok === false && <p role="alert" className="text-destructive">{t("servers.authFailed")} {probe.error}</p>}
            {(probe.host_key_status === "unknown" || changed) && (
              <Button onClick={() => void pin()} disabled={busy} className={changed ? "!bg-destructive !text-primary-fg" : ""}>
                {changed ? t("servers.acceptNewKey") : t("servers.trustKey")}
              </Button>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}
