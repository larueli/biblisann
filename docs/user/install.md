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
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=256m \
  -e BIBLISANN_BASE_URL=https://books.example.org \
  -e BIBLISANN_DATABASE_URL_FILE=/run/secrets/database_url \
  -v ./database_url:/run/secrets/database_url:ro \
  -v biblisann-data:/data \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/larueli/biblisann:<version>
```

On a fresh instance the first person to register becomes an administrator. Keep the service
accessible only to the operator until that account is created; then expose it through a TLS reverse
proxy with the desired registration policy.

With local storage, the image sets `BIBLISANN_DATA_DIR=/data` and stores covers under `/data/covers`. The named volume keeps
original covers and thumbnails across container removal; library records live in PostgreSQL, which
needs its own persistent storage and backup. Mount an existing writable directory instead if
preferred, and ensure it is writable by UID 65532. `/tmp` is temporary workspace for imports and
does not replace the data volume. For S3 storage, configure `BIBLISANN_STORAGE_BACKEND=s3` and the
S3 settings in [Configuration](configuration.md).

The publishing workflow is intended to build `linux/amd64` and `linux/arm64` images with SBOM and
provenance and sign the image digest with cosign. Pin a version, ideally by digest; the `latest`
tag can move. The chart is published separately and is not signed by this workflow.

Verify an image using the expected publishing workflow and ref, and GitHub's OIDC issuer. For a
manual `publish.yml` run from `main`, the expected identity is:

```bash
cosign verify ghcr.io/larueli/biblisann@sha256:<digest> \
  --certificate-identity 'https://github.com/larueli/biblisann/.github/workflows/publish.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

For a release that calls the local reusable workflow, use the exact workflow ref from the trusted
release run instead of assuming it is `refs/heads/main`. Establish the expected ref independently
from a trusted release or workflow record; do not accept an identity merely because it is in the
certificate. [Fulcio uses `job_workflow_ref` as the certificate identity](https://github.com/sigstore/fulcio/blob/main/docs/oidc.md), and
[GitHub resolves a local reusable workflow from the caller's commit](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows).
The repository and release pipeline have not yet been exercised on GitHub, so confirm the exact
identity and successful verification for both automatic release and manual republish before relying
on this command. Image signature verification alone does not verify the SBOM or provenance.

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
