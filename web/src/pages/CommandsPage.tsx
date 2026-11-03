import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { Badge, Empty, PageHeader, RiskBadge, ShowInactiveToggle, Tabs } from "../components/kit";
import { commandActive } from "../lib/active";
import { GroupsPanel } from "../components/GroupsPanel";
import { useList } from "../lib/queries";
import type { Command } from "../lib/types";

export function CommandsPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"commands" | "groups">("commands");
  const cmds = useList<Command>("/commands");
  const [showInactive, setShowInactive] = useState(false);
  return (
    <>
      <PageHeader title={t("commands.title")} subtitle={t("commands.subtitle")}
        action={<Link to="/commandes/nouvelle" className="inline-flex h-9 items-center gap-2 rounded-field bg-primary px-4 font-medium text-primary-fg"><Plus aria-hidden size={16} />{t("commands.add")}</Link>} />
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "commands", label: t("commands.tabCommands") }, { id: "groups", label: t("commands.tabGroups") }]} />
      {tab === "commands" && <ShowInactiveToggle count={(cmds.data ?? []).filter((c) => !commandActive(c)).length} checked={showInactive} onChange={setShowInactive} />}
      {tab === "groups" ? <GroupsPanel kind="command" members={(cmds.data ?? []).map((c) => ({ id: c.id, label: c.slug, inactive: !commandActive(c) }))} /> : (
        cmds.data?.length === 0 ? <Empty text={t("commands.empty")} action={<Link to="/commandes/nouvelle" className="rounded-field bg-primary px-4 py-2 font-medium text-primary-fg">{t("commands.add")}</Link>} /> : (
          <div className="overflow-x-auto rounded-card border border-border bg-surface">
            <table className="w-full text-left text-[13px] tabular-nums">
              <thead className="border-b border-border text-muted"><tr>
                {["slug", "name", "risk", "status", "source", "version"].map((c) => <th key={c} scope="col" className="px-4 py-2 font-medium">{t(`commands.col.${c}`)}</th>)}
              </tr></thead>
              <tbody>
                {cmds.data?.filter((c) => showInactive || commandActive(c)).map((c) => (
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
        )
      )}
    </>
  );
}
