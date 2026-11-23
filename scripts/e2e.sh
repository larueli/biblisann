#!/usr/bin/env bash
# Starts a throwaway PostgreSQL and the built binary, runs the Playwright smoke test, cleans up.
set -euo pipefail
cd "$(dirname "$0")/.."
name="biblisann-e2e-$$"
port="${E2E_PG_PORT:-55439}"
cleanup() { kill "${app_pid:-0}" 2>/dev/null || true; docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "${tmp:-}"; }
trap cleanup EXIT
tmp="$(mktemp -d)"
docker run -d --name "$name" -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=biblisann -p "127.0.0.1:${port}:5432" \
  postgres:18-alpine@sha256:0115267ab5a509022df9319c77be365cef335adb71556c75f60f5360ee247e71 >/dev/null
# The image starts a temporary server for initialization, then restarts: wait for the second "ready".
for _ in $(seq 1 60); do [ "$(docker logs "$name" 2>&1 | grep -c "ready to accept connections")" -ge 2 ] && break; sleep 1; done
BIBLISANN_DATABASE_URL="postgres://postgres:e2e@127.0.0.1:${port}/biblisann?sslmode=disable" \
BIBLISANN_COVERS_DIR="$tmp/covers" BIBLISANN_LISTEN_ADDR=127.0.0.1:18080 BIBLISANN_BASE_URL=http://127.0.0.1:18080 \
BIBLISANN_PROVIDERS=static BIBLISANN_STATIC_PROVIDER_FILE="$PWD/web/e2e/fixtures/books.json" \
  ./bin/biblisann serve >"$tmp/app.log" 2>&1 &
app_pid=$!
for _ in $(seq 1 30); do curl -fs http://127.0.0.1:18080/readyz >/dev/null && break; sleep 1; done
(cd web && BIBLISANN_E2E_URL=http://127.0.0.1:18080 pnpm run e2e -- ${E2E_ARGS:-}) || { cat "$tmp/app.log"; exit 1; }
