import { afterEach, describe, expect, it, vi } from "vitest";
import { settleStepUp, subscribeStepUp } from "../features/auth/stepup";
import { requestBackup, setCsrfToken } from "./client";

describe("backup download", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setCsrfToken("");
  });

  it("posts with CSRF and retries after a step-up", async () => {
    const calls: RequestInit[] = [];
    setCsrfToken("test-csrf");
    vi.stubGlobal("fetch", async (_url: string, init: RequestInit) => {
      calls.push(init);
      return calls.length === 1
        ? Response.json({ detail: "step_up_required" }, { status: 403 })
        : new Response("archive", { status: 200 });
    });
    const unsubscribe = subscribeStepUp((open) => {
      if (open) settleStepUp(true);
    });
    try {
      const response = await requestBackup();
      expect(await response.text()).toBe("archive");
      expect(calls).toHaveLength(2);
      expect(calls[0]?.method).toBe("POST");
      expect(new Headers(calls[0]?.headers).get("X-CSRF-Token")).toBe("test-csrf");
    } finally {
      unsubscribe();
    }
  });

  it("does not retry an unrelated refusal", async () => {
    const fetcher = vi.fn(async () => Response.json({ detail: "forbidden" }, { status: 403 }));
    vi.stubGlobal("fetch", fetcher);
    await expect(requestBackup()).rejects.toThrow("backup request failed");
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});
