import { useEffect, useState } from "react";

const EVT = "leash-toast";
export const toast = (message: string) => window.dispatchEvent(new CustomEvent(EVT, { detail: message }));

// Confirmations seulement: les erreurs importantes restent affichées dans la page (docs/UX.md §4.4).
export function Toaster() {
  const [items, setItems] = useState<{ id: number; text: string }[]>([]);
  useEffect(() => {
    let n = 0;
    const h = (e: Event) => {
      const id = ++n;
      setItems((l) => [...l, { id, text: (e as CustomEvent<string>).detail }]);
      setTimeout(() => setItems((l) => l.filter((x) => x.id !== id)), 4000);
    };
    window.addEventListener(EVT, h);
    return () => window.removeEventListener(EVT, h);
  }, []);
  return (
    <div role="status" aria-live="polite" className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {items.map((i) => <div key={i.id} className="rounded-field border border-border-strong bg-surface px-4 py-2 shadow-[var(--shadow)]">{i.text}</div>)}
    </div>
  );
}
