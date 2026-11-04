import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { Badge, Empty, ListSearch, SortTh, PageHeader, RiskBadge, ShowInactiveToggle, Tabs } from "../components/kit";
import { commandActive } from "../lib/active";
import { useListView } from "../lib/listview";
import { GroupsPanel } from "../components/GroupsPanel";
import { useList } from "../lib/queries";
import type { Command } from "../lib/types";

const RISK_ORDER: Record<string, number> = { read: 0, write: 1, destructive: 2 };

export function CommandsPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"commands" | "groups">("commands");
  const cmds = useList<Command>("/commands");
  const [showInactive, setShowInactive] = useState(false);
  const visible = (cmds.data ?? []).filter((c) => showInactive || commandActive(c));
  const view = useListView(visible, {
    text: (c) => `${c.slug} ${c.name} ${c.description} ${c.risk} ${c.status} ${c.source}`,
    sorts: { slug: (c) => c.slug, name: (c) => c.name, risk: (c) => RISK_ORDER[c.risk] ?? 9, status: (c) => c.status, source: (c) => c.source, version: (c) => c.version },
  });
  return (
    <>
      <PageHeader title={t("commands.title")} subtitle={t("commands.subtitle")}
        action={<Link to="/commandes/nouvelle" className="inline-flex h-9 items-center gap-2 rounded-field bg-primary px-4 font-medium text-primary-fg"><Plus aria-hidden size={16} />{t("commands.add")}</Link>} />
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "commands", label: t("commands.tabCommands") }, { id: "groups", label: t("commands.tabGroups") }]} />
      {tab === "commands" && <ShowInactiveToggle count={(cmds.data ?? []).filter((c) => !commandActive(c)).length} checked={showInactive} onChange={setShowInactive} />}
      {tab === "groups" ? <GroupsPanel kind="command" members={(cmds.data ?? []).map((c) => ({ id: c.id, label: c.slug, inactive: !commandActive(c) }))} /> : (
        cmds.data?.length === 0 ? <Empty text={t("commands.empty")} action={<Link to="/commandes/nouvelle" className="rounded-field bg-primary px-4 py-2 font-medium text-primary-fg">{t("commands.add")}</Link>} /> : (
          <>
          <ListSearch value={view.query} onChange={view.setQuery} label={t("commands.search")} count={view.rows.length} total={visible.length} />
          <div className="overflow-x-auto rounded-card border border-border bg-surface">
            <table className="w-full text-left text-[13px] tabular-nums">
              <thead className="border-b border-border text-muted"><tr>
                {["slug", "name", "risk", "status", "source", "version"].map((c) => <SortTh key={c} label={t(`commands.col.${c}`)} sortKey={c} sort={view.sort} onSort={view.toggle} />)}
              </tr></thead>
              <tbody>
                {view.rows.length === 0 && <tr><td colSpan={6} className="px-4 py-6 text-center text-muted">{t("common.noMatch")}</td></tr>}
                {view.rows.map((c) => (
                  <tr key={c.id} className="border-b border-border last:border-0 hover:bg-surface-2">
                    <td className="px-4 py-2"><Link to="/commandes/$id" params={{ id: c.id }} className="font-mono text-primary hover:underline">{c.slug}</Link></td>
                    <td className="px-4 py-2">{c.name}</td>
                    <td className="px-4 py-2"><RiskBadge risk={c.risk} /></td>
                    <td className="px-4 py-2"><Badge tone={c.status === "enabled" ? "read" : "muted"}>{t(`commands.status.${c.status}`)}</Badge></td>
                    <td className="px-4 py-2 text-muted">{t(`commands.source.${c.source}`)}</td>
                    <td className="px-4 py-2 text-muted">v{c.version}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          </>
        )
      )}
    </>
  );
}
