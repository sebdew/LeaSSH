import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, ConfirmDialog, Empty, Modal, MultiPick, PageHeader, ProfileBadge, Select, ShowInactiveToggle } from "../components/kit";
import { toast } from "../components/Toaster";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import { keyActive, ruleActive } from "../lib/active";
import type { Group, McpKey, Rule } from "../lib/types";

export function RulesPage() {
  const { t } = useTranslation();
  const rules = useList<Rule>("/rules");
  const keys = useList<McpKey>("/keys");
  const sgs = useList<Group>("/server-groups");
  const cgs = useList<Group>("/command-groups");
  const [editing, setEditing] = useState<Rule | "new" | null>(null);
  const [deleting, setDeleting] = useState<Rule | null>(null);
  const [showInactive, setShowInactive] = useState(false);
  const del = useApiMutation<{ id: string }>("DELETE", (v) => `/rules/${v.id}`, ["/rules", "/keys"]);
  const nameOf = (list: { id: string; name: string }[] | undefined, id: string) => list?.find((x) => x.id === id)?.name ?? "?";
  const names = (ids: string[], list?: Group[]) => ids.map((id) => nameOf(list, id)).join(", ");
  const count = (ids: string[], list?: Group[]) => new Set(ids.flatMap((id) => list?.find((x) => x.id === id)?.member_ids ?? [])).size;

  return (
    <>
      <PageHeader title={t("rules.title")} subtitle={t("rules.subtitle")} action={<Button onClick={() => setEditing("new")}><Plus aria-hidden size={16} />{t("rules.add")}</Button>} />
      <ShowInactiveToggle count={(rules.data ?? []).filter((r) => !ruleActive(r)).length} checked={showInactive} onChange={setShowInactive} />
      {rules.data?.length === 0 ? <Empty text={t("rules.empty")} action={<Button onClick={() => setEditing("new")}>{t("rules.add")}</Button>} /> : (
        <div className="grid gap-3">
          {rules.data?.filter((r) => showInactive || ruleActive(r)).map((r) => (
            <div key={r.id} className="min-w-0 break-words rounded-card border border-border bg-surface p-4">
              <div className="flex flex-wrap items-start justify-between gap-2">
                <h3 className="font-semibold">{r.name} {!r.enabled && <Badge>{t("common.disabled")}</Badge>}</h3>
                <div className="flex items-center gap-2">
                  <ProfileBadge profile={r.profile} />
                  <Button variant="ghost" className="h-8 px-2" onClick={() => setEditing(r)}>{t("common.edit")}</Button>
                  <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setDeleting(r)}>{t("common.delete")}</Button>
                </div>
              </div>
              {/* règle lisible comme une phrase (docs/specs/UX.md §5.9) */}
              <p className="mt-2 text-[15px]" data-testid="rule-sentence">
                {t("rules.sentence", {
                  keys: r.key_ids.filter((k) => { const key = keys.data?.find((x) => x.id === k); return !key || keyActive(key); }).map((k) => nameOf(keys.data?.map((x) => ({ id: x.id, name: x.name })), k)).join(", ") || t("rules.noActiveKey"),
                  commands: names(r.command_group_ids, cgs.data), servers: names(r.server_group_ids, sgs.data),
                  profile: t(`rules.profile.${r.profile}`),
                })}
              </p>
              <p className="mt-1 text-muted">
                {t("rules.counts", { servers: count(r.server_group_ids, sgs.data), commands: count(r.command_group_ids, cgs.data) })}
                {r.profile === "RW" && ` · ${r.write_requires_approval ? t("rules.writeNeedsApproval") : t("rules.writeDirect")}`}
              </p>
            </div>
          ))}
        </div>
      )}
      <Modal open={!!editing} onClose={() => setEditing(null)} title={editing && editing !== "new" ? t("rules.edit") : t("rules.add")}>
        <RuleForm key={editing && editing !== "new" ? editing.id : "new"} rule={editing && editing !== "new" ? editing : null} keys={(keys.data ?? []).filter(keyActive)} sgs={sgs.data ?? []} cgs={cgs.data ?? []} onClose={() => setEditing(null)} />
      </Modal>
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} title={t("rules.deleteTitle")} body={t("rules.deleteBody", { name: deleting?.name })} busy={del.isPending}
        onConfirm={() => deleting && del.mutate({ id: deleting.id }, { onSuccess: () => { setDeleting(null); toast(t("common.deleted")); } })} />
    </>
  );
}

function RuleForm({ rule, keys, sgs, cgs, onClose }: { rule: Rule | null; keys: McpKey[]; sgs: Group[]; cgs: Group[]; onClose: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(rule?.name ?? "");
  const [sg, setSg] = useState<string[]>(rule?.server_group_ids ?? []);
  const [cg, setCg] = useState<string[]>(rule?.command_group_ids ?? []);
  const [profile, setProfile] = useState<"RO" | "RW">(rule?.profile ?? "RO");
  const [wra, setWra] = useState(rule?.write_requires_approval ?? true);
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [keyIds, setKeyIds] = useState<string[]>((rule?.key_ids ?? []).filter((id) => keys.some((k) => k.id === id))); // seules les clés actives sont conservées
  const save = useApiMutation<unknown>(rule ? "PUT" : "POST", rule ? `/rules/${rule.id}` : "/rules", ["/rules", "/keys"]);
  return (
    <div className="flex flex-col gap-3">
      <Field id="rule-name" label={t("common.name")} value={name} onChange={(e) => setName(e.target.value)} placeholder="N1 — diagnostic BDD" />
      <MultiPick label={t("rules.keys")} options={keys.map((k) => ({ id: k.id, label: k.name }))} value={keyIds} onChange={setKeyIds} />
      <MultiPick label={t("rules.serverGroups")} options={sgs.map((x) => ({ id: x.id, label: x.name }))} value={sg} onChange={setSg} />
      <MultiPick label={t("rules.commandGroups")} options={cgs.map((x) => ({ id: x.id, label: x.name }))} value={cg} onChange={setCg} />
      <Select id="rule-profile" label={t("rules.profileLabel")} value={profile} onChange={(e) => setProfile(e.target.value as "RO" | "RW")}>
        <option value="RO">{t("rules.profile.RO")} (RO)</option><option value="RW">{t("rules.profile.RW")} (RW)</option>
      </Select>
      <Check2 id="rule-wra" label={t("rules.writeApprovalOption")} checked={wra} disabled={profile === "RO"} onChange={setWra} />
      <p className="text-muted">{t("rules.destructiveNote")}</p>
      <Check2 id="rule-enabled" label={t("common.enabled")} checked={enabled} onChange={setEnabled} />
      {(sgs.length === 0 || cgs.length === 0) && <p className="text-write">{t("rules.needGroups")}</p>}
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={save.isPending || !name || sg.length === 0 || cg.length === 0}
          onClick={() => save.mutate({ name, server_group_ids: sg, command_group_ids: cg, profile, write_requires_approval: wra, enabled, key_ids: keyIds }, { onSuccess: () => { onClose(); toast(t("common.saved")); } })}>{t("common.save")}</Button>
      </div>
    </div>
  );
}
