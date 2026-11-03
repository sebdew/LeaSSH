import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button, ErrorBox, Field } from "./ui";
import { ConfirmDialog, Empty, Modal, MultiPick, SyncMark } from "./kit";
import { toast } from "./Toaster";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import type { Group, Provisioner } from "../lib/types";

interface Props { kind: "server" | "command"; members: { id: string; label: string; inactive?: boolean }[] }

// Groupes de serveurs / de commandes: mêmes écrans, onglet « Groupes » de leur page.
export function GroupsPanel({ kind, members }: Props) {
  const { t } = useTranslation();
  const path = kind === "server" ? "/server-groups" : "/command-groups";
  const groups = useList<Group>(path);
  const provs = useList<Provisioner>("/provisioners");
  const sourceOf = (g: Group) => provs.data?.find((p) => p.id === g.managed_by)?.name ?? "";
  const [editing, setEditing] = useState<Group | "new" | null>(null);
  const [deleting, setDeleting] = useState<Group | null>(null);
  const del = useApiMutation<{ id: string }>("DELETE", (v) => `${path}/${v.id}`, [path]);
  const labelOf = (id: string) => members.find((m) => m.id === id)?.label ?? id;

  return (
    <>
      <div className="mb-4 flex justify-end">
        <Button onClick={() => setEditing("new")}><Plus aria-hidden size={16} />{t("groups.add")}</Button>
      </div>
      {groups.data?.length === 0 && <Empty text={t(`groups.empty.${kind}`)} action={<Button onClick={() => setEditing("new")}>{t("groups.add")}</Button>} />}
      <div className="grid gap-3 md:grid-cols-2">
        {groups.data?.map((g) => (
          <div key={g.id} className="rounded-card border border-border bg-surface p-4">
            <div className="flex items-start justify-between gap-2">
              <div>
                <h3 className="font-semibold">{g.name} {g.managed_by && <SyncMark source={sourceOf(g)} kind="group" />}</h3>
                <code className="font-mono text-[13px] text-muted">{g.slug}</code>
              </div>
              <div className="flex gap-1">
                <Button variant="ghost" className="h-8 px-2" onClick={() => setEditing(g)}>{t("common.edit")}</Button>
                {!g.managed_by && <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setDeleting(g)}>{t("common.delete")}</Button>}
              </div>
            </div>
            {g.description && <p className="mt-2 text-muted">{g.description}</p>}
            <p className="mt-2 text-muted">{t("groups.count", { count: g.member_ids.length })}</p>
            <p className="mt-1 line-clamp-2 font-mono text-[12px]">{g.member_ids.map(labelOf).join(", ")}</p>
          </div>
        ))}
      </div>
      <GroupForm kind={kind} path={path} members={members} editing={editing} onClose={() => setEditing(null)} />
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} title={t("groups.deleteTitle")} body={t("groups.deleteBody", { name: deleting?.name })}
        confirmWord={deleting?.slug} busy={del.isPending}
        onConfirm={() => deleting && del.mutate({ id: deleting.id }, { onSuccess: () => { setDeleting(null); toast(t("common.deleted")); } })} />
    </>
  );
}

// L'état du formulaire vit dans GroupFormBody, monté (avec une clé) à chaque ouverture: éditer un groupe existant le pré-remplit.
function GroupForm({ kind, path, members, editing, onClose }: Props & { path: string; editing: Group | "new" | null; onClose: () => void }) {
  const { t } = useTranslation();
  const g = editing && editing !== "new" ? editing : null;
  return (
    <Modal open={!!editing} onClose={onClose} title={g ? t("groups.edit") : t("groups.add")}>
      <GroupFormBody key={g?.id ?? "new"} g={g} kind={kind} path={path} members={members} onClose={onClose} />
    </Modal>
  );
}

function GroupFormBody({ g, kind, path, members, onClose }: Props & { g: Group | null; path: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [slug, setSlug] = useState(g?.slug ?? "");
  const [name, setName] = useState(g?.name ?? "");
  const [description, setDescription] = useState(g?.description ?? "");
  const [ids, setIds] = useState<string[]>(g?.member_ids ?? []);
  const managed = !!g?.managed_by; // groupe synchronisé: seuls ses membres se modifient (les membres synchronisés restent)
  const save = useApiMutation<unknown>(g ? "PUT" : "POST", g ? (managed ? `${path}/${g.id}/members` : `${path}/${g.id}`) : path, [path]);
  const body = managed ? { ids } : { slug, name, description, member_ids: ids };
  return (
    <>
      {managed && <p className="mb-3 rounded-field bg-surface-2 p-3 text-muted">{t("provisioning.groupLocked")}</p>}
      <GroupFields {...{ slug, setSlug, name, setName, description, setDescription, ids, setIds, members, kind, locked: g?.managed_member_ids ?? [], readOnly: managed }} />
      {save.error && <div className="mt-3"><ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} /></div>}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={save.isPending} onClick={() => save.mutate(body, { onSuccess: () => { onClose(); toast(t("common.saved")); } })}>{t("common.save")}</Button>
      </div>
    </>
  );
}

function GroupFields(p: {
  slug: string; setSlug: (v: string) => void; name: string; setName: (v: string) => void; description: string; setDescription: (v: string) => void;
  ids: string[]; setIds: (v: string[]) => void; members: { id: string; label: string; inactive?: boolean }[]; kind: string; locked: string[]; readOnly: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-3">
      <Field id="g-slug" label={t("common.slug")} value={p.slug} onChange={(e) => p.setSlug(e.target.value)} disabled={p.readOnly} className="h-9 rounded-field border border-border-strong bg-surface px-3 font-mono" />
      <Field id="g-name" label={t("common.name")} value={p.name} onChange={(e) => p.setName(e.target.value)} disabled={p.readOnly} />
      <Field id="g-desc" label={t("common.description")} value={p.description} onChange={(e) => p.setDescription(e.target.value)} disabled={p.readOnly} />
      {/* un élément désactivé n'est proposé que s'il est déjà membre (pour ne pas le retirer sans le vouloir) */}
      <MultiPick label={t(`groups.members.${p.kind}`)} options={p.members.filter((m) => !m.inactive || p.ids.includes(m.id)).map((m) => ({ id: m.id, label: m.inactive ? `${m.label} (${t("common.disabled").toLowerCase()})` : m.label }))} value={p.ids} onChange={p.setIds} locked={p.locked} />
    </div>
  );
}
