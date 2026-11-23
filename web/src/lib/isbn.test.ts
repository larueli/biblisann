import { describe, expect, it } from "vitest";
import { normalizeIsbn } from "./isbn";

describe("normalizeIsbn", () => {
  it.each([
    ["9780306406157", "9780306406157"],
    ["978-0-306-40615-7", "9780306406157"],
    ["ISBN 0-306-40615-2", "9780306406157"],
    ["080442957x", "9780804429573"],
    ["9791032703687", "9791032703687"],
  ])("accepts %s", (input, want) => expect(normalizeIsbn(input)).toBe(want));

  it.each(["", "123", "9780306406158", "3166480002407", "0306406153", "97803064061AB"])(
    "rejects %s",
    (input) => expect(normalizeIsbn(input)).toBeNull(),
  );
});
