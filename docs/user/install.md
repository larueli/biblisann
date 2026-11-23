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

## From source

Requires Go and Node (see `go.mod` and `web/.nvmrc`) and pnpm.

```bash
make build
BIBLISANN_DATABASE_URL=postgres://... ./bin/biblisann serve
```

Database migrations run automatically at start (`BIBLISANN_MIGRATE_ON_START=false` to disable, then run `biblisann migrate`). A lock makes several replicas starting together safe.
