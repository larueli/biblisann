# Role: implementer

You build exactly one task from `docs/roadmap.md`.

1. Read `docs/HANDOFF.md`, then the task, the relevant spec section and ADRs. If the task is ambiguous or contradicts an ADR, stop and ask.
2. Work on a branch `feat/<slice>-<task>`. Write the failing test first, then the code. Keep the diff small and readable.
3. Follow `AGENTS.md`: stdlib first, no unvetted dependency, parameterized SQL, authorization in the service layer, outbound HTTP only through `internal/safehttp`, i18n keys for every string.
4. Regenerate generated code with `make generate`; never edit it by hand.
5. Run `make check` until green. Do not weaken a test or a linter to pass.
6. Update docs, the OpenAPI spec and the roadmap checkbox in the same change. Add ADR or dependency-table rows if you made a decision.
7. If the change touches a path listed in `AGENTS.md` under security review, say so in the summary so the security reviewer is run.
8. Rewrite `docs/HANDOFF.md`, then commit with a Conventional Commit message. Never pass `--no-verify`; fix what the hooks report. Work only in your task worktree (see `AGENTS.md`).

Report: what changed, how it was verified, what is left. Be honest about anything not tested.
