# Biblisann specification

Status: draft v0.1. Slices in `roadmap.md` say what is built when. Everything here is a requirement, not a promise of the first release.

## 1. Questions the product answers

1. Where are my books?
2. What do I own?
3. Do I already own this book? Must work offline on a phone, for example in a shop.
4. Have I read it?
5. Have I lent it, or borrowed it from a person or library I must return it to? Notify me.

## 2. Domain model

- **Work**: the abstract book. **Edition**: a published form of a work, usually identified by an ISBN. **Instance**: one physical (or ebook) copy owned in a library.
- The Work/Edition catalog is shared globally. Instances, notes, grades and states belong to libraries and users.
- "Do I own this?" matches at Work level: "you own another edition" is a valid answer.
- Books without ISBN are first-class: manual entry, EAN-only, ISSN.
- **Series / collection**: ordered, with a position. Missing volumes can be added to a list in one action.
- **Instance**: free-form notes, overall state (ok, damaged, ...), kind (print, ebook). Several instances of one edition are allowed.
- **Grades and comments**: local (per user) and external (critics, providers), shown separately.
- **Reading status** per user and edition or work (to read, reading, read, abandoned), with dates and overall statistics. Optional per-user sync with Hardcover.
- **Lists**: custom lists, personal or library-owned, shareable. A list item may or may not be in a library. Every user has a default wishlist.
- **Loans**: lent to a contact (not necessarily a user) or borrowed from a person or library. Due dates and notifications.
- **Audit log**: who changed or moved what. Soft delete with undo.

## 3. Libraries and places

- Users create their own libraries. A library has places (house, storage box, ...), rooms, shelves, layers, with a visual display.
- Instances move between places and across libraries of different users.
- QR labels for places and shelves. Scanning a place label sets the current location for batch scanning. Inventory mode lists expected-but-missing books.

## 4. Metadata

- Lookup by ISBN (10 and 13, normalized) through several providers: BnF, Google Books, Open Library, Library of Congress, K10plus; more later.
- Providers are pluggable: built-in ones in the binary, addons as out-of-process HTTP services that implement a small OpenAPI contract.
- Per-field merge with provenance and per-field user lock. Caching, rate limits, circuit breakers, background jobs.
- Covers: multiple per edition, content-addressed, thumbnails. Similar-title and duplicate handling at Work level.
- Providers can be disabled per instance (queries leak what a user owns). Respect provider terms of use.

## 5. Mobile and offline

- Installable PWA, Android first. Barcode scanning with `BarcodeDetector` and a WASM fallback.
- Offline: the client holds a compact index (ISBN, title, authors, ownership, reading state) synced by cursor with tombstones. Offline scans go to an outbox replayed with idempotency keys.
- Capacitor wrapper is allowed later if scanning quality requires it. The web app stays the single UI codebase.

## 6. Users, authentication, authorization

- Local user/password (Argon2id), OIDC with autodiscovery, password reset by email when enabled in config.
- 2FA (TOTP, recovery codes) and passkeys. Passkeys can be disabled by config, notably with OIDC.
- OIDC group sync. SCIM 2.0 Users and Groups. Instance admins can be derived from an OIDC group whose name is configured.
- Libraries are shared with users and groups with a role (read-only, editor, administrator) or a custom permission profile defined per library.
- Web uses same-origin cookie sessions. API, mobile and MCP use bearer tokens. Personal access tokens for scripts.

## 7. API

- OpenAPI 3.1 generated from code, versioned with the app, served without authentication (`/openapi.json`, `/openapi.yaml`). Optional docs UI.
- RFC 9457 problem details, cursor pagination, idempotency keys, ETag with `If-Match`, rate-limit headers, `/v1` prefix.
- CI blocks accidental breaking changes (`oasdiff`).

## 8. MCP

- Streamable HTTP MCP server in the same binary. Can do everything a user can do.
- OAuth 2.1 authorization (the app is its own authorization server for local users and federates OIDC). Follow the current MCP authorization specification: protected resource metadata, PKCE, resource indicators, client registration as the spec requires.
- Scopes: read-only or read-write (read-write by default at consent time), restrictable per library.
- Curated, task-oriented tools with read-only/destructive annotations. Provider and user text returned to the model is untrusted.
- Works with MCP clients and gateways such as Executor, or directly from an agent harness.

## 9. Deployment and operations

- Bare system, Docker/Compose, Kubernetes (Helm chart; external database or CloudNativePG).
- May run behind a reverse proxy or not: explicit `BASE_URL` and trusted-proxy configuration (needed for passkeys and OAuth redirects).
- PostgreSQL with forward-only migrations, safe for rolling upgrades, run under an advisory lock.
- Cover storage: local files or S3-compatible, with a command to migrate between them. S3 recommended for HA.
- Backup and restore from the app: a logical, versioned export (NDJSON plus covers) that restores across schema versions. Database-level DR is documented separately (pg_dump, CNPG/barman).
- Health and readiness endpoints, Prometheus metrics, OpenTelemetry, structured logs.

## 10. Configuration

- Everything configurable by env var (`BIBLISANN_*`) or config file. Sensitive values accept `*_FILE`.
- Env and file for the same value at once is a startup error.

## 11. Non-functional

- Low memory and CPU (target: idle under 100 MiB RSS on the single binary, to be measured).
- Multilingual UI (en, fr first) and metadata language preferences.
- Accessibility baseline (WCAG 2.2 AA for core flows).
- GDPR: export and deletion of a user's data.
- Supply chain: pinned dependencies and actions, SBOM, signed images, provenance.
- High availability feasible: stateless app, sessions in PostgreSQL, S3 for covers.

## 12. Out of scope for now

Ebook file hosting (instances may link to files but the app does not serve them), social features, price tracking, native iOS app.
