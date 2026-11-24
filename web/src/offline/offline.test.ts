import "fake-indexeddb/auto";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BiblisannDB } from "./db";
import { enqueueScan, flushOutbox, ownedLocally, syncIndex } from "./sync";

let db: BiblisannDB;
beforeEach(async () => {
  db = new BiblisannDB(`test-${Math.random()}`);
  await db.open();
});

function jsonResponse(body: unknown, status = 200) {
  return Response.json(body, { status });
}

describe("syncIndex", () => {
  it("applies upserts and tombstones and stores the cursor", async () => {
    const pages = [
      {
        cursor: 2,
        has_more: true,
        entries: [
          {
            id: "a",
            edition_id: "e1",
            work_id: "w1",
            title: "A",
            authors: ["X"],
            isbn13: "9780306406157",
            deleted: false,
          },
          { id: "b", edition_id: "e2", work_id: "w2", title: "B", authors: [], deleted: false },
        ],
      },
      { cursor: 3, has_more: false, entries: [{ id: "a", deleted: true }] },
    ];
    const urls: string[] = [];
    vi.stubGlobal("fetch", async (req: Request) => {
      urls.push(req.url);
      return jsonResponse(pages.shift());
    });
    const n = await syncIndex("lib1", db);
    expect(n).toBe(3);
    expect(urls[0]).toContain("cursor=0");
    expect(urls[1]).toContain("cursor=2");
    expect(await db.index.count()).toBe(1);
    expect((await db.meta.get("lib1"))?.cursor).toBe(3);
    expect(await ownedLocally("9780306406157", db)).toHaveLength(0);
  });
});

describe("outbox", () => {
  it("sends with the idempotency key and records the result", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    let key = "";
    vi.stubGlobal("fetch", async (req: Request) => {
      key = req.headers.get("Idempotency-Key") ?? "";
      return jsonResponse({ status: "added", edition: { title: "Great Book" } });
    });
    expect(await flushOutbox(db)).toBe(1);
    expect(key).toBe(item.key);
    const stored = await db.outbox.get(item.key);
    expect(stored?.status).toBe("added");
    expect(stored?.title).toBe("Great Book");
  });

  it("keeps the item pending when offline, then retries with the same key", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    vi.stubGlobal("fetch", async () => {
      throw new TypeError("offline");
    });
    expect(await flushOutbox(db)).toBe(0);
    expect((await db.outbox.get(item.key))?.status).toBe("pending");
    vi.stubGlobal("fetch", async () => jsonResponse({ status: "already_owned" }));
    expect(await flushOutbox(db)).toBe(1);
    expect((await db.outbox.get(item.key))?.status).toBe("already_owned");
  });

  it("marks a refused scan as failed and does not retry it", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    vi.stubGlobal("fetch", async () => jsonResponse({ detail: "not found", status: 404 }, 404));
    await flushOutbox(db);
    expect((await db.outbox.get(item.key))?.status).toBe("failed");
  });
});

describe("flushOutbox pacing", () => {
  it("waits as told on a 429 and sends the same item again", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    let calls = 0;
    const keys: string[] = [];
    vi.stubGlobal("fetch", async (req: Request) => {
      calls++;
      keys.push(req.headers.get("Idempotency-Key") ?? "");
      if (calls === 1) {
        return new Response(JSON.stringify({ detail: "slow down" }), {
          status: 429,
          headers: { "Content-Type": "application/problem+json", "Retry-After": "3" },
        });
      }
      return jsonResponse({ status: "added" });
    });
    const waits: number[] = [];
    const sent = await flushOutbox(db, {
      sleep: async () => undefined,
      onWait: (ms) => waits.push(ms),
    });
    expect(sent).toBe(1);
    expect(keys).toEqual([item.key, item.key]);
    expect(waits[0]).toBe(3000);
    expect((await db.outbox.get(item.key))?.status).toBe("added");
  });
});
