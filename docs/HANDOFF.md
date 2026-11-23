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
- ADRs 0011 to 0023, user docs (first run, organise, security, install, configuration), `docs/dev/addons.md`.

## In progress

- Nothing.

## Next

1. Human: create the GitHub repository, releaser App and settings (`docs/dev/github.md`); push `main`; check CI (`e2e` job runs `scripts/e2e.sh`).
2. Real-world validation list in `docs/roadmap.md` (open decisions): phone scanning first.
3. Decide `1.0.0` timing (roadmap, "Version policy").

## Gotchas and decisions not yet in an ADR

- **RAM**: the host has no swap. Run Go tests with `-p 1` (or 2) and `GOMEMLIMIT`; `make test`/`make lint` already do. Do not run two `make check` at once. Never `pkill -f` a pattern that matches your own shell command.
- `node` on this machine is a bun shim: use `PATH=/usr/bin:$PATH` for web tooling.
- Tests start one small PostgreSQL container per package (`internal/dbtest`); Docker must be running.
- TypeScript is pinned to 5.9.3 (`openapi-typescript` needs it); pnpm refuses releases younger than 24 h.
- `make generate` regenerates sqlc, `api/openapi.json` and the web client; the pre-push hook fails if they are stale.
- Migrations 00002 to 00008 were edited freely before any release; from the first release they are immutable.
- Huma does not flatten embedded structs in schemas: inline fields in response types.

## Commands that matter right now

- `make check` (slow: tests with containers), `make build`, `make dev`, `make e2e`, `make generate`, `make docs`
