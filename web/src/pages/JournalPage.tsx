import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Pause, Play, Radio, X } from "lucide-react";
import { Button } from "../components/ui";
import { Empty, PageHeader, RiskBadge, Select, StatusBadge, Tabs } from "../components/kit";
import { Actor, AuditDrawer, ExecDrawer, HumanAvatar } from "../components/journal";
import { api } from "../lib/api";
import { fmtDate, fmtDuration } from "../lib/format";
import { activeFilters, toQuery, useLiveExecutions, type JournalSearch } from "../lib/journal";
import { useList } from "../lib/queries";
import type { AuditEvent, Execution, McpKey, SshServer, Command, UserAccount } from "../lib/types";

const STATUSES = ["succeeded", "failed", "denied", "pending_approval", "rejected", "expired", "timeout", "running", "error"] as const;
const PERIODS = ["1h", "24h", "7d"] as const;
const AUDIT_PREFIXES = ["user.", "server.", "server_group.", "command.", "command_group.", "identity.", "key.", "rule.", "execution.", "proposal.", "host_key.", "logs."];

interface Page<T> { items: T[]; next_cursor: string }

function useDebouncedInput(value: string, commit: (v: string) => void) {
  const [local, setLocal] = useState(value);
  const [seen, setSeen] = useState(value);
  if (value !== seen) { setSeen(value); setLocal(value); } // valeur de l'URL changée de l'extérieur
  const onCommit = useEffectEvent(commit);
  useEffect(() => {
    if (local === value) return;
    const h = setTimeout(() => onCommit(local), 300);
    return () => clearTimeout(h);
  }, [local, value]);
  return [local, setLocal] as const;
}

export function JournalView({ search, onSearch }: { search: JournalSearch; onSearch: (s: JournalSearch) => void }) {
  const { t } = useTranslation();
  const tab = search.tab ?? "exec";
  const [live, setLive] = useState(true);
  const setFilters = (patch: Partial<JournalSearch>) => onSearch({ ...search, ...patch, exec: undefined, event: undefined });
  const clear = () => onSearch({ tab: search.tab });
  const exportHref = (fmt: "csv" | "ndjson") => `/api/v1/${tab === "exec" ? "executions" : "audit"}/export?${toQuery(search, tab, { format: fmt })}`;

  return (
    <>
      <PageHeader title={t("journal.title")} subtitle={t("journal.subtitle")}
        action={<div className="flex flex-wrap items-center gap-2">
          {tab === "exec" && (
            <Button variant="secondary" onClick={() => setLive((l) => !l)} aria-pressed={live}>
              {live ? <Pause aria-hidden size={14} /> : <Play aria-hidden size={14} />}{live ? t("journal.pause") : t("journal.resume")}
            </Button>
          )}
          <a href={exportHref("csv")} download className="inline-flex h-9 items-center gap-2 rounded-field border border-border-strong bg-surface-2 px-3"><Download aria-hidden size={14} />CSV</a>
          <a href={exportHref("ndjson")} download className="inline-flex h-9 items-center gap-2 rounded-field border border-border-strong bg-surface-2 px-3"><Download aria-hidden size={14} />NDJSON</a>
        </div>} />
      <Tabs value={tab} onChange={(v) => onSearch({ tab: v })} tabs={[{ id: "exec", label: t("journal.tabExec") }, { id: "admin", label: t("journal.tabAdmin") }]} />
      <FilterBar tab={tab} search={search} setFilters={setFilters} clear={clear} />
      {tab === "exec"
        ? <ExecList search={search} live={live} onOpen={(id) => onSearch({ ...search, exec: id })} />
        : <AuditList search={search} onOpen={(id) => onSearch({ ...search, event: id || undefined })} />}
      {tab === "exec" && search.exec && <ExecDrawer id={search.exec} onClose={() => onSearch({ ...search, exec: undefined })} onFilter={(p) => onSearch({ ...search, ...p, exec: undefined })} />}
    </>
  );
}

function FilterBar({ tab, search, setFilters, clear }: { tab: "exec" | "admin"; search: JournalSearch; setFilters: (p: Partial<JournalSearch>) => void; clear: () => void }) {
  const { t } = useTranslation();
  const keys = useList<McpKey>("/keys");
  const servers = useList<SshServer>("/servers");
  const commands = useList<Command>("/commands");
  const users = useList<UserAccount>("/users"); // réservé aux admins: la liste reste vide pour les autres
  const searchRef = useRef<HTMLInputElement>(null);
  const [q, setQ] = useDebouncedInput(search.q ?? "", (v) => setFilters({ q: v || undefined }));
  const statuses = (search.status ?? "").split(",").filter(Boolean);
  const toggleStatus = (s: string) => setFilters({ status: (statuses.includes(s) ? statuses.filter((x) => x !== s) : [...statuses, s]).join(",") || undefined });
  const custom = !!(search.from || search.to);
  const active = activeFilters(search, tab);
  const [open, setOpen] = useState(false); // repli des filtres sur mobile

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (e.key === "/" && !/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)) { e.preventDefault(); searchRef.current?.focus(); }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  const label = (k: string, v: string) => {
    if (k === "key_id") return keys.data?.find((x) => x.id === v)?.name ?? v;
    if (k === "decided_by") return users.data?.find((x) => x.id === v)?.username ?? v;
    if (k === "status") return v.split(",").map((s) => t(`status.${s}`)).join(", ");
    if (k === "risk") return t(`risk.${v}`);
    return v;
  };

  return (
    <div className="mb-4 flex flex-col gap-3 rounded-card border border-border bg-surface p-3" role="search" aria-label={t("journal.filters")}>
      <button type="button" aria-expanded={open} aria-controls="journal-filters" onClick={() => setOpen((o) => !o)}
        className="flex h-9 items-center justify-between rounded-field border border-border-strong bg-surface px-3 font-medium md:hidden">
        <span>{t("journal.filtersToggle")}{active.length > 0 ? ` (${active.length})` : ""}</span><span aria-hidden>{open ? "▲" : "▼"}</span>
      </button>
      <div id="journal-filters" className={`${open ? "flex" : "hidden"} flex-col gap-3 md:flex`}>
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1.5">
          <span className="font-medium">{t("journal.period")}</span>
          <div role="group" className="inline-flex gap-1">
            {PERIODS.map((p) => (
              <button key={p} aria-pressed={search.period === p} onClick={() => setFilters({ period: search.period === p ? undefined : p, from: undefined, to: undefined })}
                className={`h-9 rounded-field border px-3 ${search.period === p ? "border-primary bg-primary/12 text-primary" : "border-border"}`}>{p}</button>
            ))}
            <button aria-pressed={custom} onClick={() => setFilters(custom ? { from: undefined, to: undefined } : { period: undefined, from: new Date(Date.now() - 86400000).toISOString() })}
              className={`h-9 rounded-field border px-3 ${custom ? "border-primary bg-primary/12 text-primary" : "border-border"}`}>{t("journal.custom")}</button>
          </div>
        </div>
        {custom && (
          <>
            <label className="flex flex-col gap-1.5 font-medium">{t("journal.from")}
              <input type="datetime-local" className="h-9 rounded-field border border-border-strong bg-surface px-2 font-normal" value={toLocal(search.from)} onChange={(e) => setFilters({ from: fromLocal(e.target.value) })} /></label>
            <label className="flex flex-col gap-1.5 font-medium">{t("journal.to")}
              <input type="datetime-local" className="h-9 rounded-field border border-border-strong bg-surface px-2 font-normal" value={toLocal(search.to)} onChange={(e) => setFilters({ to: fromLocal(e.target.value) })} /></label>
          </>
        )}
        {tab === "exec" ? (
          <>
            <Select id="f-agent" label={t("journal.agent")} value={search.key_id ?? ""} onChange={(e) => setFilters({ key_id: e.target.value || undefined })}>
              <option value="">{t("journal.all")}</option>{keys.data?.map((k) => <option key={k.id} value={k.id}>{k.name}</option>)}</Select>
            <Select id="f-server" label={t("journal.server")} value={search.server ?? ""} onChange={(e) => setFilters({ server: e.target.value || undefined })}>
              <option value="">{t("journal.all")}</option>{servers.data?.map((s) => <option key={s.id} value={s.slug}>{s.slug}</option>)}</Select>
            <Select id="f-command" label={t("journal.command")} value={search.command ?? ""} onChange={(e) => setFilters({ command: e.target.value || undefined })}>
              <option value="">{t("journal.all")}</option>{commands.data?.map((c) => <option key={c.id} value={c.slug}>{c.slug}</option>)}</Select>
            <Select id="f-risk" label={t("journal.risk")} value={search.risk ?? ""} onChange={(e) => setFilters({ risk: e.target.value || undefined })}>
              <option value="">{t("journal.all")}</option>{(["read", "write", "destructive"] as const).map((r) => <option key={r} value={r}>{t(`risk.${r}`)}</option>)}</Select>
            {(users.data?.length ?? 0) > 0 && (
              <Select id="f-validator" label={t("journal.validator")} value={search.decided_by ?? ""} onChange={(e) => setFilters({ decided_by: e.target.value || undefined })}>
                <option value="">{t("journal.all")}</option>{users.data?.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}</Select>
            )}
          </>
        ) : (
          <>
            <Select id="f-action" label={t("journal.actionType")} value={search.action ?? ""} onChange={(e) => setFilters({ action: e.target.value || undefined })}>
              <option value="">{t("journal.all")}</option>{AUDIT_PREFIXES.map((p) => <option key={p} value={p}>{p}*</option>)}</Select>
            <Select id="f-actor-type" label={t("journal.actorKind")} value={search.actor_type ?? ""} onChange={(e) => setFilters({ actor_type: e.target.value || undefined })}>
              <option value="">{t("journal.all")}</option>{(["user", "key", "system"] as const).map((a) => <option key={a} value={a}>{t(`journal.actorType.${a}`)}</option>)}</Select>
          </>
        )}
        <div className="flex min-w-48 flex-1 flex-col gap-1.5">
          <label htmlFor="f-q" className="font-medium">{t("journal.search")}</label>
          <input id="f-q" ref={searchRef} value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("journal.searchPlaceholder")} className="h-9 rounded-field border border-border-strong bg-surface px-3" />
        </div>
      </div>
      {tab === "exec" && (
        <div role="group" aria-label={t("journal.status")} className="flex flex-wrap gap-1.5">
          {STATUSES.map((s) => (
            <button key={s} aria-pressed={statuses.includes(s)} onClick={() => toggleStatus(s)}
              className={`rounded-full border px-3 py-0.5 ${statuses.includes(s) ? "border-primary bg-primary/12 text-primary" : "border-border text-muted"}`}>{t(`status.${s}`)}</button>
          ))}
        </div>
      )}
      </div>
      {active.length > 0 && (
        <div className="flex flex-wrap items-center gap-2" aria-label={t("journal.activeFilters")}>
          {active.map(([k, v]) => (
            <span key={k} className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5">
              <span className="text-muted">{t(`journal.f.${k}`)} :</span> {label(k, v)}
              <button aria-label={t("journal.removeFilter", { name: t(`journal.f.${k}`) })} onClick={() => setFilters({ [k]: undefined })}><X aria-hidden size={12} /></button>
            </span>
          ))}
          <button className="text-primary hover:underline" onClick={clear}>{t("journal.clear")}</button>
        </div>
      )}
    </div>
  );
}

const toLocal = (iso?: string) => (iso ? new Date(new Date(iso).getTime() - new Date(iso).getTimezoneOffset() * 60000).toISOString().slice(0, 16) : "");
const fromLocal = (v: string) => (v ? new Date(v).toISOString() : undefined);

function ExecList({ search, live, onOpen }: { search: JournalSearch; live: boolean; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const qs = toQuery(search, "exec");
  const key = ["/journal/exec", qs];
  const q = useInfiniteQuery({
    queryKey: key, initialPageParam: "",
    queryFn: ({ pageParam }) => api<Page<Execution>>(`/executions?${qs}${qs ? "&" : ""}limit=50${pageParam ? `&cursor=${pageParam}` : ""}`),
    getNextPageParam: (p) => p.next_cursor || undefined,
  });
  const rows = useMemo(() => q.data?.pages.flatMap((p) => p.items) ?? [], [q.data]);

  // direct: rafraîchir (anti-rebond) et surligner les nouvelles lignes
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useLiveExecutions(live, () => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => void qc.invalidateQueries({ queryKey: ["/journal/exec"] }), 250);
  });
  const known = useRef<Set<string> | null>(null);
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!q.data) return;
    const ids = new Set(rows.map((r) => r.id));
    if (known.current) {
      const added = new Set([...ids].filter((i) => !known.current!.has(i)));
      if (added.size > 0 && live) { setFresh(added); setTimeout(() => setFresh(new Set()), 2500); }
    }
    known.current = ids;
  }, [rows, q.data, live]);
  useEffect(() => { known.current = null; }, [qs]);

  if (q.isPending) return <p className="text-muted">{t("errors.loading")}</p>;
  if (q.isError) return <p role="alert" className="text-destructive">{t("errors.generic")}</p>;
  if (rows.length === 0) return <Empty text={qs ? t("journal.emptyFiltered") : t("journal.empty")} />;
  const dim = (r: Execution) => (r.status === "denied" ? "bg-surface-2" : "");
  const hl = (r: Execution) => (fresh.has(r.id) ? "bg-primary/10" : "");

  return (
    <>
      <p className="mb-2 inline-flex items-center gap-1 text-muted" aria-live="polite">{live && <><Radio aria-hidden size={14} className="text-read" />{t("journal.live")}</>}</p>
      {/* tableau dense (≥ 768 px) */}
      <div className="hidden overflow-x-auto rounded-card border border-border bg-surface md:block">
        <table className="w-full text-left text-[13px] tabular-nums">
          <thead className="border-b border-border text-muted"><tr>
            {["date", "agent", "server", "command", "risk", "status", "code", "duration", "validator"].map((c) => <th key={c} scope="col" className="whitespace-nowrap px-3 py-2 font-medium">{t(`journal.col.${c}`)}</th>)}
          </tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id} onClick={() => onOpen(r.id)} title={r.status === "denied" ? r.denial_reason : undefined}
                className={`cursor-pointer border-b border-border last:border-0 hover:bg-surface-2 ${dim(r)} ${hl(r)}`}>
                <td className="whitespace-nowrap px-3 py-2"><button className="text-left" onClick={(e) => { e.stopPropagation(); onOpen(r.id); }} aria-label={t("journal.open", { id: r.command_slug })}>{fmtDate(r.requested_at)}</button></td>
                <td className="px-3 py-2"><Actor exec={r} /></td>
                <td className="px-3 py-2 font-mono">{r.server_slug}</td>
                <td className="px-3 py-2 font-mono">{r.command_slug}</td>
                <td className="px-3 py-2"><RiskBadge risk={r.risk} /></td>
                <td className="px-3 py-2"><StatusBadge status={r.status} /></td>
                <td className="px-3 py-2">{r.exit_code != null && r.exit_code >= 0 ? r.exit_code : "—"}</td>
                <td className="px-3 py-2">{fmtDuration(r.duration_ms)}</td>
                <td className="px-3 py-2">{r.key_id && r.decided_by_name ? <HumanAvatar name={r.decided_by_name} /> : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {/* liste de cartes (< 768 px) */}
      <ul className="flex flex-col gap-2 md:hidden">
        {rows.map((r) => (
          <li key={r.id}>
            <button onClick={() => onOpen(r.id)} className={`flex w-full flex-col gap-1.5 rounded-card border border-border bg-surface p-3 text-left ${dim(r)} ${hl(r)}`}>
              <span className="flex items-center justify-between gap-2"><span className="font-mono">{r.command_slug}</span><StatusBadge status={r.status} /></span>
              <span className="flex flex-wrap items-center gap-2 text-muted"><Actor exec={r} /><span>→</span><span className="font-mono">{r.server_slug}</span><RiskBadge risk={r.risk} /></span>
              <span className="text-muted">{fmtDate(r.requested_at)} · {fmtDuration(r.duration_ms)}</span>
            </button>
          </li>
        ))}
      </ul>
      {q.hasNextPage && <div className="mt-4 flex justify-center"><Button variant="secondary" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>{t("journal.loadMore")}</Button></div>}
    </>
  );
}

function AuditList({ search, onOpen }: { search: JournalSearch; onOpen: (id: string) => void }) {
  const { t, i18n } = useTranslation();
  const qs = toQuery(search, "admin");
  const q = useInfiniteQuery({
    queryKey: ["/journal/audit", qs], initialPageParam: "",
    queryFn: ({ pageParam }) => api<Page<AuditEvent>>(`/audit?${qs}${qs ? "&" : ""}limit=50${pageParam ? `&cursor=${pageParam}` : ""}`),
    getNextPageParam: (p) => p.next_cursor || undefined,
  });
  const rows = useMemo(() => q.data?.pages.flatMap((p) => p.items) ?? [], [q.data]);
  const open = rows.find((r) => r.id === search.event);
  const actionLabel = (a: string) => (i18n.exists(`audit.action.${a}`) ? t(`audit.action.${a}`) : a);

  if (q.isPending) return <p className="text-muted">{t("errors.loading")}</p>;
  if (rows.length === 0) return <Empty text={qs ? t("journal.emptyFiltered") : t("journal.emptyAudit")} />;
  return (
    <>
      <ul className="flex flex-col gap-2 md:hidden">
        {rows.map((r) => (
          <li key={r.id}>
            <button onClick={() => onOpen(r.id)} className="flex w-full flex-col gap-1 rounded-card border border-border bg-surface p-3 text-left">
              <span>{actionLabel(r.action)} <code className="font-mono">{r.target_name}</code></span>
              <span className="flex flex-wrap items-center gap-2 text-muted">{r.actor_type === "user" ? <HumanAvatar name={r.actor_name} /> : r.actor_name || t(`journal.actorType.${r.actor_type}`)}<span>· {fmtDate(r.ts)}</span></span>
            </button>
          </li>
        ))}
      </ul>
      <div className="hidden overflow-x-auto rounded-card border border-border bg-surface md:block">
        <table className="w-full text-left text-[13px] tabular-nums">
          <thead className="border-b border-border text-muted"><tr>
            {["date", "actor", "action", "target", "ip"].map((c) => <th key={c} scope="col" className="px-3 py-2 font-medium">{t(`journal.auditCol.${c}`)}</th>)}
          </tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id} onClick={() => onOpen(r.id)} className="cursor-pointer border-b border-border last:border-0 hover:bg-surface-2">
                <td className="whitespace-nowrap px-3 py-2"><button className="text-left" onClick={(e) => { e.stopPropagation(); onOpen(r.id); }}>{fmtDate(r.ts)}</button></td>
                <td className="px-3 py-2">{r.actor_type === "key" ? <Actor exec={{ key_name: r.actor_name, decided_by_name: "", actor_user_id: "" }} /> : r.actor_type === "user" ? <HumanAvatar name={r.actor_name} /> : <span className="text-muted">{r.actor_name || t("journal.actorType.system")}</span>}</td>
                <td className="px-3 py-2">{actionLabel(r.action)} <code className="font-mono text-muted">{r.target_name}</code></td>
                <td className="px-3 py-2 text-muted">{r.target_type}</td>
                <td className="px-3 py-2 font-mono text-muted">{r.ip || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {q.hasNextPage && <div className="mt-4 flex justify-center"><Button variant="secondary" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>{t("journal.loadMore")}</Button></div>}
      {open && <AuditDrawer event={open} onClose={() => onOpen("")} />}
    </>
  );
}
