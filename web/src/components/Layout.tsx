import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { Logo } from "./Logo";
import { ClipboardCheck, RefreshCw, LayoutDashboard, ScrollText, ShieldCheck, Lightbulb, Terminal, Package, Server, KeyRound, Route, Key, FlaskConical, Settings, Menu, Search, LogOut, type LucideIcon } from "lucide-react";
import { useList } from "../lib/queries";
import type { Execution } from "../lib/types";
import { api, type User } from "../lib/api";
import { applyTheme, getTheme, type ThemeChoice } from "../lib/theme";
import { Toaster } from "./Toaster";
import { CommandPalette } from "./CommandPalette";

interface NavItem { to: string; key: string; icon: LucideIcon }
const sections: { title?: string; items: NavItem[] }[] = [
  { items: [
    { to: "/", key: "dashboard", icon: LayoutDashboard }, { to: "/journal", key: "journal", icon: ScrollText },
    { to: "/validations", key: "approvals", icon: ShieldCheck }, { to: "/propositions", key: "proposals", icon: Lightbulb } ] },
  { title: "catalog", items: [{ to: "/commandes", key: "commands", icon: Terminal }, { to: "/packs", key: "packs", icon: Package }] },
  { title: "infra", items: [{ to: "/serveurs", key: "servers", icon: Server }, { to: "/identites", key: "identities", icon: KeyRound }, { to: "/provisioning", key: "provisioning", icon: RefreshCw }] },
  { title: "access", items: [{ to: "/regles", key: "rules", icon: Route }, { to: "/cles", key: "keys", icon: Key }, { to: "/simulateur", key: "simulator", icon: FlaskConical }] },
  { items: [{ to: "/audit", key: "audit", icon: ClipboardCheck }, { to: "/reglages", key: "settings", icon: Settings }] },
];

export function Layout({ user, onLogout }: { user: User; onLogout: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  // le Journal est le seul écran pleine largeur (docs/specs/UX.md §3)
  const fullWidth = useRouterState({ select: (s) => s.location.pathname.startsWith("/journal") });
  const [drawer, setDrawer] = useState(false);
  const [palette, setPalette] = useState(false);
  const [theme, setTheme] = useState<ThemeChoice>(getTheme());
  const pending = useList<Execution>("/executions?status=pending_approval", { refetchInterval: 15000 });
  const pendingCount = pending.data?.length ?? 0;
  const proposalsPending = useList<{ id: string }>("/proposals?status=pending", { refetchInterval: 30000 });
  const proposalCount = proposalsPending.data?.length ?? 0;

  // première visite sur ce navigateur: reprendre le thème mémorisé côté serveur
  useEffect(() => {
    let local: string | null = null;
    try { local = localStorage.getItem("leassh-theme"); } catch { /* stockage indisponible */ }
    if (!local && (user.theme === "light" || user.theme === "dark")) { applyTheme(user.theme); setTheme(user.theme); }
  }, [user.theme]);

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPalette((p) => !p); }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  // raccourcis « g j » (journal) et « g v » (validations), hors champs de saisie
  useEffect(() => {
    let armed = 0;
    const h = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey || /^(INPUT|TEXTAREA|SELECT)$/.test((e.target as HTMLElement).tagName)) return;
      if (e.key === "g") { armed = Date.now(); return; }
      if (Date.now() - armed < 1000) {
        if (e.key === "j") void navigate({ to: "/journal" });
        if (e.key === "v") void navigate({ to: "/validations" });
      }
      armed = 0;
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [navigate]);

  const nav: ReactNode = (
    <nav aria-label="Navigation principale" className="flex flex-col gap-4">
      {sections.map((s, i) => (
        <div key={i} className="flex flex-col gap-0.5">
          {s.title && <p className="px-3 pb-1 text-[12px] uppercase tracking-wide text-muted">{t(`nav.${s.title}`)}</p>}
          {s.items.map(({ to, key, icon: Icon }) => (
            <Link key={to} to={to} onClick={() => setDrawer(false)} activeOptions={{ exact: to === "/" }}
              className="flex h-9 items-center gap-3 rounded-field px-3 text-muted hover:bg-surface-2 hover:text-fg"
              activeProps={{ className: "bg-surface-2 !text-fg font-medium", "aria-current": "page" }}>
              <Icon aria-hidden size={16} />{t(`nav.${key}`)}
              {key === "proposals" && proposalCount > 0 && <span aria-label={t("nav.pendingProposals", { count: proposalCount })} className="ml-auto rounded-full bg-write/15 px-2 text-[12px] font-medium text-write">{proposalCount}</span>}
              {key === "approvals" && pendingCount > 0 && <span aria-label={t("nav.pendingCount", { count: pendingCount })} className="ml-auto rounded-full bg-write/15 px-2 text-[12px] font-medium text-write">{pendingCount}</span>}
            </Link>
          ))}
        </div>
      ))}
    </nav>
  );

  return (
    <div className="min-h-screen">
      <a href="#main" className="sr-only focus:not-sr-only">{t("app.skip")}</a>
      <aside className={`fixed inset-y-0 left-0 z-30 w-60 overflow-y-auto border-r border-border bg-surface p-3 transition-transform md:translate-x-0 ${drawer ? "translate-x-0" : "-translate-x-full"}`}>
        <div className="mb-4 px-3 py-2 text-fg"><Logo className="h-9 w-auto" title={t("app.name")} /></div>
        {nav}
      </aside>
      {drawer && <div className="fixed inset-0 z-20 bg-black/40 md:hidden" onClick={() => setDrawer(false)} aria-hidden />}
      <div className="md:pl-60">
        <header className="sticky top-0 z-10 flex h-14 items-center gap-3 border-b border-border bg-surface px-4">
          <button className="md:hidden" aria-label={t("topbar.menu")} onClick={() => setDrawer(true)}><Menu aria-hidden size={20} /></button>
          <button onClick={() => setPalette(true)} className="flex h-9 flex-1 items-center gap-2 rounded-field border border-border-strong bg-surface-2 px-3 text-left text-muted md:max-w-md">
            <Search aria-hidden size={16} /><span className="flex-1">{t("topbar.search")}</span><kbd className="font-mono text-[12px]">Ctrl K</kbd>
          </button>
          <div className="ml-auto flex items-center gap-2">
            <label className="sr-only" htmlFor="theme">{t("topbar.theme")}</label>
            <select id="theme" value={theme} onChange={(e) => { const v = e.target.value as ThemeChoice; setTheme(v); applyTheme(v); void api("/auth/theme", { method: "PUT", body: { theme: v } }).catch(() => undefined); }}
              className="h-9 rounded-field border border-border-strong bg-surface px-2">
              <option value="system">{t("topbar.themeSystem")}</option>
              <option value="light">{t("topbar.themeLight")}</option>
              <option value="dark">{t("topbar.themeDark")}</option>
            </select>
            <span className="hidden sm:inline">{user.display_name}</span>
            <button onClick={onLogout} aria-label={t("topbar.logout")} title={t("topbar.logout")} className="grid h-9 w-9 place-items-center rounded-field text-muted hover:bg-surface-2"><LogOut aria-hidden size={16} /></button>
          </div>
        </header>
        <main id="main" className={`mx-auto p-4 md:p-8 ${fullWidth ? "" : "max-w-[1280px]"}`}><Outlet /></main>
      </div>
      <Toaster />
      <CommandPalette open={palette} onClose={() => setPalette(false)} />
    </div>
  );
}
