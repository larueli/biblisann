# API documentation and the OpenAPI document

The server describes its API with an OpenAPI document generated from the Go code (Huma). It is served without authentication:

| URL | Content |
|-----|---------|
| `/openapi.json`, `/openapi.yaml` | OpenAPI 3.1 |
| `/openapi-3.0.json`, `/openapi-3.0.yaml` | the same document downgraded to 3.0.3, for tools that do not read 3.1 |
| `/docs/` | chooser for the interactive viewers: Swagger UI, Redoc, Stoplight Elements, Scalar |

The viewers are self-hosted: `web/vite.config.ts` copies the vendors' prebuilt bundles from `node_modules` into `docs/vendor/` of the build, and `web/public/docs/*.html` load them from the same origin. Nothing is fetched from a CDN. To add a viewer, add its package as a dev dependency (ADR 0005 row), list its files in `apiDocViewers`, add a page in `web/public/docs/`, and extend the Playwright test (`e2e/smoke.spec.ts`), which fails on any Content-Security-Policy violation. Pages under `/docs/` get a looser policy (inline styles, blob workers), set in `internal/httpmw`.

`api/openapi.json` is committed (`make generate` rewrites it) and CI compares it with the target branch using `oasdiff` (`make api-compat BASE=origin/main`). Mark a deliberately breaking pull request with `!` in its title (`feat(api)!: ...`).
