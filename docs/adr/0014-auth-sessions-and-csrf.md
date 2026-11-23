# 0014. Local accounts, sessions, CSRF and rate limiting

Date: 2026-11-23
Status: Accepted

## Context

Slice 1 needs local authentication that is safe by default for a public, self-hosted service.

## Decision

- **Passwords**: Argon2id (m=19 MiB, t=2, p=1, PHC string), at most 256 bytes, at least 10 characters, a small deny-list, never the email. Parameters read from a stored hash are bounded before use. A dummy hash is verified for unknown accounts so timing does not reveal registered emails.
- **Registration** is `first-user` by default: only the very first account can register (it becomes the instance administrator, serialized by an advisory lock). `open` and `closed` are available. Further accounts are created by an administrator.
- **Sessions** are random 256-bit tokens in an `HttpOnly`, `SameSite=Lax` cookie (`__Host-` prefix and `Secure` when `BASE_URL` is https). Only the SHA-256 of the token is stored. Lifetime 30 days absolute; changing the password ends every session.
- **CSRF**: every non-GET request needs the `X-CSRF-Token` returned by `GET /v1/auth/session`/login (bound to the session), and an `Origin` (or `Sec-Fetch-Site`) matching `BASE_URL`. Bearer-token requests are exempt (browsers do not attach them on their own).
- **Rate limiting**: in-memory token buckets (`internal/ratelimit`) per client IP and per account for login, registration and password change; fail closed when the table is full. The `Limiter` interface lets a Valkey implementation replace it for HA (Slice 3).
- **Authorization**: `authz.Principal` in the context; library roles `read < edit < admin` are checked in `library.Service.Require`. A non-member gets 404, never 403, so library IDs cannot be probed. Token principals carry scopes (`read`, `write`).

## Consequences

- A reverse proxy must forward the original `Host`/`Origin`; `BASE_URL` must be the public origin.
- Per-process rate limits are approximate with several replicas until Valkey is wired.
