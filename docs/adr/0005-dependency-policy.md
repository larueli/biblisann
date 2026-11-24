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
| `typescript` 7.0.x | Type checking of the web app | Apache-2.0 | stay on 5.9 | Follow-ups |
| `typescript` 5.9.x + `openapi-typescript` 7.13 in `web/tools/openapi-gen` (own lockfile) | Client generation only: `openapi-typescript` needs the JavaScript compiler API that TypeScript 7 no longer ships | Apache-2.0 / MIT | `@hey-api/openapi-ts`, `orval`, `kubb` (replace `openapi-fetch` and every call site; TypeScript 7 support not verified) | Follow-ups |
| `vitest`, `jsdom`, `@testing-library/react`, `@testing-library/jest-dom` | Unit and component tests | MIT | Jest | Slice 0 |
| `@playwright/test` | End-to-end smoke tests | Apache-2.0 | Cypress | Slice 0 |
| `@biomejs/biome` | Lint and format | MIT OR Apache-2.0 | ESLint + Prettier | Slice 0 |
| `github.com/google/uuid` | UUID type used by sqlc-generated code and the API | BSD-3-Clause | `pgtype.UUID` (awkward), own type | Slice 1 |
| `golang.org/x/crypto` (argon2) | Argon2id password hashing | BSD-3-Clause | stdlib has none; `alexedwards/argon2id` (thin wrapper, not needed) | Slice 1 |
| `golang.org/x/image` (draw) | Cover resizing (CatmullRom) | BSD-3-Clause | stdlib `image` has no scaler | Slice 1 |
| `dexie`, `dexie-react-hooks` | IndexedDB offline index and scan outbox | Apache-2.0 | `idb` (lower level), raw IndexedDB | Slice 1 |
| `barcode-detector`, `zxing-wasm` | `BarcodeDetector` polyfill (WASM decoder served by the app, not a CDN) | MIT | `@zxing/library` (older, JS-only, slower), native API only (not on Firefox/iOS) | Slice 1 |
| `fake-indexeddb` (dev) | IndexedDB in unit tests | Apache-2.0 | browser tests only | Slice 1 |
| `github.com/riverqueue/river` (+ `riverdriver/riverpgxv5`, `rivermigrate`) | PostgreSQL job queue: loan reminders, notification delivery, housekeeping | MPL-2.0 | `asynq` (needs Redis), hand-rolled `SKIP LOCKED` queue | Slice 2 |
| `github.com/SherClockHolmes/webpush-go` | Web Push (VAPID) delivery; uses the safehttp client | MIT | own implementation of RFC 8291/8292 (error-prone) | Slice 2 |
| `github.com/minio/minio-go/v7` | S3-compatible cover storage | Apache-2.0 | `aws-sdk-go-v2` (much larger) | Slice 2 |
| `github.com/modelcontextprotocol/go-sdk` | MCP server (streamable HTTP) | MIT/Apache-2.0 | `mark3labs/mcp-go` (community) | Slice 2 |
| `qrcode` (web) | QR labels for places, generated in the browser | MIT | `qrcode-generator` | Slice 2 |
| `github.com/moby/moby/api` (tests only) | Docker host-config types to cap test container memory | Apache-2.0 | none (needed by the testcontainers option) | Slice 3 |
| `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2` | OIDC sign-in (the default list names `zitadel/oidc` "or equivalent certified library": go-oidc is the widely deployed client library) | Apache-2.0 / BSD-3-Clause | `zitadel/oidc` (server-oriented, larger) | Slice 3 |
| `github.com/go-webauthn/webauthn` | Passkeys (WebAuthn ceremonies and verification) | BSD-3-Clause | own implementation (error-prone) | Slice 3 |
| `github.com/redis/go-redis/v9` | Valkey/Redis client for shared rate limits | BSD-2-Clause | `valkey-io/valkey-go` (newer, smaller community) | Slice 3 |
| `github.com/fxamacker/cbor/v2` (tests only) | CBOR for the software authenticator in passkey tests | MIT | hand-rolled encoder | Slice 3 |
| `github.com/go-jose/go-jose/v4` (tests only; also a dependency of go-oidc) | Sign ID tokens in the fake OIDC provider | Apache-2.0 | none | Slice 3 |
| `github.com/prometheus/client_golang` | Prometheus metrics at `/metrics` | Apache-2.0 | OpenTelemetry metrics SDK (heavier, same data) | Slice 3 follow-up |
| `go.opentelemetry.io/otel`, `otel/sdk`, `otlptracehttp`, `contrib/.../otelhttp` | OpenTelemetry traces over OTLP/HTTP, configured by the standard `OTEL_*` variables | Apache-2.0 | none (the standard) | Slice 3 follow-up |
| `github.com/oasdiff/oasdiff` (CI tool, `go run`, not linked) | Breaking OpenAPI change detection | Apache-2.0 | `openapi-diff` (Java) | Slice 3 follow-up |
| `swagger-ui-dist`, `redoc`, `@stoplight/elements`, `@scalar/api-reference` (build time: their prebuilt bundles are copied into the app) | Self-hosted interactive API documentation | Apache-2.0, MIT, Apache-2.0, MIT | CDN-loaded viewers (third-party scripts at runtime: rejected) | Slice 3 follow-up |
| `zensical` (docs build tool, run with `uvx`, not part of the product) | Documentation site | MIT | MkDocs Material (see ADR 0012) | Slice 0 |
| `kubeconform` v0.8.0 and `kube-linter` v0.8.3 (CI tools, run with `go run`, not part of the product) | Validate the Helm chart manifests and its Pod Security posture | Apache-2.0 | `kubectl --dry-run` (needs a cluster), `kubescape` (heavier) | Follow-ups |
| `@radix-ui/react-dialog`, `@radix-ui/react-slot` | Accessible primitives under the shadcn/ui Dialog and Button (focus trap, Escape, aria) | MIT | hand-written dialog (accessibility bugs), `react-aria` (larger) | Follow-ups |
| `class-variance-authority`, `clsx`, `tailwind-merge` | Class composition used by shadcn/ui components (`cn`, variants) | Apache-2.0 / MIT / MIT | template strings (no conflict resolution) | Follow-ups |

## Consequences

Slightly slower to add libraries, much lower risk. A reviewer rejects a PR whose dependency has no row.
