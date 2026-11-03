import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AlertTriangle, Bot, Check, Circle, ServerCrash } from "lucide-react";
import { Card } from "../components/ui";
import { Empty, PageHeader, RiskBadge, StatusBadge } from "../components/kit";
import { Actor } from "../components/journal";
import { InsightsSection } from "../components/insights";
import { api } from "../lib/api";
import { fmtDate } from "../lib/format";
import type { DashboardData } from "../lib/types";

const linkBtn = "inline-flex h-9 items-center rounded-field px-4 font-medium";

export function Dashboard() {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["/dashboard"], queryFn: () => api<DashboardData>("/dashboard"), refetchInterval: 15000 });
  const d = q.data;
  if (q.isPending) return <><PageHeader title={t("dashboard.title")} /><p className="text-muted">{t("errors.loading")}</p></>;
  if (!d) return <p role="alert" className="text-destructive">{t("errors.generic")}</p>;
  const s = d.stats;
  const done = d.checklist.filter((c) => c.done).length;
  const ex = s.executions_24h;
  const total = ex.succeeded + ex.failed + ex.denied + ex.pending + ex.other;

  return (
    <>
      <PageHeader title={t("dashboard.title")} subtitle={t("dashboard.subtitle")} />
      {s.pending_approvals > 0 && (
        <div role="status" className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-card border border-write bg-write/10 p-4">
          <p className="inline-flex items-center gap-2 font-medium"><AlertTriangle aria-hidden size={18} className="text-write" />{t("dashboard.pendingBanner", { count: s.pending_approvals })}</p>
          <Link to="/validations" className={`${linkBtn} bg-primary text-primary-fg`}>{t("dashboard.review")}</Link>
        </div>
      )}
      {done < d.checklist.length && (
        <Card className="mb-4">
          <div className="mb-3 flex items-center justify-between"><h2 className="text-[18px] font-semibold">{t("dashboard.checklist")}</h2><span className="text-muted" aria-label={t("dashboard.progress", { done, total: d.checklist.length })}>{done}/{d.checklist.length}</span></div>
          <ol className="grid gap-1">
            {d.checklist.map((c, i) => (
              <li key={c.id}>
                <Link to={c.href} className="flex items-center gap-3 rounded-field px-2 py-2 hover:bg-surface-2" aria-label={`${t(`dashboard.steps.${c.id}`)} — ${c.done ? t("dashboard.stepDone") : t("dashboard.stepTodo")}`}>
                  {c.done ? <Check aria-hidden size={18} className="text-read" /> : <Circle aria-hidden size={18} className="text-muted" />}
                  <span className={c.done ? "text-muted line-through" : "font-medium"}>{i + 1}. {t(`dashboard.steps.${c.id}`)}</span>
                  {!c.done && <span className="ml-auto text-muted">{t(`dashboard.hints.${c.id}`)}</span>}
                </Link>
              </li>
            ))}
          </ol>
        </Card>
      )}

      <div className="mb-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Card className="!p-4">
          <h2 className="text-muted">{t("dashboard.executions24h")}</h2>
          <p className="my-1 text-[28px] font-semibold tabular-nums">{total}</p>
          <p className="flex flex-wrap gap-x-3 text-[13px]">
            <span className="text-read">✔ {ex.succeeded} {t("dashboard.ok")}</span>
            <span className="text-destructive">✖ {ex.failed} {t("dashboard.ko")}</span>
            <span className="text-muted">⊘ {ex.denied} {t("dashboard.refused")}</span>
          </p>
        </Card>
        <Card className="!p-4">
          <h2 className="text-muted">{t("dashboard.activeAgents")}</h2>
          <p className="my-1 inline-flex items-center gap-2 text-[28px] font-semibold tabular-nums"><Bot aria-hidden size={22} className="text-muted" />{s.active_agents_24h}</p>
        </Card>
        <Card className="!p-4">
          <h2 className="text-muted">{t("dashboard.anomalies")}</h2>
          {s.servers_in_anomaly.length === 0 ? <p className="my-1 text-read">✔ {t("dashboard.noAnomaly")}</p> : (
            <ul className="my-1 flex flex-col gap-1">
              {s.servers_in_anomaly.map((a) => (
                <li key={a.slug}><Link to="/serveurs" className="inline-flex items-center gap-1.5 text-destructive hover:underline"><ServerCrash aria-hidden size={14} /><code className="whitespace-nowrap font-mono">{a.slug}</code> — {t(`dashboard.anomaly.${a.reason}`)}</Link></li>
              ))}
            </ul>
          )}
        </Card>
        <Card className="!p-4">
          <h2 className="text-muted">{t("dashboard.proposals")}</h2>
          <p className="my-1 text-[28px] font-semibold tabular-nums">{s.pending_proposals}</p>
          {s.pending_proposals > 0 && <Link to="/propositions" className="text-primary hover:underline">{t("dashboard.reviewProposals")}</Link>}
        </Card>
      </div>

      <InsightsSection />

      <h2 className="mb-3 text-[18px] font-semibold">{t("dashboard.recent")}</h2>
      {d.recent.length === 0 ? <Empty text={t("dashboard.noActivity")} /> : (
        <ul className="divide-y divide-border rounded-card border border-border bg-surface">
          {d.recent.map((r) => (
            <li key={r.id}>
              <Link to="/journal" search={{ exec: r.id } as never} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 hover:bg-surface-2">
                <span className="w-32 shrink-0 whitespace-nowrap tabular-nums text-muted">{fmtDate(r.requested_at)}</span>
                <Actor exec={r} />
                <code className="font-mono">{r.command_slug}</code><span className="text-muted">→</span><code className="font-mono">{r.server_slug}</code>
                <span className="ml-auto inline-flex items-center gap-2"><RiskBadge risk={r.risk} /><StatusBadge status={r.status} /></span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-3"><Link to="/journal" className="text-primary hover:underline">{t("dashboard.openJournal")}</Link></p>
    </>
  );
}
