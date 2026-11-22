# 0003. PostgreSQL is the only required service; Valkey optional

Date: 2026-11-22
Status: Accepted

## Context

The project must run on a bare system as well as on Kubernetes. Each mandatory service raises the cost of a small install.

## Decision

- PostgreSQL stores data, sessions, the job queue (`SKIP LOCKED`, River) and the search index (FTS, `pg_trgm`, `unaccent`).
- Valkey is optional and only used for multi-replica rate limiting and caching.
- Migrations are forward-only, run under an advisory lock (or a Helm pre-upgrade Job), and follow expand/contract.

## Consequences

- Single-node installs need app + PostgreSQL only.
- HA needs several app replicas, S3 for covers (local files would need shared storage), and optionally Valkey.
- A feature that seems to require Valkey must first be justified in a new ADR.
