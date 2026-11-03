import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import fr from "./locales/fr.json";
import en from "./locales/en.json";

const stored = (() => { try { return localStorage.getItem("leash-lang"); } catch { return null; } })();
const initial = stored === "en" ? "en" : "fr";
if (typeof document !== "undefined") document.documentElement.lang = initial;

// Français par défaut; le choix de langue est mémorisé dans ce navigateur (lib/lang.ts).
void i18n.use(initReactI18next).init({
  resources: { fr: { translation: fr }, en: { translation: en } },
  lng: initial,
  fallbackLng: "fr",
  interpolation: { escapeValue: false },
});

export default i18n;
