# Architecture (Slice 0)

One Go binary serves everything: the REST API, the embedded web app, health probes and (later) the MCP server. PostgreSQL is the only required service. Decisions are in `docs/adr/`.

```text
cmd/biblisann        entrypoint and subcommands (serve, migrate, healthcheck, openapi, version)
internal/app         wiring and lifecycle (graceful shutdown)
internal/config      env, *_FILE and optional file; conflicts are errors; secrets are redacted
internal/httpmw      request ID, trusted-proxy client IP, security headers, access log, recover
internal/api         Huma on net/http ServeMux: health, /v1 operations, OpenAPI 3.1, RFC 9457 errors
internal/db          pgx pool, goose migrations under an advisory lock
internal/db/queries  SQL (sqlc input); generated code in internal/db/sqlc
internal/safehttp    the only outbound HTTP client (SSRF protection)
internal/webui       embedded build of web/ with SPA fallback and cache headers
migrations/          forward-only goose SQL, embedded
web/                 React + Vite + TypeScript PWA (builds into internal/webui/dist)
api/openapi.json     generated spec, committed; web/src/api/schema.d.ts is generated from it
```

## Rules enforced in code

- **Permissions**: operations are registered with `api.Register(..., perm, handler)`; an empty permission panics at start-up and a test checks every operation in the generated spec carries `x-permission`. Real authorization lives in the service layer (Slice 1).
- **Outbound HTTP**: only `internal/safehttp`. The address is checked at connect time (DNS rebinding safe), redirects are re-validated, bodies and time are bounded. `golangci-lint` (`forbidigo`) rejects `http.Get`, `http.DefaultClient` and `http.Client` elsewhere.
- **SQL**: parameterized, in `internal/db/queries/*.sql`, compiled by sqlc.
- **Generated files** (`api/openapi.json`, `internal/db/sqlc`, `web/src/api/schema.d.ts`): `make generate`; the `generate-check` hook fails if they are stale.

## Request path

`RequestID -> ClientIP (trusted proxies only) -> AccessLog -> Recover -> SecurityHeaders -> mux`. The mux serves `/healthz`, `/readyz`, `/openapi.*`, `/v1/*` (unknown paths answer a problem+json 404), and everything else goes to the SPA.

## Web

The Helm chart is rendered and checked by `make helm-check` (kubeconform for schemas, kube-linter for hardening, configuration in `scripts/kube-linter.yaml`; it runs in prek and CI). Strict TypeScript, feature folders, TanStack Router and Query, Tailwind, i18next (`en` and `fr` keys must match; a test enforces it). The language comes from the browser and falls back to English. The service worker never answers `/v1`, `/openapi.*` or probe paths. Offline storage (Dexie), the shadcn/ui components in `web/src/components/ui` and the barcode scanner are in place. The app is type-checked with TypeScript 7; `openapi-typescript` runs with TypeScript 5.9 in `web/tools/openapi-gen` (it needs the JavaScript compiler API).

## Local development

```bash
make dev            # Compose: PostgreSQL + app image (needs deploy/compose/secrets)
cd web && pnpm dev  # Vite dev server, proxies /v1 to localhost:8080
make check          # every gate, same as CI
```
