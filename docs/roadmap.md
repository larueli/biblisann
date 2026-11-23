# Roadmap

Current slice: **2 (Organise and lend)** in progress; Slice 0 and Slice 1 are code complete (Slice 0 items that need the GitHub repository are still open, see `docs/dev/github.md`). Update this line and the checkboxes in the same PR as the work.

Rule for every slice: demoable end to end, tested, documented, `make check` green, ADRs written for decisions taken.

## Slice 0: Foundations

- [x] Repo hygiene: LICENSE (AGPL-3.0), branch protection notes in `docs/dev/github.md`, GitHub Actions pinned by SHA, tool versions pinned
- [x] Go module, `cmd/biblisann`, config package (env, `*_FILE`, optional file, conflicts are errors), `slog`, graceful shutdown
- [x] Health and readiness endpoints, `BASE_URL` and trusted-proxy handling
- [x] PostgreSQL: pgx, sqlc, goose migrations run under an advisory lock, testcontainers harness
- [x] API skeleton with OpenAPI 3.1 from code, served unauthenticated, version from build info, RFC 9457 errors
- [x] `internal/safehttp` (SSRF-safe client) with tests
- [x] `web/`: Vite + React + TS strict, i18n (en, fr), PWA manifest and service worker, generated API client, embedded in the binary
- [x] `Dockerfile` (multi-stage, distroless, non-root, read-only root fs), Compose file (app + PostgreSQL), Helm chart skeleton (external DB and CNPG values, restricted PSS defaults)
- [x] Hooks with prek: hook revs frozen, `zizmor` enabled, `make commit-lint` confirmed (needs the config staged). Still to confirm: `make hooks` and `make check` on a clean clone (`make check` is green in the task worktree)
- [ ] CI green on GitHub Actions: jobs `check` (runs `make check`, same as local), `pr-title`, `image`, `helm`, `e2e` (written and pinned, never run on GitHub: needs the repository)
- [x] **Releaser, code side**: version in the build (`VERSION` build arg, `-ldflags -X`, `/v1/version`, OpenAPI `info.version`), Helm chart version through release-please `extra-files`, GitHub App token step in `release-please.yml`
- [ ] **Releaser, GitHub side** (human, `docs/dev/github.md`): create the repository, the GitHub App (`RELEASE_APP_ID`, `RELEASE_APP_PRIVATE_KEY`), squash-merge with the PR title as message, protect `main` (required checks `check` and `pr-title`); make the first release PR and merge it to validate end to end
- [ ] **Publish** (`publish.yml`, written; `Dockerfile` accepts `VERSION`): verify the multi-arch image and Helm chart pushed to GHCR with cosign signature, SBOM and provenance; verify with `cosign verify` and `docker buildx imagetools inspect`; test manual republish through `workflow_dispatch`
- [ ] Task worktrees: `make task` works on Linux (used for this slice); confirm `AGENT=claude|codex` and WSL on the target machine
- [x] `golangci-lint` and Biome configs, Playwright smoke test
- [x] `docs/dev/architecture.md` stub and `docs/user/` skeleton

## Slice 1: Scan and own (MVP)

- [~] **Spike:** PWA scan page with `BarcodeDetector` + WASM fallback implemented (ADR 0015). **Still to do by a human:** test on a real Android phone against real book backs and record the result in ADR 0015.
- [x] Schema: users, libraries, works, editions, instances, places (flat for now)
- [x] Local auth: Argon2id, cookie sessions, CSRF, rate limiting, password policy
- [x] ISBN normalization and validation, ISBN-less manual entry
- [x] Provider interface, BnF (SRU, UNIMARC) and Open Library, per-field merge with provenance, cache. Cover download is synchronous for now; the PostgreSQL job queue (River) arrives in Slice 2 with notifications
- [x] Covers: storage interface (local first), re-encoding, thumbnails
- [x] Add by scan, list, search (FTS + trigram + unaccent), "do I own this?" at Work level
- [x] Offline index sync (cursor + tombstones) and scan outbox with idempotency keys
- [x] Playwright e2e of the scan-to-list flow; user docs for first run

## Slice 2: Organise and lend

- [ ] Lists (personal, library, default wishlist), series and missing volumes
- [ ] Reading status and statistics, local grades and comments
- [ ] Loans and borrowing, notifications (email, Web Push, webhook/ntfy), ICS feed
- [ ] Places, rooms, shelves, layers with visual display, QR labels, batch scanning, inventory mode
- [ ] Sharing with users and groups, roles and custom permission profiles
- [ ] Logical backup and restore, cover storage migration (local to S3 and back)
- [ ] MCP server (read-write by default, read-only option) behind scoped personal access tokens
- [ ] Imports: Goodreads CSV first

## Slice 3: Identity and ecosystem

- [ ] OIDC with autodiscovery, group sync, admin group, safe account linking
- [ ] 2FA (TOTP, recovery codes), passkeys (config-disable), password reset by email
- [ ] Authorization server and MCP OAuth 2.1 per the current MCP spec
- [ ] SCIM 2.0 Users and Groups
- [ ] Hardcover sync, external grades
- [ ] Addon providers over HTTP, plus Google Books, Library of Congress, K10plus
- [ ] OPDS feed, more importers, Valkey-backed rate limiting and cache for HA

## Open decisions (human)

Resolved (see ADR 0009, 0011, 0012, 0013):

- [x] License: AGPL-3.0-or-later.
- [x] GitHub owner `larueli`, repository `biblisann`, Go module `github.com/larueli/biblisann`.
- [x] Documentation site: Zensical, with a separate landing page (placeholder shipped).
- [x] Public repository (agents never process issue or PR text with write permissions or secrets: `AGENTS.md`).
- [x] UI language: browser language, English fallback. Metadata lookup settings per library, per user and per request (ADR 0013).
- [x] Releaser token: GitHub App.
- [x] Pre-1.0: stay on `0.x` until Slice 3 is delivered, then cut `1.0.0` deliberately (`Release-As: 1.0.0`).

Still open (questions saved during the unattended Slice 0 build, with the default I took):

- [ ] **GitHub actions needed from you**: create the repository, the releaser App, the branch protection and Pages (`docs/dev/github.md`), then push `feat/0-foundations` and open the PR. Nothing was pushed.
- [ ] **TypeScript 5.9 instead of 7**: `openapi-typescript` 7.13 crashes on TypeScript 7 (no JS compiler API). Default taken: pin 5.9.3, revisit when the generator supports 7 (or switch generator).
- [ ] **OpenAPI docs UI**: disabled because Huma's bundled UI loads third-party scripts (CSP forbids them). Options: embed a UI (Scalar or Swagger UI) as static files, or keep only the raw spec. Default: raw spec only.
- [ ] **Breaking-change gate** (`oasdiff`, spec section 7) is not wired: it needs a baseline from `main`. Default: add it as soon as the first release exists.
- [ ] **Metrics and tracing** (Prometheus, OpenTelemetry, spec section 9) are not in Slice 0. Default: Slice 1, behind config.
- [ ] **shadcn/ui and Dexie** are in the default stack but not installed yet (nothing uses them). Default: add them with the first screen and the offline index in Slice 1.
- [ ] **Container UID** is 65532 (distroless `nonroot`); PostgreSQL 18 is the tested version (Compose, CI, tests). Confirm you want PG 18 as the minimum, or lower it.
- [ ] **Helm Pod Security**: the chart is restricted-compliant by construction, but no tool checks it in CI (kubeconform, kube-linter, or a kind cluster with PSS enforced). Default: add a `kube-linter` job later.
- [ ] **Contributions**: no DCO or CLA. Confirm that "inbound = outbound" (AGPL) is enough, which rules out a later dual license without asking contributors.
- [ ] **Source link for AGPL section 13**: the UI must offer the source to network users. Default: footer link in Slice 1 (needs a configurable URL for forks).
- [ ] **Sub-path deployment** (`https://host/biblisann/`) is rejected by `BIBLISANN_BASE_URL`. Default: origin only; revisit if requested.
- [ ] **Docs domain**: Pages at `larueli.github.io/biblisann`. Custom domain not decided.
