import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Bot, X } from "lucide-react";
import { Badge, CodeBlock, CopyButton, RiskBadge, StatusBadge, Tabs, when } from "./kit";
import { api } from "../lib/api";
import { diffRows } from "../lib/journal";
import { fmtDate, fmtDuration, initials } from "../lib/format";
import type { AuditEvent, Execution } from "../lib/types";

// Avatar d'acteur: agent (robot + nom de clé) vs humain (initiales).
export function Actor({ exec }: { exec: Pick<Execution, "key_name" | "decided_by_name" | "actor_user_id"> }) {
  const { t } = useTranslation();
  if (exec.key_name) return <span className="inline-flex items-center gap-1.5"><Bot aria-hidden size={14} className="text-muted" /><span>{exec.key_name}</span></span>;
  return <HumanAvatar name={exec.decided_by_name || t("journal.user")} />;
}

export function HumanAvatar({ name }: { name: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden className="grid h-5 w-5 place-items-center rounded-full bg-primary/15 text-[10px] font-semibold text-primary">{initials(name)}</span>
      <span>{name}</span>
    </span>
  );
}

function Drawer({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const { t } = useTranslation();
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [onClose]);
  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/40" onClick={onClose} aria-hidden />
      <aside role="dialog" aria-modal="true" aria-label={title} className="fixed inset-y-0 right-0 z-50 flex w-full max-w-[560px] flex-col overflow-y-auto border-l border-border bg-surface shadow-[var(--shadow)]">
        <div className="sticky top-0 flex items-center justify-between border-b border-border bg-surface px-5 py-3">
          <h2 className="text-[18px] font-semibold">{title}</h2>
          <button autoFocus onClick={onClose} aria-label={t("common.close")} className="grid h-8 w-8 place-items-center rounded-field text-muted hover:bg-surface-2"><X aria-hidden size={16} /></button>
        </div>
        <div className="flex flex-col gap-5 p-5">{children}</div>
      </aside>
    </>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return <section><h3 className="mb-2 text-[15px] font-semibold">{title}</h3>{children}</section>;
}

interface Step { label: string; at: string; detail?: string }

export function timeline(e: Execution, t: TFunction): Step[] {
  const who = e.key_name || e.decided_by_name || t("journal.user");
  const steps: Step[] = [{ label: t("journal.tl.requested", { who }), at: e.requested_at }];
  if (e.status === "denied") return [...steps, { label: t("journal.tl.denied"), at: e.finished_at || e.requested_at, detail: e.denial_reason }];
  if (e.key_id && (e.status === "pending_approval" || e.decided_at)) steps.push({ label: t("journal.tl.pending"), at: e.requested_at });
  if (e.decided_at && e.key_id) steps.push({ label: t(e.status === "rejected" ? "journal.tl.rejected" : "journal.tl.approved", { who: e.decided_by_name }), at: e.decided_at, detail: e.decision_comment });
  if (e.status === "expired") return [...steps, { label: t("journal.tl.expired"), at: e.finished_at }];
  if (e.started_at) steps.push({ label: t("journal.tl.started"), at: e.started_at });
  if (e.finished_at && e.status !== "rejected") steps.push({ label: t("journal.tl.finished", { code: e.exit_code ?? "—", duration: fmtDuration(e.duration_ms) }), at: e.finished_at, detail: e.status === "error" ? e.denial_reason : undefined });
  return steps;
}

function Output({ stdout, stderr, truncated }: { stdout: string; stderr: string; truncated: boolean }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"stdout" | "stderr">(stdout || !stderr ? "stdout" : "stderr");
  const [find, setFind] = useState("");
  const text = tab === "stdout" ? stdout : stderr;
  const lines = useMemo(() => text.replace(/\n$/, "").split("\n").map((l, i) => ({ n: i + 1, l })).filter((x) => !find || x.l.toLowerCase().includes(find.toLowerCase())), [text, find]);
  return (
    <div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "stdout", label: "stdout" }, { id: "stderr", label: "stderr" }]} />
      <div className="mb-2 flex items-center gap-2">
        <input aria-label={t("journal.searchOutput")} placeholder={t("journal.searchOutput")} value={find} onChange={(e) => setFind(e.target.value)} className="h-8 flex-1 rounded-field border border-border-strong bg-surface px-2" />
        <CopyButton text={text} />
        {truncated && <Badge tone="write">{t("journal.truncated")}</Badge>}
      </div>
      {text ? (
        <pre className="max-h-80 overflow-auto rounded-field bg-surface-2 p-2 font-mono text-[12px]">
          {lines.map((x) => <div key={x.n} className="flex gap-3"><span aria-hidden className="w-8 shrink-0 select-none text-right text-muted">{x.n}</span><span className="whitespace-pre-wrap break-all">{x.l}</span></div>)}
        </pre>
      ) : <p className="text-muted">{t("journal.noOutput")}</p>}
    </div>
  );
}

export function ExecDrawer({ id, onClose, onFilter }: { id: string; onClose: () => void; onFilter: (patch: Record<string, string>) => void }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["exec-detail", id], queryFn: () => api<Execution>(`/executions/${id}`), refetchInterval: (query) => (["running", "pending_approval"].includes(query.state.data?.status ?? "") ? 2000 : false) });
  const e = q.data;
  const params = useMemo(() => { try { return Object.entries(JSON.parse(e?.params || "{}") as Record<string, unknown>); } catch { return []; } }, [e?.params]);
  return (
    <Drawer title={e ? `${e.command_slug} @ ${e.server_slug}` : t("errors.loading")} onClose={onClose}>
      {q.isError && <p role="alert" className="text-destructive">{t("errors.generic")}</p>}
      {e && (
        <>
          <div className="flex flex-wrap items-center gap-2"><StatusBadge status={e.status} /><RiskBadge risk={e.risk} /><Actor exec={e} /></div>
          <Section title={t("journal.why")}><blockquote className="border-l-4 border-primary pl-3 text-[15px]">{e.reason || "—"}</blockquote></Section>
          <Section title={t("journal.renderedCommand")}>
            {e.rendered ? <CodeBlock copy>{e.rendered}</CodeBlock> : <p className="text-muted">{t("journal.notRendered")}</p>}
            {params.length > 0 && (
              <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                {params.map(([k, v]) => <div key={k} className="contents"><dt className="font-mono text-muted">{k}</dt><dd className="font-mono">{String(v)}</dd></div>)}
              </dl>
            )}
          </Section>
          <Section title={t("journal.timeline")}>
            <ol className="relative ml-2 flex flex-col gap-3 border-l border-border pl-4">
              {timeline(e, t as never).map((s, i) => (
                <li key={i}><span aria-hidden className="absolute -left-[5px] mt-1.5 h-2.5 w-2.5 rounded-full bg-primary" />
                  <div>{s.label}</div><div className="text-muted">{fmtDate(s.at)}{s.detail ? ` · « ${s.detail} »` : ""}</div></li>
              ))}
            </ol>
          </Section>
          {e.status !== "denied" && e.status !== "pending_approval" && <Section title={t("journal.output")}><Output stdout={e.stdout} stderr={e.stderr} truncated={e.output_truncated} /></Section>}
          <details className="rounded-field border border-border p-3">
            <summary className="cursor-pointer font-medium">{t("journal.metadata")}</summary>
            <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
              <dt className="text-muted">{t("journal.rule")}</dt><dd>{e.rule_name || "—"}</dd>
              <dt className="text-muted">{t("journal.version")}</dt><dd className="font-mono">v{e.command_version} · {e.command_hash.slice(0, 12)}</dd>
              <dt className="text-muted">IP</dt><dd className="font-mono">{e.client_ip || "—"}</dd>
              <dt className="text-muted">{t("journal.session")}</dt><dd className="font-mono">{e.mcp_session_id || "—"}</dd>
              <dt className="text-muted">User-Agent</dt><dd className="break-all">{e.user_agent || "—"}</dd>
              <dt className="text-muted">ID</dt><dd className="font-mono">{e.id}</dd>
            </dl>
          </details>
          <nav aria-label={t("journal.related")} className="flex flex-col gap-1">
            {e.key_id && <button className="text-left text-primary hover:underline" onClick={() => onFilter({ key_id: e.key_id })}>{t("journal.allByAgent")}</button>}
            <button className="text-left text-primary hover:underline" onClick={() => onFilter({ server: e.server_slug })}>{t("journal.allOnServer")}</button>
            <button className="text-left text-primary hover:underline" onClick={() => onFilter({ command: e.command_slug })}>{t("journal.allOfCommand")}</button>
          </nav>
        </>
      )}
    </Drawer>
  );
}

// Diff avant / après: +/− en plus de la couleur (jamais la couleur seule).
export function DiffView({ diff }: { diff: Record<string, unknown> }) {
  const { t } = useTranslation();
  const rows = diffRows(diff);
  if (rows.length === 0) return <p className="text-muted">{t("journal.noDiff")}</p>;
  const show = (v: unknown) => (typeof v === "string" ? v : JSON.stringify(v));
  return (
    <div role="table" aria-label={t("journal.diff")} className="overflow-hidden rounded-field border border-border font-mono text-[12px]">
      {rows.map((r) => (
        <div key={r.path} role="row" className="border-b border-border last:border-0">
          <div className="bg-surface-2 px-2 py-0.5 text-muted">{r.path}</div>
          {r.kind !== "added" && <div data-kind="removed" className="flex gap-2 bg-destructive/12 px-2 py-1"><span aria-label={t("journal.removed")}>−</span><span className="break-all">{show(r.before)}</span></div>}
          {r.kind !== "removed" && <div data-kind="added" className="flex gap-2 bg-read/12 px-2 py-1"><span aria-label={t("journal.added")}>+</span><span className="break-all">{show(r.after)}</span></div>}
        </div>
      ))}
    </div>
  );
}

export function AuditDrawer({ event, onClose }: { event: AuditEvent; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <Drawer title={`${event.action} · ${event.target_name || event.target_type}`} onClose={onClose}>
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone="primary">{t(`journal.actorType.${event.actor_type}`)}</Badge>
        <span>{event.actor_name || "—"}</span><span className="text-muted">{when(event.ts)}</span>
      </div>
      <Section title={t("journal.diff")}><DiffView diff={event.diff} /></Section>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
        <dt className="text-muted">{t("journal.target")}</dt><dd>{event.target_type} · {event.target_name || "—"}</dd>
        <dt className="text-muted">IP</dt><dd className="font-mono">{event.ip || "—"}</dd>
        <dt className="text-muted">ID</dt><dd className="font-mono">{event.id}</dd>
      </dl>
    </Drawer>
  );
}
