# Roadmap

Current slice: **0 (Foundations)**. Update this line and the checkboxes in the same PR as the work.

Rule for every slice: demoable end to end, tested, documented, `make check` green, ADRs written for decisions taken.

## Slice 0: Foundations

- [ ] Repo hygiene: choose and add LICENSE, branch protection notes in `docs/dev/github.md`, pin GitHub Actions by SHA, pin tool versions
- [ ] Go module, `cmd/biblisann`, config package (env, `*_FILE`, optional file, conflicts are errors), `slog`, graceful shutdown
- [ ] Health and readiness endpoints, `BASE_URL` and trusted-proxy handling
- [ ] PostgreSQL: pgx, sqlc, goose migrations run under an advisory lock, testcontainers harness
- [ ] API skeleton with OpenAPI 3.1 from code, served unauthenticated, version from build info, RFC 9457 errors
- [ ] `internal/safehttp` (SSRF-safe client) with tests
- [ ] `web/`: Vite + React + TS strict, i18n (en, fr), PWA manifest and service worker, generated API client, embedded in the binary
- [ ] `Dockerfile` (multi-stage, distroless, non-root, read-only root fs), Compose file (app + PostgreSQL), Helm chart skeleton (external DB and CNPG values, restricted PSS defaults)
- [ ] Hooks with prek: `.pre-commit-config.yaml` exists; verify `make hooks` and `make check` on a clean clone; freeze every hook `rev` with `prek update --freeze`; enable the `zizmor` hook once Actions are pinned; confirm `make commit-lint` works with the pinned prek (`--commit-msg-filename`), otherwise replace it with a small script that reuses the same type list
- [ ] CI green on GitHub Actions: jobs `check` (runs `make check`, same as local), `pr-title`, `image`, `helm`
- [ ] **Releaser** (ADR 0009, `docs/dev/versioning.md`): release-please manifest mode is configured; create the `RELEASE_PLEASE_TOKEN` secret, set squash-merge with the PR title as message, protect `main` (required checks `check` and `pr-title`), add the version to the build (`VERSION` build arg, `-ldflags -X`, `/version`, OpenAPI `info.version`) and to the Helm chart `appVersion` through release-please `extra-files`; make the first release PR and merge it to validate end to end
- [ ] **Publish** (`publish.yml`): `Dockerfile` accepts `VERSION`; multi-arch image and Helm chart pushed to GHCR with cosign signature, SBOM and provenance; verify with `cosign verify` and `docker buildx imagetools inspect`; test manual republish through `workflow_dispatch`
- [ ] Task worktrees: confirm `make task` (and `AGENT=claude|codex`) works from WSL on the target machine
- [ ] `golangci-lint` and Biome configs, Playwright smoke test
- [ ] `docs/dev/architecture.md` stub and `docs/user/` skeleton

## Slice 1: Scan and own (MVP)

- [ ] **Spike first (one day):** PWA scan page with `BarcodeDetector` + WASM fallback tested on a real Android phone against real book backs. Record the result in an ADR. If poor, plan the Capacitor wrapper.
- [ ] Schema: users, libraries, works, editions, instances, places (flat for now)
- [ ] Local auth: Argon2id, cookie sessions, CSRF, rate limiting, password policy
- [ ] ISBN normalization and validation, ISBN-less manual entry
- [ ] Provider interface, BnF (SRU, UNIMARC) and Open Library, per-field merge with provenance, cache, jobs
- [ ] Covers: storage interface (local first), re-encoding, thumbnails
- [ ] Add by scan, list, search (FTS + trigram + unaccent), "do I own this?" at Work level
- [ ] Offline index sync (cursor + tombstones) and scan outbox with idempotency keys
- [ ] Playwright e2e of the scan-to-list flow; user docs for first run

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

- [ ] License (AGPL-3.0 is the usual choice for self-hosted network services)
- [ ] GitHub owner, repository name and Go module path
- [ ] Documentation site tool (plain Markdown vs MkDocs Material)
- [ ] Public vs private repository (affects GHCR visibility and Actions minutes)
- [ ] Default language of the UI and of metadata lookups
- [ ] Token for the releaser: GitHub App (preferred) or fine-grained PAT, so CI runs on release PRs
- [ ] Pre-1.0 policy: keep `0.x` (breaking changes bump MINOR) and decide the criteria for cutting `1.0.0`
