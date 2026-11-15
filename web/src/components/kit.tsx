import { useEffect, useRef, useState, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Check, CircleDot, Copy, Eye, Hexagon, Inbox, PenLine, RefreshCw, X } from "lucide-react";
import { Button } from "./ui";
import { currentLocale } from "../lib/lang";
import type { ExecStatus, Risk } from "../lib/types";

export function PageHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="text-[24px] font-semibold">{title}</h1>
        {subtitle && <p className="mt-1 text-muted">{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}

export type Tone = "read" | "write" | "destructive" | "info" | "muted" | "primary";
const tones: Record<Tone, string> = {
  read: "bg-read/12 text-read", write: "bg-write/12 text-write", destructive: "bg-destructive/12 text-destructive",
  info: "bg-info/12 text-info", muted: "bg-surface-2 text-muted", primary: "bg-primary/12 text-primary",
};

// Marque une ressource tenue par une synchro (non modifiable à la main, hors périmètre indiqué).
export function SyncMark({ source, kind }: { source: string; kind: "server" | "group" }) {
  const { t } = useTranslation();
  const label = t(`provisioning.mark.${kind}`, { source });
  return <span title={label} aria-label={label} className="inline-flex align-middle text-primary"><RefreshCw aria-hidden size={14} /></span>;
}

export function Badge({ tone = "muted", children }: { tone?: Tone; children: ReactNode }) {
  return <span className={`inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[12px] font-medium ${tones[tone]}`}>{children}</span>;
}

// Le risque a toujours la même couleur, mais jamais la couleur seule: icône + libellé.
export function RiskBadge({ risk }: { risk: Risk | "" }) {
  const { t } = useTranslation();
  if (!risk) return <span className="text-muted">—</span>;
  const icon = { read: <CircleDot aria-hidden size={12} />, write: <AlertTriangle aria-hidden size={12} />, destructive: <Hexagon aria-hidden size={12} /> }[risk];
  return <Badge tone={risk}>{icon}{t(`risk.${risk}`)}</Badge>;
}

// Profil d'une règle, visible d'un coup d'œil: lecture seule (RO) ou lecture et écriture (RW). Jamais la couleur seule: icône + libellé.
export function ProfileBadge({ profile }: { profile: "RO" | "RW" }) {
  const { t } = useTranslation();
  const rw = profile === "RW";
  return (
    <span data-testid="rule-profile" className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-3 py-1 text-[13px] font-semibold ${tones[rw ? "write" : "read"]}`}>
      {rw ? <PenLine aria-hidden size={15} /> : <Eye aria-hidden size={15} />}
      {t(`rules.badge.${profile}`)}
    </span>
  );
}

const statusTone: Record<ExecStatus, Tone> = {
  succeeded: "read", failed: "destructive", denied: "destructive", pending_approval: "write", rejected: "muted",
  expired: "muted", timeout: "write", running: "info", error: "destructive",
};

export function StatusBadge({ status }: { status: ExecStatus }) {
  const { t } = useTranslation();
  return <Badge tone={statusTone[status]}>{t(`status.${status}`)}</Badge>;
}

export function Empty({ text, action }: { text: string; action?: ReactNode }) {
  return (
    <div className="grid place-items-center gap-3 rounded-card border border-dashed border-border p-10 text-center text-muted">
      <Inbox aria-hidden size={28} className="opacity-60" />
      <p>{text}</p>
      {action}
    </div>
  );
}

export function Modal({ open, onClose, title, children, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; wide?: boolean }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog ref={ref} onClose={onClose} aria-label={title}
      className={`m-auto w-[min(${wide ? "720px" : "520px"},94vw)] max-h-[90vh] overflow-y-auto rounded-card border border-border bg-surface p-0 text-fg backdrop:bg-black/40`}
      style={{ width: wide ? "min(720px, 94vw)" : "min(520px, 94vw)" }}>
      {open && (
        <div className="p-6">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-[18px] font-semibold">{title}</h2>
            <button onClick={onClose} aria-label={t("common.close")} className="grid h-8 w-8 place-items-center rounded-field text-muted hover:bg-surface-2"><X aria-hidden size={16} /></button>
          </div>
          {children}
        </div>
      )}
    </dialog>
  );
}

export function Select({ label, id, children, ...p }: SelectHTMLAttributes<HTMLSelectElement> & { label: string; id: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="font-medium">{label}</label>
      <select id={id} {...p} className="h-9 rounded-field border border-border-strong bg-surface px-2 text-fg">{children}</select>
    </div>
  );
}

export function Textarea({ label, id, hint, mono, ...p }: TextareaHTMLAttributes<HTMLTextAreaElement> & { label: string; id: string; hint?: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="font-medium">{label}</label>
      <textarea id={id} {...p} className={`min-h-20 rounded-field border border-border-strong bg-surface px-3 py-2 text-fg ${mono ? "font-mono text-[13px]" : ""}`} />
      {hint && <p className="text-muted">{hint}</p>}
    </div>
  );
}

export function Check2({ label, id, checked, onChange, disabled }: { label: string; id: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <label htmlFor={id} className="flex items-center gap-2">
      <input id={id} type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4 accent-primary" />
      {label}
    </label>
  );
}

// navigator.clipboard n'existe qu'en contexte sécurisé (HTTPS ou localhost): en HTTP simple on retombe sur execCommand.
// `host` doit se trouver dans le même <dialog> que le bouton, sinon la sélection est ignorée (le reste de la page est inerte).
export async function copyToClipboard(text: string, host: HTMLElement): Promise<boolean> {
  try {
    if (window.isSecureContext && navigator.clipboard) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch { /* refusé: essai de la méthode de repli */ }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.setAttribute("aria-hidden", "true");
  ta.style.cssText = "position:fixed;top:0;left:0;width:1px;height:1px;opacity:0";
  host.appendChild(ta);
  ta.focus();
  ta.select();
  ta.setSelectionRange(0, text.length);
  let ok: boolean;
  try { ok = document.execCommand("copy"); } catch { ok = false; }
  host.removeChild(ta);
  return ok;
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const { t } = useTranslation();
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  return (
    <Button variant="secondary" type="button" className="h-8 px-3" onClick={(e) => {
      const btn = e.currentTarget;
      void copyToClipboard(text, btn.parentElement ?? btn).then((ok) => {
        setState(ok ? "done" : "failed");
        setTimeout(() => setState("idle"), ok ? 1500 : 4000);
      });
    }}>
      {state === "done" ? <Check aria-hidden size={14} /> : <Copy aria-hidden size={14} />}
      <span aria-live="polite">{state === "done" ? t("common.copied") : state === "failed" ? t("common.copyFailed") : (label ?? t("common.copy"))}</span>
    </Button>
  );
}

// Bloc commande: fond surface-2, monospace, bouton copier.
export function CodeBlock({ children, copy }: { children: string; copy?: boolean }) {
  return (
    <div className="flex items-start gap-2 rounded-field bg-surface-2 p-3">
      <code className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-[13px]">{children}</code>
      {copy && <CopyButton text={children} />}
    </div>
  );
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: string }[]; value: T; onChange: (v: T) => void }) {
  return (
    <div role="tablist" className="mb-4 flex gap-1 overflow-x-auto overflow-y-hidden border-b pb-px border-border">
      {tabs.map((tb) => (
        <button key={tb.id} role="tab" aria-selected={tb.id === value} onClick={() => onChange(tb.id)}
          className={`-mb-px whitespace-nowrap border-b-2 px-4 py-2 ${tb.id === value ? "border-primary font-medium text-fg" : "border-transparent text-muted hover:text-fg"}`}>
          {tb.label}
        </button>
      ))}
    </div>
  );
}

// Confirmation explicite pour les actions dangereuses: saisir le slug de l'objet (docs/specs/UX.md §2.6).
export function ConfirmDialog({ open, onClose, onConfirm, title, body, confirmWord, busy }: {
  open: boolean; onClose: () => void; onConfirm: () => void; title: string; body: string; confirmWord?: string; busy?: boolean;
}) {
  const { t } = useTranslation();
  const [typed, setTyped] = useState("");
  useEffect(() => { if (!open) setTyped(""); }, [open]);
  const ok = !confirmWord || typed === confirmWord;
  return (
    <Modal open={open} onClose={onClose} title={title}>
      <p className="mb-4">{body}</p>
      {confirmWord && (
        <div className="mb-4 flex flex-col gap-1.5">
          <label htmlFor="confirm-word" className="font-medium">{t("common.typeToConfirm", { word: confirmWord })}</label>
          <input id="confirm-word" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" className="h-9 rounded-field border border-border-strong bg-surface px-3 font-mono" />
        </div>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>{t("common.cancel")}</Button>
        <Button disabled={!ok || busy} onClick={onConfirm} className="!bg-destructive !text-primary-fg">{t("common.confirm")}</Button>
      </div>
    </Modal>
  );
}

// Multi-sélection par cases à cocher (membres de groupes, clés d'une règle…).
export function MultiPick({ label, options, value, onChange, locked = [] }: { label: string; options: { id: string; label: string }[]; value: string[]; onChange: (v: string[]) => void; locked?: string[] }) {
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className="mb-1 font-medium">{label}</legend>
      <div className="grid max-h-48 gap-1 overflow-y-auto rounded-field border border-border p-2">
        {options.map((o) => (
          <Check2 key={o.id} id={`mp-${label}-${o.id}`} label={o.label} checked={value.includes(o.id) || locked.includes(o.id)} disabled={locked.includes(o.id)}
            onChange={(c) => onChange(c ? [...value, o.id] : value.filter((x) => x !== o.id))} />
        ))}
      </div>
    </fieldset>
  );
}

// Les listes de gestion masquent par défaut les éléments inactifs; la case les ré-affiche pour les consulter ou les réactiver.
export function ShowInactiveToggle({ count, checked, onChange }: { count: number; checked: boolean; onChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  if (count === 0) return null;
  return <div className="mb-3"><Check2 id="show-inactive" label={t("common.showInactive", { count })} checked={checked} onChange={onChange} /></div>;
}

export const when = (iso: string, lang = currentLocale()) => (iso ? new Date(iso).toLocaleString(lang, { dateStyle: "short", timeStyle: "medium" }) : "—");

// Champ de recherche d'une liste (avec compteur de résultats).
export function ListSearch({ value, onChange, label, count, total }: { value: string; onChange: (v: string) => void; label: string; count: number; total: number }) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-3">
      <input type="search" value={value} onChange={(e) => onChange(e.target.value)} aria-label={label} placeholder={label}
        className="h-9 w-full max-w-xs rounded-field border border-border-strong bg-surface px-3 text-fg" />
      {value.trim() !== "" && <span aria-live="polite" className="text-muted">{count} / {total}</span>}
    </div>
  );
}

// En-tête de colonne triable (clic ou clavier; aria-sort pour les lecteurs d'écran).
export function SortTh({ label, sortKey, sort, onSort, className = "px-4 py-2 font-medium" }: { label: string; sortKey?: string; sort: { key: string; dir: "asc" | "desc" } | null; onSort: (k: string) => void; className?: string }) {
  if (!sortKey) return <th scope="col" className={className}>{label}</th>;
  const active = sort?.key === sortKey;
  return (
    <th scope="col" className={className} aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : "none"}>
      <button type="button" onClick={() => onSort(sortKey)} className="inline-flex items-center gap-1 font-medium hover:text-fg">
        {label}<span aria-hidden className={active ? "text-fg" : "opacity-40"}>{active ? (sort.dir === "asc" ? "▲" : "▼") : "↕"}</span>
      </button>
    </th>
  );
}
