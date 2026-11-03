import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FileUp, Package, Search } from "lucide-react";
import { Button, Card, ErrorBox } from "../components/ui";
import { Badge, Check2, Modal, PageHeader, RiskBadge, Select, Textarea, when } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { currentLocale } from "../lib/lang";
import { errorMessage, useList } from "../lib/queries";
import type { BundledPack, Group, InstalledPack, PackItem, PackPreview } from "../lib/types";

type Source = { bundled: string } | { yaml: string };

export function PacksPage() {
  const { t } = useTranslation();
  const packs = useQuery({ queryKey: ["/packs"], queryFn: () => api<{ installed: InstalledPack[]; bundled: BundledPack[] }>("/packs") });
  const groups = useList<Group>("/command-groups");
  const [source, setSource] = useState<Source | null>(null);
  const [pasted, setPasted] = useState("");
  const [fileError, setFileError] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const [exportGroup, setExportGroup] = useState("");

  const readFile = async (f: File | undefined) => {
    if (!f) return;
    setFileError("");
    if (f.size > 900 * 1024) { setFileError(t("packs.tooBig")); return; }
    setSource({ yaml: await f.text() });
  };
  const exportHref = `/api/v1/packs/export${exportGroup ? `?group=${encodeURIComponent(exportGroup)}&pack_id=${groups.data?.find((g) => g.id === exportGroup)?.slug ?? "export"}&name=${encodeURIComponent(groups.data?.find((g) => g.id === exportGroup)?.name ?? "")}` : ""}`;

  return (
    <>
      <PageHeader title={t("packs.title")} subtitle={t("packs.subtitle")} />
      <BundledList packs={packs.data?.bundled ?? []} onOpen={(id) => setSource({ bundled: id })} />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="flex flex-col gap-3">
          <h2 className="text-[18px] font-semibold">{t("packs.import")}</h2>
          <div onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); void readFile(e.dataTransfer.files[0]); }}
            className="grid place-items-center gap-2 rounded-card border border-dashed border-border p-6 text-center text-muted">
            <FileUp aria-hidden size={24} />
            <p>{t("packs.dropHere")}</p>
            <input ref={fileRef} type="file" accept=".yaml,.yml" className="sr-only" aria-label={t("packs.chooseFile")} onChange={(e) => void readFile(e.target.files?.[0])} />
            <Button variant="secondary" onClick={() => fileRef.current?.click()}>{t("packs.chooseFile")}</Button>
          </div>
          {fileError && <p role="alert" className="text-destructive">{fileError}</p>}
          <Textarea id="pack-paste" mono label={t("packs.orPaste")} value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder="pack:\n  id: …" />
          <div><Button disabled={!pasted.trim()} onClick={() => setSource({ yaml: pasted })}>{t("packs.preview")}</Button></div>
        </Card>
        <Card className="flex flex-col gap-3">
          <h2 className="text-[18px] font-semibold">{t("packs.export")}</h2>
          <p className="text-muted">{t("packs.exportHelp")}</p>
          <Select id="pack-export-group" label={t("packs.exportScope")} value={exportGroup} onChange={(e) => setExportGroup(e.target.value)}>
            <option value="">{t("packs.exportAll")}</option>{groups.data?.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </Select>
          <div><a href={exportHref} download className="inline-flex h-9 items-center gap-2 rounded-field bg-primary px-4 font-medium text-primary-fg"><Download aria-hidden size={16} />{t("packs.download")}</a></div>
        </Card>
      </div>

      {(packs.data?.installed.length ?? 0) > 0 && (
        <>
          <h2 className="mb-3 mt-8 text-[18px] font-semibold">{t("packs.installed")}</h2>
          <div className="overflow-x-auto rounded-card border border-border bg-surface">
            <table className="w-full text-left text-[13px]">
              <thead className="border-b border-border text-muted"><tr>{["name", "version", "commands", "installedAt"].map((c) => <th key={c} scope="col" className="px-4 py-2 font-medium">{t(`packs.col.${c}`)}</th>)}</tr></thead>
              <tbody>{packs.data?.installed.map((p) => (
                <tr key={p.id} className="border-b border-border last:border-0"><td className="px-4 py-2"><div className="font-medium">{p.name}</div><code className="font-mono text-muted">{p.id}</code></td>
                  <td className="px-4 py-2">v{p.version}</td><td className="px-4 py-2">{p.command_count}</td><td className="px-4 py-2 text-muted">{when(p.installed_at)}</td></tr>
              ))}</tbody>
            </table>
          </div>
        </>
      )}
      <ImportDialog source={source} onClose={() => setSource(null)} />
    </>
  );
}

type PackState = "none" | "ok" | "update";
const packState = (p: BundledPack): PackState => (!p.installed_version ? "none" : p.installed_version === p.version ? "ok" : "update");

// Liste des packs fournis: recherche, filtres par catégorie et état, regroupement par catégorie. Reste lisible avec des dizaines de packs.
function BundledList({ packs, onOpen }: { packs: BundledPack[]; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [cat, setCat] = useState("");
  const [state, setState] = useState<"" | PackState>("");
  const other = t("packs.otherCategory");
  const catOf = (p: BundledPack) => p.category || other;
  const categories = [...new Set(packs.map(catOf))].sort((a, b) => a.localeCompare(b, currentLocale()));
  const needle = q.trim().toLowerCase();
  const shown = packs.filter((p) => (!cat || catOf(p) === cat) && (!state || packState(p) === state)
    && (!needle || [p.name, p.id, p.description, ...(p.command_ids ?? [])].some((x) => x.toLowerCase().includes(needle))));
  const grouped = categories.map((c) => [c, shown.filter((p) => catOf(p) === c)] as const).filter(([, l]) => l.length > 0);
  const reset = () => { setQ(""); setCat(""); setState(""); };
  return (
    <section aria-labelledby="bundled-title" className="mb-8">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <h2 id="bundled-title" className="text-[18px] font-semibold">{t("packs.bundled")} <span className="text-[14px] font-normal text-muted">({packs.length})</span></h2>
        <div className="flex flex-wrap items-center gap-2">
          <label className="relative">
            <span className="sr-only">{t("packs.search")}</span>
            <Search aria-hidden size={14} className="pointer-events-none absolute left-2.5 top-2.5 text-muted" />
            <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("packs.searchShort")} className="h-9 w-56 rounded-field border border-border-strong bg-surface pl-8 pr-3 text-fg" />
          </label>
          <label>
            <span className="sr-only">{t("packs.filterState")}</span>
            <select value={state} onChange={(e) => setState(e.target.value as "" | PackState)} className="h-9 rounded-field border border-border-strong bg-surface px-2 text-fg">
              <option value="">{t("packs.stateAll")}</option>
              <option value="none">{t("packs.notInstalled")}</option>
              <option value="ok">{t("packs.upToDate")}</option>
              <option value="update">{t("packs.updateAvailable")}</option>
            </select>
          </label>
        </div>
      </div>
      {categories.length > 1 && (
        <div role="group" aria-label={t("packs.filterCategory")} className="mb-4 flex flex-wrap gap-1.5">
          {["", ...categories].map((c) => (
            <button key={c || "all"} type="button" aria-pressed={cat === c} onClick={() => setCat(c)}
              className={`h-8 rounded-full border px-3 text-[13px] ${cat === c ? "border-primary bg-primary/10 font-medium text-primary" : "border-border bg-surface text-muted hover:bg-surface-2"}`}>
              {c || t("packs.allCategories")}{c && <span className="ml-1 text-muted">{packs.filter((p) => catOf(p) === c).length}</span>}
            </button>
          ))}
        </div>
      )}
      {grouped.length === 0 ? (
        <div className="rounded-card border border-dashed border-border p-6 text-center text-muted">
          <p>{t("packs.noMatch")}</p>
          <Button variant="secondary" className="mt-3" onClick={reset}>{t("packs.resetFilters")}</Button>
        </div>
      ) : grouped.map(([c, list]) => (
        <div key={c} className="mb-5">
          <h3 className="mb-2 text-[13px] font-semibold uppercase tracking-wide text-muted">{c} <span className="font-normal">({list.length})</span></h3>
          <ul className="divide-y divide-border overflow-hidden rounded-card border border-border bg-surface">
            {list.map((p) => <PackRow key={p.id} p={p} onOpen={onOpen} />)}
          </ul>
        </div>
      ))}
    </section>
  );
}

function PackRow({ p, onOpen }: { p: BundledPack; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  const st = packState(p);
  const r = p.risks;
  return (
    <li className="flex flex-col gap-2 px-4 py-3 lg:flex-row lg:items-center lg:gap-4" data-testid={`pack-${p.id}`}>
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-x-2 font-semibold"><Package aria-hidden size={15} className="shrink-0 text-primary" /><span>{p.name}</span><span className="font-normal text-muted">v{p.version}</span></p>
        <p className="line-clamp-2 text-muted" title={p.description.trim()}>{p.description.trim()}</p>
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 lg:shrink-0 lg:justify-end">
        <p className="text-[13px] text-muted lg:text-right">
          {t("packs.commandCount", { count: p.command_count })}
          {r && <span className="block" aria-label={t("packs.riskSummary")}>
            {t("packs.risk.read", { count: r.read })}{r.write > 0 && ` · ${t("packs.risk.write", { count: r.write })}`}{r.destructive > 0 && ` · ${t("packs.risk.destructive", { count: r.destructive })}`}
          </span>}
        </p>
        <Badge tone={st === "none" ? "muted" : st === "ok" ? "read" : "write"}>{st === "none" ? t("packs.notInstalled") : st === "ok" ? t("packs.upToDate") : t("packs.updateAvailable")}</Badge>
        <Button variant="secondary" className="h-8" onClick={() => onOpen(p.id)}>{st === "none" ? t("packs.install") : t("packs.review")}</Button>
      </div>
    </li>
  );
}

const STATUS_TONE = { new: "read", modified: "write", unchanged: "muted", invalid: "destructive" } as const;

function FieldDiff({ item }: { item: PackItem }) {
  const { t } = useTranslation();
  return (
    <div className="mt-2 flex flex-col gap-2">
      {item.changes?.map((c) => (
        <div key={c.field} className="overflow-hidden rounded-field border border-border font-mono text-[12px]">
          <div className="bg-surface-2 px-2 py-0.5 text-muted">{c.field}</div>
          <div data-kind="removed" className="flex gap-2 bg-destructive/12 px-2 py-1"><span aria-label={t("journal.removed")}>−</span><span className="break-all">{c.before || "∅"}</span></div>
          <div data-kind="added" className="flex gap-2 bg-read/12 px-2 py-1"><span aria-label={t("journal.added")}>+</span><span className="break-all">{c.after || "∅"}</span></div>
        </div>
      ))}
    </div>
  );
}

// Aperçu des changements avant import: nouvelles / modifiées (diff) / inchangées / invalides.
export function ImportDialog({ source, onClose }: { source: Source | null; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["pack-preview", source], enabled: !!source, retry: false,
    queryFn: () => api<{ preview: PackPreview; trusted: boolean }>("/packs/preview", { method: "POST", body: source }),
  });
  const [createGroup, setCreateGroup] = useState(true);
  const [ack, setAck] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const p = q.data?.preview;
  const trusted = q.data?.trusted ?? false;
  const needsAck = !!p && p.warnings > 0 && !trusted;
  const importIt = async () => {
    setBusy(true); setErr("");
    try {
      await api("/packs/import", { method: "POST", body: { ...source, create_group: createGroup, allow_warnings: ack } });
      await Promise.all(["/packs", "/commands", "/command-groups"].map((k) => qc.invalidateQueries({ queryKey: [k] })));
      toast(t("packs.imported"));
      onClose();
    } catch (e) { setErr(errorMessage(e)); } finally { setBusy(false); }
  };
  return (
    <Modal open={!!source} onClose={onClose} title={p ? t("packs.previewTitle", { name: p.pack.name, version: p.pack.version }) : t("packs.preview")} wide>
      {q.isPending && <p className="text-muted">{t("errors.loading")}</p>}
      {q.isError && <ErrorBox message={t("packs.invalid")} detail={errorMessage(q.error)} title={t("errors.details")} />}
      {p && (
        <div className="flex flex-col gap-3">
          <p className="flex flex-wrap gap-2" aria-label={t("packs.summary")}>
            {(["new", "modified", "unchanged", "invalid"] as const).map((k) => <Badge key={k} tone={STATUS_TONE[k]}>{p.counts[k] ?? 0} {t(`packs.status.${k}`)}</Badge>)}
            {p.installed_version && <span className="text-muted">{t("packs.installedVersion", { version: p.installed_version })}</span>}
          </p>
          {!trusted && <p className="rounded-field border border-write bg-write/10 p-2">{t("packs.untrusted")}</p>}
          <ul className="flex max-h-[45vh] flex-col gap-2 overflow-y-auto">
            {p.items.filter((i) => i.status !== "unchanged").map((i) => (
              <li key={i.slug} className="rounded-field border border-border p-3">
                <div className="flex flex-wrap items-center gap-2"><Badge tone={STATUS_TONE[i.status]}>{t(`packs.status.${i.status}`)}</Badge><code className="font-mono">{i.slug}</code><RiskBadge risk={i.risk} /></div>
                {i.overrides_manual && <p className="mt-1 text-write">▲ {t("packs.overridesManual")}</p>}
                {i.lint.errors.map((e, n) => <p key={n} role="alert" className="mt-1 text-destructive">✖ {e.message}</p>)}
                {i.lint.warnings.map((e, n) => <p key={n} className="mt-1 text-write">▲ {e.message}</p>)}
                {i.status === "modified" && <FieldDiff item={i} />}
              </li>
            ))}
            {p.counts.new + p.counts.modified + p.counts.invalid === 0 && <li className="text-muted">{t("packs.nothingToDo")}</li>}
          </ul>
          <Check2 id="pack-group" label={t("packs.createGroup", { name: p.pack.id })} checked={createGroup} onChange={setCreateGroup} />
          {needsAck && <Check2 id="pack-ack" label={t("packs.ackWarnings", { count: p.warnings })} checked={ack} onChange={setAck} />}
          {err && <ErrorBox message={t("packs.importFailed")} detail={err} title={t("errors.details")} />}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
            <Button disabled={busy || p.errors > 0 || (needsAck && !ack) || p.counts.new + p.counts.modified === 0} onClick={() => void importIt()}>{t("packs.confirmImport")}</Button>
          </div>
        </div>
      )}
    </Modal>
  );
}
