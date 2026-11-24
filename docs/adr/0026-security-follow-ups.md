# 0026. Security audit follow-ups: decisions

Date: 2026-11-24
Status: Accepted

Decisions that close the "known and accepted for now" list of ADR 0025. Each one is delivered by its own pull request.

## Decisions

1. **Catalog editions.** A user can always edit a book by hand, and any edition can be excluded from provider sync ("provider went crazy"). Administrators can edit or delete shared catalog rows and refresh them from providers. A refresh replaces unverified user-created data; it never touches an edition that is excluded from sync or was edited by hand.
2. **Email notification channels.** An address other than the account's own verified email must be confirmed by a link or a code sent to it before it receives anything. A daily cap per user applies to every email channel.
3. **Loans.** Every member holding `loan.write` on a library (editor and above in the standard profiles) sees loans of that library and can return them. Access is re-checked when a loan is returned.
4. **Step-up authentication.** Password change, TOTP enrolment, adding or changing a passkey, creating a token and deleting the account require a fresh authentication valid for 5 minutes. It also applies to single sign-on accounts (the identity provider is asked to re-authenticate).
5. **Quotas.** Per-user quotas on API calls and bulk work, announced with the standard `RateLimit` and `RateLimit-Policy` headers and `Retry-After` on 429. The web UI slows itself down and shows progress when it nears a limit. A bulk "add a list of ISBNs" feature is added. Account imports stream to disk.
6. **Metrics token.** `BIBLISANN_METRICS_TOKEN` and `BIBLISANN_METRICS_TOKEN_FILE` both work. The Helm chart mounts the token as a file and can reference an existing Secret and key (sops, sealed secrets).
7. **Sharing.** Sharing a library needs library administration (the API route requires the admin role; the service still honours the older `library.share` permission for custom profiles, which the API never reaches). Sharing with an unknown email does nothing and answers like a known one for non-administrators. For administrators the UI asks for explicit confirmation before an invitation is created.

## Tooling decisions

- Helm chart checked by `kube-linter` and `kubeconform` through prek.
- shadcn/ui is added to the web app.
- TypeScript 7 for the application; `openapi-typescript` (peer `typescript ^5`) runs with TypeScript 5.9 in an isolated tool directory. Replacing the generator was considered and not chosen (see the pull request that introduces the split).

## Review of the implementation (same day)

A read-only security review found no critical or high issue. Fixed: OAuth consent now needs step-up (it mints long-lived tokens); invitation tokens are redacted from access logs and kept out of traces; the Helm chart mounts a writable `/tmp` for import spooling; invitations cannot carry administration or sharing; single-use step-up is consumed atomically; duplicate cover names in an archive no longer overwrite each other; edition year and page counts have bounds.

Known and accepted, documented in `docs/user/security.md`: a library administrator who is not an instance administrator can still tell some known accounts from unknown ones (already-elevated or last-administrator answers, and the member list); other `loan.write` members see the counterparty, contact name and notes of loans in a shared library; a provider that ignores `prompt=login` and sends no `auth_time` gives a weaker step-up. Open follow-ups: filter the calendar feed by current loan permission, meter `/mcp` and `/opds` in the request quota, move the catalog administrator check into the service layer, and consider step-up for destructive administrator actions.
