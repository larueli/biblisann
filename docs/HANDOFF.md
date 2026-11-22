# Handoff

Read at the start of every session, rewrite at the end. Keep it under 60 lines. This file is how a different tool or model continues without chat history.

## Current slice and task

Slice 0 (Foundations) is code complete on branch `feat/0-foundations` (worktree `../biblisann-wt/0-foundations`). `make check` is green there. Nothing is committed or pushed yet unless `git log` says otherwise; the GitHub repository does not exist yet.

## Done since last handoff

- Decisions recorded: ADR 0011 (AGPL, `larueli/biblisann`), 0012 (Zensical docs), 0013 (UI language, metadata scope); ADR 0009 updated (GitHub App, 0.x until Slice 3).
- Go: config (env, `_FILE`, file, conflicts), httpmw (trusted proxies), api (Huma, RFC 9457, `x-permission`), db (pgx, goose, advisory lock, sqlc, testcontainers), safehttp, webui embed, app lifecycle, `cmd/biblisann` (serve, migrate, healthcheck, openapi, version).
- Web: Vite + React + TS strict, i18n (en, fr), PWA, generated API client, Biome, Vitest, Playwright smoke.
- Deploy: Dockerfile (distroless, digest-pinned), Compose, Helm chart (external DB or CNPG).
- CI/hooks: actions pinned by SHA, hook revs frozen, zizmor on, `e2e` job, `docs` workflow, App-token step in the release workflow.
- Docs: user and dev skeletons, Zensical site with landing placeholder, `docs/dev/github.md`.

## In progress

- Nothing.

## Next

1. Human: create the GitHub repository, the releaser App and settings (`docs/dev/github.md`), push the branch, open the PR (title `feat: slice 0 foundations`), check CI on GitHub.
2. Answer the open decisions at the end of `docs/roadmap.md`.
3. Slice 1 starts with the scanner spike (real phone), then schema and local auth.

## Gotchas and decisions not yet in an ADR

- This machine's `node` is a bun shim: run web tooling with `PATH=/usr/bin:$PATH` (real Node 26). CI uses Node 24 (`web/.nvmrc`).
- TypeScript is pinned to 5.9.3 because `openapi-typescript` does not work on TypeScript 7.
- pnpm refuses releases younger than 24 h (`web/pnpm-workspace.yaml`); do not add exclusions, pick an older version.
- `make commit-lint` needs `.pre-commit-config.yaml` staged.
- `api/openapi.json` is generated with version `0.0.0`; the server serves the real version.
- Verify the Codex model IDs in `.codex/agents/*.toml` with `/model` (see `docs/agents.md`).

## Commands that matter right now

- `make check`, `make build`, `make dev`, `make generate`, `make docs`
