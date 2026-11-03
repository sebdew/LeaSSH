export type ThemeChoice = "system" | "light" | "dark";
const KEY = "leash-theme";

export function getTheme(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "dark") return v;
  } catch { /* stockage indisponible */ }
  return "system";
}

export function applyTheme(t: ThemeChoice) {
  const el = document.documentElement;
  if (t === "system") delete el.dataset.theme;
  else el.dataset.theme = t;
  try { localStorage.setItem(KEY, t); } catch { /* ignoré */ }
}

// Force de mot de passe 0..3 (indicative; la règle serveur est 10 caractères minimum).
export function passwordStrength(pw: string): 0 | 1 | 2 | 3 {
  if (pw.length < 10) return 0;
  let classes = 0;
  for (const re of [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/]) if (re.test(pw)) classes++;
  if (pw.length >= 16 && classes >= 3) return 3;
  if (classes >= 3 || pw.length >= 14) return 2;
  return 1;
}
