#!/usr/bin/env bash
# Create an isolated git worktree and branch for ONE task, optionally starting an agent in it.
#
#   scripts/new-task.sh <slice-task> [claude|codex]
#   NAME=0-config AGENT=claude make task
#
# Worktrees go to a sibling directory (default: ../<repo>-wt/<name>); override with
# TASK_WORKTREES_DIR. Branch is <type>/<name> (default type: feat; override with TASK_TYPE).
# Start point is origin/main when reachable (override with BASE_BRANCH).
set -euo pipefail

name="${1:-}"
agent="${2:-}"

if [[ ! "$name" =~ ^[a-z0-9][a-z0-9-]{1,48}$ ]]; then
  echo "usage: new-task.sh <slice-task> [claude|codex]  (name: lowercase letters, digits, dashes)" >&2
  exit 2
fi
case "$agent" in
  ""|claude|codex) ;;
  *) echo "agent must be claude or codex" >&2; exit 2 ;;
esac

type="${TASK_TYPE:-feat}"
if [[ ! "$type" =~ ^(feat|fix|perf|refactor|docs|test|build|ci|chore)$ ]]; then
  echo "TASK_TYPE must be a Conventional Commit type" >&2
  exit 2
fi

# Works from the primary checkout and from any linked worktree (non-bare repositories).
common="$(git rev-parse --path-format=absolute --git-common-dir)"
main_root="$(dirname "$common")"
base="${BASE_BRANCH:-main}"
branch="${type}/${name}"
dest="${TASK_WORKTREES_DIR:-$(dirname "$main_root")/$(basename "$main_root")-wt}/${name}"

if git -C "$main_root" fetch --quiet origin "$base" 2>/dev/null; then
  start="origin/${base}"
else
  echo "warning: could not fetch origin/${base}; starting from local ${base}" >&2
  start="$base"
fi

if [ -e "$dest" ]; then
  echo "worktree already exists: $dest"
elif git -C "$main_root" show-ref --verify --quiet "refs/heads/${branch}"; then
  git -C "$main_root" worktree add "$dest" "$branch"
else
  git -C "$main_root" worktree add -b "$branch" "$dest" "$start"
fi

echo
echo "task worktree: $dest"
echo "branch:        $branch"
echo "start a session there:  cd \"$dest\" && claude    (or: codex)"
echo "remove when merged:     git worktree remove \"$dest\" && git branch -d \"$branch\""

if [ -n "$agent" ]; then
  cd "$dest"
  exec "$agent"
fi
