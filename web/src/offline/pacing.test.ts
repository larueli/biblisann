import { describe, expect, it } from "vitest";
import { waitBeforeNext } from "./pacing";

const res = (status: number, headers: Record<string, string>) =>
  new Response(null, { status, headers });

describe("waitBeforeNext", () => {
  it("does not wait while the quota has room or is unknown", () => {
    expect(
      waitBeforeNext(
        res(200, { RateLimit: '"default";r=40;t=10', "RateLimit-Policy": '"default";q=600;w=60' }),
      ),
    ).toBe(0);
    expect(waitBeforeNext(res(200, {}))).toBe(0);
  });
  it("waits for the tokens a scan needs to refill", () => {
    // 2 tokens left, a scan needs 5, 600 tokens per 60 s refill 10 per second: 0.3 s
    expect(
      waitBeforeNext(
        res(200, { RateLimit: '"default";r=2;t=10', "RateLimit-Policy": '"default";q=600;w=60' }),
      ),
    ).toBe(300);
  });
  it("obeys Retry-After on a 429", () => {
    expect(waitBeforeNext(res(429, { "Retry-After": "7" }))).toBe(7000);
    expect(waitBeforeNext(res(429, {}))).toBe(5000);
  });
});
