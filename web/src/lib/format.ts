import { decimal } from "./lang";

export const fmtDate = (iso: string) => {
  if (!iso) return "—";
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getDate())}/${p(d.getMonth() + 1)} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
};

export const fmtDuration = (ms: number | null | undefined) => (ms == null ? "—" : `${decimal((ms / 1000).toFixed(1))} s`);

export const initials = (name: string) => {
  const parts = name.trim().split(/[\s._-]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "?") + (parts[1]?.[0] ?? "")).toUpperCase();
};

// Durée lisible: 320 ms, 1,2 s, 1 min 12 s.
export const fmtMs = (ms: number) => {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const s = ms / 1000;
  if (s < 60) return `${decimal(s.toFixed(1))} s`;
  return fmtSeconds(Math.round(s));
};

export const fmtSeconds = (sec: number) => {
  if (sec < 60) return `${sec} s`;
  const m = Math.floor(sec / 60);
  if (m < 60) return sec % 60 === 0 ? `${m} min` : `${m} min ${sec % 60} s`;
  const h = Math.floor(m / 60);
  return m % 60 === 0 ? `${h} h` : `${h} h ${m % 60} min`;
};
