import { api, type Schemas } from "../api/client";
import { type BiblisannDB, db as defaultDb, type OutboxItem } from "./db";
import { waitBeforeNext } from "./pacing";

/** Downloads index changes for a library until caught up. Returns how many entries were applied. */
export async function syncIndex(libraryId: string, db: BiblisannDB = defaultDb): Promise<number> {
  const meta = await db.meta.get(libraryId);
  let cursor = meta?.cursor ?? 0;
  let applied = 0;
  for (;;) {
    const { data, error } = await api.GET("/v1/libraries/{id}/sync", {
      params: { path: { id: libraryId }, query: { cursor, limit: 200 } },
    });
    if (error || !data) throw new Error("sync failed");
    await db.transaction("rw", db.index, db.meta, async () => {
      if (data.reset) await db.index.where("libraryId").equals(libraryId).delete(); // history was compacted past our cursor
      for (const e of data.entries) {
        if (e.deleted) {
          await db.index.delete(e.id);
        } else {
          await db.index.put({
            id: e.id,
            libraryId,
            editionId: e.edition_id ?? "",
            workId: e.work_id ?? "",
            title: e.title ?? "",
            authors: e.authors ?? [],
            isbn13: e.isbn13 ?? null,
            placeId: e.place_id ?? null,
          });
        }
      }
      await db.meta.put({ libraryId, cursor: data.cursor, syncedAt: Date.now() });
    });
    applied += data.entries.length;
    cursor = data.cursor;
    if (!data.has_more) return applied;
  }
}

/** Answers "do I own this?" from the local index (works offline). */
export async function ownedLocally(isbn13: string, db: BiblisannDB = defaultDb) {
  const same = await db.index.where("isbn13").equals(isbn13).toArray();
  return same;
}

/** Queues a scan. It survives reloads and is sent by flushOutbox when the network allows. */
export async function enqueueScan(
  libraryId: string,
  isbn13: string,
  allowDuplicate = false,
  db: BiblisannDB = defaultDb,
  placeId?: string,
): Promise<OutboxItem> {
  const item: OutboxItem = {
    key: crypto.randomUUID(),
    libraryId,
    isbn13,
    allowDuplicate,
    ...(placeId ? { placeId } : {}),
    status: "pending",
    createdAt: Date.now(),
  };
  await db.outbox.add(item);
  return item;
}

type ScanResult = Schemas["ScanResult"];

/**
 * Sends pending scans in order. A network failure leaves the item pending (it is retried with
 * the same key); a refusal from the server marks it failed. Returns the number sent.
 */
export interface FlushOptions {
  /** Replaces the real timer (tests). */
  sleep?: (ms: number) => Promise<void>;
  /** Told how long the flush pauses to respect the server quota (0 when it resumes). */
  onWait?: (ms: number) => void;
}

const realSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

export async function flushOutbox(
  db: BiblisannDB = defaultDb,
  options: FlushOptions = {},
): Promise<number> {
  const sleep = options.sleep ?? realSleep;
  const pause = async (ms: number) => {
    if (ms <= 0) return;
    options.onWait?.(ms);
    await sleep(ms);
    options.onWait?.(0);
  };
  const items = await db.outbox.where("status").anyOf("pending", "sending").sortBy("createdAt");
  let sent = 0;
  for (const item of items) {
    await db.outbox.update(item.key, { status: "sending" });
    try {
      let attempt = 0;
      let result = await sendScan(item);
      // A 429 is not a refusal: wait as told and send the same item again (same key).
      while (result.response.status === 429 && attempt < 5) {
        attempt++;
        await pause(waitBeforeNext(result.response));
        result = await sendScan(item);
      }
      const { data, error, response } = result;
      await pause(waitBeforeNext(response)); // slow down before the next one when the quota is nearly spent
      if (response.status === 429) {
        await db.outbox.update(item.key, { status: "pending" });
        return sent;
      }
      if (data) {
        const r: ScanResult = data;
        await db.outbox.update(item.key, {
          status: r.status,
          ...(r.edition?.title ? { title: r.edition.title } : {}),
        });
        sent++;
      } else if (response.status === 401 || response.status >= 500) {
        await db.outbox.update(item.key, { status: "pending" }); // retry later
        return sent;
      } else {
        await db.outbox.update(item.key, {
          status: "failed",
          error:
            typeof error === "object" && error && "detail" in error
              ? String(error.detail)
              : "refused",
        });
      }
    } catch {
      await db.outbox.update(item.key, { status: "pending" }); // offline
      return sent;
    }
  }
  return sent;
}

function sendScan(item: OutboxItem) {
  return api.POST("/v1/libraries/{id}/scan", {
    params: { path: { id: item.libraryId }, header: { "Idempotency-Key": item.key } },
    body: {
      isbn: item.isbn13,
      allow_duplicate: item.allowDuplicate,
      ...(item.placeId ? { place_id: item.placeId } : {}),
    },
  });
}
