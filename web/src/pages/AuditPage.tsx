import { useRef, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, ExternalLink, FileCheck2, FileUp, Sparkles } from "lucide-react";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, Empty, PageHeader, when } from "../components/kit";
import { toast } from "../components/Toaster";
import { api, getCsrf } from "../lib/api";
import { currentLang, currentLocale } from "../lib/lang";
import { errorMessage, useApiMutation, useList } from "../lib/queries";
import type { AuditAIConfig, AuditReportMeta, VerifyResult } from "../lib/types";

const iso = (d: Date) => d.toISOString().slice(0, 10);
const daysAgo = (n: number) => { const d = new Date(); d.setUTCDate(d.getUTCDate() - n); return iso(d); };

// Périodes proposées (UTC, bornes incluses).
export const presets = (now = new Date()) => {
  const y = now.getUTCFullYear(), m = now.getUTCMonth();
  return [
    { id: "d30", from: daysAgo(29), to: iso(now) },
    { id: "d90", from: daysAgo(89), to: iso(now) },
    { id: "d365", from: daysAgo(364), to: iso(now) },
    { id: "lastMonth", from: iso(new Date(Date.UTC(y, m - 1, 1))), to: iso(new Date(Date.UTC(y, m, 0))) },
    { id: "year", from: iso(new Date(Date.UTC(y, 0, 1))), to: iso(now) },
  ];
};

export function AuditPage() {
  const { t } = useTranslation();
  const reports = useList<AuditReportMeta>("/audit-reports");
  const ai = useQuery({ queryKey: ["/settings/audit-ai"], queryFn: () => api<{ config: AuditAIConfig }>("/settings/audit-ai") });
  const aiOn = ai.data?.config.enabled ?? false;
  return (
    <>
      <PageHeader title={t("audit.title")} subtitle={t("audit.subtitle")} />
      <p role="note" className="mb-4 rounded-card border border-border border-l-4 border-l-write bg-surface p-4">{t("audit.disclaimer")}</p>
      <GenerateCard />
      <History reports={reports.data ?? []} loading={reports.isPending} aiOn={aiOn} />
      <div className="mt-6 grid gap-4 lg:grid-cols-2">
        <VerifyCard />
        <AICard config={ai.data?.config} />
      </div>
    </>
  );
}

function GenerateCard() {
  const { t } = useTranslation();
  const p = presets();
  const [from, setFrom] = useState(p[0].from);
  const [to, setTo] = useState(p[0].to);
  const create = useApiMutation<{ from: string; to: string }, AuditReportMeta>("POST", "/audit-reports", ["/audit-reports"]);
  return (
    <Card className="mb-4 flex flex-col gap-3">
      <h2 className="text-[18px] font-semibold">{t("audit.generate")}</h2>
      <p className="text-muted">{t("audit.generateHelp")}</p>
      <div className="flex flex-wrap gap-1.5" role="group" aria-label={t("audit.presets")}>
        {p.map((x) => (
          <button key={x.id} type="button" aria-pressed={from === x.from && to === x.to} onClick={() => { setFrom(x.from); setTo(x.to); }}
            className={`h-8 rounded-full border px-3 text-[13px] ${from === x.from && to === x.to ? "border-primary bg-primary/10 font-medium text-primary" : "border-border bg-surface text-muted hover:bg-surface-2"}`}>{t(`audit.preset.${x.id}`)}</button>
        ))}
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <Field id="audit-from" label={t("audit.from")} type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} />
        <Field id="audit-to" label={t("audit.to")} type="date" value={to} min={from} max={iso(new Date())} onChange={(e) => setTo(e.target.value)} />
        <Button disabled={create.isPending || !from || !to || from > to}
          onClick={() => create.mutate({ from, to }, { onSuccess: () => toast(t("audit.generated")) })}>
          <FileCheck2 aria-hidden size={16} />{create.isPending ? t("audit.generating") : t("audit.generateButton")}
        </Button>
      </div>
      {create.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(create.error)} title={t("errors.details")} />}
    </Card>
  );
}

const day = (d: string) => new Date(`${d}T00:00:00Z`).toLocaleDateString(currentLocale(), { timeZone: "UTC" });

function History({ reports, loading, aiOn }: { reports: AuditReportMeta[]; loading: boolean; aiOn: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const summarize = async (r: AuditReportMeta) => {
    setBusy(r.id); setErr("");
    try { await api(`/audit-reports/${r.id}/summary`, { method: "POST", body: { lang: currentLang() } }); await qc.invalidateQueries({ queryKey: ["/audit-reports"] }); toast(t("audit.summaryDone")); }
    catch (e) { setErr(errorMessage(e)); } finally { setBusy(""); }
  };
  return (
    <section aria-labelledby="audit-history">
      <h2 id="audit-history" className="mb-3 text-[18px] font-semibold">{t("audit.history")}</h2>
      {err && <div className="mb-3"><ErrorBox message={t("audit.summaryFailed")} detail={err} title={t("errors.details")} /></div>}
      {!loading && reports.length === 0 ? <Empty text={t("audit.empty")} /> : (
        <div className="overflow-x-auto rounded-card border border-border bg-surface">
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-muted"><tr>{["date", "period", "by", "findings", "fingerprint", ""].map((c, i) => <th key={i} scope="col" className="px-4 py-2 font-medium">{c && t(`audit.col.${c}`)}</th>)}</tr></thead>
            <tbody>
              {reports.map((r) => (
                <tr key={r.id} className="border-b border-border align-top last:border-0">
                  <td className="whitespace-nowrap px-4 py-2 tabular-nums">{when(r.created_at)}<div className="text-muted">v{r.leassh_version}</div></td>
                  <td className="whitespace-nowrap px-4 py-2">{day(r.period_from)} → {day(r.period_to)}</td>
                  <td className="px-4 py-2">{r.created_by_name || "—"}</td>
                  <td className="px-4 py-2">
                    <span className="flex flex-wrap gap-1" aria-label={t("audit.findingsAria", { high: r.findings_high, medium: r.findings_medium, low: r.findings_low })}>
                      <Badge tone={r.findings_high ? "destructive" : "muted"}>{r.findings_high} {t("audit.sev.high")}</Badge>
                      <Badge tone={r.findings_medium ? "write" : "muted"}>{r.findings_medium} {t("audit.sev.medium")}</Badge>
                      <Badge tone="muted">{r.findings_low} {t("audit.sev.low")}</Badge>
                    </span>
                    {r.has_summary && <p className="mt-1 inline-flex items-center gap-1 text-muted"><Sparkles aria-hidden size={12} />{t("audit.hasSummary", { model: r.summary_model })}</p>}
                  </td>
                  <td className="px-4 py-2"><code className="font-mono text-muted" title={r.sha256}>{r.sha256.slice(0, 12)}…</code></td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap justify-end gap-1">
                      <a href={`/api/v1/audit-reports/${r.id}/html`} target="_blank" rel="noopener" className="inline-flex h-8 items-center gap-1.5 rounded-field bg-primary px-3 font-medium text-primary-fg"><ExternalLink aria-hidden size={14} />{t("audit.open")}</a>
                      <a href={`/api/v1/audit-reports/${r.id}/html?download=1`} download className="inline-flex h-8 items-center gap-1.5 rounded-field px-2 text-muted hover:bg-surface-2"><Download aria-hidden size={14} />HTML</a>
                      <a href={`/api/v1/audit-reports/${r.id}/json`} download className="inline-flex h-8 items-center gap-1.5 rounded-field px-2 text-muted hover:bg-surface-2"><Download aria-hidden size={14} />{t("audit.signedJson")}</a>
                      {aiOn && <Button variant="secondary" className="h-8 px-3" disabled={busy !== ""} onClick={() => void summarize(r)}><Sparkles aria-hidden size={14} />{busy === r.id ? t("audit.summarizing") : r.has_summary ? t("audit.resummarize") : t("audit.summarize")}</Button>}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="mt-2 text-[13px] text-muted">{t("audit.printHint")}</p>
    </section>
  );
}

const readText = (f: File): Promise<string> =>
  typeof f.text === "function" ? f.text() : new Promise((resolve, reject) => { const r = new FileReader(); r.onload = () => resolve(String(r.result)); r.onerror = () => reject(r.error); r.readAsText(f); });

function VerifyCard() {
  const { t } = useTranslation();
  const ref = useRef<HTMLInputElement>(null);
  const [res, setRes] = useState<VerifyResult | null>(null);
  const [err, setErr] = useState("");
  const [name, setName] = useState("");
  const check = async (f: File | undefined) => {
    if (!f) return;
    setRes(null); setErr(""); setName(f.name);
    try {
      const body = await readText(f);
      const r = await fetch("/api/v1/audit-reports/verify", { method: "POST", credentials: "same-origin", headers: { "X-CSRF-Token": getCsrf() }, body });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error?.message ?? r.statusText);
      setRes(data as VerifyResult);
    } catch (e) { setErr(errorMessage(e)); }
  };
  return (
    <Card className="flex flex-col gap-3">
      <h2 className="text-[18px] font-semibold">{t("audit.verifyTitle")}</h2>
      <p className="text-muted">{t("audit.verifyHelp")}</p>
      <input ref={ref} type="file" accept=".json,application/json" className="sr-only" aria-label={t("audit.verifyChoose")} onChange={(e) => void check(e.target.files?.[0])} />
      <div><Button variant="secondary" onClick={() => ref.current?.click()}><FileUp aria-hidden size={16} />{t("audit.verifyChoose")}</Button>{name && <span className="ml-3 text-muted">{name}</span>}</div>
      {err && <p role="alert" className="text-destructive">{err}</p>}
      {res && <p role="status" className={res.tag_ok ? "text-read" : "text-destructive"}>{res.tag_ok ? "✔" : "✖"} {res.message}</p>}
    </Card>
  );
}

function AICard({ config }: { config?: AuditAIConfig }) {
  const { t } = useTranslation();
  return <AIForm key={JSON.stringify(config ?? {})} config={config} t={t} />;
}

function AIForm({ config, t }: { config?: AuditAIConfig; t: TFunction }) {
  const [enabled, setEnabled] = useState(config?.enabled ?? false);
  const [url, setUrl] = useState(config?.url ?? "");
  const [model, setModel] = useState(config?.model ?? "");
  const [key, setKey] = useState("");
  const save = useApiMutation<{ enabled: boolean; url: string; model: string; api_key: string }>("PUT", "/settings/audit-ai", ["/settings/audit-ai"]);
  return (
    <Card className="flex flex-col gap-3">
      <h2 className="text-[18px] font-semibold">{t("audit.aiTitle")}</h2>
      <p className="text-muted">{t("audit.aiHelp")}</p>
      <p role="note" className="rounded-field border border-write bg-write/10 p-3">{t("audit.aiWarning")}</p>
      <Check2 id="ai-enabled" label={t("audit.aiEnable")} checked={enabled} onChange={setEnabled} />
      <Field id="ai-url" label={t("audit.aiUrl")} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://api.exemple.com/v1" autoComplete="off" />
      <Field id="ai-model" label={t("audit.aiModel")} value={model} onChange={(e) => setModel(e.target.value)} autoComplete="off" />
      <Field id="ai-key" label={t("audit.aiKey")} type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="new-password" placeholder={config?.has_key ? t("audit.aiKeyKept") : ""} />
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div><Button disabled={save.isPending} onClick={() => save.mutate({ enabled, url, model, api_key: key }, { onSuccess: () => { setKey(""); toast(t("common.saved")); } })}>{t("common.save")}</Button></div>
    </Card>
  );
}
