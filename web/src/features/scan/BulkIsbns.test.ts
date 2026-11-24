import { describe, expect, it } from "vitest";
import { parseIsbnList } from "./BulkIsbns";

describe("parseIsbnList", () => {
  it("accepts separators, ISBN-10, hyphens, and drops duplicates and garbage", () => {
    const { valid, invalid } = parseIsbnList(
      "978-0-306-40615-7, 0306406152\n9780306406157; nope\t123",
    );
    expect(valid).toEqual(["9780306406157"]);
    expect(invalid).toEqual(["nope", "123"]);
  });
});
