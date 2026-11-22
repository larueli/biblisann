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
6. `govulncheck` runs in CI (`make vuln`). pnpm refuses releases younger than 24 hours (`minimumReleaseAge` in `web/pnpm-workspace.yaml`) and `pnpm audit` is a pre-push hook.

## Approved direct dependencies

| Name | Purpose | License | Alternatives considered | Added in |
|------|---------|---------|-------------------------|----------|
| `github.com/jackc/pgx/v5` | PostgreSQL driver and pool | MIT | `database/sql` + `lib/pq` (maintenance mode) | Slice 0 |
| `github.com/pressly/goose/v3` | Migrations, advisory-lock session locker | MIT | `golang-migrate`, `atlas` | Slice 0 |
| `github.com/danielgtaylor/huma/v2` | OpenAPI 3.1 from code, RFC 9457 errors, `net/http` adapter | MIT | `oapi-codegen` (spec first), hand-written spec | Slice 0 |
| `github.com/testcontainers/testcontainers-go` (+ `modules/postgres`) | Real PostgreSQL in tests | MIT | `dockertest`, mocks (rejected: miss SQL behavior) | Slice 0 |
| `sqlc` (tool, run with `go run`, not linked) | SQL to Go code | MIT | `pgx` by hand, ORM (forbidden) | Slice 0 |
| `golangci-lint` (tool, `go run`, not linked) | Go linters | GPL-3.0 (tool only, not distributed with the product) | `staticcheck` alone | Slice 0 |
| `govulncheck` (tool, `go run`) | Go vulnerability scan | BSD-3-Clause | `osv-scanner` | Slice 0 |
| `react`, `react-dom` | UI | MIT | Preact, Solid (smaller ecosystem for our needs) | Slice 0 |
| `vite`, `@vitejs/plugin-react`, `vite-plugin-pwa`, `workbox-build`, `workbox-window` | Build, PWA manifest and service worker | MIT | hand-written service worker | Slice 0 |
| `@tanstack/react-router`, `@tanstack/react-query` | Routing, server state | MIT | React Router, SWR | Slice 0 |
| `tailwindcss`, `@tailwindcss/vite` | Styling | MIT | CSS modules | Slice 0 |
| `i18next`, `react-i18next`, `i18next-browser-languagedetector` | i18n, browser language detection | MIT | `react-intl`, `lingui` | Slice 0 |
| `openapi-typescript`, `openapi-fetch` | Typed API client generated from the spec | MIT | `orval`, hand-written types (forbidden) | Slice 0 |
| `typescript` (pinned to 5.9.x) | Type checking | Apache-2.0 | TypeScript 7: `openapi-typescript` 7 does not support it yet (no JS compiler API). Revisit when it does. | Slice 0 |
| `vitest`, `jsdom`, `@testing-library/react`, `@testing-library/jest-dom` | Unit and component tests | MIT | Jest | Slice 0 |
| `@playwright/test` | End-to-end smoke tests | Apache-2.0 | Cypress | Slice 0 |
| `@biomejs/biome` | Lint and format | MIT OR Apache-2.0 | ESLint + Prettier | Slice 0 |
| `zensical` (docs build tool, run with `uvx`, not part of the product) | Documentation site | MIT | MkDocs Material (see ADR 0012) | Slice 0 |

## Consequences

Slightly slower to add libraries, much lower risk. A reviewer rejects a PR whose dependency has no row.
