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
| `BIBLISANN_CONFIG_FILE` | none | no | Path of an optional config file. |

## Endpoints for operators

| Path | Purpose |
|------|---------|
| `/healthz` | Liveness: the process answers. |
| `/readyz` | Readiness: the database answers. |
| `/v1/version` | Running version. |
| `/openapi.json`, `/openapi.yaml` | API description (no authentication). |

## Kubernetes note

Kubernetes injects `<SERVICE>_*` variables for every Service in the namespace. A Service named `biblisann` produces `BIBLISANN_PORT=tcp://...`. Unknown `BIBLISANN_*` variables are ignored, and the Helm chart sets `enableServiceLinks: false`.
