import { Fragment, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Check, Clock, X } from "lucide-react";
import { Button, Card, ErrorBox } from "../components/ui";
import { CodeBlock, ConfirmDialog, Empty, PageHeader, RiskBadge, StatusBadge, Textarea } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useList } from "../lib/queries";
import { useMe } from "../lib/useAuth";
import type { Execution } from "../lib/types";

const TERMINAL = ["succeeded", "failed", "timeout", "error", "rejected", "expired", "denied"];

export function useCountdown(requestedAt: string, ttlSeconds: number) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const h = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(h); }, []);
  return Math.max(0, Math.floor((new Date(requestedAt).getTime() + ttlSeconds * 1000 - now) / 1000));
}

export const fmtCountdown = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;

export function ApprovalsPage() {
  const { t } = useTranslation();
  const pending = useList<Execution>("/executions?status=pending_approval", { refetchInterval: 5000 });
  const meta = useQuery({ queryKey: ["meta"], queryFn: () => api<{ approval_ttl_seconds: number }>("/meta") });
  const me = useMe(true);
  const canDecide = me.data?.role === "admin" || me.data?.role === "approver";
  const [decided, setDecided] = useState<string[]>([]);
  const items = pending.data ?? [];
  return (
    <>
      <PageHeader title={t("approvals.title")} subtitle={t("approvals.subtitle")} />
      {!canDecide && me.data && <p className="mb-4 text-muted">{t("approvals.readOnly")}</p>}
      {items.length === 0 && decided.length === 0 && <Empty text={t("approvals.empty")} />}
      <div className="mx-auto flex max-w-3xl flex-col gap-4">
        {decided.map((id) => <DecidedCard key={id} id={id} />)}
        {items.filter((e) => !decided.includes(e.id)).map((e) => (
          <ApprovalCard key={e.id} exec={e} ttl={meta.data?.approval_ttl_seconds ?? 1800} canDecide={canDecide} onDecided={() => setDecided((d) => [e.id, ...d])} />
        ))}
      </div>
    </>
  );
}

function ApprovalCard({ exec, ttl, canDecide, onDecided }: { exec: Execution; ttl: number; canDecide: boolean; onDecided: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const left = useCountdown(exec.requested_at, ttl);
  const [comment, setComment] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const params = Object.entries(JSON.parse(exec.params || "{}") as Record<string, unknown>);

  const decide = async (decision: "approve" | "reject", confirmSlug = "") => {
    setBusy(true); setError("");
    try {
      await api(`/executions/${exec.id}/decision`, { method: "POST", body: { decision, comment, confirm_slug: confirmSlug } });
      await qc.invalidateQueries({ queryKey: ["/executions?status=pending_approval"] });
      toast(decision === "approve" ? t("approvals.approvedToast") : t("approvals.rejectedToast"));
      setConfirm(false);
      onDecided();
    } catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const reject = () => { if (!comment.trim()) { setError(t("approvals.commentRequired")); return; } void decide("reject"); };

  return (
    <Card className="flex flex-col gap-4 !p-4 sm:!p-6" >
      <div className="flex flex-wrap items-center gap-2">
        <span className="inline-flex items-center gap-1 font-medium"><Bot aria-hidden size={16} />{exec.key_name || t("approvals.admin")}</span>
        <span className="text-muted">→</span><code className="font-mono">{exec.server_slug}</code>
        <RiskBadge risk={exec.risk} />
        <span className={`ml-auto tabular-nums ${left < 120 ? "text-destructive" : "text-muted"}`} aria-label={t("approvals.expiresIn", { time: fmtCountdown(left) })}><Clock aria-hidden size={14} className="mr-1 inline align-[-2px]" />{fmtCountdown(left)}</span>
      </div>
      <blockquote className="border-l-4 border-primary pl-3 text-[16px] font-medium">{exec.reason}</blockquote>
      <div>
        <p className="mb-1 text-muted">{t("approvals.command")} <code className="font-mono">{exec.command_slug}</code></p>
        {exec.rendered && <CodeBlock copy>{exec.rendered}</CodeBlock>}
      </div>
      {params.length > 0 && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
          {params.map(([k, v]) => <Fragment key={k}><dt className="font-mono text-muted">{k}</dt><dd className="font-mono">{String(v)}</dd></Fragment>)}
        </dl>
      )}
      <p className="text-muted">{t("approvals.rule", { rule: exec.rule_name || "—" })}</p>
      {canDecide && (
        <>
          <Textarea id={`c-${exec.id}`} label={t("approvals.comment")} value={comment} onChange={(e) => setComment(e.target.value)} hint={t("approvals.commentHint")} />
          {error && <ErrorBox message={error} title={t("errors.details")} />}
          <div className="flex flex-col gap-2 sm:flex-row sm:justify-end">
            <Button variant="secondary" className="h-11 sm:h-9" disabled={busy} onClick={reject}><X aria-hidden size={16} />{t("approvals.reject")}</Button>
            <Button className="h-11 sm:h-9" disabled={busy || left === 0} onClick={() => (exec.risk === "destructive" ? setConfirm(true) : void decide("approve"))}><Check aria-hidden size={16} />{t("approvals.approve")}</Button>
          </div>
          <ConfirmDialog open={confirm} onClose={() => setConfirm(false)} title={t("approvals.destructiveTitle")} body={t("approvals.destructiveBody", { server: exec.server_slug })}
            confirmWord={exec.server_slug} busy={busy} onConfirm={() => void decide("approve", exec.server_slug)} />
        </>
      )}
    </Card>
  );
}

// Après décision, la carte se replie avec le résultat (« approuvée — en cours… → réussie »).
function DecidedCard({ id }: { id: string }) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["exec", id], queryFn: () => api<Execution>(`/executions/${id}`),
    refetchInterval: (query) => (query.state.data && TERMINAL.includes(query.state.data.status) ? false : 1000),
  });
  const e = q.data;
  return (
    <Card className="flex flex-wrap items-center gap-3 !p-4">
      {e ? <><StatusBadge status={e.status} /><code className="font-mono">{e.command_slug}@{e.server_slug}</code>
        <span className="text-muted">{t("approvals.decidedBy", { name: e.decided_by_name })}</span>
        {e.exit_code !== null && <span className="text-muted">exit {e.exit_code}</span>}</> : <span className="text-muted">{t("errors.loading")}</span>}
    </Card>
  );
}
