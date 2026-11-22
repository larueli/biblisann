#!/usr/bin/env bash
# Sanity checks for the agent-facing documentation. Run by `make docs-check` and CI.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0

required=(AGENTS.md CLAUDE.md README.md docs/spec.md docs/roadmap.md docs/HANDOFF.md docs/agents.md
  docs/dev/versioning.md .pre-commit-config.yaml release-please-config.json .release-please-manifest.json)
for f in "${required[@]}"; do
  [ -f "$f" ] || { echo "missing required file: $f"; fail=1; }
done

# Codex reads AGENTS.md up to 32 KiB; stay well under.
size=$(wc -c < AGENTS.md)
if [ "$size" -gt 28000 ]; then
  echo "AGENTS.md is ${size} bytes (limit 28000)"
  fail=1
fi

# Every role file referenced by an agent wrapper must exist.
while IFS= read -r ref; do
  [ -f "$ref" ] || { echo "dangling role reference: $ref"; fail=1; }
done < <(grep -rhoE 'docs/agents/roles/[a-z-]+\.md' .claude/agents .codex/agents | sort -u)

# Every role must be wrapped for both tools (Codex names use underscores).
for role in docs/agents/roles/*.md; do
  name=$(basename "$role" .md)
  [ -f ".claude/agents/${name}.md" ] || { echo "no Claude wrapper for role: $name"; fail=1; }
  [ -f ".codex/agents/${name//-/_}.toml" ] || { echo "no Codex wrapper for role: $name"; fail=1; }
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "docs-check: ok"
