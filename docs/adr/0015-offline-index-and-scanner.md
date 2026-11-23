# 0015. Offline index, scan outbox and barcode scanning

Date: 2026-11-23
Status: Accepted (scanner quality not yet validated on a real device)

## Context

Spec section 5: answer "do I own this?" offline in a shop, and keep scanning when the network drops.

## Decision

- **Index sync**: every instance write appends a row to `changes` (same transaction). `GET /v1/libraries/{id}/sync?cursor=N` returns the distinct instances touched after N with their current compact data, or a tombstone (`deleted: true`) when deleted. The client stores the returned cursor in IndexedDB (Dexie) and loops while `has_more`. Cursor 0 is a full download.
- **Outbox**: scans are stored locally with a random UUID used as `Idempotency-Key`, then replayed in order. A network error or 5xx keeps the item pending (same key on retry); a 4xx marks it failed. The server answers a replayed key with the first result (`library.ReplayedInstance`), even though the first call changed what is owned.
- **Offline ownership** is by ISBN against the local index. Work-level matching needs the server (the index does not hold editions that are not owned).
- **Scanner**: native `BarcodeDetector` when present, otherwise the `barcode-detector` ponyfill with its WASM decoder bundled and served by the app (`setZXingModuleOverrides`), so the CSP does not need `connect-src` to a CDN. Formats: EAN-13 only (ISBN-13 barcodes). A manual ISBN field is always offered.

## Spike result

Not run on a real Android phone against real book backs in this session (no device available). The code path is implemented and unit tested; the first real-world check is part of the user's use of the PWA. If decoding is poor, options are: restrict the camera to higher resolution, scan ISBN-10 `ean_8`/`upc` variants, or wrap the app with Capacitor (spec allows it). Record the outcome here.

## Consequences

- The index grows with changes, not with time; compaction of old `changes` rows is a later task (tombstones must outlive the slowest client).
