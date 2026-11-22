# 0005. Dependency policy

Date: 2026-11-22
Status: Accepted

## Context

Supply-chain attacks (typosquatting, hallucinated package names, abandoned packages) are a top risk, and AI agents add dependencies quickly.

## Decision

1. Standard library first.
2. Otherwise a well-known, actively maintained library with a permissive license.
3. Verify existence and health on the registry before adding (repository, maintainers, release cadence, open security issues, license).
4. Every new direct dependency is logged in the table below in the same PR.
5. Lockfiles committed. GitHub Actions pinned by SHA. Tool versions pinned. Dependabot proposes updates; a human merges.
6. `govulncheck` and `npm audit` (or equivalent) run in CI.

## Approved direct dependencies

| Name | Purpose | License | Alternatives considered | Added in |
|------|---------|---------|-------------------------|----------|
| (none yet) | | | | |

## Consequences

Slightly slower to add libraries, much lower risk. A reviewer rejects a PR whose dependency has no row.
