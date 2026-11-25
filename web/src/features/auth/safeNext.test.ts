import { describe, expect, it } from "vitest";
import { safeNext } from "./LoginPage";

describe("safeNext", () => {
  it("keeps same-origin paths", () => {
    expect(safeNext("/loans?x=1#a")).toBe(`${window.location.origin}/loans?x=1#a`);
  });
  it("keeps a normalized double-slash path on the original origin", () => {
    const next = safeNext("/a/..//example.org/path");
    expect(next).toBe(`${window.location.origin}//example.org/path`);
    expect(new URL(next ?? "").origin).toBe(window.location.origin);
  });
  it.each([
    "//evil.example",
    "/\\evil.example",
    "/\t/evil.example",
    "/\n/evil.example",
    "/\r/evil.example",
    "https://evil.example",
    "evil",
    "",
    undefined,
  ])("refuses %j", (v) => {
    expect(safeNext(v)).toBeNull();
  });
});
