# Roadmap

Current slice: **3 (Identity and ecosystem)**, code complete except the items marked `[~]` or open below; Slice 0 and Slice 1 are code complete (Slice 0 items that need the GitHub repository are still open, see `docs/dev/github.md`). Update this line and the checkboxes in the same PR as the work.

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

- [x] Lists (personal, library, default wishlist), series and missing volumes
- [x] Reading status and statistics, local grades and comments (external grades table is ready; Hardcover fills it in Slice 3)
- [x] Loans and borrowing, notifications (email, Web Push, webhook/ntfy), ICS feed. Web Push is implemented server-side; the browser subscription UI and a custom service-worker push handler are still to do (needs the `injectManifest` PWA strategy)
- [x] Places (tree, QR labels, batch scanning by scanning a label, inventory mode). A graphical shelf/layer display is not done: the tree view is plain text
- [x] Sharing with users and groups, roles and custom permission profiles (API and ADR 0017; the web UI for sharing is not built yet)
- [x] Logical backup and restore, cover storage migration (local to S3 and back). The S3 integration test runs against RustFS in a container
- [x] MCP server (read-write by default, read-only option) behind scoped personal access tokens
- [x] Imports: Goodreads CSV first

## Slice 3: Identity and ecosystem

- [x] OIDC with autodiscovery, group sync, admin group, safe account linking (tested against a fake provider, not a real IdP)
- [x] 2FA (TOTP, recovery codes), passkeys (config-disable), password reset by email (passkeys tested with a software authenticator, not real devices)
- [x] Authorization server and MCP OAuth 2.1 per the current MCP spec (not yet run against a real MCP client)
- [x] SCIM 2.0 Users and Groups (basic filters only; not run against a real IdP)
- [~] External grades done; Hardcover ratings fetched for every book in the background with rate limiting, retry and periodic refresh (ADR 0022; unverified against the live API). Two-way reading-status sync with Hardcover is **not done**
- [x] Addon providers over HTTP, plus Google Books, Library of Congress, K10plus (parsers tested on fixtures, not against live services)
- [~] OPDS feed, Valkey-backed rate limiting, and Goodreads, StoryGraph and LibraryThing importers done. Valkey cache dropped on purpose

## Open decisions (human)

Resolved (see ADR 0009, 0011, 0012, 0013):

- [x] License: AGPL-3.0-or-later.
- [x] GitHub owner `larueli`, repository `biblisann`, Go module `github.com/larueli/biblisann`.
- [x] Documentation site: Zensical, with a separate landing page (placeholder shipped).
- [x] Public repository (agents never process issue or PR text with write permissions or secrets: `AGENTS.md`).
- [x] UI language: browser language, English fallback. Metadata lookup settings per library, per user and per request (ADR 0013).
- [x] Releaser token: GitHub App.
- [x] Pre-1.0: stay on `0.x` until Slice 3 is delivered, then cut `1.0.0` deliberately (`Release-As: 1.0.0`).

Added during Slices 1 to 3 (defaults taken):

- [ ] **Real-device and real-service validation** is missing everywhere it matters: barcode scanning on a phone, passkeys on devices, OIDC/SCIM against a real IdP, MCP OAuth with a real client, BnF/Google/LoC/K10plus/Hardcover live responses, S3 beyond RustFS (the integration test now runs against RustFS 1.0.6).
- [x] Done in the follow-ups: garbage collection, per-user two-way Hardcover sync, the AGPL source link, Prometheus and OpenTelemetry, probe routes, the `oasdiff` gate, self-hosted API docs, PostgreSQL 19 tested.
- [x] **Web UI gaps** closed (ADR 0024): library sharing and profile management, group management, SCIM token creation, Web Push subscription with a custom service worker, a graphical shelf view.
- [x] **Valkey-backed cache**: decided against (no measured bottleneck; revocation risk). Revisit only with measurements. Sessions and OAuth grants can be revoked by users and administrators; reusing a refresh token revokes its whole family (ADR 0024).
- [x] **GDPR**: per-user export, per-library export/import and account deletion exist (ADR 0022). Orphaned cover files are not yet garbage-collected.
- [x] **Version policy**: stay on `0.x.y` until you have tested most of the product. Do not cut `1.0.0` before then; when ready, use `Release-As: 1.0.0`.

Still open (Slice 0 questions that are not resolved yet; the others were answered or implemented):

- [ ] **GitHub actions needed from you**: create the repository, the releaser App, the branch protection and Pages (`docs/dev/github.md`), then push and open the PR. Nothing has been pushed (no remote is configured).
- [x] **TypeScript 7** for the app; the generator runs on TypeScript 5.9 in `web/tools/openapi-gen` (ADR 0005).
- [x] **shadcn/ui**: Button, Input and Dialog live in `web/src/components/ui`; the shared class names in `app/ui.ts` derive from them, so existing screens follow. More components are added when a screen needs them.
- [x] **Helm Pod Security**: `make helm-check` (kubeconform + kube-linter, in prek and CI).

Done since: OpenAPI docs UI (self-hosted), `oasdiff` gate, metrics and tracing, AGPL source link, container UID and PostgreSQL 18 minimum, contributions (inbound = outbound), origin-only base URL, docs domain on GitHub Pages.

## Follow-ups from the security audit (ADR 0025)

Decisions taken (to be written into an ADR with the first implementing PR):

- [x] Administrator edit, delete, provider refresh and "exclude from refreshes" for catalog editions (manual edits are locked against refreshes).
- [x] Email notification channels: confirmed by a code mailed to the address; daily cap per user (`BIBLISANN_EMAIL_DAILY_CAP`).
- [x] Loans visible and returnable by every member with `loan.write`; permission re-checked on return and listing after a member loses access.
- [x] Step-up authentication (valid 5 minutes, also for OIDC users; single-use when the provider sends no `auth_time`) before: password change, TOTP enrollment, passkey add/change, token creation, account deletion, admin password set. SSO-only accounts cannot self-set a password; an administrator can.
- [ ] Quotas with standard rate-limit headers (`RateLimit` / `RateLimit-Policy`, `Retry-After`) for API and bulk imports; the web UI slows down and shows progress to respect them. New feature: bulk add from a list of ISBNs. Account imports are streamed to disk.
- [x] Helm: mount the metrics token as a file, or reference an existing Secret and key (sops, sealed secrets); the binary accepts the env var and the `_FILE` variant.
- [x] Sharing: library sharing requires library admin. Sharing with an unknown email does nothing for non-instance-administrators (same answer as a known one); instance administrators are asked to confirm before an invitation (7-day link, emailed when SMTP is set) is created; the link also lets the invitee register when registration is closed.
- [x] CI: kube-linter and kubeconform on the Helm chart through prek.
- [x] shadcn/ui added to the web UI.
- [x] Generator on TypeScript 5.9 in an isolated tool directory, application on TypeScript 7.
- [x] From the review of ADR 0026: calendar feed filtered by current `loan.write`, `/mcp` and `/opds` metered (MCP `add_book` weighs 5), catalog administrator check in `internal/catalog`, step-up for `admin-delete-user`. Left as is on purpose: private contact names visible to other loan managers, no step-up for disabling users (reversible, needed during incidents). Idea not done: step-up for the full backup download (a plain GET handler).
- [x] Metadata refresh of unknown books: weekly job, administrator-triggered runs per library or for the whole instance, per-book refresh, per-book opt-out (`sync_excluded`), result recorded per edition (`sync_status`, `synced_at`).
