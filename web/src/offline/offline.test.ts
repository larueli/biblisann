import "fake-indexeddb/auto";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BiblisannDB, dbForUser } from "./db";
import { enqueueScan, flushOutbox, ownedLocally, reconcileLibraries, syncIndex } from "./sync";

let db: BiblisannDB;
beforeEach(async () => {
  db = new BiblisannDB(`test-${Math.random()}`, "user1");
  await db.open();
});

function jsonResponse(body: unknown, status = 200) {
  return Response.json(body, { status });
}

function stubFetch(handler: (req: Request) => Promise<Response>) {
  vi.stubGlobal("fetch", async (req: Request) => {
    if (new URL(req.url).pathname === "/v1/auth/session") {
      return jsonResponse({ user: { id: "user1" }, csrf_token: "session-csrf" });
    }
    return handler(req);
  });
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
    stubFetch(async (req: Request) => {
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
    let csrf = "";
    stubFetch(async (req: Request) => {
      key = req.headers.get("Idempotency-Key") ?? "";
      csrf = req.headers.get("X-CSRF-Token") ?? "";
      return jsonResponse({ status: "added", edition: { title: "Great Book" } });
    });
    expect(await flushOutbox(db)).toBe(1);
    expect(key).toBe(item.key);
    expect(csrf).toBe("session-csrf");
    const stored = await db.outbox.get(item.key);
    expect(stored?.status).toBe("added");
    expect(stored?.title).toBe("Great Book");
  });

  it("keeps the item pending when offline, then retries with the same key", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    stubFetch(async () => {
      throw new TypeError("offline");
    });
    expect(await flushOutbox(db)).toBe(0);
    expect((await db.outbox.get(item.key))?.status).toBe("pending");
    stubFetch(async () => jsonResponse({ status: "already_owned" }));
    expect(await flushOutbox(db)).toBe(1);
    expect((await db.outbox.get(item.key))?.status).toBe("already_owned");
  });

  it("marks a refused scan as failed and does not retry it", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    stubFetch(async () => jsonResponse({ detail: "invalid", status: 422 }, 422));
    await flushOutbox(db);
    expect((await db.outbox.get(item.key))?.status).toBe("failed");
  });
});

describe("flushOutbox pacing", () => {
  it("waits as told on a 429 and sends the same item again", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    let calls = 0;
    const keys: string[] = [];
    stubFetch(async (req: Request) => {
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

describe("account isolation", () => {
  it("binds sync to the verified session and discards data if that session changes", async () => {
    let sessions = 0;
    let csrf = "";
    vi.stubGlobal("fetch", async (req: Request) => {
      if (new URL(req.url).pathname === "/v1/auth/session") {
        sessions++;
        return jsonResponse({ user: { id: "user1" }, csrf_token: sessions === 1 ? "old" : "new" });
      }
      csrf = req.headers.get("X-CSRF-Token") ?? "";
      return jsonResponse({ cursor: 1, has_more: false, entries: [] });
    });
    await expect(syncIndex("lib1", db)).rejects.toThrow("session changed");
    expect(csrf).toBe("old");
    expect(await db.meta.count()).toBe(0);
  });

  it("removes a revoked library's cached index but keeps pending scans", async () => {
    await db.meta.put({ libraryId: "lib1", cursor: 2, syncedAt: 0 });
    await db.index.put({
      id: "one",
      libraryId: "lib1",
      editionId: "e",
      workId: "w",
      title: "Book",
      authors: [],
      isbn13: null,
      placeId: null,
    });
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    stubFetch(async () => jsonResponse({ detail: "not found" }, 404));
    await reconcileLibraries(db);
    expect(await db.meta.count()).toBe(0);
    expect(await db.index.count()).toBe(0);
    expect((await db.outbox.get(item.key))?.status).toBe("pending");
    await flushOutbox(db);
    expect((await db.outbox.get(item.key))?.status).toBe("pending");
  });

  it("does not replay a previous account's pending scans under a different session", async () => {
    const item = await enqueueScan("lib1", "9780306406157", false, db);
    let writes = 0;
    vi.stubGlobal("fetch", async (req: Request) => {
      if (new URL(req.url).pathname === "/v1/auth/session") {
        return jsonResponse({ user: { id: "user2" }, csrf_token: "other-csrf" });
      }
      writes++;
      return jsonResponse({ status: "added" });
    });
    expect(await flushOutbox(db)).toBe(0);
    expect(writes).toBe(0);
    expect((await db.outbox.get(item.key))?.status).toBe("pending");
  });

  it("keeps indexes and outboxes separate for different accounts", async () => {
    const suffix = String(Math.random());
    const first = dbForUser(`user1-${suffix}`);
    const other = dbForUser(`user2-${suffix}`);
    await enqueueScan("lib1", "9780306406157", false, first);
    expect(await other.outbox.count()).toBe(0);
    await first.delete();
    await other.delete();
  });
});
