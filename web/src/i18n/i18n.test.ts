import { describe, expect, it } from "vitest";
import en from "./locales/en.json";
import fr from "./locales/fr.json";

function keys(obj: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    typeof v === "object" && v !== null
      ? keys(v as Record<string, unknown>, `${prefix}${k}.`)
      : [`${prefix}${k}`],
  );
}

describe("locales", () => {
  it("en and fr define exactly the same keys", () => {
    expect(keys(fr).sort()).toEqual(keys(en).sort());
  });

  it("keeps interpolation placeholders in sync", () => {
    const placeholders = (s: string) => (s.match(/{{\w+}}/g) ?? []).sort();
    const flat = (o: Record<string, unknown>) =>
      Object.fromEntries(
        keys(o).map((k) => [
          k,
          k.split(".").reduce<unknown>((a, p) => (a as Record<string, unknown>)[p], o) as string,
        ]),
      );
    const e = flat(en);
    const f = flat(fr);
    for (const k of Object.keys(e)) {
      expect(placeholders(f[k] ?? "")).toEqual(placeholders(e[k] ?? ""));
    }
  });
});
