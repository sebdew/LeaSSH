import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { keyActive } from "../lib/active";
import { useList } from "../lib/queries";
import type { Command, McpKey, SshServer } from "../lib/types";

interface Item { id: string; section: string; label: string; hint?: string; to: string; params?: Record<string, string> }

const PAGES: [string, string][] = [["dashboard", "/"], ["journal", "/journal"], ["approvals", "/validations"], ["proposals", "/propositions"], ["commands", "/commandes"], ["packs", "/packs"],
  ["servers", "/serveurs"], ["identities", "/identites"], ["provisioning", "/provisioning"], ["rules", "/regles"], ["keys", "/cles"], ["simulator", "/simulateur"], ["audit", "/audit"], ["settings", "/reglages"]];

// Palette Ctrl/⌘+K : pages, serveurs, commandes et clés MCP.
export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const ref = useRef<HTMLDialogElement>(null);
  const [q, setQ] = useState("");
  const [cursor, setCursor] = useState(0);
  const servers = useList<SshServer>("/servers");
  const commands = useList<Command>("/commands");
  const keys = useList<McpKey>("/keys");

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) { setQ(""); setCursor(0); d.showModal(); }
    if (!open && d.open) d.close();
  }, [open]);

  const items = useMemo(() => {
    const all: Item[] = [
      ...PAGES.map(([k, to]) => ({ id: `p-${k}`, section: t("palette.pages"), label: t(`nav.${k}`), to })),
      ...(servers.data ?? []).map((s) => ({ id: `s-${s.id}`, section: t("palette.servers"), label: s.slug, hint: s.name, to: "/serveurs" })),
      ...(commands.data ?? []).map((c) => ({ id: `c-${c.id}`, section: t("palette.commands"), label: c.slug, hint: c.name, to: "/commandes/$id", params: { id: c.id } })),
      ...(keys.data ?? []).filter(keyActive).map((k) => ({ id: `k-${k.id}`, section: t("palette.keys"), label: k.name, hint: k.prefix, to: "/cles" })),
    ];
    const needle = q.trim().toLowerCase();
    return (needle ? all.filter((i) => `${i.label} ${i.hint ?? ""}`.toLowerCase().includes(needle)) : all.filter((i) => i.section === t("palette.pages"))).slice(0, 30);
  }, [q, servers.data, commands.data, keys.data, t]);

  const go = (i: Item) => { onClose(); void nav({ to: i.to as never, params: i.params as never }); };
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") { e.preventDefault(); setCursor((c) => Math.min(c + 1, items.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setCursor((c) => Math.max(c - 1, 0)); }
    else if (e.key === "Enter" && items[cursor]) { e.preventDefault(); go(items[cursor]); }
  };

  return (
    <dialog ref={ref} onClose={onClose} aria-label={t("palette.title")} className="m-auto mt-[12vh] w-[min(560px,92vw)] rounded-card border border-border bg-surface p-0 text-fg backdrop:bg-black/40">
      <input autoFocus placeholder={t("palette.placeholder")} aria-label={t("palette.title")} value={q} onKeyDown={onKey} onChange={(e) => { setQ(e.target.value); setCursor(0); }}
        role="combobox" aria-expanded="true" aria-controls="palette-list" aria-activedescendant={items[cursor] ? `po-${items[cursor].id}` : undefined}
        className="h-11 w-full border-b border-border bg-transparent px-4 outline-none" />
      {items.length === 0 ? <p className="p-4 text-muted">{t("palette.noResult", { q })}</p> : (
        <ul id="palette-list" role="listbox" className="max-h-[50vh] overflow-y-auto p-2">
          {items.map((i, n) => (
            <li key={i.id} id={`po-${i.id}`} role="option" aria-selected={n === cursor} onMouseEnter={() => setCursor(n)} onClick={() => go(i)}
              className={`flex cursor-pointer items-center gap-3 rounded-field px-3 py-2 ${n === cursor ? "bg-surface-2" : ""}`}>
              <span className="w-20 shrink-0 text-[12px] uppercase tracking-wide text-muted">{i.section}</span>
              <span className="font-medium">{i.label}</span>{i.hint && <span className="text-muted">{i.hint}</span>}
            </li>
          ))}
        </ul>
      )}
    </dialog>
  );
}
