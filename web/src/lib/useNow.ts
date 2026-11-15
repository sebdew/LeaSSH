import { useEffect, useState } from "react";

// Horloge réactive : évite d'appeler Date.now() pendant le rendu (pureté) et rafraîchit l'affichage.
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const h = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(h);
  }, [intervalMs]);
  return now;
}
