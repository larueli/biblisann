# Install

Biblisann is one static binary that embeds the web app. The only required service is PostgreSQL (tested on 18 and 19).

Set `BIBLISANN_BASE_URL` to the public address users type in their browser (scheme and host, no path). Passkeys, OAuth and cookies depend on it.

## Docker Compose

```bash
git clone https://github.com/larueli/biblisann && cd biblisann
# make dev creates random secrets on first run; to do it by hand, see deploy/compose/secrets/README.md
cd deploy/compose/secrets
openssl rand -hex 24 | tr -d '\n' > db_password
printf 'postgres://biblisann:%s@db:5432/biblisann?sslmode=disable' "$(cat db_password)" > database_url
cd ../../..
make dev
```

Then open <http://localhost:8080>. The compose file publishes the port on the loopback interface only: put a reverse proxy with TLS in front, and set `BIBLISANN_BASE_URL` and `BIBLISANN_TRUSTED_PROXIES` accordingly.

## Container image

```bash
docker run --rm \
  --read-only --cap-drop ALL \
  -e BIBLISANN_BASE_URL=https://books.example.org \
  -e BIBLISANN_DATABASE_URL_FILE=/run/secrets/database_url \
  -v ./database_url:/run/secrets/database_url:ro \
  -p 8080:8080 \
  ghcr.io/larueli/biblisann:<version>
```

Images are published for `linux/amd64` and `linux/arm64`, signed with cosign, with SBOM and provenance. There is no `latest` tag: pin a version, ideally by digest.

```bash
cosign verify ghcr.io/larueli/biblisann:<version> \
  --certificate-identity-regexp 'https://github.com/larueli/biblisann/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Kubernetes (Helm)

```bash
helm install biblisann oci://ghcr.io/larueli/charts/biblisann --version <version> \
  --set baseUrl=https://books.example.org \
  --set database.external.existingSecret=biblisann-db   # Secret with key "database-url"
```

Use `--set database.mode=cnpg` to let the chart create a [CloudNativePG](https://cloudnative-pg.io/) cluster instead (the operator must be installed). The chart's defaults satisfy the *restricted* Pod Security Standard: non-root, read-only root filesystem, no capabilities, `RuntimeDefault` seccomp.

### Chart settings that matter

| Value | Purpose |
|-------|---------|
| `metrics.tokenSecret.name` / `.key` | An existing Secret (created by sops, sealed secrets, external-secrets...) holding the `/metrics` bearer token. It is mounted as a file and read through `BIBLISANN_METRICS_TOKEN_FILE`: the token never appears in the pod's environment. |
| `metrics.public` | `true` serves `/metrics` without a token, only when something else protects it (NetworkPolicy, ingress). |
| `tmpSizeLimit` (`256Mi`) | Size of the writable `/tmp` mounted next to the read-only root filesystem. Imports spool covers there: allow 64 MiB per concurrent import (two at a time). |
| `extraEnv`, `extraEnvFrom` | Everything in the [configuration reference](configuration.md): SMTP, S3, OIDC, `BIBLISANN_API_RATE_LIMIT`, `BIBLISANN_EMAIL_DAILY_CAP`. Put secrets in a Secret and use `_FILE` variants or `valueFrom`. |

Mail (invitations, email confirmation codes, password reset) needs SMTP settings; without them those features are off or show the link to the administrator instead.

## From source

Requires Go and Node (see `go.mod` and `web/.nvmrc`) and pnpm.

```bash
make build
BIBLISANN_DATABASE_URL=postgres://... ./bin/biblisann serve
```

Database migrations run automatically at start (`BIBLISANN_MIGRATE_ON_START=false` to disable, then run `biblisann migrate`). A lock makes several replicas starting together safe.

## Compose for a real server

`deploy/compose/compose.yaml` is for development (it builds the image). For a server, start from `deploy/compose/compose.example.yaml` and `deploy/compose/.env.example`:

```bash
mkdir -p /path/to/biblisann/secrets && cd /path/to/biblisann
# copy compose.example.yaml and .env.example here; rename .env.example to .env and edit it
umask 077
pw="$(openssl rand -hex 24)"
printf '%s' "$pw" > secrets/db_password
printf 'postgres://biblisann:%s@db:5432/biblisann?sslmode=disable' "$pw" > secrets/database_url
openssl rand -base64 32 | tr -d '\n' > secrets/secret_key
docker compose -f compose.example.yaml up -d
```

The example runs the published image with a read-only root filesystem and no capabilities, keeps secrets in files, and publishes the port on loopback for your reverse proxy. Every setting is listed in [Configuration](configuration.md).
