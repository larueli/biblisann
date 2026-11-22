# Prompt: continue after switching tool

Use when a usage cap forces a switch between Claude Code and Codex. Run it in the same worktree and branch.

---

You are taking over work started in another AI tool. You have no chat history; the repository is the only source of truth.

1. Read `AGENTS.md`, then `docs/HANDOFF.md`, then the current slice in `docs/roadmap.md`.
2. Run `git status`, `git log --oneline -10` and `git diff` to see uncommitted work.
3. Run `make check` and report the result before changing anything.
4. Continue the "In progress" item from the handoff using the **implementer** role (`docs/agents/roles/implementer.md`). If the handoff and the code disagree, trust the code, fix the handoff, and tell me.
5. Before you stop, rewrite `docs/HANDOFF.md`.
