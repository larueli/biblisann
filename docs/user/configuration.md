# Configuration

Every setting is an environment variable named `BIBLISANN_<NAME>`. Sensitive settings also accept `BIBLISANN_<NAME>_FILE`, containing the path of a file that holds the value (Docker/Kubernetes secrets).

An optional file in `KEY=value` form (full variable names, `#` comments) can be given with `BIBLISANN_CONFIG_FILE`.

!!! warning "One source per value"
    Giving the same value twice (variable and `_FILE`, or environment and config file) stops the server at startup with an error that names the setting. Error messages never print values.

| Variable | Default | Sensitive | Description |
|----------|---------|-----------|-------------|
| `BIBLISANN_DATABASE_URL` | none, required | yes (`_FILE`) | PostgreSQL connection URL. |
| `BIBLISANN_BASE_URL` | `http://localhost:8080` | no | Public origin, no path. `https` enables HSTS. |
| `BIBLISANN_LISTEN_ADDR` | `:8080` | no | Address the server listens on. |
| `BIBLISANN_TRUSTED_PROXIES` | empty | no | Comma-separated CIDRs or IPs of reverse proxies allowed to set `X-Forwarded-For`. Empty: the header is ignored. |
| `BIBLISANN_LOG_LEVEL` | `info` | no | `debug`, `info`, `warn`, `error`. |
| `BIBLISANN_LOG_FORMAT` | `json` | no | `json` or `text`. |
| `BIBLISANN_SHUTDOWN_TIMEOUT` | `20s` | no | Time allowed to finish in-flight requests on SIGTERM. |
| `BIBLISANN_MIGRATE_ON_START` | `true` | no | Apply database migrations at start. |
| `BIBLISANN_REGISTRATION` | `first-user` | no | `first-user`: only the very first account may register (it becomes the administrator). `open`: anyone. `closed`: nobody (administrators create accounts). |
| `BIBLISANN_COVERS_DIR` | `data/covers` (`/data/covers` in the image) | no | Where cover images are stored. Mount a volume. |
| `BIBLISANN_PROVIDERS` | `bnf,openlibrary` | no | Default metadata providers, in priority order. Libraries, users and single requests can override the list. |
| `BIBLISANN_DISABLED_PROVIDERS` | empty | no | Providers disabled for everyone. Each lookup sends an ISBN to the provider: disable providers you do not want to leak to. |
| `BIBLISANN_STATIC_PROVIDER_FILE` | none | no | JSON file `{"<isbn13>": {"title": "...", "authors": [...]}}` served by the `static` provider (add `static` to `BIBLISANN_PROVIDERS`). No network. |
| `BIBLISANN_STORAGE_BACKEND` | `local` | no | `local` (covers in `BIBLISANN_COVERS_DIR`) or `s3`. |
| `BIBLISANN_S3_ENDPOINT`, `_BUCKET`, `_REGION`, `_ACCESS_KEY`, `_PREFIX` | none | no | S3 location (endpoint is `host:port`, no scheme). The bucket must exist. |
| `BIBLISANN_S3_SECRET_KEY` | none | yes (`_FILE`) | S3 secret key. `BIBLISANN_S3_USE_SSL` (default `true`). |
| `BIBLISANN_SMTP_HOST`, `_PORT` (587), `_USERNAME`, `_FROM`, `_MODE` | none | no | Outgoing email. `_FROM` is required with a host. `_MODE`: `starttls` (default), `tls`, `none` (trusted local relay only). |
| `BIBLISANN_SMTP_PASSWORD` | none | yes (`_FILE`) | SMTP password; only sent over TLS. |
| `BIBLISANN_VAPID_PUBLIC_KEY`, `BIBLISANN_VAPID_PRIVATE_KEY` | none | private key: yes (`_FILE`) | Web Push keys; generate with `biblisann vapid`. Set both or none. `BIBLISANN_VAPID_SUBJECT` is the contact URL (defaults to the base URL). |
| `BIBLISANN_SECRET_KEY` | none | yes (`_FILE`) | Base64 of 32 random bytes (`openssl rand -base64 32`). Seals authenticator secrets; without it two-factor is off. Losing it disables enrolled authenticators. |
| `BIBLISANN_PASSKEYS` | `true` | no | `false` turns passkey sign-in off. Passkeys need `BIBLISANN_BASE_URL` to be the real https address (or localhost). |
| `BIBLISANN_OIDC_ISSUER`, `_CLIENT_ID`, `_SCOPES`, `_GROUPS_CLAIM` (`groups`), `_ADMIN_GROUP`, `_AUTO_CREATE` (`true`), `_LABEL` | none | no | Single sign-on. Redirect URI to register at the provider: `<BASE_URL>/v1/auth/oidc/callback`. |
| `BIBLISANN_OIDC_CLIENT_SECRET` | none | yes (`_FILE`) | OIDC client secret. |
| `BIBLISANN_GOOGLE_BOOKS_KEY` | none | yes (`_FILE`) | Optional Google Books API key (add `google` to `BIBLISANN_PROVIDERS` to use the provider). Other providers: `loc`, `k10plus`. |
| `BIBLISANN_ADDON_PROVIDERS_FILE` | none | no (the file holds tokens) | JSON list `[{"name":"mine","url":"https://...","token":"..."}]` of addon providers. |
| `BIBLISANN_HARDCOVER_TOKEN` | none | yes (`_FILE`) | Enables community ratings from Hardcover. |
| `BIBLISANN_VALKEY_URL` | none | yes (`_FILE`) | `redis://` or `rediss://` URL; shares rate limits between replicas. |
| `BIBLISANN_CONFIG_FILE` | none | no | Path of an optional config file. |

## Endpoints for operators

| Path | Purpose |
|------|---------|
| `/healthz` | Liveness: the process answers. |
| `/readyz` | Readiness: the database answers. |
| `/v1/version` | Running version. |
| `/mcp` | MCP endpoint (bearer token or OAuth). |
| `/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server` | OAuth discovery for MCP clients. |
| `/scim/v2/` | SCIM 2.0 provisioning (token created by an administrator). |
| `/opds/<feed token>/` | OPDS catalog. |
| `/openapi.json`, `/openapi.yaml` | API description (no authentication). |

## Kubernetes note

Kubernetes injects `<SERVICE>_*` variables for every Service in the namespace. A Service named `biblisann` produces `BIBLISANN_PORT=tcp://...`. Unknown `BIBLISANN_*` variables are ignored, and the Helm chart sets `enableServiceLinks: false`.
