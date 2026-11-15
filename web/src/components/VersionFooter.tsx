import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowUpCircle, ExternalLink } from "lucide-react";
import { api } from "../lib/api";

interface Meta { version?: string; author?: string; repository_url?: string; update_available?: boolean; latest_version?: string; release_url?: string }

// Version en cours d'exécution, auteur et lien vers le dépôt (ticket #43).
export function VersionFooter() {
  const { t } = useTranslation();
  const meta = useQuery({ queryKey: ["meta"], queryFn: () => api<Meta>("/meta"), staleTime: Infinity });
  const m = meta.data;
  if (!m?.version) return null;
  return (
    <footer className="mt-6 border-t border-border px-3 pt-3 text-[12px] text-muted">
      <p className="font-mono" data-testid="app-version">{t("about.version", { version: m.version })}</p>
      {m.update_available && m.release_url && (
        <a href={m.release_url} target="_blank" rel="noopener noreferrer" className="my-1 flex items-center gap-1 rounded-field bg-write/15 px-2 py-1 font-medium text-write">
          <ArrowUpCircle aria-hidden size={14} />{t("about.updateAvailable", { version: m.latest_version })}
        </a>
      )}
      {m.author && <p>{t("about.by", { author: m.author })}</p>}
      {m.repository_url && (
        <a href={m.repository_url} target="_blank" rel="noopener noreferrer" className="mt-1 inline-flex items-center gap-1 underline hover:text-fg">
          GitHub<ExternalLink aria-hidden size={12} /><span className="sr-only">{t("about.newTab")}</span>
        </a>
      )}
    </footer>
  );
}
