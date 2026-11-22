# 0008. Work / Edition / Instance data model with a shared catalog

Date: 2026-11-22
Status: Accepted

## Context

Users can own the same edition twice, own different editions of one work, and sometimes own books without an ISBN. "Do I already have this?" must be answered offline and across editions.

## Decision

- Three levels: Work, Edition (ISBN or other identifier), Instance (physical or ebook copy in a library).
- Work and Edition rows form a global catalog shared by all libraries; provider data is stored with provenance. Per-user and per-library data (notes, grades, state, location, loans) is separate.
- Identifiers are UUIDv7, generated client-side when needed so offline writes are idempotent.
- User edits override provider values per field and are lockable.

## Consequences

- Deduplication and Work matching become a core, testable domain service.
- Moving a book across libraries moves an Instance, never catalog rows.
- Deleting a catalog row is an admin operation.
