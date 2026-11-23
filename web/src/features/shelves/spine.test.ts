import { describe, expect, it } from "vitest";
import { spine } from "./spine";

describe("spine", () => {
  it("is deterministic", () => {
    expect(spine("a", "Dune", 412)).toEqual(spine("a", "Dune", 412));
  });
  it("is thicker for longer books, within bounds", () => {
    expect(spine("a", "x", 80).width).toBeLessThan(spine("a", "x", 600).width);
    expect(spine("a", "x", 1).width).toBe(16);
    expect(spine("a", "x", 5000).width).toBe(52);
  });
  it("falls back to a default thickness without a page count", () => {
    expect(spine("a", "x", undefined).width).toBe(28);
  });
  it("keeps height and hue in range", () => {
    for (const id of ["a", "b", "c", "d", "e", "f"]) {
      const s = spine(id, `title ${id}`, 200);
      expect(s.height).toBeGreaterThanOrEqual(120);
      expect(s.height).toBeLessThanOrEqual(176);
      expect(s.hue).toBeGreaterThanOrEqual(0);
      expect(s.hue).toBeLessThan(360);
    }
  });
});
