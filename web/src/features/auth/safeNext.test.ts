import { describe, expect, it } from "vitest";
import { safeNext } from "./LoginPage";

describe("safeNext", () => {
  it("keeps same-origin paths", () => {
    expect(safeNext("/loans?x=1#a")).toBe("/loans?x=1#a");
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
