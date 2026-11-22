@AGENTS.md

## Claude Code specifics

- Everything that matters is in `AGENTS.md`. Do not duplicate rules here.
- Default model is `opusplan` (see `.claude/settings.json`): Opus plans, Sonnet executes. Switch with `/model`.
- Project subagents live in `.claude/agents/`. They are thin wrappers around the tool-neutral role files in `docs/agents/roles/`.
- Subagents cannot spawn subagents. For a middle-manager pattern, run separate sessions in git worktrees (see `docs/agents.md`).
