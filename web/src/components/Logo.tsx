import { useTranslation } from "react-i18next";

// Logo LeaSSH (PNG, fichiers de web/public). Deux variantes selon le thème choisi dans l'application
// (pas seulement celui du système) : la bascule est faite en CSS (.logo-clair / .logo-sombre dans styles.css).
// `compact` affiche seulement le symbole suivi du nom (barre latérale) ; sinon le logo complet avec baseline.
export function Logo({ className = "h-8", title, compact = false }: { className?: string; title?: string; compact?: boolean }) {
  const { i18n } = useTranslation();
  const lang = i18n.language?.startsWith("en") ? "en" : "fr";
  const label = title ?? "LeaSSH";
  if (compact) {
    return (
      <span className="inline-flex items-center gap-2" role="img" aria-label={label}>
        <img src="/symbole.png" alt="" className={className} />
        <span aria-hidden="true" className="text-xl font-bold tracking-tight">LeaSSH</span>
      </span>
    );
  }
  return (
    <span role="img" aria-label={label} className="inline-block">
      <img src={`/logo-clair-${lang}.png`} alt="" className={`logo-clair ${className}`} />
      <img src={`/logo-sombre-${lang}.png`} alt="" className={`logo-sombre ${className}`} />
    </span>
  );
}
