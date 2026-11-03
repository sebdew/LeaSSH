import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Card } from "./ui";
import { api } from "../lib/api";
import { fmtMs, fmtSeconds } from "../lib/format";
import { currentLocale, percent } from "../lib/lang";
import type { HourBucket, Insights, TopItem } from "../lib/types";

type Period = 7 | 30 | 90;

interface Day { key: string; date: Date; ok: number; ko: number; denied: number; other: number }

const pad = (n: number) => String(n).padStart(2, "0");
const localKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
// Les heures du serveur sont en UTC; on les regroupe dans le fuseau de l'utilisateur.
const hourDate = (h: string) => new Date(`${h}:00:00Z`);

export function bucketByDay(hourly: HourBucket[], days: number, now = new Date()): Day[] {
  const out: Day[] = [];
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i);
    out.push({ key: localKey(d), date: d, ok: 0, ko: 0, denied: 0, other: 0 });
  }
  const idx = new Map(out.map((d) => [d.key, d]));
  for (const b of hourly) {
    const d = idx.get(localKey(hourDate(b.hour)));
    if (d) { d.ok += b.ok; d.ko += b.ko; d.denied += b.denied; d.other += b.other; }
  }
  return out;
}

// Heatmap jour de la semaine (lundi = 0) × heure, dans le fuseau local.
export function heatmap(hourly: HourBucket[]): number[][] {
  const grid = Array.from({ length: 7 }, () => Array<number>(24).fill(0));
  for (const b of hourly) {
    const d = hourDate(b.hour);
    grid[(d.getDay() + 6) % 7][d.getHours()] += b.ok + b.ko + b.denied + b.other;
  }
  return grid;
}

const dayLabel = (d: Date, long = false) => d.toLocaleDateString(currentLocale(), long ? { weekday: "long", day: "numeric", month: "long" } : { day: "numeric", month: "short" });
const WEEKDAYS = ["lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche"];

export function InsightsSection() {
  const { t } = useTranslation();
  const [days, setDays] = useState<Period>(30);
  const q = useQuery({ queryKey: ["/dashboard/insights", days], queryFn: () => api<Insights>(`/dashboard/insights?days=${days}`), refetchInterval: 60000 });
  const d = q.data;
  return (
    <section aria-labelledby="insights-title" className="mb-6">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h2 id="insights-title" className="text-[18px] font-semibold">{t("insights.title")}</h2>
        <div role="group" aria-label={t("insights.period")} className="inline-flex overflow-hidden rounded-field border border-border-strong">
          {([7, 30, 90] as Period[]).map((p) => (
            <button key={p} type="button" aria-pressed={days === p} onClick={() => setDays(p)}
              className={`h-8 px-3 text-[13px] ${days === p ? "bg-primary font-medium text-primary-fg" : "bg-surface text-muted hover:bg-surface-2"}`}>{t("insights.lastDays", { count: p })}</button>
          ))}
        </div>
      </div>
      {q.isPending && <p className="text-muted">{t("errors.loading")}</p>}
      {q.isError && <p role="alert" className="text-destructive">{t("errors.generic")}</p>}
      {d && (d.requests === 0 ? <p className="rounded-card border border-dashed border-border p-6 text-center text-muted">{t("insights.empty", { count: days })}</p> : <InsightsBody d={d} />)}
    </section>
  );
}

function Kpi({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: string }) {
  return (
    <Card className="!p-4">
      <h3 className="text-muted">{label}</h3>
      <p className={`my-1 text-[26px] font-semibold tabular-nums ${tone ?? ""}`}>{value}</p>
      {sub && <p className="text-[13px] text-muted">{sub}</p>}
    </Card>
  );
}

function InsightsBody({ d }: { d: Insights }) {
  const { t } = useTranslation();
  const finished = d.succeeded + d.failed;
  const rate = finished > 0 ? Math.round((d.succeeded / finished) * 1000) / 10 : null;
  const refusal = d.requests > 0 ? Math.round((d.denied / d.requests) * 100) : 0;
  return (
    <>
      <div className="mb-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <Kpi label={t("insights.kpi.requests")} value={d.requests.toLocaleString(currentLocale())} sub={t("insights.kpi.perDay", { value: (Math.round((d.requests / d.days) * 10) / 10).toLocaleString(currentLocale()) })} />
        <Kpi label={t("insights.kpi.success")} value={rate == null ? "—" : percent(rate)} tone={rate == null ? "" : rate >= 95 ? "text-read" : rate >= 80 ? "text-write" : "text-destructive"}
          sub={t("insights.kpi.successSub", { ok: d.succeeded, ko: d.failed })} />
        <Kpi label={t("insights.kpi.avgDuration")} value={d.executed ? fmtMs(d.avg_ms) : "—"} sub={d.executed ? t("insights.kpi.durationSub", { p50: fmtMs(d.p50_ms), p95: fmtMs(d.p95_ms) }) : undefined} />
        <Kpi label={t("insights.kpi.denied")} value={String(d.denied)} sub={t("insights.kpi.deniedSub", { percent: refusal })} />
        <Kpi label={t("insights.kpi.approval")} value={d.approvals ? fmtSeconds(d.avg_approval_seconds) : "—"} sub={t("insights.kpi.approvalSub", { count: d.approvals })} />
      </div>

      <div className="mb-4 grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Card className="!p-4"><h3 className="mb-3 font-semibold">{t("insights.daily")}</h3><DailyChart hourly={d.hourly} days={d.days} /></Card>
        <Card className="!p-4"><h3 className="mb-3 font-semibold">{t("insights.rhythm")}</h3><Rhythm hourly={d.hourly} /></Card>
      </div>

      <div className="mb-4 grid gap-4 lg:grid-cols-3">
        <TopCard title={t("insights.topCommands")} items={d.top_commands} detail={(i) => t("insights.onServers", { count: i.servers ?? 0 })} />
        <TopCard title={t("insights.topServers")} items={d.top_servers} detail={(i) => t("insights.commandsUsed", { count: i.servers ?? 0 })} />
        <TopCard title={t("insights.topKeys")} items={d.top_keys} detail={(i) => t("insights.commandsUsed", { count: i.servers ?? 0 })} />
      </div>

      <div className="mb-2 grid gap-4 lg:grid-cols-3">
        <Card className="!p-4"><h3 className="mb-3 font-semibold">{t("insights.riskTitle")}</h3><RiskSplit risk={d.risk} /></Card>
        <Card className="!p-4">
          <h3 className="mb-3 font-semibold">{t("insights.denialTitle")}</h3>
          {d.denial_reasons.length === 0 ? <p className="text-read">✔ {t("insights.noDenial")}</p> : (
            <ul className="flex flex-col gap-1.5">{d.denial_reasons.map((r) => (
              <li key={r.name} className="flex items-baseline justify-between gap-3"><span>{t(`insights.denial.${r.name}`, { defaultValue: r.name })}</span><span className="tabular-nums text-muted">{r.count}</span></li>
            ))}</ul>
          )}
        </Card>
        <Card className="!p-4">
          <h3 className="mb-3 font-semibold">{t("insights.slowestTitle")}</h3>
          {d.slowest_commands.length === 0 ? <p className="text-muted">{t("insights.noSlow")}</p> : (
            <ul className="flex flex-col gap-1.5">{d.slowest_commands.map((r) => (
              <li key={r.name} className="flex items-baseline justify-between gap-3"><code className="min-w-0 truncate font-mono">{r.name}</code><span className="whitespace-nowrap tabular-nums text-muted">{fmtMs(r.avg_ms)} · {t("insights.times", { count: r.count })}</span></li>
            ))}</ul>
          )}
        </Card>
      </div>
    </>
  );
}

const SEGMENTS = [
  { k: "ok", cls: "bg-read", label: "insights.legend.ok", icon: "✔" },
  { k: "ko", cls: "bg-destructive", label: "insights.legend.ko", icon: "✖" },
  { k: "denied", cls: "bg-write", label: "insights.legend.denied", icon: "⊘" },
  { k: "other", cls: "bg-border-strong", label: "insights.legend.other", icon: "…" },
] as const;

// Barres empilées par jour (HTML/CSS: net à toutes les largeurs). Succès, échecs, refus; chaque segment a un libellé et un tableau équivalent.
function DailyChart({ hourly, days }: { hourly: HourBucket[]; days: number }) {
  const { t } = useTranslation();
  const list = bucketByDay(hourly, days);
  const total = (x: Day) => x.ok + x.ko + x.denied + x.other;
  const max = Math.max(1, ...list.map(total));
  const [hover, setHover] = useState<Day | null>(null);
  const shown = hover ?? list[list.length - 1];
  return (
    <div>
      <div className="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-[13px]" aria-label={t("insights.legendLabel")}>
        {SEGMENTS.map((s) => <span key={s.k} className="inline-flex items-center gap-1.5 text-muted"><span aria-hidden className={`h-2.5 w-2.5 rounded-sm ${s.cls}`} />{s.icon} {t(s.label)}</span>)}
      </div>
      <div className="relative">
        <span className="absolute -top-0.5 left-0 text-[12px] tabular-nums text-muted">{max}</span>
        <div role="img" aria-label={t("insights.dailyAria", { count: list.reduce((n, x) => n + total(x), 0), days })} className="flex h-40 items-end gap-px border-b border-border pt-4" onMouseLeave={() => setHover(null)}>
          {list.map((x) => (
            <div key={x.key} onMouseEnter={() => setHover(x)} className="group flex h-full min-w-0 flex-1 flex-col justify-end" title={`${dayLabel(x.date, true)} — ${total(x)}`}>
              <div className="flex flex-col-reverse gap-0.5 group-hover:opacity-80" style={{ height: `${(total(x) / max) * 100}%` }}>
                {SEGMENTS.map((s) => x[s.k] > 0 && <div key={s.k} className={`${s.cls} first:rounded-b-none last:rounded-t-[3px]`} style={{ flexGrow: x[s.k], minHeight: 2 }} />)}
              </div>
            </div>
          ))}
        </div>
        <div className="mt-1 flex justify-between text-[12px] text-muted"><span>{dayLabel(list[0].date)}</span><span>{dayLabel(list[Math.floor(list.length / 2)].date)}</span><span>{dayLabel(list[list.length - 1].date)}</span></div>
      </div>
      <p className="mt-2 min-h-5 text-[13px]" aria-live="polite">
        <span className="font-medium first-letter:uppercase">{dayLabel(shown.date, true)}</span> — {total(shown)} {t("insights.requestsWord")}
        {" "}(✔ {shown.ok} · ✖ {shown.ko} · ⊘ {shown.denied})
      </p>
      <details className="mt-2 text-[13px]">
        <summary className="cursor-pointer text-muted">{t("insights.asTable")}</summary>
        <div className="mt-2 max-h-48 overflow-auto">
          <table className="w-full text-left"><thead className="text-muted"><tr><th className="pr-3 font-medium">{t("insights.col.day")}</th>{SEGMENTS.map((s) => <th key={s.k} className="pr-3 font-medium">{t(s.label)}</th>)}</tr></thead>
            <tbody>{[...list].reverse().map((x) => <tr key={x.key}><td className="pr-3">{dayLabel(x.date)}</td>{SEGMENTS.map((s) => <td key={s.k} className="pr-3 tabular-nums">{x[s.k]}</td>)}</tr>)}</tbody></table>
        </div>
      </details>
    </div>
  );
}

// Heatmap semaine × heure: une seule teinte (primaire), l'intensité = le volume.
function Rhythm({ hourly }: { hourly: HourBucket[] }) {
  const { t } = useTranslation();
  const grid = heatmap(hourly);
  const max = Math.max(1, ...grid.flat());
  let peak = { d: 0, h: 0, n: 0 };
  grid.forEach((row, d) => row.forEach((n, h) => { if (n > peak.n) peak = { d, h, n }; }));
  return (
    <div>
      <div role="img" aria-label={peak.n > 0 ? t("insights.peakAria", { day: WEEKDAYS[peak.d], hour: peak.h, count: peak.n }) : t("insights.rhythm")}
        className="grid gap-px" style={{ gridTemplateColumns: "2rem repeat(24, minmax(0, 1fr))" }}>
        <span />
        {Array.from({ length: 24 }, (_, h) => <span key={h} className="text-center text-[10px] text-muted">{h % 6 === 0 ? h : ""}</span>)}
        {grid.map((row, d) => (
          <div key={d} className="contents">
            <span className="pr-1 text-right text-[11px] leading-5 text-muted">{WEEKDAYS[d].slice(0, 3)}</span>
            {row.map((n, h) => (
              <span key={h} title={`${WEEKDAYS[d]} ${h} h — ${n}`} className="h-5 rounded-[3px]"
                style={{ backgroundColor: n === 0 ? "var(--surface-2)" : `color-mix(in srgb, var(--primary) ${Math.round(18 + (n / max) * 82)}%, transparent)` }} />
            ))}
          </div>
        ))}
      </div>
      <div className="mt-3 flex items-center justify-between gap-3 text-[13px] text-muted">
        <span>{peak.n > 0 ? t("insights.peak", { day: WEEKDAYS[peak.d], hour: peak.h, count: peak.n }) : ""}</span>
        <span className="inline-flex items-center gap-1.5">{t("insights.less")}
          {[0, 25, 50, 75, 100].map((p) => <span key={p} aria-hidden className="h-3 w-3 rounded-[3px]" style={{ backgroundColor: p === 0 ? "var(--surface-2)" : `color-mix(in srgb, var(--primary) ${p}%, transparent)` }} />)}
          {t("insights.more")}</span>
      </div>
    </div>
  );
}

// Classement: barres horizontales d'une seule teinte, longueur = volume relatif.
function TopCard({ title, items, detail }: { title: string; items: TopItem[]; detail: (i: TopItem) => string }) {
  const { t } = useTranslation();
  const max = Math.max(1, ...items.map((i) => i.count));
  return (
    <Card className="!p-4">
      <h3 className="mb-3 font-semibold">{title}</h3>
      {items.length === 0 ? <p className="text-muted">—</p> : (
        <ol className="flex flex-col gap-2.5">
          {items.map((i, n) => (
            <li key={i.name} data-testid="top-row">
              <div className="flex items-baseline gap-2">
                <span className="w-5 shrink-0 text-right text-[12px] tabular-nums text-muted">{n + 1}</span>
                <code className="min-w-0 flex-1 truncate font-mono" title={i.name}>{i.name}</code>
                <span className="font-semibold tabular-nums">{i.count}</span>
              </div>
              <div className="ml-7 mt-1 h-1.5 overflow-hidden rounded-full bg-surface-2"><div className="h-full rounded-full bg-primary" style={{ width: `${(i.count / max) * 100}%` }} /></div>
              <p className="ml-7 mt-0.5 text-[12px] text-muted">
                {i.failed > 0 ? <span className="text-destructive">✖ {t("insights.failures", { count: i.failed })}</span> : <span>✔ {t("insights.noFailure")}</span>}
                {i.avg_ms > 0 && ` · ${t("insights.avg", { value: fmtMs(i.avg_ms) })}`}{(i.servers ?? 0) > 0 && ` · ${detail(i)}`}
              </p>
            </li>
          ))}
        </ol>
      )}
    </Card>
  );
}

function RiskSplit({ risk }: { risk: Insights["risk"] }) {
  const { t } = useTranslation();
  const total = risk.read + risk.write + risk.destructive;
  const parts = [{ k: "read", cls: "bg-read" }, { k: "write", cls: "bg-write" }, { k: "destructive", cls: "bg-destructive" }] as const;
  if (total === 0) return <p className="text-muted">—</p>;
  return (
    <div>
      <div className="flex h-3 gap-0.5 overflow-hidden rounded-full" role="img" aria-label={t("insights.riskAria", { read: risk.read, write: risk.write, destructive: risk.destructive })}>
        {parts.map((p) => risk[p.k] > 0 && <div key={p.k} className={p.cls} style={{ flexGrow: risk[p.k] }} />)}
      </div>
      <ul className="mt-3 flex flex-col gap-1">
        {parts.map((p) => (
          <li key={p.k} className="flex items-center justify-between gap-3"><span className="inline-flex items-center gap-2"><span aria-hidden className={`h-2.5 w-2.5 rounded-sm ${p.cls}`} />{t(`risk.${p.k}`)}</span>
            <span className="tabular-nums text-muted">{risk[p.k]} · {percent(Math.round((risk[p.k] / total) * 100))}</span></li>
        ))}
      </ul>
    </div>
  );
}
