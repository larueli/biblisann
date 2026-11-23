# 0018. Background jobs and notifications

Date: 2026-11-23
Status: Accepted

## Decision

- Jobs run on **River** (PostgreSQL queue), started inside the server process; River's own schema is migrated at startup under an advisory lock. Periodic jobs: loan reminders (hourly, also at start) and maintenance (expired sessions, old idempotency keys).
- **Reminders**: one day before the due date, on the due date, and when overdue (repeated every 7 days). The mark in `loan_notifications` and the notification job are written in one transaction, so a reminder is neither lost nor duplicated across replicas.
- **Delivery** fans out to each enabled channel of the owner: email (SMTP, TLS required unless `none`), Web Push (VAPID), webhook (JSON, HMAC-SHA256 signature `X-Biblisann-Signature`) and ntfy. One failing channel does not block the others; if every channel fails, the job fails and River retries with backoff.
- Messages are composed in the owner's language (`internal/i18n`, `en` and `fr`).
- A per-user **ICS feed** (`/v1/feeds/loans.ics?token=`) lists due dates. Its token only opens that feed, is stored hashed, and is revocable.
- Webhook secrets, ntfy tokens and push keys are stored in the database as channel configuration (not encrypted at rest): protect database access and backups.

## Consequences

- Without any configured channel nothing is delivered; the UI lists which kinds the server can send.
