import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";

export const useList = <T,>(path: string, opts: { refetchInterval?: number } = {}) =>
  useQuery({ queryKey: [path], queryFn: async () => (await api<{ items: T[] }>(path)).items, ...opts });

// Mutation qui invalide les listes concernées.
export function useApiMutation<In, Out = unknown>(method: string, path: string | ((v: In) => string), invalidate: string[]) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: In) => api<Out>(typeof path === "function" ? path(v) : path, { method, body: v }),
    onSuccess: () => Promise.all(invalidate.map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  });
}

export const errorMessage = (e: unknown): string => (e instanceof Error ? e.message : String(e));
