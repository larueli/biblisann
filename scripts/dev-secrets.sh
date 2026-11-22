#!/usr/bin/env bash
# Create the local Compose secrets once (random database password). Never overwrites.
set -euo pipefail
dir="$(cd "$(dirname "$0")/.." && pwd)/deploy/compose/secrets"
[ -f "$dir/db_password" ] && [ -f "$dir/database_url" ] && exit 0
umask 077
pw="$(openssl rand -hex 24)"
printf '%s' "$pw" > "$dir/db_password"
printf 'postgres://biblisann:%s@db:5432/biblisann?sslmode=disable' "$pw" > "$dir/database_url"
echo "created Compose secrets in $dir"
