import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "@tanstack/react-router";
import { Bot, Check, Pencil, X } from "lucide-react";
import { Button, Card, ErrorBox } from "../components/ui";
import { Badge, Empty, MultiPick, Modal, PageHeader, RiskBadge, Textarea, when, type Tone } from "../components/kit";
import { CommandEditor, Highlighted } from "./CommandEditor";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import type { Group, Proposal } from "../lib/types";
import { useQuery } from "@tanstack/react-query";

const TONE: Record<Proposal["status"], Tone> = { pending: "write", invalid: "destructive", approved: "read", rejected: "muted" };

export const ProposalStatus = ({ s }: { s: Proposal["status"] }) => {
  const { t } = useTranslation();
  return <Badge tone={TONE[s]}>{t(`proposals.status.${s}`)}</Badge>;
};

export function ProposalsPage() {
  const { t } = useTranslation();
  const list = useList<Proposal>("/proposals");
  return (
    <>
      <PageHeader title={t("proposals.title")} subtitle={t("proposals.subtitle")} />
      {list.data?.length === 0 ? <Empty text={t("proposals.empty")} /> : (
        <div className="overflow-x-auto rounded-card border border-border bg-surface">
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-muted"><tr>{["agent", "slug", "risk", "status", "date"].map((c) => <th key={c} scope="col" className="px-4 py-2 font-medium">{t(`proposals.col.${c}`)}</th>)}</tr></thead>
            <tbody>
              {list.data?.map((p) => (
                <tr key={p.id} className="border-b border-border last:border-0 hover:bg-surface-2">
                  <td className="px-4 py-2"><span className="inline-flex items-center gap-1.5"><Bot aria-hidden size={14} className="text-muted" />{p.key_name}</span></td>
                  <td className="px-4 py-2"><Link to="/propositions/$id" params={{ id: p.id }} className="font-mono text-primary hover:underline">{p.payload.slug}</Link></td>
                  <td className="px-4 py-2"><RiskBadge risk={p.payload.risk} /></td>
                  <td className="px-4 py-2"><ProposalStatus s={p.status} /></td>
                  <td className="px-4 py-2 text-muted">{when(p.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

export function useProposal(id: string) {
  return useQuery({ queryKey: ["/proposals", id], queryFn: () => api<Proposal>(`/proposals/${id}`) });
}

// Détail en deux colonnes: la proposition (gauche) et le résultat du linter (droite).
export function ProposalDetail({ id }: { id: string }) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const q = useProposal(id);
  const groups = useList<Group>("/command-groups");
  const [dialog, setDialog] = useState<"approve" | "reject" | null>(null);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [comment, setComment] = useState("");
  const approve = useApiMutation<unknown>("POST", `/proposals/${id}/approve`, ["/proposals", "/commands", "/command-groups"]);
  const reject = useApiMutation<unknown>("POST", `/proposals/${id}/reject`, ["/proposals"]);
  const p = q.data;
  if (q.isPending) return <p className="text-muted">{t("errors.loading")}</p>;
  if (!p) return <p role="alert" className="text-destructive">{t("errors.generic")}</p>;
  const open = p.status === "pending" || p.status === "invalid";
  const params = Object.entries(p.payload.params ?? {});
  return (
    <>
      <PageHeader title={p.payload.slug} subtitle={p.payload.name}
        action={<Link to="/propositions" className="inline-flex h-9 items-center rounded-field border border-border-strong bg-surface-2 px-4">{t("common.back")}</Link>} />
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2"><ProposalStatus s={p.status} /><RiskBadge risk={p.payload.risk} /><span className="inline-flex items-center gap-1.5"><Bot aria-hidden size={14} />{p.key_name}</span><span className="text-muted">{when(p.created_at)}</span></div>
          <section><h2 className="mb-1 text-[15px] font-semibold">{t("proposals.justification")}</h2><blockquote className="border-l-4 border-primary pl-3">{p.justification || "—"}</blockquote></section>
          <section><h2 className="mb-1 text-[15px] font-semibold">{t("proposals.description")}</h2><p>{p.payload.description}</p></section>
          <section><h2 className="mb-1 text-[15px] font-semibold">{t("editor.template")}</h2><div className="rounded-field bg-surface-2 p-3"><Highlighted template={p.payload.template} /></div></section>
          <section><h2 className="mb-1 text-[15px] font-semibold">{t("editor.params")}</h2>
            {params.length === 0 ? <p className="text-muted">{t("editor.noParams")}</p> : (
              <ul className="flex flex-col gap-1">{params.map(([k, d]) => <li key={k}><code className="font-mono">{k}</code> <Badge>{d.type}</Badge> <span className="text-muted">{d.description}</span></li>)}</ul>
            )}
          </section>
          {p.review_comment && <p className="text-muted">{t("proposals.reviewComment")} : « {p.review_comment} »</p>}
        </Card>
        <Card className="flex flex-col gap-3">
          <h2 className="text-[15px] font-semibold">{t("proposals.lint")}</h2>
          {p.lint.errors.length === 0 && p.lint.warnings.length === 0 && <p className="text-read">✔ {t("editor.lintOk")}</p>}
          {p.lint.errors.map((e, i) => <p key={`e${i}`} role="alert" className="text-destructive">✖ {e.message}{e.param && <> <code className="font-mono">({e.param})</code></>}</p>)}
          {p.lint.warnings.map((e, i) => <p key={`w${i}`} className="text-write">▲ {e.message}</p>)}
          {p.status === "invalid" && <p className="text-muted">{t("proposals.invalidHelp")}</p>}
          {open && (
            <div className="mt-auto flex flex-wrap justify-end gap-2 pt-2">
              <Button variant="secondary" onClick={() => setDialog("reject")}><X aria-hidden size={16} />{t("approvals.reject")}</Button>
              <Link to="/propositions/$id/modifier" params={{ id }} className="inline-flex h-9 items-center gap-2 rounded-field border border-border-strong bg-surface-2 px-4"><Pencil aria-hidden size={16} />{t("proposals.editApprove")}</Link>
              <Button disabled={p.status === "invalid"} onClick={() => setDialog("approve")}><Check aria-hidden size={16} />{t("proposals.approve")}</Button>
            </div>
          )}
        </Card>
      </div>
      <Modal open={dialog === "approve"} onClose={() => setDialog(null)} title={t("proposals.approve")}>
        <div className="flex flex-col gap-3">
          <p>{t("proposals.approveBody", { risk: t(`risk.${p.payload.risk}`) })}</p>
          <MultiPick label={t("proposals.addToGroups")} options={(groups.data ?? []).map((g) => ({ id: g.id, label: g.name }))} value={groupIds} onChange={setGroupIds} />
          <Textarea id="p-comment" label={t("approvals.comment")} value={comment} onChange={(e) => setComment(e.target.value)} />
          {approve.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(approve.error)} title={t("errors.details")} />}
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setDialog(null)}>{t("common.cancel")}</Button>
            <Button disabled={approve.isPending} onClick={() => approve.mutate({ group_ids: groupIds, comment }, { onSuccess: () => { toast(t("proposals.approvedToast")); void nav({ to: "/propositions" }); } })}>{t("proposals.approve")}</Button></div>
        </div>
      </Modal>
      <Modal open={dialog === "reject"} onClose={() => setDialog(null)} title={t("approvals.reject")}>
        <div className="flex flex-col gap-3">
          <Textarea id="p-reject" label={t("approvals.comment")} value={comment} onChange={(e) => setComment(e.target.value)} hint={t("approvals.commentHint")} />
          {reject.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(reject.error)} title={t("errors.details")} />}
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setDialog(null)}>{t("common.cancel")}</Button>
            <Button disabled={reject.isPending || !comment.trim()} onClick={() => reject.mutate({ comment }, { onSuccess: () => { toast(t("proposals.rejectedToast")); void nav({ to: "/propositions" }); } })}>{t("approvals.reject")}</Button></div>
        </div>
      </Modal>
    </>
  );
}

export function ProposalEditorRoute({ id }: { id: string }) {
  const { t } = useTranslation();
  const q = useProposal(id);
  if (q.isPending) return <p className="text-muted">{t("errors.loading")}</p>;
  if (!q.data) return <p role="alert" className="text-destructive">{t("errors.generic")}</p>;
  return <CommandEditor key={q.data.id} proposal={q.data} />;
}
