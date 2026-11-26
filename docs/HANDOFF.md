# Handoff

Read at the start of every session, rewrite at the end. Keep it under 60 lines. This file is how a different tool or model continues without chat history.

## Current slice and task

Slices 0, 1, 2 and 3 are implemented on `main` (committed directly on `main` at the owner's request; nothing is pushed, the GitHub repository does not exist yet). What remains is validation on real devices and services, the web screens listed in the roadmap, and the GitHub-side setup of Slice 0.

## Done since last handoff

- Slice 1: accounts, catalog, providers, covers, libraries, search, offline index sync and outbox, scanner, e2e.
- Slice 2: sharing and profiles, reading status and grades, lists and series, loans, River jobs and notifications, ICS, tokens, MCP, backup/restore, S3 storage, Goodreads import, inventory, QR labels.
- Slice 3: OIDC, TOTP and recovery, password reset, passkeys, SCIM, OAuth server for MCP, extra providers and addons, Hardcover ratings, OPDS, Valkey rate limiting.
- Follow-up: StoryGraph and LibraryThing importers, Hardcover bulk refresh job (rate limit, retry, 3-day refresh), library/account export and import, account deletion, session and OAuth grant revocation, admin user management, S3 tested with RustFS.
- Follow-up 2: garbage collection (`biblisann gc`, daily), probe routes, Prometheus and OpenTelemetry, self-hosted API docs at `/docs/`, `/v1/about` and footer, `oasdiff` CI gate, two-way Hardcover sync per user, PostgreSQL 19 tested.
- Follow-up 3: web screens for sharing, profiles, groups and SCIM token; Web Push in the browser (custom service worker); shelf view; OAuth refresh-token family revocation.
- Security audit fixes (ADR 0025); open audit follow-ups are at the end of the roadmap.
- Follow-up 4 (ADR 0026, commits fd9836f..4e62c8e on `main`, not pushed): Helm metrics token as a file plus kubeconform/kube-linter (`make helm-check`), step-up authentication (5 minutes, single-use when the OIDC provider sends no `auth_time`), admin-set passwords and no self-service password for SSO-only accounts, loans shared by every `loan.write` member, invitations for unknown addresses (instance administrators only, confirmed in the UI), catalog administration (edit, refresh, exclude from refreshes, delete), email channel confirmation by code plus `BIBLISANN_EMAIL_DAILY_CAP`, per-user request quota with `RateLimit` headers (`BIBLISANN_API_RATE_LIMIT`) and a paced bulk ISBN list, TypeScript 7 plus shadcn/ui.
- ADRs 0011 to 0026, user docs (first run, organise, security, install, configuration), `docs/dev/addons.md`.

## In progress

- Security-audit fixes were made in the primary checkout at the owner's explicit request (ADR 0027). Imports use a shared transaction, backup downloads use step-up, traces omit request details, and release verification guidance is narrowed. Independent review and `make check` passed.
- Remaining limits: imported covers are stored best-effort after the database commit; registration capacity can be unavailable for the 24-hour client grace period. The exact image-signature identity must be confirmed on the first real release and republish.
- `BIBLISANN_DATA_DIR` replaces `BIBLISANN_COVERS_DIR`: local covers live under its `covers` subdirectory. Startup rejects the old setting; migration is in `docs/user/configuration.md`.
- Logical backup v1 restore is refused; v2 preserves identity factors and requires the original key for TOTP. Existing email addresses default unverified, existing shares remain and need review; old SCIM accounts may need administrator-assisted recovery. Unscoped offline queues remain on disk but are not automatically replayed.

- Weekly cover-file check (`internal/covercheck`, job `cover_check`, `biblisann covers-check`, Prometheus gauges); report only, no repair.
- `SECURITY.md` and `CONTRIBUTING.md` added; redoc.html lint warning fixed; `make check` is green with `PATH=/usr/bin:$PATH`.

## Next

0. Review the remaining operational limits above.

0b. **Release pin**: `release-please-config.json` pins `"release-as": "0.1.0"` (PR `ci: pin the first release to 0.1.0`). After the `0.1.0` release is cut, remove that line in a follow-up PR (`docs/dev/versioning.md`, `docs/roadmap.md`), or later releases stay forced to `0.1.0`.

1. Human, in order (`docs/dev/github.md`): create the `release` and `publish` environments (branch rule `main`), store the new App key as a `release` environment secret and delete the old repository secret and key, create the tag ruleset, push `main`, check CI is green (the earlier `e2e` failures are fixed locally), protect `main`, then merge the release PR that release-please opens. Dependabot has opened 5 PRs (#1 to #5): after the push, ask Dependabot to rebase them, then merge the ones whose CI is green, one at a time.
2. Real-world validation list in `docs/roadmap.md` (open decisions): phone scanning first.
3. Decide `1.0.0` timing (roadmap, "Version policy").

## Gotchas and decisions not yet in an ADR

- **RAM**: the host has no swap. Run Go tests with `-p 1` (or 2) and `GOMEMLIMIT`; `make test`/`make lint` already do. Do not run two `make check` at once. Never `pkill -f` a pattern that matches your own shell command.
- `node` on this machine is a bun shim: use `PATH=/usr/bin:$PATH` for web tooling.
- Tests start one small PostgreSQL container per package (`internal/dbtest`); Docker must be running.
- The web app uses TypeScript 7; `openapi-typescript` runs with TypeScript 5.9 in `web/tools/openapi-gen` (separate lockfile, installed by `pnpm run generate:api`). pnpm refuses releases younger than 24 h.
- `make generate` regenerates sqlc, `api/openapi.json` and the web client; the pre-push hook fails if they are stale.
- Migrations 00002 to 00008 were edited freely before any release; from the first release they are immutable.
- Huma does not flatten embedded structs in schemas: inline fields in response types.
- Migrations 00015 to 00019 (step-up, single-use step-up, invitations, edition sync flag, channel confirmation) are new since the last handoff and still editable until the first release.
- Not verified against real services: OIDC step-up (`auth_time` handling) with a real provider, SMTP delivery of invitation and confirmation mails, the RateLimit header draft against real clients.
- Request bodies of imports are still buffered by Huma (64 MiB); only covers are spooled to disk. True streaming needs a plain handler (spec impact); tracked in the roadmap.
- Decisions the owner did not spell out and that are my choices: catalog edits are administrators only; email confirmation uses a code, not a link; quotas are request-based only (no storage quota).

## Commands that matter right now

- `make check` (slow: tests with containers), `make build`, `make dev`, `make e2e`, `make generate`, `make docs`
