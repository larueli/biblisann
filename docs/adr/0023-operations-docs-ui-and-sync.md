# 0023. Operations endpoints, self-hosted API docs, garbage collection and Hardcover sync

Date: 2026-11-23
Status: Accepted

## Decision

- **Probes** (never authenticated, never cached, not logged): `/livez` and `/healthz` (process up), `/startupz` and `/readyz` (the database answers), `/health` (the same checks as JSON with a latency, `503` when degraded). Dependency error text never reaches the response. The Docker `HEALTHCHECK` and the Helm probes use them.
- **Metrics**: Prometheus text at `/metrics` (Go runtime, process, HTTP requests by route pattern and status, database pool). The route **does not exist** unless an operator opts in: `BIBLISANN_METRICS_TOKEN` protects it with a bearer token, `BIBLISANN_METRICS_PUBLIC=true` removes authentication for deployments where the reverse proxy or a NetworkPolicy protects it. **Traces** use OpenTelemetry over OTLP/HTTP and are configured only by the standard `OTEL_*` variables (an intentional exception to the `BIBLISANN_` prefix: every OpenTelemetry operator already knows them); with none set, tracing is off. Access logs carry the `trace_id`. The OTLP endpoint is operator infrastructure (ADR 0016).
- **API documentation** is served by the app itself under `/docs/`: Swagger UI, Redoc, Stoplight Elements and Scalar, plus the specification as OpenAPI 3.1 and 3.0 in JSON and YAML (`/openapi.json`, `/openapi.yaml`, `/openapi-3.0.json`, `/openapi-3.0.yaml`). The viewers are the vendors' prebuilt bundles copied from npm at build time, so no script, style or font is fetched from a third party (Huma's built-in page loads Stoplight from a CDN, which is a supply-chain and privacy dependency we do not want). Only `/docs/` gets a slightly looser Content-Security-Policy (inline styles, blob workers); scripts stay `self`. Two vendor defaults that would reach a third party are switched off or blocked: Scalar's font host (`withDefaultFonts: false`) and Redoc's "powered by" logo (blocked by the policy, hidden by CSS).
- **OpenAPI compatibility gate**: `oasdiff` compares `api/openapi.json` with the target branch in CI (`scripts/api-compat.sh`). A pull request whose title is marked breaking (`feat(api)!:`) is allowed to break, so the release is versioned accordingly.
- **Garbage collection** runs daily in the maintenance job and on demand (`biblisann gc`): soft-deleted copies after 90 days (`BIBLISANN_GC_SOFT_DELETE_DAYS`), editions nobody owns, lists or reads after 30 days, works without editions, cover files and rows without an edition (after 24 hours), superseded change-log rows after 30 days, forgotten tombstones after 180 days, expired short-lived records. A client whose sync cursor is older than the pruned history receives `reset: true` and downloads the library again.
- **Missing cover files** (added later): a weekly job and `biblisann covers-check` list the storage keys once and compare them with the `covers` table (full file and `.thumb`), skipping rows younger than one hour. The check only reports (log, exit status); repairing is left to the operator because the original image source is not stored.
- **Hardcover two-way sync** is per user and chosen by them (pull, push or both): reading status and rating, matched by ISBN, tokens sealed with `BIBLISANN_SECRET_KEY`. A snapshot of the last agreed state tells who changed what; one side changing alone wins, and when both changed differently the more recent edit wins. Runs are rate-limited and budgeted (resumed in the background), a rejected token stops the sync with a message, HTTP 429 delays the next run by Retry-After. Pulling never creates catalog entries.
- **PostgreSQL**: tested on 18 and 19 (`BIBLISANN_TEST_PG_IMAGE`).

## Consequences

- Operators scraping metrics must choose the token or the public flag explicitly.
- The web build output grows by about 11 MB of viewer bundles; they are excluded from the PWA precache.
