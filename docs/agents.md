# Working with AI agents

Tool-neutral rules are in `AGENTS.md`. This page explains roles, models, parallel work and switching tools (Claude Code and Codex).

## Roles and model mapping

Role instructions are written once in `docs/agents/roles/`. The files in `.claude/agents/` and `.codex/agents/` are thin wrappers that point to them and set the model.

| Role | Job | Claude Code | Codex |
|------|-----|-------------|-------|
| planner | Plans a slice, drafts ADRs, splits tasks. No feature code. | `opus` | `gpt-6.2-sol`, high effort |
| implementer | Builds one task, tests first, small diff. | `sonnet` | `gpt-6.1-terra`, medium effort |
| security-reviewer | Read-only review of risky changes. | `opus` | `gpt-6.2-sol`, high effort |
| explorer | Read-only repo search and summaries. | `haiku` | `gpt-6-luna`, low effort |
| docs-writer | User and developer docs, i18n docs. | `sonnet` | `gpt-6.1-terra`, medium effort |

Tier equivalence used here (a judgment by price and positioning, not a benchmark result):

| Claude | Codex |
|--------|-------|
| Fable | `gpt-6.2-astra` |
| Opus | `gpt-6.2-sol` |
| Sonnet | `gpt-6.1-terra` |
| Haiku | `gpt-6-luna` |

Notes:

- The top tier (Fable, Astra) is not a default seat. Escalate to it for the hardest work: the OAuth authorization server, the permission model, the data model, and a second security review of `internal/auth` and MCP authorization (`codex -m gpt-6.2-astra`).
- There is no plain `gpt-6.2` ID. GPT-6.2 exists as `gpt-6.2-astra` and `gpt-6.2-sol`. Terra is still on the 6.1 generation (`gpt-6.1-terra`, Codex CLI 0.159.0 or later) and Luna on the 6 generation (`gpt-6-luna`). Run `/model` in Codex to see what your account offers.
- If a Terra task fails review twice, rerun it on Sol instead of iterating.
- Re-check the equivalence after the first slice by comparing review findings per model, and adjust the wrapper files if it does not hold.
- Model IDs change often. They live only in the wrapper files, nowhere else.

## One worktree per task

Rule (also in `AGENTS.md`): no edits in the primary checkout. Each task or session runs in its own linked worktree on its own branch, so parallel agents never collide and the primary checkout stays clean.

```bash
NAME=0-config make task                  # creates ../biblisann-wt/0-config on branch feat/0-config
NAME=0-config AGENT=claude make task     # same, then starts claude (or codex) inside it
```

- An agent can create the worktree itself with `make task`, but a running session cannot move its own working directory. If it finds itself in the primary checkout it creates the worktree, prints the path and stops; you start the next session there. Starting through `AGENT=...` avoids that hand-off.
- Codex CLI has no built-in worktree management. In the Codex desktop app, pick "Worktree" for a thread. Both end up as plain git worktrees, so the branch and hooks behave the same.
- Hooks installed by `make hooks` live in the shared git directory and apply to every worktree.
- After merging: `git worktree remove ../biblisann-wt/<name>` and `git branch -d feat/<name>`. Never delete a worktree you did not create.

## Typical flow for one task

1. **Plan** (planner, once per slice): tasks in `docs/roadmap.md`, ADRs for new decisions.
2. **Implement** (implementer): one branch, tests first, `make check` green, handoff updated.
3. **Review** (security-reviewer for risky paths, and a review by the *other* tool for anything non-trivial, see `docs/prompts/review.md`).
4. **Human merges** after reading the diff.

## Claude Code

- `.claude/settings.json` sets `opusplan`: Opus plans, Sonnet executes.
- Subagents have their own context and cannot spawn subagents. Give them a self-contained brief.
- Every task gets its own git worktree (see "One worktree per task" below).
- Claude Code also has `claude --worktree <name>` (`-w`), gives each desktop-app session its own worktree, and lets a custom subagent set `isolation: worktree` in its frontmatter. Use the last one only for parallel subagents that edit disjoint files.

## Codex

- `.codex/config.toml` sets the default model and agent limits. Project config is only loaded for trusted projects.
- Custom agents are `.codex/agents/*.toml` with `name`, `description`, `developer_instructions`, optional `model` and `model_reasoning_effort`. Sandbox and approval settings are inherited from the parent session; set them there.
- Codex only spawns subagents when asked. `AGENTS.md` contains that ask.

## Switching tool when a usage cap is hit

1. Get `make check` green or stash the work, commit on the feature branch.
2. Update `docs/HANDOFF.md` (done, in progress, next, gotchas).
3. Start the other tool in the same worktree with `docs/prompts/switch-tool.md`.

## Cost control

- Keep the expensive model for planning and review. Do not let it grind through implementation.
- Prefer many small tasks with a clear checklist to one long session.
- Use the explorer for "where is X" questions instead of loading many files into the main context.
