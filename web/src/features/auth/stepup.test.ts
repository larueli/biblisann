import { describe, expect, it, vi } from "vitest";
import { requestStepUp, settleStepUp, subscribeStepUp } from "./stepup";

describe("step-up requests", () => {
  it("opens the dialog once for several waiting requests and settles all of them", async () => {
    const seen = vi.fn();
    const off = subscribeStepUp(seen);
    const a = requestStepUp();
    const b = requestStepUp();
    expect(seen).toHaveBeenCalledTimes(1);
    expect(seen).toHaveBeenLastCalledWith(true);
    settleStepUp(true);
    expect(await a).toBe(true);
    expect(await b).toBe(true);
    expect(seen).toHaveBeenLastCalledWith(false);
    off();
  });

  it("reports a cancellation", async () => {
    const p = requestStepUp();
    settleStepUp(false);
    expect(await p).toBe(false);
  });
});
