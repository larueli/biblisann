#!/usr/bin/env bash
# Compare the OpenAPI spec of this checkout with another git ref and fail on breaking changes.
#
#   scripts/api-compat.sh origin/main          (also: make api-compat BASE=origin/main)
#
# A breaking change is allowed when the change is declared as such: set BREAKING=1 (CI does it when the
# pull request title is a Conventional Commit marked with "!"), so the release gets the right version.
set -euo pipefail
cd "$(dirname "$0")/.."
base_ref="${1:?usage: api-compat.sh <git ref>}"
version="${OASDIFF_VERSION:-v1.39.0}"

if ! git cat-file -e "$base_ref:api/openapi.json" 2>/dev/null; then
  echo "api-compat: no api/openapi.json at $base_ref (first spec): nothing to compare"
  exit 0
fi

# The base spec is read straight from git through a pipe: no temporary file to clean up.
if GOMEMLIMIT=1GiB go run "github.com/oasdiff/oasdiff@${version}" breaking <(git show "$base_ref:api/openapi.json") api/openapi.json --fail-on ERR; then
  echo "api-compat: no breaking change against $base_ref"
elif [ "${BREAKING:-0}" = "1" ]; then
  echo "api-compat: breaking changes found, allowed because the change is declared breaking (!)"
else
  echo "api-compat: breaking API changes. Fix them, or mark the PR title with '!' and add a BREAKING CHANGE footer." >&2
  exit 1
fi
