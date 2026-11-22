# 0007. Agent workflow and tool neutrality

Date: 2026-11-22
Status: Accepted

## Context

Several AI tools will work on the project (Claude Code, Codex, possibly others) and the maintainer may switch when a usage cap is reached.

## Decision

- `AGENTS.md` is canonical. `CLAUDE.md` imports it. Tool-specific folders contain thin wrappers only.
- Role instructions live once in `docs/agents/roles/`; `.claude/agents/` and `.codex/agents/` point to them.
- Project state lives in the repo (roadmap checkboxes, ADRs, `docs/HANDOFF.md`), never only in chat.
- All gates run through `make check`, identical locally and in CI.
- Planning and review use the strongest model; implementation uses the mid-tier; read-only exploration uses the cheapest. A different model family reviews risky changes.

## Consequences

Switching tools costs one prompt (`docs/prompts/switch-tool.md`). Model IDs change often, so they are confined to the agent wrapper files.
