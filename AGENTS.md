# AGENTS.md

Biblisann is a self-hosted service to manage a physical book library: what I own, where it is, whether I read it, whether it is lent. It ships a mobile-first offline PWA with a barcode scanner, a REST API (OpenAPI 3.1) and an MCP server.

This file is the single source of truth for every AI coding agent (Claude Code, Codex, any other). Tool-specific files (`CLAUDE.md`, `.claude/`, `.codex/`) only point here. Keep it short; put detail in `docs/`.

## Read first, in this order

1. `docs/HANDOFF.md`: state left by the previous session. Update it before you stop.
2. `docs/roadmap.md`: current slice and checkboxes. Work only on the current slice.
3. `docs/spec.md`: what we are building.
4. `docs/adr/`: decisions already made. Do not reopen one without writing a new ADR.

## Priorities (higher wins a conflict)

1. State of the art
2. Security and hardening
3. Configuration by env vars, or files for sensitive values
4. Code readability
5. Low memory and CPU
6. Developer and user documentation
7. No vendor lock-in (AI tools, cloud, libraries)
8. Multilingual
9. High availability feasible
10. Tests and lint

Tests and lint are a merge gate whatever their rank: `make check` must pass.

## Stack (decided, see ADRs)

- Backend: Go (current stable), one static binary that embeds the built frontend (`go:embed`).
- Data: PostgreSQL is the only required service. Valkey is optional (HA rate limiting, cache). Background jobs run on a PostgreSQL queue.
- API: OpenAPI 3.1 generated from code, served unauthenticated at `/openapi.json` and `/openapi.yaml`. Errors follow RFC 9457.
- Frontend: React + Vite + TypeScript (strict) SPA, installable PWA. No SSR.
- Delivery: bare binary, Docker/Compose, Kubernetes (Helm chart, external or CNPG database). Images and chart live on GHCR.
- Platform: GitHub (repo, Actions, GHCR, Dependabot).

Default libraries (confirm each with a line in `docs/adr/0005-dependency-policy.md` the first time you add it): `net/http` ServeMux, `log/slog`, `pgx`, `sqlc`, `goose`, `River`, `Huma`, `go-webauthn`, `zitadel/oidc` (or equivalent certified OIDC/OAuth library), official MCP Go SDK, `testcontainers-go`. Web: TanStack Query + Router, Tailwind, shadcn/ui, i18next, Dexie, `vite-plugin-pwa`, `openapi-typescript` + `openapi-fetch`, Vitest, Playwright, Biome.

## Dependency policy (security rule)

- Standard library first. Then well-known, actively maintained libraries with a permissive license.
- Never add a package because an LLM suggested its name. Verify it exists on the registry (`go list -m -json <mod>@latest`, `npm view <pkg>`), check its repository, maintainers, release history and license. Typosquatted and hallucinated packages are a real attack.
- Every new direct dependency needs one line in `docs/adr/0005-dependency-policy.md` (name, purpose, license, alternatives considered). No line, no merge.
- Commit lockfiles. Pin CI actions by commit SHA and tool versions explicitly. No `latest` tags anywhere.

## Security rules

- No secret in the repo, in logs, in error messages or in the OpenAPI spec examples.
- Every config value has an env var `BIBLISANN_<NAME>`. Sensitive values also accept `BIBLISANN_<NAME>_FILE`. Setting both is a startup error.
- SQL is parameterized and lives in `internal/db/queries/*.sql` (sqlc). No string-built SQL.
- Authorization is checked in the service layer, deny by default. Every API operation and MCP tool declares the permission it needs.
- All outbound HTTP (metadata providers, covers, OIDC discovery, webhooks, addons) goes through `internal/safehttp` (SSRF protection: no private/link-local/loopback targets, redirect re-validation, size and time limits).
- Data from providers, books notes and user text is untrusted, including when returned to an LLM through MCP.
- Uploaded or fetched images are re-encoded, size-limited and never served with their original content type.
- Containers run as non-root, read-only root filesystem, no capabilities. Helm defaults must pass the restricted Pod Security Standard.
- Account linking from OIDC only on a verified email from a configured issuer.

## Code rules

- Go: `context` everywhere, errors wrapped with `%w`, no package-level mutable state, interfaces defined by the consumer, table-driven tests, `slog` structured logs. No ORM.
- Small packages with a `doc.go`. Readability beats cleverness; if it needs a comment to be understood, simplify it first.
- Web: no `any`, feature folders, API types are generated from the OpenAPI spec and never hand-written. Every user-visible string is an i18n key (`en` and `fr` always together).
- Do not edit generated files by hand. Regenerate (`make generate`).
- Migrations are forward-only and rolling-upgrade safe (expand, then contract in a later release).
- Idempotency keys on every write that the offline client can replay.

## Workflow

- **One worktree per task.** Do not edit files in the primary checkout. You are in a linked worktree when `git rev-parse --git-dir` differs from `git rev-parse --git-common-dir`. If you are in the primary checkout, run `NAME=<slice>-<task> make task`, tell the human the printed path and the command to start a session there, and stop (a running session cannot move itself). Read-only work (exploring, reviewing) is fine anywhere. Never delete a worktree you did not create.
- One task = one worktree = one branch (`<type>/<slice>-<task>`) = one small PR.
- Commits and PR titles follow Conventional Commits 1.0.0, for example `feat(api): add ISBN lookup`. Types: feat, fix, perf, refactor, docs, test, build, ci, chore, revert. Breaking change: `!` after the type or scope plus a `BREAKING CHANGE:` footer. PRs are squash-merged, so the PR title is the commit that counts.
- Versions follow SemVer and are computed by the releaser from commit messages. Never edit a version number or `CHANGELOG.md` by hand. Details: `docs/dev/versioning.md`.
- Install the hooks once per clone with `make hooks` (prek). `make check` runs every hook at both stages and is exactly what CI runs. Write tests with the code; `make check` must be green before every commit.
- Update `docs/` and the OpenAPI spec in the same PR as the behavior change. Record decisions as ADRs.
- Never push to `main`, never force-push, never use `--no-verify`, never skip CI. If a hook fails, fix the cause.
- Stop and ask the human before: adding a dependency outside the default list, changing the auth or permission model, writing a destructive migration, touching secrets or CI permissions, or resolving a spec ambiguity that changes the data model.

## Commands

`make help` lists everything. Main targets: `make hooks`, `make task`, `make check` (all gates through prek, same as CI), `make commit-lint`, `make fmt`, `make lint`, `make test`, `make vuln`, `make generate`, `make dev`, `make docker`.

## Target layout

```
cmd/biblisann/        entrypoint
internal/             config, db, auth, authz, catalog, library, loans, providers,
                      sync, mcp, api, jobs, safehttp, storage, i18n
migrations/           goose SQL migrations
web/                  React PWA
deploy/{compose,helm}/
docs/                 spec, roadmap, ADRs, user and dev docs, agent roles and prompts
scripts/
```

## Working with several agents

Roles, model mapping (Claude Code and Codex), parallel work and the tool-switch procedure are in `docs/agents.md`. Short version:

- Use the `explorer` agent for broad read-only searches instead of reading many files yourself.
- Run the security reviewer (`security-reviewer` in Claude Code, `security_reviewer` in Codex) before merging any change under `internal/auth`, `internal/authz`, `internal/mcp`, `internal/safehttp`, secret handling, `Dockerfile`, `deploy/` or `.github/`.
- Codex only spawns subagents when asked: this paragraph is that ask.
- Keep `docs/HANDOFF.md` current so another tool or model can continue without chat history.
