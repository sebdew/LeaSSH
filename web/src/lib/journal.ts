import { useEffect, useRef } from "react";

// État du Journal, entièrement porté par l'URL (partageable, persistant au rafraîchissement).
export interface JournalSearch {
  tab?: "exec" | "admin";
  period?: string; from?: string; to?: string;
  status?: string; key_id?: string; server?: string; command?: string; risk?: string; decided_by?: string; q?: string;
  exec?: string; // exécution ouverte dans le tiroir
  action?: string; actor_type?: string; event?: string; // onglet Administration
}

const EXEC_KEYS = ["period", "from", "to", "status", "key_id", "server", "command", "risk", "decided_by", "q"] as const;
const AUDIT_KEYS = ["period", "from", "to", "action", "actor_type", "q"] as const;

// Normalisation des search params reçus du routeur (tout est chaîne, le reste est ignoré).
export function parseSearch(raw: Record<string, unknown>): JournalSearch {
  const out: Record<string, string> = {};
  for (const k of [...EXEC_KEYS, ...AUDIT_KEYS, "exec", "event", "tab"]) {
    const v = raw[k];
    if ((typeof v === "string" || typeof v === "number") && String(v) !== "") out[k] = String(v);
  }
  if (out.tab && out.tab !== "exec" && out.tab !== "admin") delete out.tab;
  return out as JournalSearch;
}

export function toQuery(s: JournalSearch, tab: "exec" | "admin", extra: Record<string, string> = {}): string {
  const p = new URLSearchParams();
  for (const k of tab === "exec" ? EXEC_KEYS : AUDIT_KEYS) if (s[k]) p.set(k, s[k]!);
  for (const [k, v] of Object.entries(extra)) p.set(k, v);
  return p.toString();
}

export const activeFilters = (s: JournalSearch, tab: "exec" | "admin"): [keyof JournalSearch, string][] =>
  (tab === "exec" ? EXEC_KEYS : AUDIT_KEYS).filter((k) => s[k]).map((k) => [k, s[k]!]);

// ---- diff avant / après (journal d'administration) ----
export interface DiffRow { path: string; kind: "added" | "removed" | "changed" | "same"; before?: unknown; after?: unknown }

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

function flatten(v: unknown, prefix = "", out: Record<string, unknown> = {}): Record<string, unknown> {
  if (isObj(v)) { for (const [k, x] of Object.entries(v)) flatten(x, prefix ? `${prefix}.${k}` : k, out); if (Object.keys(v).length === 0 && prefix) out[prefix] = {}; }
  else out[prefix] = v;
  return out;
}

// Compare diff.before / diff.after (ou affiche tout diff plat en « ajouté »). Les secrets sont déjà masqués côté serveur.
export function diffRows(diff: Record<string, unknown> | null | undefined, includeSame = false): DiffRow[] {
  if (!diff || !isObj(diff)) return [];
  const hasSides = "before" in diff || "after" in diff;
  const before = flatten(hasSides ? diff.before : undefined);
  const after = flatten(hasSides ? diff.after : diff);
  delete before[""]; delete after[""];
  const paths = [...new Set([...Object.keys(before), ...Object.keys(after)])].sort();
  const rows: DiffRow[] = [];
  for (const path of paths) {
    const inB = path in before, inA = path in after;
    const b = before[path], a = after[path];
    if (inB && !inA) rows.push({ path, kind: "removed", before: b });
    else if (!inB && inA) rows.push({ path, kind: "added", after: a });
    else if (JSON.stringify(b) !== JSON.stringify(a)) rows.push({ path, kind: "changed", before: b, after: a });
    else if (includeSame) rows.push({ path, kind: "same", before: b, after: a });
  }
  return rows;
}

// ---- direct (SSE) ----
// Rappelle onEvent à chaque changement d'exécution, sauf en pause. Reconnexion gérée par EventSource.
export function useLiveExecutions(enabled: boolean, onEvent: (id: string) => void) {
  const cb = useRef(onEvent);
  cb.current = onEvent;
  useEffect(() => {
    if (!enabled || typeof EventSource === "undefined") return;
    const es = new EventSource("/api/v1/executions/stream");
    es.addEventListener("execution", (e) => {
      try { cb.current((JSON.parse((e as MessageEvent).data) as { id: string }).id); } catch { /* événement illisible */ }
    });
    return () => es.close();
  }, [enabled]);
}
