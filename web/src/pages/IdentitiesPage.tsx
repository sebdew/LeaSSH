import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button, ErrorBox, Field } from "../components/ui";
import { Badge, CodeBlock, ConfirmDialog, Empty, Modal, PageHeader, Select, Textarea, when } from "../components/kit";
import { toast } from "../components/Toaster";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import type { Identity } from "../lib/types";

interface Created { identity: Identity; authorized_keys_line?: string }

export function IdentitiesPage() {
  const { t } = useTranslation();
  const list = useList<Identity>("/identities");
  const [open, setOpen] = useState(false);
  const [created, setCreated] = useState<Created | null>(null);
  const [deleting, setDeleting] = useState<Identity | null>(null);
  const del = useApiMutation<{ id: string }>("DELETE", (v) => `/identities/${v.id}`, ["/identities"]);
  return (
    <>
      <PageHeader title={t("identities.title")} subtitle={t("identities.subtitle")} action={<Button onClick={() => setOpen(true)}><Plus aria-hidden size={16} />{t("identities.add")}</Button>} />
      {list.data?.length === 0 ? <Empty text={t("identities.empty")} action={<Button onClick={() => setOpen(true)}>{t("identities.add")}</Button>} /> : (
        <div className="grid gap-3 md:grid-cols-2">
          {list.data?.map((i) => (
            <div key={i.id} className="rounded-card border border-border bg-surface p-4">
              <div className="flex items-start justify-between gap-2">
                <div><h3 className="font-semibold">{i.name}</h3><code className="font-mono text-[13px] text-muted">{i.username}</code></div>
                <div className="flex items-center gap-2"><Badge tone="info">{t(`identities.type.${i.auth_type}`)}</Badge>
                  <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setDeleting(i)}>{t("common.delete")}</Button></div>
              </div>
              {i.public_key && <div className="mt-3"><CodeBlock copy>{i.public_key}</CodeBlock></div>}
              <p className="mt-2 text-muted">{when(i.created_at)}</p>
            </div>
          ))}
        </div>
      )}
      <IdentityForm open={open} onClose={() => setOpen(false)} onCreated={(c) => { setOpen(false); setCreated(c); }} />
      <Modal open={!!created} onClose={() => setCreated(null)} title={t("identities.createdTitle")}>
        <div className="flex flex-col gap-3">
          <p>{t("identities.createdBody")}</p>
          {created?.identity.public_key && <CodeBlock copy>{created.identity.public_key}</CodeBlock>}
          {created?.authorized_keys_line && <><p className="text-muted">{t("identities.pasteLine")}</p><CodeBlock copy>{created.authorized_keys_line}</CodeBlock></>}
          <p className="text-muted">{t("identities.neverShown")}</p>
          <div className="flex justify-end"><Button onClick={() => setCreated(null)}>{t("common.close")}</Button></div>
        </div>
      </Modal>
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} title={t("identities.deleteTitle")} body={t("identities.deleteBody", { name: deleting?.name })}
        confirmWord={deleting?.name} busy={del.isPending}
        onConfirm={() => deleting && del.mutate({ id: deleting.id }, { onSuccess: () => { setDeleting(null); toast(t("common.deleted")); } })} />
      {del.error && <div className="mt-4"><ErrorBox message={t("identities.inUse")} detail={errorMessage(del.error)} title={t("errors.details")} /></div>}
    </>
  );
}

function IdentityForm({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (c: Created) => void }) {
  const { t } = useTranslation();
  return (
    <Modal open={open} onClose={onClose} title={t("identities.add")}>
      <IdentityFields onClose={onClose} onCreated={onCreated} />
    </Modal>
  );
}

function IdentityFields({ onClose, onCreated }: { onClose: () => void; onCreated: (c: Created) => void }) {
  const { t } = useTranslation();
  const [mode, setMode] = useState<"generate" | "import" | "password">("generate");
  const [name, setName] = useState("");
  const [username, setUsername] = useState("root");
  const [privateKey, setPrivateKey] = useState("");
  const [password, setPassword] = useState("");
  const create = useApiMutation<unknown, Created>("POST", "/identities", ["/identities"]);
  return (
    <div className="flex flex-col gap-3">
      <Select id="id-mode" label={t("identities.mode")} value={mode} onChange={(e) => setMode(e.target.value as typeof mode)}>
        <option value="generate">{t("identities.mode_generate")}</option>
        <option value="import">{t("identities.mode_import")}</option>
        <option value="password">{t("identities.mode_password")}</option>
      </Select>
      <Field id="id-name" label={t("common.name")} value={name} onChange={(e) => setName(e.target.value)} placeholder="root homelab" />
      <Field id="id-user" label={t("identities.remoteUser")} value={username} onChange={(e) => setUsername(e.target.value)} />
      {mode === "import" && <Textarea id="id-key" mono label={t("identities.privateKey")} value={privateKey} onChange={(e) => setPrivateKey(e.target.value)} hint={t("identities.noPassphrase")} />}
      {mode === "password" && <Field id="id-pass" type="password" autoComplete="off" label={t("setup.password")} value={password} onChange={(e) => setPassword(e.target.value)} />}
      {create.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(create.error)} title={t("errors.details")} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={create.isPending || !name || !username} onClick={() => create.mutate({ name, username, mode, private_key: privateKey, password }, { onSuccess: onCreated })}>{t("common.create")}</Button>
      </div>
    </div>
  );
}
