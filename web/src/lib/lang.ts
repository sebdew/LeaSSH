import i18n from "../i18n";

export type Lang = "fr" | "en";
const KEY = "leassh-lang";

export const LANGS: { id: Lang; label: string }[] = [{ id: "fr", label: "Français" }, { id: "en", label: "English" }];

export function getLang(): Lang {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "fr" || v === "en") return v;
  } catch { /* stockage indisponible */ }
  return "fr"; // français par défaut
}

export function applyLang(l: Lang) {
  void i18n.changeLanguage(l);
  document.documentElement.lang = l;
  try { localStorage.setItem(KEY, l); } catch { /* ignoré */ }
}

// Langue courante de l'interface et paramètres régionaux associés (dates, nombres).
export const currentLang = (): Lang => (i18n.language === "en" ? "en" : "fr");
export const currentLocale = () => (currentLang() === "en" ? "en-GB" : "fr-FR");
// Décimale et pourcentage selon la langue: « 1,2 s » / « 1.2 s », « 95 % » / « 95%».
export const decimal = (s: string) => (currentLang() === "en" ? s : s.replace(".", ","));
export const percent = (v: number | string) => (currentLang() === "en" ? `${v}%` : `${String(v).replace(".", ",")} %`);
