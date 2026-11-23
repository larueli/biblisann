# 0021. More providers, addons, Hardcover, OPDS and shared rate limiting

Date: 2026-11-23
Status: Accepted

## Decision

- New built-in providers: Google Books, Library of Congress and K10plus (SRU returning MARCXML, one shared parser). They are available but **not in the default list** (`bnf,openlibrary`): every lookup sends an ISBN to the provider, so enabling them is an explicit choice (`BIBLISANN_PROVIDERS`, per library, per request). The MARCXML and Google parsers are tested on recorded fixtures, not against the live services.
- **Addon providers** are out-of-process HTTP services: `GET <url>?isbn=<isbn13>` with an optional bearer token, answering a JSON record (`docs/dev/addons.md`) or 404. They are declared in a JSON file (`BIBLISANN_ADDON_PROVIDERS_FILE`, https only, names unique), called through safehttp, and their output is treated as untrusted (only https cover URLs, bounded fields).
- **Hardcover**: community ratings are fetched on demand into the external grades table. The GraphQL query follows Hardcover's public schema but was only tested against recorded responses. Per-user two-way reading-status sync with Hardcover is **not implemented**.
- **OPDS 1.2** catalog (`/opds/<feed token>/`): metadata and covers, no acquisition links (the app hosts no book files). The feed token only opens feeds.
- **Valkey** (go-redis, also fine with Redis) shares the login/registration rate limits across replicas with a GCRA script. If it is unreachable, a per-process limiter takes over rather than failing open or closed. It is optional.

## Consequences

- Listing a provider by name never changes the default privacy posture.
