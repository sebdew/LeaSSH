import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Check, Hand, X } from "lucide-react";
import { Empty, PageHeader, Select } from "../components/kit";
import { api } from "../lib/api";
import { useList } from "../lib/queries";
import { keyActive } from "../lib/active";
import type { Cell, McpKey, SshServer } from "../lib/types";

// Cellule: ✔ direct / ✋ validation / ✖ refusé, jamais par la couleur seule.
export function CellView({ cell }: { cell: Cell }) {
  const { t } = useTranslation();
  const reason = cell.allowed ? t("simulator.allowedBy", { rule: cell.rule }) : t("simulator.denied", { reason: t(`simulator.reasons.${cell.reason}`, { defaultValue: cell.reason }) });
  const label = !cell.allowed ? t("simulator.no") : cell.requires_approval ? t("simulator.approval") : t("simulator.direct");
  const icon = !cell.allowed ? <X aria-hidden size={18} strokeWidth={2.5} className="text-destructive" /> : cell.requires_approval ? <Hand aria-hidden size={16} className="text-write" /> : <Check aria-hidden size={18} strokeWidth={2.5} className="text-read" />;
  return <span title={reason} aria-label={`${label}: ${reason}`} className="inline-flex items-center gap-1">{icon}<span className="sr-only">{label}</span></span>;
}

export function SimulatorPage() {
  const { t } = useTranslation();
  const keys = useList<McpKey>("/keys");
  const servers = useList<SshServer>("/servers");
  const [key, setKey] = useState("");
  const [server, setServer] = useState("");
  const sim = useQuery({
    queryKey: ["simulator", key, server], enabled: !!key,
    queryFn: () => api<{ cells: Cell[] }>(`/simulator?key=${encodeURIComponent(key)}${server ? `&server=${encodeURIComponent(server)}` : ""}`),
  });
  const cells = sim.data?.cells ?? [];
  const cmds = [...new Set(cells.map((c) => c.command))];
  const srvs = [...new Set(cells.map((c) => c.server))];
  const at = (s: string, c: string) => cells.find((x) => x.server === s && x.command === c);
  return (
    <>
      <PageHeader title={t("simulator.title")} subtitle={t("simulator.subtitle")} />
      <div className="mb-4 grid gap-3 sm:grid-cols-2 md:max-w-xl">
        <Select id="sim-key" label={t("simulator.key")} value={key} onChange={(e) => setKey(e.target.value)}>
          <option value="">—</option>{keys.data?.filter(keyActive).map((k) => <option key={k.id} value={k.id}>{k.name}</option>)}
        </Select>
        <Select id="sim-server" label={t("simulator.server")} value={server} onChange={(e) => setServer(e.target.value)}>
          <option value="">{t("simulator.allServers")}</option>{servers.data?.map((s) => <option key={s.id} value={s.slug}>{s.slug}</option>)}
        </Select>
      </div>
      <p className="mb-3 flex flex-wrap gap-4 text-muted">
        <span className="inline-flex items-center gap-1"><Check aria-hidden size={14} className="text-read" />{t("simulator.direct")}</span>
        <span className="inline-flex items-center gap-1"><Hand aria-hidden size={14} className="text-write" />{t("simulator.approval")}</span>
        <span className="inline-flex items-center gap-1"><X aria-hidden size={14} strokeWidth={2.5} className="text-destructive" />{t("simulator.no")}</span>
      </p>
      {!key ? <Empty text={t("simulator.pick")} /> : cmds.length === 0 ? <Empty text={t("simulator.noData")} /> : (
        <div className="overflow-x-auto rounded-card border border-border bg-surface">
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-muted"><tr>
              <th scope="col" className="px-4 py-2 font-medium">{t("simulator.command")}</th>
              {srvs.map((s) => <th key={s} scope="col" className="px-4 py-2 font-mono font-medium">{s}</th>)}
            </tr></thead>
            <tbody>
              {cmds.map((c) => (
                <tr key={c} className="border-b border-border last:border-0">
                  <th scope="row" className="px-4 py-2 text-left font-mono font-normal">{c}</th>
                  {srvs.map((s) => <td key={s} className="px-4 py-2">{at(s, c) && <CellView cell={at(s, c)!} />}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
