# 0010. prek as the hook runner, one command locally and in CI

Date: 2026-11-22
Status: Accepted

## Context

Checks must behave the same on a laptop, for an AI agent and in CI, and must not depend on one editor or tool.

## Decision

- **prek** runs git hooks and checks, configured in `.pre-commit-config.yaml` (pre-commit compatible, so the original `pre-commit` can run the same file).
- Fast checks (formatting hygiene, secret scan, workflow lint, Conventional Commit message, lint) run at `pre-commit` and `commit-msg`. Slow checks (tests, vulnerability scan, web build) run at `pre-push`.
- Project checks are `make` targets called by local hooks, so humans, agents and CI use identical commands.
- `make check` runs prek at both stages. CI installs a pinned prek and runs `make check`; the PR title is checked with `make commit-lint`.
- Hook `rev` values are frozen to commit SHAs (`prek update --freeze`), per the dependency policy.

## Consequences

- prek is young (0.x): pin its version in CI and update deliberately.
- Agents must never bypass hooks (`--no-verify` is denied in the Claude Code settings and forbidden in `AGENTS.md`).
- The `zizmor` hook stays in the manual stage until every Action is pinned by SHA.
