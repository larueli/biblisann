# Launch prompt: Slice 0

Use with Claude Code (`claude`, model `opusplan`) or Codex (`codex -m gpt-6.2-sol` for the planning pass). Paste everything below the line.

---

You are starting the Biblisann project. The repository already contains the requirements and the rules; do not ask me to repeat them.

1. Read `AGENTS.md`, then `docs/HANDOFF.md`, `docs/roadmap.md`, `docs/spec.md`, `docs/adr/` and `docs/agents.md`.
2. Act as the **planner** role (`docs/agents/roles/planner.md`). Do not write application code yet. Produce:
   - the Slice 0 task list, split into PR-sized tasks in dependency order, written into `docs/roadmap.md` under Slice 0;
   - any ADR you think is missing for Slice 0 (config format, migration tool details, API framework wiring, docs site), as `Proposed`;
   - the list of open decisions I must make (license, GitHub owner and repo name, Go module path, repository visibility, default language), each with your recommendation and why.
3. Stop and show me the plan. Wait for my answers on the open decisions.
4. After my approval, switch to the **implementer** role and execute tasks one at a time, each in its own worktree and branch (`NAME=0-<task> make task`, branch `feat/0-<task>`), tests first, `make check` green, handoff updated, one Conventional Commit per task. Run the security reviewer before merging anything that touches `internal/safehttp`, config secret handling, the Dockerfile, `deploy/` or `.github/`.
5. Implement the releaser and publish path from the Slice 0 roadmap items (release-please config, `publish.yml`, version injection) and validate it end to end: release PR, tag, GitHub Release, image and chart on GHCR with signature, SBOM and provenance.

Constraints that always apply:
- Standard library first; any new dependency must be verified on its registry and get a row in `docs/adr/0005-dependency-policy.md`. Never add a package just because its name came to mind.
- Pin GitHub Actions by commit SHA and tool versions explicitly in Slice 0.
- Do not push to `main`. Do not weaken tests or linters. Never use `--no-verify`.
- Work only in a task worktree, including for the planning pass (`TASK_TYPE=docs NAME=0-plan make task`). If you start in the primary checkout, create the worktree, print its path and stop.
- Use Conventional Commits and Conventional PR titles; never edit version numbers or the changelog (the releaser owns them).
- If something in the spec is ambiguous in a way that changes the data model or auth model, stop and ask.

Report when the plan is ready, and again after each task: what changed, how you verified it, what remains.
