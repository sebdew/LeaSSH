import { useMemo, useState } from "react";

export type SortDir = "asc" | "desc";
export interface SortState { key: string; dir: SortDir }

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

// Recherche (sous-chaîne, insensible à la casse et aux accents, tous les mots) + tri sur colonnes pour les listes des écrans d'administration.
export function useListView<T>(items: T[] | undefined, opts: { text: (x: T) => string; sorts: Record<string, (x: T) => string | number>; initial?: SortState }) {
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortState | null>(opts.initial ?? null);
  const { text, sorts } = opts;
  const rows = useMemo(() => {
    const norm = (v: string) => v.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
    const words = norm(query).split(/\s+/).filter(Boolean);
    let out = (items ?? []).filter((x) => { const h = norm(text(x)); return words.every((w) => h.includes(w)); });
    const get = sort && sorts[sort.key];
    if (sort && get) {
      const k = sort.dir === "asc" ? 1 : -1;
      out = [...out].sort((a, b) => { const x = get(a), y = get(b); return k * (typeof x === "number" && typeof y === "number" ? x - y : collator.compare(String(x), String(y))); });
    }
    return out;
  }, [items, query, sort, text, sorts]);
  const toggle = (key: string) => setSort((s) => (s?.key !== key ? { key, dir: "asc" } : s.dir === "asc" ? { key, dir: "desc" } : null));
  return { rows, query, setQuery, sort, toggle, filtered: query.trim() !== "" };
}
