import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useListView } from "./lib/listview";

const items = [{ n: "Zèbre", v: 10 }, { n: "alpha", v: 2 }, { n: "Beta", v: 1 }];
const opts = { text: (x: { n: string }) => x.n, sorts: { n: (x: { n: string }) => x.n, v: (x: { v: number }) => x.v } };

describe("useListView", () => {
  it("recherche sans casse ni accents, tous les mots", () => {
    const { result } = renderHook(() => useListView(items, opts));
    act(() => result.current.setQuery("zebre"));
    expect(result.current.rows.map((r) => r.n)).toEqual(["Zèbre"]);
    act(() => result.current.setQuery("zebre alpha"));
    expect(result.current.rows).toEqual([]);
  });
  it("trie en alternant croissant, décroissant, sans tri (nombres numériquement)", () => {
    const { result } = renderHook(() => useListView(items, opts));
    act(() => result.current.toggle("v"));
    expect(result.current.rows.map((r) => r.v)).toEqual([1, 2, 10]);
    act(() => result.current.toggle("v"));
    expect(result.current.rows.map((r) => r.v)).toEqual([10, 2, 1]);
    act(() => result.current.toggle("v"));
    expect(result.current.rows.map((r) => r.n)).toEqual(["Zèbre", "alpha", "Beta"]);
    act(() => result.current.toggle("n"));
    expect(result.current.rows.map((r) => r.n)).toEqual(["alpha", "Beta", "Zèbre"]);
  });
});
