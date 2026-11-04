import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, CodeBlock, ConfirmDialog, Empty, Modal, MultiPick, PageHeader, Select, ShowInactiveToggle, Tabs, when } from "../components/kit";
import { toast } from "../components/Toaster";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import { keyActive, ruleActive } from "../lib/active";
import type { McpKey, Rule } from "../lib/types";

export function configSnippets(token: string, origin: string) {
  const url = `${origin}/mcp`;
  return {
    claude: `claude mcp add --transport http leassh ${url} --header "Authorization: Bearer ${token}"`,
    litellm: `mcp_servers:\n  leassh:\n    url: "${url}"\n    transport: "http"\n    auth_type: "bearer_token"\n    auth_value: "${token}"`,
    json: JSON.stringify({ mcpServers: { leassh: { type: "http", url, headers: { Authorization: `Bearer ${token}` } } } }, null, 2),
  };
}

export function KeysPage() {
  const { t } = useTranslation();
  const keys = useList<McpKey>("/keys");
  const rules = useList<Rule>("/rules");
  const [open, setOpen] = useState(false);
  const [showInactive, setShowInactive] = useState(false);
  const [token, setToken] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<McpKey | null>(null);
  const revoke = useApiMutation<{ id: string }>("POST", (v) => `/keys/${v.id}/revoke`, ["/keys"]);
  const ruleName = (id: string) => rules.data?.find((r) => r.id === id)?.name ?? id;
  const state = (k: McpKey): "revoked" | "expired" | "active" => k.disabled ? "revoked" : k.expires_at && new Date(k.expires_at) < new Date() ? "expired" : "active";
  const rel = (iso: string) => {
    if (!iso) return t("keys.never");
    const m = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
    return m < 1 ? t("keys.justNow") : m < 60 ? t("keys.minutesAgo", { count: m }) : m < 1440 ? t("keys.hoursAgo", { count: Math.round(m / 60) }) : t("keys.daysAgo", { count: Math.round(m / 1440) });
  };
  return (
    <>
      <PageHeader title={t("keys.title")} subtitle={t("keys.subtitle")} action={<Button onClick={() => setOpen(true)}><Plus aria-hidden size={16} />{t("keys.add")}</Button>} />
      <ShowInactiveToggle count={(keys.data ?? []).filter((k) => !keyActive(k)).length} checked={showInactive} onChange={setShowInactive} />
      {keys.data?.length === 0 ? <Empty text={t("keys.empty")} action={<Button onClick={() => setOpen(true)}>{t("keys.add")}</Button>} /> : (
        <div className="overflow-x-auto rounded-card border border-border bg-surface">
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-muted"><tr>
              {["name", "prefix", "rules", "lastUsed", "expires", "status", ""].map((c, i) => <th key={i} scope="col" className="px-4 py-2 font-medium">{c && t(`keys.col.${c}`)}</th>)}
            </tr></thead>
            <tbody>
              {keys.data?.filter((k) => showInactive || keyActive(k)).map((k) => (
                <tr key={k.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-2"><div className="font-medium">{k.name}</div><div className="text-muted">{k.description}</div></td>
                  <td className="px-4 py-2 font-mono">{k.prefix}…</td>
                  <td className="px-4 py-2">{k.rule_ids.map(ruleName).join(", ") || <span className="text-muted">—</span>}</td>
                  <td className="px-4 py-2 text-muted">{rel(k.last_used_at)}</td>
                  <td className="px-4 py-2 text-muted">{k.expires_at ? when(k.expires_at) : t("keys.never")}</td>
                  <td className="px-4 py-2"><Badge tone={state(k) === "active" ? "read" : "destructive"}>{t(`keys.state.${state(k)}`)}</Badge></td>
                  <td className="px-4 py-2 text-right">{!k.disabled && <Button variant="ghost" className="h-8 px-2 text-destructive" onClick={() => setRevoking(k)}>{t("keys.revoke")}</Button>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Modal open={open} onClose={() => setOpen(false)} title={t("keys.add")}>
        <KeyForm rules={(rules.data ?? []).filter(ruleActive)} onClose={() => setOpen(false)} onCreated={(tok) => { setOpen(false); setToken(tok); }} />
      </Modal>
      <RevealDialog token={token} onClose={() => setToken(null)} />
      <ConfirmDialog open={!!revoking} onClose={() => setRevoking(null)} title={t("keys.revokeTitle")} body={t("keys.revokeBody", { name: revoking?.name })}
        confirmWord={revoking?.name} busy={revoke.isPending}
        onConfirm={() => revoking && revoke.mutate({ id: revoking.id }, { onSuccess: () => { setRevoking(null); toast(t("keys.revoked")); } })} />
    </>
  );
}

function KeyForm({ rules, onClose, onCreated }: { rules: Rule[]; onClose: () => void; onCreated: (token: string) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [exp, setExp] = useState<"never" | "30" | "90" | "date">("never");
  const [date, setDate] = useState("");
  const [canPropose, setCanPropose] = useState(true);
  const [ruleIds, setRuleIds] = useState<string[]>([]);
  const create = useApiMutation<unknown, { token: string }>("POST", "/keys", ["/keys", "/rules"]);
  const expiresAt = () => {
    if (exp === "never") return "";
    if (exp === "date") return date;
    return new Date(Date.now() + Number(exp) * 86400000).toISOString();
  };
  return (
    <div className="flex flex-col gap-3">
      <Field id="key-name" label={t("common.name")} value={name} onChange={(e) => setName(e.target.value)} placeholder="Agent N1" />
      <Field id="key-desc" label={t("common.description")} value={description} onChange={(e) => setDescription(e.target.value)} />
      <Select id="key-exp" label={t("keys.expiration")} value={exp} onChange={(e) => setExp(e.target.value as typeof exp)}>
        <option value="never">{t("keys.never")}</option><option value="30">30 {t("keys.days")}</option><option value="90">90 {t("keys.days")}</option><option value="date">{t("keys.date")}</option>
      </Select>
      {exp === "date" && <Field id="key-date" type="date" label={t("keys.date")} value={date} onChange={(e) => setDate(e.target.value)} />}
      <Check2 id="key-propose" label={t("keys.canPropose")} checked={canPropose} onChange={setCanPropose} />
      <MultiPick label={t("keys.rules")} options={rules.map((r) => ({ id: r.id, label: r.name }))} value={ruleIds} onChange={setRuleIds} />
      {create.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(create.error)} title={t("errors.details")} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={create.isPending || !name} onClick={() => create.mutate({ name, description, expires_at: expiresAt(), can_propose: canPropose, rule_ids: ruleIds }, { onSuccess: (r) => onCreated(r.token) })}>{t("common.create")}</Button>
      </div>
    </div>
  );
}

// Le jeton n'est affiché qu'une fois; exemples de configuration avec le jeton déjà inséré.
export function RevealDialog({ token, onClose }: { token: string | null; onClose: () => void }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"claude" | "litellm" | "json">("claude");
  const snippets = token ? configSnippets(token, window.location.origin) : null;
  return (
    <Modal open={!!token} onClose={onClose} title={t("keys.revealTitle")} wide>
      {token && snippets && (
        <div className="flex flex-col gap-3">
          <p role="alert" className="rounded-field border border-write bg-write/10 p-3">{t("keys.revealWarning")}</p>
          <CodeBlock copy>{token}</CodeBlock>
          <Tabs value={tab} onChange={setTab} tabs={[{ id: "claude", label: "Claude Code" }, { id: "litellm", label: "LiteLLM" }, { id: "json", label: "JSON" }]} />
          <CodeBlock copy>{snippets[tab]}</CodeBlock>
          <div className="flex justify-end"><Button onClick={onClose}>{t("keys.revealDone")}</Button></div>
        </div>
      )}
    </Modal>
  );
}
