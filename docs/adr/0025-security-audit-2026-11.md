# 0025. Security audit of November 2026

Date: 2026-11-23
Status: Accepted

Four independent read-only reviews covered authentication and authorization, untrusted input and
outbound requests, the web app and deployment assets, and business logic and tenant isolation.
Nothing allowed unauthenticated access.

## Fixed

- **A member who may only share a library could make themselves administrator.** Sharers can now hand out ordinary access only: no admin role, no profile with share or admin, nobody who already holds elevated rights, never themselves. Group shares need an administrator.
- **Garbage collection could fail forever** once a lent copy was purged (a CHECK conflicted with ON DELETE SET NULL). Migration 00014 relaxes it; every GC step now runs independently.
- **Token library restrictions** are honoured by "do I own this?" and by loans; the read permission is checked per library.
- **Rate limits**: email keys are trimmed like the lookup, IPv6 clients are keyed by /64.
- **Credentials**: a password reset now also revokes every token kind and OAuth grant; re-enrolling TOTP no longer switches a confirmed factor off; tokens can no longer list or revoke tokens, manage notification channels or groups (sessions only); feed and SCIM tokens ignore library restrictions instead of storing them.
- **SCIM** no longer touches instance administrators; OIDC groups never join an administrator's same-named local group.
- **OPDS feed tokens** are no longer written to access logs or traces.
- **Web**: post-login redirect rejects control characters (open redirect via a tab), the consent screen shows where the browser goes, grants no more than requested and defaults write to what was asked, logout and account deletion wipe the offline database and caches, CSP allows WebAssembly for the scanner, no directory listings, Helm refuses an empty NetworkPolicy allow-list, the release build no longer uses a shared build cache.
- **Import**: one archive creates at most 50 libraries, a repeated file is imported once, at most two imports run at a time, archive covers only attach to editions the import created.
- **Misc**: idempotency hash no longer depends on pointer addresses, scans check write permission before anything else, test notifications are rate limited, catalog field lengths are enforced on every path, internal error text no longer reaches MCP clients or CSV import reports, more IPv6 transition ranges blocked, images over 6 megapixels refused.

## Known and accepted for now (tracked in `docs/roadmap.md`)

- Editions created by users for an ISBN nobody has scanned yet are trusted as is (shared catalog): an administrator edit and provider refresh path is needed.
- Email notification channels are not confirmed by the recipient.
- Loans are visible to their creator only; other members cannot close a loan that blocks a shared copy.
- Sharing with an unknown email answers differently from a known one.
- Passkeys and tokens can be added from a stolen session without recent re-authentication.
- No per-user quotas; the account import still holds the archive in memory.
- The Helm chart passes the metrics token as an environment variable.
- Single sign-on is a single factor: the identity provider owns MFA (documented in the security guide).
