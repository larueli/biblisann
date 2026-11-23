import Dexie, { type Table } from "dexie";

/** One row of the compact offline index (a copy I own). */
export interface IndexRow {
  id: string; // instance id
  libraryId: string;
  editionId: string;
  workId: string;
  title: string;
  authors: string[];
  isbn13: string | null;
  placeId: string | null;
}

export interface SyncMeta {
  libraryId: string;
  cursor: number;
  syncedAt: number;
}

export type OutboxStatus =
  | "pending"
  | "sending"
  | "added"
  | "already_owned"
  | "not_found"
  | "failed";

/** A scan waiting to be replayed. The key makes the replay idempotent on the server. */
export interface OutboxItem {
  key: string;
  libraryId: string;
  isbn13: string;
  allowDuplicate: boolean;
  placeId?: string;
  status: OutboxStatus;
  createdAt: number;
  error?: string;
  title?: string;
}

export class BiblisannDB extends Dexie {
  index!: Table<IndexRow, string>;
  meta!: Table<SyncMeta, string>;
  outbox!: Table<OutboxItem, string>;

  constructor(name = "biblisann") {
    super(name);
    this.version(1).stores({
      index: "id, libraryId, isbn13, workId",
      meta: "libraryId",
      outbox: "key, status, createdAt",
    });
  }
}

export const db = new BiblisannDB();
