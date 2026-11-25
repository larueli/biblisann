# API guide: sensitive actions, quotas, invitations and refreshes

Conventions that apply across the REST API, beyond what the OpenAPI document (`/openapi.json`, see [API documentation](api-docs.md)) can express. Decisions behind them: ADR 0026.

## Errors

Errors are RFC 9457 problems (`application/problem+json`). Clients branch on the HTTP status and, where noted, on the `detail` string.

## Step-up authentication (sensitive actions)

Some actions need a recent re-authentication of the **browser session** (personal access tokens never can):

- password change, authenticator (TOTP) enrolment and removal, passkey add and remove, token creation, account deletion, OAuth consent for a new application;
- administrators: setting another account's password, deleting an account, creating an invitation.

Without it the call answers `403` with `detail: "step_up_required"`. Open the window with one of:

| Call | For |
|------|-----|
| `POST /v1/auth/step-up` `{password, code?}` | accounts with a password (`code`: authenticator or recovery code when two-factor is on) |
| `POST /v1/auth/step-up/passkey/begin` then `/finish` | a passkey of the account |
| browser navigation to `GET /v1/auth/oidc/step-up` | single sign-on: the provider is asked to authenticate the user again; the callback returns to `/settings?stepup=ok` |

The window lasts 5 minutes (`GET /v1/auth/session` shows `step_up_until` while it is open). When the identity provider sends no `auth_time` claim the window is single-use: the next sensitive action closes it. The web client handles this centrally: its fetch middleware catches `step_up_required`, shows a dialog and replays the request.

`GET /v1/me/security` says which methods the account can use (`has_password`, `passkeys_available`, `sso_available`, `mfa_enabled`).

## Request quota

Every authenticated `/v1/*` call, every `/mcp` call and every `/opds/*` request is metered per user (OPDS: per client address), at `BIBLISANN_API_RATE_LIMIT` tokens per minute (default 600). Calls weigh 1, except provider lookups (`GET /v1/isbn/{isbn}`, `POST /v1/libraries/{id}/scan`, the MCP `add_book` tool: 5), refreshes (10) and imports (20). Cover images are not metered.

Responses carry the IETF rate-limit fields (`draft-ietf-httpapi-ratelimit-headers`, still a draft):

```
RateLimit-Policy: "default";q=600;w=60
RateLimit: "default";r=12;t=30
```

`q` tokens per `w` seconds; `r` left, full again in `t` seconds. Over the quota: `429` with `Retry-After` in seconds. Clients should pace themselves from `r` and `t`, as the web scan outbox does (`web/src/offline/pacing.ts`). With Valkey configured the quota is shared by all replicas.

## Adding many books

There is no batch endpoint: send one `POST /v1/libraries/{id}/scan` per ISBN with its own `Idempotency-Key` and respect the quota headers. The web app's *Add a list of ISBNs* does exactly this through the offline outbox.

## Invitations

`PUT /v1/libraries/{id}/sharing/users` with an email that has no account:

- for anyone but an instance administrator the call succeeds and does nothing (the answer matches a known address);
- for an instance administrator it answers `409` with `detail: "no_account"`. Repeat with `"invite": true` (needs step-up) to create an invitation: `200 {"status":"invited","invitation_url":...}`. The link is valid 7 days and mailed when SMTP is configured.

The invitee opens `/invite?token=...`: `GET /v1/invitations/{token}` describes it, `POST /v1/auth/register` with `invitation_token` creates the account with the invited address (even when registration is closed) and grants the access, and `POST /v1/invitations/accept` redeems it for an existing signed-in account. Invitation links never carry administration or sharing rights, and are redacted from logs and traces.

## Catalog administration and metadata refresh

The catalog is shared by all libraries, so changing data by hand is for instance administrators (`PATCH /v1/editions/{id}`, locked against refreshes; `PUT /v1/editions/{id}/sync-excluded`; `DELETE /v1/editions/{id}`).

Refreshing asks the providers again and replaces only fields that were not edited by hand:

| Call | Who |
|------|-----|
| `POST /v1/editions/{id}/refresh` | instance administrators, and members who may edit a library holding the book |
| `POST /v1/libraries/{id}/catalog/refresh` | members who may edit the library (unknown books); `all: true` (also known books) needs library administration |
| `POST /v1/admin/catalog/refresh` | instance administrators: whole server or `library_id` |

The two background calls answer `202` and run as jobs: one lookup per second, at most 500 books per run. A weekly job does the same for books no provider knew. A refresh of a book no provider knows (or without ISBN) is not an error: the edition comes back unchanged with `sync_status` `not_found` or `no_isbn`, and `synced_at`. Editions with `sync_excluded: true` are never touched (a direct refresh answers `409`). When all providers fail the answer is `502` and nothing is recorded.

## Email channels

`POST /v1/me/notification-channels` with `kind: "email"` creates the channel unconfirmed and mails a 6-digit code (valid 30 minutes, five attempts). `POST /v1/me/notification-channels/{id}/confirm` `{code}` activates it, `POST .../{id}/code` sends a new code. Unconfirmed channels receive nothing. Mail per user is capped per day (`BIBLISANN_EMAIL_DAILY_CAP`, default 20, confirmation codes included).
